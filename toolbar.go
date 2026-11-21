package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	mui "github.com/simon/youtube-reader/ui"
)

func (w *Window) header(c *ui.Context, t mui.Tokens) {
	title := w.title()
	subtitle := refreshedLabel(w.app.settingsSnapshot().LastRefresh)
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
			state := mui.Default
			if w.app.Busy {
				state = mui.Loading
			} else if len(w.app.settingsSnapshot().RecentFailures) > 0 {
				state = mui.Error
			}
			if state == mui.Loading {
				ui.Text(c, "Refreshing...").FontSize(13).TextColor(t.Muted)
			}
			if len(w.app.settingsSnapshot().RecentFailures) > 0 && !w.app.Busy {
				ui.Text(c, "Refresh issue").FontSize(13).TextColor(t.Danger).Label("Refresh issue")
			}
			mui.StatefulButton(c, t, "refresh", "Refresh", mui.IconRefresh, false, state, func() {
				w.runRefresh(c, true)
			})
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

func refreshedLabel(stamp string) string {
	if stamp == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return "Refreshed " + stamp
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "Refreshed just now"
	case d < time.Hour:
		return fmt.Sprintf("Refreshed %d min ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("Refreshed %d hr ago", int(d.Hours()))
	default:
		return "Refreshed on " + t.Local().Format("Jan 2, 2006 at 15:04")
	}
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
