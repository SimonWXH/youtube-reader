package main

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
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

// A description whose seventh line the six-line clamp hides must offer More,
// and one that fits in six lines must not: the shaped line count decides, not
// a height measured with a different line height.
func TestDescriptionMoreTracksRenderedLines(t *testing.T) {
	six := []string{"One line.", "Two lines.", "Three lines.", "Four.", "Five.", "Six."}
	short := core.Video{ID: "v-short", Title: "Short description", Description: strings.Join(six, "\n")}
	long := core.Video{ID: "v-long", Title: "Long description", Description: strings.Join(append(six, "Seven."), "\n")}

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
