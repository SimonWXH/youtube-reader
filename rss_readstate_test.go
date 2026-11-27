package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
)

// rssAppTransactions puts the app's store on SQLite's rollback journal and
// returns its header's change counter. That journal moves the counter by one
// per committed transaction, so a reader action can be held to a single write
// pass instead of trusting the code to say so.
func rssAppTransactions(t *testing.T, a *App) func() uint32 {
	t.Helper()
	ctx := context.Background()
	if _, err := a.Store.DB().Query(ctx, `PRAGMA journal_mode = delete`); err != nil {
		t.Fatalf("switch the app's store to the rollback journal: %v", err)
	}
	path := filepath.Join(a.Dir, "videos.sqlite")
	return func() uint32 {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read the database: %v", err)
		}
		if len(data) < 28 {
			t.Fatalf("the database is %d bytes, too short for a header", len(data))
		}
		return binary.BigEndian.Uint32(data[24:28])
	}
}

// rssReadStateFixture seeds one folder holding two feeds, and one feed outside
// it, with their own unread articles, and returns a window on the RSS workspace
// showing the All Unread destination.
func rssReadStateFixture(t *testing.T) (*App, *Window, *ui.Tester) {
	t.Helper()
	stubNetImages(t)
	a := newTestApp(t, t.TempDir())
	t.Cleanup(func() { a.Close() })
	ctx := context.Background()
	now := time.Now().UnixMilli()

	group := core.RSSGroup{ID: "group-shelf", Name: "Shelf", FeedIDs: []string{"feed-a", "feed-b"}, UpdatedAt: now}
	if err := a.Store.SaveRSSGroup(ctx, group); err != nil {
		t.Fatalf("save the folder: %v", err)
	}
	feeds := []core.RSSFeed{
		{ID: "feed-a", FeedURL: "https://feeds.example.com/a.xml", SiteURL: "https://a.example.com", Title: "Feed A", GroupIDs: []string{"group-shelf"}, UpdatedAt: now},
		{ID: "feed-b", FeedURL: "https://feeds.example.com/b.xml", SiteURL: "https://b.example.com", Title: "Feed B", GroupIDs: []string{"group-shelf"}, UpdatedAt: now},
		{ID: "feed-c", FeedURL: "https://feeds.example.com/c.xml", SiteURL: "https://c.example.com", Title: "Feed C", UpdatedAt: now},
	}
	for _, f := range feeds {
		if err := a.Store.SaveRSSFeed(ctx, f); err != nil {
			t.Fatalf("save feed %q: %v", f.ID, err)
		}
	}
	articles := make([]core.RSSArticle, 0, 8)
	add := func(feedID string, n int) {
		for i := 0; i < n; i++ {
			articles = append(articles, core.RSSArticle{
				ID:          fmt.Sprintf("art-%s-%d", feedID, i),
				FeedID:      feedID,
				Title:       fmt.Sprintf("%s story %d", feedID, i),
				Link:        fmt.Sprintf("https://feeds.example.com/%s/%d", feedID, i),
				PublishedAt: time.Now().Add(-time.Duration(len(articles)+1) * time.Hour).Format(time.RFC3339),
				Snippet:     "A snippet long enough to fill both summary lines of the row.",
				UpdatedAt:   now,
			})
		}
	}
	add("feed-a", 3)
	add("feed-b", 2)
	add("feed-c", 2)
	if err := a.Store.SaveRSSArticles(ctx, articles); err != nil {
		t.Fatalf("save articles: %v", err)
	}
	if err := a.ReloadRSS(); err != nil {
		t.Fatalf("reload rss: %v", err)
	}
	a.Selected = "all"

	w := &Window{app: a, selectedIndex: new(int)}
	w.selectWorkspace(WorkspaceRSS)
	tt := ui.NewTester(w.shell, 1280, 808)
	tt.Frame()
	return a, w, tt
}

