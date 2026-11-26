package main

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/sfsymbols"
	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
	mui "github.com/simon/youtube-reader/ui"
)

// The four top-level workspaces.
const (
	WorkspaceVideos      = "videos"
	WorkspaceRSS         = "rss"
	WorkspaceReadLater   = "read_later"
	WorkspaceWeChatNotes = "wechat_notes"
)

// DefaultWorkspaceOrder is the standard initial order of the dock items.
var DefaultWorkspaceOrder = []string{
	WorkspaceVideos,
	WorkspaceRSS,
	WorkspaceReadLater,
	WorkspaceWeChatNotes,
}

// isValidWorkspace reports whether id is a valid top-level workspace.
func isValidWorkspace(id string) bool {
	switch id {
	case WorkspaceVideos, WorkspaceRSS, WorkspaceReadLater, WorkspaceWeChatNotes:
		return true
	default:
		return false
	}
}

// sanitizeWorkspaceOrder validates and ensures all 4 workspaces are present exactly once.
func sanitizeWorkspaceOrder(order []string) []string {
	seen := map[string]bool{}
	var res []string
	for _, id := range order {
		if isValidWorkspace(id) && !seen[id] {
			seen[id] = true
			res = append(res, id)
		}
	}
	for _, id := range DefaultWorkspaceOrder {
		if !seen[id] {
			res = append(res, id)
		}
	}
	return res
}

// workspaceDockDrag is the payload carried when dragging a dock item.
type workspaceDockDrag struct {
	ID    string
	Index int
}

// WorkspaceState isolates per-workspace parameters so switching back and forth
// restores each workspace's state without cross-contamination.
type WorkspaceState struct {
	Search         string
	Filter         string
	UnreadOnly     bool
	Selected       string
	SelectedItemID string
	SelectedIndex  *int
	// SidebarScroll is where the workspace's sidebar list is scrolled. Each
	// workspace keeps its own ScrollState, tracked directly by the sidebar
	// scroll container, so switching pages keeps each page's own place.
	SidebarScroll ui.ScrollState
	// ContentScroll is where the workspace's middle timeline is scrolled.
	ContentScroll ui.ScrollState
	// RSSList holds the virtualized article list's row measurement and scroll anchor.
	RSSList ui.ListState
	// Reader is the RSS reading view's place: the open article, where its body
	// is scrolled, and its paragraph rail. Switching pages keeps it, so coming
	// back to an article returns to the paragraph being read.
	Reader rssReaderState
}

// workspaceSidebarScroll returns the sidebar scroll state of a workspace,
// created on first use so a page keeps its own place.
func (w *Window) workspaceSidebarScroll(id string) *ui.ScrollState {
	w.ensureWorkspaceInit()
	st := w.workspaceStates[id]
	if st == nil {
		st = &WorkspaceState{Selected: "all"}
		w.workspaceStates[id] = st
	}
	return &st.SidebarScroll
}

// workspaceRSSArticlesScroll returns the article list scroll state for RSS.
func (w *Window) workspaceRSSArticlesScroll() *ui.ScrollState {
	w.ensureWorkspaceInit()
	st := w.workspaceStates[WorkspaceRSS]
	if st == nil {
		st = &WorkspaceState{Selected: "all"}
		w.workspaceStates[WorkspaceRSS] = st
	}
	return &st.ContentScroll
}

// workspaceRSSArticlesList returns the virtualized list state for the RSS article column.
func (w *Window) workspaceRSSArticlesList() *ui.ListState {
	w.ensureWorkspaceInit()
	st := w.workspaceStates[WorkspaceRSS]
	if st == nil {
		st = &WorkspaceState{Selected: "all"}
		w.workspaceStates[WorkspaceRSS] = st
	}
	return &st.RSSList
}

func (w *Window) ensureWorkspaceInit() {
	if w.workspaceStates == nil {
		w.workspaceStates = make(map[string]*WorkspaceState, 4)
		videosSelected := "all"
		if w.app != nil && w.app.Selected != "" {
			videosSelected = w.app.Selected
		}
		var videosItemID string
		var videosSearch, videosFilter string
		var videosUnreadOnly bool
		if w.app != nil {
			videosItemID = w.app.SelectedVideoID
			videosSearch = w.app.Search
			videosFilter = w.app.ChannelFilter
			videosUnreadOnly = w.app.UnreadOnly
		}
		w.workspaceStates[WorkspaceVideos] = &WorkspaceState{
			Search:         videosSearch,
			Filter:         videosFilter,
			UnreadOnly:     videosUnreadOnly,
			Selected:       videosSelected,
			SelectedItemID: videosItemID,
		}
		var rssItemID string
		if w.app != nil {
			rssItemID = w.app.SelectedRSSArticleID
		}
		w.workspaceStates[WorkspaceRSS] = &WorkspaceState{
			Selected:       "all",
			SelectedItemID: rssItemID,
		}
		w.workspaceStates[WorkspaceReadLater] = &WorkspaceState{Selected: "all"}
		w.workspaceStates[WorkspaceWeChatNotes] = &WorkspaceState{Selected: "all"}
	}
	if w.activeWorkspace == "" {
		target := WorkspaceVideos
		if w.app != nil && isValidWorkspace(w.app.Settings.ActiveWorkspace) {
			target = w.app.Settings.ActiveWorkspace
		}
		w.activeWorkspace = target
		if target != WorkspaceVideos {
			w.restoreWorkspaceState(target)
		}
	}
}

