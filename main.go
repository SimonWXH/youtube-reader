package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
	mui "github.com/simon/youtube-reader/ui"
)

const (
	defaultHeaderHeight  float32 = 128
	settingsHeaderHeight float32 = 104
)

func realMain() {
	app, err := LoadApp()
	if err != nil {
		log.Fatal(err)
	}
	defer app.Close()
	if err := app.ConsumeHandoff(); err != nil {
		log.Printf("handoff: %v", err)
	}
	if err := app.DrainQueuedSyncWrites(); err != nil {
		log.Printf("sync queue: %v", err)
	}
	selectedIndex := -1
	window := &Window{app: app, selectedIndex: &selectedIndex}
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title: "YouTube Reader",
			Width: 1280, Height: 840, MinWidth: 960, MinHeight: 640,
			Content: ui.View(window.view),
		})
		window.window = win
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

type Window struct {
	app           *App
	window        *mygo.Window
	grid          ui.GridState
	selectedIndex *int
}

func (w *Window) toast(c *ui.Context, msg string, isError bool) {
	toast := ui.Toast{Title: msg, Timeout: 2 * time.Second}
	if isError {
		toast.Type = "error"
	}
	c.AddToast(toast)
}

func (w *Window) runRefresh(c *ui.Context, force bool) {
	if w.app.Busy {
		return
	}
	w.app.Busy = true
	go func() {
		outcome := w.app.Refresh(force)
		if w.window != nil {
			w.window.Update(func() {
				w.app.Busy = false
				if outcome.Description != "" {
					isError := len(outcome.Failures) > 0
					w.toast(c, outcome.Description, isError)
				}
				if outcome.Changed {
					for _, n := range outcome.Notified {
						postNotification(n)
					}
				}
			})
		} else {
			w.app.Busy = false
		}
	}()
}

func postNotification(plan NotificationPlan) {
	body := fmt.Sprintf("%d new video%s", plan.Count, plural(plan.Count))
	if plan.NewestTitle != "" {
		body += "\n" + plan.NewestTitle
	}
	n := mygo.NewNotification(mygo.NotificationOptions{
		ID:       "ytreader-" + plan.ChannelID,
		Title:    plan.ChannelName,
		Subtitle: "New videos",
		Body:     body,
	})
	if err := n.Show(); err != nil {
		log.Printf("notification: %v", err)
	}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func (w *Window) view(c *ui.Context) {
	t := c.Theme()
	tokens := mui.NewTokens(t, t.Dark)
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		w.sidebar(c, tokens)
		w.content(c, tokens)
	})
}

func (w *Window) sidebar(c *ui.Context, t mui.Tokens) {
	sidebar := ui.Column(c).Width(256).Shrink(0).Background(t.Sidebar)
	sidebar.Draw(func(p *ui.Painter, r ui.Rect) {
		p.Fill(ui.Rect{X: r.X + r.W - 1, Y: r.Y, W: 1, H: r.H}, t.Border, 0)
	})
	sidebar.Children(func() {
		ui.Row(c).Height(72).Padding(20, 20, 16).AlignItems(ui.Center).Gap(12).Children(func() {
			ui.Column(c).Gap(2).Grow(1).Children(func() {
				ui.Text(c, "MyGo").FontSize(17).Bold().TextColor(t.Text).SingleLine()
				ui.Text(c, "YouTube Reader").FontSize(13).TextColor(t.Muted).SingleLine()
			})
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
				sectionHeader(c, t, g.Name)
				for _, id := range g.ChannelIDs {
					id := id
					ch := w.channelByID(id)
					mui.ChannelNavRow(c, t, fmt.Sprintf("group:%s:channel:%s", g.Name, id), ch.Title, ch.AvatarURL, w.app.Selected == "channel:"+id, func() {
						w.app.Selected = "channel:" + id
					})
				}
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
				ui.Text(c, "Last: "+orDash(w.app.Settings.LastRefresh)).FontSize(13).TextColor(t.Muted)
				ui.Text(c, "Next: "+orDash(w.app.Settings.NextRefresh)).FontSize(13).TextColor(t.Muted)
				for _, f := range w.app.Settings.RecentFailures {
					ui.Row(c).Gap(8).AlignItems(ui.Start).Children(func() {
						ui.Icon(c, mui.IconAlert).Size(16, 16)
						ui.Text(c, f.Message).FontSize(13).TextColor(t.Danger).MaxLines(2).Grow(1)
					})
				}
			}
		})
	})
}

