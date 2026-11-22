package main

import (
	"fmt"
	"image/color"
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
// 1018 pt closed and 678 pt open. The five-column setting divides the width
// left after the padding and scrollbar reserve.
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
	}{
		{"detail closed", designClosedPane, defaultMaxGridColumns, (designClosedPane - 2*padding - gridScrollbarReserve - 4*gap) / 5},
		{"detail open", designOpenPane, defaultMaxGridColumns, (designOpenPane - 2*padding - gridScrollbarReserve - 4*gap) / 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := gridCardMetrics(tc.pane-2*padding-gridScrollbarReserve, gap, tc.columns)
			if got.Columns != tc.columns {
				t.Fatalf("columns = %d; want %d", got.Columns, tc.columns)
			}
			if diff := abs(got.Width - tc.width); diff > 0.5 {
				t.Fatalf("card width = %v; want %v", got.Width, tc.width)
			}
			if want := got.Width*gridThumbAspect + gridCardTextBlock; abs(got.Height-want) > 0.01 {
				t.Fatalf("card height = %v; want width*9/16+%v = %v", got.Height, gridCardTextBlock, want)
			}
			if abs(got.gridWidth(gap)-(tc.pane-2*padding-gridScrollbarReserve)) > 0.01 {
				t.Fatalf("grid width %v does not fill the pane's %v", got.gridWidth(gap), tc.pane-2*padding-gridScrollbarReserve)
			}
		})
	}
}

// TestShippedGridBounds checks the panes the app ships with at the 1280 pt
// default width: the explicit column setting is honoured and the grid fills
// the pane with no trailing margin.
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
		{"detail closed", closedPane, defaultMaxGridColumns},
		{"detail open", openPane, defaultMaxGridColumns},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content := tc.pane - 2*padding - gridScrollbarReserve
			got := gridCardMetrics(content, gap, tc.columns)
			if got.Columns != tc.columns {
				t.Fatalf("columns = %d; want %d", got.Columns, tc.columns)
			}
			if got.Columns < minMaxGridColumns || got.Columns > maxMaxGridColumns {
				t.Fatalf("columns = %d outside %d-%d", got.Columns, minMaxGridColumns, maxMaxGridColumns)
			}
			if abs(got.gridWidth(gap)-content) > 0.01 {
				t.Fatalf("grid width = %v; want the full content %v", got.gridWidth(gap), content)
			}
		})
	}
}

// TestGridCardFillsEveryColumnSetting checks every supported column count at
// realistic grid-pane widths: an explicit column setting always divides the
// available content width (pane less padding and scrollbar reserve) equally,
// with no trailing margin. Widths cover the 320 pt minimum grid pane (960 pt
// window with the detail pane at its fitted maximum) through ultrawide.
func TestGridCardFillsEveryColumnSetting(t *testing.T) {
	const gap float32 = 8
	const padding float32 = 16
	for _, pane := range []float32{320, 480, 678, 960, 1024, 1280, 1440, 1920, 2560, 3840, 5120} {
		content := pane - 2*padding - gridScrollbarReserve
		for columns := minMaxGridColumns; columns <= maxMaxGridColumns; columns++ {
			m := gridCardMetrics(content, gap, columns)
			if m.Columns != columns {
				t.Fatalf("pane %v setting %d: columns = %d", pane, columns, m.Columns)
			}
			if abs(m.gridWidth(gap)-content) > 0.01 {
				t.Fatalf("pane %v setting %d: grid width %v leaves a trailing margin; want %v", pane, columns, m.gridWidth(gap), content)
			}
			if minWidth := m.gridMinWidth(gap); minWidth < m.Width-gap/float32(columns)-0.1 {
				t.Fatalf("pane %v setting %d: framework minimum %v too small for card %v", pane, columns, minWidth, m.Width)
			}
		}
	}
}

// TestGridCardClampsColumnSetting keeps the settings model's 2 to 8 limits
// even when a caller passes a bad value.
func TestGridCardClampsColumnSetting(t *testing.T) {
	const gap float32 = 8
	for _, tc := range []struct{ in, want int }{{-3, minMaxGridColumns}, {0, minMaxGridColumns}, {1, minMaxGridColumns}, {9, maxMaxGridColumns}, {100, maxMaxGridColumns}} {
		m := gridCardMetrics(640, gap, tc.in)
		if m.Columns != tc.want {
			t.Fatalf("columns(%d) = %d, want %d", tc.in, m.Columns, tc.want)
		}
	}
}

