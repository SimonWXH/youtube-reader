package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
)

// rssShortcutFixture seeds count articles into one feed, newest first, and
// returns a window on the RSS workspace with its first frame drawn. The
// articles are named Art-00 upward by publication, so the list's order is the
// index order and a test can name the article a move should reach.
func rssShortcutFixture(t *testing.T, count int) (*App, *Window, *ui.Tester) {
	t.Helper()
	dir := t.TempDir()
	a := newTestApp(t, dir)
	t.Cleanup(func() { a.Close() })
	ctx := context.Background()

	now := time.Now().UnixMilli()
	feed := core.RSSFeed{
		ID:        "feed-shortcuts",
		FeedURL:   "https://feeds.example.com/rss.xml",
		Title:     "Shortcut Feed",
		SiteURL:   "https://feeds.example.com",
		UpdatedAt: now,
	}
	if err := a.Store.SaveRSSFeed(ctx, feed); err != nil {
		t.Fatalf("save feed: %v", err)
	}
	articles := make([]core.RSSArticle, 0, count)
	for i := 0; i < count; i++ {
		articles = append(articles, core.RSSArticle{
			ID:          fmt.Sprintf("art-%02d", i),
			FeedID:      feed.ID,
			Title:       fmt.Sprintf("Article %02d", i),
			Link:        fmt.Sprintf("https://feeds.example.com/article-%02d", i),
			Snippet:     "A snippet long enough to fill both summary lines of the row with words.",
			PublishedAt: time.Now().Add(-time.Duration(i) * time.Hour).Format(time.RFC3339),
			UpdatedAt:   now,
		})
	}
	if err := a.Store.SaveRSSArticles(ctx, articles); err != nil {
		t.Fatalf("save articles: %v", err)
	}
	if err := a.ReloadRSS(); err != nil {
		t.Fatalf("reload rss: %v", err)
	}

	w := &Window{app: a, selectedIndex: new(int)}
	w.selectWorkspace(WorkspaceRSS)
	tt := ui.NewTester(w.shell, 1280, 808)
	tt.Frame()
	return a, w, tt
}

// TestRSSKeyboardStepsSelection walks the article column with J and K, from
// nothing chosen to the last entry and back, and checks that each move loads
// the article in the reader and scrolls the list so the chosen row stays in
// view.
func TestRSSKeyboardStepsSelection(t *testing.T) {
	a, w, tt := rssShortcutFixture(t, 30)

	if a.SelectedRSSArticleID != "" {
		t.Fatalf("nothing is chosen on a fresh page, got %q", a.SelectedRSSArticleID)
	}

	// J with nothing chosen takes the first entry, and the reader opens it.
	tt.Key(0, ui.KeyJ)
	tt.Frame()
	if a.SelectedRSSArticleID != "art-00" {
		t.Fatalf("J from nothing chose %q, want art-00", a.SelectedRSSArticleID)
	}
	if w.rssReader.articleID != "art-00" {
		t.Fatalf("reader shows %q, want the article J chose", w.rssReader.articleID)
	}

	// J walks down the column, one entry per press.
	for i := 1; i <= 12; i++ {
		tt.Key(0, ui.KeyJ)
		tt.Frame()
		want := fmt.Sprintf("art-%02d", i)
		if a.SelectedRSSArticleID != want {
			t.Fatalf("after %d presses of J, selection = %q, want %q", i+1, a.SelectedRSSArticleID, want)
		}
		if w.rssReader.articleID != want {
			t.Fatalf("reader shows %q, want %q", w.rssReader.articleID, want)
		}
	}

	// The list followed: it is scrolled, and the chosen row sits inside it.
	if scroll := w.workspaceRSSArticlesScroll(); scroll.Y <= 0 {
		t.Fatalf("the list did not scroll to follow the selection: Y = %v", scroll.Y)
	}
	list, ok := tt.Find("Articles")
	if !ok {
		t.Fatal("article list not found")
	}
	row, ok := tt.Find("Article 12")
	if !ok {
		t.Fatal("the chosen row is not in the list's built rows")
	}
	if row.Y < list.Y-1 || row.Y+row.H > list.Y+list.H+1 {
		t.Fatalf("chosen row %v is outside the list %v", row, list)
	}
	if err := tt.Click("Article 12"); err != nil {
		t.Fatalf("the chosen row is not reachable in the viewport: %v", err)
	}

	// K walks back up, one entry per press, and back to the first.
	tt.Key(0, ui.KeyK)
	tt.Frame()
	if a.SelectedRSSArticleID != "art-11" {
		t.Fatalf("K chose %q, want art-11", a.SelectedRSSArticleID)
	}
	for i := 0; i < 20; i++ {
		tt.Key(0, ui.KeyK)
		tt.Frame()
	}
	if a.SelectedRSSArticleID != "art-00" {
		t.Fatalf("K past the top chose %q, want the first entry", a.SelectedRSSArticleID)
	}

	// J stops at the last entry rather than running past it.
	for i := 0; i < 40; i++ {
		tt.Key(0, ui.KeyJ)
		tt.Frame()
	}
	if a.SelectedRSSArticleID != "art-29" {
		t.Fatalf("J past the bottom chose %q, want the last entry", a.SelectedRSSArticleID)
	}
}

