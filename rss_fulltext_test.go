package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
)

// rssFullTextTestBody is the prose a stubbed extraction returns for an article
// the reader opened.
const rssFullTextTestBody = "The whole article arrived from its own page, far past the summary the feed carried."

// withAsyncRSSFullText puts the fetch runner back on its goroutine and hands
// each finished fetch to the test, which drains them one at a time: the test
// can watch a fetch that is still in flight and settle it on its own thread.
func withAsyncRSSFullText(t *testing.T) chan func() {
	t.Helper()
	deliveries := make(chan func(), 16)
	previousStart, previousDeliver := rssFullTextStart, rssFullTextDeliver
	rssFullTextStart = func(job func()) { go job() }
	rssFullTextDeliver = func(deliver func()) { deliveries <- deliver }
	t.Cleanup(func() {
		rssFullTextStart, rssFullTextDeliver = previousStart, previousDeliver
	})
	return deliveries
}

// rssFullTextStoreFetch is a stubbed article fetch that writes the article it
// returns, as the app's own fetch does, so a follow-up frame reads the stored
// text.
func rssFullTextStoreFetch(a *App) func(ctx context.Context, articleID string) (core.RSSArticle, error) {
	return func(ctx context.Context, articleID string) (core.RSSArticle, error) {
		art, err := a.Store.GetRSSArticle(ctx, articleID)
		if err != nil {
			return core.RSSArticle{}, err
		}
		if art == nil {
			return core.RSSArticle{}, errors.New("article not found")
		}
		art.IsFullText = true
		art.ContentHTML = "<p>" + rssFullTextTestBody + "</p>"
		art.Blocks = []core.ArticleBlock{{Type: core.BlockParagraph, Text: rssFullTextTestBody}}
		art.UpdatedAt = time.Now().UnixMilli()
		if err := a.Store.SaveRSSArticles(ctx, []core.RSSArticle{*art}); err != nil {
			return core.RSSArticle{}, err
		}
		return *art, nil
	}
}

// openRSSFullTextSummaryRow opens the summary-only article from the list, as a
// reader would, and returns its row.
func openRSSFullTextSummaryRow(t *testing.T, tt *ui.Tester) ui.Rect {
	t.Helper()
	row, ok := tt.Find("A Summary Only Post")
	if !ok {
		t.Fatalf("summary article row not found: %v", tt.Texts())
	}
	tt.ClickAt(row.X+row.W/2, row.Y+row.H/2)
	tt.Frame()
	return row
}

// TestRSSFullTextFetchesWhenAnArticleOpens proves the reading pane now does the
// work the Refetch control used to: opening a summary article fetches its full
// text once, shows it, and stops there for an article that already holds it.
func TestRSSFullTextFetchesWhenAnArticleOpens(t *testing.T) {
	a, w, tt := rssReaderTestWindow(t)

	var mu sync.Mutex
	var fetched []string
	storeFetch := rssFullTextStoreFetch(a)
	w.onRefetchRSSArticle = func(ctx context.Context, articleID string) (core.RSSArticle, error) {
		mu.Lock()
		fetched = append(fetched, articleID)
		mu.Unlock()
		return storeFetch(ctx, articleID)
	}

	row := openRSSFullTextSummaryRow(t, tt)
	mu.Lock()
	got := append([]string(nil), fetched...)
	mu.Unlock()
	if len(got) != 1 || got[0] != "art-summary" {
		t.Fatalf("opening the article fetched %v, want it once", got)
	}
	if a.SelectedRSSArticleID != "art-summary" {
		t.Fatalf("SelectedRSSArticleID = %q, want art-summary", a.SelectedRSSArticleID)
	}
	if !tt.HasText(rssFullTextTestBody) {
		t.Fatalf("the fetched full text is not on the page: %v", tt.Texts())
	}
	for _, gone := range []string{
		"Showing the feed's summary. Open the original article in your browser for the full text.",
		"Couldn't fetch the full text. Showing the feed's summary.",
		"Fetching the full text…",
		"Retry Full Text",
	} {
		if tt.HasText(gone) {
			t.Errorf("a fetched article still shows %q: %v", gone, tt.Texts())
		}
	}
	if got := rssHeaderActionNames(t, tt); !slices.Contains(got, "Refetch") {
		t.Fatalf("the capture action is missing after the fetch: %q", got)
	}

	// Clicking the article that now holds its text is served from the store.
	tt.ClickAt(row.X+row.W/2, row.Y+row.H/2)
	tt.Frame()
	mu.Lock()
	got = append([]string(nil), fetched...)
	mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("reopening an article with its full text fetched %v, want no further request", got)
	}
}

