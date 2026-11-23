package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
	mui "github.com/simon/youtube-reader/ui"
)

// The Settings window takes its geometry from the agreed design (board A4,
// 4.6, and the gap table's row E): a separate 520 x 460 window with a 52 pt
// title bar that macOS's window controls sit in, a 40 pt pane toolbar under
// it, and a scrollable form body of a 140 pt label column beside a control
// column. Minimize and zoom stay in the title bar, dimmed, because the board
// fixes the window's size; only the close button acts.
//
// The fixed lengths below are the board's own measurements, named once. Every
// colour, radius, spacing step, focus ring, and transition comes from the T0
// token layer in ui/theme.go and ui/typography.go.
const (
	settingsWindowWidth  float32 = 520
	settingsWindowHeight float32 = 460
	settingsTitleBarH    float32 = 52
	settingsPaneBarH     float32 = 40
	settingsPaneInset    float32 = 16
	settingsLabelColumn  float32 = 140
	settingsControlH     float32 = 28
	settingsAPIKeyWidth  float32 = 240
	settingsPopupWidth   float32 = 160
	settingsSegmentH     float32 = 22
	settingsStepperValue float32 = 40
	settingsStepperStep  float32 = 26
	settingsGlyph        float32 = 16

	// The rest of the board's control geometry. These are the native
	// controls' own metrics (board B1), not spacing between blocks, so they
	// are named here rather than derived from the layout ladder: a
	// segmented control's 2 pt inset and 9 pt segment padding, its 22 pt
	// segment, the 32 x 18 switch, and the 6 pt glyph gap.
	settingsSegmentInset  float32 = 2
	settingsSegmentGap    float32 = 2
	settingsSegmentPadX   float32 = 9
	settingsSegmentRadius float32 = 4
	settingsSwitchWidth   float32 = 32
	settingsSwitchHeight  float32 = 18
	settingsSwitchRadius  float32 = 9
	settingsGlyphGap      float32 = 6
)

// The four glyphs the shared icon set does not have yet: the API key's
// reveal eye and its struck-through twin, and the stepper's minus and plus.
// They follow the set's 24 pt box, 1.5 pt stroke, and round joins.
var (
	settingsIconEye    = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M2.5 12S6 5.5 12 5.5 21.5 12 21.5 12 18 18.5 12 18.5 2.5 12 2.5 12Z"/><circle cx="12" cy="12" r="3"/></svg>`))
	settingsIconEyeOff = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M10.6 5.7A9.6 9.6 0 0 1 12 5.5c6 0 9.5 6.5 9.5 6.5a17 17 0 0 1-2.7 3.6"/><path d="M6.3 7.7A17 17 0 0 0 2.5 12S6 18.5 12 18.5a9.4 9.4 0 0 0 4-1"/><path d="M9.9 9.9a3 3 0 0 0 4.2 4.2"/><path d="m3 3 18 18"/></svg>`))
	settingsIconMinus  = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14"/></svg>`))
	settingsIconPlus   = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M12 5v14"/><path d="M5 12h14"/></svg>`))
)

// settingsPane is one of the window's three panes.
type settingsPane int

const (
	settingsGeneral settingsPane = iota
	settingsBackups
	settingsNotifications
)

var settingsPaneNames = [...]string{"General", "Backups", "Notifications"}

// title is the pane's name, which the window title follows.
func (p settingsPane) title() string {
	if int(p) < 0 || int(p) >= len(settingsPaneNames) {
		return settingsPaneNames[settingsGeneral]
	}
	return settingsPaneNames[p]
}

// name is the pane's name as settings.json keeps it.
func (p settingsPane) name() string { return p.title() }

// settingsPaneFromName restores the pane shown last time, General for a name
// this build does not know.
func settingsPaneFromName(name string) settingsPane {
	for i, candidate := range settingsPaneNames {
		if candidate == name {
			return settingsPane(i)
		}
	}
	return settingsGeneral
}

// settingsWindow is the standalone Settings window's view state. MyGo owns the
// native window; this keeps the pane and the field state that belongs to the
// window rather than to the model.
type settingsWindow struct {
	win *mygo.Window
	// pane is the pane shown, restored from the last session on open.
	pane settingsPane
	// revealAPIKey shows the API key in its field. It is never persisted.
	revealAPIKey bool
}

// refreshChoices are the refresh intervals the popup offers, in minutes. Zero
// is Manual: the app never refreshes on its own, while Refresh still does.
var refreshChoices = []struct {
	minutes int
	label   string
}{
	{0, "Manual"},
	{5, "5 min"},
	{15, "15 min"},
	{30, "30 min"},
	{60, "60 min"},
}

// defaultRefreshMinutes is the interval a new install gets.
const defaultRefreshMinutes = 15

func refreshChoiceLabel(minutes int) string {
	if minutes <= 0 {
		return refreshChoices[0].label
	}
	for _, choice := range refreshChoices {
		if choice.minutes == minutes {
			return choice.label
		}
	}
	return fmt.Sprintf("%d min", minutes)
}

// refreshChoicesFor lists the board's five intervals plus any other stored
// interval, so a value from an older build stays visible and selectable.
func refreshChoicesFor(minutes int) []settingsChoice {
	choices := make([]settingsChoice, 0, len(refreshChoices)+1)
	for _, choice := range refreshChoices {
		choices = append(choices, settingsChoice{value: choice.label, label: choice.label})
	}
	if minutes <= 0 {
		return choices
	}
	for _, choice := range refreshChoices {
		if choice.minutes == minutes {
			return choices
		}
	}
	legacy := settingsChoice{value: refreshChoiceLabel(minutes), label: refreshChoiceLabel(minutes)}
	at := len(choices)
	for i, choice := range refreshChoices {
		if choice.minutes > minutes {
			at = i
			break
		}
	}
	choices = append(choices, settingsChoice{})
	copy(choices[at+1:], choices[at:])
	choices[at] = legacy
	return choices
}

func refreshChoiceMinutes(label string) int {
	for _, choice := range refreshChoices {
		if choice.label == label {
			return choice.minutes
		}
	}
	var minutes int
	if _, err := fmt.Sscanf(label, "%d min", &minutes); err == nil && minutes > 0 {
		return minutes
	}
	return defaultRefreshMinutes
}

// settingsWindowOptions is the board's settings window: 520 x 460 and fixed,
// which is why minimize and zoom stay in the title bar dimmed and only the
// close button acts. MyGo draws the controls over the view, so the view draws
// the 52 pt title bar itself.
func settingsWindowOptions(pane settingsPane, view func(*ui.Context)) mygo.WindowOptions {
	return mygo.WindowOptions{
		Title:             pane.title(),
		Width:             int(settingsWindowWidth),
		Height:            int(settingsWindowHeight),
		DisableResize:     true,
		DisableMinimize:   true,
		DisableMaximize:   true,
		DisableFullScreen: true,
		TitleBarStyle:     mygo.TitleBarHiddenInset,
		Content:           ui.View(view),
	}
}

