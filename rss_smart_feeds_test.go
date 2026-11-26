package main

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
)

// rssTodayClock is the fixed clock the Today tests run at, in the device's own
// timezone: the window follows the reader's clock rather than a calendar day,
// so the test pins the moment and lets the machine keep its zone.
var rssTodayClock = time.Date(2026, time.December, 1, 9, 0, 0, 0, time.Local)

// rssTodayArticle is one of the seven articles the Today tests seed: how long
// before the clock it was published, and whether it has been read.
type rssTodayArticle struct {
	id   string
	age  time.Duration
	read bool
}

// rssTodayArticles are the seven articles around the window's edges: three
// unread inside it, one read inside it for the list, and three published before
// it - one exactly on the 24-hour edge, which the window still counts.
var rssTodayArticles = []rssTodayArticle{
	{id: "art-1h", age: time.Hour},
	{id: "art-2h-read", age: 2 * time.Hour, read: true},
	{id: "art-23h", age: 23 * time.Hour},
	{id: "art-edge-24h", age: 24 * time.Hour},
	{id: "art-25h", age: 25 * time.Hour},
	{id: "art-30h-read", age: 30 * time.Hour, read: true},
	{id: "art-48h", age: 48 * time.Hour},
}

// rssTodayTestApp seeds the seven articles and a window already on the RSS
// workspace, its clock pinned to rssTodayClock.
func rssTodayTestApp(t *testing.T) (*App, *Window, *ui.Tester) {
	t.Helper()
	stubNetImages(t)
	a := newTestApp(t, t.TempDir())
	t.Cleanup(func() { a.Close() })
	ctx := context.Background()

	now := rssTodayClock
	feed := core.RSSFeed{
		ID: "feed-today", FeedURL: "https://feeds.example.com/today.xml",
		Title: "The Daily Feed", SiteURL: "https://feeds.example.com", UpdatedAt: now.UnixMilli(),
	}
	if err := a.Store.SaveRSSFeed(ctx, feed); err != nil {
		t.Fatalf("save feed: %v", err)
	}
	articles := make([]core.RSSArticle, 0, len(rssTodayArticles))
	for _, tc := range rssTodayArticles {
		articles = append(articles, core.RSSArticle{
			ID:          tc.id,
			FeedID:      feed.ID,
			Title:       tc.id,
			Link:        "https://feeds.example.com/" + tc.id,
			PublishedAt: now.Add(-tc.age).Format(time.RFC3339),
			Snippet:     tc.id + " summary",
			ContentHTML: "<p>" + tc.id + "</p>",
			IsFullText:  true,
			Read:        tc.read,
			Starred:     tc.id == "art-30h-read",
			UpdatedAt:   now.UnixMilli(),
		})
	}
	if err := a.Store.SaveRSSArticles(ctx, articles); err != nil {
		t.Fatalf("save articles: %v", err)
	}
	a.now = func() time.Time { return now }
	if err := a.ReloadRSS(); err != nil {
		t.Fatalf("reload rss: %v", err)
	}

	w := &Window{app: a, selectedIndex: new(int)}
	w.selectWorkspace(WorkspaceRSS)
	tt := ui.NewTester(w.view, 1280, 808)
	tt.Frame()
	return a, w, tt
}

// rssVisibleIDs is the article column's IDs, newest first.
func rssVisibleIDs(a *App) []string {
	ids := make([]string, 0, len(a.RSSArticles))
	for _, art := range a.VisibleRSSArticles() {
		ids = append(ids, art.ID)
	}
	return ids
}

// TestRSSTodayWindowEdges pins the window itself: the 24 hours before the
// reader's clock, counted by an article's own publication time.
func TestRSSTodayWindowEdges(t *testing.T) {
	now := rssTodayClock
	for _, tc := range []struct {
		name string
		raw  string
		want bool
	}{
		{name: "an hour ago", raw: now.Add(-time.Hour).Format(time.RFC3339), want: true},
		{name: "23 hours ago", raw: now.Add(-23 * time.Hour).Format(time.RFC3339), want: true},
		{name: "exactly 24 hours ago", raw: now.Add(-24 * time.Hour).Format(time.RFC3339), want: true},
		{name: "25 hours ago", raw: now.Add(-25 * time.Hour).Format(time.RFC3339), want: false},
		{name: "in the future", raw: now.Add(time.Minute).Format(time.RFC3339), want: false},
		{name: "no stamp", raw: "", want: false},
		{name: "an unreadable stamp", raw: "not a date", want: false},
		{name: "an admin's own zone", raw: now.Add(-2 * time.Hour).In(time.FixedZone("UTC+8", 8*3600)).Format(time.RFC3339), want: true},
	} {
		art := core.RSSArticle{ID: "art", PublishedAt: tc.raw}
		if got := rssPublishedWithin(art, now); got != tc.want {
			t.Errorf("%s (%s) in Today = %v, want %v", tc.name, tc.raw, got, tc.want)
		}
	}
}

