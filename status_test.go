package main

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
	mui "github.com/simon/youtube-reader/ui"
)

// statusShotBackdrop is the design board's own fitbox colour, the backdrop the
// screenshot harness paints a panel over so its edge and arrow read.
var statusShotBackdrop = color.RGBA{R: 0xe5, G: 0xe3, B: 0xdd, A: 0xff}

// statusFixtures are the states as the app derives them, one clock for them
// all so the copy in a failure names one instant. The data is realistic: 42
// channels, a refresh two minutes old, and a stored failure that carries the
// API's own words.
func statusFixtures(now time.Time) map[statusState]status {
	published := now.Add(-3 * time.Hour).Format(time.RFC3339)
	videos := make([]core.Video, 12)
	for i := range videos {
		videos[i] = core.Video{ID: "v", PublishedAt: published}
	}
	base := statusInput{
		Now:      now,
		Online:   true,
		Settings: core.Settings{APIKey: "key", RefreshMinutes: 60},
		Channels: 42,
		Videos:   videos,
	}

	upToDate := base
	upToDate.Settings.LastRefresh = now.Add(-2 * time.Minute).Format(time.RFC3339)
	upToDate.Settings.NextRefresh = now.Add(13 * time.Minute).Format(time.RFC3339)

	refreshing := upToDate
	refreshing.Refreshing = true
	refreshing.Started = now.Add(-25 * time.Second)
	refreshing.Settings.NextRefresh = now.Add(58 * time.Minute).Format(time.RFC3339)

	offline := base
	offline.Online = false
	offline.Settings.LastRefresh = now.Add(-2*time.Hour - 5*time.Minute).Format(time.RFC3339)
	offline.Settings.NextRefresh = now.Add(-time.Hour).Format(time.RFC3339)

	failed := upToDate
	failed.Settings.RecentFailures = []core.RefreshFailure{{
		At:      now.Add(-4 * time.Minute).Format(time.RFC3339),
		Message: "Alpha: Get \"https://www.googleapis.com/youtube/v3/playlistItems\": dial tcp: no route to host",
	}}

	notConfigured := base
	notConfigured.Settings.APIKey = ""
	notConfigured.Channels = 0
	notConfigured.Videos = nil

	return map[statusState]status{
		statusUpToDate:      deriveStatus(upToDate),
		statusRefreshing:    deriveStatus(refreshing),
		statusOffline:       deriveStatus(offline),
		statusFailed:        deriveStatus(failed),
		statusNotConfigured: deriveStatus(notConfigured),
		statusNotRefreshed:  deriveStatus(base),
	}
}

func TestRelativeTime(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.Local)
	tests := []struct {
		name string
		at   time.Time
		want string
	}{
		{"now", now, "just now"},
		{"under a minute", now.Add(-59 * time.Second), "just now"},
		{"one minute", now.Add(-time.Minute), "1 min ago"},
		{"minutes", now.Add(-5 * time.Minute), "5 min ago"},
		{"under an hour", now.Add(-59 * time.Minute), "59 min ago"},
		{"one hour", now.Add(-time.Hour), "1 hr ago"},
		{"hours", now.Add(-3 * time.Hour), "3 hr ago"},
		{"under a day", now.Add(-23 * time.Hour), "23 hr ago"},
		{"one day", now.Add(-24 * time.Hour), "1 day ago"},
		{"days", now.Add(-72 * time.Hour), "3 days ago"},
		{"a week", now.Add(-7 * 24 * time.Hour), "on 22 Feb 2026"},
		{"older", now.Add(-30 * 24 * time.Hour), "on 30 Jan 2026"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := relativeTime(now, tt.at); got != tt.want {
				t.Fatalf("relativeTime(%v) = %q, want %q", now.Sub(tt.at), got, tt.want)
			}
		})
	}
}

