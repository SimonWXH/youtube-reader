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
	// ArticleTitle is the reading view's article headline at the middle body
	// step; ArticleTypeForSize gives the headline of either other step. It is
	// the page header's role, never an in-body heading: the headline is its own
	// per-rung role outside the body's heading table, the small and default
	// steps share 21 pt and the large step carries 27 pt.
	ArticleTitle = TextStyle{Size: 21, LineHeight: 27, Weight: Semibold, Tracking: -0.2}
	// ArticleHeading1 is a first-level heading inside a reading view's body, at
	// the middle body step: the second level's size plus 2 pt at the same
	// leading ratio, rounded to the point, and the largest of the body's four
	// heading roles.
	ArticleHeading1 = TextStyle{Size: 22, LineHeight: 31, Weight: Semibold, Tracking: -0.1}
	// ArticleHeading2 is a second-level heading inside a reading view's body,
	// at the middle body step.
	ArticleHeading2 = TextStyle{Size: 20, LineHeight: 28, Weight: Semibold, Tracking: -0.1}
	// ArticleHeading3 is a third-level heading inside a reading view's body,
	// at the middle body step: the level the published scale drew before the
	// section levels split apart.
	ArticleHeading3 = TextStyle{Size: 18, LineHeight: 25, Weight: Semibold, Tracking: -0.1}
	// ArticleHeading4 is a fourth-level heading and every level under it, at
	// the middle body step: one step above the body, never at it.
	ArticleHeading4 = TextStyle{Size: 16, LineHeight: 22, Weight: Semibold, Tracking: -0.1}
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

// ArticleType is the reading view's whole type scale for one body size: the
// headline, the four body heading levels, body, quote, and code roles, and the
// vertical rhythm the step sets. The body's size is the step, and one value
// carries the whole page, so a size change never leaves a role or a gap at the
// old step.
type ArticleType struct {
	Title TextStyle
	// Heading1 through Heading4 are the headings inside the body: first level,
	// second level, third level, and fourth level and below. Heading1 is never
	// the article's headline, which is Title; the headline a feed supplies as
	// content draws with Heading1, and the ArticleHeading tokens pin the sizes.
	Heading1 TextStyle
	Heading2 TextStyle
	Heading3 TextStyle
	Heading4 TextStyle
	Body     TextStyle
	Quote    TextStyle
	Code     TextStyle
	// BlockGap is the space between the body's blocks, and HeadingGap the
	// extra lead-in a section heading keeps above it.
	BlockGap   float32
	HeadingGap float32
}

// HeadingFor returns the heading role a body block at level draws with: the
// body's own first level at 1, its second and third at 2 and 3, and the
// smallest body level for 4 and every deeper one, so no level is left without
// a role. A level outside 1 to 6 is the caller's to clamp; rssBlock folds it
// to level 2 before asking.
func (a ArticleType) HeadingFor(level int) TextStyle {
	switch level {
	case 1:
		return a.Heading1
	case 2:
		return a.Heading2
	case 3:
		return a.Heading3
	default:
		return a.Heading4
	}
}

// articleTypes is the type ladder, one rung per offered body size. The body
// grows 2 pt a step with 1.6 times its size for a line (21, 24, 27), and the
// code block a step under the body so code never shouts beside prose. The
// middle rung is the published Article tokens.
//
// The four body heading levels are a hand-set table rather than ratios of the
// body: the chosen sizes are exact (22/31, 20/28, 18/25, 16/22 at the default
// step), the first level is the second level's size plus 2 pt, and each of the
// default and large steps keeps every level 2 pt under the one above, while the
// small one runs 20, 18, 16, 15, so its bottom gap is 1 pt. Each level's line
// height is the default step's own ratio of its size, rounded, so a wrapped
// heading keeps its leading as the reader enlarges the text. The headline is
// its own per-rung role outside that table: the small and default steps share
// 21 pt and the large step carries 27 pt, so the article's title is never one
// of the body's heading levels.
var articleTypes = [...]ArticleType{
	{
		Title:      TextStyle{Size: 21, LineHeight: 27, Weight: Semibold, Tracking: -0.2},
		Heading1:   TextStyle{Size: 20, LineHeight: 28, Weight: Semibold, Tracking: -0.1},
		Heading2:   TextStyle{Size: 18, LineHeight: 25, Weight: Semibold, Tracking: -0.1},
		Heading3:   TextStyle{Size: 16, LineHeight: 22, Weight: Semibold, Tracking: -0.1},
		Heading4:   TextStyle{Size: 15, LineHeight: 21, Weight: Semibold, Tracking: -0.1},
		Body:       TextStyle{Size: 13, LineHeight: 21, Weight: Regular},
		Quote:      TextStyle{Size: 13, LineHeight: 21, Weight: Regular},
		Code:       TextStyle{Family: MonoFont, Size: 11.5, LineHeight: 17, Weight: Regular},
		BlockGap:   Space(3),
		HeadingGap: Space(1.5),
	},
	{
		Title:      ArticleTitle,
		Heading1:   ArticleHeading1,
		Heading2:   ArticleHeading2,
		Heading3:   ArticleHeading3,
		Heading4:   ArticleHeading4,
		Body:       ArticleBody,
		Quote:      ArticleQuote,
		Code:       ArticleCode,
		BlockGap:   Space(4),
		HeadingGap: Space(2),
	},
	{
		Title:      TextStyle{Size: 27, LineHeight: 34, Weight: Semibold, Tracking: -0.2},
		Heading1:   TextStyle{Size: 24, LineHeight: 34, Weight: Semibold, Tracking: -0.1},
		Heading2:   TextStyle{Size: 22, LineHeight: 31, Weight: Semibold, Tracking: -0.1},
		Heading3:   TextStyle{Size: 20, LineHeight: 28, Weight: Semibold, Tracking: -0.1},
		Heading4:   TextStyle{Size: 18, LineHeight: 25, Weight: Semibold, Tracking: -0.1},
		Body:       TextStyle{Size: 17, LineHeight: 27, Weight: Regular},
		Quote:      TextStyle{Size: 17, LineHeight: 27, Weight: Regular},
		Code:       TextStyle{Family: MonoFont, Size: 15, LineHeight: 23, Weight: Regular},
		BlockGap:   Space(5),
		HeadingGap: Space(2.5),
	},
}

// ArticleSizes are the body sizes the reading view offers, smallest first,
// read from the ladder so a step is defined once, and DefaultArticleSize is
// the middle one, where a reader starts.
var (
	ArticleSizes       = articleSizes()
	DefaultArticleSize = ArticleSizes[len(ArticleSizes)/2]
)

// articleSizes lists the ladder's body sizes, smallest first.
func articleSizes() []float32 {
	sizes := make([]float32, len(articleTypes))
	for i, scale := range articleTypes {
		sizes[i] = scale.Body.Size
	}
	return sizes
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
	return articleTypes[articleStep(size)].Body.Size
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
	step, best := 0, absFloat32(articleTypes[0].Body.Size-size)
	for i := 1; i < len(articleTypes); i++ {
		if diff := absFloat32(articleTypes[i].Body.Size - size); diff <= best {
			step, best = i, diff
		}
	}
	return step
}

// absFloat32 is size's distance from zero, for measuring which rung is nearest.
func absFloat32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