// TestRSSSidebarSmartFeedsAreThreeRows pins the sidebar's smart feeds: Today,
// All Unread and Starred in that order, each on its own row with its count, in
// both languages.
func TestRSSSidebarSmartFeedsAreThreeRows(t *testing.T) {
	for _, tc := range []struct {
		locale string
		labels []string
	}{
		{locale: "en", labels: []string{"Today", "All Unread", "Starred"}},
		{locale: "zh", labels: []string{"今天", "全部未读", "标星"}},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			a, _, tt := rssTodayTestApp(t)
			tt.SetLocale(tc.locale)
			tt.Frame()

			// Each smart row draws its own label, top to bottom in the order of
			// the reference.
			var last ui.Rect
			for i, label := range tc.labels {
				box, ok := tt.Find(label)
				if !ok {
					t.Fatalf("the sidebar has no %q row: %v", label, tt.Texts())
				}
				if i > 0 && box.Y <= last.Y {
					t.Fatalf("%q sits at y %v, above the row before it at %v", label, box.Y, last.Y)
				}
				last = box
			}

			// The counts are the sidebar's own: unread inside the window, every
			// unread article, and the starred ones.
			if got, want := a.RSSTodayUnreadCount(), 3; got != want {
				t.Fatalf("Today's count = %d, want %d unread inside the window", got, want)
			}
			if got, want := a.RSSTotalUnread, 5; got != want {
				t.Fatalf("All Unread's count = %d, want %d", got, want)
			}
			if got, want := a.RSSStarredTotal, 1; got != want {
				t.Fatalf("Starred's count = %d, want %d", got, want)
			}
		})
	}
}

// TestRSSTodayDestinationSelectsTheWindow covers selecting Today: the middle
// column lists the articles published inside the window, newest first, the
// column header names Today and its unread count, and the other destinations
// keep their own lists.
func TestRSSTodayDestinationSelectsTheWindow(t *testing.T) {
	for _, tc := range []struct {
		locale      string
		row         string
		headerTitle string
		unread      string
		emptyTitle  string
	}{
		{locale: "en", row: "Today", headerTitle: "Today", unread: "3 unread", emptyTitle: "No Articles"},
		{locale: "zh", row: "今天", headerTitle: "今天", unread: "3 篇未读", emptyTitle: "暂无文章"},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			a, _, tt := rssTodayTestApp(t)
			tt.SetLocale(tc.locale)
			tt.Frame()

			if err := tt.Click(tc.row); err != nil {
				t.Fatalf("click %q: %v", tc.row, err)
			}
			tt.Frame()
			if a.Selected != todayDestination {
				t.Fatalf("after clicking %q the selection = %q, want %q", tc.row, a.Selected, todayDestination)
			}
			want := []string{"art-1h", "art-2h-read", "art-23h", "art-edge-24h"}
			if got := rssVisibleIDs(a); !slices.Equal(got, want) {
				t.Fatalf("Today's articles = %q, want %q", got, want)
			}
			if !tt.HasText(tc.headerTitle) {
				t.Fatalf("the column header does not name Today: %v", tt.Texts())
			}
			if !tt.HasText(tc.unread) {
				t.Fatalf("the column header does not count Today's unread: %v", tt.Texts())
			}
			if tt.HasText("art-25h") || tt.HasText("art-48h") {
				t.Fatalf("an article published before the window is listed: %v", tt.Texts())
			}

			// A window with nothing in it reads as the other destinations'
			// empty state does, in the same words.
			later := rssTodayClock.Add(72 * time.Hour)
			a.now = func() time.Time { return later }
			if err := a.ReloadRSS(); err != nil {
				t.Fatalf("reload rss: %v", err)
			}
			tt.Frame()
			if got := len(a.VisibleRSSArticles()); got != 0 {
				t.Fatalf("the empty window still lists %d articles", got)
			}
			if got := a.RSSTodayUnreadCount(); got != 0 {
				t.Fatalf("the empty window's count = %d, want none", got)
			}
			if !tt.HasText(tc.emptyTitle) {
				t.Fatalf("the empty Today column shows no empty state: %v", tt.Texts())
			}
		})
	}
}

// TestRSSTodayCountFollowsTheClockWithoutAReload covers the count and the list
// reading one moment: the clock moves past an article's window with no reload,
// and the sidebar count, the column header count and the list all drop it
// together.
func TestRSSTodayCountFollowsTheClockWithoutAReload(t *testing.T) {
	a, _, tt := rssTodayTestApp(t)
	if err := tt.Click("Today"); err != nil {
		t.Fatalf("click Today: %v", err)
	}
	tt.Frame()
	if got, want := a.RSSTodayUnreadCount(), 3; got != want {
		t.Fatalf("Today's count = %d, want %d", got, want)
	}

	// Half an hour later the oldest unread article has left the window; no
	// reload or read-state change runs between the two frames.
	a.now = func() time.Time { return rssTodayClock.Add(30 * time.Minute) }
	tt.Frame()

	want := []string{"art-1h", "art-2h-read", "art-23h"}
	if got := rssVisibleIDs(a); !slices.Equal(got, want) {
		t.Fatalf("Today's articles = %q, want %q", got, want)
	}
	if got, want := a.RSSTodayUnreadCount(), 2; got != want {
		t.Fatalf("Today's unread count = %d, want %d", got, want)
	}
	if !tt.HasText("2 unread") {
		t.Fatalf("the column header did not follow the clock: %v", tt.Texts())
	}
	if tt.HasText("3 unread") {
		t.Fatalf("the column header kept the count from before the clock moved: %v", tt.Texts())
	}
}

