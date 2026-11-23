package main

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
)

// settingsTestApp is the smallest app that renders every pane of the Settings
// window: a saved API key, one path pair, two channels, and one favorite.
func settingsTestApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	home := t.TempDir()
	previousHomeDir := helperHomeDir
	helperHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { helperHomeDir = previousHomeDir })
	a := &App{
		Dir:      dir,
		Selected: "all",
		Settings: core.Settings{
			APIKey:         "A" + "I" + "z" + "a" + "SyA" + strings.Repeat("1", 32),
			RefreshMinutes: defaultRefreshMinutes,
			SyncPath:       filepath.Join(dir, "sync.json"),
			BackupPath:     filepath.Join(dir, "favorites-backup.json"),
			MaxGridColumns: defaultMaxGridColumns,
			NotificationIDs: []string{
				"ch-a",
			},
		},
		Sync: core.SyncState{
			SchemaVersion: 1,
			DeviceID:      "device-test",
			Channels:      map[string]core.Channel{"ch-a": {ID: "ch-a", Title: "Alpha"}, "ch-b": {ID: "ch-b", Title: "Bravo"}},
			ReadStates:    map[string]map[string]core.ReadRecord{},
			Favorites:     map[string]core.FavoriteRecord{"video-1": {VideoID: "video-1", UpdatedAt: 1}},
			Groups:        map[string]core.ChannelGroup{},
		},
		Channels: []core.Channel{{ID: "ch-a", Title: "Alpha"}, {ID: "ch-b", Title: "Bravo"}},
	}
	return a
}

// settingsTester renders one pane of the Settings window at the board's
// 520 x 460.
func settingsTester(t *testing.T, a *App, pane settingsPane) (*Window, *ui.Tester) {
	t.Helper()
	w := &Window{app: a, selectedIndex: new(int)}
	w.settingsWindow.pane = pane
	tt := ui.NewTester(w.settingsView, int(settingsWindowWidth), int(settingsWindowHeight))
	tt.Frame()
	return w, tt
}

// loadSettingsFile reads settings.json as a fresh launch would.
func loadSettingsFile(t *testing.T, dir string) core.Settings {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}
	var settings core.Settings
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("parse settings.json: %v", err)
	}
	return settings
}

// clickField puts the caret in the text field with the given accessible name.
func clickField(t *testing.T, tt *ui.Tester, name string) ui.AccessInfo {
	t.Helper()
	info, ok := tt.AccessByRole(ui.RoleTextField, name)
	if !ok {
		t.Fatalf("no text field named %q", name)
	}
	tt.ClickAt(info.Bounds.X+info.Bounds.W/2, info.Bounds.Y+info.Bounds.H/2)
	return info
}

// TestSettingsPathsPersistOnEdit is the regression guard for the screen's
// past defect, where the sync path could not be edited: a path field must take
// the edit, keep showing it after the next frame, and have it in settings.json
// for the next launch.
func TestSettingsPathsPersistOnEdit(t *testing.T) {
	for _, pathField := range []string{"Sync file", "Backup file"} {
		t.Run(pathField, func(t *testing.T) {
			a := settingsTestApp(t)
			dir := a.Dir
			_, tt := settingsTester(t, a, settingsBackups)

			edited := filepath.Join(dir, "edited", strings.ToLower(strings.ReplaceAll(pathField, " ", "-")), "data.json")
			clickField(t, tt, pathField)
			tt.Command("selectAll")
			tt.Type(edited)
			tt.Frame()

			info, ok := tt.AccessByRole(ui.RoleTextField, pathField)
			if !ok {
				t.Fatalf("%s field disappeared", pathField)
			}
			if info.Value != edited {
				t.Fatalf("%s field shows %q after the edit, want %q", pathField, info.Value, edited)
			}
			settings := loadSettingsFile(t, dir)
			got := settings.SyncPath
			if pathField == "Backup file" {
				got = settings.BackupPath
			}
			if got != edited {
				t.Fatalf("%s saved as %q, want %q", pathField, got, edited)
			}
			// A second frame must not fall back to the value loaded at open.
			tt.Frame()
			if info, ok := tt.AccessByRole(ui.RoleTextField, pathField); !ok || info.Value != edited {
				t.Fatalf("%s lost the edit on the next frame: %+v", pathField, info)
			}
		})
	}
}

// TestSettingsPathsSurviveReopen reloads what a relaunch would read: both
// edited paths come back from settings.json.
func TestSettingsPathsSurviveReopen(t *testing.T) {
	a := settingsTestApp(t)
	_, tt := settingsTester(t, a, settingsBackups)
	for _, pathField := range []string{"Sync file", "Backup file"} {
		edited := filepath.Join(a.Dir, "reopened-"+strings.ReplaceAll(strings.ToLower(pathField), " ", "-")+".json")
		clickField(t, tt, pathField)
		tt.Command("selectAll")
		tt.Type(edited)
		tt.Frame()
	}
	reloaded := loadSettingsFile(t, a.Dir)
	if !strings.Contains(reloaded.SyncPath, "reopened-sync-file.json") {
		t.Fatalf("sync path not reloaded: %q", reloaded.SyncPath)
	}
	if !strings.Contains(reloaded.BackupPath, "reopened-backup-file.json") {
		t.Fatalf("backup path not reloaded: %q", reloaded.BackupPath)
	}
}

// TestSettingsAPIKeyValidatesOnBlur keeps the board's rule: the message comes
// after the field is left, and the key itself never shows unless revealed.
func TestSettingsAPIKeyValidatesOnBlur(t *testing.T) {
	a := settingsTestApp(t)
	validKey := a.Settings.APIKey
	_, tt := settingsTester(t, a, settingsGeneral)
	clickField(t, tt, "API key")
	tt.Command("selectAll")
	tt.Type("not-a-key")
	tt.Frame()
	if tt.HasText("That is not a YouTube Data API key.") {
		t.Fatal("the key was validated while it was still being typed")
	}
	tt.Key(0, ui.KeyTab)
	tt.Frame()
	if !tt.HasText("That is not a YouTube Data API key.") {
		t.Fatal("blurring an invalid key showed no message")
	}
	if settings := loadSettingsFile(t, a.Dir); settings.APIKey != "not-a-key" {
		t.Fatalf("api key saved as %q", settings.APIKey)
	}
	if info, ok := tt.AccessByRole(ui.RoleTextField, "API key"); !ok || !info.Password {
		t.Fatal("the key field is not masked after the edit")
	}
	// A valid key clears the message once the field is left.
	clickField(t, tt, "API key")
	tt.Command("selectAll")
	tt.Type(validKey)
	tt.Frame()
	tt.Key(0, ui.KeyTab)
	tt.Frame()
	if tt.HasText("That is not a YouTube Data API key.") {
		t.Fatal("a valid key still shows the invalid message")
	}
	if !tt.HasText("Stored on this Mac only. Never synced.") {
		t.Fatal("a valid key does not show the neutral helper after blur")
	}
	if info, ok := tt.AccessByRole(ui.RoleTextField, "API key"); !ok || !info.Password {
		t.Fatal("the API key field is not masked")
	}
}

