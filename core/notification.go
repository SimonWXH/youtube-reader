package core

import (
	"fmt"
	"strings"
	"time"
)

// channelNotificationIDPrefix makes notification IDs self-describing. MyGo
// passes the ID back to App.OnNotificationClick, including after a cold
// launch, so the app can route a click to the channel that produced it.
const channelNotificationIDPrefix = "ytreader-channel-"

// NotificationClick is a local record of a notification the helper actually
// posted while the app was not running. It is only consumed when macOS sends
// an actual notification-click callback, never on an ordinary launch.
type NotificationClick struct {
	ID        string `json:"id"`
	ChannelID string `json:"channelID"`
	Timestamp string `json:"timestamp"`
}

// NotificationClicks is the atomic local queue of helper-posted notifications
// that may produce a click callback. It is never included in sync state.
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

// NewNotificationClick builds one deduplicable record from a notification
// plan and the time it was posted.
func NewNotificationClick(plan NotificationPlan, at time.Time) NotificationClick {
	return NotificationClick{
		ID:        ChannelNotificationID(plan.ChannelID),
		ChannelID: plan.ChannelID,
		Timestamp: FormatRFC3339(at),
	}
}

// NewNotificationClicks builds one record per notification plan.
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

// ValidateNotificationClick rejects records that cannot safely route a click.
// A corrupt record must not be treated as a wildcard or skipped silently.
func ValidateNotificationClick(click NotificationClick) error {
	channelID, ok := ChannelFromNotificationID(click.ID)
	if !ok {
		return fmt.Errorf("invalid notification ID %q", click.ID)
	}
	if click.ChannelID == "" || channelID != click.ChannelID {
		return fmt.Errorf("notification ID %q does not match channel %q", click.ID, click.ChannelID)
	}
	if _, err := time.Parse(time.RFC3339, click.Timestamp); err != nil {
		return fmt.Errorf("invalid notification timestamp %q: %w", click.Timestamp, err)
	}
	return nil
}

// Validate rejects a queue containing any corrupt or duplicate record.
func (queue NotificationClicks) Validate() error {
	known := make(map[string]bool, len(queue.Clicks))
	for _, click := range queue.Clicks {
		if err := ValidateNotificationClick(click); err != nil {
			return err
		}
		key := NotificationClickKey(click)
		if known[key] {
			return fmt.Errorf("duplicate notification record %q", key)
		}
		known[key] = true
	}
	return nil
}

// LatestNotificationClick returns the most recent queued record and the queue
// containing every older entry. Ordering uses the actual post time; the
// notification ID is only a same-time tie-breaker. Corrupt queues are rejected
// instead of being partially consumed.
func LatestNotificationClick(queue NotificationClicks) (NotificationClick, NotificationClicks, error) {
	if len(queue.Clicks) == 0 {
		return NotificationClick{}, NotificationClicks{}, nil
	}
	if err := queue.Validate(); err != nil {
		return NotificationClick{}, queue, err
	}
	best := 0
	bestAt, _ := time.Parse(time.RFC3339, queue.Clicks[best].Timestamp)
	for i := 1; i < len(queue.Clicks); i++ {
		at, _ := time.Parse(time.RFC3339, queue.Clicks[i].Timestamp)
		if at.After(bestAt) || (at.Equal(bestAt) && NotificationClickKey(queue.Clicks[i]) > NotificationClickKey(queue.Clicks[best])) {
			best = i
			bestAt = at
		}
	}
	tail := NotificationClicks{Clicks: make([]NotificationClick, 0, len(queue.Clicks)-1)}
	tail.Clicks = append(tail.Clicks, queue.Clicks[:best]...)
	tail.Clicks = append(tail.Clicks, queue.Clicks[best+1:]...)
	return queue.Clicks[best], tail, nil
}

// NotificationClickKey is the local dedupe key for one posted notification.
// It starts with the RFC3339 timestamp so lexical and chronological order
// agree, and uses the ID only to separate records posted at the same second.
func NotificationClickKey(click NotificationClick) string {
	return click.Timestamp + "\x00" + click.ID
}
