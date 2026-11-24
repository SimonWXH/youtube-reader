package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/sfsymbols"
	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
	mui "github.com/simon/youtube-reader/ui"
)

type rssGroupDrag struct{ ID string }
type rssFeedDrag struct{ ID, GroupID string }

type rssGroupDragFrame struct {
	alive   bool
	dropped bool
	missed  bool
}

type rssFeedDragFrame struct {
	alive   bool
	feedID  string
	groupID string
}

func (w *Window) startRenameRSSGroup(id, name string) {
	if w.renamingRSSGroupID != "" && w.renamingRSSGroupID != id {
		w.commitRenameRSSGroup(w.renamingRSSGroupID, w.renamingRSSGroupName)
	}
	if w.renamingRSSFeedID != "" {
		w.commitRenameRSSFeed(w.renamingRSSFeedID, w.renamingRSSFeedName)
	}
	w.renamingRSSGroupID = id
	w.renamingRSSGroupName = name
}

func (w *Window) commitRenameRSSGroup(id, newName string) {
	created := w.renamingRSSGroupNew == id
	w.renamingRSSGroupID = ""
	w.renamingRSSGroupName = ""
	w.renamingRSSGroupNew = ""
	newName = strings.TrimSpace(newName)
	if newName == "" {
		if created && w.app != nil {
			_ = w.app.DeleteRSSGroup(id)
		}
		return
	}
	if w.app != nil {
		if err := w.app.RenameRSSGroup(id, newName); err != nil {
			log.Printf("rename rss group: %v", err)
		}
	}
}

func (w *Window) cancelRenameRSSGroup() {
	if id := w.renamingRSSGroupNew; id != "" && w.app != nil {
		_ = w.app.DeleteRSSGroup(id)
	}
	w.renamingRSSGroupID = ""
	w.renamingRSSGroupName = ""
	w.renamingRSSGroupNew = ""
}

func (w *Window) startRenameRSSFeed(id, name string) {
	if w.renamingRSSGroupID != "" {
		w.commitRenameRSSGroup(w.renamingRSSGroupID, w.renamingRSSGroupName)
	}
	if w.renamingRSSFeedID != "" && w.renamingRSSFeedID != id {
		w.commitRenameRSSFeed(w.renamingRSSFeedID, w.renamingRSSFeedName)
	}
	w.renamingRSSFeedID = id
	w.renamingRSSFeedName = name
}

func (w *Window) commitRenameRSSFeed(id, newName string) {
	w.renamingRSSFeedID = ""
	w.renamingRSSFeedName = ""
	newName = strings.TrimSpace(newName)
	if newName == "" || w.app == nil {
		return
	}
	if err := w.app.RenameRSSFeed(id, newName); err != nil {
		log.Printf("rename rss feed: %v", err)
	}
}

func (w *Window) cancelRenameRSSFeed() {
	w.renamingRSSFeedID = ""
	w.renamingRSSFeedName = ""
}

func (w *Window) moveRSSFeedToGroup(c *ui.Context, feedID, targetGroupID string) {
	if w.app == nil {
		return
	}
	if err := w.app.MoveRSSFeedToGroup(feedID, targetGroupID); err != nil {
		log.Printf("move rss feed: %v", err)
		return
	}
	if targetGroupID != "" {
		_ = w.app.SetRSSGroupCollapsed(targetGroupID, false)
	}
	if c != nil {
		c.Invalidate()
	}
}

func failureTooltip(locale string, f core.RSSFeed) string {
	if f.FailureCount <= 0 && f.LastError == "" {
		return ""
	}
	zh := strings.HasPrefix(strings.ToLower(locale), "zh")
	errMsg := f.LastError
	if errMsg == "" {
		errMsg = "Unknown error"
	}
	if zh {
		if f.FailureCount > 1 {
			return fmt.Sprintf("抓取失败（连续失败 %d 次）：%s", f.FailureCount, errMsg)
		}
		return fmt.Sprintf("抓取失败：%s", errMsg)
	}
	if f.FailureCount > 1 {
		return fmt.Sprintf("Fetch failed (%d attempts): %s", f.FailureCount, errMsg)
	}
	return fmt.Sprintf("Fetch failed: %s", errMsg)
}

