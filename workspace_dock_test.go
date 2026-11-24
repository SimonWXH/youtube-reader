package main

import (
	"context"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
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
// with correct labels, icons, counts, and bilingual support.
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
	metaVideos := w.dockMeta("en", WorkspaceVideos)
	metaRSS := w.dockMeta("en", WorkspaceRSS)
	metaReadLater := w.dockMeta("en", WorkspaceReadLater)
	metaWeChatNotes := w.dockMeta("en", WorkspaceWeChatNotes)

	if metaVideos.Title != "Videos" || metaVideos.Count != "2" {
		t.Fatalf("videos meta = %+v, want Videos with 2 unwatched", metaVideos)
	}
	if metaRSS.Title != "RSS" || metaRSS.Count != "0" {
		t.Fatalf("rss meta = %+v, want RSS with 0 count", metaRSS)
	}
	if metaReadLater.Title != "Read Later" || metaReadLater.Count != "" {
		t.Fatalf("read later meta = %+v, want Read Later with empty count", metaReadLater)
	}
	if metaWeChatNotes.Title != "WeChat Notes" || metaWeChatNotes.Count != "" {
		t.Fatalf("wechat notes meta = %+v, want WeChat Notes with empty count", metaWeChatNotes)
	}

	// In Chinese:
	tt.SetLocale("zh-CN")
	tt.Frame()

	metaZhVideos := w.dockMeta("zh-CN", WorkspaceVideos)
	metaZhRSS := w.dockMeta("zh-CN", WorkspaceRSS)
	metaZhReadLater := w.dockMeta("zh-CN", WorkspaceReadLater)
	metaZhWeChatNotes := w.dockMeta("zh-CN", WorkspaceWeChatNotes)

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

// TestThreeColumnResizableLayoutOnVideoAndRSS verifies that resizing dividers
// adjusts and persists SidebarWidth and DetailsWidth for both Video and RSS pages.
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

	// On RSS page, drag details divider:
	detailsLine, ok := tt.Find("Resize details pane")
	if !ok {
		t.Fatal("could not find details divider on RSS page")
	}
	dx := detailsLine.X
	tt.Press(dx, 150)
	tt.Move(dx-40, 150)
	tt.Frame()
	tt.Release(dx-40, 150)
	tt.Frame()

	newDetailsWidth := a.Settings.DetailsWidth
	if newDetailsWidth == defaultDetailsWidth {
		t.Fatalf("dragging details divider on RSS page did not change DetailsWidth from %v", defaultDetailsWidth)
	}

	// Switch back to Videos page, select a video so detail pane opens:
	w.selectWorkspace(WorkspaceVideos)
	a.SelectedVideoID = "v-1"
	tt.Frame()

	// Verify details width on Videos page reflects the newly persisted DetailsWidth:
	if w.detail.width != newDetailsWidth {
		t.Fatalf("Videos page details width = %v, want %v", w.detail.width, newDetailsWidth)
	}
}
