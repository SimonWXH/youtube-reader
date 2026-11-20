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
	if a.Search != "" || a.UnreadOnly || a.SelectedVideoID != "" {
		t.Fatalf("filters not cleared: %+v", a)
	}
	if a.Settings.LastNotificationClick != core.NotificationClickKey(newest) {
		t.Fatalf("dedupe key = %q", a.Settings.LastNotificationClick)
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

func TestConsumeNotificationClickSkipsDuplicateAndMalformedEntries(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	click := core.NewNotificationClick(core.NotificationPlan{ChannelID: "ch-1"}, mustTime(t, "2026-03-01T12:00:00Z"))
	a.Settings.LastNotificationClick = core.NotificationClickKey(click)
	writeNotificationClickQueue(t, dir, click)
	if _, ok := a.ConsumeNotificationClick(); ok {
		t.Fatal("duplicate click was consumed")
	}
	if _, err := os.Stat(filepath.Join(dir, "notification-clicks.json")); !os.IsNotExist(err) {
		t.Fatalf("stale queue was not removed: %v", err)
	}

	writeNotificationClickQueue(t, dir, core.NotificationClick{ID: "unrelated-id", ChannelID: "ch-1", Timestamp: "2026-03-02T12:00:00Z"})
	if got, ok := a.ConsumeNotificationClick(); ok {
		t.Fatalf("malformed click consumed %q", got)
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
