package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
)

// settingsTestApp is the smallest app that renders every pane of the Settings
// window: a saved API key, one path pair, two channels, and one favorite.
func settingsTestApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
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
	// A valid key clears the message as it is typed.
	clickField(t, tt, "API key")
	tt.Command("selectAll")
	tt.Type(validKey)
	tt.Frame()
	if tt.HasText("That is not a YouTube Data API key.") {
		t.Fatal("a valid key still shows the invalid message")
	}
	if info, ok := tt.AccessByRole(ui.RoleTextField, "API key"); !ok || !info.Password {
		t.Fatal("the API key field is not masked")
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

// TestSettingsRefreshChoices pins the board's five intervals and the default.
func TestSettingsRefreshChoices(t *testing.T) {
	want := []string{"Manual", "5 min", "15 min", "30 min", "60 min"}
	if len(refreshChoices) != len(want) {
		t.Fatalf("refreshChoices = %d entries, want %d", len(refreshChoices), len(want))
	}
	for i, choice := range refreshChoices {
		if choice.label != want[i] {
			t.Fatalf("choice %d = %q, want %q", i, choice.label, want[i])
		}
		if got := refreshChoiceMinutes(choice.label); got != choice.minutes {
			t.Fatalf("refreshChoiceMinutes(%q) = %d, want %d", choice.label, got, choice.minutes)
		}
	}
	if defaultRefreshMinutes != 15 {
		t.Fatalf("default refresh interval = %d, want 15", defaultRefreshMinutes)
	}
	if got := refreshChoiceMinutes("15 min"); got != 15 {
		t.Fatalf("\"15 min\" maps to %d, want 15", got)
	}
	if got := refreshChoiceLabel(0); got != "Manual" {
		t.Fatalf("zero minutes = %q, want Manual", got)
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

// TestManualRefreshNeverSchedules keeps Manual from being treated as an
// interval of zero minutes that is always due.
func TestManualRefreshNeverSchedules(t *testing.T) {
	if got := nextRefreshStamp(time.Now(), 0); got != "" {
		t.Fatalf("manual refresh scheduled %q", got)
	}
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	a.Settings.RefreshMinutes = 0
	a.Settings.LastRefresh = "2020-01-01T00:00:00Z"
	a.StartupRefreshIfDue()
	if a.Busy {
		t.Fatal("a manual interval refreshed at startup")
	}
}

// TestSettingsTabOrderFollowsVisualOrder walks the pane with Tab and checks
// each focused control is at or below the one before it, as reading order. The
// pane toolbar is one stop, as a segmented control is: the arrows move within
// it, and Tab leaves it for the form.
func TestSettingsTabOrderFollowsVisualOrder(t *testing.T) {
	a := settingsTestApp(t)
	_, tt := settingsTester(t, a, settingsGeneral)
	seen := map[string]bool{}
	lastY := float32(-1)
	for i := 0; i < 16; i++ {
		tt.Key(0, ui.KeyTab)
		tt.Frame()
		focused, ok := focusedElement(tt)
		if !ok {
			t.Fatalf("tab step %d focused nothing", i)
		}
		name := focused.Name
		if focused.Role == ui.RoleRadio {
			name = "segment " + name
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		if focused.Bounds.Y < lastY {
			t.Fatalf("%q at %v comes after a control at %v", name, focused.Bounds.Y, lastY)
		}
		lastY = focused.Bounds.Y
	}
	for _, want := range []string{"segment General", "API key", "Reveal API key", "15 min", "Show Shorts", "Decrease", "Increase", "Install helper…"} {
		if !seen[want] {
			t.Errorf("Tab never reached %q", want)
		}
	}
}

// TestSettingsPaneToolbarArrowsSwitch keeps the pane toolbar reachable and
// usable from the keyboard alone.
func TestSettingsPaneToolbarArrowsSwitch(t *testing.T) {
	a := settingsTestApp(t)
	w, tt := settingsTester(t, a, settingsGeneral)
	tt.Key(0, ui.KeyTab)
	tt.Frame()
	focused, ok := focusedElement(tt)
	if !ok || focused.Role != ui.RoleRadio {
		t.Fatalf("the first Tab stop is %+v, want the pane toolbar", focused)
	}
	tt.Key(0, ui.KeyRight)
	tt.Frame()
	if w.settingsWindow.pane != settingsBackups {
		t.Fatalf("Right moved the pane to %v, want Backups", w.settingsWindow.pane)
	}
	if settings := loadSettingsFile(t, a.Dir); settings.LastSettingsPane != "Backups" {
		t.Fatalf("pane saved as %q", settings.LastSettingsPane)
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

// TestSettingsMenuAccelerator pins the board's Command-Comma.
func TestSettingsMenuAccelerator(t *testing.T) {
	if settingsMenuAccelerator != "Cmd+," {
		t.Fatalf("settings accelerator = %q, want Cmd+,", settingsMenuAccelerator)
	}
}

// TestSettingsPanesAndTitle covers the three panes, their titles, and the
// restore of the pane shown last time.
func TestSettingsPanesAndTitle(t *testing.T) {
	if len(settingsPaneNames) != 3 {
		t.Fatalf("panes = %v", settingsPaneNames)
	}
	for _, pane := range []settingsPane{settingsGeneral, settingsBackups, settingsNotifications} {
		if got := settingsPaneFromName(pane.name()); got != pane {
			t.Fatalf("%q restored as %v", pane.name(), got)
		}
		if pane.title() != settingsPaneNames[pane] {
			t.Fatalf("%v title = %q", pane, pane.title())
		}
	}
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
	if !opts.DisableResize || !opts.DisableMinimize || !opts.DisableMaximize {
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

// TestLaunchAgentFollowsRefreshInterval checks Manual installs no interval and
// a chosen one schedules the helper for it.
func TestLaunchAgentFollowsRefreshInterval(t *testing.T) {
	paths := helperPaths{Helper: "/tmp/helper", DataDir: "/tmp/data", Home: "/tmp/home"}
	for _, tc := range []struct {
		minutes int
		want    string
	}{{0, ""}, {5, "300"}, {15, "900"}, {60, "3600"}} {
		paths.Interval = tc.minutes * 60
		plist, err := renderLaunchAgent(paths)
		if err != nil {
			t.Fatalf("render launch agent: %v", err)
		}
		if tc.want == "" {
			if strings.Contains(plist, "StartInterval") {
				t.Fatalf("manual interval scheduled the helper: %s", plist)
			}
			continue
		}
		if !strings.Contains(plist, "<key>StartInterval</key><integer>"+tc.want+"</integer>") {
			t.Fatalf("%d minutes did not schedule %s seconds: %s", tc.minutes, tc.want, plist)
		}
	}
}

func containsID(ids []string, id string) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
}