// TestSettingsEmptyAPIKeyStaysNeutral keeps a fresh install calm: an empty
// field shows the neutral helper, while a stored key that is wrong still
// reports itself as soon as the pane opens.
func TestSettingsEmptyAPIKeyStaysNeutral(t *testing.T) {
	a := settingsTestApp(t)
	a.Settings.APIKey = ""
	_, tt := settingsTester(t, a, settingsGeneral)
	if !tt.HasText("Stored on this Mac only. Never synced.") {
		t.Fatal("an empty API key does not show the neutral helper")
	}
	if tt.HasText("That is not a YouTube Data API key.") {
		t.Fatal("an untouched empty API key shows the invalid-key error")
	}
	a.Settings.APIKey = "not-a-key"
	_, tt = settingsTester(t, a, settingsGeneral)
	if !tt.HasText("That is not a YouTube Data API key.") {
		t.Fatal("a stored invalid key shows no error")
	}
}

// TestSettingsAPIKeySavesTrimmed stores exactly what validation accepts: a key
// pasted with surrounding spaces is saved without them and shown trimmed.
func TestSettingsAPIKeySavesTrimmed(t *testing.T) {
	a := settingsTestApp(t)
	key := a.Settings.APIKey
	a.Settings.APIKey = ""
	_, tt := settingsTester(t, a, settingsGeneral)
	clickField(t, tt, "API key")
	tt.SetClipboard("  " + key + "  ")
	tt.Command("selectAll")
	tt.Command("paste")
	tt.Frame()
	if a.Settings.APIKey != key {
		t.Fatalf("api key kept %q, want %q", a.Settings.APIKey, key)
	}
	if settings := loadSettingsFile(t, a.Dir); settings.APIKey != key {
		t.Fatalf("api key saved as %q, want %q", settings.APIKey, key)
	}
	if err := tt.Click("Reveal API key"); err != nil {
		t.Fatalf("revealing the key: %v", err)
	}
	tt.Frame()
	field, ok := tt.AccessByRole(ui.RoleTextField, "API key")
	if !ok || field.Value != key {
		t.Fatalf("api key field shows %q, want %q", field.Value, key)
	}
}

