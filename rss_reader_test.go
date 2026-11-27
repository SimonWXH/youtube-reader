package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/netimage"
	"github.com/simon/youtube-reader/core"
	mui "github.com/simon/youtube-reader/ui"
)

// TestRSSListWidthBounds pins the article list's width contract: its default,
// its 260-480 range, the room the reader's minimum leaves, and the sidebar
// yield that room implies.
func TestRSSListWidthBounds(t *testing.T) {
	if defaultRSSListWidth != 400 {
		t.Fatalf("defaultRSSListWidth = %v, want the 400 default", defaultRSSListWidth)
	}
	for _, tc := range []struct {
		name string
		in   float32
		want float32
	}{
		{"first launch", 0, defaultRSSListWidth},
		{"below the minimum", 120, minRSSListWidth},
		{"inside the range", 360, 360},
		{"above the maximum", 900, maxRSSListWidth},
	} {
		if got := clampRSSListWidth(tc.in); got != tc.want {
			t.Errorf("%s: clampRSSListWidth(%v) = %v, want %v", tc.name, tc.in, got, tc.want)
		}
	}

	// A roomy row keeps the chosen width.
	if got := rssListWidthForRoom(400, 1280-220-splitDividerWidth()); got != 400 {
		t.Fatalf("roomy row: list = %v, want 400", got)
	}
	// A row that cannot hold 480 and the reader's minimum narrows the list.
	rowWidth := float32(700)
	want := rowWidth - splitDividerWidth() - rssReaderMinWidth
	if got := rssListWidthForRoom(maxRSSListWidth, rowWidth); got != want {
		t.Fatalf("tight row: list = %v, want %v", got, want)
	}
	if got := rssListWidthForRoom(maxRSSListWidth, rowWidth); got+rssReaderMinWidth+splitDividerWidth() > rowWidth {
		t.Fatalf("list %v leaves the reader less than its %v minimum in a %v row", got, rssReaderMinWidth, rowWidth)
	}
	// A row narrower than the list's own minimum still keeps that minimum.
	if got := rssListWidthForRoom(400, 200); got != minRSSListWidth {
		t.Fatalf("very narrow row: list = %v, want %v", got, minRSSListWidth)
	}

	// The sidebar yields only to the list's and the reader's minimums, so at
	// 960 pt it can keep its maximum while the list narrows to fit, and at the
	// design width it keeps its stored width.
	if got := boundedRSSSidebarWidth(maxSidebarWidth, 1280); got != maxSidebarWidth {
		t.Fatalf("1280 sidebar = %v, want %v", got, maxSidebarWidth)
	}
	limit := float32(960) - 2*splitDividerWidth() - rssSidebarReserve
	if got := boundedRSSSidebarWidth(maxSidebarWidth, 960); got != min(maxSidebarWidth, limit) {
		t.Fatalf("960 sidebar = %v, want the %v limit", got, min(maxSidebarWidth, limit))
	}
	if got := boundedRSSSidebarWidth(0, 960); got != clampSidebarWidth(0) {
		t.Fatalf("960 sidebar default = %v, want %v", got, clampSidebarWidth(0))
	}
	if got := boundedRSSSidebarWidth(maxSidebarWidth, 320); got != minSidebarWidth {
		t.Fatalf("tiny window sidebar = %v, want %v", got, minSidebarWidth)
	}
	// At 960 pt with the widest sidebar, the list narrows and the reader keeps
	// its floor.
	row := float32(960) - boundedRSSSidebarWidth(maxSidebarWidth, 960) - splitDividerWidth()
	list := rssListWidthForRoom(defaultRSSListWidth, row)
	if row-list-splitDividerWidth() < rssReaderMinWidth {
		t.Fatalf("960 layout: list %v leaves the reader %v, below %v", list, row-list-splitDividerWidth(), rssReaderMinWidth)
	}
}

// TestRSSTickSections pins where the rail's marks come from: an article's
// level-2 headings, in document order, and nothing else. A picture, a video, a
// code block, a quote, and a link are blocks of the page, and a first-,
// third-, fourth- or deeper-level heading is a level of the page, so none of
// them earns a mark.
func TestRSSTickSections(t *testing.T) {
	blocks := rssRailTestArticle().Blocks
	sections := rssSections(blocks)
	if want := []int{2, 8, 13, 18}; !slices.Equal(sections, want) {
		t.Fatalf("sections = %v, want the level-2 headings at %v", sections, want)
	}
	// The fixture holds every block kind the rail must ignore, and every
	// heading level it must ignore, so the exclusion is proven and not implied.
	var kinds []core.BlockType
	var levels []int
	for _, block := range blocks {
		kinds = append(kinds, block.Type)
		if block.Type == core.BlockHeading {
			levels = append(levels, block.Level)
		}
	}
	for _, want := range []core.BlockType{
		core.BlockParagraph, core.BlockImage, core.BlockCode, core.BlockBlockquote, core.BlockLink,
	} {
		if !slices.Contains(kinds, want) {
			t.Fatalf("the fixture holds no %q block to ignore", want)
		}
	}
	for _, level := range []int{1, 3, 4} {
		if !slices.Contains(levels, level) {
			t.Fatalf("the fixture holds no level-%d heading to ignore", level)
		}
	}
	// Every block is marked exactly when it is a level-2 heading.
	for i, block := range blocks {
		want := block.Type == core.BlockHeading && block.Level == 2
		if got := slices.Contains(sections, i); got != want {
			t.Fatalf("block %d (%q level %d) marked = %v, want %v", i, block.Type, block.Level, got, want)
		}
	}
	// The bounds project the same headings in the same order.
	bounds := make([]ui.Rect, len(blocks))
	for i := range bounds {
		bounds[i] = ui.Rect{X: 900, Y: float32(i) * 40, W: 546, H: 30}
	}
	projected := rssSectionBounds(blocks, bounds)
	if len(projected) != len(sections) {
		t.Fatalf("%d section bounds for %d sections", len(projected), len(sections))
	}
	for i, section := range sections {
		if projected[i] != bounds[section] {
			t.Fatalf("section %d is at %v, want its heading's %v", i, projected[i], bounds[section])
		}
	}

	// An article without a level-2 heading has no sections at all.
	withoutSections := []core.ArticleBlock{
		{Type: core.BlockHeading, Level: 1, Text: "A Title"},
		{Type: core.BlockParagraph, Text: "One paragraph of prose."},
		{Type: core.BlockHeading, Level: 3, Text: "A note"},
		{Type: core.BlockParagraph, Text: "Another paragraph of prose."},
	}
	if got := rssSections(withoutSections); len(got) != 0 {
		t.Fatalf("sections of an article without a level-2 heading = %v, want none", got)
	}
	if got := rssSectionBounds(withoutSections, bounds[:len(withoutSections)]); len(got) != 0 {
		t.Fatalf("section bounds without a level-2 heading = %v, want none", got)
	}
	if got := rssSections(nil); len(got) != 0 {
		t.Fatalf("sections of an empty body = %v, want none", got)
	}

	// A section's preview is its first paragraph: the quote the second section
	// opens with is stepped over, and a subsection's heading is not the end of
	// its parent's prose.
	if got, want := rssSectionProse(blocks, 8), 10; got != want {
		t.Fatalf("the second section's prose = block %d, want %d", got, want)
	}
	nested := []core.ArticleBlock{
		{Type: core.BlockHeading, Level: 2, Text: "A Section"},
		{Type: core.BlockHeading, Level: 3, Text: "A Subsection"},
		{Type: core.BlockParagraph, Text: "The section's first prose, under its subsection."},
		{Type: core.BlockHeading, Level: 2, Text: "A Section With No Prose"},
	}
	if got := rssSectionProse(nested, 0); got != 2 {
		t.Fatalf("a section opening with a subsection previews block %d, want the prose at 2", got)
	}
	if got := rssSectionProse(nested, 3); got != -1 {
		t.Fatalf("a section without prose previews block %d, want none", got)
	}
}

// TestRSSTickGeometry pins the rail's geometry: the fixed 8 pt pitch, the three
// mark sizes centred on it, the track's height and its hold inside a tenth and
// a quarter of the reading viewport, and the merge a body too long for the
// ceiling gets instead of a tighter pitch.
func TestRSSTickGeometry(t *testing.T) {
	const viewportH float32 = 800
	lane := ui.Rect{X: 1200, Y: 80, W: rssTickRailWidth, H: viewportH}

	// centre is a mark's middle on the pitch line, whatever its own height.
	centre := func(r ui.Rect) float32 { return r.Y + r.H/2 }

	// The brief's acceptance numbers, pinned as literals so the test rejects a
	// self-consistent drift away from an 8 pt pitch and a 10%-25% band.
	if rssTickPitch != 8 {
		t.Fatalf("the pitch is %v, want the brief's 8", rssTickPitch)
	}
	if rssTickTrackMin != 0.10 || rssTickTrackMax != 0.25 {
		t.Fatalf("the clamp band is %v-%v, want the brief's 0.10-0.25", rssTickTrackMin, rssTickTrackMax)
	}

	t.Run("fixed pitch", func(t *testing.T) {
		for _, count := range []int{3, 5, 20} {
			rects := rssTickRects(lane, count, 0, -1, viewportH)
			if len(rects) != count {
				t.Fatalf("%d marks for %d sections", len(rects), count)
			}
			for i := 1; i < count; i++ {
				if got := centre(rects[i]) - centre(rects[i-1]); got != 8 {
					t.Fatalf("%d marks: marks %d and %d are %v apart, want the fixed 8",
						count, i-1, i, got)
				}
			}
			// The marks never spread over the lane: their span is the pitch's,
			// not the lane's height shared out.
			if got, want := centre(rects[count-1])-centre(rects[0]), float32(count-1)*8; got != want {
				t.Fatalf("%d marks span %v, want %v", count, got, want)
			}
		}
		if got := rssTickRects(lane, 0, 0, -1, viewportH); got != nil {
			t.Fatalf("no sections drew %v", got)
		}
		if got := rssTickRects(ui.Rect{X: 1200, Y: 80}, 4, -1, -1, viewportH); got != nil {
			t.Fatalf("a lane without room drew %v", got)
		}
	})

	t.Run("mark sizes", func(t *testing.T) {
		rects := rssTickRects(lane, 5, 2, 4, viewportH)
		want := []struct{ w, h float32 }{
			{rssTickInactiveW, rssTickInactiveH},
			{rssTickInactiveW, rssTickInactiveH},
			{rssTickActiveW, rssTickActiveH},
			{rssTickInactiveW, rssTickInactiveH},
			{rssTickHoverW, rssTickHoverH},
		}
		for i, tc := range want {
			if got := rects[i]; got.W != tc.w || got.H != tc.h {
				t.Fatalf("mark %d is %vx%v, want %vx%v", i, got.W, got.H, tc.w, tc.h)
			}
		}
		if rssTickInactiveW != 12 || rssTickInactiveH != 2 || rssTickHoverW != 16 || rssTickHoverH != 3 ||
			rssTickActiveW != 18 || rssTickActiveH != 4 {
			t.Fatalf("mark sizes = %vx%v idle, %vx%v hovered, %vx%v current; want 12x2, 16x3 and 18x4",
				rssTickInactiveW, rssTickInactiveH, rssTickHoverW, rssTickHoverH, rssTickActiveW, rssTickActiveH)
		}
		// Each mark is right-aligned on the pane's inset, so the grown ones
		// extend left over the gutter and the column never moves.
		for i, r := range rects {
			if got, want := r.X+r.W, lane.X+lane.W-rssTickInsetX; got != want {
				t.Fatalf("mark %d ends at %v, want %v", i, got, want)
			}
		}
		// The current mark outranks the hovered one: an active mark under the
		// pointer keeps its own size.
		if got := rssTickRects(lane, 5, 2, 2, viewportH)[2]; got.W != rssTickActiveW || got.H != rssTickActiveH {
			t.Fatalf("the current mark under the pointer is %vx%v, want the current %vx%v",
				got.W, got.H, rssTickActiveW, rssTickActiveH)
		}
	})

	t.Run("track height", func(t *testing.T) {
		for _, sections := range []int{1, 4, 40} {
			count := rssTickCount(sections, viewportH)
			height := rssTickTrackHeight(count, viewportH)
			low, high := 0.10*viewportH, 0.25*viewportH
			if height < low || height > high {
				t.Fatalf("%d sections: track %v, want it inside the %v-%v viewport share", sections, height, low, high)
			}
			// The track sits on the lane's middle, and the marks sit on the
			// track's, so both are centred and no end is favoured.
			track := rssTickTrack(lane, count, viewportH)
			if got, want := track.Y+track.H/2, lane.Y+lane.H/2; got != want {
				t.Fatalf("%d sections: the track's middle is %v, want the lane's %v", sections, got, want)
			}
			rects := rssTickRects(lane, count, -1, -1, viewportH)
			if got, want := (centre(rects[0])+centre(rects[count-1]))/2, lane.Y+lane.H/2; got != want {
				t.Fatalf("%d sections: the marks' middle is %v, want the lane's %v", sections, got, want)
			}
			for i, r := range rects {
				if r.Y < track.Y || r.Y+r.H > track.Y+track.H {
					t.Fatalf("%d sections: mark %d %v leaves the track %v", sections, i, r, track)
				}
			}
		}
		// A couple of sections is raised to the floor rather than reading as a
		// stray dot, and the room it adds stays blank at both ends: 10% of 800.
		const floor = 0.10 * viewportH
		if got := rssTickTrackHeight(1, viewportH); got != floor {
			t.Fatalf("one section's track = %v, want the %v floor", got, floor)
		}
		if got := rssTickTrackHeight(4, viewportH); got != floor {
			t.Fatalf("four sections' track = %v, want the %v floor", got, floor)
		}
		// Once the marks outgrow the floor the track follows them a pitch at a
		// time, so the surplus of a short article is never spread over them.
		if got, want := rssTickTrackHeight(10, viewportH), 9*8+rssTickActiveH+2*rssTickPad; got != want {
			t.Fatalf("ten sections' track = %v, want its own %v", got, want)
		}
		// The clamp reads the reading viewport, not the lane it is painted in:
		// a lane taller than the viewport still centres the viewport's share.
		tall := ui.Rect{X: 1200, Y: 40, W: rssTickRailWidth, H: 3 * viewportH}
		clamped := rssTickTrack(tall, 40, viewportH)
		if clamped.H > 0.25*viewportH {
			t.Fatalf("a tall lane's track is %v, want the viewport's %v ceiling", clamped.H, 0.25*viewportH)
		}
		if got, want := clamped.Y+clamped.H/2, tall.Y+tall.H/2; got != want {
			t.Fatalf("a tall lane's track is centred at %v, want the lane's %v", got, want)
		}
	})

	t.Run("overflow merges", func(t *testing.T) {
		const sections = 400
		// A quarter of 800 is 200 pt: 24 gaps of 8 pt plus the tallest 4 pt
		// mark fit, a 25th gap would not, so 25 marks is the ceiling.
		const capacity = 25
		if got := rssTickCapacity(viewportH); got != capacity {
			t.Fatalf("the viewport fits %d marks, want %d", got, capacity)
		}
		count := rssTickCount(sections, viewportH)
		if count != capacity {
			t.Fatalf("%d sections show %d marks, want the ceiling's %d", sections, count, capacity)
		}
		// The merged marks keep the pitch: merging loses marks, it never packs
		// them closer than 8 pt or squeezes them under the ceiling.
		rects := rssTickRects(lane, count, 0, -1, viewportH)
		for i := 1; i < count; i++ {
			if got := centre(rects[i]) - centre(rects[i-1]); got != 8 {
				t.Fatalf("merged marks %d and %d are %v apart, want 8", i-1, i, got)
			}
		}
		if got := rssTickExtent(count); got > 0.25*viewportH {
			t.Fatalf("the merged marks need %v, past the track's %v ceiling", got, 0.25*viewportH)
		}
		// Adjacent sections share a mark, in order, without skipping one.
		last := -1
		for section := 0; section < sections; section++ {
			mark := rssTickBucket(section, sections, count)
			if mark < last || mark > count-1 || mark-last > 1 {
				t.Fatalf("section %d maps to mark %d after %d", section, mark, last)
			}
			last = mark
		}
		if got := rssTickBucket(sections-1, sections, count); got != count-1 {
			t.Fatalf("the last section maps to mark %d, want %d", got, count-1)
		}
		// A mark reads the first section it gathers, so a click never skips a
		// heading, and the mapping round-trips.
		for mark := 0; mark < count; mark++ {
			section := rssTickBlock(mark, sections, count)
			if section < 0 || section >= sections {
				t.Fatalf("mark %d reads section %d", mark, section)
			}
			if got := rssTickBucket(section, sections, count); got != mark {
				t.Fatalf("mark %d reads section %d, which maps to mark %d", mark, section, got)
			}
			if section > 0 && rssTickBucket(section-1, sections, count) == mark {
				t.Fatalf("mark %d reads section %d, though section %d shares it", mark, section, section-1)
			}
		}
		// One capacity's worth of sections is shown whole; one more merges.
		if got := rssTickCount(count, viewportH); got != count {
			t.Fatalf("%d sections show %d marks, want them all", count, got)
		}
		if got := rssTickCount(count+1, viewportH); got != count {
			t.Fatalf("%d sections show %d marks, want the ceiling's %d", count+1, got, count)
		}
		if got := rssTickCount(0, viewportH); got != 0 {
			t.Fatalf("no sections show %d marks", got)
		}
		if got := rssTickCount(5, 0); got != 0 {
			t.Fatalf("a viewport with no height shows %d marks", got)
		}
	})
}

