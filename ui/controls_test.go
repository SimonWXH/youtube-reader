package ui

import (
	"image/color"
	"testing"

	mui "github.com/egoist/mygo/ui"
)

// foldRowWindow is the window the fold row tests build a row in: wide enough
// for the chevron and a field, and tall enough to see the row's margins.
const (
	foldRowWindowW = 240
	foldRowWindowH = 80
)

// foldRowTester builds one fold row alone in a window and returns the tester
// and the row's box, which the chevron spans because it takes the row height.
func foldRowTester(t *testing.T, tokens Tokens, row SidebarRow) (*mui.Tester, mui.Rect) {
	t.Helper()
	tt := mui.NewTester(func(c *mui.Context) {
		mui.Column(c).Fill().Padding(Space(2)).Children(func() {
			row.Build(c, tokens)
		})
	}, foldRowWindowW, foldRowWindowH)
	tt.Frame()
	chevron, ok := tt.AccessByRole(mui.RoleDisclosure, "Toggle "+row.Label)
	if !ok {
		t.Fatalf("fold row %q has no chevron", row.Label)
	}
	return tt, chevron.Bounds
}

// foldRowSurfacePixel reads the row's own surface in the strip of container
// padding left of the chevron, out of the last frame.
func foldRowSurfacePixel(t *testing.T, tokens Tokens, row SidebarRow) color.RGBA {
	t.Helper()
	tt, box := foldRowTester(t, tokens, row)
	return tt.Image().RGBAAt(int(box.X-4), int(box.Y+4))
}

// TestSidebarRenameFieldIsCentredAndWhole pins the rename treatment the
// captain asked for: the field is SidebarRenameFieldHeight tall, the row
// centres it vertically, and the 13 pt line box - descenders included - fits
// inside the field's border instead of hanging below it.
func TestSidebarRenameFieldIsCentredAndWhole(t *testing.T) {
	for _, dark := range []bool{false, true} {
		tokens := NewTokens(nil, dark)
		value := "GameDev"
		tt, rowBox := foldRowTester(t, tokens, SidebarRow{
			Key: "group:g1", Label: value, Count: "3",
			Fold: true, Expanded: true, Selected: true, Renaming: true,
			RenameValue: &value,
		})

		field, ok := tt.AccessByRole(mui.RoleTextField, "Rename "+value)
		if !ok {
			t.Fatalf("dark=%v: rename field not found", dark)
		}
		if field.Bounds.H != SidebarRenameFieldHeight {
			t.Fatalf("dark=%v: rename field height = %v, want %v", dark, field.Bounds.H, SidebarRenameFieldHeight)
		}

		fieldMid := field.Bounds.Y + field.Bounds.H/2
		rowMid := rowBox.Y + rowBox.H/2
		if d := fieldMid - rowMid; d > 0.01 || d < -0.01 {
			t.Fatalf("dark=%v: field centre %v, row centre %v: the row must centre the field", dark, fieldMid, rowMid)
		}

		caret, ok := tt.TextCaret()
		if !ok {
			t.Fatalf("dark=%v: rename field has no caret, so it is not focused", dark)
		}
		top, bottom := field.Bounds.Y+Hairline, field.Bounds.Y+field.Bounds.H-Hairline
		if caret.Y < top-0.01 || caret.Y+caret.H > bottom+0.01 {
			t.Fatalf("dark=%v: 13 pt line box %v..%v escapes the field's border %v..%v: descenders are clipped",
				dark, caret.Y, caret.Y+caret.H, top, bottom)
		}

		// The outline of a focused field, in the app's focus token rather than
		// in the theme accent MyGo would otherwise paint it with.
		border := tt.Image().RGBAAt(int(field.Bounds.X+field.Bounds.W/2), int(field.Bounds.Y))
		if want := tokens.Focus; border.R != want.R || border.G != want.G || border.B != want.B {
			t.Fatalf("dark=%v: field border = %v, want the focus token %v", dark, border, want)
		}
	}
}

// TestSidebarRenameRowDropsTheSelectedSurface checks the other half of the
// rename treatment: while the field is open the row hands its surface back to
// the sidebar, so the field's own border is the only control the eye reads.
// The paint check compares the row against an idle row of the same shape, and
// against a selected row to prove a tint is visible when there is one.
func TestSidebarRenameRowDropsTheSelectedSurface(t *testing.T) {
	value := "GameDev"
	for _, dark := range []bool{false, true} {
		tokens := NewTokens(nil, dark)

		if got := (SidebarRow{Fold: true}).foldRowSurface(tokens); got != tokens.Sidebar {
			t.Fatalf("dark=%v: idle row surface = %v, want Sidebar %v", dark, got, tokens.Sidebar)
		}
		if got := (SidebarRow{Fold: true, Selected: true}).foldRowSurface(tokens); got != tokens.AccentSoft {
			t.Fatalf("dark=%v: selected row surface = %v, want AccentSoft %v", dark, got, tokens.AccentSoft)
		}
		if got := (SidebarRow{Fold: true, Selected: true, Renaming: true}).foldRowSurface(tokens); got != tokens.Sidebar {
			t.Fatalf("dark=%v: renaming row surface = %v, want Sidebar %v", dark, got, tokens.Sidebar)
		}

		idle := foldRowSurfacePixel(t, tokens, SidebarRow{Key: "group:idle", Label: "Idle", Fold: true})
		selected := foldRowSurfacePixel(t, tokens, SidebarRow{Key: "group:selected", Label: "Selected", Fold: true, Selected: true})
		renaming := foldRowSurfacePixel(t, tokens, SidebarRow{
			Key: "group:renaming", Label: value, Fold: true, Selected: true, Renaming: true,
			RenameValue: &value,
		})

		if selected == idle {
			t.Fatalf("dark=%v: selected row paints %v, the same as an idle row: the tint this test looks for is not there", dark, selected)
		}
		if renaming != idle {
			t.Fatalf("dark=%v: renaming row paints %v, want the idle surface %v", dark, renaming, idle)
		}
	}
}

// TestSidebarRowMetrics pins the board's sidebar geometry and the type roles
// the rows consume, so a later token edit cannot quietly change the row
// height, the count figures, or the section header.
func TestSidebarRowMetrics(t *testing.T) {
	if SidebarRowHeight != 28 {
		t.Fatalf("SidebarRowHeight = %v, want 28", SidebarRowHeight)
	}
	if SidebarRenameFieldHeight != 26 {
		t.Fatalf("SidebarRenameFieldHeight = %v, want 26", SidebarRenameFieldHeight)
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