func deleteRSSGroupCopy(locale, groupName string) unsubscribeDialogCopy {
	if strings.HasPrefix(strings.ToLower(locale), "zh") {
		return unsubscribeDialogCopy{
			title:      fmt.Sprintf("删除分组 %s？", groupName),
			detail:     "分组内的订阅源不会被删除，将保留在未分组中。",
			cancelBtn:  "取消",
			confirmBtn: "删除分组",
		}
	}
	return unsubscribeDialogCopy{
		title:      fmt.Sprintf("Delete group %s?", groupName),
		detail:     "Feeds in this group will not be deleted and will become ungrouped.",
		cancelBtn:  "Cancel",
		confirmBtn: "Delete Group",
	}
}

func unsubscribeRSSCopy(locale, feedTitle string) unsubscribeDialogCopy {
	if strings.HasPrefix(strings.ToLower(locale), "zh") {
		return unsubscribeDialogCopy{
			title:      fmt.Sprintf("取消订阅 %s？", feedTitle),
			detail:     "该订阅源及其未标星的文章将从本地列表中移除。",
			cancelBtn:  "取消",
			confirmBtn: "取消订阅",
		}
	}
	return unsubscribeDialogCopy{
		title:      fmt.Sprintf("Unsubscribe from %s?", feedTitle),
		detail:     "This feed and its un-starred articles will be removed from your library.",
		cancelBtn:  "Cancel",
		confirmBtn: "Unsubscribe",
	}
}

func (w *Window) confirmDeleteRSSGroup(c *ui.Context, group core.RSSGroup) {
	words := deleteRSSGroupCopy(c.Locale(), group.Name)
	result, err := dialogMessage(mygo.MessageOptions{
		Parent:             w.dialogParent(),
		Type:               mygo.MessageWarning,
		Title:              words.title,
		Message:            words.title,
		Detail:             words.detail,
		Buttons:            []string{words.cancelBtn, words.confirmBtn},
		DefaultButton:      0,
		CancelButton:       0,
		DestructiveButtons: []int{1},
	})
	if err != nil || result.Button != 1 {
		return
	}
	if err := w.app.DeleteRSSGroup(group.ID); err != nil {
		w.toast(c, "Delete group failed: "+err.Error(), true)
		return
	}
	if w.app.Selected == "group:"+group.ID {
		w.app.SelectDestination("all")
	}
	c.Invalidate()
	w.invalidateMainWindow()
}

func (w *Window) confirmUnsubscribeRSSFeed(c *ui.Context, feed core.RSSFeed) {
	words := unsubscribeRSSCopy(c.Locale(), feed.DisplayName())
	result, err := dialogMessage(mygo.MessageOptions{
		Parent:             w.dialogParent(),
		Type:               mygo.MessageWarning,
		Title:              words.title,
		Message:            words.title,
		Detail:             words.detail,
		Buttons:            []string{words.cancelBtn, words.confirmBtn},
		DefaultButton:      0,
		CancelButton:       0,
		DestructiveButtons: []int{1},
	})
	if err != nil || result.Button != 1 {
		return
	}
	if err := w.app.DeleteRSSFeed(feed.ID); err != nil {
		w.toast(c, "Unsubscribe failed: "+err.Error(), true)
		return
	}
	if w.app.Selected == "feed:"+feed.ID {
		w.app.SelectDestination("all")
	}
	c.Invalidate()
	w.invalidateMainWindow()
}

