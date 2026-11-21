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
	tt.Key(0, ui.KeyLeft)
	tt.Frame()

	if w.detail.width != maxDetailsWidth {
		t.Fatalf("Left at the squeezed max stored %v; want the request %v", w.detail.width, maxDetailsWidth)
	}
	if w.detail.shown != settled {
		t.Fatalf("Left at the squeezed max moved the pane to %v; want %v", w.detail.shown, settled)
	}
	if got := a.Settings.DetailsWidth; got != maxDetailsWidth {
		t.Fatalf("Left at the squeezed max persisted %v; want %v", got, maxDetailsWidth)
	}

	tt.Key(0, ui.KeyRight)
	tt.Frame()

	if w.detail.shown >= settled {
		t.Fatalf("one Right press left the pane at %v; want it narrower than %v", w.detail.shown, settled)
	}
	if want := settled - detailKeyStep; w.detail.width != want {
		t.Fatalf("pane stores %v after Right; want %v", w.detail.width, want)
	}
}

// Each divider's declared grip must take a press on both sides of its 1 pt
// line, not only exactly on the seam: the pane-facing half used to belong to
// the pane committed after it.
func TestDividersTakePressesOnBothSidesOfTheSeam(t *testing.T) {
	video := core.Video{ID: "v-1", Title: "Video"}
	a := detailTestApp(t, []core.Video{video}, video.ID)
	defer a.Close()
	a.Settings.SidebarWidth = maxSidebarWidth
	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.view, 1280, 840)
	tt.Frame()

	seam := 1280 - w.detail.shown - splitDividerWidth()
	tt.Press(seam+3, 300)
	tt.Move(seam+3-detailKeyStep, 300)
	tt.Release(seam+3-detailKeyStep, 300)
	want := defaultDetailsWidth + detailKeyStep
	if w.detail.width != want {
		t.Fatalf("a press 3 pt right of the seam stored %v; want %v", w.detail.width, want)
	}

	seam = 1280 - w.detail.shown - splitDividerWidth()
	tt.Press(seam-3, 300)
	tt.Move(seam-3+detailKeyStep, 300)
	tt.Release(seam-3+detailKeyStep, 300)
	want = defaultDetailsWidth
	if w.detail.width != want {
		t.Fatalf("a press 3 pt left of the seam stored %v; want %v", w.detail.width, want)
	}

	sidebarSeam := w.sidebarWidth
	tt.Press(sidebarSeam+2, 300)
	tt.Move(sidebarSeam+2-10, 300)
	tt.Release(sidebarSeam+2-10, 300)
	sidebarWant := sidebarSeam - 10
	if w.sidebarWidth != sidebarWant {
		t.Fatalf("a press 2 pt right of the sidebar seam left it at %v; want %v", w.sidebarWidth, sidebarWant)
	}

	sidebarSeam = w.sidebarWidth
	tt.Press(sidebarSeam-2, 300)
	tt.Move(sidebarSeam-2-10, 300)
	tt.Release(sidebarSeam-2-10, 300)
	sidebarWant = sidebarSeam - 10
	if w.sidebarWidth != sidebarWant {
		t.Fatalf("a press 2 pt left of the sidebar seam left it at %v; want %v", w.sidebarWidth, sidebarWant)
	}
	if got := a.Settings.SidebarWidth; got != sidebarWant {
		t.Fatalf("the sidebar seam press persisted %v; want %v", got, sidebarWant)
	}
}

// A divider key steps from the settled width, not from the width the open
// animation is passing through, so a press during the animation cannot
// replace the chosen width with the transient one.
func TestDetailDividerKeyStepsFromTheSettledWidth(t *testing.T) {
	video := core.Video{ID: "v-1", Title: "Video"}
	a := detailTestApp(t, []core.Video{video}, "")
	defer a.Close()
	a.Settings.DetailsWidth = defaultDetailsWidth

	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.view, 1280, 840)
	tt.Frame()

	a.SelectedVideoID = video.ID
	tt.Frame()
	tt.Advance(60 * time.Millisecond)
	tt.Frame()
	if w.detail.shown <= 0 || w.detail.shown >= defaultDetailsWidth-0.5 {
		t.Fatalf("pane width %v is not mid-animation", w.detail.shown)
	}

	x := 1280 - w.detail.shown - splitDividerWidth()
	tt.Press(x, 300)
	tt.Release(x, 300)
	tt.Key(0, ui.KeyLeft)
	tt.Frame()

	want := defaultDetailsWidth + detailKeyStep
	if w.detail.width != want {
		t.Fatalf("pane stores %v after Left; want the settled %v", w.detail.width, want)
	}
	if w.detail.shown != want {
		t.Fatalf("pane shows %v after Left; want %v", w.detail.shown, want)
	}
	if got := a.Settings.DetailsWidth; got != want {
		t.Fatalf("pane persisted %v after Left; want %v", got, want)
	}
}

