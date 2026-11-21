package main

import (
	"math"
	"testing"
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

// TestGridCardWidthBounds checks the two rules at every pane width: a card is
// never wider than 280 pt unless the user's maximum forces fewer columns, and
// the column count never exceeds that maximum.
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
			if m.Columns == fill && m.Width > gridCardMaxWidth+0.01 {
				t.Fatalf("pane %v cap %d: card width %v exceeds %v", pane, cap, m.Width, gridCardMaxWidth)
			}
		}
	}
}

// TestGridCardFitsFrameworkColumns guards the link between our metrics and
// ui.GridView, which counts its columns from the minimum width it is given:
// the two must agree or the cards and their gap change under the design.
func TestGridCardFitsFrameworkColumns(t *testing.T) {
	const gap float32 = 8
	for pane := float32(320); pane <= 3000; pane += 7 {
		content := pane - 32
		for _, cap := range []int{2, 5, 8} {
			m := gridCardMetrics(content, gap, cap)
			minWidth := m.gridMinWidth(gap)
			framed := max(1, int((content+gap)/(minWidth+gap)))
			if framed != m.Columns {
				t.Fatalf("pane %v cap %d: GridView would draw %d columns, metrics say %d", pane, cap, framed, m.Columns)
			}
			drawn := (content - gap*float32(framed-1)) / float32(framed)
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
