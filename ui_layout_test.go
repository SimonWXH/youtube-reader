package main

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
)

func TestFitPaneWidths(t *testing.T) {
	t.Run("defaults without selection", func(t *testing.T) {
		sidebar, details := fitPaneWidths(0, 0, false, 1280)
		if sidebar != defaultSidebarWidth || details != defaultDetailsWidth {
			t.Fatalf("fitPaneWidths = %v, %v; want %v, %v", sidebar, details, defaultSidebarWidth, defaultDetailsWidth)
		}
	})

	t.Run("persists chosen widths", func(t *testing.T) {
		sidebar, details := fitPaneWidths(300, 500, true, 1280)
		if sidebar != 300 || details != 500 {
			t.Fatalf("fitPaneWidths = %v, %v; want 300, 500", sidebar, details)
		}
	})

	t.Run("clamps to minimum and maximum", func(t *testing.T) {
		sidebar, details := fitPaneWidths(10000, -1, true, 1280)
		if sidebar != maxSidebarWidth || details != defaultDetailsWidth {
			t.Fatalf("fitPaneWidths = %v, %v; want %v, %v", sidebar, details, maxSidebarWidth, defaultDetailsWidth)
		}
	})

	t.Run("shrinks panes to respect total width", func(t *testing.T) {
		sidebar, details := fitPaneWidths(maxSidebarWidth, 560, true, 960)
		if sidebar+details+minGridWidth > 960-2*splitDividerWidth() {
			t.Fatalf("panes overflow window: sidebar=%v details=%v", sidebar, details)
		}
		if sidebar < minSidebarWidth || details < minDetailsWidth {
			t.Fatalf("panes below minimum: sidebar=%v details=%v", sidebar, details)
		}
	})
}

func TestBoundedSidebarWidth(t *testing.T) {
	// The design board fixes the draggable range at 220 to 320 pt.
	if minSidebarWidth != 220 || maxSidebarWidth != 320 {
		t.Fatalf("sidebar bounds = %v/%v, want 220/320", minSidebarWidth, maxSidebarWidth)
	}
	if got := boundedSidebarWidth(40, false, 1280); got != minSidebarWidth {
		t.Fatalf("boundedSidebarWidth(40) = %v, want %v", got, minSidebarWidth)
	}
	if got := boundedSidebarWidth(minSidebarWidth, false, 1280); got != minSidebarWidth {
		t.Fatalf("boundedSidebarWidth(%v) = %v, want the minimum kept", minSidebarWidth, got)
	}
	if got := boundedSidebarWidth(maxSidebarWidth, false, 1280); got != maxSidebarWidth {
		t.Fatalf("boundedSidebarWidth(%v) = %v, want the maximum kept", maxSidebarWidth, got)
	}
	if got := boundedSidebarWidth(919, false, 960); got != maxSidebarWidth {
		t.Fatalf("boundedSidebarWidth(919) = %v, want %v", got, maxSidebarWidth)
	}
	if got := boundedSidebarWidth(1000, true, 1280); got != maxSidebarWidth {
		t.Fatalf("boundedSidebarWidth(1000, selected) = %v, want %v", got, maxSidebarWidth)
	}
}

func TestPersistedDetailsWidth(t *testing.T) {
	const total float32 = 1280

	if _, ok := persistedDetailsWidth(total, 400, 850, false, true); ok {
		t.Fatal("details width must not be written when there is no details pane")
	}
	if _, ok := persistedDetailsWidth(total, 400, 850, true, false); ok {
		t.Fatal("details width must not be written when the inner split did not change")
	}
	got, ok := persistedDetailsWidth(total, 400, 550, true, true)
	if !ok || got != 327 {
		t.Fatalf("persistedDetailsWidth = %v, %v; want 327, true", got, ok)
	}
	if got, ok := persistedDetailsWidth(total, 400, 990, true, true); ok || got != 0 {
		t.Fatalf("persistedDetailsWidth with too-small details = %v, %v; want 0, false", got, ok)
	}
}