func sectionHeader(c *ui.Context, t mui.Tokens, title string) {
	ui.Text(c, title).FontSize(13).TextColor(t.Faint).Padding(20, 12, 8).SingleLine()
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

func (w *Window) content(c *ui.Context, t mui.Tokens) {
	ui.Column(c).Grow(1).MinWidth(0).Background(t.App).Children(func() {
		w.header(c, t)
		if w.app.SettingsOpen {
			w.settings(c, t)
			return
		}
		if w.app.Settings.APIKey == "" {
			w.emptyState(c, t, "Set up YouTube Reader", "Add a YouTube Data API key in Settings to start following channels.", "setup-settings", "Open Settings", func(*ui.Context) { w.app.SettingsOpen = true })
			return
		}
		if len(w.app.Sync.Channels) == 0 {
			w.emptyState(c, t, "Import your channels", "Choose an OPML file from your Mac to import channel groups.", "setup-import", "Import OPML", w.importOPML)
			return
		}
		w.gridArea(c, t)
		w.drawer(c, t)
	})
}

func (w *Window) emptyState(c *ui.Context, t mui.Tokens, title, body, key, label string, action func(*ui.Context)) {
	ui.Column(c).Grow(1).AlignItems(ui.Center).Center().Gap(20).Padding(32).Children(func() {
		ui.Column(c).Width(480).AlignItems(ui.Center).Gap(12).Children(func() {
			ui.Text(c, title).Bold().FontSize(32).FixedLineHeight(38).TextColor(t.Text).TextAlign(ui.Center)
			ui.Text(c, body).FontSize(15).FixedLineHeight(22).TextColor(t.Muted).TextAlign(ui.Center)
		})
		mui.StatefulButton(c, t, key, label, nil, true, mui.Default, func() {
			action(c)
		})
	})
}

func (w *Window) header(c *ui.Context, t mui.Tokens) {
	title := w.title()
	subtitle := refreshedLabel(w.app.Settings.LastRefresh)
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
			} else if len(w.app.Settings.RecentFailures) > 0 {
				state = mui.Error
			}
			if state == mui.Loading {
				ui.Text(c, "Refreshing...").FontSize(13).TextColor(t.Muted)
			}
			if len(w.app.Settings.RecentFailures) > 0 && !w.app.Busy {
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
			ui.Checkbox(c.Key("unread"), &w.app.UnreadOnly, "Unread").Label("Unread").FontSize(14)
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
		return "Refreshed at " + t.Local().Format("15:04")
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

func (w *Window) gridArea(c *ui.Context, t mui.Tokens) {
	videos := w.app.VisibleVideos()
	ui.Column(c).Absolute().Top(defaultHeaderHeight).Left(12).Right(332).Bottom(16).Children(func() {
		if len(videos) == 0 {
			if w.app.SelectedVideoID != "" {
				w.app.SelectedVideoID = ""
			}
			none := -1
			w.selectedIndex = &none
			w.emptyState(c, t, "Nothing here yet", "Try another filter or refresh your subscriptions.", "empty-refresh", "Refresh now", func(c *ui.Context) { w.runRefresh(c, true) })
			return
		}
		target := -1
		if w.app.SelectedVideoID != "" {
			for i := range videos {
				if videos[i].ID == w.app.SelectedVideoID {
					target = i
					break
				}
			}
			if target < 0 {
				w.app.SelectedVideoID = ""
			}
		}
		w.selectedIndex = &target
		w.grid.Selected = w.selectedIndex
		grid := ui.GridView(c, &w.grid, len(videos), 240, 302, func(i int) {
			v := videos[i]
			selected := w.app.SelectedVideoID == v.ID
			card := ui.Column(c).Height(302).Gap(5).Padding(12).Radius(10).Background(t.Surface).Border(1, t.BorderSoft).Transition(mui.Fade).Clip()
			if selected {
				card.Border(1, t.Accent)
			} else if card.Hovered() {
				card.Background(t.SurfaceAlt)
			}
			card.Children(func() {
				ui.Row(c).Height(160).FillWidth().Radius(8).Clip().Children(func() {
					mui.Thumbnail(c, t, v.ThumbnailURL, 0, 0)
				})
				ui.Text(c, v.Title).Bold().FontSize(14).FixedLineHeight(20).Height(40).MaxLines(2).Label(v.Title)
				if desc := strings.TrimSpace(v.Description); desc != "" {
					ui.Text(c, desc).FontSize(13).TextColor(t.Muted).FixedLineHeight(18).Height(36).MaxLines(2)
				}
				ui.Row(c).Height(18).Gap(8).AlignItems(ui.Center).Children(func() {
					if !w.app.Sync.IsRead(v.ID) {
						ui.Row(c).Size(6, 6).Radius(3).Background(t.Accent).Label("Unread")
					}
					mui.ChannelAvatar(c, t, v.AvatarURL, v.ChannelTitle, 18)
					ui.Text(c, v.ChannelTitle).FontSize(13).TextColor(t.Muted).SingleLine().Grow(1)
					if v.IsShort {
						ui.Text(c, "Shorts").FontSize(13).TextColor(t.Faint).SingleLine()
					}
					ui.Text(c, core.RelativeDate(v.PublishedAt, time.Now())).FontSize(13).TextColor(t.Muted).SingleLine()
				})
			})
		}).Grow(1).Padding(0, 12, 16)
		if grid.Changed() {
			if w.grid.Selected != nil && *w.grid.Selected < len(videos) {
				w.app.SelectedVideoID = videos[*w.grid.Selected].ID
			}
		}
	})
}

func (w *Window) drawer(c *ui.Context, t mui.Tokens) {
	drawer := ui.Column(c).Width(320).Padding(24, 20).Gap(16).Background(t.App).Absolute().Top(defaultHeaderHeight).Right(0).Bottom(0)
	drawer.Draw(func(p *ui.Painter, r ui.Rect) {
		p.Fill(ui.Rect{X: r.X, Y: r.Y, W: 1, H: r.H}, t.Border, 0)
	})
	drawer.Children(func() {
		if w.app.SelectedVideoID == "" {
			ui.Column(c).Grow(1).Center().AlignItems(ui.Center).Gap(12).Children(func() {
				ui.Icon(c, mui.IconInfo).Size(28, 28)
				ui.Text(c, "Select a video to see details.").FontSize(14).TextColor(t.Muted).TextAlign(ui.Center)
			})
			return
		}
		v, ok := w.app.VideoByID(w.app.SelectedVideoID)
		if !ok {
			ui.Text(c, "Video not found.").FontSize(14).TextColor(t.Muted)
			return
		}
		ui.Text(c, v.Title).Bold().FontSize(20).FixedLineHeight(26).MaxLines(3)
		ui.Text(c, v.ChannelTitle+"  -  "+core.RelativeDate(v.PublishedAt, time.Now())).FontSize(13).TextColor(t.Muted)
		if v.ThumbnailURL != "" {
			ui.Row(c).Height(158).FillWidth().Radius(10).Clip().Children(func() {
				mui.Thumbnail(c, t, v.ThumbnailURL, 0, 0)
			})
		}
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Text(c, v.Description).FontSize(14).FixedLineHeight(21).TextColor(t.Text)
		})
		ui.Column(c).Gap(8).Children(func() {
			mui.StatefulButton(c, t, "open-youtube", "Open in YouTube", mui.IconExternal, true, mui.Default, func() {
				w.app.MarkRead(v)
				c.OpenURL("https://www.youtube.com/watch?v=" + v.ID)
			})
			label, key := "Favorite", "favorite"
			if w.app.Sync.IsFavorite(v.ID) {
				label, key = "Unfavorite", "unfavorite"
			}
			mui.StatefulButton(c, t, key, label, mui.IconStar, false, mui.Default, func() {
				w.app.ToggleFavorite(v)
				if w.app.Sync.IsFavorite(v.ID) {
					w.toast(c, "Added to favorites", false)
				} else {
					w.toast(c, "Removed from favorites", false)
				}
			})
			if w.app.Sync.IsRead(v.ID) {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, mui.IconInfo).Size(16, 16)
					ui.Text(c, "Read").FontSize(13).TextColor(t.Success)
				})
			}
		})
	})
}