// rssSidebar renders the RSS workspace's sources and smart feeds in the sidebar.
func (w *Window) rssSidebar(c *ui.Context, t mui.Tokens, sidebar ui.Element) {
	nav := &sidebarNav{w: w, focus: w.sidebarFocus, actions: map[string]func(){}}
	w.sidebarFocus = ""
	zh := strings.HasPrefix(strings.ToLower(c.Locale()), "zh")
	feedsHeader := "Feeds"
	allArticlesLabel := "All articles"
	starredLabel := "Starred"
	noFeedsTitle := "No RSS feeds yet"
	noFeedsDesc := "Articles will appear here once feeds are subscribed."
	addFeedBtn := "Add RSS Feed"
	if zh {
		feedsHeader = "订阅源"
		allArticlesLabel = "全部文章"
		starredLabel = "标星文章"
		noFeedsTitle = "暂无 RSS 订阅"
		noFeedsDesc = "添加订阅源后，文章将显示在此处。"
		addFeedBtn = "添加订阅源"
	}

	var drag rssGroupDragFrame
	var fDrag rssFeedDragFrame

	totalUnreadCount := 0
	starredCount := 0
	if w.app != nil {
		totalUnreadCount = w.app.RSSTotalUnread
		starredCount = w.app.RSSStarredTotal
	}

	ui.Scroll(c.Key("rss-sidebar-scroll")).Grow(1).MinHeight(0).Clip().
		TrackScroll(w.workspaceSidebarScroll(WorkspaceRSS)).
		Padding(0, mui.Space(2), mui.Space(2)+mui.ScrollBottomInset).Children(func() {
		// 1. Smart Feeds section
		sectionHeader(c, t, feedsHeader)
		nav.row(c, t, mui.SidebarRow{
			Key: "rss-all", Label: allArticlesLabel, Symbol: "newspaper", Count: fmt.Sprintf("%d", totalUnreadCount),
			Selected: w.app != nil && w.app.Selected == "all",
			OnClick: func() {
				w.commitActiveRSSRenames()
				if w.app != nil {
					w.app.SelectDestination("all")
				}
			},
		})
		nav.row(c, t, mui.SidebarRow{
			Key: "rss-starred", Label: starredLabel, Symbol: "star", Count: fmt.Sprintf("%d", starredCount),
			Selected: w.app != nil && w.app.Selected == "starred",
			OnClick: func() {
				w.commitActiveRSSRenames()
				if w.app != nil {
					w.app.SelectDestination("starred")
				}
			},
		})

		if w.app == nil || len(w.app.RSSFeeds) == 0 {
			ui.Column(c.Key("rss-sidebar-empty")).FillWidth().Padding(mui.Space(4), mui.Space(2)).AlignItems(ui.Center).Gap(mui.Space(2)).Children(func() {
				ui.Text(c, noFeedsTitle).FontSize(13).TextColor(t.Muted).SingleLine()
				ui.Text(c, noFeedsDesc).FontSize(11).TextColor(t.Faint).SingleLine()
				btn := ui.ButtonBase(c).Key("rss-empty-add-btn").Height(28).PaddingX(mui.Space(3)).
					Radius(mui.RadiusControl).Background(t.Accent).TextColor(t.AccentText).
					Cursor(ui.CursorPointer).Label(addFeedBtn)
				mui.Control.Apply(btn)
				btn.Children(func() {
					mui.Control.Apply(ui.Text(c, addFeedBtn)).TextColor(t.AccentText).SingleLine()
				})
				btn.OnClick(func() {
					w.openAddRSSDialog()
				})

				importBtnText := "Import OPML…"
				if zh {
					importBtnText = "导入 OPML…"
				}
				importBtn := ui.ButtonBase(c).Key("rss-empty-import-btn").Height(26).PaddingX(mui.Space(3)).
					Radius(mui.RadiusControl).Background(t.SurfaceAlt).Border(mui.Hairline, t.BorderSoft).
					Cursor(ui.CursorPointer).Label(importBtnText)
				mui.Control.Apply(importBtn)
				importBtn.Children(func() {
					mui.Control.Apply(ui.Text(c, importBtnText)).FontSize(12).TextColor(t.Text).SingleLine()
				})
				importBtn.OnClick(func() {
					w.importRSSOPML(c)
				})
			})
		} else {
			// 2. Render Groups
			for i, g := range w.app.RSSGroups {
				w.rssGroupSection(c, t, g, i, nav, &drag, &fDrag)
			}

			// 3. Render Ungrouped Feeds
			ungroupedFeeds := w.rssUngroupedFeeds()
			feedDragActive := fDrag.alive

			if len(ungroupedFeeds) > 0 {
				ungroupedBlock := ui.Column(c.Key("rss-ungrouped-block")).FillWidth()
				draggedFeed, feedOver := ui.DragOver[rssFeedDrag](ungroupedBlock)
				droppedFeed, feedDropped := ui.Drop[rssFeedDrag](ungroupedBlock)
				if feedDropped && droppedFeed.GroupID != "" {
					w.moveRSSFeedToGroup(c, droppedFeed.ID, "")
				}
				dropHover := feedOver && draggedFeed.GroupID != ""

				ungroupedBlock.Children(func() {
					if dropHover {
						w.renderUngroupedDropRow(c, t, true)
					} else {
						sectionHeader(c, t, ungroupedHeader(c.Locale()))
					}

					for _, f := range ungroupedFeeds {
						w.renderRSSFeedRow(c, t, f, "", nav, &fDrag)
					}
				})
			} else if feedDragActive {
				ungroupedStrip := ui.Column(c.Key("rss-ungrouped-drop-strip")).FillWidth()
				draggedFeed, feedOver := ui.DragOver[rssFeedDrag](ungroupedStrip)
				droppedFeed, feedDropped := ui.Drop[rssFeedDrag](ungroupedStrip)
				if feedDropped && droppedFeed.GroupID != "" {
					w.moveRSSFeedToGroup(c, droppedFeed.ID, "")
				}
				dropHover := feedOver && draggedFeed.GroupID != ""

				ungroupedStrip.Children(func() {
					w.renderUngroupedDropRow(c, t, dropHover)
				})
			}
		}
	})

	w.rssSidebarFooter(c, t)
	w.settleRSSGroupDrag(c, &drag)

	if nav.focused != "" && nav.focused != w.sidebarKeyboard {
		w.sidebarKeyboard = ""
	}
	sidebar.OnShortcut(0, ui.KeyDown, nav.move(1))
	sidebar.OnShortcut(0, ui.KeyUp, nav.move(-1))
}

