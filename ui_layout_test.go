package main

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
	mui "github.com/simon/youtube-reader/ui"
)

func TestFitFilterStrip(t *testing.T) {
	const checkbox, countText float32 = 93, 48
	tests := []struct {
		inner     float32
		channel   float32
		search    float32
		collapsed bool
	}{
		{inner: 600, channel: 160, search: 200},
		{inner: 500, channel: 160, search: 167},
		{inner: 430, channel: 36, search: 124},
		{inner: 380, channel: 36, search: 124},
		{inner: 350, channel: 36, search: 124},
		{inner: 340, channel: 36, search: 124},
		{inner: 332, channel: 36, search: 124, collapsed: true},
		{inner: 320, channel: 36, search: 124, collapsed: true},
		{inner: 300, channel: 36, search: 124, collapsed: true},
		{inner: 288, channel: 36, search: 124, collapsed: true},
		{inner: 284, channel: 36, search: 124, collapsed: true},
	}
	for _, tt := range tests {
		channel, search, gap, collapsed := fitFilterStrip(tt.inner, checkbox, toolbarControl, countText)
		if channel != tt.channel || search != tt.search || collapsed != tt.collapsed {
			t.Fatalf("fitFilterStrip(%v) = %v, %v, collapsed %v; want %v, %v, collapsed %v",
				tt.inner, channel, search, collapsed, tt.channel, tt.search, tt.collapsed)
		}
		if search < searchFieldMin {
			t.Fatalf("fitFilterStrip(%v) gives the search field %v, below its %v pt intrinsic minimum", tt.inner, search, searchFieldMin)
		}
		box := checkbox
		if collapsed {
			box = toolbarControl
		}
		total := channel + search + box + countText + 4*gap
		if total > tt.inner+0.01 {
			t.Fatalf("fitFilterStrip(%v) needs %v with the count still shown", tt.inner, total)
		}
	}
}

func TestFitPaneAtRest(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		sidebar := boundedSidebarWidth(0, 0, 1280)
		if sidebar != defaultSidebarWidth {
			t.Fatalf("sidebar = %v; want %v", sidebar, defaultSidebarWidth)
		}
		if got := detailWidthForRoom(defaultDetailsWidth, 1280-minSidebarWidth-splitDividerWidth()); got != defaultDetailsWidth {
			t.Fatalf("details = %v; want %v", got, defaultDetailsWidth)
		}
	})

	t.Run("keeps the chosen widths", func(t *testing.T) {
		if got := boundedSidebarWidth(300, 0, 1280); got != 300 {
			t.Fatalf("sidebar = %v; want 300", got)
		}
		if got := detailWidthForRoom(460, 1280-minSidebarWidth-splitDividerWidth()); got != 460 {
			t.Fatalf("details = %v; want 460", got)
		}
	})

	t.Run("clamps to minimum and maximum", func(t *testing.T) {
		if got := boundedSidebarWidth(10000, 0, 1280); got != maxSidebarWidth {
			t.Fatalf("sidebar = %v; want %v", got, maxSidebarWidth)
		}
		if got := detailWidthForRoom(600, 1280-minSidebarWidth-splitDividerWidth()); got != maxDetailsWidth {
			t.Fatalf("details = %v; want the %v maximum", got, maxDetailsWidth)
		}
	})

	t.Run("leaves the grid its minimum", func(t *testing.T) {
		// 960 with the widest pane the sidebar squeeze allows: sidebar 220,
		// pane 418, grid 320.
		details := detailWidthForRoom(maxDetailsWidth, 960-minSidebarWidth-splitDividerWidth())
		sidebar := boundedSidebarWidth(maxSidebarWidth, details, 960)
		if sidebar != minSidebarWidth || details != 418 {
			t.Fatalf("panes = %v, %v; want %v, 418", sidebar, details, minSidebarWidth)
		}
		if grid := 960 - sidebar - 2*splitDividerWidth() - details; grid != minGridWidth {
			t.Fatalf("grid = %v; want %v", grid, minGridWidth)
		}
	})

	t.Run("hidden sidebar reserves no room for the folded pane", func(t *testing.T) {
		// A folded sidebar and its divider take no room, so the pane may use
		// what the full row gives it.
		if got := detailWidthForRoom(maxDetailsWidth, 960); got != maxDetailsWidth {
			t.Fatalf("details with the sidebar hidden = %v; want %v", got, maxDetailsWidth)
		}
		if got := detailWidthForRoom(defaultDetailsWidth, 960); got != defaultDetailsWidth {
			t.Fatalf("details with the sidebar hidden = %v; want %v", got, defaultDetailsWidth)
		}
	})
}