func (w *Window) settings(c *ui.Context, t mui.Tokens) {
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).Gap(16).Padding(24, 32, 32).Children(func() {
			ui.Text(c, "API key (local only, never synced)").FontSize(14)
			ui.TextInput(c.Key("api-key"), &w.app.Settings.APIKey).Password().Placeholder("YouTube Data API v3 key").Width(520)
			ui.Text(c, "Refresh interval").FontSize(14)
			selected := refreshLabel(w.app.Settings.RefreshMinutes)
			if ui.Select(c.Key("interval"), &selected, []string{"15 minutes", "30 minutes", "1 hour", "2 hours", "4 hours"}).Width(520).Changed() {
				w.app.Settings.RefreshMinutes = refreshMinutes(selected)
				_ = w.app.SaveSettings()
			}
			if ui.Checkbox(c.Key("shorts"), &w.app.Settings.ShowShorts, "Show Shorts").FontSize(14).Changed() {
				_ = w.app.SaveSettings()
			}
			ui.Divider(c)
			ui.Text(c, "OPML import").Bold().FontSize(15)
			mui.StatefulButton(c, t, "import-opml", "Choose OPML file...", nil, false, mui.Default, func() { w.importOPML(c) }).AlignSelf(ui.Start)
			ui.Text(c, "Favorites backup").Bold().FontSize(15)
			ui.TextInput(c.Key("backup-path"), &w.app.Settings.BackupPath).Width(520)
			ui.Row(c).Gap(8).Children(func() {
				mui.StatefulButton(c, t, "export-fav", "Export favorites", nil, false, mui.Default, func() {
					if err := w.app.ExportFavorites(); err != nil {
						w.toast(c, "Export failed: "+err.Error(), true)
					} else {
						w.toast(c, "Favorites exported", false)
					}
				})
				mui.StatefulButton(c, t, "import-fav", "Import favorites", nil, false, mui.Default, func() {
					if err := w.app.ImportFavorites(); err != nil {
						w.toast(c, "Import failed: "+err.Error(), true)
					} else {
						w.toast(c, "Favorites imported", false)
					}
				})
			})
			ui.Text(c, "Sync file (single JSON shared with Syncthing)").Bold().FontSize(15)
			ui.TextInput(c.Key("sync-path"), &w.app.Settings.SyncPath).Width(520)
			mui.StatefulButton(c, t, "save-paths", "Save paths", nil, false, mui.Default, func() {
				_ = w.app.SaveSettings()
				w.toast(c, "Settings saved", false)
			}).AlignSelf(ui.Start)
			ui.Divider(c)
			ui.Text(c, "Notifications (local to this device)").Bold().FontSize(15)
			mui.StatefulButton(c, t, "enable-group", "Enable all for selected group", nil, false, mui.Default, func() {
				w.app.EnableNotificationsForSelection()
				w.toast(c, "Notifications enabled", false)
			}).AlignSelf(ui.Start)
			channels := make([]core.Channel, len(w.app.Channels))
			copy(channels, w.app.Channels)
			ui.Scroll(c).Height(180).Children(func() {
				for i := range channels {
					enabled := w.app.notificationEnabled(channels[i].ID)
					if ui.Checkbox(c.Key("notify-"+channels[i].ID), &enabled, channels[i].Title).Changed() {
						w.app.Settings.NotificationIDs = toggleID(w.app.Settings.NotificationIDs, channels[i].ID, enabled)
						_ = w.app.SaveSettings()
					}
				}
			})
			ui.Text(c, "Cold-launch notification click routing is not supported in this MVP.").FontSize(13).TextColor(t.Muted)
			ui.Divider(c)
			ui.Text(c, "Background helper").Bold().FontSize(15)
			ui.Text(c, "Register the standalone helper with a user LaunchAgent (RunAtLoad + StartInterval) to refresh while the app is closed.").FontSize(13).TextColor(t.Muted)
			ui.Row(c).Gap(8).Children(func() {
				mui.StatefulButton(c, t, "install-helper", "Install LaunchAgent", nil, false, mui.Default, func() {
					if err := installHelper(); err != nil {
						w.toast(c, "Install failed: "+err.Error(), true)
					} else {
						w.toast(c, "Helper installed", false)
					}
				})
				mui.StatefulButton(c, t, "uninstall-helper", "Uninstall LaunchAgent", nil, false, mui.Default, func() {
					if err := uninstallHelper(); err != nil {
						w.toast(c, "Uninstall failed: "+err.Error(), true)
					} else {
						w.toast(c, "Helper uninstalled", false)
					}
				})
			})
			mui.StatefulButton(c, t, "force-lock", "Force take over sync", nil, false, mui.Default, func() {
				w.app.Lock.Release()
				lock, err := core.AcquireLock(localLockDir())
				if err == nil {
					w.app.Lock = lock
					w.toast(c, "Sync lock taken over", false)
				} else {
					w.toast(c, "Take over failed: "+err.Error(), true)
				}
			}).AlignSelf(ui.Start)
			mui.StatefulButton(c, t, "close-settings", "Done", nil, true, mui.Default, func() {
				_ = w.app.SaveSettings()
				w.app.SettingsOpen = false
			}).AlignSelf(ui.Start)
		})
	})
}