// TestSettingsAPIKeyRevealToggles flips the field between masked and plain,
// and only then shows the key.
func TestSettingsAPIKeyRevealToggles(t *testing.T) {
	a := settingsTestApp(t)
	key := a.Settings.APIKey
	_, tt := settingsTester(t, a, settingsGeneral)
	field, ok := tt.AccessByRole(ui.RoleTextField, "API key")
	if !ok || !field.Password {
		t.Fatal("the key did not start masked")
	}
	if err := tt.Click("Reveal API key"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	field, ok = tt.AccessByRole(ui.RoleTextField, "API key")
	if !ok || field.Password {
		t.Fatal("revealing the key left the field masked")
	}
	if field.Value != key {
		t.Fatalf("revealed field shows %q", field.Value)
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "Hide API key"); !ok {
		t.Fatal("the reveal button does not offer to hide the key again")
	}
}

// TestSettingsRefreshChoices drives the interval pop-up and checks it offers
// the board's five choices, in the board's order; what a choice saves is
// driven through the pop-up by the tests below it.
func TestSettingsRefreshChoices(t *testing.T) {
	a := settingsTestApp(t)
	_, tt := settingsTester(t, a, settingsGeneral)
	if err := tt.Click("15 min"); err != nil {
		t.Fatalf("opening the interval popup: %v", err)
	}
	tt.Frame()
	var got []string
	for _, info := range tt.AccessAll() {
		if info.Role == ui.RoleOption {
			got = append(got, info.Name)
		}
	}
	want := []string{"Manual", "5 min", "15 min", "30 min", "60 min"}
	if len(got) != len(want) {
		t.Fatalf("the popup offered %d choices %v, want %v", len(got), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("the popup offered %v, want %v", got, want)
		}
	}
}

// TestSettingsLegacyRefreshIntervalStaysOffered keeps a stored interval from an
// older build visible in the pop-up button and unchanged until the user picks
// another choice.
func TestSettingsLegacyRefreshIntervalStaysOffered(t *testing.T) {
	a := settingsTestApp(t)
	a.Settings.RefreshMinutes = 120
	_, tt := settingsTester(t, a, settingsGeneral)
	if _, ok := tt.AccessByRole(ui.RolePopUpButton, "120 min"); !ok {
		t.Fatal("the stored 120 min interval is not shown in the pop-up button")
	}
	if err := tt.Click("120 min"); err != nil {
		t.Fatalf("opening the interval popup: %v", err)
	}
	tt.Frame()
	item, ok := tt.AccessByRole(ui.RoleOption, "120 min")
	if !ok {
		t.Fatal("the stored 120 min interval cannot be chosen again")
	}
	tt.ClickAt(item.Bounds.X+item.Bounds.W/2, item.Bounds.Y+item.Bounds.H/2)
	tt.Frame()
	if a.Settings.RefreshMinutes != 120 {
		t.Fatalf("stored interval changed to %d without a different choice", a.Settings.RefreshMinutes)
	}
}

// TestSettingsRefreshPopupChoosesPersists drives the pop-up button and checks
// the choice reaches settings.json.
func TestSettingsRefreshPopupChoosesPersists(t *testing.T) {
	a := settingsTestApp(t)
	_, tt := settingsTester(t, a, settingsGeneral)
	if _, ok := tt.AccessByRole(ui.RolePopUpButton, "15 min"); !ok {
		t.Fatal("no refresh interval pop-up button")
	}
	if err := tt.Click("15 min"); err != nil {
		t.Fatalf("opening the interval popup: %v", err)
	}
	tt.Frame()
	if err := tt.Click("Manual"); err != nil {
		t.Fatalf("choosing Manual: %v", err)
	}
	tt.Frame()
	if settings := loadSettingsFile(t, a.Dir); settings.RefreshMinutes != 0 {
		t.Fatalf("refresh interval saved as %d, want 0", settings.RefreshMinutes)
	}
}

// TestSettingsTabOrderFollowsVisualOrder walks the pane with Tab and checks
// each focused control is at or below the one before it, as reading order. The
// pane toolbar is one stop, as a segmented control is: it holds the focus on
// the chosen segment when the window opens, the arrows move within it, and Tab
// leaves it for the form.
func TestSettingsTabOrderFollowsVisualOrder(t *testing.T) {
	a := settingsTestApp(t)
	_, tt := settingsTester(t, a, settingsGeneral)
	seen := map[string]bool{}
	lastY := float32(-1)
	observe := func(step string) {
		t.Helper()
		focused, ok := focusedElement(tt)
		if !ok {
			t.Fatalf("%s focused nothing", step)
		}
		name := focused.Name
		if focused.Role == ui.RoleRadio {
			name = "segment " + name
		}
		if seen[name] {
			return
		}
		seen[name] = true
		if focused.Bounds.Y < lastY {
			t.Fatalf("%q at %v comes after a control at %v", name, focused.Bounds.Y, lastY)
		}
		lastY = focused.Bounds.Y
	}
	observe("opening Settings")
	for i := 0; i < 16; i++ {
		tt.Key(0, ui.KeyTab)
		tt.Frame()
		observe("Tab step " + strconv.Itoa(i))
	}
	for _, want := range []string{"segment General", "API key", "Reveal API key", "15 min", "Show Shorts", "Decrease", "Increase", "Install helper…"} {
		if !seen[want] {
			t.Errorf("Tab never reached %q", want)
		}
	}
}

// TestSettingsManualOffersNoHelper keeps Manual from installing a login item
// that would refresh without the user asking.
func TestSettingsManualOffersNoHelper(t *testing.T) {
	a := settingsTestApp(t)
	a.Settings.RefreshMinutes = 0
	_, tt := settingsTester(t, a, settingsGeneral)
	if _, ok := tt.AccessByRole(ui.RoleButton, "Install helper…"); ok {
		t.Fatal("Manual still offers to install the background helper")
	}
	if !tt.HasText("Choose a refresh interval above to run the helper while the app is closed.") {
		t.Fatal("the pane does not explain why the helper is unavailable")
	}
}

// TestSettingsPaneToolbarArrowsSwitch keeps the pane toolbar reachable and
// usable from the keyboard alone: the chosen segment holds the focus when the
// window opens, its group is named, and the arrows switch panes straight away.
func TestSettingsPaneToolbarArrowsSwitch(t *testing.T) {
	a := settingsTestApp(t)
	w, tt := settingsTester(t, a, settingsGeneral)
	focused, ok := focusedElement(tt)
	if !ok || focused.Role != ui.RoleRadio || focused.Name != "General" || !focused.Checked {
		t.Fatalf("opening Settings focuses %+v, want the checked General segment", focused)
	}
	if _, ok := tt.AccessByRole(ui.RoleRadioGroup, "Settings panes"); !ok {
		t.Fatal("the pane switcher has no accessible name")
	}
	tt.Key(0, ui.KeyRight)
	tt.Frame()
	if w.settingsWindow.pane != settingsBackups {
		t.Fatalf("Right moved the pane to %v, want Backups", w.settingsWindow.pane)
	}
	if settings := loadSettingsFile(t, a.Dir); settings.LastSettingsPane != "Backups" {
		t.Fatalf("pane saved as %q", settings.LastSettingsPane)
	}
	focused, ok = focusedElement(tt)
	if !ok || focused.Role != ui.RoleRadio || focused.Name != "Backups" || !focused.Checked {
		t.Fatalf("after Right the focus is %+v, want the checked Backups segment", focused)
	}
}

// focusedElement returns what assistive technology has the focus on.
func focusedElement(tt *ui.Tester) (ui.AccessInfo, bool) {
	for _, info := range tt.AccessAll() {
		if info.Focused {
			return info, true
		}
	}
	return ui.AccessInfo{}, false
}

// TestMenuExportRecordsLastExport keeps the Backups pane's Last export line
// honest: an export chosen from the File menu writes the backup and records
// the same timestamp the pane's own button does.
func TestMenuExportRecordsLastExport(t *testing.T) {
	a := settingsTestApp(t)
	previous := mygo.App.Menu()
	t.Cleanup(func() { mygo.App.SetMenu(previous) })
	w := &Window{app: a}
	w.installMenu()

	file := menuItemWithLabel(mygo.App.Menu().Items(), "File")
	if file == nil {
		t.Fatal("no File menu")
	}
	export := menuItemWithLabel(file.Submenu, "Export Favorites…")
	if export == nil || export.Click == nil {
		t.Fatalf("File menu's Export Favorites… is %+v", export)
	}
	export.Click(export, nil)

	if _, err := os.Stat(a.Settings.BackupPath); err != nil {
		t.Fatalf("the File menu export wrote no backup: %v", err)
	}
	if saved := loadSettingsFile(t, a.Dir); saved.LastFavoritesExport == "" {
		t.Fatal("the File menu export recorded no Last export time")
	}
}

// TestSettingsMenuSettingsItemOpensWindow invokes both Settings… entries, the
// App menu's and the overflow menu's, and checks each opens the standalone
// window on the pane the last session left it on.
func TestSettingsMenuSettingsItemOpensWindow(t *testing.T) {
	for _, entry := range []struct {
		name string
		pane settingsPane
	}{
		{"App menu", settingsBackups},
		{"overflow menu", settingsNotifications},
	} {
		t.Run(entry.name, func(t *testing.T) {
			previousCreate := newSettingsWindow
			t.Cleanup(func() { newSettingsWindow = previousCreate })
			var created mygo.WindowOptions
			newSettingsWindow = func(opts mygo.WindowOptions) *mygo.Window {
				created = opts
				return &mygo.Window{}
			}

			if entry.name == "App menu" {
				a := settingsTestApp(t)
				a.Settings.LastSettingsPane = entry.pane.name()
				previousMenu := mygo.App.Menu()
				t.Cleanup(func() { mygo.App.SetMenu(previousMenu) })
				w := &Window{app: a}
				w.installMenu()

				menu := mygo.App.Menu()
				if menu == nil {
					t.Fatal("no application menu was installed")
				}
				var settingsItem *mygo.MenuItem
				for _, top := range menu.Items() {
					for _, item := range top.Submenu {
						if item.Label == "Settings…" {
							settingsItem = item
						}
					}
				}
				if settingsItem == nil {
					t.Fatal("the application menu has no Settings… item")
				}
				if settingsItem.Click == nil {
					t.Fatal("the application menu's Settings… has no action")
				}
				settingsItem.Click(settingsItem, nil)
				checkSettingsWindowOpened(t, w, created, entry.pane)
				return
			}

			w := chromeWindow(t)
			w.app.Settings.LastSettingsPane = entry.pane.name()
			tt := ui.NewTester(w.shell, 1280, 808)
			tt.Frame()
			if err := tt.Click("More"); err != nil {
				t.Fatalf("open the overflow menu: %v", err)
			}
			if err := tt.ChooseMenuItem("Settings…"); err != nil {
				t.Fatalf("choose the overflow menu's Settings…: %v", err)
			}
			checkSettingsWindowOpened(t, w, created, entry.pane)
		})
	}
}

// checkSettingsWindowOpened asserts the Settings window opened on pane with
// that pane's title.
func checkSettingsWindowOpened(t *testing.T, w *Window, opts mygo.WindowOptions, pane settingsPane) {
	t.Helper()
	if w.settingsWindow.win == nil {
		t.Fatal("Settings… opened no window")
	}
	if w.settingsWindow.pane != pane {
		t.Fatalf("opened on %v, want %v", w.settingsWindow.pane, pane)
	}
	if opts.Title != pane.title() {
		t.Fatalf("window title = %q, want %q", opts.Title, pane.title())
	}
}

// TestSettingsUnknownPaneRestoresGeneral keeps a stored pane name another
// build wrote, or a bad one, from opening on nothing. The panes themselves
// and their titles are driven by the switcher and window-options tests.
func TestSettingsUnknownPaneRestoresGeneral(t *testing.T) {
	if got := settingsPaneFromName("Nope"); got != settingsGeneral {
		t.Fatalf("unknown pane restored as %v, want General", got)
	}
}

// TestSettingsWindowOptions pins the board's window: 520 x 460, fixed size so
// minimize and zoom stay dimmed, with the view drawing the title bar.
func TestSettingsWindowOptions(t *testing.T) {
	opts := settingsWindowOptions(settingsBackups, func(*ui.Context) {})
	if opts.Width != 520 || opts.Height != 460 {
		t.Fatalf("window = %dx%d, want 520x460", opts.Width, opts.Height)
	}
	if !opts.DisableResize || !opts.DisableMinimize || !opts.DisableMaximize || !opts.DisableFullScreen {
		t.Fatalf("window controls are not dimmed: %+v", opts)
	}
	if opts.Title != "Backups" {
		t.Fatalf("window title = %q", opts.Title)
	}
	if opts.TitleBarStyle != mygo.TitleBarHiddenInset {
		t.Fatalf("title bar style = %q", opts.TitleBarStyle)
	}
	if opts.Content == nil {
		t.Fatal("window has no content")
	}
}

// TestSettingsSwitchPersistsNotificationConsent checks the switch writes the
// channel's consent and that it is off by default.
func TestSettingsSwitchPersistsNotificationConsent(t *testing.T) {
	a := settingsTestApp(t)
	_, tt := settingsTester(t, a, settingsNotifications)
	bravo, ok := tt.AccessByRole(ui.RoleSwitch, "Bravo")
	if !ok {
		t.Fatal("no switch for the second channel")
	}
	if bravo.Checked {
		t.Fatal("a channel the user never chose started on")
	}
	tt.ClickAt(bravo.Bounds.X+bravo.Bounds.W/2, bravo.Bounds.Y+bravo.Bounds.H/2)
	tt.Frame()
	settings := loadSettingsFile(t, a.Dir)
	if !containsID(settings.NotificationIDs, "ch-b") {
		t.Fatalf("consent for ch-b not saved: %v", settings.NotificationIDs)
	}
	if !containsID(settings.NotificationIDs, "ch-a") {
		t.Fatalf("the other channel's consent was lost: %v", settings.NotificationIDs)
	}
}

// TestEnableAllNotificationsIgnoresMainWindowSelection turns on every
// subscription even when the sidebar shows Favorites, and reports whether
// anything changed.
func TestEnableAllNotificationsIgnoresMainWindowSelection(t *testing.T) {
	a := settingsTestApp(t)
	a.Selected = "favorites"
	a.Settings.NotificationIDs = nil
	if !a.EnableAllNotifications() {
		t.Fatal("enabling all notifications reported no change")
	}
	settings := loadSettingsFile(t, a.Dir)
	for _, id := range []string{"ch-a", "ch-b"} {
		if !containsID(settings.NotificationIDs, id) {
			t.Fatalf("consent for %s not saved: %v", id, settings.NotificationIDs)
		}
	}
	if a.EnableAllNotifications() {
		t.Fatal("enabling all notifications twice reported a change")
	}
}

// TestSettingsNotificationsListImportedSubscriptions sources the list and
// Enable All from the sync state, so an import before the first refresh is
// visible and actionable.
func TestSettingsNotificationsListImportedSubscriptions(t *testing.T) {
	a := settingsTestApp(t)
	a.Channels = []core.Channel{{ID: "ch-a", Title: "Alpha"}}
	a.Sync.Channels["ch-imported"] = core.Channel{ID: "ch-imported", Title: "Imported"}
	_, tt := settingsTester(t, a, settingsNotifications)
	if _, ok := tt.AccessByRole(ui.RoleSwitch, "Imported"); !ok {
		t.Fatal("an imported subscription is missing from the pane")
	}
	if err := tt.Click("Enable All"); err != nil {
		t.Fatalf("Enable All: %v", err)
	}
	tt.Frame()
	settings := loadSettingsFile(t, a.Dir)
	for _, id := range []string{"ch-a", "ch-b", "ch-imported"} {
		if !containsID(settings.NotificationIDs, id) {
			t.Fatalf("Enable All skipped %s: %v", id, settings.NotificationIDs)
		}
	}
}

// TestSettingsNotificationsOfferSystemSettings checks the refused state's one
// action.
func TestSettingsNotificationsOfferSystemSettings(t *testing.T) {
	a := settingsTestApp(t)
	a.Settings.NotificationPermissionDenied = true
	_, tt := settingsTester(t, a, settingsNotifications)
	if !tt.HasText("Notifications are turned off for YouTube Reader in System Settings.") {
		t.Fatal("the refused state is not explained")
	}
	if err := tt.Click("Open System Settings…"); err != nil {
		t.Fatalf("open system settings: %v", err)
	}
	opened := tt.OpenedURLs()
	if len(opened) != 1 || !strings.HasPrefix(opened[0], "x-apple.systempreferences:") {
		t.Fatalf("opened %v", opened)
	}
}

func notificationGroupTestApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	home := t.TempDir()
	previousHomeDir := helperHomeDir
	helperHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { helperHomeDir = previousHomeDir })
	a := &App{
		Dir:      dir,
		Selected: "all",
		Settings: core.Settings{
			APIKey:         "A" + "I" + "z" + "a" + "SyA" + strings.Repeat("1", 32),
			RefreshMinutes: defaultRefreshMinutes,
			SyncPath:       filepath.Join(dir, "sync.json"),
			BackupPath:     filepath.Join(dir, "favorites-backup.json"),
			MaxGridColumns: defaultMaxGridColumns,
		},
		Sync: core.SyncState{
			SchemaVersion: 1,
			DeviceID:      "device-test",
			Channels: map[string]core.Channel{
				"ch-1": {ID: "ch-1", Title: "Go Channel"},
				"ch-2": {ID: "ch-2", Title: "Rust Channel"},
				"ch-3": {ID: "ch-3", Title: "Python Channel"},
			},
			ReadStates: map[string]map[string]core.ReadRecord{},
			Favorites:  map[string]core.FavoriteRecord{},
			Groups: map[string]core.ChannelGroup{
				"grp-tech": {ID: "grp-tech", Name: "Tech", ChannelIDs: []string{"ch-1", "ch-2"}},
			},
		},
		Channels: []core.Channel{
			{ID: "ch-1", Title: "Go Channel"},
			{ID: "ch-2", Title: "Rust Channel"},
			{ID: "ch-3", Title: "Python Channel"},
		},
		Groups: []core.ChannelGroup{
			{ID: "grp-tech", Name: "Tech", ChannelIDs: []string{"ch-1", "ch-2"}},
		},
	}
	return a
}