func TestBoundedSidebarWidth(t *testing.T) {
	if got := boundedSidebarWidth(0, 0, 1280); got != defaultSidebarWidth {
		t.Fatalf("boundedSidebarWidth(0) = %v, want the %v default", got, defaultSidebarWidth)
	}
	if got := boundedSidebarWidth(40, 0, 1280); got != minSidebarWidth {
		t.Fatalf("boundedSidebarWidth(40) = %v, want %v", got, minSidebarWidth)
	}
	if got := boundedSidebarWidth(919, 0, 960); got != maxSidebarWidth {
		t.Fatalf("boundedSidebarWidth(919) = %v, want %v", got, maxSidebarWidth)
	}
	// The pane on screen, not the open flag, decides the squeeze: the
	// sidebar gives up exactly the room the grid needs beyond its minimum.
	if got := boundedSidebarWidth(maxSidebarWidth, defaultDetailsWidth, 960); got != 298 {
		t.Fatalf("boundedSidebarWidth(480, 340) = %v, want 298", got)
	}
	// Squeezing stops at the sidebar's own minimum; the grid then yields.
	if got := boundedSidebarWidth(maxSidebarWidth, maxDetailsWidth, 960); got != minSidebarWidth {
		t.Fatalf("boundedSidebarWidth(480, 480) = %v, want %v", got, minSidebarWidth)
	}
}

// TestPaneLayoutTracksTheShownWidth walks the open/close animation: the
// sidebar must move by exactly the pane unless the grid or the sidebar is at
// its minimum, so no frame is a hard cut and no frame takes the grid below
// its minimum while the panes still fit.
func TestPaneLayoutTracksTheShownWidth(t *testing.T) {
	const total float32 = 960
	for _, pref := range []float32{minSidebarWidth, defaultSidebarWidth, maxSidebarWidth} {
		for _, want := range []float32{minDetailsWidth, defaultDetailsWidth, maxDetailsWidth} {
			rest := detailWidthForRoom(want, total-minSidebarWidth-splitDividerWidth())
			prev := boundedSidebarWidth(pref, 0, total)
			for shown := float32(0.5); shown <= rest; shown += 0.5 {
				sidebar := boundedSidebarWidth(pref, shown, total)
				if sidebar > prev+0.01 {
					t.Fatalf("pref %v want %v: sidebar grew from %v to %v at shown %v", pref, want, prev, sidebar, shown)
				}
				if prev-sidebar > 0.51 {
					t.Fatalf("pref %v want %v: sidebar jumped from %v to %v at shown %v", pref, want, prev, sidebar, shown)
				}
				if grid := total - sidebar - 2*splitDividerWidth() - shown; grid < minGridWidth-0.01 {
					t.Fatalf("pref %v want %v: grid %v below %v at shown %v", pref, want, grid, minGridWidth, shown)
				}
				prev = sidebar
			}
		}
	}
}

func TestDetailPaneWidthBounds(t *testing.T) {
	if got := defaultDetailsWidth; got != 340 {
		t.Fatalf("defaultDetailsWidth = %v, want the board's 340", got)
	}
	if got := clampDetailsWidth(0); got != minDetailsWidth {
		t.Fatalf("clampDetailsWidth(0) = %v, want %v", got, minDetailsWidth)
	}
	if got := clampDetailsWidth(1000); got != maxDetailsWidth {
		t.Fatalf("clampDetailsWidth(1000) = %v, want %v", got, maxDetailsWidth)
	}
	if got := initialDetailsWidth(0); got != defaultDetailsWidth {
		t.Fatalf("initialDetailsWidth(0) = %v, want %v", got, defaultDetailsWidth)
	}
	if got := initialDetailsWidth(560); got != maxDetailsWidth {
		t.Fatalf("initialDetailsWidth(560) = %v, want %v", got, maxDetailsWidth)
	}
	if got := initialDetailsWidth(200); got != minDetailsWidth {
		t.Fatalf("initialDetailsWidth(200) = %v, want %v", got, minDetailsWidth)
	}
}