func toggleID(ids []string, id string, add bool) []string {
	out := []string{}
	for _, x := range ids {
		if x != id {
			out = append(out, x)
		}
	}
	if add {
		out = append(out, id)
	}
	return out
}

func refreshLabel(m int) string {
	switch m {
	case 15:
		return "15 minutes"
	case 30:
		return "30 minutes"
	case 120:
		return "2 hours"
	case 240:
		return "4 hours"
	default:
		return "1 hour"
	}
}
func refreshMinutes(s string) int {
	switch s {
	case "15 minutes":
		return 15
	case "30 minutes":
		return 30
	case "2 hours":
		return 120
	case "4 hours":
		return 240
	default:
		return 60
	}
}

func (w *Window) importOPML(c *ui.Context) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:      w.window,
		Title:       "Import OPML",
		DefaultPath: filepath.Join(os.Getenv("HOME"), "Downloads"),
		Filters:     []mygo.FileFilter{{Name: "OPML", Extensions: []string{"opml", "xml"}}},
		Multiple:    true,
	})
	if err != nil {
		w.toast(c, "Import failed: "+err.Error(), true)
		return
	}
	if len(paths) == 0 {
		return
	}
	if err := w.app.ImportOPMLPaths(paths); err != nil {
		w.toast(c, "Import failed: "+err.Error(), true)
		return
	}
	w.toast(c, "Imported channels", false)
}
