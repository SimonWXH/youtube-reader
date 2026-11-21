package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
	mui "github.com/simon/youtube-reader/ui"
)

// sidebarTestApp is the smallest window that shows the whole sidebar:
// Library, one channel group with one channel, the Channels section, and the
// pinned Status row, in 1280 x 808 as the design board measures it.
func sidebarTestApp(t *testing.T, expanded bool) *App {
	t.Helper()
	// The card footer names the channel too, so give the video a different
	// title: countText then counts only the sidebar rows.
	video := core.Video{ID: "video-1", ChannelID: "ch-a", ChannelTitle: "Card channel", Title: "Episode"}
	return &App{
		Dir:      t.TempDir(),
		Selected: "all",
		Settings: core.Settings{
			APIKey:         "test",
			SidebarWidth:   defaultSidebarWidth,
			DetailsWidth:   defaultDetailsWidth,
			ExpandedGroups: map[string]bool{"group-a": expanded},
		},
		Sync: core.SyncState{
			Favorites:  map[string]core.FavoriteRecord{},
			ReadStates: map[string]map[string]core.ReadRecord{},
			Channels:   map[string]core.Channel{"ch-a": {ID: "ch-a", Title: "Alpha channel"}},
			Groups:     map[string]core.ChannelGroup{"group-a": {ID: "group-a", Name: "Alpha", ChannelIDs: []string{"ch-a"}}},
		},
		Groups:   []core.ChannelGroup{{ID: "group-a", Name: "Alpha", ChannelIDs: []string{"ch-a"}}},
		Channels: []core.Channel{{ID: "ch-a", Title: "Alpha channel"}},
		Videos:   []core.Video{video},
	}
}

func sidebarTester(t *testing.T, a *App) *ui.Tester {
	t.Helper()
	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.view, 1280, 808)
	tt.Frame()
	return tt
}

// focusByTab moves the keyboard focus with Tab until the named row has it.
func focusByTab(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	for i := 0; i < 40; i++ {
		if tt.Focused(name) {
			return
		}
		tt.Key(0, ui.KeyTab)
	}
	t.Fatalf("Tab never reached the %q row", name)
}

// ringLines reports the first and last row of pixels in the sidebar painted
// in the focus ring colour. A ring always spans more than the 16 pt icon, so
// a run of at least a third of the sidebar width proves a ring rather than an
// accent glyph.
func ringLines(t *testing.T, tt *ui.Tester, tokens mui.Tokens, width int) (int, int, bool) {
	t.Helper()
	img := tt.Image()
	ring := func(x, y int) bool {
		r, g, b, _ := img.At(x, y).RGBA()
		return absDiff(int(r>>8), int(tokens.Focus.R)) <= 6 &&
			absDiff(int(g>>8), int(tokens.Focus.G)) <= 6 &&
			absDiff(int(b>>8), int(tokens.Focus.B)) <= 6
	}
	longest := func(y int) int {
		best, run := 0, 0
		for x := 0; x < width; x++ {
			if ring(x, y) {
				run++
				if run > best {
					best = run
				}
			} else {
				run = 0
			}
		}
		return best
	}
	first, last := -1, -1
	for y := 0; y < 808; y++ {
		if longest(y) >= width/3 {
			if first < 0 {
				first = y
			}
			last = y
		}
	}
	if first < 0 {
		return 0, 0, false
	}
	return first, last, true
}

func absDiff(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}

// TestSidebarWidthMatchesDesign checks the agreed 220 to 320 pt range that
// the drag handle enforces and that settings remember.
func TestSidebarWidthMatchesDesign(t *testing.T) {
	if minSidebarWidth != 220 || maxSidebarWidth != 320 || defaultSidebarWidth != 260 {
		t.Fatalf("sidebar widths = min %v default %v max %v, want 220 / 260 / 320",
			minSidebarWidth, defaultSidebarWidth, maxSidebarWidth)
	}
}

