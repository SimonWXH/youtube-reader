package main

import (
	"sort"
	"testing"

	"github.com/egoist/mygo/ui"
	mui "github.com/simon/youtube-reader/ui"
)

// rssTextSizeSegments returns the reader's size control segments in visual
// order, as assistive technology sees them.
func rssTextSizeSegments(t *testing.T, tt *ui.Tester) []ui.AccessInfo {
	t.Helper()
	var segments []ui.AccessInfo
	for _, info := range tt.AccessAll() {
		if info.Segment {
			segments = append(segments, info)
		}
	}
	sort.Slice(segments, func(i, j int) bool { return segments[i].Bounds.X < segments[j].Bounds.X })
	return segments
}

// rssHeaderNode returns the box of the header's node named name, so a test can
// measure what the metadata row drew.
func rssHeaderNode(t *testing.T, tt *ui.Tester, header ui.Rect, name string) (ui.Rect, bool) {
	t.Helper()
	for _, info := range tt.AccessAll() {
		box := info.Bounds
		if info.Name != name || box.W <= 0 || box.H <= 0 {
			continue
		}
		if box.X >= header.X && box.X+box.W <= header.X+header.W &&
			box.Y >= header.Y && box.Y+box.H <= header.Y+header.H {
			return box, true
		}
	}
	return ui.Rect{}, false
}

// TestRSSTextSizeControlStepsAndPersists walks the reader's three body sizes
// through the header control: the middle step on a first read, each segment
// named by the size it sets, the page re-drawn at the chosen step, the choice
// written to settings.json, and a relaunch reading it back.
func TestRSSTextSizeControlStepsAndPersists(t *testing.T) {
	a, w, tt := rssReaderTestWindow(t)
	openRSSReaderTickArticle(t, tt)

	if got := w.readingType().Body.Size; got != mui.DefaultArticleSize {
		t.Fatalf("a first read opens at %v pt, want %v", got, mui.DefaultArticleSize)
	}
	segments := rssTextSizeSegments(t, tt)
	wantNames := [3]string{"Small Text · 13 pt", "Default Text · 15 pt", "Large Text · 17 pt"}
	if len(segments) != len(wantNames) {
		t.Fatalf("the size control has %d segments, want %d", len(segments), len(wantNames))
	}
	for i, segment := range segments {
		if want := wantNames[i]; segment.Name != want {
			t.Fatalf("segment %d is named %q, want %q", i, segment.Name, want)
		}
		if got := segment.Checked; got != (i == 1) {
			t.Fatalf("segment %d checked = %v, want only the middle step", i, got)
		}
	}

	// The smallest step: the page re-draws smaller in the same frame and the
	// choice is saved, in memory and on disk.
	if err := tt.Click(segments[0].Name); err != nil {
		t.Fatalf("click the smallest step: %v", err)
	}
	tt.Frame()
	if got := w.readingType().Body.Size; got != 13 {
		t.Fatalf("after choosing the smallest step the reader draws at %v pt, want 13", got)
	}
	if got := a.settingsSnapshot().RSSReadingFontSize; got != 13 {
		t.Fatalf("saved font size = %v, want 13", got)
	}
	disk := loadSettingsFile(t, a.Dir)
	if disk.RSSReadingFontSize != 13 {
		t.Fatalf("settings.json font size = %v, want 13", disk.RSSReadingFontSize)
	}
	if segments := rssTextSizeSegments(t, tt); !segments[0].Checked || segments[1].Checked {
		t.Fatalf("the control still shows the middle step chosen: %+v", segments)
	}

	// The largest step is one click away, past the middle.
	if err := tt.Click(rssTextSizeStepLabel(2, mui.ArticleSizes[2], false)); err != nil {
		t.Fatalf("click the largest step: %v", err)
	}
	tt.Frame()
	if got := w.readingType().Body.Size; got != 17 {
		t.Fatalf("after choosing the largest step the reader draws at %v pt, want 17", got)
	}
	disk = loadSettingsFile(t, a.Dir)
	if disk.RSSReadingFontSize != 17 {
		t.Fatalf("settings.json font size after the largest step = %v, want 17", disk.RSSReadingFontSize)
	}

	// A relaunch reads the last choice back.
	a.Settings = disk
	reopened := &Window{app: a, selectedIndex: new(int)}
	reopened.selectWorkspace(WorkspaceRSS)
	reopenedTT := ui.NewTester(reopened.view, 1280, 808)
	reopenedTT.Frame()
	if got := reopened.readingType().Body.Size; got != 17 {
		t.Fatalf("relaunched reader draws at %v pt, want the saved 17", got)
	}
}

