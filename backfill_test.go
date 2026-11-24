package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/simon/youtube-reader/core"
	mui "github.com/simon/youtube-reader/ui"

	"github.com/egoist/mygo/ui"
)

// stubYouTubeHandler answers the history endpoints from one function and restores
// the real transport when the test ends, so a page can be answered by the query
// it carries rather than by its path alone.
func stubYouTubeHandler(t *testing.T, answer func(*http.Request) (int, string)) func() {
	t.Helper()
	original := http.DefaultTransport
	http.DefaultTransport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		status, body := answer(req)
		return &http.Response{
			StatusCode: status,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})
	restore := func() { http.DefaultTransport = original }
	t.Cleanup(restore)
	return restore
}

// playlistBody is one playlistItems answer for the given uploads.
func playlistBody(published string, next string, ids ...string) string {
	items := make([]string, 0, len(ids))
	for _, id := range ids {
		items = append(items, fmt.Sprintf(`{"contentDetails":{"videoId":%q},"snippet":{"title":"Episode %s","publishedAt":%q}}`, id, id, published))
	}
	body := `{"items":[` + strings.Join(items, ",") + `]`
	if next != "" {
		return body + `,"nextPageToken":"` + next + `"}`
	}
	return body + `}`
}

// hydratedBody is one videos.list answer for every ID it was asked about.
func hydratedBody(ids string, duration string) string {
	items := []string{}
	for _, id := range strings.Split(ids, ",") {
		if id == "" {
			continue
		}
		items = append(items, fmt.Sprintf(`{"id":%q,"snippet":{"title":"Episode %s","channelId":"ch-1"},"contentDetails":{"duration":%q}}`, id, id, duration))
	}
	return `{"items":[` + strings.Join(items, ",") + `]}`
}

// historyApp is an app with one subscribed channel that has no videos yet: the
// channel page a reader clicks "load history" on.
func historyApp(t *testing.T) *App {
	t.Helper()
	a := newTestApp(t, t.TempDir())
	a.Settings.APIKey = "test"
	channel := core.Channel{ID: "ch-1", Title: "Rare channel", UploadedAt: "uploads-1"}
	a.Channels = []core.Channel{channel}
	a.Sync.Channels[channel.ID] = channel
	return a
}

// TestLoadEarlierVideosSavesThePageAndItsToken proves the whole path end to end:
// a page older than the refresh window is committed, marked as backfilled, and
// its page token is saved, so the next click reads the page before it instead of
// paying for the same one again.
func TestLoadEarlierVideosSavesThePageAndItsToken(t *testing.T) {
	a := historyApp(t)
	defer a.Close()
	old := time.Now().AddDate(-2, 0, 0).UTC().Format(time.RFC3339)
	var tokens []string
	stubYouTubeHandler(t, func(req *http.Request) (int, string) {
		if strings.HasSuffix(req.URL.Path, "/playlistItems") {
			token := req.URL.Query().Get("pageToken")
			tokens = append(tokens, token)
			switch token {
			case "":
				return http.StatusOK, playlistBody(old, "page-2", "v1", "v2")
			case "page-2":
				// The same video arrives on both pages, as an overlapping page
				// or a repeated click would bring it.
				return http.StatusOK, playlistBody(old, "", "v2", "v3")
			}
			t.Errorf("unexpected page token %q", token)
			return http.StatusOK, `{"items":[]}`
		}
		return http.StatusOK, hydratedBody(req.URL.Query().Get("id"), "PT20M0S")
	})

	target, ok := a.BackfillTarget("ch-1")
	if !ok {
		t.Fatal("the subscribed channel is not a history target")
	}
	result, err := a.LoadEarlierVideos(context.Background(), target, core.BackfillCursor{})
	if err != nil {
		t.Fatalf("load the first page: %v", err)
	}
	if len(result.Videos) != 2 || result.NextPageToken != "page-2" {
		t.Fatalf("first page = %+v; want two videos and page-2", result)
	}
	if err := a.reload(); err != nil {
		t.Fatal(err)
	}
	if cursor := a.BackfillCursors["ch-1"]; !cursor.Started || cursor.Token != "page-2" || cursor.Complete() {
		t.Fatalf("saved cursor = %+v; want the second page", cursor)
	}
	if len(a.Videos) != 2 {
		t.Fatalf("the store holds %d videos; want the page's 2", len(a.Videos))
	}
	for _, v := range a.Videos {
		if !v.IsBackfilled {
			t.Fatalf("stored video %q is not marked backfilled", v.ID)
		}
	}

	// The reader watches one of them and favorites it, then pages further back.
	a.MarkRead(core.Video{ID: "v1"})
	a.ToggleFavorite(core.Video{ID: "v1"})
	cursor := a.BackfillCursors["ch-1"]
	if _, err := a.LoadEarlierVideos(context.Background(), target, cursor); err != nil {
		t.Fatalf("load the second page: %v", err)
	}
	if err := a.reload(); err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 2 || tokens[1] != "page-2" {
		t.Fatalf("page tokens read %q; want the saved page-2 second", tokens)
	}
	if cursor := a.BackfillCursors["ch-1"]; !cursor.Complete() {
		t.Fatalf("cursor = %+v after the earliest page; want complete", cursor)
	}
	if len(a.Videos) != 3 {
		t.Fatalf("two pages with one video in common hold %d rows; want 3", len(a.Videos))
	}
	if !a.Sync.IsRead("v1") {
		t.Fatal("loading an earlier page reset a video's read state")
	}
	if !a.Sync.IsFavorite("v1") {
		t.Fatal("loading an earlier page dropped a favorite")
	}
}