// newSettingsWindow creates the standalone window. Tests replace it to watch
// which window the entry points ask for.
var newSettingsWindow = mygo.NewWindow

// openSettingsWindow shows the Settings window, creating it on first use and
// restoring the pane shown last time. It runs on the main thread: from the App
// menu's Settings…, the overflow menu, and the empty state's button.
func (w *Window) openSettingsWindow() {
	if win := w.settingsWindow.win; win != nil && !win.IsDestroyed() {
		win.Show()
		win.Focus()
		return
	}
	// A closed window's state is gone: a new one starts from the pane shown
	// last time, which the close saved.
	pane := settingsPaneFromName(w.app.settingsSnapshot().LastSettingsPane)
	w.settingsWindow = settingsWindow{pane: pane}
	win := newSettingsWindow(settingsWindowOptions(pane, w.settingsView))
	if win == nil {
		return
	}
	w.settingsWindow.win = win
	win.OnClosed(func() {
		// A window that is no longer the current one was already replaced.
		if w.settingsWindow.win != win {
			return
		}
		w.settingsWindow = settingsWindow{}
	})
}

func (w *Window) closeSettingsWindow() {
	if win := w.settingsWindow.win; win != nil {
		win.Close()
	}
}

// setSettingsPane switches panes from the window's pane toolbar and keeps the
// choice for the next open.
func (w *Window) setSettingsPane(pane settingsPane) {
	if w.settingsWindow.pane == pane {
		return
	}
	w.settingsWindow.pane = pane
	if win := w.settingsWindow.win; win != nil {
		win.SetTitle(pane.title())
		win.Invalidate()
	}
	w.saveSettings(func(s *core.Settings) bool {
		if s.LastSettingsPane == pane.name() {
			return false
		}
		s.LastSettingsPane = pane.name()
		return true
	})
}

// refreshSettingsWindow repaints an open Settings window after a change made
// outside it, as a picker, an export, or a refused notification.
func (w *Window) refreshSettingsWindow() {
	if win := w.settingsWindow.win; win != nil {
		win.Invalidate()
	}
}

// saveSettings applies a change made in the Settings window. Both windows
// show several of the same values, so both repaint when one changed, and the
// menu bar item follows because the setup and paused states read settings.
func (w *Window) saveSettings(fn func(*core.Settings) bool) {
	changed, _ := w.app.updateSettings(fn)
	w.settingsChanged()
	if changed {
		w.invalidateMainWindow()
		w.refreshSettingsWindow()
	}
}

// settingsChanged rebuilds the menu bar item after a settings write, so the
// item and its popover never report different states.
func (w *Window) settingsChanged() {
	w.syncRefreshMenu()
	if w.status == nil {
		return
	}
	w.status.refresh()
	w.status.invalidate()
}

// invalidateMainWindow repaints the main window after a change to the data it
// shows, as an import, an erase, or a new notification consent.
func (w *Window) invalidateMainWindow() {
	if w.window != nil {
		w.window.Invalidate()
	}
}

// settingsView draws the window's content. It is also what the tests render,
// so it never touches the native window.
func (w *Window) settingsView(c *ui.Context) {
	t := c.Theme()
	tokens := mui.NewTokens(t, t.Dark)
	settings := w.app.settingsSnapshot()
	c.OnWindowShortcut(0, ui.KeyEscape, w.closeSettingsWindow)
	ui.Column(c).Fill().Background(tokens.App).Children(func() {
		w.settingsTitleBar(c, tokens)
		w.settingsPaneBar(c, tokens)
		ui.Scroll(c).Grow(1).Children(func() {
			// The body's bottom inset is small: the board's three panes fit
			// 368 pt almost exactly, and a pane that overflows, as a long
			// channel list does, still scrolls under it.
			ui.Column(c).FillWidth().Padding(mui.Space(4), mui.Space(5), mui.Space(2)).Gap(mui.Space(3)).Children(func() {
				switch w.settingsWindow.pane {
				case settingsBackups:
					w.backupsPane(c, tokens, settings)
				case settingsNotifications:
					w.notificationsPane(c, tokens, settings)
				default:
					w.generalPane(c, tokens, settings)
				}
			})
		})
	})
}

// settingsTitleBar draws the 52 pt bar under the window controls: the active
// pane's name, centred on the window, on the app surface. It drags the window
// as the system title bar it replaces does.
func (w *Window) settingsTitleBar(c *ui.Context, t mui.Tokens) {
	bar := ui.Row(c).Height(settingsTitleBarH).Shrink(0).FillWidth().Background(t.App).DragWindow()
	bar.Draw(func(p *ui.Painter, r ui.Rect) {
		p.Fill(ui.Rect{X: r.X, Y: r.Y + r.H - mui.Hairline, W: r.W, H: mui.Hairline}, t.BorderSoft, 0)
	})
	bar.Children(func() {
		ui.Box(c).Grow(1)
		mui.ToolbarTitle.Apply(ui.Text(c, w.settingsWindow.pane.title())).TextColor(t.Text).SingleLine()
		ui.Box(c).Grow(1)
	})
}

// settingsPaneBar draws the pane toolbar: the one segmented control that
// switches panes. It is not customisable and always shows the active pane.
func (w *Window) settingsPaneBar(c *ui.Context, t mui.Tokens) {
	bar := ui.Row(c).Height(settingsPaneBarH).Shrink(0).FillWidth().AlignItems(ui.Center).Padding(0, settingsPaneInset).Background(t.App)
	bar.Draw(func(p *ui.Painter, r ui.Rect) {
		p.Fill(ui.Rect{X: r.X, Y: r.Y + r.H - mui.Hairline, W: r.W, H: mui.Hairline}, t.BorderSoft, 0)
	})
	bar.Children(func() {
		selected := int(w.settingsWindow.pane)
		parts := ui.SegmentedBase(c.Key("settings-panes"), &selected, len(settingsPaneNames))
		// The pane toolbar takes the keyboard as the window opens, as the
		// first control of a settings window does: Tab starts at the top of
		// the visual order and the arrows switch panes straight away.
		track := parts.Track.Height(settingsControlH).Shrink(0).Padding(settingsSegmentInset).Gap(settingsSegmentGap).
			Radius(mui.RadiusControl).Background(t.SurfaceAlt).Border(mui.Hairline, t.Border).
			AlignItems(ui.Center).Label("Settings panes")
		track.Children(func() {
			for i, name := range settingsPaneNames {
				segment := parts.Segment(i).Height(settingsSegmentH).Padding(0, settingsSegmentPadX).
					Radius(settingsSegmentRadius).Center().FocusRingColor(t.Focus).FocusRingOffset(mui.FocusRingGap)
				if i == int(w.settingsWindow.pane) {
					segment.AutoFocus()
					segment.Background(t.Surface).Border(mui.Hairline, t.BorderSoft)
					segment.Children(func() {
						mui.Secondary.Apply(ui.Text(c, name)).FontWeight(int(mui.Semibold)).TextColor(t.Text).SingleLine()
					})
					continue
				}
				segment.Background(t.SurfaceAlt)
				segment.Children(func() {
					mui.Secondary.Apply(ui.Text(c, name)).TextColor(t.Muted).SingleLine()
				})
			}
		})
		if track.Changed() {
			w.setSettingsPane(settingsPane(selected))
		}
	})
}

