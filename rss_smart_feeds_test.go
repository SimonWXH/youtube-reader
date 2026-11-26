package main

import (
	"context"
	"fmt"
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
		locale        string
		row           string
		headerTitle   string
		unread        string
		emptyTitle    string
		emptySentence string
		noMatchTitle  string
	}{
		{
			locale: "en", row: "Today", headerTitle: "Today", unread: "3 unread",
			emptyTitle:    "No Articles in the Last 24 Hours",
			emptySentence: "New articles will appear here as your feeds publish them.",
			noMatchTitle:  "No Matching Articles",
		},
		{
			locale: "zh", row: "今天", headerTitle: "今天", unread: "3 篇未读",
			emptyTitle:    "最近 24 小时没有文章",
			emptySentence: "订阅源更新后，新文章会出现在这里。",
			noMatchTitle:  "无匹配文章",
		},
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

			// A window with nothing in it describes the window rather than an
			// empty library, and an active search still wins.
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
			if !tt.HasText(tc.emptyTitle) || !tt.HasText(tc.emptySentence) {
				t.Fatalf("the empty Today column does not describe the window: %v", tt.Texts())
			}
			if tt.HasText("Articles from subscribed feeds will appear here.") || tt.HasText("订阅源的文章将显示在此处。") {
				t.Fatalf("the empty Today column reads as an empty library: %v", tt.Texts())
			}

			// The window holds nothing, so a search hides nothing: the copy
			// stays the window's own.
			a.Search = "anything"
			tt.Frame()
			if !tt.HasText(tc.emptyTitle) || !tt.HasText(tc.emptySentence) {
				t.Fatalf("the empty Today column lost its window copy under a search: %v", tt.Texts())
			}
			if tt.HasText(tc.noMatchTitle) {
				t.Fatalf("a search over an already empty window named no matches: %v", tt.Texts())
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

// TestRSSArticleRowDateFollowsTheClock pins the middle column's relative date
// to the same clock the Today window uses: the seeded hour-old article reads as
// "1h ago" at the pinned moment, and moving the clock two hours re-renders it
// as "3h ago" without touching the machine's real clock.
func TestRSSArticleRowDateFollowsTheClock(t *testing.T) {
	a, _, tt := rssTodayTestApp(t)
	if err := tt.Click("The Daily Feed"); err != nil {
		t.Fatalf("open the feed: %v", err)
	}
	tt.Frame()
	if !tt.HasText("1h ago") {
		t.Fatalf("the row date does not follow the pinned clock: %v", tt.Texts())
	}

	a.now = func() time.Time { return rssTodayClock.Add(2 * time.Hour) }
	tt.Frame()
	if !tt.HasText("3h ago") {
		t.Fatalf("the row date did not follow the moved clock: %v", tt.Texts())
	}
	if tt.HasText("1h ago") {
		t.Fatalf("the row date kept the clock reading from before the move: %v", tt.Texts())
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

// rssUnreadCheckbox reports whether the filter strip draws the Unread filter.
func rssUnreadCheckbox(t *testing.T, tt *ui.Tester, label string) bool {
	t.Helper()
	for _, info := range tt.AccessAll() {
		if info.Role == ui.RoleCheckBox && info.Name == label {
			return true
		}
	}
	return false
}

// TestRSSAllUnreadStripDescribesItsList covers the filter strip at All Unread:
// the destination already forces the unread filter, so the checkbox that could
// only contradict it is gone, and the sidebar badge, the strip count and the
// drawn rows all read the same number.
func TestRSSAllUnreadStripDescribesItsList(t *testing.T) {
	for _, tc := range []struct {
		locale, allUnread, starred, today, unreadLabel, stripCount string
	}{
		{locale: "en", allUnread: "All Unread", starred: "Starred", today: "Today", unreadLabel: "Unread", stripCount: "5 articles"},
		{locale: "zh", allUnread: "全部未读", starred: "标星", today: "今天", unreadLabel: "未读", stripCount: "5 篇文章"},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			a, _, tt := rssTodayTestApp(t)
			tt.SetLocale(tc.locale)
			tt.Frame()
			if err := tt.Click(tc.allUnread); err != nil {
				t.Fatalf("click %q: %v", tc.allUnread, err)
			}
			tt.Frame()

			if rssUnreadCheckbox(t, tt, tc.unreadLabel) {
				t.Fatalf("All Unread draws the Unread filter it cannot control: %v", tt.Texts())
			}
			if !tt.HasText(tc.stripCount) {
				t.Fatalf("the strip does not count %q: %v", tc.stripCount, tt.Texts())
			}
			drawn := 0
			for _, art := range rssTodayArticles {
				if tt.HasText(art.id) {
					drawn++
				}
			}
			if drawn != a.RSSTotalUnread {
				t.Fatalf("the column draws %d article rows against All Unread's %d unread articles", drawn, a.RSSTotalUnread)
			}
			if _, ok := tt.Find(fmt.Sprint(a.RSSTotalUnread)); !ok {
				t.Fatalf("no sidebar row shows the unread badge %d: %v", a.RSSTotalUnread, tt.Texts())
			}
			for _, read := range []string{"art-2h-read", "art-30h-read"} {
				if tt.HasText(read) {
					t.Fatalf("the column lists the read article %q", read)
				}
			}

			for _, row := range []string{tc.starred, tc.today} {
				if err := tt.Click(row); err != nil {
					t.Fatalf("click %q: %v", row, err)
				}
				tt.Frame()
				if !rssUnreadCheckbox(t, tt, tc.unreadLabel) {
					t.Fatalf("%q does not offer the Unread filter: %v", row, tt.Texts())
				}
			}
		})
	}
}

// TestRSSAllUnreadEmptyStateSaysUpToDate covers the empty column at All Unread:
// with every article read it says the reader is caught up instead of claiming
// the library is empty, in both languages, and an active search still wins.
func TestRSSAllUnreadEmptyStateSaysUpToDate(t *testing.T) {
	for _, tc := range []struct {
		locale, row, title, sentence, count, genericTitle, searchLabel, searchTitle string
		feedSearchSentence                                                          string
	}{
		{
			locale: "en", row: "All Unread", title: "You are up to date",
			sentence: "New articles will appear here when your feeds publish them.",
			count:    "0 articles", genericTitle: "No Articles",
			searchLabel: "Search articles", searchTitle: "No Matching Articles",
			feedSearchSentence: "Try clearing the search term.",
		},
		{
			locale: "zh", row: "全部未读", title: "已无未读",
			sentence: "订阅源更新后，新文章会出现在这里。",
			count:    "0 篇文章", genericTitle: "暂无文章",
			searchLabel: "搜索文章", searchTitle: "无匹配文章",
			feedSearchSentence: "尝试清除搜索词。",
		},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			a, _, tt := rssTodayTestApp(t)
			tt.SetLocale(tc.locale)
			tt.Frame()
			if err := tt.Click(tc.row); err != nil {
				t.Fatalf("click %q: %v", tc.row, err)
			}
			if !tt.HasText(tc.searchLabel) {
				t.Fatalf("the strip lost its search field: %v", tt.Texts())
			}
			for _, art := range rssTodayArticles {
				if art.read {
					continue
				}
				if err := a.MarkRSSArticleRead(art.id, true); err != nil {
					t.Fatalf("mark %q read: %v", art.id, err)
				}
			}
			tt.Frame()

			if got := len(a.VisibleRSSArticles()); got != 0 {
				t.Fatalf("the column still lists %d articles", got)
			}
			if !tt.HasText(tc.title) {
				t.Fatalf("the empty All Unread column does not say it is up to date: %v", tt.Texts())
			}
			if !tt.HasText(tc.sentence) {
				t.Fatalf("the up-to-date state is missing its sentence: %v", tt.Texts())
			}
			if tt.HasText(tc.genericTitle) {
				t.Fatalf("the read library reads as an empty one: %v", tt.Texts())
			}
			if !tt.HasText(tc.count) {
				t.Fatalf("the strip does not count %q: %v", tc.count, tt.Texts())
			}

			// All Unread holds nothing to hide, so a search leaves the
			// caught-up copy in place.
			a.Search = "anything"
			tt.Frame()
			if !tt.HasText(tc.title) || !tt.HasText(tc.sentence) {
				t.Fatalf("the empty All Unread column lost its caught-up copy under a search: %v", tt.Texts())
			}
			if tt.HasText(tc.searchTitle) {
				t.Fatalf("a search over an already empty destination named no matches: %v", tt.Texts())
			}

			// The feed holds articles the search hides, so its no-match copy
			// names the search alone.
			a.Selected = "feed:feed-today"
			tt.Frame()
			if !tt.HasText(tc.feedSearchSentence) {
				t.Fatalf("the feed's no-match copy stopped naming its Unread filter: %v", tt.Texts())
			}
		})
	}
}

