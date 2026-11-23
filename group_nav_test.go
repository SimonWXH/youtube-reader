package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
	mui "github.com/simon/youtube-reader/ui"
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
	if n := countText(tt, "Group channel"); n != 0 {
		t.Fatalf("a channel in a collapsed group must not appear again below the groups; got %d matches in %q", n, tt.Texts())
	}
	if err := tt.Click("Toggle Group"); err != nil {
		t.Fatalf("click group fold chevron: %v", err)
	}
	if a.GroupCollapsed("group-1") {
		t.Fatal("clicking fold chevron did not expand group")
	}
	if !a.Settings.ExpandedGroups["group-1"] {
		t.Fatalf("group expansion not saved: %+v", a.Settings.ExpandedGroups)
	}
	if n := countText(tt, "Group channel"); n != 1 {
		t.Fatalf("expanded group should show its channel exactly once; got %d matches in %q", n, tt.Texts())
	}
	if err := tt.Click("Toggle Group"); err != nil {
		t.Fatalf("click fold chevron again: %v", err)
	}
	if !a.GroupCollapsed("group-1") {
		t.Fatal("clicking expanded fold chevron did not collapse it")
	}
	if n := countText(tt, "Group channel"); n != 0 {
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
		if info.Name == "Toggle Group" && info.Role == ui.RoleDisclosure {
			found = true
			if info.Expanded {
				t.Fatal("new group is exposed as expanded")
			}
		}
	}
	if !found {
		t.Fatal("group fold chevron is not exposed as a disclosure")
	}
	if n := countText(tt, "Group channel"); n != 0 {
		t.Fatalf("a grouped channel must not appear outside its group; got %d matches in %q", n, tt.Texts())
	}

	var disclosure ui.AccessInfo
	for i := 0; i < 20; i++ {
		tt.Key(0, ui.KeyTab)
		disclosure, _ = tt.AccessByRole(ui.RoleDisclosure, "Toggle Group")
		if disclosure.Focused {
			break
		}
	}
	if !disclosure.Focused {
		t.Fatal("Tab did not focus the group fold chevron")
	}
	tt.Key(0, ui.KeyEnter)
	if a.GroupCollapsed("group-1") {
		t.Fatal("Enter did not expand the group")
	}
	disclosure, _ = tt.AccessByRole(ui.RoleDisclosure, "Toggle Group")
	if !disclosure.Focused {
		t.Fatal("focus lost after expanding the group")
	}
	if !disclosure.Expanded {
		t.Fatal("expanded group is not exposed as expanded")
	}
	if n := countText(tt, "Group channel"); n != 1 {
		t.Fatalf("expanded group should show its channel exactly once; got %d matches in %q", n, tt.Texts())
	}
	tt.Key(0, ui.KeyEnter)
	if !a.GroupCollapsed("group-1") {
		t.Fatal("Enter did not collapse the group")
	}
	if n := countText(tt, "Group channel"); n != 0 {
		t.Fatalf("collapsed group should remove its channel row; got %d matches in %q", n, tt.Texts())
	}
	disclosure, _ = tt.AccessByRole(ui.RoleDisclosure, "Toggle Group")
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

// countText counts the sidebar rows assistive technology names want: the
// rows' labels, not the text nodes inside them, so a row counts once.
func countText(tt *ui.Tester, want string) int {
	n := 0
	for _, info := range tt.AccessAll() {
		if info.Role == ui.RoleButton && info.Name == want {
			n++
		}
	}
	return n
}

