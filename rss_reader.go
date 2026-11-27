package main

import (
	htmlpkg "html"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/sfsymbols"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/netimage"
	"github.com/simon/youtube-reader/core"
	mui "github.com/simon/youtube-reader/ui"
)

// The reader view's section rail: a 24 pt lane on the reader's right edge
// holding one short mark per level-2 heading, exactly rssTickPitch apart. The
// marks' spacing therefore says nothing about the article's length and their
// count says everything about its structure: the rail marks the sections it
// can keep apart and never spreads them over the lane to fill it. The mark of
// the section being read grows and takes the accent; the mark under the
// pointer grows and darkens and opens that section's preview; the rest stay
// thin and muted. A click on a mark takes the reader to the heading it stands
// for.
const (
	rssTickRailWidth float32 = 24
	rssTickPitch     float32 = 8 // the distance between two neighbouring marks' centres
	rssTickPad       float32 = 6 // room above the first mark and below the last
	rssTickInsetX    float32 = 3 // room between a mark's end and the pane edge
	rssTickInactiveW float32 = 12
	rssTickInactiveH float32 = 2
	rssTickHoverW    float32 = 16
	rssTickHoverH    float32 = 3
	rssTickActiveW   float32 = 18
	rssTickActiveH   float32 = 4
	// The track the marks sit in is as tall as they are plus their padding, then
	// held between a tenth and a quarter of the reading viewport - deliberately
	// the scrolling body's height and not the whole window's, because the rail
	// lives in the reading area and the header above it is not part of the
	// stretch of text the marks describe: the floor keeps a two-section piece
	// from reading as a stray dot, the ceiling keeps a long article's rail from
	// running down the whole lane. Neither end of the clamp moves the pitch:
	// surplus room a short article's floor adds stays blank above and below its
	// marks.
	rssTickTrackMin float32 = 0.10
	rssTickTrackMax float32 = 0.25
	rssReadingProbe float32 = 24 // where the reading line sits below the viewport's top
	// The hover preview is a section's opening in a light bubble left of the
	// marks: its width, the gap it keeps from the widest mark, the room it keeps
	// from the rail's ends, and how many characters of the section's first
	// paragraph it shows.
	rssPreviewWidth   float32 = 230
	rssPreviewGap     float32 = 8
	rssPreviewMargin  float32 = 6
	rssPreviewRunes   int     = 56
	rssPreviewLines   int     = 2
	rssReadingColumn  float32 = 680 // the reading text column's widest
	rssReaderImageMax float32 = 560 // the tallest an inline picture may show
	// rssHeaderFeedMin is the least room the header's metadata row leaves the
	// feed name before giving up the inline layout, so the name and its
	// separator never shrink to a stub: enough for the opening of a short
	// feed title.
	rssHeaderFeedMin float32 = 56
	// The text size control in the reader's header reuses the settings panes'
	// segmented-control metrics, the track and chip filling the header's 28 pt
	// control height, and keeps its own padding for the single-letter segments.
	rssTextSizePadX float32 = 7
)

// rssLayoutState is the RSS workspace's three-column split: the article list
// keeps a stored width, the reader takes what is left, and the sidebar keeps
// its shared SidebarWidth. The drag's overshoot is kept so the pointer does not
// burn past a bound.
type rssLayoutState struct {
	seeded    bool
	listWidth float32
	dragging  bool
	overshoot float32
	// shownWidth is the article list's width as the last frame laid it out:
	// listWidth when the room allows it, narrower when the reader's minimum
	// needs the room. The stored width is never overwritten by a tight frame,
	// so it returns when the room does.
	shownWidth float32
	// readerWidth is the reader pane's width as the last frame laid it out,
	// for the reading column's centring and inline picture sizes.
	readerWidth float32
}

// rssReaderState is the reading view's scroll place and section rail: the
// article it belongs to, the scroll offset, the body bounds the last frame laid
// out, and the section the reading line is in.
type rssReaderState struct {
	articleID string
	scroll    ui.ScrollState
	// blocks are the body blocks' window bounds as the last frame laid them
	// out, top to bottom: the body the sections, their marks, and the reading
	// line are all read from. Empty until the first body frame.
	blocks []ui.Rect
	// viewport is the scrolling body's window bounds as the last frame laid
	// it out.
	viewport ui.Rect
	// rail is the section rail's window bounds as the last frame painted it,
	// which a click reads the marks from before this frame's layout.
	rail ui.Rect
	// active is the current section: the last level-2 heading above the
	// reading line, or the first section when the whole body fits the viewport,
	// and -1 while a scrollable body's line is still in the lead or the body has
	// no sections at all.
	active int
	// hover is the mark under the pointer, -1 for none, and the section it
	// stands for is the one a click reads.
	hover int
	// jump is the section a clicked mark is scrolling to while jumping says a
	// jump is under way: its mark stays grown until the reading line takes
	// over, so the highlight does not sweep back as the body glides.
	jump    int
	jumping bool
	// parsedBlocks caches fallback blocks parsed from ContentHTML/Snippet so
	// frames do not re-parse HTML on hover/scroll when art.Blocks is empty.
	parsedBlocksArticleID string
	parsedBlocksUpdatedAt int64
	parsedBlocks          []core.ArticleBlock
	// viewFeed asks for the article's original, the version the feed delivered,
	// instead of its extracted full text. It belongs to one article: a new
	// selection starts on the full text again.
	viewFeed bool
	// feedViewBlocks caches the blocks parsed from the open article's feed body
	// while the toggle shows them, keyed the way parsedBlocks is.
	feedViewArticleID string
	feedViewUpdatedAt int64
	feedViewBlocks    []core.ArticleBlock
}

// newRSSReaderState starts an article's reader at its top, with no mark under
// the pointer, no jump under way, and no section yet.
func newRSSReaderState(articleID string) rssReaderState {
	return rssReaderState{articleID: articleID, active: -1, hover: -1, jump: -1}
}

// rssTypeState is the reading view's typography: the body size the reader
// draws with, read from local settings once so the header's size control, the
// body, and a relaunch all draw the same page.
type rssTypeState struct {
	seeded bool
	size   float32
}

// readingType is the type scale the reading view draws with: the stored step,
// or the middle rung while nothing has been read yet, so a reader always has
// a whole scale.
func (w *Window) readingType() mui.ArticleType {
	return mui.ArticleTypeForSize(w.rssType.size)
}

// seedRSSReadingType restores the reading view's body size from local
// settings once, so the size control and a relaunch read the same value.
func (w *Window) seedRSSReadingType(pref float32) {
	if w.rssType.seeded {
		return
	}
	w.rssType.seeded = true
	w.rssType.size = mui.NormalizeArticleSize(pref)
}

// setRSSReadingSize switches the reading view to the rung nearest size and
// saves it at once, because the size control has no drag to settle.
func (w *Window) setRSSReadingSize(size float32) {
	size = mui.NormalizeArticleSize(size)
	if w.rssType.size == size {
		return
	}
	w.rssType.size = size
	w.persistRSSReadingSize(size)
}

// persistRSSReadingSize writes the reader's body size to local settings.
func (w *Window) persistRSSReadingSize(size float32) {
	size = mui.NormalizeArticleSize(size)
	_, _ = w.app.updateSettings(func(s *core.Settings) bool {
		if s.RSSReadingFontSize == size {
			return false
		}
		s.RSSReadingFontSize = size
		return true
	})
}

// clampRSSListWidth keeps the article list inside its 260-480 range, with zero
// meaning the 400 default.
func clampRSSListWidth(width float32) float32 {
	if width <= 0 {
		width = defaultRSSListWidth
	}
	return clamp(width, minRSSListWidth, maxRSSListWidth)
}

// initialRSSListWidth turns a stored preference into a usable width.
func initialRSSListWidth(pref float32) float32 {
	return clampRSSListWidth(pref)
}

// rssListWidthForRoom is the list width the layout can show: up to its 480
// maximum, but never so wide that the reader falls below its minimum.
func rssListWidthForRoom(width, rowWidth float32) float32 {
	room := rowWidth - splitDividerWidth() - rssReaderMinWidth
	if room < minRSSListWidth {
		return minRSSListWidth
	}
	return clamp(width, minRSSListWidth, min(maxRSSListWidth, room))
}

// rssSidebarReserve is the room the RSS layout keeps beside the sidebar: the
// article list's minimum and the reader's minimum. A wider stored list gives
// way first, so the widths stay put in settings and return when the window or
// the sidebar gives the room back.
const rssSidebarReserve = minRSSListWidth + rssReaderMinWidth

// boundedRSSSidebarWidth fits the sidebar beside the article list's and the
// reader's minimums, so a drag that widens the side panel yields to the
// columns rather than squeezing either below its own floor.
func boundedRSSSidebarWidth(width, total float32) float32 {
	width = clampSidebarWidth(width)
	limit := maxSidebarWidth
	if total > 0 {
		limit = min(limit, total-2*splitDividerWidth()-rssSidebarReserve)
	}
	limit = max(limit, minSidebarWidth)
	return clamp(width, minSidebarWidth, limit)
}

// seedRSSLayout restores the list width from local settings once, so a
// divider drag and a relaunch read the same value.
func (w *Window) seedRSSLayout(pref float32) {
	if w.rssLayout.seeded {
		return
	}
	w.rssLayout.seeded = true
	w.rssLayout.listWidth = initialRSSListWidth(pref)
}

// setRSSListWidth commits a width from a divider key or a double click, saving
// it at once because no drag is in flight.
func (w *Window) setRSSListWidth(width, rowWidth float32) {
	next := rssListWidthForRoom(width, rowWidth)
	if next == w.rssLayout.listWidth {
		return
	}
	w.rssLayout.listWidth = next
	w.persistRSSListWidth(next)
}