// TestRSSTickInteraction pins the rail's hit test, its section mapping, the
// reading line's current section, where a click sends the body, and where the
// hover preview shows.
func TestRSSTickInteraction(t *testing.T) {
	const viewportH float32 = 800
	lane := ui.Rect{X: 1200, Y: 80, W: rssTickRailWidth, H: viewportH}
	count := 12
	rects := rssTickRects(lane, count, 3, -1, viewportH)
	centre := func(r ui.Rect) float32 { return r.Y + r.H/2 }

	t.Run("mark hit test", func(t *testing.T) {
		for i, r := range rects {
			if got := rssTickIndexAt(lane, count, viewportH, r.X+r.W/2, centre(r)); got != i {
				t.Fatalf("the middle of mark %d hits mark %d", i, got)
			}
			// The whole pitch answers for its mark, upward, so a 2 pt mark is
			// still easy to hit.
			for _, y := range []float32{centre(r) - rssTickPitch/2, centre(r) + rssTickPitch/2 - 0.5} {
				if got := rssTickIndexAt(lane, count, viewportH, r.X+r.W/2, y); got != i {
					t.Fatalf("y %v of mark %d's pitch hits mark %d", y, i, got)
				}
			}
		}
		first, last := centre(rects[0]), centre(rects[count-1])
		for _, tc := range []struct {
			name string
			x, y float32
		}{
			{"above the first mark", rects[0].X + rects[0].W/2, first - rssTickPitch/2 - 1},
			{"below the last mark", rects[count-1].X + rects[count-1].W/2, last + rssTickPitch/2},
			{"the gutter left of the marks", lane.X + 1, centre(rects[5])},
			{"outside the lane", lane.X - 1, centre(rects[5])},
			{"past the lane's edge", lane.X + lane.W, centre(rects[5])},
		} {
			if got := rssTickIndexAt(lane, count, viewportH, tc.x, tc.y); got != -1 {
				t.Fatalf("%s (%v,%v) hits mark %d, want none", tc.name, tc.x, tc.y, got)
			}
		}
		if got := rssTickIndexAt(lane, 0, viewportH, rects[0].X+rects[0].W/2, first); got != -1 {
			t.Fatalf("a lane without marks hits mark %d", got)
		}
		if got := rssTickIndexAt(lane, count, 0, rects[0].X+rects[0].W/2, first); got != -1 {
			t.Fatalf("a viewport without height hits mark %d", got)
		}
	})

	t.Run("section to mark", func(t *testing.T) {
		if got := rssTickBucket(-1, 10, 10); got != -1 {
			t.Fatalf("an invalid section maps to mark %d", got)
		}
		for section := 0; section < 10; section++ {
			if got := rssTickBucket(section, 10, 10); got != section {
				t.Fatalf("one mark per section: section %d -> mark %d", section, got)
			}
		}
		if got := rssTickBucket(0, 100, 10); got != 0 {
			t.Fatalf("the first section maps to mark %d, want 0", got)
		}
		if got := rssTickBucket(99, 100, 10); got != 9 {
			t.Fatalf("the last section maps to mark %d, want 9", got)
		}
		if got := rssTickBlock(-1, 10, 10); got != -1 {
			t.Fatalf("no mark reads section %v", got)
		}
		// A mark past the last reads the last mark's first section.
		if got, want := rssTickBlock(10, 100, 10), rssTickBlock(9, 100, 10); got != want {
			t.Fatalf("a mark past the rail reads section %d, want %d", got, want)
		}
	})

	t.Run("current section", func(t *testing.T) {
		viewport := ui.Rect{X: 900, Y: 100, W: 300, H: 600}
		// The sections' tops, in the window, as three headings of an article
		// scrolled to its top: the reading line is rssReadingProbe below the
		// viewport's top, and the current section is the last one above it.
		bounds := []ui.Rect{
			{X: 900, Y: 40, W: 300, H: 28},
			{X: 900, Y: 340, W: 300, H: 28},
			{X: 900, Y: 640, W: 300, H: 28},
		}
		if got := rssActiveBlock(bounds, viewport); got != 0 {
			t.Fatalf("at the top the current section = %d, want 0", got)
		}
		// Scrolling the body up by 300 puts the second heading on the line.
		scrolled := make([]ui.Rect, len(bounds))
		for i, b := range bounds {
			scrolled[i] = b
			scrolled[i].Y -= 300
		}
		if got := rssActiveBlock(scrolled, viewport); got != 1 {
			t.Fatalf("after a 300 pt scroll the current section = %d, want 1", got)
		}
		// A point before the second heading reaches the line leaves the first
		// current, and the point after takes the second; the third follows the
		// same way, and the last stays current past the article's end.
		for _, tc := range []struct{ scrolled, want int }{{215, 0}, {216, 1}, {515, 1}, {516, 2}, {5000, 2}} {
			shifted := make([]ui.Rect, len(bounds))
			for i, b := range bounds {
				shifted[i] = b
				shifted[i].Y -= float32(tc.scrolled)
			}
			if got := rssActiveBlock(shifted, viewport); got != tc.want {
				t.Fatalf("after %d pt the current section = %d, want %d", tc.scrolled, got, tc.want)
			}
		}
		// Before the first heading has reached the line the first section is
		// the one being read, so the rail always shows a current section.
		below := []ui.Rect{{X: 900, Y: 200, W: 300, H: 28}, {X: 900, Y: 400, W: 300, H: 28}}
		if got := rssActiveBlock(below, viewport); got != 0 {
			t.Fatalf("above every heading the current section = %d, want 0", got)
		}
		// Unlaid-out headings are skipped, and an article without one has no
		// current section at all.
		unlaid := []ui.Rect{{}, {X: 900, Y: 40, W: 300, H: 28}}
		if got := rssActiveBlock(unlaid, viewport); got != 1 {
			t.Fatalf("an unlaid-out heading: current section = %d, want 1", got)
		}
		if got := rssActiveBlock(nil, viewport); got != -1 {
			t.Fatalf("no sections: current section = %d, want -1", got)
		}
	})

	t.Run("click target", func(t *testing.T) {
		viewport := ui.Rect{X: 900, Y: 100, W: 300, H: 600}
		bounds := []ui.Rect{
			{X: 900, Y: 120, W: 300, H: 28},
			{X: 900, Y: 240, W: 300, H: 28},
			{X: 900, Y: 360, W: 300, H: 28},
			{X: 900, Y: 500, W: 300, H: 28},
		}
		// At the top of the article the second heading's content offset is 140,
		// and the body scrolls to put its top on the reading line.
		if got, want := rssBlockScrollY(bounds, viewport, 0, 1), float32(140-rssReadingProbe); got != want {
			t.Fatalf("the second section: scroll to %v, want %v", got, want)
		}
		// The same heading is the same place however far the body has scrolled.
		scrolled := make([]ui.Rect, len(bounds))
		for i, b := range bounds {
			scrolled[i] = b
			scrolled[i].Y -= 60
		}
		if got, want := rssBlockScrollY(scrolled, viewport, 60, 1), float32(140-rssReadingProbe); got != want {
			t.Fatalf("the second section while scrolled: scroll to %v, want %v", got, want)
		}
		// The first heading is above the reading line already: the body goes to
		// its start rather than above it.
		if got := rssBlockScrollY(bounds, viewport, 0, 0); got != 0 {
			t.Fatalf("the first section: scroll to %v, want the start", got)
		}
		for _, tc := range []struct {
			name    string
			section int
			holder  ui.Rect
		}{
			{"no section", -1, viewport},
			{"a section past the body", len(bounds), viewport},
			{"a body the frame has not laid out", 1, ui.Rect{X: 900, Y: 100, W: 300}},
		} {
			if got := rssBlockScrollY(bounds, tc.holder, 300, tc.section); got != 300 {
				t.Fatalf("%s: scroll to %v, want the place it was at (300)", tc.name, got)
			}
		}
	})

	t.Run("preview placement", func(t *testing.T) {
		width, height := rssPreviewWidth, rssPreviewHeight(rssPreviewLines)
		if width != 230 {
			t.Fatalf("the preview is %v pt wide, want 230", width)
		}
		if rssPreviewRadius != 10 {
			t.Fatalf("the preview's corner is %v, want 10", rssPreviewRadius)
		}
		if rssPreviewRadius != mui.RadiusCard {
			t.Fatalf("the preview's corner is %v, want the 10 pt card radius %v", rssPreviewRadius, mui.RadiusCard)
		}
		// The preview's place is in the rail's own coordinates: its right edge the
		// gap left of the widest mark, which ends at the lane's right inset.
		right := rssTickRailWidth - rssTickInsetX - rssTickActiveW - rssPreviewGap
		for i, mark := range rects {
			r := rssPreviewRect(lane, mark, width, height, rssPreviewGap, rssPreviewMargin)
			if r.X+r.W != right {
				t.Fatalf("mark %d: the preview ends at %v, want %v", i, r.X+r.W, right)
			}
			if r.X+r.W > mark.X-lane.X {
				t.Fatalf("mark %d: the preview overlaps its mark at %v", i, mark.X-lane.X)
			}
			if r.W != width || r.H != height {
				t.Fatalf("mark %d: the preview is %vx%v, want %vx%v", i, r.W, r.H, width, height)
			}
			if r.Y < rssPreviewMargin || r.Y+r.H > lane.H-rssPreviewMargin {
				t.Fatalf("mark %d: the preview %v leaves the lane %v", i, r, lane)
			}
			// Its middle is on the mark's wherever that fits, and against the
			// lane's ends otherwise.
			want := centre(mark) - lane.Y - height/2
			switch {
			case want < rssPreviewMargin:
				if r.Y != rssPreviewMargin {
					t.Fatalf("mark %d at the top: preview top %v, want the margin %v", i, r.Y, rssPreviewMargin)
				}
			case want+height > lane.H-rssPreviewMargin:
				if got, wantTop := r.Y, lane.H-rssPreviewMargin-height; got != wantTop {
					t.Fatalf("mark %d at the bottom: preview top %v, want %v", i, got, wantTop)
				}
			default:
				if r.Y != want {
					t.Fatalf("mark %d: preview top %v, want the middle on the mark's (%v)", i, r.Y, want)
				}
			}
		}
		// A lane shorter than the preview keeps the margin at its top.
		short := ui.Rect{X: 1200, Y: 80, W: rssTickRailWidth, H: 40}
		mark := rssTickRects(short, 2, 0, -1, viewportH)[1]
		if r := rssPreviewRect(short, mark, width, height, rssPreviewGap, rssPreviewMargin); r.Y != rssPreviewMargin {
			t.Fatalf("short lane: preview top %v, want %v", r.Y, rssPreviewMargin)
		}
		if got := rssPreviewRect(ui.Rect{X: 1200, Y: 80, W: rssTickRailWidth}, mark, width, height, rssPreviewGap, rssPreviewMargin); got != (ui.Rect{}) {
			t.Fatalf("lane without room: preview %v, want none", got)
		}
		// The room the bubble keeps is its padding and border, the heading's
		// line, and the two lines of the paragraph under it.
		if got, want := height, 2*mui.Space(2)+2*mui.Hairline+mui.PopoverTitle.LineHeight+
			mui.Space(0.5)+float32(rssPreviewLines)*mui.PopoverBody.LineHeight; got != want {
			t.Fatalf("preview height = %v, want the type scale's %v", got, want)
		}
	})

	t.Run("preview words", func(t *testing.T) {
		// The brief asks for the opening 40 to 60 characters, the cut included.
		if rssPreviewRunes < 40 || rssPreviewRunes >= 60 {
			t.Fatalf("rssPreviewRunes = %d, want 40 to 59", rssPreviewRunes)
		}
		if rssPreviewLines != 2 {
			t.Fatalf("the preview shows %d lines of prose, want 2", rssPreviewLines)
		}
		// A paragraph previews its words on one line, however the source breaks
		// them.
		if got := rssPreviewWords(core.ArticleBlock{Text: "  first\n\n\tsecond   line "}, rssPreviewRunes); got != "first second line" {
			t.Fatalf("preview = %q, want one line of words", got)
		}
		long := strings.Repeat("wide ", 40)
		cut := rssPreviewWords(core.ArticleBlock{Text: long}, rssPreviewRunes)
		if runes := []rune(cut); len(runes) != rssPreviewRunes+1 || !strings.HasSuffix(cut, "…") {
			t.Fatalf("long paragraph previews %d runes (%q), want %d and an ellipsis", len(runes), cut, rssPreviewRunes+1)
		}
		// A block that carries only markup still previews its text.
		if got := rssPreviewWords(core.ArticleBlock{HTML: "<p>From <b>markup</b></p>"}, rssPreviewRunes); got != "From markup" {
			t.Fatalf("markup-only preview = %q", got)
		}
		if got := rssPreviewLabel(false); got != "Section preview" {
			t.Fatalf("label = %q", got)
		}
		if got := rssPreviewLabel(true); got != "章节预览" {
			t.Fatalf("zh label = %q", got)
		}
	})
}