func TestGroupSelectionDecoupledFromCollapse(t *testing.T) {
	v1 := core.Video{ID: "v-1", ChannelID: "ch-1", Title: "Tech Video"}
	v2 := core.Video{ID: "v-2", ChannelID: "ch-2", Title: "Cooking Video"}
	a := &App{
		Dir:      t.TempDir(),
		Selected: "all",
		Settings: core.Settings{APIKey: "test-key", ExpandedGroups: map[string]bool{}},
		Sync: core.SyncState{
			Favorites:  map[string]core.FavoriteRecord{},
			ReadStates: map[string]map[string]core.ReadRecord{},
			Channels: map[string]core.Channel{
				"ch-1": {ID: "ch-1", Title: "Tech Channel"},
				"ch-2": {ID: "ch-2", Title: "Cooking Channel"},
			},
			Groups: map[string]core.ChannelGroup{
				"g-tech": {ID: "g-tech", Name: "Technology", ChannelIDs: []string{"ch-1"}},
			},
		},
		Groups: []core.ChannelGroup{
			{ID: "g-tech", Name: "Technology", ChannelIDs: []string{"ch-1"}},
		},
		Channels: []core.Channel{
			{ID: "ch-1", Title: "Tech Channel"},
			{ID: "ch-2", Title: "Cooking Channel"},
		},
		Videos: []core.Video{v1, v2},
	}
	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.view, 1280, 840)
	tt.Frame()

	if w.title() != "All videos" {
		t.Fatalf("initial title = %q, want All videos", w.title())
	}
	if len(a.VisibleVideos()) != 2 {
		t.Fatalf("all videos visible count = %d, want 2", len(a.VisibleVideos()))
	}
	if !a.GroupCollapsed("g-tech") {
		t.Fatal("group should initially be collapsed")
	}

	// 1. Click group name row: navigates to group, aggregates videos, updates title, does NOT expand group.
	if err := tt.Click("Technology"); err != nil {
		t.Fatalf("click Technology row: %v", err)
	}
	tt.Frame()

	if a.Selected != "group:g-tech" {
		t.Fatalf("after clicking group row, a.Selected = %q, want group:g-tech", a.Selected)
	}
	if w.title() != "Technology" {
		t.Fatalf("after clicking group row, toolbar title = %q, want Technology", w.title())
	}
	if !a.GroupCollapsed("g-tech") {
		t.Fatal("clicking group row must not toggle collapse state")
	}
	visible := a.VisibleVideos()
	if len(visible) != 1 || visible[0].ID != "v-1" {
		t.Fatalf("group visible videos = %+v, want only v-1", visible)
	}

	// 2. Click chevron: expands group, preserves selection.
	if err := tt.Click("Toggle Technology"); err != nil {
		t.Fatalf("click chevron: %v", err)
	}
	tt.Frame()

	if a.GroupCollapsed("g-tech") {
		t.Fatal("clicking chevron did not expand group")
	}
	if a.Selected != "group:g-tech" {
		t.Fatalf("clicking chevron changed selection to %q, want group:g-tech", a.Selected)
	}
	if countText(tt, "Tech Channel") != 1 {
		t.Fatal("expanded group should show Tech Channel row")
	}

	// 3. Click chevron again: collapses group, preserves selection.
	if err := tt.Click("Toggle Technology"); err != nil {
		t.Fatalf("click chevron again: %v", err)
	}
	tt.Frame()

	if !a.GroupCollapsed("g-tech") {
		t.Fatal("clicking chevron again did not collapse group")
	}
	if a.Selected != "group:g-tech" {
		t.Fatalf("clicking chevron again changed selection to %q, want group:g-tech", a.Selected)
	}
	if countText(tt, "Tech Channel") != 0 {
		t.Fatal("collapsed group should hide Tech Channel row")
	}

	// 4. Click group row again: stays on group, does not expand.
	if err := tt.Click("Technology"); err != nil {
		t.Fatalf("click Technology row again: %v", err)
	}
	tt.Frame()
	if !a.GroupCollapsed("g-tech") {
		t.Fatal("clicking group row again must not toggle collapse state")
	}
	if a.Selected != "group:g-tech" {
		t.Fatalf("clicking group row again changed selection to %q", a.Selected)
	}
}

