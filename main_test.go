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

// unwatchedGlyph returns the box the collapsed Unwatched control draws its
// 16 pt glyph in.
func unwatchedGlyph(t *testing.T, tt *ui.Tester) ui.Rect {
	t.Helper()
	for _, name := range []string{"Unwatched off", "Unwatched on"} {
		if r, ok := tt.Find(name); ok {
			if r.W != toolbarGlyph || r.H != toolbarGlyph {
				t.Fatalf("%s glyph is %v, want the %v pt icon", name, r, toolbarGlyph)
			}
			return r
		}
	}
	t.Fatal("the collapsed Unwatched control draws no glyph")
	return ui.Rect{}
}

// unwatchedShape reads the drawn glyph's shape from the frame: whether the
// box's top edge and the check's stroke carry ink. It samples pixels rather
// than the label assigned beside the glyph, so drawing the check while the
// filter is off cannot pass as the empty box.
func unwatchedShape(t *testing.T, tt *ui.Tester) (box, check bool) {
	t.Helper()
	r := unwatchedGlyph(t, tt)
	img := tt.Image()
	// The box's empty lower-right interior is the reference for no ink.
	blank := img.RGBAAt(int(r.X)+11, int(r.Y)+11)
	ink := func(x, y int) bool {
		c := img.RGBAAt(int(r.X)+x, int(r.Y)+y)
		dist := 0
		for _, pair := range [][2]uint8{{c.R, blank.R}, {c.G, blank.G}, {c.B, blank.B}} {
			d := int(pair[0]) - int(pair[1])
			if d < 0 {
				d = -d
			}
			dist += d
		}
		return dist > 120
	}
	// The box's top edge at x=8 is inked in both states; the check's second
	// segment crosses the box's centre at the (8, 8) pixel.
	return ink(8, 2), ink(8, 8)
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
// the search keeps its intrinsic minimum, the channel collapses to its glyph,
// and the Unwatched check box collapses to its icon while the count stays
// visible as digits.
func TestContentStripDegradesWithTheDetailPane(t *testing.T) {
	for _, tc := range []struct {
		name         string
		width        int
		detailsWidth float32
		query        string
		count        string
		channel      float32
		search       float32
		collapsed    bool
	}{
		{name: "design width", width: 1280, count: "3 videos", channel: channelMenuWidth, search: searchFieldWidth},
		{name: "minimum width", width: 960, count: "3 videos", channel: channelMenuIcon, search: searchFieldMin},
		{name: "minimum width, widest drawer", width: 960, count: "3 videos", detailsWidth: maxDetailsWidth, channel: channelMenuIcon, search: searchFieldMin, collapsed: true},
		{name: "minimum width, typed query", width: 960, query: "First", count: "1 video", channel: channelMenuIcon, search: searchFieldMin},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := chromeWindow(t)
			w.app.SelectedVideoID = "v1"
			w.app.Search = tc.query
			if tc.detailsWidth > 0 {
				w.app.Settings.DetailsWidth = tc.detailsWidth
			}
			tt := ui.NewTester(w.shell, tc.width, 808)
			tt.Frame()

			strip := chromeControl(t, tt, "Video filters").Bounds
			var channel ui.Rect
			if tc.channel < channelMenuWidth {
				control := chromeControl(t, tt, "Channel filter")
				if control.Bounds.W != tc.channel {
					t.Fatalf("channel is %v wide, want the collapsed %v", control.Bounds.W, tc.channel)
				}
				if control.Description != "All channels" {
					t.Fatalf("collapsed channel's tooltip is %q, want the selected channel", control.Description)
				}
				channel = control.Bounds
			} else {
				if !tt.HasText("All channels") {
					t.Fatalf("expanded channel has no label: %q", tt.Texts())
				}
				channel = chromeControl(t, tt, "All channels").Bounds
			}
			search, ok := tt.Find("Search videos")
			if !ok {
				t.Fatal("no search field")
			}
			unwatched := chromeControl(t, tt, "Unwatched")
			count := chromeControl(t, tt, tc.count).Bounds

			if search.W != tc.search {
				t.Fatalf("search is %v wide, want %v", search.W, tc.search)
			}
			if unwatched.Role != ui.RoleCheckBox {
				t.Fatalf("Unwatched role = %v, want a check box", unwatched.Role)
			}
			if tc.collapsed != (unwatched.Bounds.W == toolbarControl) {
				t.Fatalf("Unwatched is %v wide, collapsed want %v", unwatched.Bounds, tc.collapsed)
			}
			if count.W <= toolbarControl {
				t.Fatalf("count is %v, want visible digits", count)
			}
			for _, box := range []ui.Rect{channel, search, unwatched.Bounds, count} {
				if box.X < strip.X || box.X+box.W > strip.X+strip.W ||
					box.Y < strip.Y || box.Y+box.H > strip.Y+strip.H {
					t.Fatalf("%v leaves the strip %v", box, strip)
				}
			}
			if channel.X+channel.W > search.X || search.X+search.W > unwatched.Bounds.X ||
				unwatched.Bounds.X+unwatched.Bounds.W > count.X {
				t.Fatalf("filter controls overlap: %v %v %v %v", channel, search, unwatched.Bounds, count)
			}
			if right := strip.X + strip.W - mui.Space(4); count.X+count.W < right-mui.Space(2) {
				t.Fatalf("count ends at %v, strip's right inset is %v", count.X+count.W, right)
			}
			if tc.query != "" {
				clear := chromeControl(t, tt, "Clear").Bounds
				if clear.X < search.X || clear.X+clear.W > search.X+search.W {
					t.Fatalf("clear button %v leaves the search field %v", clear, search)
				}
				if clear.X+clear.W > unwatched.Bounds.X {
					t.Fatalf("clear button %v runs into the Unwatched check box %v", clear, unwatched.Bounds)
				}
			}
		})
	}
}