// TestRSSReaderContentPadding pins the reading column's centring: the board's
// gutter on a narrow reader and a wider gutter that holds a 680 pt column on a
// wide one.
func TestRSSReaderContentPadding(t *testing.T) {
	if got := rssReaderPadding(400); got != 24 {
		t.Fatalf("narrow reader gutter = %v, want the 24 pt gutter", got)
	}
	if got := rssReaderPadding(680 + 2*24); got != 24 {
		t.Fatalf("exact column gutter = %v, want the 24 pt gutter", got)
	}
	available := float32(1000)
	gutter := rssReaderPadding(available)
	if got := available - 2*gutter; got != 680 {
		t.Fatalf("wide reader column = %v, want the 680 pt cap", got)
	}
}

// TestRSSReaderColumnAndImageSizing pins the reading column's cap and the
// picture fit: a picture never upscales past its own pixels and never grows
// taller than the reader's maximum.
func TestRSSReaderColumnAndImageSizing(t *testing.T) {
	if got := rssReadingWidth(618); got != 546 {
		t.Fatalf("narrow column = %v, want 546 pt inside two 24 pt gutters", got)
	}
	if got := rssReadingWidth(1000); got != 680 {
		t.Fatalf("wide column = %v, want the 680 pt cap", got)
	}
	if got := rssReadingWidth(24); got != 0 {
		t.Fatalf("column with no room = %v, want 0", got)
	}

	for _, tc := range []struct {
		name         string
		column       float32
		w, h         float32
		wantW, wantH float32
	}{
		{name: "landscape fills the column", column: 546, w: 1092, h: 728, wantW: 546, wantH: 364},
		{name: "small picture is not upscaled", column: 700, w: 200, h: 100, wantW: 200, wantH: 100},
		{name: "tall picture is height capped", column: 700, w: 700, h: 1400, wantW: 280, wantH: rssReaderImageMax},
		{name: "zero column", column: 0, w: 100, h: 100},
		{name: "zero picture", column: 700, w: 0, h: 100},
	} {
		gotW, gotH := rssImageSize(tc.column, tc.w, tc.h)
		if gotW != tc.wantW || gotH != tc.wantH {
			t.Errorf("%s: rssImageSize = %vx%v, want %vx%v", tc.name, gotW, gotH, tc.wantW, tc.wantH)
		}
	}
}

// TestRSSArticleBlocksAndLink pins the body's fallback chain and the host the
// original link names.
func TestRSSArticleBlocksAndLink(t *testing.T) {
	withBlocks := core.RSSArticle{Blocks: []core.ArticleBlock{{Type: core.BlockParagraph, Text: "from blocks"}}, ContentHTML: "<p>from html</p>"}
	if blocks := rssArticleBlocks(withBlocks); len(blocks) != 1 || blocks[0].Text != "from blocks" {
		t.Fatalf("blocks should win: %+v", blocks)
	}
	withHTML := core.RSSArticle{ContentHTML: "<p>from html</p>", Snippet: "snippet"}
	if blocks := rssArticleBlocks(withHTML); len(blocks) != 1 || !strings.Contains(blocks[0].Text, "from html") {
		t.Fatalf("content HTML should come second: %+v", blocks)
	}
	emptyContent := core.RSSArticle{ContentHTML: "<div></div>", Snippet: "the snippet behind empty markup"}
	if blocks := rssArticleBlocks(emptyContent); len(blocks) != 1 || blocks[0].Text != "the snippet behind empty markup" {
		t.Fatalf("empty markup should fall back to the snippet: %+v", blocks)
	}
	withSnippet := core.RSSArticle{Snippet: "only the snippet"}
	if blocks := rssArticleBlocks(withSnippet); len(blocks) != 1 || blocks[0].Text != "only the snippet" {
		t.Fatalf("the snippet should be the last resort: %+v", blocks)
	}
	if blocks := rssArticleBlocks(core.RSSArticle{}); len(blocks) != 0 {
		t.Fatalf("an empty article should have no blocks: %+v", blocks)
	}

	for _, tc := range []struct {
		link string
		want string
	}{
		{"https://www.example.com/a/b?c=1", "example.com"},
		{"http://example.com", "example.com"},
		{"example.com/path", "example.com"},
		{"", ""},
	} {
		if got := rssLinkHost(tc.link); got != tc.want {
			t.Errorf("rssLinkHost(%q) = %q, want %q", tc.link, got, tc.want)
		}
	}

	blocks := []core.ArticleBlock{{Type: core.BlockImage, URL: "https://example.com/a.png"}}
	if !rssBlocksHoldImage(blocks, "https://example.com/a.png") {
		t.Error("an inline image should be found")
	}
	if rssBlocksHoldImage(blocks, "https://example.com/b.png") {
		t.Error("a different image should not be found")
	}
}

// rssReaderTestArticle is a full-text article with every block the reader
// styles, long enough that its body scrolls in an 808 pt window.
func rssReaderTestArticle() core.RSSArticle {
	blocks := []core.ArticleBlock{
		{Type: core.BlockHeading, Level: 2, Text: "The quiet return of the open feed"},
		{
			Type: core.BlockParagraph,
			Text: "For a decade the open web's newsreading protocol was treated as plumbing: invisible, unglamorous, and assumed to be behind us. The feeds kept working anyway.",
		},
		{
			Type: core.BlockBlockquote,
			Text: "A reader is a promise that the words arrive without the algorithm deciding which ones matter.",
		},
		{
			Type: core.BlockParagraph,
			Text: "Local-first software changed the calculation. When the subscription list and the read state live on the device, the feed reader becomes a library rather than a window into someone else's server.",
		},
		{Type: core.BlockImage, URL: "https://feeds.example.com/reading-room.png", Alt: "A reading room with tall windows", Caption: "The reading room, photographed by the author."},
		{Type: core.BlockCode, Language: "go", Text: "type Feed struct {\n\tURL   string\n\tTitle string\n}"},
		{Type: core.BlockHeading, Level: 3, Text: "Why the old protocol still fits"},
		{Type: core.BlockList, Text: "• Conditional requests cost nothing when nothing changed\n• Full text is fetched once and kept\n• Read state is local and merges later"},
		{
			Type: core.BlockParagraph,
			Text: "The result feels less like a product and more like a reading room: the list on the left, the page on the right, and a quiet column of marks showing how far into the argument you have come.",
		},
		{
			Type: core.BlockParagraph,
			Text: "That column of marks is the whole point of this screen. Each short mark is one section of the article, and the mark the reading line crosses grows and takes the accent colour.",
		},
		{
			Type: core.BlockParagraph,
			Text: "Nothing about it needs a web view. The text is shaped by the same engine as the rest of the app, so the type, the spacing, and the contrast match every other surface.",
		},
		{
			Type: core.BlockParagraph,
			Text: "When extraction fails, the reader shows what the feed itself supplied and says so in one sentence, with the original page one click away.",
		},
		{
			Type: core.BlockParagraph,
			Text: "The final paragraph exists so the last section has somewhere to end: the end of the article is a destination, not a blank stretch of scroll.",
		},
	}
	return core.RSSArticle{
		ID:          "art-reader",
		FeedID:      "feed-reader",
		Title:       "The Quiet Return of the Open Feed",
		Link:        "https://feeds.example.com/quiet-open-feed",
		Author:      "Ada Lovelace",
		PublishedAt: "2026-11-23T08:00:00Z",
		Snippet:     "For a decade the open web's newsreading protocol was treated as plumbing.",
		ContentHTML: "<p>placeholder</p>",
		IsFullText:  true,
		Blocks:      blocks,
		UpdatedAt:   time.Now().UnixMilli(),
	}
}

// rssReaderTestWindow builds an app with one full-text article and one
// summary-only article and returns a window on the RSS workspace.
func rssReaderTestWindow(t *testing.T) (*App, *Window, *ui.Tester) {
	t.Helper()
	a, w := rssReaderTestApp(t)
	tt := ui.NewTester(w.view, 1280, 808)
	tt.Frame()
	return a, w, tt
}

// rssReaderTestApp seeds the two-article library and a window already on the
// RSS workspace, so a test can choose its own window size.
func rssReaderTestApp(t *testing.T) (*App, *Window) {
	t.Helper()
	stubNetImages(t)
	dir := t.TempDir()
	a := newTestApp(t, dir)
	t.Cleanup(func() { a.Close() })
	ctx := context.Background()

	now := time.Now().UnixMilli()
	feed := core.RSSFeed{ID: "feed-reader", FeedURL: "https://feeds.example.com/rss.xml", Title: "The Long Read", SiteURL: "https://feeds.example.com", UpdatedAt: now}
	if err := a.Store.SaveRSSFeed(ctx, feed); err != nil {
		t.Fatalf("save feed: %v", err)
	}
	full := rssReaderTestArticle()
	summary := core.RSSArticle{
		ID:          "art-summary",
		FeedID:      feed.ID,
		Title:       "A Summary Only Post",
		Link:        "https://feeds.example.com/summary-only",
		Author:      "Grace Hopper",
		PublishedAt: "2026-11-22T09:30:00Z",
		Snippet:     "The feed carried only this sentence, because the site refuses to give up its full text.",
		ContentHTML: "",
		IsFullText:  false,
		UpdatedAt:   now,
	}
	if err := a.Store.SaveRSSArticles(ctx, []core.RSSArticle{full, summary}); err != nil {
		t.Fatalf("save articles: %v", err)
	}
	if err := a.ReloadRSS(); err != nil {
		t.Fatalf("reload rss: %v", err)
	}

	a.now = func() time.Time { return rssReaderTestNow }
	w := &Window{app: a, selectedIndex: new(int)}
	w.selectWorkspace(WorkspaceRSS)
	return a, w
}

// rssReaderTestNow is the clock the reader tests run at, so the header's Today
// and its year rule do not change with the day the suite runs.
var rssReaderTestNow = time.Date(2026, time.December, 1, 9, 0, 0, 0, time.UTC)

// rssHeaderActionsArea is the box the reading header's action group occupies,
// found under either locale's name for it.
func rssHeaderActionsArea(t *testing.T, tt *ui.Tester) ui.Rect {
	t.Helper()
	for _, label := range []string{rssHeaderActionsLabel(false), rssHeaderActionsLabel(true)} {
		if group, ok := tt.AccessByRole(ui.RoleGroup, label); ok {
			return group.Bounds
		}
	}
	t.Fatalf("the reading header has no action group, names = %v", rssAccessNames(tt))
	return ui.Rect{}
}

// rssHeaderActionRoles are the roles a control of the action group can carry:
// a plain button, and a menu button for the one that opens the share menu.
func rssHeaderActionRole(r ui.Role) bool {
	return r == ui.RoleButton || r == ui.RoleMenuButton
}

// rssHeaderActionNames lists the header's action controls in the order they
// draw, by the name each carries for the pointer and the accessibility tree.
func rssHeaderActionNames(t *testing.T, tt *ui.Tester) []string {
	t.Helper()
	area := rssHeaderActionsArea(t, tt)
	names := []string{}
	for _, info := range tt.AccessAll() {
		if !rssHeaderActionRole(info.Role) || !rssBoxHolds(area, info.Bounds) {
			continue
		}
		names = append(names, info.Name)
	}
	return names
}

// rssHeaderAction finds one control of the header's action group by its name.
func rssHeaderAction(t *testing.T, tt *ui.Tester, name string) ui.AccessInfo {
	t.Helper()
	area := rssHeaderActionsArea(t, tt)
	for _, info := range tt.AccessAll() {
		if rssHeaderActionRole(info.Role) && info.Name == name && rssBoxHolds(area, info.Bounds) {
			return info
		}
	}
	t.Fatalf("the reading header has no %q action, names = %v", name, rssHeaderActionNames(t, tt))
	return ui.AccessInfo{}
}

// clickRSSHeaderAction presses one of the header's icon controls.
func clickRSSHeaderAction(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	box := rssHeaderAction(t, tt, name).Bounds
	tt.ClickAt(box.X+box.W/2, box.Y+box.H/2)
	tt.Frame()
}

// rssBoxHolds reports whether a box's centre falls inside area.
func rssBoxHolds(area, box ui.Rect) bool {
	cx, cy := box.X+box.W/2, box.Y+box.H/2
	return cx >= area.X-0.5 && cx <= area.X+area.W+0.5 &&
		cy >= area.Y-0.5 && cy <= area.Y+area.H+0.5
}

// rssAccessNames lists the accessible names the last frame gives, for failure
// messages.
func rssAccessNames(tt *ui.Tester) []string {
	names := []string{}
	for _, info := range tt.AccessAll() {
		if info.Name != "" {
			names = append(names, info.Name)
		}
	}
	return names
}

