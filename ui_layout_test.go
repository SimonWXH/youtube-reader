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
