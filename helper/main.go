package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/simon/youtube-reader/core"
)

const notificationBundleID = "com.simon.youtubereader"

// HelperHandoff is the atomic JSON message the helper leaves for the app.
type HelperHandoff struct {
	ID          string                  `json:"id"`
	CreatedAt   string                  `json:"createdAt"`
	Description string                  `json:"description"`
	Result      *core.RefreshResult     `json:"result,omitempty"`
	Notified    []core.NotificationPlan `json:"notified,omitempty"`
	Failures    []core.RefreshFailure   `json:"failures,omitempty"`
	Channels    []core.Channel          `json:"channels,omitempty"`
}

// HelperRun is the injectable core of one helper invocation. It reads local
// settings and shared state, checks the network, performs the same refresh as
// the app, commits SQLite in one transaction, queues shared JSON writes while
// the app holds the flock, posts notifications and writes an atomic handoff.
func HelperRun(ctx context.Context, dir string, dryRun bool, now time.Time, post func(core.NotificationPlan) error) (HelperHandoff, error) {
	handoff := HelperHandoff{ID: fmt.Sprintf("helper-%d", now.UnixNano()), CreatedAt: core.FormatRFC3339(now)}
	var settings core.Settings
	if data, err := os.ReadFile(filepath.Join(dir, "settings.json")); err == nil {
		_ = json.Unmarshal(data, &settings)
	}
	if settings.RefreshMinutes <= 0 {
		settings.RefreshMinutes = 60
	}
	if settings.SyncPath == "" {
		settings.SyncPath = filepath.Join(dir, "youtube-reader-sync.json")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return handoff, err
	}
	writeHandoff := func() error {
		data, err := json.MarshalIndent(handoff, "", "  ")
		if err != nil {
			return err
		}
		return atomicWriteHelper(filepath.Join(dir, "inbox.json"), data)
	}

	if dryRun {
		handoff.Description = "Dry run: no refresh performed"
		return handoff, writeHandoff()
	}
	if settings.APIKey == "" {
		handoff.Description = "Add a YouTube API key in Settings to refresh."
		return handoff, writeHandoff()
	}
	if !core.NetworkAvailable(ctx) {
		handoff.Description = "Offline: YouTube is unreachable"
		return handoff, writeHandoff()
	}

	state, err := core.LoadSyncWithConflicts(settings.SyncPath, core.StableDeviceID())
	if err != nil {
		return handoff, err
	}
	store, err := core.OpenStore(ctx, filepath.Join(dir, "videos.sqlite"))
	if err != nil {
		return handoff, err
	}
	existing, err := store.Videos(ctx)
	if err != nil {
		store.Close()
		return handoff, err
	}
	var revision int64
	if raw := store.Meta(ctx, "revision"); raw != "" {
		_, _ = fmt.Sscan(raw, &revision)
	}
	client := core.YouTube{Key: settings.APIKey, Client: &http.Client{Timeout: 30 * time.Second}}
	result := core.RefreshYoutube(ctx, client, settings, state, store, existing, revision, false, now)
	store.Close()
	handoff.Description = result.Description
	handoff.Failures = result.Failures
	if result.Changed {
		handoff.Result = &result
		handoff.Notified = result.Notified
		handoff.Channels = result.Channels
	}

	// Save settings even when the refresh did nothing: failures and quota
	// pauses must be visible in the app on the next launch.
	settings.LastRefresh = core.FormatRFC3339(now)
	settings.NextRefresh = core.FormatRFC3339(now.Add(time.Duration(settings.RefreshMinutes) * time.Minute))
	settings.RecentFailures = result.Failures
	settings.LastHandoffID = handoff.ID
	if containsQuotaFailure(result.Failures) {
		settings.QuotaPausedUntil = core.FormatRFC3339(now.Add(24 * time.Hour))
	}
	if err := writeSettings(dir, settings); err != nil {
		return handoff, err
	}

	// Channel metadata is shared state. While the app holds the local flock
	// it is queued in SQLite and drained on the app's next launch.
	if err := queueOrSaveState(ctx, dir, settings, state, result.Channels); err != nil {
		return handoff, err
	}

	if result.Changed {
		for _, plan := range result.Notified {
			if post == nil {
				continue
			}
			if err := post(plan); err != nil {
				log.Printf("notification: %v", err)
			}
		}
	}
	return handoff, writeHandoff()
}

