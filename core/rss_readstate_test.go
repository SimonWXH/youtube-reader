package core

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// rssBigFeed is the article count of the largest feed the reader has to hold:
// the bulk read-state pass must serve it without a statement per article.
const rssBigFeed = 3026

// rssReadStateStore opens a store at a path the test keeps and puts it on
// SQLite's rollback journal. That journal writes the database header's change
// counter once per committed transaction, so a test can count the transactions
// a write path really ran instead of trusting the code's own report.
func rssReadStateStore(t *testing.T) (*Store, string) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "videos.sqlite")
	store, err := OpenStore(ctx, path)
	if err != nil {
		t.Fatalf("OpenStore failed: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	if _, err := store.DB().Query(ctx, `PRAGMA journal_mode = delete`); err != nil {
		t.Fatalf("switch the store to the rollback journal: %v", err)
	}
	return store, path
}

// rssCommittedTransactions reads the database header's change counter, which the
// rollback journal moves by one for every committed transaction.
func rssCommittedTransactions(t *testing.T, path string) uint32 {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the database: %v", err)
	}
	if len(data) < 28 {
		t.Fatalf("the database is %d bytes, too short for a header", len(data))
	}
	return binary.BigEndian.Uint32(data[24:28])
}

// seedRSSReadStateFeed saves count unread articles into one feed and returns
// their IDs. Every article carries a body of its own, so a bulk pass that loaded
// bodies would show up in the memory a test measures.
func seedRSSReadStateFeed(t *testing.T, store *Store, feedID string, count int, now int64) []string {
	t.Helper()
	body := "<p>" + strings.Repeat("the feed's own words ", 256) + "</p>"
	ids := make([]string, 0, count)
	articles := make([]RSSArticle, 0, count)
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("art:%s:%05d", feedID, i)
		ids = append(ids, id)
		articles = append(articles, RSSArticle{
			ID:          id,
			FeedID:      feedID,
			Title:       fmt.Sprintf("Article %05d", i),
			Link:        fmt.Sprintf("https://feeds.example.com/%s/%05d", feedID, i),
			PublishedAt: time.Now().Add(-time.Duration(i) * time.Minute).Format(time.RFC3339),
			Snippet:     "A snippet long enough to fill the row's summary lines.",
			ContentHTML: body,
			IsFullText:  true,
			UpdatedAt:   now,
		})
	}
	if err := store.SaveRSSArticles(context.Background(), articles); err != nil {
		t.Fatalf("save %d articles: %v", count, err)
	}
	if err := store.SaveRSSFeed(context.Background(), RSSFeed{ID: feedID, FeedURL: "https://feeds.example.com/" + feedID + ".xml", Title: feedID, UpdatedAt: now}); err != nil {
		t.Fatalf("save feed %q: %v", feedID, err)
	}
	return ids
}