// rssStoredCounts returns how many of a feed's articles are read and unread in
// the store, rather than in the app's memory.
func rssStoredCounts(t *testing.T, a *App, feedID string) (read, unread int) {
	t.Helper()
	ctx := context.Background()
	articles, err := a.Store.ListRSSArticleSummaries(ctx, core.RSSArticleFilter{FeedID: feedID})
	if err != nil {
		t.Fatalf("list %q's articles: %v", feedID, err)
	}
	for _, art := range articles {
		if art.Read {
			read++
		} else {
			unread++
		}
	}
	return read, unread
}

// TestRSSOpeningAnArticleMarksItReadOnceAndKeepsItRead covers the per-article
// rule: opening marks the article read and records that in the same pass,
// re-opening it writes nothing, and no later view of the article puts it back.
func TestRSSOpeningAnArticleMarksItReadOnceAndKeepsItRead(t *testing.T) {
	a, w, tt := rssReaderTestWindow(t)
	transactions := rssAppTransactions(t, a)
	ctx := context.Background()

	row, ok := tt.Find("The Quiet Return of the Open Feed")
	if !ok {
		t.Fatalf("article row not found: %v", tt.Texts())
	}
	opening := transactions()
	tt.ClickAt(row.X+row.W/2, row.Y+row.H/2)
	tt.Frame()

	art := a.SelectedRSSArticle()
	if art == nil || !art.Read || art.ReadAt == 0 {
		t.Fatalf("opening the article left it unread: %+v", art)
	}
	stored, err := a.Store.GetRSSArticle(ctx, "art-reader")
	if err != nil || stored == nil || !stored.Read {
		t.Fatalf("the store did not take the read state: %v %+v", err, stored)
	}
	// The same pass recorded this device's read state for the article, so a
	// later sync carries it instead of bringing the article back as unread.
	records, err := a.Store.RSSReadStates(ctx)
	if err != nil {
		t.Fatalf("read the read vector: %v", err)
	}
	if rec, ok := records["art-reader"][a.DeviceID]; !ok || !rec.Read {
		t.Fatalf("the open article recorded %+v, want read on %q", rec, a.DeviceID)
	}
	if got := transactions() - opening; got != 1 {
		t.Fatalf("opening one article committed %d transactions, want 1", got)
	}

	// Opening it again is not a fresh write, and it is certainly not a way back
	// to unread.
	reopening := transactions()
	tt.ClickAt(row.X+row.W/2, row.Y+row.H/2)
	tt.Frame()
	if art := a.SelectedRSSArticle(); art == nil || !art.Read {
		t.Fatalf("re-opening the article put it back to unread: %+v", art)
	}
	if got := transactions() - reopening; got != 0 {
		t.Fatalf("re-opening a read article committed %d transactions, want none", got)
	}

	// Leaving the workspace and coming back, and reloading the library from
	// SQLite, both keep it read.
	w.selectWorkspace(WorkspaceVideos)
	tt.Frame()
	w.selectWorkspace(WorkspaceRSS)
	tt.Frame()
	if art := a.SelectedRSSArticle(); art == nil || !art.Read {
		t.Fatalf("the article came back unread after a workspace trip: %+v", art)
	}
	if err := a.ReloadRSS(); err != nil {
		t.Fatalf("reload rss: %v", err)
	}
	tt.Frame()
	if art := a.SelectedRSSArticle(); art == nil || !art.Read {
		t.Fatalf("the reloaded article came back unread: %+v", art)
	}
}

