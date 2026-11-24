package main

import (
	"context"
	"encoding/json"
	"errors"
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
		rects := rssTickRects(rail, 20, 3)
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
		if got := rssTickRects(rail, 0, 0); got != nil {
			t.Fatalf("no bars: %v", got)
		}
		if got := rssTickRects(rail, 4, -1); len(got) != 4 {
			t.Fatalf("no active bar: %d bars, want 4", len(got))
		}
	})
}

// TestRSSReaderContentPadding pins the reading column's centring: the board's
// gutter on a narrow reader and a wider gutter that holds a 700 pt column on a
// wide one.
func TestRSSReaderContentPadding(t *testing.T) {
	if got := rssReaderPadding(400); got != mui.Space(6) {
		t.Fatalf("narrow reader gutter = %v, want %v", got, mui.Space(6))
	}
	if got := rssReaderPadding(rssReadingColumn + 2*mui.Space(6)); got != mui.Space(6) {
		t.Fatalf("exact column gutter = %v, want %v", got, mui.Space(6))
	}
	available := float32(1000)
	gutter := rssReaderPadding(available)
	if got := available - 2*gutter; got != rssReadingColumn {
		t.Fatalf("wide reader column = %v, want %v", got, rssReadingColumn)
	}
}

// TestRSSReaderColumnAndImageSizing pins the reading column's cap and the
// picture fit: a picture never upscales past its own pixels and never grows
// taller than the reader's maximum.
func TestRSSReaderColumnAndImageSizing(t *testing.T) {
	if got := rssReadingWidth(618); got != 618-rssTickRailWidth-2*mui.Space(6) {
		t.Fatalf("narrow column = %v", got)
	}
	if got := rssReadingWidth(1000); got != rssReadingColumn {
		t.Fatalf("wide column = %v, want the %v cap", got, rssReadingColumn)
	}
	if got := rssReadingWidth(rssTickRailWidth); got != 0 {
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

	for _, tc := range []struct {
		width  float32
		inline bool
	}{
		{width: 320, inline: false},
		{width: rssHeaderInlineActionsMin - 1, inline: false},
		{width: rssHeaderInlineActionsMin, inline: true},
		{width: 900, inline: true},
	} {
		if got := rssHeaderInlineActions(tc.width); got != tc.inline {
			t.Errorf("rssHeaderInlineActions(%v) = %v, want %v", tc.width, got, tc.inline)
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

	w := &Window{app: a, selectedIndex: new(int)}
	w.selectWorkspace(WorkspaceRSS)
	return a, w
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
		"The Long Read",                       // feed source
		"Ada Lovelace",                        // author
		rssReaderDate("2026-11-23T08:00:00Z"), // published date, local and stamped
		"The Quiet Return of the Open Feed",   // title
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
	// The picture block keeps its frame and is named for what it shows.
	if img, ok := tt.AccessByRole(ui.RoleImage, "A reading room with tall windows"); !ok || img.Bounds.H <= 0 {
		t.Fatalf("picture block = %+v, %v; want a laid-out named frame", img, ok)
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
// feed's summary reads as prose, the notice names why, and the original page
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
	if !tt.HasText("Showing the feed's summary. Open the original article in your browser for the full text.") {
		t.Fatalf("summary notice missing, texts = %v", tt.Texts())
	}
	if !tt.HasText("Read the original article") {
		t.Fatalf("original article link missing, texts = %v", tt.Texts())
	}
	if got := len(a.SelectedRSSArticle().Blocks); got != 0 {
		t.Fatalf("fixture changed: summary article carries %d blocks", got)
	}
	// The snippet still becomes a body block, so the rail maps it.
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
// 700 pt cap and centring: a wide window grows the gutters, not the line
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
	if column != rssReadingColumn {
		t.Fatalf("reading column = %v, want the %v cap", column, rssReadingColumn)
	}
	body := w.rssReader.viewport
	if body.W < column+2*mui.Space(6) {
		t.Fatalf("body is %v wide for a %v column; the gutters did not grow", body.W, column)
	}
	saveRSSReaderScreenshot(t, tt, "06-reader-wide-window.png")
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
