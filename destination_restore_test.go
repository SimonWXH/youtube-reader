package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
)

// launchHome gives the test its own home directory, so appDir and the lock
// directory are the launch's own and a launch reads the files the test wrote.
// The path is fixed before anything asks for it: appDir resolves $HOME.
func launchHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := appDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

// writeLaunchSettings writes the settings.json a launch reads. Not calling it
// is what a fresh install looks like: no file, so the defaults apply.
func writeLaunchSettings(t *testing.T, dir string, settings core.Settings) {
	t.Helper()
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// seedLaunchLibrary writes the sync file and the store a launch loads: the
// groups from the sync file and the channel rows the sidebar builds its
// destinations from.
func seedLaunchLibrary(t *testing.T, dir string, groups []core.ChannelGroup, channels []core.Channel) {
	t.Helper()
	state := core.SyncState{
		SchemaVersion: 1,
		Groups:        map[string]core.ChannelGroup{},
		Channels:      map[string]core.Channel{},
		ReadStates:    map[string]map[string]core.ReadRecord{},
		Favorites:     map[string]core.FavoriteRecord{},
	}
	for _, g := range groups {
		state.Groups[g.ID] = g
	}
	for _, ch := range channels {
		state.Channels[ch.ID] = ch
	}
	syncPath := filepath.Join(dir, "youtube-reader-sync.json")
	if err := core.SaveSyncAtomic(syncPath, state); err != nil {
		t.Fatal(err)
	}
	if len(channels) == 0 {
		return
	}
	ctx := context.Background()
	store, err := core.OpenStore(ctx, filepath.Join(dir, "videos.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CommitRefresh(ctx, core.RefreshCommit{Channels: channels}); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

// storedDestination reads the destination the launch left in settings.json.
func storedDestination(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(appDir(), "settings.json"))
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}
	var settings core.Settings
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("unmarshal settings.json: %v", err)
	}
	return settings.LastSelectedDestination
}

func TestLaunchRestoresTheSelectedDestination(t *testing.T) {
	groups := []core.ChannelGroup{{ID: "g-tech", Name: "Technology", ChannelIDs: []string{"ch-1"}}}
	channels := []core.Channel{{ID: "ch-1", Title: "Tech Channel"}}
	cases := []struct {
		name   string
		stored string
		want   string
	}{
		{name: "all videos", stored: "all", want: "all"},
		{name: "favorites", stored: "favorites", want: "favorites"},
		{name: "group", stored: "group:g-tech", want: "group:g-tech"},
		{name: "channel", stored: "channel:ch-1", want: "channel:ch-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := launchHome(t)
			writeLaunchSettings(t, dir, core.Settings{LastSelectedDestination: tc.stored})
			seedLaunchLibrary(t, dir, groups, channels)
			a, err := LoadApp()
			if err != nil {
				t.Fatalf("LoadApp: %v", err)
			}
			defer a.Close()
			if a.Selected != tc.want {
				t.Fatalf("launch selected %q, want %q", a.Selected, tc.want)
			}
		})
	}
}

func TestLaunchFallsBackToAllVideosWhenTheDestinationIsGone(t *testing.T) {
	groups := []core.ChannelGroup{{ID: "g-tech", Name: "Technology", ChannelIDs: []string{"ch-1"}}}
	channels := []core.Channel{{ID: "ch-1", Title: "Tech Channel"}}
	cases := []struct {
		name   string
		stored string
	}{
		{name: "deleted group", stored: "group:g-gone"},
		{name: "deleted channel", stored: "channel:ch-gone"},
		{name: "group without an id", stored: "group:"},
		{name: "unknown kind", stored: "search:cats"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := launchHome(t)
			writeLaunchSettings(t, dir, core.Settings{LastSelectedDestination: tc.stored})
			seedLaunchLibrary(t, dir, groups, channels)
			a, err := LoadApp()
			if err != nil {
				t.Fatalf("LoadApp: %v", err)
			}
			defer a.Close()
			if a.Selected != "all" {
				t.Fatalf("launch selected %q for stored %q, want all", a.Selected, tc.stored)
			}
		})
	}
}

func TestFreshInstallLaunchesOnAllVideos(t *testing.T) {
	dir := launchHome(t)
	seedLaunchLibrary(t, dir, nil, nil)
	a, err := LoadApp()
	if err != nil {
		t.Fatalf("LoadApp: %v", err)
	}
	defer a.Close()
	if a.Selected != "all" {
		t.Fatalf("fresh install selected %q, want all", a.Selected)
	}
}

// The sidebar is the destination's entry point, so clicking a row is what has
// to persist: the next launch opens where this one left off.
func TestSidebarSelectionIsRemembered(t *testing.T) {
	dir := launchHome(t)
	seedLaunchLibrary(t, dir,
		[]core.ChannelGroup{{ID: "g-tech", Name: "Technology", ChannelIDs: []string{"ch-1"}}},
		[]core.Channel{{ID: "ch-1", Title: "Tech Channel"}})
	a, err := LoadApp()
	if err != nil {
		t.Fatalf("LoadApp: %v", err)
	}
	defer a.Close()
	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.view, 1280, 840)
	tt.Frame()

	steps := []struct{ row, want string }{
		{row: "Technology", want: "group:g-tech"},
		{row: "Favorites", want: "favorites"},
		{row: "All videos", want: "all"},
	}
	for _, step := range steps {
		if err := tt.Click(step.row); err != nil {
			t.Fatalf("click %q: %v", step.row, err)
		}
		tt.Frame()
		if a.Selected != step.want {
			t.Fatalf("after clicking %q, Selected = %q, want %q", step.row, a.Selected, step.want)
		}
		if stored := storedDestination(t); stored != step.want {
			t.Fatalf("after clicking %q, stored destination = %q, want %q", step.row, stored, step.want)
		}
	}
}

// A click on a notification opens its channel, and that is the destination the
// next launch restores, over the one the launch itself restored.
func TestNotificationChannelBeatsTheRestoredDestination(t *testing.T) {
	dir := launchHome(t)
	writeLaunchSettings(t, dir, core.Settings{LastSelectedDestination: "favorites"})
	seedLaunchLibrary(t, dir,
		[]core.ChannelGroup{{ID: "g-tech", Name: "Technology", ChannelIDs: []string{"ch-1"}}},
		[]core.Channel{{ID: "ch-1", Title: "Tech Channel"}})
	a, err := LoadApp()
	if err != nil {
		t.Fatalf("LoadApp: %v", err)
	}
	defer a.Close()
	if a.Selected != "favorites" {
		t.Fatalf("launch selected %q, want favorites", a.Selected)
	}
	if !a.SelectChannel("ch-1") {
		t.Fatal("SelectChannel found no known channel")
	}
	if a.Selected != "channel:ch-1" {
		t.Fatalf("after the notification click, Selected = %q, want channel:ch-1", a.Selected)
	}
	if stored := storedDestination(t); stored != "channel:ch-1" {
		t.Fatalf("after the notification click, stored destination = %q, want channel:ch-1", stored)
	}
}