func (w *Window) commitActiveRSSRenames() {
	if w.renamingRSSGroupID != "" {
		w.commitRenameRSSGroup(w.renamingRSSGroupID, w.renamingRSSGroupName)
	}
	if w.renamingRSSFeedID != "" {
		w.commitRenameRSSFeed(w.renamingRSSFeedID, w.renamingRSSFeedName)
	}
}

func (w *Window) rssUngroupedFeeds() []core.RSSFeed {
	if w.app == nil {
		return nil
	}
	var ungrouped []core.RSSFeed
	for _, f := range w.app.RSSFeeds {
		if len(f.GroupIDs) == 0 {
			ungrouped = append(ungrouped, f)
		}
	}
	return orderRSSFeeds(ungrouped, w.app.settingsSnapshot().RSSUngroupedOrder)
}

func (w *Window) rssGroupSection(c *ui.Context, t mui.Tokens, g core.RSSGroup, index int, nav *sidebarNav, drag *rssGroupDragFrame, fDrag *rssFeedDragFrame) {
	collapsed := w.app.RSSGroupCollapsed(g.ID)
	renaming := w.renamingRSSGroupID == g.ID
	block := ui.Column(c.Key("rss-group-drop:" + g.ID)).FillWidth()

	draggedFeed, feedOver := ui.DragOver[rssFeedDrag](block)
	droppedFeed, feedDropped := ui.Drop[rssFeedDrag](block)
	if feedDropped && droppedFeed.GroupID != g.ID {
		w.moveRSSFeedToGroup(c, droppedFeed.ID, g.ID)
	}
	validDropTarget := !renaming && feedOver && draggedFeed.GroupID != g.ID

	// Calculate group unread count
	groupUnread := 0
	if w.app != nil {
		for _, fid := range g.FeedIDs {
			groupUnread += w.app.RSSUnreadMap[fid]
		}
	}

	block.Children(func() {
		section := ui.Column(c.Key("rss-group-block:" + g.ID)).FillWidth()
		section.Children(func() {
			row := nav.row(c, t, mui.SidebarRow{
				Key:            "rss-group:" + g.ID,
				Label:          g.Name,
				Count:          fmt.Sprintf("%d", groupUnread),
				Fold:           true,
				Expanded:       !collapsed,
				Selected:       w.app != nil && w.app.Selected == "group:"+g.ID,
				DropHover:      validDropTarget,
				DropHoverLabel: moveHereLabel(c.Locale()),
				OnClick: func() {
					w.commitActiveRSSRenames()
					if w.app != nil {
						w.app.SelectDestination("group:" + g.ID)
					}
				},
				OnFoldClick: func() {
					w.commitActiveRSSRenames()
					_ = w.app.SetRSSGroupCollapsed(g.ID, !collapsed)
				},
				OnDoubleClick: func() {
					w.startRenameRSSGroup(g.ID, g.Name)
					c.Invalidate()
				},
				OnContextMenu: func(m *ui.Menu) {
					renameText := renameMenuLabel(c.Locale())
					if m.Item(renameText).Chosen() {
						w.startRenameRSSGroup(g.ID, g.Name)
					}
					deleteText := "Delete Group"
					if strings.HasPrefix(strings.ToLower(c.Locale()), "zh") {
						deleteText = "删除分组"
					}
					if m.Item(deleteText).Chosen() {
						w.confirmDeleteRSSGroup(c, g)
					}
				},
				Renaming:    renaming,
				RenameValue: &w.renamingRSSGroupName,
				OnRename: func(newName string) {
					w.commitRenameRSSGroup(g.ID, newName)
					c.Invalidate()
				},
				OnRenameCancel: func() {
					w.cancelRenameRSSGroup()
					c.Invalidate()
				},
			})

			if !renaming {
				row.Drag(rssGroupDrag{ID: g.ID})
				if row.Dragging() {
					drag.alive = true
					row.Opacity(0.4)
				}
				if _, missed := row.DragMissed(); missed {
					drag.missed = true
				}
			}

			if collapsed {
				return
			}

			// Render member feeds
			feedsInGroup := make([]core.RSSFeed, 0, len(g.FeedIDs))
			for _, fid := range g.FeedIDs {
				f := w.app.RSSFeedByID(fid)
				if f.ID != "" {
					feedsInGroup = append(feedsInGroup, f)
				}
			}
			// Include any feeds whose GroupIDs contains g.ID but not yet in g.FeedIDs
			for _, f := range w.app.RSSFeeds {
				if slices.Contains(f.GroupIDs, g.ID) && !slices.Contains(g.FeedIDs, f.ID) {
					feedsInGroup = append(feedsInGroup, f)
				}
			}

			for _, f := range feedsInGroup {
				w.renderRSSFeedRow(c, t, f, g.ID, nav, fDrag)
			}
		})

		w.rssGroupSectionDrop(c, section, index, drag)
	})
}