// stubNetImages keeps picture blocks offline and deterministic: the frame
// holds its place and never depends on a network fetch.
func stubNetImages(t *testing.T) {
	t.Helper()
	netimage.SetClient(&http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("offline in tests")
	})})
	t.Cleanup(func() { netimage.SetClient(http.DefaultClient) })
}

// TestRSSReaderDate pins the reading header's compact stamp: a same-day
// article reads Today, one from this year names its day without the clock,
// and an older one keeps its year.
func TestRSSReaderDate(t *testing.T) {
	now := time.Date(2026, time.November, 25, 15, 0, 0, 0, time.Local)
	for _, tc := range []struct {
		name, raw, want string
	}{
		{name: "today", raw: now.Format(time.RFC3339), want: "Today"},
		{name: "this year", raw: time.Date(2026, time.November, 23, 8, 0, 0, 0, time.Local).Format(time.RFC3339), want: "Nov 23"},
		{name: "another year", raw: time.Date(2025, time.December, 31, 8, 0, 0, 0, time.Local).Format(time.RFC3339), want: "Dec 31, 2025"},
		{name: "empty", raw: "", want: ""},
		{name: "unreadable", raw: "not a date", want: "not a date"},
	} {
		if got := rssReaderDate(tc.raw, now); got != tc.want {
			t.Errorf("%s: rssReaderDate(%q) = %q, want %q", tc.name, tc.raw, got, tc.want)
		}
	}
}

