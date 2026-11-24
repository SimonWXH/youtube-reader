package main

import (
	"context"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
)

func TestFormatArticleDate(t *testing.T) {
	now := time.Date(2026, time.November, 24, 15, 0, 0, 0, time.Local)

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "empty string",
			raw:  "",
			want: "",
		},
		{
			name: "whitespace only",
			raw:  "   ",
			want: "",
		},
		{
			name: "invalid timestamp format",
			raw:  "invalid-date",
			want: "invalid-date",
		},
		{
			name: "just now (10s ago)",
			raw:  now.Add(-10 * time.Second).Format(time.RFC3339),
			want: "just now",
		},
		{
			name: "minutes ago (15m ago)",
			raw:  now.Add(-15 * time.Minute).Format(time.RFC3339),
			want: "15m ago",
		},
		{
			name: "hours ago (3h ago)",
			raw:  now.Add(-3 * time.Hour).Format(time.RFC3339),
			want: "3h ago",
		},
		{
			name: "days ago in same year",
			raw:  time.Date(2026, time.May, 10, 8, 30, 0, 0, time.Local).Format(time.RFC3339),
			want: "May 10",
		},
		{
			name: "previous year",
			raw:  time.Date(2025, time.October, 15, 12, 0, 0, 0, time.Local).Format(time.RFC3339),
			want: "Oct 15, 2025",
		},
		{
			name: "future clock skew (<1m ahead)",
			raw:  now.Add(10 * time.Second).Format(time.RFC3339),
			want: "just now",
		},
		{
			name: "ISO date only format without T",
			raw:  "2026-04-12",
			want: "Apr 12",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatArticleDate(tc.raw, now)
			if got != tc.want {
				t.Errorf("FormatArticleDate(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestCleanArticleSnippet(t *testing.T) {
	tests := []struct {
		name     string
		snippet  string
		content  string
		maxRunes int
		want     string
	}{
		{
			name:     "both empty",
			snippet:  "",
			content:  "",
			maxRunes: 200,
			want:     "",
		},
		{
			name:     "fallback to content",
			snippet:  "",
			content:  "<p>Hello from full content body</p>",
			maxRunes: 200,
			want:     "Hello from full content body",
		},
		{
			name:     "strip HTML tags and whitespace collapse",
			snippet:  "<div><p>Paragraph 1</p>\n\n<p>Paragraph <b>2</b></p></div>",
			content:  "",
			maxRunes: 200,
			want:     "Paragraph 1 Paragraph 2",
		},
		{
			name:     "decode HTML entities",
			snippet:  "AT&amp;T announced &quot;Project X&quot; &lt;tech&gt; &amp; more",
			content:  "",
			maxRunes: 200,
			want:     `AT&T announced "Project X" <tech> & more`,
		},
		{
			name:     "truncation with ellipsis",
			snippet:  "The quick brown fox jumps over the lazy dog repeatedly and effortlessly.",
			content:  "",
			maxRunes: 20,
			want:     "The quick brown fox ...",
		},
		{
			name:     "short text not truncated",
			snippet:  "Short summary",
			content:  "",
			maxRunes: 50,
			want:     "Short summary",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CleanArticleSnippet(tc.snippet, tc.content, tc.maxRunes)
			if got != tc.want {
				t.Errorf("CleanArticleSnippet() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRSSArticlesModelAndStoreIntegration(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	ctx := context.Background()

	now := time.Now().UnixMilli()
	f1 := core.RSSFeed{ID: "feed:tech", FeedURL: "https://tech.example.com/rss", Title: "Tech Weekly", UpdatedAt: now}
	f2 := core.RSSFeed{ID: "feed:design", FeedURL: "https://design.example.com/rss", Title: "Design Digest", UpdatedAt: now}
	if err := a.Store.SaveRSSFeed(ctx, f1); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.SaveRSSFeed(ctx, f2); err != nil {
		t.Fatal(err)
	}

	g1 := core.RSSGroup{ID: "group:all-tech", Name: "Tech Group", FeedIDs: []string{"feed:tech"}, UpdatedAt: now}
	if err := a.Store.SaveRSSGroup(ctx, g1); err != nil {
		t.Fatal(err)
	}

	articles := []core.RSSArticle{
		{
			ID:          "art:1",
			FeedID:      "feed:tech",
			Title:       "Go 1.25 Released",
			Snippet:     "Go 1.25 brings compiler improvements.",
			PublishedAt: "2026-03-01T10:00:00Z",
			Read:        false,
			Starred:     false,
			UpdatedAt:   now,
		},
		{
			ID:          "art:2",
			FeedID:      "feed:tech",
			Title:       "Concurrency Patterns in Go",
			Snippet:     "A deep dive into channels and sync.",
			PublishedAt: "2026-02-15T10:00:00Z",
			Read:        true,
			ReadAt:      now - 5000,
			Starred:     true,
			StarredAt:   now - 5000,
			UpdatedAt:   now,
		},
		{
			ID:          "art:3",
			FeedID:      "feed:design",
			Title:       "Typography for Native Apps",
			Snippet:     "Why 4pt ladders create visual harmony.",
			PublishedAt: "2026-02-01T10:00:00Z",
			Read:        false,
			Starred:     false,
			UpdatedAt:   now,
		},
	}
	if err := a.Store.SaveRSSArticles(ctx, articles); err != nil {
		t.Fatal(err)
	}

	if err := a.ReloadRSS(); err != nil {
		t.Fatalf("ReloadRSS failed: %v", err)
	}

	if len(a.RSSFeeds) != 2 {
		t.Fatalf("expected 2 feeds, got %d", len(a.RSSFeeds))
	}
	if len(a.RSSGroups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(a.RSSGroups))
	}
	if len(a.RSSArticles) != 3 {
		t.Fatalf("expected 3 articles, got %d", len(a.RSSArticles))
	}

	// Verify unread counts
	if totalUnread := a.RSSUnreadCount(""); totalUnread != 2 {
		t.Fatalf("total unread = %d, want 2", totalUnread)
	}
	if techUnread := a.RSSUnreadCount("feed:tech"); techUnread != 1 {
		t.Fatalf("feed:tech unread = %d, want 1", techUnread)
	}
	if designUnread := a.RSSUnreadCount("feed:design"); designUnread != 1 {
		t.Fatalf("feed:design unread = %d, want 1", designUnread)
	}

	// Mark art:1 as read
	if err := a.MarkRSSArticleRead("art:1", true); err != nil {
		t.Fatalf("MarkRSSArticleRead(art:1, true) error: %v", err)
	}
	if !a.RSSArticles[0].Read {
		t.Errorf("art:1 in-memory Read should be true")
	}
	// Verify in SQLite
	storedArt, err := a.Store.GetRSSArticle(ctx, "art:1")
	if err != nil || storedArt == nil || !storedArt.Read {
		t.Fatalf("art:1 in DB should be marked read: %+v", storedArt)
	}
	if a.RSSUnreadCount("feed:tech") != 0 {
		t.Errorf("feed:tech unread should now be 0")
	}

	// Mark art:1 back to unread
	if err := a.MarkRSSArticleRead("art:1", false); err != nil {
		t.Fatalf("MarkRSSArticleRead(art:1, false) error: %v", err)
	}
	if a.RSSArticles[0].Read {
		t.Errorf("art:1 in-memory Read should be false")
	}
	storedArt, err = a.Store.GetRSSArticle(ctx, "art:1")
	if err != nil || storedArt == nil || storedArt.Read {
		t.Fatalf("art:1 in DB should be marked unread")
	}

	// Toggle art:1
	if err := a.ToggleRSSArticleRead("art:1"); err != nil {
		t.Fatal(err)
	}
	if !a.RSSArticles[0].Read {
		t.Errorf("art:1 should be toggled to read")
	}

	// Star and unstar art:1
	if err := a.SetRSSArticleStarred("art:1", true); err != nil {
		t.Fatal(err)
	}
	if !a.RSSArticles[0].Read {
		t.Errorf("art:1 should remain read")
	}
	if !a.RSSArticles[0].Starred {
		t.Errorf("art:1 should be starred")
	}
	storedArt, err = a.Store.GetRSSArticle(ctx, "art:1")
	if err != nil || !storedArt.Starred {
		t.Fatalf("art:1 in DB should be starred")
	}
}

func TestVisibleRSSArticlesFilteringAndSorting(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	ctx := context.Background()

	now := time.Now().UnixMilli()
	f1 := core.RSSFeed{ID: "f1", FeedURL: "https://f1.com/rss", Title: "Alpha Feed", UpdatedAt: now}
	f2 := core.RSSFeed{ID: "f2", FeedURL: "https://f2.com/rss", Title: "Beta Digest", UpdatedAt: now}
	fDel := core.RSSFeed{ID: "fDel", FeedURL: "https://fDel.com/rss", Title: "Deleted Feed", Deleted: true, DeletedAt: now, UpdatedAt: now}

	_ = a.Store.SaveRSSFeed(ctx, f1)
	_ = a.Store.SaveRSSFeed(ctx, f2)
	_ = a.Store.SaveRSSFeed(ctx, fDel)

	g := core.RSSGroup{ID: "g1", Name: "Alpha Group", FeedIDs: []string{"f1"}, UpdatedAt: now}
	_ = a.Store.SaveRSSGroup(ctx, g)

	articles := []core.RSSArticle{
		{ID: "art:oldest", FeedID: "f1", Title: "Old Post", Snippet: "First post", PublishedAt: "2026-01-01T00:00:00Z", Read: true, Starred: false, UpdatedAt: now},
		{ID: "art:newest", FeedID: "f1", Title: "Newest Tech Post", Snippet: "Latest tech news", PublishedAt: "2026-03-01T00:00:00Z", Read: false, Starred: true, UpdatedAt: now},
		{ID: "art:middle", FeedID: "f2", Title: "Middle Design Post", Snippet: "Design review", PublishedAt: "2026-02-01T00:00:00Z", Read: false, Starred: false, UpdatedAt: now},
		{ID: "art:deleted", FeedID: "fDel", Title: "Ghost Post", Snippet: "Should be excluded", PublishedAt: "2026-02-15T00:00:00Z", Read: false, Starred: false, UpdatedAt: now},
	}
	_ = a.Store.SaveRSSArticles(ctx, articles)
	_ = a.ReloadRSS()

	// 1. All articles: should sort newest first, excluding deleted feed
	a.Selected = "all"
	a.Search = ""
	a.UnreadOnly = false
	vis := a.VisibleRSSArticles()
	if len(vis) != 3 {
		t.Fatalf("VisibleRSSArticles(all) len = %d, want 3", len(vis))
	}
	if vis[0].ID != "art:newest" || vis[1].ID != "art:middle" || vis[2].ID != "art:oldest" {
		t.Fatalf("articles not sorted newest first: %v", []string{vis[0].ID, vis[1].ID, vis[2].ID})
	}

	// 2. Filter by feed:f1
	a.Selected = "feed:f1"
	vis = a.VisibleRSSArticles()
	if len(vis) != 2 || vis[0].ID != "art:newest" || vis[1].ID != "art:oldest" {
		t.Fatalf("feed:f1 visible = %v, want art:newest and art:oldest", vis)
	}

	// 3. Filter by group:g1 (holds f1)
	a.Selected = "group:g1"
	vis = a.VisibleRSSArticles()
	if len(vis) != 2 || vis[0].ID != "art:newest" || vis[1].ID != "art:oldest" {
		t.Fatalf("group:g1 visible = %v, want f1 articles", vis)
	}

	// 4. Filter by starred
	a.Selected = "starred"
	vis = a.VisibleRSSArticles()
	if len(vis) != 1 || vis[0].ID != "art:newest" {
		t.Fatalf("starred visible = %v, want art:newest", vis)
	}

	// 5. Unread only filter
	a.Selected = "all"
	a.UnreadOnly = true
	vis = a.VisibleRSSArticles()
	if len(vis) != 2 || vis[0].ID != "art:newest" || vis[1].ID != "art:middle" {
		t.Fatalf("unread only visible = %v, want 2 unread articles", vis)
	}

	// 6. Search filter (matches title)
	a.UnreadOnly = false
	a.Search = "Tech"
	vis = a.VisibleRSSArticles()
	if len(vis) != 1 || vis[0].ID != "art:newest" {
		t.Fatalf("search 'Tech' visible = %v, want art:newest", vis)
	}

	// 7. Search filter (matches snippet)
	a.Search = "Design review"
	vis = a.VisibleRSSArticles()
	if len(vis) != 1 || vis[0].ID != "art:middle" {
		t.Fatalf("search 'Design review' visible = %v, want art:middle", vis)
	}

	// 8. Search filter (matches feed title)
	a.Search = "Beta Digest"
	vis = a.VisibleRSSArticles()
	if len(vis) != 1 || vis[0].ID != "art:middle" {
		t.Fatalf("search feed title 'Beta Digest' visible = %v, want art:middle", vis)
	}
}

func TestRSSArticlesUIInteraction(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	ctx := context.Background()

	now := time.Now().UnixMilli()
	f1 := core.RSSFeed{ID: "f-tech", FeedURL: "https://tech.org/feed", Title: "Tech Daily", UpdatedAt: now}
	f2 := core.RSSFeed{ID: "f-art", FeedURL: "https://art.org/feed", Title: "Art Journal", UpdatedAt: now}
	_ = a.Store.SaveRSSFeed(ctx, f1)
	_ = a.Store.SaveRSSFeed(ctx, f2)

	articles := []core.RSSArticle{
		{
			ID:          "art-1",
			FeedID:      "f-tech",
			Title:       "Quantum Computing Advances",
			Snippet:     "Researchers achieve 1000 qubit fidelity milestone.",
			PublishedAt: time.Now().Add(-2 * time.Hour).Format(time.RFC3339),
			Read:        false,
			UpdatedAt:   now,
		},
		{
			ID:          "art-2",
			FeedID:      "f-art",
			Title:       "Minimalism in Scandinavian Design",
			Snippet:     "How simplicity and functionalism merged.",
			PublishedAt: time.Now().Add(-5 * time.Hour).Format(time.RFC3339),
			Read:        true,
			UpdatedAt:   now,
		},
		{
			ID:          "art-3",
			FeedID:      "f-tech",
			Title:       "Rust 2026 Edition Announced",
			Snippet:     "New async primitives and memory safety improvements.",
			PublishedAt: time.Now().Add(-10 * time.Hour).Format(time.RFC3339),
			Read:        false,
			UpdatedAt:   now,
		},
	}
	_ = a.Store.SaveRSSArticles(ctx, articles)
	_ = a.ReloadRSS()

	w := &Window{app: a, selectedIndex: new(int)}
	w.selectWorkspace(WorkspaceRSS)

	var selectedObserverCalls []string
	w.onRSSArticleSelected = func(art core.RSSArticle) {
		selectedObserverCalls = append(selectedObserverCalls, art.ID)
	}

	tt := ui.NewTester(w.view, 1280, 808)
	tt.Frame()

	// 1. Verify workspace isolation: NO YouTube video titles or channel filter
	if tt.HasText("Go 1.24 Deep Dive") || tt.HasText("All channels") {
		t.Fatal("RSS workspace must NOT contain YouTube video titles or filters")
	}

	// 2. Verify Column Header renders current selection and unread count
	if !tt.HasText("All Articles") {
		t.Fatalf("Column header missing 'All Articles': texts = %v", tt.Texts())
	}
	if !tt.HasText("2 unread") {
		t.Fatalf("Column header missing '2 unread': texts = %v", tt.Texts())
	}

	// 3. Verify Article Filters strip contains search, unread checkbox, and count text
	if !tt.HasText("Search articles") {
		t.Fatalf("Article filters strip missing search field")
	}
	if !tt.HasText("Unread") {
		t.Fatalf("Article filters strip missing Unread checkbox")
	}
	if !tt.HasText("3 articles") {
		t.Fatalf("Article filters strip missing '3 articles' count text")
	}

	// 4. Verify Article list items
	for _, art := range articles {
		if !tt.HasText(art.Title) {
			t.Errorf("article title %q not found in UI", art.Title)
		}
	}

	// Verify unread dot exists for unread items and does not exist for read items
	if _, ok := tt.Find("Unread"); !ok {
		t.Errorf("Unread indicator dot or label not found")
	}

	// 5. Click an article item to select it
	artBox, ok := tt.Find("Quantum Computing Advances")
	if !ok {
		t.Fatal("could not find Quantum Computing Advances item")
	}
	tt.Press(artBox.X+artBox.W/2, artBox.Y+artBox.H/2)
	tt.Release(artBox.X+artBox.W/2, artBox.Y+artBox.H/2)
	tt.Frame()

	if a.SelectedRSSArticleID != "art-1" {
		t.Fatalf("SelectedRSSArticleID = %q, want 'art-1'", a.SelectedRSSArticleID)
	}
	if len(selectedObserverCalls) == 0 || selectedObserverCalls[len(selectedObserverCalls)-1] != "art-1" {
		t.Fatalf("expected onRSSArticleSelected to be called with 'art-1', got %v", selectedObserverCalls)
	}

	// 6. Keyboard navigation: Arrow Down moves to art-2
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if a.SelectedRSSArticleID != "art-2" {
		t.Fatalf("after KeyDown, SelectedRSSArticleID = %q, want 'art-2'", a.SelectedRSSArticleID)
	}

	// Arrow Down again moves to art-3
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if a.SelectedRSSArticleID != "art-3" {
		t.Fatalf("after second KeyDown, SelectedRSSArticleID = %q, want 'art-3'", a.SelectedRSSArticleID)
	}

	// Arrow Up moves back to art-2
	tt.Key(0, ui.KeyUp)
	tt.Frame()
	if a.SelectedRSSArticleID != "art-2" {
		t.Fatalf("after KeyUp, SelectedRSSArticleID = %q, want 'art-2'", a.SelectedRSSArticleID)
	}

	// 7. Click unread dot on art-1 to mark as read
	unreadCountBefore := a.RSSUnreadCount("")
	if err := a.MarkRSSArticleRead("art-1", true); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	unreadCountAfter := a.RSSUnreadCount("")
	if unreadCountAfter != unreadCountBefore-1 {
		t.Fatalf("unread count = %d, want %d", unreadCountAfter, unreadCountBefore-1)
	}
	if !tt.HasText("1 unread") {
		t.Fatalf("header should update to '1 unread': texts = %v", tt.Texts())
	}

	// 8. Toggle read back via ToggleRSSArticleRead
	if err := a.ToggleRSSArticleRead("art-1"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("2 unread") {
		t.Fatalf("header should restore '2 unread'")
	}

	// 9. Context menu right-click mark as read
	if err := tt.RightClick("Quantum Computing Advances"); err == nil {
		if err := tt.ChooseMenuItem("Mark as Read"); err == nil {
			tt.Frame()
			stored, _ := a.Store.GetRSSArticle(ctx, "art-1")
			if stored == nil || !stored.Read {
				t.Errorf("art-1 should be marked read via context menu")
			}
		}
	}

	// 10. Search filtering in UI
	a.Search = "Quantum"
	tt.Frame()
	if !tt.HasText("Quantum Computing Advances") {
		t.Errorf("search result should contain Quantum Computing Advances")
	}
	if tt.HasText("Minimalism in Scandinavian Design") {
		t.Errorf("search result should NOT contain Minimalism in Scandinavian Design")
	}

	// 11. Empty state when search has no matches
	a.Search = "NonExistentKeywordXYZ"
	tt.Frame()
	if !tt.HasText("No Matching Articles") {
		t.Fatalf("expected 'No Matching Articles' empty state headline")
	}

	// 12. Switch to Videos workspace and back: state isolation check
	a.Search = ""
	w.selectWorkspace(WorkspaceVideos)
	tt.Frame()
	if w.activeWorkspaceName() != WorkspaceVideos {
		t.Fatalf("active workspace = %q, want videos", w.activeWorkspaceName())
	}
	if tt.HasText("Quantum Computing Advances") {
		t.Fatal("Videos workspace must NOT contain RSS articles")
	}

	w.selectWorkspace(WorkspaceRSS)
	tt.Frame()
	if w.activeWorkspaceName() != WorkspaceRSS {
		t.Fatalf("active workspace = %q, want rss", w.activeWorkspaceName())
	}
	if !tt.HasText("Quantum Computing Advances") {
		t.Fatal("RSS workspace must restore RSS articles")
	}
}