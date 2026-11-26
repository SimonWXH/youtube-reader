package main

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
	mui "github.com/simon/youtube-reader/ui"
)

// setupWorkspaceTestApp creates an isolated app with channels and videos for testing.
func setupWorkspaceTestApp(t *testing.T) (*App, *Window) {
	t.Helper()
	dir := t.TempDir()
	a := newTestApp(t, dir)
	t.Cleanup(func() { a.Close() })

	channels := []core.Channel{
		{ID: "ch-1", Title: "Go Time", UpdatedAt: 100},
		{ID: "ch-2", Title: "Rust Streams", UpdatedAt: 100},
	}
	videos := []core.Video{
		{ID: "v-1", ChannelID: "ch-1", ChannelTitle: "Go Time", Title: "Go 1.24 Deep Dive", PublishedAt: "2026-01-01T00:00:00Z"},
		{ID: "v-2", ChannelID: "ch-2", ChannelTitle: "Rust Streams", Title: "Rust Async Explained", PublishedAt: "2026-01-02T00:00:00Z"},
	}

	a.Sync.Channels = map[string]core.Channel{"ch-1": channels[0], "ch-2": channels[1]}
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

	w := &Window{app: a, selectedIndex: new(int)}
	return a, w
}

// TestWorkspaceDockRendersFourEntrances verifies that the A2 dock renders all 4 entrances
// with correct labels, icons, and bilingual support.
func TestWorkspaceDockRendersFourEntrances(t *testing.T) {
	_, w := setupWorkspaceTestApp(t)
	tt := ui.NewTester(w.view, 1280, 808)
	tt.Frame()

	// Check English dock items:
	for _, name := range []string{"Videos", "RSS", "Read Later", "WeChat Notes"} {
		if _, ok := tt.Find(name); !ok {
			t.Fatalf("expected dock item %q to be present", name)
		}
	}

	// In English:
	metaVideos := dockMeta("en", WorkspaceVideos)
	metaRSS := dockMeta("en", WorkspaceRSS)
	metaReadLater := dockMeta("en", WorkspaceReadLater)
	metaWeChatNotes := dockMeta("en", WorkspaceWeChatNotes)

	if metaVideos.Title != "Videos" {
		t.Fatalf("videos meta = %+v, want Videos", metaVideos)
	}
	if metaRSS.Title != "RSS" {
		t.Fatalf("rss meta = %+v, want RSS", metaRSS)
	}
	if metaReadLater.Title != "Read Later" {
		t.Fatalf("read later meta = %+v, want Read Later", metaReadLater)
	}
	if metaWeChatNotes.Title != "WeChat Notes" {
		t.Fatalf("wechat notes meta = %+v, want WeChat Notes", metaWeChatNotes)
	}

	// In Chinese:
	tt.SetLocale("zh-CN")
	tt.Frame()

	metaZhVideos := dockMeta("zh-CN", WorkspaceVideos)
	metaZhRSS := dockMeta("zh-CN", WorkspaceRSS)
	metaZhReadLater := dockMeta("zh-CN", WorkspaceReadLater)
	metaZhWeChatNotes := dockMeta("zh-CN", WorkspaceWeChatNotes)

	if metaZhVideos.Title != "视频" {
		t.Fatalf("zh videos title = %q, want 视频", metaZhVideos.Title)
	}
	if metaZhRSS.Title != "RSS 文章" {
		t.Fatalf("zh rss title = %q, want RSS 文章", metaZhRSS.Title)
	}
	if metaZhReadLater.Title != "稍后读" {
		t.Fatalf("zh read later title = %q, want 稍后读", metaZhReadLater.Title)
	}
	if metaZhWeChatNotes.Title != "微信读书笔记" {
		t.Fatalf("zh wechat notes title = %q, want 微信读书笔记", metaZhWeChatNotes.Title)
	}
}

