package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func TestGroupCollapseSettingsStayConcurrentSafe(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	a.Settings.ExpandedGroups = map[string]bool{}

	var wg sync.WaitGroup
	wg.Add(2)
	errCh := make(chan error, 2)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			if err := a.SaveSettings(); err != nil {
				errCh <- fmt.Errorf("save settings: %w", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			collapsed := i%2 == 0
			groupID := fmt.Sprintf("group-%d", i%4)
			if err := a.SetGroupCollapsed(groupID, collapsed); err != nil {
				errCh <- fmt.Errorf("set group collapsed: %w", err)
				return
			}
			if a.GroupCollapsed(groupID) != collapsed {
				errCh <- fmt.Errorf("unexpected state for %s", groupID)
				return
			}
		}
	}()
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
}

func TestSettingsConcurrentReadersAndWritersStayConsistent(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()

	var wg sync.WaitGroup
	wg.Add(4)
	errCh := make(chan error, 4)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			if err := a.SaveSettings(); err != nil {
				errCh <- fmt.Errorf("save settings: %w", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_, _ = a.updateSettings(func(s *core.Settings) bool {
				s.RecentFailures = []core.RefreshFailure{{At: "2026-03-01T12:00:00Z", Message: fmt.Sprintf("failure %d", i)}}
				s.NotificationIDs = []string{fmt.Sprintf("channel-%d", i)}
				s.ExpandedGroups = map[string]bool{fmt.Sprintf("group-%d", i): true}
				return true
			})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = a.settingsSnapshot()
			_ = a.notificationEnabled("channel-1")
			_ = a.VisibleVideos()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			if err := a.SetGroupCollapsed(fmt.Sprintf("group-%d", i), i%2 == 0); err != nil {
				errCh <- fmt.Errorf("set group collapsed: %w", err)
				return
			}
		}
	}()
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatalf("settings file: %v", err)
	}
	var persisted core.Settings
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("unmarshal settings: %v", err)
	}
	if len(persisted.ExpandedGroups) == 0 || len(persisted.NotificationIDs) == 0 || len(persisted.RecentFailures) == 0 {
		t.Fatalf("persisted settings lost state: %+v", persisted)
	}
}

func TestCardChannelNameOpensChannelAndPreservesCardSelection(t *testing.T) {
	video := core.Video{ID: "video-1", ChannelID: "UC_1", ChannelTitle: "The PrimeTime", Title: "Episode"}
	other := core.Video{ID: "video-2", ChannelID: "UC_2", ChannelTitle: "Other channel", Title: "Another episode"}
	a := &App{
		Dir:             t.TempDir(),
		Selected:        "all",
		Settings:        core.Settings{APIKey: "test-key", ExpandedGroups: map[string]bool{}},
		SelectedVideoID: other.ID,
		Sync: core.SyncState{
			Favorites:  map[string]core.FavoriteRecord{},
			ReadStates: map[string]map[string]core.ReadRecord{},
			Channels:   map[string]core.Channel{"ch-1": {ID: "ch-1", Title: "Group channel"}},
		},
		Groups:   []core.ChannelGroup{{ID: "group-1", Name: "Group", ChannelIDs: []string{"ch-1"}}},
		Channels: []core.Channel{{ID: "ch-1", Title: "Group channel"}},
		Videos:   []core.Video{video, other},
	}
	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.view, 1280, 840)
	tt.Frame()
	if n := countText(tt, "Group channel"); n != 1 {
		t.Fatalf("collapsed group should not render its channel row; got %d matches in %q", n, tt.Texts())
	}
	if err := tt.Click("Group"); err != nil {
		t.Fatalf("click group header: %v", err)
	}
	if a.GroupCollapsed("group-1") {
		t.Fatal("clicking group header did not expand group")
	}
	if !a.Settings.ExpandedGroups["group-1"] {
		t.Fatalf("group expansion not saved: %+v", a.Settings.ExpandedGroups)
	}
	if n := countText(tt, "Group channel"); n != 2 {
		t.Fatalf("expanded group should add its channel row; got %d matches in %q", n, tt.Texts())
	}
	if err := tt.Click("Group"); err != nil {
		t.Fatalf("click group header again: %v", err)
	}
	if !a.GroupCollapsed("group-1") {
		t.Fatal("clicking expanded group header did not collapse it")
	}
	if n := countText(tt, "Group channel"); n != 1 {
		t.Fatalf("collapsed group should remove its channel row; got %d matches in %q", n, tt.Texts())
	}

	if err := tt.Click("The PrimeTime"); err != nil {
		t.Fatalf("click channel name: %v", err)
	}
	urls := tt.OpenedURLs()
	if len(urls) != 1 || urls[0] != "https://www.youtube.com/channel/UC_1" {
		t.Fatalf("opened URLs = %q", urls)
	}
	if a.SelectedVideoID != other.ID {
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
		Dir:      t.TempDir(),
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
	if n := countText(tt, "Group channel"); n != 1 {
		t.Fatalf("collapsed group should not render its channel row; got %d matches in %q", n, tt.Texts())
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
	if n := countText(tt, "Group channel"); n != 2 {
		t.Fatalf("expanded group should add its channel row; got %d matches in %q", n, tt.Texts())
	}
	tt.Key(0, ui.KeyEnter)
	if !a.GroupCollapsed("group-1") {
		t.Fatal("Enter did not collapse the group")
	}
	if n := countText(tt, "Group channel"); n != 1 {
		t.Fatalf("collapsed group should remove its channel row; got %d matches in %q", n, tt.Texts())
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
	other := core.Video{ID: "video-2", ChannelID: "UC_2", ChannelTitle: "Other channel", Title: "Another episode"}
	a := &App{
		Dir:             t.TempDir(),
		Selected:        "all",
		Settings:        core.Settings{APIKey: "test-key"},
		SelectedVideoID: other.ID,
		Sync: core.SyncState{
			Favorites:  map[string]core.FavoriteRecord{},
			ReadStates: map[string]map[string]core.ReadRecord{},
			Channels:   map[string]core.Channel{"UC_1": {ID: "UC_1", Title: "The PrimeTime"}},
		},
		Videos: []core.Video{video, other},
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
	if a.SelectedVideoID != other.ID {
		t.Fatalf("card selection changed to %q", a.SelectedVideoID)
	}
}

func TestCardChannelNameHandlesEmptyChannelID(t *testing.T) {
	video := core.Video{ID: "video-1", ChannelTitle: "Unknown", Title: "Episode"}
	a := &App{
		Dir:      t.TempDir(),
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

func countText(tt *ui.Tester, want string) int {
	n := 0
	for _, text := range tt.Texts() {
		if text == want {
			n++
		}
	}
	return n
}