func (w *Window) renderRSSFeedRow(c *ui.Context, t mui.Tokens, f core.RSSFeed, groupID string, nav *sidebarNav, fDrag *rssFeedDragFrame) {
	renaming := w.renamingRSSFeedID == f.ID
	unread := 0
	if w.app != nil {
		unread = w.app.RSSUnreadMap[f.ID]
	}

	countStr := ""
	if unread > 0 {
		countStr = fmt.Sprintf("%d", unread)
	}

	hasError := f.FailureCount > 0
	errTip := failureTooltip(c.Locale(), f)

	feedBlock := ui.Column(c.Key(fmt.Sprintf("rss-feed-drop:%s:%s", groupID, f.ID))).FillWidth()
	draggedFeed, feedOver := ui.DragOver[rssFeedDrag](feedBlock)
	droppedFeed, feedDropped := ui.Drop[rssFeedDrag](feedBlock)

	if feedDropped && droppedFeed.ID != f.ID {
		w.reorderRSSFeedNear(c, droppedFeed.ID, droppedFeed.GroupID, f.ID, groupID)
	}

	feedBlock.Children(func() {
		row := nav.row(c, t, mui.SidebarRow{
			Key:          fmt.Sprintf("rss-feed:%s:%s", groupID, f.ID),
			Label:        f.DisplayName(),
			Channel:      true,
			Avatar:       f.AvatarURL,
			Count:        countStr,
			Selected:     w.app != nil && w.app.Selected == "feed:"+f.ID,
			Error:        hasError,
			ErrorTooltip: errTip,
			OnClick: func() {
				w.commitActiveRSSRenames()
				if w.app != nil {
					w.app.SelectDestination("feed:" + f.ID)
				}
			},
			OnDoubleClick: func() {
				w.startRenameRSSFeed(f.ID, f.DisplayName())
				c.Invalidate()
			},
			OnContextMenu: func(m *ui.Menu) {
				renameText := renameMenuLabel(c.Locale())
				if m.Item(renameText).Chosen() {
					w.startRenameRSSFeed(f.ID, f.DisplayName())
				}
				copyURLText := "Copy Feed URL"
				if strings.HasPrefix(strings.ToLower(c.Locale()), "zh") {
					copyURLText = "复制源地址"
				}
				if m.Item(copyURLText).Chosen() {
					c.WriteClipboardThen(f.FeedURL, func(err error) {
						if err == nil {
							copiedToast := "Feed URL copied to clipboard"
							if strings.HasPrefix(strings.ToLower(c.Locale()), "zh") {
								copiedToast = "已复制源地址到剪贴板"
							}
							w.toast(c, copiedToast, false)
						}
					})
				}
				if f.SiteURL != "" {
					openWebText := "Open Website"
					if strings.HasPrefix(strings.ToLower(c.Locale()), "zh") {
						openWebText = "打开网站"
					}
					if m.Item(openWebText).Chosen() {
						c.OpenURL(f.SiteURL)
					}
				}
				refreshText := "Refresh Feed"
				if strings.HasPrefix(strings.ToLower(c.Locale()), "zh") {
					refreshText = "刷新订阅源"
				}
				if m.Item(refreshText).Chosen() {
					go func() {
						ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
						defer cancel()
						_, _ = w.app.FetchRSSFeed(ctx, f.ID)
						mygo.RunOnMain(func() {
							_ = w.app.ReloadRSS()
							w.invalidateMainWindow()
						})
					}()
				}
				unsubText := unsubscribeMenuLabel(c.Locale())
				if m.Item(unsubText).Chosen() {
					w.confirmUnsubscribeRSSFeed(c, f)
				}
			},
			Renaming:    renaming,
			RenameValue: &w.renamingRSSFeedName,
			OnRename: func(newName string) {
				w.commitRenameRSSFeed(f.ID, newName)
				c.Invalidate()
			},
			OnRenameCancel: func() {
				w.cancelRenameRSSFeed()
				c.Invalidate()
			},
		})

		if !renaming {
			row.Drag(rssFeedDrag{ID: f.ID, GroupID: groupID})
			if row.Dragging() {
				fDrag.alive = true
				fDrag.feedID = f.ID
				fDrag.groupID = groupID
				row.Opacity(0.4)
			}
		}

		if feedOver && draggedFeed.ID != f.ID {
			feedBlock.Border(1.5, t.Accent)
		}
	})
}

