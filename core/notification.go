package core

import (
	"strings"
	"time"
)

// channelNotificationIDPrefix makes notification IDs self-describing. MyGo
// passes the ID back to App.OnNotificationClick, including after a cold
// launch, so the app can route a click to the channel that produced it.
const channelNotificationIDPrefix = "ytreader-channel-"

// NotificationClick is a local handoff record for a notification posted while
// the app was not running. macOS gives AppleScript notifications no click ID,
// so the helper records the channel and time locally for the launching app.
type NotificationClick struct {
	ID        string `json:"id"`
	ChannelID string `json:"channelID"`
	Timestamp string `json:"timestamp"`
}

// NotificationClicks is the atomic local queue of notifications that may have
// been clicked while the app was quit. It is never included in sync state.
type NotificationClicks struct {
	Clicks []NotificationClick `json:"clicks"`
}

// ChannelNotificationID returns the stable notification ID for a channel.
func ChannelNotificationID(channelID string) string {
	return channelNotificationIDPrefix + channelID
}

// ChannelFromNotificationID decodes a notification ID produced by
// ChannelNotificationID. It rejects unrelated or malformed IDs.
func ChannelFromNotificationID(id string) (string, bool) {
	if !strings.HasPrefix(id, channelNotificationIDPrefix) {
		return "", false
	}
	channelID := strings.TrimPrefix(id, channelNotificationIDPrefix)
	if channelID == "" || strings.ContainsAny(channelID, "/\\\n\r\t ") {
		return "", false
	}
	return channelID, true
}

// NewNotificationClick builds one deduplicable handoff entry from a
// notification plan and the time it was posted.
func NewNotificationClick(plan NotificationPlan, at time.Time) NotificationClick {
	return NotificationClick{
		ID:        ChannelNotificationID(plan.ChannelID),
		ChannelID: plan.ChannelID,
		Timestamp: FormatRFC3339(at),
	}
}

// NewNotificationClicks builds one handoff entry per notification plan.
func NewNotificationClicks(plans []NotificationPlan, at time.Time) []NotificationClick {
	clicks := make([]NotificationClick, 0, len(plans))
	for _, plan := range plans {
		if plan.ChannelID == "" {
			continue
		}
		clicks = append(clicks, NewNotificationClick(plan, at))
	}
	return clicks
}

// LatestNotificationClick returns the most recent queued click and the queue
// containing every older entry. Ties keep the last queued entry. Entries with
// unparsable timestamps sort before valid ones so they cannot override a newer
// click, but remain in the returned tail for dedupe.
func LatestNotificationClick(queue NotificationClicks) (NotificationClick, NotificationClicks) {
	if len(queue.Clicks) == 0 {
		return NotificationClick{}, NotificationClicks{}
	}
	best := 0
	bestTime := time.Time{}
	for i, click := range queue.Clicks {
		at, err := time.Parse(time.RFC3339, click.Timestamp)
		if err == nil && (bestTime.IsZero() || !at.Before(bestTime)) {
			best, bestTime = i, at
		}
	}
	tail := NotificationClicks{Clicks: make([]NotificationClick, 0, len(queue.Clicks)-1)}
	tail.Clicks = append(tail.Clicks, queue.Clicks[:best]...)
	tail.Clicks = append(tail.Clicks, queue.Clicks[best+1:]...)
	return queue.Clicks[best], tail
}

// NotificationClickKey is the local dedupe key for one posted notification.
func NotificationClickKey(click NotificationClick) string {
	return click.ID + "\x00" + click.Timestamp
}
