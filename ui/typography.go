package ui

import "github.com/egoist/mygo/ui"

// SFFont is the app's default family: the system font, which is SF Pro on
// macOS with the matching CJK face for Han text. No custom font is loaded.
const SFFont = "system-ui"

// MonoFont is the system monospace family, used only for file paths.
const MonoFont = "monospace"

// Weight names the three weights the type roles use. MyGo accepts 100-900, but
// new surfaces stay within Regular, Medium, and Semibold: hierarchy comes from
// a step in size and a step in weight, never from Light or Thin. The existing
// Bold call sites in main.go, toolbar.go, and settings.go are pre-migration
// and move to these roles with their zone.
type Weight int

// Weights.
const (
	Regular  Weight = 400
	Medium   Weight = 500
	Semibold Weight = 600
)

// TextStyle is one named role of the type scale. Family, size, weight, line
// height, tracking, and tabular figures travel together so a surface never
// sets them alone. An empty Family means SFFont. Colour stays a Tokens role.
// No size is below 11 pt; 10 pt is the absolute floor for any future role.
type TextStyle struct {
	Family     string
	Size       float32
	LineHeight float32
	Weight     Weight
	Tracking   float32 // DIPs between characters, as MyGo's LetterSpacing reads it
	Tabular    bool
}

// Apply sets the role's typography on el.
func (s TextStyle) Apply(el ui.Element) ui.Element {
	family := s.Family
	if family == "" {
		family = SFFont
	}
	el = el.Font(family).FontSize(s.Size).FixedLineHeight(s.LineHeight).FontWeight(int(s.Weight)).
		LetterSpacing(s.Tracking)
	if s.Tabular {
		return el.FontFeatures("tnum")
	}
	return el.FontFeatures()
}

// The type scale, the agreed target for every zone; each role's size and line
// height are the design board's values. The board's twelve rows map onto
// these roles: ToolbarTitle, DetailTitle, CardTitle, SectionHeader, Body and
// Control, Secondary, Metadata, Helper, SidebarGroupLabel, EmptyTitle and
// EmptyBody, and MonospacePath. PopoverTitle and PopoverBody are spec-derived
// additions alongside those roles: spec section 6 names a popover title at 13
// Semibold and a popover body at 12 Regular. Call sites that still draw other
// sizes or weights are pre-migration exceptions and do not change the roles.
var (
	// Body is the default reading size for detail descriptions and prose.
	Body = TextStyle{Size: 13, LineHeight: 18, Weight: Regular}
	// Control is button, text field, select, check box, and sidebar row labels.
	Control = TextStyle{Size: 13, LineHeight: 18, Weight: Regular}
	// Secondary is supporting copy such as a refresh stamp or subheading.
	Secondary = TextStyle{Size: 12, LineHeight: 16, Weight: Regular}
	// Metadata is third-level metadata and counts, with tabular figures.
	Metadata = TextStyle{Size: 11, LineHeight: 14, Weight: Medium, Tracking: 0.11, Tabular: true}
	// SectionHeader titles a group inside a panel or the sidebar.
	SectionHeader = TextStyle{Size: 13, LineHeight: 18, Weight: Semibold}
	// DetailTitle is the selected video's title in the detail pane.
	DetailTitle = TextStyle{Size: 15, LineHeight: 20, Weight: Semibold, Tracking: -0.15}
	// ToolbarTitle is the title in the window's toolbar.
	ToolbarTitle = TextStyle{Size: 13, LineHeight: 18, Weight: Semibold}
	// PopoverTitle is the status popover's heading.
	PopoverTitle = TextStyle{Size: 13, LineHeight: 18, Weight: Semibold}
	// PopoverBody is body copy inside the status popover.
	PopoverBody = TextStyle{Size: 12, LineHeight: 16, Weight: Regular}
	// CardTitle is a grid card's two-line title.
	CardTitle = TextStyle{Size: 13, LineHeight: 18, Weight: Semibold}
	// Helper is validation and explanatory copy under a control.
	Helper = TextStyle{Size: 11, LineHeight: 14, Weight: Regular}
	// SidebarGroupLabel names a collapsible sidebar group; never upper-cased
	// and never letter-spaced.
	SidebarGroupLabel = TextStyle{Size: 11, LineHeight: 14, Weight: Semibold}
	// EmptyTitle is an empty state's headline; pair it with EmptyBody.
	EmptyTitle = TextStyle{Size: 15, LineHeight: 20, Weight: Semibold}
	// EmptyBody is an empty state's explanation.
	EmptyBody = TextStyle{Size: 13, LineHeight: 18, Weight: Regular}
	// MonospacePath is a file path in the settings window.
	MonospacePath = TextStyle{Family: MonoFont, Size: 11.5, LineHeight: 16, Weight: Regular}
)