// TestRSSReadingScaleRedrawsTheBody checks the whole page follows the step:
// a paragraph grows with the body size, and its line width stays the reading
// column's cap at every step.
func TestRSSReadingScaleRedrawsTheBody(t *testing.T) {
	_, w, tt := rssReaderTestWindow(t)
	openRSSReaderTickArticle(t, tt)
	if len(w.rssReader.ticks) < 2 {
		t.Fatal("the body laid out no blocks")
	}
	// The shipped default window lays the reader out at 618 pt: 1280 less the
	// 260 pt sidebar, the 400 pt list, and two 1 pt dividers. Its 24 pt
	// paragraph rail and 24 pt gutters leave a 546 pt column, under the 680 cap.
	column := float32(1280 - 260 - 1 - 400 - 1 - 24 - 2*24)
	paragraph := func() (height, width float32) {
		return w.rssReader.ticks[1].H, w.rssReader.ticks[1].W
	}
	var heights []float32
	for _, size := range []float32{13, 15, 17} {
		if err := tt.Click(rssTextSizeStepLabel(mui.ArticleStep(size), size, false)); err != nil {
			t.Fatalf("choose %v pt: %v", size, err)
		}
		tt.Frame()
		height, width := paragraph()
		if width != column {
			t.Fatalf("%v pt: paragraph width = %v, want the %v pt column", size, width, column)
		}
		if height <= 0 {
			t.Fatalf("%v pt: paragraph laid out no height", size)
		}
		heights = append(heights, height)
	}
	if !(heights[0] < heights[1] && heights[1] < heights[2]) {
		t.Fatalf("paragraph heights %v do not grow with the type ladder", heights)
	}
	// The setting is the step on screen, not a stale default.
	if got := w.readingType().Body.Size; got != 17 {
		t.Fatalf("reader ends at %v pt, want 17", got)
	}
}

// TestRSSReadingColumnNarrowsWithTheWindow checks a reader too narrow for the
// reading column: the text takes what the gutters leave instead of the cap, the
// blocks stay inside the body, and nothing scrolls sideways.
func TestRSSReadingColumnNarrowsWithTheWindow(t *testing.T) {
	a, w := rssReaderTestApp(t)
	a.Settings.SidebarWidth = maxSidebarWidth
	a.Settings.RSSListWidth = maxRSSListWidth
	tt := ui.NewTester(w.view, 960, 808)
	tt.Frame()
	openRSSReaderTickArticle(t, tt)

	if got := w.rssLayout.readerWidth; got != 320 {
		t.Fatalf("the minimum window lays the reader out at %v pt, want its 320 pt minimum", got)
	}
	// The reader's 24 pt paragraph rail and 24 pt gutters leave its column.
	want := float32(320 - 24 - 2*24)
	if want >= 680 {
		t.Fatalf("the narrow window leaves a %v pt column, want under the 680 pt cap", want)
	}
	for i, block := range w.rssReader.ticks {
		if block.W != want {
			t.Fatalf("block %d is %v wide, want the %v pt narrowed column", i, block.W, want)
		}
		body := w.rssReader.viewport
		if block.X < body.X || block.X+block.W > body.X+body.W {
			t.Fatalf("block %d runs outside the body: %v in %v", i, block, body)
		}
	}
	if w.rssReader.scroll.MaxX > 0 {
		t.Fatalf("the narrow reader scrolls sideways by %v pt", w.rssReader.scroll.MaxX)
	}
}