// dragRSSList moves the list/reader divider by dx DIPs from the width on
// screen, keeping the pointer's blocked travel so coming back moves the
// divider only once the pointer is over it again.
func (w *Window) dragRSSList(dx, shown, rowWidth float32) {
	if !w.rssLayout.dragging {
		w.rssLayout.dragging = true
		w.rssLayout.overshoot = 0
	}
	room := rssListWidthForRoom(maxRSSListWidth, rowWidth)
	want := shown + dx + w.rssLayout.overshoot
	next := clamp(want, minRSSListWidth, room)
	w.rssLayout.overshoot = want - next
	if next == shown {
		return
	}
	w.rssLayout.listWidth = next
	w.persistRSSListWidth(next)
}

// persistRSSListWidth writes the article list width to local settings.
func (w *Window) persistRSSListWidth(width float32) {
	width = clampRSSListWidth(width)
	_, _ = w.app.updateSettings(func(s *core.Settings) bool {
		if s.RSSListWidth == width {
			return false
		}
		s.RSSListWidth = width
		return true
	})
}

// rssReaderPadding centres the reading column: the board's 24 pt gutter, or
// the wider gutter a large reader needs to keep the text column at 680 pt.
func rssReaderPadding(available float32) float32 {
	gutter := mui.Space(6)
	if available > rssReadingColumn+2*mui.Space(6) {
		gutter = (available - rssReadingColumn) / 2
	}
	return gutter
}

// rssReadingWidth is the text column's width inside a reader: what the
// gutters leave, capped at 680 pt by the centring.
func rssReadingWidth(readerWidth float32) float32 {
	available := readerWidth - rssTickRailWidth
	return max(available-2*rssReaderPadding(available), 0)
}

// rssImageSize fits a picture into the reading column: never upscaled past
// its own pixels, and never taller than the reader's maximum, keeping its
// aspect ratio.
func rssImageSize(column, imageW, imageH float32) (float32, float32) {
	if column <= 0 || imageW <= 0 || imageH <= 0 {
		return 0, 0
	}
	width := min(column, imageW)
	height := imageH * width / imageW
	if height > rssReaderImageMax {
		height = rssReaderImageMax
		width = imageW * height / imageH
	}
	return width, height
}

// rssSectionHeading reports whether a block opens a section: the reader marks
// an article by its level-2 headings, and every other block - a picture, a
// video, a code block, a quote, a link, a heading of any other level - is a
// block of the page rather than a section of it.
func rssSectionHeading(block core.ArticleBlock) bool {
	return block.Type == core.BlockHeading && block.Level == 2
}

// rssSections returns the body blocks that open a section, in document order:
// the article's structure, and the whole of what the rail marks.
func rssSections(blocks []core.ArticleBlock) []int {
	var sections []int
	for i, block := range blocks {
		if rssSectionHeading(block) {
			sections = append(sections, i)
		}
	}
	return sections
}

// rssSectionBounds returns the window bounds of the body's level-2 headings in
// document order: the sections the rail marks, the reading line names, and a
// click lands on. A block the frame has not laid out keeps its place with an
// empty bound, which the reading line skips.
func rssSectionBounds(blocks []core.ArticleBlock, bounds []ui.Rect) []ui.Rect {
	var sections []ui.Rect
	for i, block := range blocks {
		if rssSectionHeading(block) && i < len(bounds) {
			sections = append(sections, bounds[i])
		}
	}
	return sections
}

// rssSectionProse returns the block that opens a section's prose: the first
// paragraph after its heading and before the next section's heading, or -1 for
// a section that never turns to prose. Deeper headings inside the section are
// stepped over, because the subsection is still part of it.
func rssSectionProse(blocks []core.ArticleBlock, heading int) int {
	for i := heading + 1; i < len(blocks); i++ {
		block := blocks[i]
		if block.Type == core.BlockHeading && block.Level <= 2 {
			return -1
		}
		if block.Type == core.BlockParagraph {
			return i
		}
	}
	return -1
}

// rssTickCapacity is how many marks the lane can show at the fixed pitch: as
// many as fit the track's quarter ceiling, a quarter of the reading viewport -
// deliberately the scrolling body's height, not the whole window's. Fewer
// sections than that are all shown; more merge, which is the only case that
// costs a mark.
func rssTickCapacity(viewportH float32) int {
	room := rssTickTrackMax * viewportH
	if room < rssTickActiveH {
		return 0
	}
	return int((room-rssTickActiveH)/rssTickPitch) + 1
}

// rssTickCount returns how many marks the rail shows for sectionCount level-2
// headings: one per section, or as many as the ceiling can keep 8 pt apart.
func rssTickCount(sectionCount int, viewportH float32) int {
	if sectionCount <= 0 {
		return 0
	}
	capacity := rssTickCapacity(viewportH)
	if capacity <= 0 {
		return 0
	}
	return min(sectionCount, capacity)
}

// rssTickBucket maps a section to the mark that stands for it. With one mark
// per section the section is the mark; with fewer, adjacent sections share one.
func rssTickBucket(section, sectionCount, tickCount int) int {
	if section < 0 || sectionCount <= 0 || tickCount <= 0 {
		return -1
	}
	if section >= sectionCount {
		section = sectionCount - 1
	}
	if tickCount >= sectionCount {
		return section
	}
	bucket := section * tickCount / sectionCount
	if bucket >= tickCount {
		bucket = tickCount - 1
	}
	return bucket
}

// rssTickBlock is rssTickBucket the other way: the first section a mark stands
// for, the heading a click on the mark reads. A mark that gathers several
// sections reads the first of them, the one whose place in the article the
// mark stands at. -1 asks for no mark, or a rail without one.
func rssTickBlock(bucket, sectionCount, tickCount int) int {
	if bucket < 0 || sectionCount <= 0 || tickCount <= 0 {
		return -1
	}
	if bucket >= tickCount {
		bucket = tickCount - 1
	}
	// The mark stands for the sections s whose s*tickCount/sectionCount is its
	// own, the smallest of which is the first that rounds up to it.
	section := (bucket*sectionCount + tickCount - 1) / tickCount
	if section >= sectionCount {
		section = sectionCount - 1
	}
	return section
}

// rssTickExtent is the height the marks themselves take at the fixed pitch:
// from the first mark's centre to the last, plus the tallest mark's height on
// both sides, so a grown mark never reaches past the block. It says nothing
// about the room they are given, which is the track's.
func rssTickExtent(count int) float32 {
	if count <= 0 {
		return 0
	}
	return float32(count-1)*rssTickPitch + rssTickActiveH
}

// rssTickTrackHeight is the height of the track the marks sit in: their extent
// plus the padding above and below, then held between a tenth and a quarter of
// the reading viewport - deliberately the scrolling body's height rather than
// the whole window's. A two-section piece is raised to the floor rather than
// reading as a stray dot; a long one is capped rather than running the lane.
// Neither end changes the pitch, so the surplus the floor adds stays blank.
func rssTickTrackHeight(count int, viewportH float32) float32 {
	if count <= 0 || viewportH <= 0 {
		return 0
	}
	height := rssTickExtent(count) + 2*rssTickPad
	return clamp(height, rssTickTrackMin*viewportH, rssTickTrackMax*viewportH)
}

// rssTickTrack is the track's box in the lane: the lane's width, its own
// height, and its middle on the lane's middle - the lane is as tall as the
// reading viewport the height is measured against, deliberately not the whole
// window - so a short rail sits at the height the reader's eyes rest at. It is
// a measure rather than a surface - the lane paints its marks and nothing
// behind them - so this box only decides where they stand.
func rssTickTrack(rail ui.Rect, count int, viewportH float32) ui.Rect {
	height := rssTickTrackHeight(count, viewportH)
	return ui.Rect{X: rail.X, Y: rail.Y + (rail.H-height)/2, W: rail.W, H: height}
}

// rssTickFirstCentre is the y of the first mark's centre: the track's middle
// keeps the marks' block centred in it, so surplus room is shared above and
// below instead of pushing them apart.
func rssTickFirstCentre(rail ui.Rect, count int, viewportH float32) float32 {
	track := rssTickTrack(rail, count, viewportH)
	return track.Y + (track.H-rssTickExtent(count))/2 + rssTickActiveH/2
}

// rssTickIndexAt is the mark a point in the rail is on: the mark whose half of
// the fixed pitch holds y, inside the mark's column, so a 2 pt mark is still
// easy to hit. The gutter left of the marks, the blank above the first mark and
// below the last, and everything outside the lane answer -1.
func rssTickIndexAt(rail ui.Rect, count int, viewportH, x, y float32) int {
	if count <= 0 || viewportH <= 0 || !rail.Contains(x, y) {
		return -1
	}
	left := rail.X + rail.W - rssTickInsetX - rssTickActiveW
	if x < left {
		return -1
	}
	first := rssTickFirstCentre(rail, count, viewportH)
	half := rssTickPitch / 2
	if y < first-half || y >= first+float32(count-1)*rssTickPitch+half {
		return -1
	}
	return clampInt(int((y-first)/rssTickPitch+0.5), 0, count-1)
}

// rssActiveBlock returns the current section: the level-2 heading whose top
// last passed the reading line, one line below the viewport's top. It is asked
// only about rssSectionBounds, so its bounds are level-2 headings in window
// coordinates ordered top to bottom, and headings the frame has not laid out
// yet (zero height) are skipped. Until a heading reaches the line - the lead
// above the first section - there is no current section, and it returns -1, as
// it does for a body without headings. A body that fits the viewport can never
// move a heading onto the line, so the body's own scroll range adds that case:
// the reader names the first section current, while a body that scrolls keeps
// this -1 until the line reaches the first heading.
func rssActiveBlock(bounds []ui.Rect, viewport ui.Rect) int {
	if len(bounds) == 0 || viewport.H <= 0 {
		return -1
	}
	line := viewport.Y + rssReadingProbe
	active := -1
	for i, b := range bounds {
		if b.H <= 0 {
			continue
		}
		if b.Y > line {
			break
		}
		active = i
	}
	return active
}

