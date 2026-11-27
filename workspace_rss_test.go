package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo"

	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
)

func setupRSSTestApp(t *testing.T) (*App, *Window) {
	t.Helper()
	dir := t.TempDir()
	a := newTestApp(t, dir)
	t.Cleanup(func() { a.Close() })

	channels := []core.Channel{
		{ID: "yt-ch-1", Title: "YouTube Tech", UpdatedAt: 100},
	}
	videos := []core.Video{
		{ID: "yt-v-1", ChannelID: "yt-ch-1", ChannelTitle: "YouTube Tech", Title: "YouTube Tech Video", PublishedAt: "2026-01-01T00:00:00Z"},
	}
	a.Sync.Channels = map[string]core.Channel{"yt-ch-1": channels[0]}
	a.Channels = channels
	a.Videos = videos
	a.Selected = "all"
	a.Settings.APIKey = "test-key"
	a.Settings.LastRefresh = "2026-01-01T00:00:00Z"
	a.Settings.SidebarWidth = defaultSidebarWidth
	a.Settings.DetailsWidth = defaultDetailsWidth

	ctx := context.Background()
	_ = a.Store.CommitRefresh(ctx, core.RefreshCommit{
		Channels: channels,
		Videos:   videos,
		Revision: 1,
	})

	_ = a.ReloadRSS()
	w := &Window{app: a, selectedIndex: new(int)}
	w.ensureWorkspaceInit()
	return a, w
}

func TestRSSSidebarWorkspaceIsolation(t *testing.T) {
	a, w := setupRSSTestApp(t)

	// Add an RSS group and feeds
	g, err := a.CreateRSSGroup("Tech Feeds")
	if err != nil {
		t.Fatalf("CreateRSSGroup failed: %v", err)
	}
	feed := core.RSSFeed{
		ID:      core.FeedID("https://news.ycombinator.com/rss"),
		FeedURL: "https://news.ycombinator.com/rss",
		Title:   "Hacker News",
	}
	if err := a.SubscribeRSSFeed(feed, g.ID); err != nil {
		t.Fatalf("SubscribeRSSFeed failed: %v", err)
	}

	tt := ui.NewTester(w.shell, 1280, 808)
	tt.Frame()

	// In initial Videos workspace, YouTube channel shows, RSS does not show in sidebar
	if !tt.HasText("YouTube Tech Video") {
		t.Fatal("videos workspace should show YouTube video")
	}

	// Switch to RSS workspace
	w.selectWorkspace(WorkspaceRSS)
	tt.Frame()

	// 1. Strict Isolation: ZERO YouTube channels or videos in the RSS sidebar
	if tt.HasText("YouTube Tech Video") {
		t.Fatal("RSS workspace must NOT show YouTube video")
	}
	if tt.HasText("YouTube Tech") {
		t.Fatal("RSS workspace must NOT show YouTube channels in sidebar")
	}

	// 2. RSS elements present
	if !tt.HasText("Tech Feeds") {
		t.Fatal("RSS sidebar should display RSS group Tech Feeds")
	}
	if !tt.HasText("Hacker News") {
		t.Fatal("RSS sidebar should display RSS feed Hacker News")
	}
	for _, want := range []string{"Today", "All Unread", "Starred"} {
		if !tt.HasText(want) {
			t.Fatalf("RSS sidebar should display the smart feed %q", want)
		}
	}

	// Switch back to Videos workspace
	w.selectWorkspace(WorkspaceVideos)
	tt.Frame()

	if !tt.HasText("YouTube Tech Video") {
		t.Fatal("switching back to Videos workspace should restore video view")
	}
	if tt.HasText("Tech Feeds") {
		t.Fatal("Videos workspace should not show RSS groups")
	}
}

