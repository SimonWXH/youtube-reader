package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/simon/youtube-reader/core"
)

// stubYouTube answers the YouTube API endpoints a refresh calls with canned
// bodies by swapping the default transport, which core.YouTube uses when no
// client is set. The returned func restores the original transport.
func stubYouTube(t *testing.T, responses map[string]string) func() {
	t.Helper()
	original := http.DefaultTransport
	http.DefaultTransport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		for path, body := range responses {
			if strings.Contains(req.URL.Path, path) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(body)),
				}, nil
			}
		}
		return nil, fmt.Errorf("unexpected YouTube API path %s", req.URL.Path)
	})
	return func() { http.DefaultTransport = original }
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// offlineArticlePages keeps tests off the network: the reader's full-text
// fetch fails at once instead of reaching a real article page.
func offlineArticlePages() *http.Client {
	return &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("offline in tests")
	})}
}

func newTestApp(t *testing.T, dir string) *App {
	t.Helper()
	ctx := context.Background()
	store, err := core.OpenStore(ctx, filepath.Join(dir, "videos.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	lock, err := core.AcquireLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := &App{
		Dir:      dir,
		Store:    store,
		Lock:     lock,
		Settings: core.Settings{SyncPath: filepath.Join(dir, "sync.json"), RefreshMinutes: 60},
		Sync: core.SyncState{
			SchemaVersion: 1,
			Channels:      map[string]core.Channel{},
			ReadStates:    map[string]map[string]core.ReadRecord{},
			Favorites:     map[string]core.FavoriteRecord{},
			Groups:        map[string]core.ChannelGroup{},
		},
		// Opening an article starts its full-text fetch, so a test app answers
		// article pages offline: no test reaches the network, and a summary
		// article settles into the reader's failure state at once.
		rssHTTPClient: offlineArticlePages(),
	}
	return a
}

func TestConsumeHandoffAppliesRefreshResults(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()

	handoff := struct {
		ID          string                  `json:"id"`
		CreatedAt   string                  `json:"createdAt"`
		Description string                  `json:"description"`
		Result      *core.RefreshResult     `json:"result"`
		Notified    []core.NotificationPlan `json:"notified"`
		Failures    []core.RefreshFailure   `json:"failures"`
	}{
		ID:          "handoff-1",
		CreatedAt:   "2026-03-01T12:00:00Z",
		Description: "Refreshed 2 videos",
		Result:      &core.RefreshResult{Description: "Refreshed 2 videos", Changed: true},
		Notified:    []core.NotificationPlan{{ChannelID: "ch-1", ChannelName: "Channel", Count: 2, NewestTitle: "New"}},
		Failures:    []core.RefreshFailure{{At: "2026-03-01T11:59:00Z", Message: "channel failed"}},
	}
	data, _ := json.Marshal(handoff)
	if err := os.WriteFile(filepath.Join(dir, "inbox.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.ConsumeHandoff(); err != nil {
		t.Fatal(err)
	}
	if a.Settings.LastHandoffID != "handoff-1" || a.Settings.LastRefresh != "2026-03-01T12:00:00Z" {
		t.Fatalf("settings: %+v", a.Settings)
	}
	if a.LastRefreshMessage != "Refreshed 2 videos" || len(a.LastNotified) != 1 {
		t.Fatalf("status: %q %+v", a.LastRefreshMessage, a.LastNotified)
	}
	if len(a.Settings.RecentFailures) != 1 || a.Settings.RecentFailures[0].Message != "channel failed" {
		t.Fatalf("failures: %+v", a.Settings.RecentFailures)
	}
	if _, err := os.Stat(filepath.Join(dir, "inbox.json")); !os.IsNotExist(err) {
		t.Fatalf("inbox not removed: %v", err)
	}
	// A duplicate handoff must be ignored.
	if err := a.ConsumeHandoff(); err != nil {
		t.Fatal(err)
	}
	if a.LastRefreshMessage != "Refreshed 2 videos" {
		t.Fatalf("status changed: %q", a.LastRefreshMessage)
	}
}

func TestConsumeHandoffNoopDoesNotAdvanceRefresh(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	last := "2026-03-01T10:00:00Z"
	a.Settings.LastRefresh = last
	handoff := map[string]any{
		"id":          "handoff-noop",
		"createdAt":   "2026-03-01T12:00:00Z",
		"description": "Up to date",
	}
	data, _ := json.Marshal(handoff)
	if err := os.WriteFile(filepath.Join(dir, "inbox.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.ConsumeHandoff(); err != nil {
		t.Fatal(err)
	}
	if a.Settings.LastRefresh != last || a.Settings.LastHandoffID != "handoff-noop" {
		t.Fatalf("settings: %+v", a.Settings)
	}
	if a.LastRefreshMessage != "Up to date" {
		t.Fatalf("status: %q", a.LastRefreshMessage)
	}
}

func TestConsumeHandoffDoesNotRewindNewerRefresh(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	current := "2026-03-01T12:00:00Z"
	a.Settings.LastRefresh = current
	handoff := map[string]any{
		"id":        "handoff-old",
		"createdAt": "2026-03-01T10:00:00Z",
		"result":    core.RefreshResult{Changed: true, Description: "Refreshed 1 videos"},
	}
	data, _ := json.Marshal(handoff)
	if err := os.WriteFile(filepath.Join(dir, "inbox.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.ConsumeHandoff(); err != nil {
		t.Fatal(err)
	}
	if a.Settings.LastRefresh != current {
		t.Fatalf("last refresh rewound: %+v", a.Settings)
	}
}

func TestConsumeHandoffQuotaFailurePausesRefresh(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	handoff := map[string]any{
		"id":          "handoff-quota",
		"createdAt":   "2026-03-01T12:00:00Z",
		"description": "quota paused until 2026-03-02T12:00:00Z",
		"failures":    []core.RefreshFailure{{At: "2026-03-01T12:00:00Z", Message: "quota paused until 2026-03-02T12:00:00Z"}},
	}
	data, _ := json.Marshal(handoff)
	if err := os.WriteFile(filepath.Join(dir, "inbox.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.ConsumeHandoff(); err != nil {
		t.Fatal(err)
	}
	if a.Settings.QuotaPausedUntil != "2026-03-02T12:00:00Z" {
		t.Fatalf("quota pause: %+v", a.Settings)
	}
}

// TestConsumeHandoffDoesNotRearmPauseFromHistory proves a quota pause is
// armed only by the failure the refresh itself produced: a handoff whose
// current failure is an ordinary error keeps the earlier quota failure as
// history without arming a pause the refresh did not earn.
func TestConsumeHandoffDoesNotRearmPauseFromHistory(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	failures := []core.RefreshFailure{
		{At: "2026-03-02T12:00:00Z", Message: "Get \"https://www.googleapis.com/youtube/v3/channels\": dial tcp: no route to host"},
		{At: "2026-03-01T12:00:00Z", Message: "quota paused until 2026-03-02T12:00:00Z"},
	}
	handoff := map[string]any{
		"id":          "handoff-stale-quota",
		"createdAt":   "2026-03-02T12:00:00Z",
		"description": failures[0].Message,
		"failures":    failures,
	}
	data, _ := json.Marshal(handoff)
	if err := os.WriteFile(filepath.Join(dir, "inbox.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.ConsumeHandoff(); err != nil {
		t.Fatal(err)
	}
	if a.Settings.QuotaPausedUntil != "" {
		t.Fatalf("carried quota history re-armed a pause: %+v", a.Settings.QuotaPausedUntil)
	}
	if len(a.Settings.RecentFailures) != 2 ||
		a.Settings.RecentFailures[0].Message != failures[0].Message ||
		a.Settings.RecentFailures[1].Message != failures[1].Message {
		t.Fatalf("failures: %+v", a.Settings.RecentFailures)
	}
}

func TestRefreshNoopDoesNotAdvanceLastRefresh(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	last := time.Now().UTC().Format(time.RFC3339)
	a.Settings.APIKey = "local-key"
	a.Settings.LastRefresh = last
	a.Settings.RefreshMinutes = 240
	outcome := a.Refresh(false, a.refreshSnapshot())
	if outcome.Changed || outcome.Description != "Up to date" {
		t.Fatalf("outcome: %+v", outcome)
	}
	if a.Settings.LastRefresh != last {
		t.Fatalf("last refresh advanced on noop: %+v", a.Settings)
	}
}

// TestRefreshBlockedKeepsRecordedFailures proves a refresh the reader refuses
// for a quota pause or a missing key leaves the failure it reports in place,
// rather than clearing the very state the user is reading.
func TestRefreshBlockedKeepsRecordedFailures(t *testing.T) {
	cases := []struct {
		name  string
		block func(*core.Settings)
	}{
		{"quota pause", func(s *core.Settings) {
			s.QuotaPausedUntil = time.Now().Add(24 * time.Hour).Format(time.RFC3339)
		}},
		{"missing key", func(s *core.Settings) { s.APIKey = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			a := newTestApp(t, dir)
			defer a.Close()
			a.Settings.APIKey = "key"
			failure := core.RefreshFailure{
				At:      time.Now().UTC().Format(time.RFC3339),
				Message: "quota paused until 2026-03-02T12:00:00Z",
			}
			a.Settings.RecentFailures = []core.RefreshFailure{failure}
			tc.block(&a.Settings)
			outcome := a.Refresh(true, a.refreshSnapshot())
			if outcome.Changed {
				t.Fatalf("blocked refresh reported a change: %+v", outcome)
			}
			if len(a.Settings.RecentFailures) != 1 || a.Settings.RecentFailures[0].Message != failure.Message {
				t.Fatalf("blocked refresh cleared the failure: %+v", a.Settings.RecentFailures)
			}
			persisted := loadTestSettings(t, filepath.Join(dir, "settings.json"))
			if len(persisted.RecentFailures) != 1 || persisted.RecentFailures[0].Message != failure.Message {
				t.Fatalf("blocked refresh cleared the persisted failure: %+v", persisted.RecentFailures)
			}
		})
	}
}

func TestSaveSettingsPreservesNewerOnDiskStatus(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	a.Settings.APIKey = "local-key"
	a.Settings.LastRefresh = "2026-03-01T09:00:00Z"
	disk := core.Settings{
		APIKey:      "disk-key",
		SyncPath:    a.Settings.SyncPath,
		LastRefresh: "2026-03-01T12:00:00Z",
	}
	data, _ := json.Marshal(disk)
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.SaveSettings(); err != nil {
		t.Fatal(err)
	}
	loaded := loadTestSettings(t, filepath.Join(dir, "settings.json"))
	if loaded.APIKey != "local-key" || loaded.LastRefresh != "2026-03-01T12:00:00Z" {
		t.Fatalf("settings: %+v", loaded)
	}
}

func TestSaveSyncMergesNewerOnDiskState(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	a.DeviceID = "device"
	a.Sync = core.SyncState{
		SchemaVersion: 1,
		UpdatedAt:     10,
		Channels:      map[string]core.Channel{"local": {ID: "local", Title: "Local", UpdatedAt: 10}},
		ReadStates:    map[string]map[string]core.ReadRecord{},
		Favorites:     map[string]core.FavoriteRecord{},
	}
	remote := core.SyncState{
		SchemaVersion: 1,
		UpdatedAt:     100,
		Channels:      map[string]core.Channel{"remote": {ID: "remote", Title: "Remote", UpdatedAt: 100}},
		ReadStates:    map[string]map[string]core.ReadRecord{},
		Favorites:     map[string]core.FavoriteRecord{},
	}
	if err := core.SaveSyncAtomic(a.Settings.SyncPath, remote); err != nil {
		t.Fatal(err)
	}
	if err := a.SaveSync(); err != nil {
		t.Fatal(err)
	}
	loaded, err := core.LoadSyncWithConflicts(a.Settings.SyncPath, a.DeviceID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded.Channels["remote"]; !ok {
		t.Fatalf("remote channel lost: %+v", loaded.Channels)
	}
	if _, ok := loaded.Channels["local"]; !ok {
		t.Fatalf("local channel lost: %+v", loaded.Channels)
	}
}

func loadTestSettings(t *testing.T, path string) core.Settings {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var settings core.Settings
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	return settings
}

// TestRefreshFinishedAppliesRefreshedChannels checks the refreshed channel
// metadata a real refresh run produced reaches the sync state and the sync
// file, so the Notifications pane and other devices see the titles YouTube
// has now. It drives the whole path through RefreshYoutube, whose in-place
// channel writes must not corrupt the snapshot the writeback guard compares
// against: a guard fed the refresh's own output would discard every record
// the refresh updated.
func TestRefreshFinishedAppliesRefreshedChannels(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	a.Settings.APIKey = "test-key"
	a.Sync.Channels["ch-1"] = core.Channel{ID: "ch-1", Title: "Old title", UpdatedAt: 10}
	restore := stubYouTube(t, map[string]string{
		"/channels":      `{"items":[{"id":"ch-1","snippet":{"title":"New title"},"contentDetails":{"relatedPlaylists":{"uploads":"uploads-1"}}}]}`,
		"/playlistItems": `{"items":[]}`,
	})
	defer restore()
	w := &Window{app: a, selectedIndex: new(int)}
	snapshot := a.refreshSnapshot()
	outcome := a.Refresh(true, snapshot)
	if !outcome.Changed {
		t.Fatalf("refresh did not run: %+v", outcome)
	}
	w.refreshFinished(outcome, snapshot)
	if got := a.Sync.Channels["ch-1"].Title; got != "New title" {
		t.Fatalf("sync state kept stale title %q", got)
	}
	saved, err := core.LoadSyncWithConflicts(a.Settings.SyncPath, a.DeviceID)
	if err != nil {
		t.Fatal(err)
	}
	if got := saved.Channels["ch-1"].Title; got != "New title" {
		t.Fatalf("sync file kept stale title %q", got)
	}
}

// TestRefreshFinishedKeepsNewerImportedChannel checks a refresh that began
// before an OPML import finished does not write its stale snapshot back over
// the import's channel record, in memory or in the sync file, whether the
// import's stamp is newer than the refresh's or landed inside the window the
// refresh stamped later.
func TestRefreshFinishedKeepsNewerImportedChannel(t *testing.T) {
	for _, tc := range []struct {
		name         string
		refreshStamp int64
		importStamp  int64
	}{
		{"import stamped after the refresh", 20, 30},
		{"import stamped before the refresh", 30, 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			a := newTestApp(t, dir)
			defer a.Close()
			w := &Window{app: a, selectedIndex: new(int)}
			a.Sync.Channels["ch-1"] = core.Channel{ID: "ch-1", Title: "Old title", UpdatedAt: 10}
			snapshot := a.refreshSnapshot()
			a.Sync.Channels["ch-1"] = core.Channel{ID: "ch-1", Title: "Imported title", GroupIDs: []string{"g1", "g2"}, UpdatedAt: tc.importStamp}
			w.refreshFinished(RefreshOutcome{
				Changed:  true,
				Channels: []core.Channel{{ID: "ch-1", Title: "Fetched title", GroupIDs: []string{"g1"}, UpdatedAt: tc.refreshStamp}},
			}, snapshot)
			stored, ok := a.Sync.Channels["ch-1"]
			if !ok || stored.Title != "Imported title" || !slices.Equal(stored.GroupIDs, []string{"g1", "g2"}) {
				t.Fatalf("refresh rolled back the import: %+v", stored)
			}
			saved, err := core.LoadSyncWithConflicts(a.Settings.SyncPath, a.DeviceID)
			if err != nil {
				t.Fatal(err)
			}
			if got := saved.Channels["ch-1"]; got.Title != "Imported title" || !slices.Equal(got.GroupIDs, []string{"g1", "g2"}) {
				t.Fatalf("sync file rolled back the import: %+v", got)
			}
		})
	}
}

func TestDrainQueuedSyncWritesAfterHelper(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()

	queued := core.SyncState{
		SchemaVersion: 1,
		UpdatedAt:     123,
		Channels:      map[string]core.Channel{"ch-1": {ID: "ch-1", Title: "Channel", UpdatedAt: 123}},
		ReadStates:    map[string]map[string]core.ReadRecord{},
		Favorites:     map[string]core.FavoriteRecord{},
		Groups:        map[string]core.ChannelGroup{},
	}
	if err := a.Store.Queue(context.Background(), map[string]any{"state": queued}); err != nil {
		t.Fatal(err)
	}
	if err := a.DrainQueuedSyncWrites(); err != nil {
		t.Fatal(err)
	}
	if got := a.Sync.Channels["ch-1"].Title; got != "Channel" {
		t.Fatalf("channel = %+v", a.Sync.Channels)
	}
	saved, err := core.LoadSyncWithConflicts(a.Settings.SyncPath, a.DeviceID)
	if err != nil {
		t.Fatal(err)
	}
	if got := saved.Channels["ch-1"].Title; got != "Channel" {
		t.Fatalf("saved channel = %q", got)
	}
}

// groupNames lists the sidebar group names in order.
func groupNames(groups []core.ChannelGroup) []string {
	names := make([]string, 0, len(groups))
	for _, g := range groups {
		names = append(names, g.Name)
	}
	return names
}

// TestOrderGroupsLoadsStoredOrder covers how the sidebar orders the synced
// groups on load: the stored order wins exactly, a group the stored order does
// not name joins the end in name order, and a stored ID that names no group is
// ignored.
func TestOrderGroupsLoadsStoredOrder(t *testing.T) {
	groups := map[string]core.ChannelGroup{
		"group-a": {ID: "group-a", Name: "Alpha"},
		"group-b": {ID: "group-b", Name: "Bravo"},
		"group-c": {ID: "group-c", Name: "Charlie"},
	}
	cases := []struct {
		name   string
		stored []string
		want   []string
	}{
		{"stored order exactly", []string{"group-c", "group-a", "group-b"}, []string{"Charlie", "Alpha", "Bravo"}},
		{"missing groups appended by name", []string{"group-b"}, []string{"Bravo", "Alpha", "Charlie"}},
		{"stale IDs ignored", []string{"group-x", "group-b", "group-a"}, []string{"Bravo", "Alpha", "Charlie"}},
		{"no stored order", nil, []string{"Alpha", "Bravo", "Charlie"}},
	}
	for _, c := range cases {
		if got := groupNames(orderGroups(groups, c.stored)); !slices.Equal(got, c.want) {
			t.Errorf("%s: order = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestImportOPMLPathsKeepsDocumentOrder imports an OPML file whose groups are
// not in name order: the sidebar starts in the file's order, that order is
// saved locally, and a relaunch reads it back unchanged. A later import that
// adds groups appends them in its own file order and keeps the order the user
// already has.
func TestImportOPMLPathsKeepsDocumentOrder(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	writeOPML := func(name string, names ...string) string {
		t.Helper()
		var body []byte
		for _, n := range names {
			body = append(body, []byte(`<outline text="`+n+`"/>`)...)
		}
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, []byte(`<?xml version="1.0"?><opml><body>`+string(body)+`</body></opml>`), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}

	first := writeOPML("first.opml", "Zebra", "Alpha", "Mike")
	if err := a.ImportOPMLPaths([]string{first}); err != nil {
		t.Fatalf("first import: %v", err)
	}
	want := []string{"Zebra", "Alpha", "Mike"}
	if got := groupNames(a.Groups); !slices.Equal(got, want) {
		t.Fatalf("groups after import = %q, want the file's order %q", got, want)
	}

	// Relaunch: read settings.json the way LoadApp does and reload the same
	// synced groups.
	settings := defaultSettings()
	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatalf("settings file: %v", err)
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("settings decode: %v", err)
	}
	wantIDs := []string{core.StableGroupID("Zebra"), core.StableGroupID("Alpha"), core.StableGroupID("Mike")}
	if !slices.Equal(settings.GroupOrder, wantIDs) {
		t.Fatalf("saved group order = %q, want %q", settings.GroupOrder, wantIDs)
	}
	relaunched := &App{Dir: dir, Store: a.Store, Settings: settings, Sync: a.Sync}
	if err := relaunched.reload(); err != nil {
		t.Fatalf("relaunch reload: %v", err)
	}
	if got := groupNames(relaunched.Groups); !slices.Equal(got, want) {
		t.Fatalf("groups after relaunch = %q, want %q", got, want)
	}

	// A second import adds groups: they join the end in its file order, while
	// the groups already here keep their order.
	second := writeOPML("second.opml", "Bella", "Aaron")
	if err := a.ImportOPMLPaths([]string{second}); err != nil {
		t.Fatalf("second import: %v", err)
	}
	want = []string{"Zebra", "Alpha", "Mike", "Bella", "Aaron"}
	if got := groupNames(a.Groups); !slices.Equal(got, want) {
		t.Fatalf("groups after second import = %q, want %q", got, want)
	}
}