// TestLoadEarlierVideosRefusesAnExhaustedChannel keeps a click that cannot read
// anything from spending a request: the channel's earliest video is loaded.
func TestLoadEarlierVideosRefusesAnExhaustedChannel(t *testing.T) {
	a := historyApp(t)
	defer a.Close()
	stubYouTubeHandler(t, func(req *http.Request) (int, string) {
		t.Errorf("an exhausted channel issued a %s request", req.URL.Path)
		return http.StatusOK, `{"items":[]}`
	})
	target, _ := a.BackfillTarget("ch-1")
	_, err := a.LoadEarlierVideos(context.Background(), target, core.BackfillCursor{Started: true})
	if !errors.Is(err, core.ErrBackfillExhausted) {
		t.Fatalf("click on an exhausted channel = %v; want ErrBackfillExhausted", err)
	}
}

// TestLoadEarlierVideosReportsAFailedPage proves a failure commits nothing and
// moves no cursor, so a retry reads the same page rather than skipping it.
func TestLoadEarlierVideosReportsAFailedPage(t *testing.T) {
	a := historyApp(t)
	defer a.Close()
	stubYouTubeHandler(t, func(req *http.Request) (int, string) {
		if strings.HasSuffix(req.URL.Path, "/playlistItems") {
			return http.StatusOK, playlistBody(time.Now().AddDate(-1, 0, 0).UTC().Format(time.RFC3339), "page-2", "v1")
		}
		return http.StatusInternalServerError, `{"error":{"message":"backend error"}}`
	})
	target, _ := a.BackfillTarget("ch-1")
	if _, err := a.LoadEarlierVideos(context.Background(), target, core.BackfillCursor{}); err == nil {
		t.Fatal("a page that could not be hydrated reported success")
	}
	if err := a.reload(); err != nil {
		t.Fatal(err)
	}
	if len(a.Videos) != 0 {
		t.Fatalf("a failed page stored %d videos; want none", len(a.Videos))
	}
	if cursor := a.BackfillCursors["ch-1"]; cursor.Started {
		t.Fatalf("a failed page advanced the cursor to %+v", cursor)
	}
}

// TestBackfillFailureTextNamesTheFailure keeps the failure readable: the two
// causes the app explains say so in the reader's language, and a transport
// failure hands back its recorded reason with the API key redacted.
func TestBackfillFailureTextNamesTheFailure(t *testing.T) {
	words := mui.ChannelHistoryCopy("en")
	if got := backfillFailureText(core.ErrQuota, "en"); got != words.Quota {
		t.Fatalf("quota failure = %q; want %q", got, words.Quota)
	}
	if got := backfillFailureText(core.ErrChannelNotFound, "en"); got != words.NoUploads {
		t.Fatalf("missing channel = %q; want %q", got, words.NoUploads)
	}
	const key = "AIzaSyD-backfill-failure-key"
	err := errors.New("Get \"https://www.googleapis.com/youtube/v3/playlistItems?key=" + key + "\": connection reset")
	got := backfillFailureText(err, "en")
	if strings.Contains(got, key) {
		t.Fatalf("the failure leaks the API key: %q", got)
	}
	if !strings.HasPrefix(got, words.Failed) {
		t.Fatalf("failure = %q; want it to open with %q", got, words.Failed)
	}
	if !strings.Contains(got, "connection reset") {
		t.Fatalf("failure = %q; want the recorded reason", got)
	}
}