// TestNotificationGroupDerivedStates covers all-off, mixed, and all-on states
// for a group's master switch and summary text.
func TestNotificationGroupDerivedStates(t *testing.T) {
	// Case 1: All off
	a := notificationGroupTestApp(t)
	a.Settings.NotificationIDs = nil
	_, tt := settingsTester(t, a, settingsNotifications)
	techSwitch, ok := tt.AccessByRole(ui.RoleSwitch, "Tech")
	if !ok {
		t.Fatal("Tech group master switch missing")
	}
	if techSwitch.Checked {
		t.Fatal("expected Tech group switch to be off when no channels are enabled")
	}
	if !tt.HasText("0/2 enabled") {
		t.Fatalf("expected '0/2 enabled' text, got: %v", tt.Texts())
	}

	// Case 2: Mixed (1 of 2 on)
	a.Settings.NotificationIDs = []string{"ch-1"}
	_, tt = settingsTester(t, a, settingsNotifications)
	techSwitch, ok = tt.AccessByRole(ui.RoleSwitch, "Tech")
	if !ok {
		t.Fatal("Tech group master switch missing")
	}
	if techSwitch.Checked {
		t.Fatal("expected Tech group switch to be off in mixed state")
	}
	if !tt.HasText("1/2 enabled") {
		t.Fatalf("expected '1/2 enabled' text, got: %v", tt.Texts())
	}

	// Case 3: All on (2 of 2 on)
	a.Settings.NotificationIDs = []string{"ch-1", "ch-2"}
	_, tt = settingsTester(t, a, settingsNotifications)
	techSwitch, ok = tt.AccessByRole(ui.RoleSwitch, "Tech")
	if !ok {
		t.Fatal("Tech group master switch missing")
	}
	if !techSwitch.Checked {
		t.Fatal("expected Tech group switch to be on when all channels are enabled")
	}
	if !tt.HasText("2/2 enabled") {
		t.Fatalf("expected '2/2 enabled' text, got: %v", tt.Texts())
	}
}

