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
	a.Selected = "all"

	registerNotificationClick(a)
	notificationClickHandler(a)(core.ChannelNotificationID("ch-1"))
	if a.Selected != "channel:ch-1" || a.Search != "" || a.UnreadOnly || a.SelectedVideoID != "" {
		t.Fatalf("id route: %+v", a)
	}
}

func TestNotificationClickWiringAndQueueFallback(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	a.Channels = []core.Channel{{ID: "ch-2"}}
	click := core.NewNotificationClick(core.NotificationPlan{ChannelID: "ch-2"}, time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC))
	data, _ := json.Marshal(core.NotificationClicks{Clicks: []core.NotificationClick{click}})
	if err := os.WriteFile(filepath.Join(dir, "notification-clicks.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	channelID, ok := a.ConsumeNotificationClick()
	if !ok || !a.SelectChannel(channelID) || a.Selected != "channel:ch-2" {
		t.Fatalf("fallback route = %q, %v, %q", channelID, ok, a.Selected)
	}
}
