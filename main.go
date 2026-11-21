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
	defaultHeaderHeight float32 = 128

	defaultSidebarWidth float32 = 260
	defaultDetailsWidth float32 = 320

	minSidebarWidth float32 = 220
	maxSidebarWidth float32 = 320
	minDetailsWidth float32 = 280
	maxDetailsWidth float32 = 560
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
			Width: 1280, Height: 840, MinWidth: 960, MinHeight: 640,
			Content: ui.View(window.view),
		})
		window.window = win
		// Zone A owns the app's full menu bar; until it lands, this puts
		// Settings… and its Command-Comma accelerator in the App menu.
		window.installSettingsCommand()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

type Window struct {
	app                *App
	window             *mygo.Window
	grid               ui.GridState
	selectedIndex      *int
	sidebarFocus       string // sidebar row that takes the focus in the next frame
	sidebarKeyboard    string // sidebar row that arrow navigation focused
	sidebarWidth       float32
	contentWidth       float32
	detailsWidth       float32
	gridAvailableWidth float32
	// settingsWindow is the standalone Settings window and its view state.
	settingsWindow settingsWindow
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
						w.postNotification(n)
					}
				}
			})
		} else {
			w.app.Busy = false
		}
	}()
}

// postNotification shows one local notification and reports what the system
// said, so a refusal can be recorded for the Notifications pane.
func postNotification(plan NotificationPlan) error {
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
		return err
	}
	return nil
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
		w.header(c, t)
		if w.app.settingsSnapshot().APIKey == "" {
			w.emptyState(c, t, "Set up YouTube Reader", "Add a YouTube Data API key in Settings to start following channels.", "setup-settings", "Open Settings", func(*ui.Context) { w.openSettingsWindow() })
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
