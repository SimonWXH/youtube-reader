package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
	mui "github.com/simon/youtube-reader/ui"
)

func TestRefreshedLabel(t *testing.T) {
	old := time.Now().Add(-72 * time.Hour).Truncate(time.Second)
	tests := []struct {
		name  string
		stamp string
		want  string
	}{
		{"empty", "", ""},
		{"invalid", "not-a-time", "Last refresh not-a-time"},
		{"just now", time.Now().UTC().Format(time.RFC3339), "Last refresh just now"},
		{"minutes", time.Now().Add(-5 * time.Minute).UTC().Format(time.RFC3339), "Last refresh 5 min ago"},
		{"hours", time.Now().Add(-3 * time.Hour).UTC().Format(time.RFC3339), "Last refresh 3 hr ago"},
		{"older than a day", old.UTC().Format(time.RFC3339), "Last refresh on " + old.Local().Format("Jan 2, 2006 at 15:04")},
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
	a.Videos = []core.Video{{ID: "v-1", ChannelID: "ch-1"}}
	a.Selected = "all"
	a.ChannelFilter = "ch-other"
	a.Search = "old"
	a.UnreadOnly = true
	a.SelectedVideoID = "v-1"

	registerNotificationClick(a)
	notificationClickHandler(a)(core.ChannelNotificationID("ch-1"))
	if a.Selected != "channel:ch-1" || a.ChannelFilter != "" || a.Search != "" || a.UnreadOnly || a.SelectedVideoID != "" {
		t.Fatalf("id route: %+v", a)
	}
	if got := a.VisibleVideos(); len(got) != 1 || got[0].ID != "v-1" {
		t.Fatalf("routed videos = %+v", got)
	}
}

func TestNotificationClickHandlerDoesNotRouteOnOrdinaryLaunch(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	a.Channels = []core.Channel{{ID: "ch-2"}}
	a.Videos = []core.Video{{ID: "v-2", ChannelID: "ch-2"}}
	a.Selected = "all"
	click := core.NewNotificationClick(core.NotificationPlan{ChannelID: "ch-2"}, time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC))
	data, _ := json.Marshal(core.NotificationClicks{Clicks: []core.NotificationClick{click}})
	if err := os.WriteFile(filepath.Join(dir, "notification-clicks.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	// realMain only registers the listener on launch; it no longer consumes
	// the queue. A pending helper post alone must not change the selection.
	registerNotificationClick(a)
	if a.Selected != "all" {
		t.Fatalf("ordinary launch routed to %q", a.Selected)
	}
}

func TestNotificationClickHandlerRoutesWarmAndColdChannelIDs(t *testing.T) {
	for _, name := range []string{"warm", "cold"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			a := newTestApp(t, dir)
			defer a.Close()
			a.Channels = []core.Channel{{ID: "ch-2"}}
			a.Videos = []core.Video{{ID: "v-2", ChannelID: "ch-2"}}
			a.Selected = "all"
			a.ChannelFilter = "ch-other"
			off := registerNotificationClick(a)
			defer off()
			notificationClickHandler(a)(core.ChannelNotificationID("ch-2"))
			if a.Selected != "channel:ch-2" || a.ChannelFilter != "" {
				t.Fatalf("route = %q, %q", a.Selected, a.ChannelFilter)
			}
			if got := a.VisibleVideos(); len(got) != 1 || got[0].ID != "v-2" {
				t.Fatalf("routed videos = %+v", got)
			}
		})
	}
}

