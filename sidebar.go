package main

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/sfsymbols"
	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
	mui "github.com/simon/youtube-reader/ui"
)

// The sidebar footer, from the approved board: a fixed 34 pt bar below the
// scrolling list, holding one compact 28 by 28 pt icon button inset 8 pt from
// the sidebar's leading edge. The list gives up exactly the bar's height, so a
// row never sits under it, also at the minimum window height.
const (
	sidebarFooterHeight float32 = 34
	sidebarAddSize      float32 = 28
	sidebarAddGlyph     float32 = 14
	sidebarFooterInset  float32 = 8
	sidebarMenuWidth    float32 = 196
)

// The plus menu's icons, made once from the app's own SVG line set.
var (
	sidebarMenuNewGroupIcon = mustMenuIcon(mui.IconFolderPlus)
	sidebarMenuAddSubIcon   = mustMenuIcon(mui.IconPlayRectangle)
)

// sidebarMenuWords are the plus menu's two commands, and the sentence a group
// that cannot be written says, in the reader's language.
type sidebarMenuWords struct {
	newGroup        string
	addSubscription string
	groupSaveFailed string
}

// sidebarMenuCopy returns the plus menu's words in the reader's language.
func sidebarMenuCopy(locale string) sidebarMenuWords {
	if strings.HasPrefix(strings.ToLower(locale), "zh") {
		return sidebarMenuWords{
			newGroup:        "新建分组",
			addSubscription: "新增订阅…",
			groupSaveFailed: "无法保存这个分组。请重试。",
		}
	}
	return sidebarMenuWords{
		newGroup:        "New Group",
		addSubscription: "Add Subscription…",
		groupSaveFailed: "Could not save the group. Try again.",
	}
}

// defaultNewGroupName is the name a group starts with, so Enter on the inline
// field keeps a group the reader can find rather than an unnamed one.
func defaultNewGroupName(locale string) string {
	if strings.HasPrefix(strings.ToLower(locale), "zh") {
		return "新分组"
	}
	return "New Group"
}

// mustMenuIcon makes a menu icon of one SVG, or none when it cannot be drawn:
// a menu without its glyph still names its commands.
func mustMenuIcon(svg *ui.SVG) ui.MenuIcon {
	icon, err := mygo.NewMenuIconSVG(svg, true)
	if err != nil {
		return nil
	}
	return icon
}

func (w *Window) sidebar(c *ui.Context, t mui.Tokens) {
	w.ensureWorkspaceInit()
	sidebar := ui.Column(c).Fill().Shrink(0).Background(t.Sidebar)
	sidebar.Children(func() {
		w.sidebarWorkspaceDock(c, t)
		ui.Box(c).FillWidth().Height(mui.Hairline).Background(t.BorderSoft)

		switch w.activeWorkspaceName() {
		case WorkspaceRSS:
			w.rssSidebar(c, t)
		case WorkspaceReadLater:
			w.readLaterSidebar(c, t)
		case WorkspaceWeChatNotes:
			w.wechatNotesSidebar(c, t)
		default:
			w.videosSidebar(c, t, sidebar)
		}
	})
}