// TestSidebarArrowKeysWalkSelection proves the keyboard-only selection
// model: Down and Up move the selection row by row and skip the fold rows in
// between, while the fold rows themselves stay reachable with Tab.
func TestSidebarArrowKeysWalkSelection(t *testing.T) {
	a := sidebarTestApp(t, true)
	tt := sidebarTester(t, a)
	tt.UseScreenReader()

	focusByTab(t, tt, "All videos")
	if a.Selected != "all" {
		t.Fatalf("Tab alone changed the selection to %q", a.Selected)
	}

	tt.Key(0, ui.KeyDown)
	if a.Selected != "favorites" {
		t.Fatalf("Down from All videos selected %q, want favorites", a.Selected)
	}
	if !tt.Focused("Favorites") {
		t.Fatal("Down moved the selection but not the focus")
	}

	tt.Key(0, ui.KeyDown)
	if a.Selected != "channel:ch-a" {
		t.Fatalf("Down from Favorites selected %q, want channel:ch-a", a.Selected)
	}
	if tt.Focused("Alpha") {
		t.Fatal("the arrow keys stopped on the fold row instead of skipping it")
	}

	_, before, _ := ringLines(t, tt, mui.NewTokens(nil, false), int(defaultSidebarWidth))
	tt.Key(0, ui.KeyDown)
	if a.Selected != "channel:ch-a" {
		t.Fatalf("Down to the Channels row selected %q, want the same channel", a.Selected)
	}
	_, after, ok := ringLines(t, tt, mui.NewTokens(nil, false), int(defaultSidebarWidth))
	if !ok || after <= before {
		t.Fatalf("focus ring stayed at %d after Down; want it to move down", after)
	}

	tt.Key(0, ui.KeyUp)
	tt.Key(0, ui.KeyUp)
	if a.Selected != "favorites" {
		t.Fatalf("two Ups selected %q, want favorites", a.Selected)
	}
	tt.Key(0, ui.KeyUp)
	if a.Selected != "all" {
		t.Fatalf("Up from Favorites selected %q, want all", a.Selected)
	}
	tt.Key(0, ui.KeyUp)
	if a.Selected != "all" {
		t.Fatalf("Up at the first row moved the selection to %q", a.Selected)
	}

	// A fold row keeps its keyboard reach: Tab focuses it, and the arrows
	// carry on from wherever it sits in the list.
	focusByTab(t, tt, "Alpha")
	if a.Selected != "all" {
		t.Fatalf("focusing the fold row changed the selection to %q", a.Selected)
	}
	tt.Key(0, ui.KeyDown)
	if a.Selected != "channel:ch-a" {
		t.Fatalf("Down from the fold row selected %q, want channel:ch-a", a.Selected)
	}
}

// TestSidebarReturnOpensFocusedRow proves Return activates the row that has
// the keyboard focus, not the row the selection happens to be on.
func TestSidebarReturnOpensFocusedRow(t *testing.T) {
	a := sidebarTestApp(t, false)
	a.Selected = "channel:ch-a"
	tt := sidebarTester(t, a)

	focusByTab(t, tt, "All videos")
	if a.Selected != "channel:ch-a" {
		t.Fatalf("Tab alone changed the selection to %q", a.Selected)
	}
	tt.Key(0, ui.KeyEnter)
	if a.Selected != "all" {
		t.Fatalf("Return on the All videos row selected %q, want all", a.Selected)
	}

	// Return also drives the fold rows: the group is collapsed, so Return on
	// its row opens it and the channel row appears under it.
	focusByTab(t, tt, "Alpha")
	tt.Key(0, ui.KeyEnter)
	if a.GroupCollapsed("group-a") {
		t.Fatal("Return on the group row did not expand the group")
	}
	if n := countText(tt, "Alpha channel"); n != 2 {
		t.Fatalf("expanded group should add its channel row; got %d in %q", n, tt.Texts())
	}
	tt.Key(0, ui.KeyEnter)
	if !a.GroupCollapsed("group-a") {
		t.Fatal("Return on the expanded group row did not collapse it")
	}
	if n := countText(tt, "Alpha channel"); n != 1 {
		t.Fatalf("collapsed group should drop its channel row; got %d in %q", n, tt.Texts())
	}
}

// TestSidebarFocusRingOnEveryRow proves the keyboard focus reads on a
// navigation row, a channel row, a fold row, and the pinned status row.
func TestSidebarFocusRingOnEveryRow(t *testing.T) {
	for _, dark := range []bool{false, true} {
		a := sidebarTestApp(t, true)
		w := &Window{app: a, selectedIndex: new(int)}
		tt := ui.NewTester(w.view, 1280, 808)
		tt.SetDark(dark)
		tt.Frame()
		tokens := mui.NewTokens(nil, dark)

		for _, name := range []string{"All videos", "Alpha channel", "Alpha", "Status"} {
			focusByTab(t, tt, name)
			first, last, ok := ringLines(t, tt, tokens, int(defaultSidebarWidth))
			if !ok {
				t.Fatalf("dark=%v: %q row has no visible focus ring", dark, name)
			}
			if last-first < int(mui.SidebarRowHeight) {
				t.Fatalf("dark=%v: %q row focus ring spans %d px, want at least one %v pt row", dark, name, last-first, mui.SidebarRowHeight)
			}
		}
	}
}