func TestRSSSidebarEmptyStateAndAddButton(t *testing.T) {
	_, w := setupRSSTestApp(t)

	w.selectWorkspace(WorkspaceRSS)
	tt := ui.NewTester(w.shell, 1280, 808)
	tt.Frame()

	if !tt.HasText("No RSS feeds yet") {
		t.Fatal("empty RSS library should display empty state title")
	}
	if !tt.HasText("Add RSS Feed") {
		t.Fatal("empty RSS library should offer Add RSS Feed button")
	}

	// Click the empty state Add RSS Feed button
	if err := tt.Click("Add RSS Feed"); err != nil {
		t.Fatalf("Click Add RSS Feed failed: %v", err)
	}
	tt.Frame()

	if !w.addRSSDialog.open {
		t.Fatal("clicking Add RSS Feed button should open addRSSDialog")
	}

	// Close dialog
	w.closeAddRSSDialog(nil)
	tt.Frame()
	if w.addRSSDialog.open {
		t.Fatal("dialog should be closed")
	}
}

func TestRSSSidebarFailureIndicatorAndTooltip(t *testing.T) {
	a, w := setupRSSTestApp(t)

	feed := core.RSSFeed{
		ID:           core.FeedID("https://broken.example.com/feed.xml"),
		FeedURL:      "https://broken.example.com/feed.xml",
		Title:        "Broken Blog",
		FailureCount: 3,
		LastError:    "HTTP 404 Not Found",
	}
	if err := a.SubscribeRSSFeed(feed, ""); err != nil {
		t.Fatalf("SubscribeRSSFeed failed: %v", err)
	}

	tipEn := failureTooltip("en_US", a.RSSFeedByID(feed.ID))
	wantEn := "Fetch failed (3 attempts): HTTP 404 Not Found"
	if tipEn != wantEn {
		t.Fatalf("failureTooltip EN = %q, want %q", tipEn, wantEn)
	}

	tipZh := failureTooltip("zh_CN", a.RSSFeedByID(feed.ID))
	wantZh := "抓取失败（连续失败 3 次）：HTTP 404 Not Found"
	if tipZh != wantZh {
		t.Fatalf("failureTooltip ZH = %q, want %q", tipZh, wantZh)
	}

	w.selectWorkspace(WorkspaceRSS)
	tt := ui.NewTester(w.shell, 1280, 808)
	tt.Frame()

	if !tt.HasText("Broken Blog") {
		t.Fatal("RSS sidebar should display Broken Blog")
	}

	// Clear failure (simulate successful fetch)
	loaded := a.RSSFeedByID(feed.ID)
	loaded.FailureCount = 0
	loaded.LastError = ""
	_ = a.Store.SaveRSSFeed(context.Background(), loaded)
	_ = a.ReloadRSS()

	clearedTip := failureTooltip("en_US", a.RSSFeedByID(feed.ID))
	if clearedTip != "" {
		t.Fatalf("expected empty failure tooltip after recovery, got %q", clearedTip)
	}
}

func TestRSSSidebarInlineRenaming(t *testing.T) {
	a, w := setupRSSTestApp(t)

	g, err := a.CreateRSSGroup("Original Group")
	if err != nil {
		t.Fatalf("CreateRSSGroup failed: %v", err)
	}
	feed := core.RSSFeed{
		ID:      core.FeedID("https://example.com/feed.xml"),
		FeedURL: "https://example.com/feed.xml",
		Title:   "Original Feed",
	}
	if err := a.SubscribeRSSFeed(feed, g.ID); err != nil {
		t.Fatalf("SubscribeRSSFeed failed: %v", err)
	}

	w.selectWorkspace(WorkspaceRSS)
	tt := ui.NewTester(w.shell, 1280, 808)
	tt.Frame()

	// 1. Rename Group inline
	w.startRenameRSSGroup(g.ID, g.Name)
	if w.renamingRSSGroupID != g.ID {
		t.Fatalf("renamingRSSGroupID = %q, want %q", w.renamingRSSGroupID, g.ID)
	}
	w.commitRenameRSSGroup(g.ID, "Renamed Group")
	if w.renamingRSSGroupID != "" {
		t.Fatal("renamingRSSGroupID should be cleared after commit")
	}
	if a.RSSGroupByID(g.ID).Name != "Renamed Group" {
		t.Fatalf("group name = %q, want Renamed Group", a.RSSGroupByID(g.ID).Name)
	}

	// 2. Rename Feed inline
	w.startRenameRSSFeed(feed.ID, feed.DisplayName())
	if w.renamingRSSFeedID != feed.ID {
		t.Fatalf("renamingRSSFeedID = %q, want %q", w.renamingRSSFeedID, feed.ID)
	}
	w.commitRenameRSSFeed(feed.ID, "Renamed Feed")
	if w.renamingRSSFeedID != "" {
		t.Fatal("renamingRSSFeedID should be cleared after commit")
	}
	if a.RSSFeedByID(feed.ID).DisplayName() != "Renamed Feed" {
		t.Fatalf("feed display name = %q, want Renamed Feed", a.RSSFeedByID(feed.ID).DisplayName())
	}
}