// TestRSSTodayEmptyStateNeedsAFeed covers Today's empty copy on a library that
// has nothing to read yet: without any subscription it keeps the generic copy
// instead of describing a window that has nothing in it.
func TestRSSTodayEmptyStateNeedsAFeed(t *testing.T) {
	for _, tc := range []struct {
		locale, genericTitle, genericSentence, todayTitle string
	}{
		{
			locale: "en", genericTitle: "No Articles",
			genericSentence: "Articles from subscribed feeds will appear here.",
			todayTitle:      "No Articles in the Last 24 Hours",
		},
		{
			locale: "zh", genericTitle: "暂无文章",
			genericSentence: "订阅源的文章将显示在此处。",
			todayTitle:      "最近 24 小时没有文章",
		},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			a := newTestApp(t, t.TempDir())
			t.Cleanup(func() { a.Close() })
			w := &Window{app: a, selectedIndex: new(int)}
			w.selectWorkspace(WorkspaceRSS)
			tt := ui.NewTester(w.view, 1280, 808)
			tt.SetLocale(tc.locale)
			tt.Frame()

			if err := tt.Click(map[bool]string{true: "今天", false: "Today"}[tc.locale == "zh"]); err != nil {
				t.Fatalf("click Today: %v", err)
			}
			tt.Frame()
			if a.Selected != todayDestination {
				t.Fatalf("the empty library's destination = %q, want today", a.Selected)
			}
			if !tt.HasText(tc.genericTitle) || !tt.HasText(tc.genericSentence) {
				t.Fatalf("the empty library does not show its generic copy: %v", tt.Texts())
			}
			if tt.HasText(tc.todayTitle) {
				t.Fatalf("a library with no feeds describes a Today window: %v", tt.Texts())
			}
		})
	}
}