// TestNotificationGroupMasterToggleBulkAddAndRemove verifies that flipping the
// group master switch in mixed/all-off state bulk-adds all channels in that group,
// and flipping in all-on state bulk-removes all channels in that group.
func TestNotificationGroupMasterToggleBulkAddAndRemove(t *testing.T) {
	a := notificationGroupTestApp(t)
	// Start with mixed state: only ch-1 is enabled
	a.Settings.NotificationIDs = []string{"ch-1"}
	_, tt := settingsTester(t, a, settingsNotifications)

	techSwitch, ok := tt.AccessByRole(ui.RoleSwitch, "Tech")
	if !ok {
		t.Fatal("Tech group switch not found")
	}
	if techSwitch.Checked {
		t.Fatal("expected Tech switch to start unchecked in mixed state")
	}

	// Flip from mixed -> should bulk-add all channels in Tech group
	tt.ClickAt(techSwitch.Bounds.X+techSwitch.Bounds.W/2, techSwitch.Bounds.Y+techSwitch.Bounds.H/2)
	tt.Frame()

	settings := loadSettingsFile(t, a.Dir)
	if !containsID(settings.NotificationIDs, "ch-1") || !containsID(settings.NotificationIDs, "ch-2") {
		t.Fatalf("expected both ch-1 and ch-2 in settings.NotificationIDs after bulk enable, got: %v", settings.NotificationIDs)
	}

	// Verify UI reflects all on
	techSwitch, ok = tt.AccessByRole(ui.RoleSwitch, "Tech")
	if !ok || !techSwitch.Checked {
		t.Fatal("expected Tech group switch to be checked after bulk enable")
	}
	if !tt.HasText("2/2 enabled") {
		t.Fatalf("expected '2/2 enabled', got: %v", tt.Texts())
	}

	// Flip from all on -> should bulk-remove all channels in Tech group
	tt.ClickAt(techSwitch.Bounds.X+techSwitch.Bounds.W/2, techSwitch.Bounds.Y+techSwitch.Bounds.H/2)
	tt.Frame()

	settings = loadSettingsFile(t, a.Dir)
	if containsID(settings.NotificationIDs, "ch-1") || containsID(settings.NotificationIDs, "ch-2") {
		t.Fatalf("expected neither ch-1 nor ch-2 in settings.NotificationIDs after bulk disable, got: %v", settings.NotificationIDs)
	}

	// Verify UI reflects all off
	techSwitch, ok = tt.AccessByRole(ui.RoleSwitch, "Tech")
	if !ok || techSwitch.Checked {
		t.Fatal("expected Tech group switch to be unchecked after bulk disable")
	}
	if !tt.HasText("0/2 enabled") {
		t.Fatalf("expected '0/2 enabled', got: %v", tt.Texts())
	}
}

// TestNotificationGroupDefaultOffForNewlyGroupedChannel checks that when a new
// channel is added to an existing group whose channels were all enabled, the new
// channel starts off by default and the group switch falls back to the mixed off state.
func TestNotificationGroupDefaultOffForNewlyGroupedChannel(t *testing.T) {
	a := notificationGroupTestApp(t)
	// Initially, Tech group only has ch-1 and it is enabled (1/1 enabled)
	a.Sync.Groups["grp-tech"] = core.ChannelGroup{
		ID:         "grp-tech",
		Name:       "Tech",
		ChannelIDs: []string{"ch-1"},
	}
	a.Groups = []core.ChannelGroup{
		{ID: "grp-tech", Name: "Tech", ChannelIDs: []string{"ch-1"}},
	}
	a.Settings.NotificationIDs = []string{"ch-1"}

	_, tt := settingsTester(t, a, settingsNotifications)
	techSwitch, ok := tt.AccessByRole(ui.RoleSwitch, "Tech")
	if !ok || !techSwitch.Checked {
		t.Fatal("expected Tech group switch to be checked initially")
	}
	if !tt.HasText("1/1 enabled") {
		t.Fatalf("expected '1/1 enabled', got: %v", tt.Texts())
	}

	// Now add ch-2 to the Tech group without adding it to NotificationIDs
	a.Sync.Groups["grp-tech"] = core.ChannelGroup{
		ID:         "grp-tech",
		Name:       "Tech",
		ChannelIDs: []string{"ch-1", "ch-2"},
	}
	a.Groups = []core.ChannelGroup{
		{ID: "grp-tech", Name: "Tech", ChannelIDs: []string{"ch-1", "ch-2"}},
	}

	// Re-render
	_, tt = settingsTester(t, a, settingsNotifications)

	// Newly grouped channel ch-2 ("Rust Channel") must be off by default
	rustSwitch, ok := tt.AccessByRole(ui.RoleSwitch, "Rust Channel")
	if !ok {
		t.Fatal("missing Rust Channel switch")
	}
	if rustSwitch.Checked {
		t.Fatal("newly grouped channel started checked, want off by default")
	}

	// Tech group switch must now be off (mixed state)
	techSwitch, ok = tt.AccessByRole(ui.RoleSwitch, "Tech")
	if !ok {
		t.Fatal("Tech group switch missing")
	}
	if techSwitch.Checked {
		t.Fatal("Tech group switch remained checked after newly grouped channel was added")
	}
	if !tt.HasText("1/2 enabled") {
		t.Fatalf("expected '1/2 enabled', got: %v", tt.Texts())
	}
}

