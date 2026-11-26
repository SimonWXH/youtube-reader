package main

import (
	"context"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
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

	// 1. All Unread: only the unread articles, newest first, excluding the
	// deleted feed
	a.Selected = "all"
	a.Search = ""
	a.UnreadOnly = false
	vis := a.VisibleRSSArticles()
	if len(vis) != 2 {
		t.Fatalf("VisibleRSSArticles(all) len = %d, want the 2 unread articles", len(vis))
	}
	if vis[0].ID != "art:newest" || vis[1].ID != "art:middle" {
		t.Fatalf("unread articles not sorted newest first: %v", []string{vis[0].ID, vis[1].ID})
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
			Snippet:     "",
			ContentHTML: "<p>New async primitives and <b>memory safety</b> improvements.</p>",
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
	if !tt.HasText("All Unread") {
		t.Fatalf("Column header missing 'All Unread': texts = %v", tt.Texts())
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
	if !tt.HasText("2 articles") {
		t.Fatalf("Article filters strip missing '2 articles' count text")
	}

	// 4. Verify the unread list items; the read article stays out of it
	for _, art := range articles {
		if art.Read {
			if tt.HasText(art.Title) {
				t.Errorf("read article %q is listed under All Unread", art.Title)
			}
			continue
		}
		if !tt.HasText(art.Title) {
			t.Errorf("article title %q not found in UI", art.Title)
		}
	}

	// Rows without a stored snippet show plain text derived from ContentHTML
	if !tt.HasText("New async primitives and memory safety improvements.") {
		t.Errorf("expected snippet fallback from ContentHTML to render as plain text: %v", tt.Texts())
	}

	// Verify the unread dot exists for unread items
	if _, ok := tt.Find("Unread: Quantum Computing Advances"); !ok {
		t.Errorf("unread dot missing for unread article art-1")
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

	// 6. Keyboard navigation walks the unread list: Arrow Down moves to art-3,
	// which is the last unread row, and Arrow Up comes back to art-1
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if a.SelectedRSSArticleID != "art-3" {
		t.Fatalf("after KeyDown, SelectedRSSArticleID = %q, want 'art-3'", a.SelectedRSSArticleID)
	}

	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if a.SelectedRSSArticleID != "art-3" {
		t.Fatalf("KeyDown past the last unread row chose %q, want 'art-3'", a.SelectedRSSArticleID)
	}

	tt.Key(0, ui.KeyUp)
	tt.Frame()
	if a.SelectedRSSArticleID != "art-1" {
		t.Fatalf("after KeyUp, SelectedRSSArticleID = %q, want 'art-1'", a.SelectedRSSArticleID)
	}

	// 7. The unread dot is a status indicator: clicking its area selects the row
	// and leaves the article unread.
	unreadCountBefore := a.RSSUnreadCount("")
	if err := tt.Click("Unread: Quantum Computing Advances"); err != nil {
		t.Fatalf("click unread dot area: %v", err)
	}
	tt.Frame()
	if a.SelectedRSSArticleID != "art-1" {
		t.Fatalf("clicking the dot area should select art-1, SelectedRSSArticleID = %q", a.SelectedRSSArticleID)
	}
	if got := a.RSSUnreadCount(""); got != unreadCountBefore {
		t.Fatalf("clicking the dot must not mark the article read, unread count = %d, want %d", got, unreadCountBefore)
	}

	// 8. Context menu right-click marks the article read, which takes it out of
	// the All Unread column while the reader keeps it open
	if err := tt.RightClick("Quantum Computing Advances"); err != nil {
		t.Fatalf("right-click article row: %v", err)
	}
	if err := tt.ChooseMenuItem("Mark as Read"); err != nil {
		t.Fatalf("choose 'Mark as Read': %v", err)
	}
	tt.Frame()
	storedMenu, err := a.Store.GetRSSArticle(ctx, "art-1")
	if err != nil || storedMenu == nil || !storedMenu.Read {
		t.Fatalf("art-1 should be marked read via context menu: %v %+v", err, storedMenu)
	}
	if got := a.RSSUnreadCount(""); got != unreadCountBefore-1 {
		t.Fatalf("unread count after context-menu read = %d, want %d", got, unreadCountBefore-1)
	}
	if !tt.HasText("1 unread") {
		t.Fatalf("header should show '1 unread' after context-menu read: texts = %v", tt.Texts())
	}
	for _, art := range a.VisibleRSSArticles() {
		if art.ID == "art-1" {
			t.Errorf("the read article is still listed under All Unread: %+v", art)
		}
	}
	if _, ok := tt.Find("Unread: Quantum Computing Advances"); ok {
		t.Errorf("the read article's unread dot is still drawn")
	}
	if a.SelectedRSSArticleID != "art-1" {
		t.Fatalf("marking the open article read moved the selection to %q", a.SelectedRSSArticleID)
	}

	// 9. The reader's toggle marks the still-open article unread again, and it
	// returns to the column
	if err := tt.Click("Mark as Unread (M)"); err != nil {
		t.Fatalf("click the reader's read toggle: %v", err)
	}
	tt.Frame()
	storedMenu, err = a.Store.GetRSSArticle(ctx, "art-1")
	if err != nil || storedMenu == nil || storedMenu.Read {
		t.Fatalf("art-1 should be marked unread via the reader toggle: %v %+v", err, storedMenu)
	}
	if !tt.HasText("2 unread") {
		t.Fatalf("header should restore '2 unread': texts = %v", tt.Texts())
	}
	if _, ok := tt.Find("Unread: Quantum Computing Advances"); !ok {
		t.Errorf("the unread article should return to the All Unread column")
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

func TestRSSArticleKeyboardNavigationRevealsSelection(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	ctx := context.Background()

	now := time.Now().UnixMilli()
	feed := core.RSSFeed{ID: "f-feed", FeedURL: "https://feed.example.com/rss", Title: "Daily Feed", UpdatedAt: now}
	if err := a.Store.SaveRSSFeed(ctx, feed); err != nil {
		t.Fatal(err)
	}

	articles := make([]core.RSSArticle, 0, 30)
	for i := 0; i < 30; i++ {
		articles = append(articles, core.RSSArticle{
			ID:          fmt.Sprintf("art-%02d", i),
			FeedID:      feed.ID,
			Title:       fmt.Sprintf("Article %02d", i),
			Snippet:     "Snippet body long enough to fill the two summary lines of the row.",
			PublishedAt: time.Now().Add(-time.Duration(i) * time.Hour).Format(time.RFC3339),
			UpdatedAt:   now,
		})
	}
	if err := a.Store.SaveRSSArticles(ctx, articles); err != nil {
		t.Fatal(err)
	}
	if err := a.ReloadRSS(); err != nil {
		t.Fatalf("ReloadRSS failed: %v", err)
	}

	w := &Window{app: a, selectedIndex: new(int)}
	w.selectWorkspace(WorkspaceRSS)
	tt := ui.NewTester(w.shell, 1280, 808)
	tt.Frame()

	if err := tt.Click("Article 00"); err != nil {
		t.Fatalf("click first article row: %v", err)
	}
	tt.Frame()
	if a.SelectedRSSArticleID != "art-00" {
		t.Fatalf("SelectedRSSArticleID = %q, want art-00", a.SelectedRSSArticleID)
	}

	const moves = 12
	for i := 0; i < moves; i++ {
		tt.Key(0, ui.KeyDown)
		tt.Frame()
	}
	want := fmt.Sprintf("art-%02d", moves)
	if a.SelectedRSSArticleID != want {
		t.Fatalf("after %d KeyDown, SelectedRSSArticleID = %q, want %q", moves, a.SelectedRSSArticleID, want)
	}

	selected := fmt.Sprintf("Article %02d", moves)
	if err := tt.Click(selected); err != nil {
		t.Fatalf("selected row %q is not reachable in the viewport: %v", selected, err)
	}
	tt.Frame()
	if a.SelectedRSSArticleID != want {
		t.Fatalf("SelectedRSSArticleID = %q, want %q", a.SelectedRSSArticleID, want)
	}
}

func saveEvidenceScreenshot(t *testing.T, tt *ui.Tester, filename string) {
	t.Helper()
	evidenceDir := "/Users/simon/.no-mistakes/evidence/01M8714Q13ANXZJVMJ7APQ2A6B"
	if fi, err := os.Stat(evidenceDir); err == nil && fi.IsDir() {
		outPath := filepath.Join(evidenceDir, filename)
		img := tt.Image()
		if img != nil {
			f, err := os.Create(outPath)
			if err != nil {
				t.Logf("failed to create evidence file %s: %v", outPath, err)
				return
			}
			defer f.Close()
			if err := png.Encode(f, img); err != nil {
				t.Logf("failed to encode png %s: %v", outPath, err)
			}
		}
	}
}

func TestRSSScenariosEndToEnd(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	ctx := context.Background()

	nowMs := time.Now().UnixMilli()

	f1 := core.RSSFeed{ID: "feed-1", Title: "Tech Weekly", FeedURL: "https://example.com/tech.xml", SiteURL: "https://example.com", UpdatedAt: nowMs}
	f2 := core.RSSFeed{ID: "feed-2", Title: "Design Systems", FeedURL: "https://example.com/design.xml", SiteURL: "https://example.com", UpdatedAt: nowMs}
	f3 := core.RSSFeed{ID: "feed-3", Title: "Science Journal", FeedURL: "https://example.com/science.xml", SiteURL: "https://example.com", UpdatedAt: nowMs}

	for _, f := range []core.RSSFeed{f1, f2, f3} {
		if err := a.Store.SaveRSSFeed(ctx, f); err != nil {
			t.Fatalf("save feed %s: %v", f.ID, err)
		}
	}

	g1 := core.RSSGroup{
		ID:        "group-1",
		Name:      "Engineering",
		FeedIDs:   []string{"feed-1", "feed-2"},
		SortOrder: 0,
		UpdatedAt: nowMs,
	}
	if err := a.Store.SaveRSSGroup(ctx, g1); err != nil {
		t.Fatalf("save group: %v", err)
	}

	articles := []core.RSSArticle{
		{
			ID:          "art-101",
			FeedID:      "feed-1",
			Title:       "Next-Gen Quantum Processors Exceed 1000 Qubits with High Fidelity",
			Link:        "https://example.com/quantum",
			Author:      "Dr. Alice Vance",
			PublishedAt: time.Now().Add(-15 * time.Minute).Format(time.RFC3339),
			Snippet:     "A team of experimental physicists has demonstrated sustained fault-tolerant quantum error correction across a two-dimensional grid of superconducting qubits.",
			Read:        false,
			Starred:     false,
			UpdatedAt:   nowMs,
		},
		{
			ID:          "art-102",
			FeedID:      "feed-2",
			Title:       "Crafting Spatial UI: Principles of Depth and Motion in Desktop Apps",
			Link:        "https://example.com/spatial-ui",
			Author:      "Bob Miller",
			PublishedAt: time.Now().Add(-3 * time.Hour).Format(time.RFC3339),
			Snippet:     "Exploring elevation tokens, dynamic border hairlines, and spring animations to establish tactile spatial hierarchies in native desktop interfaces.",
			Read:        true,
			Starred:     true,
			UpdatedAt:   nowMs,
		},
		{
			ID:          "art-103",
			FeedID:      "feed-1",
			Title:       "Go 1.25 Runtime Scheduler and Memory Management Enhancements",
			Link:        "https://example.com/go125",
			Author:      "Carol Danvers",
			PublishedAt: time.Now().Add(-26 * time.Hour).Format(time.RFC3339),
			Snippet:     "An in-depth analysis of the revised GC pacing heuristics and thread pooling optimizations tailored for high-concurrency microservices.",
			Read:        false,
			Starred:     true,
			UpdatedAt:   nowMs,
		},
		{
			ID:          "art-104",
			FeedID:      "feed-3",
			Title:       "Atmospheric Methane Monitoring from Low-Earth Orbit Constellations",
			Link:        "https://example.com/methane-leo",
			Author:      "Dave Bowman",
			PublishedAt: time.Now().Add(-72 * time.Hour).Format(time.RFC3339),
			Snippet:     "High-resolution hyperspectral satellite imagery unveils localized point-source emissions across major industrial basins with unprecedented fidelity.",
			Read:        true,
			Starred:     false,
			UpdatedAt:   nowMs,
		},
		{
			ID:          "art-105",
			FeedID:      "feed-3",
			Title:       "Historical Archives of the Early Web and Open Feed Protocols",
			Link:        "https://example.com/rss-history",
			Author:      "Eve Adams",
			PublishedAt: "2025-08-14T10:00:00Z",
			Snippet:     "Retrospective examination of RSS 2.0 and Atom syndication standards and their enduring resilience against centralized platform lock-in.",
			Read:        false,
			Starred:     false,
			UpdatedAt:   nowMs,
		},
	}

	if err := a.Store.SaveRSSArticles(ctx, articles); err != nil {
		t.Fatalf("save articles: %v", err)
	}

	if err := a.ReloadRSS(); err != nil {
		t.Fatalf("reload rss: %v", err)
	}

	selectedIndex := -1
	w := &Window{app: a, selectedIndex: &selectedIndex}
	w.selectWorkspace(WorkspaceRSS)

	var selectedDispatches []string
	w.onRSSArticleSelected = func(art core.RSSArticle) {
		selectedDispatches = append(selectedDispatches, art.ID)
	}

	tt := ui.NewTester(w.shell, 1280, 808)
	tt.Frame()

	// Scenario 1: NetNewsWire-style article card layout and styling
	t.Run("Scenario 1: NetNewsWire-style article card layout and styling", func(t *testing.T) {
		for _, art := range articles {
			if art.Read {
				if tt.HasText(art.Title) {
					t.Errorf("read article %q is listed under All Unread", art.Title)
				}
				continue
			}
			if !tt.HasText(art.Title) {
				t.Errorf("expected article title %q to be rendered", art.Title)
			}
		}

		if !tt.HasText("Tech Weekly") || !tt.HasText("Science Journal") {
			t.Errorf("expected the feeds with unread articles to be visible on line 1")
		}

		if !tt.HasText("15m ago") || !tt.HasText("Aug 14, 2025") {
			t.Errorf("expected formatted dates on line 1: texts = %v", tt.Texts())
		}

		if !tt.HasText("fault-tolerant quantum error correction") {
			t.Errorf("expected snippet text on line 3-4")
		}

		saveEvidenceScreenshot(t, tt, "scenario1_article_cards_layout.png")
	})

	// Scenario 2: Column header, count, unread filter, and chronological sorting
	t.Run("Scenario 2: Column header, count, unread filter, and sorting", func(t *testing.T) {
		if !tt.HasText("All Unread") {
			t.Errorf("column header must show 'All Unread'")
		}
		if !tt.HasText("3 unread") {
			t.Errorf("column header must show '3 unread', texts = %v", tt.Texts())
		}
		if !tt.HasText("3 articles") {
			t.Errorf("toolbar strip must show '3 articles', texts = %v", tt.Texts())
		}
		if !tt.HasText("Search articles") {
			t.Errorf("toolbar strip must have search input placeholder")
		}

		vis := a.VisibleRSSArticles()
		if len(vis) != 3 {
			t.Fatalf("expected the 3 unread articles, got %d", len(vis))
		}
		for i := 1; i < len(vis); i++ {
			if vis[i-1].PublishedAt < vis[i].PublishedAt {
				t.Fatalf("articles not sorted newest first: [%d]=%s, [%d]=%s", i-1, vis[i-1].PublishedAt, i, vis[i].PublishedAt)
			}
		}

		a.UnreadOnly = true
		tt.Frame()

		if !tt.HasText("3 unread") {
			t.Errorf("with unread filter active, expected header '3 unread', texts = %v", tt.Texts())
		}
		if !tt.HasText("3 articles") {
			t.Errorf("with unread filter active, expected strip '3 articles', texts = %v", tt.Texts())
		}
		if !tt.HasText("Next-Gen Quantum Processors Exceed 1000 Qubits with High Fidelity") {
			t.Errorf("unread article should remain visible under unread filter")
		}
		if tt.HasText("Crafting Spatial UI: Principles of Depth and Motion in Desktop Apps") {
			t.Errorf("read article should be hidden under unread filter")
		}

		saveEvidenceScreenshot(t, tt, "scenario2_header_counts_and_unread_filter.png")

		a.UnreadOnly = false
		tt.Frame()
	})

	// Scenario 3: Article selection and stable keyboard arrow navigation
	t.Run("Scenario 3: Selection and stable keyboard navigation", func(t *testing.T) {
		rect, ok := tt.Find("Next-Gen Quantum Processors Exceed 1000 Qubits with High Fidelity")
		if !ok {
			t.Fatal("could not find article card art-101")
		}
		tt.ClickAt(rect.X+rect.W/2, rect.Y+rect.H/2)
		tt.Frame()

		if a.SelectedRSSArticleID != "art-101" {
			t.Fatalf("expected SelectedRSSArticleID to be 'art-101', got %q", a.SelectedRSSArticleID)
		}
		if len(selectedDispatches) == 0 || selectedDispatches[len(selectedDispatches)-1] != "art-101" {
			t.Fatalf("expected observer callback dispatched with 'art-101'")
		}

		tt.Key(0, ui.KeyDown)
		tt.Frame()
		if a.SelectedRSSArticleID != "art-103" {
			t.Fatalf("after KeyDown, expected SelectedRSSArticleID 'art-103', got %q", a.SelectedRSSArticleID)
		}

		tt.Key(0, ui.KeyDown)
		tt.Frame()
		if a.SelectedRSSArticleID != "art-105" {
			t.Fatalf("after second KeyDown, expected SelectedRSSArticleID 'art-105', got %q", a.SelectedRSSArticleID)
		}

		tt.Key(0, ui.KeyUp)
		tt.Frame()
		if a.SelectedRSSArticleID != "art-103" {
			t.Fatalf("after KeyUp, expected SelectedRSSArticleID 'art-103', got %q", a.SelectedRSSArticleID)
		}

		saveEvidenceScreenshot(t, tt, "scenario3_selection_and_keyboard_navigation.png")
	})

	// Scenario 4: Marking articles read/unread and database persistence
	t.Run("Scenario 4: Marking articles read/unread and database persistence", func(t *testing.T) {
		initialUnread := a.RSSUnreadCount("")
		if initialUnread != 3 {
			t.Fatalf("initial unread count = %d, want 3", initialUnread)
		}

		if err := tt.RightClick("Go 1.25 Runtime Scheduler and Memory Management Enhancements"); err != nil {
			t.Fatalf("right-click article row: %v", err)
		}
		if err := tt.ChooseMenuItem("Mark as Read"); err != nil {
			t.Fatalf("choose 'Mark as Read': %v", err)
		}
		tt.Frame()

		if count := a.RSSUnreadCount(""); count != 2 {
			t.Fatalf("after marking read, in-memory unread count = %d, want 2", count)
		}
		if !tt.HasText("2 unread") {
			t.Errorf("header should show '2 unread', texts = %v", tt.Texts())
		}
		if a.SelectedRSSArticleID != "art-103" {
			t.Fatalf("marking the open article read moved the selection to %q", a.SelectedRSSArticleID)
		}

		stored, err := a.Store.GetRSSArticle(ctx, "art-103")
		if err != nil || stored == nil {
			t.Fatalf("failed to retrieve art-103 from sqlite store: %v", err)
		}
		if !stored.Read || stored.ReadAt == 0 {
			t.Errorf("store did not persist Read=true and ReadAt > 0: %+v", stored)
		}

		if err := tt.Click("Mark as Unread (M)"); err != nil {
			t.Fatalf("click the reader's read toggle: %v", err)
		}
		tt.Frame()

		storedUnread, err := a.Store.GetRSSArticle(ctx, "art-103")
		if err != nil || storedUnread == nil || storedUnread.Read {
			t.Fatalf("store did not persist Read=false after the toggle: %v %+v", err, storedUnread)
		}
		if !tt.HasText("3 unread") {
			t.Errorf("header should restore '3 unread', texts = %v", tt.Texts())
		}

		saveEvidenceScreenshot(t, tt, "scenario4_mark_read_and_unread_state.png")
	})

	// Scenario 5: Workspace isolation and empty state handling
	t.Run("Scenario 5: Workspace isolation and empty state handling", func(t *testing.T) {
		w.selectWorkspace(WorkspaceVideos)
		tt.Frame()
		if w.activeWorkspaceName() != WorkspaceVideos {
			t.Fatalf("expected active workspace Videos, got %q", w.activeWorkspaceName())
		}
		if tt.HasText("Next-Gen Quantum Processors Exceed 1000 Qubits with High Fidelity") {
			t.Fatal("Videos workspace must NOT contain RSS articles")
		}

		w.selectWorkspace(WorkspaceRSS)
		tt.Frame()
		if w.activeWorkspaceName() != WorkspaceRSS {
			t.Fatalf("expected active workspace RSS, got %q", w.activeWorkspaceName())
		}
		if !tt.HasText("Next-Gen Quantum Processors Exceed 1000 Qubits with High Fidelity") {
			t.Fatal("RSS workspace must restore RSS articles")
		}

		a.Search = "NonExistentQuantumX999"
		tt.Frame()

		if !tt.HasText("No Matching Articles") {
			t.Fatalf("expected 'No Matching Articles' empty state headline, texts = %v", tt.Texts())
		}
		if !tt.HasText("Try clearing search terms or the unread filter.") {
			t.Fatalf("expected empty state sentence")
		}

		saveEvidenceScreenshot(t, tt, "scenario5_workspace_isolation_and_empty_state.png")
	})
}