func (w *Window) videosSidebar(c *ui.Context, t mui.Tokens, sidebar ui.Element) {
	nav := &sidebarNav{w: w, focus: w.sidebarFocus, actions: map[string]func(){}}
	w.sidebarFocus = ""
	counts := w.sidebarCounts()
	var drag groupDragFrame
	var chDrag channelDragFrame

	// The list clips at its own edge, so a row scrolled past the last
	// visible one stops at the footer instead of painting under it.
	ui.Scroll(c).Grow(1).MinHeight(0).Clip().Padding(0, mui.Space(2), mui.Space(2)+mui.ScrollBottomInset).Children(func() {
		sectionHeader(c, t, "Library")
		nav.row(c, t, mui.SidebarRow{
			Key: "all", Label: "All videos", Symbol: "play.rectangle", Count: fmt.Sprintf("%d", counts.total),
			Selected: w.app.Selected == "all",
			OnClick: func() {
				if w.renamingGroupID != "" {
					w.commitRenameGroup(w.renamingGroupID, w.renamingGroupName)
				}
				w.app.SelectDestination("all")
			},
		})
		nav.row(c, t, mui.SidebarRow{
			Key: "favorites", Label: "Favorites", Symbol: "star", Count: fmt.Sprintf("%d", counts.favorites),
			Selected: w.app.Selected == "favorites",
			OnClick: func() {
				if w.renamingGroupID != "" {
					w.commitRenameGroup(w.renamingGroupID, w.renamingGroupName)
				}
				w.app.SelectDestination("favorites")
			},
		})
		for i, g := range w.app.Groups {
			w.groupSection(c, t, g, i, nav, counts, &drag, &chDrag)
		}
		grouped := groupedChannelIDs(w.app.Groups)
		ungrouped := make([]core.Channel, 0, len(w.app.Channels))
		for _, ch := range w.app.Channels {
			if !grouped[ch.ID] {
				ungrouped = append(ungrouped, ch)
			}
		}

		channelDragActive := chDrag.alive || w.channelDraggingID != ""

		if len(w.app.Channels) == 0 {
			mui.SidebarEmpty(c, t, func() { w.importOPML(c) })
		} else if len(ungrouped) > 0 {
			ungroupedBlock := ui.Column(c.Key("ungrouped-block")).FillWidth()
			draggedChan, chanOver := ui.DragOver[channelDrag](ungroupedBlock)
			droppedChan, chanDropped := ui.Drop[channelDrag](ungroupedBlock)
			if chanDropped && !w.channelIsUngrouped(droppedChan.ID) {
				w.moveChannelToGroup(c, droppedChan.ID, "")
			}
			dropHover := chanOver && !w.channelIsUngrouped(draggedChan.ID)

			ungroupedBlock.Children(func() {
				if dropHover {
					w.renderUngroupedDropRow(c, t, true)
				} else {
					sectionHeader(c, t, ungroupedHeader(c.Locale()))
				}
				for _, ch := range ungrouped {
					ch := ch
					row := nav.row(c, t, mui.SidebarRow{
						Key: "all-channels:" + ch.ID, Label: ch.Title, Channel: true, Avatar: ch.AvatarURL,
						Count:    fmt.Sprintf("%d", counts.perChannel[ch.ID]),
						Selected: w.app.Selected == "channel:"+ch.ID,
						OnClick: func() {
							if w.renamingGroupID != "" {
								w.commitRenameGroup(w.renamingGroupID, w.renamingGroupName)
							}
							w.app.SelectDestination("channel:" + ch.ID)
						},
						OnContextMenu: func(m *ui.Menu) {
							if m.Item(unsubscribeMenuLabel(c.Locale())).Chosen() {
								w.confirmUnsubscribeChannel(c, ch)
							}
						},
					})
					row.Drag(channelDrag{ID: ch.ID})
					if row.Dragging() {
						chDrag.alive = true
						chDrag.channelID = ch.ID
						row.Opacity(0.4)
					}
				}
			})
		} else if channelDragActive {
			ungroupedStrip := ui.Column(c.Key("ungrouped-drop-strip")).FillWidth()
			draggedChan, chanOver := ui.DragOver[channelDrag](ungroupedStrip)
			droppedChan, chanDropped := ui.Drop[channelDrag](ungroupedStrip)
			if chanDropped && !w.channelIsUngrouped(droppedChan.ID) {
				w.moveChannelToGroup(c, droppedChan.ID, "")
			}
			dropHover := chanOver && !w.channelIsUngrouped(draggedChan.ID)

			ungroupedStrip.Children(func() {
				w.renderUngroupedDropRow(c, t, dropHover)
			})
		}
	})
	w.sidebarFooter(c, t)

	w.settleGroupDrag(c, &drag)
	if chDrag.alive {
		w.channelDraggingID = chDrag.channelID
	} else {
		w.channelDraggingID = ""
	}
	if nav.focused != "" && nav.focused != w.sidebarKeyboard {
		w.sidebarKeyboard = ""
	}
	// Up and Down move the sidebar's selection. MyGo routes a shortcut to the
	// element whose subtree holds the focus, so the keys work from any row
	// and never leak into the grid or the toolbar.
	sidebar.OnShortcut(0, ui.KeyDown, nav.move(1))
	sidebar.OnShortcut(0, ui.KeyUp, nav.move(-1))
}

