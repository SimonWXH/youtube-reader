package core

import (
	"context"
	"path/filepath"
	"testing"
	"time"
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
	if err := store.MarkRSSArticleRead(ctx, artID, true, readTime); err != nil {
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

func TestRSSStoreRetentionNeverDeletesUnreadOrStarred(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	feedID := "feed:test-retention"
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	articles := []RSSArticle{
		// 1. Unread, old (published 60 days ago) -> MUST NOT be deleted
		{
			ID:          "art-unread-old",
			FeedID:      feedID,
			Title:       "Unread Old Article",
			PublishedAt: now.Add(-60 * 24 * time.Hour).Format(time.RFC3339),
			Read:        false,
			Starred:     false,
		},
		// 2. Starred, old (published 60 days ago, read) -> MUST NOT be deleted
		{
			ID:          "art-starred-old",
			FeedID:      feedID,
			Title:       "Starred Old Article",
			PublishedAt: now.Add(-60 * 24 * time.Hour).Format(time.RFC3339),
			Read:        true,
			Starred:     true,
		},
		// 3. Read, old (published 60 days ago, not starred) -> MUST BE DELETED
		{
			ID:          "art-read-old",
			FeedID:      feedID,
			Title:       "Read Old Article",
			PublishedAt: now.Add(-60 * 24 * time.Hour).Format(time.RFC3339),
			Read:        true,
			Starred:     false,
		},
		// 4. Read, recent (published 2 days ago, not starred) -> KEPT if within limit
		{
			ID:          "art-read-recent",
			FeedID:      feedID,
			Title:       "Read Recent Article",
			PublishedAt: now.Add(-2 * 24 * time.Hour).Format(time.RFC3339),
			Read:        true,
			Starred:     false,
		},
		// 5. Unread, recent (published 2 days ago) -> MUST NOT be deleted
		{
			ID:          "art-unread-recent",
			FeedID:      feedID,
			Title:       "Unread Recent Article",
			PublishedAt: now.Add(-2 * 24 * time.Hour).Format(time.RFC3339),
			Read:        false,
			Starred:     false,
		},
	}

	if err := store.SaveRSSArticles(ctx, articles); err != nil {
		t.Fatalf("SaveRSSArticles failed: %v", err)
	}

	// Retention policy: max 1 read article, older than 30 days
	deletedCount, err := store.CleanOldRSSArticles(ctx, feedID, 1, 30*24*time.Hour, now)
	if err != nil {
		t.Fatalf("CleanOldRSSArticles failed: %v", err)
	}
	if deletedCount != 1 {
		t.Fatalf("expected exactly 1 article deleted, got %d", deletedCount)
	}

	// Verify each article's survival
	for _, id := range []string{"art-unread-old", "art-starred-old", "art-read-recent", "art-unread-recent"} {
		a, err := store.GetRSSArticle(ctx, id)
		if err != nil || a == nil {
			t.Fatalf("protected article %q was mistakenly deleted!", id)
		}
	}

	// Verify only art-read-old was deleted
	deletedArt, _ := store.GetRSSArticle(ctx, "art-read-old")
	if deletedArt != nil {
		t.Fatalf("expected art-read-old to be deleted, but still exists")
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

	if err := store.MarkRSSArticleRead(ctx, "art:test-softdel:unstarred", true, now); err != nil {
		t.Fatal(err)
	}

	if err := store.DeleteRSSFeed(ctx, feed.ID, false); err != nil {
		t.Fatalf("DeleteRSSFeed soft failed: %v", err)
	}

	unstarred, err := store.GetRSSArticle(ctx, "art:test-softdel:unstarred")
	if err != nil || unstarred == nil {
		t.Fatalf("GetRSSArticle unstarred failed: %v", err)
	}
	if unstarred.Deleted {
		t.Fatalf("soft-deleting a feed must not tombstone its articles, got deleted=true")
	}

	starred, err := store.GetRSSArticle(ctx, "art:test-softdel:starred")
	if err != nil || starred == nil {
		t.Fatalf("GetRSSArticle starred failed: %v", err)
	}
	if starred.Deleted {
		t.Fatalf("starred article should NOT be deleted, got deleted=true")
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

	// Re-subscribing the same feed must restore its previously fetched articles
	// with their read/starred state intact.
	feed.Deleted = false
	feed.DeletedAt = 0
	feed.UpdatedAt = now + 1000
	if err := store.SaveRSSFeed(ctx, feed); err != nil {
		t.Fatalf("SaveRSSFeed re-subscribe failed: %v", err)
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
}
