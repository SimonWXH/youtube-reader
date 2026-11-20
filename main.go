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
	"github.com/egoist/mygo/ui/netimage"
	"github.com/simon/youtube-reader/core"
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
	window := &Window{app: app}
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title: "YouTube Reader",
			Width: 1180, Height: 800, MinWidth: 900, MinHeight: 600,
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
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		w.sidebar(c, t)
		w.content(c, t)
	})
}

func (w *Window) sidebar(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Width(216).Shrink(0).Background(t.Background).Border(0, t.Border).AlignSelf(ui.Stretch).Children(func() {
		ui.Text(c, "YouTube Reader").Bold().FontSize(16).Padding(14, 14, 8)
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Sidebar(c, &w.app.Selected, func() {
				ui.SidebarItem(c, "all", nil, "All")
				ui.SidebarItem(c, "favorites", nil, "Favorites")
				for _, g := range w.app.Groups {
					open := true
					ui.SidebarSection(c, g.Name, &open, func() {
						for _, id := range g.ChannelIDs {
							ch := w.channelByID(id)
							ui.SidebarItem(c, "channel:"+id, nil, ch.Title)
						}
					})
				}
				ui.SidebarSection(c, "Channels", boolPtr(true), func() {
					for _, ch := range w.app.Channels {
						ui.SidebarItem(c, "channel:"+ch.ID, nil, ch.Title)
					}
				})
			})
		})
		statusTitle := "Status"
		if w.app.StatusExpanded {
			statusTitle = "Status (last refresh / next / failures)"
		}
		if ui.Button(c.Key("status"), statusTitle).Label("Toggle status").Clicked() {
			w.app.StatusExpanded = !w.app.StatusExpanded
		}
		if w.app.StatusExpanded {
			ui.Text(c, "Last: "+orDash(w.app.Settings.LastRefresh)).FontSize(11).TextColor(t.TextMuted).Padding(4, 12, 0)
			ui.Text(c, "Next: "+orDash(w.app.Settings.NextRefresh)).FontSize(11).TextColor(t.TextMuted).Padding(0, 12, 4)
			for _, f := range w.app.Settings.RecentFailures {
				ui.Text(c, "! "+f.Message).FontSize(10).TextColor(t.Danger).MaxLines(2).Padding(0, 12, 2)
			}
		}
	})
}

func boolPtr(b bool) *bool { return &b }

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

func (w *Window) content(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Grow(1).MinWidth(0).Children(func() {
		ui.Row(c).Height(52).Padding(0, 12).AlignItems(ui.Center).Shrink(0).Children(func() { w.topControls(c, t) })
		if w.app.SettingsOpen {
			w.settings(c, t)
			return
		}
		if w.app.Settings.APIKey == "" {
			ui.Column(c).Grow(1).AlignItems(ui.Center).Center().Gap(12).Children(func() {
				ui.Text(c, "Set up YouTube Reader").Bold().FontSize(20)
				ui.Text(c, "Add a YouTube Data API key in Settings to start following channels.").TextColor(t.TextMuted)
				if ui.Button(c.Key("setup-settings"), "Open Settings").Clicked() {
					w.app.SettingsOpen = true
				}
			})
			return
		}
		if len(w.app.Sync.Channels) == 0 {
			ui.Column(c).Grow(1).AlignItems(ui.Center).Center().Gap(12).Children(func() {
				ui.Text(c, "Import your channels").Bold().FontSize(20)
				ui.Text(c, "Choose an OPML file from your Mac to import channel groups.").TextColor(t.TextMuted)
				if ui.Button(c.Key("setup-import"), "Import OPML").Clicked() {
					w.importOPML(c)
				}
			})
			return
		}
		ui.Row(c).Grow(1).MinWidth(0).Children(func() {
			w.gridArea(c, t)
			w.drawer(c, t)
		})
	})
}

func (w *Window) topControls(c *ui.Context, t *ui.Theme) {
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, w.title()).Bold().FontSize(17).Grow(1).SingleLine()
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
		ui.SearchField(c.Key("search"), &w.app.Search).Width(180).Label("Search videos")
		if ui.Checkbox(c.Key("unread"), &w.app.UnreadOnly, "Unread").Changed() {
		}
		if ui.Button(c.Key("refresh"), "Refresh").Clicked() {
			w.runRefresh(c, true)
		}
		if ui.Button(c.Key("settings"), "Settings").Clicked() {
			w.app.SettingsOpen = !w.app.SettingsOpen
		}
		if w.app.Busy {
			ui.Text(c, "...").TextColor(t.TextMuted)
		}
		if len(w.app.Settings.RecentFailures) > 0 {
			ui.Text(c, "●").TextColor(t.Danger).Label("Refresh issues")
		}
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

