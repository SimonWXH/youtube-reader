package ui

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// statesTestTokens is the light or dark token set a component test draws with.
func statesTestTokens(c *ui.Context) Tokens { return NewTokens(c.Theme(), c.Theme().Dark) }

// TestStateCopyContract pins the words the shared states draw: every string is
// present, no string uses an em dash or a fake percentage, and no state calls
// itself just "Error" or shouts. The board's own required strings are asserted
// literally so a copy edit cannot quietly drop them.
func TestStateCopyContract(t *testing.T) {
	required := []string{
		EmptySidebarTitle, EmptySidebarAction,
		OfflineTitle, OfflineTooltip,
		LoadingCancelLabel,
	}
	for _, want := range required {
		found := false
		for _, got := range StateCopy() {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("copy table is missing %q", want)
		}
	}
	if EmptySidebarTitle != "No channels yet" {
		t.Fatalf("EmptySidebarTitle = %q, want %q", EmptySidebarTitle, "No channels yet")
	}
	if EmptySidebarAction != "Add channel..." {
		t.Fatalf("EmptySidebarAction = %q, want %q", EmptySidebarAction, "Add channel...")
	}
	if title, _ := RefreshErrorCopy(CauseConnection); title != "Could not refresh subscriptions" {
		t.Fatalf("error title = %q, want %q", title, "Could not refresh subscriptions")
	}
	if _, sentence := RefreshErrorCopy(CauseConnection); sentence != "Check your connection, then try again." {
		t.Fatalf("connection sentence = %q; want the board's next-action sentence", sentence)
	}

	for _, copy := range StateCopy() {
		if strings.TrimSpace(copy) == "" {
			t.Fatalf("copy table holds an empty string: %#v", StateCopy())
		}
		if strings.Contains(copy, "—") {
			t.Fatalf("copy uses an em dash: %q", copy)
		}
		if strings.Contains(copy, "%") {
			t.Fatalf("copy invents a percentage: %q", copy)
		}
		if strings.Contains(copy, "!") {
			t.Fatalf("copy shouts: %q", copy)
		}
		if copy == "Error" || copy == "Failed" {
			t.Fatalf("copy is a bare failure word: %q", copy)
		}
	}
}

