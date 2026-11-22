package main

import (
	"bytes"
	"context"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
	mui "github.com/simon/youtube-reader/ui"
)

// statesTestApp is a window's worth of data with one channel and one video,
// which every state below mutates into the case it tests. Avatars and
// thumbnails carry no URL, so a frame never waits on the network.
func statesTestApp(t *testing.T) *App {
	t.Helper()
	channel := core.Channel{ID: "ch-a", Title: "Alpha channel"}
	video := core.Video{
		ID: "v-a", ChannelID: channel.ID, ChannelTitle: channel.Title,
		Title: "A quiet morning in Kyoto", PublishedAt: time.Now().Add(-2 * time.Hour).Format(time.RFC3339),
	}
	return &App{
		Dir:      t.TempDir(),
		Selected: "all",
		Settings: core.Settings{
			APIKey:         "test-key",
			SyncPath:       filepath.Join(t.TempDir(), "sync.json"),
			BackupPath:     filepath.Join(t.TempDir(), "favorites.json"),
			RefreshMinutes: 60,
			SidebarWidth:   defaultSidebarWidth,
			DetailsWidth:   defaultDetailsWidth,
			MaxGridColumns: defaultMaxGridColumns,
			ExpandedGroups: map[string]bool{},
		},
		Sync: core.SyncState{
			Channels:   map[string]core.Channel{channel.ID: channel},
			Groups:     map[string]core.ChannelGroup{},
			Favorites:  map[string]core.FavoriteRecord{},
			ReadStates: map[string]map[string]core.ReadRecord{},
		},
		Channels: []core.Channel{channel},
		Videos:   []core.Video{video},
	}
}

// statesTester renders the whole window, toolbar and all, so a state is
// asserted in the frame the reader actually sees.
func statesTester(t *testing.T, w *Window) *ui.Tester {
	t.Helper()
	tt := ui.NewTester(w.shell, 1280, 808)
	tt.Frame()
	return tt
}

// refreshingControls counts the toolbar controls that say a refresh is
// running. The board allows exactly one activity sign while the content pane
// loads, and it lives in the toolbar, never in the pane.
func refreshingControls(tt *ui.Tester) int {
	n := 0
	for _, info := range tt.AccessAll() {
		if info.Role == ui.RoleButton && info.Name == "Refreshing" {
			n++
		}
	}
	return n
}

// standaloneProgress counts progress indicators that are not the toolbar's
// refresh control. The pane's skeleton must never draw one.
func standaloneProgress(tt *ui.Tester) int {
	n := 0
	for _, info := range tt.AccessAll() {
		if info.Role == ui.RoleProgress {
			n++
		}
	}
	return n
}

// TestEmptyCenterStatesInTheWindow drives the board's four empty contexts
// through the shipping window and checks each one's copy and action.
func TestEmptyCenterStatesInTheWindow(t *testing.T) {
	t.Run("no API key", func(t *testing.T) {
		a := statesTestApp(t)
		a.Settings.APIKey = ""
		a.Sync.Channels = map[string]core.Channel{}
		a.Channels = nil
		a.Videos = nil
		w := &Window{app: a, selectedIndex: new(int)}
		tt := statesTester(t, w)

		if !tt.HasText("Set up YouTube Reader") {
			t.Fatalf("setup state texts = %q", tt.Texts())
		}
		if !tt.HasText(mui.EmptySidebarTitle) {
			t.Fatalf("the sidebar is blank: %q", tt.Texts())
		}
		if _, ok := tt.AccessByRole(ui.RoleButton, "Open Settings"); !ok {
			t.Fatalf("setup has no settings action: %q", tt.Texts())
		}
		previousCreate := newSettingsWindow
		t.Cleanup(func() { newSettingsWindow = previousCreate })
		created := 0
		newSettingsWindow = func(opts mygo.WindowOptions) *mygo.Window {
			created++
			return &mygo.Window{}
		}
		if err := tt.Click("Open Settings"); err != nil {
			t.Fatalf("click Open Settings: %v", err)
		}
		if created != 1 {
			t.Fatalf("Open Settings created %d settings windows, want 1", created)
		}
	})

	t.Run("no channels", func(t *testing.T) {
		a := statesTestApp(t)
		a.Sync.Channels = map[string]core.Channel{}
		a.Channels = nil
		a.Videos = nil
		w := &Window{app: a, selectedIndex: new(int)}
		tt := statesTester(t, w)

		if !tt.HasText("Import your channels") {
			t.Fatalf("import state texts = %q", tt.Texts())
		}
		if _, ok := tt.AccessByRole(ui.RoleButton, "Import OPML"); !ok {
			t.Fatalf("import has no action: %q", tt.Texts())
		}
		if !tt.HasText(mui.EmptySidebarAction) {
			t.Fatalf("the empty sidebar offers no action: %q", tt.Texts())
		}
	})

	t.Run("no favorites", func(t *testing.T) {
		a := statesTestApp(t)
		a.Selected = "favorites"
		w := &Window{app: a, selectedIndex: new(int)}
		tt := statesTester(t, w)

		if !tt.HasText("No favorites yet") {
			t.Fatalf("favorites state texts = %q", tt.Texts())
		}
		if err := tt.Click("Browse videos"); err != nil {
			t.Fatalf("click Browse videos: %v", err)
		}
		if a.Selected != "all" {
			t.Fatalf("Browse videos selected %q, want all", a.Selected)
		}
	})

	t.Run("filters hide everything", func(t *testing.T) {
		a := statesTestApp(t)
		a.Search = "nothing matches this"
		a.ChannelFilter = "ch-a"
		a.UnreadOnly = true
		w := &Window{app: a, selectedIndex: new(int)}
		tt := statesTester(t, w)

		if !tt.HasText("No videos match your filters") {
			t.Fatalf("filtered state texts = %q", tt.Texts())
		}
		if err := tt.Click("Clear filters"); err != nil {
			t.Fatalf("click Clear filters: %v", err)
		}
		if a.Search != "" || a.ChannelFilter != "" || a.UnreadOnly {
			t.Fatalf("Clear filters left search=%q channel=%q unwatched=%v", a.Search, a.ChannelFilter, a.UnreadOnly)
		}
	})
}

