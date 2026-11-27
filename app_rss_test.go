package main

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/simon/youtube-reader/core"
)

func TestAppRSSGroupCRUDAndOrdering(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()

	if err := a.ReloadRSS(); err != nil {
		t.Fatalf("ReloadRSS failed: %v", err)
	}

	// 1. Create groups
	g1, err := a.CreateRSSGroup("Tech")
	if err != nil {
		t.Fatalf("CreateRSSGroup Tech failed: %v", err)
	}
	g2, err := a.CreateRSSGroup("News")
	if err != nil {
		t.Fatalf("CreateRSSGroup News failed: %v", err)
	}

	if len(a.RSSGroups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(a.RSSGroups))
	}
	if a.RSSGroups[0].Name != "Tech" || a.RSSGroups[1].Name != "News" {
		t.Fatalf("groups order = [%s, %s], want [Tech, News]", a.RSSGroups[0].Name, a.RSSGroups[1].Name)
	}

	// 2. Reorder groups: put News first
	if err := a.SetRSSGroupOrder([]string{g2.ID, g1.ID}); err != nil {
		t.Fatalf("SetRSSGroupOrder failed: %v", err)
	}
	if a.RSSGroups[0].Name != "News" || a.RSSGroups[1].Name != "Tech" {
		t.Fatalf("reordered groups = [%s, %s], want [News, Tech]", a.RSSGroups[0].Name, a.RSSGroups[1].Name)
	}

	// Check reload preserves order
	if err := a.ReloadRSS(); err != nil {
		t.Fatalf("ReloadRSS failed: %v", err)
	}
	if a.RSSGroups[0].Name != "News" || a.RSSGroups[1].Name != "Tech" {
		t.Fatalf("reloaded groups = [%s, %s], want [News, Tech]", a.RSSGroups[0].Name, a.RSSGroups[1].Name)
	}

	// 3. Rename group
	if err := a.RenameRSSGroup(g1.ID, "Technology"); err != nil {
		t.Fatalf("RenameRSSGroup failed: %v", err)
	}
	found := a.RSSGroupByID(g1.ID)
	if found.Name != "Technology" {
		t.Fatalf("renamed group name = %q, want Technology", found.Name)
	}

	// 4. Delete group
	if err := a.DeleteRSSGroup(g2.ID); err != nil {
		t.Fatalf("DeleteRSSGroup failed: %v", err)
	}
	if len(a.RSSGroups) != 1 || a.RSSGroups[0].ID != g1.ID {
		t.Fatalf("expected 1 remaining group %s, got %d groups", g1.ID, len(a.RSSGroups))
	}
}

