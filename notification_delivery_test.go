package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/simon/youtube-reader/core"
)

// TestHelperHandoffMakesTheAnnouncedVideoClickable drives the app side of the
// helper's hand-off: the refresh the helper committed to the shared store
// while this app was running is read, so a click on its notification opens the
// channel already showing the videos it announced.
func TestHelperHandoffMakesTheAnnouncedVideoClickable(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	a.Selected = "all"

	channel := core.Channel{ID: "ch-1", Title: "Channel"}
	video := core.Video{
		ID: "v-new", ChannelID: "ch-1", ChannelTitle: "Channel", Title: "Just announced",
		PublishedAt: core.FormatRFC3339(time.Now().Add(-time.Hour)), Duration: "PT10M0S",
	}
	if err := a.Store.CommitRefresh(context.Background(), core.RefreshCommit{
		Channels: []core.Channel{channel},
		Videos:   []core.Video{video},
	}); err != nil {
		t.Fatal(err)
	}
	if got := len(a.Videos); got != 0 {
		t.Fatalf("the app already shows %d videos before the hand-off", got)
	}

	w := &Window{app: a}
	w.reloadHelperRefresh()
	if got := len(a.Channels); got != 1 {
		t.Fatalf("channels after hand-off = %d, want 1", got)
	}

	notificationClickHandler(a)(core.ChannelNotificationID("ch-1"))
	if a.Selected != "channel:ch-1" {
		t.Fatalf("click routed to %q", a.Selected)
	}
	visible := a.VisibleVideos()
	if len(visible) != 1 || visible[0].ID != "v-new" {
		t.Fatalf("the announced video is not in the clicked channel's view: %+v", visible)
	}
}

// TestPostNotificationRecordsOnlyPermissionRefusals proves the Notifications
// pane's answer tracks what macOS said about permission alone: a failure that is
// not a refusal leaves the recorded answer as it was.
func TestPostNotificationRecordsOnlyPermissionRefusals(t *testing.T) {
	previous := showNotificationForApp
	defer func() { showNotificationForApp = previous }()

	for _, tc := range []struct {
		name  string
		err   error
		start bool
		want  bool
	}{
		{"shown", nil, true, false},
		{"refused", fmt.Errorf("wrapped: %w", mygo.ErrNotificationsDenied), false, true},
		{"not a refusal", errors.New("notifications unsupported"), true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			a := newTestApp(t, dir)
			defer a.Close()
			if _, err := a.updateSettings(func(s *core.Settings) bool {
				s.NotificationPermissionDenied = tc.start
				return true
			}); err != nil {
				t.Fatal(err)
			}
			showNotificationForApp = func(NotificationPlan) error { return tc.err }

			a.postNotification(core.NotificationPlan{ChannelID: "ch-1", ChannelName: "Channel", Count: 1})
			if got := a.settingsSnapshot().NotificationPermissionDenied; got != tc.want {
				t.Fatalf("recorded denial = %v, want %v", got, tc.want)
			}
			persisted := core.Settings{}
			data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(data, &persisted); err != nil {
				t.Fatal(err)
			}
			if persisted.NotificationPermissionDenied != tc.want {
				t.Fatalf("persisted denial = %v, want %v", persisted.NotificationPermissionDenied, tc.want)
			}
		})
	}
}

// TestNotifyOnlyDeliversAndConsumesHandshakeWithoutWindow proves that when the
// app is invoked with --notify-only, it consumes the pending handshake file
// and delivers the native notifications through the existing app postNotification
// path without instantiating any Window.
func TestNotifyOnlyDeliversAndConsumesHandshakeWithoutWindow(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()

	plans := []core.NotificationPlan{
		{ChannelID: "ch-alpha", ChannelName: "Alpha", Count: 3, NewestTitle: "Alpha Video"},
		{ChannelID: "ch-beta", ChannelName: "Beta", Count: 1, NewestTitle: "Beta Video"},
	}
	if err := core.WritePendingNotifications(dir, plans); err != nil {
		t.Fatal(err)
	}

	var delivered []NotificationPlan
	prevShow := showNotificationForApp
	showNotificationForApp = func(plan NotificationPlan) error {
		delivered = append(delivered, plan)
		return nil
	}
	defer func() { showNotificationForApp = prevShow }()

	var loopRan bool
	prevLoop := runNotifyOnlyLoop
	runNotifyOnlyLoop = func(fn func()) error {
		loopRan = true
		fn()
		return nil
	}
	defer func() { runNotifyOnlyLoop = prevLoop }()

	runNotifyOnly(a)

	if !loopRan {
		t.Fatal("runNotifyOnlyLoop was not called")
	}
	if len(delivered) != 2 || delivered[0].ChannelID != "ch-alpha" || delivered[1].ChannelID != "ch-beta" {
		t.Fatalf("delivered notifications = %+v, want %+v", delivered, plans)
	}

	// Verify handshake file was removed
	remaining, err := core.ReadPendingNotifications(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 {
		t.Fatalf("pending notifications were not consumed, remaining: %+v", remaining)
	}

	// Prove that cold-start click on the delivered notification routes to channel
	notificationClickHandler(a)(core.ChannelNotificationID("ch-alpha"))
	if a.Selected != "channel:ch-alpha" {
		t.Fatalf("click routing selected = %q, want channel:ch-alpha", a.Selected)
	}
}
