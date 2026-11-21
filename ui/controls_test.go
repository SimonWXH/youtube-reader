package ui

import "testing"

// TestSidebarRowMetrics pins the board's sidebar geometry and the type roles
// the rows consume, so a later token edit cannot quietly change the row
// height, the count figures, or the section header.
func TestSidebarRowMetrics(t *testing.T) {
	if SidebarRowHeight != 28 {
		t.Fatalf("SidebarRowHeight = %v, want 28", SidebarRowHeight)
	}
	for _, tc := range []struct {
		name    string
		role    TextStyle
		size    float32
		weight  Weight
		tabular bool
	}{
		{"row label (Control)", Control, 13, Regular, false},
		{"group label (SidebarGroupLabel)", SidebarGroupLabel, 11, Semibold, false},
		{"count (Metadata)", Metadata, 11, Medium, true},
		{"section header (SectionHeader)", SectionHeader, 13, Semibold, false},
	} {
		if tc.role.Size != tc.size || tc.role.Weight != tc.weight || tc.role.Tabular != tc.tabular {
			t.Fatalf("%s = %v/%v tabular=%v, want %v/%v tabular=%v",
				tc.name, tc.role.Size, tc.role.Weight, tc.role.Tabular, tc.size, tc.weight, tc.tabular)
		}
	}
}

// TestSidebarContrastContracts checks every token pair the sidebar draws
// against the 4.5:1 floor on the surface it actually sits on. The light
// Faint token fails on the sidebar, which is why the fold chevron, the
// section headers, the group labels, and the counts all use Muted.
func TestSidebarContrastContracts(t *testing.T) {
	const min = 4.5
	for _, dark := range []bool{false, true} {
		tokens := NewTokens(nil, dark)
		if got := contrastRatio(tokens.Muted, tokens.Sidebar); got < min {
			t.Fatalf("dark=%v: Muted on Sidebar = %.2f:1, want at least %.1f:1", dark, got, min)
		}
		if got := contrastRatio(tokens.Accent, tokens.Sidebar); got < min {
			t.Fatalf("dark=%v: Accent on Sidebar = %.2f:1, want at least %.1f:1", dark, got, min)
		}
		if got := contrastRatio(tokens.Accent, tokens.AccentSoft); got < min {
			t.Fatalf("dark=%v: Accent on AccentSoft = %.2f:1, want at least %.1f:1", dark, got, min)
		}
		if got := contrastRatio(tokens.Text, tokens.Sidebar); got < min {
			t.Fatalf("dark=%v: Text on Sidebar = %.2f:1, want at least %.1f:1", dark, got, min)
		}
		if got := contrastRatio(tokens.Danger, tokens.Sidebar); got < min {
			t.Fatalf("dark=%v: Danger on Sidebar = %.2f:1, want at least %.1f:1", dark, got, min)
		}
	}
	// The sidebar must not fall back to Faint, which only clears the floor on
	// a flat Surface in the light appearance.
	light := NewTokens(nil, false)
	if got := contrastRatio(light.Faint, light.Sidebar); got >= min {
		t.Fatalf("light Faint on Sidebar = %.2f:1 unexpectedly passes; the sidebar must keep using Muted", got)
	}
}

// TestSidebarSelectionIsATint checks the agreed selection treatment: the
// AccentSoft fill with accent text, never a solid accent fill.
func TestSidebarSelectionIsATint(t *testing.T) {
	for _, dark := range []bool{false, true} {
		tokens := NewTokens(nil, dark)
		face := tokens.Face(Active, false, true)
		if face.Background != tokens.AccentSoft {
			t.Fatalf("dark=%v selected background = %+v, want AccentSoft %+v", dark, face.Background, tokens.AccentSoft)
		}
		if face.Text != tokens.Accent {
			t.Fatalf("dark=%v selected text = %+v, want Accent %+v", dark, face.Text, tokens.Accent)
		}
		if face.Background == tokens.Accent {
			t.Fatalf("dark=%v selected row uses a solid accent fill", dark)
		}
	}
}