func TestAppRenameGroupPersistsToSyncFile(t *testing.T) {
	dir := t.TempDir()
	syncPath := filepath.Join(dir, "youtube-reader-sync.json")
	a := newTestApp(t, dir)
	defer a.Close()
	a.Settings.SyncPath = syncPath

	g := core.ChannelGroup{ID: "group-1", Name: "Original", ChannelIDs: []string{"ch-1"}, UpdatedAt: 100}
	a.Sync.Groups = map[string]core.ChannelGroup{"group-1": g}
	a.Groups = []core.ChannelGroup{g}
	if err := a.SaveSync(); err != nil {
		t.Fatalf("save initial sync: %v", err)
	}

	if err := a.RenameGroup("group-1", "   "); err == nil {
		t.Fatal("RenameGroup with empty name should return error")
	}
	if err := a.RenameGroup("nonexistent", "New Name"); err == nil {
		t.Fatal("RenameGroup with nonexistent ID should return error")
	}

	if err := a.RenameGroup("group-1", "Renamed Tech"); err != nil {
		t.Fatalf("RenameGroup: %v", err)
	}
	if a.Sync.Groups["group-1"].Name != "Renamed Tech" {
		t.Fatalf("sync group name = %q, want Renamed Tech", a.Sync.Groups["group-1"].Name)
	}
	if a.Groups[0].Name != "Renamed Tech" {
		t.Fatalf("app group name = %q, want Renamed Tech", a.Groups[0].Name)
	}

	// Verify persistence in sync.json
	data, err := os.ReadFile(syncPath)
	if err != nil {
		t.Fatalf("read sync file: %v", err)
	}
	var loaded core.SyncState
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshal sync file: %v", err)
	}
	persisted, ok := loaded.Groups["group-1"]
	if !ok {
		t.Fatalf("group-1 not found in persisted sync: %+v", loaded.Groups)
	}
	if persisted.Name != "Renamed Tech" {
		t.Fatalf("persisted group name = %q, want Renamed Tech", persisted.Name)
	}
	if persisted.UpdatedAt <= 100 {
		t.Fatalf("persisted UpdatedAt = %d, want > 100", persisted.UpdatedAt)
	}
}

func TestGroupContextMenuRenameUIAndPersistence(t *testing.T) {
	dir := t.TempDir()
	syncPath := filepath.Join(dir, "youtube-reader-sync.json")
	a := newTestApp(t, dir)
	defer a.Close()
	a.Settings.SyncPath = syncPath

	g := core.ChannelGroup{ID: "group-1", Name: "Science", ChannelIDs: []string{"ch-1"}, UpdatedAt: 100}
	a.Sync.Groups = map[string]core.ChannelGroup{"group-1": g}
	a.Groups = []core.ChannelGroup{g}
	a.Channels = []core.Channel{{ID: "ch-1", Title: "Science Channel"}}
	a.Selected = "group:group-1"
	if err := a.SaveSync(); err != nil {
		t.Fatalf("save initial sync: %v", err)
	}

	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.view, 1280, 840)
	tt.Frame()

	if w.title() != "Science" {
		t.Fatalf("initial title = %q, want Science", w.title())
	}

	// Right-click on the group row to open context menu.
	if err := tt.RightClick("Science"); err != nil {
		t.Fatalf("right click group row: %v", err)
	}
	menu := tt.Menu()
	if len(menu) == 0 || menu[0] != "Rename" {
		t.Fatalf("context menu items = %q, want [Rename]", menu)
	}

	// Choose "Rename" from menu
	if err := tt.ChooseMenuItem("Rename"); err != nil {
		t.Fatalf("choose Rename: %v", err)
	}

	// Rename text input is now visible and focused.
	// Type new name and press Enter.
	w.renamingGroupName = "Computer Science"
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()

	if a.Sync.Groups["group-1"].Name != "Computer Science" {
		t.Fatalf("sync group name after rename = %q, want Computer Science", a.Sync.Groups["group-1"].Name)
	}
	if a.Groups[0].Name != "Computer Science" {
		t.Fatalf("app group name after rename = %q, want Computer Science", a.Groups[0].Name)
	}
	if w.title() != "Computer Science" {
		t.Fatalf("toolbar title after rename = %q, want Computer Science", w.title())
	}

	// Check persisted file
	data, err := os.ReadFile(syncPath)
	if err != nil {
		t.Fatalf("read sync file: %v", err)
	}
	var loaded core.SyncState
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshal sync file: %v", err)
	}
	if loaded.Groups["group-1"].Name != "Computer Science" {
		t.Fatalf("persisted group name = %q, want Computer Science", loaded.Groups["group-1"].Name)
	}
}