// TestWorkspaceSwitchingAndIsolation verifies that switching to RSS completely hides
// video elements, and search/filter/selection states are strictly isolated.
func TestWorkspaceSwitchingAndIsolation(t *testing.T) {
	a, w := setupWorkspaceTestApp(t)
	tt := ui.NewTester(w.view, 1280, 808)
	tt.Frame()

	// Initial state is Videos workspace:
	if w.activeWorkspaceName() != WorkspaceVideos {
		t.Fatalf("initial workspace = %q, want videos", w.activeWorkspaceName())
	}
	if !tt.HasText("Go 1.24 Deep Dive") {
		t.Fatal("videos workspace should show video title")
	}

	// Set video search and filter:
	a.Search = "Go 1.24"
	a.UnreadOnly = true
	a.ChannelFilter = "ch-1"
	a.Selected = "channel:ch-1"
	a.SelectedVideoID = "v-1"
	tt.Frame()

	// Switch to RSS workspace via clicking dock item:
	rssSlot, ok := tt.Find("RSS")
	if !ok {
		t.Fatal("could not find RSS dock item")
	}
	tt.Press(rssSlot.X+rssSlot.W/2, rssSlot.Y+rssSlot.H/2)
	tt.Release(rssSlot.X+rssSlot.W/2, rssSlot.Y+rssSlot.H/2)
	tt.Frame()

	if w.activeWorkspaceName() != WorkspaceRSS {
		t.Fatalf("after clicking RSS dock item, active workspace = %q, want rss", w.activeWorkspaceName())
	}

	// RSS workspace must NOT contain any video elements or video filter controls:
	if tt.HasText("Go 1.24 Deep Dive") {
		t.Fatal("RSS workspace must NOT contain video titles")
	}
	if tt.HasText("Rust Async Explained") {
		t.Fatal("RSS workspace must NOT contain video titles")
	}
	if tt.HasText("All channels") {
		t.Fatal("RSS workspace must NOT contain video channel filter")
	}

	// RSS should have isolated state:
	if a.Search != "" {
		t.Fatalf("RSS search should be clean, got %q", a.Search)
	}
	if a.SelectedVideoID != "" {
		t.Fatalf("RSS SelectedVideoID should be empty, got %q", a.SelectedVideoID)
	}

	// Set RSS-specific search:
	a.Search = "Tech News"
	a.UnreadOnly = false
	tt.Frame()

	// Switch back to Videos workspace:
	w.selectWorkspace(WorkspaceVideos)
	tt.Frame()

	if w.activeWorkspaceName() != WorkspaceVideos {
		t.Fatalf("after switching back, active workspace = %q, want videos", w.activeWorkspaceName())
	}

	// Videos state must be completely restored!
	if a.Search != "Go 1.24" {
		t.Fatalf("Videos search was not restored, got %q, want 'Go 1.24'", a.Search)
	}
	if !a.UnreadOnly {
		t.Fatal("Videos UnreadOnly was not restored")
	}
	if a.ChannelFilter != "ch-1" {
		t.Fatalf("Videos ChannelFilter was not restored, got %q", a.ChannelFilter)
	}
	if a.Selected != "channel:ch-1" {
		t.Fatalf("Videos Selected was not restored, got %q", a.Selected)
	}
	if a.SelectedVideoID != "v-1" {
		t.Fatalf("Videos SelectedVideoID was not restored, got %q", a.SelectedVideoID)
	}
	if !tt.HasText("Go 1.24 Deep Dive") {
		t.Fatal("Videos titles should be back in view")
	}

	// Switch back to RSS: verify RSS search was preserved!
	w.selectWorkspace(WorkspaceRSS)
	tt.Frame()
	if a.Search != "Tech News" {
		t.Fatalf("RSS search was not restored, got %q, want 'Tech News'", a.Search)
	}
}

// TestWorkspaceDockOrderPersistence verifies that reordering dock items
// is persisted to settings and survives reload.
func TestWorkspaceDockOrderPersistence(t *testing.T) {
	a, w := setupWorkspaceTestApp(t)
	tt := ui.NewTester(w.view, 1280, 808)
	tt.Frame()

	initialOrder := w.currentWorkspaceOrder()
	wantInitial := []string{WorkspaceVideos, WorkspaceRSS, WorkspaceReadLater, WorkspaceWeChatNotes}
	if !slices.Equal(initialOrder, wantInitial) {
		t.Fatalf("initial order = %v, want %v", initialOrder, wantInitial)
	}

	// Move RSS to position 0:
	w.moveWorkspace(WorkspaceRSS, 0)
	tt.Frame()

	wantReordered := []string{WorkspaceRSS, WorkspaceVideos, WorkspaceReadLater, WorkspaceWeChatNotes}
	if got := w.currentWorkspaceOrder(); !slices.Equal(got, wantReordered) {
		t.Fatalf("reordered = %v, want %v", got, wantReordered)
	}
	if got := a.Settings.WorkspaceOrder; !slices.Equal(got, wantReordered) {
		t.Fatalf("settings WorkspaceOrder = %v, want %v", got, wantReordered)
	}

	// Reload a new window with the same app/settings:
	w2 := &Window{app: a, selectedIndex: new(int)}
	tt2 := ui.NewTester(w2.view, 1280, 808)
	tt2.Frame()

	if got := w2.currentWorkspaceOrder(); !slices.Equal(got, wantReordered) {
		t.Fatalf("after reload, order = %v, want %v", got, wantReordered)
	}
}