// activeWorkspaceName returns the currently active workspace.
func (w *Window) activeWorkspaceName() string {
	w.ensureWorkspaceInit()
	return w.activeWorkspace
}

// currentWorkspaceOrder returns the dock items in order.
func (w *Window) currentWorkspaceOrder() []string {
	if w.app != nil {
		return w.app.WorkspaceOrder()
	}
	return append([]string(nil), DefaultWorkspaceOrder...)
}

// selectWorkspace switches to the specified workspace, preserving state isolation.
func (w *Window) selectWorkspace(target string) {
	w.ensureWorkspaceInit()
	if !isValidWorkspace(target) || target == w.activeWorkspace {
		return
	}
	if w.renamingGroupID != "" {
		w.commitRenameGroup(w.renamingGroupID, w.renamingGroupName)
	}
	if w.renamingRSSGroupID != "" {
		w.commitRenameRSSGroup(w.renamingRSSGroupID, w.renamingRSSGroupName)
	}
	if w.renamingRSSFeedID != "" {
		w.commitRenameRSSFeed(w.renamingRSSFeedID, w.renamingRSSFeedName)
	}
	w.saveWorkspaceState(w.activeWorkspace)
	w.activeWorkspace = target
	if w.app != nil {
		_ = w.app.SetActiveWorkspace(target)
	}
	w.restoreWorkspaceState(target)
}

// selectWorkspaceByIndex selects the workspace at position index in current order.
func (w *Window) selectWorkspaceByIndex(index int) {
	order := w.currentWorkspaceOrder()
	if index >= 0 && index < len(order) {
		w.selectWorkspace(order[index])
	}
}

// moveWorkspace moves the dock item with id to target index to.
func (w *Window) moveWorkspace(id string, to int) {
	order := w.currentWorkspaceOrder()
	idx := slices.Index(order, id)
	if idx < 0 {
		return
	}
	order = slices.Delete(order, idx, idx+1)
	if to > idx {
		to--
	}
	if to < 0 {
		to = 0
	}
	if to > len(order) {
		to = len(order)
	}
	order = slices.Insert(order, to, id)
	if w.app != nil {
		_ = w.app.SetWorkspaceOrder(order)
	}
}

// saveWorkspaceState captures the active workspace's state before switching away.
func (w *Window) saveWorkspaceState(id string) {
	w.ensureWorkspaceInit()
	st := w.workspaceStates[id]
	if st == nil {
		st = &WorkspaceState{}
		w.workspaceStates[id] = st
	}
	if w.app != nil {
		st.Search = w.app.Search
		st.Filter = w.app.ChannelFilter
		st.UnreadOnly = w.app.UnreadOnly
		st.Selected = w.app.Selected
		if id == WorkspaceVideos {
			st.SelectedItemID = w.app.SelectedVideoID
		} else if id == WorkspaceRSS {
			st.SelectedItemID = w.app.SelectedRSSArticleID
			st.Reader = w.rssReader
		}
	}
	st.SelectedIndex = w.selectedIndex
}

// restoreWorkspaceState restores the target workspace's state.
func (w *Window) restoreWorkspaceState(id string) {
	w.ensureWorkspaceInit()
	st := w.workspaceStates[id]
	if st == nil {
		st = &WorkspaceState{Selected: "all"}
		w.workspaceStates[id] = st
	}
	if w.app != nil {
		w.app.Search = st.Search
		w.app.ChannelFilter = st.Filter
		w.app.UnreadOnly = st.UnreadOnly
		w.app.Selected = st.Selected
		if id == WorkspaceVideos {
			w.app.SelectedVideoID = st.SelectedItemID
		} else {
			w.app.SelectedVideoID = ""
		}
		if id == WorkspaceRSS {
			w.app.SelectedRSSArticleID = st.SelectedItemID
		} else {
			w.app.SelectedRSSArticleID = ""
		}
	}
	if id == WorkspaceRSS {
		w.rssReader = st.Reader
		// A selection the window restores is an article the reader opens
		// again, so it gets the same fetch a click would start.
		if w.app != nil {
			if art := w.app.SelectedRSSArticle(); art != nil {
				w.openRSSFullText(*art)
			}
		}
	} else {
		w.rssReader = rssReaderState{}
	}
	w.selectedIndex = st.SelectedIndex
}

type dockItemMeta struct {
	Icon  mui.WorkspaceIconKind
	Title string
}