// TestRSSFullTextPlaceholderReplacesTheBrowserNotice covers what the reader
// shows mid-fetch: the summary stays readable under one quiet line, and the
// browser's call to action is nowhere while the text is on its way.
func TestRSSFullTextPlaceholderReplacesTheBrowserNotice(t *testing.T) {
	_, w, tt := rssReaderTestWindow(t)
	deliveries := withAsyncRSSFullText(t)

	release := make(chan struct{})
	started := make(chan string, 1)
	w.onRefetchRSSArticle = func(ctx context.Context, articleID string) (core.RSSArticle, error) {
		started <- articleID
		<-release
		return core.RSSArticle{ID: articleID, IsFullText: true}, nil
	}

	openRSSFullTextSummaryRow(t, tt)
	select {
	case id := <-started:
		if id != "art-summary" {
			t.Fatalf("fetching %q, want art-summary", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("opening the article did not start a fetch")
	}

	tt.Frame()
	if !tt.HasText("Fetching the full text…") {
		t.Fatalf("the fetching placeholder is missing: %v", tt.Texts())
	}
	if got := rssHeaderActionNames(t, tt); !slices.Contains(got, "Fetching…") {
		t.Fatalf("the capture action does not show progress: %q", got)
	}
	if !tt.HasText("The feed carried only this sentence") {
		t.Fatalf("the summary left the page while its full text is fetched: %v", tt.Texts())
	}
	for _, gone := range []string{
		"Showing the feed's summary. Open the original article in your browser for the full text.",
		"当前展示摘要，可点击在浏览器中查看原网页",
		"Retry Full Text",
	} {
		if tt.HasText(gone) {
			t.Errorf("a fetch in progress still shows %q: %v", gone, tt.Texts())
		}
	}

	close(release)
	select {
	case deliver := <-deliveries:
		deliver()
	case <-time.After(2 * time.Second):
		t.Fatal("the finished fetch never reached the window")
	}
	tt.Frame()
	if tt.HasText("Fetching the full text…") || tt.HasText("Fetching…") {
		t.Fatalf("the fetch's progress outlived the fetch: %v", tt.Texts())
	}
}

// TestRSSFullTextFailureKeepsTheSummaryAndOffersRetry pins the failure state:
// the summary stays, one compact line says the fetch gave up, the capture
// action becomes the retry, and opening the article again tries once more.
// The retry runs through the same fetch.
func TestRSSFullTextFailureKeepsTheSummaryAndOffersRetry(t *testing.T) {
	a, w, tt := rssReaderTestWindow(t)

	var mu sync.Mutex
	calls := 0
	storeFetch := rssFullTextStoreFetch(a)
	w.onRefetchRSSArticle = func(ctx context.Context, articleID string) (core.RSSArticle, error) {
		mu.Lock()
		calls++
		attempt := calls
		mu.Unlock()
		if attempt <= 2 {
			return core.RSSArticle{}, errors.New("the site refused the page")
		}
		return storeFetch(ctx, articleID)
	}

	row := openRSSFullTextSummaryRow(t, tt)
	callsNow := func() int {
		mu.Lock()
		defer mu.Unlock()
		return calls
	}
	if got := callsNow(); got != 1 {
		t.Fatalf("opening the article fetched %d times, want once", got)
	}
	if !tt.HasText("Couldn't fetch the full text. Showing the feed's summary.") {
		t.Fatalf("the failure state is missing: %v", tt.Texts())
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "Retry Full Text"); !ok {
		t.Fatalf("the body toggle does not name the retry: %v", rssHeaderActionNames(t, tt))
	}
	if !tt.HasText("The feed carried only this sentence") {
		t.Fatalf("the summary left the page when the fetch failed: %v", tt.Texts())
	}
	if tt.HasText("Showing the feed's summary. Open the original article in your browser for the full text.") {
		t.Fatalf("a failed fetch still offers the browser's call to action: %v", tt.Texts())
	}

	// Opening the article again is a fresh attempt, so it asks the site once
	// more and its failure keeps the retry on screen.
	tt.ClickAt(row.X+row.W/2, row.Y+row.H/2)
	tt.Frame()
	if got := callsNow(); got != 2 {
		t.Fatalf("reopening a failed article fetched %d times, want one more attempt", got)
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "Retry Full Text"); !ok {
		t.Fatalf("reopening a failed article lost its retry: %v", rssHeaderActionNames(t, tt))
	}

	clickRSSHeaderAction(t, tt, "Retry Full Text")
	if got := callsNow(); got != 3 {
		t.Fatalf("the retry fetched %d times, want the same path once more", got)
	}
	if !tt.HasText(rssFullTextTestBody) {
		t.Fatalf("the retry's full text is not on the page: %v", tt.Texts())
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "Retry Full Text"); ok {
		t.Fatalf("the failure state outlived the retry that cured it: %v", rssHeaderActionNames(t, tt))
	}
	if tt.HasText("Couldn't fetch the full text. Showing the feed's summary.") {
		t.Fatalf("the failure line outlived the retry that cured it: %v", tt.Texts())
	}
	if got := rssHeaderActionNames(t, tt); !slices.Contains(got, "Refetch") {
		t.Fatalf("the capture action did not return to its ordinary label: %q", got)
	}
}

// TestRSSFullTextExtractsFromTheArticlePage is the end-to-end path with no
// third party involved: the app's own extraction reads the article's page from
// a local server, stores the text, and the reader shows it.
func TestRSSFullTextExtractsFromTheArticlePage(t *testing.T) {
	page := `<!doctype html><html><head><title>A Summary Only Post</title></head><body><article>
		<h1>A Summary Only Post</h1>
		<p>Grace Hopper once said that the most dangerous phrase in the language is that we have always done it this way, and the reading room keeps that sentence on the wall.</p>
		<p>The second paragraph carries enough prose for the reader's extraction to treat this page as the article rather than as a teaser the feed already supplied.</p>
	</article></body></html>`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	}))
	defer ts.Close()

	a, _, tt := rssReaderTestWindow(t)
	// The local page is this test's only network source, and it replaces the
	// offline client the test app carries.
	a.rssHTTPClient = &http.Client{Timeout: 5 * time.Second}
	ctx := context.Background()
	art, err := a.Store.GetRSSArticle(ctx, "art-summary")
	if err != nil || art == nil {
		t.Fatalf("read the summary article: %v", err)
	}
	art.Link = ts.URL + "/summary-only"
	if err := a.Store.SaveRSSArticles(ctx, []core.RSSArticle{*art}); err != nil {
		t.Fatalf("point the article at the local page: %v", err)
	}
	if err := a.ReloadRSS(); err != nil {
		t.Fatalf("reload articles: %v", err)
	}

	openRSSFullTextSummaryRow(t, tt)

	stored, err := a.Store.GetRSSArticle(ctx, "art-summary")
	if err != nil || stored == nil {
		t.Fatalf("read the fetched article: %v", err)
	}
	if !stored.IsFullText {
		t.Fatalf("the extracted article is not marked full text: %+v", stored)
	}
	if !strings.Contains(stored.ContentHTML, "the most dangerous phrase in the language") {
		t.Fatalf("the stored body is not the page's text: %q", stored.ContentHTML)
	}
	if !tt.HasText("the most dangerous phrase in the language") {
		t.Fatalf("the reader does not show the page's text: %v", tt.Texts())
	}
	// The article column keeps summaries: the fetch pages the one article it
	// stores, never every body into memory.
	for _, listed := range a.RSSArticles {
		if listed.ContentHTML != "" || len(listed.Blocks) != 0 {
			t.Fatalf("the article column holds %q's body after the fetch", listed.ID)
		}
	}
}

