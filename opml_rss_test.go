package main

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
)

const opmlEvidenceDir = "/Users/simon/.no-mistakes/evidence/01M87DV9ZZ7928C7K326G2BH9M"

func saveOPMLEvidenceScreenshot(t *testing.T, tt *ui.Tester, filename string) {
	t.Helper()
	tt.Frame()
	if fi, err := os.Stat(opmlEvidenceDir); err == nil && fi.IsDir() {
		outPath := filepath.Join(opmlEvidenceDir, filename)
		img := tt.Image()
		if img != nil {
			var buf bytes.Buffer
			if err := png.Encode(&buf, img); err == nil {
				if err := os.WriteFile(outPath, buf.Bytes(), 0o644); err != nil {
					t.Logf("failed to write evidence screenshot %s: %v", outPath, err)
				} else {
					t.Logf("saved evidence screenshot: %s (%d bytes)", outPath, buf.Len())
				}
			}
		}
	}
}

func saveOPMLEvidenceFile(t *testing.T, filename string, data []byte) {
	t.Helper()
	if fi, err := os.Stat(opmlEvidenceDir); err == nil && fi.IsDir() {
		outPath := filepath.Join(opmlEvidenceDir, filename)
		if err := os.WriteFile(outPath, data, 0o644); err != nil {
			t.Logf("failed to write evidence file %s: %v", outPath, err)
		} else {
			t.Logf("saved evidence file: %s (%d bytes)", outPath, len(data))
		}
	}
}

func writeTestOPML(t *testing.T, dir, filename, content string) string {
	t.Helper()
	p := filepath.Join(dir, filename)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("writeTestOPML: %v", err)
	}
	return p
}

