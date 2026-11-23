package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon/youtube-reader/core"
)

// TestHelperNotificationSubprocessBoundary drives the helper's real osascript
// call through a disposable PATH shim while no app listens on its socket. The
// shim records what the helper asked the OS to do; no notification is posted
// and no app is launched. The script must be a plain display notification that
// never names the app bundle, so a background refresh cannot pull up a window.
func TestHelperNotificationSubprocessBoundary(t *testing.T) {
	shimDir := t.TempDir()
	logPath := filepath.Join(shimDir, "calls.log")
	shim := filepath.Join(shimDir, "osascript")
	if err := os.WriteFile(shim, []byte("#!/bin/sh\nprintf 'osascript %s\\n' \"$*\" >> \""+logPath+"\"\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", shimDir); err != nil {
		t.Fatal(err)
	}
	defer os.Setenv("PATH", oldPath)

	dir := t.TempDir()
	notificationTestRun(t, dir, []core.NotificationPlan{{ChannelID: "ch-demo", ChannelName: "Demo", Count: 2, NewestTitle: "New video"}}, postHelperNotifications)
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	calls := strings.Split(strings.TrimSpace(string(data)), "\n")
	want := `osascript -e display notification "2 new videos\nNew video" with title "Demo" subtitle "New videos" sound name "default"`
	if len(calls) != 1 || calls[0] != want {
		t.Fatalf("subprocess calls = %q, want [%q]", calls, want)
	}
}
