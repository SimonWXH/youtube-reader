package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

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

func TestConsumeHandoffNoopDoesNotAdvanceRefresh(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	last := "2026-03-01T10:00:00Z"
	a.Settings.LastRefresh = last
	handoff := map[string]any{
		"id":          "handoff-noop",
		"createdAt":   "2026-03-01T12:00:00Z",
		"description": "Up to date",
	}
	data, _ := json.Marshal(handoff)
	if err := os.WriteFile(filepath.Join(dir, "inbox.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.ConsumeHandoff(); err != nil {
		t.Fatal(err)
	}
	if a.Settings.LastRefresh != last || a.Settings.LastHandoffID != "handoff-noop" {
		t.Fatalf("settings: %+v", a.Settings)
	}
	if a.LastRefreshMessage != "Up to date" {
		t.Fatalf("status: %q", a.LastRefreshMessage)
	}
}

func TestConsumeHandoffDoesNotRewindNewerRefresh(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	current := "2026-03-01T12:00:00Z"
	a.Settings.LastRefresh = current
	handoff := map[string]any{
		"id":        "handoff-old",
		"createdAt": "2026-03-01T10:00:00Z",
		"result":    core.RefreshResult{Changed: true, Description: "Refreshed 1 videos"},
	}
	data, _ := json.Marshal(handoff)
	if err := os.WriteFile(filepath.Join(dir, "inbox.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.ConsumeHandoff(); err != nil {
		t.Fatal(err)
	}
	if a.Settings.LastRefresh != current {
		t.Fatalf("last refresh rewound: %+v", a.Settings)
	}
}

func TestConsumeHandoffQuotaFailurePausesRefresh(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	handoff := map[string]any{
		"id":          "handoff-quota",
		"createdAt":   "2026-03-01T12:00:00Z",
		"description": "quota paused until 2026-03-02T12:00:00Z",
		"failures":    []core.RefreshFailure{{At: "2026-03-01T12:00:00Z", Message: "quota paused until 2026-03-02T12:00:00Z"}},
	}
	data, _ := json.Marshal(handoff)
	if err := os.WriteFile(filepath.Join(dir, "inbox.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.ConsumeHandoff(); err != nil {
		t.Fatal(err)
	}
	if a.Settings.QuotaPausedUntil != "2026-03-02T12:00:00Z" {
		t.Fatalf("quota pause: %+v", a.Settings)
	}
}

// TestConsumeHandoffDoesNotRearmPauseFromHistory proves a quota pause is
// armed only by the failure the refresh itself produced: a handoff whose
// current failure is an ordinary error keeps the earlier quota failure as
// history without arming a pause the refresh did not earn.
func TestConsumeHandoffDoesNotRearmPauseFromHistory(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	failures := []core.RefreshFailure{
		{At: "2026-03-02T12:00:00Z", Message: "Get \"https://www.googleapis.com/youtube/v3/channels\": dial tcp: no route to host"},
		{At: "2026-03-01T12:00:00Z", Message: "quota paused until 2026-03-02T12:00:00Z"},
	}
	handoff := map[string]any{
		"id":          "handoff-stale-quota",
		"createdAt":   "2026-03-02T12:00:00Z",
		"description": failures[0].Message,
		"failures":    failures,
	}
	data, _ := json.Marshal(handoff)
	if err := os.WriteFile(filepath.Join(dir, "inbox.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.ConsumeHandoff(); err != nil {
		t.Fatal(err)
	}
	if a.Settings.QuotaPausedUntil != "" {
		t.Fatalf("carried quota history re-armed a pause: %+v", a.Settings.QuotaPausedUntil)
	}
	if len(a.Settings.RecentFailures) != 2 ||
		a.Settings.RecentFailures[0].Message != failures[0].Message ||
		a.Settings.RecentFailures[1].Message != failures[1].Message {
		t.Fatalf("failures: %+v", a.Settings.RecentFailures)
	}
}

func TestRefreshNoopDoesNotAdvanceLastRefresh(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	last := time.Now().UTC().Format(time.RFC3339)
	a.Settings.APIKey = "local-key"
	a.Settings.LastRefresh = last
	a.Settings.RefreshMinutes = 240
	outcome := a.Refresh(false)
	if outcome.Changed || outcome.Description != "Up to date" {
		t.Fatalf("outcome: %+v", outcome)
	}
	if a.Settings.LastRefresh != last {
		t.Fatalf("last refresh advanced on noop: %+v", a.Settings)
	}
}

// TestRefreshBlockedKeepsRecordedFailures proves a refresh the reader refuses
// for a quota pause or a missing key leaves the failure it reports in place,
// rather than clearing the very state the user is reading.
func TestRefreshBlockedKeepsRecordedFailures(t *testing.T) {
	cases := []struct {
		name  string
		block func(*core.Settings)
	}{
		{"quota pause", func(s *core.Settings) {
			s.QuotaPausedUntil = time.Now().Add(24 * time.Hour).Format(time.RFC3339)
		}},
		{"missing key", func(s *core.Settings) { s.APIKey = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			a := newTestApp(t, dir)
			defer a.Close()
			a.Settings.APIKey = "key"
			failure := core.RefreshFailure{
				At:      time.Now().UTC().Format(time.RFC3339),
				Message: "quota paused until 2026-03-02T12:00:00Z",
			}
			a.Settings.RecentFailures = []core.RefreshFailure{failure}
			tc.block(&a.Settings)
			outcome := a.Refresh(true)
			if outcome.Changed {
				t.Fatalf("blocked refresh reported a change: %+v", outcome)
			}
			if len(a.Settings.RecentFailures) != 1 || a.Settings.RecentFailures[0].Message != failure.Message {
				t.Fatalf("blocked refresh cleared the failure: %+v", a.Settings.RecentFailures)
			}
			persisted := loadTestSettings(t, filepath.Join(dir, "settings.json"))
			if len(persisted.RecentFailures) != 1 || persisted.RecentFailures[0].Message != failure.Message {
				t.Fatalf("blocked refresh cleared the persisted failure: %+v", persisted.RecentFailures)
			}
		})
	}
}

func TestSaveSettingsPreservesNewerOnDiskStatus(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	a.Settings.APIKey = "local-key"
	a.Settings.LastRefresh = "2026-03-01T09:00:00Z"
	disk := core.Settings{
		APIKey:      "disk-key",
		SyncPath:    a.Settings.SyncPath,
		LastRefresh: "2026-03-01T12:00:00Z",
	}
	data, _ := json.Marshal(disk)
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.SaveSettings(); err != nil {
		t.Fatal(err)
	}
	loaded := loadTestSettings(t, filepath.Join(dir, "settings.json"))
	if loaded.APIKey != "local-key" || loaded.LastRefresh != "2026-03-01T12:00:00Z" {
		t.Fatalf("settings: %+v", loaded)
	}
}

func TestSaveSyncMergesNewerOnDiskState(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	a.DeviceID = "device"
	a.Sync = core.SyncState{
		SchemaVersion: 1,
		UpdatedAt:     10,
		Channels:      map[string]core.Channel{"local": {ID: "local", Title: "Local", UpdatedAt: 10}},
		ReadStates:    map[string]map[string]core.ReadRecord{},
		Favorites:     map[string]core.FavoriteRecord{},
	}
	remote := core.SyncState{
		SchemaVersion: 1,
		UpdatedAt:     100,
		Channels:      map[string]core.Channel{"remote": {ID: "remote", Title: "Remote", UpdatedAt: 100}},
		ReadStates:    map[string]map[string]core.ReadRecord{},
		Favorites:     map[string]core.FavoriteRecord{},
	}
	if err := core.SaveSyncAtomic(a.Settings.SyncPath, remote); err != nil {
		t.Fatal(err)
	}
	if err := a.SaveSync(); err != nil {
		t.Fatal(err)
	}
	loaded, err := core.LoadSyncWithConflicts(a.Settings.SyncPath, a.DeviceID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded.Channels["remote"]; !ok {
		t.Fatalf("remote channel lost: %+v", loaded.Channels)
	}
	if _, ok := loaded.Channels["local"]; !ok {
		t.Fatalf("local channel lost: %+v", loaded.Channels)
	}
}

func loadTestSettings(t *testing.T, path string) core.Settings {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var settings core.Settings
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	return settings
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