// TestRSSBulkMarkReadIsOneTransactionForTheLargestFeed proves the 3026-article
// feed is marked read in one committed transaction, that every article keeps its
// body, and that the same pass records this device's read state for every one of
// them.
func TestRSSBulkMarkReadIsOneTransactionForTheLargestFeed(t *testing.T) {
	ctx := context.Background()
	store, path := rssReadStateStore(t)
	now := time.Now().UnixMilli()
	ids := seedRSSReadStateFeed(t, store, "feed-big", rssBigFeed, now)

	seeded := rssCommittedTransactions(t, path)
	markAt := now + 1
	changed, err := store.MarkRSSArticlesRead(ctx, RSSReadScope{FeedIDs: []string{"feed-big"}}, "device-mac", true, markAt)
	if err != nil {
		t.Fatalf("mark the feed read: %v", err)
	}
	if changed != rssBigFeed || len(ids) != rssBigFeed {
		t.Fatalf("the pass changed %d articles, want %d", changed, rssBigFeed)
	}
	if got := rssCommittedTransactions(t, path) - seeded; got != 1 {
		t.Fatalf("marking %d articles read committed %d transactions, want 1", rssBigFeed, got)
	}

	unread, err := store.RSSUnreadCounts(ctx)
	if err != nil {
		t.Fatalf("read the unread counts: %v", err)
	}
	if len(unread) != 0 {
		t.Fatalf("the library still holds unread counts: %v", unread)
	}

	// The same pass wrote this device's read record for every article.
	records, err := store.RSSReadStates(ctx)
	if err != nil {
		t.Fatalf("read the read vector: %v", err)
	}
	if len(records) != rssBigFeed {
		t.Fatalf("the read vector holds %d articles, want %d", len(records), rssBigFeed)
	}
	for _, id := range ids {
		rec, ok := records[id]["device-mac"]
		if !ok {
			t.Fatalf("article %q has no read record", id)
		}
		if !rec.Read || rec.UpdatedAt != markAt {
			t.Fatalf("article %q recorded %+v, want read at %d", id, rec, markAt)
		}
	}

	// The record covers the whole scope, so a pass over articles that are
	// already read still gains their records while their rows stay put, and a
	// feed marked read before the record existed is completed the next time it
	// is marked.
	settled := rssCommittedTransactions(t, path)
	changed, err = store.MarkRSSArticlesRead(ctx, RSSReadScope{FeedIDs: []string{"feed-big"}}, "device-mac", true, markAt+1)
	if err != nil {
		t.Fatalf("mark the feed read again: %v", err)
	}
	if changed != 0 {
		t.Fatalf("the second pass changed %d articles, want none", changed)
	}
	if got := rssCommittedTransactions(t, path) - settled; got != 1 {
		t.Fatalf("the second pass committed %d transactions, want the one record write", got)
	}
	records, err = store.RSSReadStates(ctx)
	if err != nil {
		t.Fatalf("read the read vector again: %v", err)
	}
	if len(records) != rssBigFeed {
		t.Fatalf("the read vector holds %d articles after a settled pass, want %d", len(records), rssBigFeed)
	}
}

// TestRSSBulkMarkUnreadWritesTheRecord covers the other direction: marking a
// feed unread moves every article back in one transaction and records the
// unread state on this device, so a sync carries it as it carries the read one.
func TestRSSBulkMarkUnreadWritesTheRecord(t *testing.T) {
	ctx := context.Background()
	store, path := rssReadStateStore(t)
	now := time.Now().UnixMilli()
	ids := seedRSSReadStateFeed(t, store, "feed-big", 12, now)

	if _, err := store.MarkRSSArticlesRead(ctx, RSSReadScope{FeedIDs: []string{"feed-big"}}, "device-mac", true, now); err != nil {
		t.Fatalf("mark the library read: %v", err)
	}
	seeded := rssCommittedTransactions(t, path)
	unreadAt := now + 5
	changed, err := store.MarkRSSArticlesRead(ctx, RSSReadScope{FeedIDs: []string{"feed-big"}}, "device-mac", false, unreadAt)
	if err != nil {
		t.Fatalf("mark the feed unread: %v", err)
	}
	if changed != int64(len(ids)) {
		t.Fatalf("the pass changed %d articles, want %d", changed, len(ids))
	}
	if got := rssCommittedTransactions(t, path) - seeded; got != 1 {
		t.Fatalf("marking a feed unread committed %d transactions, want 1", got)
	}

	records, err := store.RSSReadStates(ctx)
	if err != nil {
		t.Fatalf("read the read vector: %v", err)
	}
	for _, id := range ids {
		rec, ok := records[id]["device-mac"]
		if !ok || rec.Read || rec.UpdatedAt != unreadAt {
			t.Fatalf("article %q recorded %+v, want unread at %d", id, rec, unreadAt)
		}
	}
	stored, err := store.GetRSSArticle(ctx, ids[0])
	if err != nil || stored == nil {
		t.Fatalf("read %q back: %v", ids[0], err)
	}
	if stored.Read || stored.ReadAt != 0 {
		t.Fatalf("the article stayed read: %+v", stored)
	}
}