// TestSidebarDragHandleClampsAndPersists proves the divider keeps the agreed
// 220 to 320 pt range, keeps a 6 pt grab band around the 1 pt rule with the
// resize cursor, and writes the chosen width to settings.
func TestSidebarDragHandleClampsAndPersists(t *testing.T) {
	a := sidebarTestApp(t, true)
	tt := sidebarTester(t, a)
	divider := defaultSidebarWidth

	// The grab band reaches past the 1 pt rule on both sides.
	tt.Move(divider+2, 400)
	tt.Frame()
	if got := tt.Cursor(); got != ui.CursorResizeEW {
		t.Fatalf("cursor 2 pt right of the divider = %v, want the resize cursor", got)
	}
	tt.Move(divider-2, 400)
	tt.Frame()
	if got := tt.Cursor(); got != ui.CursorResizeEW {
		t.Fatalf("cursor 2 pt left of the divider = %v, want the resize cursor", got)
	}
	tt.Move(divider+12, 400)
	tt.Frame()
	if got := tt.Cursor(); got == ui.CursorResizeEW {
		t.Fatal("the grab band reaches 12 pt from the divider")
	}

	drag := func(to float32) {
		from := a.Settings.SidebarWidth
		tt.Press(from+1, 400)
		tt.Move(from+(to-from)/2, 400)
		tt.Move(to, 400)
		tt.Release(to, 400)
		tt.Frame()
	}
	drag(700)
	if got := a.Settings.SidebarWidth; got != maxSidebarWidth {
		t.Fatalf("dragging past the maximum saved %v, want %v", got, maxSidebarWidth)
	}
	drag(40)
	if got := a.Settings.SidebarWidth; got != minSidebarWidth {
		t.Fatalf("dragging past the minimum saved %v, want %v", got, minSidebarWidth)
	}

	// The remembered width is what the next frame lays out.
	w := &Window{app: a, selectedIndex: new(int)}
	next := ui.NewTester(w.view, 1280, 808)
	next.Frame()
	if w.sidebarWidth != minSidebarWidth {
		t.Fatalf("rebuilt sidebar = %v pt, want the remembered %v", w.sidebarWidth, minSidebarWidth)
	}
}

// TestGroupFoldStateSurvivesRelaunch folds and unfolds a group through the
// sidebar, then reads settings.json back the way LoadApp does on launch.
func TestGroupFoldStateSurvivesRelaunch(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	a.Settings.ExpandedGroups = map[string]bool{}
	a.Groups = []core.ChannelGroup{{ID: "group-a", Name: "Alpha", ChannelIDs: []string{"ch-a"}}}
	a.Channels = []core.Channel{{ID: "ch-a", Title: "Alpha channel"}}
	a.Sync.Groups["group-a"] = a.Groups[0]
	a.Sync.Channels["ch-a"] = a.Channels[0]

	tt := sidebarTester(t, a)
	if err := tt.Click("Alpha"); err != nil {
		t.Fatalf("click group row: %v", err)
	}
	tt.Frame()
	if a.GroupCollapsed("group-a") {
		t.Fatal("clicking the group row did not expand it")
	}
	if data, err := os.ReadFile(filepath.Join(dir, "settings.json")); err != nil {
		t.Fatalf("settings file: %v", err)
	} else if len(data) == 0 {
		t.Fatal("settings file is empty")
	}

	// LoadApp seeds defaultSettings and unmarshals settings.json over it; do
	// the same read against the same directory to relaunch.
	settings := defaultSettings()
	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatalf("relaunch read: %v", err)
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("relaunch decode: %v", err)
	}
	relaunched := &App{Settings: settings}
	if relaunched.GroupCollapsed("group-a") {
		t.Fatal("the expanded fold state did not survive the relaunch")
	}

	// Folding it back persists the collapse too.
	if err := tt.Click("Alpha"); err != nil {
		t.Fatalf("click group row again: %v", err)
	}
	tt.Frame()
	settings = defaultSettings()
	data, err = os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	if !(&App{Settings: settings}).GroupCollapsed("group-a") {
		t.Fatal("the collapsed fold state did not survive the relaunch")
	}
}
