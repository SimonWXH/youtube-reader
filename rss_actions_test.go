package main

import (
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
)

// openRSSReaderFullTextArticle opens the seeded full-text article in the reader
// and returns its row, so an action test starts from the page a reader sees.
func openRSSReaderFullTextArticle(t *testing.T, tt *ui.Tester) ui.Rect {
	t.Helper()
	row, ok := tt.Find("The Quiet Return of the Open Feed")
	if !ok {
		t.Fatalf("article row not found: %v", tt.Texts())
	}
	tt.ClickAt(row.X+row.W/2, row.Y+row.H/2)
	tt.Frame()
	return row
}

// TestRSSReaderActionGroupIsSixIcons pins the action group': its order, its
// boxes, and the disabled and copied states each control carries.
func TestRSSReaderActionGroupIsSixIcons(t *testing.T) {
	a, w, tt := rssReaderTestWindow(t)
	openRSSReaderFullTextArticle(t, tt)

	want := []string{"Mark as Read (M)", "Star article (S)", "Show Original", "Share", "Open in Browser", "Refetch"}
	if got := rssHeaderActionNames(t, tt); !slices.Equal(got, want) {
		t.Fatalf("header actions = %q, want %q", got, want)
	}

	// The six boxes draw left to right in that order, each the 24 pt the
	// reference gives it, and 1.5 pt apart.
	area := rssHeaderActionsArea(t, tt)
	var last ui.Rect
	for i, name := range want {
		box := rssHeaderAction(t, tt, name).Bounds
		if box.W != rssHeaderActionSize || box.H != rssHeaderActionSize {
			t.Fatalf("%s is %vx%v pt, want the %v pt box", name, box.W, box.H, rssHeaderActionSize)
		}
		if i > 0 {
			if gap := box.X - (last.X + last.W); gap != rssHeaderActionGap {
				t.Fatalf("%s starts %v pt after %s, want the %v pt optical gap", name, gap, want[i-1], rssHeaderActionGap)
			}
		}
		last = box
	}
	if last.X+last.W > area.X+area.W+0.5 {
		t.Fatalf("the last action at %v runs past the group %v", last, area)
	}

	// Every control but share acts on this article, which carries a link, and
	// every one of them is reachable without a mouse.
	for _, name := range want {
		info := rssHeaderAction(t, tt, name)
		if info.Disabled {
			t.Fatalf("%s is disabled on an article it can act on", name)
		}
		if !info.Focusable {
			t.Fatalf("%s cannot take the keyboard focus", name)
		}
	}

	// An article without a link cannot be shared, and says why.
	linkless := rssReaderTestArticle()
	linkless.ID, linkless.Link = "art-linkless", ""
	if err := a.Store.SaveRSSArticles(context.Background(), []core.RSSArticle{linkless}); err != nil {
		t.Fatalf("save the linkless article: %v", err)
	}
	if err := a.ReloadRSS(); err != nil {
		t.Fatalf("reload rss: %v", err)
	}
	a.SelectedRSSArticleID = linkless.ID
	tt.Frame()
	share := rssHeaderAction(t, tt, "Share")
	if !share.Disabled {
		t.Fatal("share is enabled on an article without a link")
	}
	if share.Description != "This article has no link" {
		t.Fatalf("share's tooltip = %q, want the reason it cannot act", share.Description)
	}
	_ = w

	// The Chinese group carries the same six controls in the same order, named
	// in the reader's language.
	tt.SetLocale("zh")
	tt.Frame()
	zhWant := []string{"标记为已读 (M)", "标星 (S)", "显示原文", "分享", "在浏览器中打开", "重新抓取"}
	if got := rssHeaderActionNames(t, tt); !slices.Equal(got, zhWant) {
		t.Fatalf("Chinese header actions = %q, want %q", got, zhWant)
	}
	if got := rssHeaderAction(t, tt, "分享").Description; got != "这篇文章没有链接" {
		t.Fatalf("Chinese share tooltip = %q, want the reason it cannot act", got)
	}
}