// dockMeta names one dock entrance. Its title is the workspace's name in the
// active locale and is also all its tooltip says: the dock carries no unread
// count, so no tooltip adds one.
func dockMeta(locale, id string) dockItemMeta {
	zh := strings.HasPrefix(strings.ToLower(locale), "zh")
	switch id {
	case WorkspaceVideos:
		title := "Videos"
		if zh {
			title = "视频"
		}
		return dockItemMeta{Icon: mui.WorkspaceIconVideos, Title: title}
	case WorkspaceRSS:
		title := "RSS"
		if zh {
			title = "RSS 文章"
		}
		return dockItemMeta{Icon: mui.WorkspaceIconRSS, Title: title}
	case WorkspaceReadLater:
		title := "Read Later"
		if zh {
			title = "稍后读"
		}
		return dockItemMeta{Icon: mui.WorkspaceIconReadLater, Title: title}
	case WorkspaceWeChatNotes:
		title := "WeChat Notes"
		if zh {
			title = "微信读书笔记"
		}
		return dockItemMeta{Icon: mui.WorkspaceIconWeChatNotes, Title: title}
	}
	return dockItemMeta{Title: id}
}

// sidebarWorkspaceDock renders the A2 horizontal dock at the top of the sidebar.
func (w *Window) sidebarWorkspaceDock(c *ui.Context, t mui.Tokens) {
	order := w.currentWorkspaceOrder()
	active := w.activeWorkspaceName()
	locale := c.Locale()

	dock := ui.Row(c.Key("workspace-dock")).Height(54).FillWidth().
		Padding(4, 6).Gap(4).AlignItems(ui.Center).Background(t.Sidebar).
		Role(ui.RoleTabList).Label("Workspaces")

	dock.Children(func() {
		for i, id := range order {
			i := i
			id := id
			meta := dockMeta(locale, id)
			selected := id == active

			slot := ui.Box(c.Key("dock-item-" + id)).
				Grow(1).MinWidth(0).Height(46).Radius(mui.RadiusControl).
				Role(ui.RoleTab).Label(meta.Title).Tooltip(meta.Title).AccessSelected(selected).
				Cursor(ui.CursorPointer)

			slot.Drag(workspaceDockDrag{ID: id, Index: i})
			if slot.Dragging() {
				slot.Opacity(0.4)
			}
			dragged, over := ui.DragOver[workspaceDockDrag](slot)
			dropped, isDrop := ui.Drop[workspaceDockDrag](slot)

			insertAfter := false
			if over || isDrop {
				if x, _, inside := slot.PointerPosition(); inside {
					insertAfter = x >= slot.LayoutBounds().W/2
				}
			}

			if isDrop && (dragged.ID != "" || dropped.ID != "") {
				dropID := dragged.ID
				if dropID == "" {
					dropID = dropped.ID
				}
				to := i
				if insertAfter {
					to = i + 1
				}
				w.moveWorkspace(dropID, to)
				c.Invalidate()
			}

			// Every glyph keeps its own colour in every state, a
			// placeholder workspace's included: a workspace that is not
			// built yet says so in its content, not by fading its icon.
			// The active slot shows an accent edge bar and hover and
			// press an outline, never a plate behind the ink, so no state
			// lowers a glyph's contrast.
			iconColor := t.WorkspaceIconColor(meta.Icon)

			slot.Draw(func(p *ui.Painter, r ui.Rect) {
				if selected {
					p.Fill(ui.Rect{X: r.X, Y: r.Y + 4, W: 2, H: r.H - 8}, t.Accent, 1)
				}
				switch {
				case slot.Pressed():
					p.Stroke(r, t.Accent, mui.RadiusControl, 1)
				case slot.Hovered():
					p.Stroke(r, t.WorkspaceDockHoverOutline, mui.RadiusControl, 1)
				}

				if over && dragged.ID != "" && dragged.ID != id {
					indX := r.X
					if insertAfter {
						indX = r.X + r.W - 2
					}
					p.Fill(ui.Rect{X: indX, Y: r.Y, W: 2, H: r.H}, t.Accent, 1)
				}

				mui.WorkspaceIconDraw(p, r, meta.Icon, iconColor)
			})

			if slot.Clicked() {
				w.selectWorkspace(id)
			}
		}
	})
}

// readLaterSidebar renders the placeholder sidebar for Read Later.
func (w *Window) readLaterSidebar(c *ui.Context, t mui.Tokens) {
	zh := strings.HasPrefix(strings.ToLower(c.Locale()), "zh")
	title := "Read Later"
	desc := "Saved items will appear here."
	if zh {
		title = "稍后读"
		desc = "保存的项目将显示在此处。"
	}

	ui.Scroll(c.Key("read-later-sidebar-scroll")).Grow(1).MinHeight(0).Clip().
		TrackScroll(w.workspaceSidebarScroll(WorkspaceReadLater)).
		Padding(mui.Space(4), mui.Space(2)).Children(func() {
		sectionHeader(c, t, title)
		ui.Column(c).FillWidth().Padding(mui.Space(4), mui.Space(2)).AlignItems(ui.Center).Children(func() {
			ui.Text(c, desc).FontSize(12).TextColor(t.Muted).SingleLine()
		})
	})
}