func TestGroupContextMenuRenameChineseLocaleAndCancel(t *testing.T) {
	dir := t.TempDir()
	syncPath := filepath.Join(dir, "youtube-reader-sync.json")
	a := newTestApp(t, dir)
	defer a.Close()
	a.Settings.SyncPath = syncPath

	g := core.ChannelGroup{ID: "group-zh", Name: "科技", ChannelIDs: []string{"ch-1"}, UpdatedAt: 100}
	a.Sync.Groups = map[string]core.ChannelGroup{"group-zh": g}
	a.Groups = []core.ChannelGroup{g}
	_ = a.SaveSync()

	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.view, 1280, 840)
	tt.SetLocale("zh-Hans")
	tt.Frame()

	// Right-click group in Chinese locale shows "重命名"
	if err := tt.RightClick("科技"); err != nil {
		t.Fatalf("right click group row: %v", err)
	}
	menu := tt.Menu()
	if len(menu) == 0 || menu[0] != "重命名" {
		t.Fatalf("Chinese locale menu = %q, want [重命名]", menu)
	}

	if err := tt.ChooseMenuItem("重命名"); err != nil {
		t.Fatalf("choose 重命名: %v", err)
	}

	// In renaming state, modify buffer but press Escape to cancel
	w.renamingGroupName = "新名称"
	tt.Key(0, ui.KeyEscape)
	tt.Frame()

	// Should keep original name
	if a.Sync.Groups["group-zh"].Name != "科技" {
		t.Fatalf("after Escape, name changed to %q, want 科技", a.Sync.Groups["group-zh"].Name)
	}
}