// TestRSSFullTextRequestDedupesAndCapsThePool pins the queue's own contract: a
// sweep of selections opens at most the pool's worth of requests, each article
// once, and the rest wait their turn.
func TestRSSFullTextRequestDedupesAndCapsThePool(t *testing.T) {
	withAsyncRSSFullText(t)

	var mu sync.Mutex
	running, peak := 0, 0
	calls := map[string]int{}
	release := make(chan struct{})
	started := make(chan string, 32)
	fetcher := &rssFullTextFetcher{fetch: func(ctx context.Context, articleID string) (core.RSSArticle, error) {
		mu.Lock()
		running++
		calls[articleID]++
		peak = max(peak, running)
		mu.Unlock()
		started <- articleID
		<-release
		mu.Lock()
		running--
		mu.Unlock()
		return core.RSSArticle{ID: articleID, IsFullText: true}, nil
	}}

	ids := []string{"a", "b", "c", "d", "e", "f"}
	for _, id := range ids {
		if phase := fetcher.request(id, false); phase != rssFullTextFetching {
			t.Fatalf("requesting %q reported %v, want fetching", id, phase)
		}
	}
	for range rssFullTextWorkers {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d fetches started, want the pool's %d", rssFullTextWorkers, rssFullTextWorkers)
		}
	}
	select {
	case id := <-started:
		t.Fatalf("a fourth fetch (%s) started past the pool's %d workers", id, rssFullTextWorkers)
	case <-time.After(50 * time.Millisecond):
	}
	// A second request for an article already in flight joins it.
	if phase := fetcher.request("a", false); phase != rssFullTextFetching {
		t.Fatalf("a second request for an in-flight article reported %v, want fetching", phase)
	}

	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		done := 0
		for _, id := range ids {
			if calls[id] > 0 && fetcher.phase(id) == rssFullTextSettled {
				done++
			}
		}
		mu.Unlock()
		if done == len(ids) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d of %d queued fetches finished", done, len(ids))
		}
		time.Sleep(2 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if peak > rssFullTextWorkers {
		t.Fatalf("%d fetches ran at once, want at most %d", peak, rssFullTextWorkers)
	}
	for id, n := range calls {
		if n != 1 {
			t.Fatalf("article %q was fetched %d times, want once", id, n)
		}
	}
}

