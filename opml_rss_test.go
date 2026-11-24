package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
)

func writeTestOPML(t *testing.T, dir, filename, content string) string {
	t.Helper()
	p := filepath.Join(dir, filename)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("writeTestOPML: %v", err)
	}
	return p
}

// TestImportRSSOPMLPaths verifies basic batch import of OPML into the RSS workspace store.
func TestImportRSSOPMLPaths(t *testing.T) {
	a, _ := setupRSSTestApp(t)
	dir := t.TempDir()

	opmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <head>
    <title>Subscriptions</title>
  </head>
  <body>
    <outline text="Technology" title="Technology">
      <outline type="rss" text="Ars Technica" title="Ars Technica" xmlUrl="https://feeds.arstechnica.com/arstechnica/index" htmlUrl="https://arstechnica.com"/>
      <outline type="rss" text="The Verge" title="The Verge" xmlUrl="https://www.theverge.com/rss/index.xml" htmlUrl="https://www.theverge.com"/>
    </outline>
    <outline type="rss" text="xkcd" title="xkcd" xmlUrl="https://xkcd.com/rss.xml" htmlUrl="https://xkcd.com"/>
  </body>
</opml>`

	file := writeTestOPML(t, dir, "subs.opml", opmlContent)

	res, err := a.ImportRSSOPMLPaths(context.Background(), []string{file})
	if err != nil {
		t.Fatalf("ImportRSSOPMLPaths failed: %v", err)
	}

	if res.FeedsImported != 3 {
		t.Errorf("FeedsImported = %d; want 3", res.FeedsImported)
	}
	if res.FeedsSkipped != 0 {
		t.Errorf("FeedsSkipped = %d; want 0", res.FeedsSkipped)
	}
	if res.GroupsImported != 1 {
		t.Errorf("GroupsImported = %d; want 1", res.GroupsImported)
	}
	if len(res.NewFeedIDs) != 3 {
		t.Errorf("len(NewFeedIDs) = %d; want 3", len(res.NewFeedIDs))
	}

	// Verify in-memory state
	if len(a.RSSFeeds) != 3 {
		t.Fatalf("len(a.RSSFeeds) = %d; want 3", len(a.RSSFeeds))
	}
	if len(a.RSSGroups) != 1 {
		t.Fatalf("len(a.RSSGroups) = %d; want 1", len(a.RSSGroups))
	}

	group := a.RSSGroups[0]
	if group.Name != "Technology" {
		t.Errorf("group.Name = %q; want Technology", group.Name)
	}
	if len(group.FeedIDs) != 2 {
		t.Errorf("len(group.FeedIDs) = %d; want 2", len(group.FeedIDs))
	}

	// Verify database persistence
	ctx := context.Background()
	dbFeeds, err := a.Store.ListRSSFeeds(ctx, false)
	if err != nil || len(dbFeeds) != 3 {
		t.Fatalf("dbFeeds count = %d; want 3 (err: %v)", len(dbFeeds), err)
	}
	dbGroups, err := a.Store.ListRSSGroups(ctx, false)
	if err != nil || len(dbGroups) != 1 {
		t.Fatalf("dbGroups count = %d; want 1 (err: %v)", len(dbGroups), err)
	}

	// Verify YouTube channels were not contaminated
	if len(a.Channels) != 1 || a.Channels[0].ID != "yt-ch-1" {
		t.Errorf("YouTube channels contaminated: %+v", a.Channels)
	}
}

// TestImportRSSOPMLDeduplication verifies that existing subscriptions are not re-added,
// and groups with the same name are reused without duplication.
func TestImportRSSOPMLDeduplication(t *testing.T) {
	a, _ := setupRSSTestApp(t)
	dir := t.TempDir()

	firstOPML := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <body>
    <outline text="News">
      <outline type="rss" text="BBC" xmlUrl="https://feeds.bbci.co.uk/news/rss.xml"/>
      <outline type="rss" text="CNN" xmlUrl="https://rss.cnn.com/rss/edition.rss"/>
    </outline>
  </body>
</opml>`

	file1 := writeTestOPML(t, dir, "first.opml", firstOPML)

	res1, err := a.ImportRSSOPMLPaths(context.Background(), []string{file1})
	if err != nil {
		t.Fatalf("import first failed: %v", err)
	}
	if res1.FeedsImported != 2 || res1.GroupsImported != 1 {
		t.Fatalf("unexpected res1: %+v", res1)
	}

	// Re-importing exact same OPML file
	res2, err := a.ImportRSSOPMLPaths(context.Background(), []string{file1})
	if err != nil {
		t.Fatalf("import second failed: %v", err)
	}
	if res2.FeedsImported != 0 {
		t.Errorf("res2.FeedsImported = %d; want 0", res2.FeedsImported)
	}
	if res2.FeedsSkipped != 2 {
		t.Errorf("res2.FeedsSkipped = %d; want 2", res2.FeedsSkipped)
	}
	if res2.GroupsImported != 0 {
		t.Errorf("res2.GroupsImported = %d; want 0", res2.GroupsImported)
	}
	if len(a.RSSFeeds) != 2 || len(a.RSSGroups) != 1 {
		t.Errorf("feeds=%d, groups=%d; expected 2 feeds and 1 group", len(a.RSSFeeds), len(a.RSSGroups))
	}

	// Import third OPML with 1 duplicate feed (BBC) and 1 new feed (NPR) in same group "News"
	secondOPML := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <body>
    <outline text="News">
      <outline type="rss" text="BBC" xmlUrl="https://feeds.bbci.co.uk/news/rss.xml"/>
      <outline type="rss" text="NPR" xmlUrl="https://feeds.npr.org/1001/rss.xml"/>
    </outline>
  </body>
</opml>`

	file2 := writeTestOPML(t, dir, "second.opml", secondOPML)
	res3, err := a.ImportRSSOPMLPaths(context.Background(), []string{file2})
	if err != nil {
		t.Fatalf("import third failed: %v", err)
	}
	if res3.FeedsImported != 1 {
		t.Errorf("res3.FeedsImported = %d; want 1", res3.FeedsImported)
	}
	if res3.FeedsSkipped != 1 {
		t.Errorf("res3.FeedsSkipped = %d; want 1", res3.FeedsSkipped)
	}
	if res3.GroupsImported != 0 {
		t.Errorf("res3.GroupsImported = %d; want 0 (reused News)", res3.GroupsImported)
	}

	if len(a.RSSFeeds) != 3 {
		t.Fatalf("len(a.RSSFeeds) = %d; want 3", len(a.RSSFeeds))
	}
	if len(a.RSSGroups) != 1 {
		t.Fatalf("len(a.RSSGroups) = %d; want 1", len(a.RSSGroups))
	}

	// News group should now have all 3 feeds: BBC, CNN, NPR
	newsGroup := a.RSSGroups[0]
	if len(newsGroup.FeedIDs) != 3 {
		t.Errorf("News group feed count = %d; want 3", len(newsGroup.FeedIDs))
	}
}

// TestImportRSSOPMLRevivesSoftDeleted verifies that re-importing a soft-deleted feed revives it.
func TestImportRSSOPMLRevivesSoftDeleted(t *testing.T) {
	a, _ := setupRSSTestApp(t)
	dir := t.TempDir()

	opmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <body>
    <outline type="rss" text="Daring Fireball" xmlUrl="https://daringfireball.net/feeds/main"/>
  </body>
</opml>`
	file := writeTestOPML(t, dir, "feed.opml", opmlContent)

	_, err := a.ImportRSSOPMLPaths(context.Background(), []string{file})
	if err != nil {
		t.Fatalf("initial import failed: %v", err)
	}
	if len(a.RSSFeeds) != 1 {
		t.Fatalf("len(a.RSSFeeds) = %d; want 1", len(a.RSSFeeds))
	}
	feedID := a.RSSFeeds[0].ID

	// Delete the feed
	if err := a.DeleteRSSFeed(feedID); err != nil {
		t.Fatalf("DeleteRSSFeed failed: %v", err)
	}
	if len(a.RSSFeeds) != 0 {
		t.Fatalf("feed not deleted from in-memory state")
	}

	// Re-import OPML
	res, err := a.ImportRSSOPMLPaths(context.Background(), []string{file})
	if err != nil {
		t.Fatalf("second import failed: %v", err)
	}
	if res.FeedsImported != 1 {
		t.Errorf("res.FeedsImported = %d; want 1 (revived)", res.FeedsImported)
	}
	if len(a.RSSFeeds) != 1 {
		t.Fatalf("len(a.RSSFeeds) = %d; want 1 revived feed", len(a.RSSFeeds))
	}
}