// TestCollapsedChannelFilterCarriesTheChoiceInItsTooltip checks the chevron
// form at a narrow width: it is the 36 pt glyph, names the chosen channel in
// its tooltip, and its menu changes the filter like the expanded select.
func TestCollapsedChannelFilterCarriesTheChoiceInItsTooltip(t *testing.T) {
	w := chromeWindow(t)
	w.app.SelectedVideoID = "v1"
	w.app.Settings.DetailsWidth = maxDetailsWidth
	tt := ui.NewTester(w.shell, 960, 808)
	tt.Frame()

	channel := chromeControl(t, tt, "Channel filter")
	if channel.Bounds.W != channelMenuIcon {
		t.Fatalf("collapsed channel is %v wide, want %v", channel.Bounds.W, channelMenuIcon)
	}
	if channel.Description != "All channels" {
		t.Fatalf("collapsed channel's tooltip is %q, want All channels", channel.Description)
	}
	if err := tt.Click("Channel filter"); err != nil {
		t.Fatalf("open the channel menu: %v", err)
	}
	if err := tt.ChooseMenuItem("X調查"); err != nil {
		t.Fatalf("choose a channel: %v", err)
	}
	tt.Frame()
	if w.app.ChannelFilter != "ch-1" {
		t.Fatalf("choosing X調查 set the filter to %q", w.app.ChannelFilter)
	}
	if got := chromeControl(t, tt, "Channel filter").Description; got != "X調查" {
		t.Fatalf("collapsed channel's tooltip after choosing is %q, want X調查", got)
	}
	if !tt.HasText("1 video") {
		t.Fatalf("filtered count is %q", tt.Texts())
	}
}