func TestRSSSidebarDragAndDropReorderAndMove(t *testing.T) {
	a, w := setupRSSTestApp(t)

	g1, _ := a.CreateRSSGroup("Group A")
	g2, _ := a.CreateRSSGroup("Group B")

	feed1 := core.RSSFeed{ID: core.FeedID("https://example.com/f1.xml"), FeedURL: "https://example.com/f1.xml", Title: "F1"}
	feed2 := core.RSSFeed{ID: core.FeedID("https://example.com/f2.xml"), FeedURL: "https://example.com/f2.xml", Title: "F2"}
	_ = a.SubscribeRSSFeed(feed1, g1.ID)
	_ = a.SubscribeRSSFeed(feed2, g1.ID)

	// Live move group order: B before A
	w.liveMoveRSSGroup(g2.ID, 0)
	frame := rssGroupDragFrame{dropped: true}
	w.settleRSSGroupDrag(nil, &frame)

	if a.RSSGroups[0].ID != g2.ID || a.RSSGroups[1].ID != g1.ID {
		t.Fatalf("reordered groups = [%s, %s], want [g2, g1]", a.RSSGroups[0].ID, a.RSSGroups[1].ID)
	}

	// Move feed1 from g1 to g2
	w.moveRSSFeedToGroup(nil, feed1.ID, g2.ID)
	g1Feeds := a.RSSGroupByID(g1.ID).FeedIDs
	g2Feeds := a.RSSGroupByID(g2.ID).FeedIDs
	if slices.Contains(g1Feeds, feed1.ID) {
		t.Fatalf("g1 should no longer contain feed1, got %v", g1Feeds)
	}
	if !slices.Contains(g2Feeds, feed1.ID) {
		t.Fatalf("g2 should contain feed1, got %v", g2Feeds)
	}

	// Move feed1 to ungrouped
	w.moveRSSFeedToGroup(nil, feed1.ID, "")
	if len(a.RSSFeedByID(feed1.ID).GroupIDs) != 0 {
		t.Fatalf("feed1 should be ungrouped, got %v", a.RSSFeedByID(feed1.ID).GroupIDs)
	}

	// Reorder within group
	feed3 := core.RSSFeed{ID: core.FeedID("https://example.com/f3.xml"), FeedURL: "https://example.com/f3.xml", Title: "F3"}
	_ = a.SubscribeRSSFeed(feed3, g1.ID)
	w.reorderRSSFeedNear(nil, feed3.ID, g1.ID, feed2.ID, g1.ID)
	updatedG1 := a.RSSGroupByID(g1.ID).FeedIDs
	if len(updatedG1) != 2 || updatedG1[0] != feed3.ID || updatedG1[1] != feed2.ID {
		t.Fatalf("reordered in g1 = %v, want [%s, %s]", updatedG1, feed3.ID, feed2.ID)
	}
}