// TestRSSMarkingOneArticleWritesItsRecord covers the single-article path the
// reader's own control uses: the row and this device's read record are written
// in the same transaction.
func TestRSSMarkingOneArticleWritesItsRecord(t *testing.T) {
	ctx := context.Background()
	store, path := rssReadStateStore(t)
	now := time.Now().UnixMilli()
	ids := seedRSSReadStateFeed(t, store, "feed-one", 1, now)

	seeded := rssCommittedTransactions(t, path)
	if err := store.MarkRSSArticleRead(ctx, "device-mac", ids[0], true, now+2); err != nil {
		t.Fatalf("mark the article read: %v", err)
	}
	if got := rssCommittedTransactions(t, path) - seeded; got != 1 {
		t.Fatalf("marking one article read committed %d transactions, want 1", got)
	}
	records, err := store.RSSReadStates(ctx)
	if err != nil {
		t.Fatalf("read the read vector: %v", err)
	}
	rec, ok := records[ids[0]]["device-mac"]
	if !ok || !rec.Read || rec.UpdatedAt != now+2 {
		t.Fatalf("the article recorded %+v, want read at %d", rec, now+2)
	}
}

// TestRSSMarkingAnArticleAlreadyInItsStateKeepsItsStamp covers the no-op touch
// on the single-article path: this device's record is written, while the row's
// read and update stamps stay where the last real change left them, mirroring
// the bulk pass.
func TestRSSMarkingAnArticleAlreadyInItsStateKeepsItsStamp(t *testing.T) {
	ctx := context.Background()
	store, path := rssReadStateStore(t)
	now := time.Now().UnixMilli()
	if err := store.SaveRSSFeed(ctx, RSSFeed{ID: "feed-settled", FeedURL: "https://feeds.example.com/settled.xml", Title: "Settled", UpdatedAt: now}); err != nil {
		t.Fatalf("save the feed: %v", err)
	}
	if err := store.SaveRSSArticles(ctx, []RSSArticle{
		{
			ID: "art-read-settled", FeedID: "feed-settled", Title: "Read and settled",
			Link: "https://feeds.example.com/settled/0", Snippet: "A snippet.",
			PublishedAt: time.Now().Add(-time.Hour).Format(time.RFC3339),
			Read:        true, ReadAt: now - 5000, UpdatedAt: now - 5000,
		},
		{
			ID: "art-unread-settled", FeedID: "feed-settled", Title: "Unread and settled",
			Link: "https://feeds.example.com/settled/1", Snippet: "A snippet.",
			PublishedAt: time.Now().Add(-2 * time.Hour).Format(time.RFC3339),
			UpdatedAt:   now - 4000,
		},
	}); err != nil {
		t.Fatalf("save the articles: %v", err)
	}

	for _, tc := range []struct {
		id            string
		read          bool
		wantReadAt    int64
		wantUpdatedAt int64
	}{
		{id: "art-read-settled", read: true, wantReadAt: now - 5000, wantUpdatedAt: now - 5000},
		{id: "art-unread-settled", read: false, wantReadAt: 0, wantUpdatedAt: now - 4000},
	} {
		seeded := rssCommittedTransactions(t, path)
		if err := store.MarkRSSArticleRead(ctx, "device-mac", tc.id, tc.read, now+1); err != nil {
			t.Fatalf("touch %q: %v", tc.id, err)
		}
		if got := rssCommittedTransactions(t, path) - seeded; got != 1 {
			t.Fatalf("touching %q committed %d transactions, want the one record write", tc.id, got)
		}
		stored, err := store.GetRSSArticle(ctx, tc.id)
		if err != nil || stored == nil {
			t.Fatalf("read %q back: %v", tc.id, err)
		}
		if stored.Read != tc.read || stored.ReadAt != tc.wantReadAt || stored.UpdatedAt != tc.wantUpdatedAt {
			t.Fatalf("touching %q changed a settled row: %+v", tc.id, stored)
		}
		records, err := store.RSSReadStates(ctx)
		if err != nil {
			t.Fatalf("read the read vector: %v", err)
		}
		rec, ok := records[tc.id]["device-mac"]
		if !ok || rec.Read != tc.read || rec.UpdatedAt != now+1 {
			t.Fatalf("%q recorded %+v, want read=%v at %d", tc.id, rec, tc.read, now+1)
		}
	}
}

