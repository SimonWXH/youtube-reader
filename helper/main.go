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

// Injected for tests so HelperRun's notification side effect can be verified
// without network or a real refresh.
type refreshFunc func(context.Context, core.YouTube, core.Settings, core.SyncState, *core.Store, []core.Video, int64, bool, time.Time) core.RefreshResult

var refreshForHelper refreshFunc = core.RefreshYoutube
var networkAvailableForHelper = core.NetworkAvailable

// HelperHandoff is the atomic JSON message the helper leaves for the app.
type HelperHandoff struct {
	ID          string                   `json:"id"`
	CreatedAt   string                   `json:"createdAt"`
	Description string                   `json:"description"`
	Result      *core.RefreshResult      `json:"result,omitempty"`
	Notified    []core.NotificationPlan  `json:"notified,omitempty"`
	Failures    []core.RefreshFailure    `json:"failures,omitempty"`
	Channels    []core.Channel           `json:"channels,omitempty"`
	Clicks      []core.NotificationClick `json:"clicks,omitempty"`
}

// notificationClicksPath is local app state: AppleScript notifications have
// no click ID, so the helper records their channel and post time here for the
// click callback to consume. It is never synced or sent to any service.
func notificationClicksPath(dir string) string {
	return filepath.Join(dir, "notification-clicks.json")
}

// queueNotificationClicks appends one entry per notification the helper
// actually posted and keeps the file atomic and private. A repeated post at
// the same second is a duplicate and does not create a second record.
func queueNotificationClicks(dir string, plans []core.NotificationPlan, at time.Time) error {
	path := notificationClicksPath(dir)
	queue := core.NotificationClicks{}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &queue); err != nil {
			return fmt.Errorf("parse notification click queue: %w", err)
		}
		if err := queue.Validate(); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	known := make(map[string]bool, len(queue.Clicks))
	for _, click := range queue.Clicks {
		known[core.NotificationClickKey(click)] = true
	}
	for _, click := range core.NewNotificationClicks(plans, at) {
		key := core.NotificationClickKey(click)
		if !known[key] {
			queue.Clicks = append(queue.Clicks, click)
			known[key] = true
		}
	}
	data, err := json.MarshalIndent(queue, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteHelper(path, data)
}

func loadHelperSettings(dir string) core.Settings {
	var settings core.Settings
	if data, err := os.ReadFile(filepath.Join(dir, "settings.json")); err == nil {
		_ = json.Unmarshal(data, &settings)
	}
	settings.APIKey = strings.TrimSpace(settings.APIKey)
	// A zero interval is Manual: no automatic refresh, so a helper launched by
	// a stale login item must do nothing. Keep it as settings.json has it.
	if settings.SyncPath == "" {
		settings.SyncPath = filepath.Join(dir, "youtube-reader-sync.json")
	}
	return settings
}

