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

// TestRSSTextSizeControlStepsAndPersists walks the reader's three body sizes
// through the header control: the middle step on a first read, each segment
// named by the size it sets, the page re-drawn at the chosen step, the choice
// written to settings.json, and a relaunch reading it back.
func TestRSSTextSizeControlStepsAndPersists(t *testing.T) {
	a, w, tt := rssReaderTestWindow(t)
	openRSSReaderTickArticle(t, tt)

	if got := w.readingType().Size; got != defaultRSSReadingFontSize {
		t.Fatalf("a first read opens at %v pt, want %v", got, defaultRSSReadingFontSize)
	}
	segments := rssTextSizeSegments(t, tt)
	if len(segments) != len(mui.ArticleSizes) {
		t.Fatalf("the size control has %d segments, want %d", len(segments), len(mui.ArticleSizes))
	}
	for i, segment := range segments {
		if want := rssTextSizeStepLabel(i, mui.ArticleSizes[i], false); segment.Name != want {
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
	if got := w.readingType().Size; got != 13 {
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
	if got := w.readingType().Size; got != 17 {
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
	if got := reopened.readingType().Size; got != 17 {
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
	// The test article opens with a section heading then its first paragraph.
	column := rssReadingWidth(w.rssLayout.readerWidth)
	paragraph := func() (height, width float32) {
		return w.rssReader.ticks[1].H, w.rssReader.ticks[1].W
	}
	var heights []float32
	for _, size := range mui.ArticleSizes {
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
	if got := w.readingType().Size; got != 17 {
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

	available := w.rssLayout.readerWidth - rssTickRailWidth
	want := available - 2*rssReaderPadding(available)
	if want <= 0 || want >= rssReadingColumn {
		t.Fatalf("the minimum window leaves a %v pt column, want under the %v cap", want, rssReadingColumn)
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
// pane: on a wide reader it shares the actions' row, and on the narrowest
// reader it takes its own row above them, each segment inside the reader.
func TestRSSHeaderSizeControlFitsTheReader(t *testing.T) {
	for _, tc := range []struct {
		name   string
		width  int
		inline bool
	}{
		{name: "design width", width: 1280, inline: true},
		{name: "minimum width", width: 960, inline: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, w := rssReaderTestApp(t)
			if tc.width <= 960 {
				a.Settings.SidebarWidth = maxSidebarWidth
				a.Settings.RSSListWidth = maxRSSListWidth
			} else {
				a.Settings.SidebarWidth = minSidebarWidth
				a.Settings.RSSListWidth = minRSSListWidth
			}
			tt := ui.NewTester(w.view, tc.width, 808)
			tt.Frame()
			openRSSReaderTickArticle(t, tt)

			if got := rssHeaderInlineActions(w.rssLayout.readerWidth); got != tc.inline {
				t.Fatalf("inline actions = %v at a %v pt reader, want %v", got, w.rssLayout.readerWidth, tc.inline)
			}
			header, ok := tt.AccessByRole(ui.RoleGroup, "Article header")
			if !ok {
				t.Fatal("the reader header has no accessible group")
			}
			segments := rssTextSizeSegments(t, tt)
			if len(segments) != len(mui.ArticleSizes) {
				t.Fatalf("the size control has %d segments, want %d", len(segments), len(mui.ArticleSizes))
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
		})
	}
}

// TestRSSReadingSizeBilingualNames checks the size control speaks both
// languages, as the reader's other chrome does.
func TestRSSReadingSizeBilingualNames(t *testing.T) {
	_, _, tt := rssReaderTestWindow(t)
	tt.SetLocale("zh")
	openRSSReaderTickArticle(t, tt)
	segments := rssTextSizeSegments(t, tt)
	if len(segments) != len(mui.ArticleSizes) {
		t.Fatalf("the size control has %d segments, want %d", len(segments), len(mui.ArticleSizes))
	}
	for i, segment := range segments {
		if want := rssTextSizeStepLabel(i, mui.ArticleSizes[i], true); segment.Name != want {
			t.Fatalf("segment %d is named %q, want %q", i, segment.Name, want)
		}
	}
	if _, ok := tt.AccessByRole(ui.RoleRadioGroup, "字号"); !ok {
		t.Fatalf("the size control has no Chinese group name, texts = %v", tt.Texts())
	}
}