// TestRSSRestoredSelectionMarksNothingRead covers the distinction the selection
// path has to record: pointing the reader at an article nobody chose marks
// nothing, while the reader's own click marks it read.
func TestRSSRestoredSelectionMarksNothingRead(t *testing.T) {
	a, _, tt := rssReaderTestWindow(t)
	transactions := rssAppTransactions(t, a)

	before := transactions()
	a.SelectRSSArticle("art-reader")
	tt.Frame()

	if art := a.SelectedRSSArticle(); art == nil || art.Read {
		t.Fatalf("a restored selection marked the article read: %+v", art)
	}
	if got := transactions() - before; got != 0 {
		t.Fatalf("a restored selection committed %d transactions, want none", got)
	}
	// The toolbar and the list agree with the state it kept: the row still
	// carries its unread dot and the control offers the read.
	if _, ok := tt.Find("Unread: The Quiet Return of the Open Feed"); !ok {
		t.Fatalf("the restored article lost its unread dot: %v", tt.Texts())
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "Mark as Read (M)"); !ok {
		t.Fatalf("the reader control does not offer the read: %q", rssHeaderActionNames(t, tt))
	}

	// The reader's own click on the same article is the act that marks it.
	row, ok := tt.Find("The Quiet Return of the Open Feed")
	if !ok {
		t.Fatal("article row not found")
	}
	tt.ClickAt(row.X+row.W/2, row.Y+row.H/2)
	tt.Frame()
	if art := a.SelectedRSSArticle(); art == nil || !art.Read {
		t.Fatalf("the reader's own click left the article unread: %+v", art)
	}
}

// TestRSSReaderToolbarAndListAgreeOnTheReadState walks the read state both ways
// and checks the reader's control, the row's unread dot and the unread counts
// say the same thing at every step.
func TestRSSReaderToolbarAndListAgreeOnTheReadState(t *testing.T) {
	a, _, tt := rssReaderTestWindow(t)
	before := a.RSSTotalUnread

	row, ok := tt.Find("The Quiet Return of the Open Feed")
	if !ok {
		t.Fatal("article row not found")
	}
	tt.ClickAt(row.X+row.W/2, row.Y+row.H/2)
	tt.Frame()

	if _, ok := tt.Find("Unread: The Quiet Return of the Open Feed"); ok {
		t.Fatal("the list still draws the unread dot of an opened article")
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "Mark as Unread (M)"); !ok {
		t.Fatalf("the reader control disagrees with the list: %q", rssHeaderActionNames(t, tt))
	}
	if got := a.RSSTotalUnread; got != before-1 {
		t.Fatalf("the unread total = %d after opening an article, want %d", got, before-1)
	}

	// M flips it back, and the three agree again.
	tt.Key(0, ui.KeyM)
	tt.Frame()
	if _, ok := tt.Find("Unread: The Quiet Return of the Open Feed"); !ok {
		t.Fatal("the list lost the unread dot M returned")
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "Mark as Read (M)"); !ok {
		t.Fatalf("the reader control did not follow M back: %q", rssHeaderActionNames(t, tt))
	}
	if got := a.RSSTotalUnread; got != before {
		t.Fatalf("the unread total = %d after M marked it unread, want %d", got, before)
	}

	// The toolbar control flips it a second time, with the same agreement.
	if err := tt.Click("Mark as Read (M)"); err != nil {
		t.Fatalf("click the reader's read control: %v", err)
	}
	tt.Frame()
	if _, ok := tt.Find("Unread: The Quiet Return of the Open Feed"); ok {
		t.Fatal("the list still draws the unread dot the control cleared")
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "Mark as Unread (M)"); !ok {
		t.Fatalf("the reader control did not follow its own click: %q", rssHeaderActionNames(t, tt))
	}
}