// sidebarFooter is the fixed bar under the list: one compact plus button that
// opens the menu of the two things a reader adds from here. The column lays the
// scrolling list out with the room that is left, so the bar never sits over a
// row, also at the minimum window height.
func (w *Window) sidebarFooter(c *ui.Context, t mui.Tokens) {
	bar := ui.Row(c).Key("sidebar-footer").Height(sidebarFooterHeight).Shrink(0).FillWidth().
		AlignItems(ui.Center).Padding(0, sidebarFooterInset).Background(t.Sidebar).
		Role(ui.RoleGroup).Label("Library actions")
	bar.Draw(func(p *ui.Painter, r ui.Rect) {
		p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: mui.Hairline}, t.BorderSoft, 0)
	})
	bar.Children(func() {
		w.sidebarAddButton(c, t)
	})
}

// sidebarAddButton is the board's compact icon button: 28 by 28 pt, a 14 pt
// plus, a 6 pt radius, and no label. It rests quiet on the sidebar, lightens
// under the pointer, and keeps the accent face while its menu shows.
func (w *Window) sidebarAddButton(c *ui.Context, t mui.Tokens) {
	button := ui.ButtonBase(c).Key("sidebar-add").Size(sidebarAddSize, sidebarAddSize).Padding(0).
		Radius(mui.RadiusControl).Label("New group or subscription").
		Cursor(ui.CursorPointer).Transition(mui.Fade).
		FocusRingColor(t.Focus).FocusRingOffset(mui.FocusRingGap)
	color := t.Muted
	switch {
	case w.sidebarMenuOpen:
		color = t.Accent
	case button.Hovered() || button.Pressed():
		color = t.Text
	}
	button.Children(func() {
		sfsymbols.Symbol(c, "plus").Size(sidebarAddGlyph).Color(color).Element.Role(ui.RoleNone)
	})
	button.Draw(func(p *ui.Painter, r ui.Rect) {
		switch {
		case w.sidebarMenuOpen || button.Pressed():
			p.Fill(r, t.AccentSoft, mui.RadiusControl)
			p.Stroke(r, t.Accent, mui.RadiusControl, mui.Hairline)
		case button.Hovered():
			p.Fill(r, t.SurfaceAlt, mui.RadiusControl)
			p.Stroke(r, t.BorderSoft, mui.RadiusControl, mui.Hairline)
		}
	})
	words := sidebarMenuCopy(c.Locale())
	button.PopoverMenuButton(func(m *ui.Menu) {
		if m.Item(words.newGroup).Icon(sidebarMenuNewGroupIcon).Chosen() {
			w.newGroupFromMenu(c)
		}
		if m.Item(words.addSubscription).Icon(sidebarMenuAddSubIcon).Chosen() {
			w.openAddDialog()
		}
	}, ui.PopoverMenuButtonOptions{
		// The menu opens upward from the button, as a footer control's does, so
		// it never runs past the window's bottom edge.
		At: ui.AnchorTopStart, Self: ui.AnchorBottomStart, Open: &w.sidebarMenuOpen,
		Panel: func(panel ui.Element) {
			panel.MinWidth(sidebarMenuWidth).Radius(mui.RadiusNotice).
				Border(mui.Hairline/2, t.Border).
				Shadow(0, mui.ElevationOffsetY, mui.ElevationBlur, mui.ElevationSpread, t.Shadow)
		},
	})
}

// newGroupFromMenu appends a group and puts it straight into the sidebar's
// inline rename, which stays the app's one naming surface for a group.
func (w *Window) newGroupFromMenu(c *ui.Context) {
	group, err := w.app.CreateGroup(defaultNewGroupName(c.Locale()))
	if err != nil {
		w.toast(c, sidebarMenuCopy(c.Locale()).groupSaveFailed, true)
		return
	}
	w.renamingGroupNew = group.ID
	w.startRenameGroup(group.ID, group.Name)
	c.Invalidate()
}

// groupedChannelIDs is the set of every channel that appears in a group.
// Group membership is authoritative: a channel listed in a group is shown
// there alone, never again at the foot of the sidebar.
func groupedChannelIDs(groups []core.ChannelGroup) map[string]bool {
	ids := make(map[string]bool)
	for _, g := range groups {
		for _, id := range g.ChannelIDs {
			ids[id] = true
		}
	}
	return ids
}