func TestDetailDividerKeys(t *testing.T) {
	// The divider sits left of the pane, so Left widens it by the board's
	// 8 pt and Right narrows it by the same step.
	if got, ok := detailsWidthForKey(340, ui.KeyLeft); !ok || got != 348 {
		t.Fatalf("Left = %v, %v; want 348, true", got, ok)
	}
	if got, ok := detailsWidthForKey(340, ui.KeyRight); !ok || got != 332 {
		t.Fatalf("Right = %v, %v; want 332, true", got, ok)
	}
	// A key keeps the width within the 280-480 bounds.
	if got, _ := detailsWidthForKey(minDetailsWidth, ui.KeyRight); got != minDetailsWidth {
		t.Fatalf("Right at the minimum = %v, want %v", got, minDetailsWidth)
	}
	if got, _ := detailsWidthForKey(maxDetailsWidth, ui.KeyLeft); got != maxDetailsWidth {
		t.Fatalf("Left at the maximum = %v, want %v", got, maxDetailsWidth)
	}
	// Home and End follow the divider direction like the arrows: Home widens
	// to the 480 bound and End narrows to the 280 bound from anywhere.
	if got, _ := detailsWidthForKey(360, ui.KeyHome); got != maxDetailsWidth {
		t.Fatalf("Home = %v, want %v", got, maxDetailsWidth)
	}
	if got, _ := detailsWidthForKey(360, ui.KeyEnd); got != minDetailsWidth {
		t.Fatalf("End = %v, want %v", got, minDetailsWidth)
	}
	// The double-click reset is the board's default.
	if got, _ := detailsWidthForKey(defaultDetailsWidth, ui.KeyHome); got != maxDetailsWidth {
		t.Fatalf("Home after reset = %v, want %v", got, maxDetailsWidth)
	}
	if defaultDetailsWidth < minDetailsWidth || defaultDetailsWidth > maxDetailsWidth {
		t.Fatalf("reset width %v is outside %v-%v", defaultDetailsWidth, minDetailsWidth, maxDetailsWidth)
	}
}

func TestDetailWidthForRoom(t *testing.T) {
	// 1280: sidebar 260 + 1 pt divider leaves 1019; the pane may use 480
	// and still leave the grid its 320 minimum.
	if got := detailWidthForRoom(480, 1019); got != 480 {
		t.Fatalf("detailWidthForRoom(480, 1019) = %v, want 480", got)
	}
	// 960 at the 220 pt sidebar minimum leaves 739: at most 418 for the
	// pane before the grid drops below 320.
	if got := detailWidthForRoom(480, 739); got != 418 {
		t.Fatalf("detailWidthForRoom(480, 739) = %v, want 418", got)
	}
	if got := detailWidthForRoom(280, 739); got != minDetailsWidth {
		t.Fatalf("detailWidthForRoom(280, 739) = %v, want %v", got, minDetailsWidth)
	}
	if got := detailWidthForRoom(100, 739); got != minDetailsWidth {
		t.Fatalf("detailWidthForRoom(100, 739) = %v, want %v", got, minDetailsWidth)
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

// TestShippedGridBounds checks the panes the app ships with at the 1280 pt
// default width: the cards they produce must never be wider than 280 pt and
// never outnumber the user's maximum.
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
// geometry (sidebar 260, details 340) and a 1220 pt window with the detail pane
// open. Where the setting's maximum columns binds, the cards stay exactly
// 280 pt wide and the pane's leftover width is trailing margin.
func TestGridCardCapKeepsCeiling(t *testing.T) {
	const gap float32 = 8
	const padding float32 = 16
	const designClosedContent float32 = 1280 - 260 - 2 - 2*padding
	const designOpenContent float32 = 1280 - 260 - 340 - 2 - 2*padding
	detailOpen1220Content := float32(1220) - defaultSidebarWidth - 2*splitDividerWidth() - defaultDetailsWidth - 2*splitDividerWidth() - 2*padding
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
		{"1220 detail open, cap 2", detailOpen1220Content, 2, 2, 280, detailOpen1220Content - 568},
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

// TestCardTitleColor keeps the chosen card's title on the accent its selection
// stroke and halo use, and every unchosen title on the body text colour.
func TestCardTitleColor(t *testing.T) {
	accent, text := ui.Hex("#2f6699"), ui.Hex("#1f2023")
	tokens := mui.Tokens{Text: text, Accent: accent}
	if got := cardTitleColor(tokens, true); got != accent {
		t.Fatalf("chosen card title colour = %v; want the accent %v", got, accent)
	}
	if got := cardTitleColor(tokens, false); got != text {
		t.Fatalf("unchosen card title colour = %v; want the text colour %v", got, text)
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
	// Choosing a card opens the detail pane, which rebuilds the grid under
	// the split; the rebuilt pass leaves no element with the pointer focus.
	// Let the 240 ms width transition finish, then Tab from the top reaches
	// the grid and shows its ring, as a keyboard user arrives.
	tt.Advance(300 * time.Millisecond)
	for range 60 {
		tt.Key(0, ui.KeyTab)
		if w.gridKeyboardFocus {
			break
		}
	}
	if !w.gridKeyboardFocus {
		t.Fatalf("Tab did not reach the grid: %q", tt.Texts())
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
