package main

import (
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
		sidebar, details := fitPaneWidths(400, 500, true, 1280)
		if sidebar != 400 || details != 500 {
			t.Fatalf("fitPaneWidths = %v, %v; want 400, 500", sidebar, details)
		}
	})

	t.Run("clamps to minimum and maximum", func(t *testing.T) {
		sidebar, details := fitPaneWidths(10000, -1, true, 1280)
		if sidebar != maxSidebarWidth || details != defaultDetailsWidth {
			t.Fatalf("fitPaneWidths = %v, %v; want %v, %v", sidebar, details, maxSidebarWidth, defaultDetailsWidth)
		}
	})

	t.Run("shrinks panes to respect total width", func(t *testing.T) {
		sidebar, details := fitPaneWidths(480, 560, true, 960)
		if sidebar+details+minGridWidth > 960-2*splitDividerWidth() {
			t.Fatalf("panes overflow window: sidebar=%v details=%v", sidebar, details)
		}
		if sidebar < minSidebarWidth || details < minDetailsWidth {
			t.Fatalf("panes below minimum: sidebar=%v details=%v", sidebar, details)
		}
	})
}

func TestBoundedSidebarWidth(t *testing.T) {
	if got := boundedSidebarWidth(40, false, 1280); got != minSidebarWidth {
		t.Fatalf("boundedSidebarWidth(40) = %v, want %v", got, minSidebarWidth)
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

func TestGridMinWidthForAvailable(t *testing.T) {
	const available float32 = 974
	const gap float32 = 8
	for _, columns := range []int{2, 3, 5, 8, 20} {
		minWidth := gridMinWidthForAvailable(available, gap, columns)
		computed := max(1, int((available+gap)/(minWidth+gap)))
		if computed > clampInt(columns, minMaxGridColumns, maxMaxGridColumns) {
			t.Fatalf("columns = %v for cap %v", computed, columns)
		}
	}
	if got := gridMinWidthForAvailable(available, gap, 5); got <= 0 {
		t.Fatalf("gridMinWidthForAvailable = %v; want positive", got)
	}
}