// TestExportRSSOPMLToFileAndRoundTrip verifies export from database to file,
// followed by import into another fresh App instance.
func TestExportRSSOPMLToFileAndRoundTrip(t *testing.T) {
	a1, _ := setupRSSTestApp(t)
	dir := t.TempDir()

	// Subscribe feeds into a1
	gTech, err := a1.CreateRSSGroup("Tech")
	if err != nil {
		t.Fatalf("CreateRSSGroup failed: %v", err)
	}
	feed1 := core.RSSFeed{
		ID:      core.FeedID("https://news.ycombinator.com/rss"),
		FeedURL: "https://news.ycombinator.com/rss",
		Title:   "Hacker News",
	}
	if err := a1.SubscribeRSSFeed(feed1, gTech.ID); err != nil {
		t.Fatalf("SubscribeRSSFeed 1 failed: %v", err)
	}

	gComics, err := a1.CreateRSSGroup("Comics")
	if err != nil {
		t.Fatalf("CreateRSSGroup Comics failed: %v", err)
	}
	feed2 := core.RSSFeed{
		ID:      core.FeedID("https://xkcd.com/rss.xml"),
		FeedURL: "https://xkcd.com/rss.xml",
		Title:   "xkcd",
	}
	if err := a1.SubscribeRSSFeed(feed2, gComics.ID); err != nil {
		t.Fatalf("SubscribeRSSFeed 2 failed: %v", err)
	}

	feedUngrouped := core.RSSFeed{
		ID:      core.FeedID("https://daringfireball.net/feeds/main"),
		FeedURL: "https://daringfireball.net/feeds/main",
		Title:   "Daring Fireball",
	}
	if err := a1.SubscribeRSSFeed(feedUngrouped, ""); err != nil {
		t.Fatalf("SubscribeRSSFeed ungrouped failed: %v", err)
	}

	// Export to file
	exportPath := filepath.Join(dir, "exported.opml")
	if err := a1.ExportRSSOPMLToFile(exportPath); err != nil {
		t.Fatalf("ExportRSSOPMLToFile failed: %v", err)
	}

	data, err := os.ReadFile(exportPath)
	if err != nil || len(data) == 0 {
		t.Fatalf("exported file empty or unreadable: %v", err)
	}

	// Create a second fresh App instance
	a2, _ := setupRSSTestApp(t)
	if len(a2.RSSFeeds) != 0 || len(a2.RSSGroups) != 0 {
		t.Fatalf("a2 not clean")
	}

	// Import into a2
	res, err := a2.ImportRSSOPMLPaths(context.Background(), []string{exportPath})
	if err != nil {
		t.Fatalf("Import into a2 failed: %v", err)
	}
	if res.FeedsImported != 3 {
		t.Errorf("a2 FeedsImported = %d; want 3", res.FeedsImported)
	}
	if res.GroupsImported != 2 {
		t.Errorf("a2 GroupsImported = %d; want 2", res.GroupsImported)
	}

	if len(a2.RSSFeeds) != 3 {
		t.Fatalf("a2.RSSFeeds = %d; want 3", len(a2.RSSFeeds))
	}
	if len(a2.RSSGroups) != 2 {
		t.Fatalf("a2.RSSGroups = %d; want 2", len(a2.RSSGroups))
	}

	// Verify group names and feed assignments in a2
	techFound := false
	comicsFound := false
	for _, g := range a2.RSSGroups {
		if g.Name == "Tech" {
			techFound = true
			if len(g.FeedIDs) != 1 {
				t.Errorf("Tech group has %d feeds; want 1", len(g.FeedIDs))
			}
		} else if g.Name == "Comics" {
			comicsFound = true
			if len(g.FeedIDs) != 1 {
				t.Errorf("Comics group has %d feeds; want 1", len(g.FeedIDs))
			}
		}
	}
	if !techFound || !comicsFound {
		t.Errorf("missing expected groups in a2: %+v", a2.RSSGroups)
	}
}

