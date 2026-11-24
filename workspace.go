package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo/plugins/sfsymbols"
	"github.com/egoist/mygo/ui"
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
		w.workspaceStates[WorkspaceRSS] = &WorkspaceState{Selected: "all"}
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
	}
	w.selectedIndex = st.SelectedIndex
}

// unwatchedVideoCount counts how many videos in the library are unwatched.
func (w *Window) unwatchedVideoCount() int {
	if w.app == nil {
		return 0
	}
	showShorts := w.app.settingsSnapshot().ShowShorts
	unwatched := 0
	for _, v := range w.app.Videos {
		if !w.app.videoVisible(v, nil, showShorts) {
			continue
		}
		if !w.app.Sync.IsRead(v.ID) {
			unwatched++
		}
	}
	return unwatched
}

type dockItemMeta struct {
	ID          string
	Symbol      string
	Title       string
	Count       string
	Tooltip     string
	Placeholder bool
}

func (w *Window) dockMeta(locale, id string) dockItemMeta {
	zh := strings.HasPrefix(strings.ToLower(locale), "zh")
	switch id {
	case WorkspaceVideos:
		count := w.unwatchedVideoCount()
		title := "Videos"
		tooltip := fmt.Sprintf("Videos (%d unwatched)", count)
		if zh {
			title = "视频"
			tooltip = fmt.Sprintf("视频 (%d 未看)", count)
		}
		return dockItemMeta{
			ID:      WorkspaceVideos,
			Symbol:  "play.rectangle.fill",
			Title:   title,
			Count:   fmt.Sprintf("%d", count),
			Tooltip: tooltip,
		}
	case WorkspaceRSS:
		count := 0
		if w.app != nil {
			count = w.app.RSSTotalUnread
		}
		title := "RSS"
		tooltip := fmt.Sprintf("RSS Articles (%d unread)", count)
		if zh {
			title = "RSS 文章"
			tooltip = fmt.Sprintf("RSS 文章 (%d 未读)", count)
		}
		return dockItemMeta{
			ID:      WorkspaceRSS,
			Symbol:  "newspaper.fill",
			Title:   title,
			Count:   fmt.Sprintf("%d", count),
			Tooltip: tooltip,
		}
	case WorkspaceReadLater:
		title := "Read Later"
		tooltip := "Read Later (Coming soon)"
		if zh {
			title = "稍后读"
			tooltip = "稍后读（后续提供）"
		}
		return dockItemMeta{
			ID:          WorkspaceReadLater,
			Symbol:      "bookmark.fill",
			Title:       title,
			Count:       "0",
			Tooltip:     tooltip,
			Placeholder: true,
		}
	case WorkspaceWeChatNotes:
		title := "WeChat Notes"
		tooltip := "WeChat Notes (Coming soon)"
		if zh {
			title = "微信读书笔记"
			tooltip = "微信读书笔记（后续提供）"
		}
		return dockItemMeta{
			ID:          WorkspaceWeChatNotes,
			Symbol:      "book.closed.fill",
			Title:       title,
			Count:       "0",
			Tooltip:     tooltip,
			Placeholder: true,
		}
	}
	return dockItemMeta{ID: id, Symbol: "square", Title: id}
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
			meta := w.dockMeta(locale, id)
			selected := id == active

			slot := ui.Box(c.Key("dock-item-" + id)).
				Grow(1).MinWidth(0).Height(46).Radius(mui.RadiusControl).
				Role(ui.RoleTab).Label(meta.Title).Tooltip(meta.Tooltip).
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

			iconColor := t.Muted
			countColor := t.Muted
			if meta.Placeholder {
				iconColor = t.Faint
				countColor = t.Faint
			}
			switch {
			case selected:
				iconColor = t.Accent
				countColor = t.Accent
			case slot.Hovered() || slot.Pressed():
				iconColor = t.Text
				countColor = t.Text
			}

			slot.Draw(func(p *ui.Painter, r ui.Rect) {
				switch {
				case selected:
					p.Fill(r, t.AccentSoft, mui.RadiusControl)
				case slot.Pressed():
					p.Fill(r, t.AccentSoft, mui.RadiusControl)
				case slot.Hovered():
					p.Fill(r, t.SurfaceAlt, mui.RadiusControl)
				}

				if over && dragged.ID != "" && dragged.ID != id {
					indX := r.X
					if insertAfter {
						indX = r.X + r.W - 2
					}
					p.Fill(ui.Rect{X: indX, Y: r.Y + 4, W: 2, H: r.H - 8}, t.Accent, 1)
				}

				if meta.Count != "" {
					cw, _ := p.MeasureText(0, ui.Span{Text: meta.Count, Size: 11})
					cx := r.X + (r.W-cw)/2
					cy := r.Y + 28
					p.Text(cx, cy, meta.Count, 11, countColor)
				}
			})

			slot.Children(func() {
				ui.Column(c).Fill().AlignItems(ui.Center).Padding(4, 0, 0, 0).Children(func() {
					sfsymbols.Symbol(c, meta.Symbol).Size(18).Color(iconColor).Element.Role(ui.RoleNone)
				})
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

// rssMainArea renders the 3-column RSS layout with middle timeline and right reader view.
func (w *Window) rssMainArea(c *ui.Context, t mui.Tokens, rowWidth float32) {
	d := &w.detail
	detailsWidth := detailWidthForRoom(d.width, rowWidth)
	column2Width := max(rowWidth-detailsWidth-splitDividerWidth(), 0)
	w.contentWidth = column2Width
	w.gridAvailableWidth = column2Width

	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			w.rssContent(c, t)
		})
		line := w.detailsDivider(c, t)
		ui.Column(c).Width(max(detailsWidth, 0)).Shrink(0).Children(func() {
			w.rssReader(c, t)
		})
		w.detailsDividerGrip(c, line, w.contentWidth)
	})
}