// rssTickRects lays the rail's marks inside rail: count centres exactly
// rssTickPitch apart, the block of them centred in the track, each mark as tall
// as its state around its own centre and right-aligned on the pane's edge. The
// pitch never stretches, so a short article's raised track shows its marks
// gathered in the middle with blank room at both ends.
func rssTickRects(rail ui.Rect, count, active, hover int, viewportH float32) []ui.Rect {
	if count <= 0 || rail.W <= 0 || rail.H <= 0 || viewportH <= 0 {
		return nil
	}
	first := rssTickFirstCentre(rail, count, viewportH)
	rects := make([]ui.Rect, count)
	for i := range rects {
		width, height := rssTickInactiveW, rssTickInactiveH
		switch {
		case i == active:
			width, height = rssTickActiveW, rssTickActiveH
		case i == hover:
			width, height = rssTickHoverW, rssTickHoverH
		}
		centre := first + float32(i)*rssTickPitch
		rects[i] = ui.Rect{
			X: rail.X + rail.W - rssTickInsetX - width,
			Y: centre - height/2,
			W: width,
			H: height,
		}
	}
	return rects
}

// rssPreviewHeight is the room the hover preview keeps: its padding and
// border, the section heading's line, and the lines of the paragraph below it.
// A section with no paragraph before the next level-2 heading - or one whose
// paragraph holds no words - gets no body line, so its bubble keeps the heading
// alone rather than two blank lines; a section that opens with a quote, code
// block or list but turns to prose before the next heading still gets its two
// lines. The preview places itself by that room rather than by what its own
// text measures, so its top stays put as the pointer moves from a section whose
// paragraph fills both lines to one whose words fill a single line.
func rssPreviewHeight(lines int) float32 {
	height := 2*mui.Space(2) + 2*mui.Hairline + mui.PopoverTitle.LineHeight
	if lines > 0 {
		height += mui.Space(0.5) + float32(lines)*mui.PopoverBody.LineHeight
	}
	return height
}

// rssPreviewRect places the hover preview in the rail's own coordinates: its
// right edge the gap left of the widest mark, its middle on the hovered mark's
// middle, and its top kept the margin inside the rail's ends where the mark
// sits at either one. The bubble is wider than the lane, so x is negative: it
// hangs over the reading column.
func rssPreviewRect(rail, mark ui.Rect, width, height, gap, margin float32) ui.Rect {
	if rail.H <= 0 || width <= 0 || height <= 0 {
		return ui.Rect{}
	}
	top := mark.Y + mark.H/2 - rail.Y - height/2
	lowest := max(rail.H-margin-height, margin)
	return ui.Rect{
		X: rail.W - rssTickInsetX - rssTickActiveW - gap - width,
		Y: clamp(top, margin, lowest),
		W: width,
		H: height,
	}
}

// rssBlockScrollY is the body offset that puts a block's top on the reading
// line, where a click sends it: the block's own Y in the content, less where
// the reading line sits below the viewport's top, so the line holds exactly the
// heading the click read when the glide lands. Bounds and viewport are the last
// frame's, in window coordinates, with the offset the body was scrolled to
// then; the start of the body is 0 however far above the first block the line
// would fall.
func rssBlockScrollY(bounds []ui.Rect, viewport ui.Rect, scrollY float32, block int) float32 {
	if block < 0 || block >= len(bounds) || viewport.H <= 0 {
		return max(scrollY, 0)
	}
	return max(bounds[block].Y-viewport.Y+scrollY-rssReadingProbe, 0)
}

// rssReaderPane renders Column 3: the article reading view with its header,
// body, and section rail, or the empty state when nothing is selected.
func (w *Window) rssReaderPane(c *ui.Context, t mui.Tokens) {
	var art *core.RSSArticle
	if w.app != nil {
		art = w.app.SelectedRSSArticle()
	}
	if art == nil {
		// A closed article keeps no scroll place or rail: the next selection
		// starts at its top instead of inheriting this article's offset.
		w.rssReader = rssReaderState{}
		w.rssReaderEmpty(c, t)
		return
	}
	if w.rssReader.articleID != art.ID {
		w.rssReader = newRSSReaderState(art.ID)
	}
	// The header's toggle decides which body the reader shows: the extracted
	// full text, or the version the feed itself delivered.
	feedView := w.rssReader.viewFeed && art.IsFullText
	blocks := w.rssReaderBlocks(*art, feedView)

	ui.Row(c.Key("rss-reader-pane")).Fill().Background(t.App).AlignItems(ui.Stretch).Children(func() {
		// The reading page is one selectable scope: its headline, metadata and
		// body words can be dragged and copied like ordinary text, while the
		// controls inside it keep their own text and clicks.
		ui.Column(c).Grow(1).MinWidth(0).MinHeight(0).Clip().Selectable().Children(func() {
			w.rssReaderHeader(c, t, *art)
			ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).MinHeight(0).Children(func() {
					w.rssReaderBody(c, t, *art, blocks, feedView)
				})
				w.rssTickRail(c, t, blocks)
			})
		})
	})
}

// rssReaderEmpty is the reader pane's state before an article is chosen.
func (w *Window) rssReaderEmpty(c *ui.Context, t mui.Tokens) {
	zh := strings.HasPrefix(strings.ToLower(c.Locale()), "zh")
	title := "No Article Selected"
	sentence := "Select an article from the list to read."
	if zh {
		title = "未选择文章"
		sentence = "从列表中选择一篇文章进行阅读。"
	}
	ui.Column(c.Key("rss-reader-empty-pane")).Fill().Background(t.App).Children(func() {
		mui.EmptyPane(c, t, mui.EmptySpec{
			Key: "rss-reader-empty",
			Copy: mui.EmptyStateCopy{
				Symbol:   "doc.text",
				Title:    title,
				Sentence: sentence,
			},
		})
	})
}

// The reading header's action group, as NetNewsWire's: six borderless icon
// controls in one pill on the header's leading edge. Each box is 24 pt with a
// 16 pt glyph, the boxes keep 1.5 pt of optical space between them so the six
// read as one group rather than six buttons, and the pill carries 5 pt of
// vertical and 6 pt of horizontal padding inside its border.
const (
	rssHeaderActionSize  float32 = 24
	rssHeaderActionGap   float32 = 1.5
	rssHeaderActionGlyph float32 = 16
	rssHeaderActions     int     = 6
	rssHeaderActionPadY  float32 = 5
	rssHeaderActionPadX  float32 = 6
)

// rssHeaderActionsLabel names the action group for the accessibility tree.
func rssHeaderActionsLabel(zh bool) string {
	if zh {
		return "文章操作"
	}
	return "Article actions"
}

// rssReaderHeader is the fixed article header: its source and date, its title,
// its author, the text size control, and the action group - the read toggle,
// the star, the original/full-text toggle, share, open in browser, and the
// full-text fetch that reads as Refetch, Fetching… or Retry full text as the
// fetch moves on.
func (w *Window) rssReaderHeader(c *ui.Context, t mui.Tokens, art core.RSSArticle) {
	feedName := art.FeedID
	if w.app != nil {
		if title := w.app.FeedTitle(art.FeedID); title != "" {
			feedName = title
		}
	}

	gutter := rssReaderPadding(w.rssLayout.readerWidth - rssTickRailWidth)
	content := w.rssLayout.readerWidth - rssTickRailWidth - 2*gutter
	now := time.Now()
	if w.app != nil {
		now = w.app.clockNow()
	}
	date := rssReaderDate(art.PublishedAt, now)
	rowGap := mui.Space(2)
	pillWidth := rssHeaderActionsWidth()
	sizeWidth := rssTextSizeControlWidth(c)
	sourceWidth := rssHeaderSourceWidth(c, feedName, date)
	sizeOnFirstRow := pillWidth+2*rowGap+sizeWidth <= content
	sourceOnFirstRow := sourceWidth > 0 && pillWidth+2*rowGap+sourceWidth <= content &&
		(!sizeOnFirstRow || pillWidth+3*rowGap+sourceWidth+sizeWidth <= content)
	actions := func() {
		w.rssHeaderActions(c, t, art)
	}
	source := func() {
		if feedName != "" {
			mui.Metadata.Apply(ui.Text(c, feedName)).TextColor(t.Accent).SingleLine().
				Shrink(1).MinWidth(min(rssHeaderFeedMin, rssTextWidth(c, mui.Metadata, feedName)))
		}
		if date != "" {
			if feedName != "" {
				mui.Metadata.Apply(ui.Text(c, "·")).TextColor(t.Faint).Shrink(0)
			}
			mui.Metadata.Apply(ui.Text(c, date)).TextColor(t.Muted).SingleLine().Shrink(0)
		}
	}

	head := ui.Column(c.Key("rss-reader-header")).Shrink(0).FillWidth().
		Padding(mui.Space(3), rssTickRailWidth+gutter, mui.Space(3), gutter).Gap(mui.Space(1)).
		Role(ui.RoleGroup).Label("Article header")
	head.Draw(func(p *ui.Painter, r ui.Rect) {
		p.Fill(ui.Rect{X: r.X, Y: r.Y + r.H - mui.Hairline, W: r.W, H: mui.Hairline}, t.BorderSoft, 0)
	})
	head.Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(rowGap).Children(func() {
			actions()
			if sourceOnFirstRow {
				source()
			}
			ui.Spacer(c)
			if sizeOnFirstRow {
				w.rssTextSizeControl(c, t)
			}
		})
		if !sizeOnFirstRow {
			// A reader too narrow for the pill and the size control together
			// keeps the pill leading the first row and gives the size control
			// its own trailing-aligned row beneath it.
			ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
				w.rssTextSizeControl(c, t)
			})
		}
		if !sourceOnFirstRow {
			// A narrow reader gives the source its own row, so the feed name
			// keeps its room and nothing leaves the pane.
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(rowGap).Children(source)
		}
		w.readingType().Title.Apply(ui.Text(c, art.Title)).TextColor(t.Text).MaxLines(3).Wrap()
		if author := strings.TrimSpace(art.Author); author != "" {
			mui.Secondary.Apply(ui.Text(c, author)).TextColor(t.Muted).SingleLine()
		}
	})
}