// TestGridCardMetricsDesign pins the agreed design geometry: at 1280 pt with
// a 260 pt sidebar and a 340 pt details pane the split dividers leave the grid
// 1018 pt closed and 678 pt open, so four 241 pt cards show closed and three
// 210 pt cards open.
func TestGridCardMetricsDesign(t *testing.T) {
	const gap float32 = 8
	const padding float32 = 16
	const designClosedPane float32 = 1280 - 260 - 2
	const designOpenPane float32 = 1280 - 260 - 340 - 2
	cases := []struct {
		name    string
		pane    float32
		columns int
		width   float32
		height  float32
	}{
		{"detail closed", designClosedPane, 4, 241, 211},
		{"detail open", designOpenPane, 3, 210, 194},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := gridCardMetrics(tc.pane-2*padding, gap, defaultMaxGridColumns)
			if got.Columns != tc.columns {
				t.Fatalf("columns = %d; want %d", got.Columns, tc.columns)
			}
			if diff := abs(got.Width - tc.width); diff > 0.5 {
				t.Fatalf("card width = %v; want %v", got.Width, tc.width)
			}
			if diff := abs(got.Height - tc.height); diff > 0.5 {
				t.Fatalf("card height = %v; want %v", got.Height, tc.height)
			}
			if want := got.Width*gridThumbAspect + gridCardTextBlock; got.Height != want {
				t.Fatalf("card height = %v; want width*9/16+%v = %v", got.Height, gridCardTextBlock, want)
			}
		})
	}
}

// TestShippedGridBounds checks the panes the app ships with (sidebar 256,
// details 320) at the 1280 pt default width: the cards they produce must never
// be wider than 280 pt and never outnumber the user's maximum.
func TestShippedGridBounds(t *testing.T) {
	const gap float32 = 8
	const padding float32 = 16
	const window float32 = 1280
	closedPane := window - defaultSidebarWidth - 2*splitDividerWidth()
	openPane := closedPane - defaultDetailsWidth - 2*splitDividerWidth()
	cases := []struct {
		name    string
		pane    float32
		columns int
	}{
		{"detail closed", closedPane, 4},
		{"detail open", openPane, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := gridCardMetrics(tc.pane-2*padding, gap, defaultMaxGridColumns)
			if got.Columns != tc.columns {
				t.Fatalf("columns = %d; want %d", got.Columns, tc.columns)
			}
			if got.Columns > defaultMaxGridColumns {
				t.Fatalf("columns = %d; exceeds the maximum %d", got.Columns, defaultMaxGridColumns)
			}
			if got.Width > gridCardMaxWidth+0.01 {
				t.Fatalf("card width = %v; exceeds %v", got.Width, gridCardMaxWidth)
			}
		})
	}
}

// TestGridCardWidthBounds checks both hard limits at every pane width and
// every maximum-columns setting: no card is ever wider than 280 pt, and the
// grid never takes more columns than the setting allows. Where the setting
// asks for fewer columns than the width rule needs, the leftover width stays
// as a trailing margin instead of wider cards.
func TestGridCardWidthBounds(t *testing.T) {
	const gap float32 = 8
	for pane := float32(320); pane <= 3000; pane += 1 {
		content := pane - 32
		fill := max(int(math.Ceil(float64((content+gap)/(gridCardMaxWidth+gap)))), 1)
		for _, cap := range []int{2, 3, 5, 8} {
			m := gridCardMetrics(content, gap, cap)
			if m.Columns < 1 {
				t.Fatalf("pane %v cap %d: columns = %d", pane, cap, m.Columns)
			}
			if m.Columns > cap {
				t.Fatalf("pane %v cap %d: columns = %d exceeds the cap", pane, cap, m.Columns)
			}
			if want := min(fill, cap); m.Columns != want {
				t.Fatalf("pane %v cap %d: columns = %d; want %d", pane, cap, m.Columns, want)
			}
			if m.Width > gridCardMaxWidth+0.01 {
				t.Fatalf("pane %v cap %d: card width %v exceeds %v", pane, cap, m.Width, gridCardMaxWidth)
			}
			if used, want := m.gridWidth(gap), min(content, float32(m.Columns)*gridCardMaxWidth+gap*float32(m.Columns-1)); abs(used-want) > 0.01 {
				t.Fatalf("pane %v cap %d: grid width %v; want %v", pane, cap, used, want)
			}
			if m.gridWidth(gap) > content+0.01 {
				t.Fatalf("pane %v cap %d: grid width %v overflows the content %v", pane, cap, m.gridWidth(gap), content)
			}
			if fill > cap && abs(m.Width-gridCardMaxWidth) > 0.01 {
				t.Fatalf("pane %v cap %d: capped card width = %v; want exactly %v", pane, cap, m.Width, gridCardMaxWidth)
			}
		}
	}
}