// TestRSSAllUnreadEmptyStateNeedsAFeed covers the caught-up column's other
// condition: an RSS library with no subscription at all keeps the generic empty
// copy instead of claiming there is nothing left to read.
func TestRSSAllUnreadEmptyStateNeedsAFeed(t *testing.T) {
	for _, tc := range []struct {
		locale, genericTitle, genericSentence, upToDate string
	}{
		{locale: "en", genericTitle: "No Articles", genericSentence: "Articles from subscribed feeds will appear here.", upToDate: "You are up to date"},
		{locale: "zh", genericTitle: "暂无文章", genericSentence: "订阅源的文章将显示在此处。", upToDate: "已无未读"},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			a := newTestApp(t, t.TempDir())
			t.Cleanup(func() { a.Close() })
			w := &Window{app: a, selectedIndex: new(int)}
			w.selectWorkspace(WorkspaceRSS)
			tt := ui.NewTester(w.view, 1280, 808)
			tt.SetLocale(tc.locale)
			tt.Frame()

			if len(a.RSSFeeds) != 0 {
				t.Fatalf("the fixture holds %d feeds, want a library with none", len(a.RSSFeeds))
			}
			if a.Selected != "all" {
				t.Fatalf("the empty library's destination = %q, want all", a.Selected)
			}
			if !tt.HasText(tc.genericTitle) || !tt.HasText(tc.genericSentence) {
				t.Fatalf("the empty library does not show its generic copy: %v", tt.Texts())
			}
			if tt.HasText(tc.upToDate) {
				t.Fatalf("an empty library claims to be caught up: %v", tt.Texts())
			}
		})
	}
}

