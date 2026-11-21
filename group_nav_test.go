// Package main tests for local UI preferences and channel link navigation.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
)

func TestGroupCollapseDefaultsCollapsedAndPersists(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	a.Settings.ExpandedGroups = nil

	if !a.GroupCollapsed("group-1") {
		t.Fatal("new group should default to collapsed")
	}
	if err := a.SetGroupCollapsed("group-1", false); err != nil {
		t.Fatalf("expand group: %v", err)
	}
	if a.GroupCollapsed("group-1") {
		t.Fatal("expanded group should not be collapsed")
	}
	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatalf("settings file: %v", err)
	}
	var settings core.Settings
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("settings JSON: %v", err)
	}
	if !settings.ExpandedGroups["group-1"] {
		t.Fatalf("expanded group not persisted: %+v", settings.ExpandedGroups)
	}

	if err := a.SetGroupCollapsed("group-1", true); err != nil {
		t.Fatalf("collapse group: %v", err)
	}
	data, err = os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	if settings.ExpandedGroups["group-1"] {
		t.Fatalf("collapsed group persisted as expanded: %+v", settings.ExpandedGroups)
	}

	a.Settings.ExpandedGroups = nil
	if !a.GroupCollapsed("group-1") {
		t.Fatal("unknown groups must default to collapsed")
	}
}

func TestCardChannelNameOpensChannelAndPreservesCardSelection(t *testing.T) {
	video := core.Video{ID: "video-1", ChannelID: "UC_1", ChannelTitle: "The PrimeTime", Title: "Episode"}
	a := &App{
		Selected:        "all",
		Settings:        core.Settings{APIKey: "test-key", ExpandedGroups: map[string]bool{}},
		SelectedVideoID: video.ID,
		Sync: core.SyncState{
			Favorites:  map[string]core.FavoriteRecord{},
			ReadStates: map[string]map[string]core.ReadRecord{},
			Channels:   map[string]core.Channel{"ch-1": {ID: "ch-1", Title: "Group channel"}},
		},
		Groups:   []core.ChannelGroup{{ID: "group-1", Name: "Group", ChannelIDs: []string{"ch-1"}}},
		Channels: []core.Channel{{ID: "ch-1", Title: "Group channel"}},
		Videos:   []core.Video{video},
	}
	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.view, 1280, 840)
	tt.Frame()
	if err := tt.Click("Group"); err != nil {
		t.Fatalf("click group header: %v", err)
	}
	if a.GroupCollapsed("group-1") {
		t.Fatal("clicking group header did not expand group")
	}
	if !a.Settings.ExpandedGroups["group-1"] {
		t.Fatalf("group expansion not saved: %+v", a.Settings.ExpandedGroups)
	}
	tt.Frame()
	found := false
	for _, text := range tt.Texts() {
		if text == "Group channel" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expanded group does not render channels: %q", tt.Texts())
	}
	if err := tt.Click("Group"); err != nil {
		t.Fatalf("click group header again: %v", err)
	}
	if !a.GroupCollapsed("group-1") {
		t.Fatal("clicking expanded group header did not collapse it")
	}

	if err := tt.Click("The PrimeTime"); err != nil {
		t.Fatalf("click channel name: %v", err)
	}
	urls := tt.OpenedURLs()
	if len(urls) != 1 || urls[0] != "https://www.youtube.com/channel/UC_1" {
		t.Fatalf("opened URLs = %q", urls)
	}
	if a.SelectedVideoID != video.ID {
		t.Fatalf("card selection changed to %q", a.SelectedVideoID)
	}
	if a.Sync.IsRead(video.ID) {
		t.Fatal("clicking channel name marked video read")
	}
	if a.Selected != "all" || a.ChannelFilter != "" {
		t.Fatalf("sidebar navigation changed: %q, %q", a.Selected, a.ChannelFilter)
	}
}