// TestGridCardCapKeepsCeiling pins both hard limits against the agreed design
// geometry (sidebar 260, details 340) and a 1200 pt window with the detail pane
// open. Where the setting's maximum columns binds, the cards stay exactly
// 280 pt wide and the pane's leftover width is trailing margin.
func TestGridCardCapKeepsCeiling(t *testing.T) {
	const gap float32 = 8
	const padding float32 = 16
	const designClosedContent float32 = 1280 - 260 - 2 - 2*padding
	const designOpenContent float32 = 1280 - 260 - 340 - 2 - 2*padding
	detailOpen1200Content := float32(1200) - defaultSidebarWidth - 2*splitDividerWidth() - defaultDetailsWidth - 2*splitDividerWidth() - 2*padding
	cases := []struct {
		name     string
		content  float32
		cap      int
		columns  int
		width    float32
		leftover float32
	}{
		{"design 1280 detail closed, cap 2", designClosedContent, 2, 2, 280, designClosedContent - 568},
		{"design 1280 detail closed, cap 3", designClosedContent, 3, 3, 280, designClosedContent - 856},
		{"design 1280 detail open, cap 2", designOpenContent, 2, 2, 280, designOpenContent - 568},
		{"design 1280 detail open, cap 3", designOpenContent, 3, 3, 210, 0},
		{"1200 detail open, cap 2", detailOpen1200Content, 2, 2, 280, detailOpen1200Content - 568},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := gridCardMetrics(tc.content, gap, tc.cap)
			if got.Columns != tc.columns {
				t.Fatalf("columns = %d; want %d", got.Columns, tc.columns)
			}
			if diff := abs(got.Width - tc.width); diff > 0.5 {
				t.Fatalf("card width = %v; want %v", got.Width, tc.width)
			}
			if leftover := tc.content - got.gridWidth(gap); abs(leftover-tc.leftover) > 0.5 {
				t.Fatalf("trailing margin = %v; want %v", leftover, tc.leftover)
			}
		})
	}
}

// TestGridRendersCeilingWhenCapBinds renders the grid in a 1920 pt window,
// where six 280 pt columns would fit but the shipped five-column setting binds:
// the cards must stay 280 pt wide and the grid must stay left aligned, leaving
// the pane's extra width as trailing margin.
func TestGridRendersCeilingWhenCapBinds(t *testing.T) {
	videos := make([]core.Video, 30)
	for i := range videos {
		videos[i] = core.Video{
			ID:           fmt.Sprintf("video-%d", i),
			Title:        fmt.Sprintf("Video %d", i),
			ChannelID:    "ch-1",
			ChannelTitle: "Channel",
			PublishedAt:  time.Now().Add(-time.Duration(i) * time.Hour).UTC().Format(time.RFC3339),
		}
	}
	a := &App{
		Dir:      t.TempDir(),
		Selected: "all",
		Settings: core.Settings{
			APIKey:         "test-key",
			ExpandedGroups: map[string]bool{},
			MaxGridColumns: defaultMaxGridColumns,
			RefreshMinutes: 60,
		},
		Sync: core.SyncState{
			Favorites:  map[string]core.FavoriteRecord{},
			ReadStates: map[string]map[string]core.ReadRecord{},
			Groups:     map[string]core.ChannelGroup{},
			Channels:   map[string]core.Channel{"ch-1": {ID: "ch-1", Title: "Channel"}},
		},
		Channels: []core.Channel{{ID: "ch-1", Title: "Channel"}},
		Videos:   videos,
	}
	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.view, 1920, 840)
	tt.Frame()
	first, ok := tt.Find("Video 0")
	if !ok {
		t.Fatalf("the grid does not show its first card: %q", tt.Texts())
	}
	second, ok := tt.Find("Video 1")
	if !ok {
		t.Fatalf("the grid does not show its second card: %q", tt.Texts())
	}
	if step := second.X - first.X; abs(step-(gridCardMaxWidth+8)) > 0.5 {
		t.Fatalf("adjacent cards are %v pt apart; want %v", step, gridCardMaxWidth+8)
	}
	if want := defaultSidebarWidth + splitDividerWidth() + 16 + 8; abs(first.X-want) > 1.5 {
		t.Fatalf("the first card starts at %v; want the grid's 16 pt padding at %v", first.X, want)
	}
	fifth, ok := tt.Find("Video 4")
	if !ok {
		t.Fatalf("the grid does not show its fifth card: %q", tt.Texts())
	}
	if step := fifth.X - first.X; abs(step-4*(gridCardMaxWidth+8)) > 0.5 {
		t.Fatalf("the fifth card starts %v pt after the first; want %v", step, 4*(gridCardMaxWidth+8))
	}
	if sixth, ok := tt.Find("Video 5"); ok && sixth.Y-first.Y < 100 {
		t.Fatalf("a sixth card shares the first row at y=%v; the row starts at %v", sixth.Y, first.Y)
	}
	if right := fifth.X - 8 + gridCardMaxWidth; right > 1920-16 {
		t.Fatalf("the last card ends at %v; want the trailing margin inside %v", right, 1920-16)
	}
}