// TestInitialLoadSkeletonAndCancel covers the first load: 12 neutral cards, no
// spinner inside the pane, one toolbar spinner, and a working Cancel.
func TestInitialLoadSkeletonAndCancel(t *testing.T) {
	a := statesTestApp(t)
	a.Videos = nil
	a.Busy = true
	cancelled := false
	w := &Window{app: a, selectedIndex: new(int)}
	w.cancelRefresh = func() { cancelled = true }
	tt := statesTester(t, w)

	if got := refreshingControls(tt); got != 1 {
		t.Fatalf("the first load shows %d refreshing controls, want the toolbar's one", got)
	}
	if got := standaloneProgress(tt); got != 0 {
		t.Fatalf("the skeleton draws %d progress indicators of its own; the toolbar owns the one", got)
	}
	if _, ok := tt.Find(mui.LoadingGridLabel); !ok {
		t.Fatalf("the first load draws no skeleton: %q", tt.Texts())
	}
	if !tt.HasText(mui.LoadingCancelLabel) {
		t.Fatalf("the first load offers no Cancel: %q", tt.Texts())
	}
	if tt.HasText("No videos yet") {
		t.Fatal("the first load fell through to the empty state")
	}
	if err := tt.Click(mui.LoadingCancelLabel); err != nil {
		t.Fatalf("click Cancel: %v", err)
	}
	if !cancelled {
		t.Fatal("Cancel did not cancel the refresh context")
	}
}

// TestRefreshKeepsContentVisible proves a refresh with content on screen never
// reloads the pane: the cards stay and the activity lives in the toolbar.
func TestRefreshKeepsContentVisible(t *testing.T) {
	a := statesTestApp(t)
	a.Busy = true
	w := &Window{app: a, selectedIndex: new(int)}
	tt := statesTester(t, w)

	if !tt.HasText(a.Videos[0].Title) {
		t.Fatalf("the grid lost its content while refreshing: %q", tt.Texts())
	}
	if got := refreshingControls(tt); got != 1 {
		t.Fatalf("a refresh with content shows %d refreshing controls, want the toolbar's one", got)
	}
	if tt.HasText(mui.LoadingCancelLabel) {
		t.Fatal("a refresh with content offered Cancel; it keeps the content instead of reloading")
	}
}

// TestOfflineKeepsContentAndDisablesRefresh checks the offline shape: cached
// content stays, the toolbar control is disabled with the reason, and an empty
// pane names the state instead of an empty library.
func TestOfflineKeepsContentAndDisablesRefresh(t *testing.T) {
	t.Run("with cached content", func(t *testing.T) {
		a := statesTestApp(t)
		w := &Window{app: a, selectedIndex: new(int), offline: true}
		tt := statesTester(t, w)

		if !tt.HasText(a.Videos[0].Title) {
			t.Fatalf("offline lost the cached content: %q", tt.Texts())
		}
		refresh, ok := tt.AccessByRole(ui.RoleButton, "Refresh")
		if !ok {
			t.Fatalf("no refresh control in %q", tt.Texts())
		}
		if !refresh.Disabled {
			t.Fatal("the refresh control is enabled while offline")
		}
		if refresh.Description != mui.OfflineTooltip {
			t.Fatalf("refresh tooltip = %q, want %q", refresh.Description, mui.OfflineTooltip)
		}
	})

	t.Run("with nothing cached", func(t *testing.T) {
		a := statesTestApp(t)
		a.Videos = nil
		w := &Window{app: a, selectedIndex: new(int), offline: true}
		tt := statesTester(t, w)

		if !tt.HasText(mui.OfflineTitle) || !tt.HasText(mui.OfflineSentence) {
			t.Fatalf("offline pane texts = %q", tt.Texts())
		}
		retry, ok := tt.AccessByRole(ui.RoleButton, mui.OfflineActionLabel)
		if !ok {
			t.Fatalf("the offline pane has no Retry face: %q", tt.Texts())
		}
		if !retry.Disabled || retry.Description != mui.OfflineTooltip {
			t.Fatalf("offline Retry = disabled %v, reason %q; want disabled with %q", retry.Disabled, retry.Description, mui.OfflineTooltip)
		}
	})
}

