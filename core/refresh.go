package core

import (
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"
	"time"
)

// NotificationPlan is the local notification intent produced by a refresh.
// Both the app and the helper create it from the same new-video detection.
type NotificationPlan struct {
	ChannelID   string `json:"channelID"`
	ChannelName string `json:"channelName"`
	Count       int    `json:"count"`
	NewestTitle string `json:"newestTitle"`
}

// VideoNotification records that a video's arrival was announced. It lives
// beside the refresh rather than among the retained videos, so retention's
// window and video cap cannot make an announced video look new again.
type VideoNotification struct {
	VideoID    string `json:"videoID"`
	NotifiedAt int64  `json:"notifiedAt"`
}

// notifiedVideoRetention is how long an announced video stays known. It is far
// longer than the 30-day uploads window every refresh reads, so a video that a
// later refresh can still fetch is always still known as announced.
const notifiedVideoRetention = 45 * 24 * time.Hour

// RefreshResult carries everything a refresh caller needs: data is committed
// to the local Store by RefreshYoutube; callers own settings and notifications.
type RefreshResult struct {
	Videos      []Video            `json:"videos"`
	Channels    []Channel          `json:"channels"`
	Failures    []RefreshFailure   `json:"failures"`
	Notified    []NotificationPlan `json:"notifications"`
	Description string             `json:"description"`
	Changed     bool               `json:"changed"`
	Revision    int64              `json:"revision"`
}