// TestRSSFolderMenuOffersOnlyMarkAllRead pins the folder's menu to exactly one
// read entry, its label in both languages, and the one bulk pass choosing it
// runs: one transaction, one counter refresh, and a read record per article.
func TestRSSFolderMenuOffersOnlyMarkAllRead(t *testing.T) {
	a, _, tt := rssReadStateFixture(t)
	transactions := rssAppTransactions(t, a)
	ctx := context.Background()

	for _, tc := range []struct {
		locale string
		want   []string
	}{
		{
			locale: "en",
			want:   []string{"Mark All Read", "-", "Rename", "Delete Group"},
		},
		{
			locale: "zh",
			want:   []string{"标记整个文件夹已读", "-", "重命名", "删除分组"},
		},
	} {
		tt.SetLocale(tc.locale)
		tt.Frame()
		if err := tt.RightClick("Shelf"); err != nil {
			t.Fatalf("%s: open the folder's menu: %v", tc.locale, err)
		}
		menu := tt.Menu()
		if !slices.Equal(menu, tc.want) {
			t.Fatalf("%s: folder menu = %q, want exactly %q", tc.locale, menu, tc.want)
		}
		tt.CloseMenu()
		tt.Frame()
	}

	// The English menu's one read entry marks the folder read, in one pass.
	tt.SetLocale("en")
	tt.Frame()
	if err := tt.RightClick("Shelf"); err != nil {
		t.Fatalf("open the folder's menu: %v", err)
	}
	before := transactions()
	a.RSCCountReloads = 0
	if err := tt.ChooseMenuItem("Mark All Read"); err != nil {
		t.Fatalf("choose the folder's read entry: %v", err)
	}
	tt.Frame()

	if got := transactions() - before; got != 1 {
		t.Fatalf("the folder's read action committed %d transactions, want 1", got)
	}
	if a.RSCCountReloads != 1 {
		t.Fatalf("the folder's read action refreshed the unread counters %d times, want once", a.RSCCountReloads)
	}
	for _, feedID := range []string{"feed-a", "feed-b"} {
		read, unread := rssStoredCounts(t, a, feedID)
		if unread != 0 || read == 0 {
			t.Fatalf("%s holds %d read and %d unread articles after the folder's read, want all read", feedID, read, unread)
		}
	}
	if read, unread := rssStoredCounts(t, a, "feed-c"); read != 0 || unread != 2 {
		t.Fatalf("the feed outside the folder holds %d read and %d unread articles, want its 2 unread", read, unread)
	}
	// Every article the folder's pass changed recorded its state on this device.
	records, err := a.Store.RSSReadStates(ctx)
	if err != nil {
		t.Fatalf("read the read vector: %v", err)
	}
	for _, id := range []string{"art-feed-a-0", "art-feed-b-1"} {
		if rec, ok := records[id][a.DeviceID]; !ok || !rec.Read {
			t.Fatalf("folder article %q recorded %+v, want read on %q", id, rec, a.DeviceID)
		}
	}
	if _, ok := records["art-feed-c-0"]; ok {
		t.Fatal("the folder's read action recorded a feed outside it")
	}
	if a.RSSTotalUnread != 2 {
		t.Fatalf("the unread total = %d after the folder's read, want the 2 outside it", a.RSSTotalUnread)
	}
}

// TestRSSFeedMenuOffersBothReadActions pins the feed's menu to its two read
// entries and checks each marks the feed in one pass.
func TestRSSFeedMenuOffersBothReadActions(t *testing.T) {
	a, _, tt := rssReadStateFixture(t)
	transactions := rssAppTransactions(t, a)

	if err := tt.RightClick("Feed C"); err != nil {
		t.Fatalf("open the feed's menu: %v", err)
	}
	menu := tt.Menu()
	for _, want := range []string{"Mark Feed as Read", "Mark Feed as Unread"} {
		if !slices.Contains(menu, want) {
			t.Fatalf("the feed's menu %q has no %q entry", menu, want)
		}
	}
	if readAt, unreadAt := slices.Index(menu, "Mark Feed as Read"), slices.Index(menu, "Mark Feed as Unread"); readAt > unreadAt {
		t.Fatalf("the feed's menu %q puts unread before read", menu)
	}
	tt.CloseMenu()
	tt.Frame()

	// The read entry marks the feed, in one transaction and one refresh.
	if err := tt.RightClick("Feed C"); err != nil {
		t.Fatalf("open the feed's menu again: %v", err)
	}
	before := transactions()
	a.RSCCountReloads = 0
	if err := tt.ChooseMenuItem("Mark Feed as Read"); err != nil {
		t.Fatalf("choose the feed's read entry: %v", err)
	}
	tt.Frame()
	if got := transactions() - before; got != 1 {
		t.Fatalf("the feed's read action committed %d transactions, want 1", got)
	}
	if a.RSCCountReloads != 1 {
		t.Fatalf("the feed's read action refreshed the unread counters %d times, want once", a.RSCCountReloads)
	}
	if read, unread := rssStoredCounts(t, a, "feed-c"); read != 2 || unread != 0 {
		t.Fatalf("feed C holds %d read and %d unread articles, want all read", read, unread)
	}
	if read, _ := rssStoredCounts(t, a, "feed-a"); read != 0 {
		t.Fatalf("a feed outside the action holds %d read articles, want none", read)
	}

	// The Chinese menu carries both entries in the feed's language, and the
	// unread entry puts the feed back.
	tt.SetLocale("zh")
	tt.Frame()
	if err := tt.RightClick("Feed C"); err != nil {
		t.Fatalf("open the feed's menu in Chinese: %v", err)
	}
	zhMenu := tt.Menu()
	for _, want := range []string{"标记本订阅源已读", "标记本订阅源未读"} {
		if !slices.Contains(zhMenu, want) {
			t.Fatalf("the Chinese feed menu %q has no %q entry", zhMenu, want)
		}
	}
	if err := tt.ChooseMenuItem("标记本订阅源未读"); err != nil {
		t.Fatalf("choose the feed's unread entry: %v", err)
	}
	tt.Frame()
	if read, unread := rssStoredCounts(t, a, "feed-c"); read != 0 || unread != 2 {
		t.Fatalf("feed C holds %d read and %d unread articles after the unread entry, want all unread", read, unread)
	}
}