func TestAppRSSFeedSubscriptionAndMoving(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()

	g1, err := a.CreateRSSGroup("Blogs")
	if err != nil {
		t.Fatalf("CreateRSSGroup failed: %v", err)
	}
	g2, err := a.CreateRSSGroup("Media")
	if err != nil {
		t.Fatalf("CreateRSSGroup failed: %v", err)
	}

	feed1 := core.RSSFeed{
		ID:      core.FeedID("https://example.com/feed1.xml"),
		FeedURL: "https://example.com/feed1.xml",
		Title:   "Example 1",
	}
	feed2 := core.RSSFeed{
		ID:      core.FeedID("https://example.com/feed2.xml"),
		FeedURL: "https://example.com/feed2.xml",
		Title:   "Example 2",
	}

	// Subscribe feed1 into g1
	if err := a.SubscribeRSSFeed(feed1, g1.ID); err != nil {
		t.Fatalf("SubscribeRSSFeed feed1 failed: %v", err)
	}
	// Subscribe feed2 into ungrouped
	if err := a.SubscribeRSSFeed(feed2, ""); err != nil {
		t.Fatalf("SubscribeRSSFeed feed2 failed: %v", err)
	}

	if len(a.RSSFeeds) != 2 {
		t.Fatalf("expected 2 feeds, got %d", len(a.RSSFeeds))
	}
	if !a.HasRSSFeed(feed1.ID) || !a.HasRSSFeed(feed2.ID) {
		t.Fatal("expected both feeds to be present in app")
	}

	// Check group membership
	g1Reloaded := a.RSSGroupByID(g1.ID)
	if len(g1Reloaded.FeedIDs) != 1 || g1Reloaded.FeedIDs[0] != feed1.ID {
		t.Fatalf("g1 FeedIDs = %v, want [%s]", g1Reloaded.FeedIDs, feed1.ID)
	}

	// Rename feed1
	if err := a.RenameRSSFeed(feed1.ID, "Custom Example 1"); err != nil {
		t.Fatalf("RenameRSSFeed failed: %v", err)
	}
	if a.RSSFeedByID(feed1.ID).DisplayName() != "Custom Example 1" {
		t.Fatalf("feed1 display name = %q, want Custom Example 1", a.RSSFeedByID(feed1.ID).DisplayName())
	}

	// Move feed1 from g1 to g2
	if err := a.MoveRSSFeedToGroup(feed1.ID, g2.ID); err != nil {
		t.Fatalf("MoveRSSFeedToGroup to g2 failed: %v", err)
	}
	g1After := a.RSSGroupByID(g1.ID)
	g2After := a.RSSGroupByID(g2.ID)
	if len(g1After.FeedIDs) != 0 {
		t.Fatalf("expected g1 to be empty, got %v", g1After.FeedIDs)
	}
	if len(g2After.FeedIDs) != 1 || g2After.FeedIDs[0] != feed1.ID {
		t.Fatalf("expected g2 to hold feed1, got %v", g2After.FeedIDs)
	}

	// Move feed1 to ungrouped
	if err := a.MoveRSSFeedToGroup(feed1.ID, ""); err != nil {
		t.Fatalf("MoveRSSFeedToGroup to ungrouped failed: %v", err)
	}
	g2Empty := a.RSSGroupByID(g2.ID)
	if len(g2Empty.FeedIDs) != 0 {
		t.Fatalf("expected g2 to be empty, got %v", g2Empty.FeedIDs)
	}
	if len(a.RSSFeedByID(feed1.ID).GroupIDs) != 0 {
		t.Fatalf("expected feed1 to have empty GroupIDs, got %v", a.RSSFeedByID(feed1.ID).GroupIDs)
	}

	// Delete feed2
	if err := a.DeleteRSSFeed(feed2.ID); err != nil {
		t.Fatalf("DeleteRSSFeed failed: %v", err)
	}
	if a.HasRSSFeed(feed2.ID) {
		t.Fatal("feed2 should have been deleted")
	}

	// 5. Duplicate subscription with empty CustomTitle preserves custom title and group hygiene
	resubFeed := core.RSSFeed{
		ID:      feed1.ID,
		FeedURL: feed1.FeedURL,
		Title:   "Remote Title Changed",
	}
	if err := a.SubscribeRSSFeed(resubFeed, g1.ID); err != nil {
		t.Fatalf("re-subscribing feed1 to g1 failed: %v", err)
	}
	if a.RSSFeedByID(feed1.ID).DisplayName() != "Custom Example 1" {
		t.Fatalf("expected custom title preserved, got %q", a.RSSFeedByID(feed1.ID).DisplayName())
	}
	g1Final := a.RSSGroupByID(g1.ID)
	g2Final := a.RSSGroupByID(g2.ID)
	if !slices.Contains(g1Final.FeedIDs, feed1.ID) {
		t.Fatalf("expected g1 to contain feed1, got %v", g1Final.FeedIDs)
	}
	if slices.Contains(g2Final.FeedIDs, feed1.ID) {
		t.Fatalf("expected g2 to NOT contain feed1, got %v", g2Final.FeedIDs)
	}
}