func (w *Window) reorderRSSFeedNear(c *ui.Context, srcFeedID, srcGroupID, targetFeedID, targetGroupID string) {
	if w.app == nil || srcFeedID == targetFeedID {
		return
	}
	// If moving to a different group
	if srcGroupID != targetGroupID {
		_ = w.app.MoveRSSFeedToGroup(srcFeedID, targetGroupID)
	}

	if targetGroupID != "" {
		g := w.app.RSSGroupByID(targetGroupID)
		var feedIDs []string
		for _, fid := range g.FeedIDs {
			if fid != srcFeedID {
				feedIDs = append(feedIDs, fid)
			}
		}
		targetIdx := slices.Index(feedIDs, targetFeedID)
		if targetIdx < 0 {
			feedIDs = append(feedIDs, srcFeedID)
		} else {
			feedIDs = slices.Insert(feedIDs, targetIdx, srcFeedID)
		}
		_ = w.app.ReorderRSSFeedsInGroup(targetGroupID, feedIDs)
	} else {
		ungrouped := w.rssUngroupedFeeds()
		var feedIDs []string
		for _, f := range ungrouped {
			if f.ID != srcFeedID {
				feedIDs = append(feedIDs, f.ID)
			}
		}
		targetIdx := slices.Index(feedIDs, targetFeedID)
		if targetIdx < 0 {
			feedIDs = append(feedIDs, srcFeedID)
		} else {
			feedIDs = slices.Insert(feedIDs, targetIdx, srcFeedID)
		}
		_ = w.app.SetRSSUngroupedOrder(feedIDs)
	}

	if c != nil {
		c.Invalidate()
	}
}

func (w *Window) rssGroupSectionDrop(c *ui.Context, section ui.Element, index int, drag *rssGroupDragFrame) {
	dragged, over := ui.DragOver[rssGroupDrag](section)
	droppedGroup, dropped := ui.Drop[rssGroupDrag](section)
	if dropped {
		drag.dropped = true
	}
	if !over && !dropped {
		return
	}
	to := index
	if _, y, inside := section.PointerPosition(); inside && y >= section.LayoutBounds().H/2 {
		to = index + 1
	}
	id := dragged.ID
	if dropped {
		id = droppedGroup.ID
	}
	if w.liveMoveRSSGroup(id, to) {
		c.Invalidate()
	}
}

func (w *Window) liveMoveRSSGroup(id string, to int) bool {
	if w.rssGroupDragOrder == nil {
		w.rssGroupDragOrder = rssGroupIDs(w.app.RSSGroups)
	}
	return w.moveRSSGroup(id, to)
}

func rssGroupIDs(groups []core.RSSGroup) []string {
	ids := make([]string, 0, len(groups))
	for _, g := range groups {
		ids = append(ids, g.ID)
	}
	return ids
}

func (w *Window) moveRSSGroup(id string, to int) bool {
	from := -1
	for i, g := range w.app.RSSGroups {
		if g.ID == id {
			from = i
			break
		}
	}
	if from < 0 || to == from || to == from+1 {
		return false
	}
	picked := w.app.RSSGroups[from]
	rest := make([]core.RSSGroup, 0, len(w.app.RSSGroups)-1)
	for i, g := range w.app.RSSGroups {
		if i != from {
			rest = append(rest, g)
		}
	}
	if to > from {
		to--
	}
	to = clampInt(to, 0, len(rest))
	ordered := make([]core.RSSGroup, 0, len(w.app.RSSGroups))
	ordered = append(ordered, rest[:to]...)
	ordered = append(ordered, picked)
	ordered = append(ordered, rest[to:]...)
	w.app.RSSGroups = ordered
	return true
}