// TestRSSKeyboardStepsKeepTheAnchor checks a step still lands beside the open
// article after M or S hides it from the filtered column, instead of jumping
// back to the top of the list.
func TestRSSKeyboardStepsKeepTheAnchor(t *testing.T) {
	a, w, tt := rssShortcutFixture(t, 6)
	a.UnreadOnly = true
	tt.Frame()

	for i := 0; i < 3; i++ {
		tt.Key(0, ui.KeyJ)
		tt.Frame()
	}
	if a.SelectedRSSArticleID != "art-02" {
		t.Fatalf("chosen article = %q, want art-02", a.SelectedRSSArticleID)
	}

	// M hides the open article from the unread column while it stays open.
	tt.Key(0, ui.KeyM)
	tt.Frame()
	if a.SelectedRSSArticleID != "art-02" {
		t.Fatalf("marking read changed the open article to %q", a.SelectedRSSArticleID)
	}
	if visible := a.VisibleRSSArticles(); len(visible) != 5 {
		t.Fatalf("the unread column holds %d articles, want 5", len(visible))
	}

	// J carries on with the article after the hidden one, not the top entry.
	tt.Key(0, ui.KeyJ)
	tt.Frame()
	if a.SelectedRSSArticleID != "art-03" {
		t.Fatalf("J after the open article was hidden chose %q, want art-03", a.SelectedRSSArticleID)
	}
	if w.rssReader.articleID != "art-03" {
		t.Fatalf("reader shows %q, want art-03", w.rssReader.articleID)
	}

	tt.Key(0, ui.KeyM)
	tt.Frame()
	if visible := a.VisibleRSSArticles(); len(visible) != 4 {
		t.Fatalf("the unread column holds %d articles, want 4", len(visible))
	}

	// K goes back to the article before both hidden ones.
	tt.Key(0, ui.KeyK)
	tt.Frame()
	if a.SelectedRSSArticleID != "art-01" {
		t.Fatalf("K after the open article was hidden chose %q, want art-01", a.SelectedRSSArticleID)
	}

	// The arrow keys share the move and keep the same anchor.
	if err := tt.Click("Article 01"); err != nil {
		t.Fatalf("focus the open row: %v", err)
	}
	tt.Frame()
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if a.SelectedRSSArticleID != "art-04" {
		t.Fatalf("the down arrow chose %q, want art-04", a.SelectedRSSArticleID)
	}
}