// TestWorkspaceDockDragReorder tests dragging a dock item over another to reorder.
func TestWorkspaceDockDragReorder(t *testing.T) {
	_, w := setupWorkspaceTestApp(t)
	tt := ui.NewTester(w.view, 1280, 808)
	tt.Frame()

	videosBox, ok1 := tt.Find("Videos")
	rssBox, ok2 := tt.Find("RSS")
	if !ok1 || !ok2 {
		t.Fatal("could not find dock items")
	}

	// Drag RSS over Videos (left half of Videos):
	sx, sy := rssBox.X+rssBox.W/2, rssBox.Y+rssBox.H/2
	tx, ty := videosBox.X+videosBox.W/4, videosBox.Y+videosBox.H/2

	tt.Press(sx, sy)
	tt.Move(sx, sy-5)
	tt.Move(tx, ty)
	tt.Frame()
	tt.Release(tx, ty)
	tt.Frame()

	// RSS should now be before Videos:
	order := w.currentWorkspaceOrder()
	if order[0] != WorkspaceRSS || order[1] != WorkspaceVideos {
		t.Fatalf("order after drag = %v, want RSS at index 0", order)
	}

	// Now drag RSS over Videos (right half of Videos):
	videosBox, _ = tt.Find("Videos")
	rssBox, _ = tt.Find("RSS")
	sx, sy = rssBox.X+rssBox.W/2, rssBox.Y+rssBox.H/2
	tx, ty = videosBox.X+videosBox.W*3/4, videosBox.Y+videosBox.H/2

	tt.Press(sx, sy)
	tt.Move(sx, sy-5)
	tt.Move(tx, ty)
	tt.Frame()
	tt.Release(tx, ty)
	tt.Frame()

	// RSS should now be inserted after Videos:
	order = w.currentWorkspaceOrder()
	if order[0] != WorkspaceVideos || order[1] != WorkspaceRSS {
		t.Fatalf("order after right-half drag = %v, want Videos at index 0, RSS at index 1", order)
	}
}

func TestWorkspaceInitializationActiveSelection(t *testing.T) {
	a, w := setupWorkspaceTestApp(t)
	a.Settings.ActiveWorkspace = WorkspaceRSS
	a.Selected = "channel:ch-1"
	a.SelectedVideoID = "v-1"

	tt := ui.NewTester(w.view, 1280, 808)
	tt.Frame()

	if w.activeWorkspaceName() != WorkspaceRSS {
		t.Fatalf("active workspace = %q, want %q", w.activeWorkspaceName(), WorkspaceRSS)
	}
	if a.Selected != "all" {
		t.Fatalf("RSS active selection = %q, want 'all'", a.Selected)
	}
	if a.SelectedVideoID != "" {
		t.Fatalf("RSS SelectedVideoID = %q, want empty", a.SelectedVideoID)
	}

	w.selectWorkspace(WorkspaceVideos)
	tt.Frame()

	if w.activeWorkspaceName() != WorkspaceVideos {
		t.Fatalf("after switching to videos, active workspace = %q, want %q", w.activeWorkspaceName(), WorkspaceVideos)
	}
	if a.Selected != "channel:ch-1" {
		t.Fatalf("restored videos selection = %q, want 'channel:ch-1'", a.Selected)
	}
	if a.SelectedVideoID != "v-1" {
		t.Fatalf("restored videos SelectedVideoID = %q, want 'v-1'", a.SelectedVideoID)
	}
}

func TestWorkspaceInitializationInvalidActiveWorkspace(t *testing.T) {
	a, w := setupWorkspaceTestApp(t)
	a.Settings.ActiveWorkspace = "unknown_workspace"
	a.Selected = "channel:ch-1"

	tt := ui.NewTester(w.view, 1280, 808)
	tt.Frame()

	if w.activeWorkspaceName() != WorkspaceVideos {
		t.Fatalf("active workspace = %q, want fallback to %q", w.activeWorkspaceName(), WorkspaceVideos)
	}
	if a.Selected != "channel:ch-1" {
		t.Fatalf("videos selection = %q, want 'channel:ch-1'", a.Selected)
	}
}