// wechatNotesSidebar renders the placeholder sidebar for WeChat Notes.
func (w *Window) wechatNotesSidebar(c *ui.Context, t mui.Tokens) {
	zh := strings.HasPrefix(strings.ToLower(c.Locale()), "zh")
	title := "WeChat Notes"
	desc := "Synced notes will appear here."
	if zh {
		title = "微信读书笔记"
		desc = "同步的读书笔记将显示在此处。"
	}

	ui.Scroll(c.Key("wechat-notes-sidebar-scroll")).Grow(1).MinHeight(0).Clip().
		TrackScroll(w.workspaceSidebarScroll(WorkspaceWeChatNotes)).
		Padding(mui.Space(4), mui.Space(2)).Children(func() {
		sectionHeader(c, t, title)
		ui.Column(c).FillWidth().Padding(mui.Space(4), mui.Space(2)).AlignItems(ui.Center).Children(func() {
			ui.Text(c, desc).FontSize(12).TextColor(t.Muted).SingleLine()
		})
	})
}

// rssMainArea renders the RSS workspace's three columns: the article list at
// its stored width, the divider the user drags, and the adaptive reader.
func (w *Window) rssMainArea(c *ui.Context, t mui.Tokens, rowWidth float32) {
	w.installRSSShortcuts(c)
	listWidth := rssListWidthForRoom(w.rssLayout.listWidth, rowWidth)
	w.rssLayout.shownWidth = listWidth
	w.rssLayout.readerWidth = max(rowWidth-listWidth-splitDividerWidth(), 0)
	w.contentWidth = listWidth
	w.gridAvailableWidth = listWidth

	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Width(listWidth).Shrink(0).MinWidth(0).Children(func() {
			w.rssContent(c, t)
		})
		line := w.rssListDivider(c, t, listWidth, rowWidth)
		ui.Column(c).Grow(1).MinWidth(rssReaderMinWidth).Children(func() {
			w.rssReaderPane(c, t)
		})
		w.rssListDividerGrip(c, t, line, listWidth, rowWidth)
	})
}

// rssListDivider is the 1 pt line between the article list and the reader.
// A press focuses it and the arrows step the list width, so the split works
// without a pointer.
func (w *Window) rssListDivider(c *ui.Context, t mui.Tokens, listWidth, rowWidth float32) ui.Element {
	line := ui.Box(c).Key("rss-list-divider").Width(splitDividerWidth()).FillHeight().
		Focusable().Role(ui.RoleSplitter).Label("Resize article list").
		Range(float64(minRSSListWidth), float64(maxRSSListWidth), float64(listWidth)).
		Step(float64(rssListKeyStep)).FocusRingColor(t.Focus).FocusRingOffset(mui.FocusRingGap)
	line.Draw(func(p *ui.Painter, r ui.Rect) {
		p.Fill(r, t.Border, 0)
	})
	if line.Shortcut(0, ui.KeyLeft) {
		w.setRSSListWidth(listWidth-rssListKeyStep, rowWidth)
	}
	if line.Shortcut(0, ui.KeyRight) {
		w.setRSSListWidth(listWidth+rssListKeyStep, rowWidth)
	}
	return line
}

// rssListDividerGrip carries the drag, the col-resize cursor, the hover ink
// and the double-click reset for the list/reader seam.
func (w *Window) rssListDividerGrip(c *ui.Context, t mui.Tokens, line ui.Element, listWidth, rowWidth float32) {
	handle := ui.Box(c).Key("rss-list-divider-grip").Absolute().Top(0).FillHeight().
		Width(detailsDividerGrip).
		Left(listWidth - (detailsDividerGrip-splitDividerWidth())/2).Cursor(ui.CursorResizeEW)
	dx, _, held := handle.Dragged()
	switch {
	case held:
		line.Focus()
		if dx != 0 {
			w.dragRSSList(dx, listWidth, rowWidth)
		}
	case w.rssLayout.dragging:
		w.rssLayout.dragging = false
		w.rssLayout.overshoot = 0
	}
	if handle.DoubleClicked() {
		w.setRSSListWidth(defaultRSSListWidth, rowWidth)
	}
	handle.Draw(func(p *ui.Painter, r ui.Rect) {
		if handle.Hovered() || w.rssLayout.dragging {
			x := r.X + (detailsDividerGrip-splitDividerWidth())/2
			p.Fill(ui.Rect{X: x, Y: r.Y, W: splitDividerWidth(), H: r.H}, t.Accent, 0)
		}
	})
}

// rssListKeyStep is the article list width an arrow key asks for.
var rssListKeyStep = mui.Space(2)

// rssContent renders Column 2: the RSS middle timeline and search filter bar.
func (w *Window) rssContent(c *ui.Context, t mui.Tokens) {
	var articles []core.RSSArticle
	var feedTitles map[string]string
	if w.app != nil {
		articles = w.app.VisibleRSSArticles()
		feedTitles = w.app.feedTitlesByID()
	}
	ui.Column(c).Grow(1).MinWidth(0).Background(t.App).Children(func() {
		w.rssColumnHeader(c, t)
		w.rssContentStrip(c, t, len(articles))
		w.rssArticlesArea(c, t, articles, feedTitles)
	})
}