// settingsRow is one label-and-control row of a form: the 140 pt label column,
// its first line on the control's centre, and the control column beside it.
// The control column holds the row's control and, under it, its helpers.
func (w *Window) settingsRow(c *ui.Context, t mui.Tokens, label string, control func()) {
	ui.Row(c).AlignItems(ui.Start).Gap(mui.Space(3)).Children(func() {
		ui.Box(c).Width(settingsLabelColumn).Shrink(0).Height(settingsControlH).AlignItems(ui.Start).Justify(ui.Center).Children(func() {
			mui.Control.Apply(ui.Text(c, label)).TextColor(t.Text).SingleLine()
		})
		ui.Column(c).Grow(1).MinWidth(0).Gap(mui.Space(1)).Children(control)
	})
}

// settingsHelper is the 11 pt line under a control; in Danger it is the
// field's validation message.
func (w *Window) settingsHelper(c *ui.Context, t mui.Tokens, text string, isError bool) {
	color := t.Muted
	if isError {
		color = t.Danger
	}
	mui.Helper.Apply(ui.Text(c, text)).TextColor(color).MaxLines(2).Wrap()
}

// settingsSectionHeader titles a group inside a pane.
func (w *Window) settingsSectionHeader(c *ui.Context, t mui.Tokens, text string) {
	mui.SectionHeader.Apply(ui.Text(c, text)).TextColor(t.Text).SingleLine()
}

// settingsDivider is the 1 pt rule between groups, as wide as the form.
func (w *Window) settingsDivider(c *ui.Context, t mui.Tokens) {
	ui.Box(c).Height(mui.Hairline).FillWidth().Background(t.BorderSoft).MarginY(mui.Space(1))
}

// generalPane holds the API key, the refresh schedule, the grid, the
// background helper, and the subscription import, as the board's A4 General
// pane draws it.
func (w *Window) generalPane(c *ui.Context, t mui.Tokens, settings core.Settings) {
	apiKey := strings.TrimSpace(settings.APIKey)
	invalidKey := false
	w.settingsRow(c, t, "API key", func() {
		ui.Row(c).Gap(mui.Space(2)).AlignItems(ui.Center).Children(func() {
			field := ui.TextInput(c.Key("settings-api-key"), &apiKey).Width(settingsAPIKeyWidth).Height(settingsControlH).
				PaddingX(mui.Space(2)).Radius(mui.RadiusControl).Background(t.Surface).Border(mui.Hairline, t.Border).
				TextColor(t.Text).Label("API key").Tooltip("YouTube Data API v3 key").
				FocusRingColor(t.Focus).FocusRingOffset(mui.FocusRingGap)
			mui.Control.Apply(field)
			if !w.settingsWindow.revealAPIKey {
				field.Password()
			}
			// The board validates on blur: while the field has the focus the
			// message stays neutral, and a stored key that is wrong shows its
			// message as soon as the pane opens. An empty key stays neutral.
			invalidKey = !field.Focused() && apiKey != "" && !validAPIKey(apiKey)
			if field.Changed() {
				key := strings.TrimSpace(apiKey)
				w.saveSettings(func(s *core.Settings) bool {
					if s.APIKey == key {
						return false
					}
					s.APIKey = key
					return true
				})
			}
			revealLabel := "Reveal API key"
			revealGlyph := settingsIconEye
			if w.settingsWindow.revealAPIKey {
				revealLabel = "Hide API key"
				revealGlyph = settingsIconEyeOff
			}
			w.settingsButton(c, t, "settings-reveal-key", "", revealGlyph, settingsButtonSecondary, true, revealLabel, func() {
				w.settingsWindow.revealAPIKey = !w.settingsWindow.revealAPIKey
				w.refreshSettingsWindow()
			})
		})
		if invalidKey {
			w.settingsHelper(c, t, "That is not a YouTube Data API key.", true)
			return
		}
		w.settingsHelper(c, t, "Stored on this Mac only. Never synced.", false)
	})

	w.settingsRow(c, t, "Refresh interval", func() {
		selected := refreshChoiceLabel(settings.RefreshMinutes)
		popup := w.settingsPopup(c, t, "settings-refresh-interval", settingsPopupWidth, refreshChoicesFor(settings.RefreshMinutes), &selected)
		if popup.Changed() {
			minutes := refreshChoiceMinutes(selected)
			changed := false
			w.saveSettings(func(s *core.Settings) bool {
				if s.RefreshMinutes == minutes {
					return false
				}
				s.RefreshMinutes = minutes
				changed = true
				return true
			})
			if changed {
				// An installed helper schedules the interval its login item was
				// written with. Manual has no background helper at all.
				switch {
				case !helperInstalled():
				case minutes <= 0:
					if err := uninstallHelper(); err != nil {
						w.toast(c, "Helper: "+err.Error(), true)
					}
					w.refreshSettingsWindow()
				default:
					if err := installHelper(minutes); err != nil {
						w.toast(c, "Helper: "+err.Error(), true)
					}
				}
			}
		}
		if settings.RefreshMinutes <= 0 {
			w.settingsHelper(c, t, "Refreshes only when you choose Refresh.", false)
		}
	})

	showShorts := settings.ShowShorts
	w.settingsRow(c, t, "Show Shorts", func() {
		ui.Row(c).Height(settingsControlH).AlignItems(ui.Center).Children(func() {
			if w.settingsSwitch(c, t, "settings-show-shorts", &showShorts, "Show Shorts").Changed() {
				w.saveSettings(func(s *core.Settings) bool {
					if s.ShowShorts == showShorts {
						return false
					}
					s.ShowShorts = showShorts
					return true
				})
			}
		})
	})

	columns := settings.MaxGridColumns
	if columns < minMaxGridColumns || columns > maxMaxGridColumns {
		columns = defaultMaxGridColumns
	}
	w.settingsRow(c, t, "Grid columns", func() {
		ui.Row(c).Height(settingsControlH).AlignItems(ui.Center).Gap(mui.Space(2)).Children(func() {
			w.settingsStepper(c, t, "settings-grid-columns", columns, minMaxGridColumns, maxMaxGridColumns, func(next int) {
				w.saveSettings(func(s *core.Settings) bool {
					if s.MaxGridColumns == next {
						return false
					}
					s.MaxGridColumns = next
					return true
				})
			})
			w.settingsHelper(c, t, fmt.Sprintf("%d to %d columns", minMaxGridColumns, maxMaxGridColumns), false)
		})
	})

	w.settingsDivider(c, t)

	w.settingsSectionHeader(c, t, "Background helper")
	installed := helperInstalled()
	switch {
	case settings.RefreshMinutes > 0:
		mui.Secondary.Apply(ui.Text(c, "Refreshes subscriptions while the app is closed. Installs a login item you can remove at any time.")).
			TextColor(t.Muted).MaxLines(2).Wrap()
	case installed:
		mui.Secondary.Apply(ui.Text(c, "Installed while an interval was set. Manual runs no background helper.")).
			TextColor(t.Muted).MaxLines(2).Wrap()
	default:
		mui.Secondary.Apply(ui.Text(c, "Choose a refresh interval above to run the helper while the app is closed.")).
			TextColor(t.Muted).MaxLines(2).Wrap()
	}
	if installed || settings.RefreshMinutes > 0 {
		ui.Row(c).Gap(mui.Space(2)).AlignItems(ui.Center).Children(func() {
			label := "Install helper…"
			if installed {
				label = "Uninstall helper…"
			}
			w.settingsButton(c, t, "settings-helper", label, nil, settingsButtonSecondary, true, "", func() {
				var err error
				if installed {
					err = uninstallHelper()
				} else {
					err = installHelper(settings.RefreshMinutes)
				}
				if err != nil {
					w.toast(c, "Helper: "+err.Error(), true)
					return
				}
				w.refreshSettingsWindow()
				w.invalidateMainWindow()
			})
			status := "Not installed"
			if installed {
				status = "Installed"
			}
			w.settingsHelper(c, t, status, false)
		})
	}

	w.settingsRow(c, t, "Subscriptions", func() {
		ui.Row(c).Gap(mui.Space(2)).AlignItems(ui.Center).Children(func() {
			w.settingsButton(c, t, "settings-import-opml", "Import OPML…", nil, settingsButtonSecondary, true, "", func() {
				w.importOPML(c)
			})
			w.settingsHelper(c, t, "Channels and groups merge by ID.", false)
		})
	})
}