// TestWorkspaceKeyboardShortcuts verifies Cmd+1..4 shortcuts switch workspaces.
func TestWorkspaceKeyboardShortcuts(t *testing.T) {
	_, w := setupWorkspaceTestApp(t)
	tt := ui.NewTester(w.view, 1280, 808)
	tt.Frame()

	// Initially on Videos (index 0):
	if w.activeWorkspaceName() != WorkspaceVideos {
		t.Fatalf("initial workspace = %q, want videos", w.activeWorkspaceName())
	}

	// Cmd+2 switches to index 1 (RSS):
	tt.Key(ui.Cmd, ui.Key2)
	tt.Frame()
	if w.activeWorkspaceName() != WorkspaceRSS {
		t.Fatalf("after Cmd+2, workspace = %q, want rss", w.activeWorkspaceName())
	}

	// Cmd+3 switches to index 2 (Read Later):
	tt.Key(ui.Cmd, ui.Key3)
	tt.Frame()
	if w.activeWorkspaceName() != WorkspaceReadLater {
		t.Fatalf("after Cmd+3, workspace = %q, want read_later", w.activeWorkspaceName())
	}

	// Cmd+4 switches to index 3 (WeChat Notes):
	tt.Key(ui.Cmd, ui.Key4)
	tt.Frame()
	if w.activeWorkspaceName() != WorkspaceWeChatNotes {
		t.Fatalf("after Cmd+4, workspace = %q, want wechat_notes", w.activeWorkspaceName())
	}

	// Cmd+1 switches back to index 0 (Videos):
	tt.Key(ui.Cmd, ui.Key1)
	tt.Frame()
	if w.activeWorkspaceName() != WorkspaceVideos {
		t.Fatalf("after Cmd+1, workspace = %q, want videos", w.activeWorkspaceName())
	}
}

// TestWorkspacePlaceholdersBilingualEmptyStates verifies that placeholder pages
// show bilingual empty states and never leave a blank screen.
func TestWorkspacePlaceholdersBilingualEmptyStates(t *testing.T) {
	_, w := setupWorkspaceTestApp(t)
	tt := ui.NewTester(w.view, 1280, 808)
	tt.Frame()

	// Read Later in English:
	w.selectWorkspace(WorkspaceReadLater)
	tt.Frame()
	if !tt.HasText("Read Later") {
		t.Fatal("Read Later empty state missing title")
	}
	if !tt.HasText("Save articles and videos to read later. This feature will be available in an upcoming update.") {
		t.Fatal("Read Later empty state missing description")
	}

	// Read Later in Chinese:
	tt.SetLocale("zh-CN")
	tt.Frame()
	if !tt.HasText("稍后读") {
		t.Fatal("Read Later zh empty state missing title")
	}
	if !tt.HasText("保存文章与视频以便稍后阅读。该功能将在后续版本中推出。") {
		t.Fatal("Read Later zh empty state missing description")
	}

	// WeChat Notes in Chinese:
	w.selectWorkspace(WorkspaceWeChatNotes)
	tt.Frame()
	if !tt.HasText("微信读书笔记") {
		t.Fatal("WeChat Notes zh empty state missing title")
	}
	if !tt.HasText("同步微信读书的划线与阅读笔记。该功能将在后续版本中推出。") {
		t.Fatal("WeChat Notes zh empty state missing description")
	}

	// WeChat Notes in English:
	tt.SetLocale("en")
	tt.Frame()
	if !tt.HasText("WeChat Notes") {
		t.Fatal("WeChat Notes en empty state missing title")
	}
	if !tt.HasText("Sync reading notes and annotations from WeChat Read. This feature will be available in an upcoming update.") {
		t.Fatal("WeChat Notes en empty state missing description")
	}
}