// selectedRSSSourceTitle returns the human-readable title for the currently selected RSS source/folder.
func (w *Window) selectedRSSSourceTitle(locale string) string {
	zh := strings.HasPrefix(strings.ToLower(locale), "zh")
	if w.app == nil || w.app.Selected == "all" || w.app.Selected == "" {
		if zh {
			return "全部未读"
		}
		return "All Unread"
	}
	if w.app.Selected == todayDestination {
		if zh {
			return "今天"
		}
		return "Today"
	}
	if w.app.Selected == "starred" {
		if zh {
			return "标星"
		}
		return "Starred"
	}
	if strings.HasPrefix(w.app.Selected, "feed:") {
		fid := strings.TrimPrefix(w.app.Selected, "feed:")
		if title := w.app.FeedTitle(fid); title != "" {
			return title
		}
		return fid
	}
	if strings.HasPrefix(w.app.Selected, "group:") {
		gid := strings.TrimPrefix(w.app.Selected, "group:")
		for _, g := range w.app.RSSGroups {
			if g.ID == gid {
				return g.Name
			}
		}
		return gid
	}
	return w.app.Selected
}

// rssSelectionUnreadCount returns the unread count matching the current selection.
func (w *Window) rssSelectionUnreadCount() int {
	if w.app == nil {
		return 0
	}
	selected := w.app.Selected
	if selected == "all" || selected == "" {
		return w.app.RSSUnreadCount("")
	}
	if selected == todayDestination {
		return w.app.RSSTodayUnreadCount()
	}
	if selected == "starred" {
		cnt := 0
		for _, art := range w.app.RSSArticles {
			if art.Starred && !art.Read {
				cnt++
			}
		}
		return cnt
	}
	if strings.HasPrefix(selected, "feed:") {
		fid := strings.TrimPrefix(selected, "feed:")
		return w.app.RSSUnreadCount(fid)
	}
	if strings.HasPrefix(selected, "group:") {
		gid := strings.TrimPrefix(selected, "group:")
		allowed := make(map[string]bool)
		for _, g := range w.app.RSSGroups {
			if g.ID == gid {
				for _, fid := range g.FeedIDs {
					allowed[fid] = true
				}
				break
			}
		}
		cnt := 0
		for _, art := range w.app.RSSArticles {
			if allowed[art.FeedID] && !art.Read {
				cnt++
			}
		}
		return cnt
	}
	return w.app.RSSUnreadCount("")
}

// rssColumnHeader renders the NetNewsWire-style column header with source title and unread count.
func (w *Window) rssColumnHeader(c *ui.Context, t mui.Tokens) {
	locale := c.Locale()
	zh := strings.HasPrefix(strings.ToLower(locale), "zh")
	title := w.selectedRSSSourceTitle(locale)
	unread := w.rssSelectionUnreadCount()

	var unreadText string
	if unread == 1 {
		unreadText = "1 unread"
		if zh {
			unreadText = "1 篇未读"
		}
	} else {
		unreadText = fmt.Sprintf("%d unread", unread)
		if zh {
			unreadText = fmt.Sprintf("%d 篇未读", unread)
		}
	}

	header := ui.Row(c.Key("rss-column-header")).Height(36).Shrink(0).FillWidth().
		Padding(0, mui.Space(4)).AlignItems(ui.Center).
		Role(ui.RoleGroup).Label("Article column header")
	header.Draw(func(p *ui.Painter, r ui.Rect) {
		p.Fill(ui.Rect{X: r.X, Y: r.Y + r.H - mui.Hairline, W: r.W, H: mui.Hairline}, t.BorderSoft, 0)
	})
	header.Children(func() {
		mui.SectionHeader.Apply(ui.Text(c, title)).TextColor(t.Text).SingleLine().Shrink(1)
		ui.Spacer(c)
		mui.Metadata.Apply(ui.Text(c, unreadText)).TextColor(t.Muted).SingleLine().Shrink(0)
	})
}

