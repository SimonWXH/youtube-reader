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

// TestRSSTickMapping pins the paragraph rail's geometry and mapping: the bar
// count a rail can hold, which bar a block belongs to, which block the reading
// line is in, and where the bars are drawn.
func TestRSSTickMapping(t *testing.T) {
	t.Run("bar count", func(t *testing.T) {
		if got := rssTickCount(0, 400); got != 0 {
			t.Fatalf("no blocks: %v bars", got)
		}
		if got := rssTickCount(12, 400); got != 12 {
			t.Fatalf("12 short blocks: %v bars, want 12", got)
		}
		room := float32(400) - 2*rssTickInset
		maxBars := int(room / rssTickMinPitch)
		if got := rssTickCount(500, 400); got != maxBars {
			t.Fatalf("500 blocks: %v bars, want the %v the rail can separate", got, maxBars)
		}
		if got := rssTickCount(20, rssTickInset); got != 0 {
			t.Fatalf("rail with no room: %v bars, want 0", got)
		}
	})

	t.Run("block to bar", func(t *testing.T) {
		if got := rssTickBucket(-1, 10, 10); got != -1 {
			t.Fatalf("invalid block: %v", got)
		}
		for block := 0; block < 10; block++ {
			if got := rssTickBucket(block, 10, 10); got != block {
				t.Fatalf("one bar per block: block %d -> bar %d", block, got)
			}
		}
		if got := rssTickBucket(0, 100, 10); got != 0 {
			t.Fatalf("first block -> bar %d, want 0", got)
		}
		if got := rssTickBucket(99, 100, 10); got != 9 {
			t.Fatalf("last block -> bar %d, want 9", got)
		}
		last := -1
		for block := 0; block < 100; block++ {
			bar := rssTickBucket(block, 100, 10)
			if bar < last || bar > 9 || bar-last > 1 {
				t.Fatalf("bar mapping not monotone: block %d -> %d after %d", block, bar, last)
			}
			last = bar
		}
	})

	t.Run("reading line", func(t *testing.T) {
		viewport := ui.Rect{X: 900, Y: 100, W: 300, H: 600}
		// The reading line is rssReadingProbe below the viewport's top: the
		// block whose top last passed it is the one being read.
		line := viewport.Y + rssReadingProbe
		bounds := []ui.Rect{
			{X: 900, Y: 0, W: 300, H: 100},
			{X: 900, Y: 120, W: 300, H: 100},
			{X: 900, Y: 240, W: 300, H: 120},
			{X: 900, Y: 380, W: 300, H: 100},
			{X: 900, Y: 500, W: 300, H: 100},
		}
		if got, want := rssActiveBlock(bounds, viewport), 1; got != want {
			t.Fatalf("active block = %d, want %d (line %v)", got, want, line)
		}
		// At the top of the article nothing has crossed the line yet, so the
		// first block is the one about to be read.
		atTop := []ui.Rect{
			{X: 900, Y: 131, W: 300, H: 23},
			{X: 900, Y: 170, W: 300, H: 69},
		}
		if got := rssActiveBlock(atTop, viewport); got != 0 {
			t.Fatalf("at the top: active block = %d, want 0", got)
		}
		// A gap between blocks keeps the block whose top passed the line.
		gapped := []ui.Rect{
			{X: 900, Y: 0, W: 300, H: 100},
			{X: 900, Y: 400, W: 300, H: 100},
		}
		if got := rssActiveBlock(gapped, viewport); got != 0 {
			t.Fatalf("in a gap: active block = %d, want the block above it", got)
		}
		// Scrolled past every top, the last block stays active.
		scrolled := []ui.Rect{
			{X: 900, Y: -400, W: 300, H: 100},
			{X: 900, Y: -300, W: 300, H: 100},
			{X: 900, Y: -200, W: 300, H: 100},
		}
		if got := rssActiveBlock(scrolled, viewport); got != len(scrolled)-1 {
			t.Fatalf("at the end: active block = %d, want %d", got, len(scrolled)-1)
		}
		// Blocks the frame has not laid out yet are skipped.
		unlaid := []ui.Rect{{}, {X: 900, Y: 0, W: 300, H: 20}, {X: 900, Y: 40, W: 300, H: 100}}
		if got := rssActiveBlock(unlaid, viewport); got != 2 {
			t.Fatalf("unlaid block: active = %d, want 2", got)
		}
		if got := rssActiveBlock(nil, viewport); got != -1 {
			t.Fatalf("empty body: active = %d, want -1", got)
		}
		if got := rssActiveBlock(bounds, ui.Rect{X: 900, Y: 100, W: 300}); got != -1 {
			t.Fatalf("unlaid viewport: active = %d, want -1", got)
		}
	})

	t.Run("bar geometry", func(t *testing.T) {
		rail := ui.Rect{X: 1200, Y: 80, W: rssTickRailWidth, H: 500}
		rects := rssTickRects(rail, 20, 3, -1)
		if len(rects) != 20 {
			t.Fatalf("rects = %d, want 20", len(rects))
		}
		previousBottom := rail.Y
		for i, r := range rects {
			if r.X < rail.X || r.X+r.W > rail.X+rail.W || r.Y < rail.Y || r.Y+r.H > rail.Y+rail.H {
				t.Fatalf("bar %d %v leaves the rail %v", i, r, rail)
			}
			if r.Y < previousBottom {
				t.Fatalf("bar %d %v overlaps the one above it", i, r)
			}
			previousBottom = r.Y + r.H
			if i != 3 && (r.W != rssTickInactiveW || r.H != rssTickInactiveH) {
				t.Fatalf("bar %d is %vx%v, want the inactive %vx%v", i, r.W, r.H, rssTickInactiveW, rssTickInactiveH)
			}
		}
		active := rects[3]
		if active.W != rssTickActiveW || active.H != rssTickActiveH {
			t.Fatalf("active bar is %vx%v, want the active %vx%v", active.W, active.H, rssTickActiveW, rssTickActiveH)
		}
		// Bars end on the rail's right inset, and the active one is longest.
		if last := rects[0]; last.X+last.W != rail.X+rail.W-rssTickInsetX {
			t.Fatalf("bar ends at %v, want %v", last.X+last.W, rail.X+rail.W-rssTickInsetX)
		}
		if active.X >= rects[0].X {
			t.Fatalf("active bar starts at %v, want left of the inactive %v", active.X, rects[0].X)
		}
		if got := rssTickRects(rail, 0, 0, -1); got != nil {
			t.Fatalf("no bars: %v", got)
		}
		if got := rssTickRects(rail, 4, -1, -1); len(got) != 4 {
			t.Fatalf("no active bar: %d bars, want 4", len(got))
		}
		// The bar under the pointer grows between the inactive and the active
		// size, and an active bar under the pointer keeps its own.
		hovered := rssTickRects(rail, 20, 3, 7)
		if got := hovered[7]; got.W != rssTickHoverW || got.H != rssTickHoverH {
			t.Fatalf("hovered bar is %vx%v, want the hover %vx%v", got.W, got.H, rssTickHoverW, rssTickHoverH)
		}
		if got := hovered[7]; got.X+got.W != rects[0].X+rects[0].W {
			t.Fatalf("hovered bar ends at %v, want the same right edge as the rest", got.X+got.W)
		}
		if got := hovered[3]; got.W != rssTickActiveW || got.H != rssTickActiveH {
			t.Fatalf("active bar under the pointer is %vx%v, want the active %vx%v", got.W, got.H, rssTickActiveW, rssTickActiveH)
		}
		for i, r := range hovered {
			if i == 3 || i == 7 {
				continue
			}
			if r != rects[i] {
				t.Fatalf("bar %d moved to %v for a pointer on bar 7, want %v", i, r, rects[i])
			}
		}
	})
}