// backupsPane holds the favorites backup file, its export and import, the
// shared sync file, and the pane's one destructive action.
func (w *Window) backupsPane(c *ui.Context, t mui.Tokens, settings core.Settings) {
	backupPath := settings.BackupPath
	w.settingsRow(c, t, "Backup file", func() {
		ui.Row(c).Gap(mui.Space(2)).AlignItems(ui.Center).Children(func() {
			w.settingsPathField(c, t, "settings-backup-path", "Backup file", &backupPath)
			w.settingsButton(c, t, "settings-choose-backup", "Choose…", nil, settingsButtonSecondary, true, "Choose backup file", func() {
				path, ok := w.chooseSavePath("Favorites backup", backupPath, "Favorites backup", "json")
				if !ok {
					return
				}
				w.saveSettings(func(s *core.Settings) bool {
					if s.BackupPath == path {
						return false
					}
					s.BackupPath = path
					return true
				})
				w.refreshSettingsWindow()
			})
		})
		ui.Row(c).Gap(mui.Space(2)).AlignItems(ui.Center).Children(func() {
			w.settingsButton(c, t, "settings-export-favorites", "Export favorites…", nil, settingsButtonSecondary, true, "", func() {
				w.exportFavorites(c)
			}).Grow(1)
			w.settingsButton(c, t, "settings-import-favorites", "Restore favorites…", nil, settingsButtonSecondary, true, "", func() {
				w.restoreFavorites(c)
			}).Grow(1)
		})
		lastExport := "Last export: Never"
		if at := core.RelativeDate(settings.LastFavoritesExport, time.Now()); at != "" {
			lastExport = "Last export: " + at
		}
		mui.Secondary.Apply(ui.Text(c, lastExport)).TextColor(t.Muted).SingleLine()
	})

	w.settingsDivider(c, t)

	syncPath := settings.SyncPath
	w.settingsRow(c, t, "Sync file", func() {
		ui.Row(c).Gap(mui.Space(2)).AlignItems(ui.Center).Children(func() {
			w.settingsPathField(c, t, "settings-sync-path", "Sync file", &syncPath)
			w.settingsButton(c, t, "settings-choose-sync", "Choose…", nil, settingsButtonSecondary, true, "Choose sync file", func() {
				path, ok := w.chooseSavePath("Sync file", syncPath, "Sync file", "json")
				if !ok {
					return
				}
				w.saveSettings(func(s *core.Settings) bool {
					if s.SyncPath == path {
						return false
					}
					s.SyncPath = path
					return true
				})
				w.refreshSettingsWindow()
			})
		})
		w.settingsHelper(c, t, "One JSON file shared with Syncthing. Groups, reads and favorites only.", false)
		w.settingsButton(c, t, "settings-force-takeover", "Force take over sync", nil, settingsButtonSecondary, true, "", func() {
			if w.app.Lock != nil {
				w.app.Lock.Release()
			}
			lock, err := core.AcquireLock(localLockDir())
			if err != nil {
				w.toast(c, "Take over failed: "+err.Error(), true)
				return
			}
			w.app.Lock = lock
			w.toast(c, "Sync lock taken over", false)
		}).AlignSelf(ui.Start)
	})

	// The destructive action sits 24 pt below the rest of the form.
	ui.Box(c).Height(mui.Space(3))
	w.settingsDivider(c, t)
	w.settingsRow(c, t, "", func() {
		ui.Row(c).Gap(mui.Space(2)).AlignItems(ui.Center).Children(func() {
			w.settingsButton(c, t, "settings-erase-favorites", "Erase local favorites…", nil, settingsButtonDanger, len(w.app.Sync.Favorites) > 0, "", func() {
				w.confirmEraseFavorites(c)
			})
		})
		w.settingsHelper(c, t, "Removes every favorite from this Mac. Cannot be undone here.", false)
	})
}