// TestRSSStarredEmptyState covers the Starred smart feed's own empty state:
// with nothing starred it says how an article gets there, a search that hides
// a starred article still wins with the no-match copy, and a library with no
// feeds keeps the generic copy.
func TestRSSStarredEmptyState(t *testing.T) {
	for _, tc := range []struct {
		locale, starred, emptyTitle, emptySentence string
		noMatch, genericTitle, genericSentence     string
	}{
		{
			locale: "en", starred: "Starred",
			emptyTitle: "No Starred Articles", emptySentence: "Star an article and it will wait for you here.",
			noMatch: "No Matching Articles", genericTitle: "No Articles",
			genericSentence: "Articles from subscribed feeds will appear here.",
		},
		{
			locale: "zh", starred: "标星",
			emptyTitle: "没有标星文章", emptySentence: "把文章标星，它就会出现在这里。",
			noMatch: "无匹配文章", genericTitle: "暂无文章",
			genericSentence: "订阅源的文章将显示在此处。",
		},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			a, _, tt := rssTodayTestApp(t)
			tt.SetLocale(tc.locale)
			tt.Frame()
			if err := a.SetRSSArticleStarred("art-30h-read", false); err != nil {
				t.Fatalf("unstar the seeded article: %v", err)
			}
			if err := tt.Click(tc.starred); err != nil {
				t.Fatalf("click %s: %v", tc.starred, err)
			}
			tt.Frame()
			if !tt.HasText(tc.emptyTitle) || !tt.HasText(tc.emptySentence) {
				t.Fatalf("an empty Starred list does not describe itself: %v", tt.Texts())
			}
			if tt.HasText(tc.genericSentence) {
				t.Fatalf("an empty Starred list reads as an empty library: %v", tt.Texts())
			}

			if err := a.SetRSSArticleStarred("art-30h-read", true); err != nil {
				t.Fatalf("star the seeded article again: %v", err)
			}
			a.Search = "zzz"
			tt.Frame()
			if !tt.HasText(tc.noMatch) {
				t.Fatalf("a search that hides a starred article does not say so: %v", tt.Texts())
			}
			if tt.HasText(tc.emptyTitle) {
				t.Fatalf("the Starred empty copy outranked the search's no-match state: %v", tt.Texts())
			}
			a.Search = ""
		})

		t.Run(tc.locale+"/no feeds", func(t *testing.T) {
			a := newTestApp(t, t.TempDir())
			t.Cleanup(func() { a.Close() })
			w := &Window{app: a, selectedIndex: new(int)}
			w.selectWorkspace(WorkspaceRSS)
			tt := ui.NewTester(w.view, 1280, 808)
			tt.SetLocale(tc.locale)
			tt.Frame()
			if err := tt.Click(tc.starred); err != nil {
				t.Fatalf("click %s: %v", tc.starred, err)
			}
			tt.Frame()
			if !tt.HasText(tc.genericTitle) || !tt.HasText(tc.genericSentence) {
				t.Fatalf("a library with no feeds does not show its generic copy: %v", tt.Texts())
			}
			if tt.HasText(tc.emptyTitle) {
				t.Fatalf("a library with no feeds describes an empty Starred list: %v", tt.Texts())
			}
		})
	}
}

// TestRSSStarredChineseSurfaceNamesOneDestination covers the Starred smart
// feed's Chinese name: the sidebar row and the column header above its list
// read the same word.
func TestRSSStarredChineseSurfaceNamesOneDestination(t *testing.T) {
	_, _, tt := rssTodayTestApp(t)
	tt.SetLocale("zh")
	tt.Frame()
	if err := tt.Click("标星"); err != nil {
		t.Fatalf("click the Starred row: %v", err)
	}
	tt.Frame()

	row, ok := tt.AccessByRole(ui.RoleButton, "标星")
	if !ok {
		t.Fatalf("the sidebar has no 标星 row: %v", tt.Texts())
	}
	label, ok := tt.Find("标星")
	if !ok || label.X < row.Bounds.X || label.X+label.W > row.Bounds.X+row.Bounds.W {
		t.Fatalf("the sidebar row's own label is %v, want it inside %v", label, row.Bounds)
	}
	if !rssColumnHeaderHas(t, tt, "标星") {
		t.Fatalf("the Starred column header does not read 标星: %v", tt.Texts())
	}
	if rssColumnHeaderHas(t, tt, "标星文章") {
		t.Fatalf("the Starred column header keeps the sidebar row's old name: %v", tt.Texts())
	}
}

