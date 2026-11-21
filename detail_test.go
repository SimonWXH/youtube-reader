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
// decides at the pane's settled width, not the animated one, so the button does
// not pop in while the pane opens.
func TestDescriptionMoreTracksRenderedLines(t *testing.T) {
	inner := defaultDetailsWidth - 2*mui.Space(4)
	short := core.Video{ID: "v-short", Title: "Short description", Description: wrappedDescription(t, inner, 6)}
	long := core.Video{ID: "v-long", Title: "Long description", Description: wrappedDescription(t, inner, 7)}

	a := detailTestApp(t, []core.Video{short, long}, "")
	defer a.Close()
	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.view, 1280, 840)
	tt.Frame()

	a.SelectedVideoID = short.ID
	tt.Frame()
	tt.Advance(60 * time.Millisecond)
	tt.Frame()
	if w.detail.shown <= 0 || w.detail.shown >= defaultDetailsWidth-0.5 {
		t.Fatalf("pane width %v is not mid-animation", w.detail.shown)
	}
	if n := countText(tt, "More"); n != 0 {
		t.Fatalf("the opening pane offered More %d times for six rendered lines; want none", n)
	}
	tt.Advance(300 * time.Millisecond)
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

// Closing the error pane keeps the error on screen for the whole close
// animation rather than sliding out an empty state.
func TestDetailErrorStaysThroughTheCloseAnimation(t *testing.T) {
	a := detailTestApp(t, []core.Video{{ID: "v-1", Title: "Video"}}, "v-gone")
	defer a.Close()
	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.view, 1280, 840)
	tt.Frame()

	if !tt.HasText("Couldn't load this video") {
		t.Fatalf("a missing video shows no error: %q", tt.Texts())
	}
	if err := tt.Click("Close details"); err != nil {
		t.Fatalf("click Close details: %v", err)
	}
	tt.Advance(60 * time.Millisecond)
	tt.Frame()
	if !tt.HasText("Couldn't load this video") {
		t.Fatalf("the error vanished during the close animation: %q", tt.Texts())
	}
	if tt.HasText("Select a video to see details") {
		t.Fatal("the empty state slid out under the closing error")
	}
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	if tt.HasText("Couldn't load this video") {
		t.Fatal("the closed pane kept the error")
	}
}

// At the minimum window with the widest requested pane, the divider shows less
// than the request; one arrow press must still move it.
func TestSqueezedDetailDividerKeyMovesThePane(t *testing.T) {
	video := core.Video{ID: "v-1", Title: "Video"}
	a := detailTestApp(t, []core.Video{video}, video.ID)
	defer a.Close()
	a.Settings.DetailsWidth = maxDetailsWidth

	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.view, 960, 640)
	tt.Frame()

	settled := detailWidthForRoom(maxDetailsWidth, 960-minSidebarWidth-splitDividerWidth())
	if w.detail.shown != settled {
		t.Fatalf("pane shows %v; want the room's %v", w.detail.shown, settled)
	}
	if w.detail.width != maxDetailsWidth {
		t.Fatalf("pane stores %v; want %v", w.detail.width, maxDetailsWidth)
	}

	x := 960 - w.detail.shown - splitDividerWidth()
	tt.Press(x, 300)
	tt.Release(x, 300)
	tt.Key(0, ui.KeyRight)
	tt.Frame()

	if w.detail.shown >= settled {
		t.Fatalf("one Right press left the pane at %v; want it narrower than %v", w.detail.shown, settled)
	}
	if want := settled - detailKeyStep; w.detail.width != want {
		t.Fatalf("pane stores %v after Right; want %v", w.detail.width, want)
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

// A sidebar drag that the open pane leaves no room for must keep its full
// travel in the requested width, never the squeezed width on screen, so closing
// the pane brings the sidebar back to what the user asked for.
func TestSidebarDragKeepsTheRequestedWidthWhenSqueezed(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	defer a.Close()
	a.Selected = "all"
	a.Settings.SidebarWidth = 340
	a.Settings.DetailsWidth = defaultDetailsWidth
	a.Videos = []core.Video{{ID: "v-1", Title: "Video"}}
	a.SelectedVideoID = "v-1"

	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.view, 960, 640)
	tt.Frame()

	shown := detailWidthForRoom(defaultDetailsWidth, 960-minSidebarWidth-splitDividerWidth())
	visible := boundedSidebarWidth(340, shown, 960)
	if w.sidebarWidth != visible {
		t.Fatalf("sidebar shows %v; want the room's %v", w.sidebarWidth, visible)
	}

	const moves float32 = 3
	const step float32 = 10
	x := visible
	tt.Press(x, 320)
	for i := float32(1); i <= moves; i++ {
		tt.Move(x+i*step, 320)
		if grid := 960 - w.sidebarWidth - 2*splitDividerWidth() - w.detail.shown; grid < minGridWidth-0.01 {
			t.Fatalf("grid %v went below its %v minimum during the drag", grid, minGridWidth)
		}
		if w.sidebarWidth != visible {
			t.Fatalf("the blocked drag resized the sidebar to %v; want %v", w.sidebarWidth, visible)
		}
	}
	tt.Release(x+moves*step, 320)

	want := 340 + moves*step
	if got := a.Settings.SidebarWidth; got != want {
		t.Fatalf("the blocked drag stored %v; want the requested %v", got, want)
	}

	a.SelectedVideoID = ""
	tt.Frame()
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	if w.sidebarWidth != want {
		t.Fatalf("sidebar reopened at %v; want the remembered %v", w.sidebarWidth, want)
	}
}