// TestChannelHistoryControlInTheWindow covers the control's three states as the
// window draws them: the footer button under a channel's cards, the empty pane's
// primary action for a channel with nothing in the window, and the muted line
// once the channel's earliest video is loaded.
func TestChannelHistoryControlInTheWindow(t *testing.T) {
	old := time.Now().AddDate(-2, 0, 0).UTC().Format(time.RFC3339)
	video := core.Video{ID: "v1", ChannelID: "ch-a", ChannelTitle: "Alpha channel", Title: "Episode", PublishedAt: old}
	a := sidebarTestApp(t, true)
	a.Selected = "channel:ch-a"
	a.Videos = []core.Video{video}
	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.view, 1280, 808)
	tt.Frame()

	// A channel with videos offers the next page under its cards.
	if _, ok := tt.Find("Load 50 earlier videos"); !ok {
		t.Fatalf("a channel with videos shows no history button: %q", tt.Texts())
	}
	// A channel whose uploads the refresh window holds none of offers the same
	// read from the empty pane, so the dead end is gone.
	a.Videos = nil
	tt.Frame()
	if _, ok := tt.Find("Load history videos"); !ok {
		t.Fatalf("an empty channel shows no history action: %q", tt.Texts())
	}
	if _, ok := tt.Find("No recent videos from this channel"); !ok {
		t.Fatalf("an empty channel does not say why: %q", tt.Texts())
	}
	// The channel's earliest video loaded: the button becomes the muted line.
	a.Videos = []core.Video{video}
	a.BackfillCursors = map[string]core.BackfillCursor{"ch-a": {Started: true}}
	tt.Frame()
	if _, ok := tt.Find("All earlier videos loaded"); !ok {
		t.Fatalf("a fully loaded channel does not say so: %q", tt.Texts())
	}
	if _, ok := tt.Find("Load 50 earlier videos"); ok {
		t.Fatal("a fully loaded channel still offers a page")
	}
	// With nothing loaded either, the pane keeps its headline and offers no
	// button at all.
	a.Videos = nil
	tt.Frame()
	if _, ok := tt.Find("Load history videos"); ok {
		t.Fatalf("a channel with nothing earlier still offers the action: %q", tt.Texts())
	}
	if _, ok := tt.Find("This channel has no earlier videos. Refresh to check for new uploads."); !ok {
		t.Fatalf("the empty channel pane does not name the end: %q", tt.Texts())
	}
}

// TestChannelHistoryControlHoldsBackWhileOffline keeps the control honest about
// what it can do now: offline, it is disabled with the reason.
func TestChannelHistoryControlHoldsBackWhileOffline(t *testing.T) {
	video := core.Video{ID: "v1", ChannelID: "ch-a", ChannelTitle: "Alpha channel", Title: "Episode"}
	a := sidebarTestApp(t, true)
	a.Selected = "channel:ch-a"
	a.Videos = []core.Video{video}
	w := &Window{app: a, selectedIndex: new(int), offline: true}
	tt := ui.NewTester(w.view, 1280, 808)
	tt.Frame()
	if _, ok := tt.Find("Load 50 earlier videos"); !ok {
		t.Fatalf("the disabled button lost its label: %q", tt.Texts())
	}
}