func TestRemainingTime(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.Local)
	tests := []struct {
		name string
		at   time.Time
		want string
	}{
		{"due", now.Add(-time.Hour), "now"},
		{"exactly now", now, "now"},
		{"under a minute", now.Add(40 * time.Second), "1 min"},
		{"a minute", now.Add(time.Minute), "1 min"},
		{"minutes", now.Add(13*time.Minute + 20*time.Second), "14 min"},
		{"under an hour", now.Add(59 * time.Minute), "59 min"},
		{"rounds up to the next hour", now.Add(59*time.Minute + 1*time.Second), "1 hr"},
		{"an hour", now.Add(time.Hour), "1 hr"},
		{"hours", now.Add(2 * time.Hour), "2 hr"},
		{"rounds up to the next day", now.Add(23*time.Hour + 30*time.Minute), "1 day"},
		{"days", now.Add(30 * time.Hour), "2 days"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := remainingTime(now, tt.at); got != tt.want {
				t.Fatalf("remainingTime(%v) = %q, want %q", tt.at.Sub(now), got, tt.want)
			}
		})
	}
}

// TestDeriveStatusCopy pins the states' copy and actions to the design board,
// and the cases around them: a refresh in flight wins over offline, a missing
// key and a never-refreshed reader are setup states rather than healthy or
// failed ones, a recorded failure quotes its own message, and a quota pause is
// held out of the popover as a warning while its resume time still shows.
func TestDeriveStatusCopy(t *testing.T) {
	now := time.Date(2026, 3, 1, 14, 32, 0, 0, time.Local)
	fixtures := statusFixtures(now)
	settings := core.Settings{
		APIKey:         "key",
		LastRefresh:    now.Add(-2 * time.Minute).Format(time.RFC3339),
		NextRefresh:    now.Add(13 * time.Minute).Format(time.RFC3339),
		RecentFailures: []core.RefreshFailure{},
	}
	tests := []struct {
		name            string
		in              statusInput
		state           statusState
		title           string
		body            string
		primary         statusAction
		secondary       statusAction
		primaryDisabled bool
		details         []string // "Label: Value" for every row, in order
	}{
		{
			name: "up to date",
			in: statusInput{Now: now, Online: true, Settings: settings, Channels: 42,
				Videos: []core.Video{{PublishedAt: now.Add(-2 * time.Hour).Format(time.RFC3339)}}},
			state:   statusUpToDate,
			title:   "Up to date",
			body:    "Last refresh 2 min ago",
			primary: statusRefreshNow, secondary: statusOpenSettings,
			details: []string{"Next refresh: 13 min", "Channels: 42", "New videos today: 1"},
		},
		{
			name: "never refreshed",
			in: statusInput{Now: now, Online: true, Channels: 0,
				Settings: core.Settings{APIKey: "key"}},
			state:   statusNotRefreshed,
			title:   "Not refreshed yet",
			body:    "Refresh to fetch your subscriptions and videos.",
			primary: statusRefreshNow, secondary: statusOpenSettings,
			details: []string{"Channels: 0"},
		},
		{
			name: "refreshing",
			in: statusInput{Now: now, Online: true, Refreshing: true, Started: now.Add(-25 * time.Second),
				Settings: settings, Channels: 42},
			state:   statusRefreshing,
			title:   "Refreshing",
			body:    "42 channels",
			primary: statusRefreshNow, secondary: statusNoAction,
			details: []string{"Started: 14:31", "Channels: 42"},
		},
		{
			name:    "offline",
			in:      statusInput{Now: now, Online: false, Settings: settings, Channels: 42},
			state:   statusOffline,
			title:   "Offline",
			body:    "Showing cached videos from 14:30",
			primary: statusRetry, secondary: statusOpenSettings,
			details: []string{"Last refresh: 2 min ago", "Channels: 42"},
		},
		{
			name:    "offline without a refresh",
			in:      statusInput{Now: now, Online: false, Settings: core.Settings{APIKey: "key"}, Channels: 42},
			state:   statusOffline,
			title:   "Offline",
			body:    "Showing cached videos",
			primary: statusRetry, secondary: statusOpenSettings,
			details: []string{"Channels: 42"},
		},
		{
			// The attempt the user started is what the surface reports.
			name: "offline during a refresh",
			in: statusInput{Now: now, Online: false, Refreshing: true, Started: now,
				Settings: settings, Channels: 42},
			state:   statusRefreshing,
			title:   "Refreshing",
			body:    "42 channels",
			primary: statusRefreshNow, secondary: statusNoAction,
			details: []string{"Started: 14:32", "Channels: 42"},
		},
		{
			name: "failed",
			in: statusInput{Now: now, Online: true, Channels: 42, Settings: core.Settings{
				APIKey:      "key",
				LastRefresh: now.Add(-2 * time.Minute).Format(time.RFC3339),
				RecentFailures: []core.RefreshFailure{{
					At:      now.Add(-4 * time.Minute).Format(time.RFC3339),
					Message: "Get \"https://www.googleapis.com/youtube/v3/channels\": dial tcp: no route to host",
				}},
			}},
			state:   statusFailed,
			title:   "Couldn't refresh subscriptions",
			body:    "Get \"https://www.googleapis.com/youtube/v3/channels\": dial tcp: no route to host",
			primary: statusRetry, secondary: statusOpenSettings,
			details: []string{"Failed: 4 min ago", "Channels: 42"},
		},
		{
			name: "rejected key",
			in: statusInput{Now: now, Online: true, Channels: 42, Settings: core.Settings{
				APIKey:      "key",
				LastRefresh: now.Add(-2 * time.Minute).Format(time.RFC3339),
				RecentFailures: []core.RefreshFailure{{
					At:      now.Add(-4 * time.Minute).Format(time.RFC3339),
					Message: "API key not valid. Please pass a valid API key.",
				}},
			}},
			state:   statusFailed,
			title:   "Couldn't refresh subscriptions",
			body:    "The YouTube API rejected the key. Check it in Settings, then try again.",
			primary: statusRetry, secondary: statusOpenSettings,
			details: []string{"Failed: 4 min ago", "Channels: 42"},
		},
		{
			name: "missing key",
			in: statusInput{Now: now, Online: true, Channels: 3,
				Settings: core.Settings{}},
			state:   statusNotConfigured,
			title:   "Set up YouTube Reader",
			body:    "Add a YouTube Data API key in Settings to get started.",
			primary: statusOpenSettings, secondary: statusNoAction,
			details: []string{"Channels: 3"},
		},
		{
			name: "quota paused",
			in: statusInput{Now: now, Online: true, Channels: 42, Settings: core.Settings{
				APIKey:           "key",
				LastRefresh:      now.Add(-2 * time.Minute).Format(time.RFC3339),
				NextRefresh:      now.Add(13 * time.Minute).Format(time.RFC3339),
				QuotaPausedUntil: now.Add(24 * time.Hour).Format(time.RFC3339),
			}},
			state:   statusUpToDate,
			title:   "Up to date",
			body:    "Last refresh 2 min ago",
			primary: statusRefreshNow, secondary: statusOpenSettings, primaryDisabled: true,
			details: []string{"Next refresh: Paused until 2 Mar, 14:32", "Channels: 42", "New videos today: 0"},
		},
		{
			name: "never refreshed while paused",
			in: statusInput{Now: now, Online: true, Channels: 3, Settings: core.Settings{
				APIKey:           "key",
				QuotaPausedUntil: now.Add(24 * time.Hour).Format(time.RFC3339),
			}},
			state:   statusNotRefreshed,
			title:   "Not refreshed yet",
			body:    "Refresh to fetch your subscriptions and videos.",
			primary: statusRefreshNow, secondary: statusOpenSettings, primaryDisabled: true,
			details: []string{"Next refresh: Paused until 2 Mar, 14:32", "Channels: 3"},
		},
		{
			name: "offline while paused",
			in: statusInput{Now: now, Online: false, Channels: 42, Settings: core.Settings{
				APIKey:           "key",
				LastRefresh:      now.Add(-2 * time.Minute).Format(time.RFC3339),
				QuotaPausedUntil: now.Add(24 * time.Hour).Format(time.RFC3339),
			}},
			state:   statusOffline,
			title:   "Offline",
			body:    "Showing cached videos from 14:30",
			primary: statusRetry, secondary: statusOpenSettings, primaryDisabled: true,
			details: []string{"Next refresh: Paused until 2 Mar, 14:32", "Last refresh: 2 min ago", "Channels: 42"},
		},
		{
			name: "stored failure redacts the key",
			in: statusInput{Now: now, Online: true, Channels: 42, Settings: core.Settings{
				APIKey:      "key",
				LastRefresh: now.Add(-2 * time.Minute).Format(time.RFC3339),
				RecentFailures: []core.RefreshFailure{{
					At:      now.Add(-4 * time.Minute).Format(time.RFC3339),
					Message: "Get \"https://www.googleapis.com/youtube/v3/channels?part=snippet&key=AIzaSyStored\": dial tcp: no route to host",
				}},
			}},
			state:   statusFailed,
			title:   "Couldn't refresh subscriptions",
			body:    "Get \"https://www.googleapis.com/youtube/v3/channels?part=snippet&key=[redacted]\": dial tcp: no route to host",
			primary: statusRetry, secondary: statusOpenSettings,
			details: []string{"Failed: 4 min ago", "Channels: 42"},
		},
		{
			name: "refreshing while paused",
			in: statusInput{Now: now, Online: true, Refreshing: true, Started: now.Add(-25 * time.Second),
				Channels: 42, Settings: core.Settings{
					APIKey:           "key",
					LastRefresh:      now.Add(-2 * time.Minute).Format(time.RFC3339),
					QuotaPausedUntil: now.Add(24 * time.Hour).Format(time.RFC3339),
				}},
			state:   statusRefreshing,
			title:   "Refreshing",
			body:    "42 channels",
			primary: statusRefreshNow, secondary: statusNoAction, primaryDisabled: true,
			details: []string{"Next refresh: Paused until 2 Mar, 14:32", "Started: 14:31", "Channels: 42"},
		},
		{
			name: "quota failure",
			in: statusInput{Now: now, Online: true, Channels: 42, Settings: core.Settings{
				APIKey:      "key",
				LastRefresh: now.Add(-2 * time.Minute).Format(time.RFC3339),
				RecentFailures: []core.RefreshFailure{{
					At:      now.Add(-4 * time.Minute).Format(time.RFC3339),
					Message: "quota paused until 2026-03-02T14:32:00Z",
				}},
				QuotaPausedUntil: now.Add(24 * time.Hour).Format(time.RFC3339),
			}},
			state:   statusFailed,
			title:   "Couldn't refresh subscriptions",
			body:    "YouTube API quota reached. Refresh resumes on 2 Mar, 14:32.",
			primary: statusNoAction, secondary: statusOpenSettings,
			details: []string{"Failed: 4 min ago", "Channels: 42"},
		},
		{
			name: "quota failure after the pause",
			in: statusInput{Now: now, Online: true, Channels: 42, Settings: core.Settings{
				APIKey:      "key",
				LastRefresh: now.Add(-2 * time.Minute).Format(time.RFC3339),
				RecentFailures: []core.RefreshFailure{{
					At:      now.Add(-4 * time.Minute).Format(time.RFC3339),
					Message: "quota paused until 2026-03-01T00:00:00Z",
				}},
				QuotaPausedUntil: now.Add(-time.Hour).Format(time.RFC3339),
			}},
			state:   statusFailed,
			title:   "Couldn't refresh subscriptions",
			body:    "YouTube API quota reached. Retry to refresh again.",
			primary: statusRetry, secondary: statusOpenSettings,
			details: []string{"Failed: 4 min ago", "Channels: 42"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deriveStatus(tt.in)
			if got.State != tt.state {
				t.Fatalf("state = %v, want %v", got.State, tt.state)
			}
			if got.Title != tt.title || got.Body != tt.body {
				t.Fatalf("copy = %q / %q, want %q / %q", got.Title, got.Body, tt.title, tt.body)
			}
			if got.Primary != tt.primary || got.Secondary != tt.secondary {
				t.Fatalf("actions = %v / %v, want %v / %v", got.Primary, got.Secondary, tt.primary, tt.secondary)
			}
			if got.PrimaryDisabled != tt.primaryDisabled {
				t.Fatalf("primary disabled = %v, want %v", got.PrimaryDisabled, tt.primaryDisabled)
			}
			var details []string
			for _, d := range got.Details {
				details = append(details, d.Label+": "+d.Value)
			}
			if strings.Join(details, " · ") != strings.Join(tt.details, " · ") {
				t.Fatalf("details = %q, want %q", details, tt.details)
			}
			wantPaused := tt.name == "quota paused" || tt.name == "quota failure" || tt.name == "refreshing while paused" ||
				tt.name == "never refreshed while paused" || tt.name == "offline while paused"
			if paused := !got.PausedUntil.IsZero(); paused != wantPaused {
				t.Fatalf("paused = %v, want %v", paused, wantPaused)
			}
			// Every state names itself in words, and its action labels follow.
			if statusActionLabel(tt.primary, tt.state) == "" && tt.primary != statusNoAction {
				t.Fatal("the primary action has no label")
			}
			if statusActionLabel(tt.secondary, tt.state) == "" && tt.secondary != statusNoAction {
				t.Fatal("the secondary action has no label")
			}
		})
	}
	// The item's own title keeps offline, failure and setup legible without
	// colour, and says nothing while the reader works.
	if got := statusItemTitle(fixtures[statusOffline].State); got != "Offline" {
		t.Fatalf("offline item title = %q", got)
	}
	if got := statusItemTitle(fixtures[statusFailed].State); got != "Refresh failed" {
		t.Fatalf("failed item title = %q", got)
	}
	if got := statusItemTitle(fixtures[statusNotConfigured].State); got != "Set up" {
		t.Fatalf("not-configured item title = %q", got)
	}
	if got := statusItemTitle(fixtures[statusNotRefreshed].State); got != "Not refreshed" {
		t.Fatalf("not-refreshed item title = %q", got)
	}
	for _, state := range []statusState{statusUpToDate, statusRefreshing} {
		if got := statusItemTitle(state); got != "" {
			t.Fatalf("state %v shows the item title %q", state, got)
		}
	}
	// The action labels are the board's words, in the state's voice.
	if got := statusActionLabel(statusRetry, statusOffline); got != "Retry" {
		t.Fatalf("offline retry label = %q", got)
	}
	if got := statusActionLabel(statusOpenSettings, statusFailed); got != "Open Settings…" {
		t.Fatalf("failed settings label = %q", got)
	}
	if got := statusActionLabel(statusOpenSettings, statusNotConfigured); got != "Open Settings…" {
		t.Fatalf("not-configured settings label = %q", got)
	}
	if got := statusActionLabel(statusOpenSettings, statusUpToDate); got != "Settings…" {
		t.Fatalf("healthy settings label = %q", got)
	}
	// The warning a pause raises is an alert of its own, with the resume time.
	title, message, detail := pausedAlert(time.Date(2026, 3, 2, 14, 32, 0, 0, time.Local))
	if title != "Refresh paused" {
		t.Fatalf("paused alert title = %q", title)
	}
	if !strings.Contains(message, "quota") || !strings.Contains(detail, "2 Mar at 14:32") {
		t.Fatalf("paused alert = %q / %q", message, detail)
	}
	// The item's hover text repeats the popover's own copy.
	if got := statusTooltip(fixtures[statusOffline]); got != "Offline - Showing cached videos from 12:27" {
		t.Fatalf("tooltip = %q", got)
	}
}