// TestRSSReaderViewRendersArticle covers the reading view end to end: an
// article's header, metadata, block body, section rail, and both actions.
func TestRSSReaderViewRendersArticle(t *testing.T) {
	a, w, tt := rssReaderTestWindow(t)

	// Nothing selected yet: the reader explains how to start.
	if !tt.HasText("No Article Selected") || !tt.HasText("Select an article from the list to read.") {
		t.Fatalf("empty reader copy missing, texts = %v", tt.Texts())
	}
	saveRSSReaderScreenshot(t, tt, "04-reader-empty.png")

	refetched := make(chan string, 1)
	w.onRefetchRSSArticle = func(ctx context.Context, articleID string) (core.RSSArticle, error) {
		refetched <- articleID
		return rssReaderTestArticle(), nil
	}

	// Choosing the article in the middle list opens it in the reader.
	row, ok := tt.Find("The Quiet Return of the Open Feed")
	if !ok {
		t.Fatal("article row not found")
	}
	tt.ClickAt(row.X+row.W/2, row.Y+row.H/2)
	tt.Frame()

	if a.SelectedRSSArticleID != "art-reader" {
		t.Fatalf("SelectedRSSArticleID = %q, want art-reader", a.SelectedRSSArticleID)
	}
	for _, want := range []string{
		"The Long Read", // feed source
		"Ada Lovelace",  // author
		rssReaderDate("2026-11-23T08:00:00Z", rssReaderTestNow), // the article's day
		"The Quiet Return of the Open Feed",                     // title
		"The quiet return of the open feed",                     // heading block
		"Why the old protocol still fits",                       // sub-heading block
		"A reader is a promise that the words arrive",           // quote block
		"type Feed struct",                                      // code block
		"Conditional requests cost nothing",                     // list block
		"Read the original article",                             // permanent way out
		"feeds.example.com",
	} {
		if !tt.HasText(want) {
			t.Errorf("reader is missing %q, texts = %v", want, tt.Texts())
		}
	}
	if tt.HasText("Showing the feed's summary") {
		t.Error("a full-text article must not show the summary notice")
	}
	// The action group is NetNewsWire's six icons, in its order: the read
	// toggle, the star, the original/full-text toggle, share, open in browser,
	// and the refresh.
	if got, want := rssHeaderActionNames(t, tt), []string{
		"Mark as Read (M)", "Star article (S)", "Show Original", "Share", "Open in Browser", "Refetch",
	}; !slices.Equal(got, want) {
		t.Fatalf("header actions = %q, want %q", got, want)
	}
	if got := rssHeaderAction(t, tt, "Share").Bounds.W; got != 24 {
		t.Fatalf("an action box is %v pt wide, want the reference's 24 pt box", got)
	}
	// The rail marks the article's one level-2 heading, and that section is the
	// one being read at the top.
	if got, want := len(rssSections(a.SelectedRSSArticle().Blocks)), 1; got != want {
		t.Fatalf("the fixture's full text holds %d sections, want %d", got, want)
	}
	if got, want := len(w.rssReader.blocks), len(a.SelectedRSSArticle().Blocks); got != want {
		t.Fatalf("body blocks laid out = %d, want one per block (%d)", got, want)
	}
	if w.rssReader.active != 0 {
		t.Fatalf("at the top the current section = %d, want 0", w.rssReader.active)
	}
	// The picture block keeps its frame and is named for what it shows. The
	// stubbed network never delivers the picture, so the frame settles into the
	// failed-image placeholder, which still carries the alt text as its name and
	// is itself the link to the original image.
	deadline := time.Now().Add(5 * time.Second)
	for {
		img, ok := tt.AccessByRole(ui.RoleLink, "A reading room with tall windows")
		if ok && img.Bounds.H > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("picture block = %+v, %v; want a laid-out named frame", img, ok)
		}
		tt.Frame()
		time.Sleep(5 * time.Millisecond)
	}

	saveRSSReaderScreenshot(t, tt, "01-reader-full-text.png")

	// Open in Browser hands the original URL to the system.
	clickRSSHeaderAction(t, tt, "Open in Browser")
	if urls := tt.OpenedURLs(); len(urls) != 1 || urls[0] != "https://feeds.example.com/quiet-open-feed" {
		t.Fatalf("opened URLs = %v, want the article link", urls)
	}

	// Dark mode keeps the same reading column and rail. The clock moves past
	// the appearance fade so the capture is the settled palette.
	tt.SetDark(true)
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	saveRSSReaderScreenshot(t, tt, "03-reader-dark.png")
	tt.SetDark(false)
	tt.Frame()

	// Refetch asks for the open article's full text.
	clickRSSHeaderAction(t, tt, "Refetch")
	select {
	case id := <-refetched:
		if id != "art-reader" {
			t.Fatalf("refetched = %q, want art-reader", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Refetch did not reach the app")
	}
}

// TestRSSReaderSummarizesWhenExtractionFails covers the graceful fallback: the
// feed's summary reads as prose, the fetch that gave up says so in one compact
// line, the retry replaces the ordinary capture action, and the original page
// stays one click away.
func TestRSSReaderSummarizesWhenExtractionFails(t *testing.T) {
	a, w, tt := rssReaderTestWindow(t)

	row, ok := tt.Find("A Summary Only Post")
	if !ok {
		t.Fatal("summary article row not found")
	}
	tt.ClickAt(row.X+row.W/2, row.Y+row.H/2)
	tt.Frame()

	if a.SelectedRSSArticleID != "art-summary" {
		t.Fatalf("SelectedRSSArticleID = %q, want art-summary", a.SelectedRSSArticleID)
	}
	if !tt.HasText("A Summary Only Post") {
		t.Fatalf("summary title missing, texts = %v", tt.Texts())
	}
	if !tt.HasText("The feed carried only this sentence") {
		t.Fatalf("summary body missing, texts = %v", tt.Texts())
	}
	if !tt.HasText("Couldn't fetch the full text. Showing the feed's summary.") {
		t.Fatalf("failure notice missing, texts = %v", tt.Texts())
	}
	if tt.HasText("Showing the feed's summary. Open the original article in your browser for the full text.") {
		t.Fatalf("a failed fetch still offers the browser's call to action, texts = %v", tt.Texts())
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "Retry Full Text"); !ok {
		t.Fatalf("the body toggle is not a retry after a failure, actions = %v", rssHeaderActionNames(t, tt))
	}
	if got := rssHeaderAction(t, tt, "Refetch").Description; got != "Retry Full Text" {
		t.Fatalf("the refresh action's tooltip = %q, want the retry", got)
	}
	if !tt.HasText("Read the original article") {
		t.Fatalf("original article link missing, texts = %v", tt.Texts())
	}
	if a.SelectedRSSArticle().IsFullText {
		t.Fatal("a fetch that gave up marked the article as full text")
	}
	// The snippet is the body, so the reader lays it out; a summary carries no
	// level-2 heading, so the rail has nothing to mark and draws no lane.
	if want := len(rssArticleBlocks(*a.SelectedRSSArticle())); want == 0 || len(w.rssReader.blocks) != want {
		t.Fatalf("body blocks laid out = %d, want the %d snippet blocks", len(w.rssReader.blocks), want)
	}
	if got := len(rssSections(rssArticleBlocks(*a.SelectedRSSArticle()))); got != 0 {
		t.Fatalf("the snippet holds %d sections; the case needs none", got)
	}
	if _, ok := tt.AccessByRole(ui.RoleGroup, "Reading position"); ok {
		t.Fatal("an article without a level-2 heading draws a section rail")
	}
	saveRSSReaderScreenshot(t, tt, "02-reader-summary.png")

	// The permanent original link opens the article too.
	tt.Click("Read the original article")
	tt.Frame()
	if urls := tt.OpenedURLs(); len(urls) != 1 || urls[0] != "https://feeds.example.com/summary-only" {
		t.Fatalf("opened URLs = %v, want the summary article link", urls)
	}
}

// rssDragCopyText drags from inside one rendered text to inside another, as
// a reader selecting words with the pointer, then copies with Cmd+C.
func rssDragCopyText(t *testing.T, tt *ui.Tester, from, to string) string {
	t.Helper()
	start, ok := tt.Find(from)
	if !ok {
		t.Fatalf("text %q not found: %v", from, tt.Texts())
	}
	end, ok := tt.Find(to)
	if !ok {
		t.Fatalf("text %q not found: %v", to, tt.Texts())
	}
	tt.SetClipboard("")
	tx, ty := start.X+2, start.Y+start.H/2
	ex, ey := end.X+end.W-2, end.Y+end.H/2
	tt.Press(tx, ty)
	tt.Move(ex, ey)
	tt.Release(ex, ey)
	tt.Key(ui.Cmd, ui.KeyC)
	return tt.Clipboard()
}

// TestRSSReaderTextSelectsAndCopies follows the real reading path: open a
// full-text article, drag across its prose, and copy. The reading page offers
// a text cursor over its words, and the copied run holds the dragged words,
// including a drag that crosses from one paragraph into the next.
func TestRSSReaderTextSelectsAndCopies(t *testing.T) {
	_, _, tt := rssReaderTestWindow(t)
	openRSSReaderTickArticle(t, tt)

	const first = "For a decade the open web's newsreading protocol was treated as plumbing: invisible, unglamorous, and assumed to be behind us. The feeds kept working anyway."
	const second = "Local-first software changed the calculation. When the subscription list and the read state live on the device, the feed reader becomes a library rather than a window into someone else's server."
	paragraph, ok := tt.Find(first)
	if !ok {
		t.Fatalf("body paragraph not found: %v", tt.Texts())
	}
	tt.Move(paragraph.X+paragraph.W/2, paragraph.Y+paragraph.H/2)
	tt.Frame()
	if got, want := tt.Cursor(), ui.CursorText; got != want {
		t.Fatalf("cursor over the article body = %v, want the text cursor %v", got, want)
	}
	// The selectable page does not absorb its actions: a button keeps its
	// own pointer cursor instead of turning the header into a text field.
	action := rssHeaderAction(t, tt, "Open in Browser").Bounds
	tt.Move(action.X+action.W/2, action.Y+action.H/2)
	tt.Frame()
	if got, want := tt.Cursor(), ui.CursorPointer; got != want {
		t.Fatalf("cursor over the reader action = %v, want the pointer %v", got, want)
	}

	copied := rssDragCopyText(t, tt, first, first)
	if !strings.Contains(copied, "unglamorous") || !strings.Contains(copied, "feeds kept working") {
		t.Fatalf("a drag across one paragraph copied %q", copied)
	}

	copied = rssDragCopyText(t, tt, first, second)
	if !strings.Contains(copied, "unglamorous") || !strings.Contains(copied, "Local-first software") {
		t.Fatalf("a drag across two paragraphs copied %q", copied)
	}

	// The Chinese interface keeps the same selection behaviour: copying is a
	// property of the reading page, not of one locale's labels.
	tt.SetLocale("zh-Hans")
	tt.Frame()
	copied = rssDragCopyText(t, tt, first, second)
	if !strings.Contains(copied, "Local-first software") {
		t.Fatalf("in the Chinese interface a body drag copied %q", copied)
	}
}

// rssRailTestArticle is an article whose structure is the section rail's
// subject: a level-1 heading over a standfirst, four level-2 sections that each
// open with prose, and every block that must earn no mark at all - a picture, a
// code block, a quote, a link, and headings of the third and fourth levels.
// Long enough that its body scrolls in the shipped 1280 x 808 window.
func rssRailTestArticle() core.RSSArticle {
	blocks := []core.ArticleBlock{
		{Type: core.BlockHeading, Level: 1, Text: "The Quiet Return of the Open Feed"},
		{
			Type: core.BlockParagraph,
			Text: "A standfirst belongs to no section at all: it introduces the piece before the first heading has said what the piece is about, and the rail marks the sections rather than the introduction.",
		},
		{Type: core.BlockHeading, Level: 2, Text: "Where the protocol went"},
		{
			Type: core.BlockParagraph,
			Text: "For a decade the open web's newsreading protocol was treated as plumbing: invisible, unglamorous, and assumed to be behind us. The feeds kept working anyway, and the readers that read them kept reading.",
		},
		{
			Type: core.BlockImage, URL: "https://feeds.example.com/reading-room.png",
			Alt: "A reading room with tall windows", Caption: "The reading room, photographed by the author.",
		},
		{Type: core.BlockHeading, Level: 3, Text: "A subsection of the first section"},
		{
			Type: core.BlockParagraph,
			Text: "A subsection is part of its section: the rail marks the section itself and never the level below it, so these words preview under the section's own heading rather than under a mark of their own.",
		},
		{Type: core.BlockCode, Language: "go", Text: "type Feed struct {\n\tURL   string\n\tTitle string\n}"},
		{Type: core.BlockHeading, Level: 2, Text: "Why the old protocol still fits"},
		{
			Type: core.BlockBlockquote,
			Text: "A reader is a promise that the words arrive without the algorithm deciding which ones matter.",
		},
		{
			Type: core.BlockParagraph,
			Text: "Local-first software changed the calculation. When the subscription list and the read state live on the device, the feed reader becomes a library rather than a window into someone else's server.",
		},
		{Type: core.BlockLink, URL: "https://feeds.example.com/spec", Text: "The specification, if you want to read it"},
		{
			Type: core.BlockParagraph,
			Text: "Nothing about it needs a web view. The text is shaped by the same engine as the rest of the app, so the type, the spacing, and the contrast match every other surface.",
		},
		{Type: core.BlockHeading, Level: 2, Text: "A library, not a window"},
		{
			Type: core.BlockParagraph,
			Text: "The result feels less like a product and more like a reading room: the list on the left, the page on the right, and a quiet column of marks showing how far into the argument you have come.",
		},
		{Type: core.BlockHeading, Level: 4, Text: "A note too deep to mark"},
		{
			Type: core.BlockParagraph,
			Text: "A fourth-level heading is a note inside a section rather than a section of its own, so it earns no mark and the paragraph under it is never a preview.",
		},
		{
			Type: core.BlockParagraph,
			Text: "A longer stretch of prose gives the body room to scroll, so a click on a mark has somewhere to land when it puts that section's heading on the reading line.",
		},
		{Type: core.BlockHeading, Level: 2, Text: "What the marks are for"},
		{
			Type: core.BlockParagraph,
			Text: "Each mark is one section, and the mark the reading line is in grows and takes the accent colour, so the rail answers how far through the argument the reader has come.",
		},
		{
			Type: core.BlockParagraph,
			Text: "The marks keep their distance: a fixed pitch reads as a structure, where a spread over the whole lane reads as a progress bar with extra steps, and the scroll already answers progress.",
		},
		{
			Type: core.BlockParagraph,
			Text: "The last paragraph exists so the last section has prose of its own, and so the body can scroll its final heading up to the reading line.",
		},
	}
	return core.RSSArticle{
		ID:          "art-rail",
		Title:       "The Quiet Return of the Open Feed",
		Link:        "https://feeds.example.com/quiet-open-feed",
		Author:      "Ada Lovelace",
		PublishedAt: "2026-11-23T08:00:00Z",
		Snippet:     "A standfirst belongs to no section at all.",
		ContentHTML: "<p>placeholder</p>",
		IsFullText:  true,
		Blocks:      blocks,
		UpdatedAt:   time.Now().UnixMilli(),
	}
}

// rssRailTestWindow opens the rail's own article in the shipped window and lays
// the two frames that record its body and rail.
func rssRailTestWindow(t *testing.T) (*App, *Window, *ui.Tester) {
	t.Helper()
	return rssReaderScopedWindow(t, rssRailTestArticle())
}

// TestRSSReaderSectionsMarkTheArticle checks the rail in a real frame: one mark
// per level-2 heading at the fixed pitch, the lane as tall as the reading
// viewport, and the first section current at the top of the article.
func TestRSSReaderSectionsMarkTheArticle(t *testing.T) {
	a, w, tt := rssRailTestWindow(t)
	blocks := a.SelectedRSSArticle().Blocks
	sections := rssSections(blocks)
	if len(sections) != 4 {
		t.Fatalf("the article holds %d sections, want its four level-2 headings", len(sections))
	}
	rail := rssReaderTickRail(t, tt)
	// The lane stretches with the body, so the height the track is clamped
	// against is the lane's own.
	if rail.H != w.rssReader.viewport.H {
		t.Fatalf("the lane is %v tall, the reading viewport %v", rail.H, w.rssReader.viewport.H)
	}
	marks := rssReaderSectionMarks(t, rail, len(sections))
	if len(marks) != len(sections) {
		t.Fatalf("%d marks for %d sections", len(marks), len(sections))
	}
	for i := 1; i < len(marks); i++ {
		if got := marks[i].Y + marks[i].H/2 - (marks[i-1].Y + marks[i-1].H/2); got != 8 {
			t.Fatalf("marks %d and %d are %v apart, want the fixed 8", i-1, i, got)
		}
	}
	if w.rssReader.active != 0 {
		t.Fatalf("at the top the current section = %d, want 0", w.rssReader.active)
	}
	// The headings that earn no mark still read in the body: the page marks its
	// sections, it does not cut its other levels out.
	for _, heading := range []string{"The Quiet Return of the Open Feed", "A subsection of the first section", "A note too deep to mark"} {
		if !tt.HasText(heading) {
			t.Errorf("the body is missing %q", heading)
		}
	}
	saveRSSTickScreenshot(t, tt, "01-idle-light.png")

	tt.SetDark(true)
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	saveRSSTickScreenshot(t, tt, "02-idle-dark.png")
	tt.SetDark(false)
	tt.Frame()
}

// TestRSSReaderSectionsFollowScroll checks the current section in a real frame:
// scrolling the body to a heading's place makes that section current, and the
// highlight follows down and back as the reader scrolls.
func TestRSSReaderSectionsFollowScroll(t *testing.T) {
	a, w, tt := rssRailTestWindow(t)
	blocks := a.SelectedRSSArticle().Blocks
	sections := rssSections(blocks)
	if len(sections) < 3 {
		t.Fatalf("the article holds %d sections; the case needs three", len(sections))
	}
	if w.rssReader.scroll.MaxY <= 0 {
		t.Fatalf("the article does not scroll in the shipped window: MaxY = %v", w.rssReader.scroll.MaxY)
	}
	if w.rssReader.active != 0 {
		t.Fatalf("at rest the current section = %d, want 0", w.rssReader.active)
	}
	for _, want := range []int{1, 2} {
		target := rssBlockScrollY(w.rssReader.blocks, w.rssReader.viewport, w.rssReader.scroll.Y, sections[want])
		if target > w.rssReader.scroll.MaxY {
			t.Fatalf("section %d's heading needs a %v scroll, past the body's %v", want, target, w.rssReader.scroll.MaxY)
		}
		w.rssReader.scroll.Y = target
		tt.Frame()
		tt.Frame()
		if w.rssReader.active != want {
			t.Fatalf("with section %d's heading on the reading line the current section = %d", want, w.rssReader.active)
		}
		saveRSSTickScreenshot(t, tt, fmt.Sprintf("03-scrolled-section-%d.png", want))
	}
	// Back to the top: the first section is current again.
	w.rssReader.scroll.Y = 0
	tt.Frame()
	tt.Frame()
	if w.rssReader.active != 0 {
		t.Fatalf("back at the top the current section = %d, want 0", w.rssReader.active)
	}
}

// TestRSSReaderSectionMarkClickJumps covers a click on a mark: the body glides
// to that section's heading and lands it on the reading line, and the mark
// turns current in the frame of the click and stays so.
func TestRSSReaderSectionMarkClickJumps(t *testing.T) {
	a, w, tt := rssRailTestWindow(t)
	blocks := a.SelectedRSSArticle().Blocks
	sections := rssSections(blocks)
	rail := rssReaderTickRail(t, tt)
	marks := rssReaderSectionMarks(t, rail, len(sections))
	if len(marks) != len(sections) {
		t.Fatalf("%d marks for %d sections, want one each", len(marks), len(sections))
	}
	if w.rssReader.scroll.MaxY <= 0 {
		t.Fatalf("the article does not scroll: MaxY = %v", w.rssReader.scroll.MaxY)
	}

	// Down the article: the second mark takes the body to its heading's top.
	for _, section := range []int{1, 2} {
		mark := marks[section]
		tt.ClickAt(mark.X+mark.W/2, mark.Y+mark.H/2)
		tt.Frame()
		// The mark grows in the click's own frame, before the body has moved.
		if w.rssReader.active != section || !w.rssReader.jumping {
			t.Fatalf("the frame of the click: current section = %d, jumping = %v; want section %d on its way",
				w.rssReader.active, w.rssReader.jumping, section)
		}
		saveRSSTickScreenshot(t, tt, fmt.Sprintf("04-click-jumping-%d.png", section))
		settleRSSReaderScroll(t, tt, w)
		if w.rssReader.jumping {
			t.Fatal("the jump outlived the glide")
		}
		if w.rssReader.active != section {
			t.Fatalf("after the glide the current section = %d, want the clicked %d", w.rssReader.active, section)
		}
		// The heading's top sits on the reading line, within the brief's 10 pt.
		settled := w.rssReader.blocks[sections[section]].Y - w.rssReader.viewport.Y
		if settled < rssReadingProbe-10 || settled > rssReadingProbe+10 {
			t.Fatalf("section %d's heading sits %v pt below the viewport's top, want %v ± 10",
				section, settled, rssReadingProbe)
		}
		// The accent paints the section's own mark, only the four-pixel current
		// mark being that wide and blue.
		accent := rssAccentMark(t, tt, rail)
		if want := marks[section]; !closeTo(accent.Y+accent.H/2, want.Y+want.H/2, 0.5) {
			t.Fatalf("the current mark is painted at %v, want mark %d at %v", accent.Y+accent.H/2, section, want.Y+want.H/2)
		}
		if accent.H != rssTickActiveH {
			t.Fatalf("the current mark is painted %v pt tall, want %v", accent.H, rssTickActiveH)
		}
		saveRSSTickScreenshot(t, tt, fmt.Sprintf("05-click-arrived-%d.png", section))
	}

	// Back up: the first mark returns the body to the first section.
	first := marks[0]
	tt.ClickAt(first.X+first.W/2, first.Y+first.H/2)
	tt.Frame()
	if w.rssReader.active != 0 || !w.rssReader.jumping {
		t.Fatalf("clicking the first mark: current section = %d, jumping = %v", w.rssReader.active, w.rssReader.jumping)
	}
	settleRSSReaderScroll(t, tt, w)
	if got := w.rssReader.blocks[sections[0]].Y - w.rssReader.viewport.Y; got < rssReadingProbe-10 || got > rssReadingProbe+10 {
		t.Fatalf("the first heading sits %v pt below the viewport's top, want %v ± 10", got, rssReadingProbe)
	}

	// The last mark reaches the end of the article, where the body can go no
	// further: it lands there and the reading line names whatever it finds.
	lastMark := marks[len(marks)-1]
	tt.ClickAt(lastMark.X+lastMark.W/2, lastMark.Y+lastMark.H/2)
	tt.Frame()
	settleRSSReaderScroll(t, tt, w)
	if got, want := w.rssReader.active, rssActiveBlock(rssSectionBounds(blocks, w.rssReader.blocks), w.rssReader.viewport); got != want {
		t.Fatalf("at the end the current section = %d, want the reading line's %d", got, want)
	}
}

// TestRSSReaderSectionPreview covers the hover preview: a section's heading on
// one bold line over the first two lines of its first paragraph, in a bubble of
// the brief's width and corner hanging left of the marks, and gone as the
// pointer leaves the rail.
func TestRSSReaderSectionPreview(t *testing.T) {
	a, w, tt := rssRailTestWindow(t)
	blocks := a.SelectedRSSArticle().Blocks
	sections := rssSections(blocks)
	rail := rssReaderTickRail(t, tt)
	marks := rssReaderSectionMarks(t, rail, len(sections))

	section := 1
	mark := marks[section]
	tt.Move(mark.X+mark.W/2, mark.Y+mark.H/2)
	tt.Frame()
	if w.rssReader.hover != section {
		t.Fatalf("hovering mark %d, the reader has %d", section, w.rssReader.hover)
	}
	// The preview fades in: its frame shows it once the fade has come.
	tt.Advance(mui.MotionPopover + 20*time.Millisecond)
	tt.Frame()

	bubble, ok := tt.AccessByRole(ui.RoleGroup, "Section preview")
	if !ok {
		t.Fatalf("no section preview in %v", tt.Texts())
	}
	// 230 pt wide, its right edge the gap left of the marks, its middle on the
	// mark's, inside the lane's ends.
	if bubble.Bounds.W != rssPreviewWidth || rssPreviewWidth != 230 {
		t.Fatalf("the preview is %v pt wide, want 230", bubble.Bounds.W)
	}
	if want := rail.X + rail.W - rssTickInsetX - rssTickActiveW - rssPreviewGap; !closeTo(bubble.Bounds.X+bubble.Bounds.W, want, 0.5) {
		t.Fatalf("the preview ends at %v, want the gap's %v left of the marks", bubble.Bounds.X+bubble.Bounds.W, want)
	}
	if want := mark.Y + mark.H/2; !closeTo(bubble.Bounds.Y+bubble.Bounds.H/2, want, 0.5) {
		t.Fatalf("the preview's middle is at %v, want the mark's %v", bubble.Bounds.Y+bubble.Bounds.H/2, want)
	}
	if want := rssPreviewHeight(rssPreviewLines); !closeTo(bubble.Bounds.H, want, 0.5) {
		t.Fatalf("the preview is %v tall, want the %v of its heading and two lines", bubble.Bounds.H, want)
	}
	if bubble.Bounds.Y < rail.Y+rssPreviewMargin || bubble.Bounds.Y+bubble.Bounds.H > rail.Y+rail.H-rssPreviewMargin {
		t.Fatalf("the preview %v leaves the rail %v", bubble.Bounds, rail)
	}
	// Its corner is the card radius, not a square: the pixel just inside the
	// corner is not the bubble's surface.
	img := tt.Image()
	if img == nil {
		t.Fatal("no frame to read the preview's corner from")
	}
	surface := img.RGBAAt(int(bubble.Bounds.X+bubble.Bounds.W/2), int(bubble.Bounds.Y+3))
	if corner := img.RGBAAt(int(bubble.Bounds.X+0.5), int(bubble.Bounds.Y+0.5)); corner == surface {
		t.Fatalf("the preview's corner paints %v, the same as its surface: the corner is square", corner)
	}
	// The heading is one line and the prose is two: each text's laid-out box is
	// exactly its lines' height, so anything longer is cut.
	heading := blocks[sections[section]].Text
	title, ok := rssTextIn(tt, bubble.Bounds, heading)
	if !ok {
		t.Fatalf("the preview does not show %q, texts = %v", heading, tt.Texts())
	}
	if title.H != mui.PopoverTitle.LineHeight {
		t.Fatalf("the heading line is %v tall, want its one line's %v", title.H, mui.PopoverTitle.LineHeight)
	}
	words := rssPreviewWords(blocks[rssSectionProse(blocks, sections[section])], rssPreviewRunes)
	prose, ok := rssTextIn(tt, bubble.Bounds, words)
	if !ok {
		t.Fatalf("the preview does not show %q, texts = %v", words, tt.Texts())
	}
	if prose.H != float32(rssPreviewLines)*mui.PopoverBody.LineHeight {
		t.Fatalf("the prose is %v tall, want the two lines' %v",
			prose.H, float32(rssPreviewLines)*mui.PopoverBody.LineHeight)
	}
	saveRSSTickScreenshot(t, tt, "06-hover-light.png")

	// The keys' section is not the next one: along the rail the preview follows
	// the pointer and shows that mark's section.
	other := marks[2]
	tt.Move(other.X+other.W/2, other.Y+other.H/2)
	tt.Frame()
	if w.rssReader.hover != 2 {
		t.Fatalf("hovering mark 2, the reader has %d", w.rssReader.hover)
	}
	next, ok := tt.AccessByRole(ui.RoleGroup, "Section preview")
	if !ok {
		t.Fatalf("no preview over the third mark, texts = %v", tt.Texts())
	}
	if want := other.Y + other.H/2; !closeTo(next.Bounds.Y+next.Bounds.H/2, want, 0.5) {
		t.Fatalf("the second preview's middle is at %v, want its mark's %v", next.Bounds.Y+next.Bounds.H/2, want)
	}

	// The bubble takes no hover of its own: the pointer over it leaves the rail,
	// so the preview goes rather than trapping the pointer.
	tt.Move(next.Bounds.X+next.Bounds.W/2, next.Bounds.Y+next.Bounds.H/2)
	tt.Frame()
	if w.rssReader.hover != -1 {
		t.Fatalf("the pointer on the preview hovers mark %d", w.rssReader.hover)
	}
	tt.Advance(mui.MotionInteraction + 20*time.Millisecond)
	tt.Frame()
	if _, ok := tt.AccessByRole(ui.RoleGroup, "Section preview"); ok {
		t.Fatal("the preview outlived the pointer leaving the rail")
	}

	// The lane's gutter is no mark: no preview there.
	tt.Move(rail.X+1, mark.Y+mark.H/2)
	tt.Frame()
	if w.rssReader.hover != -1 {
		t.Fatalf("the gutter hovers mark %d", w.rssReader.hover)
	}

	// Dark mode keeps the preview's contrast, and it is still the same section.
	tt.Move(mark.X+mark.W/2, mark.Y+mark.H/2)
	tt.Advance(mui.MotionPopover + 20*time.Millisecond)
	tt.Frame()
	if _, ok := tt.AccessByRole(ui.RoleGroup, "Section preview"); !ok {
		t.Fatalf("no preview before dark mode, texts = %v", tt.Texts())
	}
	tt.SetDark(true)
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	if _, ok := tt.AccessByRole(ui.RoleGroup, "Section preview"); !ok {
		t.Fatalf("the preview went out with dark mode, texts = %v", tt.Texts())
	}
	saveRSSTickScreenshot(t, tt, "07-hover-dark.png")
	tt.SetDark(false)
	tt.Frame()

	// A narrower window moves the lane: the preview follows it and stays inside.
	tt.SetSize(1180, 700)
	tt.Frame()
	rail = rssReaderTickRail(t, tt)
	marks = rssReaderSectionMarks(t, rail, len(sections))
	mark = marks[section]
	tt.Move(mark.X+mark.W/2, mark.Y+mark.H/2)
	tt.Frame()
	tt.Advance(mui.MotionPopover + 20*time.Millisecond)
	tt.Frame()
	narrow, ok := tt.AccessByRole(ui.RoleGroup, "Section preview")
	if !ok {
		t.Fatalf("no preview after the window narrowed, texts = %v", tt.Texts())
	}
	if narrow.Bounds.Y < rail.Y+rssPreviewMargin || narrow.Bounds.Y+narrow.Bounds.H > rail.Y+rail.H-rssPreviewMargin {
		t.Fatalf("the narrowed preview %v leaves the rail %v", narrow.Bounds, rail)
	}
	if want := rail.X + rail.W - rssTickInsetX - rssTickActiveW - rssPreviewGap; !closeTo(narrow.Bounds.X+narrow.Bounds.W, want, 0.5) {
		t.Fatalf("the narrowed preview ends at %v, want %v", narrow.Bounds.X+narrow.Bounds.W, want)
	}
	tt.SetSize(1280, 808)
	tt.Frame()

	// 简体中文 names the preview in its own words.
	tt.SetLocale("zh-Hans")
	tt.Frame()
	rail = rssReaderTickRail(t, tt)
	marks = rssReaderSectionMarks(t, rail, len(sections))
	mark = marks[section]
	tt.Move(mark.X+mark.W/2, mark.Y+mark.H/2)
	tt.Advance(mui.MotionPopover + 20*time.Millisecond)
	tt.Frame()
	if _, ok := tt.AccessByRole(ui.RoleGroup, "章节预览"); !ok {
		t.Fatalf("no Chinese section preview, texts = %v", tt.Texts())
	}
	tt.SetLocale("en")
	tt.Frame()
}

// TestRSSReaderSectionRailMerges covers a body the rail cannot keep apart: the
// marks merge, each stands for the sections it gathers, and a click on one takes
// the reader to the first of them.
func TestRSSReaderSectionRailMerges(t *testing.T) {
	blocks := make([]core.ArticleBlock, 0, 80)
	for i := range 40 {
		blocks = append(blocks,
			core.ArticleBlock{Type: core.BlockHeading, Level: 2, Text: fmt.Sprintf("Section %d", i)},
			core.ArticleBlock{
				Type: core.BlockParagraph,
				Text: fmt.Sprintf("Paragraph %d of a long piece that keeps going for as long as the rail can follow it, one mark at a time.", i),
			})
	}
	a, w, tt := rssReaderScopedWindow(t, core.RSSArticle{
		ID: "art-rail-long", Title: "A Long Read", Link: "https://feeds.example.com/long", IsFullText: true, Blocks: blocks,
	})
	sections := rssSections(a.SelectedRSSArticle().Blocks)
	if len(sections) != 40 {
		t.Fatalf("the article holds %d sections, want 40", len(sections))
	}
	rail := rssReaderTickRail(t, tt)
	count := rssTickCount(len(sections), rail.H)
	if count <= 0 || count >= len(sections) {
		t.Fatalf("the rail shows %d marks for %d sections, want fewer marks than sections", count, len(sections))
	}
	marks := rssReaderSectionMarks(t, rail, len(sections))
	if len(marks) != count {
		t.Fatalf("%d marks, want the %d the rail can keep apart", len(marks), count)
	}

	// A merged mark reads, previews, and jumps to the first section it stands
	// for, so no heading is skipped by a click.
	mark := count / 2
	section := rssTickBlock(mark, len(sections), count)
	if section <= 0 {
		t.Fatalf("the middle mark reads section %d", section)
	}
	rect := marks[mark]
	tt.Move(rect.X+rect.W/2, rect.Y+rect.H/2)
	tt.Frame()
	if w.rssReader.hover != mark {
		t.Fatalf("hovering mark %d, the reader has %d", mark, w.rssReader.hover)
	}
	tt.Advance(mui.MotionPopover + 20*time.Millisecond)
	tt.Frame()
	preview, ok := tt.AccessByRole(ui.RoleGroup, "Section preview")
	if !ok {
		t.Fatalf("no preview over mark %d, texts = %v", mark, tt.Texts())
	}
	if _, ok := rssTextIn(tt, preview.Bounds, fmt.Sprintf("Section %d", section)); !ok {
		t.Fatalf("the preview does not name section %d, texts = %v", section, tt.Texts())
	}

	tt.ClickAt(rect.X+rect.W/2, rect.Y+rect.H/2)
	tt.Frame()
	if w.rssReader.active != section || !w.rssReader.jumping {
		t.Fatalf("the click reads section %d (%v jumping), want section %d on its way",
			w.rssReader.active, w.rssReader.jumping, section)
	}
	settleRSSReaderScroll(t, tt, w)
	if w.rssReader.active != section {
		t.Fatalf("after the glide the current section = %d, want %d", w.rssReader.active, section)
	}
	if got := w.rssReader.blocks[sections[section]].Y - w.rssReader.viewport.Y; got < rssReadingProbe-10 || got > rssReadingProbe+10 {
		t.Fatalf("the gathered section's heading sits %v pt below the viewport's top, want %v ± 10", got, rssReadingProbe)
	}
	// The accent paints the merged mark the current section belongs to, not the
	// mark with the section's number.
	accent := rssAccentMark(t, tt, rail)
	want := marks[rssTickBucket(section, len(sections), count)]
	if !closeTo(accent.Y+accent.H/2, want.Y+want.H/2, 0.5) {
		t.Fatalf("the current mark is painted at %v, want mark %d at %v (section %d of %d in %d marks)",
			accent.Y+accent.H/2, rssTickBucket(section, len(sections), count), want.Y+want.H/2,
			section, len(sections), count)
	}
	saveRSSTickScreenshot(t, tt, "08-merged-click.png")
}

// TestRSSReaderSectionRailAbsent covers the articles a rail cannot map: without
// a level-2 heading the lane draws nothing and answers nothing, and a single
// section still gets its one mark.
func TestRSSReaderSectionRailAbsent(t *testing.T) {
	t.Run("no level-2 heading", func(t *testing.T) {
		blocks := []core.ArticleBlock{
			{Type: core.BlockHeading, Level: 1, Text: "A Note"},
			{Type: core.BlockParagraph, Text: "The first paragraph of a piece with no sections of its own."},
			{Type: core.BlockHeading, Level: 3, Text: "A Level-Three Heading"},
			{Type: core.BlockParagraph, Text: "A third-level heading is a level of the page, not a section of it, so the rail has nothing to mark."},
		}
		_, w, tt := rssReaderScopedWindow(t, core.RSSArticle{
			ID: "art-no-sections", Title: "No Sections", Link: "https://feeds.example.com/none", IsFullText: true, Blocks: blocks,
		})
		// The lane is not a rail at all: nothing is named to the accessibility
		// tree, so a screen reader never hears of a rail with no marks.
		if _, ok := tt.AccessByRole(ui.RoleGroup, "Reading position"); ok {
			t.Fatal("an article without a level-2 heading draws a rail")
		}
		// It still holds its width, so the reading column is the width it would
		// have beside a rail.
		if want := rssReadingWidth(w.rssLayout.readerWidth); w.rssReader.blocks[1].W != want {
			t.Fatalf("the reading column is %v wide, want the %v the lane leaves", w.rssReader.blocks[1].W, want)
		}
		// Its place is inert: pointing at it and clicking it answers nothing.
		lane := ui.Rect{X: w.rssReader.viewport.X + w.rssReader.viewport.W, Y: w.rssReader.viewport.Y,
			W: rssTickRailWidth, H: w.rssReader.viewport.H}
		tt.Move(lane.X+lane.W-6, lane.Y+lane.H/2)
		tt.Frame()
		if w.rssReader.hover != -1 {
			t.Fatalf("the empty lane hovers mark %d", w.rssReader.hover)
		}
		if _, ok := tt.AccessByRole(ui.RoleGroup, "Section preview"); ok {
			t.Fatal("the empty lane shows a preview")
		}
		tt.ClickAt(lane.X+lane.W-6, lane.Y+lane.H/2)
		tt.Frame()
		settleRSSReaderScroll(t, tt, w)
		if w.rssReader.jumping || w.rssReader.scroll.Y != 0 || w.rssReader.active != -1 {
			t.Fatalf("the empty lane answered a click: current section = %d, jumping = %v, Y = %v",
				w.rssReader.active, w.rssReader.jumping, w.rssReader.scroll.Y)
		}
		saveRSSTickScreenshot(t, tt, "09-no-sections.png")
	})

	t.Run("one section", func(t *testing.T) {
		blocks := []core.ArticleBlock{
			{Type: core.BlockHeading, Level: 1, Text: "A Short Note"},
			{Type: core.BlockParagraph, Text: "The standfirst of a piece that carries one section only."},
			{Type: core.BlockHeading, Level: 2, Text: "The Only Section"},
			{Type: core.BlockParagraph, Text: "One section is enough structure for one mark, and its words are the mark's preview."},
		}
		_, w, tt := rssReaderScopedWindow(t, core.RSSArticle{
			ID: "art-one-section", Title: "One Section", Link: "https://feeds.example.com/one", IsFullText: true, Blocks: blocks,
		})
		rail := rssReaderTickRail(t, tt)
		marks := rssReaderSectionMarks(t, rail, 1)
		if len(marks) != 1 {
			t.Fatalf("%d marks for one section, want 1", len(marks))
		}
		// The single mark sits in the middle of the lane, on the track the floor
		// raised around it.
		if got, want := marks[0].Y+marks[0].H/2, rail.Y+rail.H/2; !closeTo(got, want, 0.5) {
			t.Fatalf("the only mark's middle is %v, want the lane's %v", got, want)
		}
		if w.rssReader.active != 0 {
			t.Fatalf("the current section = %d, want the only one", w.rssReader.active)
		}
		tt.Move(marks[0].X+marks[0].W/2, marks[0].Y+marks[0].H/2)
		tt.Frame()
		tt.Advance(mui.MotionPopover + 20*time.Millisecond)
		tt.Frame()
		preview, ok := tt.AccessByRole(ui.RoleGroup, "Section preview")
		if !ok {
			t.Fatalf("no preview over the only mark, texts = %v", tt.Texts())
		}
		if _, ok := rssTextIn(tt, preview.Bounds, "The Only Section"); !ok {
			t.Fatalf("the preview does not name the section, texts = %v", tt.Texts())
		}
		// Clicking it takes the body to its own heading, which is already the
		// section being read.
		tt.ClickAt(marks[0].X+marks[0].W/2, marks[0].Y+marks[0].H/2)
		tt.Frame()
		settleRSSReaderScroll(t, tt, w)
		if w.rssReader.jumping || w.rssReader.active != 0 {
			t.Fatalf("the only mark's click: current section = %d, jumping = %v", w.rssReader.active, w.rssReader.jumping)
		}
	})
}

// openRSSReaderTickArticle opens the fixture's full-text article in the reader
// and lays the frames that record its blocks and rail.
func openRSSReaderTickArticle(t *testing.T, tt *ui.Tester) {
	t.Helper()
	row, ok := tt.Find("The Quiet Return of the Open Feed")
	if !ok {
		t.Fatal("article row not found")
	}
	tt.ClickAt(row.X+row.W/2, row.Y+row.H/2)
	tt.Frame()
	tt.Frame()
}

// rssReaderTickRail is the section rail's box in the last frame, as the group
// it labels shows it.
func rssReaderTickRail(t *testing.T, tt *ui.Tester) ui.Rect {
	t.Helper()
	rail, ok := tt.AccessByRole(ui.RoleGroup, "Reading position")
	if !ok || rail.Bounds.H <= 0 || rail.Bounds.W <= 0 {
		t.Fatalf("rail = %+v, %v; want a laid-out lane", rail.Bounds, ok)
	}
	return rail.Bounds
}

// rssReaderSectionMarks is where a rail's marks show in the window, as the
// rail's own geometry draws them. The lane stretches with the reading viewport,
// so its height is the viewport height the track is clamped against.
func rssReaderSectionMarks(t *testing.T, rail ui.Rect, sectionCount int) []ui.Rect {
	t.Helper()
	count := rssTickCount(sectionCount, rail.H)
	if count <= 0 {
		t.Fatalf("a rail of %v shows no marks for %d sections", rail, sectionCount)
	}
	return rssTickRects(rail, count, -1, -1, rail.H)
}

// rssTextIn is the box of the text a frame shows inside box, for the text a
// preview shows a second time over the reading column.
func rssTextIn(tt *ui.Tester, box ui.Rect, text string) (ui.Rect, bool) {
	for _, node := range tt.AccessAll() {
		if node.Name == text && rectHolds(box, node.Bounds, 0.5) {
			return node.Bounds, true
		}
	}
	return ui.Rect{}, false
}

// rssAccentMark is where the last frame painted the current section's mark: the
// rows of the lane whose ink is the accent's blue rather than the muted grey of
// the other marks. The marks are painted, not built, so the pixels are the only
// proof of which one the reader grew.
func rssAccentMark(t *testing.T, tt *ui.Tester, lane ui.Rect) ui.Rect {
	t.Helper()
	img := tt.Image()
	if img == nil {
		t.Fatal("no frame to read the rail from")
	}
	left := int(lane.X+lane.W) - int(rssTickInsetX) - int(rssTickActiveW)
	right := int(lane.X + lane.W)
	top, bottom := -1, -1
	for y := int(lane.Y); y < int(lane.Y+lane.H); y++ {
		for x := left; x <= right; x++ {
			c := img.RGBAAt(x, y)
			if int(c.B)-int(c.R) > 40 {
				if top < 0 {
					top = y
				}
				bottom = y
				break
			}
		}
	}
	if top < 0 {
		t.Fatal("the rail painted no accent mark")
	}
	return ui.Rect{X: lane.X, Y: float32(top), W: lane.W, H: float32(bottom - top + 1)}
}

// settleRSSReaderScroll advances the tester's clock until the body's glide
// arrives, laying each frame it passes.
func settleRSSReaderScroll(t *testing.T, tt *ui.Tester, w *Window) {
	t.Helper()
	for range 60 {
		if !w.rssReader.scroll.Scrolling() {
			tt.Frame()
			return
		}
		tt.Advance(100 * time.Millisecond)
		tt.Frame()
	}
	t.Fatalf("the body's glide never arrived: %+v", w.rssReader.scroll)
}

// rssReaderScopedWindow opens a reader on one article of its own, for the
// bodies the shared fixture does not have.
func rssReaderScopedWindow(t *testing.T, art core.RSSArticle) (*App, *Window, *ui.Tester) {
	t.Helper()
	stubNetImages(t)
	a := newTestApp(t, t.TempDir())
	t.Cleanup(func() { a.Close() })
	ctx := context.Background()
	now := time.Now().UnixMilli()
	feed := core.RSSFeed{ID: "feed-scoped", FeedURL: "https://feeds.example.com/scoped.xml", Title: "Scoped Reads", UpdatedAt: now}
	if err := a.Store.SaveRSSFeed(ctx, feed); err != nil {
		t.Fatalf("save feed: %v", err)
	}
	art.FeedID = feed.ID
	art.UpdatedAt = now
	if err := a.Store.SaveRSSArticles(ctx, []core.RSSArticle{art}); err != nil {
		t.Fatalf("save article: %v", err)
	}
	if err := a.ReloadRSS(); err != nil {
		t.Fatalf("reload rss: %v", err)
	}
	w := &Window{app: a, selectedIndex: new(int)}
	w.selectWorkspace(WorkspaceRSS)
	a.SelectRSSArticle(art.ID)
	tt := ui.NewTester(w.view, 1280, 808)
	tt.Frame()
	tt.Frame()
	return a, w, tt
}

// closeTo reports whether got is want within tolerance, for the DIPs a laid-out
// box rounds by.
func closeTo(got, want, tolerance float32) bool {
	return got >= want-tolerance && got <= want+tolerance
}

// rectHolds reports whether inner sits inside outer, within tolerance.
func rectHolds(outer, inner ui.Rect, tolerance float32) bool {
	return inner.X >= outer.X-tolerance && inner.Y >= outer.Y-tolerance &&
		inner.X+inner.W <= outer.X+outer.W+tolerance && inner.Y+inner.H <= outer.Y+outer.H+tolerance
}

// saveRSSTickScreenshot writes the rendered frame to the task's proof
// directory, outside the repository, for visual inspection.
func saveRSSTickScreenshot(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	dir := "/Volumes/ZHITAI_1TB/Projects/firstmate/data/yt-air-rail/screenshots"
	if fi, err := os.Stat(filepath.Dir(dir)); err != nil || !fi.IsDir() {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Logf("screenshot dir: %v", err)
		return
	}
	img := tt.Image()
	if img == nil {
		return
	}
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Logf("screenshot file: %v", err)
		return
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Logf("screenshot encode: %v", err)
	}
}

