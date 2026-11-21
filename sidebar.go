package main

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
	mui "github.com/simon/youtube-reader/ui"
)

func (w *Window) sidebar(c *ui.Context, t mui.Tokens) {
	nav := &sidebarNav{w: w, focus: w.sidebarFocus, targets: map[string]string{}}
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
			}, "all")
			nav.row(c, t, mui.SidebarRow{
				Key: "favorites", Label: "Favorites", Glyph: mui.IconStar, Count: fmt.Sprintf("%d", counts.favorites),
				Selected: w.app.Selected == "favorites",
				OnClick:  func() { w.app.Selected = "favorites" },
			}, "favorites")
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
				}, "channel:"+ch.ID)
			}
		})
		w.statusSection(c, t, nav)
	})
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
	targets map[string]string // row key -> the selection it opens
	focus   string            // row to take the focus in this frame
	at      int               // index of the focused row, -1 for none
}

// row builds a row and adds it to the keyboard order. target is the
// selection the row opens, empty for a row that folds or toggles instead.
func (n *sidebarNav) row(c *ui.Context, t mui.Tokens, r mui.SidebarRow, target string) {
	n.observe(r.Build(c, t), r.Key, target)
}

// observe records a built row and applies a pending arrow-key move to it.
func (n *sidebarNav) observe(el ui.Element, key, target string) {
	if target != "" {
		n.targets[key] = target
	}
	if n.focus == key {
		el.Focus()
	}
	if el.Focused() {
		n.at = len(n.keys)
	}
	n.keys = append(n.keys, key)
}

// move returns the Up or Down handler: it walks from the focused row in the
// direction delta, stops at the first row that navigates, and puts the focus
// on it for the next frame so the row scrolls itself into view.
func (n *sidebarNav) move(delta int) func() {
	return func() {
		if n.at < 0 {
			return
		}
		for i := n.at + delta; i >= 0 && i < len(n.keys); i += delta {
			key := n.keys[i]
			target, ok := n.targets[key]
			if !ok {
				continue
			}
			n.w.sidebarFocus = key
			n.w.app.Selected = target
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
	}, "")
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
		}, "channel:"+id)
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
		nav.row(c, t, status, "")
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
// visibility rule, so a count matches the videos its destination shows.
type sidebarCounts struct {
	total      int
	favorites  int
	perChannel map[string]int
}

func (w *Window) sidebarCounts() sidebarCounts {
	showShorts := w.app.settingsSnapshot().ShowShorts
	q := strings.ToLower(w.app.Search)
	counts := sidebarCounts{perChannel: make(map[string]int, len(w.app.Channels))}
	for _, v := range w.app.Videos {
		if !w.app.videoVisible(v, nil, showShorts, q) {
			continue
		}
		counts.total++
		counts.perChannel[v.ChannelID]++
		if _, ok := w.app.Sync.Favorites[v.ID]; ok {
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