// TestGridFillsPaneAtFourColumns renders the grid in a 1920 pt window with
// the reported four-column setting: the row must fill the pane up to the
// scrollbar clearance, with no large trailing margin.
func TestGridFillsPaneAtFourColumns(t *testing.T) {
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
			MaxGridColumns: 4,
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
	// The framework rounds column widths to whole points, so derive the
	// real step from the rendered cards and use it for the row's right edge.
	metrics := gridCardMetrics(1920-defaultSidebarWidth-splitDividerWidth()-2*16-gridScrollbarReserve, 8, 4)
	step := second.X - first.X
	if abs(step-(metrics.Width+8)) > 3 {
		t.Fatalf("adjacent cards are %v pt apart; want %v", step, metrics.Width+8)
	}
	if want := defaultSidebarWidth + splitDividerWidth() + 16 + 8; abs(first.X-want) > 1.5 {
		t.Fatalf("the first card starts at %v; want the grid's 16 pt padding at %v", first.X, want)
	}
	fourth, ok := tt.Find("Video 3")
	if !ok {
		t.Fatalf("the grid does not show its fourth card: %q", tt.Texts())
	}
	if span := fourth.X - first.X; abs(span-3*step) > 3 {
		t.Fatalf("the fourth card starts %v pt after the first; want %v", span, 3*step)
	}
	if fifth, ok := tt.Find("Video 4"); ok && fifth.Y-first.Y < 100 {
		t.Fatalf("a fifth card shares the first row at y=%v; the row starts at %v", fifth.Y, first.Y)
	}
	// Independent oracle: the label box starts 8 pt inside the card, and
	// the framework rounds each column to a whole point, so the card's
	// trailing edge is fourth.X - 8 + step - gap. The grid reserves the
	// scrollbar's track and clearance after the cards.
	const textInset float32 = 8
	cardRight := fourth.X - textInset + step - 8
	wantRight := float32(1920) - 16 - gridScrollbarReserve
	if abs(cardRight-wantRight) > 8 {
		t.Fatalf("the last card ends at %v; want the scrollbar reserve at %v", cardRight, wantRight)
	}
}