// TestRSSHeaderSizeControlFitsTheReader checks the control never leaves the
// pane: on the shipped default window and the widest reader it shares the
// actions' row, and on readers too narrow for it and a readable feed name it
// takes its own row above them, each segment inside the reader and the track's
// inset, and the feed name never squeezed below its minimum.
func TestRSSHeaderSizeControlFitsTheReader(t *testing.T) {
	for _, tc := range []struct {
		name    string
		width   int
		sidebar float32
		list    float32
		reader  float32
		inline  bool
	}{
		{name: "default window", width: 1280, sidebar: defaultSidebarWidth, list: defaultRSSListWidth, reader: 618, inline: true},
		{name: "widest reader", width: 1280, sidebar: minSidebarWidth, list: minRSSListWidth, reader: 798, inline: true},
		{name: "band under an inline row", width: 1280, sidebar: maxSidebarWidth, list: defaultRSSListWidth, reader: 558, inline: false},
		{name: "minimum window", width: 960, sidebar: maxSidebarWidth, list: maxRSSListWidth, reader: rssReaderMinWidth, inline: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, w := rssReaderTestApp(t)
			a.Settings.SidebarWidth = tc.sidebar
			a.Settings.RSSListWidth = tc.list
			tt := ui.NewTester(w.view, tc.width, 808)
			tt.Frame()
			openRSSReaderTickArticle(t, tt)

			if got := w.rssLayout.readerWidth; got != tc.reader {
				t.Fatalf("the reader is %v pt wide, want %v", got, tc.reader)
			}
			header, ok := tt.AccessByRole(ui.RoleGroup, "Article header")
			if !ok {
				t.Fatal("the reader header has no accessible group")
			}
			segments := rssTextSizeSegments(t, tt)
			if len(segments) != 3 {
				t.Fatalf("the size control has %d segments, want the three steps", len(segments))
			}
			track, ok := rssHeaderNode(t, tt, header.Bounds, "Text Size")
			if !ok {
				t.Fatal("the size control has no track")
			}
			if track.H != 28 {
				t.Fatalf("the size control's track is %v tall, want the header's 28 pt control height", track.H)
			}
			action, ok := tt.Find("Open in Browser")
			if !ok {
				t.Fatal("the reader's Open in Browser action is missing")
			}
			for i, segment := range segments {
				box := segment.Bounds
				if box.X < header.Bounds.X || box.X+box.W > header.Bounds.X+header.Bounds.W {
					t.Fatalf("segment %d at %v leaves the header %v", i, box, header.Bounds)
				}
				if box.H != 22 || box.Y != track.Y+3 {
					t.Fatalf("segment %d at %v does not fill the track's 2 pt inset and 1 pt border, want y %v, height 22",
						i, box, track.Y+3)
				}
				if box.Y < header.Bounds.Y || box.Y+box.H > header.Bounds.Y+header.Bounds.H {
					t.Fatalf("segment %d at %v leaves the header %v", i, box, header.Bounds)
				}
				sharesRow := box.Y+box.H > action.Y && box.Y < action.Y+action.H
				if sharesRow != tc.inline {
					t.Fatalf("segment %d at %v shares the actions' row = %v, want %v", i, box, sharesRow, tc.inline)
				}
			}
			if action.X+action.W > header.Bounds.X+header.Bounds.W {
				t.Fatalf("the actions run outside the header: %v in %v", action, header.Bounds)
			}
			rssHeaderFeedBox(t, tt, header.Bounds)
		})
	}

	t.Run("every row keeps the feed name's room", func(t *testing.T) {
		_, w := rssReaderTestApp(t)
		w.rssRefetchingID = "art-reader"
		inlineRows, widths := 0, 0
		for width := 1200; width <= 1300; width += 2 {
			tt := ui.NewTester(w.view, width, 808)
			tt.Frame()
			openRSSReaderTickArticle(t, tt)
			tt.Frame()
			header, ok := tt.AccessByRole(ui.RoleGroup, "Article header")
			if !ok {
				t.Fatal("the reader header has no accessible group")
			}
			segments := rssTextSizeSegments(t, tt)
			if len(segments) != 3 {
				t.Fatalf("%d pt: the size control has %d segments, want the three steps", width, len(segments))
			}
			action, ok := tt.Find("Fetching…")
			if !ok {
				t.Fatalf("%d pt: the re-fetch action is missing while fetching", width)
			}
			if box := segments[0].Bounds; box.Y+box.H > action.Y && box.Y < action.Y+action.H {
				inlineRows++
			}
			rssHeaderFeedBox(t, tt, header.Bounds)
			widths++
		}
		if inlineRows == 0 || inlineRows == widths {
			t.Fatalf("%d of %d rows were inline, want both layouts across the band", inlineRows, widths)
		}
	})
}

// rssHeaderFeedBox checks the row drew its feed name at least at the minimum
// and its separator after it, so neither is squeezed away.
func rssHeaderFeedBox(t *testing.T, tt *ui.Tester, header ui.Rect) {
	t.Helper()
	feed, ok := rssHeaderNode(t, tt, header, "The Long Read")
	if !ok {
		t.Fatal("the header draws no feed name")
	}
	if feed.W < 56 {
		t.Fatalf("the feed name is %v wide, want at least its 56 pt readable minimum", feed.W)
	}
	sep, ok := rssHeaderNode(t, tt, header, "·")
	if !ok {
		t.Fatal("the header draws no separator between the feed name and the date")
	}
	if sep.X < feed.X+feed.W {
		t.Fatalf("the separator at %v precedes the feed name %v", sep, feed)
	}
}

// TestRSSReadingSizeBilingualNames checks the size control speaks both
// languages, as the reader's other chrome does.
func TestRSSReadingSizeBilingualNames(t *testing.T) {
	_, _, tt := rssReaderTestWindow(t)
	tt.SetLocale("zh")
	openRSSReaderTickArticle(t, tt)
	segments := rssTextSizeSegments(t, tt)
	wantNames := [3]string{"小号文字 · 13 pt", "默认字号 · 15 pt", "大号文字 · 17 pt"}
	if len(segments) != len(wantNames) {
		t.Fatalf("the size control has %d segments, want %d", len(segments), len(wantNames))
	}
	for i, segment := range segments {
		if want := wantNames[i]; segment.Name != want {
			t.Fatalf("segment %d is named %q, want %q", i, segment.Name, want)
		}
	}
	if _, ok := tt.AccessByRole(ui.RoleRadioGroup, "字号"); !ok {
		t.Fatalf("the size control has no Chinese group name, texts = %v", tt.Texts())
	}
}