func TestNotificationClickHandlerRoutesQueuedHelperClickOnlyOnCallback(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	a.Channels = []core.Channel{{ID: "ch-1"}, {ID: "ch-2"}}
	a.Videos = []core.Video{{ID: "v-1", ChannelID: "ch-1"}, {ID: "v-2", ChannelID: "ch-2"}}
	a.Selected = "all"
	old := core.NewNotificationClick(core.NotificationPlan{ChannelID: "ch-1"}, time.Date(2026, 3, 1, 11, 0, 0, 0, time.UTC))
	newer := core.NewNotificationClick(core.NotificationPlan{ChannelID: "ch-2"}, time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC))
	data, _ := json.Marshal(core.NotificationClicks{Clicks: []core.NotificationClick{old, newer}})
	if err := os.WriteFile(filepath.Join(dir, "notification-clicks.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	notificationClickHandler(a)("")
	if a.Selected != "channel:ch-2" {
		t.Fatalf("cold click route = %q", a.Selected)
	}
	if got := a.VisibleVideos(); len(got) != 1 || got[0].ID != "v-2" {
		t.Fatalf("routed videos = %+v", got)
	}
	// A later click consumes the retained older record instead of rerouting
	// to a queued post from a different channel.
	notificationClickHandler(a)("")
	if a.Selected != "channel:ch-1" {
		t.Fatalf("retained click route = %q", a.Selected)
	}
}

func TestNotificationClickHandlerDoesNotRouteUnrelatedID(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	a.Channels = []core.Channel{{ID: "ch-1"}}
	a.Selected = "all"
	click := core.NewNotificationClick(core.NotificationPlan{ChannelID: "ch-1"}, time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC))
	data, _ := json.Marshal(core.NotificationClicks{Clicks: []core.NotificationClick{click}})
	if err := os.WriteFile(filepath.Join(dir, "notification-clicks.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	notificationClickHandler(a)("unrelated-id")
	if a.Selected != "all" {
		t.Fatalf("unrelated click routed to %q", a.Selected)
	}
}

// chromeWindow is the frame zone A owns: a 52 pt toolbar, a 44 pt content
// strip, and a grid behind them.
func chromeWindow(t *testing.T) *Window {
	t.Helper()
	channels := []core.Channel{
		{ID: "ch-1", Title: "X調查"},
		{ID: "ch-2", Title: "Trafotin"},
		{ID: "ch-3", Title: "GothamChess"},
	}
	synced := map[string]core.Channel{}
	for _, ch := range channels {
		synced[ch.ID] = ch
	}
	return &Window{app: &App{
		Dir:      t.TempDir(),
		Selected: "all",
		Settings: core.Settings{
			APIKey:         "test-key",
			ExpandedGroups: map[string]bool{},
			LastRefresh:    time.Now().Add(-5 * time.Minute).UTC().Format(time.RFC3339),
		},
		Sync:     core.SyncState{Favorites: map[string]core.FavoriteRecord{}, ReadStates: map[string]map[string]core.ReadRecord{}, Channels: synced},
		Channels: channels,
		Groups:   []core.ChannelGroup{{ID: "group-1", Name: "Blender 3D", ChannelIDs: []string{"ch-2"}}},
		Videos: []core.Video{
			{ID: "v1", ChannelID: "ch-1", ChannelTitle: "X調查", Title: "First video", PublishedAt: "2026-11-21T10:00:00Z"},
			{ID: "v2", ChannelID: "ch-2", ChannelTitle: "Trafotin", Title: "Second video", PublishedAt: "2026-11-21T09:00:00Z"},
			{ID: "v3", ChannelID: "ch-3", ChannelTitle: "GothamChess", Title: "Third video", PublishedAt: "2026-11-21T08:00:00Z"},
		},
	}, selectedIndex: new(int)}
}

func chromeControl(t *testing.T, tt *ui.Tester, name string) ui.AccessInfo {
	t.Helper()
	for _, info := range tt.AccessAll() {
		if info.Name == name {
			return info
		}
	}
	t.Fatalf("no control named %q in %q", name, tt.Texts())
	return ui.AccessInfo{}
}

// TestToolbarChrome checks the frame the board's A1/A1.1 draws: the 52 pt
// toolbar with its three 28 pt controls and a title centred on the window,
// and the 44 pt content strip with its four controls and the Unwatched label.
func TestToolbarChrome(t *testing.T) {
	for _, size := range [][2]int{{1280, 808}, {960, 640}} {
		width := float32(size[0])
		w := chromeWindow(t)
		tt := ui.NewTester(w.shell, size[0], size[1])
		tt.Frame()

		title, ok := tt.Find("All videos")
		if !ok {
			t.Fatalf("%dx%d: toolbar title missing: %q", size[0], size[1], tt.Texts())
		}
		if center := title.X + title.W/2; center < width/2-1 || center > width/2+1 {
			t.Fatalf("%dx%d: title centre %v, want %v", size[0], size[1], center, width/2)
		}
		for _, name := range []string{"Hide Sidebar", "Refresh", "More"} {
			box := chromeControl(t, tt, name).Bounds
			if box.W != 28 || box.H != 28 {
				t.Fatalf("%dx%d: %s is %vx%v, want 28x28", size[0], size[1], name, box.W, box.H)
			}
			if box.Y+box.H/2 != 26 {
				t.Fatalf("%dx%d: %s centred at y=%v, want 26 in the 52 pt toolbar", size[0], size[1], name, box.Y+box.H/2)
			}
		}
		if role := chromeControl(t, tt, "More").Role; role != ui.RoleMenuButton {
			t.Fatalf("%dx%d: More role = %v, want a menu button", size[0], size[1], role)
		}

		strip := chromeControl(t, tt, "Video filters").Bounds
		if strip.Y != 52 || strip.X+strip.W != width || strip.H != 44 {
			t.Fatalf("%dx%d: filter strip at %v, want y=52 h=44 to the window's right edge (%v)", size[0], size[1], strip, width)
		}
		for _, label := range []string{"All channels", "Search videos", "Unwatched"} {
			if !tt.HasText(label) {
				t.Fatalf("%dx%d: filter strip has no %q", size[0], size[1], label)
			}
		}
		if role := chromeControl(t, tt, "Unwatched").Role; role != ui.RoleCheckBox {
			t.Fatalf("%dx%d: Unwatched role = %v, want a check box", size[0], size[1], role)
		}
		if !tt.HasText("3 videos") {
			t.Fatalf("%dx%d: count missing: %q", size[0], size[1], tt.Texts())
		}
	}
}

// TestContentStripDegradesWithTheDetailPane checks the filter strip keeps all
// four controls on one line at the 960 pt minimum with the detail pane open:
// the search and channel keep their narrow floors and the count becomes its
// glyph rather than running under the drawer.
func TestContentStripDegradesWithTheDetailPane(t *testing.T) {
	for _, tc := range []struct {
		width    int
		channel  float32
		search   float32
		iconOnly bool
	}{
		{width: 1280, channel: channelMenuWidth, search: searchFieldWidth},
		{width: 960, channel: channelMenuFloor, search: searchFieldFloor, iconOnly: true},
	} {
		w := chromeWindow(t)
		w.app.SelectedVideoID = "v1"
		tt := ui.NewTester(w.shell, tc.width, 808)
		tt.Frame()

		strip := chromeControl(t, tt, "Video filters").Bounds
		channel := chromeControl(t, tt, "All channels").Bounds
		search, ok := tt.Find("Search videos")
		if !ok {
			t.Fatalf("%d: no search field", tc.width)
		}
		unwatched := chromeControl(t, tt, "Unwatched").Bounds
		count := chromeControl(t, tt, "3 videos").Bounds

		if channel.W != tc.channel {
			t.Fatalf("%d: channel is %v wide, want %v", tc.width, channel.W, tc.channel)
		}
		if search.W != tc.search {
			t.Fatalf("%d: search is %v wide, want %v", tc.width, search.W, tc.search)
		}
		if tc.iconOnly != (count.W == countIconWidth && count.H == countIconWidth) {
			t.Fatalf("%d: count is %v, icon-only want %v", tc.width, count, tc.iconOnly)
		}
		for _, box := range []ui.Rect{channel, search, unwatched, count} {
			if box.X < strip.X || box.X+box.W > strip.X+strip.W ||
				box.Y < strip.Y || box.Y+box.H > strip.Y+strip.H {
				t.Fatalf("%d: %v leaves the strip %v", tc.width, box, strip)
			}
		}
		if channel.X+channel.W > search.X || search.X+search.W > unwatched.X ||
			unwatched.X+unwatched.W > count.X {
			t.Fatalf("%d: filter controls overlap: %v %v %v %v", tc.width, channel, search, unwatched, count)
		}
		if right := strip.X + strip.W - mui.Space(4); count.X+count.W < right-mui.Space(2) {
			t.Fatalf("%d: count ends at %v, strip's right inset is %v", tc.width, count.X+count.W, right)
		}
	}
}

// TestToolbarSidebarToggleFoldsSidebar checks the leading control really
// hides the sidebar pane, and that the grid reflows rather than leaving a
// gap for it.
func TestToolbarSidebarToggleFoldsSidebar(t *testing.T) {
	w := chromeWindow(t)
	tt := ui.NewTester(w.shell, 1280, 808)
	tt.Frame()
	if !tt.HasText("Blender 3D") {
		t.Fatalf("sidebar missing before hiding: %q", tt.Texts())
	}
	gridBefore := w.contentWidth
	if err := tt.Click("Hide Sidebar"); err != nil {
		t.Fatal(err)
	}
	if !w.sidebarHidden {
		t.Fatal("hiding the sidebar did not record the state")
	}
	if tt.HasText("Blender 3D") || tt.HasText("Library") {
		t.Fatalf("sidebar still drawn: %q", tt.Texts())
	}
	if w.contentWidth <= gridBefore {
		t.Fatalf("grid did not take the sidebar's width: %v -> %v", gridBefore, w.contentWidth)
	}
	if !tt.HasText("Show Sidebar") {
		t.Fatalf("toolbar does not offer the sidebar back: %q", tt.Texts())
	}
	if err := tt.Click("Show Sidebar"); err != nil {
		t.Fatal(err)
	}
	if w.sidebarHidden || !tt.HasText("Blender 3D") {
		t.Fatalf("sidebar did not come back: hidden=%v", w.sidebarHidden)
	}
}

// TestOverflowMenuMirrorsToolbarCommands checks the overflow menu's items,
// that the status row is dimmed rather than hidden, and that Escape closes
// the menu.
func TestOverflowMenuMirrorsToolbarCommands(t *testing.T) {
	w := chromeWindow(t)
	tt := ui.NewTester(w.shell, 1280, 808)
	tt.Frame()
	if err := tt.Click("More"); err != nil {
		t.Fatalf("open overflow menu: %v", err)
	}
	items := tt.Menu()
	for _, want := range []string{"Last refresh 5 min ago", "Refresh", "Settings…", "Import OPML…", "Export Favorites…", "Restore Favorites…"} {
		if !slices.Contains(items, want) {
			t.Fatalf("overflow menu %q has no %q", items, want)
		}
	}
	err := tt.ChooseMenuItem("Last refresh 5 min ago")
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("the status row is choosable: %v", err)
	}
	tt.Key(0, ui.KeyEscape)
	if items := tt.Menu(); items != nil {
		t.Fatalf("Escape left the overflow menu open: %q", items)
	}
}

// TestRefreshFailureSurfacesItsMessage checks a refresh that reports a failure
// reaches the view as a toast: the outcome arrives between frames, so the
// message has to wait for the next build instead of being dropped.
func TestRefreshFailureSurfacesItsMessage(t *testing.T) {
	w := chromeWindow(t)
	tt := ui.NewTester(w.shell, 1280, 808)
	tt.Frame()

	w.refreshFinished(RefreshOutcome{
		Description: "Refresh failed: quota paused",
		Failures:    []core.RefreshFailure{{Message: "quota paused"}},
	})
	if tt.HasText("Refresh failed: quota paused") {
		t.Fatal("the toast showed before a frame consumed it")
	}
	tt.Frame()
	if !tt.HasText("Refresh failed: quota paused") {
		t.Fatalf("refresh failure has no toast: %q", tt.Texts())
	}
}

// TestMenuKeepsTheStandardBar checks the app menu repeats the native standard
// menus and their shortcuts, with the app's own commands on top: File closes
// the window on Command-W and carries the document commands the overflow menu
// offers, while View and the App menu mirror the toolbar.
func TestMenuKeepsTheStandardBar(t *testing.T) {
	w := chromeWindow(t)
	w.installMenu()
	items := mygo.App.Menu().Items()

	app := items[0]
	for _, role := range []mygo.MenuRole{mygo.RoleAbout, mygo.RoleServices, mygo.RoleHide, mygo.RoleQuit} {
		if menuItemWithRole(app.Submenu, role) == nil {
			t.Fatalf("App menu has no %s item", role)
		}
	}
	if settings := menuItemWithLabel(app.Submenu, "Settings…"); settings == nil || settings.Accelerator != "Cmd+," {
		t.Fatalf("App menu's Settings… is %+v", settings)
	}

	file := menuItemWithLabel(items, "File")
	if file == nil {
		t.Fatalf("no File menu in %v", menuLabels(items))
	}
	closeItem := menuItemWithRole(file.Submenu, mygo.RoleClose)
	if closeItem == nil || closeItem.Accelerator != "CmdOrCtrl+W" {
		t.Fatalf("File menu's Close Window is %+v", closeItem)
	}
	for _, label := range []string{"Import OPML…", "Export Favorites…", "Restore Favorites…"} {
		if menuItemWithLabel(file.Submenu, label) == nil {
			t.Fatalf("File menu has no %q: %v", label, menuLabels(file.Submenu))
		}
	}

	if menuItemWithRole(items, mygo.RoleEditMenu) == nil {
		t.Fatal("no Edit menu")
	}

	view := menuItemWithLabel(items, "View")
	if view == nil {
		t.Fatalf("no View menu in %v", menuLabels(items))
	}
	if refresh := menuItemWithLabel(view.Submenu, "Refresh"); refresh == nil || refresh.Accelerator != "Cmd+R" {
		t.Fatalf("View menu's Refresh is %+v", refresh)
	}
	if sidebar := menuItemWithLabel(view.Submenu, "Hide Sidebar"); sidebar == nil || sidebar.Accelerator != "Ctrl+Cmd+S" {
		t.Fatalf("View menu's sidebar command is %+v", sidebar)
	}
	if menuItemWithRole(view.Submenu, mygo.RoleToggleFullScreen) == nil {
		t.Fatal("View menu has no full screen command")
	}

	if menuItemWithRole(items, mygo.RoleWindowMenu) == nil {
		t.Fatal("no Window menu")
	}
	if menuItemWithRole(items, mygo.RoleHelp) == nil {
		t.Fatal("no Help menu")
	}
}

func menuItemWithLabel(items []*mygo.MenuItem, label string) *mygo.MenuItem {
	for _, item := range items {
		if item.Label == label {
			return item
		}
	}
	return nil
}

func menuItemWithRole(items []*mygo.MenuItem, role mygo.MenuRole) *mygo.MenuItem {
	for _, item := range items {
		if item.Role == role {
			return item
		}
	}
	return nil
}

func menuLabels(items []*mygo.MenuItem) []string {
	labels := make([]string, len(items))
	for i, item := range items {
		labels[i] = item.Label
	}
	return labels
}

// TestToolbarRefreshDimsWhileBusy checks the unavailable state the board
// asks for: the control and the menu command are dimmed, not hidden.
func TestToolbarRefreshDimsWhileBusy(t *testing.T) {
	w := chromeWindow(t)
	w.app.Busy = true
	tt := ui.NewTester(w.shell, 1280, 808)
	tt.Frame()
	refresh := chromeControl(t, tt, "Refreshing")
	if !refresh.Disabled {
		t.Fatal("refresh control is not dimmed while a refresh runs")
	}
	for _, info := range tt.AccessAll() {
		if info.Name == "Refresh" {
			t.Fatal("refresh control still offers its idle label while busy")
		}
	}
	_ = tt.Click("Refreshing")
	if !w.app.Busy {
		t.Fatal("pressing the busy refresh control started another refresh")
	}
	if err := tt.Click("More"); err != nil {
		t.Fatal(err)
	}
	err := tt.ChooseMenuItem("Refresh")
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("the menu's Refresh is choosable while busy: %v", err)
	}
}