// TestLoadingSkeletonStartsAtGridOrigin renders the first load in a 1920 pt
// window, where the five-column cap leaves a trailing margin: the skeleton's
// cards must start at the grid's leading padding instead of being centred in
// the pane and snapping left when the real cards arrive.
func TestLoadingSkeletonStartsAtGridOrigin(t *testing.T) {
	a := statesTestApp(t)
	a.Videos = nil
	a.Busy = true
	w := &Window{app: a, selectedIndex: new(int)}
	w.cancelRefresh = func() {}
	tt := ui.NewTester(w.view, 1920, 840)
	tt.Frame()

	skeleton, ok := tt.Find(mui.LoadingGridLabel)
	if !ok {
		t.Fatalf("the first load draws no skeleton: %q", tt.Texts())
	}
	if want := defaultSidebarWidth + splitDividerWidth() + 16; abs(skeleton.X-want) > 1.5 {
		t.Fatalf("the skeleton starts at %v; want the grid's 16 pt leading padding at %v", skeleton.X, want)
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

// TestScrollSurfacesReserveBottomInset checks F-05 through the rendered
// window, toolbar included: at the minimum height the three scrolling
// surfaces must lay their last item out short of the window's bottom edge by
// at least the shared inset, so nothing is clipped there. Rendering the whole
// shell, not just the pane row, is what proves the panes end at the window's
// edge rather than 52 pt past it under the toolbar.
func TestScrollSurfacesReserveBottomInset(t *testing.T) {
	const h float32 = 640
	videos := make([]core.Video, 24)
	for i := range videos {
		videos[i] = core.Video{
			ID:           fmt.Sprintf("video-%d", i),
			Title:        fmt.Sprintf("Video %d", i),
			ChannelID:    "ch-a",
			ChannelTitle: "Alpha channel",
			PublishedAt:  time.Now().Add(-time.Duration(i) * time.Hour).UTC().Format(time.RFC3339),
		}
	}
	a := sidebarTestApp(t, true)
	a.Channels = append(a.Channels, core.Channel{ID: "ch-lone", Title: "Lone channel"})
	a.Sync.Channels["ch-lone"] = a.Channels[len(a.Channels)-1]
	a.Videos = videos
	a.SelectedVideoID = videos[0].ID
	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.shell, 960, int(h))
	tt.Frame()
	tt.Advance(300 * time.Millisecond)

	// Scroll every surface to its end with pointer scrolls, then check the
	// last item's visible edge sits above the reserved bottom inset.
	scrollToEnd := func(x, y float32) {
		for range 60 {
			tt.Scroll(x, y, 0, 400)
		}
	}
	check := func(label string, r ui.Rect) {
		t.Helper()
		if r.Y+r.H > h+0.5 {
			t.Fatalf("%s ends at %v, past the %v window bottom", label, r.Y+r.H, h)
		}
		if gap := h - (r.Y + r.H); gap < mui.ScrollBottomInset-1.5 {
			t.Fatalf("%s leaves only %v below it; want >= the %v shared inset", label, gap, mui.ScrollBottomInset)
		}
	}
	// Sidebar: scroll inside its column, then check the last ungrouped row.
	scrollToEnd(120, 300)
	lastSidebar, ok := tt.Find("Lone channel")
	if !ok {
		t.Fatalf("sidebar's last row is missing: %q", tt.Texts())
	}
	check("sidebar last row", lastSidebar)
	// Grid: scroll inside the centre pane, then check the last card's title.
	// At 960 pt with the detail open the grid spans roughly x 221..541.
	scrollToEnd(400, 300)
	lastCard, ok := tt.Find(fmt.Sprintf("Video %d", len(videos)-1))
	if !ok {
		t.Fatalf("grid's last card is missing: %q", tt.Texts())
	}
	check("grid last card", lastCard)
	// Detail: its actions are in a fixed column under the scroll body; the
	// bottom action's box must end short of the window bottom by the inset.
	action, ok := tt.Find("Copy link")
	if !ok {
		t.Fatalf("detail's bottom action is missing: %q", tt.Texts())
	}
	if action.Y+action.H > h+0.5 {
		t.Fatalf("detail bottom action ends at %v, past the %v window bottom", action.Y+action.H, h)
	}
	if gap := h - (action.Y + action.H); gap < mui.ScrollBottomInset-1.5 {
		t.Fatalf("detail leaves only %v below its bottom action; want >= %v", gap, mui.ScrollBottomInset)
	}
}

// TestMinimumHeightLayoutKeepsControlsVisible drives the whole window at the
// 960 x 640 minimum and checks the sidebar's last channel and the detail
// pane's actions remain within the window and clickable.
func TestMinimumHeightLayoutKeepsControlsVisible(t *testing.T) {
	videos := make([]core.Video, 12)
	for i := range videos {
		videos[i] = core.Video{
			ID:           fmt.Sprintf("video-%d", i),
			Title:        fmt.Sprintf("Video %d", i),
			ChannelID:    "ch-a",
			ChannelTitle: "Alpha channel",
			PublishedAt:  time.Now().Add(-time.Duration(i) * time.Hour).UTC().Format(time.RFC3339),
		}
	}
	a := sidebarTestApp(t, true)
	a.Channels = append(a.Channels, core.Channel{ID: "ch-lone", Title: "Lone channel"})
	a.Sync.Channels["ch-lone"] = a.Channels[len(a.Channels)-1]
	a.Videos = videos
	a.SelectedVideoID = videos[0].ID
	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.shell, 960, 640)
	tt.Frame()
	tt.Advance(300 * time.Millisecond) // finish the detail pane's width transition

	for _, name := range []string{"Alpha channel", "Lone channel"} {
		boxes := sidebarRowBoxes(tt, name)
		if len(boxes) == 0 {
			t.Fatalf("minimum height: missing sidebar row %q: %q", name, tt.Texts())
		}
		r := boxes[0]
		if r.Y < 0 || r.Y+r.H > 640 {
			t.Fatalf("minimum height: sidebar row %q at %v is clipped", name, r)
		}
	}
	for _, label := range []string{"Open on YouTube…", "Copy link", "Favorite"} {
		r, ok := tt.Find(label)
		if !ok {
			t.Fatalf("minimum height: missing detail action %q: %q", label, tt.Texts())
		}
		if r.Y < 0 || r.Y+r.H > 640 {
			t.Fatalf("minimum height: detail action %q at %v is clipped", label, r)
		}
	}
	if err := tt.Click("Copy link"); err != nil {
		t.Fatalf("minimum height: the bottom detail action is not clickable: %v", err)
	}
}