func TestAppRSSFailureStateAndUnreadCounts(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()

	ctx := context.Background()

	feed := core.RSSFeed{
		ID:           core.FeedID("https://example.com/feed.xml"),
		FeedURL:      "https://example.com/feed.xml",
		Title:        "Failing Feed",
		FailureCount: 2,
		LastError:    "HTTP 404 Not Found",
		LastErrorAt:  time.Now().UnixMilli(),
	}
	if err := a.SubscribeRSSFeed(feed, ""); err != nil {
		t.Fatalf("SubscribeRSSFeed failed: %v", err)
	}

	// Verify failure count and error loaded
	loaded := a.RSSFeedByID(feed.ID)
	if loaded.FailureCount != 2 || loaded.LastError != "HTTP 404 Not Found" {
		t.Fatalf("loaded failure = %d, error = %q; want 2 and HTTP 404 Not Found", loaded.FailureCount, loaded.LastError)
	}

	// Add unread and read articles
	art1 := core.RSSArticle{
		ID:          "art-1",
		FeedID:      feed.ID,
		Title:       "Article 1",
		PublishedAt: "2026-01-01T00:00:00Z",
		Read:        false,
		Starred:     false,
	}
	art2 := core.RSSArticle{
		ID:          "art-2",
		FeedID:      feed.ID,
		Title:       "Article 2",
		PublishedAt: "2026-01-02T00:00:00Z",
		Read:        false,
		Starred:     true,
	}
	art3 := core.RSSArticle{
		ID:          "art-3",
		FeedID:      feed.ID,
		Title:       "Article 3",
		PublishedAt: "2026-01-03T00:00:00Z",
		Read:        true,
		Starred:     false,
	}
	if err := a.Store.SaveRSSArticles(ctx, []core.RSSArticle{art1, art2, art3}); err != nil {
		t.Fatalf("SaveRSSArticles failed: %v", err)
	}

	if err := a.ReloadRSS(); err != nil {
		t.Fatalf("ReloadRSS failed: %v", err)
	}

	if a.RSSTotalUnread != 2 {
		t.Fatalf("total unread = %d, want 2", a.RSSTotalUnread)
	}
	if a.RSSStarredTotal != 1 {
		t.Fatalf("starred total = %d, want 1", a.RSSStarredTotal)
	}
	if a.RSSUnreadMap[feed.ID] != 2 {
		t.Fatalf("feed unread = %d, want 2", a.RSSUnreadMap[feed.ID])
	}

	// Mark art1 read: count drops to 1
	if err := a.Store.MarkRSSArticleRead(ctx, a.DeviceID, art1.ID, true, time.Now().UnixMilli()); err != nil {
		t.Fatalf("MarkRSSArticleRead failed: %v", err)
	}
	if err := a.ReloadRSS(); err != nil {
		t.Fatalf("ReloadRSS failed: %v", err)
	}
	if a.RSSTotalUnread != 1 {
		t.Fatalf("total unread after mark read = %d, want 1", a.RSSTotalUnread)
	}

	// Simulate successful fetch: clears failure count and error
	loaded.FailureCount = 0
	loaded.LastError = ""
	loaded.LastFetchedAt = time.Now().UnixMilli()
	_ = a.Store.SaveRSSFeed(ctx, loaded)
	_ = a.ReloadRSS()

	cleared := a.RSSFeedByID(feed.ID)
	if cleared.FailureCount != 0 || cleared.LastError != "" {
		t.Fatalf("expected cleared failure state, got count %d, error %q", cleared.FailureCount, cleared.LastError)
	}
}

func TestAppReloadRSSLoadsSummariesAndSelectsFullArticleOnDemand(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()

	ctx := context.Background()
	feed := core.RSSFeed{
		ID:        "feed:ondemand",
		FeedURL:   "https://example.com/feed.xml",
		Title:     "On-Demand Feed",
		UpdatedAt: 1000,
	}
	if err := a.Store.SaveRSSFeed(ctx, feed); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.SaveRSSArticles(ctx, []core.RSSArticle{{
		ID:          "art:ondemand:1",
		FeedID:      feed.ID,
		Title:       "Heavy Article",
		Link:        "https://example.com/heavy",
		PublishedAt: "2026-03-01T12:00:00Z",
		Snippet:     "Short summary",
		ContentHTML: "<p>Full heavy article body</p>",
		Blocks:      []core.ArticleBlock{{Type: core.BlockParagraph, Text: "Full heavy article body"}},
		UpdatedAt:   1000,
	}}); err != nil {
		t.Fatal(err)
	}

	if err := a.ReloadRSS(); err != nil {
		t.Fatalf("ReloadRSS failed: %v", err)
	}
	if len(a.RSSArticles) != 1 {
		t.Fatalf("got %d articles, want 1", len(a.RSSArticles))
	}
	if a.RSSArticles[0].ContentHTML != "" || len(a.RSSArticles[0].Blocks) != 0 {
		t.Fatalf("App.RSSArticles must hold summaries only, got html=%q blocks=%d", a.RSSArticles[0].ContentHTML, len(a.RSSArticles[0].Blocks))
	}

	a.SelectRSSArticle("art:ondemand:1")
	selected := a.SelectedRSSArticle()
	if selected == nil {
		t.Fatal("SelectedRSSArticle returned nil")
	}
	if selected.ContentHTML != "<p>Full heavy article body</p>" || len(selected.Blocks) != 1 {
		t.Fatalf("SelectedRSSArticle must load body on demand, got html=%q blocks=%d", selected.ContentHTML, len(selected.Blocks))
	}

	if err := a.MarkRSSArticleRead("art:ondemand:1", true); err != nil {
		t.Fatalf("MarkRSSArticleRead failed: %v", err)
	}
	if updated := a.SelectedRSSArticle(); updated == nil || !updated.Read {
		t.Fatalf("SelectedRSSArticle did not reflect MarkRSSArticleRead")
	}
}