// TestCollapsedUnwatchedToggleFilters checks the collapsed Unwatched control
// keeps the check box's role and activation: a click and Space toggle the
// filter and the count follows, and the drawn glyph shows the state in
// greyscale terms, an empty box while off and a check while on.
func TestCollapsedUnwatchedToggleFilters(t *testing.T) {
	w := chromeWindow(t)
	w.app.SelectedVideoID = "v1"
	w.app.Settings.DetailsWidth = maxDetailsWidth
	w.app.Sync.ReadStates = map[string]map[string]core.ReadRecord{
		"v2": {"device": {VideoID: "v2", Read: true}},
		"v3": {"device": {VideoID: "v3", Read: true}},
	}
	tt := ui.NewTester(w.shell, 960, 808)
	tt.Frame()

	box := chromeControl(t, tt, "Unwatched")
	if box.Bounds.W != toolbarControl || box.Role != ui.RoleCheckBox {
		t.Fatalf("Unwatched is %v in role %v, want the collapsed check box", box.Bounds, box.Role)
	}
	if box.Checked {
		t.Fatal("collapsed check box reports checked while the filter is off")
	}
	if boxInk, checkInk := unwatchedShape(t, tt); !boxInk || checkInk {
		t.Fatalf("Unwatched off draws box %v check %v, want the empty box", boxInk, checkInk)
	}
	if !tt.HasText("3 videos") {
		t.Fatalf("count before filtering is %q", tt.Texts())
	}
	if err := tt.Click("Unwatched"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !w.app.UnreadOnly {
		t.Fatal("clicking the collapsed check box did not enable the filter")
	}
	if box = chromeControl(t, tt, "Unwatched"); !box.Checked {
		t.Fatal("collapsed check box does not report checked while the filter is on")
	}
	if boxInk, checkInk := unwatchedShape(t, tt); !boxInk || !checkInk {
		t.Fatalf("Unwatched on draws box %v check %v, want the check", boxInk, checkInk)
	}
	if !tt.HasText("1 video") {
		t.Fatalf("count after filtering is %q", tt.Texts())
	}
	tt.Key(0, ui.KeySpace)
	tt.Frame()
	if w.app.UnreadOnly {
		t.Fatal("Space did not clear the collapsed check box")
	}
	if box = chromeControl(t, tt, "Unwatched"); box.Checked {
		t.Fatal("collapsed check box reports checked after Space cleared it")
	}
	if boxInk, checkInk := unwatchedShape(t, tt); !boxInk || checkInk {
		t.Fatalf("Unwatched after clearing draws box %v check %v, want the empty box", boxInk, checkInk)
	}
	if !tt.HasText("3 videos") {
		t.Fatalf("count after clearing is %q", tt.Texts())
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
	help := menuItemWithRole(items, mygo.RoleHelp)
	if help == nil {
		t.Fatal("no Help menu")
	}
	if help.Submenu == nil || help.Type != mygo.MenuItemSubmenu {
		t.Fatalf("Help is not a submenu: %+v", help)
	}

	bound := map[string]string{}
	var walk func([]*mygo.MenuItem)
	walk = func(menuItems []*mygo.MenuItem) {
		for _, item := range menuItems {
			if item.Accelerator != "" {
				if label, ok := bound[item.Accelerator]; ok {
					t.Fatalf("%q is bound to both %q and %q", item.Accelerator, label, item.Label)
				}
				bound[item.Accelerator] = item.Label
			}
			walk(item.Submenu)
		}
	}
	walk(items)
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

// TestSettingsCommandAlwaysOpensThePage checks Settings… is a command, not a
// toggle: choosing it from the App menu or the overflow menu while the page is
// open leaves the page open, and its Done button is what closes it.
func TestSettingsCommandAlwaysOpensThePage(t *testing.T) {
	w := chromeWindow(t)
	w.installMenu()
	app := mygo.App.Menu().Items()[0]
	settings := menuItemWithLabel(app.Submenu, "Settings…")
	if settings == nil || settings.Click == nil {
		t.Fatalf("App menu's Settings… is %+v", settings)
	}
	settings.Click(settings, nil)
	if !w.app.SettingsOpen {
		t.Fatal("the App menu's Settings… did not open the page")
	}
	settings.Click(settings, nil)
	if !w.app.SettingsOpen {
		t.Fatal("choosing Settings… again closed the page")
	}

	tt := ui.NewTester(w.shell, 1280, 808)
	tt.Frame()
	for i := 0; i < 2; i++ {
		if err := tt.Click("More"); err != nil {
			t.Fatal(err)
		}
		if err := tt.ChooseMenuItem("Settings…"); err != nil {
			t.Fatal(err)
		}
		if !w.app.SettingsOpen {
			t.Fatalf("the overflow menu's Settings… closed the page on press %d", i+1)
		}
	}
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
	if !tt.Focused("All videos") {
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

// TestHiddenSidebarDetailDragKeepsSidebarWidth checks a detail-pane drag while
// the sidebar is folded saves only the detail width, not the fitted width of a
// sidebar the user did not touch.
func TestHiddenSidebarDetailDragKeepsSidebarWidth(t *testing.T) {
	const (
		storedSidebar float32 = 300
		width         float32 = 960
		drag          float32 = 60
	)
	w := chromeWindow(t)
	w.app.Settings.SidebarWidth = storedSidebar
	w.app.SelectedVideoID = "v1"
	w.sidebarHidden = true
	tt := ui.NewTester(w.shell, 960, 808)
	tt.Frame()

	if got := w.app.settingsSnapshot().SidebarWidth; got != storedSidebar {
		t.Fatalf("opening the frame saved sidebar width %v, want %v", got, storedSidebar)
	}
	drawer := chromeControl(t, tt, "Video details").Bounds
	if drawer.W != defaultDetailsWidth {
		t.Fatalf("hidden-sidebar drawer is %v wide, want the stored %v", drawer.W, defaultDetailsWidth)
	}
	if want := width - defaultDetailsWidth - splitDividerWidth(); w.contentWidth != want {
		t.Fatalf("grid pane is %v wide, want %v with no sidebar room reserved", w.contentWidth, want)
	}
	divider, ok := tt.AccessByRole(ui.RoleSplitter, "")
	if !ok {
		t.Fatal("no divider between the grid and the detail pane")
	}
	x, y := divider.Bounds.X+divider.Bounds.W/2, divider.Bounds.Y+200
	tt.Press(x, y)
	tt.Move(x-drag, y)
	tt.Release(x-drag, y)
	tt.Frame()

	want := drawer.W + drag
	dragged := chromeControl(t, tt, "Video details").Bounds
	if dragged.W != want {
		t.Fatalf("dragged detail pane is %v wide, want %v", dragged.W, want)
	}
	if got := w.app.settingsSnapshot().DetailsWidth; got != dragged.W {
		t.Fatalf("detail drag saved %v, want the displayed %v", got, dragged.W)
	}
	if got := w.app.settingsSnapshot().SidebarWidth; got != storedSidebar {
		t.Fatalf("detail drag saved sidebar width %v, want the stored %v", got, storedSidebar)
	}
	tt.Frame()
	if again := chromeControl(t, tt, "Video details").Bounds; again.W != dragged.W {
		t.Fatalf("the saved detail width snapped back from %v to %v", dragged.W, again.W)
	}
	w.setSidebarHidden(false)
	tt.Frame()
	if got := w.app.settingsSnapshot().SidebarWidth; got != storedSidebar {
		t.Fatalf("showing the sidebar again used %v, want the stored %v", got, storedSidebar)
	}
}