// TestRSSReaderReadToggleFlipsAndPersists covers the manual read toggle: it
// flips the open article's flag, the row's unread mark follows, the sidebar
// count follows, and the state the store keeps is the one the next launch
// reads.
func TestRSSReaderReadToggleFlipsAndPersists(t *testing.T) {
	a, w, tt := rssReaderTestWindow(t)
	openRSSReaderFullTextArticle(t, tt)

	if a.SelectedRSSArticle().Read {
		t.Fatal("the seeded article starts read")
	}
	before := a.RSSTotalUnread
	clickRSSHeaderAction(t, tt, "Mark as Read (M)")

	if !a.SelectedRSSArticle().Read {
		t.Fatal("the read toggle did not mark the article read")
	}
	if got := a.RSSTotalUnread; got != before-1 {
		t.Fatalf("total unread = %d after marking one read, want %d", got, before-1)
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "Mark as Unread (M)"); !ok {
		t.Fatalf("the toggle did not take the read state's name: %q", rssHeaderActionNames(t, tt))
	}
	if got := a.SelectedRSSArticleID; got != "art-reader" {
		t.Fatalf("the toggle moved the selection to %q", got)
	}

	// The store keeps what the toggle wrote, so the next read of the article
	// finds it read.
	stored, err := a.Store.GetRSSArticle(context.Background(), "art-reader")
	if err != nil || stored == nil {
		t.Fatalf("read the article back: %v, %v", stored, err)
	}
	if !stored.Read {
		t.Fatal("the store kept the article unread")
	}

	// A second press flips it back, and the count returns with it.
	clickRSSHeaderAction(t, tt, "Mark as Unread (M)")
	if a.SelectedRSSArticle().Read {
		t.Fatal("the read toggle did not mark the article unread again")
	}
	if got := a.RSSTotalUnread; got != before {
		t.Fatalf("total unread = %d after flipping back, want %d", got, before)
	}
	_ = w
}

// TestRSSReaderStarActionStillToggles guards the star that predates this change:
// the box keeps its place, flips the article, and moves the sidebar count.
func TestRSSReaderStarActionStillToggles(t *testing.T) {
	a, _, tt := rssReaderTestWindow(t)
	openRSSReaderFullTextArticle(t, tt)

	if a.SelectedRSSArticle().Starred {
		t.Fatal("the seeded article starts starred")
	}
	before := a.RSSStarredTotal
	clickRSSHeaderAction(t, tt, "Star article (S)")
	if !a.SelectedRSSArticle().Starred {
		t.Fatal("the star action did not star the article")
	}
	if got := a.RSSStarredTotal; got != before+1 {
		t.Fatalf("the starred count = %d after starring one, want %d", got, before+1)
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "Unstar article (S)"); !ok {
		t.Fatalf("the star did not take its starred state's name: %q", rssHeaderActionNames(t, tt))
	}
	clickRSSHeaderAction(t, tt, "Unstar article (S)")
	if a.SelectedRSSArticle().Starred {
		t.Fatal("the star action did not unstar the article")
	}
}