// A drag that starts during the open animation bases on the settled width
// too, so the width the animation is passing through cannot replace the
// chosen one.
func TestDetailDragStepsFromTheSettledWidth(t *testing.T) {
	video := core.Video{ID: "v-1", Title: "Video"}
	a := detailTestApp(t, []core.Video{video}, "")
	defer a.Close()

	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.view, 1280, 840)
	tt.Frame()

	a.SelectedVideoID = video.ID
	tt.Frame()
	tt.Advance(60 * time.Millisecond)
	tt.Frame()
	if w.detail.shown <= 0 || w.detail.shown >= defaultDetailsWidth-0.5 {
		t.Fatalf("pane width %v is not mid-animation", w.detail.shown)
	}

	x := 1280 - w.detail.shown - splitDividerWidth()
	tt.Press(x, 300)
	tt.Move(x-8, 300)
	tt.Release(x-8, 300)

	want := defaultDetailsWidth + detailKeyStep
	if w.detail.width != want {
		t.Fatalf("pane stores %v after an 8 pt drag; want the settled %v", w.detail.width, want)
	}
	if w.detail.shown != want {
		t.Fatalf("pane shows %v after the drag; want %v", w.detail.shown, want)
	}
	if got := a.Settings.DetailsWidth; got != want {
		t.Fatalf("pane persisted %v after the drag; want %v", got, want)
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

// The open pane squeezes the sidebar, but the squeeze never rewrites the
// stored width: a drag with no room changes nothing, and one that moves
// stores exactly the width on screen, so closing the pane keeps the user's
// last choice.
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

	shown := detailWidthForRoom(defaultDetailsWidth, 960-minSidebarWidth-splitDividerWidth())
	visible := boundedSidebarWidth(maxSidebarWidth, shown, 960)
	if visible != 298 {
		t.Fatalf("the room leaves %v for the sidebar; want 298", visible)
	}
	if w.sidebarWidth != visible {
		t.Fatalf("sidebar shows %v; want the room's %v", w.sidebarWidth, visible)
	}

	x := visible
	tt.Press(x, 320)
	for i := float32(1); i <= 3; i++ {
		tt.Move(x+i*10, 320)
		if w.sidebarWidth != visible {
			t.Fatalf("the blocked drag resized the sidebar to %v; want %v", w.sidebarWidth, visible)
		}
		if w.gridAvailableWidth < minGridWidth-0.01 {
			t.Fatalf("grid %v fell below its %v minimum during the blocked drag", w.gridAvailableWidth, minGridWidth)
		}
	}
	tt.Release(x+30, 320)
	if got := a.Settings.SidebarWidth; got != maxSidebarWidth {
		t.Fatalf("the blocked drag stored %v; want the requested %v", got, maxSidebarWidth)
	}

	// The blocked travel is kept: coming back 10 pt still leaves the pointer
	// right of the divider, so the divider has not moved yet.
	tt.Press(x, 320)
	tt.Move(x+30, 320)
	tt.Move(x+20, 320)
	if w.sidebarWidth != visible {
		t.Fatalf("the divider moved to %v while the pointer was still right of it; want %v", w.sidebarWidth, visible)
	}
	tt.Release(x+20, 320)

	tt.Press(x, 320)
	tt.Move(x-10, 320)
	moved := visible - 10
	if w.sidebarWidth != moved {
		t.Fatalf("the first narrow step left the sidebar at %v; want %v", w.sidebarWidth, moved)
	}
	if w.gridAvailableWidth < minGridWidth-0.01 {
		t.Fatalf("grid %v fell below its %v minimum during the narrowing drag", w.gridAvailableWidth, minGridWidth)
	}
	if got := a.Settings.SidebarWidth; got != moved {
		t.Fatalf("the narrowing drag stored %v; want the visible %v", got, moved)
	}
	tt.Release(x-10, 320)

	a.SelectedVideoID = ""
	tt.Frame()
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	if w.sidebarWidth != moved {
		t.Fatalf("sidebar reopened at %v; want the chosen %v", w.sidebarWidth, moved)
	}
}