// TestRSSBulkScopeCoversOnlyItsFeeds checks the scope means what it says: a
// folder action leaves the feeds outside it alone, and a folder that holds no
// feed reaches no article at all.
func TestRSSBulkScopeCoversOnlyItsFeeds(t *testing.T) {
	ctx := context.Background()
	store, path := rssReadStateStore(t)
	now := time.Now().UnixMilli()
	inside := seedRSSReadStateFeed(t, store, "feed-inside", 3, now)
	outside := seedRSSReadStateFeed(t, store, "feed-outside", 2, now)

	if _, err := store.MarkRSSArticlesRead(ctx, RSSReadScope{FeedIDs: []string{"feed-inside"}}, "device-mac", true, now+1); err != nil {
		t.Fatalf("mark the folder read: %v", err)
	}
	counts, err := store.RSSUnreadCounts(ctx)
	if err != nil {
		t.Fatalf("read the unread counts: %v", err)
	}
	if _, ok := counts["feed-inside"]; ok {
		t.Fatalf("the folder's feed still holds unread articles: %v", counts)
	}
	if counts["feed-outside"] != len(outside) {
		t.Fatalf("the feed outside the folder holds %d unread articles, want %d", counts["feed-outside"], len(outside))
	}
	for _, id := range inside {
		stored, err := store.GetRSSArticle(ctx, id)
		if err != nil || stored == nil || !stored.Read {
			t.Fatalf("folder article %q is not read: %v %+v", id, err, stored)
		}
	}

	// A scope that names no feed changes nothing and commits nothing.
	settled := rssCommittedTransactions(t, path)
	changed, err := store.MarkRSSArticlesRead(ctx, RSSReadScope{}, "device-mac", true, now+2)
	if err != nil {
		t.Fatalf("mark an empty scope read: %v", err)
	}
	if changed != 0 {
		t.Fatalf("an empty scope changed %d articles, want none", changed)
	}
	if got := rssCommittedTransactions(t, path) - settled; got != 0 {
		t.Fatalf("an empty scope committed %d transactions, want none", got)
	}
}