// TestRSSTodayEmptyStateFollowsTheWindow covers which empty variant Today
// shows: a window with nothing in it keeps the 24-hour copy even when the
// unread filter is on, a window whose articles a filter hides shows the
// no-match copy, and a blank search counts as no filter at all.
func TestRSSTodayEmptyStateFollowsTheWindow(t *testing.T) {
	for _, tc := range []struct {
		locale, row, windowTitle, windowSentence, noMatchTitle string
	}{
		{
			locale: "en", row: "Today",
			windowTitle:    "No Articles in the Last 24 Hours",
			windowSentence: "New articles will appear here as your feeds publish them.",
			noMatchTitle:   "No Matching Articles",
		},
		{
			locale: "zh", row: "今天",
			windowTitle:    "最近 24 小时没有文章",
			windowSentence: "订阅源更新后，新文章会出现在这里。",
			noMatchTitle:   "无匹配文章",
		},
	} {
		t.Run(tc.locale+"/empty window with the unread filter on", func(t *testing.T) {
			a, _, tt := rssTodayTestApp(t)
			tt.SetLocale(tc.locale)
			tt.Frame()
			if err := tt.Click(tc.row); err != nil {
				t.Fatalf("click %q: %v", tc.row, err)
			}
			later := rssTodayClock.Add(72 * time.Hour)
			a.now = func() time.Time { return later }
			if err := a.ReloadRSS(); err != nil {
				t.Fatalf("reload rss: %v", err)
			}
			a.UnreadOnly = true
			tt.Frame()
			if got := len(a.VisibleRSSArticles()); got != 0 {
				t.Fatalf("the empty window still lists %d articles", got)
			}
			if !tt.HasText(tc.windowTitle) || !tt.HasText(tc.windowSentence) {
				t.Fatalf("an empty window with the unread filter on lost its 24-hour copy: %v", tt.Texts())
			}
			if tt.HasText(tc.noMatchTitle) {
				t.Fatalf("the unread filter was blamed for an empty window: %v", tt.Texts())
			}
		})

		t.Run(tc.locale+"/read articles with the unread filter on", func(t *testing.T) {
			a, _, tt := rssTodayTestApp(t)
			tt.SetLocale(tc.locale)
			tt.Frame()
			if err := tt.Click(tc.row); err != nil {
				t.Fatalf("click %q: %v", tc.row, err)
			}
			a.UnreadOnly = true
			for _, art := range rssTodayArticles {
				if art.read {
					continue
				}
				if err := a.MarkRSSArticleRead(art.id, true); err != nil {
					t.Fatalf("mark %q read: %v", art.id, err)
				}
			}
			tt.Frame()
			if got := len(a.VisibleRSSArticles()); got != 0 {
				t.Fatalf("the filtered window still lists %d articles", got)
			}
			if !tt.HasText(tc.noMatchTitle) {
				t.Fatalf("articles hidden by the unread filter did not name it: %v", tt.Texts())
			}
			if tt.HasText(tc.windowTitle) {
				t.Fatalf("the window copy outranked the filter that hides its articles: %v", tt.Texts())
			}
		})

		t.Run(tc.locale+"/blank search", func(t *testing.T) {
			a, _, tt := rssTodayTestApp(t)
			tt.SetLocale(tc.locale)
			tt.Frame()
			if err := tt.Click(tc.row); err != nil {
				t.Fatalf("click %q: %v", tc.row, err)
			}
			later := rssTodayClock.Add(72 * time.Hour)
			a.now = func() time.Time { return later }
			if err := a.ReloadRSS(); err != nil {
				t.Fatalf("reload rss: %v", err)
			}
			a.Search = "   "
			tt.Frame()
			if got := len(a.VisibleRSSArticles()); got != 0 {
				t.Fatalf("the empty window still lists %d articles", got)
			}
			if !tt.HasText(tc.windowTitle) || !tt.HasText(tc.windowSentence) {
				t.Fatalf("a blank search replaced the window copy: %v", tt.Texts())
			}
			if tt.HasText(tc.noMatchTitle) {
				t.Fatalf("a blank search selected the no-match copy: %v", tt.Texts())
			}
		})
	}
}