func TestGroupHeaderKeyboardToggleKeepsFocus(t *testing.T) {
	a := &App{
		Settings: core.Settings{APIKey: "test-key", ExpandedGroups: map[string]bool{}},
		Groups:   []core.ChannelGroup{{ID: "group-1", Name: "Group", ChannelIDs: []string{"ch-1"}}},
		Channels: []core.Channel{{ID: "ch-1", Title: "Group channel"}},
	}
	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.view, 1280, 840)
	tt.UseScreenReader()
	tt.Frame()

	found := false
	for _, info := range tt.AccessAll() {
		if info.Name == "Group" && info.Role == ui.RoleDisclosure {
			found = true
			if info.Expanded {
				t.Fatal("new group is exposed as expanded")
			}
		}
	}
	if !found {
		t.Fatal("group header is not exposed as a disclosure")
	}

	var disclosure ui.AccessInfo
	for i := 0; i < 20; i++ {
		tt.Key(0, ui.KeyTab)
		disclosure, _ = tt.AccessByRole(ui.RoleDisclosure, "Group")
		if disclosure.Focused {
			break
		}
	}
	if !disclosure.Focused {
		t.Fatal("Tab did not focus the group header")
	}
	tt.Key(0, ui.KeyEnter)
	if a.GroupCollapsed("group-1") {
		t.Fatal("Enter did not expand the group")
	}
	disclosure, _ = tt.AccessByRole(ui.RoleDisclosure, "Group")
	if !disclosure.Focused {
		t.Fatal("focus lost after expanding the group")
	}
	if !disclosure.Expanded {
		t.Fatal("expanded group is not exposed as expanded")
	}
	tt.Key(0, ui.KeyEnter)
	if !a.GroupCollapsed("group-1") {
		t.Fatal("Enter did not collapse the group")
	}
	disclosure, _ = tt.AccessByRole(ui.RoleDisclosure, "Group")
	if !disclosure.Focused {
		t.Fatal("focus lost after collapsing the group")
	}
	if disclosure.Expanded {
		t.Fatal("collapsed group is still exposed as expanded")
	}
}

func TestCardChannelNameKeyboardOpensChannel(t *testing.T) {
	video := core.Video{ID: "video-1", ChannelID: "UC_1", ChannelTitle: "The PrimeTime", Title: "Episode"}
	a := &App{
		Selected:        "all",
		Settings:        core.Settings{APIKey: "test-key"},
		SelectedVideoID: video.ID,
		Sync: core.SyncState{
			Favorites:  map[string]core.FavoriteRecord{},
			ReadStates: map[string]map[string]core.ReadRecord{},
			Channels:   map[string]core.Channel{"UC_1": {ID: "UC_1", Title: "The PrimeTime"}},
		},
		Videos: []core.Video{video},
	}
	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.view, 1280, 840)
	tt.UseScreenReader()
	tt.Frame()

	link, ok := tt.AccessByRole(ui.RoleLink, "The PrimeTime")
	if !ok || !link.Focusable {
		t.Fatalf("channel name is not exposed as a focusable link: %+v, %v", link, ok)
	}

	for i := 0; i < 20; i++ {
		tt.Key(0, ui.KeyTab)
		link, _ = tt.AccessByRole(ui.RoleLink, "The PrimeTime")
		if link.Focused {
			break
		}
	}
	if !link.Focused {
		t.Fatal("Tab did not focus the channel name")
	}
	tt.Key(0, ui.KeyEnter)
	urls := tt.OpenedURLs()
	if len(urls) != 1 || urls[0] != "https://www.youtube.com/channel/UC_1" {
		t.Fatalf("opened URLs = %q", urls)
	}
	if a.SelectedVideoID != video.ID {
		t.Fatalf("card selection changed to %q", a.SelectedVideoID)
	}
}

func TestCardChannelNameHandlesEmptyChannelID(t *testing.T) {
	video := core.Video{ID: "video-1", ChannelTitle: "Unknown", Title: "Episode"}
	a := &App{
		Selected: "all",
		Settings: core.Settings{APIKey: "test-key"},
		Sync: core.SyncState{
			Favorites:  map[string]core.FavoriteRecord{},
			ReadStates: map[string]map[string]core.ReadRecord{},
			Channels:   map[string]core.Channel{"ch-x": {ID: "ch-x", Title: "Other"}},
		},
		Videos: []core.Video{video},
	}
	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.view, 1280, 840)
	tt.Frame()

	if err := tt.Click("Unknown"); err != nil {
		t.Fatalf("click channel name: %v", err)
	}
	for _, url := range tt.OpenedURLs() {
		if strings.Contains(url, "channel/") {
			t.Fatalf("opened invalid channel URL: %q", url)
		}
	}
}
