package main

import (
	"context"
	htmlpkg "html"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/sfsymbols"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/netimage"
	"github.com/simon/youtube-reader/core"
	mui "github.com/simon/youtube-reader/ui"
)

// The reader view's paragraph rail: a 24 pt lane on the reader's right edge
// holding one short bar per article block, spread over the lane's height so a
// bar's place is its position in the article. The bar the reading line is in
// grows and takes the accent; the bar under the pointer grows and darkens; the
// rest stay thin and muted. A click on a bar takes the reader to the paragraph
// it shows, and the bar under the pointer opens the preview beside it.
const (
	rssTickRailWidth float32 = 24
	rssTickInset     float32 = 6 // room above the first and below the last bar
	rssTickInsetX    float32 = 3 // room between a bar's end and the pane edge
	rssTickMinPitch  float32 = 5 // closest two slots may sit, clear of the grown bar
	rssTickInactiveW float32 = 12
	rssTickInactiveH float32 = 2
	rssTickHoverW    float32 = 16
	rssTickHoverH    float32 = 3
	rssTickActiveW   float32 = 18
	rssTickActiveH   float32 = 4
	rssReadingProbe  float32 = 24 // where the reading line sits below the viewport's top
	// The hover preview is a light bubble left of the bars: its width, the gap
	// it keeps from the widest bar, the room it keeps from the rail's ends, and
	// how many characters of the paragraph it shows.
	rssPreviewWidth  float32 = 236
	rssPreviewGap    float32 = 8
	rssPreviewMargin float32 = 6
	rssPreviewRunes  int     = 56
	rssPreviewLines  int     = 2
	// rssHeaderInlineActionsMin is the reader width from which the header's
	// star, labelled actions and the text size control share the metadata's row,
	// with room left for the feed name beside them.
	rssHeaderInlineActionsMin float32 = 680
	rssReadingColumn          float32 = 680 // the reading text column's widest
	rssReaderImageMax         float32 = 560 // the tallest an inline picture may show
	// The text size control in the reader's header is a three-segment track
	// as tall as the header's actions, one segment per body size.
	rssTextSizeTrackH   float32 = 28
	rssTextSizeSegmentH float32 = 24
	rssTextSizeInset    float32 = 2
	rssTextSizeGap      float32 = 2
	rssTextSizePadX     float32 = 7
	rssTextSizeRadius   float32 = 4
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

// rssReaderState is the reading view's scroll place and paragraph rail: the
// article it belongs to, the scroll offset, the block bounds the last frame
// laid out, and the block the reading line is in.
type rssReaderState struct {
	articleID string
	scroll    ui.ScrollState
	// ticks are the body blocks' window bounds as the last frame laid them
	// out, top to bottom. Empty until the first body frame.
	ticks []ui.Rect
	// viewport is the scrolling body's window bounds as the last frame laid
	// it out.
	viewport ui.Rect
	// rail is the paragraph rail's window bounds as the last frame painted
	// it, which a click reads the bars from before this frame's layout.
	rail ui.Rect
	// active is the block index at the reading line, -1 with no body.
	active int
	// hover is the bar under the pointer, -1 for none, and the block it
	// shows is the one a click reads.
	hover int
	// jump is the block a clicked bar is scrolling to while jumping says a
	// jump is under way: its bar stays grown until the reading line takes
	// over, so the highlight does not sweep back as the body glides.
	jump    int
	jumping bool
}

// newRSSReaderState starts an article's reader at its top, with no bar under
// the pointer and no jump under way.
func newRSSReaderState(articleID string) rssReaderState {
	return rssReaderState{articleID: articleID, hover: -1, jump: -1}
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

// rssTickCount returns how many bars a rail of railHeight shows for blockCount
// blocks: one per block when they can keep the minimum pitch apart, and fewer
// buckets when the article is longer than the rail can keep apart.
func rssTickCount(blockCount int, railHeight float32) int {
	if blockCount <= 0 || railHeight <= 0 {
		return 0
	}
	room := railHeight - 2*rssTickInset
	if room <= 0 {
		return 0
	}
	maxTicks := int(room / rssTickMinPitch)
	if maxTicks < 1 {
		maxTicks = 1
	}
	if blockCount < maxTicks {
		return blockCount
	}
	return maxTicks
}

// rssTickBucket maps a body block to the bar that shows it. With one bar per
// block the block is the bar; with fewer, adjacent blocks share one.
func rssTickBucket(block, blockCount, tickCount int) int {
	if block < 0 || blockCount <= 0 || tickCount <= 0 {
		return -1
	}
	if block >= blockCount {
		block = blockCount - 1
	}
	if tickCount >= blockCount {
		return block
	}
	bucket := block * tickCount / blockCount
	if bucket >= tickCount {
		bucket = tickCount - 1
	}
	return bucket
}

// rssTickBlock is rssTickBucket the other way: the first body block a bar
// shows, the paragraph a click on the bar reads. A bar that gathers several
// blocks reads the first of them, the one whose place in the article the bar
// stands at. -1 asks for no bar, or a rail without one.
func rssTickBlock(bucket, blockCount, tickCount int) int {
	if bucket < 0 || blockCount <= 0 || tickCount <= 0 {
		return -1
	}
	if bucket >= tickCount {
		bucket = tickCount - 1
	}
	// The bar shows the blocks b whose block*tickCount/blockCount is its own,
	// the smallest of which is the first that rounds up to it.
	block := (bucket*blockCount + tickCount - 1) / tickCount
	if block >= blockCount {
		block = blockCount - 1
	}
	return block
}

// rssTickIndexAt is the bar a point in the rail is on: the slot its y falls in,
// within the bars' right-aligned column, so the whole pitch of a bar answers
// for it and a 2 pt mark is easy to hit. The lane's insets, the gutter left of
// the bars and everything outside the rail answer -1.
func rssTickIndexAt(rail ui.Rect, count int, x, y float32) int {
	if count <= 0 || !rail.Contains(x, y) {
		return -1
	}
	room := rail.H - 2*rssTickInset
	if room <= 0 {
		return -1
	}
	left := rail.X + rail.W - rssTickInsetX - max(rssTickInactiveW, max(rssTickHoverW, rssTickActiveW))
	if x < left {
		return -1
	}
	first := rail.Y + rssTickInset
	pitch := room / float32(count)
	if y < first || y >= first+float32(count)*pitch {
		return -1
	}
	return min(int((y-first)/pitch), count-1)
}

// rssActiveBlock returns the block the reading line falls in: one line below
// the viewport's top, the block whose top last passed that line is the one
// being read, so at the article's top the first block is active and scrolling
// advances the bar as each block reaches the line. Bounds are window
// coordinates ordered top to bottom, and blocks the frame has not laid out yet
// (zero height) are skipped. A body with no blocks returns -1.
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
	if active < 0 {
		active = 0
	}
	return active
}

// rssTickRects lays the rail's bars inside rail: count slots spread over the
// lane's whole height, so a bar's place is its position in the article and the
// grown active bar shows how far through the piece the reader is. The bar
// under the pointer grows too, under the active one's size, and an active one
// under the pointer keeps its own. Bars are right-aligned on the pane's edge.
func rssTickRects(rail ui.Rect, count, active, hover int) []ui.Rect {
	if count <= 0 || rail.W <= 0 || rail.H <= 0 {
		return nil
	}
	room := rail.H - 2*rssTickInset
	if room <= 0 {
		return nil
	}
	pitch := room / float32(count)
	rects := make([]ui.Rect, count)
	for i := range rects {
		width, height := rssTickInactiveW, rssTickInactiveH
		switch {
		case i == active:
			width, height = rssTickActiveW, rssTickActiveH
		case i == hover:
			width, height = rssTickHoverW, rssTickHoverH
		}
		rects[i] = ui.Rect{
			X: rail.X + rail.W - rssTickInsetX - width,
			Y: rail.Y + rssTickInset + float32(i)*pitch + (pitch-height)/2,
			W: width,
			H: height,
		}
	}
	return rects
}

// rssPreviewHeight is the room the hover preview keeps: its padding and
// border, the block kind's line, and the lines of the paragraph below it. The
// preview places itself by that room rather than by what its own text measures,
// so its top stays put as the pointer moves from a bar whose paragraph fills
// both lines to one whose words fill a single line.
func rssPreviewHeight(lines int) float32 {
	lines = max(lines, 1)
	return 2*mui.Space(2) + 2*mui.Hairline + mui.Metadata.LineHeight +
		mui.Space(0.5) + float32(lines)*mui.Secondary.LineHeight
}

// rssPreviewRect places the hover preview in the rail's own coordinates: its
// right edge the gap left of the widest bar, its middle on the hovered bar's
// middle, and its top kept the margin inside the rail's ends where the bar sits
// at either one. The bubble is wider than the lane, so x is negative: it hangs
// over the reading column.
func rssPreviewRect(rail, bar ui.Rect, width, height, gap, margin float32) ui.Rect {
	if rail.H <= 0 || width <= 0 || height <= 0 {
		return ui.Rect{}
	}
	top := bar.Y + bar.H/2 - rail.Y - height/2
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
// paragraph the click read when the glide lands. Bounds and viewport are the
// last frame's, in window coordinates, with the offset the body was scrolled to
// then; the start of the body is 0 however far above the first block the line
// would fall.
func rssBlockScrollY(bounds []ui.Rect, viewport ui.Rect, scrollY float32, block int) float32 {
	if block < 0 || block >= len(bounds) || viewport.H <= 0 {
		return max(scrollY, 0)
	}
	return max(bounds[block].Y-viewport.Y+scrollY-rssReadingProbe, 0)
}

// rssReaderPane renders Column 3: the article reading view with its header,
// body, and paragraph rail, or the empty state when nothing is selected.
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
	blocks := rssArticleBlocks(*art)

	ui.Row(c.Key("rss-reader-pane")).Fill().Background(t.App).AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).MinHeight(0).Clip().Children(func() {
			w.rssReaderHeader(c, t, *art)
			ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).MinHeight(0).Children(func() {
					w.rssReaderBody(c, t, *art, blocks)
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

// rssReaderHeader is the fixed article header: its source and date, its title,
// its author, the star that toggles the article's star, the text size control,
// and the two actions that leave the app's rendering.
func (w *Window) rssReaderHeader(c *ui.Context, t mui.Tokens, art core.RSSArticle) {
	zh := strings.HasPrefix(strings.ToLower(c.Locale()), "zh")
	openLabel := "Open in Browser"
	refetchLabel := "Refetch"
	refetchingLabel := "Fetching…"
	if zh {
		openLabel = "在浏览器中打开"
		refetchLabel = "重新抓取"
		refetchingLabel = "抓取中…"
	}
	feedName := art.FeedID
	if w.app != nil {
		if title := w.app.FeedTitle(art.FeedID); title != "" {
			feedName = title
		}
	}

	gutter := rssReaderPadding(w.rssLayout.readerWidth - rssTickRailWidth)
	inline := rssHeaderInlineActions(w.rssLayout.readerWidth)
	busy := w.rssRefetchingID == art.ID
	refetchText := refetchLabel
	if busy {
		refetchText = refetchingLabel
	}
	actions := func() {
		ui.Row(c).Shrink(0).Gap(mui.Space(2)).AlignItems(ui.Center).Children(func() {
			w.rssReaderStarAction(c, t, art)
			w.rssReaderAction(c, t, "rss-reader-open", openLabel, false, func() {
				if art.Link != "" {
					c.OpenURL(art.Link)
				}
			})
			w.rssReaderAction(c, t, "rss-reader-refetch", refetchText, busy, func() {
				w.refetchRSSArticle(c, art)
			})
		})
	}

	head := ui.Column(c.Key("rss-reader-header")).Shrink(0).FillWidth().
		Padding(mui.Space(3), rssTickRailWidth+gutter, mui.Space(3), gutter).Gap(mui.Space(1)).
		Role(ui.RoleGroup).Label("Article header")
	head.Draw(func(p *ui.Painter, r ui.Rect) {
		p.Fill(ui.Rect{X: r.X, Y: r.Y + r.H - mui.Hairline, W: r.W, H: mui.Hairline}, t.BorderSoft, 0)
	})
	head.Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(mui.Space(2)).Children(func() {
			if feedName != "" {
				mui.Metadata.Apply(ui.Text(c, feedName)).TextColor(t.Accent).SingleLine().Shrink(1)
			}
			if date := rssReaderDate(art.PublishedAt); date != "" {
				if feedName != "" {
					mui.Metadata.Apply(ui.Text(c, "·")).TextColor(t.Faint).Shrink(0)
				}
				mui.Metadata.Apply(ui.Text(c, date)).TextColor(t.Muted).SingleLine().Shrink(0)
			}
			if inline {
				ui.Spacer(c)
				w.rssTextSizeControl(c, t)
				actions()
			}
		})
		w.readingType().Title.Apply(ui.Text(c, art.Title)).TextColor(t.Text).MaxLines(3).Wrap()
		if author := strings.TrimSpace(art.Author); author != "" {
			mui.Secondary.Apply(ui.Text(c, author)).TextColor(t.Muted).SingleLine()
		}
		if !inline {
			// A narrow reader gives the size control and the actions their
			// own rows under the metadata, so neither leaves the pane.
			ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
				w.rssTextSizeControl(c, t)
			})
			ui.Row(c).FillWidth().Justify(ui.End).Children(actions)
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
	selected := mui.ArticleStep(w.readingType().Size)
	parts := ui.SegmentedBase(c.Key("rss-text-size"), &selected, len(mui.ArticleSizes))
	track := parts.Track.Height(rssTextSizeTrackH).Shrink(0).Padding(rssTextSizeInset).Gap(rssTextSizeGap).
		Radius(mui.RadiusControl).Background(t.SurfaceAlt).Border(mui.Hairline, t.Border).
		AlignItems(ui.Center).Label(rssTextSizeTitle(zh))
	track.Children(func() {
		for i, size := range mui.ArticleSizes {
			label := rssTextSizeStepLabel(i, size, zh)
			segment := parts.Segment(i).Height(rssTextSizeSegmentH).Padding(0, rssTextSizePadX).
				Radius(rssTextSizeRadius).Center().FocusRingColor(t.Focus).FocusRingOffset(mui.FocusRingGap).
				Label(label).Tooltip(label)
			glyph := mui.TextStyle{Size: size, LineHeight: size + 6, Weight: mui.Medium}
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

// rssHeaderInlineActions reports whether the reader is wide enough for the
// header's star, size control and actions to share the metadata's row. Below it
// they take their own rows, because the star, the date, the control, and two
// labelled buttons cannot fit beside the feed name.
func rssHeaderInlineActions(readerWidth float32) bool {
	return readerWidth >= rssHeaderInlineActionsMin
}

// rssReaderAction is a 28 pt quiet action in the reader header, sized to its
// label. The disabled state covers a re-fetch already in flight.
func (w *Window) rssReaderAction(c *ui.Context, t mui.Tokens, key, label string, busy bool, onClick func()) ui.Element {
	el := ui.ButtonBase(c).Key(key).Height(mui.Space(7)).
		Padding(0, mui.Space(3)).Radius(mui.RadiusControl).Border(mui.Hairline, t.Border).
		Background(t.Surface).TextColor(t.Text).Cursor(ui.CursorPointer).
		Transition(mui.Fade).FocusRingColor(t.Focus).FocusRingOffset(mui.FocusRingGap).
		Role(ui.RoleButton).Label(label).Tooltip(label)
	if busy {
		el.TextColor(t.Muted).Cursor(ui.CursorDefault)
	}
	el.Children(func() {
		text := mui.Control.Apply(ui.Text(c, label)).SingleLine()
		if busy {
			text.TextColor(t.Muted)
		}
	})
	if !busy {
		el.Draw(rssActionFace(el, t))
		el.OnClick(onClick)
	}
	return el
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

// rssReaderStarAction is the header's star toggle: one 28 pt icon that fills
// and takes the accent while the article is starred, so both the state and the
// way to change it sit in the header beside the ways out of the app.
func (w *Window) rssReaderStarAction(c *ui.Context, t mui.Tokens, art core.RSSArticle) ui.Element {
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
	el := ui.ButtonBase(c).Key("rss-reader-star").Size(mui.Space(7), mui.Space(7)).
		Padding(0).Radius(mui.RadiusControl).Border(mui.Hairline, t.Border).
		Background(t.Surface).Cursor(ui.CursorPointer).Transition(mui.Fade).
		FocusRingColor(t.Focus).FocusRingOffset(mui.FocusRingGap).
		Role(ui.RoleButton).Label(label).Tooltip(label)
	el.Children(func() {
		sfsymbols.Symbol(c, glyph).Size(mui.Space(4)).Color(color).Element.Role(ui.RoleNone)
	})
	el.Draw(rssActionFace(el, t))
	el.OnClick(func() {
		w.toggleRSSArticleStar(c, art)
	})
	return el
}

// refetchRSSArticle re-extracts the open article's full text from its source
// page in the background, then reloads the library so the body shows it.
func (w *Window) refetchRSSArticle(c *ui.Context, art core.RSSArticle) {
	if w.app == nil || art.Link == "" || w.rssRefetchingID == art.ID {
		return
	}
	fetch := w.app.RefetchRSSArticleContent
	if w.onRefetchRSSArticle != nil {
		fetch = w.onRefetchRSSArticle
	}
	w.rssRefetchingID = art.ID
	c.Invalidate()
	go func(articleID string) {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		updated, err := fetch(ctx, articleID)
		mygo.RunOnMain(func() {
			if w.rssRefetchingID == articleID {
				w.rssRefetchingID = ""
			}
			if reloadErr := w.app.ReloadRSS(); reloadErr != nil {
				w.toast(nil, "Couldn't reload articles", true)
				w.invalidateMainWindow()
				return
			}
			switch {
			case err != nil:
				w.toast(nil, "Couldn't fetch the full article", true)
			case updated.IsFullText:
				w.toast(nil, "Full text updated", false)
			default:
				w.toast(nil, "Still showing the feed's summary", false)
			}
			w.invalidateMainWindow()
		})
	}(art.ID)
}

// rssReaderBody is the scrolling article body: the reading column of blocks,
// the summary notice when only the feed's own text is available, and the
// original article link. blocks are the body the pane parsed once for this
// frame, which the paragraph rail maps.
func (w *Window) rssReaderBody(c *ui.Context, t mui.Tokens, art core.RSSArticle, blocks []core.ArticleBlock) {
	st := &w.rssReader
	scale := w.readingType()
	degraded := !art.IsFullText || len(blocks) == 0
	gutter := rssReaderPadding(w.rssLayout.readerWidth - rssTickRailWidth)

	body := ui.Scroll(c.Key("rss-reader-body:"+art.ID)).Grow(1).MinHeight(0).Clip().
		TrackScroll(&st.scroll).
		Padding(mui.Space(5), gutter, mui.Space(8)+mui.ScrollBottomInset, gutter).
		Role(ui.RoleGroup).Label("Article body")

	bounds := make([]ui.Rect, len(blocks))
	body.Children(func() {
		ui.Column(c.Key("rss-reader-content:" + art.ID)).FillWidth().Gap(scale.BlockGap).Children(func() {
			if degraded {
				w.rssDegradedNotice(c, t, art)
			}
			if art.HeroImage != "" && !rssBlocksHoldImage(blocks, art.HeroImage) {
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
	st.ticks = bounds
	st.active = rssActiveBlock(bounds, st.viewport)
	if st.jumping {
		// A bar's click reads its paragraph before the body has glided there:
		// its bar stays grown until the scroll arrives and the reading line
		// takes over, so the highlight never sweeps back on the way.
		if st.scroll.Scrolling() && st.jump >= 0 && st.jump < len(bounds) {
			st.active = st.jump
		} else {
			st.jumping = false
		}
	}
}

// rssArticleBlocks is the structured body the reader renders: the article's
// parsed blocks, or the content HTML parsed again, or finally its snippet so a
// failed extraction still reads as prose instead of a blank page.
func rssArticleBlocks(art core.RSSArticle) []core.ArticleBlock {
	if len(art.Blocks) > 0 {
		return art.Blocks
	}
	if blocks := core.ParseHTMLBlocks(art.ContentHTML); len(blocks) > 0 {
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

// rssReaderDate formats an article's timestamp for the reading header: the
// full stamped date rather than the list's relative one, because the header
// answers "when was this written" once the article is open.
func rssReaderDate(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return raw
	}
	return strings.ToUpper(t.Local().Format("Jan 2, 2006 at 15:04"))
}

// rssDegradedNotice tells the reader the body is the feed's own text and the
// full article is one click away in the browser.
func (w *Window) rssDegradedNotice(c *ui.Context, t mui.Tokens, art core.RSSArticle) {
	zh := strings.HasPrefix(strings.ToLower(c.Locale()), "zh")
	sentence := "Showing the feed's summary. Open the original article in your browser for the full text."
	if zh {
		sentence = "当前展示摘要，可点击在浏览器中查看原网页"
	}
	notice := ui.Row(c.Key("rss-reader-summary-notice")).FillWidth().Gap(mui.Space(2)).
		Padding(mui.Space(2.5), mui.Space(3)).Radius(mui.RadiusNotice).
		Background(t.SurfaceAlt).Border(1, t.BorderSoft).AlignItems(ui.Center).
		Role(ui.RoleLink).Label(sentence)
	if strings.TrimSpace(art.Link) != "" {
		open := func() { c.OpenURL(art.Link) }
		notice.Cursor(ui.CursorPointer).Tooltip(art.Link).
			Focusable().FocusRingColor(t.Focus).FocusRingOffset(mui.FocusRingGap).
			OnClick(open).OnShortcut(0, ui.KeyEnter, open)
	}
	notice.Children(func() {
		sfsymbols.Symbol(c, "doc.plaintext").Size(mui.Space(4)).Color(t.Warning).Element.Role(ui.RoleNone)
		mui.Secondary.Apply(ui.Text(c, sentence)).TextColor(t.Muted).Wrap().Grow(1)
		if strings.TrimSpace(art.Link) != "" {
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

// rssTickRail is the paragraph indicator lane: one bar per body block, the
// block at the reading line grown and accented, the bar under the pointer
// grown and darker, the rest thin and muted. A click on a bar takes the reader
// to the paragraph it shows, and the bar under the pointer opens its preview.
// The lane is painted, not built per tick, so a long article costs one draw
// call per frame: the pointer's place in the lane names the bar.
func (w *Window) rssTickRail(c *ui.Context, t mui.Tokens, blocks []core.ArticleBlock) {
	st := &w.rssReader
	rail := ui.Column(c.Key("rss-ticks")).Width(rssTickRailWidth).Shrink(0).
		Role(ui.RoleGroup).Label("Reading position")
	// One block is not a structure to map: a lone bar would read as a stray
	// mark rather than a position in the article, and it answers no click.
	st.hover = -1
	count := 0
	if len(st.ticks) >= 2 {
		count = rssTickCount(len(st.ticks), st.rail.H)
	}
	if count > 0 {
		// The pointer's place is the last frame's: the lane it was over is the
		// one the bars were laid in then, and the frame that follows a move
		// rebuilds from it.
		if x, y, over := rail.PointerPosition(); over {
			st.hover = rssTickIndexAt(st.rail, count, st.rail.X+x, st.rail.Y+y)
		}
	}
	if st.hover >= 0 && rail.Clicked() {
		w.jumpToTick(blocks, count)
	}
	rail.Draw(func(p *ui.Painter, r ui.Rect) {
		st.rail = r
		bars := 0
		if len(st.ticks) >= 2 {
			bars = rssTickCount(len(st.ticks), r.H)
		}
		if bars <= 0 {
			return
		}
		active := rssTickBucket(st.active, len(st.ticks), bars)
		for i, tick := range rssTickRects(r, bars, active, st.hover) {
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
			w.rssTickPreview(c, t, blocks, count)
		})
	}
}

// jumpToTick scrolls the body to the paragraph the bar under the pointer
// shows, and grows that bar in this same frame: the highlight holds on it
// while the body glides, then the reading line takes over where it lands.
func (w *Window) jumpToTick(blocks []core.ArticleBlock, count int) {
	st := &w.rssReader
	block := rssTickBlock(st.hover, len(st.ticks), count)
	if block < 0 || block >= len(blocks) {
		return
	}
	st.jump, st.jumping, st.active = block, true, block
	st.scroll.ScrollTo(0, rssBlockScrollY(st.ticks, st.viewport, st.scroll.Y, block), ui.Smooth)
}

// rssTickPreview is the hover preview: the kind of block the bar under the
// pointer shows and the paragraph's opening words, in a light bubble hanging
// over the reading column beside the bar. It ignores the pointer, so it never
// takes the hover that shows it, and it fades in and out as the pointer comes
// along the rail and leaves it.
func (w *Window) rssTickPreview(c *ui.Context, t mui.Tokens, blocks []core.ArticleBlock, count int) {
	st := &w.rssReader
	if st.hover < 0 || count <= 0 || len(st.ticks) < 2 {
		return
	}
	block := rssTickBlock(st.hover, len(blocks), count)
	if block < 0 || block >= len(blocks) {
		return
	}
	bars := rssTickRects(st.rail, count, -1, st.hover)
	if st.hover >= len(bars) {
		return
	}
	zh := strings.HasPrefix(strings.ToLower(c.Locale()), "zh")
	kind := rssBlockKind(blocks[block], zh)
	words := rssPreviewWords(blocks[block], rssPreviewRunes)
	r := rssPreviewRect(st.rail, bars[st.hover], rssPreviewWidth,
		rssPreviewHeight(rssPreviewLines), rssPreviewGap, rssPreviewMargin)
	bubble := ui.Column(c.Key("rss-tick-preview")).Absolute().Left(r.X).Top(r.Y).Width(r.W).
		Padding(mui.Space(2)).Gap(mui.Space(0.5)).Radius(mui.RadiusNotice).
		Background(t.Surface).Border(mui.Hairline, t.Border).
		Shadow(0, mui.ElevationOffsetY, mui.ElevationBlur, mui.ElevationSpread, t.Shadow).
		IgnoreInput(true).Role(ui.RoleGroup).Label(rssPreviewLabel(kind, zh)).
		// Only its colours and shadow ease: its place is the bar's, which the
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
		mui.Metadata.Apply(ui.Text(c, kind)).TextColor(t.Muted).SingleLine()
		mui.Secondary.Apply(ui.Text(c, words)).TextColor(t.Text).MaxLines(rssPreviewLines).Wrap()
	})
}

// rssBlockKind names a body block in the reader's words, for the rail's
// preview.
func rssBlockKind(block core.ArticleBlock, zh bool) string {
	en, cn := "Paragraph", "段落"
	switch block.Type {
	case core.BlockHeading:
		en, cn = "Heading", "标题"
	case core.BlockBlockquote:
		en, cn = "Quote", "引用"
	case core.BlockCode:
		en, cn = "Code block", "代码块"
	case core.BlockImage:
		en, cn = "Image", "图片"
	case core.BlockList:
		en, cn = "List", "列表"
	}
	if zh {
		return cn
	}
	return en
}

// rssPreviewWords is a block's opening words for the rail's preview: its text
// run together on one line and cut at limit characters, or what a picture
// shows when it carries no words of its own. A block left without any previews
// its kind alone.
func rssPreviewWords(block core.ArticleBlock, limit int) string {
	text := block.Text
	if strings.TrimSpace(text) == "" {
		switch block.Type {
		case core.BlockImage:
			text = block.Alt
			if strings.TrimSpace(text) == "" {
				text = block.Caption
			}
		default:
			text = core.CleanSnippet("", block.HTML, 0)
		}
	}
	text = strings.Join(strings.Fields(text), " ")
	if runes := []rune(text); limit > 0 && len(runes) > limit {
		text = strings.TrimSpace(string(runes[:limit])) + "…"
	}
	return text
}

// rssPreviewLabel names the preview for assistive technology and for the tests
// that place it: the kind of block it shows, and that it is a preview.
func rssPreviewLabel(kind string, zh bool) string {
	if zh {
		return kind + "预览"
	}
	return kind + " preview"
}

// rssBlock renders one structured article block at scale. Every block takes the
// reading column's width, and the element is returned so the caller can record
// the bounds the paragraph rail reads.
func (w *Window) rssBlock(c *ui.Context, t mui.Tokens, art core.RSSArticle, block core.ArticleBlock, index int, scale mui.ArticleType) ui.Element {
	key := "rss-block:" + art.ID + ":" + strconv.Itoa(index)
	switch block.Type {
	case core.BlockHeading:
		level := block.Level
		if level < 1 || level > 6 {
			level = 2
		}
		style := scale.Heading
		if level <= 1 {
			style = scale.Title
		}
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
	opts := netimage.Options{MaxSide: int(max(readerWidth, rssReadingColumn) * 2)}
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