// TestRSSTickInteraction pins the rail's click and hover arithmetic: which
// block a bar reads, which bar a point is on, where a click leaves the body,
// and where the hover preview shows.
func TestRSSTickInteraction(t *testing.T) {
	t.Run("bar to block", func(t *testing.T) {
		if got := rssTickBlock(-1, 10, 10); got != -1 {
			t.Fatalf("no bar: block %v", got)
		}
		for block := 0; block < 10; block++ {
			if got := rssTickBlock(block, 10, 10); got != block {
				t.Fatalf("one bar per block: bar %d -> block %d", block, got)
			}
		}
		// Every bar reads a block that maps back to it, and reads the first of
		// the blocks it gathers, so a bar never skips a paragraph.
		for _, size := range []struct{ blocks, bars int }{
			{100, 10}, {13, 5}, {7, 3}, {400, 60}, {2, 1}, {1, 1},
		} {
			last := -1
			for bar := 0; bar < size.bars; bar++ {
				block := rssTickBlock(bar, size.blocks, size.bars)
				if block <= last || block >= size.blocks {
					t.Fatalf("%d blocks in %d bars: bar %d reads block %d after %d", size.blocks, size.bars, bar, block, last)
				}
				if got := rssTickBucket(block, size.blocks, size.bars); got != bar {
					t.Fatalf("%d blocks in %d bars: bar %d reads block %d, which maps to bar %d", size.blocks, size.bars, bar, block, got)
				}
				if block > 0 {
					if prev := rssTickBucket(block-1, size.blocks, size.bars); prev == bar {
						t.Fatalf("%d blocks in %d bars: bar %d reads block %d, though block %d shares it", size.blocks, size.bars, bar, block, block-1)
					}
				}
				last = block
			}
			// The last bar reaches the last block's bucket, however many bars
			// the rail could keep apart.
			if got := rssTickBucket(size.blocks-1, size.blocks, size.bars); got != size.bars-1 {
				t.Fatalf("%d blocks in %d bars: the last block maps to bar %d", size.blocks, size.bars, got)
			}
		}
		// A bar past the last reads the last bar's first block.
		if got, want := rssTickBlock(10, 100, 10), rssTickBlock(9, 100, 10); got != want {
			t.Fatalf("bar past the rail = block %d, want %d", got, want)
		}
	})

	t.Run("bar hit test", func(t *testing.T) {
		rail := ui.Rect{X: 1200, Y: 80, W: rssTickRailWidth, H: 500}
		bars := 20
		rects := rssTickRects(rail, bars, 3, -1)
		pitch := (rail.H - 2*rssTickInset) / float32(bars)
		for i, r := range rects {
			if got := rssTickIndexAt(rail, bars, r.X+r.W/2, r.Y+r.H/2); got != i {
				t.Fatalf("the middle of bar %d hits bar %d", i, got)
			}
			// The whole pitch answers for its bar, so a 2 pt mark is easy to hit.
			for _, y := range []float32{
				rail.Y + rssTickInset + float32(i)*pitch + 0.5,
				rail.Y + rssTickInset + float32(i+1)*pitch - 0.5,
			} {
				if got := rssTickIndexAt(rail, bars, r.X+r.W/2, y); got != i {
					t.Fatalf("y %v in slot %d hits bar %d", y, i, got)
				}
			}
		}
		if got := rssTickIndexAt(rail, bars, rects[5].X+rects[5].W/2, rail.Y+rssTickInset+5); got != 0 {
			t.Fatalf("the pitch above the first bar hits bar %d, want 0", got)
		}
		for _, tc := range []struct {
			name string
			x, y float32
		}{
			{"above the first bar", rects[0].X + rects[0].W/2, rail.Y + 1},
			{"below the last bar", rects[bars-1].X + rects[bars-1].W/2, rail.Y + rail.H - 1},
			{"the gutter left of the bars", rail.X + 2, rects[5].Y + rects[5].H/2},
			{"outside the lane", rail.X - 1, rects[5].Y + rects[5].H/2},
			{"past the lane's edge", rail.X + rail.W, rects[5].Y + rects[5].H/2},
		} {
			if got := rssTickIndexAt(rail, bars, tc.x, tc.y); got != -1 {
				t.Fatalf("%s (%v,%v) hits bar %d, want none", tc.name, tc.x, tc.y, got)
			}
		}
		if got := rssTickIndexAt(rail, 0, rail.X+rail.W-6, rail.Y+100); got != -1 {
			t.Fatalf("a lane without bars hits bar %d", got)
		}
	})

	t.Run("click target", func(t *testing.T) {
		viewport := ui.Rect{X: 900, Y: 100, W: 300, H: 600}
		bounds := []ui.Rect{
			{X: 900, Y: 120, W: 300, H: 100},
			{X: 900, Y: 240, W: 300, H: 100},
			{X: 900, Y: 360, W: 300, H: 100},
			{X: 900, Y: 500, W: 300, H: 100},
		}
		// At the top of the article, the second block's content offset is 140,
		// and the body scrolls to put its top on the reading line.
		if got, want := rssBlockScrollY(bounds, viewport, 0, 1), float32(140-rssReadingProbe); got != want {
			t.Fatalf("second block: scroll to %v, want %v", got, want)
		}
		// The same paragraph is the same place however far the body has
		// scrolled: its blocks show nearer the top by that much.
		scrolled := make([]ui.Rect, len(bounds))
		for i, b := range bounds {
			scrolled[i] = b
			scrolled[i].Y -= 60
		}
		if got, want := rssBlockScrollY(scrolled, viewport, 60, 1), float32(140-rssReadingProbe); got != want {
			t.Fatalf("second block while scrolled: scroll to %v, want %v", got, want)
		}
		// The first paragraph is above the reading line already: the body goes
		// to its start rather than above it.
		if got := rssBlockScrollY(bounds, viewport, 0, 0); got != 0 {
			t.Fatalf("first block: scroll to %v, want the start", got)
		}
		for _, tc := range []struct {
			name   string
			block  int
			holder ui.Rect
		}{
			{"no block", -1, viewport},
			{"a block past the body", len(bounds), viewport},
			{"a body the frame has not laid out", 1, ui.Rect{X: 900, Y: 100, W: 300}},
		} {
			if got := rssBlockScrollY(bounds, tc.holder, 300, tc.block); got != 300 {
				t.Fatalf("%s: scroll to %v, want the place it was at (300)", tc.name, got)
			}
		}
	})

	t.Run("preview placement", func(t *testing.T) {
		rail := ui.Rect{X: 1200, Y: 80, W: rssTickRailWidth, H: 500}
		bars := 20
		width, height := rssPreviewWidth, rssPreviewHeight(rssPreviewLines)
		// The preview's place is in the rail's own coordinates: its right edge the
		// gap left of the widest bar, which ends at the lane's right inset.
		right := rssTickRailWidth - rssTickInsetX - rssTickActiveW - rssPreviewGap
		for i, bar := range rssTickRects(rail, bars, 3, -1) {
			r := rssPreviewRect(rail, bar, width, height, rssPreviewGap, rssPreviewMargin)
			if r.X+r.W != right {
				t.Fatalf("bar %d: the preview ends at %v, want %v", i, r.X+r.W, right)
			}
			if r.X+r.W > bar.X-rail.X {
				t.Fatalf("bar %d: the preview overlaps its bar at %v", i, bar.X-rail.X)
			}
			if r.W != width || r.H != height {
				t.Fatalf("bar %d: the preview is %vx%v, want %vx%v", i, r.W, r.H, width, height)
			}
			if r.Y < rssPreviewMargin || r.Y+r.H > rail.H-rssPreviewMargin {
				t.Fatalf("bar %d: the preview %v leaves the lane %v", i, r, rail)
			}
			// Its middle is on the bar's middle wherever that fits, and against
			// the lane's ends otherwise.
			center := bar.Y + bar.H/2 - rail.Y - height/2
			switch {
			case center < rssPreviewMargin:
				if r.Y != rssPreviewMargin {
					t.Fatalf("bar %d at the top: preview top %v, want the margin %v", i, r.Y, rssPreviewMargin)
				}
			case center+height > rail.H-rssPreviewMargin:
				if want := rail.H - rssPreviewMargin - height; r.Y != want {
					t.Fatalf("bar %d at the bottom: preview top %v, want %v", i, r.Y, want)
				}
			default:
				if r.Y != center {
					t.Fatalf("bar %d: preview top %v, want the middle on the bar's (%v)", i, r.Y, center)
				}
			}
		}
		// A lane shorter than the preview keeps the margin at its top.
		short := ui.Rect{X: 1200, Y: 80, W: rssTickRailWidth, H: 40}
		bar := rssTickRects(short, 2, 0, -1)[1]
		if r := rssPreviewRect(short, bar, width, height, rssPreviewGap, rssPreviewMargin); r.Y != rssPreviewMargin {
			t.Fatalf("short lane: preview top %v, want %v", r.Y, rssPreviewMargin)
		}
		if got := rssPreviewRect(ui.Rect{X: 1200, Y: 80, W: rssTickRailWidth}, bar, width, height, rssPreviewGap, rssPreviewMargin); got != (ui.Rect{}) {
			t.Fatalf("lane without room: preview %v, want none", got)
		}
		if got := rssPreviewHeight(rssPreviewLines); got != 2*mui.Space(2)+2*mui.Hairline+mui.Metadata.LineHeight+mui.Space(0.5)+float32(rssPreviewLines)*mui.Secondary.LineHeight {
			t.Fatalf("preview height = %v, want the type scale's %v lines", got, rssPreviewLines)
		}
	})

	t.Run("preview words", func(t *testing.T) {
		// The brief asks for the opening 40 to 60 characters, the cut included.
		if rssPreviewRunes < 40 || rssPreviewRunes >= 60 {
			t.Fatalf("rssPreviewRunes = %d, want 40 to 59", rssPreviewRunes)
		}
		for _, tc := range []struct {
			block core.ArticleBlock
			zh    bool
			want  string
		}{
			{core.ArticleBlock{Type: core.BlockParagraph}, false, "Paragraph"},
			{core.ArticleBlock{Type: core.BlockHeading}, false, "Heading"},
			{core.ArticleBlock{Type: core.BlockBlockquote}, false, "Quote"},
			{core.ArticleBlock{Type: core.BlockCode}, false, "Code block"},
			{core.ArticleBlock{Type: core.BlockList}, false, "List"},
			{core.ArticleBlock{Type: core.BlockImage}, false, "Image"},
			{core.ArticleBlock{Type: core.BlockParagraph}, true, "段落"},
			{core.ArticleBlock{Type: core.BlockCode}, true, "代码块"},
			{core.ArticleBlock{Type: core.BlockBlockquote}, true, "引用"},
		} {
			if got := rssBlockKind(tc.block, tc.zh); got != tc.want {
				t.Fatalf("kind of %v (zh %v) = %q, want %q", tc.block.Type, tc.zh, got, tc.want)
			}
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
		// A picture without words says what it shows, and one without any
		// previews its kind alone.
		if got := rssPreviewWords(core.ArticleBlock{Type: core.BlockImage, Alt: "A reading room"}, rssPreviewRunes); got != "A reading room" {
			t.Fatalf("picture alt preview = %q", got)
		}
		if got := rssPreviewWords(core.ArticleBlock{Type: core.BlockImage, Caption: "The reading room"}, rssPreviewRunes); got != "The reading room" {
			t.Fatalf("picture caption preview = %q", got)
		}
		if got := rssPreviewWords(core.ArticleBlock{Type: core.BlockImage}, rssPreviewRunes); got != "" {
			t.Fatalf("bare picture preview = %q, want none", got)
		}
		// A block that carries only markup still previews its text.
		if got := rssPreviewWords(core.ArticleBlock{HTML: "<p>From <b>markup</b></p>"}, rssPreviewRunes); got != "From markup" {
			t.Fatalf("markup-only preview = %q", got)
		}
		if got := rssPreviewLabel("Quote", false); got != "Quote preview" {
			t.Fatalf("label = %q", got)
		}
		if got := rssPreviewLabel("引用", true); got != "引用预览" {
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
			Text: "That column of marks is the whole point of this screen. Each short bar is one block of the article, and the bar the reading line crosses grows and takes the accent colour.",
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
			Text: "The final paragraph exists so the last bar has a place to be: the end of the article is a destination, not a blank stretch of scroll.",
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

	w := &Window{app: a, selectedIndex: new(int), now: func() time.Time { return rssReaderTestNow }}
	w.selectWorkspace(WorkspaceRSS)
	return a, w
}

// rssReaderTestNow is the clock the reader tests run at, so the header's Today
// and its year rule do not change with the day the suite runs.
var rssReaderTestNow = time.Date(2026, time.December, 1, 9, 0, 0, 0, time.UTC)

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
// article's header, metadata, block body, paragraph rail, and both actions.
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
		"Open in Browser",
		"Refetch",
		"The quiet return of the open feed",           // heading block
		"Why the old protocol still fits",             // sub-heading block
		"A reader is a promise that the words arrive", // quote block
		"type Feed struct",                            // code block
		"Conditional requests cost nothing",           // list block
		"Read the original article",                   // permanent way out
		"feeds.example.com",
	} {
		if !tt.HasText(want) {
			t.Errorf("reader is missing %q, texts = %v", want, tt.Texts())
		}
	}
	if tt.HasText("Showing the feed's summary") {
		t.Error("a full-text article must not show the summary notice")
	}
	// The rail has one bar per block, and the first block is active at the top.
	if got, want := len(w.rssReader.ticks), len(a.SelectedRSSArticle().Blocks); got != want {
		t.Fatalf("rail ticks = %d, want one per block (%d)", got, want)
	}
	if w.rssReader.active != 0 {
		t.Fatalf("at the top the active block = %d, want 0", w.rssReader.active)
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
	tt.Click("Open in Browser")
	tt.Frame()
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
	tt.Click("Refetch")
	tt.Frame()
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
	if !tt.HasText("Retry full text") {
		t.Fatalf("the capture action is not a retry after a failure, texts = %v", tt.Texts())
	}
	if !tt.HasText("Read the original article") {
		t.Fatalf("original article link missing, texts = %v", tt.Texts())
	}
	if a.SelectedRSSArticle().IsFullText {
		t.Fatal("a fetch that gave up marked the article as full text")
	}
	// The snippet is the body, so the rail maps it.
	if want := len(rssArticleBlocks(*a.SelectedRSSArticle())); want == 0 || len(w.rssReader.ticks) != want {
		t.Fatalf("rail ticks = %d, want the %d snippet blocks", len(w.rssReader.ticks), want)
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
	action, ok := tt.Find("Open in Browser")
	if !ok {
		t.Fatal("the reader's Open in Browser action is missing")
	}
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

// TestRSSReaderTicksFollowScroll checks the rail's live highlight in a real
// frame: scrolling the body advances the active block and with it the bar.
func TestRSSReaderTicksFollowScroll(t *testing.T) {
	a, w, tt := rssReaderTestWindow(t)

	row, ok := tt.Find("The Quiet Return of the Open Feed")
	if !ok {
		t.Fatal("article row not found")
	}
	tt.ClickAt(row.X+row.W/2, row.Y+row.H/2)
	// Two frames: the first lays the body and records its blocks.
	tt.Frame()
	tt.Frame()

	if a.SelectedRSSArticleID != "art-reader" {
		t.Fatalf("SelectedRSSArticleID = %q", a.SelectedRSSArticleID)
	}
	if w.rssReader.active != 0 {
		t.Fatalf("active block at rest = %d, want 0", w.rssReader.active)
	}
	if w.rssReader.scroll.MaxY <= 0 {
		t.Fatalf("article body does not scroll at 808 pt: MaxY = %v", w.rssReader.scroll.MaxY)
	}
	if w.rssReader.viewport.H <= 0 {
		t.Fatalf("reader viewport has no height: %v", w.rssReader.viewport)
	}

	// Scroll far enough that several blocks pass the reading line.
	w.rssReader.scroll.Y = w.rssReader.scroll.MaxY / 2
	tt.Frame()
	tt.Frame()

	if w.rssReader.active <= 0 {
		t.Fatalf("after scrolling, active block = %d, want a later block", w.rssReader.active)
	}
	activeAtHalf := w.rssReader.active

	// The bar the rail draws is the bucket for that block, and it draws at
	// all: the rail's geometry is the one the pure test checks.
	count := rssTickCount(len(w.rssReader.ticks), w.rssReader.viewport.H)
	if count <= 0 {
		t.Fatalf("rail has no bars for %d blocks", len(w.rssReader.ticks))
	}
	if got := rssTickBucket(activeAtHalf, len(w.rssReader.ticks), count); got < 0 {
		t.Fatalf("active block %d maps to no bar", activeAtHalf)
	}

	// Away and back: the highlight follows down and returns.
	w.rssReader.scroll.Y = w.rssReader.scroll.MaxY
	tt.Frame()
	tt.Frame()
	if w.rssReader.active <= activeAtHalf {
		t.Fatalf("at the end active block = %d, want past %d", w.rssReader.active, activeAtHalf)
	}
}

// TestRSSReaderTickClickJumps covers a click on a bar: the body glides to the
// paragraph the bar shows, lands it within 10 pt of the reading line, and the
// bar turns active in the frame of the click and stays so.
func TestRSSReaderTickClickJumps(t *testing.T) {
	a, w, tt := rssReaderTestWindow(t)
	openRSSReaderTickArticle(t, tt)
	blocks := rssArticleBlocks(*a.SelectedRSSArticle())
	rail := rssReaderTickRail(t, tt)
	if w.rssReader.scroll.MaxY <= 0 {
		t.Fatalf("the article does not scroll: MaxY = %v", w.rssReader.scroll.MaxY)
	}
	if w.rssReader.active != 0 {
		t.Fatalf("at rest the active block = %d, want 0", w.rssReader.active)
	}

	// The fifth block, far enough down the article that the jump moves the body,
	// and far enough from its end that the reading line can take its top.
	block := 5
	if reach := w.rssReader.scroll.MaxY + w.rssReader.viewport.H - rssReadingProbe; w.rssReader.ticks[block].Y-w.rssReader.viewport.Y+w.rssReader.scroll.Y > reach {
		t.Fatalf("block %d cannot reach the reading line: the case needs one the body can bring there", block)
	}
	bar := rssReaderTickBar(t, rail, len(blocks), block)
	tt.ClickAt(bar.X+bar.W/2, bar.Y+bar.H/2)
	tt.Frame()

	// The bar grows in the click's own frame, before the body has moved.
	if w.rssReader.active != block || !w.rssReader.jumping {
		t.Fatalf("the frame of the click: active = %d, jumping = %v; want block %d on its way",
			w.rssReader.active, w.rssReader.jumping, block)
	}
	saveRSSTickScreenshot(t, tt, "01-click-jumping.png")

	settleRSSReaderScroll(t, tt, w)
	if w.rssReader.jumping {
		t.Fatal("the jump outlived the glide")
	}
	if w.rssReader.active != block {
		t.Fatalf("after the glide active = %d, want the clicked block %d", w.rssReader.active, block)
	}
	if got, want := rssTickBucket(w.rssReader.active, len(blocks), rssTickCount(len(blocks), rail.H)), rssTickBucket(block, len(blocks), rssTickCount(len(blocks), rail.H)); got != want {
		t.Fatalf("the grown bar is %d, want the clicked bar %d", got, want)
	}
	// The brief allows the paragraph's top 10 pt from where it should land.
	settled := w.rssReader.ticks[block].Y - w.rssReader.viewport.Y
	if settled < rssReadingProbe-10 || settled > rssReadingProbe+10 {
		t.Fatalf("the paragraph's top sits %v pt below the viewport's top, want %v ± 10", settled, rssReadingProbe)
	}
	saveRSSTickScreenshot(t, tt, "02-click-arrived.png")

	// A bar above the reading line brings the body back up the same way.
	for _, back := range []int{1, 0} {
		up := rssReaderTickBar(t, rail, len(blocks), back)
		tt.ClickAt(up.X+up.W/2, up.Y+up.H/2)
		tt.Frame()
		if w.rssReader.active != back || !w.rssReader.jumping {
			t.Fatalf("clicking bar %d: active = %d, jumping = %v", back, w.rssReader.active, w.rssReader.jumping)
		}
		settleRSSReaderScroll(t, tt, w)
		if w.rssReader.active != back {
			t.Fatalf("back at bar %d active = %d", back, w.rssReader.active)
		}
		if got := w.rssReader.ticks[back].Y - w.rssReader.viewport.Y; got < rssReadingProbe-10 || got > rssReadingProbe+10 {
			t.Fatalf("block %d's top sits %v pt below the viewport's top, want %v ± 10", back, got, rssReadingProbe)
		}
	}

	// The last bar of an article whose end shows no more than a screenful: the
	// body goes as far as it can, to its end, and the highlight follows the
	// reading line where it lands.
	last := rssReaderTickBar(t, rail, len(blocks), len(blocks)-1)
	tt.ClickAt(last.X+last.W/2, last.Y+last.H/2)
	tt.Frame()
	settleRSSReaderScroll(t, tt, w)
	if !closeTo(w.rssReader.scroll.Y, w.rssReader.scroll.MaxY, 0.5) {
		t.Fatalf("the last bar left the body at %v of %v", w.rssReader.scroll.Y, w.rssReader.scroll.MaxY)
	}
	if want := rssActiveBlock(w.rssReader.ticks, w.rssReader.viewport); w.rssReader.active != want {
		t.Fatalf("at the end active = %d, want the reading line's %d", w.rssReader.active, want)
	}
}

// TestRSSReaderTickHoverPreview covers the rail's hover preview: it names the
// kind of block a bar shows and its opening words, hangs left of the bar, and
// goes as the pointer leaves.
func TestRSSReaderTickHoverPreview(t *testing.T) {
	a, w, tt := rssReaderTestWindow(t)
	openRSSReaderTickArticle(t, tt)
	blocks := rssArticleBlocks(*a.SelectedRSSArticle())
	rail := rssReaderTickRail(t, tt)
	count := rssTickCount(len(blocks), rail.H)

	// The quote, whose kind reads differently from a paragraph's.
	block := 2
	bar := rssReaderTickBar(t, rail, len(blocks), block)
	tt.Move(bar.X+bar.W/2, bar.Y+bar.H/2)
	tt.Frame()
	if want := rssTickBucket(block, len(blocks), count); w.rssReader.hover != want {
		t.Fatalf("hovering bar %d, the reader has %d", want, w.rssReader.hover)
	}
	// The preview fades in: its frame shows it once the fade has come.
	tt.Advance(mui.MotionPopover + 20*time.Millisecond)
	tt.Frame()

	bubble, ok := tt.AccessByRole(ui.RoleGroup, "Quote preview")
	if !ok {
		t.Fatalf("no quote preview in %v", tt.Texts())
	}
	// Left of the bars with the gap from the widest one, its middle on the
	// bar's middle, and inside the rail's height.
	if want := rail.X + rail.W - rssTickInsetX - rssTickActiveW - rssPreviewGap; !closeTo(bubble.Bounds.X+bubble.Bounds.W, want, 0.5) {
		t.Fatalf("the preview ends at %v, want the gap's %v left of the widest bar", bubble.Bounds.X+bubble.Bounds.W, want)
	}
	if want := rssPreviewHeight(rssPreviewLines); !closeTo(bubble.Bounds.H, want, 0.5) {
		t.Fatalf("the preview is %v tall, want the %v of its two lines", bubble.Bounds.H, want)
	}
	if want := bar.Y + bar.H/2; !closeTo(bubble.Bounds.Y+bubble.Bounds.H/2, want, 0.5) {
		t.Fatalf("the preview's middle is at %v, want the bar's %v", bubble.Bounds.Y+bubble.Bounds.H/2, want)
	}
	if bubble.Bounds.Y < rail.Y+rssPreviewMargin || bubble.Bounds.Y+bubble.Bounds.H > rail.Y+rail.H-rssPreviewMargin {
		t.Fatalf("the preview %v leaves the rail %v", bubble.Bounds, rail)
	}
	if !tt.HasText("Quote") {
		t.Fatalf("the preview does not name the block kind, texts = %v", tt.Texts())
	}
	// Its words are the paragraph's, cut to the brief's length, inside it.
	words := rssPreviewWords(blocks[block], rssPreviewRunes)
	if len([]rune(words)) > 60 {
		t.Fatalf("the preview shows %d runes, want at most 60", len([]rune(words)))
	}
	text, ok := tt.Find(words)
	if !ok {
		t.Fatalf("the preview does not show %q, texts = %v", words, tt.Texts())
	}
	if !rectHolds(bubble.Bounds, text, 0.5) {
		t.Fatalf("the words %v sit outside their preview %v", text, bubble.Bounds)
	}
	saveRSSTickScreenshot(t, tt, "03-hover-preview.png")

	// Along the rail the preview follows the pointer, showing that bar's block.
	other := 7
	otherBar := rssReaderTickBar(t, rail, len(blocks), other)
	tt.Move(otherBar.X+otherBar.W/2, otherBar.Y+otherBar.H/2)
	tt.Frame()
	if want := rssTickBucket(other, len(blocks), count); w.rssReader.hover != want {
		t.Fatalf("hovering bar %d, the reader has %d", want, w.rssReader.hover)
	}
	if _, ok := tt.AccessByRole(ui.RoleGroup, "Quote preview"); ok {
		t.Fatal("the quote's preview stayed on another bar")
	}
	list, ok := tt.AccessByRole(ui.RoleGroup, "List preview")
	if !ok {
		t.Fatalf("no list preview over the list's bar, texts = %v", tt.Texts())
	}
	if want := rail.X + rail.W - rssTickInsetX - rssTickActiveW - rssPreviewGap; !closeTo(list.Bounds.X+list.Bounds.W, want, 0.5) {
		t.Fatalf("the list's preview ends at %v, want beside its bar %v", list.Bounds.X+list.Bounds.W, otherBar)
	}

	// The lane's gutter is no bar: no preview there.
	tt.Move(rail.X+1, bar.Y+bar.H/2)
	tt.Frame()
	if w.rssReader.hover != -1 {
		t.Fatalf("the gutter hovers bar %d", w.rssReader.hover)
	}
	if _, ok := tt.AccessByRole(ui.RoleGroup, "List preview"); ok {
		t.Fatal("the preview stayed after the pointer left the bars")
	}
	tt.Advance(mui.MotionInteraction + 20*time.Millisecond)
	tt.Frame()
	if _, ok := tt.AccessByRole(ui.RoleGroup, "List preview"); ok {
		t.Fatal("the preview outlived its fade")
	}

	// The top bar's preview keeps the rail's margin rather than hanging over
	// the article header.
	top := rssReaderTickBar(t, rail, len(blocks), 0)
	tt.Move(top.X+top.W/2, top.Y+top.H/2)
	if top.Y+top.H/2-rail.Y >= rssPreviewHeight(rssPreviewLines)/2+rssPreviewMargin {
		t.Fatal("the top bar leaves the preview room: the check would prove nothing")
	}
	tt.Frame()
	heading, ok := tt.AccessByRole(ui.RoleGroup, "Heading preview")
	if !ok {
		t.Fatalf("no preview over the heading's bar, texts = %v", tt.Texts())
	}
	if !closeTo(heading.Bounds.Y, rail.Y+rssPreviewMargin, 0.5) {
		t.Fatalf("the top preview starts at %v, want the rail's margin %v", heading.Bounds.Y, rail.Y+rssPreviewMargin)
	}
	tt.Advance(mui.MotionPopover + 20*time.Millisecond)
	tt.Frame()
	saveRSSTickScreenshot(t, tt, "04-hover-top.png")

	// Dark mode keeps the preview's contrast: the panel is the surface over the
	// app's background, its border the panel rule.
	tt.SetDark(true)
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	if _, ok := tt.AccessByRole(ui.RoleGroup, "Heading preview"); !ok {
		t.Fatalf("the preview went out with dark mode, texts = %v", tt.Texts())
	}
	saveRSSTickScreenshot(t, tt, "05-hover-dark.png")
	tt.SetDark(false)
	tt.Frame()

	// A narrower window moves the lane: the preview follows it to the new bars
	// and stays inside it.
	tt.SetSize(1180, 700)
	tt.Frame()
	rail = rssReaderTickRail(t, tt)
	bar = rssReaderTickBar(t, rail, len(blocks), block)
	tt.Move(bar.X+bar.W/2, bar.Y+bar.H/2)
	tt.Frame()
	tt.Advance(mui.MotionPopover + 20*time.Millisecond)
	tt.Frame()
	shrunk, ok := tt.AccessByRole(ui.RoleGroup, "Quote preview")
	if !ok {
		t.Fatalf("no preview after the window narrowed, texts = %v", tt.Texts())
	}
	if shrunk.Bounds.Y < rail.Y+rssPreviewMargin || shrunk.Bounds.Y+shrunk.Bounds.H > rail.Y+rail.H-rssPreviewMargin {
		t.Fatalf("the narrowed preview %v leaves the rail %v", shrunk.Bounds, rail)
	}
	if want := rail.X + rail.W - rssTickInsetX - rssTickActiveW - rssPreviewGap; !closeTo(shrunk.Bounds.X+shrunk.Bounds.W, want, 0.5) {
		t.Fatalf("the narrowed preview ends at %v, want %v", shrunk.Bounds.X+shrunk.Bounds.W, want)
	}
	tt.SetSize(1280, 808)
	tt.Frame()
	rail = rssReaderTickRail(t, tt)

	// 简体中文 names the block kinds in its own words.
	tt.SetLocale("zh-Hans")
	tt.Frame()
	paragraph := 9
	paragraphBar := rssReaderTickBar(t, rail, len(blocks), paragraph)
	tt.Move(paragraphBar.X+paragraphBar.W/2, paragraphBar.Y+paragraphBar.H/2)
	tt.Frame()
	if _, ok := tt.AccessByRole(ui.RoleGroup, "段落预览"); !ok {
		t.Fatalf("no Chinese paragraph preview, texts = %v", tt.Texts())
	}
	if !tt.HasText("段落") {
		t.Fatalf("the Chinese preview does not name the kind, texts = %v", tt.Texts())
	}
	tt.SetLocale("en")
	tt.Frame()
}

// TestRSSReaderTickLongArticle covers a body the rail cannot keep apart: bars
// gather paragraphs, each reads and previews the first of them, and the last
// bar lands at the end of the article.
func TestRSSReaderTickLongArticle(t *testing.T) {
	blocks := make([]core.ArticleBlock, 0, 400)
	for i := range 400 {
		blocks = append(blocks, core.ArticleBlock{
			Type: core.BlockParagraph,
			Text: fmt.Sprintf("Paragraph %d of a long piece that keeps going for as long as the rail can follow it, one short bar at a time.", i),
		})
	}
	a, w, tt := rssReaderScopedWindow(t, core.RSSArticle{
		ID: "art-long", Title: "A Long Read", Link: "https://feeds.example.com/long", IsFullText: true, Blocks: blocks,
	})
	body := rssArticleBlocks(*a.SelectedRSSArticle())
	if len(body) != 400 {
		t.Fatalf("the body holds %d blocks, want 400", len(body))
	}
	rail := rssReaderTickRail(t, tt)
	count := rssTickCount(len(body), rail.H)
	if count <= 0 || count >= len(body) {
		t.Fatalf("the rail shows %d bars for %d blocks, want the bars it can keep apart", count, len(body))
	}

	// A bar gathers several paragraphs: it reads and previews the first.
	bar := count / 2
	block := rssTickBlock(bar, len(body), count)
	if block <= 0 || rssTickBucket(block-1, len(body), count) == bar {
		t.Fatalf("bar %d reads block %d, which the bar before does not keep apart", bar, block)
	}
	rect := rssTickRects(rail, count, -1, -1)[bar]
	tt.Move(rect.X+rect.W/2, rect.Y+rect.H/2)
	tt.Frame()
	if w.rssReader.hover != bar {
		t.Fatalf("hovering bar %d, the reader has %d", bar, w.rssReader.hover)
	}
	preview, ok := tt.AccessByRole(ui.RoleGroup, "Paragraph preview")
	if !ok {
		t.Fatalf("no preview over bar %d, texts = %v", bar, tt.Texts())
	}
	words := rssPreviewWords(body[block], rssPreviewRunes)
	if text, ok := tt.Find(words); !ok || !rectHolds(preview.Bounds, text, 0.5) {
		t.Fatalf("the preview %v does not show block %d's words (%v, %v)", preview.Bounds, block, text, ok)
	}

	// Clicking the bar takes the body to that paragraph, within the 10 pt.
	tt.ClickAt(rect.X+rect.W/2, rect.Y+rect.H/2)
	tt.Frame()
	if w.rssReader.active != block || !w.rssReader.jumping {
		t.Fatalf("the click reads block %d (%v jumping), want block %d on its way", w.rssReader.active, w.rssReader.jumping, block)
	}
	settleRSSReaderScroll(t, tt, w)
	if w.rssReader.active != block {
		t.Fatalf("after the glide active = %d, want %d", w.rssReader.active, block)
	}
	if got := w.rssReader.ticks[block].Y - w.rssReader.viewport.Y; got < rssReadingProbe-10 || got > rssReadingProbe+10 {
		t.Fatalf("the gathered paragraph's top sits %v pt below the viewport's top, want %v ± 10", got, rssReadingProbe)
	}

	// The last bar reaches the end of the article, where the body can go no
	// further: it lands there, on the paragraph the reading line shows.
	lastBar := rssTickRects(rail, count, -1, -1)[count-1]
	tt.ClickAt(lastBar.X+lastBar.W/2, lastBar.Y+lastBar.H/2)
	tt.Frame()
	settleRSSReaderScroll(t, tt, w)
	if !closeTo(w.rssReader.scroll.Y, w.rssReader.scroll.MaxY, 0.5) {
		t.Fatalf("the last bar left the body at %v of %v", w.rssReader.scroll.Y, w.rssReader.scroll.MaxY)
	}
	if want := rssActiveBlock(w.rssReader.ticks, w.rssReader.viewport); w.rssReader.active != want {
		t.Fatalf("at the end active = %d, want the reading line's %d", w.rssReader.active, want)
	}
	if w.rssReader.active <= block {
		t.Fatalf("the last bar left the body at block %d, no further than the middle bar's %d", w.rssReader.active, block)
	}
}

// TestRSSReaderTickShortArticle covers the bodies a rail cannot map: one block
// shows no bars and answers no click, and a body too short to scroll keeps its
// place.
func TestRSSReaderTickShortArticle(t *testing.T) {
	t.Run("one block", func(t *testing.T) {
		_, w, tt := rssReaderScopedWindow(t, core.RSSArticle{
			ID: "art-one", Title: "A Note", Link: "https://feeds.example.com/note", IsFullText: true,
			Blocks: []core.ArticleBlock{{Type: core.BlockParagraph, Text: "A single paragraph is not a map."}},
		})
		rail := rssReaderTickRail(t, tt)
		tt.Move(rail.X+rail.W-rssTickInsetX-rssTickInactiveW/2, rail.Y+rail.H/2)
		tt.Frame()
		if w.rssReader.hover != -1 {
			t.Fatalf("one block hovers bar %d", w.rssReader.hover)
		}
		if _, ok := tt.AccessByRole(ui.RoleGroup, "Paragraph preview"); ok {
			t.Fatal("one block shows a preview")
		}
		tt.ClickAt(rail.X+rail.W-rssTickInsetX-rssTickInactiveW/2, rail.Y+rail.H/2)
		tt.Frame()
		settleRSSReaderScroll(t, tt, w)
		if w.rssReader.jumping || w.rssReader.active != 0 || w.rssReader.scroll.Y != 0 {
			t.Fatalf("one block answered a click: active = %d, jumping = %v, Y = %v",
				w.rssReader.active, w.rssReader.jumping, w.rssReader.scroll.Y)
		}
	})

	t.Run("a body too short to scroll", func(t *testing.T) {
		_, w, tt := rssReaderScopedWindow(t, core.RSSArticle{
			ID: "art-two", Title: "Two Lines", Link: "https://feeds.example.com/two", IsFullText: true,
			Blocks: []core.ArticleBlock{
				{Type: core.BlockParagraph, Text: "The first of two short paragraphs."},
				{Type: core.BlockParagraph, Text: "The second, still inside the viewport."},
			},
		})
		if w.rssReader.scroll.MaxY != 0 {
			t.Fatalf("the two paragraphs scroll %v; the case needs a body that fits", w.rssReader.scroll.MaxY)
		}
		rail := rssReaderTickRail(t, tt)
		bar := rssReaderTickBar(t, rail, 2, 1)
		tt.Move(bar.X+bar.W/2, bar.Y+bar.H/2)
		tt.Frame()
		if got := w.rssReader.hover; got != rssTickBucket(1, 2, rssTickCount(2, rail.H)) {
			t.Fatalf("hovering the second bar, the reader has %d", got)
		}
		if _, ok := tt.AccessByRole(ui.RoleGroup, "Paragraph preview"); !ok {
			t.Fatalf("no preview over the second bar, texts = %v", tt.Texts())
		}
		tt.ClickAt(bar.X+bar.W/2, bar.Y+bar.H/2)
		tt.Frame()
		settleRSSReaderScroll(t, tt, w)
		// Nothing scrolls, so the reading line cannot move: the first block stays
		// the one being read, and the jump ends without moving the body.
		if w.rssReader.scroll.Y != 0 || w.rssReader.active != 0 || w.rssReader.jumping {
			t.Fatalf("a body that fits: active = %d, jumping = %v, Y = %v",
				w.rssReader.active, w.rssReader.jumping, w.rssReader.scroll.Y)
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

// rssReaderTickRail is the paragraph rail's box in the last frame, as the
// group it labels shows it.
func rssReaderTickRail(t *testing.T, tt *ui.Tester) ui.Rect {
	t.Helper()
	rail, ok := tt.AccessByRole(ui.RoleGroup, "Reading position")
	if !ok || rail.Bounds.H <= 0 || rail.Bounds.W <= 0 {
		t.Fatalf("rail = %+v, %v; want a laid-out lane", rail.Bounds, ok)
	}
	return rail.Bounds
}

// rssReaderTickBar is where the bar for a block shows, in the window.
func rssReaderTickBar(t *testing.T, rail ui.Rect, blockCount, block int) ui.Rect {
	t.Helper()
	count := rssTickCount(blockCount, rail.H)
	if count <= 0 {
		t.Fatalf("a rail of %v shows no bars for %d blocks", rail, blockCount)
	}
	return rssTickRects(rail, count, -1, -1)[rssTickBucket(block, blockCount, count)]
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
	dir := "/Volumes/ZHITAI_1TB/Projects/firstmate/data/youtube-reader-rss-ticks-jump-6/screenshots"
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
	if w.rssReader.articleID != "" || len(w.rssReader.ticks) != 0 {
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
	for _, want := range []string{"在浏览器中打开", "重新抓取", "阅读原文"} {
		if !tt.HasText(want) {
			t.Errorf("Chinese reader is missing %q, texts = %v", want, tt.Texts())
		}
	}
	_ = w
	tt.SetLocale("en")
	tt.Frame()
	if !tt.HasText("Open in Browser") || !tt.HasText("Refetch") {
		t.Fatalf("reader did not return to English, texts = %v", tt.Texts())
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
	rail, ok := tt.AccessByRole(ui.RoleGroup, "Reading position")
	if !ok {
		t.Fatal("reading rail missing at the minimum width")
	}
	for i, bar := range w.rssReader.ticks {
		if bar.X+bar.W > rail.Bounds.X {
			t.Fatalf("block %d runs into the rail: %v at %v", i, bar, rail.Bounds)
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

	if len(w.rssReader.ticks) < 2 {
		t.Fatal("rail has no blocks to measure")
	}
	column := w.rssReader.ticks[0].W
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
