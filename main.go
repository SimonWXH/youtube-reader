package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/netimage"
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

	// The RSS workspace's three columns: the article list keeps a stored width
	// and the reader takes the rest, so the list has room to read in while the
	// reader still never falls below rssReaderMinWidth.
	defaultRSSListWidth float32 = 400
	minRSSListWidth     float32 = 260
	maxRSSListWidth     float32 = 480
	rssReaderMinWidth   float32 = 320

	defaultMaxGridColumns = 5
	minMaxGridColumns     = 2
	maxMaxGridColumns     = 8
)

func hasNotifyOnlyFlag(args []string) bool {
	for _, arg := range args {
		if arg == "--notify-only" || arg == "-notify-only" {
			return true
		}
	}
	return false
}

var consumePendingNotificationsForApp = core.ConsumePendingNotifications

var runNotifyOnlyLoop = func(fn func()) error {
	mygo.App.WhenReady(func() {
		defer mygo.App.Quit()
		fn()
	})
	return mygo.App.Run()
}

func deliverPendingNotifications(app *App) int {
	plans, err := consumePendingNotificationsForApp(app.Dir)
	if err != nil {
		log.Printf("pending notifications: %v", err)
		return 0
	}
	for _, plan := range plans {
		app.postNotification(plan)
	}
	return len(plans)
}

func runNotifyOnly(app *App) {
	err := runNotifyOnlyLoop(func() {
		deliverPendingNotifications(app)
	})
	if err != nil {
		log.Fatal(err)
	}
}

