package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
	mui "github.com/simon/youtube-reader/ui"
)

// sidebarTestApp is the smallest window that shows the whole sidebar:
// Library, one channel group with one channel, and the Channels section, in
// 1280 x 808 as the design board measures it. The refresh status is not in the
// sidebar: it left with the menu bar status item, and status_test.go pins that
// it draws nothing here.
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

// TestSidebarCountsMatchTheirDestination pins the invariant the sidebar
// comment states: each row's count equals the videos its destination shows.
// The scenarios cover a favorite that is only in the sync file, a favorited
// Short while Shorts are hidden, a favorite whose video leaves the store, and
// that the transient filter bar stays out of the counts.
func TestSidebarCountsMatchTheirDestination(t *testing.T) {
	favorite := core.Video{ID: "fav", ChannelID: "ch-a", Title: "Favourite"}
	plain := core.Video{ID: "plain", ChannelID: "ch-b", Title: "Plain"}
	shortFavorite := core.Video{ID: "short-fav", ChannelID: "ch-a", Title: "Short favourite", IsShort: true}
	readFavorite := core.Video{ID: "read-fav", ChannelID: "ch-b", Title: "Read favourite"}
	a := &App{
		Dir:      t.TempDir(),
		Selected: "favorites",
		Settings: core.Settings{ExpandedGroups: map[string]bool{}},
		Sync: core.SyncState{
			Favorites: map[string]core.FavoriteRecord{
				favorite.ID:      {VideoID: favorite.ID},
				shortFavorite.ID: {VideoID: shortFavorite.ID},
				readFavorite.ID:  {VideoID: readFavorite.ID},
				"backup-only":    {VideoID: "backup-only"},
			},
			ReadStates: map[string]map[string]core.ReadRecord{
				readFavorite.ID: {"device": {Read: true}},
			},
		},
		Videos: []core.Video{favorite, plain, shortFavorite, readFavorite},
	}
	w := &Window{app: a, selectedIndex: new(int)}
	check := func(name string, total, favorites, chA, chB int) {
		t.Helper()
		got := w.sidebarCounts()
		if got.total != total || got.favorites != favorites || got.perChannel["ch-a"] != chA || got.perChannel["ch-b"] != chB {
			t.Fatalf("%s: counts = total %d, favorites %d, ch-a %d, ch-b %d; want %d, %d, %d, %d",
				name, got.total, got.favorites, got.perChannel["ch-a"], got.perChannel["ch-b"], total, favorites, chA, chB)
		}
	}

	check("shorts hidden", 3, 2, 1, 2)

	a.Settings.ShowShorts = true
	check("shorts shown", 4, 3, 2, 2)

	a.Settings.ShowShorts = false
	a.UnreadOnly = true
	check("unread only stays out of counts", 3, 2, 1, 2)

	a.UnreadOnly = false
	a.Settings.ShowShorts = true
	a.Search = "short"
	check("search stays out of counts", 4, 3, 2, 2)

	a.Search = ""
	a.Settings.ShowShorts = false
	a.ChannelFilter = "ch-b"
	check("channel filter stays out of counts", 3, 2, 1, 2)

	// A refresh that drops the channel leaves its favorite in the sync file
	// but removes the video from the store, so neither count includes it.
	a.ChannelFilter = ""
	a.Videos = []core.Video{favorite, plain, shortFavorite}
	check("favorite left the store", 2, 1, 1, 1)
}

