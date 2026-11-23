package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/simon/youtube-reader/core"
)

// TestHelperNotificationSubprocessBoundary drives the helper's real
// osascript and pgrep calls through disposable PATH shims. The shims record
// what the helper asked the OS to do; no notification is posted and no app is
// launched. With the app closed the script must be a plain display
// notification, and with it running the same script must be targeted at the
// bundle and queued as click-routable.
func TestHelperNotificationSubprocessBoundary(t *testing.T) {
	shimDir := t.TempDir()
	logPath := filepath.Join(shimDir, "calls.log")
	shim := func(name, body string) {
		path := filepath.Join(shimDir, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	shim("osascript", "printf 'osascript %s\\n' \"$*\" >> \""+logPath+"\"\nexit 0\n")
	shim("pgrep", "printf 'pgrep %s\\n' \"$*\" >> \""+logPath+"\"\nexit \"$PGREP_EXIT\"\n")

	oldPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", shimDir); err != nil {
		t.Fatal(err)
	}
	defer os.Setenv("PATH", oldPath)

	oldRefresh, oldNetwork := refreshForHelper, networkAvailableForHelper
	refreshForHelper = func(ctx context.Context, client core.YouTube, settings core.Settings, state core.SyncState, store *core.Store, existing []core.Video, revision int64, force bool, at time.Time) core.RefreshResult {
		return core.RefreshResult{Changed: true, Notified: []core.NotificationPlan{{ChannelID: "ch-demo", ChannelName: "Demo", Count: 2, NewestTitle: "New video"}}}
	}
	networkAvailableForHelper = func(ctx context.Context) bool { return true }
	defer func() { refreshForHelper, networkAvailableForHelper = oldRefresh, oldNetwork }()

	for _, tc := range []struct {
		name     string
		pgrepOut string
		wantLine string
		wantLink bool
	}{
		{"app closed", "1", "osascript -e display notification \"2 new videos\\nNew video\" with title \"Demo\" subtitle \"New videos\" sound name \"default\"", false},
		{"app running", "0", "osascript -e tell application id \"com.simon.youtubereader\" to display notification \"2 new videos\\nNew video\" with title \"Demo\" subtitle \"New videos\" sound name \"default\"", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := writeSettings(dir, core.Settings{APIKey: "key", RefreshMinutes: 60, SyncPath: filepath.Join(dir, "sync.json")}); err != nil {
				t.Fatal(err)
			}
			if err := os.Setenv("PGREP_EXIT", tc.pgrepOut); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(logPath); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			h, err := HelperRun(context.Background(), dir, false, time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC), postHelperNotification)
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			calls := strings.Split(strings.TrimSpace(string(data)), "\n")
			if len(calls) != 2 || !strings.Contains(calls[0], "pgrep -u "+strconv.Itoa(os.Getuid())+" -x YouTube Reader") || calls[1] != tc.wantLine {
				t.Fatalf("subprocess calls = %q, want pgrep for the current user then %q", calls, tc.wantLine)
			}
			if got := len(h.Clicks); (got > 0) != tc.wantLink {
				t.Fatalf("click queue entries = %d, want click-routable = %v", got, tc.wantLink)
			}
		})
	}
}