// rssContentStrip renders the filter and search bar for RSS articles. The
// Unread filter is left out at the All Unread destination, where the list is
// already unread-only and the checkbox would have nothing to control.
func (w *Window) rssContentStrip(c *ui.Context, t mui.Tokens, visibleCount int) {
	zh := strings.HasPrefix(strings.ToLower(c.Locale()), "zh")
	searchLabel := "Search articles"
	unreadLabel := "Unread"
	if zh {
		searchLabel = "搜索文章"
		unreadLabel = "未读"
	}
	unreadForced := w.app != nil && (w.app.Selected == "all" || w.app.Selected == "")

	var countText string
	if visibleCount == 1 {
		countText = "1 article"
		if zh {
			countText = "1 篇文章"
		}
	} else {
		countText = fmt.Sprintf("%d articles", visibleCount)
		if zh {
			countText = fmt.Sprintf("%d 篇文章", visibleCount)
		}
	}

	var checkboxWidth float32
	if !unreadForced {
		labelWidth, _ := c.MeasureText(0, ui.Span{Text: unreadLabel, Size: mui.Control.Size, Weight: int(mui.Control.Weight)})
		checkboxWidth = mui.Space(4) + mui.Space(2) + labelWidth
	}
	countWidth, _ := c.MeasureText(0, ui.Span{Text: countText, Size: mui.Metadata.Size, Weight: int(mui.Metadata.Weight)})
	available := w.contentWidth
	if available <= 0 {
		available = windowWidth(c)
	}
	gaps := 3
	if unreadForced {
		gaps = 2
	}
	_, searchWidth, gap, _ := fitFilterStrip(
		max(0, available-2*mui.Space(4)), 0, checkboxWidth, 0, countWidth+2, gaps,
	)

	strip := ui.Row(c.Key("rss-filter-strip")).Height(contentStripHeight).Shrink(0).FillWidth().
		Padding(0, mui.Space(4)).AlignItems(ui.Center).Gap(gap).
		Role(ui.RoleGroup).Label("Article filters")
	strip.Draw(func(p *ui.Painter, r ui.Rect) {
		p.Fill(ui.Rect{X: r.X, Y: r.Y + r.H - mui.Hairline, W: r.W, H: mui.Hairline}, t.BorderSoft, 0)
	})

	strip.Children(func() {
		if w.app != nil {
			ui.SearchField(c.Key("rss-search"), &w.app.Search).
				Width(searchWidth).MinWidth(searchWidth).MaxWidth(searchWidth).Shrink(0).
				Height(toolbarControl).FontSize(mui.Control.Size).Label(searchLabel)
			if !unreadForced {
				ui.Checkbox(c.Key("rss-unread"), &w.app.UnreadOnly, unreadLabel).
					FontSize(mui.Control.Size).FocusRingColor(t.Focus).FocusRingOffset(mui.FocusRingGap)
			}
		}
		ui.Spacer(c)
		ui.Text(c, countText).FontSize(mui.Metadata.Size).TextColor(t.Muted).SingleLine().Shrink(0)
	})
}

// rssArticlesArea renders the virtualized scrollable list of article item rows or the empty state.
func (w *Window) rssArticlesArea(c *ui.Context, t mui.Tokens, articles []core.RSSArticle, feedTitles map[string]string) {
	revealID := w.rssRevealSelection
	w.rssRevealSelection = ""
	if len(articles) == 0 {
		w.rssEmptyState(c, t)
		return
	}

	listState := w.workspaceRSSArticlesList()
	listState.Key = func(i int) any { return articles[i].ID }
	listState.Label = func(i int) string { return articles[i].Title }
	if revealID != "" {
		for i := range articles {
			if articles[i].ID == revealID {
				listState.ScrollIntoView(i)
				break
			}
		}
	}

	scroll := ui.List(c.Key("rss-articles-scroll"), listState, len(articles), func(i int) {
		art := articles[i]
		isSelected := w.app.SelectedRSSArticleID == art.ID
		w.rssArticleItemRow(c, t, art, isSelected, feedTitles, false)
	}).Grow(1).MinHeight(0).Clip().
		TrackScroll(w.workspaceRSSArticlesScroll()).
		Padding(mui.Space(2), mui.Space(3), mui.Space(3)+mui.ScrollBottomInset, mui.Space(3)).
		Gap(mui.Space(1)).
		Role(ui.RoleList).Label("Articles")

	scroll.OnShortcut(0, ui.KeyDown, func() {
		w.moveRSSSelection(articles, 1)
	})
	scroll.OnShortcut(0, ui.KeyUp, func() {
		w.moveRSSSelection(articles, -1)
	})
}