func containsQuotaFailure(failures []core.RefreshFailure) bool {
	for _, f := range failures {
		if strings.HasPrefix(f.Message, "quota paused") {
			return true
		}
	}
	return false
}

func writeSettings(dir string, settings core.Settings) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteHelper(filepath.Join(dir, "settings.json"), data)
}

// queueOrSaveState writes fresh channel metadata to the Syncthing JSON only
// when the app does not hold the local flock. Otherwise it queues the state
// in SQLite for the app to merge and save at next launch. Force takeover,
// used from the app UI, keeps working because the app holds the lock itself.
func queueOrSaveState(ctx context.Context, dir string, settings core.Settings, state core.SyncState, channels []core.Channel) error {
	if len(channels) == 0 {
		return nil
	}
	lockDir := localLockDirFor(settings.SyncPath)
	lock, lockErr := core.TryLockDir(lockDir)
	mergeChannels := func() {
		for _, ch := range channels {
			if old, ok := state.Channels[ch.ID]; !ok || ch.UpdatedAt > old.UpdatedAt {
				state.Channels[ch.ID] = ch
			}
		}
		state.UpdatedAt = time.Now().UnixMilli()
	}
	if lockErr == nil {
		defer lock.Release()
		mergeChannels()
		return core.SaveSyncAtomic(settings.SyncPath, state)
	}
	if !errors.Is(lockErr, os.ErrExist) && !isWouldBlock(lockErr) {
		return lockErr
	}
	store, err := core.OpenStore(ctx, filepath.Join(dir, "videos.sqlite"))
	if err != nil {
		return err
	}
	defer store.Close()
	mergeChannels()
	return store.Queue(ctx, map[string]any{"kind": "sync-state", "state": state})
}

// localLockDirFor mirrors the app's lock location, including its default
// path, without depending on main.go code that is out of this task's scope.
func localLockDirFor(syncPath string) string {
	home, _ := os.UserHomeDir()
	defaultDir := filepath.Join(home, "Library", "Application Support", "YouTubeReader")
	if filepath.Clean(syncPath) == filepath.Join(defaultDir, "youtube-reader-sync.json") {
		return filepath.Join(home, "Library", "Application Support", "YouTubeReaderLocalLock")
	}
	return filepath.Join(filepath.Dir(syncPath), "YouTubeReaderLocalLock")
}

func isWouldBlock(err error) bool {
	return errors.Is(err, syscall.EWOULDBLOCK)
}

func postHelperNotification(plan core.NotificationPlan) error {
	body := fmt.Sprintf("%d new video%s", plan.Count, plural(plan.Count))
	if plan.NewestTitle != "" {
		body += "\n" + plan.NewestTitle
	}
	if runtime.GOOS != "darwin" {
		return nil
	}
	// A standalone helper is not an app bundle, so UNUserNotificationCenter
	// attributes notifications to the process. Ask the app bundle to display
	// them so they appear under the app's notification settings and keep the
	// same per-channel identity used while the app is running.
	scriptBody := strings.ReplaceAll(body, "\\", "\\\\")
	scriptBody = strings.ReplaceAll(scriptBody, "\"", "\\\"")
	script := fmt.Sprintf("display notification \"%s\" with title \"%s\" subtitle \"New videos\" sound name \"default\"", scriptBody, escapeAppleScript(plan.ChannelName))
	cmd := exec.Command("osascript", "-e", fmt.Sprintf("tell application id \"%s\" to %s", notificationBundleID, script))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("osascript: %w: %s", err, out)
	}
	return nil
}

func escapeAppleScript(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	return strings.ReplaceAll(s, "\"", "\\\"")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func main() {
	dataDir := flag.String("data-dir", "", "application data directory")
	dryRun := flag.Bool("dry-run", false, "write a handoff without calling YouTube")
	flag.Parse()
	dir := *dataDir
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, "Library", "Application Support", "YouTubeReader")
	}
	if _, err := HelperRun(context.Background(), dir, *dryRun, time.Now(), postHelperNotification); err != nil {
		log.Fatal(err)
	}
}

func atomicWriteHelper(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".helper-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