func realMain() {
	// A picture whose server accepts the connection and then stalls has to end
	// in the reader's failed-image placeholder, not hold the frame forever, so
	// bound every image download netimage makes rather than wait on it.
	netimage.SetClient(&http.Client{Timeout: 15 * time.Second})
	app, err := LoadApp()
	if err != nil {
		log.Fatal(err)
	}
	defer app.Close()

	if hasNotifyOnlyFlag(os.Args[1:]) {
		runNotifyOnly(app)
		return
	}

	if err := app.ConsumeHandoff(); err != nil {
		log.Printf("handoff: %v", err)
	}
	selectedIndex := -1
	window := &Window{app: app, selectedIndex: &selectedIndex}
	// Register before Run so MyGo can deliver a click from an earlier run.
	registerNotificationClick(window)
	// The socket is up before the first frame so a helper run that starts
	// while the app is launching still hands its notifications over.
	go serveNotificationDeliveries(app, window)
	if err := app.DrainQueuedSyncWrites(); err != nil {
		log.Printf("sync queue: %v", err)
	}
	mygo.App.WhenReady(func() {
		// The saved frame comes from local settings, and the displays it is
		// checked against exist only once the app is ready.
		options := mainWindowOptions(window.app.settingsSnapshot(), mainWindowDisplays())
		options.Content = ui.View(window.shell)
		win := mygo.NewWindow(options)
		window.window = win
		window.watchMainWindowFrame()
		// The menu bar carries Settings… at Command-Comma and mirrors every
		// toolbar command, so the toolbar is never the only way to act.
		window.installMenu()
		// The status surface lives in the menu bar: one item over the window
		// and one popover rather than status copy inside the window.
		window.status = newStatusBar(window)
		deliverPendingNotifications(app)
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

	// pendingToasts are messages raised while no frame is building; the next
	// frame shows them, since AddToast only works while building.
	pendingToasts []ui.Toast

	// sidebarHidden folds the sidebar pane away from the toolbar's leading
	// control or the View menu; the split layout reads it in detail.go.
	sidebarHidden bool
	// activeWorkspace is the currently displayed workspace.
	activeWorkspace string
	// workspaceStates holds the isolated state for each workspace.
	workspaceStates map[string]*WorkspaceState
	// menuRefresh and menuSidebar are the system menu's mirror of the toolbar.
	menuRefresh *mygo.MenuItem
	menuSidebar *mygo.MenuItem

	// offline is macOS's network answer, kept by the status surface: the
	// content pane and the refresh control read it, so no surface asks the
	// network twice or disagrees with the menu bar item. False means online.
	offline bool
	// cancelRefresh ends the refresh in flight, when the pane that has
	// nothing cached offers Cancel. Nil when no refresh is running.
	cancelRefresh context.CancelFunc

	grid               ui.GridState
	gridKeyboardFocus  bool
	rssRevealSelection string // article ID a keyboard move just selected
	selectedIndex      *int
	sidebarFocus       string // sidebar row that takes the focus in the next frame
	sidebarKeyboard    string // sidebar row that arrow navigation focused
	sidebarWidth       float32
	sidebarDragging    bool
	sidebarOvershoot   float32
	channelDraggingID  string
	contentWidth       float32
	gridAvailableWidth float32
	detail             detailState
	// rssLayout is the RSS workspace's three-column split and rssReader the
	// reading view's scroll and paragraph-tick state.
	rssLayout rssLayoutState
	rssReader rssReaderState
	// rssType is the reading view's type scale, the body size control's
	// choice restored from local settings.
	rssType rssTypeState
	// rssRefetchingID is the article whose full-text re-fetch is in flight,
	// empty when none is: the reader's Refetch action shows progress and
	// refuses a second request while it is set.
	rssRefetchingID string
	// now is the window's clock, the wall clock in the app and a fixed moment
	// in tests, for header copy that depends on today's date.
	now func() time.Time
	// groupDragOrder is the group order before the drag that is moving one of
	// them began, so giving that drag up puts the groups back as they were.
	// nil when no group drag has moved the list.
	groupDragOrder    []string
	rssGroupDragOrder []string

	// renamingGroupID is the ID of the group currently being inline-renamed in the sidebar.
	renamingGroupID   string
	renamingGroupName string
	// renamingGroupNew is the ID of the group the plus menu just created, so
	// giving the inline rename up removes the group again instead of leaving an
	// empty one behind.
	renamingGroupNew string

	// sidebarMenuOpen is the plus menu's open flag. The button owns it so its
	// pressed face stays painted while the menu shows, and a test can read it.
	sidebarMenuOpen bool
	// addDialog is the in-window add-subscription panel.
	addDialog addDialog
	// onAddedChannel, when set, takes the fetch of the one channel an add
	// starts. The app leaves it nil and fetches in the background; a test
	// records the request instead of starting a lookup that outlives it.
	onAddedChannel func(channel core.Channel)
	// onRSSArticleSelected, when set, is invoked when an RSS article is selected.
	// The app leaves it nil; tests or observers record selection dispatches.
	onRSSArticleSelected func(article core.RSSArticle)
	// onRefetchRSSArticle, when set, takes the reader's Refetch action instead
	// of the app's background fetch. The app leaves it nil; tests record the
	// request without touching the network.
	onRefetchRSSArticle func(ctx context.Context, articleID string) (core.RSSArticle, error)

	// RSS inline renaming state:
	renamingRSSGroupID   string
	renamingRSSGroupName string
	renamingRSSGroupNew  string
	renamingRSSFeedID    string
	renamingRSSFeedName  string
	rssSidebarMenuOpen   bool
	addRSSDialog         addRSSDialog

	// settingsWindow is the standalone Settings window and its view state.
	settingsWindow settingsWindow

	// frameMemory keeps the main window's size and position in local settings,
	// so the next launch opens where this one was left. Nil until the window
	// exists.
	frameMemory *mainWindowFrameMemory
}

func (w *Window) startRenameGroup(id, name string) {
	if w.renamingGroupID != "" && w.renamingGroupID != id {
		w.commitRenameGroup(w.renamingGroupID, w.renamingGroupName)
	}
	w.renamingGroupID = id
	w.renamingGroupName = name
}

func (w *Window) commitRenameGroup(id, newName string) {
	created := w.renamingGroupNew == id
	w.renamingGroupID = ""
	w.renamingGroupName = ""
	w.renamingGroupNew = ""
	newName = strings.TrimSpace(newName)
	if newName == "" {
		if created {
			// A group the menu created and the reader left unnamed is no
			// group at all: remove it rather than keeping a placeholder.
			if err := w.app.DeleteGroup(id); err != nil {
				log.Printf("delete group: %v", err)
			}
		}
		return
	}
	if err := w.app.RenameGroup(id, newName); err != nil {
		log.Printf("rename group: %v", err)
	}
}

func (w *Window) cancelRenameGroup() {
	if id := w.renamingGroupNew; id != "" {
		if err := w.app.DeleteGroup(id); err != nil {
			log.Printf("delete group: %v", err)
		}
	}
	w.renamingGroupID = ""
	w.renamingGroupName = ""
	w.renamingGroupNew = ""
}

func (w *Window) channelOnlyInGroup(channelID, groupID string) bool {
	return w.app.channelOnlyInGroup(channelID, groupID)
}

func (w *Window) channelIsUngrouped(channelID string) bool {
	return w.app.channelIsUngrouped(channelID)
}

func (w *Window) moveChannelToGroup(c *ui.Context, channelID, targetGroupID string) {
	moved, err := w.app.MoveChannelToGroup(channelID, targetGroupID)
	if err != nil {
		log.Printf("move channel to group: %v", err)
		return
	}
	if moved {
		if targetGroupID != "" {
			_ = w.app.SetGroupCollapsed(targetGroupID, false)
		}
		if c != nil {
			c.Invalidate()
		}
	}
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
// starts and returns. The window's own refresh faces gate on offline through
// refreshDisabledReason; the status popover's Retry stays available and reaches
// this path so an offline reader still has a working next action.
func (w *Window) runRefresh(force bool) {
	if w.app.Busy || w.app.refreshBlocked(time.Now()) {
		return
	}
	// The refresh runs under a context the content pane can cancel while it
	// shows the initial-load skeleton, which is the only refresh with nothing
	// on screen to lose.
	ctx, cancel := context.WithCancel(context.Background())
	w.cancelRefresh = cancel
	w.app.Busy = true
	w.syncRefreshMenu()
	if w.window != nil {
		w.window.Invalidate()
	}
	if w.status != nil {
		w.status.started = time.Now()
		w.status.refresh()
		w.status.invalidate()
	}
	snapshot := w.app.refreshSnapshot()
	go func() {
		outcome, cancelled := w.app.RefreshContext(ctx, force, snapshot)
		cancel()
		finish := func() {
			w.cancelRefresh = nil
			// A cancelled attempt has no result to apply: the reader ended it,
			// and there is no failure or new content to report.
			if cancelled {
				w.app.Busy = false
				w.syncRefreshMenu()
				if w.status != nil {
					w.status.started = time.Time{}
					w.status.refresh()
					w.status.invalidate()
				}
				return
			}
			w.refreshFinished(outcome, snapshot)
		}
		if w.window != nil {
			w.window.Update(finish)
		} else {
			finish()
		}
	}()
}

// refreshFinished returns the window and the status item to idle and applies
// what the refresh brought: channel metadata reaches the sync state, and the
// views reload. Every completion path, with or without a window, ends here.
func (w *Window) refreshFinished(outcome RefreshOutcome, snapshot refreshSnapshot) {
	w.app.Busy = false
	w.syncRefreshMenu()
	if w.status != nil {
		w.status.started = time.Time{}
		w.status.refresh()
		w.status.invalidate()
	}
	if outcome.Changed {
		w.app.applyRefreshedChannels(outcome.Channels, snapshot.state.Channels)
		for _, n := range outcome.Notified {
			w.postNotification(n)
		}
		_ = w.app.reload()
		w.refreshSettingsWindow()
	}
}

// cancelRefreshRun ends the refresh in flight from the pane that has nothing
// cached to keep. It only cancels: the returning goroutine clears Busy and
// rebuilds the frame once the attempt has stopped.
func (w *Window) cancelRefreshRun() {
	if w.cancelRefresh != nil {
		w.cancelRefresh()
	}
}

// showNotificationForApp displays one notification through the app's native
// notification center. It is a seam so the permission bookkeeping can be
// tested without a packaged app to notify from.
var showNotificationForApp = showNotification

// showNotification displays one local notification and reports what the system
// said, so a refusal can be recorded for the Notifications pane.
func showNotification(plan NotificationPlan) error {
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

func registerNotificationClick(w *Window) (off func()) {
	return mygo.App.OnNotificationClick(notificationClickHandler(w))
}

func notificationClickHandler(w *Window) func(string) {
	return func(id string) {
		// Every notification YouTube Reader shows carries its channel's ID, so
		// a click is complete on its own, also when it launched the app.
		if channelID, ok := core.ChannelFromNotificationID(id); ok {
			selectNotificationChannel(w, channelID)
		}
	}
}

// serveNotificationDeliveries shows the notifications the background helper
// hands the running app when it is active. The listener lives as long as the process.
func serveNotificationDeliveries(app *App, w *Window) {
	show := func(plans []core.NotificationPlan) {
		for _, plan := range plans {
			// A refused permission is recorded in settings by the poster
			// itself, so the Notifications pane can offer System Settings.
			app.postNotification(plan)
		}
		w.helperRefreshed()
	}
	if err := core.ServeNotifications(context.Background(), app.Dir, show); err != nil {
		log.Printf("notification socket: %v", err)
	}
}

// helperRefreshed reads the refresh the helper committed to the shared store
// while this app was running, so a click on its notification opens a channel
// view that already holds the videos it announced. MyGo runs it on the main
// thread once the app is ready, so a hand-off that lands while the app is still
// launching is read rather than dropped, and the window the socket goroutine
// reads is the one the main thread installed.
func (w *Window) helperRefreshed() {
	mygo.App.WhenReady(func() {
		w.reloadHelperRefresh()
		if w.window != nil {
			w.window.Invalidate()
		}
	})
}

// reloadHelperRefresh is helperRefreshed's main-thread half: it reads the
// store the helper committed to and repaints everything that reads it.
func (w *Window) reloadHelperRefresh() {
	if err := w.app.reload(); err != nil {
		log.Printf("helper refresh: %v", err)
		return
	}
	w.refreshSettingsWindow()
}

func selectNotificationChannel(w *Window, channelID string) {
	w.selectWorkspace(WorkspaceVideos)
	if !w.app.SelectChannel(channelID) {
		log.Printf("notification click: unknown channel %q", channelID)
	}
}

// navigateToChannel routes the main window to a channel's view, expanding its
// owning group if collapsed, clearing search text, and closing the detail pane.
func (w *Window) navigateToChannel(channelID string) {
	if channelID == "" {
		return
	}
	if w.renamingGroupID != "" {
		w.commitRenameGroup(w.renamingGroupID, w.renamingGroupName)
	}
	if w.detail.errID != "" {
		w.detail.errDismissed = true
	}
	if w.app != nil {
		w.selectWorkspace(WorkspaceVideos)
		w.app.SelectChannel(channelID)
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
		w.contentStrip(c, t)
		in := w.contentPaneInput()
		switch mui.ContentPaneState(in) {
		case mui.PaneLoading:
			w.loadingPane(c, t)
		case mui.PaneOffline:
			mui.OfflinePane(c, t, mui.OfflineSpec{Key: "offline-pane"})
		case mui.PaneError:
			w.errorPane(c, t, in)
		case mui.PaneEmpty:
			w.emptyPane(c, t, in)
		default:
			w.gridArea(c, t)
		}
	})
}

// contentPaneInput reads the content pane's state from the app. One function
// builds the input for the pane and its strip, so the empty state, the
// skeleton, and the toolbar's refresh control never disagree.
func (w *Window) contentPaneInput() mui.PaneInput {
	settings := w.app.settingsSnapshot()
	counts := w.sidebarCounts()
	in := mui.PaneInput{
		HasAPIKey:         settings.APIKey != "",
		ChannelCount:      len(w.app.Sync.Channels),
		LibraryVideos:     counts.total,
		DestinationVideos: w.destinationCount(counts),
		VisibleVideos:     len(w.app.VisibleVideos()),
		Refreshing:        w.app.Busy,
		Online:            !w.offline,
		Selected:          w.app.Selected,
		Search:            w.app.Search,
		ChannelFilter:     w.app.ChannelFilter,
		UnreadOnly:        w.app.UnreadOnly,
	}
	if len(settings.RecentFailures) > 0 {
		in.Failure = settings.RecentFailures[0].Message
	}
	return in
}

// destinationCount is how many videos the selected destination holds before
// the filter bar, the same count its sidebar row shows. It is what tells an
// empty destination apart from one the filters hide, and what keeps Favorites
// from drawing the initial-load skeleton while the library refreshes.
func (w *Window) destinationCount(counts sidebarCounts) int {
	switch {
	case w.app.Selected == "favorites":
		return counts.favorites
	case w.app.Selected == "all":
		return counts.total
	case destinationID(w.app.Selected, "channel:") != "":
		return counts.perChannel[destinationID(w.app.Selected, "channel:")]
	case destinationID(w.app.Selected, "group:") != "":
		gid := destinationID(w.app.Selected, "group:")
		if g, ok := w.app.Sync.Groups[gid]; ok {
			return counts.group(g.ChannelIDs)
		}
		for _, g := range w.app.Groups {
			if g.ID == gid {
				return counts.group(g.ChannelIDs)
			}
		}
	}
	return counts.total
}

// destinationID returns the id a destination names after prefix, or empty.
func destinationID(selected, prefix string) string {
	if len(selected) > len(prefix) && selected[:len(prefix)] == prefix {
		return selected[len(prefix):]
	}
	return ""
}

// emptyPane draws the pane's empty state for its destination. The action is
// the one step that moves the reader forward: settings, the importer, a
// cleared filter bar, or a refresh, and the refresh face stays visible and
// disabled while it cannot run.
func (w *Window) emptyPane(c *ui.Context, t mui.Tokens, in mui.PaneInput) {
	// An empty library with a destination to show clears a selection that is no
	// longer visible. Setup and import leave it alone: they are not a filter
	// hiding the reader's selection.
	if in.HasAPIKey && in.ChannelCount > 0 {
		if w.app.SelectedVideoID != "" {
			w.app.SelectedVideoID = ""
		}
		none := -1
		w.selectedIndex = &none
	}

	variant := mui.EmptyVariantFor(in)
	spec := mui.EmptySpec{Key: "empty-pane", Copy: mui.CopyForEmpty(variant)}
	switch variant {
	case mui.EmptySetup:
		spec.OnAction = func() { w.openSettingsWindow() }
	case mui.EmptyImportChannels:
		spec.OnAction = func() { w.importOPML(c) }
	case mui.EmptyFiltered:
		spec.OnAction = func() {
			w.app.Search = ""
			w.app.ChannelFilter = ""
			w.app.UnreadOnly = false
		}
	case mui.EmptyFavorites:
		spec.OnAction = func() { w.app.SelectDestination("all") }
	case mui.EmptyChannel:
		w.emptyChannelPane(c, in, &spec)
	default:
		spec.OnAction = func() { w.runRefresh(true) }
		spec.ActionDisabled, spec.ActionTooltip = w.refreshDisabledReason()
	}
	mui.EmptyPane(c, t, spec)
}

// emptyChannelPane turns the empty channel pane into the one step that fills it.
// A refresh reads the last 30 days of uploads only, so a channel that has not
// posted in that window is empty however often it is refreshed: the pane offers
// the channel's history instead, and names the end of it once that is loaded.
func (w *Window) emptyChannelPane(c *ui.Context, in mui.PaneInput, spec *mui.EmptySpec) {
	channelID := destinationID(in.Selected, "channel:")
	cursor := w.app.BackfillCursors[channelID]
	spec.Copy = mui.ChannelEmptyCopy(c.Locale(), cursor.Complete())
	spec.Reason = w.app.BackfillErr
	if cursor.Complete() {
		return
	}
	if w.app.BackfillBusy == channelID {
		// The reader's click is in flight: the pane's one action says so and
		// cannot be pressed again.
		spec.Copy.Action = mui.ChannelHistoryCopy(c.Locale()).Loading
	}
	spec.OnAction = func() { w.startBackfill(channelID) }
	spec.ActionDisabled, spec.ActionTooltip = w.historyDisabledReason(c.Locale())
}

// loadingPane draws the first load. The cards keep the real grid's geometry.
// The Cancel that ends it sits in the strip above, which is this pane's header,
// so the skeleton itself carries no control and no spinner.
func (w *Window) loadingPane(c *ui.Context, t mui.Tokens) {
	gap := c.Theme().Space(2)
	padding := mui.Space(4)
	metrics := gridCardMetrics(w.gridContentWidth(padding)-gridScrollbarReserve, gap, w.app.settingsSnapshot().MaxGridColumns)
	ui.Column(c).Grow(1).Padding(padding).AlignItems(ui.Start).Children(func() {
		mui.SkeletonGrid(c, t, mui.SkeletonSpec{
			Count:       mui.SkeletonCards,
			Columns:     metrics.Columns,
			CardWidth:   metrics.Width,
			CardHeight:  metrics.Height,
			ThumbHeight: metrics.Width * gridThumbAspect,
			Gap:         gap,
		})
	})
}

// errorPane draws the last refresh's failure inside the pane. It appears only
// when the pane has nothing cached to keep; with content on screen the status
// surface carries the failure, so it is never reported twice.
func (w *Window) errorPane(c *ui.Context, t mui.Tokens, in mui.PaneInput) {
	settings := w.app.settingsSnapshot()
	message := ""
	if len(settings.RecentFailures) > 0 {
		message = settings.RecentFailures[0].Message
	}
	reason := statusFailureText(message)
	if strings.HasPrefix(message, "quota paused") {
		reason = ""
	}
	disabled, tooltip := w.refreshDisabledReason()
	cause := refreshErrorCause(message)
	if cause == mui.CauseQuota && !w.app.refreshBlocked(time.Now()) {
		cause = mui.CauseQuotaExpired
	}
	mui.ErrorPane(c, t, mui.ErrorSpec{
		Key:            "pane-error",
		Cause:          cause,
		Reason:         reason,
		ActionDisabled: disabled,
		ActionTooltip:  tooltip,
		OnRetry:        func() { w.runRefresh(true) },
	})
}

// refreshErrorCause tells the error copy why the refresh failed, using the
// same reading of the stored message as the status surface. The offline state
// is never an error cause: the pane shows its own offline state whenever the
// library is empty and offline, so an error pane is always online.
func refreshErrorCause(message string) mui.ErrorCause {
	switch {
	case rejectedKey(message):
		return mui.CauseRejectedKey
	case strings.HasPrefix(message, "quota paused"):
		return mui.CauseQuota
	default:
		return mui.CauseConnection
	}
}

// refreshDisabledReason says whether a refresh action cannot run and why. The
// offline reason is the tooltip the board specifies; the others keep the face
// disabled without inventing a short explanation the status surface already
// gives.
func (w *Window) refreshDisabledReason() (bool, string) {
	switch {
	case w.offline:
		return true, mui.OfflineTooltip
	case w.app.Busy:
		return true, "Refreshing"
	case w.app.refreshBlocked(time.Now()):
		return true, ""
	}
	return false, ""
}