// TestRSSReaderActionsTakeTheKeyboard proves the header's controls are reachable
// without a mouse: Tab walks the group, and Return runs the control it lands on.
func TestRSSReaderActionsTakeTheKeyboard(t *testing.T) {
	a, _, tt := rssReaderTestWindow(t)
	openRSSReaderFullTextArticle(t, tt)

	for _, name := range rssHeaderActionNames(t, tt) {
		reached := false
		for range 200 {
			if rssHeaderAction(t, tt, name).Focused {
				reached = true
				break
			}
			tt.Key(0, ui.KeyTab)
		}
		if !reached {
			t.Fatalf("Tab never reached %q", name)
		}
	}
	// The read toggle ran by keyboard behaves exactly as its click does.
	for range 200 {
		if rssHeaderAction(t, tt, "Mark as Read (M)").Focused {
			break
		}
		tt.Key(0, ui.KeyTab)
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if !a.SelectedRSSArticle().Read {
		t.Fatal("Return on the read toggle did not mark the article read")
	}
}

// TestRSSReaderShareMenuCopiesBothItems covers the share menu: exactly its two
// commands, each putting exactly the expected text on the clipboard.
func TestRSSReaderShareMenuCopiesBothItems(t *testing.T) {
	// The whole shell, because the copy's confirmation is a toast the window's
	// own layer draws.
	_, w := rssReaderTestApp(t)
	tt := ui.NewTester(w.shell, 1280, 808)
	tt.Frame()
	const link = "https://feeds.example.com/quiet-open-feed"
	const title = "The Quiet Return of the Open Feed"
	openRSSReaderFullTextArticle(t, tt)

	for _, tc := range []struct {
		locale   string
		items    []string
		copyLink string
		copyBoth string
		copied   [2]string
		wants    [2]string
	}{
		{
			locale:   "en",
			items:    []string{"Copy Link", "Copy Title and Link"},
			copyLink: "Copy Link", copyBoth: "Copy Title and Link",
			copied: [2]string{"Link copied", "Title and link copied"},
			wants:  [2]string{link, title + "\n" + link},
		},
		{
			locale:   "zh",
			items:    []string{"复制链接", "复制标题与链接"},
			copyLink: "复制链接", copyBoth: "复制标题与链接",
			copied: [2]string{"已复制链接", "已复制标题与链接"},
			wants:  [2]string{link, title + "\n" + link},
		},
	} {
		tt.SetLocale(tc.locale)
		tt.SetClipboard("")
		tt.Frame()
		clickRSSHeaderAction(t, tt, rssShareWords(tc.locale).action)
		if got := tt.Menu(); !slices.Equal(got, tc.items) {
			t.Fatalf("%s: share menu = %q, want exactly %q", tc.locale, got, tc.items)
		}
		if err := tt.ChooseMenuItem(tc.copyLink); err != nil {
			t.Fatalf("%s: choose %q: %v", tc.locale, tc.copyLink, err)
		}
		tt.Frame()
		if got := tt.Clipboard(); got != tc.wants[0] {
			t.Fatalf("%s: %q copied %q, want %q", tc.locale, tc.copyLink, got, tc.wants[0])
		}
		if !tt.HasText(tc.copied[0]) {
			t.Fatalf("%s: the copy left no confirmation: %v", tc.locale, tt.Texts())
		}

		tt.SetClipboard("")
		clickRSSHeaderAction(t, tt, rssShareWords(tc.locale).action)
		if err := tt.ChooseMenuItem(tc.copyBoth); err != nil {
			t.Fatalf("%s: choose %q: %v", tc.locale, tc.copyBoth, err)
		}
		tt.Frame()
		if got := tt.Clipboard(); got != tc.wants[1] {
			t.Fatalf("%s: %q copied %q, want %q", tc.locale, tc.copyBoth, got, tc.wants[1])
		}
	}
}

// TestRSSReaderBodyToggleSwitchesOriginalAndFullText covers the original/full
// text toggle: an article that holds both bodies switches between them, an
// article that holds only its summary asks for the text instead, and an article
// written before the feed's body was kept falls back to its summary rather than
// a blank page.
func TestRSSReaderBodyToggleSwitchesOriginalAndFullText(t *testing.T) {
	a, w, tt := rssReaderTestWindow(t)

	// Both bodies on one article: the full text it was extracted from, and the
	// feed's own version, which the store keeps beside it.
	art := rssReaderTestArticle()
	art.ContentHTML = "<p>the extracted full text</p>"
	art.Blocks = []core.ArticleBlock{{Type: core.BlockParagraph, Text: "the extracted full text"}}
	art.FeedContentHTML = "<p>the feed's own version</p>"
	if err := a.Store.SaveRSSArticles(context.Background(), []core.RSSArticle{art}); err != nil {
		t.Fatalf("save the article's two bodies: %v", err)
	}
	if err := a.ReloadRSS(); err != nil {
		t.Fatalf("reload rss: %v", err)
	}
	openRSSReaderFullTextArticle(t, tt)

	if !tt.HasText("the extracted full text") || tt.HasText("the feed's own version") {
		t.Fatalf("the reader did not open on the full text: %v", tt.Texts())
	}
	clickRSSHeaderAction(t, tt, "Show Original")
	if !tt.HasText("the feed's own version") || tt.HasText("the extracted full text") {
		t.Fatalf("the toggle did not show the feed's own version: %v", tt.Texts())
	}
	if !w.rssReader.viewFeed {
		t.Fatal("the toggle did not record the article's original view")
	}
	if got := rssHeaderActionNames(t, tt)[2]; got != "Show Full Text" {
		t.Fatalf("the toggle reads %q while the original shows, want the way back to the full text", got)
	}
	// The original is a whole article, not a fallback: no fetch line above it.
	if tt.HasText("Showing the feed's summary") || tt.HasText("Couldn't fetch the full text") {
		t.Fatalf("the original view carried a fetch notice: %v", tt.Texts())
	}
	clickRSSHeaderAction(t, tt, "Show Full Text")
	if !tt.HasText("the extracted full text") {
		t.Fatalf("the toggle did not return to the full text: %v", tt.Texts())
	}

	// A new selection starts on the full text again: the view belongs to one
	// article, not to the reader.
	clickRSSHeaderAction(t, tt, "Show Original")
	row, ok := tt.Find("A Summary Only Post")
	if !ok {
		t.Fatal("the summary article's row is missing")
	}
	tt.ClickAt(row.X+row.W/2, row.Y+row.H/2)
	tt.Frame()
	tt.ClickAt(row.X+row.W/2, row.Y+row.H/2)
	tt.Frame()
	if w.rssReader.viewFeed {
		t.Fatal("switching articles kept the previous article's view")
	}

	// An article whose feed body was never kept falls back to its summary, with
	// the notice that says so, rather than a blank page.
	old := rssReaderTestArticle()
	old.ID, old.FeedContentHTML = "art-old", ""
	old.ContentHTML = "<p>text extracted before the feed's own version was kept</p>"
	old.Blocks = []core.ArticleBlock{{Type: core.BlockParagraph, Text: "text extracted before the feed's own version was kept"}}
	if err := a.Store.SaveRSSArticles(context.Background(), []core.RSSArticle{old}); err != nil {
		t.Fatalf("save the older article: %v", err)
	}
	if err := a.ReloadRSS(); err != nil {
		t.Fatalf("reload rss: %v", err)
	}
	a.SelectedRSSArticleID = old.ID
	tt.Frame()
	clickRSSHeaderAction(t, tt, "Show Original")
	if !tt.HasText("For a decade the open web's newsreading protocol") {
		t.Fatalf("an article without a kept feed body showed nothing: %v", tt.Texts())
	}
	if !tt.HasText("Showing the feed's summary. Open the original article in your browser for the full text.") {
		t.Fatalf("the fallback did not say it is the feed's summary: %v", tt.Texts())
	}
}

// TestRSSReaderBodyToggleFetchesUntilTheTextArrives covers the toggle on an
// article that holds only its summary: it starts the same fetch every other
// part of the reader uses, shows the fetching state, then offers the retry the
// fetch that gave up leaves behind.
func TestRSSReaderBodyToggleFetchesUntilTheTextArrives(t *testing.T) {
	_, w, tt := rssReaderTestWindow(t)
	deliveries := withAsyncRSSFullText(t)
	release := make(chan struct{})
	started := make(chan string, 1)
	w.onRefetchRSSArticle = func(ctx context.Context, articleID string) (core.RSSArticle, error) {
		started <- articleID
		<-release
		return core.RSSArticle{}, errRSSFetchRefused
	}

	// The seeded summary article opens on its own fetch; let that one give up
	// first, so the toggle starts from the failed state.
	row, ok := tt.Find("A Summary Only Post")
	if !ok {
		t.Fatal("summary article row not found")
	}
	tt.ClickAt(row.X+row.W/2, row.Y+row.H/2)
	tt.Frame()
	<-started
	close(release)
	select {
	case deliver := <-deliveries:
		deliver()
	case <-time.After(2 * time.Second):
		t.Fatal("the failed fetch never reached the window")
	}
	tt.Frame()
	if _, ok := tt.AccessByRole(ui.RoleButton, "Retry Full Text"); !ok {
		t.Fatalf("the toggle does not offer the retry after a failure: %q", rssHeaderActionNames(t, tt))
	}
	if got := rssHeaderAction(t, tt, "Refetch").Description; got != "Retry Full Text" {
		t.Fatalf("the refresh tooltip = %q, want the retry", got)
	}
}

// errRSSFetchRefused stands in for an article page the site refused.
var errRSSFetchRefused = &rssFetchRefusedError{}

// rssFetchRefusedError is the error a stubbed extraction returns when the site
// refuses the page, so a test does not depend on the network's own wording.
type rssFetchRefusedError struct{}

func (*rssFetchRefusedError) Error() string { return "the site refused the page" }

// TestRSSAirScreenshots renders the reading pane's action group and the sidebar
// in both languages under YTREADER_AIR_SCREENSHOTS, from the shipping view, so
// the reference comparison reads the app's own pixels.
func TestRSSAirScreenshots(t *testing.T) {
	dir := os.Getenv("YTREADER_AIR_SCREENSHOTS")
	if dir == "" {
		t.Skip("YTREADER_AIR_SCREENSHOTS is unset")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, locale := range []string{"en", "zh"} {
		tt := rssAirShotWindow(t, locale)
		// The shots show the settled palette, not a frame mid-fade.
		tt.Advance(300 * time.Millisecond)
		tt.Frame()
		saveRSSAirShot(t, tt, filepath.Join(dir, "reader-"+locale+".png"), image.Rect(0, 0, 0, 0))

		// The action group and the metadata row around it, enlarged so the
		// six boxes and their spacing read at their own scale.
		header, ok := tt.AccessByRole(ui.RoleGroup, "Article header")
		if !ok {
			t.Fatal("the reader header has no accessible group")
		}
		crop := image.Rect(int(header.Bounds.X), int(header.Bounds.Y),
			int(header.Bounds.X+header.Bounds.W), int(header.Bounds.Y+header.Bounds.H))
		saveRSSAirShot(t, tt, filepath.Join(dir, "toolbar-"+locale+".png"), crop)

		// The smart feeds and the feed rows under them.
		sidebar := image.Rect(0, int(header.Bounds.Y)-80, int(header.Bounds.X), int(header.Bounds.Y)+260)
		saveRSSAirShot(t, tt, filepath.Join(dir, "sidebar-"+locale+".png"), sidebar)

		// The share menu, and the same header with the star and the text size
		// control in the states a reader meets.
		clickRSSHeaderAction(t, tt, rssShareWords(locale).action)
		saveRSSAirShot(t, tt, filepath.Join(dir, "share-"+locale+".png"), crop)
		tt.CloseMenu()
		tt.Frame()
	}

	// The action group in dark mode and at the minimum window, where the header
	// stacks the size control and the actions under the metadata.
	_, w := rssReaderTestApp(t)
	dark := ui.NewTester(w.view, 1280, 808)
	dark.Frame()
	openRSSReaderFullTextArticle(t, dark)
	dark.SetDark(true)
	dark.Advance(300 * time.Millisecond)
	dark.Frame()
	if header, ok := dark.AccessByRole(ui.RoleGroup, "Article header"); ok {
		saveRSSAirShot(t, dark, filepath.Join(dir, "toolbar-dark.png"),
			image.Rect(int(header.Bounds.X), int(header.Bounds.Y),
				int(header.Bounds.X+header.Bounds.W), int(header.Bounds.Y+header.Bounds.H)))
	} else {
		t.Fatal("the dark reader header has no accessible group")
	}

	narrowApp, narrowWindow := rssReaderTestApp(t)
	narrowApp.Settings.SidebarWidth = maxSidebarWidth
	narrowApp.Settings.RSSListWidth = maxRSSListWidth
	narrow := ui.NewTester(narrowWindow.view, 960, 808)
	narrow.Frame()
	openRSSReaderFullTextArticle(t, narrow)
	if header, ok := narrow.AccessByRole(ui.RoleGroup, "Article header"); ok {
		saveRSSAirShot(t, narrow, filepath.Join(dir, "toolbar-narrow.png"),
			image.Rect(int(header.Bounds.X), int(header.Bounds.Y),
				int(header.Bounds.X+header.Bounds.W), int(header.Bounds.Y+header.Bounds.H)))
	} else {
		t.Fatal("the narrow reader header has no accessible group")
	}
}

// rssAirShotWindow opens a full-text article in a fresh window at the locale.
func rssAirShotWindow(t *testing.T, locale string) *ui.Tester {
	t.Helper()
	_, _, tt := rssReaderTestWindow(t)
	tt.SetLocale(locale)
	tt.Frame()
	openRSSReaderFullTextArticle(t, tt)
	tt.Frame()
	return tt
}

// saveRSSAirShot writes the frame, or crop of it, as a PNG.
func saveRSSAirShot(t *testing.T, tt *ui.Tester, path string, crop image.Rectangle) {
	t.Helper()
	img := tt.Image()
	if img == nil {
		t.Fatalf("%s: the frame has no image", path)
	}
	src := image.Image(img)
	if !crop.Empty() {
		clipped := crop.Intersect(img.Bounds())
		if clipped.Empty() {
			t.Fatalf("%s: crop %v misses the frame %v", path, crop, img.Bounds())
		}
		src = img.SubImage(clipped)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	defer f.Close()
	if err := png.Encode(f, src); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	t.Logf("saved %s", path)
}