func (w *Window) gridArea(c *ui.Context, t *ui.Theme) {
	videos := w.app.VisibleVideos()
	ui.Column(c).Grow(1).MinWidth(0).Children(func() {
		ui.Text(c, fmt.Sprintf("%d videos", len(videos))).FontSize(12).TextColor(t.TextMuted).Padding(0, 14, 4)
		if len(videos) == 0 {
			ui.Column(c).Grow(1).AlignItems(ui.Center).Center().Children(func() {
				ui.Text(c, "No videos here yet.").TextColor(t.TextMuted)
				if ui.Button(c.Key("empty-refresh"), "Refresh now").Clicked() {
					w.runRefresh(c, true)
				}
			})
			return
		}
		w.grid.Selected = w.selectedIndex
		grid := ui.GridView(c, &w.grid, len(videos), 220, 210, func(i int) {
			v := videos[i]
			selected := w.app.SelectedVideoID == v.ID
			card := ui.Column(c).Gap(3).Padding(6).Radius(8).Background(t.Surface)
			if v.IsShort {
				card.Border(2, t.Accent)
			}
			if selected {
				card.Border(2, t.Accent)
			}
			card.Children(func() {
				ui.Row(c).Height(112).Radius(6).Children(func() {
					netimage.Image(c, v.ThumbnailURL, netimage.Options{MaxSide: 336}).Size(196, 112).Radius(6)
					if v.IsShort {
						ui.Badge(c, "SHORTS").Label("Shorts")
					}
					if !w.app.Sync.IsRead(v.ID) {
						ui.Badge(c, "NEW").Label("Unread")
					}
				})
				ui.Text(c, v.Title).Bold().MaxLines(2).Label(v.Title)
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					if v.AvatarURL != "" {
						netimage.Image(c, v.AvatarURL, netimage.Options{MaxSide: 96}).Size(20, 20).Radius(10)
					}
					ui.Text(c, v.ChannelTitle).FontSize(11).TextColor(t.TextMuted).SingleLine().Grow(1)
					ui.Text(c, core.RelativeDate(v.PublishedAt, time.Now())).FontSize(11).TextColor(t.TextMuted)
				})
				ui.Text(c, v.Description).FontSize(11).TextColor(t.TextMuted).MaxLines(2)
			})
		}).Grow(1).Padding(0, 8, 8)
		if grid.Changed() {
			if w.grid.Selected != nil && *w.grid.Selected < len(videos) {
				w.app.SelectedVideoID = videos[*w.grid.Selected].ID
			}
		}
	})
}

func (w *Window) drawer(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Width(300).Shrink(0).Padding(14).Gap(10).Background(t.Background).Border(0, t.Border).AlignSelf(ui.Stretch).Children(func() {
		if w.app.SelectedVideoID == "" {
			ui.Text(c, "Select a video to see details.").TextColor(t.TextMuted)
			return
		}
		v, ok := w.app.VideoByID(w.app.SelectedVideoID)
		if !ok {
			ui.Text(c, "Video not found.").TextColor(t.TextMuted)
			return
		}
		ui.Text(c, v.Title).Bold().FontSize(15)
		ui.Text(c, v.ChannelTitle+" - "+core.RelativeDate(v.PublishedAt, time.Now())).FontSize(12).TextColor(t.TextMuted)
		if v.ThumbnailURL != "" {
			netimage.Image(c, v.ThumbnailURL, netimage.Options{MaxSide: 480}).Width(270).Height(152).Radius(8)
		}
		ui.Text(c, v.Description).FontSize(12)
		if ui.Button(c.Key("open-youtube"), "Open in YouTube").Clicked() {
			w.app.MarkRead(v)
			c.OpenURL("https://www.youtube.com/watch?v=" + v.ID)
		}
		if w.app.Sync.IsFavorite(v.ID) {
			if ui.Button(c.Key("unfavorite"), "Unfavorite").Clicked() {
				w.app.ToggleFavorite(v)
				w.toast(c, "Removed from favorites", false)
			}
		} else if ui.Button(c.Key("favorite"), "Favorite").Clicked() {
			w.app.ToggleFavorite(v)
			w.toast(c, "Added to favorites", false)
		}
		if w.app.Sync.IsRead(v.ID) {
			ui.Text(c, "Read").FontSize(11).TextColor(t.TextMuted)
		}
	})
}

