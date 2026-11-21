package ui

import "github.com/egoist/mygo/ui"

// SFFont is the app's only family: the system font, which is SF Pro on macOS
// with the matching CJK face for Han text. No custom font is ever loaded.
const SFFont = "system-ui"

// Weight is one of the three weights the app uses. MyGo accepts 100-900, but
// surfaces stay within Regular, Medium, and Semibold: hierarchy comes from a
// step in size and a step in weight, never from Light or Thin.
type Weight int

// Weights.
const (
	Regular  Weight = 400
	Medium   Weight = 500
	Semibold Weight = 600
)

// TextStyle is one named role of the type scale. Size, weight, line height,
// tracking, and tabular figures travel together so a surface never sets them
// alone. Colour stays a Tokens role. No size is below 11 pt; 10 pt is the
// absolute floor for any future role.
type TextStyle struct {
	Size       float32
	LineHeight float32
	Weight     Weight
	Tracking   float32 // DIPs between characters, as MyGo's LetterSpacing reads it
	Tabular    bool
}

// Apply sets the role's typography on el.
func (s TextStyle) Apply(el ui.Element) ui.Element {
	el = el.Font(SFFont).FontSize(s.Size).FixedLineHeight(s.LineHeight).FontWeight(int(s.Weight)).
		LetterSpacing(s.Tracking)
	if s.Tabular {
		return el.FontFeatures("tnum")
	}
	return el.FontFeatures()
}

// The type scale. Use the role that names the thing you are drawing.
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
)