// TestRSSSidebarMenuOPMLEntries verifies that the RSS sidebar footer plus menu
// includes "Import OPML…" and "Export OPML…" entries with correct bilingual labels.
func TestRSSSidebarMenuOPMLEntries(t *testing.T) {
	_, w := setupRSSTestApp(t)
	w.selectWorkspace(WorkspaceRSS)

	tt := ui.NewTester(w.shell, 1280, 808)
	tt.SetLocale("en")
	tt.Frame()

	// Click plus button in RSS sidebar
	if err := tt.Click("New group or RSS subscription"); err != nil {
		t.Fatalf("click rss sidebar add button: %v", err)
	}

	menu := tt.Menu()
	for _, want := range []string{"New Group", "Add Subscription…", "Import OPML…", "Export OPML…"} {
		if !slices.Contains(menu, want) {
			t.Errorf("en: plus menu missing %q, got: %v", want, menu)
		}
	}

	// Test Chinese locale
	tt.Key(0, ui.KeyEscape)
	tt.SetLocale("zh-CN")
	tt.Frame()

	if err := tt.Click("New group or RSS subscription"); err != nil {
		t.Fatalf("click rss sidebar add button (zh): %v", err)
	}

	menuZH := tt.Menu()
	for _, want := range []string{"新建分组", "新增订阅…", "导入 OPML…", "导出 OPML…"} {
		if !slices.Contains(menuZH, want) {
			t.Errorf("zh: plus menu missing %q, got: %v", want, menuZH)
		}
	}
}

// TestRSSEmptyStateImportButton verifies that when there are no RSS subscriptions,
// an "Import OPML…" action button is rendered in the empty state.
func TestRSSEmptyStateImportButton(t *testing.T) {
	_, w := setupRSSTestApp(t)
	w.selectWorkspace(WorkspaceRSS)

	tt := ui.NewTester(w.shell, 1280, 808)
	tt.SetLocale("en")
	tt.Frame()

	if _, ok := tt.AccessByRole(ui.RoleButton, "Add RSS Feed"); !ok {
		t.Fatal("empty state missing Add RSS Feed button")
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "Import OPML…"); !ok {
		t.Fatal("empty state missing Import OPML… button")
	}

	// In Chinese
	tt.SetLocale("zh-CN")
	tt.Frame()

	if _, ok := tt.AccessByRole(ui.RoleButton, "添加订阅源"); !ok {
		t.Fatal("empty state missing 添加订阅源 button")
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "导入 OPML…"); !ok {
		t.Fatal("empty state missing 导入 OPML… button")
	}
}
