package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
	mui "github.com/simon/youtube-reader/ui"
)

func TestDetailDuration(t *testing.T) {
	cases := map[string]string{
		"PT27M26S": "27:26",
		"PT1H2M3S": "1:02:03",
		"PT4M0S":   "4:00",
		"PT45S":    "0:45",
		"":         "",
	}
	for raw, want := range cases {
		if got := detailDuration(raw); got != want {
			t.Fatalf("detailDuration(%q) = %q, want %q", raw, got, want)
		}
	}
	// An unknown shape is shown as stored instead of being dropped.
	if got := detailDuration("27:26"); got != "27:26" {
		t.Fatalf("detailDuration(27:26) = %q, want it unchanged", got)
	}
}

func detailTestApp(t *testing.T, videos []core.Video, selected string) *App {
	t.Helper()
	return &App{
		Dir:             t.TempDir(),
		Selected:        "all",
		SelectedVideoID: selected,
		Videos:          videos,
		Sync: core.SyncState{
			Favorites:  map[string]core.FavoriteRecord{},
			ReadStates: map[string]map[string]core.ReadRecord{},
			Channels:   map[string]core.Channel{},
		},
	}
}

// wrappedDescription returns a single paragraph without hard newlines that
// shapes to exactly the requested number of lines at width, so fixtures come
// from the wrap, not from newline characters.
func wrappedDescription(t *testing.T, width float32, lines int) string {
	t.Helper()
	words := strings.Fields(strings.Repeat("the quick brown fox jumps over a lazy dog while reading the list ", 200))
	font := ui.Font{Family: mui.SFFont, Size: mui.Body.Size, Weight: int(mui.Body.Weight)}
	for n := 1; n <= len(words); n++ {
		text := strings.Join(words[:n], " ")
		if len(ui.ShapeText(text, font, width).Lines()) == lines {
			return text
		}
	}
	t.Fatalf("no paragraph wraps to %d lines at %v", lines, width)
	return ""
}

// A description whose seventh wrapped line the six-line clamp hides must offer
// More, and one that wraps to exactly six lines must not: the shaped line count
// decides, not a count of hard newlines or a height measured with a different
// line height.
func TestDescriptionMoreTracksRenderedLines(t *testing.T) {
	inner := defaultDetailsWidth - 2*mui.Space(4)
	short := core.Video{ID: "v-short", Title: "Short description", Description: wrappedDescription(t, inner, 6)}
	long := core.Video{ID: "v-long", Title: "Long description", Description: wrappedDescription(t, inner, 7)}

	a := detailTestApp(t, []core.Video{short, long}, short.ID)
	defer a.Close()
	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.view, 1280, 840)
	tt.Frame()

	if n := countText(tt, "More"); n != 0 {
		t.Fatalf("six rendered lines offer More %d times; want no More", n)
	}

	a.SelectedVideoID = long.ID
	tt.Frame()
	if n := countText(tt, "More"); n == 0 {
		t.Fatal("a hidden seventh line must offer More")
	}
	if err := tt.Click("More"); err != nil {
		t.Fatalf("click More: %v", err)
	}
	tt.Frame()
	if !tt.HasText("Show less") {
		t.Fatal("More must reveal the full description")
	}
}

