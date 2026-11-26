package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
)

const (
	// rssFullTextWorkers is how many article pages the reader fetches at once.
	// A sweep through the article list queues behind a few requests instead of
	// opening one per article as the selection moves.
	rssFullTextWorkers = 3
	// rssFullTextTimeout bounds one article's fetch, the same budget the
	// reader's manual re-fetch has always given the page.
	rssFullTextTimeout = 45 * time.Second
)

// rssFullTextStart starts one fetch job off the frame. rssFullTextDeliver
// hands a finished fetch to the thread that owns the window. Tests put both
// back on the calling goroutine, whose fake backend never drains mygo's main
// loop, so a fetch a test starts has settled by the time the selection that
// started it returns instead of racing the frame the test asserts on. The app
// never replaces either.
var (
	rssFullTextStart   = func(job func()) { go job() }
	rssFullTextDeliver = mygo.RunOnMain
)

// rssFullTextPhase is what the reader shows of an article's full-text fetch.
type rssFullTextPhase int

const (
	// rssFullTextSettled means no fetch is running: the article holds its
	// full text, or the reader shows the feed's own text as it always has.
	rssFullTextSettled rssFullTextPhase = iota
	// rssFullTextFetching means the article's fetch is queued or in flight.
	rssFullTextFetching
	// rssFullTextFailed means the last attempt gave up: the reader keeps the
	// summary and offers the retry.
	rssFullTextFailed
)

// rssFullTextStatus is the phase the reader shows for one article's fetch.
type rssFullTextStatus struct {
	articleID string
	phase     rssFullTextPhase
}

// phaseOf reports the phase stored for articleID. Another article's status is
// not this one's business, so it reads as settled.
func (s rssFullTextStatus) phaseOf(articleID string) rssFullTextPhase {
	if s.articleID != articleID || articleID == "" {
		return rssFullTextSettled
	}
	return s.phase
}

// rssFullTextJob is one article waiting for its full text, manual when the
// reader asked for it through the retry rather than by opening the article.
type rssFullTextJob struct {
	articleID string
	manual    bool
}

// rssFullTextFetcher fetches article full text away from the frame. One small
// pool of workers drains a queue of article IDs, the same article is never in
// flight twice, and an article whose fetch gave up keeps that state until the
// article is requested again.
type rssFullTextFetcher struct {
	fetch func(ctx context.Context, articleID string) (core.RSSArticle, error)
	// onResult runs for every finished fetch, in the worker. The window moves
	// it onto the main thread before it touches anything a frame reads.
	onResult func(articleID string, updated core.RSSArticle, err error, manual bool)

	mu      sync.Mutex
	queue   []rssFullTextJob
	pending map[string]bool // queued or in flight
	failed  map[string]bool // attempts that gave up, until the next request
	running int
}

// request asks for one article's full text and reports the phase the reader
// should show for it: fetching when the request queued or an earlier one is
// still in flight, and settled when there is nothing to fetch. Every request
// is one attempt of its own, so it clears the record of an earlier attempt that
// gave up; an article already queued or in flight is never asked for twice.
func (f *rssFullTextFetcher) request(articleID string, manual bool) rssFullTextPhase {
	if articleID == "" {
		return rssFullTextSettled
	}
	f.mu.Lock()
	if f.pending[articleID] {
		f.mu.Unlock()
		return rssFullTextFetching
	}
	if f.pending == nil {
		f.pending = map[string]bool{}
	}
	if f.failed == nil {
		f.failed = map[string]bool{}
	}
	delete(f.failed, articleID)
	f.pending[articleID] = true
	f.queue = append(f.queue, rssFullTextJob{articleID: articleID, manual: manual})
	f.mu.Unlock()
	f.pump()
	// An inline runner has finished the fetch here already, so the phase read
	// after the queue moved is the one the reader shows.
	return f.phase(articleID)
}

// keepOnly drops queued fetches for the articles the reader has moved past, so
// sweeping through the list leaves at most the open article waiting. A fetch
// already in flight runs to its end: its result still belongs in the store,
// and the pool bounds how many can be under way at once.
func (f *rssFullTextFetcher) keepOnly(articleID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	kept := f.queue[:0]
	for _, job := range f.queue {
		if job.articleID != articleID {
			delete(f.pending, job.articleID)
			continue
		}
		kept = append(kept, job)
	}
	for i := len(kept); i < len(f.queue); i++ {
		f.queue[i] = rssFullTextJob{}
	}
	f.queue = kept
}

// pump starts as many queued fetches as the pool has workers for.
func (f *rssFullTextFetcher) pump() {
	f.mu.Lock()
	free := rssFullTextWorkers - f.running
	if free > len(f.queue) {
		free = len(f.queue)
	}
	if free <= 0 {
		f.mu.Unlock()
		return
	}
	jobs := append([]rssFullTextJob(nil), f.queue[:free]...)
	f.queue = f.queue[free:]
	f.running += len(jobs)
	f.mu.Unlock()
	for _, job := range jobs {
		job := job
		rssFullTextStart(func() { f.run(job) })
	}
}