// TestStatusPausedPrimaryIsDisabled proves the popover's own button follows the
// model: a refresh action the pause makes impossible takes no input, while an
// ordinary refresh action still runs.
func TestStatusPausedPrimaryIsDisabled(t *testing.T) {
	now := time.Date(2026, 3, 1, 14, 32, 0, 0, time.Local)
	paused := deriveStatus(statusInput{Now: now, Online: true, Channels: 42, Settings: core.Settings{
		APIKey:           "key",
		LastRefresh:      now.Add(-2 * time.Minute).Format(time.RFC3339),
		QuotaPausedUntil: now.Add(24 * time.Hour).Format(time.RFC3339),
	}})
	called := false
	tt := ui.NewTester(statusPanelView(paused, statusActions{refresh: func() { called = true }}, -1),
		int(statusPopoverWidth), int(statusPopoverMaxHeight))
	tt.Frame()
	button := accessNamed(tt, "Refresh now")
	if button == nil || !button.Disabled {
		t.Fatalf("paused primary = %+v, want a disabled Refresh now", button)
	}
	if err := tt.Click("Refresh now"); err == nil && called {
		t.Fatal("a disabled primary started a refresh")
	}

	healthy := deriveStatus(statusInput{Now: now, Online: true, Channels: 42, Settings: core.Settings{
		APIKey:      "key",
		LastRefresh: now.Add(-2 * time.Minute).Format(time.RFC3339),
	}})
	called = false
	tt = ui.NewTester(statusPanelView(healthy, statusActions{refresh: func() { called = true }}, -1),
		int(statusPopoverWidth), int(statusPopoverMaxHeight))
	tt.Frame()
	if err := tt.Click("Refresh now"); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("the healthy primary did not refresh")
	}
}

