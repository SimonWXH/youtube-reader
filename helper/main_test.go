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

func TestQueueOrSaveStateWhenAppHoldsLock(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	syncPath := filepath.Join(dir, "sync.json")
	settings := core.Settings{SyncPath: syncPath, RefreshMinutes: 60}
	state := core.SyncState{
		SchemaVersion: 1,
		Channels:      map[string]core.Channel{},
		ReadStates:    map[string]map[string]core.ReadRecord{},
		Favorites:     map[string]core.FavoriteRecord{},
	}
	lockDir := localLockDirFor(syncPath)
	lock, err := core.AcquireLock(lockDir)
	if err != nil {
		t.Fatal(err)
	}
	ch := core.Channel{ID: "ch-1", Title: "Channel", UpdatedAt: 99}
	if err := queueOrSaveState(context.Background(), dir, settings, state, []core.Channel{ch}); err != nil {
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
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestQueueOrSaveStateWritesSyncWhenUnlocked(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	syncPath := filepath.Join(dir, "sync.json")
	settings := core.Settings{SyncPath: syncPath, RefreshMinutes: 60}
	state := core.SyncState{
		SchemaVersion: 1,
		Channels:      map[string]core.Channel{},
		ReadStates:    map[string]map[string]core.ReadRecord{},
		Favorites:     map[string]core.FavoriteRecord{},
	}
	ch := core.Channel{ID: "ch-1", Title: "Channel", UpdatedAt: 99}
	if err := queueOrSaveState(context.Background(), dir, settings, state, []core.Channel{ch}); err != nil {
		t.Fatal(err)
	}
	loaded, err := core.LoadSyncWithConflicts(syncPath, "device")
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Channels["ch-1"].Title; got != "Channel" {
		t.Fatalf("written channel title = %q", got)
	}
}