// TestToolbarKeyboardOrder checks the keyboard model: the toolbar is one stop
// of Tab with the arrows moving between its controls, and the filter strip
// follows it in reading order.
func TestToolbarKeyboardOrder(t *testing.T) {
	w := chromeWindow(t)
	tt := ui.NewTester(w.shell, 1280, 808)
	tt.UseScreenReader()
	tt.Frame()

	tt.Key(0, ui.KeyTab)
	if !tt.Focused("Hide Sidebar") {
		t.Fatalf("first Tab reached %q", focusedName(tt))
	}
	tt.Key(0, ui.KeyRight)
	if !tt.Focused("Refresh") {
		t.Fatalf("Right reached %q", focusedName(tt))
	}
	tt.Key(0, ui.KeyRight)
	if !tt.Focused("More") {
		t.Fatalf("Right reached %q", focusedName(tt))
	}
	tt.Key(0, ui.KeyTab)
	if !tt.Focused("All") {
		t.Fatalf("Tab left the toolbar for %q", focusedName(tt))
	}
}

func focusedName(tt *ui.Tester) string {
	for _, info := range tt.AccessAll() {
		if info.Focused {
			return info.Name
		}
	}
	return ""
}

// TestSettingsTakesTheWindowTitle checks the toolbar's contextual title and
// that the settings page replaces the filter strip.
func TestSettingsTakesTheWindowTitle(t *testing.T) {
	w := chromeWindow(t)
	w.app.SettingsOpen = true
	tt := ui.NewTester(w.shell, 1280, 808)
	tt.Frame()
	if !tt.HasText("Settings") {
		t.Fatalf("toolbar title is not Settings: %q", tt.Texts())
	}
	if tt.HasText("All channels") {
		t.Fatal("filter strip is drawn over the settings page")
	}
}

