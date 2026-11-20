package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/simon/youtube-reader/core"
)

type App struct {
	Settings core.Settings
	Sync     core.SyncState
	Store    *core.Store
	Dir      string
	DeviceID string
	Lock     *core.SyncLock

	Groups   []core.ChannelGroup
	Channels []core.Channel
	Videos   []core.Video

	Selected        string
	Search          string
	ChannelFilter   string
	UnreadOnly      bool
	StatusExpanded  bool
	SettingsOpen    bool
	SelectedVideoID string
	Busy            bool
	Revision        int64

	LastRefreshMessage string
	LastNotified       []core.NotificationPlan
	LastSyncWrite      time.Time
}

type RefreshOutcome = core.RefreshResult

type NotificationPlan = core.NotificationPlan

func appDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "YouTubeReader")
}

func localLockDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "YouTubeReaderLocalLock")
}

func defaultSettings() core.Settings {
	dir := appDir()
	return core.Settings{
		RefreshMinutes:  60,
		SyncPath:        filepath.Join(dir, "youtube-reader-sync.json"),
		BackupPath:      filepath.Join(dir, "favorites-backup.json"),
		NotificationIDs: []string{},
		RecentFailures:  []core.RefreshFailure{},
	}
}

func LoadApp() (*App, error) {
	dir := appDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	settings := defaultSettings()
	if data, err := os.ReadFile(filepath.Join(dir, "settings.json")); err == nil {
		_ = json.Unmarshal(data, &settings)
	}
	if settings.RefreshMinutes <= 0 {
		settings.RefreshMinutes = 60
	}
	if settings.SyncPath == "" {
		settings.SyncPath = filepath.Join(dir, "youtube-reader-sync.json")
	}
	if settings.BackupPath == "" {
		settings.BackupPath = filepath.Join(dir, "favorites-backup.json")
	}
	device := core.StableDeviceID()
	lock, err := core.AcquireLock(localLockDir())
	if err != nil {
		return nil, fmt.Errorf("acquire local lock: %w", err)
	}
	state, err := core.LoadSyncWithConflicts(settings.SyncPath, device)
	if err != nil {
		lock.Release()
		return nil, err
	}
	core.CleanupSyncConflicts(settings.SyncPath, func(p string) bool { return strings.Contains(filepath.Base(p), "sync-conflict") })
	store, err := core.OpenStore(context.Background(), filepath.Join(dir, "videos.sqlite"))
	if err != nil {
		lock.Release()
		return nil, err
	}
	app := &App{Settings: settings, Sync: state, Store: store, Dir: dir, DeviceID: device, Lock: lock, Selected: "all"}
	if err := app.reload(); err != nil {
		store.Close()
		lock.Release()
		return nil, err
	}
	return app, nil
}

func (a *App) reload() error {
	ctx := context.Background()
	var err error
	a.Channels, err = a.Store.Channels(ctx)
	if err != nil {
		return err
	}
	a.Videos, err = a.Store.Videos(ctx)
	if err != nil {
		return err
	}
	a.Groups = make([]core.ChannelGroup, 0, len(a.Sync.Groups))
	for _, g := range a.Sync.Groups {
		a.Groups = append(a.Groups, g)
	}
	sort.Slice(a.Groups, func(i, j int) bool { return a.Groups[i].Name < a.Groups[j].Name })
	if rev := a.Store.Meta(ctx, "revision"); rev != "" {
		fmt.Sscan(rev, &a.Revision)
	}
	return nil
}

func (a *App) Close() {
	if a.Store != nil {
		a.Store.Close()
	}
	if a.Lock != nil {
		a.Lock.Release()
	}
}

func (a *App) SaveSettings() error {
	path := filepath.Join(a.Dir, "settings.json")
	if data, err := os.ReadFile(path); err == nil {
		var disk core.Settings
		if json.Unmarshal(data, &disk) == nil && settingsStatusTime(disk).After(settingsStatusTime(a.Settings)) {
			a.Settings.LastRefresh = disk.LastRefresh
			a.Settings.NextRefresh = disk.NextRefresh
			a.Settings.RecentFailures = disk.RecentFailures
			a.Settings.QuotaPausedUntil = disk.QuotaPausedUntil
		}
	}
	data, err := json.MarshalIndent(a.Settings, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, data)
}

func settingsStatusTime(s core.Settings) time.Time {
	var latest time.Time
	for _, raw := range []string{s.LastRefresh, s.QuotaPausedUntil} {
		if t, err := time.Parse(time.RFC3339, raw); err == nil && t.After(latest) {
			latest = t
		}
	}
	for _, f := range s.RecentFailures {
		if t, err := time.Parse(time.RFC3339, f.At); err == nil && t.After(latest) {
			latest = t
		}
	}
	return latest
}

