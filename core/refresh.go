package core

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
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
		msg := err.Error()
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
			failures = append(failures, RefreshFailure{At: FormatRFC3339(now), Message: ch.Title + ": " + err.Error()})
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
		for _, v := range videos {
			v.ChannelID = id
			v.ChannelTitle = ch.Title
			v.AvatarURL = ch.AvatarURL
			v.IsShort = IsShort(v.Duration, v.Width, v.Height)
			if !existingIDs[v.ID] {
				newCount++
				if newest == "" {
					newest = v.Title
				}
			}
			all = append(all, v)
		}
		if newCount > 0 && notificationEnabled(settings, id) {
			notifications = append(notifications, NotificationPlan{ChannelID: id, ChannelName: ch.Title, Count: newCount, NewestTitle: newest})
		}
	}
	all = DedupeVideos(all)
	missing := []Video{}
	for _, v := range all {
		if v.Duration == "" {
			missing = append(missing, v)
		}
	}
	if len(missing) > 0 {
		hydrated, err := client.Hydrate(ctx, missing)
		if err != nil {
			if errors.Is(err, ErrQuota) {
				return failed(err)
			}
			failures = append(failures, RefreshFailure{At: FormatRFC3339(now), Message: "hydrate: " + err.Error()})
		} else {
			byID := map[string]Video{}
			for _, v := range hydrated {
				byID[v.ID] = v
			}
			for i, v := range all {
				if hv, ok := byID[v.ID]; ok {
					hv.ChannelID = v.ChannelID
					hv.ChannelTitle = v.ChannelTitle
					hv.AvatarURL = v.AvatarURL
					if hv.ThumbnailURL == "" {
						hv.ThumbnailURL = v.ThumbnailURL
					}
					all[i] = hv
				}
			}
		}
	}
	protected := func(v Video) bool { return !state.IsRead(v.ID) || state.IsFavorite(v.ID) }
	all = ApplyRetention(all, protected, now, DefaultRetention())

	result := RefreshResult{
		Videos: all, Channels: channels, Failures: failures, Notified: notifications,
		Changed: true, Description: fmt.Sprintf("Refreshed %d videos", len(all)),
		Revision: revision + 1,
	}
	if store != nil {
		if err := store.CommitRefresh(ctx, RefreshCommit{Channels: channels, Videos: all, Revision: revision + 1}); err != nil {
			return failed(err)
		}
	}
	return result
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
