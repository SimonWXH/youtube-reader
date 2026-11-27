package core

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "test.sqlite")
	store, err := OpenStore(ctx, path)
	if err != nil {
		t.Fatalf("OpenStore failed: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestRSSStoreFeedCRUDAndTombstone(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	now := time.Now().UnixMilli()
	feed := RSSFeed{
		ID:                  "feed:12345678",
		FeedURL:             "https://example.com/rss.xml",
		SiteURL:             "https://example.com",
		Title:               "Original Title",
		CustomTitle:         "My Custom Feed",
		GroupIDs:            []string{"grp-1", "grp-2"},
		AvatarURL:           "https://example.com/avatar.png",
		ETag:                `W/"etag-xyz"`,
		LastModified:        "Mon, 10 Feb 2026 12:00:00 GMT",
		RefreshIntervalMins: 45,
		UpdatedAt:           now,
	}

	if err := store.SaveRSSFeed(ctx, feed); err != nil {
		t.Fatalf("SaveRSSFeed failed: %v", err)
	}

	got, err := store.GetRSSFeed(ctx, feed.ID)
	if err != nil || got == nil {
		t.Fatalf("GetRSSFeed failed: %v", err)
	}
	if got.Title != "Original Title" || got.CustomTitle != "My Custom Feed" || got.DisplayName() != "My Custom Feed" {
		t.Errorf("feed fields mismatch: %+v", got)
	}
	if got.RefreshIntervalMins != 45 || got.ETag != `W/"etag-xyz"` {
		t.Errorf("metadata mismatch: %+v", got)
	}
	if len(got.GroupIDs) != 2 || got.GroupIDs[0] != "grp-1" {
		t.Errorf("groupIDs mismatch: %+v", got.GroupIDs)
	}

	// Soft delete with tombstone
	if err := store.DeleteRSSFeed(ctx, feed.ID, false); err != nil {
		t.Fatalf("DeleteRSSFeed soft failed: %v", err)
	}

	activeFeeds, err := store.ListRSSFeeds(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(activeFeeds) != 0 {
		t.Fatalf("expected 0 active feeds, got %d", len(activeFeeds))
	}

	allFeeds, err := store.ListRSSFeeds(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(allFeeds) != 1 || !allFeeds[0].Deleted || allFeeds[0].DeletedAt == 0 {
		t.Fatalf("expected 1 tombstoned feed, got %+v", allFeeds)
	}

	// Hard delete
	if err := store.DeleteRSSFeed(ctx, feed.ID, true); err != nil {
		t.Fatalf("DeleteRSSFeed hard failed: %v", err)
	}
	allFeedsAfter, _ := store.ListRSSFeeds(ctx, true)
	if len(allFeedsAfter) != 0 {
		t.Fatalf("expected 0 feeds after hard delete, got %d", len(allFeedsAfter))
	}
}

func TestSaveRSSFeedMetadataDoesNotReviveTombstone(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	now := time.Now().UnixMilli()
	feed := RSSFeed{
		ID:        "feed:tombstone-preserved",
		FeedURL:   "https://example.com/rss",
		Title:     "Original Title",
		UpdatedAt: now,
	}
	if err := store.SaveRSSFeed(ctx, feed); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteRSSFeed(ctx, feed.ID, false); err != nil {
		t.Fatal(err)
	}

	// A background refresh holds a snapshot taken before the deletion and saves it after.
	stale := feed
	stale.Title = "Title From Refresh"
	stale.UpdatedAt = now + 5000
	if err := store.SaveRSSFeed(ctx, stale); err != nil {
		t.Fatal(err)
	}

	active, err := store.ListRSSFeeds(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("metadata save must not revive a deleted feed, got %+v", active)
	}
	got, err := store.GetRSSFeed(ctx, feed.ID)
	if err != nil || got == nil {
		t.Fatalf("GetRSSFeed failed: %v", err)
	}
	if !got.Deleted || got.DeletedAt == 0 {
		t.Fatalf("tombstone must survive metadata saves, got %+v", got)
	}
	if got.Title != "Title From Refresh" {
		t.Fatalf("metadata updates should still apply to deleted feeds, got %q", got.Title)
	}

	// Only the explicit subscription path revives a feed.
	if err := store.SubscribeRSSFeed(ctx, feed); err != nil {
		t.Fatalf("SubscribeRSSFeed failed: %v", err)
	}
	active, err = store.ListRSSFeeds(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].ID != feed.ID || active[0].Deleted || active[0].DeletedAt != 0 {
		t.Fatalf("re-subscribe must revive the feed, got %+v", active)
	}
}

func TestRSSStoreGroupCRUDAndTombstone(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	now := time.Now().UnixMilli()
	group := RSSGroup{
		ID:        "rss-group-tech",
		Name:      "Technology",
		FeedIDs:   []string{"feed:1", "feed:2"},
		SortOrder: 1,
		UpdatedAt: now,
	}

	if err := store.SaveRSSGroup(ctx, group); err != nil {
		t.Fatalf("SaveRSSGroup failed: %v", err)
	}

	got, err := store.GetRSSGroup(ctx, group.ID)
	if err != nil || got == nil {
		t.Fatalf("GetRSSGroup failed: %v", err)
	}
	if got.Name != "Technology" || len(got.FeedIDs) != 2 {
		t.Fatalf("group data mismatch: %+v", got)
	}

	// Soft delete
	if err := store.DeleteRSSGroup(ctx, group.ID, false); err != nil {
		t.Fatal(err)
	}
	activeGroups, _ := store.ListRSSGroups(ctx, false)
	if len(activeGroups) != 0 {
		t.Fatalf("expected 0 active groups, got %d", len(activeGroups))
	}

	allGroups, _ := store.ListRSSGroups(ctx, true)
	if len(allGroups) != 1 || !allGroups[0].Deleted {
		t.Fatalf("expected 1 tombstoned group, got %+v", allGroups)
	}
}

func TestRSSStoreArticleIdempotencyPreservesReadAndStarred(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	now := time.Now().UnixMilli()
	artID := "art:feed:1:item-1"

	// First fetch saves initial unread, unstarred article
	initialArticles := []RSSArticle{
		{
			ID:          artID,
			FeedID:      "feed:1",
			Title:       "Original Article Title",
			Link:        "https://example.com/item/1",
			PublishedAt: "2026-02-10T12:00:00Z",
			Snippet:     "Original snippet",
			ContentHTML: "<p>Original content</p>",
			IsFullText:  false,
			Read:        false,
			Starred:     false,
			UpdatedAt:   now,
		},
	}

	if err := store.SaveRSSArticles(ctx, initialArticles); err != nil {
		t.Fatalf("SaveRSSArticles failed: %v", err)
	}

	// User marks article as read and starred
	readTime := now + 1000
	starTime := now + 2000
	if err := store.MarkRSSArticleRead(ctx, "device-test", artID, true, readTime); err != nil {
		t.Fatalf("MarkRSSArticleRead failed: %v", err)
	}
	if err := store.SetRSSArticleStarred(ctx, artID, true, starTime); err != nil {
		t.Fatalf("SetRSSArticleStarred failed: %v", err)
	}

	// Verify it is read and starred
	beforeRefetch, err := store.GetRSSArticle(ctx, artID)
	if err != nil || beforeRefetch == nil {
		t.Fatal(err)
	}
	if !beforeRefetch.Read || beforeRefetch.ReadAt != readTime {
		t.Fatalf("article should be read, got: %+v", beforeRefetch)
	}
	if !beforeRefetch.Starred || beforeRefetch.StarredAt != starTime {
		t.Fatalf("article should be starred, got: %+v", beforeRefetch)
	}

	// Repeated fetch arrives later: it carries updated content, but Read: false, Starred: false
	refetchedArticles := []RSSArticle{
		{
			ID:          artID,
			FeedID:      "feed:1",
			Title:       "Updated Article Title",
			Link:        "https://example.com/item/1",
			PublishedAt: "2026-02-10T12:00:00Z",
			Snippet:     "Updated snippet",
			ContentHTML: "<p>Updated full content</p>",
			IsFullText:  true,
			Read:        false, // Fresh feed items are unread by default
			Starred:     false, // Fresh feed items are unstarred by default
			UpdatedAt:   now + 5000,
		},
	}

	if err := store.SaveRSSArticles(ctx, refetchedArticles); err != nil {
		t.Fatalf("SaveRSSArticles on refetch failed: %v", err)
	}

	afterRefetch, err := store.GetRSSArticle(ctx, artID)
	if err != nil || afterRefetch == nil {
		t.Fatal(err)
	}

	// INVARIANT: Content was updated, but Read and Starred states were NOT reset!
	if afterRefetch.Title != "Updated Article Title" {
		t.Errorf("title should be updated, got %q", afterRefetch.Title)
	}
	if afterRefetch.ContentHTML != "<p>Updated full content</p>" {
		t.Errorf("contentHTML should be updated, got %q", afterRefetch.ContentHTML)
	}
	if !afterRefetch.IsFullText {
		t.Errorf("isFullText should be updated to true")
	}

	// Read and Starred must remain preserved
	if !afterRefetch.Read || afterRefetch.ReadAt != readTime {
		t.Fatalf("CRITICAL BUG: Article read state was overwritten on refetch! got read=%v readAt=%d", afterRefetch.Read, afterRefetch.ReadAt)
	}
	if !afterRefetch.Starred || afterRefetch.StarredAt != starTime {
		t.Fatalf("CRITICAL BUG: Article starred state was overwritten on refetch! got starred=%v starredAt=%d", afterRefetch.Starred, afterRefetch.StarredAt)
	}
}

func TestRSSStoreRetentionNeverPurgesUnreadOrStarred(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	feedID := "feed:test-retention"
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	body := "<p>body</p>"
	feedBody := "<p>feed body</p>"

	articles := []RSSArticle{
		{
			ID:          "art-unread-old",
			FeedID:      feedID,
			Title:       "Unread Old Article",
			ContentHTML: body,
			PublishedAt: now.Add(-60 * 24 * time.Hour).Format(time.RFC3339),
			Read:        false,
		},
		{
			ID:          "art-starred-old",
			FeedID:      feedID,
			Title:       "Starred Old Article",
			ContentHTML: body,
			PublishedAt: now.Add(-60 * 24 * time.Hour).Format(time.RFC3339),
			Read:        true,
			Starred:     true,
		},
		{
			ID:          "art-read-old",
			FeedID:      feedID,
			Title:       "Read Old Article",
			ContentHTML: body,
			PublishedAt: now.Add(-60 * 24 * time.Hour).Format(time.RFC3339),
			Read:        true,
		},
		{
			ID:          "art-read-recent",
			FeedID:      feedID,
			Title:       "Read Recent Article",
			ContentHTML: body,
			PublishedAt: now.Add(-2 * 24 * time.Hour).Format(time.RFC3339),
			Read:        true,
		},
		{
			ID:          "art-unread-recent",
			FeedID:      feedID,
			Title:       "Unread Recent Article",
			ContentHTML: body,
			PublishedAt: now.Add(-2 * 24 * time.Hour).Format(time.RFC3339),
			Read:        false,
		},
	}

	if err := store.SaveRSSArticles(ctx, articles); err != nil {
		t.Fatalf("SaveRSSArticles failed: %v", err)
	}

	// Every article keeps the feed's own body beside its extracted one.
	for i := range articles {
		articles[i].FeedContentHTML = feedBody
	}
	if err := store.SaveRSSArticles(ctx, articles); err != nil {
		t.Fatalf("SaveRSSArticles with the feed bodies failed: %v", err)
	}

	// Over the read cap by one: only the oldest read, unstarred article may be purged.
	purgedCount, err := store.CleanOldRSSArticles(ctx, feedID, 1, 30*24*time.Hour, now)
	if err != nil {
		t.Fatalf("CleanOldRSSArticles failed: %v", err)
	}
	if purgedCount != 1 {
		t.Fatalf("expected exactly 1 article purged, got %d", purgedCount)
	}

	for _, id := range []string{"art-unread-old", "art-starred-old", "art-read-recent", "art-unread-recent"} {
		a, err := store.GetRSSArticle(ctx, id)
		if err != nil || a == nil {
			t.Fatalf("protected article %q was mistakenly purged", id)
		}
		if a.ContentHTML != body {
			t.Fatalf("protected article %q lost its content, got %q", id, a.ContentHTML)
		}
		if a.FeedContentHTML != feedBody {
			t.Fatalf("protected article %q lost the feed's own body, got %q", id, a.FeedContentHTML)
		}
	}

	old, err := store.GetRSSArticle(ctx, "art-read-old")
	if err != nil || old == nil {
		t.Fatalf("purged article metadata must survive, got %+v (err %v)", old, err)
	}
	if old.ContentHTML != "" || old.FeedContentHTML != "" || len(old.Blocks) != 0 || old.IsFullText {
		t.Fatalf("purged article body should be cleared, got %+v", old)
	}
	if !old.Read {
		t.Fatalf("purged article must keep its read state")
	}
}

func TestRSSStoreRetentionOnlyPurgesAboveCap(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	feedID := "feed:test-retention-under-cap"
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	body := "<p>body</p>"

	var articles []RSSArticle
	for _, age := range []int{90, 60, 31} {
		articles = append(articles, RSSArticle{
			ID:          "art-below-" + string(rune('a'+age)),
			FeedID:      feedID,
			Title:       "Old Read Article",
			ContentHTML: body,
			PublishedAt: now.Add(-time.Duration(age) * 24 * time.Hour).Format(time.RFC3339),
			Read:        true,
		})
	}
	if err := store.SaveRSSArticles(ctx, articles); err != nil {
		t.Fatal(err)
	}

	purgedCount, err := store.CleanOldRSSArticles(ctx, feedID, 500, 30*24*time.Hour, now)
	if err != nil {
		t.Fatalf("CleanOldRSSArticles failed: %v", err)
	}
	if purgedCount != 0 {
		t.Fatalf("age alone must never purge articles below the cap, purged %d", purgedCount)
	}

	for _, a := range articles {
		got, err := store.GetRSSArticle(ctx, a.ID)
		if err != nil || got == nil || got.ContentHTML != body {
			t.Fatalf("below-cap article %q lost content: %+v", a.ID, got)
		}
	}
}

func TestRSSStoreRetentionPreservesReadStateAcrossRefetch(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	feedID := "feed:test-retention-refetch"
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	articles := []RSSArticle{
		{
			ID:          "art-refetch-old",
			FeedID:      feedID,
			Title:       "Old Read Article",
			ContentHTML: "<p>old body</p>",
			PublishedAt: now.Add(-60 * 24 * time.Hour).Format(time.RFC3339),
			Read:        true,
			ReadAt:      now.Add(-59 * 24 * time.Hour).UnixMilli(),
		},
		{
			ID:          "art-refetch-new",
			FeedID:      feedID,
			Title:       "New Read Article",
			ContentHTML: "<p>new body</p>",
			PublishedAt: now.Add(-1 * 24 * time.Hour).Format(time.RFC3339),
			Read:        true,
		},
	}
	if err := store.SaveRSSArticles(ctx, articles); err != nil {
		t.Fatal(err)
	}

	if purgedCount, err := store.CleanOldRSSArticles(ctx, feedID, 1, 30*24*time.Hour, now); err != nil || purgedCount != 1 {
		t.Fatalf("expected 1 article purged, got %d (err %v)", purgedCount, err)
	}

	// The feed still publishes the article, so the next refresh re-saves it as unread.
	if err := store.SaveRSSArticles(ctx, []RSSArticle{{
		ID:          "art-refetch-old",
		FeedID:      feedID,
		Title:       "Old Read Article",
		ContentHTML: "<p>refreshed body</p>",
		PublishedAt: now.Add(-60 * 24 * time.Hour).Format(time.RFC3339),
		Read:        false,
		UpdatedAt:   now.UnixMilli(),
	}}); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetRSSArticle(ctx, "art-refetch-old")
	if err != nil || got == nil {
		t.Fatalf("GetRSSArticle failed: %v", err)
	}
	if !got.Read || got.ReadAt == 0 {
		t.Fatalf("a refresh must not turn a purged read article back into unread: %+v", got)
	}
	if got.ContentHTML != "<p>refreshed body</p>" {
		t.Fatalf("refresh should refill the purged body, got %q", got.ContentHTML)
	}
}

func TestRSSStoreArticleFiltering(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	feed1 := "feed:1"
	feed2 := "feed:2"

	articles := []RSSArticle{
		{ID: "a1", FeedID: feed1, Title: "Go 1.24 Announcement", Snippet: "New features in Go", PublishedAt: "2026-02-10T12:00:00Z", Read: false, Starred: true},
		{ID: "a2", FeedID: feed1, Title: "Rust Updates", Snippet: "Cargo improvements", PublishedAt: "2026-02-09T12:00:00Z", Read: true, Starred: false},
		{ID: "a3", FeedID: feed2, Title: "Go Tooling", Snippet: "Compiler speedups", PublishedAt: "2026-02-08T12:00:00Z", Read: false, Starred: false},
	}
	if err := store.SaveRSSArticles(ctx, articles); err != nil {
		t.Fatal(err)
	}

	// Filter by FeedID
	f1Arts, _ := store.ListRSSArticles(ctx, RSSArticleFilter{FeedID: feed1})
	if len(f1Arts) != 2 {
		t.Errorf("expected 2 articles for feed1, got %d", len(f1Arts))
	}

	// Filter UnreadOnly
	unreadArts, _ := store.ListRSSArticles(ctx, RSSArticleFilter{UnreadOnly: true})
	if len(unreadArts) != 2 {
		t.Errorf("expected 2 unread articles, got %d", len(unreadArts))
	}

	// Filter StarredOnly
	starredArts, _ := store.ListRSSArticles(ctx, RSSArticleFilter{StarredOnly: true})
	if len(starredArts) != 1 || starredArts[0].ID != "a1" {
		t.Errorf("expected a1 starred, got %+v", starredArts)
	}

	// Search
	searchArts, _ := store.ListRSSArticles(ctx, RSSArticleFilter{Search: "Compiler"})
	if len(searchArts) != 1 || searchArts[0].ID != "a3" {
		t.Errorf("expected a3 matching search, got %+v", searchArts)
	}

	// Limit and Offset
	paged, _ := store.ListRSSArticles(ctx, RSSArticleFilter{Limit: 1, Offset: 1})
	if len(paged) != 1 || paged[0].ID != "a2" {
		t.Errorf("expected a2 on second page, got %+v", paged)
	}
}

func TestRSSStoreNotesCRUD(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	now := time.Now().UnixMilli()
	note1 := ArticleNoteRecord{
		ID:             "note-1",
		ArticleID:      "art-1",
		ParagraphIndex: 2,
		ParagraphHash:  "hash-abc",
		SelectedText:   "Concurrency patterns in Go",
		NoteText:       "Remember to review worker pool implementation here.",
		UpdatedAt:      now,
	}
	note2 := ArticleNoteRecord{
		ID:             "note-2",
		ArticleID:      "art-1",
		ParagraphIndex: 0,
		ParagraphHash:  "hash-xyz",
		SelectedText:   "Introduction",
		NoteText:       "Good summary.",
		UpdatedAt:      now,
	}

	if err := store.SaveRSSNote(ctx, note1); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRSSNote(ctx, note2); err != nil {
		t.Fatal(err)
	}

	notes, err := store.ListRSSNotes(ctx, "art-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 2 {
		t.Fatalf("expected 2 notes, got %d", len(notes))
	}
	// Ordered by paragraph index
	if notes[0].ParagraphIndex != 0 || notes[1].ParagraphIndex != 2 {
		t.Errorf("notes not sorted by paragraph index: %+v", notes)
	}
}

func TestRSSStoreMigrationCompatibility(t *testing.T) {
	ctx := context.Background()
	// openOldStore creates a DB with only legacy channels and videos
	oldPath := openOldStore(t)
	store, err := OpenStore(ctx, oldPath)
	if err != nil {
		t.Fatalf("OpenStore on older DB failed: %v", err)
	}
	defer store.Close()

	// 1. Existing channels and videos must remain intact
	channels, err := store.Channels(ctx)
	if err != nil || len(channels) != 1 || channels[0].ID != "ch-1" {
		t.Fatalf("existing channel damaged during migration: %+v", channels)
	}
	videos, err := store.Videos(ctx)
	if err != nil || len(videos) != 1 || videos[0].ID != "v1" {
		t.Fatalf("existing video damaged during migration: %+v", videos)
	}

	// 2. New RSS tables are immediately operational
	now := time.Now().UnixMilli()
	feed := RSSFeed{
		ID:        "feed:migrated",
		FeedURL:   "https://example.com/feed",
		Title:     "Migrated Feed",
		UpdatedAt: now,
	}
	if err := store.SaveRSSFeed(ctx, feed); err != nil {
		t.Fatalf("SaveRSSFeed on migrated store failed: %v", err)
	}

	gotFeed, err := store.GetRSSFeed(ctx, "feed:migrated")
	if err != nil || gotFeed == nil || gotFeed.Title != "Migrated Feed" {
		t.Fatalf("GetRSSFeed on migrated store failed: %+v", gotFeed)
	}

	if store.Count(ctx, "rss_feeds") != 1 {
		t.Fatalf("Count for rss_feeds should be 1, got %d", store.Count(ctx, "rss_feeds"))
	}
}

func TestSaveRSSArticlesPreservesFullTextOnRefresh(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	now := time.Now().UnixMilli()
	feed := RSSFeed{
		ID:        "feed:test-fulltext",
		FeedURL:   "https://example.com/rss",
		Title:     "Test Feed",
		UpdatedAt: now,
	}
	if err := store.SaveRSSFeed(ctx, feed); err != nil {
		t.Fatal(err)
	}

	initialArticle := RSSArticle{
		ID:          "art:test-fulltext:1",
		FeedID:      feed.ID,
		Title:       "Sample Article",
		Link:        "https://example.com/post/1",
		ContentHTML: "<article><p>Comprehensive full text article body here.</p></article>",
		IsFullText:  true,
		Blocks: []ArticleBlock{
			{Type: BlockParagraph, Text: "Comprehensive full text article body here.", Hash: "hash123"},
		},
		UpdatedAt: now,
	}

	if err := store.SaveRSSArticles(ctx, []RSSArticle{initialArticle}); err != nil {
		t.Fatalf("initial SaveRSSArticles failed: %v", err)
	}

	got, err := store.GetRSSArticle(ctx, initialArticle.ID)
	if err != nil || got == nil {
		t.Fatalf("GetRSSArticle failed: %v", err)
	}
	if !got.IsFullText || got.ContentHTML != initialArticle.ContentHTML || len(got.Blocks) != 1 {
		t.Fatalf("expected full text and blocks preserved initially, got: %+v", got)
	}

	refreshedSummaryArticle := RSSArticle{
		ID:          initialArticle.ID,
		FeedID:      feed.ID,
		Title:       "Sample Article",
		Link:        "https://example.com/post/1",
		Snippet:     "Summary snippet",
		ContentHTML: "<p>Short summary from feed.</p>",
		IsFullText:  false,
		Blocks:      nil,
		UpdatedAt:   now + 1000,
	}

	if err := store.SaveRSSArticles(ctx, []RSSArticle{refreshedSummaryArticle}); err != nil {
		t.Fatalf("refresh SaveRSSArticles failed: %v", err)
	}

	gotAfter, err := store.GetRSSArticle(ctx, initialArticle.ID)
	if err != nil || gotAfter == nil {
		t.Fatalf("GetRSSArticle after refresh failed: %v", err)
	}
	if !gotAfter.IsFullText {
		t.Fatalf("IsFullText should remain true, got false")
	}
	if gotAfter.ContentHTML != initialArticle.ContentHTML {
		t.Fatalf("ContentHTML was overwritten by summary: got %q, want %q", gotAfter.ContentHTML, initialArticle.ContentHTML)
	}
	if len(gotAfter.Blocks) != 1 || gotAfter.Blocks[0].Text != initialArticle.Blocks[0].Text {
		t.Fatalf("Blocks were wiped by summary refresh: got %+v", gotAfter.Blocks)
	}
}

func TestDeleteRSSFeedSoftDeleteArticles(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	now := time.Now().UnixMilli()
	feed := RSSFeed{
		ID:        "feed:test-softdel",
		FeedURL:   "https://example.com/rss",
		Title:     "Soft Delete Test Feed",
		UpdatedAt: now,
	}
	if err := store.SaveRSSFeed(ctx, feed); err != nil {
		t.Fatal(err)
	}

	articles := []RSSArticle{
		{
			ID:          "art:test-softdel:unstarred",
			FeedID:      feed.ID,
			Title:       "Unstarred Article",
			Link:        "https://example.com/post/1",
			ContentHTML: "<p>Post 1</p>",
			Starred:     false,
			Read:        false,
			UpdatedAt:   now,
		},
		{
			ID:          "art:test-softdel:starred",
			FeedID:      feed.ID,
			Title:       "Starred Article",
			Link:        "https://example.com/post/2",
			ContentHTML: "<p>Post 2</p>",
			Starred:     true,
			StarredAt:   now,
			Read:        false,
			UpdatedAt:   now,
		},
	}
	if err := store.SaveRSSArticles(ctx, articles); err != nil {
		t.Fatal(err)
	}

	if err := store.MarkRSSArticleRead(ctx, "device-test", "art:test-softdel:unstarred", true, now); err != nil {
		t.Fatal(err)
	}

	if err := store.DeleteRSSFeed(ctx, feed.ID, false); err != nil {
		t.Fatalf("DeleteRSSFeed soft failed: %v", err)
	}

	unstarred, err := store.GetRSSArticle(ctx, "art:test-softdel:unstarred")
	if err != nil || unstarred == nil {
		t.Fatalf("GetRSSArticle unstarred failed: %v", err)
	}
	if unstarred.ContentHTML == "" {
		t.Fatalf("soft-deleting a feed must keep article content so the feed can be revived")
	}

	starred, err := store.GetRSSArticle(ctx, "art:test-softdel:starred")
	if err != nil || starred == nil {
		t.Fatalf("GetRSSArticle starred failed: %v", err)
	}
	if starred.ContentHTML == "" {
		t.Fatalf("starred article content should be preserved")
	}

	unreadList, err := store.ListRSSArticles(ctx, RSSArticleFilter{UnreadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(unreadList) != 0 {
		t.Fatalf("expected 0 unread articles from soft-deleted feeds, got %d", len(unreadList))
	}

	allList, err := store.ListRSSArticles(ctx, RSSArticleFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(allList) != 0 {
		t.Fatalf("expected 0 general articles from soft-deleted feeds, got %d", len(allList))
	}

	starredList, err := store.ListRSSArticles(ctx, RSSArticleFilter{StarredOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(starredList) != 1 || starredList[0].ID != "art:test-softdel:starred" {
		t.Fatalf("expected 1 starred article preserved, got %+v", starredList)
	}
	starredCnt, err := store.RSSStarredCount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if starredCnt != 0 {
		t.Fatalf("expected RSSStarredCount 0 while the feed is tombstoned, got %d", starredCnt)
	}

	// Re-subscribing the same feed must restore its previously fetched articles
	// with their read/starred state intact.
	feed.UpdatedAt = now + 1000
	if err := store.SubscribeRSSFeed(ctx, feed); err != nil {
		t.Fatalf("SubscribeRSSFeed re-subscribe failed: %v", err)
	}

	restored, err := store.ListRSSArticles(ctx, RSSArticleFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(restored) != 2 {
		t.Fatalf("expected both articles to reappear after re-subscribe, got %d: %+v", len(restored), restored)
	}
	for _, a := range restored {
		if a.ID == "art:test-softdel:unstarred" && !a.Read {
			t.Fatalf("read state must survive feed soft delete and re-subscribe")
		}
	}
	if starredCnt, err := store.RSSStarredCount(ctx); err != nil || starredCnt != 1 {
		t.Fatalf("RSSStarredCount after re-subscribe = %d (%v), want 1", starredCnt, err)
	}
}

func TestRSSStoreRetentionDoesNotRepurgePurgedRows(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	feedID := "feed:test-retention-repurge"
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	body := "<p>body</p>"

	articles := []RSSArticle{
		{
			ID:          "art-repurge-1",
			FeedID:      feedID,
			Title:       "Oldest Read",
			ContentHTML: body,
			PublishedAt: now.Add(-90 * 24 * time.Hour).Format(time.RFC3339),
			Read:        true,
		},
		{
			ID:          "art-repurge-2",
			FeedID:      feedID,
			Title:       "Middle Read",
			ContentHTML: body,
			PublishedAt: now.Add(-80 * 24 * time.Hour).Format(time.RFC3339),
			Read:        true,
		},
		{
			ID:          "art-repurge-3",
			FeedID:      feedID,
			Title:       "Newest Read",
			ContentHTML: body,
			PublishedAt: now.Add(-70 * 24 * time.Hour).Format(time.RFC3339),
			Read:        true,
		},
	}
	if err := store.SaveRSSArticles(ctx, articles); err != nil {
		t.Fatal(err)
	}

	first, err := store.CleanOldRSSArticles(ctx, feedID, 2, 30*24*time.Hour, now)
	if err != nil {
		t.Fatalf("CleanOldRSSArticles failed: %v", err)
	}
	if first != 1 {
		t.Fatalf("expected 1 article purged on the first run, got %d", first)
	}

	second, err := store.CleanOldRSSArticles(ctx, feedID, 2, 30*24*time.Hour, now)
	if err != nil {
		t.Fatalf("CleanOldRSSArticles failed: %v", err)
	}
	if second != 0 {
		t.Fatalf("already purged rows must not be purged or counted again, purged %d", second)
	}

	// A feed that gains another old read article purges exactly one more row.
	if err := store.SaveRSSArticles(ctx, []RSSArticle{{
		ID:          "art-repurge-4",
		FeedID:      feedID,
		Title:       "Another Read",
		ContentHTML: body,
		PublishedAt: now.Add(-60 * 24 * time.Hour).Format(time.RFC3339),
		Read:        true,
	}}); err != nil {
		t.Fatal(err)
	}
	third, err := store.CleanOldRSSArticles(ctx, feedID, 2, 30*24*time.Hour, now)
	if err != nil {
		t.Fatalf("CleanOldRSSArticles failed: %v", err)
	}
	if third != 1 {
		t.Fatalf("expected exactly 1 additional article purged, got %d", third)
	}

	for id, wantPurged := range map[string]bool{
		"art-repurge-1": true,
		"art-repurge-2": true,
		"art-repurge-3": false,
		"art-repurge-4": false,
	} {
		got, err := store.GetRSSArticle(ctx, id)
		if err != nil || got == nil {
			t.Fatalf("GetRSSArticle(%q) failed: %v", id, err)
		}
		if (got.ContentHTML == "") != wantPurged {
			t.Fatalf("article %q purged=%v, want %v", id, got.ContentHTML == "", wantPurged)
		}
		if !got.Read {
			t.Fatalf("article %q lost its read state", id)
		}
	}
}

func TestRSSStoreSaveFeedPreservesCustomTitle(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	feed := RSSFeed{
		ID:          "feed:custom-title-test",
		FeedURL:     "https://example.com/feed.xml",
		Title:       "Original Title",
		CustomTitle: "User Custom Name",
		UpdatedAt:   1000,
	}
	if err := store.SaveRSSFeed(ctx, feed); err != nil {
		t.Fatalf("initial SaveRSSFeed failed: %v", err)
	}

	incoming := RSSFeed{
		ID:        feed.ID,
		FeedURL:   feed.FeedURL,
		Title:     "Updated Site Title",
		UpdatedAt: 2000,
	}
	if err := store.SaveRSSFeed(ctx, incoming); err != nil {
		t.Fatalf("subsequent SaveRSSFeed failed: %v", err)
	}

	saved, err := store.GetRSSFeed(ctx, feed.ID)
	if err != nil || saved == nil {
		t.Fatalf("GetRSSFeed failed: %v", err)
	}
	if saved.CustomTitle != "User Custom Name" {
		t.Fatalf("CustomTitle = %q, want %q", saved.CustomTitle, "User Custom Name")
	}
	if saved.Title != "Updated Site Title" {
		t.Fatalf("Title = %q, want %q", saved.Title, "Updated Site Title")
	}
}

func TestRSSStoreListSummariesOmitsBodiesAndCapsFingerprints(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	feed := RSSFeed{
		ID:        "feed:summary-test",
		FeedURL:   "https://example.com/feed.xml",
		Title:     "Summary Feed",
		UpdatedAt: 1000,
	}
	if err := store.SaveRSSFeed(ctx, feed); err != nil {
		t.Fatal(err)
	}

	blocks := []ArticleBlock{{Type: BlockParagraph, Text: "Full article paragraph", HTML: "<p>Full article paragraph</p>"}}
	if err := store.SaveRSSArticles(ctx, []RSSArticle{
		{
			ID:          "art-with-snippet",
			FeedID:      feed.ID,
			Title:       "Has Snippet",
			Link:        "https://example.com/1",
			PublishedAt: "2026-03-01T12:00:00Z",
			Snippet:     "Explicit summary",
			ContentHTML: "<p>Full HTML body that must not be loaded into summary slice</p>",
			Blocks:      blocks,
			HeroImage:   "https://example.com/1.png",
			UpdatedAt:   1000,
		},
		{
			ID:          "art-empty-snippet",
			FeedID:      feed.ID,
			Title:       "Needs Snippet Fallback",
			Link:        "https://example.com/2",
			PublishedAt: "2026-03-01T11:00:00Z",
			Snippet:     "",
			ContentHTML: "<p>Derived <b>snippet</b> from content HTML.</p>",
			Blocks:      blocks,
			UpdatedAt:   1000,
		},
	}); err != nil {
		t.Fatal(err)
	}

	summaries, err := store.ListRSSArticleSummaries(ctx, RSSArticleFilter{FeedID: feed.ID})
	if err != nil {
		t.Fatalf("ListRSSArticleSummaries failed: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("got %d summaries, want 2", len(summaries))
	}
	if summaries[0].ID != "art-with-snippet" || summaries[0].Snippet != "Explicit summary" {
		t.Fatalf("unexpected first summary: %+v", summaries[0])
	}
	if summaries[0].ContentHTML != "" || len(summaries[0].Blocks) != 0 {
		t.Fatalf("summary must omit ContentHTML and Blocks, got html=%q blocks=%d", summaries[0].ContentHTML, len(summaries[0].Blocks))
	}
	if summaries[1].Snippet != "Derived snippet from content HTML." {
		t.Fatalf("fallback snippet = %q, want %q", summaries[1].Snippet, "Derived snippet from content HTML.")
	}
	if summaries[1].ContentHTML != "" || len(summaries[1].Blocks) != 0 {
		t.Fatalf("fallback summary must omit ContentHTML and Blocks")
	}

	// Full GetRSSArticle must still return the complete body and blocks.
	full, err := store.GetRSSArticle(ctx, "art-with-snippet")
	if err != nil || full == nil {
		t.Fatalf("GetRSSArticle failed: %v", err)
	}
	if full.ContentHTML == "" || len(full.Blocks) != 1 {
		t.Fatalf("GetRSSArticle lost body: html=%q blocks=%d", full.ContentHTML, len(full.Blocks))
	}

	// Fingerprints match saved articles and reflect purged ContentHTML.
	fps, err := store.ListRSSArticleFingerprints(ctx, feed.ID)
	if err != nil {
		t.Fatalf("ListRSSArticleFingerprints failed: %v", err)
	}
	if len(fps) != 2 || !fps["art-with-snippet"].HasBody {
		t.Fatalf("unexpected fingerprints: %+v", fps)
	}
}

// TestRSSFeedFullTextChoice pins the per-feed full-text setting's storage: a
// feed saved without an opinion keeps the default of fetching its articles'
// text, a feed whose reader turned it off keeps that answer across a reload and
// a later save that carries none, and a row an older build wrote - a NULL
// column - reads as the default too.
func TestRSSFeedFullTextChoice(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	now := time.Now().UnixMilli()

	feed := RSSFeed{ID: "feed:text", FeedURL: "https://example.com/rss.xml", Title: "Text", UpdatedAt: now}
	if err := store.SaveRSSFeed(ctx, feed); err != nil {
		t.Fatalf("SaveRSSFeed failed: %v", err)
	}
	stored, err := store.GetRSSFeed(ctx, feed.ID)
	if err != nil || stored == nil {
		t.Fatalf("GetRSSFeed failed: %v", err)
	}
	if stored.FetchFullText != nil {
		t.Fatalf("a feed saved with no choice stored one: %v", *stored.FetchFullText)
	}
	if !stored.AutoFetchFullText() {
		t.Fatal("a feed with no stored choice must fetch its articles' full text")
	}

	off := false
	feed.FetchFullText = &off
	feed.UpdatedAt = now + 1
	if err := store.SaveRSSFeed(ctx, feed); err != nil {
		t.Fatalf("SaveRSSFeed with the setting off failed: %v", err)
	}
	stored, err = store.GetRSSFeed(ctx, feed.ID)
	if err != nil || stored == nil {
		t.Fatalf("GetRSSFeed after saving the setting failed: %v", err)
	}
	if stored.FetchFullText == nil || stored.AutoFetchFullText() {
		t.Fatalf("the feed's choice did not survive: %+v", stored.FetchFullText)
	}

	// A writer that carries no opinion, as a feed refresh's own save does, must
	// not put the reader's choice back to the default.
	opinionless := RSSFeed{ID: feed.ID, FeedURL: feed.FeedURL, Title: "Text renamed", UpdatedAt: now + 2}
	if err := store.SaveRSSFeed(ctx, opinionless); err != nil {
		t.Fatalf("SaveRSSFeed without the setting failed: %v", err)
	}
	stored, err = store.GetRSSFeed(ctx, feed.ID)
	if err != nil || stored == nil {
		t.Fatalf("GetRSSFeed after the opinionless save failed: %v", err)
	}
	if stored.AutoFetchFullText() {
		t.Fatal("a save carrying no choice reset the feed's setting to the default")
	}
	if stored.Title != "Text renamed" {
		t.Fatalf("the opinionless save did not apply its other fields: %q", stored.Title)
	}

	// Rows an earlier build wrote have no value in the column at all.
	if _, err := store.db.Execute(ctx, `UPDATE rss_feeds SET fetch_full_text = NULL WHERE id = ?`, feed.ID); err != nil {
		t.Fatalf("clearing the stored choice failed: %v", err)
	}
	stored, err = store.GetRSSFeed(ctx, feed.ID)
	if err != nil || stored == nil {
		t.Fatalf("GetRSSFeed after clearing the column failed: %v", err)
	}
	if stored.FetchFullText != nil || !stored.AutoFetchFullText() {
		t.Fatalf("a NULL column must read as the default: %+v", stored.FetchFullText)
	}

	feeds, err := store.ListRSSFeeds(ctx, false)
	if err != nil {
		t.Fatalf("ListRSSFeeds failed: %v", err)
	}
	if len(feeds) != 1 || !feeds[0].AutoFetchFullText() {
		t.Fatalf("listed feeds lost the default: %+v", feeds)
	}
}

// TestStoreAddsTheFullTextChoiceToAnOlderFeedTable is the setting's upgrade
// path: a store whose rss_feeds table predates the column gains it when the app
// opens it, its rows keep the values they held, and the missing answer reads as
// the default of fetching the articles' text.
func TestStoreAddsTheFullTextChoiceToAnOlderFeedTable(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old-feeds.sqlite")
	db, err := sqlite.Open(ctx, path, sqlite.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, ddl := range []string{
		`CREATE TABLE rss_feeds (id TEXT PRIMARY KEY, feed_url TEXT NOT NULL, site_url TEXT, title TEXT, custom_title TEXT, group_ids TEXT, avatar_url TEXT, etag TEXT, last_modified TEXT, last_fetched_at INTEGER NOT NULL DEFAULT 0, failure_count INTEGER NOT NULL DEFAULT 0, last_error TEXT, last_error_at INTEGER NOT NULL DEFAULT 0, refresh_interval_mins INTEGER NOT NULL DEFAULT 30, updated_at INTEGER NOT NULL, deleted INTEGER NOT NULL DEFAULT 0, deleted_at INTEGER NOT NULL DEFAULT 0)`,
		`INSERT INTO rss_feeds(id,feed_url,title,updated_at) VALUES('feed:old','https://example.com/rss.xml','Old Feed',7)`,
	} {
		if _, err := db.Execute(ctx, ddl); err != nil {
			t.Fatalf("%s: %v", ddl, err)
		}
	}
	db.Close()

	store, err := OpenStore(ctx, path)
	if err != nil {
		t.Fatalf("open the older store: %v", err)
	}
	defer store.Close()

	feed, err := store.GetRSSFeed(ctx, "feed:old")
	if err != nil || feed == nil {
		t.Fatalf("read the older store's feed: %v", err)
	}
	if feed.Title != "Old Feed" || feed.FeedURL != "https://example.com/rss.xml" {
		t.Fatalf("the upgrade lost the feed's fields: %+v", feed)
	}
	if !feed.AutoFetchFullText() {
		t.Fatal("a feed written before the setting must fetch its articles' text")
	}

	off := false
	feed.FetchFullText = &off
	feed.UpdatedAt = time.Now().UnixMilli()
	if err := store.SaveRSSFeed(ctx, *feed); err != nil {
		t.Fatalf("save the new setting to the upgraded store: %v", err)
	}
	feed, err = store.GetRSSFeed(ctx, "feed:old")
	if err != nil || feed == nil {
		t.Fatalf("read the upgraded store's feed back: %v", err)
	}
	if feed.AutoFetchFullText() {
		t.Fatal("the upgraded store did not keep the setting")
	}
}

// TestStoreKeepsTheFeedsOwnBodyBesideTheFullText covers what the reader's
// original/full-text toggle reads: the body the feed delivered is kept when
// extraction replaces it, a later refresh never drops it, and the list queries
// leave it in the store so no article list carries every feed's HTML.
func TestStoreKeepsTheFeedsOwnBodyBesideTheFullText(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	now := time.Now().UnixMilli()
	feed := RSSFeed{ID: "feed:original", FeedURL: "https://example.com/rss", Title: "A Feed", UpdatedAt: now}
	if err := store.SaveRSSFeed(ctx, feed); err != nil {
		t.Fatal(err)
	}

	// The feed delivered its own body.
	article := RSSArticle{
		ID: "art:original:1", FeedID: feed.ID, Title: "A Post", Link: "https://example.com/a",
		PublishedAt: "2026-11-23T08:00:00Z", Snippet: "the feed's opening words",
		ContentHTML: "<p>the feed's own body</p>", UpdatedAt: now,
	}
	if err := store.SaveRSSArticles(ctx, []RSSArticle{article}); err != nil {
		t.Fatal(err)
	}

	// Extraction replaced the body and kept the feed's own beside it.
	article.ContentHTML = "<p>the extracted full text</p>"
	article.FeedContentHTML = "<p>the feed's own body</p>"
	article.IsFullText = true
	if err := store.SaveRSSArticles(ctx, []RSSArticle{article}); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetRSSArticle(ctx, article.ID)
	if err != nil || got == nil {
		t.Fatalf("read the article back: %v %v", got, err)
	}
	if got.ContentHTML != "<p>the extracted full text</p>" {
		t.Fatalf("the article's body = %q, want the extracted text", got.ContentHTML)
	}
	if got.FeedContentHTML != "<p>the feed's own body</p>" {
		t.Fatalf("the feed's own body = %q, want it kept beside the full text", got.FeedContentHTML)
	}

	// A later refresh writes the article without a feed body and must not lose
	// the one the store already holds.
	article.FeedContentHTML = ""
	article.UpdatedAt = now + 1
	if err := store.SaveRSSArticles(ctx, []RSSArticle{article}); err != nil {
		t.Fatal(err)
	}
	if got, err = store.GetRSSArticle(ctx, article.ID); err != nil || got == nil {
		t.Fatalf("read the article back after a refresh: %v %v", got, err)
	}
	if got.FeedContentHTML != "<p>the feed's own body</p>" {
		t.Fatalf("a refresh dropped the feed's own body: %q", got.FeedContentHTML)
	}

	// The list queries stay lean: the article list never carries the body, so
	// the reader loads the one version it shows on demand.
	summaries, err := store.ListRSSArticleSummaries(ctx, RSSArticleFilter{})
	if err != nil || len(summaries) != 1 {
		t.Fatalf("list the summaries: %v %v", summaries, err)
	}
	if summaries[0].FeedContentHTML != "" {
		t.Fatalf("the summary list carried the feed's own body: %q", summaries[0].FeedContentHTML)
	}
	articles, err := store.ListRSSArticles(ctx, RSSArticleFilter{})
	if err != nil || len(articles) != 1 {
		t.Fatalf("list the articles: %v %v", articles, err)
	}
	if articles[0].FeedContentHTML != "" {
		t.Fatalf("the article list carried the feed's own body: %q", articles[0].FeedContentHTML)
	}
}

// TestStoreAddsTheFeedsOwnBodyToAnOlderArticleTable is the column's upgrade
// path: articles a store wrote before it existed keep every field they held,
// read with no original body, and gain one the next time they are saved.
func TestStoreAddsTheFeedsOwnBodyToAnOlderArticleTable(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old-articles.sqlite")
	db, err := sqlite.Open(ctx, path, sqlite.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, ddl := range []string{
		`CREATE TABLE rss_feeds (id TEXT PRIMARY KEY, feed_url TEXT NOT NULL, site_url TEXT, title TEXT, custom_title TEXT, group_ids TEXT, avatar_url TEXT, etag TEXT, last_modified TEXT, last_fetched_at INTEGER NOT NULL DEFAULT 0, failure_count INTEGER NOT NULL DEFAULT 0, last_error TEXT, last_error_at INTEGER NOT NULL DEFAULT 0, refresh_interval_mins INTEGER NOT NULL DEFAULT 30, updated_at INTEGER NOT NULL, deleted INTEGER NOT NULL DEFAULT 0, deleted_at INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE rss_articles (id TEXT PRIMARY KEY, feed_id TEXT NOT NULL, title TEXT NOT NULL, link TEXT, guid TEXT, author TEXT, published_at TEXT, snippet TEXT, content_html TEXT, is_full_text INTEGER NOT NULL DEFAULT 0, hero_image TEXT, blocks_json TEXT, read INTEGER NOT NULL DEFAULT 0, read_at INTEGER NOT NULL DEFAULT 0, starred INTEGER NOT NULL DEFAULT 0, starred_at INTEGER NOT NULL DEFAULT 0, updated_at INTEGER NOT NULL)`,
		`INSERT INTO rss_feeds(id,feed_url,title,updated_at) VALUES('feed:old','https://example.com/rss.xml','Old Feed',7)`,
		`INSERT INTO rss_articles(id,feed_id,title,link,published_at,snippet,content_html,is_full_text,read,updated_at) VALUES('art:old','feed:old','An Old Post','https://example.com/old','2026-11-23T08:00:00Z','an old summary','<p>an old body</p>',1,1,7)`,
	} {
		if _, err := db.Execute(ctx, ddl); err != nil {
			t.Fatalf("%s: %v", ddl, err)
		}
	}
	db.Close()

	store, err := OpenStore(ctx, path)
	if err != nil {
		t.Fatalf("open an older store: %v", err)
	}
	defer store.Close()

	got, err := store.GetRSSArticle(ctx, "art:old")
	if err != nil || got == nil {
		t.Fatalf("read the older article: %v %v", got, err)
	}
	if got.Title != "An Old Post" || got.ContentHTML != "<p>an old body</p>" || !got.IsFullText || !got.Read {
		t.Fatalf("the upgrade lost the article's fields: %+v", got)
	}
	if got.FeedContentHTML != "" {
		t.Fatalf("an article written before the column reads %q as its original, want none", got.FeedContentHTML)
	}

	// The next save writes the original body the article gains.
	got.FeedContentHTML = "<p>the feed's own body</p>"
	got.UpdatedAt = time.Now().UnixMilli()
	if err := store.SaveRSSArticles(ctx, []RSSArticle{*got}); err != nil {
		t.Fatalf("save the upgraded article: %v", err)
	}
	if again, err := store.GetRSSArticle(ctx, "art:old"); err != nil || again == nil {
		t.Fatalf("read the upgraded article back: %v %v", again, err)
	} else if again.FeedContentHTML != "<p>the feed's own body</p>" {
		t.Fatalf("the upgraded store did not keep the original body: %q", again.FeedContentHTML)
	}
}
