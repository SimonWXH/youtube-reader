package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
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

// groupNames lists the sidebar group names in order.
func groupNames(groups []core.ChannelGroup) []string {
	names := make([]string, 0, len(groups))
	for _, g := range groups {
		names = append(names, g.Name)
	}
	return names
}

// TestOrderGroupsLoadsStoredOrder covers how the sidebar orders the synced
// groups on load: the stored order wins exactly, a group the stored order does
// not name joins the end in name order, and a stored ID that names no group is
// ignored.
func TestOrderGroupsLoadsStoredOrder(t *testing.T) {
	groups := map[string]core.ChannelGroup{
		"group-a": {ID: "group-a", Name: "Alpha"},
		"group-b": {ID: "group-b", Name: "Bravo"},
		"group-c": {ID: "group-c", Name: "Charlie"},
	}
	cases := []struct {
		name   string
		stored []string
		want   []string
	}{
		{"stored order exactly", []string{"group-c", "group-a", "group-b"}, []string{"Charlie", "Alpha", "Bravo"}},
		{"missing groups appended by name", []string{"group-b"}, []string{"Bravo", "Alpha", "Charlie"}},
		{"stale IDs ignored", []string{"group-x", "group-b", "group-a"}, []string{"Bravo", "Alpha", "Charlie"}},
		{"no stored order", nil, []string{"Alpha", "Bravo", "Charlie"}},
	}
	for _, c := range cases {
		if got := groupNames(orderGroups(groups, c.stored)); !slices.Equal(got, c.want) {
			t.Errorf("%s: order = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestImportOPMLPathsKeepsDocumentOrder imports an OPML file whose groups are
// not in name order: the sidebar starts in the file's order, that order is
// saved locally, and a relaunch reads it back unchanged. A later import that
// adds groups appends them in its own file order and keeps the order the user
// already has.
func TestImportOPMLPathsKeepsDocumentOrder(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	writeOPML := func(name string, names ...string) string {
		t.Helper()
		var body []byte
		for _, n := range names {
			body = append(body, []byte(`<outline text="`+n+`"/>`)...)
		}
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, []byte(`<?xml version="1.0"?><opml><body>`+string(body)+`</body></opml>`), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}

	first := writeOPML("first.opml", "Zebra", "Alpha", "Mike")
	if err := a.ImportOPMLPaths([]string{first}); err != nil {
		t.Fatalf("first import: %v", err)
	}
	want := []string{"Zebra", "Alpha", "Mike"}
	if got := groupNames(a.Groups); !slices.Equal(got, want) {
		t.Fatalf("groups after import = %q, want the file's order %q", got, want)
	}

	// Relaunch: read settings.json the way LoadApp does and reload the same
	// synced groups.
	settings := defaultSettings()
	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatalf("settings file: %v", err)
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("settings decode: %v", err)
	}
	wantIDs := []string{core.StableGroupID("Zebra"), core.StableGroupID("Alpha"), core.StableGroupID("Mike")}
	if !slices.Equal(settings.GroupOrder, wantIDs) {
		t.Fatalf("saved group order = %q, want %q", settings.GroupOrder, wantIDs)
	}
	relaunched := &App{Dir: dir, Store: a.Store, Settings: settings, Sync: a.Sync}
	if err := relaunched.reload(); err != nil {
		t.Fatalf("relaunch reload: %v", err)
	}
	if got := groupNames(relaunched.Groups); !slices.Equal(got, want) {
		t.Fatalf("groups after relaunch = %q, want %q", got, want)
	}

	// A second import adds groups: they join the end in its file order, while
	// the groups already here keep their order.
	second := writeOPML("second.opml", "Bella", "Aaron")
	if err := a.ImportOPMLPaths([]string{second}); err != nil {
		t.Fatalf("second import: %v", err)
	}
	want = []string{"Zebra", "Alpha", "Mike", "Bella", "Aaron"}
	if got := groupNames(a.Groups); !slices.Equal(got, want) {
		t.Fatalf("groups after second import = %q, want %q", got, want)
	}
}