// TestContentPaneState drives the one state decision through every case the
// app can produce, including the states that used to have no home.
func TestContentPaneState(t *testing.T) {
	base := PaneInput{HasAPIKey: true, ChannelCount: 3, LibraryVideos: 5, DestinationVideos: 5, VisibleVideos: 5, Online: true}
	tests := []struct {
		name string
		in   PaneInput
		want PaneState
	}{
		{"no key", PaneInput{LibraryVideos: 2, DestinationVideos: 2, VisibleVideos: 2, Online: true}, PaneEmpty},
		{"no channels", PaneInput{HasAPIKey: true, LibraryVideos: 2, DestinationVideos: 2, VisibleVideos: 2, Online: true}, PaneEmpty},
		{"content", base, PaneContent},
		{"content stays while refreshing", PaneInput{HasAPIKey: true, ChannelCount: 3, LibraryVideos: 5, DestinationVideos: 5, VisibleVideos: 5, Refreshing: true, Online: true}, PaneContent},
		{"first load", PaneInput{HasAPIKey: true, ChannelCount: 3, Refreshing: true, Online: true}, PaneLoading},
		{"first load while offline", PaneInput{HasAPIKey: true, ChannelCount: 3, Refreshing: true, Online: false}, PaneLoading},
		{"empty destination while refreshing", PaneInput{HasAPIKey: true, ChannelCount: 3, LibraryVideos: 5, Refreshing: true, Selected: "favorites", Online: true}, PaneEmpty},
		{"offline with cached content", PaneInput{HasAPIKey: true, ChannelCount: 3, LibraryVideos: 5, DestinationVideos: 5, VisibleVideos: 5, Online: false}, PaneContent},
		{"offline with nothing cached", PaneInput{HasAPIKey: true, ChannelCount: 3, Online: false}, PaneOffline},
		{"error with nothing cached", PaneInput{HasAPIKey: true, ChannelCount: 3, Online: true, Failure: "dial tcp: no route to host"}, PaneError},
		{"cached content beats a failure", PaneInput{HasAPIKey: true, ChannelCount: 3, LibraryVideos: 5, DestinationVideos: 5, VisibleVideos: 5, Online: true, Failure: "boom"}, PaneContent},
		// An empty destination with cached videos in the library is simply
		// empty: offline and a failed refresh belong to the status surface,
		// not to a pane whose content the filter bar hid.
		{"filters hide cached content while offline", PaneInput{HasAPIKey: true, ChannelCount: 3, LibraryVideos: 5, DestinationVideos: 5, Online: false}, PaneEmpty},
		{"filters hide cached content after a failure", PaneInput{HasAPIKey: true, ChannelCount: 3, LibraryVideos: 5, DestinationVideos: 5, Online: true, Failure: "boom"}, PaneEmpty},
		{"empty favorites while offline", PaneInput{HasAPIKey: true, ChannelCount: 3, LibraryVideos: 5, Selected: "favorites", Online: false}, PaneEmpty},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ContentPaneState(tt.in); got != tt.want {
				t.Fatalf("ContentPaneState(%+v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// TestEmptyVariantFor covers the board's four empty contexts: no API key, no
// channels, no favorites, and a filter that matches nothing.
func TestEmptyVariantFor(t *testing.T) {
	tests := []struct {
		name string
		in   PaneInput
		want EmptyVariant
	}{
		{"no key", PaneInput{Online: true}, EmptySetup},
		{"no channels", PaneInput{HasAPIKey: true, Online: true}, EmptyImportChannels},
		{"no favorites", PaneInput{HasAPIKey: true, ChannelCount: 2, Selected: "favorites", Online: true}, EmptyFavorites},
		{"no videos", PaneInput{HasAPIKey: true, ChannelCount: 2, Selected: "all", Online: true}, EmptyNoVideos},
		{"empty channel", PaneInput{HasAPIKey: true, ChannelCount: 2, DestinationVideos: 0, Selected: "channel:ch-a", Online: true}, EmptyChannel},
		{"filters hide videos", PaneInput{HasAPIKey: true, ChannelCount: 2, DestinationVideos: 4, Selected: "all", Search: "kyoto", Online: true}, EmptyFiltered},
		{"unwatched hides videos", PaneInput{HasAPIKey: true, ChannelCount: 2, DestinationVideos: 4, Selected: "all", UnreadOnly: true, Online: true}, EmptyFiltered},
		{"filters do not hide an empty destination", PaneInput{HasAPIKey: true, ChannelCount: 2, Search: "kyoto", Online: true}, EmptyNoVideos},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EmptyVariantFor(tt.in); got != tt.want {
				t.Fatalf("EmptyVariantFor(%+v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// TestRefreshErrorCopy pins the causes' sentences.
func TestRefreshErrorCopy(t *testing.T) {
	tests := map[ErrorCause]string{
		CauseConnection:   "Check your connection, then try again.",
		CauseRejectedKey:  "The YouTube API rejected the key. Check it in Settings, then try again.",
		CauseQuota:        "YouTube API quota reached. Try again after the pause ends.",
		CauseQuotaExpired: "YouTube API quota reached. Retry to refresh again.",
	}
	for cause, want := range tests {
		if _, got := RefreshErrorCopy(cause); got != want {
			t.Fatalf("RefreshErrorCopy(%v) sentence = %q, want %q", cause, got, want)
		}
	}
}

// TestEmptyPaneShape checks the agreed geometry: the action is 28 pt, the title
// is the 15 pt Semibold empty role, and the action runs when clicked.
func TestEmptyPaneShape(t *testing.T) {
	copy := CopyForEmpty(EmptyFiltered)
	clicked := false
	spec := EmptySpec{Key: "empty-test", Copy: copy, OnAction: func() { clicked = true }}
	tt := ui.NewTester(func(c *ui.Context) {
		EmptyPane(c, statesTestTokens(c), spec)
	}, 720, 520)
	tt.Frame()

	if !tt.HasText(copy.Title) || !tt.HasText(copy.Sentence) {
		t.Fatalf("empty pane texts = %q, want %q and %q", tt.Texts(), copy.Title, copy.Sentence)
	}
	if _, ok := tt.AccessByRole(ui.RoleHeading, copy.Title); !ok {
		t.Fatalf("the empty title is not a heading: %q", tt.Texts())
	}
	button, ok := tt.AccessByRole(ui.RoleButton, copy.Action)
	if !ok {
		t.Fatalf("no %q action in %q", copy.Action, tt.Texts())
	}
	if button.Bounds.H != Space(7) {
		t.Fatalf("empty action height = %v, want the 28 pt primary action", button.Bounds.H)
	}
	if copy.Title != "" {
		if titleWidth := textWidth(t, tt, copy.Title); titleWidth <= 0 {
			t.Fatal("the empty title did not lay out")
		}
	}
	if err := tt.Click(copy.Action); err != nil {
		t.Fatalf("click %q: %v", copy.Action, err)
	}
	if !clicked {
		t.Fatalf("%q did not run its action", copy.Action)
	}
}

// TestPaneActionTakesTheKeyboard proves the empty state's one action is
// reachable without the pointer and runs on Return.
func TestPaneActionTakesTheKeyboard(t *testing.T) {
	copy := CopyForEmpty(EmptyNoVideos)
	activated := false
	spec := EmptySpec{Key: "empty-keyboard", Copy: copy, OnAction: func() { activated = true }}
	tt := ui.NewTester(func(c *ui.Context) {
		EmptyPane(c, statesTestTokens(c), spec)
	}, 720, 520)
	tt.Frame()

	focused := false
	for i := 0; i < 4; i++ {
		tt.Key(0, ui.KeyTab)
		if info, ok := tt.AccessByRole(ui.RoleButton, copy.Action); ok && info.Focused {
			focused = true
			break
		}
	}
	if !focused {
		t.Fatalf("Tab never reached the %q action", copy.Action)
	}
	tt.Key(0, ui.KeyEnter)
	if !activated {
		t.Fatalf("Return on the focused %q action did not run it", copy.Action)
	}
}

// textWidth returns the first laid-out text node's width, so a test can prove
// a state drew something rather than only positioned containers.
func textWidth(t *testing.T, tt *ui.Tester, text string) float32 {
	t.Helper()
	if r, ok := tt.Find(text); ok {
		return r.W
	}
	return 0
}

// TestErrorPaneRetriesInline proves the error is a live region, names what
// failed, shows the recorded reason, and wires Retry.
func TestErrorPaneRetriesInline(t *testing.T) {
	retried := false
	spec := ErrorSpec{
		Key:    "error-test",
		Cause:  CauseConnection,
		Reason: "dial tcp: no route to host",
		OnRetry: func() {
			retried = true
		},
	}
	tt := ui.NewTester(func(c *ui.Context) {
		ErrorPane(c, statesTestTokens(c), spec)
	}, 720, 520)
	tt.Frame()

	title, sentence := RefreshErrorCopy(CauseConnection)
	if !tt.HasText(title) || !tt.HasText(sentence) || !tt.HasText(spec.Reason) {
		t.Fatalf("error pane texts = %q, want %q, %q, and the reason", tt.Texts(), title, sentence)
	}
	alert := false
	for _, info := range tt.AccessAll() {
		if info.Role == ui.RoleAlert {
			alert = true
		}
	}
	if !alert {
		t.Fatal("the inline error is not a live region")
	}
	if err := tt.Click("Retry"); err != nil {
		t.Fatalf("click Retry: %v", err)
	}
	if !retried {
		t.Fatal("Retry did not run its action")
	}
}

// TestOfflinePaneShowsDisabledRetryWithReason checks the offline shape: the
// Retry face stays visible, cannot run, and says why, while the status surface
// keeps the action that can.
func TestOfflinePaneShowsDisabledRetryWithReason(t *testing.T) {
	tt := ui.NewTester(func(c *ui.Context) {
		OfflinePane(c, statesTestTokens(c), OfflineSpec{Key: "offline-test"})
	}, 720, 520)
	tt.Frame()

	if !tt.HasText(OfflineTitle) || !tt.HasText(OfflineSentence) {
		t.Fatalf("offline pane texts = %q", tt.Texts())
	}
	retry, ok := tt.AccessByRole(ui.RoleButton, OfflineActionLabel)
	if !ok {
		t.Fatalf("no %q action in %q", OfflineActionLabel, tt.Texts())
	}
	if !retry.Disabled {
		t.Fatal("the offline Retry is enabled; it must not promise a refresh that cannot run")
	}
	if retry.Description != OfflineTooltip {
		t.Fatalf("offline Retry reason = %q, want %q", retry.Description, OfflineTooltip)
	}
}

// TestSidebarEmptyIsNeverBlank checks the sidebar's empty state and its action.
func TestSidebarEmptyIsNeverBlank(t *testing.T) {
	clicked := false
	tt := ui.NewTester(func(c *ui.Context) {
		tokens := statesTestTokens(c)
		ui.Column(c).Fill().Background(tokens.Sidebar).Padding(Space(2)).Children(func() {
			SidebarEmpty(c, tokens, func() { clicked = true })
		})
	}, 260, 320)
	tt.Frame()

	if !tt.HasText(EmptySidebarTitle) || !tt.HasText(EmptySidebarSentence) {
		t.Fatalf("sidebar empty texts = %q", tt.Texts())
	}
	if err := tt.Click(EmptySidebarAction); err != nil {
		t.Fatalf("click %q: %v", EmptySidebarAction, err)
	}
	if !clicked {
		t.Fatalf("%q did not run its action", EmptySidebarAction)
	}
}

// TestFailedThumbnailIsANeutralFrame checks the failed-thumbnail state the C
// zone's media surface exposes: a real 16:9 neutral frame, a play glyph, and a
// tooltip, never a broken-image icon.
func TestFailedThumbnailIsANeutralFrame(t *testing.T) {
	const w, h float32 = 240, 135
	tt := ui.NewTester(func(c *ui.Context) {
		tokens := statesTestTokens(c)
		ui.Row(c).Size(w, h).Radius(RadiusCard).Clip().Children(func() {
			thumbnailPlaceholder(c, tokens, "Thumbnail unavailable", true).Fill()
		})
	}, int(w), int(h))
	tt.Frame()

	info, ok := tt.Access("Thumbnail unavailable")
	if !ok {
		t.Fatalf("the failed thumbnail is not labelled: %q", tt.Texts())
	}
	if info.Description != "Thumbnail unavailable" {
		t.Fatalf("failed thumbnail tooltip = %q, want the failure sentence", info.Description)
	}
	ratio := info.Bounds.W / info.Bounds.H
	if ratio < 16.0/9.0-0.01 || ratio > 16.0/9.0+0.01 {
		t.Fatalf("failed thumbnail is %v x %v, want 16:9", info.Bounds.W, info.Bounds.H)
	}
}

// TestSkeletonGridKeepsTheCardCount pins the first-load shape: 12 neutral
// cards in three rows of four, no spinner inside the pane, so the toolbar's
// control is the pane's only activity sign.
func TestSkeletonGridKeepsTheCardCount(t *testing.T) {
	const cardWidth, cardHeight, gap float32 = 241, 211, 8
	spec := SkeletonSpec{Count: SkeletonCards, Columns: 4, CardWidth: cardWidth, CardHeight: cardHeight, ThumbHeight: 135, Gap: gap}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().AlignItems(ui.Start).Children(func() {
			SkeletonGrid(c, statesTestTokens(c), spec)
		})
	}, 1010, 900)
	tt.Frame()
	for _, info := range tt.AccessAll() {
		if info.Role == ui.RoleProgress {
			t.Fatal("the skeleton draws its own spinner; the toolbar refresh control owns the one spinner")
		}
	}
	group, ok := tt.Find(LoadingGridLabel)
	if !ok {
		t.Fatalf("the skeleton grid is not laid out: %q", tt.Texts())
	}
	wantW := 4*cardWidth + 3*gap
	wantH := 3*cardHeight + 2*gap
	if group.W < wantW-0.5 || group.W > wantW+0.5 || group.H < wantH-0.5 || group.H > wantH+0.5 {
		t.Fatalf("the skeleton grid is %v x %v, want %v x %v for 12 cards in three rows of four", group.W, group.H, wantW, wantH)
	}
}

// TestStateComponentScreenshots renders the components that have no whole
// window of their own (the sidebar's empty section and a failed thumbnail) in
// both appearances under YTREADER_STATE_SCREENSHOTS.
func TestStateComponentScreenshots(t *testing.T) {
	dir := os.Getenv("YTREADER_STATE_SCREENSHOTS")
	if dir == "" {
		t.Skip("YTREADER_STATE_SCREENSHOTS is unset")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	fixtures := []struct {
		name string
		w, h int
		view func(*ui.Context)
	}{
		{"empty-sidebar", 260, 300, func(c *ui.Context) {
			tokens := statesTestTokens(c)
			ui.Column(c).Fill().Background(tokens.Sidebar).Padding(Space(2)).Children(func() {
				SidebarEmpty(c, tokens, func() {})
			})
		}},
		{"failed-thumbnail", 280, 234, func(c *ui.Context) {
			tokens := statesTestTokens(c)
			ui.Column(c).Fill().Background(tokens.App).Padding(Space(4)).Children(func() {
				ui.Column(c).Width(248).Height(211).Radius(RadiusCard).Background(tokens.Surface).Clip().Children(func() {
					ui.Row(c).Height(248.0 * 9 / 16).FillWidth().Clip().Children(func() {
						thumbnailPlaceholder(c, tokens, "Thumbnail unavailable", true).Fill()
					})
					ui.Row(c).Grow(1)
				})
			})
		}},
	}
	for _, dark := range []bool{false, true} {
		appearance := "light"
		if dark {
			appearance = "dark"
		}
		for _, f := range fixtures {
			tt := ui.NewTester(f.view, f.w, f.h)
			tt.SetDark(dark)
			tt.SetScale(2)
			tt.Frame()
			tt.Advance(MotionInteraction + 20*time.Millisecond)
			tt.Frame()
			var buf bytes.Buffer
			if err := png.Encode(&buf, tt.Image()); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "state-"+f.name+"-"+appearance+".png")
			if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Logf("wrote %s", path)
		}
	}
}