// ungroupedHeader names the section for channels in no group. The app's copy
// is English; a Chinese locale gets the agreed translation.
func ungroupedHeader(locale string) string {
	if strings.HasPrefix(strings.ToLower(locale), "zh") {
		return "未分组"
	}
	return "Ungrouped"
}

// moveHereLabel returns the label for the drop target badge when hovering a valid group.
func moveHereLabel(locale string) string {
	if strings.HasPrefix(strings.ToLower(locale), "zh") {
		return "+ 移入此组"
	}
	return "+ Move here"
}

// renameMenuLabel returns the context menu item label for renaming groups.
func renameMenuLabel(locale string) string {
	if strings.HasPrefix(strings.ToLower(locale), "zh") {
		return "重命名"
	}
	return "Rename"
}

// unsubscribeMenuLabel returns the context menu item label for unsubscribing channels.
func unsubscribeMenuLabel(locale string) string {
	if strings.HasPrefix(strings.ToLower(locale), "zh") {
		return "取消订阅"
	}
	return "Unsubscribe"
}

type unsubscribeDialogCopy struct {
	title      string
	detail     string
	cancelBtn  string
	confirmBtn string
}

func unsubscribeCopy(locale, channelTitle string) unsubscribeDialogCopy {
	if strings.HasPrefix(strings.ToLower(locale), "zh") {
		return unsubscribeDialogCopy{
			title:      fmt.Sprintf("取消订阅 %s？", channelTitle),
			detail:     "该频道及其未收藏的视频将从本地列表中移除。",
			cancelBtn:  "取消",
			confirmBtn: "取消订阅",
		}
	}
	return unsubscribeDialogCopy{
		title:      fmt.Sprintf("Unsubscribe from %s?", channelTitle),
		detail:     "This channel and its un-favorited videos will be removed from your library.",
		cancelBtn:  "Cancel",
		confirmBtn: "Unsubscribe",
	}
}

var dialogMessage = func(opts mygo.MessageOptions) (mygo.MessageResult, error) {
	return mygo.Dialog.Message(opts)
}

// confirmUnsubscribeChannel displays a confirmation dialog and unsubscribes the
// channel if confirmed, preserving favorited videos.
func (w *Window) confirmUnsubscribeChannel(c *ui.Context, ch core.Channel) {
	words := unsubscribeCopy(c.Locale(), ch.Title)
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
	if err := w.app.UnsubscribeChannel(ch.ID); err != nil {
		w.toast(c, "Unsubscribe failed: "+err.Error(), true)
		return
	}
	c.Invalidate()
	w.invalidateMainWindow()
	w.refreshSettingsWindow()
}

// sidebarNav keeps the sidebar's keyboard order for one frame. Rows register
// themselves as they build, so Up and Down can move the selection from the
// focused row to the next navigable row.
type sidebarNav struct {
	w       *Window
	keys    []string          // every focusable row, in visual order
	actions map[string]func() // row key -> the action its click runs
	focus   string            // row to take the focus in this frame
	at      int               // index into keys of the row that reported focus
	focused string            // row that actually has the focus this frame
}

// row builds a row and adds it to the keyboard order, returning the element so
// a caller can make it a drag source. A row with an action opens the same
// selection whether it is clicked, opened with Return, or reached with Up or
// Down.
func (n *sidebarNav) row(c *ui.Context, t mui.Tokens, r mui.SidebarRow) ui.Element {
	key := r.Key
	if r.OnClick != nil {
		action := r.OnClick
		r.OnClick = func() {
			n.clearKeyboardFocus()
			action()
		}
		if !r.Renaming {
			n.actions[key] = action
		}
	}
	el := r.Build(c, t)
	if n.focus == key && !r.Renaming {
		el.Focus()
		if el.Focused() {
			n.w.sidebarKeyboard = key
		}
	}
	if n.w.sidebarKeyboard == key && el.Focused() {
		// The row owns the ring for arrow-driven focus: the raised layer
		// keeps its 3 pt offset above the next row's background on the same
		// frame as the move.
		el.ZIndex(1)
		el.DrawOver(func(p *ui.Painter, box ui.Rect) {
			if n.w.sidebarKeyboard != key || !el.Focused() {
				return
			}
			d := mui.FocusRingGap + mui.FocusRing
			p.Stroke(ui.Rect{X: box.X - d, Y: box.Y - d, W: box.W + 2*d, H: box.H + 2*d}, t.Focus, mui.RadiusControl+d, mui.FocusRing)
		})
	}
	n.observe(el, key)
	return el
}