// TestGridScrollbarTrailingEdge checks F-06 through the rendered frame: the
// grid's vertical scrollbar sits at the centre pane's trailing edge and does
// not move with the column setting. A test-only scrollbar colour makes the
// otherwise translucent bar measurable in the captured image; the expected
// behaviour comes from the board's requirement that the bar track the pane.
func TestGridScrollbarTrailingEdge(t *testing.T) {
	const windowWidth, windowHeight float32 = 1280, 808
	trackColor := color.RGBA{211, 31, 47, 255}
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
			APIKey: "test", ExpandedGroups: map[string]bool{},
			MaxGridColumns: defaultMaxGridColumns, RefreshMinutes: 60,
		},
		Sync: core.SyncState{
			Favorites: map[string]core.FavoriteRecord{}, ReadStates: map[string]map[string]core.ReadRecord{},
			Groups: map[string]core.ChannelGroup{}, Channels: map[string]core.Channel{"ch-1": {ID: "ch-1", Title: "Channel"}},
		},
		Channels: []core.Channel{{ID: "ch-1", Title: "Channel"}},
		Videos:   videos,
	}
	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(func(c *ui.Context) {
		th := *c.Theme()
		th.ScrollbarStyle = ui.ScrollbarStyle{TrackWidth: 12, ThumbWidth: 6, Track: ui.RGBA(trackColor.R, trackColor.G, trackColor.B, float32(trackColor.A)/255), Thumb: ui.RGBA(trackColor.R, trackColor.G, trackColor.B, float32(trackColor.A)/255)}
		c.SetTheme(&th)
		w.shell(c)
	}, int(windowWidth), int(windowHeight))
	tt.Frame()
	// Scrollbars paint only while the pointer is over the scroll container.
	// Move into the grid's trailing region so its vertical bar shows.
	tt.Move(600, 300)

	// Locate the track's painted column by colour. It sits just to the
	// trailing side of the centre pane, clear of the last card.
	trackXs := func() (minX, maxX int) {
		t.Helper()
		img := tt.Image()
		minX, maxX = -1, -1
		for x := 0; x < int(windowWidth); x++ {
			for y := 200; y < 600; y++ {
				c := img.RGBAAt(x, y)
				if c.R == trackColor.R && c.G == trackColor.G && c.B == trackColor.B {
					if minX < 0 {
						minX = x
					}
					maxX = x
					break
				}
			}
		}
		if minX < 0 {
			t.Fatalf("no scrollbar track painted in the frame")
		}
		return minX, maxX + 1
	}
	trackLeft, trackRightX := trackXs()
	if trackRightX-trackLeft != 12 {
		t.Fatalf("scrollbar track is %d pt wide; want 12", trackRightX-trackLeft)
	}
	// The cards end before the track's leading edge with a real gap. The
	// title label sits 8 pt inside the card, and the framework rounds the
	// column step to whole points, so derive the card's right edge from
	// the rendered step: label.X - 8 + step - gap.
	last, ok := tt.Find(fmt.Sprintf("Video %d", defaultMaxGridColumns-1))
	if !ok {
		t.Fatalf("the last card of the first row is missing: %q", tt.Texts())
	}
	first, ok := tt.Find("Video 0")
	if !ok {
		t.Fatalf("the first card is missing: %q", tt.Texts())
	}
	step := (last.X - first.X) / float32(defaultMaxGridColumns-1)
	cardRight := last.X - 8 + step - 8
	if gap := float32(trackLeft) - cardRight; abs(gap-gridScrollbarGap) > 3 {
		t.Fatalf("last card ends at %v and the bar starts at %d: gap %v; want %v", cardRight, trackLeft, gap, gridScrollbarGap)
	}
	// Changing the column setting or opening the detail pane must not move
	// the bar: it tracks the centre pane, not the cards.
	for _, columns := range []int{minMaxGridColumns, maxMaxGridColumns} {
		a.Settings.MaxGridColumns = columns
		tt.Frame()
		tt.Move(600, 300)
		if left, right := trackXs(); left != trackLeft || right != trackRightX {
			t.Fatalf("%d columns moved the scrollbar to %d..%d; want %d..%d", columns, left, right, trackLeft, trackRightX)
		}
	}
	// The bar follows the centre pane when the detail pane opens: it keeps
	// its width, moves left with the pane, and still leaves the 8 pt clearance
	// from the trailing card at its new edge. The card's right edge comes from
	// the rendered step, as before, but using the open pane's column count.
	a.Settings.MaxGridColumns = defaultMaxGridColumns
	tt.Frame()
	a.SelectedVideoID = "video-0"
	tt.Frame()
	tt.Advance(300 * time.Millisecond)
	// Put the pointer inside the narrower centre pane so its bar paints.
	tt.Move(400, 300)
	openLeft, openRight := trackXs()
	if openRight-openLeft != 12 {
		t.Fatalf("with the detail open the bar is %d pt wide; want 12", openRight-openLeft)
	}
	if want := float32(openRight); want >= float32(trackRightX) {
		t.Fatalf("with the detail open the bar ends at %d; want it left of %d", openRight, trackRightX)
	}
	openFirst, ok := tt.Find("Video 0")
	if !ok {
		t.Fatalf("with the detail open the first card is missing: %q", tt.Texts())
	}
	openLast, ok := tt.Find(fmt.Sprintf("Video %d", defaultMaxGridColumns-1))
	if !ok {
		t.Fatalf("with the detail open the last card of the first row is missing: %q", tt.Texts())
	}
	openStep := (openLast.X - openFirst.X) / float32(defaultMaxGridColumns-1)
	openCardRight := openLast.X - 8 + openStep - 8
	if gap := float32(openLeft) - openCardRight; abs(gap-gridScrollbarGap) > 3 {
		t.Fatalf("with the detail open the last card ends at %v and the bar starts at %d: gap %v; want %v", openCardRight, openLeft, gap, gridScrollbarGap)
	}
}