// TestGroupRenameFieldSitsCentredOnItsRow checks the rename row in the real
// sidebar: the field keeps the 26 pt height, its 13 pt line box stays inside
// the field's border, it sits on the row's centre, and the row stops painting
// the selected tint while it is being renamed.
func TestGroupRenameFieldSitsCentredOnItsRow(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()

	g1 := core.ChannelGroup{ID: "group-1", Name: "Science", ChannelIDs: []string{"ch-1"}, UpdatedAt: 100}
	g2 := core.ChannelGroup{ID: "group-2", Name: "Tech", ChannelIDs: []string{"ch-2"}, UpdatedAt: 100}
	a.Sync.Groups = map[string]core.ChannelGroup{"group-1": g1, "group-2": g2}
	a.Groups = []core.ChannelGroup{g1, g2}
	a.Channels = []core.Channel{{ID: "ch-1", Title: "Science Channel"}, {ID: "ch-2", Title: "Tech Channel"}}
	a.Settings.SidebarWidth = defaultSidebarWidth
	a.Selected = "group:group-1"

	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.view, 1280, 840)
	tt.Frame()

	// An unselected group row is the sidebar's own surface behind a fold row.
	tech, ok := tt.AccessByRole(ui.RoleDisclosure, "Toggle Tech")
	if !ok {
		t.Fatal("Tech fold chevron not found")
	}
	idlePixel := tt.Image().RGBAAt(int(tech.Bounds.X-4), int(tech.Bounds.Y+4))

	if err := tt.RightClick("Science"); err != nil {
		t.Fatalf("right click group row: %v", err)
	}
	if err := tt.ChooseMenuItem("Rename"); err != nil {
		t.Fatalf("choose Rename: %v", err)
	}
	tt.Frame()
	// The row's Fade colour transition settles a frame or two after the state
	// flips, as it does on screen.
	tt.Advance(mui.MotionInteraction + 20*time.Millisecond)
	tt.Frame()

	field, ok := tt.AccessByRole(ui.RoleTextField, "Rename Science")
	if !ok {
		t.Fatal("rename text field not found")
	}
	row, ok := tt.AccessByRole(ui.RoleDisclosure, "Toggle Science")
	if !ok {
		t.Fatal("Science fold chevron not found")
	}

	if field.Bounds.H != mui.SidebarRenameFieldHeight {
		t.Fatalf("rename field height = %v, want %v", field.Bounds.H, mui.SidebarRenameFieldHeight)
	}
	if fieldMid, rowMid := field.Bounds.Y+field.Bounds.H/2, row.Bounds.Y+row.Bounds.H/2; fieldMid != rowMid {
		t.Fatalf("field centre %v, row centre %v: the field must sit on the row's centre", fieldMid, rowMid)
	}

	caret, ok := tt.TextCaret()
	if !ok {
		t.Fatal("rename field has no caret, so it is not focused")
	}
	top, bottom := field.Bounds.Y+mui.Hairline, field.Bounds.Y+field.Bounds.H-mui.Hairline
	if caret.Y < top || caret.Y+caret.H > bottom {
		t.Fatalf("13 pt line box %v..%v escapes the field's border %v..%v: descenders are clipped",
			caret.Y, caret.Y+caret.H, top, bottom)
	}

	// The selected row the field replaced no longer paints its tint.
	renamePixel := tt.Image().RGBAAt(int(row.Bounds.X-4), int(row.Bounds.Y+4))
	if renamePixel != idlePixel {
		t.Fatalf("renaming row paints %v, want the sidebar surface %v of an unselected row", renamePixel, idlePixel)
	}
}

func TestGroupRenameDoesNotTriggerGroupDrag(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()

	g1 := core.ChannelGroup{ID: "group-1", Name: "Science", ChannelIDs: []string{"ch-1"}, UpdatedAt: 100}
	g2 := core.ChannelGroup{ID: "group-2", Name: "Tech", ChannelIDs: []string{"ch-2"}, UpdatedAt: 100}
	a.Sync.Groups = map[string]core.ChannelGroup{"group-1": g1, "group-2": g2}
	a.Groups = []core.ChannelGroup{g1, g2}
	a.Channels = []core.Channel{{ID: "ch-1", Title: "Science Channel"}, {ID: "ch-2", Title: "Tech Channel"}}
	a.Settings.SidebarWidth = defaultSidebarWidth

	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.view, 1280, 840)
	tt.Frame()

	if err := tt.RightClick("Science"); err != nil {
		t.Fatalf("right click group row: %v", err)
	}
	if err := tt.ChooseMenuItem("Rename"); err != nil {
		t.Fatalf("choose Rename: %v", err)
	}
	tt.Frame()

	inputField, ok := tt.AccessByRole(ui.RoleTextField, "Rename Science")
	if !ok {
		t.Fatal("rename text field not found")
	}
	techBox := rowBox(t, tt, "Tech")

	dragPointer(tt, inputField.Bounds, techBox.X+20, techBox.Y+techBox.H/2)

	want := []string{"Science", "Tech"}
	if got := groupNames(a.Groups); !slices.Equal(got, want) {
		t.Fatalf("group reordered during rename drag: got %q, want %q", got, want)
	}
	if w.groupDragOrder != nil {
		t.Fatalf("groupDragOrder = %v, want nil", w.groupDragOrder)
	}
}