// TestNotificationUngroupedAndBilingualLocalization verifies that ungrouped channels
// render under an Ungrouped card, localized to 未分组 with matching X/Y 已启用 summary
// in Chinese locale, and that the Ungrouped master switch functions properly.
func TestNotificationUngroupedAndBilingualLocalization(t *testing.T) {
	a := notificationGroupTestApp(t)
	// ch-1 and ch-2 are in Tech group. ch-3 ("Python Channel") is ungrouped.
	a.Settings.NotificationIDs = []string{"ch-3"}

	// 1. English locale
	_, tt := settingsTester(t, a, settingsNotifications)
	if !tt.HasText("Ungrouped") {
		t.Fatalf("expected Ungrouped section header in English, got: %v", tt.Texts())
	}
	if !tt.HasText("1/1 enabled") {
		t.Fatalf("expected 1/1 enabled for ungrouped, got: %v", tt.Texts())
	}

	ungroupedSwitch, ok := tt.AccessByRole(ui.RoleSwitch, "Ungrouped")
	if !ok {
		t.Fatal("Ungrouped master switch missing")
	}
	if !ungroupedSwitch.Checked {
		t.Fatal("Ungrouped master switch should be checked when all ungrouped channels are enabled")
	}

	// 2. Chinese locale
	tt.SetLocale("zh-Hans")
	tt.Frame()

	if !tt.HasText("未分组") {
		t.Fatalf("expected 未分组 in Chinese locale, got: %v", tt.Texts())
	}
	if tt.HasText("Ungrouped") {
		t.Fatalf("unexpected Ungrouped in Chinese locale, got: %v", tt.Texts())
	}
	if !tt.HasText("1/1 已启用") {
		t.Fatalf("expected 1/1 已启用 in Chinese locale, got: %v", tt.Texts())
	}

	ungroupedSwitchZh, ok := tt.AccessByRole(ui.RoleSwitch, "未分组")
	if !ok {
		t.Fatal("未分组 master switch missing in Chinese locale")
	}
	if !ungroupedSwitchZh.Checked {
		t.Fatal("未分组 master switch should be checked")
	}
}

// TestNotificationDisableAll verifies that clicking the global Disable All button
// clears all channel consent from settings.NotificationIDs.
func TestNotificationDisableAll(t *testing.T) {
	a := notificationGroupTestApp(t)
	a.Settings.NotificationIDs = []string{"ch-1", "ch-2", "ch-3"}
	_, tt := settingsTester(t, a, settingsNotifications)

	if err := tt.Click("Disable All"); err != nil {
		t.Fatalf("click Disable All: %v", err)
	}
	tt.Frame()

	settings := loadSettingsFile(t, a.Dir)
	if len(settings.NotificationIDs) != 0 {
		t.Fatalf("expected NotificationIDs to be empty after Disable All, got: %v", settings.NotificationIDs)
	}
}

func TestNotificationMultiGroupChannel(t *testing.T) {
	a := notificationGroupTestApp(t)
	a.Sync.Groups = map[string]core.ChannelGroup{
		"grp-tech":    {ID: "grp-tech", Name: "Tech", ChannelIDs: []string{"ch-1", "ch-2"}},
		"grp-science": {ID: "grp-science", Name: "Science", ChannelIDs: []string{"ch-1"}},
	}
	a.Groups = []core.ChannelGroup{
		{ID: "grp-tech", Name: "Tech", ChannelIDs: []string{"ch-1", "ch-2"}},
		{ID: "grp-science", Name: "Science", ChannelIDs: []string{"ch-1"}},
	}
	a.Settings.NotificationIDs = []string{"ch-1"}

	_, tt := settingsTester(t, a, settingsNotifications)

	techSwitch, ok := tt.AccessByRole(ui.RoleSwitch, "Tech")
	if !ok {
		t.Fatal("Tech group master switch missing")
	}
	scienceSwitch, ok := tt.AccessByRole(ui.RoleSwitch, "Science")
	if !ok {
		t.Fatal("Science group master switch missing")
	}

	if techSwitch.Checked {
		t.Fatal("expected Tech group switch to be off in mixed state")
	}
	if !scienceSwitch.Checked {
		t.Fatal("expected Science group switch to be on")
	}

	var goSwitches []ui.AccessInfo
	for _, info := range tt.AccessAll() {
		if info.Role == ui.RoleSwitch && info.Name == "Go Channel" {
			goSwitches = append(goSwitches, info)
		}
	}
	if len(goSwitches) != 2 {
		t.Fatalf("expected 2 'Go Channel' switches across groups, got %d", len(goSwitches))
	}
	for i, sw := range goSwitches {
		if !sw.Checked {
			t.Fatalf("switch %d for 'Go Channel' should be checked", i)
		}
	}

	tt.ClickAt(scienceSwitch.Bounds.X+scienceSwitch.Bounds.W/2, scienceSwitch.Bounds.Y+scienceSwitch.Bounds.H/2)
	tt.Frame()

	settings := loadSettingsFile(t, a.Dir)
	if containsID(settings.NotificationIDs, "ch-1") {
		t.Fatalf("expected ch-1 to be removed from NotificationIDs, got: %v", settings.NotificationIDs)
	}

	goSwitches = nil
	for _, info := range tt.AccessAll() {
		if info.Role == ui.RoleSwitch && info.Name == "Go Channel" {
			goSwitches = append(goSwitches, info)
		}
	}
	if len(goSwitches) != 2 {
		t.Fatalf("expected 2 'Go Channel' switches after toggle, got %d", len(goSwitches))
	}
	for i, sw := range goSwitches {
		if sw.Checked {
			t.Fatalf("switch %d for 'Go Channel' should be unchecked after Science group toggled off", i)
		}
	}
}

// TestEraseFavoritesClearsAndSaves is the destructive action behind the alert.
func TestEraseFavoritesClearsAndSaves(t *testing.T) {
	a := settingsTestApp(t)
	a.Sync.Favorites["video-2"] = core.FavoriteRecord{VideoID: "video-2", UpdatedAt: 2}
	if err := a.eraseFavorites(); err != nil {
		t.Fatalf("erase favorites: %v", err)
	}
	if len(a.Sync.Favorites) != 0 {
		t.Fatalf("favorites left: %v", a.Sync.Favorites)
	}
	data, err := os.ReadFile(a.Settings.SyncPath)
	if err != nil {
		t.Fatalf("sync file: %v", err)
	}
	var state core.SyncState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("sync JSON: %v", err)
	}
	if len(state.Favorites) != 0 {
		t.Fatalf("favorites left in the sync file: %v", state.Favorites)
	}
}