// TestChannelHistoryScreenshots renders the channel page in each of the control's
// states, in both appearances, into PNGs under MYGO_CHANNEL_SCREENSHOTS. It
// draws the shipping view, so a review reads what the window shows rather than a
// mockup.
func TestChannelHistoryScreenshots(t *testing.T) {
	dir := os.Getenv("MYGO_CHANNEL_SCREENSHOTS")
	if dir == "" {
		t.Skip("MYGO_CHANNEL_SCREENSHOTS is unset")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	videos := make([]core.Video, 9)
	for i := range videos {
		videos[i] = core.Video{
			ID:           fmt.Sprintf("video-%d", i),
			Title:        fmt.Sprintf("An earlier episode %d", i),
			ChannelID:    "ch-a",
			ChannelTitle: "Alpha channel",
			PublishedAt:  now.AddDate(-2, -i/4, -i).UTC().Format(time.RFC3339),
			Duration:     "PT24M0S",
			IsBackfilled: true,
		}
	}
	type shot struct {
		name   string
		videos []core.Video
		cursor *core.BackfillCursor
		note   string
		busy   bool
	}
	shots := []shot{
		{name: "available", videos: videos, cursor: nil},
		{name: "loading", videos: videos, cursor: &core.BackfillCursor{Started: true, Token: "page-3"}, busy: true},
		{name: "loaded", videos: videos, cursor: &core.BackfillCursor{Started: true}},
		{name: "failed", videos: videos, cursor: nil, note: "Could not load earlier videos: The network connection was lost."},
		{name: "empty-channel", videos: nil, cursor: nil},
		{name: "empty-loaded", videos: nil, cursor: &core.BackfillCursor{Started: true}},
	}
	for _, dark := range []bool{false, true} {
		appearance := "light"
		if dark {
			appearance = "dark"
		}
		for _, s := range shots {
			a := sidebarTestApp(t, true)
			a.Selected = "channel:ch-a"
			a.Videos = s.videos
			a.BackfillCursors = map[string]core.BackfillCursor{}
			if s.cursor != nil {
				a.BackfillCursors["ch-a"] = *s.cursor
			}
			a.BackfillErr = s.note
			if s.busy {
				a.BackfillBusy = "ch-a"
			}
			w := &Window{app: a, selectedIndex: new(int)}
			tt := ui.NewTester(w.view, 1280, 808)
			tt.SetDark(dark)
			tt.SetScale(2)
			tt.Frame()
			tt.Advance(mui.MotionInteraction + 20*time.Millisecond)
			tt.Frame()
			var buf bytes.Buffer
			if err := png.Encode(&buf, tt.Image()); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "channel-history-"+s.name+"-"+appearance+".png")
			if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Logf("wrote %s", path)
		}
	}
}

// TestChannelHistoryEmptyPaneReportsAndHoldsOn covers the empty channel pane's
// two feedback states: a failure stays readable there too, and the reader's click
// in flight is named and cannot be pressed again.
func TestChannelHistoryEmptyPaneReportsAndHoldsOn(t *testing.T) {
	words := mui.ChannelHistoryCopy("en")
	a := sidebarTestApp(t, true)
	a.Selected = "channel:ch-a"
	a.Videos = nil
	a.BackfillErr = "Could not load earlier videos: The network connection was lost."
	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.view, 1280, 808)
	tt.Frame()
	if _, ok := tt.Find(a.BackfillErr); !ok {
		t.Fatalf("the empty channel pane hides the failure: %q", tt.Texts())
	}
	a.BackfillErr = ""
	a.BackfillBusy = "ch-a"
	tt.Frame()
	if _, ok := tt.Find(words.Loading); !ok {
		t.Fatalf("the empty channel pane does not name the page in flight: %q", tt.Texts())
	}
	if _, ok := tt.Find(words.Action); ok {
		t.Fatal("the empty channel pane still offers the action while a page is in flight")
	}
}

// TestStartBackfillDropsASecondClick keeps one page in flight: a click while the
// channel's page is still being read issues no request at all.
func TestStartBackfillDropsASecondClick(t *testing.T) {
	a := historyApp(t)
	defer a.Close()
	a.BackfillBusy = "ch-1"
	requests := 0
	stubYouTubeHandler(t, func(req *http.Request) (int, string) {
		requests++
		return http.StatusOK, `{"items":[]}`
	})
	w := &Window{app: a, selectedIndex: new(int)}
	w.startBackfill("ch-1")
	if requests != 0 {
		t.Fatalf("a second click issued %d requests while a page was in flight", requests)
	}
}