// notificationsPane holds explicit per-channel consent, grouped like the left
// sidebar with derived per-group batch toggles, and the permission state when
// macOS refused the app's notifications.
func (w *Window) notificationsPane(c *ui.Context, t mui.Tokens, settings core.Settings) {
	if settings.NotificationPermissionDenied {
		w.notificationsDeniedBanner(c, t)
	}
	mui.Secondary.Apply(ui.Text(c, notificationsDescription(c.Locale()))).
		TextColor(t.Muted).MaxLines(3).Wrap()

	groups := w.notificationGroups()
	totalChannels := 0
	for _, grp := range groups {
		totalChannels += len(grp.Channels)
	}

	channels := w.app.channelSnapshot()

	w.settingsRow(c, t, "", func() {
		ui.Row(c).Gap(mui.Space(2)).AlignItems(ui.Center).Children(func() {
			w.settingsButton(c, t, "settings-enable-all", enableAllLabel(c.Locale()), nil, settingsButtonSecondary, len(channels) > 0, "", func() {
				if !w.app.EnableAllNotifications() {
					return
				}
				w.toast(c, notificationsToastEnabled(c.Locale()), false)
				w.invalidateMainWindow()
				w.refreshSettingsWindow()
			})
			w.settingsHelper(c, t, notificationsHelperText(c.Locale()), false)
		})
	})

	if totalChannels == 0 {
		mui.Secondary.Apply(ui.Text(c, notificationsEmptyText(c.Locale()))).TextColor(t.Muted).SingleLine()
		return
	}

	ui.Column(c).FillWidth().Gap(mui.Space(3)).Children(func() {
		for _, grp := range groups {
			w.notificationGroupCard(c, t, settings, grp)
		}
	})
}

// notificationGroup holds the display state for one group card in the
// settings notifications pane.
type notificationGroup struct {
	ID          string
	Name        string
	IsUngrouped bool
	Channels    []core.Channel
}

// notificationGroups partitions channels into ordered group sections that match
// the left sidebar, followed by an Ungrouped section if any channels lack a group.
func (w *Window) notificationGroups() []notificationGroup {
	allChannels := w.app.channelSnapshot()
	channelsByID := make(map[string]core.Channel, len(allChannels)+len(w.app.Channels))
	for _, ch := range w.app.Channels {
		channelsByID[ch.ID] = ch
	}
	for _, ch := range allChannels {
		existing, ok := channelsByID[ch.ID]
		if ok {
			if ch.Title == "" && existing.Title != "" {
				ch.Title = existing.Title
			}
			if ch.AvatarURL == "" && existing.AvatarURL != "" {
				ch.AvatarURL = existing.AvatarURL
			}
		}
		channelsByID[ch.ID] = ch
	}
	for id, ch := range channelsByID {
		found := false
		for _, existing := range allChannels {
			if existing.ID == id {
				found = true
				break
			}
		}
		if !found {
			allChannels = append(allChannels, ch)
		}
	}

	var rawGroups []core.ChannelGroup
	if len(w.app.Groups) > 0 && len(w.app.Groups) == len(w.app.Sync.Groups) {
		rawGroups = w.app.Groups
	} else if len(w.app.Sync.Groups) > 0 {
		rawGroups = orderGroups(w.app.Sync.Groups, w.app.settingsSnapshot().GroupOrder)
	}

	var displayGroups []notificationGroup
	groupedIDs := make(map[string]bool)

	for _, g := range rawGroups {
		seen := make(map[string]bool)
		var groupChannels []core.Channel
		for _, cid := range g.ChannelIDs {
			if cid == "" || seen[cid] {
				continue
			}
			seen[cid] = true
			ch, ok := channelsByID[cid]
			if !ok {
				ch = w.channelByID(cid)
			}
			groupChannels = append(groupChannels, ch)
			groupedIDs[cid] = true
		}
		displayGroups = append(displayGroups, notificationGroup{
			ID:          g.ID,
			Name:        g.Name,
			IsUngrouped: false,
			Channels:    groupChannels,
		})
	}

	var ungroupedChannels []core.Channel
	for _, ch := range allChannels {
		if !groupedIDs[ch.ID] {
			ungroupedChannels = append(ungroupedChannels, ch)
		}
	}
	sort.Slice(ungroupedChannels, func(i, j int) bool {
		if ungroupedChannels[i].Title != ungroupedChannels[j].Title {
			return ungroupedChannels[i].Title < ungroupedChannels[j].Title
		}
		return ungroupedChannels[i].ID < ungroupedChannels[j].ID
	})

	if len(ungroupedChannels) > 0 {
		displayGroups = append(displayGroups, notificationGroup{
			ID:          "ungrouped",
			Name:        "",
			IsUngrouped: true,
			Channels:    ungroupedChannels,
		})
	}

	return displayGroups
}

// notificationGroupCard renders one group card with a master switch and summary
// header over its channel rows.
func (w *Window) notificationGroupCard(c *ui.Context, t mui.Tokens, settings core.Settings, grp notificationGroup) {
	total := len(grp.Channels)
	enabledCount := 0
	targetIDs := make([]string, 0, total)
	for _, ch := range grp.Channels {
		targetIDs = append(targetIDs, ch.ID)
		if containsID(settings.NotificationIDs, ch.ID) {
			enabledCount++
		}
	}
	allOn := total > 0 && enabledCount == total
	groupSwitchOn := allOn
	groupTitle := grp.Name
	if grp.IsUngrouped {
		groupTitle = ungroupedHeader(c.Locale())
	}
	summary := notificationsGroupSummary(c.Locale(), enabledCount, total)
	switchKey := "settings-notify-group-" + grp.ID

	card := ui.Column(c.Key("settings-notify-card-"+grp.ID)).
		FillWidth().
		Border(mui.Hairline, t.BorderSoft).
		Radius(mui.RadiusNotice).
		Background(t.Surface).
		Clip()

	card.Children(func() {
		// Group card header
		ui.Row(c).Height(36).FillWidth().AlignItems(ui.Center).Gap(mui.Space(2)).
			Padding(0, mui.Space(3)).Background(t.SurfaceAlt).Children(func() {
			mui.SectionHeader.Apply(ui.Text(c, groupTitle)).TextColor(t.Text).SingleLine().Grow(1)
			mui.Metadata.Apply(ui.Text(c, summary)).TextColor(t.Muted).SingleLine()
			sw := w.settingsSwitch(c, t, switchKey, &groupSwitchOn, groupTitle)
			if total == 0 {
				sw.Disabled(true)
			}
			if sw.Changed() {
				w.saveSettings(func(s *core.Settings) bool {
					if allOn {
						s.NotificationIDs = removeIDs(s.NotificationIDs, targetIDs)
					} else {
						s.NotificationIDs = addIDs(s.NotificationIDs, targetIDs)
					}
					return true
				})
			}
		})

		if total == 0 {
			ui.Box(c).Height(mui.Hairline).FillWidth().Background(t.BorderSoft)
			ui.Row(c).Height(30).FillWidth().AlignItems(ui.Center).Padding(0, mui.Space(3)).Children(func() {
				emptyLabel := "No channels in this group"
				if strings.HasPrefix(strings.ToLower(c.Locale()), "zh") {
					emptyLabel = "该分组暂无频道"
				}
				mui.Secondary.Apply(ui.Text(c, emptyLabel)).TextColor(t.Muted).SingleLine()
			})
			return
		}

		ui.Box(c).Height(mui.Hairline).FillWidth().Background(t.BorderSoft)

		// Channel rows
		for i, channel := range grp.Channels {
			if i > 0 {
				ui.Box(c).Height(mui.Hairline).FillWidth().Background(t.BorderSoft)
			}
			channel := channel
			title := channel.Title
			if title == "" {
				title = channel.ID
			}
			enabled := containsID(settings.NotificationIDs, channel.ID)
			channelSwitchKey := fmt.Sprintf("settings-notify-%s-%s", grp.ID, channel.ID)

			ui.Row(c).Height(mui.SidebarChannelRowHeight).FillWidth().AlignItems(ui.Center).Gap(mui.Space(2)).
				Padding(0, mui.Space(3)).Children(func() {
				mui.ChannelAvatar(c, t, channel.AvatarURL, title, mui.SidebarChannelAvatar)
				mui.Control.Apply(ui.Text(c, title)).TextColor(t.Text).SingleLine().Grow(1)
				if w.settingsSwitch(c, t, channelSwitchKey, &enabled, title).Changed() {
					id := channel.ID
					w.saveSettings(func(s *core.Settings) bool {
						s.NotificationIDs = toggleID(s.NotificationIDs, id, enabled)
						return true
					})
				}
			})
		}
	})
}