// TestRSSKeyboardStarKeepsTheAnchorInStarred checks the Starred destination
// keeps the same anchor when S unstars the open row.
func TestRSSKeyboardStarKeepsTheAnchorInStarred(t *testing.T) {
	a, _, tt := rssShortcutFixture(t, 4)
	for _, id := range []string{"art-00", "art-01", "art-02"} {
		if err := a.SetRSSArticleStarred(id, true); err != nil {
			t.Fatalf("star %s: %v", id, err)
		}
	}
	if err := tt.Click("Starred"); err != nil {
		t.Fatalf("open the Starred destination: %v", err)
	}
	tt.Frame()

	tt.Key(0, ui.KeyJ)
	tt.Frame()
	tt.Key(0, ui.KeyJ)
	tt.Frame()
	if a.SelectedRSSArticleID != "art-01" {
		t.Fatalf("chosen starred article = %q, want art-01", a.SelectedRSSArticleID)
	}

	tt.Key(0, ui.KeyS)
	tt.Frame()
	if visible := a.VisibleRSSArticles(); len(visible) != 2 {
		t.Fatalf("the Starred column holds %d articles, want 2", len(visible))
	}

	tt.Key(0, ui.KeyJ)
	tt.Frame()
	if a.SelectedRSSArticleID != "art-02" {
		t.Fatalf("J after unstarring the open row chose %q, want art-02", a.SelectedRSSArticleID)
	}

	tt.Key(0, ui.KeyK)
	tt.Frame()
	if a.SelectedRSSArticleID != "art-00" {
		t.Fatalf("K chose %q, want art-00", a.SelectedRSSArticleID)
	}
}

// TestRSSKeyboardTogglesTakeThePressAlone checks holding M, S or Cmd+O acts
// once, while J and K still step once per auto-repeat.
func TestRSSKeyboardTogglesTakeThePressAlone(t *testing.T) {
	a, _, tt := rssShortcutFixture(t, 6)

	tt.Key(0, ui.KeyJ)
	tt.Frame()

	tt.Key(0, ui.KeyM)
	tt.Frame()
	for i := 0; i < 3; i++ {
		tt.KeyRepeat(0, ui.KeyM)
		tt.Frame()
	}
	if art := a.SelectedRSSArticle(); art == nil || !art.Read {
		t.Fatalf("the auto-repeats flipped the article back to unread: %+v", art)
	}
	if a.RSSTotalUnread != 5 {
		t.Fatalf("unread total after holding M = %d, want 5", a.RSSTotalUnread)
	}

	tt.Key(0, ui.KeyS)
	tt.Frame()
	for i := 0; i < 3; i++ {
		tt.KeyRepeat(0, ui.KeyS)
		tt.Frame()
	}
	if art := a.SelectedRSSArticle(); art == nil || !art.Starred {
		t.Fatalf("the auto-repeats removed the star: %+v", art)
	}
	if a.RSSStarredTotal != 1 {
		t.Fatalf("starred total after holding S = %d, want 1", a.RSSStarredTotal)
	}

	tt.Key(ui.Cmd, ui.KeyO)
	tt.Frame()
	for i := 0; i < 3; i++ {
		tt.KeyRepeat(ui.Cmd, ui.KeyO)
		tt.Frame()
	}
	if opened := tt.OpenedURLs(); len(opened) != 1 {
		t.Fatalf("holding Cmd+O opened %v, want one link", opened)
	}

	// J steps once per repeat, as stepping while the key is held should.
	tt.KeyDown(ui.KeyJ)
	tt.Frame()
	tt.KeyRepeat(0, ui.KeyJ)
	tt.Frame()
	tt.KeyRepeat(0, ui.KeyJ)
	tt.Frame()
	tt.KeyUp(ui.KeyJ)
	tt.Frame()
	if a.SelectedRSSArticleID != "art-03" {
		t.Fatalf("three J presses chose %q, want art-03", a.SelectedRSSArticleID)
	}
}