func (w *Window) settleRSSGroupDrag(c *ui.Context, drag *rssGroupDragFrame) {
	if w.rssGroupDragOrder == nil {
		return
	}
	switch {
	case drag.dropped || drag.missed:
		_ = w.app.SetRSSGroupOrder(rssGroupIDs(w.app.RSSGroups))
		w.rssGroupDragOrder = nil
	case !drag.alive:
		w.app.RSSGroups = orderRSSGroups(w.app.RSSGroups, w.rssGroupDragOrder)
		w.rssGroupDragOrder = nil
		c.Invalidate()
	}
}

func (w *Window) rssSidebarFooter(c *ui.Context, t mui.Tokens) {
	bar := ui.Row(c).Key("rss-sidebar-footer").Height(sidebarFooterHeight).Shrink(0).FillWidth().
		AlignItems(ui.Center).Padding(0, sidebarFooterInset).Background(t.Sidebar).
		Role(ui.RoleGroup).Label("RSS actions")
	bar.Draw(func(p *ui.Painter, r ui.Rect) {
		p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: mui.Hairline}, t.BorderSoft, 0)
	})
	bar.Children(func() {
		w.rssSidebarAddButton(c, t)
	})
}

func (w *Window) rssSidebarAddButton(c *ui.Context, t mui.Tokens) {
	button := ui.ButtonBase(c).Key("rss-sidebar-add").Size(sidebarAddSize, sidebarAddSize).Padding(0).
		Radius(mui.RadiusControl).Label("New group or RSS subscription").
		Cursor(ui.CursorPointer).Transition(mui.Fade).
		FocusRingColor(t.Focus).FocusRingOffset(mui.FocusRingGap)
	color := t.Muted
	switch {
	case w.rssSidebarMenuOpen:
		color = t.Accent
	case button.Hovered() || button.Pressed():
		color = t.Text
	}
	button.Children(func() {
		sfsymbols.Symbol(c, "plus").Size(sidebarAddGlyph).Color(color).Element.Role(ui.RoleNone)
	})
	button.Draw(func(p *ui.Painter, r ui.Rect) {
		switch {
		case w.rssSidebarMenuOpen || button.Pressed():
			p.Fill(r, t.AccentSoft, mui.RadiusControl)
			p.Stroke(r, t.Accent, mui.RadiusControl, mui.Hairline)
		case button.Hovered():
			p.Fill(r, t.SurfaceAlt, mui.RadiusControl)
			p.Stroke(r, t.BorderSoft, mui.RadiusControl, mui.Hairline)
		}
	})

	words := rssSidebarMenuCopy(c.Locale())
	button.PopoverMenuButton(func(m *ui.Menu) {
		if m.Item(words.newGroup).Icon(sidebarMenuNewGroupIcon).Chosen() {
			w.newRSSGroupFromMenu(c)
		}
		if m.Item(words.addSubscription).Icon(sidebarMenuAddSubIcon).Chosen() {
			w.openAddRSSDialog()
		}
		m.Separator()
		if m.Item(words.importOPML).Icon(sidebarMenuImportOPMLIcon).Chosen() {
			w.importRSSOPML(c)
		}
		if m.Item(words.exportOPML).Icon(sidebarMenuExportOPMLIcon).Chosen() {
			w.exportRSSOPML(c)
		}
	}, ui.PopoverMenuButtonOptions{
		At: ui.AnchorTopStart, Self: ui.AnchorBottomStart, Open: &w.rssSidebarMenuOpen,
		Panel: func(panel ui.Element) {
			panel.MinWidth(sidebarMenuWidth).Radius(mui.RadiusNotice).
				Border(mui.Hairline/2, t.Border).
				Shadow(0, mui.ElevationOffsetY, mui.ElevationBlur, mui.ElevationSpread, t.Shadow)
		},
	})
}

type rssSidebarMenuWords struct {
	newGroup        string
	addSubscription string
	groupSaveFailed string
	importOPML      string
	exportOPML      string
}

func rssSidebarMenuCopy(locale string) rssSidebarMenuWords {
	if strings.HasPrefix(strings.ToLower(locale), "zh") {
		return rssSidebarMenuWords{
			newGroup:        "新建分组",
			addSubscription: "新增订阅…",
			groupSaveFailed: "无法保存这个分组。请重试。",
			importOPML:      "导入 OPML…",
			exportOPML:      "导出 OPML…",
		}
	}
	return rssSidebarMenuWords{
		newGroup:        "New Group",
		addSubscription: "Add Subscription…",
		groupSaveFailed: "Could not save the group. Try again.",
		importOPML:      "Import OPML…",
		exportOPML:      "Export OPML…",
	}
}

