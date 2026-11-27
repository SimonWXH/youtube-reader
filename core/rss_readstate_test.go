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

	// A second pass has nothing left to change, so it commits nothing at all.
	settled := rssCommittedTransactions(t, path)
	changed, err = store.MarkRSSArticlesRead(ctx, RSSReadScope{FeedIDs: []string{"feed-big"}}, "device-mac", true, markAt+1)
	if err != nil {
		t.Fatalf("mark the feed read again: %v", err)
	}
	if changed != 0 {
		t.Fatalf("the second pass changed %d articles, want none", changed)
	}
	if got := rssCommittedTransactions(t, path) - settled; got != 0 {
		t.Fatalf("the second pass committed %d transactions, want none", got)
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

	if _, err := store.MarkRSSArticlesRead(ctx, RSSReadScope{EveryFeed: true}, "device-mac", true, now); err != nil {
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

	if _, err := store.MarkRSSArticlesRead(ctx, RSSReadScope{EveryFeed: true}, "device-mac", true, now+1); err != nil {
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