// TestRSSReaderSelectionResetsScroll checks each article opens at its own top
// and that a closed reader forgets the last article's place.
func TestRSSReaderSelectionResetsScroll(t *testing.T) {
	a, w, tt := rssReaderTestWindow(t)

	row, ok := tt.Find("The Quiet Return of the Open Feed")
	if !ok {
		t.Fatal("article row not found")
	}
	tt.ClickAt(row.X+row.W/2, row.Y+row.H/2)
	tt.Frame()
	tt.Frame()
	w.rssReader.scroll.Y = 600
	tt.Frame()

	if w.rssReader.scroll.Y <= 0 {
		t.Fatalf("body did not scroll: Y = %v", w.rssReader.scroll.Y)
	}
	if w.rssReader.articleID != "art-reader" {
		t.Fatalf("reader article = %q", w.rssReader.articleID)
	}

	// Selecting the other article starts its body at the top.
	row, ok = tt.Find("A Summary Only Post")
	if !ok {
		t.Fatal("summary article row not found")
	}
	tt.ClickAt(row.X+row.W/2, row.Y+row.H/2)
	tt.Frame()
	if w.rssReader.articleID != "art-summary" {
		t.Fatalf("reader article = %q, want art-summary", w.rssReader.articleID)
	}
	if w.rssReader.scroll.Y != 0 {
		t.Fatalf("new article inherited scroll %v, want 0", w.rssReader.scroll.Y)
	}
	if a.SelectedRSSArticleID != "art-summary" {
		t.Fatalf("SelectedRSSArticleID = %q", a.SelectedRSSArticleID)
	}

	// Leaving the page frees the live reader, and coming back restores the open
	// article with its place, so a page switch does not lose the reading spot.
	w.selectWorkspace(WorkspaceVideos)
	tt.Frame()
	if w.rssReader.articleID != "" || len(w.rssReader.blocks) != 0 {
		t.Fatalf("reader kept live state after leaving RSS: %+v", w.rssReader)
	}
	w.selectWorkspace(WorkspaceRSS)
	tt.Frame()
	if w.rssReader.articleID != "art-summary" {
		t.Fatalf("restored reader article = %q, want art-summary", w.rssReader.articleID)
	}
	if want := a.settingsSnapshot().RSSListWidth; w.rssLayout.listWidth != initialRSSListWidth(want) {
		t.Fatalf("restored list width = %v, want %v", w.rssLayout.listWidth, want)
	}
}