func (w *Window) importRSSOPML(c *ui.Context) {
	locale := ""
	if c != nil {
		locale = c.Locale()
	}
	zh := strings.HasPrefix(strings.ToLower(locale), "zh")

	title := "Import OPML"
	if zh {
		title = "导入 OPML"
	}

	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:      w.dialogParent(),
		Title:       title,
		DefaultPath: filepath.Join(os.Getenv("HOME"), "Downloads"),
		Filters:     []mygo.FileFilter{{Name: "OPML", Extensions: []string{"opml", "xml"}}},
		Multiple:    true,
	})
	if err != nil {
		errMsg := "Import failed: " + err.Error()
		if zh {
			errMsg = "导入失败：" + err.Error()
		}
		w.toast(c, errMsg, true)
		return
	}
	if len(paths) == 0 {
		return
	}

	go func() {
		res, err := w.app.ImportRSSOPMLPaths(context.Background(), paths)
		mygo.RunOnMain(func() {
			if err != nil {
				errMsg := "Import failed: " + err.Error()
				if zh {
					errMsg = "导入失败：" + err.Error()
				}
				w.toast(nil, errMsg, true)
				return
			}

			var msg string
			if zh {
				switch {
				case res.FeedsImported > 0 && res.GroupsImported > 0:
					msg = fmt.Sprintf("已导入 %d 个订阅源，%d 个分组", res.FeedsImported, res.GroupsImported)
				case res.FeedsImported > 0:
					msg = fmt.Sprintf("已导入 %d 个订阅源", res.FeedsImported)
				case res.GroupsImported > 0:
					msg = fmt.Sprintf("已导入 %d 个分组", res.GroupsImported)
				default:
					msg = "未发现新订阅源（已全部订阅）"
				}
			} else {
				switch {
				case res.FeedsImported > 0 && res.GroupsImported > 0:
					msg = fmt.Sprintf("Imported %d feed%s and %d group%s",
						res.FeedsImported, plural(res.FeedsImported),
						res.GroupsImported, plural(res.GroupsImported))
				case res.FeedsImported > 0:
					msg = fmt.Sprintf("Imported %d feed%s",
						res.FeedsImported, plural(res.FeedsImported))
				case res.GroupsImported > 0:
					msg = fmt.Sprintf("Imported %d group%s",
						res.GroupsImported, plural(res.GroupsImported))
				default:
					msg = "No new feeds to import (already subscribed)"
				}
			}

			w.toast(nil, msg, false)
			w.invalidateMainWindow()
		})

		// Background fetch newly imported feeds without blocking the UI
		if len(res.NewFeedIDs) > 0 && w.app.Store != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			fetcher := core.NewRSSFetcher(w.app.Store)
			for _, fid := range res.NewFeedIDs {
				if f, err := w.app.Store.GetRSSFeed(ctx, fid); err == nil && f != nil {
					_, _ = fetcher.FetchFeed(ctx, *f)
				}
			}
			mygo.RunOnMain(func() {
				_ = w.app.ReloadRSS()
				w.invalidateMainWindow()
			})
		}
	}()
}

func (w *Window) exportRSSOPML(c *ui.Context) {
	locale := ""
	if c != nil {
		locale = c.Locale()
	}
	zh := strings.HasPrefix(strings.ToLower(locale), "zh")

	title := "Export OPML"
	if zh {
		title = "导出 OPML"
	}

	defaultPath := filepath.Join(os.Getenv("HOME"), "Downloads", "subscriptions.opml")
	path, ok := w.chooseSavePath(title, defaultPath, "OPML", "opml")
	if !ok || path == "" {
		return
	}

	if err := w.app.ExportRSSOPMLToFile(path); err != nil {
		errMsg := "Export failed: " + err.Error()
		if zh {
			errMsg = "导出失败：" + err.Error()
		}
		w.toast(c, errMsg, true)
		return
	}

	msg := "OPML exported successfully"
	if zh {
		msg = "OPML 导出成功"
	}
	w.toast(c, msg, false)
}

func (w *Window) newRSSGroupFromMenu(c *ui.Context) {
	if w.app == nil {
		return
	}
	group, err := w.app.CreateRSSGroup(defaultNewGroupName(c.Locale()))
	if err != nil {
		w.toast(c, sidebarMenuCopy(c.Locale()).groupSaveFailed, true)
		return
	}
	w.renamingRSSGroupNew = group.ID
	w.startRenameRSSGroup(group.ID, group.Name)
	c.Invalidate()
}