// rssTextSizeTitle labels the size control as a whole, for the pointer and the
// accessibility tree.
func rssTextSizeTitle(zh bool) string {
	if zh {
		return "字号"
	}
	return "Text Size"
}

// rssTextSizeNames name the three body sizes, smallest first, as the size
// control offers them to the pointer.
var rssTextSizeNames = [...]struct{ en, zh string }{
	{"Small Text", "小号文字"},
	{"Default Text", "默认字号"},
	{"Large Text", "大号文字"},
}

// rssTextSizeStepLabel names one step of the size control: its place on the
// ladder and the size it sets.
func rssTextSizeStepLabel(index int, size float32, zh bool) string {
	name := rssTextSizeNames[0].en
	if index >= 0 && index < len(rssTextSizeNames) {
		name = rssTextSizeNames[index].en
		if zh {
			name = rssTextSizeNames[index].zh
		}
	}
	return name + " · " + strconv.Itoa(int(size)) + " pt"
}

// rssTextSizeControl is the reader's body size control: one segment per
// offered size, each carrying its own A so the ladder reads as a growing
// letter, and the chosen step raised on the track's surface as the settings
// panes' control is. Changing the step re-draws the whole page at the new
// scale in the same frame and saves the choice.
func (w *Window) rssTextSizeControl(c *ui.Context, t mui.Tokens) {
	zh := strings.HasPrefix(strings.ToLower(c.Locale()), "zh")
	selected := mui.ArticleStep(w.readingType().Body.Size)
	parts := ui.SegmentedBase(c.Key("rss-text-size"), &selected, len(mui.ArticleSizes))
	track := parts.Track.Height(settingsControlH).Shrink(0).Padding(settingsSegmentInset).Gap(settingsSegmentGap).
		Radius(mui.RadiusControl).Background(t.SurfaceAlt).Border(mui.Hairline, t.Border).
		AlignItems(ui.Center).Label(rssTextSizeTitle(zh))
	track.Children(func() {
		for i, size := range mui.ArticleSizes {
			label := rssTextSizeStepLabel(i, size, zh)
			segment := parts.Segment(i).Height(settingsSegmentH).Padding(0, rssTextSizePadX).
				Radius(settingsSegmentRadius).Center().FocusRingColor(t.Focus).FocusRingOffset(mui.FocusRingGap).
				Label(label).Tooltip(label)
			glyph := rssTextSizeGlyph(size)
			if i == selected {
				segment.Background(t.Surface).Border(mui.Hairline, t.BorderSoft)
				segment.Children(func() {
					glyph.Apply(ui.Text(c, "A")).TextColor(t.Text).SingleLine()
				})
				continue
			}
			segment.Background(t.SurfaceAlt)
			segment.Children(func() {
				glyph.Apply(ui.Text(c, "A")).TextColor(t.Muted).SingleLine()
			})
		}
	})
	if track.Changed() {
		w.setRSSReadingSize(mui.ArticleSizes[selected])
	}
}

// rssHeaderSourceWidth is the width the feed name, its separator and the day
// stamp need, including the gaps between them.
func rssHeaderSourceWidth(c *ui.Context, feedName, date string) float32 {
	width := float32(0)
	children := 0
	if feedName != "" {
		width += min(rssHeaderFeedMin, rssTextWidth(c, mui.Metadata, feedName))
		children++
	}
	if date != "" {
		width += rssTextWidth(c, mui.Metadata, date)
		children++
		if feedName != "" {
			width += rssTextWidth(c, mui.Metadata, "·")
			children++
		}
	}
	if children > 1 {
		width += float32(children-1) * mui.Space(2)
	}
	return width
}

// rssHeaderActionsWidth is the action pill's own width: its border and
// horizontal padding around six 24 pt boxes and the 1.5 pt optical space
// between them.
func rssHeaderActionsWidth() float32 {
	boxes := float32(rssHeaderActions)*rssHeaderActionSize + float32(rssHeaderActions-1)*rssHeaderActionGap
	return 2*mui.Hairline + 2*rssHeaderActionPadX + boxes
}

// rssTextWidth measures one run in the given role, the width it draws at.
func rssTextWidth(c *ui.Context, style mui.TextStyle, text string) float32 {
	span := ui.Span{Text: text, Font: style.Family, Size: style.Size, Weight: int(style.Weight), LetterSpacing: style.Tracking}
	if style.Tabular {
		span.Features = "tnum"
	}
	width, _ := c.MeasureText(0, span)
	return width
}

// rssTextSizeGlyph is the A one size segment draws, its line box the chip's
// inner height so the letter sits inside the segment's border.
func rssTextSizeGlyph(size float32) mui.TextStyle {
	return mui.TextStyle{Size: size, LineHeight: settingsSegmentH - 2*mui.Hairline, Weight: mui.Medium}
}

// rssTextSizeControlWidth is the size control's width: the track's border,
// inset, and gaps, and one padded segment per offered size drawing its own A,
// with the chosen segment's border on top.
func rssTextSizeControlWidth(c *ui.Context) float32 {
	width := 2*mui.Hairline + 2*settingsSegmentInset + float32(len(mui.ArticleSizes)-1)*settingsSegmentGap + 2*mui.Hairline
	for _, size := range mui.ArticleSizes {
		width += rssTextWidth(c, rssTextSizeGlyph(size), "A") + 2*rssTextSizePadX
	}
	return width
}

// rssHeaderActionSpec is one control of the reader header's action group: a
// 24 pt box drawing a glyph, a spinner while it works, and a dimmed glyph with
// its tooltip while it cannot act.
type rssHeaderActionSpec struct {
	key      string
	label    string   // the control's name, for the pointer, the tooltip and the accessibility tree
	glyph    string   // SF Symbol name; a busy control draws a spinner instead
	color    ui.Color // the glyph's colour; the zero colour asks for the muted face
	busy     bool     // work is in flight: the control shows a spinner and takes no input
	disabled bool     // the control cannot act right now
	on       bool     // the control's own menu is showing: drawn on the accent tint
	tooltip  string   // the pointer's words when they differ from the label
	onClick  func()
}

// rssHeaderAction is one borderless icon control of the reader header's action
// group: a 24 pt box with a 16 pt glyph, the quiet accent face under the
// pointer, and the shared focus ring. The label is what the pointer's tooltip
// and the accessibility tree read. A disabled control takes no pointer tooltip
// in mygo, so the reason it cannot act is carried as its accessible
// description, matching ui.PaneAction.
func (w *Window) rssHeaderAction(c *ui.Context, t mui.Tokens, spec rssHeaderActionSpec) ui.Element {
	color := spec.color
	switch {
	case spec.busy || spec.disabled:
		color = t.Faint
	case spec.on:
		color = t.Accent
	case color == (ui.Color{}):
		color = t.Muted
	}
	idle := !spec.busy && !spec.disabled
	tooltip := spec.tooltip
	if tooltip == "" {
		tooltip = spec.label
	}
	var face ui.Color
	if spec.on {
		face = t.AccentSoft
	}
	el := ui.ButtonBase(c).Key(spec.key).Size(rssHeaderActionSize, rssHeaderActionSize).
		Padding(0).Radius(mui.RadiusControl).Background(face).Cursor(ui.CursorPointer).Transition(mui.Fade).
		FocusRingColor(t.Focus).FocusRingOffset(mui.FocusRingGap).
		Role(ui.RoleButton).Label(spec.label).Tooltip(tooltip)
	if !idle {
		el = el.Cursor(ui.CursorDefault).Disabled(true)
	}
	el.Children(func() {
		if spec.busy {
			ui.Spinner(c).Size(mui.Space(4), mui.Space(4)).TextColor(t.Muted).Label(spec.label)
			return
		}
		sfsymbols.Symbol(c, spec.glyph).Size(rssHeaderActionGlyph).Color(color).Element.Role(ui.RoleNone)
	})
	if idle {
		el.Draw(rssActionFace(el, t))
		if spec.onClick != nil {
			el.OnClick(spec.onClick)
		}
	}
	return el
}

// rssHeaderActions is the reading header's action group, in NetNewsWire's
// order: the read toggle, the star, the original/full-text toggle, share, open
// in browser, and the full-text fetch that doubles as the refresh.
func (w *Window) rssHeaderActions(c *ui.Context, t mui.Tokens, art core.RSSArticle) {
	zh := strings.HasPrefix(strings.ToLower(c.Locale()), "zh")
	ui.Row(c).Key("rss-reader-actions").Shrink(0).Gap(rssHeaderActionGap).
		Padding(rssHeaderActionPadY, rssHeaderActionPadX).
		Background(t.SurfaceAlt).Border(mui.Hairline, t.BorderSoft).Radius(mui.RadiusNotice).
		AlignItems(ui.Center).Role(ui.RoleGroup).Label(rssHeaderActionsLabel(zh)).Children(func() {
		w.rssReadAction(c, t, art)
		w.rssStarAction(c, t, art)
		w.rssBodyToggleAction(c, t, art)
		w.rssShareAction(c, t, art)
		w.rssOpenAction(c, t, art)
		w.rssRefreshAction(c, t, art)
	})
}