// TestRSSReaderKeepsScrollAcrossWorkspaces checks the reading place survives a
// trip to another workspace: the page keeps its own scroll, like the list's.
func TestRSSReaderKeepsScrollAcrossWorkspaces(t *testing.T) {
	_, w, tt := rssReaderTestWindow(t)
	row, ok := tt.Find("The Quiet Return of the Open Feed")
	if !ok {
		t.Fatal("article row not found")
	}
	tt.ClickAt(row.X+row.W/2, row.Y+row.H/2)
	tt.Frame()
	tt.Frame()
	if w.rssReader.scroll.MaxY <= 0 {
		t.Fatalf("body does not scroll: MaxY = %v", w.rssReader.scroll.MaxY)
	}
	w.rssReader.scroll.Y = w.rssReader.scroll.MaxY
	tt.Frame()
	place := w.rssReader.scroll.Y

	w.selectWorkspace(WorkspaceVideos)
	tt.Frame()
	w.selectWorkspace(WorkspaceRSS)
	tt.Frame()
	if got := w.rssReader.scroll.Y; got != place {
		t.Fatalf("reading place = %v, want %v", got, place)
	}
	if w.rssReader.articleID != "art-reader" {
		t.Fatalf("restored reader article = %q", w.rssReader.articleID)
	}
}

// TestRSSListDividerDragClampsAndPersists drags the list/reader seam inside its
// range, checks the reader keeps its minimum, and reads the saved width back
// the way a relaunch does.
func TestRSSListDividerDragClampsAndPersists(t *testing.T) {
	a, w, tt := rssReaderTestWindow(t)

	line, ok := tt.Find("Resize article list")
	if !ok {
		t.Fatal("article list divider not found")
	}
	if got := w.rssLayout.listWidth; got != defaultRSSListWidth {
		t.Fatalf("list starts at %v, want %v", got, defaultRSSListWidth)
	}

	// Drag right 40 pt: the list widens, the reader gives the room.
	tt.Press(line.X, 300)
	tt.Move(line.X+40, 300)
	tt.Frame()
	tt.Release(line.X+40, 300)
	tt.Frame()

	want := float32(defaultRSSListWidth + 40)
	if got := w.rssLayout.listWidth; got != want {
		t.Fatalf("list after drag = %v, want %v", got, want)
	}
	if got := a.settingsSnapshot().RSSListWidth; got != want {
		t.Fatalf("saved list width = %v, want %v", got, want)
	}
	readerWidth := w.rssLayout.readerWidth
	if readerWidth < rssReaderMinWidth {
		t.Fatalf("reader width = %v, below its %v minimum", readerWidth, rssReaderMinWidth)
	}

	// Far past the maximum the list stops at 480.
	line, _ = tt.Find("Resize article list")
	tt.Press(line.X, 300)
	tt.Move(line.X+1000, 300)
	tt.Frame()
	tt.Release(line.X+1000, 300)
	tt.Frame()
	if got := w.rssLayout.listWidth; got != maxRSSListWidth {
		t.Fatalf("list at the far right = %v, want %v", got, maxRSSListWidth)
	}
	if got := w.rssLayout.shownWidth; got != maxRSSListWidth {
		t.Fatalf("laid-out list at the far right = %v, want %v", got, maxRSSListWidth)
	}

	// Far past the minimum it stops at 260.
	line, _ = tt.Find("Resize article list")
	tt.Press(line.X, 300)
	tt.Move(line.X-1000, 300)
	tt.Frame()
	tt.Release(line.X-1000, 300)
	tt.Frame()
	if got := w.rssLayout.listWidth; got != minRSSListWidth {
		t.Fatalf("list at the far left = %v, want %v", got, minRSSListWidth)
	}
	if got := a.settingsSnapshot().RSSListWidth; got != minRSSListWidth {
		t.Fatalf("saved list width = %v, want %v", got, minRSSListWidth)
	}

	// The width a relaunch reads is the one on disk.
	data, err := os.ReadFile(filepath.Join(a.Dir, "settings.json"))
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}
	var disk core.Settings
	if err := json.Unmarshal(data, &disk); err != nil {
		t.Fatalf("decode settings.json: %v", err)
	}
	if disk.RSSListWidth != minRSSListWidth {
		t.Fatalf("settings.json RSSListWidth = %v, want %v", disk.RSSListWidth, minRSSListWidth)
	}

	a.Settings = disk
	reopened := &Window{app: a, selectedIndex: new(int)}
	reopened.selectWorkspace(WorkspaceRSS)
	reopenedTT := ui.NewTester(reopened.view, 1280, 808)
	reopenedTT.Frame()
	if got := reopened.rssLayout.listWidth; got != minRSSListWidth {
		t.Fatalf("relaunched list width = %v, want %v", got, minRSSListWidth)
	}
	if got := reopened.rssLayout.readerWidth; got < rssReaderMinWidth {
		t.Fatalf("relaunched reader width = %v, below its minimum", got)
	}
}

// TestRSSListDividerDoubleClickResets checks the seam's reset gesture.
func TestRSSListDividerDoubleClickResets(t *testing.T) {
	_, w, tt := rssReaderTestWindow(t)
	w.setRSSListWidth(maxRSSListWidth, 1280)
	tt.Frame()
	if got := w.rssLayout.listWidth; got != maxRSSListWidth {
		t.Fatalf("list = %v, want %v", got, maxRSSListWidth)
	}
	line, ok := tt.Find("Resize article list")
	if !ok {
		t.Fatal("article list divider not found")
	}
	tt.ClickAt(line.X, 300)
	tt.ClickAt(line.X, 300)
	tt.Frame()
	if got := w.rssLayout.listWidth; got != defaultRSSListWidth {
		t.Fatalf("list after double click = %v, want %v", got, defaultRSSListWidth)
	}
}

// TestRSSReaderHidesVideoContent keeps the workspace boundary honest: the RSS
// page shows article material and none of the video library.
func TestRSSReaderHidesVideoContent(t *testing.T) {
	a, _, tt := rssReaderTestWindow(t)
	a.Videos = []core.Video{{ID: "v-1", ChannelID: "ch-1", ChannelTitle: "Go Time", Title: "Go 1.24 Deep Dive", PublishedAt: "2026-01-01T00:00:00Z"}}
	row, ok := tt.Find("The Quiet Return of the Open Feed")
	if !ok {
		t.Fatal("article row not found")
	}
	tt.ClickAt(row.X+row.W/2, row.Y+row.H/2)
	tt.Frame()

	for _, forbidden := range []string{"Go 1.24 Deep Dive", "Go Time", "Unwatched videos"} {
		if tt.HasText(forbidden) {
			t.Fatalf("RSS reader shows video material %q", forbidden)
		}
	}
}

