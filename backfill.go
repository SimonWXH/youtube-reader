package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
	mui "github.com/simon/youtube-reader/ui"
)

// Reading a channel's history is the channel page's "load more" action, which the
// empty channel pane offers too: a refresh reads only the last 30 days of
// uploads, so a channel that posts rarely shows nothing at all. One click reads
// one page further back with two quota units, commits it to the local store, and
// advances the page token, so a repeated click never pays for the same page and a
// later refresh never drops what it brought.

// BackfillTarget is the channel record a history page pages through. The sync
// record holds the title and avatar the sidebar shows, and the store's row holds
// the uploads playlist once any fetch learned it.
func (a *App) BackfillTarget(channelID string) (core.Channel, bool) {
	channel, ok := a.Sync.Channels[channelID]
	if !ok {
		return core.Channel{}, false
	}
	for _, stored := range a.Channels {
		if stored.ID != channelID {
			continue
		}
		if channel.Title == "" {
			channel.Title = stored.Title
		}
		if channel.AvatarURL == "" {
			channel.AvatarURL = stored.AvatarURL
		}
		if channel.UploadedAt == "" {
			channel.UploadedAt = stored.UploadedAt
		}
		break
	}
	channel.ID = channelID
	return channel, true
}

// LoadEarlierVideos reads one page of a channel's older uploads and commits it to
// the local store, then advances the channel's saved page token. It runs off the
// main thread and writes no app field: channel and cursor are read from the app
// before it starts, and the caller reloads on the main thread so the frames that
// read the app never see a half-applied page.
//
// The videos are committed before the token moves. A failure or a quit between
// the two re-reads the same page on the next click, and the videos' primary key
// makes that repeat harmless: no duplicate row, no change to whether a video was
// read, and no favorite touched.
func (a *App) LoadEarlierVideos(ctx context.Context, channel core.Channel, cursor core.BackfillCursor) (core.BackfillResult, error) {
	if channel.ID == "" {
		return core.BackfillResult{}, errors.New("no channel to page")
	}
	if cursor.Complete() {
		return core.BackfillResult{}, core.ErrBackfillExhausted
	}
	settings := a.settingsSnapshot()
	if settings.APIKey == "" {
		return core.BackfillResult{}, errors.New("a YouTube API key is needed to read a channel's history")
	}
	if until, err := time.Parse(time.RFC3339, settings.QuotaPausedUntil); err == nil && time.Now().Before(until) {
		return core.BackfillResult{}, core.ErrQuota
	}
	result, err := core.BackfillChannel(ctx, appYouTubeClient(settings.APIKey), channel, cursor.Token)
	if err != nil {
		if errors.Is(err, core.ErrQuota) {
			a.NoteQuotaPause(time.Now())
		}
		return result, err
	}
	if a.Store == nil {
		return result, nil
	}
	if err := a.Store.CommitRefresh(ctx, core.RefreshCommit{Videos: result.Videos, Revision: a.storeRevision(ctx)}); err != nil {
		return result, err
	}
	if err := a.Store.SetBackfillCursor(ctx, channel.ID, result.NextPageToken); err != nil {
		return result, err
	}
	return result, nil
}

// storeRevision is the store's own revision counter, read off the main thread so
// a commit never has to read the frame's copy of it.
func (a *App) storeRevision(ctx context.Context) int64 {
	var revision int64
	if a.Store == nil {
		return revision
	}
	if raw := a.Store.Meta(ctx, "revision"); raw != "" {
		_, _ = fmt.Sscan(raw, &revision)
	}
	return revision
}

// startBackfill reads one page of a channel's earlier uploads off the main
// thread. The reader's click is the only trigger, and a second click while the
// page is in flight is dropped rather than read twice.
func (w *Window) startBackfill(channelID string) {
	if w.app == nil || channelID == "" || w.app.BackfillBusy != "" {
		return
	}
	if w.offline || w.app.refreshBlocked(time.Now()) {
		return
	}
	channel, ok := w.app.BackfillTarget(channelID)
	if !ok {
		return
	}
	cursor := w.app.BackfillCursors[channelID]
	if cursor.Complete() {
		return
	}
	locale := w.locale()
	w.app.BackfillBusy = channelID
	w.app.BackfillErr = ""
	w.invalidateMainWindow()
	go func() {
		result, err := w.app.LoadEarlierVideos(context.Background(), channel, cursor)
		mygo.RunOnMain(func() {
			w.app.BackfillBusy = ""
			switch {
			case err != nil:
				w.app.BackfillErr = backfillFailureText(err, locale)
			default:
				w.app.BackfillErr = ""
				_ = w.app.reload()
				if len(result.Videos) == 0 && !result.Complete() {
					// The page was read and kept, but it held no long video:
					// say so, so the unchanged page is not a silent click.
					w.toast(nil, mui.ChannelHistoryCopy(locale).EmptyPage, false)
				}
			}
			w.invalidateMainWindow()
		})
	}()
}

// backfillFailureText is what a failed history page reports: the failure the app
// can name, or the recorded reason redacted of the API key it may quote.
func backfillFailureText(err error, locale string) string {
	words := mui.ChannelHistoryCopy(locale)
	switch {
	case errors.Is(err, core.ErrQuota):
		return words.Quota
	case errors.Is(err, core.ErrChannelNotFound):
		return words.NoUploads
	case errors.Is(err, core.ErrBackfillExhausted):
		return words.Loaded
	}
	reason := core.RedactKey(err.Error())
	if reason == "" {
		return words.Failed
	}
	return words.Failed + ": " + reason
}

// historyDisabledReason says whether the channel page's history control cannot
// run and why: a page is already in flight, the machine is offline, or no refresh
// could run either because the key is missing or the quota is paused. The tooltip
// names what the app can say; a paused quota keeps its reason on the status
// surface.
func (w *Window) historyDisabledReason(locale string) (bool, string) {
	switch {
	case w.app == nil:
		return true, ""
	case w.app.BackfillBusy != "":
		return true, mui.ChannelHistoryCopy(locale).Loading
	case w.offline:
		return true, mui.OfflineTooltip
	case w.app.refreshBlocked(time.Now()):
		return true, ""
	}
	return false, ""
}

// locale is the language the window draws in, for text built outside a frame.
func (w *Window) locale() string {
	if w.frame != nil && w.frame.Valid() {
		return w.frame.Locale()
	}
	return ""
}

// channelHistoryFooter draws the channel page's history control under the grid's
// last row. It appears only on a channel's own page: every other destination
// mixes channels, and a page belongs to one.
func (w *Window) channelHistoryFooter(c *ui.Context, t mui.Tokens, channelID string, width float32) {
	locale := c.Locale()
	cursor := w.app.BackfillCursors[channelID]
	disabled, tooltip := w.historyDisabledReason(locale)
	ui.Column(c).Shrink(0).Padding(mui.Space(3), 0, 0, 0).Children(func() {
		mui.BackfillControl(c, t, mui.BackfillSpec{
			Key:      "channel-history",
			Stage:    mui.BackfillStageFor(cursor.Complete(), w.app.BackfillBusy == channelID),
			PageSize: core.PlaylistPageSize,
			Locale:   locale,
			Width:    width,
			Note:     w.app.BackfillErr,
			Disabled: disabled,
			Tooltip:  tooltip,
			OnLoad:   func() { w.startBackfill(channelID) },
		})
	})
}