// TestToolbarSidebarHiddenKeepsTheDetailPane checks the hidden sidebar leaves
// no gap and keeps the detail pane's width: the grid takes the sidebar's room,
// the divider meets the drawer, and a drag while folded saves what it shows.
func TestToolbarSidebarHiddenKeepsTheDetailPane(t *testing.T) {
	const width float32 = 1280
	w := chromeWindow(t)
	w.app.SelectedVideoID = "v1"
	tt := ui.NewTester(w.shell, 1280, 808)
	tt.Frame()
	drawerShown := chromeControl(t, tt, "Video details").Bounds

	w.sidebarHidden = true
	tt.Frame()
	drawerHidden := chromeControl(t, tt, "Video details").Bounds

	if drawerHidden.W != drawerShown.W {
		t.Fatalf("detail pane width changed: %v -> %v", drawerShown.W, drawerHidden.W)
	}
	if drawerHidden.X+drawerHidden.W != width {
		t.Fatalf("detail pane ends at %v, want the window's %v", drawerHidden.X+drawerHidden.W, width)
	}
	divider, ok := tt.AccessByRole(ui.RoleSplitter, "")
	if !ok {
		t.Fatal("no divider between the grid and the detail pane")
	}
	if divider.Bounds.X+divider.Bounds.W != drawerHidden.X {
		t.Fatalf("gap between the divider at %v and the detail pane at %v", divider.Bounds, drawerHidden)
	}

	target := drawerShown.W + 60
	x, y := divider.Bounds.X+divider.Bounds.W/2, divider.Bounds.Y+200
	tt.Press(x, y)
	tt.Move(x-60, y)
	tt.Release(x-60, y)
	tt.Frame()
	dragged := chromeControl(t, tt, "Video details").Bounds
	if dragged.W != target {
		t.Fatalf("dragged detail pane is %v wide, want %v", dragged.W, target)
	}
	if got := w.app.settingsSnapshot().DetailsWidth; got != target {
		t.Fatalf("dragged detail pane saved %v, want %v", got, target)
	}
}