// TestRefreshErrorIsInlineAndRedacted checks the failed refresh with nothing
// cached: the pane says what failed, why, and the next action, and the stored
// key never reaches the copy.
func TestRefreshErrorIsInlineAndRedacted(t *testing.T) {
	a := statesTestApp(t)
	a.Videos = nil
	a.Settings.RecentFailures = []core.RefreshFailure{{
		At:      time.Now().Format(time.RFC3339),
		Message: "Get \"https://www.googleapis.com/youtube/v3/channels?part=snippet&key=AIzaStored\": dial tcp: no route to host",
	}}
	w := &Window{app: a, selectedIndex: new(int)}
	tt := statesTester(t, w)

	title, sentence := mui.RefreshErrorCopy(mui.CauseConnection)
	if !tt.HasText(title) || !tt.HasText(sentence) {
		t.Fatalf("inline error texts = %q, want %q and %q", tt.Texts(), title, sentence)
	}
	if !tt.HasText("Get \"https://www.googleapis.com/youtube/v3/channels\": dial tcp: no route to host") {
		t.Fatalf("the inline error does not show the redacted reason: %q", tt.Texts())
	}
	for _, text := range tt.Texts() {
		if strings.Contains(text, "AIzaStored") {
			t.Fatalf("the inline error leaked the API key: %q", text)
		}
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, "Retry"); !ok {
		t.Fatalf("the inline error has no Retry: %q", tt.Texts())
	}
}

// TestCachedContentBeatsOfflineAndErrors checks the pane a cached library
// still owns: a filter that matches nothing shows the filtered empty state,
// not the offline or failed-refresh state, because the content is still there
// and only the filter hides it.
func TestCachedContentBeatsOfflineAndErrors(t *testing.T) {
	t.Run("offline", func(t *testing.T) {
		a := statesTestApp(t)
		a.Search = "nothing matches this"
		w := &Window{app: a, selectedIndex: new(int), offline: true}
		tt := statesTester(t, w)
		if !tt.HasText("No videos match your filters") {
			t.Fatalf("filtered offline texts = %q", tt.Texts())
		}
		if tt.HasText(mui.OfflineTitle) {
			t.Fatal("offline replaced the filtered state though the library still has cached videos")
		}
	})

	t.Run("failed refresh", func(t *testing.T) {
		a := statesTestApp(t)
		a.Search = "nothing matches this"
		a.Settings.RecentFailures = []core.RefreshFailure{{
			At:      time.Now().Format(time.RFC3339),
			Message: "dial tcp: no route to host",
		}}
		w := &Window{app: a, selectedIndex: new(int)}
		tt := statesTester(t, w)
		if !tt.HasText("No videos match your filters") {
			t.Fatalf("filtered error texts = %q", tt.Texts())
		}
		if tt.HasText("Could not refresh subscriptions") {
			t.Fatal("the inline error replaced the filtered state though the status surface owns a refresh failure with cached content")
		}
	})
}

// TestRefreshErrorCause covers the mapping from a stored message to the error
// copy, including the key the app redacts at display time.
func TestRefreshErrorCause(t *testing.T) {
	tests := []struct {
		name    string
		message string
		online  bool
		want    mui.ErrorCause
	}{
		{"offline", "dial tcp: no route to host", false, mui.CauseOffline},
		{"rejected key", "Error 400: API key not valid", true, mui.CauseRejectedKey},
		{"quota", "quota paused until 2026-03-02T14:32:00Z", true, mui.CauseQuota},
		{"connection", "dial tcp: no route to host", true, mui.CauseConnection},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := refreshErrorCause(tt.message, tt.online); got != tt.want {
				t.Fatalf("refreshErrorCause(%q, %v) = %v, want %v", tt.message, tt.online, got, tt.want)
			}
		})
	}
}

