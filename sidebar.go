package main

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
	mui "github.com/simon/youtube-reader/ui"
)

func (w *Window) sidebar(c *ui.Context, t mui.Tokens) {
	nav := &sidebarNav{w: w, focus: w.sidebarFocus, actions: map[string]func(){}}
	w.sidebarFocus = ""
	counts := w.sidebarCounts()

	sidebar := ui.Column(c).Fill().Shrink(0).Background(t.Sidebar)
	sidebar.Children(func() {
		ui.Scroll(c).Grow(1).Padding(0, mui.Space(2), mui.Space(2)).Children(func() {
			sectionHeader(c, t, "Library")
			nav.row(c, t, mui.SidebarRow{
				Key: "all", Label: "All videos", Glyph: mui.IconPlay, Count: fmt.Sprintf("%d", counts.total),
				Selected: w.app.Selected == "all",
				OnClick:  func() { w.app.Selected = "all" },
			})
			nav.row(c, t, mui.SidebarRow{
				Key: "favorites", Label: "Favorites", Glyph: mui.IconStar, Count: fmt.Sprintf("%d", counts.favorites),
				Selected: w.app.Selected == "favorites",
				OnClick:  func() { w.app.Selected = "favorites" },
			})
			for _, g := range w.app.Groups {
				w.groupSection(c, t, g, nav, counts)
			}
			sectionHeader(c, t, "Channels")
			if len(w.app.Channels) == 0 {
				mui.Secondary.Apply(ui.Text(c, "No channels yet")).TextColor(t.Muted).Padding(mui.Space(1), mui.Space(2)).SingleLine()
			}
			for _, ch := range w.app.Channels {
				ch := ch
				nav.row(c, t, mui.SidebarRow{
					Key: "all-channels:" + ch.ID, Label: ch.Title, Channel: true, Avatar: ch.AvatarURL,
					Count:    fmt.Sprintf("%d", counts.perChannel[ch.ID]),
					Selected: w.app.Selected == "channel:"+ch.ID,
					OnClick:  func() { w.app.Selected = "channel:" + ch.ID },
				})
			}
		})
		w.statusSection(c, t, nav)
	})
	if w.sidebarKeyboard != "" && nav.focused != w.sidebarKeyboard {
		w.sidebarKeyboard = ""
	}
	// Up and Down move the sidebar's selection. MyGo routes a shortcut to the
	// element whose subtree holds the focus, so the keys work from any row
	// and never leak into the grid or the toolbar.
	sidebar.OnShortcut(0, ui.KeyDown, nav.move(1))
	sidebar.OnShortcut(0, ui.KeyUp, nav.move(-1))
}

// sidebarNav keeps the sidebar's keyboard order for one frame. Rows register
// themselves as they build, so Up and Down can move the selection from the
// focused row to the next row that navigates, skipping the fold rows in
// between exactly as a native source list does.
type sidebarNav struct {
	w       *Window
	keys    []string          // every focusable row, in visual order
	actions map[string]func() // row key -> the action its click runs
	focus   string            // row to take the focus in this frame
	at      int               // index of the focused row, -1 for none
	focused string            // row that actually has the focus this frame
}

// row builds a row and adds it to the keyboard order. A row with an action
// opens the same selection whether it is clicked, opened with Return, or
// reached with Up or Down. The fold rows toggle, so the arrows skip them.
func (n *sidebarNav) row(c *ui.Context, t mui.Tokens, r mui.SidebarRow) {
	key := r.Key
	if r.OnClick != nil {
		action := r.OnClick
		r.OnClick = func() {
			n.clearKeyboardFocus()
			action()
		}
		if !r.Fold {
			n.actions[key] = action
		}
	}
	el := r.Build(c, t)
	if n.focus == key {
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

func (w *Window) groupSection(c *ui.Context, t mui.Tokens, g core.ChannelGroup, nav *sidebarNav, counts sidebarCounts) {
	collapsed := w.app.GroupCollapsed(g.ID)
	key := "group-collapse:" + g.ID
	nav.row(c, t, mui.SidebarRow{
		Key: key, Label: g.Name, Count: fmt.Sprintf("%d", counts.group(g.ChannelIDs)),
		Fold: true, Expanded: !collapsed,
		OnClick: func() { _ = w.app.SetGroupCollapsed(g.ID, !collapsed) },
	})
	if collapsed {
		return
	}
	for _, id := range g.ChannelIDs {
		id := id
		ch := w.channelByID(id)
		nav.row(c, t, mui.SidebarRow{
			Key: fmt.Sprintf("group:%s:channel:%s", g.Name, id), Label: ch.Title, Channel: true, Avatar: ch.AvatarURL,
			Count:    fmt.Sprintf("%d", counts.perChannel[id]),
			Selected: w.app.Selected == "channel:"+id,
			OnClick:  func() { w.app.Selected = "channel:" + id },
		})
	}
}

// statusSection keeps the refresh status at the foot of the sidebar until
// the status surface replaces it, styled as one more row of the list.
func (w *Window) statusSection(c *ui.Context, t mui.Tokens, nav *sidebarNav) {
	ui.Column(c).Shrink(0).Padding(mui.Space(2), mui.Space(2), mui.Space(2)).Gap(mui.Space(2)).Background(t.Sidebar).Children(func() {
		status := mui.SidebarRow{
			Key: "status", Label: "Status", Fold: true, Expanded: w.app.StatusExpanded,
			OnClick: func() { w.app.StatusExpanded = !w.app.StatusExpanded },
		}
		if failures := len(w.app.settingsSnapshot().RecentFailures); failures > 0 {
			status.Count = fmt.Sprintf("%d", failures)
		}
		nav.row(c, t, status)
		if !w.app.StatusExpanded {
			return
		}
		statusSettings := w.app.settingsSnapshot()
		// The details align with the status label, one fold indent in.
		ui.Column(c).Gap(mui.Space(2)).Padding(0, 0, 0, mui.Space(5)).Children(func() {
			mui.Secondary.Apply(ui.Text(c, "Last: "+orDash(statusSettings.LastRefresh))).TextColor(t.Muted).SingleLine()
			mui.Secondary.Apply(ui.Text(c, "Next: "+orDash(statusSettings.NextRefresh))).TextColor(t.Muted).SingleLine()
			for _, f := range statusSettings.RecentFailures {
				ui.Row(c).Gap(mui.Space(2)).AlignItems(ui.Start).Children(func() {
					ui.Icon(c, mui.IconAlert).Size(16, 16).TextColor(t.Danger)
					mui.Secondary.Apply(ui.Text(c, f.Message)).TextColor(t.Danger).MaxLines(2).Grow(1)
				})
			}
		})
	})
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
	return core.Channel{ID: id, Title: id}
}

func orDash(s string) string {
	if s == "" {
		return "never"
	}
	return s
}