func (w *Window) settings(c *ui.Context, t *ui.Theme) {
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).Gap(12).Padding(4, 16, 24).Children(func() {
			ui.Text(c, "Settings").Bold().FontSize(20)
			ui.Text(c, "API key (local only, never synced)").FontSize(12)
			ui.TextInput(c.Key("api-key"), &w.app.Settings.APIKey).Password().Placeholder("YouTube Data API v3 key").Width(420)
			ui.Text(c, "Refresh interval").FontSize(12)
			selected := refreshLabel(w.app.Settings.RefreshMinutes)
			if ui.Select(c.Key("interval"), &selected, []string{"15 minutes", "30 minutes", "1 hour", "2 hours", "4 hours"}).Changed() {
				w.app.Settings.RefreshMinutes = refreshMinutes(selected)
				_ = w.app.SaveSettings()
			}
			if ui.Checkbox(c.Key("shorts"), &w.app.Settings.ShowShorts, "Show Shorts").Changed() {
				_ = w.app.SaveSettings()
			}
			ui.Divider(c)
			ui.Text(c, "OPML import").Bold()
			if ui.Button(c.Key("import-opml"), "Choose OPML file...").Clicked() {
				w.importOPML(c)
			}
			ui.Text(c, "Favorites backup").Bold()
			ui.TextInput(c.Key("backup-path"), &w.app.Settings.BackupPath).Width(520)
			ui.Row(c).Gap(8).Children(func() {
				if ui.Button(c.Key("export-fav"), "Export favorites").Clicked() {
					err := w.app.ExportFavorites()
					if err != nil {
						w.toast(c, "Export failed: "+err.Error(), true)
					} else {
						w.toast(c, "Favorites exported", false)
					}
				}
				if ui.Button(c.Key("import-fav"), "Import favorites").Clicked() {
					err := w.app.ImportFavorites()
					if err != nil {
						w.toast(c, "Import failed: "+err.Error(), true)
					} else {
						w.toast(c, "Favorites imported", false)
					}
				}
			})
			ui.Text(c, "Sync file (single JSON shared with Syncthing)").Bold()
			ui.TextInput(c.Key("sync-path"), &w.app.Settings.SyncPath).Width(520)
			if ui.Button(c.Key("save-paths"), "Save paths").Clicked() {
				_ = w.app.SaveSettings()
				w.toast(c, "Settings saved", false)
			}
			ui.Divider(c)
			ui.Text(c, "Notifications (local to this device)").Bold()
			if ui.Button(c.Key("enable-group"), "Enable all for selected group").Clicked() {
				w.app.EnableNotificationsForSelection()
				w.toast(c, "Notifications enabled", false)
			}
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
			ui.Text(c, "Cold-launch notification click routing is not supported in this MVP.").FontSize(11).TextColor(t.TextMuted)
			ui.Divider(c)
			ui.Text(c, "Background helper").Bold()
			ui.Text(c, "Register the standalone helper with a user LaunchAgent (RunAtLoad + StartInterval) to refresh while the app is closed.").FontSize(11).TextColor(t.TextMuted)
			ui.Row(c).Gap(8).Children(func() {
				if ui.Button(c.Key("install-helper"), "Install LaunchAgent").Clicked() {
					if err := installHelper(); err != nil {
						w.toast(c, "Install failed: "+err.Error(), true)
					} else {
						w.toast(c, "Helper installed", false)
					}
				}
				if ui.Button(c.Key("uninstall-helper"), "Uninstall LaunchAgent").Clicked() {
					if err := uninstallHelper(); err != nil {
						w.toast(c, "Uninstall failed: "+err.Error(), true)
					} else {
						w.toast(c, "Helper uninstalled", false)
					}
				}
			})
			if ui.Button(c.Key("force-lock"), "Force take over sync").Clicked() {
				w.app.Lock.Release()
				lock, err := core.AcquireLock(localLockDir())
				if err == nil {
					w.app.Lock = lock
					w.toast(c, "Sync lock taken over", false)
				} else {
					w.toast(c, "Take over failed: "+err.Error(), true)
				}
			}
			if ui.Button(c.Key("close-settings"), "Done").Clicked() {
				w.app.SettingsOpen = false
			}
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