// TestRSSBulkMarkReadKeepsBodiesAndPaging checks the pass leaves every stored
// body where it was: marking an article read is a state change, not a cleanup.
func TestRSSBulkMarkReadKeepsBodiesAndPaging(t *testing.T) {
	ctx := context.Background()
	store, _ := rssReadStateStore(t)
	now := time.Now().UnixMilli()
	ids := seedRSSReadStateFeed(t, store, "feed-bodies", 4, now)

	if _, err := store.MarkRSSArticlesRead(ctx, RSSReadScope{FeedIDs: []string{"feed-bodies"}}, "device-mac", true, now+1); err != nil {
		t.Fatalf("mark the library read: %v", err)
	}
	for _, id := range ids {
		stored, err := store.GetRSSArticle(ctx, id)
		if err != nil || stored == nil {
			t.Fatalf("read %q back: %v", id, err)
		}
		if stored.ContentHTML == "" || !stored.IsFullText {
			t.Fatalf("the read pass dropped the body of %q: %+v", id, stored)
		}
	}
	// The list still pages: a summary query holds no body, and the one article
	// the reader opens still loads its own.
	summaries, err := store.ListRSSArticleSummaries(ctx, RSSArticleFilter{Limit: 2})
	if err != nil {
		t.Fatalf("list summaries: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("the list returned %d summaries, want the 2 it asked for", len(summaries))
	}
	for _, summary := range summaries {
		if summary.ContentHTML != "" {
			t.Fatalf("a summary list loaded a body: %+v", summary)
		}
	}
}

// TestRSSBulkArticleScopeMarksOnlyTheNamedArticles covers the article-ID form of
// the scope a smart feed uses: the pass reaches the named articles and nothing
// else, in one transaction, and records each of them on this device.
func TestRSSBulkArticleScopeMarksOnlyTheNamedArticles(t *testing.T) {
	ctx := context.Background()
	store, path := rssReadStateStore(t)
	now := time.Now().UnixMilli()
	named := seedRSSReadStateFeed(t, store, "feed-today", 3, now)
	outside := seedRSSReadStateFeed(t, store, "feed-old", 2, now)

	scope := RSSReadScope{ArticleIDs: []string{named[0], named[2]}}
	seeded := rssCommittedTransactions(t, path)
	changed, err := store.MarkRSSArticlesRead(ctx, scope, "device-mac", true, now+1)
	if err != nil {
		t.Fatalf("mark the named articles read: %v", err)
	}
	if changed != 2 {
		t.Fatalf("the pass changed %d articles, want the 2 it named", changed)
	}
	if got := rssCommittedTransactions(t, path) - seeded; got != 1 {
		t.Fatalf("marking the named articles read committed %d transactions, want 1", got)
	}

	for _, id := range append([]string{named[1]}, outside...) {
		stored, err := store.GetRSSArticle(ctx, id)
		if err != nil || stored == nil {
			t.Fatalf("read %q back: %v", id, err)
		}
		if stored.Read {
			t.Fatalf("an article the scope did not name is read: %q", id)
		}
	}
	records, err := store.RSSReadStates(ctx)
	if err != nil {
		t.Fatalf("read the read vector: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("the read vector holds %d articles, want the 2 the scope named", len(records))
	}
	for _, id := range []string{named[0], named[2]} {
		if rec, ok := records[id]["device-mac"]; !ok || !rec.Read {
			t.Fatalf("named article %q recorded %+v, want read", id, rec)
		}
	}
	if _, ok := records[named[1]]; ok {
		t.Fatalf("the pass recorded an article outside the scope: %q", named[1])
	}
}

// TestRSSBulkReadSkipsUnsubscribedFeeds covers the library's own visibility
// guard: a feed the reader unsubscribed from keeps its articles, but no bulk
// pass counts or writes them.
func TestRSSBulkReadSkipsUnsubscribedFeeds(t *testing.T) {
	ctx := context.Background()
	store, path := rssReadStateStore(t)
	now := time.Now().UnixMilli()
	kept := seedRSSReadStateFeed(t, store, "feed-kept", 2, now)
	gone := seedRSSReadStateFeed(t, store, "feed-gone", 2, now)
	if err := store.DeleteRSSFeed(ctx, "feed-gone", false); err != nil {
		t.Fatalf("unsubscribe from the feed: %v", err)
	}

	seeded := rssCommittedTransactions(t, path)
	changed, err := store.MarkRSSArticlesRead(ctx, RSSReadScope{FeedIDs: []string{"feed-kept", "feed-gone"}}, "device-mac", true, now+1)
	if err != nil {
		t.Fatalf("mark the library read: %v", err)
	}
	if changed != int64(len(kept)) {
		t.Fatalf("the pass changed %d articles, want the %d of the subscribed feed", changed, len(kept))
	}
	if got := rssCommittedTransactions(t, path) - seeded; got != 1 {
		t.Fatalf("the pass committed %d transactions, want 1", got)
	}
	for _, id := range gone {
		stored, err := store.GetRSSArticle(ctx, id)
		if err != nil || stored == nil {
			t.Fatalf("read %q back: %v", id, err)
		}
		if stored.Read {
			t.Fatalf("the pass marked the unsubscribed feed's article %q read", id)
		}
	}

	// A scope that names the unsubscribed feed alone, by feed or by article,
	// reaches nothing and commits nothing.
	for _, scope := range []RSSReadScope{
		{FeedIDs: []string{"feed-gone"}},
		{ArticleIDs: []string{gone[0]}},
	} {
		settled := rssCommittedTransactions(t, path)
		changed, err := store.MarkRSSArticlesRead(ctx, scope, "device-mac", true, now+2)
		if err != nil {
			t.Fatalf("mark an unsubscribed feed's scope read: %v", err)
		}
		if changed != 0 {
			t.Fatalf("the scope changed %d articles, want none", changed)
		}
		if got := rssCommittedTransactions(t, path) - settled; got != 0 {
			t.Fatalf("the scope committed %d transactions, want none", got)
		}
	}
}

// TestRSSBulkMarkReadRecordsArticlesThatWereAlreadyRead covers a library
// written before the read vector existed: articles read in their row and with
// no record gain this device's record when their feed is marked read again, and
// the stored state survives a reopen.
func TestRSSBulkMarkReadRecordsArticlesThatWereAlreadyRead(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "videos.sqlite")
	store, err := OpenStore(ctx, path)
	if err != nil {
		t.Fatalf("OpenStore failed: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	now := time.Now().UnixMilli()
	if err := store.SaveRSSFeed(ctx, RSSFeed{ID: "feed-legacy", FeedURL: "https://feeds.example.com/legacy.xml", Title: "Legacy", UpdatedAt: now}); err != nil {
		t.Fatalf("save the feed: %v", err)
	}
	// SaveRSSArticles keeps an article's read state without writing a record,
	// which is the shape a library written before the vector holds.
	if err := store.SaveRSSArticles(ctx, []RSSArticle{{
		ID: "art-legacy", FeedID: "feed-legacy", Title: "Already read",
		Link: "https://feeds.example.com/legacy/0", Snippet: "A snippet.",
		PublishedAt: time.Now().Add(-time.Hour).Format(time.RFC3339),
		Read:        true, ReadAt: now - 1000, UpdatedAt: now - 1000,
	}}); err != nil {
		t.Fatalf("save the article: %v", err)
	}
	if records, err := store.RSSReadStates(ctx); err != nil || len(records) != 0 {
		t.Fatalf("the saved library already holds read records: %v %v", records, err)
	}

	changed, err := store.MarkRSSArticlesRead(ctx, RSSReadScope{FeedIDs: []string{"feed-legacy"}}, "device-mac", true, now+1)
	if err != nil {
		t.Fatalf("mark the feed read: %v", err)
	}
	if changed != 0 {
		t.Fatalf("the pass changed %d articles, want none", changed)
	}
	records, err := store.RSSReadStates(ctx)
	if err != nil {
		t.Fatalf("read the read vector: %v", err)
	}
	if rec, ok := records["art-legacy"]["device-mac"]; !ok || !rec.Read || rec.UpdatedAt != now+1 {
		t.Fatalf("the already read article recorded %+v, want read at %d", rec, now+1)
	}

	reopened, err := OpenStore(ctx, path)
	if err != nil {
		t.Fatalf("reopen the store: %v", err)
	}
	t.Cleanup(func() { reopened.Close() })
	stored, err := reopened.GetRSSArticle(ctx, "art-legacy")
	if err != nil || stored == nil || !stored.Read {
		t.Fatalf("the reopened store lost the read state: %v %+v", err, stored)
	}
	if stored.ReadAt != now-1000 {
		t.Fatalf("the pass moved the read stamp of an already read article to %d, want %d", stored.ReadAt, now-1000)
	}
}