// TestGridCardFitsFrameworkColumns guards the link between our metrics and
// ui.GridView, which counts its columns from the width and minimum width it is
// given: the two must agree or the cards and their gap change under the design.
func TestGridCardFitsFrameworkColumns(t *testing.T) {
	const gap float32 = 8
	for pane := float32(320); pane <= 3000; pane += 7 {
		content := pane - 32
		for _, cap := range []int{2, 5, 8} {
			m := gridCardMetrics(content, gap, cap)
			minWidth := m.gridMinWidth(gap)
			inner := m.gridWidth(gap)
			framed := max(1, int(math.Floor(float64((inner+gap)/(minWidth+gap)))))
			if framed != m.Columns {
				t.Fatalf("pane %v cap %d: GridView would draw %d columns, metrics say %d", pane, cap, framed, m.Columns)
			}
			drawn := (inner - gap*float32(framed-1)) / float32(framed)
			if diff := abs(drawn - m.Width); diff > 0.01 {
				t.Fatalf("pane %v cap %d: GridView width %v, metrics %v", pane, cap, drawn, m.Width)
			}
		}
	}
}

func abs(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// TestGridFocusScroll pins when the grid brings the card under the keyboard
// ring into view: whenever the grid takes the keyboard focus, and when the
// card under the ring changes while the grid keeps it. The user's own wheel
// scroll moves neither, so it is not undone.
func TestGridFocusScroll(t *testing.T) {
	cases := []struct {
		name                string
		focused, wasFocused bool
		focusIndex, last    int
		want                bool
	}{
		{"focus arrives on the first card", true, false, 0, 0, true},
		{"focus arrives on a chosen card", true, false, 42, 42, true},
		{"focus arrives after the user scrolled away", true, false, 0, 42, true},
		{"focus kept, a wheel scroll leaves the card", true, true, 42, 42, false},
		{"focus kept, a filter moved the chosen card", true, true, 12, 42, true},
		{"focus kept, the selection was cleared", true, true, 0, 42, true},
		{"focus lost", false, true, 42, 42, false},
		{"unfocused, the target moved", false, false, 0, 42, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := gridFocusScroll(tc.focused, tc.wasFocused, tc.focusIndex, tc.last)
			if got != tc.want {
				t.Fatalf("gridFocusScroll(focused %v, was %v, index %d, last %d) = %v; want %v",
					tc.focused, tc.wasFocused, tc.focusIndex, tc.last, got, tc.want)
			}
		})
	}
}