// TestRSSKeyboardTogglesRead checks M flips the chosen article between read
// and unread, that the store keeps it, and that the header's unread count and
// the row's unread dot follow at once.
func TestRSSKeyboardTogglesRead(t *testing.T) {
	a, _, tt := rssShortcutFixture(t, 3)
	ctx := context.Background()

	if a.RSSTotalUnread != 3 {
		t.Fatalf("fresh unread total = %d, want 3", a.RSSTotalUnread)
	}
	tt.Key(0, ui.KeyJ)
	tt.Frame()
	if !tt.HasText("3 unread") {
		t.Fatalf("choosing an article leaves it unread, so the header keeps 3 unread, texts = %v", tt.Texts())
	}
	if _, ok := tt.Find("Unread: Article 00"); !ok {
		t.Fatal("the chosen unread article shows no unread dot")
	}

	tt.Key(0, ui.KeyM)
	tt.Frame()
	art := a.SelectedRSSArticle()
	if art == nil || !art.Read || art.ReadAt == 0 {
		t.Fatalf("M left the article unread: %+v", art)
	}
	if a.RSSTotalUnread != 2 {
		t.Fatalf("unread total after M = %d, want 2", a.RSSTotalUnread)
	}
	if !tt.HasText("2 unread") {
		t.Fatalf("header should show 2 unread, texts = %v", tt.Texts())
	}
	if _, ok := tt.Find("Unread: Article 00"); ok {
		t.Fatal("a read article still shows the unread dot")
	}
	stored, err := a.Store.GetRSSArticle(ctx, "art-00")
	if err != nil || stored == nil {
		t.Fatalf("read the article back: %v", err)
	}
	if !stored.Read || stored.ReadAt == 0 {
		t.Fatalf("the store kept the article unread: %+v", stored)
	}

	// A relaunch reads the same state from SQLite.
	if err := a.ReloadRSS(); err != nil {
		t.Fatalf("reload rss: %v", err)
	}
	tt.Frame()
	if art := a.SelectedRSSArticle(); art == nil || !art.Read {
		t.Fatalf("the reloaded article is unread: %+v", art)
	}

	// M again returns it to unread.
	tt.Key(0, ui.KeyM)
	tt.Frame()
	if art := a.SelectedRSSArticle(); art == nil || art.Read || art.ReadAt != 0 {
		t.Fatalf("M did not return the article to unread: %+v", art)
	}
	if a.RSSTotalUnread != 3 {
		t.Fatalf("unread total after the second M = %d, want 3", a.RSSTotalUnread)
	}
	stored, err = a.Store.GetRSSArticle(ctx, "art-00")
	if err != nil || stored == nil {
		t.Fatalf("read the article back: %v", err)
	}
	if stored.Read {
		t.Fatalf("the store kept the article read: %+v", stored)
	}
}

// TestRSSKeyboardTogglesStar covers S and the star control: the header button
// and the row's mark show the state, the store keeps it, the sidebar's Starred
// count follows, and the Starred destination lists exactly the starred
// articles.
func TestRSSKeyboardTogglesStar(t *testing.T) {
	a, _, tt := rssShortcutFixture(t, 3)
	ctx := context.Background()

	tt.Key(0, ui.KeyJ)
	tt.Frame()

	// The header offers the star before there is one.
	if _, ok := tt.Find("Star article (S)"); !ok {
		t.Fatalf("the reader header has no star control, texts = %v", tt.Texts())
	}
	if a.RSSStarredTotal != 0 {
		t.Fatalf("fresh starred total = %d, want 0", a.RSSStarredTotal)
	}

	tt.Key(0, ui.KeyS)
	tt.Frame()
	art := a.SelectedRSSArticle()
	if art == nil || !art.Starred || art.StarredAt == 0 {
		t.Fatalf("S left the article unstarred: %+v", art)
	}
	if a.RSSStarredTotal != 1 {
		t.Fatalf("starred total after S = %d, want 1", a.RSSStarredTotal)
	}
	stored, err := a.Store.GetRSSArticle(ctx, "art-00")
	if err != nil || stored == nil {
		t.Fatalf("read the article back: %v", err)
	}
	if !stored.Starred || stored.StarredAt == 0 {
		t.Fatalf("the store kept the article unstarred: %+v", stored)
	}
	if _, ok := tt.Find("Starred: Article 00"); !ok {
		t.Fatalf("the starred row shows no star mark, texts = %v", tt.Texts())
	}
	if _, ok := tt.Find("Unstar article (S)"); !ok {
		t.Fatalf("the header does not show the starred state, texts = %v", tt.Texts())
	}

	// The sidebar's Starred entry lists only the starred article.
	if err := tt.Click("Starred"); err != nil {
		t.Fatalf("open the Starred destination: %v", err)
	}
	tt.Frame()
	if a.Selected != "starred" {
		t.Fatalf("destination = %q, want starred", a.Selected)
	}
	visible := a.VisibleRSSArticles()
	if len(visible) != 1 || visible[0].ID != "art-00" {
		t.Fatalf("Starred lists %v, want only art-00", visible)
	}
	if !tt.HasText("Article 00") {
		t.Fatal("the starred article is missing from the list")
	}
	for _, other := range []string{"Article 01", "Article 02"} {
		if tt.HasText(other) {
			t.Fatalf("the Starred destination lists unstarred %q", other)
		}
	}

	// The header's star button removes the star, which empties the destination
	// while the reader keeps the article open.
	if err := tt.Click("Unstar article (S)"); err != nil {
		t.Fatalf("click the header star: %v", err)
	}
	tt.Frame()
	if art := a.SelectedRSSArticle(); art == nil || art.Starred {
		t.Fatalf("the header star left the article starred: %+v", art)
	}
	if a.RSSStarredTotal != 0 {
		t.Fatalf("starred total after unstarring = %d, want 0", a.RSSStarredTotal)
	}
	if !tt.HasText("No Articles") {
		t.Fatalf("the empty Starred destination should say so, texts = %v", tt.Texts())
	}
	stored, err = a.Store.GetRSSArticle(ctx, "art-00")
	if err != nil || stored == nil || stored.Starred || stored.StarredAt != 0 {
		t.Fatalf("the store kept the star: %v %+v", err, stored)
	}

	// S on the still-open article stars it again, and it returns to the list.
	tt.Key(0, ui.KeyS)
	tt.Frame()
	if art := a.SelectedRSSArticle(); art == nil || !art.Starred {
		t.Fatalf("S did not star the open article again: %+v", art)
	}
	if !tt.HasText("Article 00") {
		t.Fatal("the re-starred article is missing from the Starred list")
	}
}