func notificationsDescription(locale string) string {
	if strings.HasPrefix(strings.ToLower(locale), "zh") {
		return "开启频道的通知，在其发布新视频时收到提醒。通知仅在此 Mac 上生效。"
	}
	return "Turn on a channel to be notified when it publishes a new video. Notifications are local to this Mac."
}

func enableAllLabel(locale string) string {
	if strings.HasPrefix(strings.ToLower(locale), "zh") {
		return "全部开启"
	}
	return "Enable All"
}

func notificationsHelperText(locale string) string {
	if strings.HasPrefix(strings.ToLower(locale), "zh") {
		return "为你关注的每个频道设置。"
	}
	return "For every channel you follow."
}

func notificationsEmptyText(locale string) string {
	if strings.HasPrefix(strings.ToLower(locale), "zh") {
		return "导入订阅后选择频道。"
	}
	return "Import subscriptions to choose channels."
}

func notificationsGroupSummary(locale string, enabled, total int) string {
	if strings.HasPrefix(strings.ToLower(locale), "zh") {
		return fmt.Sprintf("%d/%d 已启用", enabled, total)
	}
	return fmt.Sprintf("%d/%d enabled", enabled, total)
}

func notificationsToastEnabled(locale string) string {
	if strings.HasPrefix(strings.ToLower(locale), "zh") {
		return "已开启通知"
	}
	return "Notifications enabled"
}