// HelperRun is the injectable core of one helper invocation. It reads local
// settings and shared state, checks the network, performs the same refresh as
// the app, commits SQLite in one transaction, queues shared JSON writes while
// the app holds the flock, posts notifications and writes an atomic handoff.
func HelperRun(ctx context.Context, dir string, dryRun bool, now time.Time, post func(core.NotificationPlan) error) (HelperHandoff, error) {
	handoff := HelperHandoff{ID: fmt.Sprintf("helper-%d", now.UnixNano()), CreatedAt: core.FormatRFC3339(now)}
	settings := loadHelperSettings(dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return handoff, err
	}
	recordPostedClicks := func(posted []core.NotificationPlan) {
		if len(posted) == 0 {
			return
		}
		handoff.Clicks = core.NewNotificationClicks(posted, now)
		if err := queueNotificationClicks(dir, posted, now); err != nil {
			log.Printf("notification click queue: %v", err)
		}
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
	if settings.RefreshMinutes <= 0 {
		handoff.Description = "Manual: no background refresh scheduled."
		return handoff, writeHandoff()
	}
	if settings.APIKey == "" {
		handoff.Description = "Add a YouTube API key in Settings to refresh."
		return handoff, writeHandoff()
	}
	if !networkAvailableForHelper(ctx) {
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
	result := refreshForHelper(ctx, client, settings, state, store, existing, revision, false, now)
	store.Close()
	handoff.Description = result.Description
	handoff.Failures = result.Failures
	if result.Changed {
		handoff.Result = &result
		handoff.Notified = result.Notified
		handoff.Channels = result.Channels
	}

	// Persist failures and quota pauses so they are visible in the app on the
	// next launch.
	if result.Changed || len(result.Failures) > 0 {
		if err := commitHelperResult(ctx, dir, result, now); err != nil {
			return handoff, err
		}
	}

	if result.Changed {
		posted := make([]core.NotificationPlan, 0, len(result.Notified))
		for _, plan := range result.Notified {
			if post == nil {
				continue
			}
			if err := post(plan); err != nil {
				log.Printf("notification: %v", err)
				continue
			}
			posted = append(posted, plan)
		}
		recordPostedClicks(posted)
	}
	return handoff, writeHandoff()
}

func mergeHelperSettings(settings core.Settings, result core.RefreshResult, now time.Time) core.Settings {
	if result.Changed {
		settings.LastRefresh = core.FormatRFC3339(now)
	}
	if result.Changed || len(result.Failures) > 0 {
		settings.RecentFailures = result.Failures
	}
	if core.FailedOnQuota(result.Failures) {
		settings.QuotaPausedUntil = core.FormatRFC3339(now.Add(24 * time.Hour))
	}
	return settings
}

func commitHelperResult(ctx context.Context, dir string, result core.RefreshResult, now time.Time) error {
	settings := loadHelperSettings(dir)
	lock, lockErr := core.TryLockDir(localLockDirFor(settings.SyncPath))
	if lockErr != nil {
		if !errors.Is(lockErr, os.ErrExist) && !isWouldBlock(lockErr) {
			return lockErr
		}
		return queueHelperChannels(ctx, dir, result.Channels, now)
	}
	defer lock.Release()
	settings = loadHelperSettings(dir)
	if err := writeSettings(dir, mergeHelperSettings(settings, result, now)); err != nil {
		return err
	}
	return saveHelperChannels(settings.SyncPath, result.Channels, now)
}

func writeSettings(dir string, settings core.Settings) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteHelper(filepath.Join(dir, "settings.json"), data)
}

func mergeHelperChannels(state *core.SyncState, channels []core.Channel) {
	for _, ch := range channels {
		if old, ok := state.Channels[ch.ID]; !ok || ch.UpdatedAt > old.UpdatedAt {
			state.Channels[ch.ID] = ch
		}
	}
}

// saveHelperChannels merges refreshed channel metadata into the current shared
// JSON under the local flock so concurrent read/favorite changes survive.
func saveHelperChannels(syncPath string, channels []core.Channel, now time.Time) error {
	if len(channels) == 0 {
		return nil
	}
	state, err := core.LoadSyncWithConflicts(syncPath, core.StableDeviceID())
	if err != nil {
		return err
	}
	mergeHelperChannels(&state, channels)
	if millis := now.UnixMilli(); millis > state.UpdatedAt {
		state.UpdatedAt = millis
	}
	return core.SaveSyncAtomic(syncPath, state)
}

func queuedChannelState(channels []core.Channel, now time.Time) core.SyncState {
	state := core.SyncState{
		SchemaVersion: 1,
		DeviceID:      core.StableDeviceID(),
		UpdatedAt:     now.UnixMilli(),
		Channels:      map[string]core.Channel{},
	}
	mergeHelperChannels(&state, channels)
	return state
}

// queueHelperChannels queues only refreshed channel metadata while the app
// holds the local flock; the app merges it into its own fresh state on drain.
func queueHelperChannels(ctx context.Context, dir string, channels []core.Channel, now time.Time) error {
	if len(channels) == 0 {
		return nil
	}
	store, err := core.OpenStore(ctx, filepath.Join(dir, "videos.sqlite"))
	if err != nil {
		return err
	}
	defer store.Close()
	return store.Queue(ctx, map[string]any{"kind": "sync-state", "state": queuedChannelState(channels, now)})
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
	script := fmt.Sprintf("display notification \"%s\" with title \"%s\" subtitle \"New videos\" sound name \"default\"", escapeAppleScript(body), escapeAppleScript(plan.ChannelName))
	cmd := exec.Command("osascript", "-e", fmt.Sprintf("tell application id \"%s\" to %s", notificationBundleID, script))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("osascript: %w: %s", err, out)
	}
	return nil
}

func escapeAppleScript(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\r", "\\r")
	return strings.ReplaceAll(s, "\n", "\\n")
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