// TestRSSFullTextDropsQueuedSelectionsTheReaderMovedPast pins the
// deprioritisation: when the selection moves on, the queued fetches for
// articles nobody is looking at leave the queue, so a sweep cannot pile up.
func TestRSSFullTextDropsQueuedSelectionsTheReaderMovedPast(t *testing.T) {
	withAsyncRSSFullText(t)

	var mu sync.Mutex
	calls := map[string]int{}
	release := make(chan struct{})
	started := make(chan string, 32)
	fetcher := &rssFullTextFetcher{fetch: func(ctx context.Context, articleID string) (core.RSSArticle, error) {
		mu.Lock()
		calls[articleID]++
		mu.Unlock()
		started <- articleID
		<-release
		return core.RSSArticle{ID: articleID, IsFullText: true}, nil
	}}

	for _, id := range []string{"a", "b", "c"} {
		fetcher.request(id, false)
	}
	for range rssFullTextWorkers {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("the pool did not fill with the first three articles")
		}
	}
	fetcher.request("abandoned", false)
	fetcher.request("open", false)
	fetcher.keepOnly("open")
	close(release)

	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		openDone := calls["open"] > 0
		mu.Unlock()
		if openDone {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the article the reader stayed on never fetched")
		}
		time.Sleep(2 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls["abandoned"] != 0 {
		t.Fatalf("the abandoned article was fetched %d times, want none", calls["abandoned"])
	}
	for _, id := range []string{"a", "b", "c"} {
		if calls[id] != 1 {
			t.Fatalf("the fetch already in flight for %q ran %d times, want once", id, calls[id])
		}
	}
}