func TestAddRSSModalDialogLifecycle(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rss := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel><title>Modal Test Feed</title><link>` + r.Host + `</link><item><title>Item 1</title></item></channel></rss>`
		w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
		_, _ = w.Write([]byte(rss))
	}))
	defer ts.Close()

	a, w := setupRSSTestApp(t)

	w.selectWorkspace(WorkspaceRSS)
	tt := ui.NewTester(w.shell, 1280, 808)
	tt.Frame()

	// Open Add Dialog
	w.openAddRSSDialog()
	tt.Frame()

	if !w.addRSSDialog.open {
		t.Fatal("expected addRSSDialog to be open")
	}
	if !tt.HasText("Add RSS Feed") {
		t.Fatal("modal dialog should show title Add RSS Feed")
	}

	// Type feed URL and resolve
	w.addRSSDialog.input = ts.URL
	res, err := core.ResolveRSSSource(context.Background(), ts.Client(), ts.URL)
	if err != nil {
		t.Fatalf("ResolveRSSSource failed: %v", err)
	}

	w.applyResolveRSS(w.addRSSDialog.seq, res, nil)
	tt.Frame()

	if w.addRSSDialog.state != addRSSDialogResolved {
		t.Fatalf("expected dialog state resolved, got %v with error %q", w.addRSSDialog.state, w.addRSSDialog.errMessage)
	}

	if !tt.HasText("Modal Test Feed") {
		t.Fatal("resolved feed card should show feed title Modal Test Feed")
	}

	// Submit dialog
	w.submitAddRSSDialog(nil)
	tt.Frame()

	if w.addRSSDialog.open {
		t.Fatal("dialog should close after submission")
	}
	feedID := core.FeedID(core.CanonicalURL(ts.URL))
	if !a.HasRSSFeed(feedID) {
		t.Fatalf("expected feed %s to be subscribed in app", feedID)
	}
}

// TestRSSUnreadCountMatchesLibrary keeps the app's RSS unread total, which the
// sidebar labels and the column header read, tied to the stored articles. The
// dock itself no longer shows a count.
func TestRSSUnreadCountMatchesLibrary(t *testing.T) {
	a, _ := setupRSSTestApp(t)

	feed := core.RSSFeed{ID: core.FeedID("https://example.com/rss"), FeedURL: "https://example.com/rss", Title: "Feed 1"}
	_ = a.SubscribeRSSFeed(feed, "")

	ctx := context.Background()
	articles := []core.RSSArticle{
		{ID: "art-1", FeedID: feed.ID, Title: "A1", PublishedAt: "2026-01-01T00:00:00Z", Read: false},
		{ID: "art-2", FeedID: feed.ID, Title: "A2", PublishedAt: "2026-01-02T00:00:00Z", Read: false},
		{ID: "art-3", FeedID: feed.ID, Title: "A3", PublishedAt: "2026-01-03T00:00:00Z", Read: true},
	}
	_ = a.Store.SaveRSSArticles(ctx, articles)
	_ = a.ReloadRSS()

	if got := a.RSSTotalUnread; got != 2 {
		t.Fatalf("unread total = %d, want 2 unread articles", got)
	}
}

// TestRSSFolderDeleteKeepsEveryDrawnFeedVisible drives the folder's own delete
// entry from the sidebar. A feed the folder draws only because the feed names
// the folder must land in Ungrouped and stay on screen, rather than vanish with
// a dangling group id that no folder draws any more.
func TestRSSFolderDeleteKeepsEveryDrawnFeedVisible(t *testing.T) {
	stubNetImages(t)
	origDialog := dialogMessage
	defer func() { dialogMessage = origDialog }()
	dialogMessage = func(opts mygo.MessageOptions) (mygo.MessageResult, error) {
		return mygo.MessageResult{Button: 1}, nil
	}

	a, w := setupRSSTestApp(t)
	ctx := context.Background()
	now := time.Now().UnixMilli()

	// The folder's own list names one feed only; the second names the folder.
	group := core.RSSGroup{ID: "group-shelf", Name: "Shelf", FeedIDs: []string{"feed-listed"}, UpdatedAt: now}
	if err := a.Store.SaveRSSGroup(ctx, group); err != nil {
		t.Fatalf("save the folder: %v", err)
	}
	feeds := []core.RSSFeed{
		{ID: "feed-listed", FeedURL: "https://feeds.example.com/listed.xml", Title: "Feed Listed", GroupIDs: []string{"group-shelf"}, UpdatedAt: now},
		{ID: "feed-named", FeedURL: "https://feeds.example.com/named.xml", Title: "Feed Named", GroupIDs: []string{"group-shelf"}, UpdatedAt: now},
	}
	for _, f := range feeds {
		if err := a.Store.SaveRSSFeed(ctx, f); err != nil {
			t.Fatalf("save feed %q: %v", f.ID, err)
		}
	}
	articles := []core.RSSArticle{
		{ID: "art-listed", FeedID: "feed-listed", Title: "Listed story", Link: "https://feeds.example.com/listed/1", PublishedAt: time.Now().Add(-time.Hour).Format(time.RFC3339), UpdatedAt: now},
		{ID: "art-named", FeedID: "feed-named", Title: "Named story", Link: "https://feeds.example.com/named/1", PublishedAt: time.Now().Add(-2 * time.Hour).Format(time.RFC3339), UpdatedAt: now},
	}
	if err := a.Store.SaveRSSArticles(ctx, articles); err != nil {
		t.Fatalf("save articles: %v", err)
	}
	if err := a.ReloadRSS(); err != nil {
		t.Fatalf("reload rss: %v", err)
	}
	a.Selected = "all"
	w.selectWorkspace(WorkspaceRSS)
	tt := ui.NewTester(w.shell, 1280, 808)
	tt.Frame()

	// The folder draws both feeds before it is deleted.
	if !tt.HasText("Feed Listed") || !tt.HasText("Feed Named") {
		t.Fatalf("the folder does not draw both feeds: %v", tt.Texts())
	}
	saveRSSReadStateScreenshot(t, tt, "23-folder-delete-before.png")

	if err := tt.RightClick("Shelf"); err != nil {
		t.Fatalf("open the folder's menu: %v", err)
	}
	if err := tt.ChooseMenuItem("Delete Group"); err != nil {
		t.Fatalf("choose Delete Group: %v", err)
	}
	tt.Frame()

	if a.RSSGroupByID(group.ID).ID != "" {
		t.Fatalf("the deleted folder is still in the library")
	}
	for _, f := range feeds {
		stored, err := a.Store.GetRSSFeed(ctx, f.ID)
		if err != nil || stored == nil {
			t.Fatalf("read %q back: %v", f.ID, err)
		}
		if len(stored.GroupIDs) != 0 {
			t.Fatalf("feed %q still names the deleted folder: %v", f.ID, stored.GroupIDs)
		}
	}
	texts := tt.Texts()
	if tt.HasText("Shelf") {
		t.Fatalf("the deleted folder is still drawn: %v", texts)
	}
	if !tt.HasText("Ungrouped") {
		t.Fatalf("the sidebar draws no Ungrouped section: %v", texts)
	}
	ungrouped := make([]string, 0, 2)
	for _, f := range w.rssUngroupedFeeds() {
		ungrouped = append(ungrouped, f.Title)
	}
	slices.Sort(ungrouped)
	if !slices.Equal(ungrouped, []string{"Feed Listed", "Feed Named"}) {
		t.Fatalf("the Ungrouped section holds %q, want both feeds the folder drew", ungrouped)
	}
	if !tt.HasText("Feed Listed") || !tt.HasText("Feed Named") {
		t.Fatalf("a feed the folder drew vanished from the sidebar: %v", texts)
	}
	saveRSSReadStateScreenshot(t, tt, "24-folder-delete-after-ungrouped.png")
}