// observe records a built row and notes which row has the focus this frame.
func (n *sidebarNav) observe(el ui.Element, key string) {
	if el.Focused() {
		n.at = len(n.keys)
		n.focused = key
	}
	n.keys = append(n.keys, key)
}

func (n *sidebarNav) clearKeyboardFocus() {
	n.w.sidebarFocus = ""
	n.w.sidebarKeyboard = ""
}

// move returns the Up or Down handler: it walks from the focused row in the
// direction delta, stops at the first row that navigates, and runs that row's
// action so the arrows and the pointer share one activation path.
func (n *sidebarNav) move(delta int) func() {
	return func() {
		if n.at < 0 {
			return
		}
		for i := n.at + delta; i >= 0 && i < len(n.keys); i += delta {
			key := n.keys[i]
			action, ok := n.actions[key]
			if !ok {
				continue
			}
			n.w.sidebarFocus = key
			action()
			return
		}
	}
}

// groupSection lays one group out as a block: its fold row, which is the drag
// handle that reorders the groups, over the channels it holds. The block is
// keyed by the group, so a drag moves it with its rows' state, and it is the
// destination a dragged group drops on, so a folded-out group answers anywhere
// over its channels rather than only over its row.
func (w *Window) groupSection(c *ui.Context, t mui.Tokens, g core.ChannelGroup, index int, nav *sidebarNav, counts sidebarCounts, drag *groupDragFrame, chDrag *channelDragFrame) {
	collapsed := w.app.GroupCollapsed(g.ID)
	focused := false
	block := ui.Column(c.Key("group-channel-drop:" + g.ID)).FillWidth()
	renaming := w.renamingGroupID == g.ID
	// Check channel drop target on this group block
	draggedChan, chanOver := ui.DragOver[channelDrag](block)
	droppedChan, chanDropped := ui.Drop[channelDrag](block)
	if chanDropped && !w.channelOnlyInGroup(droppedChan.ID, g.ID) {
		w.moveChannelToGroup(c, droppedChan.ID, g.ID)
	}
	validDropTarget := !renaming && chanOver && !w.channelOnlyInGroup(draggedChan.ID, g.ID)

	block.Children(func() {
		section := ui.Column(c.Key("group-block:" + g.ID)).FillWidth()
		section.Children(func() {
			row := nav.row(c, t, mui.SidebarRow{
				Key: "group:" + g.ID, Label: g.Name, Count: fmt.Sprintf("%d", counts.group(g.ChannelIDs)),
				Fold: true, Expanded: !collapsed,
				Selected:       w.app.Selected == "group:"+g.ID,
				DropHover:      validDropTarget,
				DropHoverLabel: moveHereLabel(c.Locale()),
				OnClick: func() {
					if w.renamingGroupID != "" && w.renamingGroupID != g.ID {
						w.commitRenameGroup(w.renamingGroupID, w.renamingGroupName)
					}
					w.app.SelectDestination("group:" + g.ID)
				},
				OnFoldClick: func() {
					if w.renamingGroupID != "" {
						w.commitRenameGroup(w.renamingGroupID, w.renamingGroupName)
					}
					_ = w.app.SetGroupCollapsed(g.ID, !collapsed)
				},
				OnContextMenu: func(m *ui.Menu) {
					renameText := renameMenuLabel(c.Locale())
					if m.Item(renameText).Chosen() {
						w.startRenameGroup(g.ID, g.Name)
					}
				},
				Renaming:    renaming,
				RenameValue: &w.renamingGroupName,
				OnRename: func(newName string) {
					w.commitRenameGroup(g.ID, newName)
					c.Invalidate()
				},
				OnRenameCancel: func() {
					w.cancelRenameGroup()
					c.Invalidate()
				},
			})
			focused = focused || row.Focused()
			// The row drags its group. MyGo keeps a press that moved a few DIPs
			// from clicking, so a drag never selects the row; the row stays as
			// the place the group would land. Drag is disabled during rename.
			if !renaming {
				row.Drag(groupDrag{ID: g.ID})
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
			for _, id := range g.ChannelIDs {
				id := id
				ch := w.channelByID(id)
				row := nav.row(c, t, mui.SidebarRow{
					Key: fmt.Sprintf("group:%s:channel:%s", g.ID, id), Label: ch.Title, Channel: true, Avatar: ch.AvatarURL,
					Count:    fmt.Sprintf("%d", counts.perChannel[id]),
					Selected: w.app.Selected == "channel:"+id,
					OnClick: func() {
						if w.renamingGroupID != "" {
							w.commitRenameGroup(w.renamingGroupID, w.renamingGroupName)
						}
						w.app.SelectDestination("channel:" + id)
					},
					OnContextMenu: func(m *ui.Menu) {
						if m.Item(unsubscribeMenuLabel(c.Locale())).Chosen() {
							w.confirmUnsubscribeChannel(c, ch)
						}
					},
				})
				focused = focused || row.Focused()
				row.Drag(channelDrag{ID: id})
				if row.Dragging() {
					chDrag.alive = true
					chDrag.channelID = id
					row.Opacity(0.4)
				}
			}
		})
		if focused {
			block.ZIndex(1)
			section.ZIndex(1)
		}
		w.groupSectionDrop(c, section, index, drag)
	})
}

func (w *Window) renderUngroupedDropRow(c *ui.Context, t mui.Tokens, dropHover bool) {
	row := ui.Row(c.Key("ungrouped-drop-row")).
		Height(32).FillWidth().Gap(0).Padding(0, mui.Space(2)).
		Radius(mui.RadiusControl).AlignItems(ui.Center).MarginY(1)
	if dropHover {
		row.Background(t.AccentSoft).Border(1.5, t.Accent)
		row.Children(func() {
			mui.Control.Apply(ui.Text(c, ungroupedHeader(c.Locale()))).FontSize(13).TextColor(t.Accent).SingleLine().Grow(1)
			bubble := ui.Row(c.Key("ungrouped-drop-bubble")).
				Padding(2, mui.Space(1.5)).Radius(mui.RadiusCheck).
				Background(t.AccentSoft).Border(1, t.Accent).
				AlignItems(ui.Center)
			bubble.Children(func() {
				mui.Control.Apply(ui.Text(c, moveHereLabel(c.Locale()))).FontSize(11).TextColor(t.Accent).SingleLine()
			})
		})
	} else {
		row.Background(t.Sidebar).Border(1, t.Border).BorderStyle(ui.BorderDashed)
		row.Children(func() {
			mui.Control.Apply(ui.Text(c, ungroupedHeader(c.Locale()))).FontSize(12).TextColor(t.Muted).SingleLine()
		})
	}
}

// channelDrag is the payload carried when dragging a channel row.
// It is type-distinct from groupDrag so channel drag and group reorder never cross.
type channelDrag struct{ ID string }

// channelDragFrame tracks channel drag state within the current frame.
type channelDragFrame struct {
	alive     bool
	channelID string
}

// groupDrag is the value a group header row drags. Only a group row reorders
// the groups, so a channel row or a fold click never starts one.
type groupDrag struct{ ID string }

// groupDragFrame is what one frame of a group drag showed the sidebar: the
// dragged row is still held, a block took the drop, or the pointer let the row
// go over no block. The sidebar settles the order with them once every block is
// built.
type groupDragFrame struct {
	alive   bool
	dropped bool
	missed  bool
}

// groupSectionDrop takes a dragged group row over a block and moves the dragged
// group to the edge the pointer shows: above the block while it is in the top
// half, after the block below the middle. The list takes the move at once, so
// the order a drop would make shows before the pointer lets go.
func (w *Window) groupSectionDrop(c *ui.Context, section ui.Element, index int, drag *groupDragFrame) {
	dragged, over := ui.DragOver[groupDrag](section)
	droppedGroup, dropped := ui.Drop[groupDrag](section)
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
	// On the frame a release lands on, the drag is already over and DragOver
	// carries no value; the value the pointer delivered is the one that says
	// which group the drop moves.
	id := dragged.ID
	if dropped {
		id = droppedGroup.ID
	}
	if w.liveMoveGroup(id, to) {
		// The blocks built this frame show the order before the move: ask for
		// the frame that lays them out in it.
		c.Invalidate()
	}
}

// liveMoveGroup moves the dragged group to the boundary the pointer showed,
// keeping the order the drag began with the first time it moves one.
func (w *Window) liveMoveGroup(id string, to int) bool {
	if w.groupDragOrder == nil {
		w.groupDragOrder = groupIDs(w.app.Groups)
	}
	return w.moveGroup(id, to)
}

// settleGroupDrag ends a group drag the frame showed end. A drop, or the
// pointer letting the row go over no group block, keeps the order the drag
// moved the list to and saves it; giving the drag up, with Escape or the
// system taking the press, puts the groups back as they were.
func (w *Window) settleGroupDrag(c *ui.Context, drag *groupDragFrame) {
	if w.groupDragOrder == nil {
		return
	}
	switch {
	case drag.dropped || drag.missed:
		_ = w.app.SetGroupOrder(groupIDs(w.app.Groups))
		w.groupDragOrder = nil
	case !drag.alive:
		byID := make(map[string]core.ChannelGroup, len(w.app.Groups))
		for _, g := range w.app.Groups {
			byID[g.ID] = g
		}
		w.app.Groups = orderGroups(byID, w.groupDragOrder)
		w.groupDragOrder = nil
		// The blocks built this frame show the order the drag moved them to:
		// ask for the frame that lays them out as they were.
		c.Invalidate()
	}
}

// moveGroup puts the group with id where boundary to says, an index into the
// order before the move, and reports whether it moved. A move back where the
// group already was changes nothing.
func (w *Window) moveGroup(id string, to int) bool {
	from := -1
	for i, g := range w.app.Groups {
		if g.ID == id {
			from = i
			break
		}
	}
	if from < 0 || to == from || to == from+1 {
		return false
	}
	picked := w.app.Groups[from]
	rest := make([]core.ChannelGroup, 0, len(w.app.Groups)-1)
	for i, g := range w.app.Groups {
		if i != from {
			rest = append(rest, g)
		}
	}
	if to > from {
		to--
	}
	to = clampInt(to, 0, len(rest))
	ordered := make([]core.ChannelGroup, 0, len(w.app.Groups))
	ordered = append(ordered, rest[:to]...)
	ordered = append(ordered, picked)
	ordered = append(ordered, rest[to:]...)
	w.app.Groups = ordered
	return true
}

// sectionHeader titles a section of the sidebar. Its 13 pt Semibold Muted
// label is the token pair that clears the 4.5:1 floor on the sidebar
// surface in both appearances.
func sectionHeader(c *ui.Context, t mui.Tokens, title string) {
	header := mui.SectionHeader.Apply(ui.Text(c, title)).TextColor(t.Muted).SingleLine()
	header.Role(ui.RoleHeading).Level(2).Padding(mui.Space(3), mui.Space(2), mui.Space(1))
}

// sidebarCounts is the trailing count of every sidebar row: videos per
// channel, the total for All videos, favorites for Favorites, and the sum
// of a group's channels for the group row. Every count runs the grid's
// permanent visibility rule, ShowShorts and membership in the destination,
// so a count matches the videos its destination shows once the transient
// filter bar is cleared. ChannelFilter, Search and UnreadOnly are tools
// applied on top of a destination, not part of it, so they never change a
// count.
type sidebarCounts struct {
	total      int
	favorites  int
	perChannel map[string]int
}

func (w *Window) sidebarCounts() sidebarCounts {
	showShorts := w.app.settingsSnapshot().ShowShorts
	favorites := make(map[string]bool, len(w.app.Sync.Favorites))
	for id := range w.app.Sync.Favorites {
		favorites[id] = true
	}
	counts := sidebarCounts{perChannel: make(map[string]int, len(w.app.Channels))}
	for _, v := range w.app.Videos {
		if !w.app.videoVisible(v, nil, showShorts) {
			continue
		}
		counts.total++
		counts.perChannel[v.ChannelID]++
		if w.app.videoVisible(v, favorites, showShorts) {
			counts.favorites++
		}
	}
	return counts
}

func (c sidebarCounts) group(channelIDs []string) int {
	sum := 0
	for _, id := range channelIDs {
		sum += c.perChannel[id]
	}
	return sum
}

func (w *Window) channelByID(id string) core.Channel {
	for _, ch := range w.app.Channels {
		if ch.ID == id {
			return ch
		}
	}
	if ch, ok := w.app.Sync.Channels[id]; ok {
		return ch
	}
	return core.Channel{ID: id, Title: id}
}