// TestRSSFeedFullTextToggleStopsAutomaticFetchAndPersists covers the feed's
// setting from its menu: choosing it turns opening-time fetches off for that
// feed, keeps the choice in the store for the next launch, and shows the
// browser notice the automatic fetch would otherwise replace.
func TestRSSFeedFullTextToggleStopsAutomaticFetchAndPersists(t *testing.T) {
	a, w, tt := rssReaderTestWindow(t)

	var mu sync.Mutex
	fetches := 0
	w.onRefetchRSSArticle = func(ctx context.Context, articleID string) (core.RSSArticle, error) {
		mu.Lock()
		fetches++
		mu.Unlock()
		return core.RSSArticle{}, errors.New("the site refused the page")
	}

	if err := tt.RightClick("The Long Read"); err != nil {
		t.Fatalf("open the feed's context menu: %v", err)
	}
	if items := tt.Menu(); !slices.Contains(items, "Fetch Full Text by Default") {
		t.Fatalf("the feed's menu %q has no full-text setting", items)
	}
	if err := tt.ChooseMenuItem("Fetch Full Text by Default"); err != nil {
		t.Fatalf("turn the feed's full-text fetch off: %v", err)
	}
	tt.Frame()

	ctx := context.Background()
	stored, err := a.Store.GetRSSFeed(ctx, "feed-reader")
	if err != nil || stored == nil {
		t.Fatalf("read the feed back: %v", err)
	}
	if stored.AutoFetchFullText() {
		t.Fatal("the feed's full-text setting did not reach the store")
	}
	// A relaunch reads the same answer out of the same database.
	reopened, err := core.OpenStore(ctx, filepath.Join(a.Dir, "videos.sqlite"))
	if err != nil {
		t.Fatalf("reopen the store: %v", err)
	}
	defer reopened.Close()
	again, err := reopened.GetRSSFeed(ctx, "feed-reader")
	if err != nil || again == nil {
		t.Fatalf("read the feed from the reopened store: %v", err)
	}
	if again.AutoFetchFullText() {
		t.Fatal("the feed's setting did not survive a relaunch")
	}

	// Opening one of the feed's articles now leaves it on the feed's summary,
	// with the browser notice the reader has always had.
	row := openRSSFullTextSummaryRow(t, tt)
	mu.Lock()
	gotFetches := fetches
	mu.Unlock()
	if gotFetches != 0 {
		t.Fatalf("a feed with the setting off fetched %d times, want none", gotFetches)
	}
	if !tt.HasText("Showing the feed's summary. Open the original article in your browser for the full text.") {
		t.Fatalf("a feed with the setting off lost the browser notice: %v", tt.Texts())
	}
	if tt.HasText("Fetching the full text…") {
		t.Fatalf("a feed with the setting off showed a fetch state: %v", tt.Texts())
	}
	// The capture action stays available for a single article on request.
	if got := rssHeaderActionNames(t, tt); !slices.Contains(got, "Fetch Full Text") || !slices.Contains(got, "Refetch") {
		t.Fatalf("a feed with the setting off lost its full-text actions: %q", got)
	}

	// Turning the setting back on leaves the open article alone: the switch
	// governs the articles opened after it, and this one keeps its summary.
	if err := tt.RightClick("The Long Read"); err != nil {
		t.Fatalf("open the feed's context menu again: %v", err)
	}
	if err := tt.ChooseMenuItem("Fetch Full Text by Default"); err != nil {
		t.Fatalf("turn the feed's full-text fetch back on: %v", err)
	}
	tt.Frame()
	mu.Lock()
	gotFetches = fetches
	mu.Unlock()
	if gotFetches != 0 {
		t.Fatalf("turning the setting back on fetched %d times, want the open article left alone", gotFetches)
	}
	if !tt.HasText("Showing the feed's summary. Open the original article in your browser for the full text.") {
		t.Fatalf("the open article lost its summary when the setting came back on: %v", tt.Texts())
	}
	if got := rssHeaderActionNames(t, tt); !slices.Contains(got, "Refetch") {
		t.Fatalf("the open article lost its capture action: %q", got)
	}

	// The articles opened from now on follow the setting again.
	tt.ClickAt(row.X+row.W/2, row.Y+row.H/2)
	tt.Frame()
	mu.Lock()
	gotFetches = fetches
	mu.Unlock()
	if gotFetches != 1 {
		t.Fatalf("opening an article after the setting came back on fetched %d times, want once", gotFetches)
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "Retry Full Text"); !ok {
		t.Fatalf("the failed fetch did not leave its retry: %v", rssHeaderActionNames(t, tt))
	}
}