// TestGridTabRevealsFirstCard scrolls the grid away from its first card and
// checks that tabbing to the grid, with nothing chosen, brings that card back:
// it is where the keyboard ring lands, and the ring must be visible.
func TestGridTabRevealsFirstCard(t *testing.T) {
	videos := make([]core.Video, 120)
	for i := range videos {
		videos[i] = core.Video{
			ID:           fmt.Sprintf("video-%d", i),
			Title:        fmt.Sprintf("Video %d", i),
			ChannelID:    "ch-1",
			ChannelTitle: "Channel",
			PublishedAt:  time.Now().Add(-time.Duration(i) * time.Hour).UTC().Format(time.RFC3339),
		}
	}
	a := &App{
		Dir:      t.TempDir(),
		Selected: "all",
		Settings: core.Settings{
			APIKey:         "test-key",
			ExpandedGroups: map[string]bool{},
			MaxGridColumns: defaultMaxGridColumns,
			RefreshMinutes: 60,
		},
		Sync: core.SyncState{
			Favorites:  map[string]core.FavoriteRecord{},
			ReadStates: map[string]map[string]core.ReadRecord{},
			Groups:     map[string]core.ChannelGroup{},
			Channels:   map[string]core.Channel{"ch-1": {ID: "ch-1", Title: "Channel"}},
		},
		Channels: []core.Channel{{ID: "ch-1", Title: "Channel"}},
		Videos:   videos,
	}
	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.view, 1280, 840)
	tt.Frame()
	first, ok := tt.Find("Video 0")
	if !ok {
		t.Fatalf("the grid does not show its first card: %q", tt.Texts())
	}
	x, y := first.X+first.W/2, first.Y+first.H/2
	for range 15 {
		tt.Wheel(x, y, 0, 100)
		tt.Advance(200 * time.Millisecond)
	}
	if _, ok := tt.Find("Video 0"); ok {
		t.Fatal("the wheel did not scroll the first card away")
	}
	for range 60 {
		tt.Key(0, ui.KeyTab)
		if _, ok := tt.Find("Video 0"); ok {
			return
		}
	}
	t.Fatal("tabbing to the grid did not bring the first card into view")
}

// TestGridFocusFollowsMovedSelection keeps the grid's keyboard focus on a
// chosen video, then reverses the list so that video moves to the first card
// while the grid still shows the end. The ring follows the video, so the grid
// must scroll back to the card it now marks.
func TestGridFocusFollowsMovedSelection(t *testing.T) {
	videos := make([]core.Video, 120)
	for i := range videos {
		videos[i] = core.Video{
			ID:           fmt.Sprintf("video-%d", i),
			Title:        fmt.Sprintf("Video %d", i),
			ChannelID:    "ch-1",
			ChannelTitle: "Channel",
			PublishedAt:  time.Now().Add(-time.Duration(i) * time.Hour).UTC().Format(time.RFC3339),
		}
	}
	a := &App{
		Dir:      t.TempDir(),
		Selected: "all",
		Settings: core.Settings{
			APIKey:         "test-key",
			ExpandedGroups: map[string]bool{},
			MaxGridColumns: defaultMaxGridColumns,
			RefreshMinutes: 60,
		},
		Sync: core.SyncState{
			Favorites:  map[string]core.FavoriteRecord{},
			ReadStates: map[string]map[string]core.ReadRecord{},
			Groups:     map[string]core.ChannelGroup{},
			Channels:   map[string]core.Channel{"ch-1": {ID: "ch-1", Title: "Channel"}},
		},
		Channels: []core.Channel{{ID: "ch-1", Title: "Channel"}},
		Videos:   videos,
	}
	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.view, 1280, 840)
	tt.Frame()
	if err := tt.Click("Video 3"); err != nil {
		t.Fatalf("click a card: %v", err)
	}
	if a.SelectedVideoID != videos[3].ID {
		t.Fatalf("the click chose %q; want %q", a.SelectedVideoID, videos[3].ID)
	}
	for range 60 {
		tt.Key(0, ui.KeyTab)
		if w.gridKeyboardFocus {
			break
		}
	}
	if !w.gridKeyboardFocus {
		t.Fatal("Tab did not reach the grid")
	}
	tt.Key(0, ui.KeyEnd)
	if want := videos[len(videos)-1].ID; a.SelectedVideoID != want {
		t.Fatalf("End on the grid chose %q; want %q", a.SelectedVideoID, want)
	}
	if !w.gridKeyboardFocus {
		t.Fatal("the grid lost the keyboard focus")
	}
	for i, j := 0, len(a.Videos)-1; i < j; i, j = i+1, j-1 {
		a.Videos[i], a.Videos[j] = a.Videos[j], a.Videos[i]
	}
	tt.Frame()
	if _, ok := tt.Find("Video 116"); !ok {
		t.Fatalf("the grid did not follow the chosen card to its new place: %q", tt.Texts())
	}
}