// TestStoredFailureRedactsAPIKey proves a message already on disk from before
// the write-side redaction still cannot show the key in the popover or the
// item's hover text.
func TestStoredFailureRedactsAPIKey(t *testing.T) {
	const key = "AIzaSyD-stored-failure-key"
	now := time.Date(2026, 3, 1, 14, 32, 0, 0, time.Local)
	m := deriveStatus(statusInput{Now: now, Online: true, Channels: 1, Settings: core.Settings{
		APIKey:      "key",
		LastRefresh: now.Add(-2 * time.Minute).Format(time.RFC3339),
		RecentFailures: []core.RefreshFailure{{
			At:      now.Add(-4 * time.Minute).Format(time.RFC3339),
			Message: "Get \"https://www.googleapis.com/youtube/v3/channels?part=snippet&key=" + key + "\": dial tcp: no route to host",
		}},
	}})
	if strings.Contains(m.Body, key) || strings.Contains(statusTooltip(m), key) {
		t.Fatalf("stored failure leaks the key: body=%q tooltip=%q", m.Body, statusTooltip(m))
	}
	if !strings.Contains(m.Body, "key=[redacted]") {
		t.Fatalf("stored failure lost its redaction: %q", m.Body)
	}
}

func accessNamed(tt *ui.Tester, name string) *ui.AccessInfo {
	for _, info := range tt.AccessAll() {
		if info.Name == name {
			return &info
		}
	}
	return nil
}