func handoffStatusTime(createdAt string, failures []core.RefreshFailure) time.Time {
	latest, _ := time.Parse(time.RFC3339, createdAt)
	for _, f := range failures {
		if t, err := time.Parse(time.RFC3339, f.At); err == nil && t.After(latest) {
			latest = t
		}
	}
	return latest
}

func (a *App) SaveSync() error {
	if current, err := core.LoadSyncWithConflicts(a.Settings.SyncPath, a.DeviceID); err == nil && current.UpdatedAt > a.Sync.UpdatedAt {
		a.Sync = core.MergeSync(current, a.Sync)
	}
	now := time.Now().UnixMilli()
	if a.Sync.UpdatedAt < now {
		a.Sync.UpdatedAt = now
	}
	if err := core.SaveSyncAtomic(a.Settings.SyncPath, a.Sync); err != nil {
		return err
	}
	a.LastSyncWrite = time.Now()
	return nil
}

func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".youtube-reader-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func (a *App) ImportOPMLPaths(paths []string) error {
	var mergedGroups []core.ChannelGroup
	var mergedChannels []core.Channel
	for _, path := range paths {
		groups, channels, err := core.ParseOPMLFile(path)
		if err != nil {
			return err
		}
		mergedGroups = append(mergedGroups, groups...)
		mergedChannels = append(mergedChannels, channels...)
	}
	now := time.Now().UnixMilli()
	a.Sync.MergeImported(mergedGroups, mergedChannels, now)
	if err := a.SaveSync(); err != nil {
		return err
	}
	return a.reload()
}

func (a *App) ImportOPML(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	groups, channels, err := core.ParseOPML(data)
	if err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	a.Sync.ReplaceGroups(groups, channels, now)
	if err := a.SaveSync(); err != nil {
		return err
	}
	return a.reload()
}

func (a *App) MarkRead(video core.Video) {
	if a.Sync.IsRead(video.ID) {
		return
	}
	a.Sync.MarkRead(video.ID, time.Now().UnixMilli())
	_ = a.SaveSync()
}

func (a *App) ToggleFavorite(video core.Video) {
	a.Sync.SetFavorite(video, !a.Sync.IsFavorite(video.ID), time.Now().UnixMilli())
	_ = a.SaveSync()
}