// containsID reports whether id appears in ids.
func containsID(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// addIDs adds targetIDs to ids without duplicates, preserving order.
func addIDs(ids []string, targetIDs []string) []string {
	seen := make(map[string]bool, len(ids)+len(targetIDs))
	out := make([]string, 0, len(ids)+len(targetIDs))
	for _, id := range ids {
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, id := range targetIDs {
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// removeIDs removes all targetIDs from ids, preserving order.
func removeIDs(ids []string, targetIDs []string) []string {
	remove := make(map[string]bool, len(targetIDs))
	for _, id := range targetIDs {
		remove[id] = true
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !remove[id] {
			out = append(out, id)
		}
	}
	return out
}

// notificationsDeniedBanner offers the one useful action when macOS refused
// the app's notifications: the System Settings pane that allows them.
func (w *Window) notificationsDeniedBanner(c *ui.Context, t mui.Tokens) {
	ui.Row(c).FillWidth().Padding(mui.Space(2), settingsSegmentPadX).Gap(mui.Space(2)).
		AlignItems(ui.Center).Radius(mui.RadiusNotice).
		Background(t.Warning.Alpha(0.12)).Border(mui.Hairline, t.Warning.Alpha(0.28)).Children(func() {
		ui.Icon(c, mui.IconAlert).Size(settingsGlyph, settingsGlyph).TextColor(t.Warning)
		mui.Secondary.Apply(ui.Text(c, "Notifications are turned off for YouTube Reader in System Settings.")).
			TextColor(t.Warning).Grow(1).MaxLines(2).Wrap()
		w.settingsButton(c, t, "settings-open-notification-settings", "Open System Settings…", nil, settingsButtonSecondary, true, "", func() {
			c.OpenURL("x-apple.systempreferences:com.apple.Notifications-Settings.extension")
		})
	})
}

// settingsPathField is one editable file path: 11.5 pt monospace on the
// surface, with the whole path in its tooltip. Every edit is saved at once, so
// closing the window or switching panes never loses it.
func (w *Window) settingsPathField(c *ui.Context, t mui.Tokens, key, label string, path *string) ui.Element {
	field := ui.TextInput(c.Key(key), path).Height(settingsControlH).PaddingX(mui.Space(2)).
		Radius(mui.RadiusControl).Background(t.Surface).Border(mui.Hairline, t.Border).
		TextColor(t.Text).Label(label).Tooltip(*path).Grow(1).MinWidth(0).
		FocusRingColor(t.Focus).FocusRingOffset(mui.FocusRingGap)
	mui.MonospacePath.Apply(field)
	if field.Changed() {
		next := *path
		w.saveSettings(func(s *core.Settings) bool {
			switch key {
			case "settings-sync-path":
				if s.SyncPath == next {
					return false
				}
				s.SyncPath = next
			case "settings-backup-path":
				if s.BackupPath == next {
					return false
				}
				s.BackupPath = next
			}
			return true
		})
	}
	return field
}

// settingsSwitch is the board's 32 x 18 switch. MyGo draws it natively, with
// the platform's white knob and Reduce Motion honoured; the themed copy gives
// it the T0 accent instead of MyGo's default blue, so it matches the window.
func (w *Window) settingsSwitch(c *ui.Context, t mui.Tokens, key string, on *bool, label string) ui.Element {
	themed := *c.Theme()
	themed.Accent = t.Accent
	themed.AccentText = t.AccentText
	themed.InactiveAccent = t.Accent
	themed.Focus = t.Focus
	themed.FocusRingWidth = mui.FocusRing
	themed.FocusRingOffset = mui.FocusRingGap
	var sw ui.Element
	ui.Themed(c, &themed).Background(ui.Transparent).Shrink(0).Children(func() {
		sw = ui.Switch(c.Key(key), on).Size(settingsSwitchWidth, settingsSwitchHeight).Radius(settingsSwitchRadius).
			Shrink(0).Label(label).FocusRingColor(t.Focus).FocusRingOffset(mui.FocusRingGap)
	})
	return sw
}

// settingsStepper is the 28 pt numeric stepper for the grid's column count: two
// 26 pt buttons around a 40 pt tabular value, as board B1 draws it.
func (w *Window) settingsStepper(c *ui.Context, t mui.Tokens, key string, value, lo, hi int, onChange func(int)) {
	clampValue := func(v int) int { return clampInt(v, lo, hi) }
	step := ui.Row(c).Height(settingsControlH).Shrink(0).AlignItems(ui.Center).
		Radius(mui.RadiusControl).Background(t.Surface).Border(mui.Hairline, t.Border).Clip()
	step.Children(func() {
		w.settingsStepperButton(c, t, key+"-minus", "Decrease", settingsIconMinus, value > lo, func() {
			onChange(clampValue(value - 1))
		})
		ui.Box(c).Width(settingsStepperValue).Height(settingsControlH).Center().
			BorderStartWidth(mui.Hairline).BorderEndWidth(mui.Hairline).BorderColor(t.BorderSoft).Children(func() {
			mui.Metadata.Apply(ui.Text(c, fmt.Sprintf("%d", value))).TextColor(t.Text)
		})
		w.settingsStepperButton(c, t, key+"-plus", "Increase", settingsIconPlus, value < hi, func() {
			onChange(clampValue(value + 1))
		})
	})
}

func (w *Window) settingsStepperButton(c *ui.Context, t mui.Tokens, key, label string, glyph *ui.SVG, enabled bool, onClick func()) {
	background := t.Surface
	if !enabled {
		background = t.SurfaceAlt
	}
	button := ui.ButtonBase(c.Key(key)).Width(settingsStepperStep).Height(settingsControlH).Center().
		Shrink(0).Background(background).Label(label).Cursor(ui.CursorPointer).
		FocusRingColor(t.Focus).FocusRingOffset(-mui.FocusRingGap)
	if !enabled {
		button.Disabled(true)
	}
	button.Children(func() {
		ui.Icon(c, glyph).Size(14, 14).TextColor(t.Muted)
	})
	button.Draw(func(p *ui.Painter, r ui.Rect) {
		if !enabled {
			return
		}
		if face, ok := buttonPressFace(button, t); ok {
			p.Fill(r, face, 0)
		}
	})
	if button.Clicked() {
		onClick()
	}
}

// settingsChoice is one option of the pop-up button.
type settingsChoice struct {
	value string
	label string
}

// settingsPopup is the 28 pt pop-up button: the chosen label and a chevron on
// a bordered surface, opening a token-styled list of choices. MyGo's
// SelectBase supplies the keyboard model: arrows to open and move, Enter and
// Space to choose, Escape to close.
func (w *Window) settingsPopup(c *ui.Context, t mui.Tokens, key string, width float32, choices []settingsChoice, selected *string) ui.Element {
	parts := ui.SelectBase(c.Key(key), selected)
	trigger := parts.Trigger.Height(settingsControlH).Width(width).Shrink(0).PaddingX(mui.Space(2)).
		Justify(ui.SpaceBetween).AlignItems(ui.Center).Label(*selected).
		Radius(mui.RadiusControl).Background(t.Surface).Border(mui.Hairline, t.Border).
		FocusRingColor(t.Focus).FocusRingOffset(mui.FocusRingGap)
	trigger.Children(func() {
		mui.Control.Apply(ui.Text(c, *selected)).TextColor(t.Text).SingleLine()
		ui.Icon(c, mui.IconChevronDown).Size(12, 12).TextColor(t.Muted)
	})
	trigger.Draw(func(p *ui.Painter, r ui.Rect) {
		if face, ok := buttonPressFace(trigger, t); ok {
			p.Fill(ui.Rect{X: r.X - 1, Y: r.Y - 1, W: r.W + 2, H: r.H + 2}, face, mui.RadiusControl)
		}
	})
	parts.Popup(func(panel ui.Element) {
		panel.MinWidth(width).Padding(mui.Space(1)).Radius(mui.RadiusNotice).
			Background(t.Surface).Border(mui.Hairline, t.Border).
			Shadow(0, mui.ElevationOffsetY, mui.ElevationBlur, mui.ElevationSpread, t.Shadow)
		for _, choice := range choices {
			item := parts.Item(choice.value).Height(settingsControlH).Padding(0, settingsSegmentPadX).
				Radius(mui.RadiusControl).Justify(ui.Start).AlignItems(ui.Center)
			switch {
			case item.Highlighted():
				item.Background(t.AccentSoft)
			case choice.value == *selected:
				item.Background(t.SurfaceAlt)
			}
			item.Children(func() {
				color := t.Text
				if item.Highlighted() {
					color = t.Accent
				}
				mui.Control.Apply(ui.Text(c, choice.label)).TextColor(color).SingleLine()
			})
		}
	})
	return trigger
}

// settingsButtonKind selects one of the board's button faces.
type settingsButtonKind int

const (
	settingsButtonSecondary settingsButtonKind = iota
	settingsButtonDanger
)

// settingsButton is the board's 28 pt button: 13 pt label, a 6 pt radius, a
// 1 pt outline, 12 pt inner padding, and the 2 pt focus ring at its 3 pt
// offset. A dangerous button has no outline; hover and press paint a
// danger-tinted face instead.
func (w *Window) settingsButton(c *ui.Context, t mui.Tokens, key, label string, glyph *ui.SVG, kind settingsButtonKind, enabled bool, accessibleName string, onClick func()) ui.Element {
	background, border, text := t.Surface, t.Border, t.Text
	switch kind {
	case settingsButtonDanger:
		background, border, text = ui.Transparent, ui.Transparent, t.Danger
	}
	if !enabled {
		background, border, text = t.SurfaceAlt, t.Border, t.Muted
	}
	button := ui.ButtonBase(c.Key(key)).Height(settingsControlH).Shrink(0).AlignItems(ui.Center).
		Radius(mui.RadiusControl).Background(background).Border(mui.Hairline, border).TextColor(text).
		Cursor(ui.CursorPointer).Transition(mui.Fade).
		FocusRingColor(t.Focus).FocusRingOffset(mui.FocusRingGap)
	if label == "" {
		button.Width(settingsControlH).Padding(0)
	} else {
		button.Gap(settingsGlyphGap).Padding(0, mui.Space(3))
	}
	if !enabled {
		button.Disabled(true)
	}
	if accessibleName != "" {
		button.Label(accessibleName)
	}
	button.Children(func() {
		if glyph != nil {
			ui.Icon(c, glyph).Size(settingsGlyph, settingsGlyph)
		}
		if label != "" {
			mui.Control.Apply(ui.Text(c, label)).SingleLine()
		}
	})
	button.Draw(func(p *ui.Painter, r ui.Rect) {
		if !enabled {
			return
		}
		switch {
		case button.Pressed():
			p.Fill(ui.Rect{X: r.X - 1, Y: r.Y - 1, W: r.W + 2, H: r.H + 2}, settingsButtonPressedFace(t, kind), mui.RadiusControl)
		case button.Hovered():
			p.Fill(ui.Rect{X: r.X - 1, Y: r.Y - 1, W: r.W + 2, H: r.H + 2}, settingsButtonHoverFace(t, kind), mui.RadiusControl)
		}
	})
	if button.Clicked() && onClick != nil {
		onClick()
	}
	return button
}

func settingsButtonHoverFace(t mui.Tokens, kind settingsButtonKind) ui.Color {
	switch kind {
	case settingsButtonDanger:
		return t.Danger.Alpha(0.10)
	default:
		return t.SurfaceAlt
	}
}

func settingsButtonPressedFace(t mui.Tokens, kind settingsButtonKind) ui.Color {
	switch kind {
	case settingsButtonDanger:
		return t.Danger.Alpha(0.16)
	default:
		return t.BorderSoft
	}
}

// buttonPressFace returns the face a control paints over itself while pressed
// or hovered, for controls that draw their own outline.
func buttonPressFace(el ui.Element, t mui.Tokens) (ui.Color, bool) {
	switch {
	case el.Pressed():
		return t.BorderSoft, true
	case el.Hovered():
		return t.SurfaceAlt, true
	}
	return ui.Transparent, false
}

// validAPIKey accepts a YouTube Data API v3 key: 39 characters, the "AIza"
// prefix every key Google issues. It never returns the key itself.
func validAPIKey(key string) bool {
	return len(key) == 39 && strings.HasPrefix(key, "AIza")
}

// dialogParent is the window a sheet or alert attaches to: the Settings
// window when it invoked the dialog, otherwise the main window.
func (w *Window) dialogParent() *mygo.Window {
	if win := w.settingsWindow.win; win != nil && !win.IsDestroyed() {
		return win
	}
	return w.window
}

// helperInstalled reports whether the login item is on disk, which is the
// truth the pane shows rather than a remembered flag.
func helperInstalled() bool {
	_, err := os.Stat(helperLaunchAgentPath())
	return err == nil
}

// chooseSavePath asks where to write a destination file and returns the path,
// false when the panel was cancelled. Both choosers name files the app writes,
// so they use the save panel rather than one that only selects existing files.
func (w *Window) chooseSavePath(title, current, filterName, extension string) (string, bool) {
	defaultPath := current
	if defaultPath == "" {
		defaultPath = filepath.Join(os.Getenv("HOME"), "Downloads", title+"."+extension)
	}
	path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Parent:      w.dialogParent(),
		Title:       title,
		DefaultPath: defaultPath,
		Filters:     []mygo.FileFilter{{Name: filterName, Extensions: []string{extension}}},
	})
	if err != nil || path == "" {
		return "", false
	}
	return path, true
}

// confirmEraseFavorites asks with the system's own alert before the one action
// that loses data. The Erase button is marked destructive, and Cancel is both
// the default and what Escape chooses.
func (w *Window) confirmEraseFavorites(c *ui.Context) {
	count := len(w.app.Sync.Favorites)
	if count == 0 {
		return
	}
	plural := "favorites"
	if count == 1 {
		plural = "favorite"
	}
	result, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent:             w.dialogParent(),
		Type:               mygo.MessageWarning,
		Title:              "Erase local favorites?",
		Message:            fmt.Sprintf("Erase all %d %s from this Mac?", count, plural),
		Detail:             "This cannot be undone here. Other devices that still hold these favorites can bring them back when they sync.",
		Buttons:            []string{"Cancel", "Erase"},
		DefaultButton:      0,
		CancelButton:       0,
		DestructiveButtons: []int{1},
	})
	if err != nil {
		w.toast(c, "Erase failed: "+err.Error(), true)
		return
	}
	if result.Button != 1 {
		return
	}
	if err := w.app.eraseFavorites(); err != nil {
		w.toast(c, "Erase failed: "+err.Error(), true)
		return
	}
	w.toast(c, "Favorites erased", false)
	w.invalidateMainWindow()
	w.refreshSettingsWindow()
}

