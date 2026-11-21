package main

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	mui "github.com/simon/youtube-reader/ui"
)

// header draws the window's title row and, outside settings, its filter row.
// It carries no status copy: a refresh, a failure, and the offline state are
// the menu bar status item's to report, so the window never says them twice.
func (w *Window) header(c *ui.Context, t mui.Tokens) {
	title := w.title()
	subtitle := ""
	if w.app.SettingsOpen {
		title = "Settings"
		subtitle = "Refresh, imports, sync and notifications"
	}
	headerHeight := defaultHeaderHeight
	if w.app.SettingsOpen {
		headerHeight = settingsHeaderHeight
	}
	ui.Column(c).Padding(16, 24, 12).Gap(12).Height(headerHeight).Shrink(0).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Gap(2).Grow(1).Children(func() {
				ui.Text(c, title).Bold().FontSize(28).FixedLineHeight(34).TextColor(t.Text).SingleLine()
				if subtitle != "" {
					ui.Text(c, subtitle).FontSize(13).TextColor(t.Muted).SingleLine()
				}
			})
			// The refresh control keeps its loading face while a refresh runs
			// and stands down while the machine is offline, where the item shows
			// the shape and the popover offers Retry.
			state := mui.Default
			switch {
			case w.app.Busy:
				state = mui.Loading
			case !w.online():
				state = mui.Disabled
			}
			refresh := mui.StatefulButton(c, t, "refresh", "Refresh", mui.IconRefresh, false, state, func() {
				w.runRefresh(true)
			})
			if state == mui.Disabled {
				refresh.Tooltip("You are offline")
			}
			mui.StatefulButton(c, t, "settings", "Settings", mui.IconSettings, false, mui.Default, func() {
				if w.app.SettingsOpen {
					_ = w.app.SaveSettings()
				}
				w.app.SettingsOpen = !w.app.SettingsOpen
			})
		})
		if w.app.SettingsOpen {
			return
		}
		ui.Row(c).Height(34).AlignItems(ui.Center).Gap(12).Children(func() {
			options := []string{"All channels"}
			ids := []string{""}
			for _, ch := range w.app.Channels {
				options = append(options, ch.Title)
				ids = append(ids, ch.ID)
			}
			selected := "All channels"
			for i, id := range ids {
				if id == w.app.ChannelFilter {
					selected = options[i]
				}
			}
			if ui.Select(c.Key("channel-filter"), &selected, options).Changed() {
				for i, label := range options {
					if label == selected {
						w.app.ChannelFilter = ids[i]
					}
				}
			}
			ui.SearchField(c.Key("search"), &w.app.Search).Width(220).Label("Search videos")
			ui.Checkbox(c.Key("unread"), &w.app.UnreadOnly, "未观看").Label("未观看").FontSize(14)
			ui.Row(c).Grow(1)
			ui.Text(c, fmt.Sprintf("%d videos", len(w.app.VisibleVideos()))).FontSize(13).TextColor(t.Muted).SingleLine()
		})
	})
}

func (w *Window) title() string {
	switch {
	case w.app.Selected == "all":
		return "All videos"
	case w.app.Selected == "favorites":
		return "Favorites"
	case strings.HasPrefix(w.app.Selected, "group:"):
		gid := strings.TrimPrefix(w.app.Selected, "group:")
		if g, ok := w.app.Sync.Groups[gid]; ok {
			return g.Name
		}
	case strings.HasPrefix(w.app.Selected, "channel:"):
		ch := w.channelByID(strings.TrimPrefix(w.app.Selected, "channel:"))
		return ch.Title
	}
	return "YouTube Reader"
}