// rssActionFace is the hover and press fill the reader header's actions share:
// the accent's quiet face, drawn a hairline past the control's own bounds so it
// sits inside the border rather than over it.
func rssActionFace(el ui.Element, t mui.Tokens) func(p *ui.Painter, r ui.Rect) {
	return func(p *ui.Painter, r ui.Rect) {
		grown := ui.Rect{X: r.X - mui.Hairline, Y: r.Y - mui.Hairline, W: r.W + 2*mui.Hairline, H: r.H + 2*mui.Hairline}
		switch {
		case el.Pressed():
			p.Fill(grown, t.Face(mui.Active, false, false).Background, mui.RadiusControl)
		case el.Hovered():
			p.Fill(grown, t.Face(mui.Hover, false, false).Background, mui.RadiusControl)
		}
	}
}

// rssReadAction toggles the open article's read state. A filled circle marks an
// article already read and a hollow one an unread article, the pair
// NetNewsWire's list uses, so the control shows the state and changes it.
func (w *Window) rssReadAction(c *ui.Context, t mui.Tokens, art core.RSSArticle) ui.Element {
	zh := strings.HasPrefix(strings.ToLower(c.Locale()), "zh")
	label, glyph := "Mark as Read (M)", "circle"
	if zh {
		label = "标记为已读 (M)"
	}
	color := t.Muted
	if art.Read {
		label, glyph, color = "Mark as Unread (M)", "largecircle.fill.circle", t.Accent
		if zh {
			label = "标记为未读 (M)"
		}
	}
	return w.rssHeaderAction(c, t, rssHeaderActionSpec{
		key: "rss-reader-read", label: label, glyph: glyph, color: color,
		onClick: func() { w.toggleRSSArticleRead(c, art) },
	})
}

// rssStarAction is the header's star toggle: it fills and takes the accent while
// the article is starred, so the state and the way to change it share one box.
func (w *Window) rssStarAction(c *ui.Context, t mui.Tokens, art core.RSSArticle) ui.Element {
	zh := strings.HasPrefix(strings.ToLower(c.Locale()), "zh")
	label := "Star article (S)"
	if art.Starred {
		label = "Unstar article (S)"
	}
	if zh {
		label = "标星 (S)"
		if art.Starred {
			label = "取消标星 (S)"
		}
	}
	glyph, color := "star", t.Muted
	if art.Starred {
		glyph, color = "star.fill", t.Accent
	}
	return w.rssHeaderAction(c, t, rssHeaderActionSpec{
		key: "rss-reader-star", label: label, glyph: glyph, color: color,
		onClick: func() { w.toggleRSSArticleStar(c, art) },
	})
}

// rssBodyToggleAction switches the open article between the feed's own version
// and its extracted full text. Until an article holds full text the box starts
// the fetch that gets it, and a fetch that gave up offers the same retry the
// reader's own line beside the body does.
func (w *Window) rssBodyToggleAction(c *ui.Context, t mui.Tokens, art core.RSSArticle) ui.Element {
	zh := strings.HasPrefix(strings.ToLower(c.Locale()), "zh")
	label := "Show Original"
	glyph, color := "doc.text", t.Muted
	switch {
	case w.rssFullTextPhase(art.ID) == rssFullTextFetching:
		label := "Fetching…"
		if zh {
			label = "抓取中…"
		}
		return w.rssHeaderAction(c, t, rssHeaderActionSpec{
			key: "rss-reader-body-toggle", label: label, glyph: glyph, busy: true,
		})
	case w.rssFullTextPhase(art.ID) == rssFullTextFailed && !art.IsFullText:
		// This attempt gave up on the article, so the control names the one
		// thing it can still do, and keeps its own glyph: it is still the way
		// to the article's text.
		label, color = "Retry Full Text", t.Warning
		if zh {
			label = "重试抓取"
		}
	case !art.IsFullText:
		label = "Fetch Full Text"
		if zh {
			label = "抓取全文"
		}
	case w.rssReader.viewFeed:
		label, glyph = "Show Full Text", "doc.plaintext"
		if zh {
			label = "显示全文"
		}
	default:
		if zh {
			label = "显示原文"
		}
	}
	spec := rssHeaderActionSpec{
		key: "rss-reader-body-toggle", label: label, glyph: glyph, color: color,
		onClick: func() { w.toggleRSSBodyView(c, art) },
	}
	if strings.TrimSpace(art.Link) == "" && !art.IsFullText {
		spec.disabled = true
		spec.tooltip = rssNoLinkReason(c.Locale())
	}
	return w.rssHeaderAction(c, t, spec)
}

// rssOpenAction opens the article's original page in the default browser.
func (w *Window) rssOpenAction(c *ui.Context, t mui.Tokens, art core.RSSArticle) ui.Element {
	zh := strings.HasPrefix(strings.ToLower(c.Locale()), "zh")
	label := "Open in Browser"
	if zh {
		label = "在浏览器中打开"
	}
	link := strings.TrimSpace(art.Link)
	spec := rssHeaderActionSpec{key: "rss-reader-open", label: label, glyph: "safari", disabled: link == ""}
	if link == "" {
		spec.tooltip = rssNoLinkReason(c.Locale())
	} else {
		spec.onClick = func() { c.OpenURL(link) }
	}
	return w.rssHeaderAction(c, t, spec)
}

// rssRefreshAction is the header's re-fetch: idle as Refetch, a spinner while
// the fetch runs, and the warning face once an attempt gave up. Its name stays
// the control's own, and the tooltip carries the phase, so the accessible name
// of the box never changes under the pointer while the body toggle already
// names the fetch itself.
func (w *Window) rssRefreshAction(c *ui.Context, t mui.Tokens, art core.RSSArticle) ui.Element {
	zh := strings.HasPrefix(strings.ToLower(c.Locale()), "zh")
	label, tooltip, color := "Refetch", "", t.Muted
	if zh {
		label = "重新抓取"
	}
	spec := rssHeaderActionSpec{key: "rss-reader-refetch", label: label, glyph: "arrow.clockwise", color: color}
	switch w.rssFullTextPhase(art.ID) {
	case rssFullTextFetching:
		spec.busy = true
		tooltip = "Fetching…"
		if zh {
			tooltip = "抓取中…"
		}
	case rssFullTextFailed:
		// This attempt gave up on the article, so the tooltip names the one
		// thing the control can still do.
		tooltip, spec.color = "Retry Full Text", t.Warning
		if zh {
			tooltip = "重试抓取"
		}
	}
	if tooltip != "" {
		spec.tooltip = tooltip
	}
	if strings.TrimSpace(art.Link) == "" {
		spec.busy = false
		spec.disabled = true
		spec.tooltip = rssNoLinkReason(c.Locale())
	} else {
		spec.onClick = func() { w.refetchRSSArticle(c, art) }
	}
	return w.rssHeaderAction(c, t, spec)
}

// rssShareMenuWidth is the share menu's panel width, wide enough for its two
// commands and the pointer's room beside them.
const rssShareMenuWidth float32 = 196

// rssShareCopyWords are the share menu's two commands and the sentences that
// confirm them.
type rssShareCopyWords struct {
	action     string
	copyLink   string
	copyBoth   string
	copiedLink string
	copiedBoth string
}

// rssNoLinkReason is the reason every control that needs the article's page
// gives when the feed delivered no link for it.
func rssNoLinkReason(locale string) string {
	if strings.HasPrefix(strings.ToLower(locale), "zh") {
		return "这篇文章没有链接"
	}
	return "This article has no link"
}

// rssShareWords returns the share menu's words in the reader's language.
func rssShareWords(locale string) rssShareCopyWords {
	if strings.HasPrefix(strings.ToLower(locale), "zh") {
		return rssShareCopyWords{
			action:     "分享",
			copyLink:   "复制链接",
			copyBoth:   "复制标题与链接",
			copiedLink: "已复制链接",
			copiedBoth: "已复制标题与链接",
		}
	}
	return rssShareCopyWords{
		action:     "Share",
		copyLink:   "Copy Link",
		copyBoth:   "Copy Title and Link",
		copiedLink: "Link copied",
		copiedBoth: "Title and link copied",
	}
}

// rssShareAction is the header's share control: a two-item menu that puts the
// article's link, or its title and link, on the clipboard. There is no system
// share sheet behind it, and an article without a link cannot be shared.
func (w *Window) rssShareAction(c *ui.Context, t mui.Tokens, art core.RSSArticle) ui.Element {
	words := rssShareWords(c.Locale())
	link := strings.TrimSpace(art.Link)
	spec := rssHeaderActionSpec{
		key: "rss-reader-share", label: words.action, glyph: "square.and.arrow.up",
		disabled: link == "", on: w.rssShareMenuOpen && link != "",
	}
	if link == "" {
		// Nothing to share, and the control says why.
		spec.tooltip = rssNoLinkReason(c.Locale())
		return w.rssHeaderAction(c, t, spec)
	}
	el := w.rssHeaderAction(c, t, spec)
	el.PopoverMenuButton(func(m *ui.Menu) {
		if m.Item(words.copyLink).Chosen() {
			w.copyRSSArticleText(c, words.copiedLink, link)
		}
		if m.Item(words.copyBoth).Chosen() {
			w.copyRSSArticleText(c, words.copiedBoth, rssShareTitleAndLink(art))
		}
	}, ui.PopoverMenuButtonOptions{
		At: ui.AnchorBottomEnd, Self: ui.AnchorTopEnd, Open: &w.rssShareMenuOpen,
		Panel: func(panel ui.Element) {
			panel.MinWidth(rssShareMenuWidth).Radius(mui.RadiusNotice).
				Border(mui.Hairline/2, t.Border).
				Shadow(0, mui.ElevationOffsetY, mui.ElevationBlur, mui.ElevationSpread, t.Shadow)
		},
	})
	return el
}