// TestRSSSmartFeedsKeepTheOtherDestinations guards what the new rows sit beside:
// Starred still lists only starred articles, and the feed destination still
// lists every article it holds, read ones included.
func TestRSSSmartFeedsKeepTheOtherDestinations(t *testing.T) {
	a, _, tt := rssTodayTestApp(t)

	if err := tt.Click("Starred"); err != nil {
		t.Fatalf("click Starred: %v", err)
	}
	tt.Frame()
	if a.Selected != "starred" {
		t.Fatalf("after clicking Starred the selection = %q", a.Selected)
	}
	if got, want := rssVisibleIDs(a), []string{"art-30h-read"}; !slices.Equal(got, want) {
		t.Fatalf("Starred's articles = %q, want %q", got, want)
	}

	if err := tt.Click("The Daily Feed"); err != nil {
		t.Fatalf("click The Daily Feed: %v", err)
	}
	tt.Frame()
	if a.Selected != "feed:feed-today" {
		t.Fatalf("after clicking the feed the selection = %q", a.Selected)
	}
	if got, want := len(rssVisibleIDs(a)), len(rssTodayArticles); got != want {
		t.Fatalf("the feed lists %d articles, want its whole %d", got, want)
	}
}

// TestRSSAllUnreadSmartFeedListsOnlyUnread covers the second smart feed: with
// read and unread articles seeded, the row and the column header both say All
// Unread, the count is the unread total, and the list holds exactly the unread
// articles.
func TestRSSAllUnreadSmartFeedListsOnlyUnread(t *testing.T) {
	for _, tc := range []struct {
		locale, row, title, unread string
	}{
		{locale: "en", row: "All Unread", title: "All Unread", unread: "5 unread"},
		{locale: "zh", row: "全部未读", title: "全部未读", unread: "5 篇未读"},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			a, _, tt := rssTodayTestApp(t)
			tt.SetLocale(tc.locale)
			tt.Frame()

			if err := tt.Click(tc.row); err != nil {
				t.Fatalf("click %q: %v", tc.row, err)
			}
			tt.Frame()
			if a.Selected != "all" {
				t.Fatalf("after clicking %q the selection = %q, want all", tc.row, a.Selected)
			}
			want := []string{"art-1h", "art-23h", "art-edge-24h", "art-25h", "art-48h"}
			if got := rssVisibleIDs(a); !slices.Equal(got, want) {
				t.Fatalf("All Unread's articles = %q, want the unread ones %q", got, want)
			}
			if got, want := len(want), a.RSSTotalUnread; got != want {
				t.Fatalf("All Unread lists %d articles against a count of %d", got, want)
			}
			if !rssColumnHeaderHas(t, tt, tc.title) {
				t.Fatalf("the column header does not name %q: %v", tc.title, tt.Texts())
			}
			if !rssColumnHeaderHas(t, tt, tc.unread) {
				t.Fatalf("the column header does not count %q: %v", tc.unread, tt.Texts())
			}
			for _, read := range []string{"art-2h-read", "art-30h-read"} {
				if tt.HasText(read) {
					t.Fatalf("All Unread lists the read article %q", read)
				}
			}
		})
	}
}

// rssColumnHeaderHas reports whether the article column header draws text.
func rssColumnHeaderHas(t *testing.T, tt *ui.Tester, text string) bool {
	t.Helper()
	header, ok := tt.AccessByRole(ui.RoleGroup, "Article column header")
	if !ok {
		t.Fatal("the article column has no accessible header")
	}
	for _, info := range tt.AccessAll() {
		if info.Name == text && rssBoxHolds(header.Bounds, info.Bounds) {
			return true
		}
	}
	return false
}

// TestRSSSmartFeedRowMetrics pins the rows the reference measures: the smart
// feeds and the feeds under them share the sidebar's 28 pt row, and a feed's
// icon is the reference's compact 16 pt favicon.
func TestRSSSmartFeedRowMetrics(t *testing.T) {
	_, _, tt := rssTodayTestApp(t)
	for _, label := range []string{"Today", "All Unread", "Starred", "The Daily Feed"} {
		box, ok := tt.Find(label)
		if !ok {
			t.Fatalf("the sidebar has no %q row: %v", label, tt.Texts())
		}
		row, ok := tt.AccessByRole(ui.RoleButton, label)
		if !ok {
			t.Fatalf("the %q row carries no accessible name", label)
		}
		if row.Bounds.H != 28 {
			t.Fatalf("%q's row is %v pt tall, want the reference's 28 pt row", label, row.Bounds.H)
		}
		if box.X < row.Bounds.X || box.X+box.W > row.Bounds.X+row.Bounds.W {
			t.Fatalf("%q's label at %v leaves its row %v", label, box, row.Bounds)
		}
	}
}
