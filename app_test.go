package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/simon/youtube-reader/core"
)

func newTestApp(t *testing.T, dir string) *App {
	t.Helper()
	ctx := context.Background()
	store, err := core.OpenStore(ctx, filepath.Join(dir, "videos.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	lock, err := core.AcquireLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := &App{
		Dir:      dir,
		Store:    store,
		Lock:     lock,
		Settings: core.Settings{SyncPath: filepath.Join(dir, "sync.json"), RefreshMinutes: 60},
		Sync: core.SyncState{
			SchemaVersion: 1,
			Channels:      map[string]core.Channel{},
			ReadStates:    map[string]map[string]core.ReadRecord{},
			Favorites:     map[string]core.FavoriteRecord{},
			Groups:        map[string]core.ChannelGroup{},
		},
	}
	return a
}

func TestConsumeHandoffAppliesRefreshResults(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()

	handoff := struct {
		ID          string                  `json:"id"`
		CreatedAt   string                  `json:"createdAt"`
		Description string                  `json:"description"`
		Result      *core.RefreshResult     `json:"result"`
		Notified    []core.NotificationPlan `json:"notified"`
		Failures    []core.RefreshFailure   `json:"failures"`
	}{
		ID:          "handoff-1",
		CreatedAt:   "2026-03-01T12:00:00Z",
		Description: "Refreshed 2 videos",
		Result:      &core.RefreshResult{Description: "Refreshed 2 videos", Changed: true},
		Notified:    []core.NotificationPlan{{ChannelID: "ch-1", ChannelName: "Channel", Count: 2, NewestTitle: "New"}},
		Failures:    []core.RefreshFailure{{At: "2026-03-01T11:59:00Z", Message: "channel failed"}},
	}
	data, _ := json.Marshal(handoff)
	if err := os.WriteFile(filepath.Join(dir, "inbox.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.ConsumeHandoff(); err != nil {
		t.Fatal(err)
	}
	if a.Settings.LastHandoffID != "handoff-1" || a.Settings.LastRefresh != "2026-03-01T12:00:00Z" {
		t.Fatalf("settings: %+v", a.Settings)
	}
	if a.LastRefreshMessage != "Refreshed 2 videos" || len(a.LastNotified) != 1 {
		t.Fatalf("status: %q %+v", a.LastRefreshMessage, a.LastNotified)
	}
	if len(a.Settings.RecentFailures) != 1 || a.Settings.RecentFailures[0].Message != "channel failed" {
		t.Fatalf("failures: %+v", a.Settings.RecentFailures)
	}
	if _, err := os.Stat(filepath.Join(dir, "inbox.json")); !os.IsNotExist(err) {
		t.Fatalf("inbox not removed: %v", err)
	}
	// A duplicate handoff must be ignored.
	if err := a.ConsumeHandoff(); err != nil {
		t.Fatal(err)
	}
	if a.LastRefreshMessage != "Refreshed 2 videos" {
		t.Fatalf("status changed: %q", a.LastRefreshMessage)
	}
}

func TestDrainQueuedSyncWritesAfterHelper(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()

	queued := core.SyncState{
		SchemaVersion: 1,
		UpdatedAt:     123,
		Channels:      map[string]core.Channel{"ch-1": {ID: "ch-1", Title: "Channel", UpdatedAt: 123}},
		ReadStates:    map[string]map[string]core.ReadRecord{},
		Favorites:     map[string]core.FavoriteRecord{},
		Groups:        map[string]core.ChannelGroup{},
	}
	if err := a.Store.Queue(context.Background(), map[string]any{"state": queued}); err != nil {
		t.Fatal(err)
	}
	if err := a.DrainQueuedSyncWrites(); err != nil {
		t.Fatal(err)
	}
	if got := a.Sync.Channels["ch-1"].Title; got != "Channel" {
		t.Fatalf("channel = %+v", a.Sync.Channels)
	}
	saved, err := core.LoadSyncWithConflicts(a.Settings.SyncPath, a.DeviceID)
	if err != nil {
		t.Fatal(err)
	}
	if got := saved.Channels["ch-1"].Title; got != "Channel" {
		t.Fatalf("saved channel = %q", got)
	}
}
