package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
	mui "github.com/simon/youtube-reader/ui"
)

func (w *Window) settings(c *ui.Context, t mui.Tokens) {
	settings := w.app.settingsSnapshot()
	apiKey := settings.APIKey
	backupPath := settings.BackupPath
	syncPath := settings.SyncPath
	showShorts := settings.ShowShorts
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).Gap(16).Padding(24, 32, 32).Children(func() {
			ui.Text(c, "Layout").Bold().FontSize(15)
			ui.Text(c, "Maximum columns in each row (2-8)").FontSize(14)
			columns := float64(settings.MaxGridColumns)
			if ui.Stepper(c, &columns, float64(minMaxGridColumns), float64(maxMaxGridColumns), 1).Changed() {
				_, _ = w.app.updateSettings(func(s *core.Settings) bool {
					if s.MaxGridColumns == int(columns) {
						return false
					}
					s.MaxGridColumns = int(columns)
					return true
				})
			}
			ui.Divider(c)
			ui.Text(c, "API key (local only, never synced)").FontSize(14)
			apiKeyInput := ui.TextInput(c.Key("api-key"), &apiKey).Password().Placeholder("YouTube Data API v3 key").Width(520)
			if apiKeyInput.Changed() {
				_, _ = w.app.updateSettings(func(s *core.Settings) bool {
					if s.APIKey == apiKey {
						return false
					}
					s.APIKey = apiKey
					return true
				})
			}
			ui.Text(c, "Refresh interval").FontSize(14)
			selected := refreshLabel(settings.RefreshMinutes)
			if ui.Select(c.Key("interval"), &selected, []string{"15 minutes", "30 minutes", "1 hour", "2 hours", "4 hours"}).Width(520).Changed() {
				_, _ = w.app.updateSettings(func(s *core.Settings) bool {
					if s.RefreshMinutes == refreshMinutes(selected) {
						return false
					}
					s.RefreshMinutes = refreshMinutes(selected)
					return true
				})
			}
			if ui.Checkbox(c.Key("shorts"), &showShorts, "Show Shorts").FontSize(14).Changed() {
				_, _ = w.app.updateSettings(func(s *core.Settings) bool {
					if s.ShowShorts == showShorts {
						return false
					}
					s.ShowShorts = showShorts
					return true
				})
			}
			ui.Divider(c)
			ui.Text(c, "OPML import").Bold().FontSize(15)
			mui.StatefulButton(c, t, "import-opml", "Choose OPML file...", nil, false, mui.Default, func() { w.importOPML(c) }).AlignSelf(ui.Start)
			ui.Text(c, "Favorites backup (standalone JSON, no API key or history)").Bold().FontSize(15)
			if ui.TextInput(c.Key("backup-path"), &backupPath).Width(520).Changed() {
				_, _ = w.app.updateSettings(func(s *core.Settings) bool {
					if s.BackupPath == backupPath {
						return false
					}
					s.BackupPath = backupPath
					return true
				})
			}
			ui.Text(c, "Export writes the current favorites to the backup path below. Restore reads a chosen favorites JSON file and merges by stable video ID; newer local favorites win.").FontSize(13).TextColor(t.Muted)
			ui.Row(c).Gap(8).Children(func() {
				mui.StatefulButton(c, t, "export-fav", "Export favorites", nil, false, mui.Default, func() {
					if err := w.app.ExportFavorites(); err != nil {
						w.toast(c, "Export failed: "+err.Error(), true)
					} else {
						w.toast(c, "Favorites exported", false)
					}
				})
				mui.StatefulButton(c, t, "restore-fav", "Restore favorites from file...", nil, false, mui.Default, func() {
					w.restoreFavorites(c)
				})
			})
			ui.Text(c, "Sync file (single JSON shared with Syncthing)").Bold().FontSize(15)
			if ui.TextInput(c.Key("sync-path"), &syncPath).Width(520).Changed() {
				_, _ = w.app.updateSettings(func(s *core.Settings) bool {
					if s.SyncPath == syncPath {
						return false
					}
					s.SyncPath = syncPath
					return true
				})
			}
			mui.StatefulButton(c, t, "save-paths", "Save paths", nil, false, mui.Default, func() {
				_, _ = w.app.updateSettings(func(s *core.Settings) bool {
					changed := s.APIKey != apiKey || s.BackupPath != backupPath || s.SyncPath != syncPath
					s.APIKey = apiKey
					s.BackupPath = backupPath
					s.SyncPath = syncPath
					return changed
				})
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
					id := channels[i].ID
					if ui.Checkbox(c.Key("notify-"+id), &enabled, channels[i].Title).Changed() {
						_, _ = w.app.updateSettings(func(s *core.Settings) bool {
							s.NotificationIDs = toggleID(s.NotificationIDs, id, enabled)
							return true
						})
					}
				}
			})
			ui.Text(c, "Notification clicks open the channel's filtered view, including after a cold launch.").FontSize(13).TextColor(t.Muted)
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

func (w *Window) restoreFavorites(c *ui.Context) {
	defaultPath := w.app.settingsSnapshot().BackupPath
	if defaultPath == "" {
		defaultPath = filepath.Join(os.Getenv("HOME"), "Downloads")
	}
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:      w.window,
		Title:       "Restore favorites",
		DefaultPath: defaultPath,
		Filters:     []mygo.FileFilter{{Name: "Favorites backup", Extensions: []string{"json"}}},
	})
	if err != nil {
		w.toast(c, "Restore failed: "+err.Error(), true)
		return
	}
	if len(paths) == 0 {
		return
	}
	changed, err := w.app.ImportFavorites(paths[0])
	if err != nil {
		w.toast(c, "Restore failed: "+err.Error(), true)
		return
	}
	if changed == 0 {
		w.toast(c, "No newer favorites to restore", false)
		return
	}
	w.toast(c, fmt.Sprintf("Restored %d favorite%s", changed, plural(changed)), false)
}