// TestRSSKeyboardOpensTheArticleInBrowser checks Cmd+O hands the open
// article's own link to the system browser, and does nothing without one.
func TestRSSKeyboardOpensTheArticleInBrowser(t *testing.T) {
	_, _, tt := rssShortcutFixture(t, 2)

	tt.Key(ui.Cmd, ui.KeyO)
	tt.Frame()
	if opened := tt.OpenedURLs(); len(opened) != 0 {
		t.Fatalf("Cmd+O opened %v with no article chosen", opened)
	}

	tt.Key(0, ui.KeyJ)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyO)
	tt.Frame()
	opened := tt.OpenedURLs()
	if len(opened) != 1 || opened[0] != "https://feeds.example.com/article-00" {
		t.Fatalf("Cmd+O opened %v, want the chosen article's link", opened)
	}
}

// TestRSSSingleKeyShortcutsLeaveTypingAlone checks J, K, M and S stay letters
// while the article search field has the focus: the text arrives, and no
// article is chosen, marked, or starred behind the field.
func TestRSSSingleKeyShortcutsLeaveTypingAlone(t *testing.T) {
	a, _, tt := rssShortcutFixture(t, 3)

	if err := tt.Click("Search articles"); err != nil {
		t.Fatalf("focus the article search field: %v", err)
	}
	tt.Frame()
	if !tt.Focused("Search articles") {
		t.Fatal("the article search field did not take the focus")
	}

	for _, typed := range []struct {
		key  ui.Key
		text string
	}{{ui.KeyJ, "j"}, {ui.KeyM, "m"}, {ui.KeyS, "s"}, {ui.KeyK, "k"}} {
		tt.TypeKey(0, typed.key, typed.text)
		tt.Frame()
	}

	if a.Search != "jmsk" {
		t.Fatalf("search = %q, want the four typed letters", a.Search)
	}
	if a.SelectedRSSArticleID != "" {
		t.Fatalf("a letter typed in the search field chose %q", a.SelectedRSSArticleID)
	}
	if a.RSSTotalUnread != 3 {
		t.Fatalf("a letter typed in the search field changed the unread total to %d", a.RSSTotalUnread)
	}
	if a.RSSStarredTotal != 0 {
		t.Fatalf("a letter typed in the search field starred %d articles", a.RSSStarredTotal)
	}
}