// TestThreeColumnResizableLayoutOnVideoAndRSS verifies that each workspace
// drags the divider it owns: the shared sidebar width on both pages, the
// details pane width on Videos, and the article list width on RSS, each
// persisted where the next launch reads it.
func TestThreeColumnResizableLayoutOnVideoAndRSS(t *testing.T) {
	a, w := setupWorkspaceTestApp(t)
	tt := ui.NewTester(w.view, 1280, 808)
	tt.Frame()

	// On Videos page, drag sidebar divider:
	sidebarLine, ok := tt.Find("Resize sidebar")
	if !ok {
		t.Fatal("could not find sidebar divider")
	}
	sx := sidebarLine.X
	tt.Press(sx, 100)
	tt.Move(sx+30, 100)
	tt.Frame()
	tt.Release(sx+30, 100)
	tt.Frame()

	newSidebarWidth := a.Settings.SidebarWidth
	if newSidebarWidth == defaultSidebarWidth {
		t.Fatalf("dragging sidebar divider did not change SidebarWidth from %v", defaultSidebarWidth)
	}

	// Switch to RSS page:
	w.selectWorkspace(WorkspaceRSS)
	tt.Frame()

	// Verify sidebar width on RSS page matches the persisted SidebarWidth:
	if w.sidebarWidth != newSidebarWidth {
		t.Fatalf("RSS page sidebarWidth = %v, want %v", w.sidebarWidth, newSidebarWidth)
	}

	// On RSS page, the divider between the list and the reader resizes the
	// list and leaves the Videos pane's own width alone.
	listLine, ok := tt.Find("Resize article list")
	if !ok {
		t.Fatal("could not find article list divider on RSS page")
	}
	lx := listLine.X
	tt.Press(lx, 150)
	tt.Move(lx-40, 150)
	tt.Frame()
	tt.Release(lx-40, 150)
	tt.Frame()

	newListWidth := a.Settings.RSSListWidth
	if newListWidth == defaultRSSListWidth {
		t.Fatalf("dragging the RSS divider did not change RSSListWidth from %v", defaultRSSListWidth)
	}
	if newListWidth >= defaultRSSListWidth {
		t.Fatalf("dragging left should narrow the list: %v", newListWidth)
	}
	if a.Settings.DetailsWidth != defaultDetailsWidth {
		t.Fatalf("RSS list drag changed DetailsWidth to %v", a.Settings.DetailsWidth)
	}
	if got := w.rssLayout.listWidth; got != newListWidth {
		t.Fatalf("live list width = %v, want %v", got, newListWidth)
	}

	// Switch back to Videos page, select a video so detail pane opens:
	w.selectWorkspace(WorkspaceVideos)
	a.SelectedVideoID = "v-1"
	tt.Frame()

	// Verify details width on Videos page keeps the separate DetailsWidth.
	if w.detail.width != defaultDetailsWidth {
		t.Fatalf("Videos page details width = %v, want %v", w.detail.width, defaultDetailsWidth)
	}
}

// TestWorkspaceScrollPositionIsIsolated verifies that each workspace keeps its
// own sidebar scroll position: scrolling the video list, switching to RSS and
// back restores the video list's scroll without bleeding the other page's.
func TestWorkspaceScrollPositionIsIsolated(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	t.Cleanup(func() { a.Close() })

	// Enough channels that the sidebar list scrolls at 808 pt tall.
	channels := make([]core.Channel, 60)
	synced := make(map[string]core.Channel, len(channels))
	for i := range channels {
		id := fmt.Sprintf("ch-%02d", i)
		channels[i] = core.Channel{ID: id, Title: fmt.Sprintf("Channel %02d", i), UpdatedAt: 100}
		synced[id] = channels[i]
	}
	a.Sync.Channels = synced
	a.Channels = channels
	a.Selected = "all"
	a.Settings.APIKey = "test-key"
	a.Settings.LastRefresh = "2026-01-01T00:00:00Z"
	a.Settings.SidebarWidth = defaultSidebarWidth
	a.Settings.DetailsWidth = defaultDetailsWidth

	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.view, 1280, 808)
	tt.Frame()

	if w.activeWorkspaceName() != WorkspaceVideos {
		t.Fatalf("initial workspace = %q, want videos", w.activeWorkspaceName())
	}

	// Scroll the video sidebar down with the pointer over the list.
	for range 10 {
		tt.Scroll(120, 300, 0, 200)
	}
	tt.Frame()

	videosScroll := w.workspaceSidebarScroll(WorkspaceVideos)
	if videosScroll.Y <= 0 {
		t.Fatalf("video sidebar did not scroll: Y = %v", videosScroll.Y)
	}
	scrolledY := videosScroll.Y

	// Switch to RSS: its sidebar must start at the top.
	w.selectWorkspace(WorkspaceRSS)
	tt.Frame()

	rssScroll := w.workspaceSidebarScroll(WorkspaceRSS)
	if rssScroll.Y != 0 {
		t.Fatalf("RSS sidebar inherited scroll %v; want its own 0", rssScroll.Y)
	}
	if videosScroll.Y != scrolledY {
		t.Fatalf("video scroll changed while hidden: %v -> %v", scrolledY, videosScroll.Y)
	}

	// Switch back to Videos: the video sidebar keeps its place.
	w.selectWorkspace(WorkspaceVideos)
	tt.Frame()
	if got := w.workspaceSidebarScroll(WorkspaceVideos).Y; got != scrolledY {
		t.Fatalf("video sidebar scroll restored to %v; want %v", got, scrolledY)
	}
}