// A selection whose row is gone from the list shows the inline error with
// Retry, and Retry re-reads the store so a restored row brings the pane back.
func TestDetailErrorRetryReloadsTheStore(t *testing.T) {
	ctx := context.Background()
	a := newTestApp(t, t.TempDir())
	defer a.Close()
	a.Selected = "all"
	a.Settings.APIKey = "test-key"
	a.Sync.Channels["ch-1"] = core.Channel{ID: "ch-1", Title: "Channel"}
	video := core.Video{ID: "v-gone", ChannelID: "ch-1", ChannelTitle: "Channel", Title: "A video that vanishes"}

	if err := a.Store.CommitRefresh(ctx, core.RefreshCommit{Videos: []core.Video{video}}); err != nil {
		t.Fatal(err)
	}
	if err := a.reload(); err != nil {
		t.Fatal(err)
	}
	a.SelectedVideoID = video.ID

	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.view, 1280, 840)
	tt.Frame()
	if !tt.HasText("Open on YouTube…") {
		t.Fatalf("the pane did not open on the selected video: %q", tt.Texts())
	}

	if err := a.Store.CommitRefresh(ctx, core.RefreshCommit{DeleteIDs: []string{video.ID}}); err != nil {
		t.Fatal(err)
	}
	if err := a.reload(); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.SelectedVideoID != "" {
		t.Fatalf("the grid did not clear the removed selection: %q", a.SelectedVideoID)
	}
	tt.Frame()
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	if !tt.HasText("Couldn't load this video") {
		t.Fatalf("a selection missing from the list shows no error: %q", tt.Texts())
	}
	if !tt.HasText("Retry") {
		t.Fatalf("the error offers no Retry: %q", tt.Texts())
	}
	if tt.HasText("Open on YouTube…") {
		t.Fatal("the pane kept the stale content instead of the error")
	}

	if err := a.Store.CommitRefresh(ctx, core.RefreshCommit{Videos: []core.Video{video}}); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Retry"); err != nil {
		t.Fatalf("click Retry: %v", err)
	}
	tt.Frame()
	if !tt.HasText("Open on YouTube…") {
		t.Fatalf("Retry did not reload the video: %q", tt.Texts())
	}
	if a.SelectedVideoID != video.ID {
		t.Fatalf("selection after Retry = %q, want %q", a.SelectedVideoID, video.ID)
	}
}

// A new video starts at the top of the pane instead of inheriting the last
// video's scroll offset.
func TestDetailScrollStartsAtTheTopForANewVideo(t *testing.T) {
	first := core.Video{
		ID:          "v-first",
		Title:       "First video",
		Description: strings.Repeat("A line of the description.\n", 40),
	}
	second := core.Video{ID: "v-second", Title: "Second video", Description: "Short."}

	a := detailTestApp(t, []core.Video{first, second}, first.ID)
	defer a.Close()
	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.view, 1280, 300)
	tt.Frame()

	top, ok := tt.Find("Description")
	if !ok {
		t.Fatal("the pane shows no description heading")
	}
	tt.Move(1100, 150)
	tt.Scroll(1100, 150, 0, 200)
	tt.Frame()
	tt.Frame()
	moved, ok := tt.Find("Description")
	if !ok {
		t.Fatal("the scrolled pane shows no description heading")
	}
	if moved.Y >= top.Y {
		t.Fatalf("pane did not scroll: heading moved from %v to %v", top.Y, moved.Y)
	}

	a.SelectedVideoID = second.ID
	tt.Frame()
	next, ok := tt.Find("Description")
	if !ok {
		t.Fatal("the next video shows no description heading")
	}
	if next.Y < top.Y-0.5 || next.Y > top.Y+0.5 {
		t.Fatalf("next video starts at %v; want the top at %v", next.Y, top.Y)
	}
}

// A sidebar drag that the open pane leaves no room for must keep the pointer's
// requested width, not the squeezed width the room allows, so closing the pane
// brings the sidebar back to what the user asked for.
func TestSidebarDragKeepsTheRequestedWidthWhenSqueezed(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	defer a.Close()
	a.Selected = "all"
	a.Settings.SidebarWidth = maxSidebarWidth
	a.Settings.DetailsWidth = defaultDetailsWidth
	a.Videos = []core.Video{{ID: "v-1", Title: "Video"}}
	a.SelectedVideoID = "v-1"

	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.view, 960, 640)
	tt.Frame()

	visible := boundedSidebarWidth(maxSidebarWidth, defaultDetailsWidth, 960)
	if w.sidebarWidth != visible {
		t.Fatalf("sidebar shows %v; want the room's %v", w.sidebarWidth, visible)
	}

	tt.Press(visible, 320)
	tt.Move(visible+30, 320)
	tt.Release(visible+30, 320)
	tt.Frame()

	if w.sidebarWidth != visible {
		t.Fatalf("the blocked drag resized the sidebar to %v; want %v", w.sidebarWidth, visible)
	}
	if got := a.Settings.SidebarWidth; got != visible+30 {
		t.Fatalf("the blocked drag stored %v; want the requested %v", got, visible+30)
	}

	a.SelectedVideoID = ""
	tt.Frame()
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	if w.sidebarWidth != visible+30 {
		t.Fatalf("sidebar reopened at %v; want the remembered %v", w.sidebarWidth, visible+30)
	}
}
