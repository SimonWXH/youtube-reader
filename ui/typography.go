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
// Bold call sites in main.go and settings.go are pre-migration and move to
// these roles with their zone.
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
	// ArticleTitle is the reading view's article headline, at the middle
	// body step; ArticleTypeForSize gives the headline of either other step.
	ArticleTitle = TextStyle{Size: 24, LineHeight: 30, Weight: Semibold, Tracking: -0.2}
	// ArticleHeading is a section heading inside a reading view's body, at
	// the middle body step.
	ArticleHeading = TextStyle{Size: 18, LineHeight: 25, Weight: Semibold, Tracking: -0.1}
	// ArticleBody is long-form reading copy, a step larger and looser than
	// Body so the reading column stays comfortable over a long article.
	ArticleBody = TextStyle{Size: 15, LineHeight: 24, Weight: Regular}
	// ArticleQuote is a pull quote in a reading view's body.
	ArticleQuote = TextStyle{Size: 15, LineHeight: 24, Weight: Regular}
	// ArticleCode is a code block in a reading view's body.
	ArticleCode = TextStyle{Family: MonoFont, Size: 13, LineHeight: 20, Weight: Regular}
	// MonospacePath is a file path in the settings window.
	MonospacePath = TextStyle{Family: MonoFont, Size: 11.5, LineHeight: 16, Weight: Regular}
)

// ArticleSizes are the body sizes the reading view offers, smallest first,
// and DefaultArticleSize is the middle one, where a reader starts.
var ArticleSizes = [...]float32{13, 15, 17}

const DefaultArticleSize float32 = 15

// ArticleType is the reading view's whole type scale for one body size: the
// headline, section heading, body, quote, and code roles sized from that
// body, and the vertical rhythm the step sets. One value carries the whole
// page, so a size change never leaves a role or a gap at the old step.
type ArticleType struct {
	// Size is the body's point size, the step the scale is built from.
	Size  float32
	Title TextStyle
	// Heading is a section heading inside the body; a first-level heading
	// takes Title, the article's own size.
	Heading TextStyle
	Body    TextStyle
	Quote   TextStyle
	Code    TextStyle
	// BlockGap is the space between the body's blocks, and HeadingGap the
	// extra lead-in a section heading keeps above it.
	BlockGap   float32
	HeadingGap float32
}

// articleStepSize is the 2 pt step between ArticleSizes rungs; the ladder is
// evenly spaced, so a stored size maps to a rung by rounding.
const articleStepSize float32 = 2

// articleTypes is the type ladder, one rung per ArticleSizes entry. The body
// grows 2 pt a step with 1.6 times its size for a line (21, 24, 27), the
// headline sits at 1.6 times the body, a section heading at 1.2, and code a
// step under the body so code never shouts beside prose. The middle rung is
// the published Article tokens.
var articleTypes = [...]ArticleType{
	{
		Size:       13,
		Title:      TextStyle{Size: 21, LineHeight: 27, Weight: Semibold, Tracking: -0.2},
		Heading:    TextStyle{Size: 16, LineHeight: 22, Weight: Semibold, Tracking: -0.1},
		Body:       TextStyle{Size: 13, LineHeight: 21, Weight: Regular},
		Quote:      TextStyle{Size: 13, LineHeight: 21, Weight: Regular},
		Code:       TextStyle{Family: MonoFont, Size: 11.5, LineHeight: 17, Weight: Regular},
		BlockGap:   Space(3),
		HeadingGap: Space(1.5),
	},
	{
		Size:       15,
		Title:      ArticleTitle,
		Heading:    ArticleHeading,
		Body:       ArticleBody,
		Quote:      ArticleQuote,
		Code:       ArticleCode,
		BlockGap:   Space(4),
		HeadingGap: Space(2),
	},
	{
		Size:       17,
		Title:      TextStyle{Size: 27, LineHeight: 34, Weight: Semibold, Tracking: -0.2},
		Heading:    TextStyle{Size: 20, LineHeight: 28, Weight: Semibold, Tracking: -0.1},
		Body:       TextStyle{Size: 17, LineHeight: 27, Weight: Regular},
		Quote:      TextStyle{Size: 17, LineHeight: 27, Weight: Regular},
		Code:       TextStyle{Family: MonoFont, Size: 15, LineHeight: 23, Weight: Regular},
		BlockGap:   Space(5),
		HeadingGap: Space(2.5),
	},
}

// ArticleTypeForSize returns the scale of the offered body size nearest size.
// A size that names no rung, as a first launch's zero does, takes the middle
// one, so the reading view always has a whole scale to draw with.
func ArticleTypeForSize(size float32) ArticleType {
	return articleTypes[articleStep(size)]
}

// NormalizeArticleSize turns a stored body size into the offered rung nearest
// it, for the settings file and the size control to agree on what is saved.
func NormalizeArticleSize(size float32) float32 {
	return articleTypes[articleStep(size)].Size
}

// ArticleStep returns the place, from the smallest, of the offered size nearest
// size, so a control can tell which step its scale is on.
func ArticleStep(size float32) int {
	return articleStep(size)
}

// articleStep is the rung nearest size. A tie goes to the larger rung, and a
// size that names nothing, zero or below, takes the middle rung.
func articleStep(size float32) int {
	if size <= 0 {
		return len(articleTypes) / 2
	}
	step := int((size-articleTypes[0].Size)/articleStepSize + 0.5)
	if step < 0 {
		return 0
	}
	if step >= len(articleTypes) {
		return len(articleTypes) - 1
	}
	return step
}