// rssContent renders Column 2: the RSS middle timeline and search filter bar.
func (w *Window) rssContent(c *ui.Context, t mui.Tokens) {
	ui.Column(c).Grow(1).MinWidth(0).Background(t.App).Children(func() {
		w.rssContentStrip(c, t)
		w.rssEmptyState(c, t)
	})
}

// rssContentStrip renders the filter and search bar for RSS articles.
func (w *Window) rssContentStrip(c *ui.Context, t mui.Tokens) {
	zh := strings.HasPrefix(strings.ToLower(c.Locale()), "zh")
	searchLabel := "Search articles"
	unreadLabel := "Unread"
	countText := "0 articles"
	if zh {
		searchLabel = "搜索文章"
		unreadLabel = "未读"
		countText = "0 篇文章"
	}

	labelWidth, _ := c.MeasureText(0, ui.Span{Text: unreadLabel, Size: mui.Control.Size, Weight: int(mui.Control.Weight)})
	countWidth, _ := c.MeasureText(0, ui.Span{Text: countText, Size: mui.Metadata.Size, Weight: int(mui.Metadata.Weight)})
	checkboxWidth := mui.Space(4) + mui.Space(2) + labelWidth
	available := w.contentWidth
	if available <= 0 {
		available = windowWidth(c)
	}
	_, searchWidth, gap, _ := fitFilterStrip(
		max(0, available-2*mui.Space(4)), 0, checkboxWidth, 0, countWidth+2, 3,
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
			ui.Checkbox(c.Key("rss-unread"), &w.app.UnreadOnly, unreadLabel).
				FontSize(mui.Control.Size).FocusRingColor(t.Focus).FocusRingOffset(mui.FocusRingGap)
		}
		ui.Spacer(c)
		ui.Text(c, countText).FontSize(mui.Metadata.Size).TextColor(t.Muted).SingleLine().Shrink(0)
	})
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

	mui.EmptyPane(c, t, mui.EmptySpec{
		Key: "rss-empty-timeline",
		Copy: mui.EmptyStateCopy{
			Symbol:   "newspaper",
			Title:    title,
			Sentence: sentence,
		},
	})
}

// rssReader renders Column 3: the RSS reader view pane.
func (w *Window) rssReader(c *ui.Context, t mui.Tokens) {
	zh := strings.HasPrefix(strings.ToLower(c.Locale()), "zh")
	title := "No Article Selected"
	sentence := "Select an article from the list to read."
	if zh {
		title = "未选择文章"
		sentence = "从列表中选择一篇文章进行阅读。"
	}

	ui.Column(c.Key("rss-reader-pane")).Fill().Background(t.App).Children(func() {
		mui.EmptyPane(c, t, mui.EmptySpec{
			Key: "rss-reader-empty",
			Copy: mui.EmptyStateCopy{
				Symbol:   "doc.text",
				Title:    title,
				Sentence: sentence,
			},
		})
	})
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