// TestEraseFavoritesDisabledWithoutFavorites keeps the destructive control when
// it could act, and dims it when there is nothing to erase: a command that
// cannot run is disabled, not a live button that returns silently.
func TestEraseFavoritesDisabledWithoutFavorites(t *testing.T) {
	a := settingsTestApp(t)
	a.Sync.Favorites = map[string]core.FavoriteRecord{}
	_, tt := settingsTester(t, a, settingsBackups)
	button, ok := tt.AccessByRole(ui.RoleButton, "Erase local favorites…")
	if !ok {
		t.Fatal("the Backups pane has no erase button")
	}
	if !button.Disabled {
		t.Fatal("Erase local favorites is enabled with nothing to erase")
	}

	a.Sync.Favorites["video-1"] = core.FavoriteRecord{VideoID: "video-1", UpdatedAt: 1}
	_, tt = settingsTester(t, a, settingsBackups)
	button, ok = tt.AccessByRole(ui.RoleButton, "Erase local favorites…")
	if !ok || button.Disabled {
		t.Fatalf("Erase local favorites with a favorite to erase is %+v", button)
	}
}

// TestSettingsGridStepperStopsAtItsBounds dims the arrow that cannot move the
// value, as a native stepper does, at both ends of the 2 to 8 range.
func TestSettingsGridStepperStopsAtItsBounds(t *testing.T) {
	for _, tc := range []struct {
		columns    int
		decreaseOn bool
		increaseOn bool
	}{
		{minMaxGridColumns, false, true},
		{4, true, true},
		{maxMaxGridColumns, true, false},
	} {
		a := settingsTestApp(t)
		a.Settings.MaxGridColumns = tc.columns
		_, tt := settingsTester(t, a, settingsGeneral)
		decrease, ok := tt.AccessByRole(ui.RoleButton, "Decrease")
		if !ok {
			t.Fatalf("%d columns: no Decrease button", tc.columns)
		}
		if enabled := !decrease.Disabled; enabled != tc.decreaseOn {
			t.Fatalf("%d columns: Decrease enabled = %v, want %v", tc.columns, enabled, tc.decreaseOn)
		}
		increase, ok := tt.AccessByRole(ui.RoleButton, "Increase")
		if !ok {
			t.Fatalf("%d columns: no Increase button", tc.columns)
		}
		if enabled := !increase.Disabled; enabled != tc.increaseOn {
			t.Fatalf("%d columns: Increase enabled = %v, want %v", tc.columns, enabled, tc.increaseOn)
		}
	}
}

// TestLaunchAgentFollowsRefreshInterval checks Manual writes neither a login
// run nor an interval, and a chosen interval schedules both, read back from
// the property list.
func TestLaunchAgentFollowsRefreshInterval(t *testing.T) {
	paths := helperPaths{Helper: "/tmp/helper", DataDir: "/tmp/data"}
	for _, tc := range []struct {
		minutes int
		seconds int
	}{{0, 0}, {5, 300}, {15, 900}, {60, 3600}} {
		if got := helperIntervalSeconds(tc.minutes); got != tc.seconds {
			t.Fatalf("%d minutes = %d seconds, want %d", tc.minutes, got, tc.seconds)
		}
		plist, err := renderLaunchAgent(paths, tc.minutes)
		if err != nil {
			t.Fatalf("render launch agent: %v", err)
		}
		values := plistValues(t, plist)
		if values["Label"] != helperLabel {
			t.Fatalf("label = %q, want %q", values["Label"], helperLabel)
		}
		wantRunAtLoad := ""
		wantInterval := ""
		if tc.seconds > 0 {
			wantRunAtLoad = "true"
			wantInterval = strconv.Itoa(tc.seconds)
		}
		if got := values["RunAtLoad"]; got != wantRunAtLoad {
			t.Fatalf("%d minutes RunAtLoad = %q, want %q", tc.minutes, got, wantRunAtLoad)
		}
		if got := values["StartInterval"]; got != wantInterval {
			t.Fatalf("%d minutes scheduled %q, want %q seconds", tc.minutes, got, wantInterval)
		}
	}
}

// TestHelperScheduleReconcilesWithSettings rewrites a login item an older
// build installed with a fixed interval, leaves a matching one alone, and
// removes the item under Manual.
func TestHelperScheduleReconcilesWithSettings(t *testing.T) {
	paths := helperPaths{Helper: "/tmp/helper", DataDir: t.TempDir()}
	dir := t.TempDir()
	path := filepath.Join(dir, helperLabel+".plist")
	var calls []string
	previous := helperLaunchctl
	helperLaunchctl = func(args ...string) error {
		calls = append(calls, strings.Join(args, " "))
		return nil
	}
	t.Cleanup(func() { helperLaunchctl = previous })

	old, err := renderLaunchAgent(paths, 60)
	if err != nil {
		t.Fatalf("render old launch agent: %v", err)
	}
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatalf("write old launch agent: %v", err)
	}

	if err := reconcileHelperItem(paths, path, 15); err != nil {
		t.Fatalf("reconcile 15 minutes: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read reconciled launch agent: %v", err)
	}
	values := plistValues(t, string(data))
	if got := values["StartInterval"]; got != "900" {
		t.Fatalf("reconciled StartInterval = %q, want 900", got)
	}
	if got := values["RunAtLoad"]; got != "true" {
		t.Fatalf("reconciled RunAtLoad = %q, want true", got)
	}

	calls = nil
	if err := reconcileHelperItem(paths, path, 15); err != nil {
		t.Fatalf("reconcile a matching interval: %v", err)
	}
	if len(calls) != 0 {
		t.Fatalf("a matching schedule was reloaded: %v", calls)
	}

	stale := helperPaths{Helper: "/tmp/old-helper", DataDir: t.TempDir()}
	stalePlist, err := renderLaunchAgent(stale, 15)
	if err != nil {
		t.Fatalf("render stale launch agent: %v", err)
	}
	if err := os.WriteFile(path, []byte(stalePlist), 0o600); err != nil {
		t.Fatalf("write stale launch agent: %v", err)
	}
	if err := reconcileHelperItem(paths, path, 15); err != nil {
		t.Fatalf("reconcile a moved helper: %v", err)
	}
	if len(calls) == 0 {
		t.Fatal("a login item naming another helper executable was not reloaded")
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("read reconciled launch agent: %v", err)
	}
	values = plistValues(t, string(data))
	if got, want := values["StandardOutPath"], filepath.Join(paths.DataDir, "helper.log"); got != want {
		t.Fatalf("reconciled StandardOutPath = %q, want %q", got, want)
	}
	arguments := plistStrings(t, string(data))
	if !containsString(arguments, paths.Helper) || containsString(arguments, stale.Helper) {
		t.Fatalf("reconciled ProgramArguments = %v, want %q", arguments, paths.Helper)
	}

	if err := reconcileHelperItem(paths, path, 0); err != nil {
		t.Fatalf("reconcile Manual: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("Manual kept the login item: %v", err)
	}

	missing := filepath.Join(dir, "missing.plist")
	if err := reconcileHelperItem(paths, missing, 15); err != nil {
		t.Fatalf("reconcile without a login item: %v", err)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("reconcile installed a helper nobody chose: %v", err)
	}
}