// TestRSSFullTextStatesReadInBothLanguages pins the new copy in Chinese: the
// progress line, the failure line, and the retry label each replace the
// browser's call to action for a fetch that is no longer a settled summary.
func TestRSSFullTextStatesReadInBothLanguages(t *testing.T) {
	_, w, tt := rssReaderTestWindow(t)
	tt.SetLocale("zh")
	deliveries := withAsyncRSSFullText(t)

	release := make(chan struct{})
	w.onRefetchRSSArticle = func(ctx context.Context, articleID string) (core.RSSArticle, error) {
		<-release
		return core.RSSArticle{}, errors.New("the site refused the page")
	}

	openRSSFullTextSummaryRow(t, tt)
	tt.Frame()
	if !tt.HasText("正在抓取全文…") {
		t.Fatalf("the Chinese reader is missing the fetching line: %v", tt.Texts())
	}
	if got, want := rssHeaderActionNames(t, tt), []string{
		"标记为未读 (M)", "标星 (S)", "抓取中…", "分享", "在浏览器中打开", "重新抓取",
	}; !slices.Equal(got, want) {
		t.Fatalf("Chinese header actions mid-fetch = %q, want %q", got, want)
	}
	if got := rssHeaderAction(t, tt, "重新抓取").Description; got != "抓取中…" {
		t.Fatalf("the Chinese refresh tooltip = %q, want the fetching state", got)
	}
	if tt.HasText("当前展示摘要，可点击在浏览器中查看原网页") {
		t.Fatalf("the Chinese reader still sends the reader to the browser mid-fetch: %v", tt.Texts())
	}

	close(release)
	select {
	case deliver := <-deliveries:
		deliver()
	case <-time.After(2 * time.Second):
		t.Fatal("the finished fetch never reached the window")
	}
	tt.Frame()
	if !tt.HasText("全文抓取失败，当前显示摘要。") {
		t.Fatalf("the Chinese reader is missing the failure line: %v", tt.Texts())
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "重试抓取"); !ok {
		t.Fatalf("the Chinese reader is missing the retry: %v", rssHeaderActionNames(t, tt))
	}
	if tt.HasText("当前展示摘要，可点击在浏览器中查看原网页") {
		t.Fatalf("a failed fetch still offers the browser's call to action: %v", tt.Texts())
	}
}

// TestRSSFullTextFollowsTheKeyboardSelection proves the fetch is part of
// opening an article however it is opened: a J step fetches the article it
// lands on, exactly as a click does.
func TestRSSFullTextFollowsTheKeyboardSelection(t *testing.T) {
	a, w, tt := rssShortcutFixture(t, 3)

	var mu sync.Mutex
	var fetched []string
	storeFetch := rssFullTextStoreFetch(a)
	w.onRefetchRSSArticle = func(ctx context.Context, articleID string) (core.RSSArticle, error) {
		mu.Lock()
		fetched = append(fetched, articleID)
		mu.Unlock()
		return storeFetch(ctx, articleID)
	}

	tt.Key(0, ui.KeyJ)
	tt.Frame()
	tt.Key(0, ui.KeyJ)
	tt.Frame()

	mu.Lock()
	got := append([]string(nil), fetched...)
	mu.Unlock()
	want := []string{"art-00", "art-01"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("the keyboard walk fetched %v, want %v", got, want)
	}
	if !tt.HasText(rssFullTextTestBody) {
		t.Fatalf("the article the keyboard opened does not show its full text: %v", tt.Texts())
	}
}