// eraseFavorites removes every favorite from the sync state and writes the
// file, taking a newer file from another device first so the erase does not
// drop its groups or read states.
func (a *App) eraseFavorites() error {
	a.settingsMu.RLock()
	syncPath := a.Settings.SyncPath
	device := a.DeviceID
	a.settingsMu.RUnlock()
	if current, err := core.LoadSyncWithConflicts(syncPath, device); err == nil && current.UpdatedAt > a.Sync.UpdatedAt {
		a.Sync = core.MergeSync(current, a.Sync)
	}
	a.Sync.Favorites = map[string]core.FavoriteRecord{}
	a.Sync.UpdatedAt = time.Now().UnixMilli()
	return a.SaveSync()
}

// postNotification shows one local notification and records a refusal, so the
// Notifications pane can offer System Settings instead of retrying forever.
// It reports whether the recorded permission state changed. The background
// helper's notifications arrive here too, through the local socket, so a
// refusal is remembered whichever process asked for the refresh.
func (a *App) postNotification(plan NotificationPlan) bool {
	err := showNotificationForApp(plan)
	if err != nil && !errors.Is(err, mygo.ErrNotificationsDenied) {
		// A failure that is not a refusal - notifications unsupported in an
		// unbundled run, or the event loop stopping - says nothing about the
		// permission macOS recorded, so the pane keeps its answer.
		return false
	}
	denied := errors.Is(err, mygo.ErrNotificationsDenied)
	changed, _ := a.updateSettings(func(s *core.Settings) bool {
		if s.NotificationPermissionDenied == denied {
			return false
		}
		s.NotificationPermissionDenied = denied
		return true
	})
	return changed
}

// postNotification shows one notification and rebuilds the surfaces that report
// a refused permission.
func (w *Window) postNotification(plan NotificationPlan) {
	if !w.app.postNotification(plan) {
		return
	}
	w.settingsChanged()
	w.invalidateMainWindow()
	w.refreshSettingsWindow()
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

func (w *Window) importOPML(c *ui.Context) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:      w.dialogParent(),
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
	w.invalidateMainWindow()
	w.refreshSettingsWindow()
}

func (w *Window) restoreFavorites(c *ui.Context) {
	defaultPath := w.app.settingsSnapshot().BackupPath
	if defaultPath == "" {
		defaultPath = filepath.Join(os.Getenv("HOME"), "Downloads")
	}
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:      w.dialogParent(),
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
	w.invalidateMainWindow()
	w.refreshSettingsWindow()
}