// rssArticleItemRow renders one NetNewsWire-style article card in the middle column.
func (w *Window) rssArticleItemRow(c *ui.Context, t mui.Tokens, art core.RSSArticle, isSelected bool, feedTitles map[string]string, reveal bool) {
	zh := strings.HasPrefix(strings.ToLower(c.Locale()), "zh")
	row := ui.ButtonBase(c).Key("rss-article:"+art.ID).
		FillWidth().Padding(mui.Space(2), mui.Space(2.5)).
		Radius(mui.RadiusControl).Cursor(ui.CursorPointer).
		FocusRingColor(t.Focus).FocusRingOffset(mui.FocusRingGap).
		Transition(mui.Fade).Role(ui.RoleListItem).Label(art.Title)

	row.ContextMenu(func(m *ui.Menu) {
		readLabel := "Mark as Read"
		if zh {
			readLabel = "标记为已读"
		}
		if art.Read {
			readLabel = "Mark as Unread"
			if zh {
				readLabel = "标记为未读"
			}
		}
		if m.Item(readLabel).Chosen() {
			w.toggleRSSArticleRead(c, art)
		}
		starLabel := "Star"
		if zh {
			starLabel = "标星"
		}
		if art.Starred {
			starLabel = "Unstar"
			if zh {
				starLabel = "取消标星"
			}
		}
		if m.Item(starLabel).Chosen() {
			w.toggleRSSArticleStar(c, art)
		}
	})

	row.Draw(func(p *ui.Painter, r ui.Rect) {
		if isSelected {
			p.Fill(r, t.AccentSoft, mui.RadiusControl)
		} else if row.Hovered() || row.Pressed() {
			p.Fill(r, t.SurfaceAlt, mui.RadiusControl)
		}
		if !isSelected && !row.Hovered() {
			p.Fill(ui.Rect{X: r.X + mui.Space(2), Y: r.Y + r.H - mui.Hairline, W: r.W - 2*mui.Space(2), H: mui.Hairline}, t.BorderSoft, 0)
		}
	})

	row.Children(func() {
		ui.Row(c).FillWidth().Gap(mui.Space(2)).AlignItems(ui.Start).Children(func() {
			// Left indicator column: fixed 12pt wide, holds 6pt accent dot when unread
			ui.Column(c).Width(12).AlignItems(ui.Center).Padding(4, 0, 0, 0).Children(func() {
				if !art.Read {
					ui.Row(c).Key("rss-unread-dot:"+art.ID).
						Size(6, 6).Radius(mui.RadiusCheck).Background(t.Accent).
						Label("Unread: " + art.Title)
				}
			})

			// Text column: Source+Date, Title, Snippet
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				// Line 1: Source Name (muted gray) + Published Date
				ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
					sourceName := feedTitles[art.FeedID]
					if sourceName == "" {
						sourceName = art.Author
					}
					if sourceName == "" {
						sourceName = "RSS"
					}
					mui.Metadata.Apply(ui.Text(c, sourceName)).TextColor(t.Muted).SingleLine().Shrink(1)
					if art.Starred {
						ui.Row(c).Key("rss-starred-mark:"+art.ID).Shrink(0).AlignItems(ui.Center).
							Margin(0, 0, 0, mui.Space(1)).
							Role(ui.RoleImage).Label("Starred: " + art.Title).Children(func() {
							sfsymbols.Symbol(c, "star.fill").Size(mui.Space(3)).Color(t.Accent).Element.Role(ui.RoleNone)
						})
					}
					ui.Spacer(c)
					dateStr := FormatArticleDate(art.PublishedAt, time.Now())
					mui.Metadata.Apply(ui.Text(c, dateStr)).TextColor(t.Muted).SingleLine().Shrink(0)
				})

				// Line 2: Title (Semibold, max 2 lines)
				titleColor := t.Text
				if isSelected {
					titleColor = t.Accent
				} else if art.Read {
					titleColor = t.Muted
				}
				mui.CardTitle.Apply(ui.Text(c, art.Title)).TextColor(titleColor).MaxLines(2).Wrap()

				// Line 3-4: 2-line plain text snippet
				snippet := art.Snippet
				if strings.TrimSpace(snippet) == "" {
					snippet = core.CleanSnippet(art.ContentHTML, "", 200)
				}
				if snippet != "" {
					snippetColor := t.Muted
					if art.Read {
						snippetColor = t.Faint
					}
					mui.Secondary.Apply(ui.Text(c, snippet)).TextColor(snippetColor).MaxLines(2).Wrap()
				}
			})
		})
	})

	if reveal {
		row.ScrollIntoView()
	}

	if row.Clicked() {
		w.selectRSSArticle(art.ID)
	}
}

// selectRSSArticle selects the article by ID, updating app state and firing observer.
// Opening an article also starts its full-text fetch when the feed asks for it.
func (w *Window) selectRSSArticle(id string) {
	if w.app == nil {
		return
	}
	w.app.SelectRSSArticle(id)
	art := w.app.SelectedRSSArticle()
	if art != nil {
		w.openRSSFullText(*art)
		if w.onRSSArticleSelected != nil {
			w.onRSSArticleSelected(*art)
		}
	}
}

// moveRSSSelection moves the selected article up or down in visual order.
func (w *Window) moveRSSSelection(articles []core.RSSArticle, delta int) {
	if len(articles) == 0 {
		return
	}
	current := -1
	for i, art := range articles {
		if art.ID == w.app.SelectedRSSArticleID {
			current = i
			break
		}
	}
	target := 0
	if current >= 0 {
		target = current + delta
	} else if sel := w.app.SelectedRSSArticle(); sel != nil {
		target = rssNeighbourIndex(articles, *sel, delta)
	}
	if target < 0 {
		target = 0
	}
	if target >= len(articles) {
		target = len(articles) - 1
	}
	w.selectRSSArticle(articles[target].ID)
	w.rssRevealSelection = articles[target].ID
}

