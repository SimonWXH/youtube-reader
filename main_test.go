package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/simon/youtube-reader/core"
)

func TestRefreshedLabel(t *testing.T) {
	old := time.Now().Add(-72 * time.Hour).Truncate(time.Second)
	tests := []struct {
		name  string
		stamp string
		want  string
	}{
		{"empty", "", ""},
		{"invalid", "not-a-time", "Refreshed not-a-time"},
		{"just now", time.Now().UTC().Format(time.RFC3339), "Refreshed just now"},
		{"minutes", time.Now().Add(-5 * time.Minute).UTC().Format(time.RFC3339), "Refreshed 5 min ago"},
		{"hours", time.Now().Add(-3 * time.Hour).UTC().Format(time.RFC3339), "Refreshed 3 hr ago"},
		{"older than a day", old.UTC().Format(time.RFC3339), "Refreshed on " + old.Local().Format("Jan 2, 2006 at 15:04")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := refreshedLabel(tt.stamp)
			if got != tt.want {
				t.Fatalf("refreshedLabel(%q) = %q, want %q", tt.stamp, got, tt.want)
			}
			if tt.name == "older than a day" && !strings.Contains(got, old.Local().Format("2006")) {
				t.Fatalf("older refresh label omits the year: %q", got)
			}
		})
	}
}

func TestRegisterNotificationClickRoutesChannelID(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	a.Channels = []core.Channel{{ID: "ch-1"}}
	a.Videos = []core.Video{{ID: "v-1", ChannelID: "ch-1"}}
	a.Selected = "all"
	a.ChannelFilter = "ch-other"
	a.Search = "old"
	a.UnreadOnly = true
	a.SelectedVideoID = "v-1"

	registerNotificationClick(a)
	notificationClickHandler(a)(core.ChannelNotificationID("ch-1"))
	if a.Selected != "channel:ch-1" || a.ChannelFilter != "" || a.Search != "" || a.UnreadOnly || a.SelectedVideoID != "" {
		t.Fatalf("id route: %+v", a)
	}
	if got := a.VisibleVideos(); len(got) != 1 || got[0].ID != "v-1" {
		t.Fatalf("routed videos = %+v", got)
	}
}

func TestNotificationClickHandlerDoesNotRouteOnOrdinaryLaunch(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	a.Channels = []core.Channel{{ID: "ch-2"}}
	a.Videos = []core.Video{{ID: "v-2", ChannelID: "ch-2"}}
	a.Selected = "all"
	click := core.NewNotificationClick(core.NotificationPlan{ChannelID: "ch-2"}, time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC))
	data, _ := json.Marshal(core.NotificationClicks{Clicks: []core.NotificationClick{click}})
	if err := os.WriteFile(filepath.Join(dir, "notification-clicks.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	// realMain only registers the listener on launch; it no longer consumes
	// the queue. A pending helper post alone must not change the selection.
	registerNotificationClick(a)
	if a.Selected != "all" {
		t.Fatalf("ordinary launch routed to %q", a.Selected)
	}
}

func TestNotificationClickHandlerRoutesWarmAndColdChannelIDs(t *testing.T) {
	for _, name := range []string{"warm", "cold"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			a := newTestApp(t, dir)
			defer a.Close()
			a.Channels = []core.Channel{{ID: "ch-2"}}
			a.Videos = []core.Video{{ID: "v-2", ChannelID: "ch-2"}}
			a.Selected = "all"
			a.ChannelFilter = "ch-other"
			off := registerNotificationClick(a)
			defer off()
			notificationClickHandler(a)(core.ChannelNotificationID("ch-2"))
			if a.Selected != "channel:ch-2" || a.ChannelFilter != "" {
				t.Fatalf("route = %q, %q", a.Selected, a.ChannelFilter)
			}
			if got := a.VisibleVideos(); len(got) != 1 || got[0].ID != "v-2" {
				t.Fatalf("routed videos = %+v", got)
			}
		})
	}
}

func TestNotificationClickHandlerRoutesQueuedHelperClickOnlyOnCallback(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	a.Channels = []core.Channel{{ID: "ch-1"}, {ID: "ch-2"}}
	a.Videos = []core.Video{{ID: "v-1", ChannelID: "ch-1"}, {ID: "v-2", ChannelID: "ch-2"}}
	a.Selected = "all"
	old := core.NewNotificationClick(core.NotificationPlan{ChannelID: "ch-1"}, time.Date(2026, 3, 1, 11, 0, 0, 0, time.UTC))
	newer := core.NewNotificationClick(core.NotificationPlan{ChannelID: "ch-2"}, time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC))
	data, _ := json.Marshal(core.NotificationClicks{Clicks: []core.NotificationClick{old, newer}})
	if err := os.WriteFile(filepath.Join(dir, "notification-clicks.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	notificationClickHandler(a)("")
	if a.Selected != "channel:ch-2" {
		t.Fatalf("cold click route = %q", a.Selected)
	}
	if got := a.VisibleVideos(); len(got) != 1 || got[0].ID != "v-2" {
		t.Fatalf("routed videos = %+v", got)
	}
	// A later click consumes the retained older record instead of rerouting
	// to a queued post from a different channel.
	notificationClickHandler(a)("")
	if a.Selected != "channel:ch-1" {
		t.Fatalf("retained click route = %q", a.Selected)
	}
}

func TestNotificationClickHandlerDoesNotRouteUnrelatedID(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	a.Channels = []core.Channel{{ID: "ch-1"}}
	a.Selected = "all"
	click := core.NewNotificationClick(core.NotificationPlan{ChannelID: "ch-1"}, time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC))
	data, _ := json.Marshal(core.NotificationClicks{Clicks: []core.NotificationClick{click}})
	if err := os.WriteFile(filepath.Join(dir, "notification-clicks.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	notificationClickHandler(a)("unrelated-id")
	if a.Selected != "all" {
		t.Fatalf("unrelated click routed to %q", a.Selected)
	}
}