// TestRSSReaderBilingualHeader checks the header and actions switch language.
func TestRSSReaderBilingualHeader(t *testing.T) {
	_, w, tt := rssReaderTestWindow(t)
	tt.SetLocale("zh")
	row, ok := tt.Find("The Quiet Return of the Open Feed")
	if !ok {
		t.Fatal("article row not found")
	}
	tt.ClickAt(row.X+row.W/2, row.Y+row.H/2)
	tt.Frame()
	if !tt.HasText("阅读原文") {
		t.Errorf("Chinese reader is missing the original link, texts = %v", tt.Texts())
	}
	if got, want := rssHeaderActionNames(t, tt), []string{
		"标记为已读 (M)", "标星 (S)", "显示原文", "分享", "在浏览器中打开", "重新抓取",
	}; !slices.Equal(got, want) {
		t.Fatalf("Chinese header actions = %q, want %q", got, want)
	}
	_ = w
	tt.SetLocale("en")
	tt.Frame()
	if got, want := rssHeaderActionNames(t, tt), []string{
		"Mark as Read (M)", "Star article (S)", "Show Original", "Share", "Open in Browser", "Refetch",
	}; !slices.Equal(got, want) {
		t.Fatalf("reader did not return to English: actions = %q, want %q", got, want)
	}
}

// TestRSSThreeColumnLayoutAtMinimumWidth pins the three columns at the
// minimum 960 pt window: every column keeps its floor, the row adds up to the
// window, and the reader still shows its body beside the rail.
func TestRSSThreeColumnLayoutAtMinimumWidth(t *testing.T) {
	a, w := rssReaderTestApp(t)
	a.Settings.SidebarWidth = maxSidebarWidth
	a.Settings.RSSListWidth = maxRSSListWidth
	tt := ui.NewTester(w.view, 960, 808)
	tt.Frame()

	if got := w.sidebarWidth; got < minSidebarWidth || got > maxSidebarWidth {
		t.Fatalf("sidebar = %v, want %v..%v", got, minSidebarWidth, maxSidebarWidth)
	}
	if got := w.rssLayout.shownWidth; got < minRSSListWidth || got > maxRSSListWidth {
		t.Fatalf("list = %v, want %v..%v", got, minRSSListWidth, maxRSSListWidth)
	}
	if got := w.rssLayout.listWidth; got != maxRSSListWidth {
		t.Fatalf("a tight frame overwrote the stored list width: %v", got)
	}
	if got := w.rssLayout.readerWidth; got < rssReaderMinWidth {
		t.Fatalf("reader = %v, want at least %v", got, rssReaderMinWidth)
	}
	total := w.sidebarWidth + splitDividerWidth() + w.rssLayout.shownWidth + splitDividerWidth() + w.rssLayout.readerWidth
	if diff := total - 960; diff > 0.5 || diff < -0.5 {
		t.Fatalf("columns add up to %v, want the 960 pt window", total)
	}

	// Dragging the sidebar to its widest still leaves both right columns
	// their floors, and the new width is remembered.
	side, ok := tt.Find("Resize sidebar")
	if !ok {
		t.Fatal("sidebar divider not found at the minimum width")
	}
	tt.Press(side.X, 200)
	tt.Move(side.X+1000, 200)
	tt.Frame()
	tt.Release(side.X+1000, 200)
	tt.Frame()
	if got := w.sidebarWidth; got != maxSidebarWidth {
		t.Fatalf("dragged sidebar = %v, want %v", got, maxSidebarWidth)
	}
	if got := a.settingsSnapshot().SidebarWidth; got != maxSidebarWidth {
		t.Fatalf("saved sidebar = %v, want %v", got, maxSidebarWidth)
	}
	if got := w.rssLayout.readerWidth; got < rssReaderMinWidth {
		t.Fatalf("reader after the sidebar drag = %v, below %v", got, rssReaderMinWidth)
	}
	if got := w.rssLayout.shownWidth; got < minRSSListWidth {
		t.Fatalf("list after the sidebar drag = %v, below %v", got, minRSSListWidth)
	}

	row, ok := tt.Find("The Quiet Return of the Open Feed")
	if !ok {
		t.Fatal("article row not found at the minimum width")
	}
	tt.ClickAt(row.X+row.W/2, row.Y+row.H/2)
	tt.Frame()
	tt.Frame()
	if !tt.HasText("The quiet return of the open feed") {
		t.Fatalf("reader body missing at the minimum width, texts = %v", tt.Texts())
	}
	lane, ok := tt.AccessByRole(ui.RoleGroup, "Reading position")
	if !ok {
		t.Fatal("reading rail missing at the minimum width")
	}
	for i, block := range w.rssReader.blocks {
		if block.X+block.W > lane.Bounds.X {
			t.Fatalf("block %d runs into the rail: %v at %v", i, block, lane.Bounds)
		}
	}
	saveRSSReaderScreenshot(t, tt, "05-reader-minimum-width.png")
}

// TestRSSReadingColumnStaysNarrowOnAWideWindow checks the reading column's
// 680 pt cap and centring: a wide window grows the gutters, not the line
// length.
func TestRSSReadingColumnStaysNarrowOnAWideWindow(t *testing.T) {
	_, w := rssReaderTestApp(t)
	tt := ui.NewTester(w.view, 1680, 900)
	tt.Frame()
	row, ok := tt.Find("The Quiet Return of the Open Feed")
	if !ok {
		t.Fatal("article row not found")
	}
	tt.ClickAt(row.X+row.W/2, row.Y+row.H/2)
	tt.Frame()
	tt.Frame()

	if len(w.rssReader.blocks) < 2 {
		t.Fatal("rail has no blocks to measure")
	}
	column := w.rssReader.blocks[0].W
	if column != 680 {
		t.Fatalf("reading column = %v, want the 680 pt cap", column)
	}
	body := w.rssReader.viewport
	if body.W < column+2*24 {
		t.Fatalf("body is %v wide for a %v column; the gutters did not grow", body.W, column)
	}
	saveRSSReaderScreenshot(t, tt, "06-reader-wide-window.png")
}

// TestRSSReaderDegradedNoticeClickAndBilingual verifies that when viewing a degraded
// summary-only article, the banner shows bilingual text and clicking it opens the original link.
func TestRSSReaderDegradedNoticeClickAndBilingual(t *testing.T) {
	a, _, tt := rssReaderTestWindow(t)
	// The notice is the settled state of a feed that does not fetch its
	// articles' text, so the summary is what the reader opens on.
	if err := a.SetRSSFeedAutoFullText("feed-reader", false); err != nil {
		t.Fatalf("turn the feed's full-text fetch off: %v", err)
	}

	row, ok := tt.Find("A Summary Only Post")
	if !ok {
		t.Fatal("summary article row not found")
	}
	tt.ClickAt(row.X+row.W/2, row.Y+row.H/2)
	tt.Frame()

	// 1. English check
	tt.SetLocale("en")
	tt.Frame()
	const noticeEnText = "Showing the feed's summary. Open the original article in your browser for the full text."
	noticeEn, ok := tt.Find(noticeEnText)
	if !ok {
		t.Fatalf("English summary notice missing, texts = %v", tt.Texts())
	}
	for range 100 {
		if tt.Focused(noticeEnText) {
			break
		}
		tt.Key(0, ui.KeyTab)
	}
	if !tt.Focused(noticeEnText) {
		t.Fatalf("the summary notice never took the keyboard focus, texts = %v", tt.Texts())
	}
	tt.Key(0, ui.KeyEnter)
	if urls := tt.OpenedURLs(); len(urls) != 1 || urls[0] != "https://feeds.example.com/summary-only" {
		t.Fatalf("opened URLs after Enter on the summary notice = %v, want https://feeds.example.com/summary-only", urls)
	}
	noticeEn, ok = tt.Find(noticeEnText)
	if !ok {
		t.Fatalf("English summary notice missing after focusing it, texts = %v", tt.Texts())
	}
	tt.ClickAt(noticeEn.X+noticeEn.W/2, noticeEn.Y+noticeEn.H/2)
	tt.Frame()
	if urls := tt.OpenedURLs(); len(urls) != 2 || urls[1] != "https://feeds.example.com/summary-only" {
		t.Fatalf("opened URLs after clicking English notice banner = %v, want https://feeds.example.com/summary-only", urls)
	}

	// 2. Chinese check
	tt.SetLocale("zh")
	tt.Frame()
	noticeZh, ok := tt.Find("当前展示摘要，可点击在浏览器中查看原网页")
	if !ok {
		t.Fatalf("Chinese summary notice missing, texts = %v", tt.Texts())
	}
	tt.ClickAt(noticeZh.X+noticeZh.W/2, noticeZh.Y+noticeZh.H/2)
	tt.Frame()
	if urls := tt.OpenedURLs(); len(urls) != 3 || urls[2] != "https://feeds.example.com/summary-only" {
		t.Fatalf("opened URLs after clicking Chinese notice banner = %v, want https://feeds.example.com/summary-only", urls)
	}
}

// TestRSSReaderImageAntiHotlinkAndErrorDegradation verifies that when an image fails with HTTP 403
// (anti-hotlink) or 404/network error, the reader does not white-screen or panic, renders an elegant
// placeholder showing the Alt text, allows clicking to open original image in browser, and renders
// adjacent paragraphs cleanly.
func TestRSSReaderImageAntiHotlinkAndErrorDegradation(t *testing.T) {
	a, w := rssReaderTestApp(t)
	ctx := context.Background()
	now := time.Now().UnixMilli()

	hotlinkArt := core.RSSArticle{
		ID:          "art-hotlink-img",
		FeedID:      "feed-reader",
		Title:       "Post With Hotlinked Image",
		Link:        "https://feeds.example.com/hotlink-post",
		Author:      "Alan Turing",
		PublishedAt: "2026-11-24T10:00:00Z",
		Snippet:     "An article with an image protected by anti-hotlinking rules.",
		ContentHTML: "<p>Intro paragraph</p><img src=\"https://anti-hotlink.example.com/secret.png\" alt=\"Architecture Blueprint\"><p>Outro paragraph</p>",
		IsFullText:  true,
		Blocks: []core.ArticleBlock{
			{Type: core.BlockParagraph, Text: "Intro paragraph before image."},
			{Type: core.BlockImage, URL: "https://anti-hotlink.example.com/secret.png", Alt: "Architecture Blueprint"},
			{Type: core.BlockParagraph, Text: "Outro paragraph after image."},
		},
		UpdatedAt: now,
	}

	if err := a.Store.SaveRSSArticles(ctx, []core.RSSArticle{hotlinkArt}); err != nil {
		t.Fatalf("save article: %v", err)
	}
	if err := a.ReloadRSS(); err != nil {
		t.Fatalf("reload rss: %v", err)
	}

	tt := ui.NewTester(w.view, 1280, 808)
	tt.Frame()

	row, ok := tt.Find("Post With Hotlinked Image")
	if !ok {
		t.Fatal("hotlink article row not found")
	}
	tt.ClickAt(row.X+row.W/2, row.Y+row.H/2)
	tt.Frame()

	// Paragraphs before and after must render smoothly without white-screen or blockage
	if !tt.HasText("Intro paragraph before image.") {
		t.Fatalf("intro paragraph missing, texts = %v", tt.Texts())
	}
	if !tt.HasText("Outro paragraph after image.") {
		t.Fatalf("outro paragraph missing, texts = %v", tt.Texts())
	}

	// The picture resolves off the frame's path, so render until its failure
	// reaches the placeholder instead of assuming it has landed by now.
	deadline := time.Now().Add(5 * time.Second)
	for !tt.HasText("Open original image in browser") {
		if time.Now().After(deadline) {
			t.Fatalf("image failure placeholder missing, texts = %v", tt.Texts())
		}
		tt.Frame()
		time.Sleep(5 * time.Millisecond)
	}
	if !tt.HasText("Architecture Blueprint") {
		t.Fatalf("image alt text missing from placeholder, texts = %v", tt.Texts())
	}

	// The placeholder is a link: Tab reaches it and Enter opens the image.
	for range 100 {
		if tt.Focused("Architecture Blueprint") {
			break
		}
		tt.Key(0, ui.KeyTab)
	}
	if !tt.Focused("Architecture Blueprint") {
		t.Fatalf("the failed image placeholder never took the keyboard focus, texts = %v", tt.Texts())
	}
	tt.Key(0, ui.KeyEnter)
	if urls := tt.OpenedURLs(); len(urls) != 1 || urls[0] != "https://anti-hotlink.example.com/secret.png" {
		t.Fatalf("opened URLs after Enter on the placeholder = %v, want image URL https://anti-hotlink.example.com/secret.png", urls)
	}
	tt.Key(0, ui.KeySpace)
	if urls := tt.OpenedURLs(); len(urls) != 1 {
		t.Fatalf("Space on the placeholder opened %v, want only the Enter's image URL", urls)
	}

	// Clicking the placeholder opens the image URL in the browser
	clickTarget, ok := tt.Find("Open original image in browser")
	if !ok {
		t.Fatal("open image target not found")
	}
	tt.ClickAt(clickTarget.X+clickTarget.W/2, clickTarget.Y+clickTarget.H/2)
	tt.Frame()

	if urls := tt.OpenedURLs(); len(urls) != 2 || urls[1] != "https://anti-hotlink.example.com/secret.png" {
		t.Fatalf("opened URLs = %v, want image URL https://anti-hotlink.example.com/secret.png", urls)
	}

	// Bilingual check: in Chinese, shows "在浏览器中打开原图"
	tt.SetLocale("zh")
	tt.Frame()
	if !tt.HasText("在浏览器中打开原图") {
		t.Fatalf("Chinese open image label missing, texts = %v", tt.Texts())
	}
}

// saveRSSReaderScreenshot writes the rendered frame to the task's proof
// directory for visual inspection, outside the repository.
func saveRSSReaderScreenshot(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	dir := "/Volumes/ZHITAI_1TB/Projects/firstmate/data/youtube-reader-rss-reader-5/screenshots"
	if fi, err := os.Stat(filepath.Dir(dir)); err != nil || !fi.IsDir() {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Logf("screenshot dir: %v", err)
		return
	}
	img := tt.Image()
	if img == nil {
		return
	}
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Logf("screenshot file: %v", err)
		return
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Logf("screenshot encode: %v", err)
	}
}