// TestStatusPopoverCap proves the 420 pt cap: a panel whose rows exceed it
// stops at the cap and scrolls, while a real state stays under it and takes
// only the height its content needs.
func TestStatusPopoverCap(t *testing.T) {
	now := time.Date(2026, 3, 1, 14, 32, 0, 0, time.Local)
	fixtures := statusFixtures(now)
	real := fixtures[statusUpToDate]
	tall := real
	tall.Details = nil
	for i := 0; i < 40; i++ {
		tall.Details = append(tall.Details, statusDetail{Symbol: "clock", Label: "Row", Value: "1 min"})
	}
	measure := func(m status) float32 {
		return panelHeight(t, m)
	}
	if got := measure(real); got <= 0 || got >= statusPopoverMaxHeight {
		t.Fatalf("a three-row panel is %v pt tall, want more than 0 and under the cap", got)
	}
	if got := measure(tall); got != statusPopoverMaxHeight {
		t.Fatalf("a forty-row panel is %v pt tall, want the %v pt cap", got, statusPopoverMaxHeight)
	}
}

// TestStatusItemIconSet proves the menu bar ships one template image per state,
// each a 2x drawing for the 18 pt item and never full-colour art.
func TestStatusItemIconSet(t *testing.T) {
	states := []statusState{
		statusUpToDate, statusRefreshing, statusOffline, statusFailed,
		statusNotConfigured, statusNotRefreshed,
	}
	seen := map[string]statusState{}
	for _, state := range states {
		data := statusIcon(state)
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("state %v: %v", state, err)
		}
		bounds := img.Bounds()
		if bounds.Dx() != 36 || bounds.Dy() != 36 {
			t.Fatalf("state %v is %dx%d, want 36x36 (18 pt at 2x)", state, bounds.Dx(), bounds.Dy())
		}
		// A template image is ink on transparency: no pixel may carry a hue.
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				r, g, b, _ := img.At(x, y).RGBA()
				if r>>8 != g>>8 || g>>8 != b>>8 {
					t.Fatalf("state %v has a colour at %d,%d", state, x, y)
				}
			}
		}
		// The glyph must sit inside the canvas: ink on an edge pixel means the
		// drawing was clipped by the bitmap.
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			for _, y := range []int{bounds.Min.Y, bounds.Max.Y - 1} {
				if _, _, _, a := img.At(x, y).RGBA(); a != 0 {
					t.Fatalf("state %v ink touches the canvas edge at %d,%d", state, x, y)
				}
			}
		}
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for _, x := range []int{bounds.Min.X, bounds.Max.X - 1} {
				if _, _, _, a := img.At(x, y).RGBA(); a != 0 {
					t.Fatalf("state %v ink touches the canvas edge at %d,%d", state, x, y)
				}
			}
		}
		seen[string(data)] = state
	}
	if len(seen) != len(states) {
		t.Fatalf("the %d states share %d images, want one distinct shape each", len(states), len(seen))
	}
}