// TestRSSShortcutsStayInTheRSSWorkspace checks the four single keys and Cmd+O
// do nothing outside the RSS page: whatever the reader had chosen keeps its
// read and star state, and no link opens.
func TestRSSShortcutsStayInTheRSSWorkspace(t *testing.T) {
	a, w, tt := rssShortcutFixture(t, 2)

	tt.Key(0, ui.KeyJ)
	tt.Frame()
	if a.SelectedRSSArticleID != "art-00" {
		t.Fatalf("chosen article = %q, want art-00", a.SelectedRSSArticleID)
	}

	w.selectWorkspace(WorkspaceVideos)
	tt.Frame()
	if w.activeWorkspaceName() != WorkspaceVideos {
		t.Fatalf("active workspace = %q, want videos", w.activeWorkspaceName())
	}

	for _, key := range []ui.Key{ui.KeyJ, ui.KeyK, ui.KeyM, ui.KeyS} {
		tt.Key(0, key)
		tt.Frame()
	}
	tt.Key(ui.Cmd, ui.KeyO)
	tt.Frame()
	if opened := tt.OpenedURLs(); len(opened) != 0 {
		t.Fatalf("the RSS keys opened %v from the video page", opened)
	}

	w.selectWorkspace(WorkspaceRSS)
	tt.Frame()
	if a.SelectedRSSArticleID != "art-00" {
		t.Fatalf("the RSS selection moved while another page showed: %q", a.SelectedRSSArticleID)
	}
	art := a.SelectedRSSArticle()
	if art == nil {
		t.Fatal("the RSS article is gone after the trip to the video page")
	}
	if art.Read || art.Starred {
		t.Fatalf("a key pressed on the video page changed the article: %+v", art)
	}
	if a.RSSTotalUnread != 2 || a.RSSStarredTotal != 0 {
		t.Fatalf("counts changed outside the RSS page: unread %d, starred %d", a.RSSTotalUnread, a.RSSStarredTotal)
	}

	// The window's own page keys still work from the RSS page.
	tt.Key(ui.Cmd, ui.Key1)
	tt.Frame()
	if w.activeWorkspaceName() != WorkspaceVideos {
		t.Fatalf("Cmd+1 left the active workspace at %q", w.activeWorkspaceName())
	}
}

// TestSetRSSArticleStarredKeepsCountsInStep pins the app's star write: the
// article in memory, its star timestamp, the sidebar's starred total, and the
// row SQLite keeps all move together in both directions.
func TestSetRSSArticleStarredKeepsCountsInStep(t *testing.T) {
	a, _, _ := rssShortcutFixture(t, 2)
	ctx := context.Background()

	if a.RSSStarredTotal != 0 || a.RSSArticles[0].Starred {
		t.Fatalf("fresh state is starred: total %d, article %+v", a.RSSStarredTotal, a.RSSArticles[0])
	}

	if err := a.SetRSSArticleStarred("art-00", true); err != nil {
		t.Fatalf("star the article: %v", err)
	}
	art := a.SelectedRSSArticle()
	if art != nil {
		t.Fatalf("starring chose an article: %+v", art)
	}
	for _, inMemory := range a.RSSArticles {
		if inMemory.ID != "art-00" {
			continue
		}
		if !inMemory.Starred || inMemory.StarredAt == 0 || inMemory.UpdatedAt == 0 {
			t.Fatalf("in-memory article did not take the star: %+v", inMemory)
		}
	}
	if a.RSSStarredTotal != 1 {
		t.Fatalf("starred total = %d, want 1", a.RSSStarredTotal)
	}
	stored, err := a.Store.GetRSSArticle(ctx, "art-00")
	if err != nil || stored == nil || !stored.Starred || stored.StarredAt == 0 {
		t.Fatalf("the store did not take the star: %v %+v", err, stored)
	}

	if err := a.SetRSSArticleStarred("art-00", false); err != nil {
		t.Fatalf("unstar the article: %v", err)
	}
	if a.RSSStarredTotal != 0 {
		t.Fatalf("starred total after unstarring = %d, want 0", a.RSSStarredTotal)
	}
	stored, err = a.Store.GetRSSArticle(ctx, "art-00")
	if err != nil || stored == nil || stored.Starred || stored.StarredAt != 0 {
		t.Fatalf("the store kept the star: %v %+v", err, stored)
	}
}