// rssShareTitleAndLink is what the share menu's second command puts on the
// clipboard: the article's title, then its link on the next line, so a paste
// into a note or a message keeps both.
func rssShareTitleAndLink(art core.RSSArticle) string {
	title, link := strings.TrimSpace(art.Title), strings.TrimSpace(art.Link)
	if title == "" {
		return link
	}
	return title + "\n" + link
}

// copyRSSArticleText puts text on the clipboard and confirms it, or says the
// copy failed when the system refused it. The confirmation rides the frame the
// clipboard's answer arrives on rather than the frame the menu opened in, whose
// context the menu itself has outlived.
func (w *Window) copyRSSArticleText(c *ui.Context, copied, text string) {
	locale := c.Locale()
	c.WriteClipboardThen(text, func(err error) {
		if err != nil {
			w.toast(nil, rssCopyFailed(locale), true)
			return
		}
		w.toast(nil, copied, false)
	})
}

// rssCopyFailed is the sentence a clipboard write that never landed shows.
func rssCopyFailed(locale string) string {
	if strings.HasPrefix(strings.ToLower(locale), "zh") {
		return "无法复制到剪贴板"
	}
	return "Couldn't copy to the clipboard"
}

// toggleRSSBodyView switches the open article between its extracted full text
// and the feed's own version. An article that holds no full text yet has
// nothing to switch to, so the same control asks for the text instead, through
// the fetch every other part of the reader uses.
func (w *Window) toggleRSSBodyView(c *ui.Context, art core.RSSArticle) {
	if w.rssFullTextPhase(art.ID) == rssFullTextFetching {
		return
	}
	if !art.IsFullText {
		w.refetchRSSArticle(c, art)
		return
	}
	w.rssReader.viewFeed = !w.rssReader.viewFeed
	if c != nil {
		c.Invalidate()
	}
}

// rssReaderBody is the scrolling article body: the reading column of blocks,
// the line above them that follows the full-text fetch whenever only the
// feed's own text is available, and the original article link. blocks are the
// body the pane parsed once for this frame, which the section rail maps, and
// feedView says those blocks are the feed's version rather than the extracted
// text.
func (w *Window) rssReaderBody(c *ui.Context, t mui.Tokens, art core.RSSArticle, blocks []core.ArticleBlock, feedView bool) {
	st := &w.rssReader
	scale := w.readingType()
	// An article the reader asked to see from its feed is showing the original,
	// not a fallback, so it carries no fetch line; one whose feed body was never
	// kept falls back to its summary and says so.
	missingFeedBody := feedView && strings.TrimSpace(art.FeedContentHTML) == ""
	degraded := !art.IsFullText || missingFeedBody || len(blocks) == 0
	gutter := rssReaderPadding(w.rssLayout.readerWidth - rssTickRailWidth)

	body := ui.Scroll(c.Key("rss-reader-body:"+art.ID)).Grow(1).MinHeight(0).Clip().
		TrackScroll(&st.scroll).
		Padding(mui.Space(5), gutter, mui.Space(8)+mui.ScrollBottomInset, gutter).
		Role(ui.RoleGroup).Label("Article body")

	bounds := make([]ui.Rect, len(blocks))
	body.Children(func() {
		ui.Column(c.Key("rss-reader-content:" + art.ID)).FillWidth().Gap(scale.BlockGap).Children(func() {
			if degraded {
				// The line above the body follows the fetch: a quiet placeholder
				// while the text is on its way, a compact failure state when it
				// did not arrive, and the browser's call to action only once
				// there is nothing left to try.
				switch w.rssFullTextPhase(art.ID) {
				case rssFullTextFetching:
					w.rssFetchingPlaceholder(c, t)
				case rssFullTextFailed:
					w.rssFetchFailedNotice(c, t)
				default:
					w.rssDegradedNotice(c, t, art)
				}
			}
			if art.HeroImage != "" && !feedView && !rssBlocksHoldImage(blocks, art.HeroImage) {
				w.rssImageBlock(c, t, art.ID+":hero", core.ArticleBlock{Type: core.BlockImage, URL: art.HeroImage}, w.rssLayout.readerWidth, true)
			}
			for i, block := range blocks {
				el := w.rssBlock(c, t, art, block, i, scale)
				bounds[i] = el.LayoutBounds()
			}
			if strings.TrimSpace(art.Link) != "" {
				w.rssOriginalLink(c, t, art)
			}
		})
	})
	st.viewport = body.LayoutBounds()
	st.blocks = bounds
	sections := rssSectionBounds(blocks, bounds)
	st.active = rssActiveBlock(sections, st.viewport)
	if st.active < 0 && st.scroll.MaxY == 0 && len(sections) > 0 {
		// The body fits the viewport, so the whole article is on screen and no
		// scroll can lift a heading to the line: its first section is current.
		st.active = 0
	}
	if st.jumping {
		// A mark's click reads its section before the body has glided there:
		// its mark stays grown until the scroll arrives and the reading line
		// takes over, so the highlight never sweeps back on the way.
		if st.scroll.Scrolling() && st.jump >= 0 && st.jump < len(sections) {
			st.active = st.jump
		} else {
			st.jumping = false
		}
	}
}

// rssReaderBlocks is the body the reader draws for an article: its parsed
// blocks, or the feed's own version while the header's toggle asks for the
// original. feedView is the toggle's choice for this article.
func (w *Window) rssReaderBlocks(art core.RSSArticle, feedView bool) []core.ArticleBlock {
	if feedView {
		return w.rssFeedViewBlocks(art)
	}
	if len(art.Blocks) > 0 {
		return art.Blocks
	}
	st := &w.rssReader
	if st.parsedBlocksArticleID == art.ID && st.parsedBlocksUpdatedAt == art.UpdatedAt && st.parsedBlocks != nil {
		return st.parsedBlocks
	}
	blocks := rssArticleBlocks(art)
	st.parsedBlocksArticleID = art.ID
	st.parsedBlocksUpdatedAt = art.UpdatedAt
	st.parsedBlocks = blocks
	return blocks
}

// rssFeedViewBlocks is the body the reader shows for an article's original: the
// version the feed itself delivered, parsed once per article so the rail and
// the scroll frame do not re-parse it, and the summary the article carries when
// a row written before that version was kept has none, so the pane is never
// blank.
func (w *Window) rssFeedViewBlocks(art core.RSSArticle) []core.ArticleBlock {
	st := &w.rssReader
	if st.feedViewArticleID == art.ID && st.feedViewUpdatedAt == art.UpdatedAt && st.feedViewBlocks != nil {
		return st.feedViewBlocks
	}
	var blocks []core.ArticleBlock
	if body := strings.TrimSpace(art.FeedContentHTML); body != "" {
		blocks = core.ParseHTMLBlocksWithBase(body, art.Link)
	}
	if len(blocks) == 0 {
		if snippet := strings.TrimSpace(art.Snippet); snippet != "" {
			blocks = core.ParseHTMLBlocks("<p>" + htmlpkg.EscapeString(snippet) + "</p>")
		}
	}
	if blocks == nil {
		blocks = []core.ArticleBlock{}
	}
	st.feedViewArticleID = art.ID
	st.feedViewUpdatedAt = art.UpdatedAt
	st.feedViewBlocks = blocks
	return blocks
}

// rssArticleBlocks is the structured body the reader renders: the article's
// parsed blocks, or the content HTML parsed again, or finally its snippet so a
// failed extraction still reads as prose instead of a blank page.
func rssArticleBlocks(art core.RSSArticle) []core.ArticleBlock {
	if len(art.Blocks) > 0 {
		return art.Blocks
	}
	if blocks := core.ParseHTMLBlocksWithBase(art.ContentHTML, art.Link); len(blocks) > 0 {
		return blocks
	}
	snippet := strings.TrimSpace(art.Snippet)
	if snippet == "" {
		return nil
	}
	return core.ParseHTMLBlocks("<p>" + htmlpkg.EscapeString(snippet) + "</p>")
}

// rssBlocksHoldImage reports whether the body already shows url, so a feed's
// hero image is not rendered twice when it is also the first inline picture.
func rssBlocksHoldImage(blocks []core.ArticleBlock, url string) bool {
	for _, b := range blocks {
		if b.Type == core.BlockImage && b.URL == url {
			return true
		}
	}
	return false
}

// rssReaderDate formats an article's timestamp for the reading header: Today
// for a same-day article and the day it fell on for any other, without the
// clock the list and the body already carry.
func rssReaderDate(raw string, now time.Time) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return raw
	}
	published, today := t.Local(), now.Local()
	if published.Year() == today.Year() && published.YearDay() == today.YearDay() {
		return "Today"
	}
	return formatArticleDay(published, today)
}

// rssFetchingPlaceholder is the reader's line above the feed's own text while
// the full-text fetch runs: the summary stays readable and the page says what
// is happening in one quiet line, so nothing sends the reader to the browser
// for text that is already on its way.
func (w *Window) rssFetchingPlaceholder(c *ui.Context, t mui.Tokens) {
	zh := strings.HasPrefix(strings.ToLower(c.Locale()), "zh")
	sentence := "Fetching the full text…"
	if zh {
		sentence = "正在抓取全文…"
	}
	row := ui.Row(c.Key("rss-reader-fetching")).FillWidth().Gap(mui.Space(2)).
		Padding(mui.Space(1), 0).AlignItems(ui.Center)
	row.Children(func() {
		sfsymbols.Symbol(c, "arrow.triangle.2.circlepath").Size(mui.Space(3.5)).Color(t.Faint).Element.Role(ui.RoleNone)
		mui.Secondary.Apply(ui.Text(c, sentence)).TextColor(t.Faint).Wrap().Grow(1)
	})
}