// TestStatusScreenshots renders every state in both appearances and writes the
// PNGs under MYGO_STATUS_SCREENSHOTS. It is the shipping view's own pixels at
// 2x, not a mockup, so the review reads what the popover shows.
func TestStatusScreenshots(t *testing.T) {
	dir := os.Getenv("MYGO_STATUS_SCREENSHOTS")
	if dir == "" {
		t.Skip("MYGO_STATUS_SCREENSHOTS is unset")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 3, 1, 14, 32, 0, 0, time.Local)
	fixtures := statusFixtures(now)
	names := map[statusState]string{
		statusUpToDate:      "up-to-date",
		statusRefreshing:    "refreshing",
		statusOffline:       "offline",
		statusFailed:        "failed",
		statusNotConfigured: "not-configured",
		statusNotRefreshed:  "not-refreshed",
	}
	for _, dark := range []bool{false, true} {
		appearance := "light"
		if dark {
			appearance = "dark"
		}
		for state, name := range names {
			m := fixtures[state]
			tt := ui.NewTester(statusPanelView(m, statusActions{}, statusPopoverWidth/2),
				int(statusPopoverWidth), int(statusPopoverMaxHeight+statusArrowStrip)+200)
			tt.SetDark(dark)
			tt.SetScale(2)
			tt.Frame()
			// The tester's clock stands still, so the token colour
			// transitions would keep the first frame's light faces: move it
			// past the fade before capturing.
			tt.Advance(mui.MotionInteraction + 20*time.Millisecond)
			tt.Frame()
			// The tester draws at 2x, so the panel's pixels are its points
			// doubled; composite them onto the board's own presentation
			// backdrop, which lets the panel's edge and arrow read in both
			// appearances.
			rect := panelBounds(t, tt)
			shot := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
			draw.Draw(shot, shot.Bounds(), image.NewUniform(statusShotBackdrop), image.Point{}, draw.Src)
			draw.Draw(shot, shot.Bounds(), tt.Image(), rect.Min, draw.Over)
			var buf bytes.Buffer
			if err := png.Encode(&buf, shot); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "status-"+name+"-"+appearance+".png")
			if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Logf("wrote %s (%d x %d px, %v x %v pt)", path, shot.Bounds().Dx(), shot.Bounds().Dy(), rect.Dx()/2, rect.Dy()/2)
		}
	}
}

