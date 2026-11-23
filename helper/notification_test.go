package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/simon/youtube-reader/core"
)

// notificationTestRun performs one helper run whose refresh reports the given
// plans, without network or a real API key.
func notificationTestRun(t *testing.T, dir string, plans []core.NotificationPlan, notify notifyFunc) HelperHandoff {
	t.Helper()
	previousRefresh, previousNetwork := refreshForHelper, networkAvailableForHelper
	t.Cleanup(func() { refreshForHelper, networkAvailableForHelper = previousRefresh, previousNetwork })
	refreshForHelper = func(ctx context.Context, client core.YouTube, settings core.Settings, state core.SyncState, store *core.Store, existing []core.Video, revision int64, force bool, at time.Time) core.RefreshResult {
		return core.RefreshResult{Changed: true, Description: "Refreshed 2 videos", Notified: plans}
	}
	networkAvailableForHelper = func(context.Context) bool { return true }
	if err := writeSettings(dir, core.Settings{APIKey: "key", SyncPath: filepath.Join(dir, "sync.json"), RefreshMinutes: 60}); err != nil {
		t.Fatal(err)
	}
	h, err := HelperRun(context.Background(), dir, false, time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC), notify)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// TestPostHelperNotificationsPrefersTheRunningApp proves a running app shows
// the notification itself, so it is attributed to YouTube Reader and its click
// opens the channel there instead of Script Editor.
func TestPostHelperNotificationsPrefersTheRunningApp(t *testing.T) {
	var delivered bool
	var scripts []string
	previousDeliver, previousOSA := deliverNotificationsForHelper, osascriptForHelper
	deliverNotificationsForHelper = func(dir string, plans []core.NotificationPlan) error {
		delivered = true
		if len(plans) != 1 || plans[0].ChannelID != "ch-1" {
			t.Fatalf("delivered plans = %+v", plans)
		}
		return nil
	}
	osascriptForHelper = func(script string) error {
		scripts = append(scripts, script)
		return nil
	}
	defer func() { deliverNotificationsForHelper, osascriptForHelper = previousDeliver, previousOSA }()

	postHelperNotifications(t.TempDir(), []core.NotificationPlan{{ChannelID: "ch-1", ChannelName: "One", Count: 1}})
	if !delivered {
		t.Fatal("the running app was not handed the notification")
	}
	if len(scripts) != 0 {
		t.Fatalf("osascript also posted: %q", scripts)
	}
}

// TestPostHelperNotificationsFallsBackToPlainAppleScript proves a closed app
// still gets notified, by a plain display notification that never names the app
// bundle, so the refresh cannot pull its window up (PR #24).
func TestPostHelperNotificationsFallsBackToPlainAppleScript(t *testing.T) {
	var scripts []string
	previousDeliver, previousOSA := deliverNotificationsForHelper, osascriptForHelper
	deliverNotificationsForHelper = func(dir string, plans []core.NotificationPlan) error {
		return os.ErrClosed
	}
	osascriptForHelper = func(script string) error {
		scripts = append(scripts, script)
		return nil
	}
	defer func() { deliverNotificationsForHelper, osascriptForHelper = previousDeliver, previousOSA }()

	postHelperNotifications(t.TempDir(), []core.NotificationPlan{
		{ChannelID: "ch-1", ChannelName: "A \"quoted\" channel", Count: 2, NewestTitle: "Newest video"},
		{ChannelID: "ch-2", ChannelName: "Two", Count: 1},
	})
	if len(scripts) != 2 {
		t.Fatalf("osascript calls = %d, want 2", len(scripts))
	}
	want := `display notification "2 new videos\nNewest video" with title "A \"quoted\" channel" subtitle "New videos" sound name "default"`
	if scripts[0] != want {
		t.Fatalf("script = %q, want %q", scripts[0], want)
	}
	for _, script := range scripts {
		if strings.Contains(script, "tell application") {
			t.Fatalf("script targets an application, which can launch the main app: %q", script)
		}
	}
}

// TestHelperRunHandsItsPlansToTheAppOnce proves the helper passes the whole
// refresh's plans on in one hand-off, and only when a refresh produced them.
func TestHelperRunHandsItsPlansToTheAppOnce(t *testing.T) {
	plans := []core.NotificationPlan{
		{ChannelID: "ch-1", ChannelName: "One", Count: 2},
		{ChannelID: "ch-2", ChannelName: "Two", Count: 4},
	}
	var calls int
	var got []core.NotificationPlan
	h := notificationTestRun(t, t.TempDir(), plans, func(dir string, plans []core.NotificationPlan) {
		calls++
		got = plans
	})
	if calls != 1 || len(got) != 2 || got[1].ChannelID != "ch-2" {
		t.Fatalf("notify calls = %d, plans = %+v", calls, got)
	}
	if len(h.Notified) != 2 {
		t.Fatalf("handoff notified = %+v", h.Notified)
	}

	calls = 0
	notificationTestRun(t, t.TempDir(), nil, func(string, []core.NotificationPlan) { calls++ })
	if calls != 0 {
		t.Fatalf("a refresh without new videos notified %d times", calls)
	}
}

// TestHelperNotificationsReachTheRunningApp drives the real socket the app
// serves: the helper's plan arrives at the app's notification path, which is
// the same path a click on it routes through.
func TestHelperNotificationsReachTheRunningApp(t *testing.T) {
	// The socket path must fit the platform's Unix socket limit, which a test
	// runner's temporary directory can exceed.
	dir, err := os.MkdirTemp("/tmp", "ytreader")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	shown := make(chan core.NotificationPlan, 4)
	go func() {
		_ = core.ServeNotifications(ctx, dir, func(plans []core.NotificationPlan) {
			shown <- plans[0]
		})
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(core.NotificationSocketPath(dir)); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the app socket never appeared")
		}
		time.Sleep(5 * time.Millisecond)
	}

	plan := core.NotificationPlan{ChannelID: "ch-1", ChannelName: "One", Count: 2, NewestTitle: "New"}
	notificationTestRun(t, dir, []core.NotificationPlan{plan}, postHelperNotifications)
	select {
	case got := <-shown:
		if got != plan {
			t.Fatalf("app showed %+v, want %+v", got, plan)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the running app never received the helper's notification")
	}
}