// TestEmptyDetailFallbackDraws checks the detail pane's default branch: an open
// pane with no video shows its one sentence instead of a blank column.
func TestEmptyDetailFallbackDraws(t *testing.T) {
	a := statesTestApp(t)
	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(func(c *ui.Context) {
		tokens := mui.NewTokens(c.Theme(), c.Theme().Dark)
		ui.Column(c).Fill().Width(defaultDetailsWidth).Background(tokens.App).Children(func() {
			w.drawer(c, tokens)
		})
	}, int(defaultDetailsWidth), 520)
	tt.Frame()

	if !tt.HasText(mui.EmptyDetailSentence) {
		t.Fatalf("the open empty detail pane texts = %q, want %q", tt.Texts(), mui.EmptyDetailSentence)
	}
}

// TestCancelledRefreshRecordsNoFailure proves Cancel does not leave a failure
// behind for the next refresh to inherit. The context is already cancelled, so
// the attempt fails before any network call and the branch that records
// failures is skipped.
func TestCancelledRefreshRecordsNoFailure(t *testing.T) {
	a := statesTestApp(t)
	store, err := core.OpenStore(context.Background(), filepath.Join(t.TempDir(), "videos.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a.Store = store

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	outcome := a.RefreshContext(ctx, true, a.refreshSnapshot())
	if len(outcome.Failures) != 0 {
		t.Fatalf("a cancelled refresh recorded %d failures", len(outcome.Failures))
	}
	if outcome.Description != "Cancelled" {
		t.Fatalf("cancelled outcome description = %q, want Cancelled", outcome.Description)
	}
	if got := a.settingsSnapshot().RecentFailures; len(got) != 0 {
		t.Fatalf("a cancelled refresh stored %d failures", len(got))
	}
}

// TestStateWindowScreenshots renders every whole-window state in both
// appearances under YTREADER_STATE_SCREENSHOTS, from the shipping view, so the
// board comparison reads the app's own pixels.
func TestStateWindowScreenshots(t *testing.T) {
	dir := os.Getenv("YTREADER_STATE_SCREENSHOTS")
	if dir == "" {
		t.Skip("YTREADER_STATE_SCREENSHOTS is unset")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	type windowState struct {
		name  string
		build func(*testing.T) *Window
	}
	states := []windowState{
		{"empty-setup", func(t *testing.T) *Window {
			a := statesTestApp(t)
			a.Settings.APIKey = ""
			a.Sync.Channels = map[string]core.Channel{}
			a.Channels, a.Videos = nil, nil
			return &Window{app: a, selectedIndex: new(int)}
		}},
		{"empty-import", func(t *testing.T) *Window {
			a := statesTestApp(t)
			a.Sync.Channels = map[string]core.Channel{}
			a.Channels, a.Videos = nil, nil
			return &Window{app: a, selectedIndex: new(int)}
		}},
		{"empty-filtered", func(t *testing.T) *Window {
			a := statesTestApp(t)
			a.Search = "nothing matches this"
			return &Window{app: a, selectedIndex: new(int)}
		}},
		{"empty-favorites", func(t *testing.T) *Window {
			a := statesTestApp(t)
			a.Selected = "favorites"
			return &Window{app: a, selectedIndex: new(int)}
		}},
		{"loading", func(t *testing.T) *Window {
			a := statesTestApp(t)
			a.Videos = nil
			a.Busy = true
			w := &Window{app: a, selectedIndex: new(int)}
			w.cancelRefresh = func() {}
			return w
		}},
		{"offline", func(t *testing.T) *Window {
			a := statesTestApp(t)
			a.Videos = nil
			return &Window{app: a, selectedIndex: new(int), offline: true}
		}},
		{"error", func(t *testing.T) *Window {
			a := statesTestApp(t)
			a.Videos = nil
			a.Settings.RecentFailures = []core.RefreshFailure{{
				At:      time.Now().Format(time.RFC3339),
				Message: "Get \"https://www.googleapis.com/youtube/v3/channels?part=snippet&key=AIzaStored\": dial tcp: no route to host",
			}}
			return &Window{app: a, selectedIndex: new(int)}
		}},
		{"content-refresh", func(t *testing.T) *Window {
			a := statesTestApp(t)
			a.Busy = true
			return &Window{app: a, selectedIndex: new(int)}
		}},
	}
	for _, dark := range []bool{false, true} {
		appearance := "light"
		if dark {
			appearance = "dark"
		}
		for _, state := range states {
			w := state.build(t)
			tt := ui.NewTester(w.shell, 1280, 808)
			tt.SetDark(dark)
			tt.SetScale(2)
			tt.Frame()
			tt.Advance(mui.MotionInteraction + 20*time.Millisecond)
			tt.Frame()
			var buf bytes.Buffer
			if err := png.Encode(&buf, tt.Image()); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "state-"+state.name+"-"+appearance+".png")
			if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Logf("wrote %s", path)
		}
	}
}
