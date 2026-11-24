package main

import (
	"context"
	"os"
	"path/filepath"
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
// opens the channel there directly.
func TestPostHelperNotificationsPrefersTheRunningApp(t *testing.T) {
	var delivered bool
	var launchedBundle string
	var pendingWritten bool

	prevDeliver := deliverNotificationsForHelper
	prevWrite := writePendingNotificationsForHelper
	prevLaunch := launchNotifyOnlyForHelper
	deliverNotificationsForHelper = func(dir string, plans []core.NotificationPlan) error {
		delivered = true
		if len(plans) != 1 || plans[0].ChannelID != "ch-1" {
			t.Fatalf("delivered plans = %+v", plans)
		}
		return nil
	}
	writePendingNotificationsForHelper = func(dir string, plans []core.NotificationPlan) error {
		pendingWritten = true
		return nil
	}
	launchNotifyOnlyForHelper = func(bundlePath string) error {
		launchedBundle = bundlePath
		return nil
	}
	defer func() {
		deliverNotificationsForHelper = prevDeliver
		writePendingNotificationsForHelper = prevWrite
		launchNotifyOnlyForHelper = prevLaunch
	}()

	postHelperNotifications(t.TempDir(), []core.NotificationPlan{{ChannelID: "ch-1", ChannelName: "One", Count: 1}})
	if !delivered {
		t.Fatal("the running app was not handed the notification")
	}
	if pendingWritten {
		t.Fatal("pending notifications were written even though app was running")
	}
	if launchedBundle != "" {
		t.Fatalf("app was launched even though it was running: %q", launchedBundle)
	}
}

// TestPostHelperNotificationsFallsBackToPendingNotifyOnly proves a closed app
// has its notifications written to the pending handshake file and is launched
// hidden with --notify-only.
func TestPostHelperNotificationsFallsBackToPendingNotifyOnly(t *testing.T) {
	var launchedBundle string
	prevDeliver := deliverNotificationsForHelper
	prevResolve := resolveAppBundleForHelper
	prevLaunch := launchNotifyOnlyForHelper
	deliverNotificationsForHelper = func(dir string, plans []core.NotificationPlan) error {
		return os.ErrClosed
	}
	resolveAppBundleForHelper = func() (string, error) {
		return "/Applications/YouTube Reader.app", nil
	}
	launchNotifyOnlyForHelper = func(bundlePath string) error {
		launchedBundle = bundlePath
		return nil
	}
	defer func() {
		deliverNotificationsForHelper = prevDeliver
		resolveAppBundleForHelper = prevResolve
		launchNotifyOnlyForHelper = prevLaunch
	}()

	dir := t.TempDir()
	plans := []core.NotificationPlan{
		{ChannelID: "ch-1", ChannelName: "A \"quoted\" channel", Count: 2, NewestTitle: "Newest video"},
		{ChannelID: "ch-2", ChannelName: "Two", Count: 1},
	}
	postHelperNotifications(dir, plans)

	if launchedBundle != "/Applications/YouTube Reader.app" {
		t.Fatalf("launched bundle = %q, want /Applications/YouTube Reader.app", launchedBundle)
	}

	pending, err := core.ReadPendingNotifications(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 || pending[0].ChannelID != "ch-1" || pending[1].ChannelID != "ch-2" {
		t.Fatalf("pending notifications = %+v, want %+v", pending, plans)
	}
}

// TestAppBundleForExecutable proves that an executable inside an .app bundle
// correctly resolves its enclosing bundle path.
func TestAppBundleForExecutable(t *testing.T) {
	dir := t.TempDir()
	appPath := filepath.Join(dir, "YouTube Reader.app")
	contentsDir := filepath.Join(appPath, "Contents")
	resourcesDir := filepath.Join(contentsDir, "Resources")
	if err := os.MkdirAll(resourcesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	helperExe := filepath.Join(resourcesDir, "ytreader-helper")

	got := appBundleForExecutable(helperExe)
	if got != appPath {
		t.Fatalf("appBundleForExecutable(%q) = %q, want %q", helperExe, got, appPath)
	}

	// Non-bundle path returns empty string
	if gotStandalone := appBundleForExecutable("/tmp/bin/ytreader-helper"); gotStandalone != "" {
		t.Fatalf("standalone exe resolved to %q, want empty", gotStandalone)
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

// TestIntegratedClosedAppToDeliveryCompleted drives the entire integration path:
// when the app is not running (socket unavailable), helper detects this, writes
// the handshake file, and executes the notify-only launch hook, which consumes
// the handshake file and posts the notifications to the native delivery stand-in.
func TestIntegratedClosedAppToDeliveryCompleted(t *testing.T) {
	dir := t.TempDir()

	// 1. Stand-in for native delivery (the app's UNUserNotificationCenter poster)
	var nativeDelivered []core.NotificationPlan
	nativePoster := func(plan core.NotificationPlan) error {
		nativeDelivered = append(nativeDelivered, plan)
		return nil
	}

	// 2. Seams for helper
	prevDeliver := deliverNotificationsForHelper
	prevResolve := resolveAppBundleForHelper
	prevLaunch := launchNotifyOnlyForHelper

	// App socket is not listening (simulating app not running)
	deliverNotificationsForHelper = func(dir string, plans []core.NotificationPlan) error {
		return os.ErrClosed
	}
	resolveAppBundleForHelper = func() (string, error) {
		return "/Applications/YouTube Reader.app", nil
	}

	var launchedBundle string
	launchNotifyOnlyForHelper = func(bundlePath string) error {
		launchedBundle = bundlePath
		// Stand-in for --notify-only app launch:
		// It consumes the pending handshake file and calls nativePoster
		plans, err := core.ConsumePendingNotifications(dir)
		if err != nil {
			return err
		}
		for _, plan := range plans {
			if err := nativePoster(plan); err != nil {
				return err
			}
		}
		return nil
	}
	defer func() {
		deliverNotificationsForHelper = prevDeliver
		resolveAppBundleForHelper = prevResolve
		launchNotifyOnlyForHelper = prevLaunch
	}()

	// 3. Helper discovers new videos while app is closed
	plans := []core.NotificationPlan{
		{ChannelID: "ch-prime", ChannelName: "Prime Channel", Count: 5, NewestTitle: "Epic Video"},
	}
	postHelperNotifications(dir, plans)

	// 4. Assertions:
	// App bundle was resolved and launched with --notify-only
	if launchedBundle != "/Applications/YouTube Reader.app" {
		t.Fatalf("launched bundle = %q, want /Applications/YouTube Reader.app", launchedBundle)
	}

	// Native delivery received the notification
	if len(nativeDelivered) != 1 || nativeDelivered[0].ChannelID != "ch-prime" {
		t.Fatalf("native delivery received %+v, want %+v", nativeDelivered, plans)
	}

	// Handshake file was consumed and removed
	if _, err := os.Stat(core.PendingNotificationsPath(dir)); !os.IsNotExist(err) {
		t.Fatal("handshake file still exists after delivery completed")
	}
}

func TestAppBundleForExecutable(t *testing.T) {
	dir := t.TempDir()
	bundleDir := filepath.Join(dir, "YouTube Reader.app")
	resourcesDir := filepath.Join(bundleDir, "Contents", "Resources")
	if err := os.MkdirAll(resourcesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	helperPath := filepath.Join(resourcesDir, "ytreader-helper")

	got := appBundleForExecutable(helperPath)
	if got != bundleDir {
		t.Fatalf("appBundleForExecutable(%q) = %q, want %q", helperPath, got, bundleDir)
	}

	outsidePath := filepath.Join(dir, "ytreader-helper")
	if got := appBundleForExecutable(outsidePath); got != "" {
		t.Fatalf("appBundleForExecutable(%q) = %q, want empty", outsidePath, got)
	}
}