// rssFetchFailedNotice is the reader's line above the feed's own text when the
// full-text fetch did not arrive: the summary stays, the line says why, and
// the header's retry is the way to try again, so the browser's call to action
// does not open over text the reader asked the app to fetch.
func (w *Window) rssFetchFailedNotice(c *ui.Context, t mui.Tokens) {
	zh := strings.HasPrefix(strings.ToLower(c.Locale()), "zh")
	sentence := "Couldn't fetch the full text. Showing the feed's summary."
	if zh {
		sentence = "全文抓取失败，当前显示摘要。"
	}
	row := ui.Row(c.Key("rss-reader-fetch-failed")).FillWidth().Gap(mui.Space(2)).
		Padding(mui.Space(1), 0).AlignItems(ui.Center)
	row.Children(func() {
		sfsymbols.Symbol(c, "exclamationmark.triangle").Size(mui.Space(3.5)).Color(t.Warning).Element.Role(ui.RoleNone)
		mui.Secondary.Apply(ui.Text(c, sentence)).TextColor(t.Muted).Wrap().Grow(1)
	})
}

// rssDegradedNotice tells the reader the body is the feed's own text and the
// full article is one click away in the browser.
func (w *Window) rssDegradedNotice(c *ui.Context, t mui.Tokens, art core.RSSArticle) {
	zh := strings.HasPrefix(strings.ToLower(c.Locale()), "zh")
	link := strings.TrimSpace(art.Link)
	sentence := "Showing the feed's summary. Open the original article in your browser for the full text."
	role := ui.RoleLink
	if zh {
		sentence = "当前展示摘要，可点击在浏览器中查看原网页"
	}
	if link == "" {
		sentence = "Showing the feed's summary."
		role = ui.RoleGroup
		if zh {
			sentence = "当前展示摘要。"
		}
	}
	notice := ui.Row(c.Key("rss-reader-summary-notice")).FillWidth().Gap(mui.Space(2)).
		Padding(mui.Space(2.5), mui.Space(3)).Radius(mui.RadiusNotice).
		Background(t.SurfaceAlt).Border(1, t.BorderSoft).AlignItems(ui.Center).
		Role(role).Label(sentence)
	if link != "" {
		open := func() { c.OpenURL(link) }
		notice.Cursor(ui.CursorPointer).Tooltip(link).
			Focusable().FocusRingColor(t.Focus).FocusRingOffset(mui.FocusRingGap).
			OnClick(open).OnShortcut(0, ui.KeyEnter, open)
	}
	notice.Children(func() {
		sfsymbols.Symbol(c, "doc.plaintext").Size(mui.Space(4)).Color(t.Warning).Element.Role(ui.RoleNone)
		mui.Secondary.Apply(ui.Text(c, sentence)).TextColor(t.Muted).Wrap().Grow(1)
		if link != "" {
			sfsymbols.Symbol(c, "link").Size(mui.Space(3.5)).Color(t.Accent).Element.Role(ui.RoleNone)
		}
	})
}

// rssOriginalLink is the article's permanent way out of the app: the reading
// body's last line links to the original page by host name.
func (w *Window) rssOriginalLink(c *ui.Context, t mui.Tokens, art core.RSSArticle) {
	zh := strings.HasPrefix(strings.ToLower(c.Locale()), "zh")
	label := "Read the original article"
	if zh {
		label = "阅读原文"
	}
	host := rssLinkHost(art.Link)
	row := ui.Row(c.Key("rss-reader-original")).FillWidth().Gap(mui.Space(2)).
		AlignItems(ui.Center).Padding(mui.Space(2), 0, 0, 0)
	row.Children(func() {
		sfsymbols.Symbol(c, "link").Size(mui.Space(3.5)).Color(t.Accent).Element.Role(ui.RoleNone)
		link := ui.ButtonBase(c).Key("rss-reader-original-link").Height(mui.Space(5)).
			Padding(0).Radius(mui.RadiusControl).Background(ui.Color{}).Border(0, ui.Color{}).
			Cursor(ui.CursorPointer).Transition(mui.Fade).
			FocusRingColor(t.Focus).FocusRingOffset(mui.FocusRingGap).
			Role(ui.RoleLink).Label(label + ": " + art.Link).Tooltip(art.Link)
		link.Children(func() {
			ui.Row(c).Gap(mui.Space(1)).AlignItems(ui.Center).Children(func() {
				mui.Secondary.Apply(ui.Text(c, label)).TextColor(t.Accent).Underline().SingleLine()
				if host != "" {
					mui.Secondary.Apply(ui.Text(c, "·")).TextColor(t.Faint).Shrink(0)
					mui.Secondary.Apply(ui.Text(c, host)).TextColor(t.Muted).SingleLine()
				}
			})
		})
		link.OnClick(func() {
			c.OpenURL(art.Link)
		})
	})
}

// rssLinkHost is the host an article link names, for the reading body's
// original-article line.
func rssLinkHost(link string) string {
	trimmed := strings.TrimSpace(link)
	if trimmed == "" {
		return ""
	}
	if i := strings.Index(trimmed, "://"); i >= 0 {
		trimmed = trimmed[i+3:]
	}
	trimmed = strings.TrimPrefix(trimmed, "www.")
	if i := strings.IndexAny(trimmed, "/?#"); i >= 0 {
		trimmed = trimmed[:i]
	}
	return trimmed
}

// rssTickRail is the section indicator lane: one mark per level-2 heading, the
// mark at the reading line grown and accented, the mark under the pointer grown
// and darker, the rest thin and muted. A click on a mark takes the reader to
// the heading it stands for, and the mark under the pointer opens its section's
// preview. The lane spans the reading viewport - deliberately the scrolling
// body's height, not the whole window's - so its marks and the track they sit
// in are measured against the stretch of text they describe. The lane is
// painted, not built per mark, so a long article costs one draw call per frame:
// the pointer's place in the lane names the mark.
func (w *Window) rssTickRail(c *ui.Context, t mui.Tokens, blocks []core.ArticleBlock) {
	st := &w.rssReader
	st.hover = -1
	sections := rssSections(blocks)
	count := rssTickCount(len(sections), st.viewport.H)
	if count <= 0 {
		// An article with no level-2 heading has no structure to map. The lane
		// still holds its width, which the reading column's maths is built on,
		// but it is a spacer rather than a rail: nothing is drawn, nothing is
		// named, and no click lands on it.
		ui.Box(c.Key("rss-ticks")).Width(rssTickRailWidth).Shrink(0)
		return
	}
	rail := ui.Column(c.Key("rss-ticks")).Width(rssTickRailWidth).Shrink(0).
		Role(ui.RoleGroup).Label("Reading position")
	// The pointer's place is the last frame's: the lane it was over is the one
	// the marks were laid in then, and the frame that follows a move rebuilds
	// from it.
	if x, y, over := rail.PointerPosition(); over {
		st.hover = rssTickIndexAt(st.rail, count, st.viewport.H, st.rail.X+x, st.rail.Y+y)
	}
	if st.hover >= 0 && rail.Clicked() {
		w.jumpToTick(blocks, sections, count)
	}
	// The current section is a place in the article, its mark a place on the
	// rail: a merged mark carries the section it gathers, so the accent goes on
	// the mark that section maps to rather than on a mark with its number.
	active := rssTickBucket(st.active, len(sections), count)
	rail.Draw(func(p *ui.Painter, r ui.Rect) {
		st.rail = r
		for i, tick := range rssTickRects(r, count, active, st.hover, st.viewport.H) {
			color := t.Border
			switch i {
			case active:
				color = t.Accent
			case st.hover:
				color = t.Muted
			}
			p.Fill(tick, color, tick.H/2)
		}
	})
	if st.hover >= 0 {
		rail.Children(func() {
			w.rssTickPreview(c, t, blocks, sections, count, active)
		})
	}
}

// jumpToTick scrolls the body to the section the mark under the pointer stands
// for - the first of the headings it gathers - and grows that mark in this same
// frame: the highlight holds on it while the body glides, then the reading line
// takes over where it lands.
func (w *Window) jumpToTick(blocks []core.ArticleBlock, sections []int, count int) {
	st := &w.rssReader
	section := rssTickBlock(st.hover, len(sections), count)
	bounds := rssSectionBounds(blocks, st.blocks)
	if section < 0 || section >= len(bounds) {
		return
	}
	st.jump, st.jumping, st.active = section, true, section
	st.scroll.ScrollTo(0, rssBlockScrollY(bounds, st.viewport, st.scroll.Y, section), ui.Smooth)
}