// TestSidebarHasNoStatusRow proves the move left nothing of the in-window
// status surface behind: the sidebar shows navigation only, so no state of the
// app can be drawn twice.
func TestSidebarHasNoStatusRow(t *testing.T) {
	a := sidebarTestApp(t, true)
	a.Settings.RecentFailures = []core.RefreshFailure{{At: time.Now().Format(time.RFC3339), Message: "boom"}}
	a.Settings.LastRefresh = time.Now().Format(time.RFC3339)
	tt := sidebarTester(t, a)
	if _, ok := tt.Find("Status"); ok {
		t.Fatal("the sidebar still draws a Status row")
	}
	if tt.HasText("Status") {
		t.Fatalf("the sidebar still shows status text: %q", tt.Texts())
	}
	for _, text := range tt.Texts() {
		if strings.Contains(text, "Last:") || strings.Contains(text, "Next:") || strings.Contains(text, "Refreshed") {
			t.Fatalf("the sidebar still shows a refresh stamp: %q", text)
		}
	}
	// The window chrome repeats none of the menu bar item's copy either.
	chrome := ui.NewTester(func(c *ui.Context) {
		(&Window{app: a, selectedIndex: new(int)}).shell(c)
	}, 900, 700)
	chrome.Frame()
	for _, text := range chrome.Texts() {
		if strings.Contains(text, "Refresh issue") || strings.Contains(text, "Refreshing") || strings.Contains(text, "Refreshed") {
			t.Fatalf("the window chrome still shows status copy: %q", text)
		}
	}
}