// TestRSSNoMatchCopyNamesOnlyActiveFilters covers the no-match sentence: it
// names the search only, the unread filter only, or both, according to what is
// actually hiding the destination's articles.
func TestRSSNoMatchCopyNamesOnlyActiveFilters(t *testing.T) {
	for _, tc := range []struct {
		locale, starred, allUnread, searchOnly, unreadOnly, both string
	}{
		{
			locale: "en", starred: "Starred", allUnread: "All Unread",
			searchOnly: "Try clearing the search term.",
			unreadOnly: "Try clearing the unread filter.",
			both:       "Try clearing search terms or the unread filter.",
		},
		{
			locale: "zh", starred: "标星", allUnread: "全部未读",
			searchOnly: "尝试清除搜索词。",
			unreadOnly: "尝试清除未读过滤。",
			both:       "尝试清除搜索词或未读过滤。",
		},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			a, _, tt := rssTodayTestApp(t)
			tt.SetLocale(tc.locale)
			tt.Frame()
			if err := tt.Click(tc.starred); err != nil {
				t.Fatalf("click %s: %v", tc.starred, err)
			}

			a.Search = "anything"
			tt.Frame()
			if !rssHasSentence(t, tt, tc.searchOnly) {
				t.Fatalf("a search alone did not name the search: %v", tt.Texts())
			}
			if rssHasSentence(t, tt, tc.unreadOnly) || rssHasSentence(t, tt, tc.both) {
				t.Fatalf("a search alone named the unread filter that is off: %v", tt.Texts())
			}

			a.Search = ""
			a.UnreadOnly = true
			tt.Frame()
			if !rssHasSentence(t, tt, tc.unreadOnly) {
				t.Fatalf("the unread filter alone did not name itself: %v", tt.Texts())
			}
			if rssHasSentence(t, tt, tc.both) || rssHasSentence(t, tt, tc.searchOnly) {
				t.Fatalf("the unread filter alone named a search that is blank: %v", tt.Texts())
			}

			a.Search = "anything"
			tt.Frame()
			if !rssHasSentence(t, tt, tc.both) {
				t.Fatalf("both filters did not name both: %v", tt.Texts())
			}

			a.Search = "   "
			tt.Frame()
			if !rssHasSentence(t, tt, tc.unreadOnly) {
				t.Fatalf("a blank search counted as an active one: %v", tt.Texts())
			}
			if rssHasSentence(t, tt, tc.both) {
				t.Fatalf("a blank search made the copy name both filters: %v", tt.Texts())
			}

			a.UnreadOnly = false
			a.Search = "anything"
			if err := tt.Click(tc.allUnread); err != nil {
				t.Fatalf("click %s: %v", tc.allUnread, err)
			}
			tt.Frame()
			if !rssHasSentence(t, tt, tc.searchOnly) {
				t.Fatalf("All Unread's search did not name the search alone: %v", tt.Texts())
			}
			if rssHasSentence(t, tt, tc.unreadOnly) || rssHasSentence(t, tt, tc.both) {
				t.Fatalf("All Unread's copy named its forced unread filter: %v", tt.Texts())
			}
		})
	}
}

// rssHasSentence reports whether the column draws text exactly.
func rssHasSentence(t *testing.T, tt *ui.Tester, text string) bool {
	t.Helper()
	_, ok := tt.Find(text)
	return ok
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