// TestRSSShiftCmdUMarksTheScopeRead checks the shortcut's scope follows the
// selected destination: a feed, then a folder, then the whole library for a
// smart feed.
func TestRSSShiftCmdUMarksTheScopeRead(t *testing.T) {
	for _, tc := range []struct {
		name        string
		destination string
		wantRead    []string
		wantUnread  []string
		wantTotal   int
	}{
		{
			name: "feed", destination: "feed:feed-c",
			wantRead: []string{"feed-c"}, wantUnread: []string{"feed-a", "feed-b"}, wantTotal: 5,
		},
		{
			name: "folder", destination: "group:group-shelf",
			wantRead: []string{"feed-a", "feed-b"}, wantUnread: []string{"feed-c"}, wantTotal: 2,
		},
		{
			name: "smart feed", destination: "all",
			wantRead: []string{"feed-a", "feed-b", "feed-c"},
		},
		{
			name: "today", destination: todayDestination,
			wantRead: []string{"feed-a", "feed-b", "feed-c"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _, tt := rssReadStateFixture(t)
			transactions := rssAppTransactions(t, a)
			a.Selected = tc.destination
			tt.Frame()

			before := transactions()
			a.RSCCountReloads = 0
			tt.Key(ui.Cmd|ui.Shift, ui.KeyU)
			tt.Frame()

			if a.RSSTotalUnread != tc.wantTotal {
				t.Fatalf("the unread total = %d after the shortcut, want %d", a.RSSTotalUnread, tc.wantTotal)
			}
			if got := transactions() - before; got != 1 {
				t.Fatalf("the shortcut committed %d transactions, want 1", got)
			}
			if a.RSCCountReloads != 1 {
				t.Fatalf("the shortcut refreshed the unread counters %d times, want once", a.RSCCountReloads)
			}
			for _, feedID := range tc.wantRead {
				if read, unread := rssStoredCounts(t, a, feedID); unread != 0 || read == 0 {
					t.Fatalf("%s holds %d read and %d unread articles, want all read", feedID, read, unread)
				}
			}
			for _, feedID := range tc.wantUnread {
				if read, unread := rssStoredCounts(t, a, feedID); read != 0 || unread == 0 {
					t.Fatalf("%s holds %d read and %d unread articles, want all unread", feedID, read, unread)
				}
			}
		})
	}
}

