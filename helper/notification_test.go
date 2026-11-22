package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/simon/youtube-reader/core"
)

func TestQueueNotificationClicksDeduplicatesAndPersists(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	plans := []core.NotificationPlan{
		{ChannelID: "ch-1", ChannelName: "One", Count: 2},
		{ChannelID: "ch-2", ChannelName: "Two", Count: 4},
	}
	if err := queueNotificationClicks(dir, plans, now); err != nil {
		t.Fatal(err)
	}
	if err := queueNotificationClicks(dir, plans, now); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(notificationClicksPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	var queue core.NotificationClicks
	if err := json.Unmarshal(data, &queue); err != nil {
		t.Fatal(err)
	}
	if len(queue.Clicks) != 2 {
		t.Fatalf("queue = %+v", queue.Clicks)
	}
	if queue.Clicks[0].ID != "ytreader-channel-ch-1" || queue.Clicks[0].Timestamp != "2026-03-01T12:00:00Z" {
		t.Fatalf("click = %+v", queue.Clicks[0])
	}
	info, err := os.Stat(notificationClicksPath(dir))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, err = %v", info, err)
	}
}

func TestQueueNotificationClicksPreservesEarlierPosts(t *testing.T) {
	dir := t.TempDir()
	early := time.Date(2026, 3, 1, 11, 0, 0, 0, time.UTC)
	if err := queueNotificationClicks(dir, []core.NotificationPlan{{ChannelID: "ch-1"}}, early); err != nil {
		t.Fatal(err)
	}
	if err := queueNotificationClicks(dir, []core.NotificationPlan{{ChannelID: "ch-2"}}, early.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(notificationClicksPath(dir))
	var queue core.NotificationClicks
	if err := json.Unmarshal(data, &queue); err != nil {
		t.Fatal(err)
	}
	if len(queue.Clicks) != 2 || queue.Clicks[0].ChannelID != "ch-1" || queue.Clicks[1].ChannelID != "ch-2" {
		t.Fatalf("queue = %+v", queue.Clicks)
	}
}

func TestPostHelperNotificationDoesNotLaunchTheApp(t *testing.T) {
	var scripts []string
	previous := osascriptForHelper
	previousRunning := appRunningForHelper
	osascriptForHelper = func(script string) error {
		scripts = append(scripts, script)
		return nil
	}
	appRunningForHelper = func() bool { return false }
	defer func() { osascriptForHelper, appRunningForHelper = previous, previousRunning }()

	plan := core.NotificationPlan{ChannelName: "A \"quoted\" channel", Count: 2, NewestTitle: "Newest video"}
	if err := postHelperNotification(plan); err != nil {
		t.Fatal(err)
	}
	if len(scripts) != 1 {
		t.Fatalf("osascript calls = %d, want 1", len(scripts))
	}
	script := scripts[0]
	if strings.Contains(script, "tell application") {
		t.Fatalf("script targets an application, which can launch the main app: %q", script)
	}
	for _, want := range []string{
		`display notification "2 new videos\nNewest video"`,
		`with title "A \"quoted\" channel"`,
		`subtitle "New videos" sound name "default"`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script %q does not contain %q", script, want)
		}
	}
}

func TestPostHelperNotificationTargetsRunningApp(t *testing.T) {
	var scripts []string
	previous := osascriptForHelper
	previousRunning := appRunningForHelper
	osascriptForHelper = func(script string) error {
		scripts = append(scripts, script)
		return nil
	}
	appRunningForHelper = func() bool { return true }
	defer func() { osascriptForHelper, appRunningForHelper = previous, previousRunning }()

	plan := core.NotificationPlan{ChannelName: "Channel", Count: 1, NewestTitle: "Video"}
	if err := postHelperNotification(plan); err != nil {
		t.Fatal(err)
	}
	if len(scripts) != 1 {
		t.Fatalf("osascript calls = %d, want 1", len(scripts))
	}
	want := `tell application id "com.simon.youtubereader" to display notification "1 new video\nVideo" with title "Channel" subtitle "New videos" sound name "default"`
	if scripts[0] != want {
		t.Fatalf("script = %q, want %q", scripts[0], want)
	}
}

func TestHelperRunRecordsClickHandoffWhenNotified(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	postCalls := 0
	originalRefresh := refreshForHelper
	originalNetwork := networkAvailableForHelper
	refreshForHelper = func(ctx context.Context, client core.YouTube, settings core.Settings, state core.SyncState, store *core.Store, existing []core.Video, revision int64, force bool, at time.Time) core.RefreshResult {
		return core.RefreshResult{
			Changed:     true,
			Description: "Refreshed 2 videos",
			Notified:    []core.NotificationPlan{{ChannelID: "ch-1", ChannelName: "One", Count: 2}},
		}
	}
	networkAvailableForHelper = func(ctx context.Context) bool { return true }
	defer func() { refreshForHelper = originalRefresh; networkAvailableForHelper = originalNetwork }()

	if err := writeSettings(dir, core.Settings{APIKey: "key", SyncPath: filepath.Join(dir, "sync.json"), RefreshMinutes: 60}); err != nil {
		t.Fatal(err)
	}
	h, err := HelperRun(context.Background(), dir, false, now, func(plan core.NotificationPlan) error {
		postCalls++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if postCalls != 1 || len(h.Clicks) != 1 || h.Clicks[0].ChannelID != "ch-1" {
		t.Fatalf("posts=%d handoff=%+v", postCalls, h)
	}
	data, err := os.ReadFile(notificationClicksPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	var queue core.NotificationClicks
	if err := json.Unmarshal(data, &queue); err != nil {
		t.Fatal(err)
	}
	if len(queue.Clicks) != 1 || queue.Clicks[0].ChannelID != "ch-1" {
		t.Fatalf("queue = %+v", queue.Clicks)
	}
}