// rssTickPreview is the hover preview: the section the mark under the pointer
// stands for, its heading on one bold line and its first paragraph's opening
// over the two lines beneath it, in a light bubble hanging over the reading
// column beside the mark. It ignores the pointer, so it never takes the hover
// that shows it, and it fades in and out as the pointer comes along the rail
// and leaves it.
func (w *Window) rssTickPreview(c *ui.Context, t mui.Tokens, blocks []core.ArticleBlock, sections []int, count, active int) {
	st := &w.rssReader
	if st.hover < 0 || count <= 0 {
		return
	}
	section := rssTickBlock(st.hover, len(sections), count)
	if section < 0 || section >= len(sections) || sections[section] >= len(blocks) {
		return
	}
	marks := rssTickRects(st.rail, count, active, st.hover, st.viewport.H)
	if st.hover >= len(marks) {
		return
	}
	heading := blocks[sections[section]]
	words := ""
	lines := 0
	if prose := rssSectionProse(blocks, sections[section]); prose >= 0 {
		words = rssPreviewWords(blocks[prose], rssPreviewRunes)
		if words != "" {
			lines = rssPreviewLines
		}
	}
	zh := strings.HasPrefix(strings.ToLower(c.Locale()), "zh")
	r := rssPreviewRect(st.rail, marks[st.hover], rssPreviewWidth,
		rssPreviewHeight(lines), rssPreviewGap, rssPreviewMargin)
	bubble := ui.Column(c.Key("rss-tick-preview")).Absolute().Left(r.X).Top(r.Y).Width(r.W).Height(r.H).
		Padding(mui.Space(2)).Gap(mui.Space(0.5)).Radius(mui.RadiusCard).
		Background(t.Surface).Border(mui.Hairline, t.Border).
		Shadow(0, mui.ElevationOffsetY, mui.ElevationBlur, mui.ElevationSpread, t.Shadow).
		IgnoreInput(true).Role(ui.RoleGroup).Label(rssPreviewLabel(zh)).
		// Only its colours and shadow ease: its place is the mark's, which the
		// pointer already follows, so the bubble never trails behind it.
		Transition(ui.ElementTransition{
			Duration:     mui.MotionPopover,
			Ease:         mui.Ease,
			ExitDuration: mui.MotionInteraction,
			Colors:       true,
			Shadows:      true,
			Enter:        &ui.Motion{Opacity: 0},
			Exit:         &ui.Motion{Opacity: 0},
		})
	bubble.Children(func() {
		mui.PopoverTitle.Apply(ui.Text(c, heading.Text)).TextColor(t.Text).SingleLine()
		if words != "" {
			mui.PopoverBody.Apply(ui.Text(c, words)).TextColor(t.Muted).MaxLines(rssPreviewLines).Wrap()
		}
	})
}

// rssPreviewWords is a paragraph's opening words for the section rail's
// preview: its text run together on one line and cut at limit characters, or
// the words of its markup when the paragraph holds no text of its own. A
// paragraph left without any previews nothing.
func rssPreviewWords(block core.ArticleBlock, limit int) string {
	text := block.Text
	if strings.TrimSpace(text) == "" {
		text = core.CleanSnippet("", block.HTML, 0)
	}
	text = strings.Join(strings.Fields(text), " ")
	if runes := []rune(text); limit > 0 && len(runes) > limit {
		text = strings.TrimSpace(string(runes[:limit])) + "…"
	}
	return text
}

// rssPreviewLabel names the hover preview for assistive technology and for the
// tests that place it. It is the only group in the reader that carries it, so
// the label needs no heading of its own.
func rssPreviewLabel(zh bool) string {
	if zh {
		return "章节预览"
	}
	return "Section preview"
}

// rssBlock renders one structured article block at scale. Every block takes the
// reading column's width, and the element is returned so the caller can record
// the bounds the section rail reads.
func (w *Window) rssBlock(c *ui.Context, t mui.Tokens, art core.RSSArticle, block core.ArticleBlock, index int, scale mui.ArticleType) ui.Element {
	key := "rss-block:" + art.ID + ":" + strconv.Itoa(index)
	switch block.Type {
	case core.BlockHeading:
		level := block.Level
		if level < 1 || level > 6 {
			level = 2
		}
		style := scale.HeadingFor(level)
		return style.Apply(ui.Text(c, block.Text)).TextColor(t.Text).
			Key(key).FillWidth().Margin(scale.HeadingGap, 0, 0, 0).Wrap()

	case core.BlockBlockquote:
		quote := ui.Row(c.Key(key)).FillWidth().Gap(mui.Space(3)).
			Padding(0, 0, 0, mui.Space(3)).AlignItems(ui.Stretch)
		quote.Draw(func(p *ui.Painter, r ui.Rect) {
			p.Fill(ui.Rect{X: r.X, Y: r.Y, W: 3, H: r.H}, t.Accent, 1.5)
		})
		quote.Children(func() {
			scale.Quote.Apply(ui.Text(c, block.Text)).TextColor(t.Muted).Wrap().Grow(1)
		})
		return quote

	case core.BlockCode:
		code := ui.Column(c.Key(key)).FillWidth().Radius(mui.RadiusNotice).
			Padding(mui.Space(3), mui.Space(3)).Background(t.SurfaceAlt)
		code.Children(func() {
			if lang := strings.TrimSpace(block.Language); lang != "" {
				mui.Metadata.Apply(ui.Text(c, strings.ToUpper(lang))).TextColor(t.Faint).SingleLine()
			}
			scale.Code.Apply(ui.Text(c, strings.TrimSuffix(block.Text, "\n"))).TextColor(t.Text).Wrap()
		})
		return code

	case core.BlockImage:
		return w.rssImageBlock(c, t, key, block, w.rssLayout.readerWidth, false)

	case core.BlockList:
		return scale.Body.Apply(ui.Text(c, block.Text)).TextColor(t.Text).
			Key(key).FillWidth().Wrap()

	default:
		text := block.Text
		if strings.TrimSpace(text) == "" {
			text = core.CleanSnippet("", block.HTML, 0)
		}
		return scale.Body.Apply(ui.Text(c, text)).TextColor(t.Text).
			Key(key).FillWidth().Wrap()
	}
}

// rssImageBlock is one inline picture in the reading body, held in a neutral
// frame while it comes and captioned when the feed supplies a caption. A
// picture that never comes - an anti-hotlink 403, a 404, a timeout - leaves a
// bordered placeholder naming it by its alt text rather than a blank frame.
func (w *Window) rssImageBlock(c *ui.Context, t mui.Tokens, key string, block core.ArticleBlock, readerWidth float32, hero bool) ui.Element {
	url := strings.TrimSpace(block.URL)
	if url == "" {
		return ui.Box(c).Key(key).Height(0)
	}
	label := rssBlockImageLabel(block.Alt, hero)
	column := rssReadingWidth(readerWidth)
	frame := ui.Row(c.Key(key)).FillWidth().
		Radius(mui.RadiusCard).Clip().Background(t.SurfaceAlt).Center().
		Role(ui.RoleImage).Label(label)
	opts := netimage.Options{MaxSide: int(rssReadingColumn * 2)}
	src, err := netimage.Load(c, url, opts)
	switch {
	case src != nil:
		imageW, imageH := ui.ImageSize(src)
		width, height := rssImageSize(column, imageW, imageH)
		// The frame's surface is only the place a picture waits in: once it
		// shows, the picture stands on the page, centred in the column.
		frame.Height(height).Background(ui.Color{})
		frame.Children(func() {
			ui.Image(c, src).Fit(ui.Contain).Size(width, height)
		})
	case err != nil:
		zh := strings.HasPrefix(strings.ToLower(c.Locale()), "zh")
		openImageLabel := "Open original image in browser"
		imageFailedText := "Image failed to load"
		if zh {
			openImageLabel = "在浏览器中打开原图"
			imageFailedText = "图片无法加载"
		}
		altText := strings.TrimSpace(block.Alt)
		altLabel := altText
		if altLabel == "" {
			altLabel = imageFailedText
		}

		// The frame is the picture's way out, so it becomes a link rather than
		// the image role it was built with: RoleLink takes Return but leaves
		// Space to the reading view, as every other reader link does.
		frame.MinHeight(mui.Space(24)).Padding(mui.Space(4), mui.Space(4)).
			Background(t.SurfaceAlt).Border(1, t.BorderSoft).
			Cursor(ui.CursorPointer).Tooltip(url).Role(ui.RoleLink).
			Focusable().FocusRingColor(t.Focus).FocusRingOffset(mui.FocusRingGap).
			OnClick(func() { c.OpenURL(url) }).
			OnShortcut(0, ui.KeyEnter, func() { c.OpenURL(url) })

		frame.Children(func() {
			ui.Column(c).FillWidth().Gap(mui.Space(2)).AlignItems(ui.Center).Children(func() {
				ui.Row(c).Gap(mui.Space(2)).AlignItems(ui.Center).Children(func() {
					sfsymbols.Symbol(c, "photo").Size(mui.Space(5)).Color(t.Faint).Element.Role(ui.RoleNone)
					mui.Secondary.Apply(ui.Text(c, altLabel)).TextColor(t.Muted).Wrap()
				})
				ui.Row(c).Gap(mui.Space(1)).AlignItems(ui.Center).Children(func() {
					sfsymbols.Symbol(c, "link").Size(mui.Space(3)).Color(t.Accent).Element.Role(ui.RoleNone)
					mui.Helper.Apply(ui.Text(c, openImageLabel)).TextColor(t.Accent).Underline().SingleLine()
				})
			})
		})
	default:
		frame.MinHeight(mui.Space(30)).Children(func() {
			sfsymbols.Symbol(c, "photo").Size(mui.Space(6)).Color(t.Faint).Element.Role(ui.RoleNone)
		})
	}
	if caption := strings.TrimSpace(block.Caption); caption != "" {
		figure := ui.Column(c).FillWidth().Gap(mui.Space(1)).Children(func() {
			frame.FillWidth()
			mui.Helper.Apply(ui.Text(c, caption)).TextColor(t.Muted).Wrap()
		})
		return figure
	}
	return frame
}

// rssBlockImageLabel names a picture for the accessibility tree without
// promising content the feed did not supply.
func rssBlockImageLabel(alt string, hero bool) string {
	if alt = strings.TrimSpace(alt); alt != "" {
		return alt
	}
	if hero {
		return "Article image"
	}
	return "Inline image"
}