// TestRSSFilterStripFitsItsColumn checks the RSS filter row keeps its search
// field, Unread check box and article count on one line inside the middle
// column at the minimum 960 pt window, including the widest sidebar and
// reader the dividers allow, with the count still visible as text.
func TestRSSFilterStripFitsItsColumn(t *testing.T) {
	for _, tc := range []struct {
		name         string
		width        int
		listWidth    float32
		sidebarWidth float32
		fullSearch   bool
	}{
		{name: "design width", width: 1280, fullSearch: true},
		{name: "design width, widest list", width: 1280, listWidth: maxRSSListWidth, fullSearch: true},
		{name: "minimum width", width: 960},
		{name: "minimum width, widest list and sidebar", width: 960, listWidth: maxRSSListWidth, sidebarWidth: maxSidebarWidth},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, w := setupWorkspaceTestApp(t)
			a.Settings.ActiveWorkspace = WorkspaceRSS
			if tc.listWidth > 0 {
				a.Settings.RSSListWidth = tc.listWidth
			}
			if tc.sidebarWidth > 0 {
				a.Settings.SidebarWidth = tc.sidebarWidth
			}
			tt := ui.NewTester(w.view, tc.width, 808)
			tt.Frame()

			if w.activeWorkspaceName() != WorkspaceRSS {
				t.Fatalf("active workspace = %q, want %q", w.activeWorkspaceName(), WorkspaceRSS)
			}

			strip := chromeControl(t, tt, "Article filters").Bounds
			search, ok := tt.Find("Search articles")
			if !ok {
				t.Fatal("no article search field")
			}
			unread := chromeControl(t, tt, "Unread")
			count := chromeControl(t, tt, "0 articles").Bounds

			if tc.fullSearch && search.W != searchFieldWidth {
				t.Fatalf("search is %v wide, want %v with the column's full room", search.W, searchFieldWidth)
			}
			if search.W < searchFieldMin || search.W > searchFieldWidth {
				t.Fatalf("search is %v wide, want %v..%v", search.W, searchFieldMin, searchFieldWidth)
			}
			if unread.Role != ui.RoleCheckBox {
				t.Fatalf("Unread role = %v, want a check box", unread.Role)
			}
			if count.W <= toolbarControl {
				t.Fatalf("article count is %v wide, want visible text", count.W)
			}
			for _, box := range []ui.Rect{search, unread.Bounds, count} {
				if box.X < strip.X || box.X+box.W > strip.X+strip.W ||
					box.Y < strip.Y || box.Y+box.H > strip.Y+strip.H {
					t.Fatalf("%v leaves the filter strip %v", box, strip)
				}
			}
			if search.X+search.W > unread.Bounds.X || unread.Bounds.X+unread.Bounds.W > count.X {
				t.Fatalf("article filters overlap: %v %v %v", search, unread.Bounds, count)
			}
			if right := strip.X + strip.W - mui.Space(4); count.X+count.W < right-mui.Space(2) {
				t.Fatalf("count ends at %v, strip's right inset is %v", count.X+count.W, right)
			}
		})
	}
}

