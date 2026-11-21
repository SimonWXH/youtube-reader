package main

import (
	"fmt"
	"log"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
	mui "github.com/simon/youtube-reader/ui"
)

const (
	defaultSidebarWidth float32 = 260
	defaultDetailsWidth float32 = 340

	minSidebarWidth float32 = 220
	maxSidebarWidth float32 = 320
	minDetailsWidth float32 = 280
	maxDetailsWidth float32 = 480
	minGridWidth    float32 = 320

	defaultMaxGridColumns = 5
	minMaxGridColumns     = 2
	maxMaxGridColumns     = 8
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
	// Register before Run so MyGo can deliver a click from an earlier run.
	// AppleScript helper notifications carry no ID; the handler uses the
	// local queue only after an actual click callback, never on launch.
	registerNotificationClick(app)
	if err := app.DrainQueuedSyncWrites(); err != nil {
		log.Printf("sync queue: %v", err)
	}
	selectedIndex := -1
	window := &Window{app: app, selectedIndex: &selectedIndex}
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title: "YouTube Reader",
			Width: 1280, Height: 808, MinWidth: 960, MinHeight: 640,
			// The view draws the 52 pt toolbar itself, under the traffic lights.
			TitleBarStyle: mygo.TitleBarHiddenInset,
			Content:       ui.View(window.shell),
		})
		window.window = win
		// The menu bar carries Settings… at Command-Comma and mirrors every
		// toolbar command, so the toolbar is never the only way to act.
		window.installMenu()
		// The status surface lives in the menu bar: one item over the window
		// and one popover rather than status copy inside the window.
		window.status = newStatusBar(window)
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

type Window struct {
	app    *App
	window *mygo.Window
	frame  *ui.Context // the last frame's context, for menu commands
	status *statusBar

	// pendingToasts are messages raised between frames, as a refresh outcome
	// is; the next frame shows them, since AddToast only works while building.
	pendingToasts []ui.Toast

	// sidebarHidden folds the sidebar pane away from the toolbar's leading
	// control or the View menu; the split layout reads it in detail.go.
	sidebarHidden bool
	// menuRefresh and menuSidebar are the system menu's mirror of the toolbar.
	menuRefresh *mygo.MenuItem
	menuSidebar *mygo.MenuItem

	grid               ui.GridState
	gridKeyboardFocus  bool
	selectedIndex      *int
	sidebarFocus       string // sidebar row that takes the focus in the next frame
	sidebarKeyboard    string // sidebar row that arrow navigation focused
	sidebarWidth       float32
	sidebarDragging    bool
	sidebarOvershoot   float32
	contentWidth       float32
	gridAvailableWidth float32
	detail             detailState
}

func (w *Window) toast(c *ui.Context, msg string, isError bool) {
	toast := ui.Toast{Title: msg, Timeout: 2 * time.Second}
	if isError {
		toast.Type = "error"
	}
	if c == nil {
		c = w.frame
	}
	if c != nil && c.Valid() {
		c.AddToast(toast)
		return
	}
	w.pendingToasts = append(w.pendingToasts, toast)
	if w.window != nil {
		w.window.Invalidate()
	}
}

// runRefresh starts one refresh off the main thread. Refresh feedback belongs
// to the menu bar status item, so a refresh neither toasts nor writes status
// text into the window; the item and an open popover follow the refresh as it
// starts and returns.
func (w *Window) runRefresh(force bool) {
	if w.app.Busy {
		return
	}
	w.app.Busy = true
	w.setMenuBusy(true)
	if w.window != nil {
		w.window.Invalidate()
	}
	if w.status != nil {
		w.status.started = time.Now()
		w.status.refresh()
		w.status.invalidate()
	}
	go func() {
		outcome := w.app.Refresh(force)
		if w.window == nil {
			w.app.Busy = false
			w.setMenuBusy(false)
			if w.status != nil {
				w.status.refresh()
			}
			return
		}
		w.window.Update(func() {
			w.app.Busy = false
			w.setMenuBusy(false)
			if w.status != nil {
				w.status.started = time.Time{}
				w.status.refresh()
				w.status.invalidate()
			}
			if outcome.Changed {
				for _, n := range outcome.Notified {
					postNotification(n)
				}
			}
		})
	}()
}

func postNotification(plan NotificationPlan) {
	body := fmt.Sprintf("%d new video%s", plan.Count, plural(plan.Count))
	if plan.NewestTitle != "" {
		body += "\n" + plan.NewestTitle
	}
	n := mygo.NewNotification(mygo.NotificationOptions{
		ID:       core.ChannelNotificationID(plan.ChannelID),
		Title:    plan.ChannelName,
		Subtitle: "New videos",
		Body:     body,
	})
	if err := n.Show(); err != nil {
		log.Printf("notification: %v", err)
	}
}

func registerNotificationClick(app *App) (off func()) {
	return mygo.App.OnNotificationClick(notificationClickHandler(app))
}

func notificationClickHandler(app *App) func(string) {
	return func(id string) {
		if channelID, ok := core.ChannelFromNotificationID(id); ok {
			selectNotificationChannel(app, channelID)
			return
		}
		if id != "" {
			return
		}
		// A helper's AppleScript notification has no ID. Only a real click
		// callback reaches here, so consuming one queued post is safe.
		if channelID, ok := app.ConsumeNotificationClick(); ok {
			selectNotificationChannel(app, channelID)
		}
	}
}

func selectNotificationChannel(app *App, channelID string) {
	if !app.SelectChannel(channelID) {
		log.Printf("notification click: unknown channel %q", channelID)
	}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func (w *Window) content(c *ui.Context, t mui.Tokens) {
	ui.Column(c).Grow(1).MinWidth(0).Background(t.App).Children(func() {
		if !w.app.SettingsOpen {
			w.contentStrip(c, t)
		}
		if w.app.SettingsOpen {
			w.settings(c, t)
			return
		}
		if w.app.settingsSnapshot().APIKey == "" {
			w.emptyState(c, t, "Set up YouTube Reader", "Add a YouTube Data API key in Settings to start following channels.", "setup-settings", "Open Settings", func(*ui.Context) { w.app.SettingsOpen = true })
			return
		}
		if len(w.app.Sync.Channels) == 0 {
			w.emptyState(c, t, "Import your channels", "Choose an OPML file from your Mac to import channel groups.", "setup-import", "Import OPML", w.importOPML)
			return
		}
		w.gridArea(c, t)
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