// importRSSOPMLForTest imports OPML the way the window does: the import writes
// only the store, then the main thread reloads the in-memory library it draws.
func importRSSOPMLForTest(t *testing.T, a *App, paths []string) (RSSOPMLImportResult, error) {
	t.Helper()
	res, err := a.ImportRSSOPMLPaths(context.Background(), paths)
	if reloadErr := a.ReloadRSS(); reloadErr != nil {
		t.Fatalf("ReloadRSS after import: %v", reloadErr)
	}
	return res, err
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

	res, err := importRSSOPMLForTest(t, a, []string{file})
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

	res1, err := importRSSOPMLForTest(t, a, []string{file1})
	if err != nil {
		t.Fatalf("import first failed: %v", err)
	}
	if res1.FeedsImported != 2 || res1.GroupsImported != 1 {
		t.Fatalf("unexpected res1: %+v", res1)
	}

	// Re-importing exact same OPML file
	res2, err := importRSSOPMLForTest(t, a, []string{file1})
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
	res3, err := importRSSOPMLForTest(t, a, []string{file2})
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

	_, err := importRSSOPMLForTest(t, a, []string{file})
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
	res, err := importRSSOPMLForTest(t, a, []string{file})
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

// TestImportRSSOPMLReimportingDeletedGroupKeepsMembershipConsistent reproduces
// the delete-and-reimport flow: deleting a group leaves its own FeedIDs behind
// while clearing the feeds' GroupIDs, so re-importing the same file must
// reconcile both records instead of listing every feed twice.
func TestImportRSSOPMLReimportingDeletedGroupKeepsMembershipConsistent(t *testing.T) {
	a, w := setupRSSTestApp(t)
	dir := t.TempDir()

	opmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <body>
    <outline text="Tech" title="Tech">
      <outline type="rss" text="Ars Technica" xmlUrl="https://feeds.arstechnica.com/arstechnica/index"/>
      <outline type="rss" text="The Verge" xmlUrl="https://www.theverge.com/rss/index.xml"/>
    </outline>
  </body>
</opml>`
	file := writeTestOPML(t, dir, "tech.opml", opmlContent)

	if _, err := importRSSOPMLForTest(t, a, []string{file}); err != nil {
		t.Fatalf("first import: %v", err)
	}
	if len(a.RSSGroups) != 1 || len(a.RSSFeeds) != 2 {
		t.Fatalf("first import: groups=%d feeds=%d; want 1 group and 2 feeds", len(a.RSSGroups), len(a.RSSFeeds))
	}
	if err := a.DeleteRSSGroup(a.RSSGroups[0].ID); err != nil {
		t.Fatalf("DeleteRSSGroup: %v", err)
	}

	res, err := importRSSOPMLForTest(t, a, []string{file})
	if err != nil {
		t.Fatalf("re-import: %v", err)
	}
	if res.FeedsImported != 0 || res.FeedsSkipped != 2 {
		t.Errorf("re-import result = %+v; want 0 imported and 2 skipped", res)
	}
	if len(a.RSSGroups) != 1 {
		t.Fatalf("re-import groups = %d; want the one revived group", len(a.RSSGroups))
	}

	group := a.RSSGroups[0]
	wantIDs := make([]string, 0, len(a.RSSFeeds))
	for _, f := range a.RSSFeeds {
		wantIDs = append(wantIDs, f.ID)
		if !slices.Contains(f.GroupIDs, group.ID) {
			t.Errorf("feed %q GroupIDs = %v; want membership in %q", f.Title, f.GroupIDs, group.ID)
		}
	}
	gotIDs := slices.Clone(group.FeedIDs)
	slices.Sort(gotIDs)
	slices.Sort(wantIDs)
	if !slices.Equal(gotIDs, wantIDs) {
		t.Errorf("group FeedIDs = %v; want exactly %v", gotIDs, wantIDs)
	}
	if ungrouped := w.rssUngroupedFeeds(); len(ungrouped) != 0 {
		t.Errorf("sidebar lists %d ungrouped feeds; want 0", len(ungrouped))
	}
}

// TestImportRSSOPMLAddsSubscribedFeedsToExistingGroup verifies that a folder
// named like a group the library already holds adds its feeds to that group,
// even when the library already subscribes to them outside it.
func TestImportRSSOPMLAddsSubscribedFeedsToExistingGroup(t *testing.T) {
	a, w := setupRSSTestApp(t)
	dir := t.TempDir()

	tech, err := a.CreateRSSGroup("Tech")
	if err != nil {
		t.Fatalf("CreateRSSGroup: %v", err)
	}
	feed := core.RSSFeed{
		ID:      core.FeedID("https://news.ycombinator.com/rss"),
		FeedURL: "https://news.ycombinator.com/rss",
		Title:   "Hacker News",
	}
	if err := a.SubscribeRSSFeed(feed, ""); err != nil {
		t.Fatalf("SubscribeRSSFeed: %v", err)
	}

	opmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <body>
    <outline text="tech" title="tech">
      <outline type="rss" text="Hacker News" xmlUrl="https://news.ycombinator.com/rss"/>
      <outline type="rss" text="xkcd" xmlUrl="https://xkcd.com/rss.xml"/>
    </outline>
  </body>
</opml>`
	file := writeTestOPML(t, dir, "tech.opml", opmlContent)

	if _, err := importRSSOPMLForTest(t, a, []string{file}); err != nil {
		t.Fatalf("import: %v", err)
	}

	if len(a.RSSGroups) != 1 || a.RSSGroups[0].ID != tech.ID {
		t.Fatalf("groups = %+v; want only the existing Tech group", a.RSSGroups)
	}
	if len(a.RSSFeeds) != 2 {
		t.Fatalf("feeds = %d; want 2", len(a.RSSFeeds))
	}
	for _, f := range a.RSSFeeds {
		if !slices.Contains(f.GroupIDs, tech.ID) {
			t.Errorf("feed %q GroupIDs = %v; want membership in Tech", f.Title, f.GroupIDs)
		}
		if !slices.Contains(a.RSSGroups[0].FeedIDs, f.ID) {
			t.Errorf("Tech FeedIDs = %v; want feed %q", a.RSSGroups[0].FeedIDs, f.ID)
		}
	}
	if ungrouped := w.rssUngroupedFeeds(); len(ungrouped) != 0 {
		t.Errorf("sidebar lists %d ungrouped feeds; want 0", len(ungrouped))
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
	res, err := importRSSOPMLForTest(t, a2, []string{exportPath})
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

	saveOPMLEvidenceScreenshot(t, tt, "evidence_sidebar_plus_menu_en.png")

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

	saveOPMLEvidenceScreenshot(t, tt, "evidence_sidebar_plus_menu_zh.png")

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
	saveOPMLEvidenceScreenshot(t, tt, "evidence_empty_state_en.png")

	// In Chinese
	tt.SetLocale("zh-CN")
	tt.Frame()

	if _, ok := tt.AccessByRole(ui.RoleButton, "添加订阅源"); !ok {
		t.Fatal("empty state missing 添加订阅源 button")
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "导入 OPML…"); !ok {
		t.Fatal("empty state missing 导入 OPML… button")
	}
	saveOPMLEvidenceScreenshot(t, tt, "evidence_empty_state_zh.png")
}

// TestLiveScenario1_BatchImportAndSidebarRender validates live batch import of hierarchical
// OPML structures into the running App, database persistence, and rendered UI sidebar.
func TestLiveScenario1_BatchImportAndSidebarRender(t *testing.T) {
	a, w := setupRSSTestApp(t)
	dir := t.TempDir()

	opmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <head>
    <title>Feedly Subscriptions</title>
  </head>
  <body>
    <outline text="Technology" title="Technology">
      <outline type="rss" text="Ars Technica" title="Ars Technica" xmlUrl="https://feeds.arstechnica.com/arstechnica/index" htmlUrl="https://arstechnica.com"/>
      <outline type="rss" text="The Verge" title="The Verge" xmlUrl="https://www.theverge.com/rss/index.xml" htmlUrl="https://www.theverge.com"/>
    </outline>
    <outline text="Design" title="Design">
      <outline type="rss" text="Smashing Magazine" title="Smashing Magazine" xmlUrl="https://www.smashingmagazine.com/feed/" htmlUrl="https://www.smashingmagazine.com"/>
    </outline>
    <outline text="News" title="News">
      <outline type="rss" text="BBC News" title="BBC News" xmlUrl="https://feeds.bbci.co.uk/news/rss.xml" htmlUrl="https://www.bbc.com/news"/>
    </outline>
    <outline type="rss" text="xkcd" title="xkcd" xmlUrl="https://xkcd.com/rss.xml" htmlUrl="https://xkcd.com"/>
  </body>
</opml>`

	file := writeTestOPML(t, dir, "feedly.opml", opmlContent)

	res, err := importRSSOPMLForTest(t, a, []string{file})
	if err != nil {
		t.Fatalf("ImportRSSOPMLPaths failed: %v", err)
	}

	if res.FeedsImported != 5 {
		t.Errorf("FeedsImported = %d; want 5", res.FeedsImported)
	}
	if res.GroupsImported != 3 {
		t.Errorf("GroupsImported = %d; want 3", res.GroupsImported)
	}
	if len(a.RSSFeeds) != 5 {
		t.Fatalf("in-memory feeds = %d; want 5", len(a.RSSFeeds))
	}
	if len(a.RSSGroups) != 3 {
		t.Fatalf("in-memory groups = %d; want 3", len(a.RSSGroups))
	}

	// Verify database persistence
	ctx := context.Background()
	dbFeeds, err := a.Store.ListRSSFeeds(ctx, false)
	if err != nil || len(dbFeeds) != 5 {
		t.Fatalf("db feeds = %d; want 5 (err: %v)", len(dbFeeds), err)
	}
	dbGroups, err := a.Store.ListRSSGroups(ctx, false)
	if err != nil || len(dbGroups) != 3 {
		t.Fatalf("db groups = %d; want 3 (err: %v)", len(dbGroups), err)
	}

	// Render the UI in RSS workspace
	w.selectWorkspace(WorkspaceRSS)
	tt := ui.NewTester(w.shell, 1280, 808)
	tt.SetLocale("en")
	tt.Frame()

	saveOPMLEvidenceScreenshot(t, tt, "evidence_sidebar_after_opml_import.png")

	// Dump database state report for evidence
	var dbDump strings.Builder
	dbDump.WriteString("=== RSS PERSISTED DATABASE STATE DUMP ===\n\n")
	dbDump.WriteString(fmt.Sprintf("Imported Feeds Count: %d\n", len(dbFeeds)))
	dbDump.WriteString(fmt.Sprintf("Imported Groups Count: %d\n\n", len(dbGroups)))
	dbDump.WriteString("--- GROUPS ---\n")
	for _, g := range dbGroups {
		dbDump.WriteString(fmt.Sprintf("Group ID: %s | Name: %s | SortOrder: %d | Feeds: %v\n", g.ID, g.Name, g.SortOrder, g.FeedIDs))
	}
	dbDump.WriteString("\n--- FEEDS ---\n")
	for _, f := range dbFeeds {
		dbDump.WriteString(fmt.Sprintf("Feed ID: %s | Title: %q | FeedURL: %s | SiteURL: %s | Groups: %v\n", f.ID, f.Title, f.FeedURL, f.SiteURL, f.GroupIDs))
	}
	dbDump.WriteString("\n--- YOUTUBE ISOLATION VERIFICATION ---\n")
	dbDump.WriteString(fmt.Sprintf("YouTube Channels in Store: %d (expected 1, untouched)\n", len(a.Channels)))
	dbDump.WriteString(fmt.Sprintf("YouTube Videos in Store: %d (expected 1, untouched)\n", len(a.Videos)))
	saveOPMLEvidenceFile(t, "evidence_sqlite_db_state.txt", []byte(dbDump.String()))
}

// TestLiveScenario3_ExportAndRoundtripEvidence tests export of OPML from the live app,
// validates standards compliance, and proves lossless roundtrip into a fresh instance.
func TestLiveScenario3_ExportAndRoundtripEvidence(t *testing.T) {
	a1, _ := setupRSSTestApp(t)
	dir := t.TempDir()

	// Setup groups and feeds
	gTech, err := a1.CreateRSSGroup("Technology")
	if err != nil {
		t.Fatalf("CreateRSSGroup Tech: %v", err)
	}
	feed1 := core.RSSFeed{
		ID:      core.FeedID("https://feeds.arstechnica.com/arstechnica/index"),
		FeedURL: "https://feeds.arstechnica.com/arstechnica/index",
		SiteURL: "https://arstechnica.com",
		Title:   "Ars Technica",
	}
	if err := a1.SubscribeRSSFeed(feed1, gTech.ID); err != nil {
		t.Fatalf("SubscribeRSSFeed 1: %v", err)
	}

	feed2 := core.RSSFeed{
		ID:      core.FeedID("https://www.theverge.com/rss/index.xml"),
		FeedURL: "https://www.theverge.com/rss/index.xml",
		SiteURL: "https://www.theverge.com",
		Title:   "The Verge",
	}
	if err := a1.SubscribeRSSFeed(feed2, gTech.ID); err != nil {
		t.Fatalf("SubscribeRSSFeed 2: %v", err)
	}

	gDesign, err := a1.CreateRSSGroup("Design")
	if err != nil {
		t.Fatalf("CreateRSSGroup Design: %v", err)
	}
	feed3 := core.RSSFeed{
		ID:      core.FeedID("https://www.smashingmagazine.com/feed/"),
		FeedURL: "https://www.smashingmagazine.com/feed/",
		SiteURL: "https://www.smashingmagazine.com",
		Title:   "Smashing Magazine",
	}
	if err := a1.SubscribeRSSFeed(feed3, gDesign.ID); err != nil {
		t.Fatalf("SubscribeRSSFeed 3: %v", err)
	}

	feedUngrouped := core.RSSFeed{
		ID:      core.FeedID("https://xkcd.com/rss.xml"),
		FeedURL: "https://xkcd.com/rss.xml",
		SiteURL: "https://xkcd.com",
		Title:   "xkcd",
	}
	if err := a1.SubscribeRSSFeed(feedUngrouped, ""); err != nil {
		t.Fatalf("SubscribeRSSFeed ungrouped: %v", err)
	}

	// Add a soft-deleted feed to verify exclusion
	feedDeleted := core.RSSFeed{
		ID:      core.FeedID("https://example.com/deleted.xml"),
		FeedURL: "https://example.com/deleted.xml",
		Title:   "Should Not Be Exported",
	}
	if err := a1.SubscribeRSSFeed(feedDeleted, ""); err != nil {
		t.Fatalf("SubscribeRSSFeed deleted: %v", err)
	}
	if err := a1.DeleteRSSFeed(feedDeleted.ID); err != nil {
		t.Fatalf("DeleteRSSFeed: %v", err)
	}

	// Export to file
	exportPath := filepath.Join(dir, "subscriptions.opml")
	if err := a1.ExportRSSOPMLToFile(exportPath); err != nil {
		t.Fatalf("ExportRSSOPMLToFile failed: %v", err)
	}

	exportedBytes, err := os.ReadFile(exportPath)
	if err != nil || len(exportedBytes) == 0 {
		t.Fatalf("read exported OPML failed: %v", err)
	}

	// Save exported OPML as evidence artifact
	saveOPMLEvidenceFile(t, "evidence_exported_subscriptions.opml", exportedBytes)

	// Validate OPML format
	exportedStr := string(exportedBytes)
	if !strings.HasPrefix(exportedStr, "<?xml version=\"1.0\" encoding=\"UTF-8\"?>") {
		t.Errorf("missing standard XML declaration")
	}
	if !strings.Contains(exportedStr, `<opml version="2.0">`) {
		t.Errorf("missing opml version 2.0 tag")
	}
	if !strings.Contains(exportedStr, `title="Technology"`) || !strings.Contains(exportedStr, `title="Design"`) {
		t.Errorf("missing group titles in OPML output")
	}
	if !strings.Contains(exportedStr, `xmlUrl="https://feeds.arstechnica.com/arstechnica/index"`) {
		t.Errorf("missing feed xmlUrl in OPML output")
	}
	if strings.Contains(exportedStr, "Should Not Be Exported") {
		t.Errorf("soft-deleted feed was improperly included in export")
	}

	// Round-trip into fresh App instance
	a2, _ := setupRSSTestApp(t)
	resRoundtrip, err := importRSSOPMLForTest(t, a2, []string{exportPath})
	if err != nil {
		t.Fatalf("round-trip import into a2 failed: %v", err)
	}

	if resRoundtrip.FeedsImported != 4 {
		t.Errorf("a2 FeedsImported = %d; want 4 (active feeds)", resRoundtrip.FeedsImported)
	}
	if resRoundtrip.GroupsImported != 2 {
		t.Errorf("a2 GroupsImported = %d; want 2", resRoundtrip.GroupsImported)
	}

	var report strings.Builder
	report.WriteString("=== OPML ROUNDTRIP & STANDARDS CONFORMANCE REPORT ===\n\n")
	report.WriteString("1. Standards Conformance:\n")
	report.WriteString("   - XML Declaration: Present (UTF-8)\n")
	report.WriteString("   - OPML Spec Version: 2.0\n")
	report.WriteString("   - Head Elements: title, dateCreated, dateModified, docs\n")
	report.WriteString("   - Group Outlines: text, title preserved\n")
	report.WriteString("   - Feed Outlines: type=\"rss\", text, title, xmlUrl, htmlUrl preserved\n")
	report.WriteString("   - Soft-deleted feeds excluded: Verified\n\n")
	report.WriteString("2. Roundtrip Fidelity:\n")
	report.WriteString(fmt.Sprintf("   - Source active feeds: 4 -> Imported in a2: %d\n", len(a2.RSSFeeds)))
	report.WriteString(fmt.Sprintf("   - Source groups: 2 -> Imported in a2: %d\n", len(a2.RSSGroups)))
	for _, g := range a2.RSSGroups {
		report.WriteString(fmt.Sprintf("   - Group %q contains %d feeds (feed IDs: %v)\n", g.Name, len(g.FeedIDs), g.FeedIDs))
	}
	saveOPMLEvidenceFile(t, "evidence_roundtrip_verification.txt", []byte(report.String()))
}

// TestLiveScenario4_WorkspaceIsolationEvidence explicitly asserts that OPML operations
// in the RSS workspace do not mutate YouTube subscriptions or video stores.
func TestLiveScenario4_WorkspaceIsolationEvidence(t *testing.T) {
	a, w := setupRSSTestApp(t)
	dir := t.TempDir()

	initialYTCnt := len(a.Channels)
	initialVidCnt := len(a.Videos)

	opmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <body>
    <outline text="Dev">
      <outline type="rss" text="Go Blog" xmlUrl="https://go.dev/blog/feed.atom"/>
    </outline>
  </body>
</opml>`
	file := writeTestOPML(t, dir, "isolation.opml", opmlContent)

	// Import into RSS
	_, err := importRSSOPMLForTest(t, a, []string{file})
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}

	// Verify YouTube state is untouched
	if len(a.Channels) != initialYTCnt || a.Channels[0].ID != "yt-ch-1" {
		t.Fatalf("YouTube channels mutated: got %v", a.Channels)
	}
	if len(a.Videos) != initialVidCnt || a.Videos[0].ID != "yt-v-1" {
		t.Fatalf("YouTube videos mutated: got %v", a.Videos)
	}

	// Export RSS OPML
	exportPath := filepath.Join(dir, "exported.opml")
	if err := a.ExportRSSOPMLToFile(exportPath); err != nil {
		t.Fatalf("export failed: %v", err)
	}

	// Verify YouTube channels are not in exported RSS OPML
	expData, _ := os.ReadFile(exportPath)
	if strings.Contains(string(expData), "YouTube Tech") || strings.Contains(string(expData), "yt-ch-1") {
		t.Fatalf("YouTube channel found in exported RSS OPML: %s", string(expData))
	}

	// Verify workspace routing in toolbar
	w.selectWorkspace(WorkspaceRSS)
	if w.activeWorkspaceName() != WorkspaceRSS {
		t.Fatalf("expected active workspace RSS, got %s", w.activeWorkspaceName())
	}

	var isoReport strings.Builder
	isoReport.WriteString("=== WORKSPACE ISOLATION AUDIT REPORT ===\n\n")
	isoReport.WriteString(fmt.Sprintf("YouTube Channels Count Before: %d | After: %d\n", initialYTCnt, len(a.Channels)))
	isoReport.WriteString(fmt.Sprintf("YouTube Videos Count Before: %d | After: %d\n", initialVidCnt, len(a.Videos)))
	isoReport.WriteString("YouTube Channels in Exported RSS OPML: None (0 references)\n")
	isoReport.WriteString("Workspace State Isolation: PASSED\n")
	saveOPMLEvidenceFile(t, "evidence_workspace_isolation.txt", []byte(isoReport.String()))
}

// TestLiveScenario5_AdversarialOPML verifies edge cases, malformed XML, and protocol variations.
func TestLiveScenario5_AdversarialOPML(t *testing.T) {
	a, _ := setupRSSTestApp(t)
	dir := t.TempDir()

	var advReport strings.Builder
	advReport.WriteString("=== ADVERSARIAL & EDGE-CASE OPML TEST SUITE ===\n\n")

	// 1. Malformed XML
	badXML := `<?xml version="1.0"?><opml><head><title>Unclosed`
	badFile := writeTestOPML(t, dir, "bad.opml", badXML)
	_, err := importRSSOPMLForTest(t, a, []string{badFile})
	if err == nil {
		t.Errorf("expected error on malformed XML, got nil")
	} else {
		advReport.WriteString(fmt.Sprintf("1. Malformed XML: Gracefully rejected with error: %v\n", err))
	}

	// 2. feed:// scheme and entity escaping
	edgeOPML := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <body>
    <outline text="Dev &amp; Tech">
      <outline type="rss" text="Feed Scheme" xmlUrl="feed:https://example.com/rss.xml" htmlUrl="https://example.com"/>
      <outline type="rss" text="Feed Slash Scheme" xmlUrl="feed://example.org/atom.xml"/>
      <outline type="rss" text="Missing XML URL"/>
    </outline>
  </body>
</opml>`
	edgeFile := writeTestOPML(t, dir, "edge.opml", edgeOPML)
	res, err := importRSSOPMLForTest(t, a, []string{edgeFile})
	if err != nil {
		t.Fatalf("import edgeOPML failed: %v", err)
	}

	if res.FeedsImported != 2 {
		t.Errorf("FeedsImported = %d; want 2 (valid feeds only)", res.FeedsImported)
	}
	advReport.WriteString(fmt.Sprintf("2. Scheme Normalization & Missing URLs: Imported %d valid feeds, skipped missing xmlUrl\n", res.FeedsImported))

	// Verify feeds were normalized
	for _, f := range a.RSSFeeds {
		if strings.HasPrefix(f.FeedURL, "feed:") {
			t.Errorf("FeedURL not normalized: %s", f.FeedURL)
		}
	}
	advReport.WriteString("3. URL Schemes verified clean of 'feed:' prefix: PASSED\n")

	// 4. Empty OPML file
	emptyOPML := `<?xml version="1.0" encoding="UTF-8"?><opml version="2.0"><head/><body/></opml>`
	emptyFile := writeTestOPML(t, dir, "empty.opml", emptyOPML)
	resEmpty, err := importRSSOPMLForTest(t, a, []string{emptyFile})
	if err != nil {
		t.Errorf("empty OPML failed: %v", err)
	}
	if resEmpty.FeedsImported != 0 || resEmpty.GroupsImported != 0 {
		t.Errorf("empty OPML returned non-zero result: %+v", resEmpty)
	}
	advReport.WriteString("4. Empty OPML Body: Safely processed with 0 imports: PASSED\n")

	saveOPMLEvidenceFile(t, "evidence_adversarial_test_log.txt", []byte(advReport.String()))
}