// TestLaunchReconcileSkipsWithoutAHelperBinary keeps launch from replacing a
// working login item with a schedule whose program is missing: even from a
// bundled app, a launch reconcile whose helper path is absent touches nothing.
func TestLaunchReconcileSkipsWithoutAHelperBinary(t *testing.T) {
	previousHomeDir := helperHomeDir
	home := t.TempDir()
	helperHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { helperHomeDir = previousHomeDir })
	// Run as if from a bundled app, so the reconcile reaches the helper-path
	// check; a dev build's missing bundle would skip every launch reconcile.
	previousBundle := runningFromAppBundle
	runningFromAppBundle = func() bool { return true }
	t.Cleanup(func() { runningFromAppBundle = previousBundle })
	previousPathsFor := helperPathsFor
	helperPathsFor = func() helperPaths {
		return helperPaths{Helper: filepath.Join(t.TempDir(), "missing-helper"), DataDir: t.TempDir()}
	}
	t.Cleanup(func() { helperPathsFor = previousPathsFor })
	var calls []string
	previousLaunchctl := helperLaunchctl
	helperLaunchctl = func(args ...string) error {
		calls = append(calls, strings.Join(args, " "))
		return nil
	}
	t.Cleanup(func() { helperLaunchctl = previousLaunchctl })

	bundled := helperPaths{Helper: "/Applications/YouTube Reader.app/Contents/Resources/ytreader-helper", DataDir: t.TempDir()}
	path := helperLaunchAgentPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create LaunchAgents: %v", err)
	}
	installed, err := renderLaunchAgent(bundled, 60)
	if err != nil {
		t.Fatalf("render the installed login item: %v", err)
	}
	if err := os.WriteFile(path, []byte(installed), 0o600); err != nil {
		t.Fatalf("write the installed login item: %v", err)
	}

	// The helper binary the bundle would name is absent, so launch reconcile
	// must skip however the interval resolves, leaving the login item alone.
	if err := reconcileHelperInterval(15); err != nil {
		t.Fatalf("reconcile at launch: %v", err)
	}
	if len(calls) != 0 {
		t.Fatalf("the launch reconcile touched launchd: %v", calls)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the login item: %v", err)
	}
	if string(data) != installed {
		t.Fatal("the launch reconcile rewrote a working login item")
	}
}

// TestFailedHelperBootstrapLeavesNoLoginItem keeps the pane honest: when launchd
// refuses the job the install failed, so no login item is left behind for
// helperInstalled to report as installed.
func TestFailedHelperBootstrapLeavesNoLoginItem(t *testing.T) {
	paths := helperPaths{Helper: "/tmp/helper", DataDir: t.TempDir()}
	path := filepath.Join(t.TempDir(), helperLabel+".plist")
	var calls []string
	previous := helperLaunchctl
	helperLaunchctl = func(args ...string) error {
		calls = append(calls, strings.Join(args, " "))
		if len(args) > 0 && args[0] == "bootstrap" {
			return errors.New("launchd refused the job")
		}
		return nil
	}
	t.Cleanup(func() { helperLaunchctl = previous })

	if err := writeHelperItem(paths, path, 15); err == nil {
		t.Fatal("writeHelperItem reported success after launchd refused the job")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("a failed bootstrap left the login item on disk: %v", err)
	}
	if len(calls) < 2 || !strings.HasPrefix(calls[len(calls)-1], "bootout ") {
		t.Fatalf("a failed bootstrap did not unload what it wrote: %v", calls)
	}
}

// TestReconcileFailedBootstrapKeepsThePriorLoginItem keeps a launchd refusal
// while rewriting a working login item from destroying the schedule it had:
// the previously installed property list goes back on disk, launchd is asked
// to load it again so the schedule that ran keeps running, and the error is
// still reported.
func TestReconcileFailedBootstrapKeepsThePriorLoginItem(t *testing.T) {
	paths := helperPaths{Helper: "/tmp/helper", DataDir: t.TempDir()}
	path := filepath.Join(t.TempDir(), helperLabel+".plist")
	var calls []string
	refused := false
	previous := helperLaunchctl
	helperLaunchctl = func(args ...string) error {
		call := strings.Join(args, " ")
		calls = append(calls, call)
		if strings.HasPrefix(call, "bootstrap ") && !refused {
			refused = true
			return errors.New("launchd refused the job")
		}
		return nil
	}
	t.Cleanup(func() { helperLaunchctl = previous })

	installed, err := renderLaunchAgent(helperPaths{Helper: "/tmp/old-helper", DataDir: t.TempDir()}, 60)
	if err != nil {
		t.Fatalf("render the installed login item: %v", err)
	}
	if err := os.WriteFile(path, []byte(installed), 0o600); err != nil {
		t.Fatalf("write the installed login item: %v", err)
	}

	if err := reconcileHelperItem(paths, path, 15); err == nil {
		t.Fatal("reconcile reported success after launchd refused the rewrite")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the failed rewrite removed the prior login item: %v", err)
	}
	if string(data) != installed {
		t.Fatal("the failed rewrite left a different property list on disk")
	}
	if got, want := calls[len(calls)-1], "bootstrap gui/"+uid()+" "+path; got != want {
		t.Fatalf("the restored login item was not loaded again: last call %q of %v, want %q", got, calls, want)
	}
}

// plistValues decodes a property list's scalar entries by key.
func plistValues(t *testing.T, data string) map[string]string {
	t.Helper()
	values := map[string]string{}
	dec := xml.NewDecoder(strings.NewReader(data))
	key := ""
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return values
		}
		if err != nil {
			t.Fatalf("parse property list: %v", err)
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "key":
			var name string
			if err := dec.DecodeElement(&name, &start); err != nil {
				t.Fatalf("parse property list key: %v", err)
			}
			key = name
		case "array", "dict":
			key = ""
		case "string", "integer":
			if key == "" {
				continue
			}
			var value string
			if err := dec.DecodeElement(&value, &start); err != nil {
				t.Fatalf("parse property list value: %v", err)
			}
			values[key] = value
			key = ""
		case "true", "false":
			if key == "" {
				continue
			}
			values[key] = start.Name.Local
			key = ""
		}
	}
}

// plistStrings collects every string value in a property list, including the
// entries of an array.
func plistStrings(t *testing.T, data string) []string {
	t.Helper()
	var values []string
	dec := xml.NewDecoder(strings.NewReader(data))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return values
		}
		if err != nil {
			t.Fatalf("parse property list: %v", err)
		}
		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Local != "string" {
			continue
		}
		var value string
		if err := dec.DecodeElement(&value, &start); err != nil {
			t.Fatalf("parse property list value: %v", err)
		}
		values = append(values, value)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