// run fetches one queued article and keeps its outcome for the next request.
func (f *rssFullTextFetcher) run(job rssFullTextJob) {
	ctx, cancel := context.WithTimeout(context.Background(), rssFullTextTimeout)
	defer cancel()
	updated, err := f.fetch(ctx, job.articleID)

	f.mu.Lock()
	delete(f.pending, job.articleID)
	// An extraction that degrades to the feed's own text is no better than
	// the failure the reader is looking at, so it is remembered the same way.
	if err == nil && updated.IsFullText {
		delete(f.failed, job.articleID)
	} else {
		f.failed[job.articleID] = true
	}
	f.running--
	f.mu.Unlock()

	if f.onResult != nil {
		f.onResult(job.articleID, updated, err, job.manual)
	}
	f.pump()
}

// phase reports what the reader shows for articleID now.
func (f *rssFullTextFetcher) phase(articleID string) rssFullTextPhase {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case f.pending[articleID]:
		return rssFullTextFetching
	case f.failed[articleID]:
		return rssFullTextFailed
	}
	return rssFullTextSettled
}

// rssFullTextPhase reports the phase of the open article's fetch. It belongs to
// the reader's frame, so the header and the body never disagree.
func (w *Window) rssFullTextPhase(articleID string) rssFullTextPhase {
	return w.rssFullText.phaseOf(articleID)
}

// rssFullTextFetcherFor returns the window's fetcher, building it on first use.
// Its fetch reads the window's replacement hook each time, so a test that
// installs one late still takes effect.
func (w *Window) rssFullTextFetcherFor() *rssFullTextFetcher {
	if w.rssFullTextFetcher != nil {
		return w.rssFullTextFetcher
	}
	w.rssFullTextFetcher = &rssFullTextFetcher{
		fetch: func(ctx context.Context, articleID string) (core.RSSArticle, error) {
			return w.fetchRSSFullText(ctx, articleID)
		},
		onResult: func(articleID string, updated core.RSSArticle, err error, manual bool) {
			rssFullTextDeliver(func() { w.applyRSSFullText(articleID, updated, err, manual) })
		},
	}
	return w.rssFullTextFetcher
}

// fetchRSSFullText runs one article's full-text fetch: the app's background
// extraction, or the replacement a test installed instead of it.
func (w *Window) fetchRSSFullText(ctx context.Context, articleID string) (core.RSSArticle, error) {
	if w.onRefetchRSSArticle != nil {
		return w.onRefetchRSSArticle(ctx, articleID)
	}
	if w.app == nil {
		return core.RSSArticle{}, errNoFullTextFetch
	}
	return w.app.RefetchRSSArticleContent(ctx, articleID)
}

// openRSSFullText starts the fetch that opening art asks for: the article's
// whole text when the feed carries only its summary, and nothing when the
// article already holds its text or its feed has the behaviour turned off.
func (w *Window) openRSSFullText(art core.RSSArticle) {
	if w.app == nil || strings.TrimSpace(art.Link) == "" || art.IsFullText {
		return
	}
	if !w.app.RSSFeedByID(art.FeedID).AutoFetchFullText() {
		return
	}
	w.requestRSSFullText(nil, art, false)
}

// requestRSSFullText puts art in the window's fetch queue and shows the phase
// the queue reports, so a queued fetch reads as one in progress and a fresh
// attempt clears the failure the header showed for an earlier one.
func (w *Window) requestRSSFullText(c *ui.Context, art core.RSSArticle, manual bool) {
	if w.app == nil || art.ID == "" || strings.TrimSpace(art.Link) == "" {
		return
	}
	fetcher := w.rssFullTextFetcherFor()
	if !manual {
		fetcher.keepOnly(art.ID)
	}
	phase := fetcher.request(art.ID, manual)
	if w.rssFullText.phaseOf(art.ID) != phase {
		w.rssFullText = rssFullTextStatus{articleID: art.ID, phase: phase}
		c.Invalidate()
		w.invalidateMainWindow()
	}
}

// applyRSSFullText takes one finished fetch on the main thread: the reader's
// state follows it, the library reloads, and a retry the reader asked for
// reports its outcome. An automatic fetch stays quiet, because the article it
// settles is often no longer the one on screen.
func (w *Window) applyRSSFullText(articleID string, updated core.RSSArticle, err error, manual bool) {
	phase := rssFullTextSettled
	if err != nil || !updated.IsFullText {
		phase = rssFullTextFailed
	}
	if w.rssFullText.articleID == articleID {
		w.rssFullText.phase = phase
	}
	if w.app == nil {
		return
	}
	if reloadErr := w.app.ReloadRSS(); reloadErr != nil {
		w.toast(nil, "Couldn't reload articles", true)
		w.invalidateMainWindow()
		return
	}
	if manual {
		switch {
		case err != nil:
			w.toast(nil, "Couldn't fetch the full article", true)
		case updated.IsFullText:
			w.toast(nil, "Full text updated", false)
		default:
			w.toast(nil, "Still showing the feed's summary", false)
		}
	}
	w.invalidateMainWindow()
}

// refetchRSSArticle asks again for the open article's full text, through the
// same queue an automatic fetch uses.
func (w *Window) refetchRSSArticle(c *ui.Context, art core.RSSArticle) {
	w.requestRSSFullText(c, art, true)
}

// errNoFullTextFetch is what a window with neither an app nor a replacement
// hook reports instead of fetching.
var errNoFullTextFetch = errors.New("no article fetch available")
