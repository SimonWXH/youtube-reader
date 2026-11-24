package core

import (
	"context"
	"errors"
	"strings"
)

// Reading a channel's history is what the channel page's "load more" control
// does. A refresh only ever reads the uploads of the last 30 days, so a channel
// that posts rarely holds nothing at all after a refresh; this reads one page
// further back on each click instead, and the store keeps what it brought.

// BackfillCursor is where a channel's history paging stands.
type BackfillCursor struct {
	// Started is false until a page has been read, which is what tells a
	// channel nobody has paged from one whose earliest video is loaded.
	Started bool
	// Token reads the page before the ones already read, empty once the
	// channel has no earlier upload.
	Token string
}

// Complete reports whether the cursor has reached the channel's earliest video.
func (c BackfillCursor) Complete() bool { return c.Started && c.Token == "" }

// ErrBackfillExhausted is a paging click for a channel whose earliest video the
// store already has. The channel page offers no button then; this guards the
// path that would otherwise re-read the newest page.
var ErrBackfillExhausted = errors.New("no earlier videos")

// BackfillResult is one read of a channel's history: the long videos the page
// held, and the token that reads the page before it, empty at the channel's
// earliest video.
type BackfillResult struct {
	Videos        []Video `json:"videos"`
	NextPageToken string  `json:"nextPageToken"`
}

// Complete reports whether this page reached the channel's earliest video.
func (r BackfillResult) Complete() bool { return r.NextPageToken == "" }

// BackfillChannel reads one page of a channel's older uploads, the page token
// names. A page costs two quota units: one playlistItems.list call for up to
// PlaylistPageSize uploads and one videos.list call that hydrates their
// durations.
//
// Only long videos are kept, the rule every listing here follows: a Short is
// not what a reader paging through a channel's history is looking for, and its
// duration is what identifies it. A page whose hydration fails reads as a
// failure rather than as a page of videos of unknown length, so the caller
// keeps its token and a retry reads the same page.
//
// The channel's uploads playlist normally arrives with its metadata; a channel
// whose metadata no refresh has fetched yet costs one more unit to look up.
func BackfillChannel(ctx context.Context, client YouTube, channel Channel, token string) (BackfillResult, error) {
	if channel.ID == "" {
		return BackfillResult{}, errors.New("no channel to page")
	}
	uploads := channel.UploadedAt
	if strings.TrimSpace(uploads) == "" {
		infos, err := client.ChannelInfo(ctx, []string{channel.ID})
		if err != nil {
			return BackfillResult{}, err
		}
		for _, info := range infos {
			if info.ID == channel.ID {
				uploads = info.UploadedAt
				break
			}
		}
		if uploads == "" {
			return BackfillResult{}, ErrChannelNotFound
		}
	}
	page, next, err := client.PlaylistPage(ctx, uploads, token)
	if err != nil {
		return BackfillResult{}, err
	}
	for i := range page {
		page[i].ChannelID = channel.ID
		page[i].ChannelTitle = channel.Title
		page[i].AvatarURL = channel.AvatarURL
		page[i].IsBackfilled = true
	}
	page = DedupeVideos(page)
	if page, err = hydrateDurations(ctx, client, page); err != nil {
		return BackfillResult{}, err
	}
	long := make([]Video, 0, len(page))
	for _, v := range page {
		v.IsShort = IsShort(v.Duration, v.Width, v.Height)
		if v.IsShort {
			continue
		}
		long = append(long, v)
	}
	return BackfillResult{Videos: long, NextPageToken: next}, nil
}
