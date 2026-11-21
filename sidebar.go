package main

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
	mui "github.com/simon/youtube-reader/ui"
)

func (w *Window) sidebar(c *ui.Context, t mui.Tokens) {
	sidebar := ui.Column(c).Fill().Shrink(0).Background(t.Sidebar)
	sidebar.Draw(func(p *ui.Painter, r ui.Rect) {
		p.Fill(ui.Rect{X: r.X + r.W - 1, Y: r.Y, W: 1, H: r.H}, t.Border, 0)
	})
	sidebar.Children(func() {
		ui.Row(c).Height(72).Padding(20, 20, 16).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "YouTube Reader").FontSize(17).Bold().TextColor(t.Text).SingleLine()
		})
		ui.Scroll(c).Grow(1).Padding(0, 12, 12).Children(func() {
			sectionHeader(c, t, "Library")
			mui.NavRow(c, t, "all", mui.IconPlay, "All", w.app.Selected == "all", func() {
				w.app.Selected = "all"
			})
			mui.NavRow(c, t, "favorites", mui.IconStar, "Favorites", w.app.Selected == "favorites", func() {
				w.app.Selected = "favorites"
			})
			for _, g := range w.app.Groups {
				w.groupSection(c, t, g)
			}
			sectionHeader(c, t, "Channels")
			for _, ch := range w.app.Channels {
				ch := ch
				mui.ChannelNavRow(c, t, "all-channels:"+ch.ID, ch.Title, ch.AvatarURL, w.app.Selected == "channel:"+ch.ID, func() {
					w.app.Selected = "channel:" + ch.ID
				})
			}
		})
		statusTitle := "Status"
		if w.app.StatusExpanded {
			statusTitle = "Status (last refresh / next / failures)"
		}
		ui.Column(c).Padding(12, 12, 16).Gap(8).Background(t.Sidebar).Children(func() {
			mui.NavRow(c, t, "status", nil, statusTitle, false, func() {
				w.app.StatusExpanded = !w.app.StatusExpanded
			})
			if w.app.StatusExpanded {
				statusSettings := w.app.settingsSnapshot()
				ui.Text(c, "Last: "+orDash(statusSettings.LastRefresh)).FontSize(13).TextColor(t.Muted)
				ui.Text(c, "Next: "+orDash(statusSettings.NextRefresh)).FontSize(13).TextColor(t.Muted)
				for _, f := range statusSettings.RecentFailures {
					ui.Row(c).Gap(8).AlignItems(ui.Start).Children(func() {
						ui.Icon(c, mui.IconAlert).Size(16, 16)
						ui.Text(c, f.Message).FontSize(13).TextColor(t.Danger).MaxLines(2).Grow(1)
					})
				}
			}
		})
	})
}

func (w *Window) groupSection(c *ui.Context, t mui.Tokens, g core.ChannelGroup) {
	key := "group-collapse:" + g.ID
	collapsed := w.app.GroupCollapsed(g.ID)
	headline := ui.ButtonBase(c).Key(key).Height(28).FillWidth().Gap(mui.Space(1)).Padding(0, mui.Space(1)).Radius(6).Background(t.Sidebar).TextColor(t.Muted).FontSize(13).Cursor(ui.CursorPointer).Transition(mui.Fade).FocusRingColor(t.Focus).FocusRingOffset(2).Role(ui.RoleDisclosure).Expanded(!collapsed).Label(g.Name)
	headline.Children(func() {
		glyph := mui.IconChevron
		if !collapsed {
			glyph = mui.IconChevronDown
		}
		ui.Icon(c, glyph).Size(14, 14)
		ui.Text(c, g.Name).FontSize(13).SingleLine().Grow(1)
		ui.Text(c, fmt.Sprintf("%d", len(g.ChannelIDs))).FontSize(12).TextColor(t.Muted).SingleLine()
	})
	if headline.Clicked() {
		_ = w.app.SetGroupCollapsed(g.ID, !collapsed)
	}
	if !collapsed {
		for _, id := range g.ChannelIDs {
			id := id
			ch := w.channelByID(id)
			mui.ChannelNavRow(c, t, fmt.Sprintf("group:%s:channel:%s", g.Name, id), ch.Title, ch.AvatarURL, w.app.Selected == "channel:"+id, func() {
				w.app.Selected = "channel:" + id
			})
		}
	}
}

func sectionHeader(c *ui.Context, t mui.Tokens, title string) {
	ui.Text(c, title).FontSize(13).TextColor(t.Muted).Padding(20, 12, 8).SingleLine()
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
