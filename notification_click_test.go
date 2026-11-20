package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/simon/youtube-reader/core"
)

func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return at
}

func writeNotificationClickQueue(t *testing.T, dir string, clicks ...core.NotificationClick) {
	t.Helper()
	data, err := json.Marshal(core.NotificationClicks{Clicks: clicks})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notification-clicks.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestConsumeNotificationClickRoutesNewestChannel(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	a.Channels = []core.Channel{{ID: "ch-1"}, {ID: "ch-2"}}
	a.Videos = []core.Video{{ID: "v-1", ChannelID: "ch-1"}, {ID: "v-2", ChannelID: "ch-2"}}
	a.ChannelFilter = "ch-1"
	old := core.NewNotificationClick(core.NotificationPlan{ChannelID: "ch-1"}, mustTime(t, "2026-03-01T11:00:00Z"))
	newest := core.NewNotificationClick(core.NotificationPlan{ChannelID: "ch-2"}, mustTime(t, "2026-03-01T12:00:00Z"))
	writeNotificationClickQueue(t, dir, old, newest)

	got, ok := a.ConsumeNotificationClick()
	if !ok || got != "ch-2" {
		t.Fatalf("consume = %q, %v", got, ok)
	}
	if !a.SelectChannel(got) || a.Selected != "channel:ch-2" {
		t.Fatalf("selection = %q", a.Selected)
	}
	if a.ChannelFilter != "" || a.Search != "" || a.UnreadOnly || a.SelectedVideoID != "" {
		t.Fatalf("filters not cleared: %+v", a)
	}
	if got := a.VisibleVideos(); len(got) != 1 || got[0].ID != "v-2" {
		t.Fatalf("routed videos = %+v", got)
	}
	data, err := os.ReadFile(filepath.Join(dir, "notification-clicks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var queue core.NotificationClicks
	if err := json.Unmarshal(data, &queue); err != nil {
		t.Fatal(err)
	}
	if len(queue.Clicks) != 1 || queue.Clicks[0].ID != old.ID {
		t.Fatalf("remaining queue = %+v", queue.Clicks)
	}
}

func TestConsumeNotificationClickKeepsOlderRecordsAfterNewerConsumed(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	a.Channels = []core.Channel{{ID: "ch-1"}, {ID: "ch-2"}}
	older := core.NewNotificationClick(core.NotificationPlan{ChannelID: "ch-1"}, mustTime(t, "2026-03-01T11:00:00Z"))
	newer := core.NewNotificationClick(core.NotificationPlan{ChannelID: "ch-2"}, mustTime(t, "2026-03-01T12:00:00Z"))
	writeNotificationClickQueue(t, dir, older, newer)

	if got, ok := a.ConsumeNotificationClick(); !ok || got != "ch-2" {
		t.Fatalf("consume newer = %q, %v", got, ok)
	}
	if got, ok := a.ConsumeNotificationClick(); !ok || got != "ch-1" {
		t.Fatalf("consume retained older = %q, %v", got, ok)
	}
	if got, ok := a.ConsumeNotificationClick(); ok {
		t.Fatalf("empty queue consumed %q", got)
	}
}

func TestConsumeNotificationClickOrdersByPostTimeNotChannelID(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	a.Channels = []core.Channel{{ID: "ch-z"}, {ID: "ch-a"}}
	// The lexically larger channel ID is an older post and must not win.
	older := core.NewNotificationClick(core.NotificationPlan{ChannelID: "ch-z"}, mustTime(t, "2026-03-01T11:00:00Z"))
	newer := core.NewNotificationClick(core.NotificationPlan{ChannelID: "ch-a"}, mustTime(t, "2026-03-01T12:00:00Z"))
	writeNotificationClickQueue(t, dir, older, newer)
	if got, ok := a.ConsumeNotificationClick(); !ok || got != "ch-a" {
		t.Fatalf("consume by time = %q, %v", got, ok)
	}
	if got, ok := a.ConsumeNotificationClick(); !ok || got != "ch-z" {
		t.Fatalf("consume older = %q, %v", got, ok)
	}
}

func TestConsumeNotificationClickRejectsCorruptState(t *testing.T) {
	for name, data := range map[string]string{
		"bad JSON":          `{`,
		"bad ID":            `{"clicks":[{"id":"unrelated","channelID":"ch-1","timestamp":"2026-03-01T12:00:00Z"}]}`,
		"mismatched ID":     `{"clicks":[{"id":"ytreader-channel-ch-2","channelID":"ch-1","timestamp":"2026-03-01T12:00:00Z"}]}`,
		"duplicate ID":      `{"clicks":[{"id":"ytreader-channel-ch-1","channelID":"ch-1","timestamp":"2026-03-01T12:00:00Z"},{"id":"ytreader-channel-ch-1","channelID":"ch-1","timestamp":"2026-03-01T12:00:00Z"}]}`,
		"corrupt timestamp": `{"clicks":[{"id":"ytreader-channel-ch-1","channelID":"ch-1","timestamp":"bad"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			a := newTestApp(t, dir)
			defer a.Close()
			path := filepath.Join(dir, "notification-clicks.json")
			if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			if got, ok := a.ConsumeNotificationClick(); ok {
				t.Fatalf("corrupt state consumed %q", got)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != data {
				t.Fatalf("corrupt queue changed: %q, %v", after, err)
			}
			if err := a.RecordNotificationClicks([]core.NotificationClick{core.NewNotificationClick(core.NotificationPlan{ChannelID: "ch-1"}, mustTime(t, "2026-03-01T13:00:00Z"))}); err == nil {
				t.Fatal("corrupt queue accepted a new record")
			}
		})
	}
}

func TestConsumeHandoffDedupesNotificationClickRecords(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	clicks := []core.NotificationClick{core.NewNotificationClick(core.NotificationPlan{ChannelID: "ch-1"}, mustTime(t, "2026-03-01T12:00:00Z"))}
	handoff := map[string]any{
		"id":        "handoff-clicks-1",
		"createdAt": "2026-03-01T12:00:00Z",
		"clicks":    clicks,
	}
	data, _ := json.Marshal(handoff)
	if err := os.WriteFile(filepath.Join(dir, "inbox.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.ConsumeHandoff(); err != nil {
		t.Fatal(err)
	}
	queueData, err := os.ReadFile(filepath.Join(dir, "notification-clicks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var queue core.NotificationClicks
	if err := json.Unmarshal(queueData, &queue); err != nil {
		t.Fatal(err)
	}
	if len(queue.Clicks) != 1 {
		t.Fatalf("queue = %+v", queue.Clicks)
	}
	if err := a.RecordNotificationClicks(clicks); err != nil {
		t.Fatal(err)
	}
	queueData, _ = os.ReadFile(filepath.Join(dir, "notification-clicks.json"))
	_ = json.Unmarshal(queueData, &queue)
	if len(queue.Clicks) != 1 {
		t.Fatalf("duplicate was recorded: %+v", queue.Clicks)
	}
}

func TestNotificationClickIDRoutesChannelFromApp(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	a.Channels = []core.Channel{{ID: "ch-1"}}
	a.Selected = "all"
	channelID, ok := core.ChannelFromNotificationID(core.ChannelNotificationID("ch-1"))
	if !ok || !a.SelectChannel(channelID) || a.Selected != "channel:ch-1" {
		t.Fatalf("id route: %q %v %q", channelID, ok, a.Selected)
	}
}

func TestRecordNotificationClicksRejectsInvalidRecord(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	bad := core.NotificationClick{ID: "unrelated", ChannelID: "ch-1", Timestamp: "2026-03-01T12:00:00Z"}
	if err := a.RecordNotificationClicks([]core.NotificationClick{bad}); err == nil {
		t.Fatal("invalid click record was accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "notification-clicks.json")); !os.IsNotExist(err) {
		t.Fatalf("queue was written for invalid record: %v", err)
	}
}