func (a *App) ExportFavorites() error {
	favs := a.Sync.FavoritesSorted()
	data, err := json.MarshalIndent(map[string]any{"schemaVersion": 1, "exportedAt": time.Now().UTC().Format(time.RFC3339), "favorites": favs}, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(a.Settings.BackupPath, data)
}

func (a *App) ImportFavorites() error {
	data, err := os.ReadFile(a.Settings.BackupPath)
	if err != nil {
		return err
	}
	var payload struct {
		Favorites []core.Video `json:"favorites"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	for _, v := range payload.Favorites {
		a.Sync.SetFavorite(v, true, now)
	}
	_ = a.SaveSync()
	return nil
}

func (a *App) Refresh(force bool) RefreshOutcome {
	ctx := context.Background()
	now := time.Now()
	client := core.YouTube{Key: a.Settings.APIKey}
	outcome := core.RefreshYoutube(ctx, client, a.Settings, a.Sync, a.Store, a.Videos, a.Revision, force, now)
	if outcome.Changed {
		a.Settings.LastRefresh = core.FormatRFC3339(now)
		a.Settings.NextRefresh = core.FormatRFC3339(now.Add(time.Duration(a.Settings.RefreshMinutes) * time.Minute))
	}
	a.Settings.RecentFailures = outcome.Failures
	if containsQuotaFailure(outcome.Failures) {
		a.Settings.QuotaPausedUntil = core.FormatRFC3339(now.Add(24 * time.Hour))
	}
	_ = a.SaveSettings()
	_ = a.reload()
	if outcome.Changed {
		outcome.Channels = a.Channels
	}
	return outcome
}

func containsQuotaFailure(failures []core.RefreshFailure) bool {
	for _, f := range failures {
		if strings.HasPrefix(f.Message, "quota paused") {
			return true
		}
	}
	return false
}

func (a *App) notificationEnabled(channelID string) bool {
	for _, id := range a.Settings.NotificationIDs {
		if id == channelID {
			return true
		}
	}
	return false
}

func (a *App) EnableNotificationsForSelection() {
	add := func(id string) {
		if !a.notificationEnabled(id) {
			a.Settings.NotificationIDs = append(a.Settings.NotificationIDs, id)
		}
	}
	if a.Selected == "all" {
		for id := range a.Sync.Channels {
			add(id)
		}
	} else if strings.HasPrefix(a.Selected, "group:") {
		if g, ok := a.Sync.Groups[strings.TrimPrefix(a.Selected, "group:")]; ok {
			for _, id := range g.ChannelIDs {
				add(id)
			}
		}
	} else if strings.HasPrefix(a.Selected, "channel:") {
		add(strings.TrimPrefix(a.Selected, "channel:"))
	}
	_ = a.SaveSettings()
}

func (a *App) StartupRefreshIfDue() {
	if a.Settings.LastRefresh == "" {
		return
	}
	last, err := time.Parse(time.RFC3339, a.Settings.LastRefresh)
	if err != nil || time.Since(last) < time.Duration(a.Settings.RefreshMinutes)*time.Minute {
		return
	}
	if paused, err := time.Parse(time.RFC3339, a.Settings.QuotaPausedUntil); err == nil && time.Now().Before(paused) {
		return
	}
	go func() { _ = a.Refresh(false) }()
}

func (a *App) DrainQueuedSyncWrites() error {
	ctx := context.Background()
	items, err := a.Store.DrainQueue(ctx)
	if err != nil {
		return err
	}
	for _, raw := range items {
		state, err := queueSyncState(raw)
		if err != nil {
			continue
		}
		a.Sync = core.MergeSync(a.Sync, state)
	}
	if len(items) > 0 {
		if err := a.SaveSync(); err != nil {
			return err
		}
		return a.reload()
	}
	return nil
}

func (a *App) ConsumeHandoff() error {
	path := filepath.Join(a.Dir, "inbox.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var handoff struct {
		ID          string                   `json:"id"`
		CreatedAt   string                   `json:"createdAt"`
		Description string                   `json:"description"`
		Result      *core.RefreshResult      `json:"result"`
		Notified    []core.NotificationPlan  `json:"notified"`
		Failures    []core.RefreshFailure    `json:"failures"`
		Clicks      []core.NotificationClick `json:"clicks"`
	}
	if err := json.Unmarshal(data, &handoff); err != nil {
		return nil
	}
	if handoff.ID != "" && handoff.ID == a.Settings.LastHandoffID {
		return nil
	}
	fresh := handoffStatusTime(handoff.CreatedAt, handoff.Failures).After(settingsStatusTime(a.Settings))
	if handoff.Result != nil || handoff.Description != "" {
		a.LastRefreshMessage = handoff.Description
	}
	if len(handoff.Notified) > 0 {
		a.LastNotified = handoff.Notified
	}
	if len(handoff.Clicks) > 0 {
		if err := a.RecordNotificationClicks(handoff.Clicks); err != nil {
			log.Printf("notification click handoff: %v", err)
		}
	}
	if fresh && len(handoff.Failures) > 0 {
		a.Settings.RecentFailures = handoff.Failures
		if containsQuotaFailure(handoff.Failures) {
			for _, f := range handoff.Failures {
				if strings.HasPrefix(f.Message, "quota paused") {
					if at, err := time.Parse(time.RFC3339, f.At); err == nil {
						a.Settings.QuotaPausedUntil = core.FormatRFC3339(at.Add(24 * time.Hour))
					}
					break
				}
			}
		}
	}
	if fresh && handoff.Result != nil && handoff.CreatedAt != "" {
		a.Settings.LastRefresh = handoff.CreatedAt
		if at, err := time.Parse(time.RFC3339, handoff.CreatedAt); err == nil {
			a.Settings.NextRefresh = core.FormatRFC3339(at.Add(time.Duration(a.Settings.RefreshMinutes) * time.Minute))
		}
	}
	a.Settings.LastHandoffID = handoff.ID
	_ = a.SaveSettings()
	_ = os.Remove(path)
	return a.reload()
}

// notificationClicksPath is the local, atomic queue of helper-posted
// notifications that may produce a click callback. It is never synced.
func (a *App) notificationClicksPath() string {
	return filepath.Join(a.Dir, "notification-clicks.json")
}

func readNotificationClicks(path string) (core.NotificationClicks, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return core.NotificationClicks{}, nil
	}
	if err != nil {
		return core.NotificationClicks{}, err
	}
	var queue core.NotificationClicks
	if err := json.Unmarshal(data, &queue); err != nil {
		return core.NotificationClicks{}, fmt.Errorf("parse notification click queue: %w", err)
	}
	if err := queue.Validate(); err != nil {
		return core.NotificationClicks{}, err
	}
	return queue, nil
}

func writeNotificationClicks(path string, queue core.NotificationClicks) error {
	data, err := json.MarshalIndent(queue, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, data)
}

// RecordNotificationClicks appends helper-posted notification records after
// dropping duplicates. Older records are kept: consuming a newer click must
// not suppress a still-pending notification.
func (a *App) RecordNotificationClicks(clicks []core.NotificationClick) error {
	path := a.notificationClicksPath()
	queue, err := readNotificationClicks(path)
	if err != nil {
		return err
	}
	known := make(map[string]bool, len(queue.Clicks)+len(clicks))
	for _, click := range queue.Clicks {
		known[core.NotificationClickKey(click)] = true
	}
	for _, click := range clicks {
		if err := core.ValidateNotificationClick(click); err != nil {
			return err
		}
		key := core.NotificationClickKey(click)
		if known[key] {
			continue
		}
		queue.Clicks = append(queue.Clicks, click)
		known[key] = true
	}
	return writeNotificationClicks(path, queue)
}

// ConsumeNotificationClick selects the newest pending helper-posted record and
// leaves older records for later click callbacks. It must only be called from
// an actual notification-click handler, never during an ordinary launch.
// Corrupt state is rejected and left untouched rather than failing open.
func (a *App) ConsumeNotificationClick() (string, bool) {
	path := a.notificationClicksPath()
	queue, err := readNotificationClicks(path)
	if err != nil {
		log.Printf("notification click queue: %v", err)
		return "", false
	}
	if len(queue.Clicks) == 0 {
		return "", false
	}
	latest, tail, err := core.LatestNotificationClick(queue)
	if err != nil {
		log.Printf("notification click queue: %v", err)
		return "", false
	}
	if err := writeNotificationClicks(path, tail); err != nil {
		log.Printf("notification click queue: %v", err)
		return "", false
	}
	return latest.ChannelID, true
}

// SelectChannel routes the UI to a channel's filtered view and clears the
// transient search/unread filters so the destination is unambiguous.
func (a *App) SelectChannel(channelID string) bool {
	if channelID == "" {
		return false
	}
	for _, ch := range a.Channels {
		if ch.ID == channelID {
			a.Selected = "channel:" + channelID
			a.ChannelFilter = ""
			a.Search = ""
			a.UnreadOnly = false
			a.SelectedVideoID = ""
			return true
		}
	}
	return false
}

func (a *App) VisibleVideos() []core.Video {
	selected := a.Selected
	var allowed map[string]bool
	if selected == "favorites" {
		allowed = map[string]bool{}
		for id := range a.Sync.Favorites {
			allowed[id] = true
		}
	} else if strings.HasPrefix(selected, "group:") {
		if g, ok := a.Sync.Groups[strings.TrimPrefix(selected, "group:")]; ok {
			allowed = map[string]bool{}
			for _, id := range g.ChannelIDs {
				allowed[id] = true
			}
		}
	} else if strings.HasPrefix(selected, "channel:") {
		allowed = map[string]bool{strings.TrimPrefix(selected, "channel:"): true}
	}
	out := make([]core.Video, 0, len(a.Videos))
	q := strings.ToLower(a.Search)
	for _, v := range a.Videos {
		if !a.Settings.ShowShorts && v.IsShort {
			continue
		}
		if allowed != nil && !allowed[v.ID] && !allowed[v.ChannelID] {
			continue
		}
		if a.ChannelFilter != "" && v.ChannelID != a.ChannelFilter {
			continue
		}
		if a.UnreadOnly && a.Sync.IsRead(v.ID) {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(v.Title), q) && !strings.Contains(strings.ToLower(v.Description), q) {
			continue
		}
		out = append(out, v)
	}
	if selected == "favorites" {
		sort.Slice(out, func(i, j int) bool { return out[i].FavoritedAt > out[j].FavoritedAt })
	}
	return out
}

func (a *App) VideoByID(id string) (core.Video, bool) {
	for _, v := range a.Videos {
		if v.ID == id {
			return v, true
		}
	}
	return core.Video{}, false
}

func queueSyncState(raw json.RawMessage) (core.SyncState, error) {
	var outer string
	var payload struct {
		State core.SyncState `json:"state"`
	}
	if err := json.Unmarshal(raw, &outer); err == nil {
		raw = json.RawMessage(outer)
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return core.SyncState{}, err
	}
	return payload.State, nil
}