// TestWorkspaceSwitchCommitsPendingGroupRename checks that switching pages
// from the sidebar settles an inline group rename in progress, exactly as the
// sidebar's own row actions do, so the typed name is not dropped on the floor.
func TestWorkspaceSwitchCommitsPendingGroupRename(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	t.Cleanup(func() { a.Close() })

	g := core.ChannelGroup{ID: "group-1", Name: "Science", ChannelIDs: []string{"ch-1"}, UpdatedAt: 100}
	a.Sync.Groups = map[string]core.ChannelGroup{"group-1": g}
	a.Groups = []core.ChannelGroup{g}
	a.Channels = []core.Channel{{ID: "ch-1", Title: "Science Channel"}}
	a.Sync.Channels = map[string]core.Channel{"ch-1": a.Channels[0]}
	a.Selected = "group:group-1"
	a.Settings.APIKey = "test-key"
	a.Settings.LastRefresh = "2026-01-01T00:00:00Z"
	a.Settings.SidebarWidth = defaultSidebarWidth
	a.Settings.DetailsWidth = defaultDetailsWidth

	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.view, 1280, 840)
	tt.Frame()

	if err := tt.RightClick("Science"); err != nil {
		t.Fatalf("right click group row: %v", err)
	}
	if err := tt.ChooseMenuItem("Rename"); err != nil {
		t.Fatalf("choose Rename: %v", err)
	}
	w.renamingGroupName = "Computer Science"
	tt.Frame()

	dock, ok := tt.Find("RSS")
	if !ok {
		t.Fatal("could not find RSS dock item")
	}
	tt.Press(dock.X+dock.W/2, dock.Y+dock.H/2)
	tt.Release(dock.X+dock.W/2, dock.Y+dock.H/2)
	tt.Frame()

	if w.activeWorkspaceName() != WorkspaceRSS {
		t.Fatalf("active workspace = %q, want %q", w.activeWorkspaceName(), WorkspaceRSS)
	}
	if w.renamingGroupID != "" {
		t.Fatalf("rename still pending on %q after the page switch", w.renamingGroupID)
	}
	if got := a.Sync.Groups["group-1"].Name; got != "Computer Science" {
		t.Fatalf("group name after the page switch = %q, want Computer Science", got)
	}
	if len(a.Groups) != 1 || a.Groups[0].Name != "Computer Science" {
		t.Fatalf("app groups after the page switch = %+v, want one Computer Science group", a.Groups)
	}
}

// TestNotificationClickOpensTheVideosWorkspace checks a channel notification
// takes the reader to the video page with the channel selected, whatever page
// was on screen, and that the page it left keeps its own search and selection
// for the way back.
func TestNotificationClickOpensTheVideosWorkspace(t *testing.T) {
	a, w := setupWorkspaceTestApp(t)
	a.Settings.ActiveWorkspace = WorkspaceRSS
	tt := ui.NewTester(w.view, 1280, 808)
	tt.Frame()

	if w.activeWorkspaceName() != WorkspaceRSS {
		t.Fatalf("active workspace = %q, want %q", w.activeWorkspaceName(), WorkspaceRSS)
	}
	a.Search = "rust"
	a.Selected = "starred"
	tt.Frame()

	notificationClickHandler(w)(core.ChannelNotificationID("ch-2"))
	tt.Frame()

	if w.activeWorkspaceName() != WorkspaceVideos {
		t.Fatalf("after the notification click, active workspace = %q, want %q", w.activeWorkspaceName(), WorkspaceVideos)
	}
	if a.Selected != "channel:ch-2" {
		t.Fatalf("click routed to %q, want channel:ch-2", a.Selected)
	}
	if !tt.HasText("Rust Async Explained") {
		t.Fatalf("the clicked channel's videos are missing: %q", tt.Texts())
	}

	w.selectWorkspace(WorkspaceRSS)
	tt.Frame()
	if a.Selected != "starred" || a.Search != "rust" {
		t.Fatalf("the RSS page came back as %q, %q; want its own starred view and search", a.Selected, a.Search)
	}
}

// TestRSSSidebarRowsSelectTheSmartFeed checks the RSS smart-feed rows still
// answer a click with their own selection after moving onto the sidebar's
// shared row.
func TestRSSSidebarRowsSelectTheSmartFeed(t *testing.T) {
	a, w := setupWorkspaceTestApp(t)
	a.Settings.ActiveWorkspace = WorkspaceRSS
	a.Selected = "all"
	tt := ui.NewTester(w.view, 1280, 808)
	tt.Frame()

	if err := tt.Click("Starred"); err != nil {
		t.Fatalf("click Starred: %v", err)
	}
	tt.Frame()
	if a.Selected != "starred" {
		t.Fatalf("after clicking Starred, selection = %q, want starred", a.Selected)
	}

	if err := tt.Click("All Unread"); err != nil {
		t.Fatalf("click All articles: %v", err)
	}
	tt.Frame()
	if a.Selected != "all" {
		t.Fatalf("after clicking All articles, selection = %q, want all", a.Selected)
	}
}