// rssNeighbourIndex reports which visible article a step from sel should
// reach when sel itself is no longer listed: the first one older than it for
// +1, or past the end when it has no older neighbour; the last one newer for
// -1, or -1 when it has no newer one.
func rssNeighbourIndex(articles []core.RSSArticle, sel core.RSSArticle, delta int) int {
	if delta > 0 {
		for i, art := range articles {
			if rssArticleNewer(sel, art) {
				return i
			}
		}
		return len(articles)
	}
	target := -1
	for i, art := range articles {
		if rssArticleNewer(art, sel) {
			target = i
		}
	}
	return target
}

// FormatArticleDate formats an article's published timestamp for display in the middle list row.
// For recent articles (< 24h), it returns relative time (e.g. "just now", "15m ago", "2h ago").
// If published in the current year, it formats as "Jan 2" (e.g. "Nov 23").
// Otherwise, it formats as "Jan 2, 2006".
func FormatArticleDate(raw string, now time.Time) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return raw
	}
	t = t.Local()
	nowLocal := now.Local()
	d := nowLocal.Sub(t)
	if d > -time.Minute && d < 24*time.Hour {
		if rel := core.RelativeDate(raw, now); rel != "" {
			return rel
		}
		return "just now"
	}
	return formatArticleDay(t, nowLocal)
}

// formatArticleDay names the day a published time fell on, with the year only
// when it is not now's: "Nov 23" this year, "Dec 31, 2025" any other.
func formatArticleDay(t, now time.Time) string {
	if t.Year() == now.Year() {
		return t.Format("Jan 2")
	}
	return t.Format("Jan 2, 2006")
}

// rssEmptyState renders the empty article timeline.
func (w *Window) rssEmptyState(c *ui.Context, t mui.Tokens) {
	zh := strings.HasPrefix(strings.ToLower(c.Locale()), "zh")
	title := "No Articles"
	sentence := "Articles from subscribed feeds will appear here."
	if zh {
		title = "暂无文章"
		sentence = "订阅源的文章将显示在此处。"
	}

	unreadForced := w.app != nil && (w.app.Selected == "all" || w.app.Selected == "")
	switch {
	case w.app != nil && w.app.Search != "":
		title, sentence = rssNoMatchCopy(zh, unreadForced)
	case unreadForced && len(w.app.RSSFeeds) > 0:
		title = "You are up to date"
		sentence = "New articles will appear here when your feeds publish them."
		if zh {
			title = "已无未读"
			sentence = "订阅源更新后，新文章会出现在这里。"
		}
	case w.app != nil && w.app.UnreadOnly && !unreadForced:
		title, sentence = rssNoMatchCopy(zh, unreadForced)
	}

	mui.EmptyPane(c, t, mui.EmptySpec{
		Key: "rss-empty-timeline",
		Copy: mui.EmptyStateCopy{
			Symbol:   "newspaper",
			Title:    title,
			Sentence: sentence,
		},
	})
}

// rssNoMatchCopy words the empty column when nothing matches: it names the
// unread filter only at the destinations that still draw one.
func rssNoMatchCopy(zh, unreadForced bool) (string, string) {
	if zh {
		if unreadForced {
			return "无匹配文章", "尝试清除搜索词。"
		}
		return "无匹配文章", "尝试清除搜索词或未读过滤。"
	}
	if unreadForced {
		return "No Matching Articles", "Try clearing the search term."
	}
	return "No Matching Articles", "Try clearing search terms or the unread filter."
}

// readLaterMainArea renders the bilingual empty state for Read Later.
func (w *Window) readLaterMainArea(c *ui.Context, t mui.Tokens, rowWidth float32) {
	zh := strings.HasPrefix(strings.ToLower(c.Locale()), "zh")
	title := "Read Later"
	sentence := "Save articles and videos to read later. This feature will be available in an upcoming update."
	if zh {
		title = "稍后读"
		sentence = "保存文章与视频以便稍后阅读。该功能将在后续版本中推出。"
	}

	ui.Column(c.Key("read-later-main")).Grow(1).MinWidth(0).Background(t.App).Children(func() {
		mui.EmptyPane(c, t, mui.EmptySpec{
			Key: "read-later-empty",
			Copy: mui.EmptyStateCopy{
				Symbol:   "bookmark.fill",
				Title:    title,
				Sentence: sentence,
			},
		})
	})
}

// wechatNotesMainArea renders the bilingual empty state for WeChat Notes.
func (w *Window) wechatNotesMainArea(c *ui.Context, t mui.Tokens, rowWidth float32) {
	zh := strings.HasPrefix(strings.ToLower(c.Locale()), "zh")
	title := "WeChat Notes"
	sentence := "Sync reading notes and annotations from WeChat Read. This feature will be available in an upcoming update."
	if zh {
		title = "微信读书笔记"
		sentence = "同步微信读书的划线与阅读笔记。该功能将在后续版本中推出。"
	}

	ui.Column(c.Key("wechat-notes-main")).Grow(1).MinWidth(0).Background(t.App).Children(func() {
		mui.EmptyPane(c, t, mui.EmptySpec{
			Key: "wechat-notes-empty",
			Copy: mui.EmptyStateCopy{
				Symbol:   "book.closed.fill",
				Title:    title,
				Sentence: sentence,
			},
		})
	})
}