// RefreshYoutube performs one full refresh using the same API sequence,
// quota rules, Shorts logic and retention policy for the app and helper.
// store may be nil; then the result is computed without committing.
// now is injectable for tests; pass time.Now() in production.
func RefreshYoutube(ctx context.Context, client YouTube, settings Settings, state SyncState, store *Store, existing []Video, revision int64, force bool, now time.Time) RefreshResult {
	failed := func(err error) RefreshResult {
		msg := RedactKey(err.Error())
		if errors.Is(err, ErrQuota) {
			msg = "quota paused until " + FormatRFC3339(now.Add(24*time.Hour))
		}
		f := RefreshFailure{At: FormatRFC3339(now), Message: msg}
		failures := append([]RefreshFailure{f}, settings.RecentFailures...)
		if len(failures) > 5 {
			failures = failures[:5]
		}
		return RefreshResult{Failures: failures, Description: msg}
	}
	if settings.APIKey == "" {
		return RefreshResult{Description: "Add a YouTube API key in Settings to refresh."}
	}
	if !force && settings.LastRefresh != "" {
		if last, err := time.Parse(time.RFC3339, settings.LastRefresh); err == nil && now.Sub(last) < time.Duration(settings.RefreshMinutes)*time.Minute {
			return RefreshResult{Description: "Up to date"}
		}
	}
	if paused, err := time.Parse(time.RFC3339, settings.QuotaPausedUntil); err == nil && now.Before(paused) {
		return RefreshResult{Description: "Quota paused until " + settings.QuotaPausedUntil}
	}

	ids := make([]string, 0, len(state.Channels))
	for id := range state.Channels {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	notifiedIDs := map[string]bool{}
	if store != nil {
		var err error
		if notifiedIDs, err = store.NotifiedVideoIDs(ctx); err != nil {
			return failed(err)
		}
	}
	infos, err := client.ChannelInfo(ctx, ids)
	if err != nil {
		return failed(err)
	}
	byID := map[string]Channel{}
	for _, ch := range infos {
		byID[ch.ID] = ch
	}
	channels := make([]Channel, 0, len(state.Channels))
	for id, ch := range state.Channels {
		if info, ok := byID[id]; ok {
			ch.AvatarURL = info.AvatarURL
			if info.Title != "" {
				ch.Title = info.Title
			}
			ch.UploadedAt = info.UploadedAt
			if info.UpdatedAt > ch.UpdatedAt {
				ch.UpdatedAt = info.UpdatedAt
			}
			state.Channels[id] = ch
		}
		channels = append(channels, ch)
	}
	sort.Slice(channels, func(i, j int) bool { return channels[i].ID < channels[j].ID })

	all := []Video{}
	failures := []RefreshFailure{}
	notifications := []NotificationPlan{}
	announced := []VideoNotification{}
	for _, id := range ids {
		ch := state.Channels[id]
		uploads := ch.UploadedAt
		if info, ok := byID[id]; ok {
			uploads = info.UploadedAt
		}
		if uploads == "" {
			failures = append(failures, RefreshFailure{At: FormatRFC3339(now), Message: "no uploads playlist for " + ch.Title})
			continue
		}
		videos, err := client.PlaylistVideos(ctx, uploads, 3)
		if err != nil {
			if errors.Is(err, ErrQuota) {
				return failed(err)
			}
			failures = append(failures, RefreshFailure{At: FormatRFC3339(now), Message: RedactKey(ch.Title + ": " + err.Error())})
			continue
		}
		existingIDs := map[string]bool{}
		for _, v := range existing {
			if v.ChannelID == id {
				existingIDs[v.ID] = true
			}
		}
		newCount := 0
		newest := ""
		newIDs := []string{}
		for _, v := range videos {
			v.ChannelID = id
			v.ChannelTitle = ch.Title
			v.AvatarURL = ch.AvatarURL
			v.IsShort = IsShort(v.Duration, v.Width, v.Height)
			// A video counts as new only while it has never been stored,
			// never been announced and never been read: retention may drop
			// its row from the store, and that must not re-announce it.
			if !existingIDs[v.ID] && !notifiedIDs[v.ID] && !state.IsRead(v.ID) {
				newCount++
				newIDs = append(newIDs, v.ID)
				if newest == "" {
					newest = v.Title
				}
			}
			all = append(all, v)
		}
		if newCount > 0 && notificationEnabled(settings, id) {
			notifications = append(notifications, NotificationPlan{ChannelID: id, ChannelName: ch.Title, Count: newCount, NewestTitle: newest})
			for _, videoID := range newIDs {
				announced = append(announced, VideoNotification{VideoID: videoID, NotifiedAt: now.UnixMilli()})
			}
		}
	}
	all = DedupeVideos(all)
	all, err = hydrateDurations(ctx, client, all)
	if err != nil {
		if errors.Is(err, ErrQuota) {
			return failed(err)
		}
		failures = append(failures, RefreshFailure{At: FormatRFC3339(now), Message: RedactKey("hydrate: " + err.Error())})
	}
	protected := func(v Video) bool { return !state.IsRead(v.ID) || state.IsFavorite(v.ID) }
	all = ApplyRetention(all, protected, now, DefaultRetention())

	result := RefreshResult{
		Videos: all, Channels: channels, Failures: failures, Notified: notifications,
		Changed: true, Description: fmt.Sprintf("Refreshed %d videos", len(all)),
		Revision: revision + 1,
	}
	if store != nil {
		if err := store.CommitRefresh(ctx, RefreshCommit{Channels: channels, Videos: all, Revision: revision + 1, Notified: announced}); err != nil {
			return failed(err)
		}
	}
	return result
}

// hydrateDurations asks videos.list for the videos a playlist listing left
// without a duration, and fills in what it brings: the duration decides whether
// a video is a Short, and the higher-resolution thumbnail that arrives with it
// replaces the playlist's own. A failed hydration leaves the videos as they
// came, so its caller reports the failure without losing the listing.
func hydrateDurations(ctx context.Context, client YouTube, videos []Video) ([]Video, error) {
	missing := []Video{}
	for _, v := range videos {
		if v.Duration == "" {
			missing = append(missing, v)
		}
	}
	if len(missing) == 0 {
		return videos, nil
	}
	hydrated, err := client.Hydrate(ctx, missing)
	if err != nil {
		return videos, err
	}
	byID := map[string]Video{}
	for _, v := range hydrated {
		byID[v.ID] = v
	}
	out := append([]Video(nil), videos...)
	for i, v := range out {
		if hv, ok := byID[v.ID]; ok {
			hv.ChannelID = v.ChannelID
			hv.ChannelTitle = v.ChannelTitle
			hv.AvatarURL = v.AvatarURL
			// The mark belongs to how the video arrived, not to the metadata the
			// hydration brings, so it survives being replaced by it.
			hv.IsBackfilled = hv.IsBackfilled || v.IsBackfilled
			if hv.ThumbnailURL == "" {
				hv.ThumbnailURL = v.ThumbnailURL
			}
			out[i] = hv
		}
	}
	return out, nil
}

// FetchChannel reads one channel and its recent uploads, for the fetch the
// add-subscription dialog runs right after it adds a channel. It commits
// nothing and applies no retention: the subscription itself is already saved,
// so the caller can drop a failed fetch without touching the rest of the
// library. YouTube's quota is the failure that happens at a bad moment, and a
// channel that is added but not yet fetched is still a working subscription.
func FetchChannel(ctx context.Context, client YouTube, channel Channel) (Channel, []Video, error) {
	infos, err := client.ChannelInfo(ctx, []string{channel.ID})
	if err != nil {
		return channel, nil, err
	}
	updated := channel
	found := false
	for _, info := range infos {
		if info.ID != channel.ID {
			continue
		}
		found = true
		if info.Title != "" {
			updated.Title = info.Title
		}
		if info.AvatarURL != "" {
			updated.AvatarURL = info.AvatarURL
		}
		updated.UploadedAt = info.UploadedAt
		if info.UpdatedAt > updated.UpdatedAt {
			updated.UpdatedAt = info.UpdatedAt
		}
	}
	if !found {
		return channel, nil, ErrChannelNotFound
	}
	if strings.TrimSpace(updated.Title) == "" {
		return channel, nil, ErrChannelNotFound
	}
	if updated.UploadedAt == "" {
		// A channel with no uploads playlist has nothing to fetch; its
		// metadata is still an improvement the caller can commit.
		return updated, nil, nil
	}
	videos, err := client.PlaylistVideos(ctx, updated.UploadedAt, 3)
	if err != nil {
		return updated, nil, err
	}
	for i := range videos {
		videos[i].ChannelID = updated.ID
		videos[i].ChannelTitle = updated.Title
		videos[i].AvatarURL = updated.AvatarURL
		videos[i].IsShort = IsShort(videos[i].Duration, videos[i].Width, videos[i].Height)
	}
	videos = DedupeVideos(videos)
	videos, err = hydrateDurations(ctx, client, videos)
	if err != nil {
		return updated, videos, err
	}
	for i := range videos {
		videos[i].IsShort = IsShort(videos[i].Duration, videos[i].Width, videos[i].Height)
	}
	return updated, videos, nil
}

// FailedOnQuota reports whether a refresh's own failure is the quota failure.
// RefreshYoutube puts the failure the attempt produced first, so the retained
// history a failed refresh carries never reads as this attempt's quota
// failure; the settings' quota pause is armed from this answer alone.
func FailedOnQuota(failures []RefreshFailure) bool {
	return len(failures) > 0 && strings.HasPrefix(failures[0].Message, "quota paused")
}

// apiKeyQuery matches the key parameter Go's request errors quote in the full
// request URL, so a failure message never carries the key into settings, logs
// or the status surface.
var apiKeyQuery = regexp.MustCompile(`(?i)([?&]key=)[^&\s"]*`)

// RedactKey hides the API key from a message taken from a network error, whose
// text quotes the whole request URL. It is exported so every reader of a stored
// failure, including one written by an older build, redacts at display time.
func RedactKey(message string) string {
	return apiKeyQuery.ReplaceAllString(message, "${1}[redacted]")
}

func notificationEnabled(settings Settings, channelID string) bool {
	for _, id := range settings.NotificationIDs {
		if id == channelID {
			return true
		}
	}
	return false
}

// NetworkAvailable performs a short local network check before a helper run.
// It never calls YouTube when the machine is offline.
func NetworkAvailable(ctx context.Context) bool {
	// A plain UDP dial does not send packets but asks the OS to choose a route.
	conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "udp", "8.8.8.8:53")
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
