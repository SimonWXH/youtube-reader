package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/simon/youtube-reader/core"
)

func TestHelperRunDryRunWritesAtomicHandoff(t *testing.T) {
	dir := t.TempDir()
	h, err := HelperRun(context.Background(), dir, true, time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC), nil)
	if err != nil {
		t.Fatal(err)
	}
	if h.ID == "" || h.CreatedAt != "2026-03-01T12:00:00Z" {
		t.Fatalf("handoff: %+v", h)
	}
	data, err := os.ReadFile(filepath.Join(dir, "inbox.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got HelperHandoff
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != h.ID || got.Description != "Dry run: no refresh performed" {
		t.Fatalf("persisted handoff: %+v", got)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Fatalf("left temporary file: %s", e.Name())
		}
	}
}

func TestHelperRunNoAPIKeyDoesNotCallNetwork(t *testing.T) {
	dir := t.TempDir()
	if err := writeSettings(dir, core.Settings{
		SyncPath:       filepath.Join(dir, "sync.json"),
		RefreshMinutes: 60,
	}); err != nil {
		t.Fatal(err)
	}
	h, err := HelperRun(context.Background(), dir, false, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if h.Description != "Add a YouTube API key in Settings to refresh." {
		t.Fatalf("description: %q", h.Description)
	}
	if h.Result != nil {
		t.Fatal("unexpected refresh result")
	}
}

// TestHelperRunStaleQuotaHistoryDoesNotRearmPause proves a quota pause is
// armed only by the failure the refresh itself produced: after the pause the
// stored quota failure announces has expired, a refresh that fails on the
// transport carries that failure as history and must not arm a new pause.
func TestHelperRunStaleQuotaHistoryDoesNotRearmPause(t *testing.T) {
	dir := t.TempDir()
	syncPath := filepath.Join(dir, "sync.json")
	state := core.SyncState{
		SchemaVersion: 1,
		DeviceID:      "device",
		UpdatedAt:     1,
		Channels:      map[string]core.Channel{"ch-1": {ID: "ch-1", Title: "Channel"}},
	}
	if err := core.SaveSyncAtomic(syncPath, state); err != nil {
		t.Fatal(err)
	}
	expired := "2026-03-02T12:00:00Z"
	history := core.RefreshFailure{At: "2026-03-01T12:00:00Z", Message: "quota paused until 2026-03-02T12:00:00Z"}
	if err := writeSettings(dir, core.Settings{
		APIKey:           "key",
		RefreshMinutes:   60,
		SyncPath:         syncPath,
		QuotaPausedUntil: expired,
		RecentFailures:   []core.RefreshFailure{history},
	}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"message":"backend error"}}`))
	}))
	defer srv.Close()
	refresh, network := refreshForHelper, networkAvailableForHelper
	refreshForHelper = func(ctx context.Context, client core.YouTube, settings core.Settings, sync core.SyncState, store *core.Store, existing []core.Video, revision int64, force bool, now time.Time) core.RefreshResult {
		client.BaseURL = srv.URL + "/youtube/v3"
		return core.RefreshYoutube(ctx, client, settings, sync, store, existing, revision, force, now)
	}
	networkAvailableForHelper = func(context.Context) bool { return true }
	defer func() { refreshForHelper, networkAvailableForHelper = refresh, network }()

	now := time.Date(2026, 3, 3, 12, 0, 0, 0, time.UTC)
	if _, err := HelperRun(context.Background(), dir, false, now, nil); err != nil {
		t.Fatal(err)
	}
	persisted := loadHelperSettings(dir)
	if persisted.QuotaPausedUntil != expired {
		t.Fatalf("carried quota history re-armed the pause: %q", persisted.QuotaPausedUntil)
	}
	if len(persisted.RecentFailures) != 2 ||
		persisted.RecentFailures[0].Message != "backend error" ||
		persisted.RecentFailures[1].Message != history.Message {
		t.Fatalf("failures: %+v", persisted.RecentFailures)
	}
}

func TestHelperRunManualDoesNotRefresh(t *testing.T) {
	dir := t.TempDir()
	if err := writeSettings(dir, core.Settings{
		SyncPath:       filepath.Join(dir, "sync.json"),
		RefreshMinutes: 0,
		APIKey:         "test-key",
	}); err != nil {
		t.Fatal(err)
	}
	previous := refreshForHelper
	refreshForHelper = func(context.Context, core.YouTube, core.Settings, core.SyncState, *core.Store, []core.Video, int64, bool, time.Time) core.RefreshResult {
		t.Error("Manual ran a background refresh")
		return core.RefreshResult{}
	}
	previousNetwork := networkAvailableForHelper
	networkAvailableForHelper = func(context.Context) bool { return true }
	t.Cleanup(func() {
		refreshForHelper = previous
		networkAvailableForHelper = previousNetwork
	})

	h, err := HelperRun(context.Background(), dir, false, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if h.Result != nil {
		t.Fatalf("Manual produced a refresh result: %+v", h.Result)
	}
	if h.Description == "" {
		t.Fatal("Manual produced no explanation")
	}
}

func TestCommitHelperResultQueuesChannelsWhenAppHoldsLock(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	syncPath := filepath.Join(dir, "sync.json")
	if err := writeSettings(dir, core.Settings{SyncPath: syncPath, RefreshMinutes: 60}); err != nil {
		t.Fatal(err)
	}
	lock, err := core.AcquireLock(localLockDirFor(syncPath))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	ch := core.Channel{ID: "ch-1", Title: "Channel", UpdatedAt: 99}
	result := core.RefreshResult{Changed: true, Channels: []core.Channel{ch}, Description: "Refreshed 1 videos"}
	if err := commitHelperResult(context.Background(), dir, result, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(syncPath); !os.IsNotExist(err) {
		t.Fatalf("sync JSON was written while the app held the lock: %v", err)
	}
	store, err := core.OpenStore(context.Background(), filepath.Join(dir, "videos.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	items, err := store.DrainQueue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("queue length = %d, want 1", len(items))
	}
	var payload struct {
		State core.SyncState `json:"state"`
	}
	if err := json.Unmarshal(items[0], &payload); err != nil {
		t.Fatal(err)
	}
	if got := payload.State.Channels["ch-1"].Title; got != "Channel" {
		t.Fatalf("queued channel title = %q", got)
	}
	if len(payload.State.ReadStates) != 0 || len(payload.State.Favorites) != 0 {
		t.Fatalf("queued stale state: %+v", payload.State)
	}
}

func TestCommitHelperResultMergesFreshSyncState(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	syncPath := filepath.Join(dir, "sync.json")
	if err := writeSettings(dir, core.Settings{SyncPath: syncPath, RefreshMinutes: 60}); err != nil {
		t.Fatal(err)
	}
	current := core.SyncState{
		SchemaVersion: 1,
		UpdatedAt:     100,
		Channels:      map[string]core.Channel{"ch-1": {ID: "ch-1", Title: "Old", UpdatedAt: 1}},
		ReadStates:    map[string]map[string]core.ReadRecord{"v1": {"device": {VideoID: "v1", DeviceID: "device", Read: true, UpdatedAt: 50}}},
		Favorites:     map[string]core.FavoriteRecord{"v2": {VideoID: "v2", UpdatedAt: 50}},
	}
	if err := core.SaveSyncAtomic(syncPath, current); err != nil {
		t.Fatal(err)
	}
	result := core.RefreshResult{Changed: true, Channels: []core.Channel{{ID: "ch-1", Title: "Channel", UpdatedAt: 200}}}
	if err := commitHelperResult(context.Background(), dir, result, time.Now()); err != nil {
		t.Fatal(err)
	}
	loaded, err := core.LoadSyncWithConflicts(syncPath, "device")
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Channels["ch-1"].Title; got != "Channel" {
		t.Fatalf("written channel title = %q", got)
	}
	if !loaded.IsRead("v1") {
		t.Fatal("concurrent read state was lost")
	}
	if _, ok := loaded.Favorites["v2"]; !ok {
		t.Fatal("concurrent favorite was lost")
	}
}

func TestCommitHelperResultLeavesRefreshAndHandoffOwnership(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	syncPath := filepath.Join(dir, "sync.json")
	last := "2026-03-01T10:00:00Z"
	if err := writeSettings(dir, core.Settings{SyncPath: syncPath, RefreshMinutes: 240, LastRefresh: last, LastHandoffID: "app-owned"}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	failure := core.RefreshFailure{At: now.Format(time.RFC3339), Message: "network unavailable"}
	if err := commitHelperResult(context.Background(), dir, core.RefreshResult{Failures: []core.RefreshFailure{failure}}, now); err != nil {
		t.Fatal(err)
	}
	settings := loadHelperSettings(dir)
	if settings.LastRefresh != last || settings.LastHandoffID != "app-owned" {
		t.Fatalf("failed refresh advanced status: %+v", settings)
	}
	if len(settings.RecentFailures) != 1 {
		t.Fatalf("failures not persisted: %+v", settings.RecentFailures)
	}
	if err := commitHelperResult(context.Background(), dir, core.RefreshResult{Changed: true}, now); err != nil {
		t.Fatal(err)
	}
	settings = loadHelperSettings(dir)
	if settings.LastRefresh != now.Format(time.RFC3339) {
		t.Fatalf("successful refresh status: %+v", settings)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var persisted map[string]any
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatal(err)
	}
	if _, ok := persisted["nextRefresh"]; ok {
		t.Fatal("helper wrote a next refresh stamp")
	}
	if settings.LastHandoffID != "app-owned" {
		t.Fatalf("helper changed app-owned handoff ID: %+v", settings)
	}
}

func TestEscapeAppleScript(t *testing.T) {
	got := escapeAppleScript("2 new videos\nA \"quote\" \\ path")
	want := "2 new videos\\nA \\\"quote\\\" \\\\ path"
	if got != want {
		t.Fatalf("escaped = %q, want %q", got, want)
	}
}