// TestSidebarRendersDestinationCounts checks the count each row actually
// paints, not just the helper's arithmetic: every destination row shows the
// number of videos its destination holds, in both the collapsed and the
// expanded group, and in both places a channel row appears.
func TestSidebarRendersDestinationCounts(t *testing.T) {
	type channelFixture struct {
		id, title string
		videos    int
	}
	channels := []channelFixture{
		{"ch-a", "Alpha channel", 17},
		{"ch-b", "Beta channel", 12},
		{"ch-c", "Gamma channel", 7},
		{"ch-d", "Delta channel", 4},
	}
	const favoriteVideos = 23
	var (
		videos       []core.Video
		appChannels  []core.Channel
		syncChannels = map[string]core.Channel{}
		perChannel   = map[string]int{}
	)
	for _, ch := range channels {
		appChannels = append(appChannels, core.Channel{ID: ch.id, Title: ch.title})
		syncChannels[ch.id] = core.Channel{ID: ch.id, Title: ch.title}
		perChannel[ch.id] = ch.videos
		for i := 0; i < ch.videos; i++ {
			videos = append(videos, core.Video{
				ID:           fmt.Sprintf("%s-%d", ch.id, i),
				ChannelID:    ch.id,
				ChannelTitle: ch.title,
				Title:        fmt.Sprintf("%s video %d", ch.title, i),
			})
		}
	}
	favorites := make(map[string]core.FavoriteRecord, favoriteVideos)
	for _, v := range videos[:favoriteVideos] {
		favorites[v.ID] = core.FavoriteRecord{VideoID: v.ID}
	}
	groups := []core.ChannelGroup{
		{ID: "group-a", Name: "Alpha group", ChannelIDs: []string{"ch-a", "ch-b"}},
		{ID: "group-b", Name: "Beta group", ChannelIDs: []string{"ch-c", "ch-d"}},
	}
	a := &App{
		Dir:      t.TempDir(),
		Selected: "all",
		Settings: core.Settings{
			APIKey:         "test",
			SidebarWidth:   defaultSidebarWidth,
			DetailsWidth:   defaultDetailsWidth,
			ExpandedGroups: map[string]bool{"group-a": true},
		},
		Sync: core.SyncState{
			Favorites:  favorites,
			ReadStates: map[string]map[string]core.ReadRecord{},
			Channels:   syncChannels,
			Groups:     map[string]core.ChannelGroup{"group-a": groups[0], "group-b": groups[1]},
		},
		Groups:   groups,
		Channels: appChannels,
		Videos:   videos,
	}
	tt := sidebarTester(t, a)

	assertSidebarCount(t, tt, "All videos", fmt.Sprintf("%d", len(videos)), 1)
	assertSidebarCount(t, tt, "Favorites", fmt.Sprintf("%d", favoriteVideos), 1)
	assertSidebarCount(t, tt, "Alpha group", fmt.Sprintf("%d", perChannel["ch-a"]+perChannel["ch-b"]), 1)
	assertSidebarCount(t, tt, "Beta group", fmt.Sprintf("%d", perChannel["ch-c"]+perChannel["ch-d"]), 1)
	assertSidebarCount(t, tt, "Alpha channel", fmt.Sprintf("%d", perChannel["ch-a"]), 1)
	assertSidebarCount(t, tt, "Beta channel", fmt.Sprintf("%d", perChannel["ch-b"]), 1)
	if n := countText(tt, "Gamma channel"); n != 0 {
		t.Fatalf("a channel in a group must not appear again below the groups: %q", tt.Texts())
	}
	if n := countText(tt, "Delta channel"); n != 0 {
		t.Fatalf("a channel in a group must not appear again below the groups: %q", tt.Texts())
	}
}

// sidebarRowBoxes returns the boxes of the sidebar rows named name. A group
// row is a disclosure and a navigation row is a button.
func sidebarRowBoxes(tt *ui.Tester, name string) []ui.Rect {
	var boxes []ui.Rect
	for _, info := range tt.AccessAll() {
		if info.Name == name && (info.Role == ui.RoleButton || info.Role == ui.RoleDisclosure) {
			boxes = append(boxes, info.Bounds)
		}
	}
	return boxes
}