// TestRSSShiftCmdUHandlesTheLargestFeedInOneTransaction drives the shortcut over
// the 3026-article feed: one committed transaction, one counter refresh, a read
// record per article, and a list that keeps its rows and its paging.
func TestRSSShiftCmdUHandlesTheLargestFeedInOneTransaction(t *testing.T) {
	const count = 3026
	a, _, tt := rssShortcutFixture(t, count)
	transactions := rssAppTransactions(t, a)
	ctx := context.Background()

	before := transactions()
	a.RSCCountReloads = 0
	tt.Key(ui.Cmd|ui.Shift, ui.KeyU)
	tt.Frame()

	if a.RSSTotalUnread != 0 {
		t.Fatalf("the unread total = %d after the shortcut, want 0", a.RSSTotalUnread)
	}
	if got := transactions() - before; got != 1 {
		t.Fatalf("marking %d articles read committed %d transactions, want 1", count, got)
	}
	if a.RSCCountReloads != 1 {
		t.Fatalf("marking %d articles read refreshed the unread counters %d times, want once", count, a.RSCCountReloads)
	}
	if !tt.HasText(fmt.Sprintf("Marked %d articles as read", count)) {
		t.Fatalf("the bulk action left no confirmation of %d articles: %v", count, tt.Texts())
	}
	records, err := a.Store.RSSReadStates(ctx)
	if err != nil {
		t.Fatalf("read the read vector: %v", err)
	}
	if len(records) != count {
		t.Fatalf("the read vector holds %d articles, want %d", len(records), count)
	}
	// The column keeps its rows, and reading is still on demand: the summaries
	// in memory hold no bodies and the open article loads its own.
	if got := len(a.VisibleRSSArticles()); got != count {
		t.Fatalf("the column lists %d articles after the shortcut, want all %d", got, count)
	}
	for _, art := range a.RSSArticles {
		if art.ContentHTML != "" {
			t.Fatalf("the article column holds a body: %+v", art)
		}
	}
}

// TestRSSBulkMarkReadLoadsNoArticleBodies proves the bulk pass updates state
// without pulling the feed's bodies into memory: the pass over a library of
// megabytes of article text allocates a small fraction of it.
func TestRSSBulkMarkReadLoadsNoArticleBodies(t *testing.T) {
	const count = 3026
	a := newTestApp(t, t.TempDir())
	t.Cleanup(func() { a.Close() })
	ctx := context.Background()
	now := time.Now().UnixMilli()

	feed := core.RSSFeed{ID: "feed-heavy", FeedURL: "https://feeds.example.com/heavy.xml", Title: "Heavy", UpdatedAt: now}
	if err := a.Store.SaveRSSFeed(ctx, feed); err != nil {
		t.Fatalf("save the feed: %v", err)
	}
	body := "<p>" + strings.Repeat("the feed's own words ", 256) + "</p>"
	articles := make([]core.RSSArticle, 0, count)
	for i := 0; i < count; i++ {
		articles = append(articles, core.RSSArticle{
			ID:          fmt.Sprintf("art-heavy-%05d", i),
			FeedID:      feed.ID,
			Title:       fmt.Sprintf("Heavy article %05d", i),
			Link:        fmt.Sprintf("https://feeds.example.com/heavy/%05d", i),
			PublishedAt: time.Now().Add(-time.Duration(i) * time.Minute).Format(time.RFC3339),
			Snippet:     "A snippet long enough to fill the row's summary lines.",
			ContentHTML: body,
			IsFullText:  true,
			UpdatedAt:   now,
		})
	}
	if err := a.Store.SaveRSSArticles(ctx, articles); err != nil {
		t.Fatalf("save the articles: %v", err)
	}
	if err := a.ReloadRSS(); err != nil {
		t.Fatalf("reload rss: %v", err)
	}

	bodyBytes := uint64(len(body)) * uint64(count)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	changed, err := a.MarkRSSReadScope(core.RSSReadScope{EveryFeed: true}, true)
	if err != nil {
		t.Fatalf("mark the library read: %v", err)
	}
	runtime.ReadMemStats(&after)

	if changed != count {
		t.Fatalf("the pass changed %d articles, want %d", changed, count)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > bodyBytes/4 {
		t.Fatalf("the bulk pass allocated %d bytes for a library of %d bytes of bodies, so it loaded them", allocated, bodyBytes)
	}
	// The bodies are still where they were: reading is still on demand.
	stored, err := a.Store.GetRSSArticle(ctx, "art-heavy-00000")
	if err != nil || stored == nil || stored.ContentHTML != body {
		t.Fatalf("the read pass did not keep the article's body: %v", err)
	}
}