// panelBounds renders the panel of one model in a window taller than the cap
// and returns the box its pixels occupy: everything that differs from the
// window background the tester leaves around it. It measures the shipping
// view, so a cap it stops at is the cap the popover takes.
func panelBounds(t *testing.T, tt *ui.Tester) image.Rectangle {
	t.Helper()
	tt.Frame()
	img := tt.Image()
	bounds := img.Bounds()
	background := img.At(bounds.Min.X, bounds.Max.Y-1)
	same := func(x, y int) bool {
		r, g, b, a := img.At(x, y).RGBA()
		br, bg, bb, ba := background.RGBA()
		return absDiff(int(r>>8), int(br>>8)) <= 2 &&
			absDiff(int(g>>8), int(bg>>8)) <= 2 &&
			absDiff(int(b>>8), int(bb>>8)) <= 2 &&
			absDiff(int(a>>8), int(ba>>8)) <= 2
	}
	var box image.Rectangle
	found := false
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if same(x, y) {
				continue
			}
			if !found {
				box = image.Rect(x, y, x+1, y+1)
				found = true
				continue
			}
			if x < box.Min.X {
				box.Min.X = x
			}
			if y < box.Min.Y {
				box.Min.Y = y
			}
			if x >= box.Max.X {
				box.Max.X = x + 1
			}
			if y >= box.Max.Y {
				box.Max.Y = y + 1
			}
		}
	}
	if !found {
		t.Fatal("the panel painted nothing")
	}
	return box
}

// panelHeight is the height the popover takes for one model, in points.
func panelHeight(t *testing.T, m status) float32 {
	t.Helper()
	tt := ui.NewTester(statusPanelView(m, statusActions{}, -1), int(statusPopoverWidth), int(statusPopoverMaxHeight)+200)
	return float32(panelBounds(t, tt).Dy())
}