// assertSidebarCount checks that name names the expected number of sidebar
// rows and that each one paints count inside its own box.
func assertSidebarCount(t *testing.T, tt *ui.Tester, name, count string, rows int) {
	t.Helper()
	boxes := sidebarRowBoxes(tt, name)
	if len(boxes) != rows {
		t.Fatalf("%q has %d sidebar rows, want %d", name, len(boxes), rows)
	}
	shown := 0
	for _, text := range tt.Texts() {
		if text == count {
			shown++
		}
	}
	if shown != rows {
		t.Fatalf("%q count %q appears %d times, want once per row (%d); texts = %q", name, count, shown, rows, tt.Texts())
	}
	text, ok := tt.Find(count)
	if !ok {
		t.Fatalf("%q shows no %q count", name, count)
	}
	for _, box := range boxes {
		if text.X >= box.X && text.X+text.W <= box.X+box.W && text.Y >= box.Y && text.Y+text.H <= box.Y+box.H {
			return
		}
	}
	t.Fatalf("%q rows at %+v do not hold the %q count at %+v", name, boxes, count, text)
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

	// The grouped channel is now the last row: Down at the end must leave
	// both the selection and the ring where they are.
	_, before, ok := ringLines(t, tt, mui.NewTokens(nil, false), int(defaultSidebarWidth))
	if !ok {
		t.Fatal("the channel row has no focus ring")
	}
	tt.Key(0, ui.KeyDown)
	if a.Selected != "channel:ch-a" {
		t.Fatalf("Down past the last row selected %q, want the same channel", a.Selected)
	}
	_, after, ok := ringLines(t, tt, mui.NewTokens(nil, false), int(defaultSidebarWidth))
	if !ok || after != before {
		t.Fatalf("Down past the last row moved the ring from %d to %d", before, after)
	}

	tt.Key(0, ui.KeyUp)
	if a.Selected != "favorites" {
		t.Fatalf("Up from the last row selected %q, want favorites", a.Selected)
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

// TestSidebarArrowFocusRingAfterPointerClick proves the keyboard focus reads
// after a pointer press, which clears MyGo's focus-visible flag: clicking a
// row must not leave the next arrow move without a ring.
func TestSidebarArrowFocusRingAfterPointerClick(t *testing.T) {
	for _, dark := range []bool{false, true} {
		a := sidebarTestApp(t, true)
		w := &Window{app: a, selectedIndex: new(int)}
		tt := ui.NewTester(w.view, 1280, 808)
		tt.SetDark(dark)
		tt.Frame()
		tokens := mui.NewTokens(nil, dark)

		if err := tt.Click("Favorites"); err != nil {
			t.Fatalf("dark=%v: click Favorites: %v", dark, err)
		}
		tt.Frame()
		if a.Selected != "favorites" {
			t.Fatalf("dark=%v: click selected %q, want favorites", dark, a.Selected)
		}
		if _, _, ok := ringLines(t, tt, tokens, int(defaultSidebarWidth)); ok {
			t.Fatalf("dark=%v: pointer focus painted a keyboard ring", dark)
		}

		tt.Key(0, ui.KeyDown)
		if a.Selected != "channel:ch-a" {
			t.Fatalf("dark=%v: Down after a click selected %q, want channel:ch-a", dark, a.Selected)
		}
		tt.Frame()
		first, last, ok := ringLines(t, tt, tokens, int(defaultSidebarWidth))
		if !ok {
			t.Fatalf("dark=%v: Down after a click moved the focus without a ring", dark)
		}
		if last-first < int(mui.SidebarRowHeight) {
			t.Fatalf("dark=%v: ring spans %d px, want at least one %v pt row", dark, last-first, mui.SidebarRowHeight)
		}
	}
}

// TestSidebarArrowFocusRingSurvivesWindowBlur proves the ring on the
// arrow-focused row comes back with the window: switching away and back must
// not leave a focused row with no indicator until the next arrow press.
func TestSidebarArrowFocusRingSurvivesWindowBlur(t *testing.T) {
	for _, dark := range []bool{false, true} {
		a := sidebarTestApp(t, true)
		w := &Window{app: a, selectedIndex: new(int)}
		tt := ui.NewTester(w.view, 1280, 808)
		tt.SetDark(dark)
		tt.Frame()
		tokens := mui.NewTokens(nil, dark)

		if err := tt.Click("Favorites"); err != nil {
			t.Fatalf("dark=%v: click Favorites: %v", dark, err)
		}
		tt.Key(0, ui.KeyDown)
		if a.Selected != "channel:ch-a" {
			t.Fatalf("dark=%v: Down after a click selected %q, want channel:ch-a", dark, a.Selected)
		}
		first, last, ok := ringLines(t, tt, tokens, int(defaultSidebarWidth))
		if !ok {
			t.Fatalf("dark=%v: arrow focus has no ring before the window loses focus", dark)
		}

		tt.SetFocused(false)
		tt.Frame()
		if _, _, ok := ringLines(t, tt, tokens, int(defaultSidebarWidth)); ok {
			t.Fatalf("dark=%v: the inactive window still paints the keyboard ring", dark)
		}
		tt.SetFocused(true)
		tt.Frame()
		againFirst, againLast, ok := ringLines(t, tt, tokens, int(defaultSidebarWidth))
		if !ok {
			t.Fatalf("dark=%v: the arrow-focused row lost its ring when the window came back", dark)
		}
		if againFirst != first || againLast != last {
			t.Fatalf("dark=%v: ring moved from %d-%d to %d-%d across a window blur", dark, first, last, againFirst, againLast)
		}
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
	if n := countText(tt, "Alpha channel"); n != 1 {
		t.Fatalf("expanded group should show its channel exactly once; got %d in %q", n, tt.Texts())
	}
	tt.Key(0, ui.KeyEnter)
	if !a.GroupCollapsed("group-a") {
		t.Fatal("Return on the expanded group row did not collapse it")
	}
	if n := countText(tt, "Alpha channel"); n != 0 {
		t.Fatalf("collapsed group should drop its channel row; got %d in %q", n, tt.Texts())
	}
}

// TestSidebarFocusRingOnEveryRow proves the keyboard focus reads on a
// navigation row, a channel row, and a fold row.
func TestSidebarFocusRingOnEveryRow(t *testing.T) {
	for _, dark := range []bool{false, true} {
		a := sidebarTestApp(t, true)
		w := &Window{app: a, selectedIndex: new(int)}
		tt := ui.NewTester(w.view, 1280, 808)
		tt.SetDark(dark)
		tt.Frame()
		tokens := mui.NewTokens(nil, dark)

		for _, name := range []string{"All videos", "Alpha channel", "Alpha"} {
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

// TestSidebarGroupFocusRingClosesAtBlockEdge focuses a group block's last row
// with another block right below it. The ring reaches 5 pt past the row and
// the next block's opaque row background begins 2 pt below, so the block must
// paint above its following sibling or the ring's bottom band disappears, in
// both the collapsed and the expanded block.
func TestSidebarGroupFocusRingClosesAtBlockEdge(t *testing.T) {
	for _, tc := range []struct {
		name   string
		focus  string
		expand bool
	}{
		{"collapsed group", "Alpha", false},
		{"expanded group", "Alpha channel", true},
	} {
		for _, dark := range []bool{false, true} {
			a := reorderTestApp(t)
			a.Channels = []core.Channel{{ID: "ch-a1", Title: "Alpha channel"}}
			a.Sync.Channels = map[string]core.Channel{"ch-a1": {ID: "ch-a1", Title: "Alpha channel"}}
			a.Groups[0].ChannelIDs = []string{"ch-a1"}
			a.Sync.Groups["group-a"] = a.Groups[0]
			a.Settings.ExpandedGroups["group-a"] = tc.expand
			w := &Window{app: a, selectedIndex: new(int)}
			tt := ui.NewTester(w.view, 1280, 808)
			tt.SetDark(dark)
			tt.Frame()
			tokens := mui.NewTokens(nil, dark)
			focusByTab(t, tt, tc.focus)
			boxes := sidebarRowBoxes(tt, tc.focus)
			if len(boxes) == 0 {
				t.Fatalf("%s dark=%v: %q has no sidebar row", tc.name, dark, tc.focus)
			}
			box := boxes[0]
			_, last, ok := ringLines(t, tt, tokens, int(defaultSidebarWidth))
			if !ok {
				t.Fatalf("%s dark=%v: %q has no visible focus ring", tc.name, dark, tc.focus)
			}
			// The ring's outer edge sits FocusRingGap+FocusRing below the row,
			// and the 2 pt stroke covers the last row inside it.
			want := int(box.Y + box.H + mui.FocusRingGap + mui.FocusRing - 1)
			if last < want {
				t.Fatalf("%s dark=%v: %q ring ends at y=%d, want at least %d (row box %+v)", tc.name, dark, tc.focus, last, want, box)
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

// reorderTestApp is a collapsed three-group sidebar with no channels, so each
// group block is exactly one row tall and the edge a drop picks is exact.
func reorderTestApp(t *testing.T) *App {
	t.Helper()
	groups := []core.ChannelGroup{
		{ID: "group-a", Name: "Alpha"},
		{ID: "group-b", Name: "Bravo"},
		{ID: "group-c", Name: "Charlie"},
	}
	syncGroups := make(map[string]core.ChannelGroup, len(groups))
	for _, g := range groups {
		syncGroups[g.ID] = g
	}
	return &App{
		Dir:      t.TempDir(),
		Selected: "all",
		Settings: core.Settings{
			APIKey:         "test",
			SidebarWidth:   defaultSidebarWidth,
			DetailsWidth:   defaultDetailsWidth,
			ExpandedGroups: map[string]bool{},
		},
		Sync: core.SyncState{
			Favorites:  map[string]core.FavoriteRecord{},
			ReadStates: map[string]map[string]core.ReadRecord{},
			Channels:   map[string]core.Channel{},
			Groups:     syncGroups,
		},
		Groups:   groups,
		Channels: []core.Channel{},
		Videos:   []core.Video{},
	}
}

// rowBox returns the box of the single sidebar row named name.
func rowBox(t *testing.T, tt *ui.Tester, name string) ui.Rect {
	t.Helper()
	boxes := sidebarRowBoxes(tt, name)
	if len(boxes) != 1 {
		t.Fatalf("%q boxes = %+v, want one sidebar row", name, boxes)
	}
	return boxes[0]
}

// dragPointer drags the pointer from the row box to (x, y) in four steps, as
// a hand does, and releases it there.
func dragPointer(tt *ui.Tester, from ui.Rect, x, y float32) {
	sx, sy := from.X+20, from.Y+from.H/2
	tt.Press(sx, sy)
	for k := 1; k <= 4; k++ {
		f := float32(k) / 4
		tt.Move(sx+(x-sx)*f, sy+(y-sy)*f)
	}
	tt.Release(x, y)
	tt.Frame()
}

// TestSidebarGroupDragPreviewsTheOrder drags the last group over the first
// block's top half and checks the list takes the new order while the pointer
// is still held, then keeps it when the pointer lets go, in both appearances.
func TestSidebarGroupDragPreviewsTheOrder(t *testing.T) {
	want := []string{"Charlie", "Alpha", "Bravo"}
	for _, dark := range []bool{false, true} {
		a := reorderTestApp(t)
		w := &Window{app: a, selectedIndex: new(int)}
		tt := ui.NewTester(w.view, 1280, 808)
		tt.SetDark(dark)
		tt.Frame()
		charlie := rowBox(t, tt, "Charlie")
		alpha := rowBox(t, tt, "Alpha")
		sx, sy := charlie.X+20, charlie.Y+charlie.H/2
		tt.Press(sx, sy)
		tt.Move(sx, sy-10)
		tt.Move(alpha.X+20, alpha.Y+4)
		tt.Frame()
		if got := groupNames(a.Groups); !slices.Equal(got, want) {
			t.Fatalf("dark=%v: order while held = %q, want the drop's order %q", dark, got, want)
		}
		// The rows show the new order before the pointer lets go: Charlie's
		// block is now above Alpha's.
		if box, top := rowBox(t, tt, "Charlie"), rowBox(t, tt, "Alpha"); box.Y >= top.Y {
			t.Fatalf("dark=%v: Charlie at y=%v is not above Alpha at y=%v", dark, box.Y, top.Y)
		}
		tt.Release(alpha.X+20, alpha.Y+4)
		tt.Frame()
		if got := groupNames(a.Groups); !slices.Equal(got, want) {
			t.Fatalf("dark=%v: order after the drop = %q, want %q", dark, got, want)
		}
	}
}

// TestSidebarGroupDragEscapeRestoresTheOrder gives a group drag up with
// Escape: the list goes back to the order it had, an order is not saved, and
// the release that follows changes nothing.
func TestSidebarGroupDragEscapeRestoresTheOrder(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	for _, g := range []core.ChannelGroup{
		{ID: "group-a", Name: "Alpha"},
		{ID: "group-b", Name: "Bravo"},
		{ID: "group-c", Name: "Charlie"},
	} {
		a.Sync.Groups[g.ID] = g
	}
	if err := a.reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	a.Settings.SidebarWidth = defaultSidebarWidth
	tt := sidebarTester(t, a)
	charlie := rowBox(t, tt, "Charlie")
	alpha := rowBox(t, tt, "Alpha")
	sx, sy := charlie.X+20, charlie.Y+charlie.H/2
	tt.Press(sx, sy)
	tt.Move(sx, sy-10)
	tt.Move(alpha.X+20, alpha.Y+4)
	tt.Frame()
	if got := groupNames(a.Groups); !slices.Equal(got, []string{"Charlie", "Alpha", "Bravo"}) {
		t.Fatalf("order while held = %q, want the drop's order", got)
	}

	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	before := []string{"Alpha", "Bravo", "Charlie"}
	if got := groupNames(a.Groups); !slices.Equal(got, before) {
		t.Fatalf("order after Escape = %q, want %q", got, before)
	}
	tt.Release(alpha.X+20, alpha.Y+4)
	tt.Frame()
	if got := groupNames(a.Groups); !slices.Equal(got, before) {
		t.Fatalf("order after the release that followed Escape = %q, want %q", got, before)
	}
	settings := defaultSettings()
	if data, err := os.ReadFile(filepath.Join(dir, "settings.json")); err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			t.Fatalf("settings decode: %v", err)
		}
	}
	if len(settings.GroupOrder) != 0 {
		t.Fatalf("Escape saved the order %q", settings.GroupOrder)
	}
}

// TestSidebarGroupDragIntoAnOpenGroup drags a group onto the channels of an
// open group: the block takes the drop anywhere over its channels, the fold it
// crossed stays open, and the dropped group stays folded as it was.
func TestSidebarGroupDragIntoAnOpenGroup(t *testing.T) {
	a := reorderTestApp(t)
	a.Channels = []core.Channel{{ID: "ch-a1", Title: "One"}, {ID: "ch-a2", Title: "Two"}}
	a.Sync.Channels = map[string]core.Channel{
		"ch-a1": {ID: "ch-a1", Title: "One"},
		"ch-a2": {ID: "ch-a2", Title: "Two"},
	}
	a.Groups[0].ChannelIDs = []string{"ch-a1", "ch-a2"}
	a.Sync.Groups["group-a"] = a.Groups[0]
	a.Settings.ExpandedGroups["group-a"] = true
	tt := sidebarTester(t, a)
	charlie := rowBox(t, tt, "Charlie")
	// The all-channels section repeats the channel's name; the group's own
	// row is the first one, inside Alpha's block.
	two := sidebarRowBoxes(tt, "Two")[0]
	dragPointer(tt, charlie, two.X+20, two.Y+two.H/2)

	if got := groupNames(a.Groups); !slices.Equal(got, []string{"Alpha", "Charlie", "Bravo"}) {
		t.Fatalf("order after dropping on a channel of Alpha = %q, want Charlie after Alpha", got)
	}
	if a.GroupCollapsed("group-a") {
		t.Fatal("the drag closed the group it crossed")
	}
	if !a.GroupCollapsed("group-c") {
		t.Fatal("the drag opened the dragged group")
	}
}

// TestSidebarGroupDragReleasedOutsideKeepsTheOrder drags a group over another
// block and lets it go over the Library rows above the groups: the order the
// list showed is what it keeps, and it is saved.
func TestSidebarGroupDragReleasedOutsideKeepsTheOrder(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	for _, g := range []core.ChannelGroup{
		{ID: "group-a", Name: "Alpha"},
		{ID: "group-b", Name: "Bravo"},
		{ID: "group-c", Name: "Charlie"},
	} {
		a.Sync.Groups[g.ID] = g
	}
	if err := a.reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	a.Settings.SidebarWidth = defaultSidebarWidth
	tt := sidebarTester(t, a)
	charlie := rowBox(t, tt, "Charlie")
	alpha := rowBox(t, tt, "Alpha")
	sx, sy := charlie.X+20, charlie.Y+charlie.H/2
	tt.Press(sx, sy)
	tt.Move(sx, sy-10)
	tt.Move(alpha.X+20, alpha.Y+4)
	tt.Frame()
	favorites := rowBox(t, tt, "Favorites")
	tt.Release(favorites.X+20, favorites.Y+favorites.H/2)
	tt.Frame()

	want := []string{"Charlie", "Alpha", "Bravo"}
	if got := groupNames(a.Groups); !slices.Equal(got, want) {
		t.Fatalf("order after releasing over Favorites = %q, want the shown order %q", got, want)
	}
	settings := defaultSettings()
	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatalf("settings file: %v", err)
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("settings decode: %v", err)
	}
	if got := settings.GroupOrder; !slices.Equal(got, []string{"group-c", "group-a", "group-b"}) {
		t.Fatalf("saved group order = %q, want the kept order", got)
	}
}

// TestSidebarGroupDragToTheEnd drags the first group past the last block's
// middle: the group joins the end, where the sidebar keeps it.
func TestSidebarGroupDragToTheEnd(t *testing.T) {
	a := reorderTestApp(t)
	tt := sidebarTester(t, a)
	alpha := rowBox(t, tt, "Alpha")
	charlie := rowBox(t, tt, "Charlie")
	dragPointer(tt, alpha, charlie.X+20, charlie.Y+charlie.H-4)

	if got := groupNames(a.Groups); !slices.Equal(got, []string{"Bravo", "Charlie", "Alpha"}) {
		t.Fatalf("order after dropping past the last block = %q, want Alpha at the end", got)
	}
}

// TestSidebarGroupDragDropLandsAtTheReleasePoint releases a fast drag where
// the pointer never settles between moves: the group lands at the boundary the
// release point shows, not where the last hovered frame left it.
func TestSidebarGroupDragDropLandsAtTheReleasePoint(t *testing.T) {
	a := reorderTestApp(t)
	tt := sidebarTester(t, a)
	charlie := rowBox(t, tt, "Charlie")
	alpha := rowBox(t, tt, "Alpha")
	sx, sy := charlie.X+20, charlie.Y+charlie.H/2
	tt.Press(sx, sy)
	tt.Move(sx, sy-10)
	// The last build sees the pointer over Alpha's bottom half, which puts
	// Charlie after Alpha.
	tt.Move(alpha.X+20, alpha.Y+alpha.H-4)
	tt.Frame()
	if got := groupNames(a.Groups); !slices.Equal(got, []string{"Alpha", "Charlie", "Bravo"}) {
		t.Fatalf("order at the last hover = %q, want Charlie after Alpha", got)
	}
	// The release lands higher, over Alpha's top half: the drop belongs to
	// the frame that reads the release, not the one the pointer last crossed.
	alpha = rowBox(t, tt, "Alpha")
	tt.Release(alpha.X+20, alpha.Y+4)
	tt.Frame()
	if got := groupNames(a.Groups); !slices.Equal(got, []string{"Charlie", "Alpha", "Bravo"}) {
		t.Fatalf("order after the flick release = %q, want Charlie above Alpha", got)
	}
}

// TestGroupedChannelAppearsOnce proves F-02: a channel in a group is listed
// only inside that group; an unlisted channel appears only in the Ungrouped
// section beneath it. The section stays out of the sidebar when every
// channel is grouped.
func TestGroupedChannelAppearsOnce(t *testing.T) {
	a := sidebarTestApp(t, true)
	a.Channels = []core.Channel{
		{ID: "ch-a", Title: "Alpha channel", AvatarURL: "https://example.invalid/alpha.png"},
		{ID: "ch-lone", Title: "Lone channel"},
	}
	a.Sync.Channels = map[string]core.Channel{
		"ch-a":    a.Channels[0],
		"ch-lone": a.Channels[1],
	}
	tt := sidebarTester(t, a)

	if got := countText(tt, "Alpha channel"); got != 1 {
		t.Fatalf("a grouped channel appears %d times, want once: %q", got, tt.Texts())
	}
	if got := countText(tt, "Lone channel"); got != 1 {
		t.Fatalf("an ungrouped channel appears %d times, want once: %q", got, tt.Texts())
	}
	if !tt.HasText("Ungrouped") {
		t.Fatalf("the ungrouped section has no header: %q", tt.Texts())
	}
	alpha, ok := tt.Find("Alpha channel")
	if !ok {
		t.Fatal("the grouped channel row is missing")
	}
	lone, ok := tt.Find("Lone channel")
	if !ok {
		t.Fatal("the ungrouped channel row is missing")
	}
	if lone.Y <= alpha.Y {
		t.Fatalf("the ungrouped row at %v is not below the group row at %v", lone.Y, alpha.Y)
	}
}

// TestUngroupedHeaderIsLocalized checks the agreed Ungrouped / 未分组 label
// through the framework's Context.Locale, as set by ui.Tester.SetLocale.
func TestUngroupedHeaderIsLocalized(t *testing.T) {
	a := sidebarTestApp(t, false)
	a.Channels = append(a.Channels, core.Channel{ID: "ch-lone", Title: "Lone channel"})
	a.Sync.Channels["ch-lone"] = a.Channels[len(a.Channels)-1]
	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.view, 1280, 808)
	tt.SetLocale("zh-Hans")
	tt.Frame()
	if !tt.HasText("未分组") {
		t.Fatalf("Chinese locale did not localize the ungrouped header: %q", tt.Texts())
	}
	if tt.HasText("Ungrouped") {
		t.Fatalf("Chinese locale still shows the English header: %q", tt.Texts())
	}
}

// TestNoUngroupedSectionWhenAllChannelsAreGrouped checks that the header and
// its empty state stay out of the sidebar once every channel has a group.
func TestNoUngroupedSectionWhenAllChannelsAreGrouped(t *testing.T) {
	a := sidebarTestApp(t, false)
	tt := sidebarTester(t, a)
	if tt.HasText("Ungrouped") {
		t.Fatalf("an all-grouped sidebar shows an empty Ungrouped header: %q", tt.Texts())
	}
}

// TestSidebarChannelRowUsesItsOwnMetrics pins F-04 at the 260 pt design width:
// channel rows are 34 pt with a 24 pt avatar, while navigation rows and the
// fold row stay at 28 pt.
func TestSidebarChannelRowUsesItsOwnMetrics(t *testing.T) {
	a := sidebarTestApp(t, true)
	a.Channels = append(a.Channels, core.Channel{ID: "ch-lone", Title: "Lone channel"})
	a.Sync.Channels["ch-lone"] = a.Channels[len(a.Channels)-1]
	tt := sidebarTester(t, a)

	if mui.SidebarChannelRowHeight != 34 || mui.SidebarChannelAvatar != 24 || mui.SidebarRowHeight != 28 {
		t.Fatalf("sidebar metrics: channel row %v, avatar %v, nav row %v", mui.SidebarChannelRowHeight, mui.SidebarChannelAvatar, mui.SidebarRowHeight)
	}
	channel := rowBox(t, tt, "Lone channel")
	nav := rowBox(t, tt, "All videos")
	if diff := abs(channel.H - mui.SidebarChannelRowHeight); diff > 0.5 {
		t.Fatalf("channel row is %v pt tall, want %v", channel.H, mui.SidebarChannelRowHeight)
	}
	if diff := abs(nav.H - mui.SidebarRowHeight); diff > 0.5 {
		t.Fatalf("navigation row is %v pt tall, want %v", nav.H, mui.SidebarRowHeight)
	}
}

// TestChannelAvatarShowsLoadedImageAndFallsBack proves F-03 from a stored
// avatar URL down to the sidebar row. With a loadable picture the row draws
// that picture in the 24 pt slot; with a failed (or absent) URL it draws the
// monogram instead of a blank disc. The failure path is compared against a
// no-URL render, which is the defined monogram behaviour.
func TestChannelAvatarShowsLoadedImageAndFallsBack(t *testing.T) {
	// A tiny solid-colour PNG served locally makes the avatar a real,
	// loadable image whose centre pixel is known independently of the app.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(pngAvatar(t, color.RGBA{12, 200, 34, 255}))
	}))
	defer srv.Close()

	a := sidebarTestApp(t, false)
	a.Groups[0].ChannelIDs = nil
	a.Sync.Groups["group-a"] = a.Groups[0]
	a.Channels = []core.Channel{{ID: "ch-a", Title: "Alpha channel", AvatarURL: srv.URL + "/avatar.png"}}
	a.Sync.Channels["ch-a"] = a.Channels[0]
	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.view, 1280, 808)
	tt.Frame()

	// The picture arrives asynchronously; keep rendering until the avatar's
	// slot shows the picture's green rather than the neutral placeholder.
	row := rowBox(t, tt, "Alpha channel")
	center := func(img *image.RGBA) color.RGBA {
		return img.RGBAAt(int(row.X+mui.SidebarChannelAvatar/2), int(row.Y+row.H/2))
	}
	loaded := false
	for range 100 {
		tt.Frame()
		c := center(tt.Image())
		if c.G > 150 && c.R < 80 && c.B < 80 {
			loaded = true
			break
		}
	}
	if !loaded {
		t.Fatalf("the avatar slot never showed the loaded image: %v", center(tt.Image()))
	}

	// A failed URL must fall back to the same monogram an absent URL draws,
	// not a blank disc. Compare a patch of the slot against a render with no
	// URL at all: that is the defined monogram behaviour, so equality proves
	// the failure path reaches it.
	a.Channels[0].AvatarURL = "http://127.0.0.1:1/avatar.png"
	a.Sync.Channels["ch-a"] = a.Channels[0]
	tt = ui.NewTester(w.view, 1280, 808)
	tt.Frame()
	failedSlot := slotPixels(t, tt, row)
	a.Channels[0].AvatarURL = ""
	a.Sync.Channels["ch-a"] = a.Channels[0]
	tt = ui.NewTester(w.view, 1280, 808)
	tt.Frame()
	monogramSlot := slotPixels(t, tt, row)
	if failedSlot != monogramSlot {
		t.Fatalf("a failed avatar URL did not render the monogram: %v vs %v", failedSlot, monogramSlot)
	}
	if got := countText(tt, "Alpha channel"); got != 1 {
		t.Fatalf("the avatar fallback lost its channel row: %q", tt.Texts())
	}
}

// slotPixels hashes the rendered pixels of the avatar slot in row, so two
// renders can be compared for having drawn the same picture or monogram.
func slotPixels(t *testing.T, tt *ui.Tester, row ui.Rect) uint32 {
	t.Helper()
	img := tt.Image()
	const n = 12
	midX, midY := int(row.X+mui.SidebarChannelAvatar/2), int(row.Y+row.H/2)
	h := uint32(2166136261)
	for dy := -n; dy <= n; dy++ {
		for dx := -n; dx <= n; dx++ {
			c := img.RGBAAt(midX+dx, midY+dy)
			h = (h ^ uint32(c.R) ^ uint32(c.G)<<8 ^ uint32(c.B)<<16) * 16777619
		}
	}
	return h
}

// pngAvatar encodes a solid square test image, the picture the avatar test
// serves for its loadable URL.
func pngAvatar(t *testing.T, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
