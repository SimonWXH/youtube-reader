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
	"sync"
	"time"

	"github.com/simon/youtube-reader/core"
)

type App struct {
	Settings   core.Settings
	settingsMu sync.RWMutex
	Sync       core.SyncState
	Store      *core.Store
	Dir        string
	DeviceID   string
	Lock       *core.SyncLock

	Groups   []core.ChannelGroup
	Channels []core.Channel
	Videos   []core.Video

	Selected        string
	Search          string
	ChannelFilter   string
	UnreadOnly      bool
	SettingsOpen    bool
	SelectedVideoID string
	Busy            bool
	Revision        int64

	LastRefreshMessage string
	LastNotified       []core.NotificationPlan
	LastSyncWrite      time.Time
}

// GroupCollapsed reports whether a subscription group's channel list is
// folded. Groups default to collapsed when no preference has been stored.
func (a *App) GroupCollapsed(groupID string) bool {
	a.settingsMu.RLock()
	defer a.settingsMu.RUnlock()
	if a.Settings.ExpandedGroups != nil {
		if expanded, ok := a.Settings.ExpandedGroups[groupID]; ok {
			return !expanded
		}
	}
	return true
}

// SetGroupCollapsed stores a group's folding state in local settings.
func (a *App) SetGroupCollapsed(groupID string, collapsed bool) error {
	_, err := a.updateSettings(func(s *core.Settings) bool {
		if s.ExpandedGroups == nil {
			s.ExpandedGroups = map[string]bool{}
		}
		s.ExpandedGroups[groupID] = !collapsed
		return true
	})
	return err
}

func cloneSettings(s core.Settings) core.Settings {
	cloned := s
	if s.NotificationIDs != nil {
		cloned.NotificationIDs = append([]string(nil), s.NotificationIDs...)
	}
	if s.RecentFailures != nil {
		cloned.RecentFailures = append([]core.RefreshFailure(nil), s.RecentFailures...)
	}
	if s.ExpandedGroups != nil {
		cloned.ExpandedGroups = make(map[string]bool, len(s.ExpandedGroups))
		for id, expanded := range s.ExpandedGroups {
			cloned.ExpandedGroups[id] = expanded
		}
	}
	return cloned
}

func (a *App) settingsSnapshot() core.Settings {
	a.settingsMu.RLock()
	defer a.settingsMu.RUnlock()
	return cloneSettings(a.Settings)
}

func (a *App) settingsBytes(s core.Settings) ([]byte, error) {
	return json.MarshalIndent(s, "", "  ")
}

func (a *App) saveSettings(data []byte) error {
	return atomicWrite(filepath.Join(a.Dir, "settings.json"), data)
}

func (a *App) updateSettings(fn func(*core.Settings) bool) (bool, error) {
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	if !fn(&a.Settings) {
		return false, nil
	}
	a.mergeNewerDiskStatus()
	data, err := a.settingsBytes(cloneSettings(a.Settings))
	if err != nil {
		return true, err
	}
	return true, a.saveSettings(data)
}

// mergeNewerDiskStatus takes the refresh status the background helper wrote to
// settings.json over the stale copy kept in memory. Every settings write goes
// through it, so a local preference change cannot clobber failures or a quota
// pause the helper persisted in the meantime. The caller holds settingsMu.
func (a *App) mergeNewerDiskStatus() {
	data, err := os.ReadFile(filepath.Join(a.Dir, "settings.json"))
	if err != nil {
		return
	}
	var disk core.Settings
	if json.Unmarshal(data, &disk) != nil || !settingsStatusTime(disk).After(settingsStatusTime(a.Settings)) {
		return
	}
	a.Settings.LastRefresh = disk.LastRefresh
	a.Settings.NextRefresh = disk.NextRefresh
	a.Settings.RecentFailures = disk.RecentFailures
	a.Settings.QuotaPausedUntil = disk.QuotaPausedUntil
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
		SidebarWidth:    defaultSidebarWidth,
		DetailsWidth:    defaultDetailsWidth,
		MaxGridColumns:  defaultMaxGridColumns,
		ExpandedGroups:  map[string]bool{},
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
	if settings.MaxGridColumns < minMaxGridColumns || settings.MaxGridColumns > maxMaxGridColumns {
		settings.MaxGridColumns = defaultMaxGridColumns
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
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	a.mergeNewerDiskStatus()
	data, err := a.settingsBytes(cloneSettings(a.Settings))
	if err != nil {
		return err
	}
	return a.saveSettings(data)
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
	a.settingsMu.RLock()
	syncPath := a.Settings.SyncPath
	a.settingsMu.RUnlock()
	if current, err := core.LoadSyncWithConflicts(syncPath, a.DeviceID); err == nil && current.UpdatedAt > a.Sync.UpdatedAt {
		a.Sync = core.MergeSync(current, a.Sync)
	}
	now := time.Now().UnixMilli()
	if a.Sync.UpdatedAt < now {
		a.Sync.UpdatedAt = now
	}
	if err := core.SaveSyncAtomic(syncPath, a.Sync); err != nil {
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

type favoritesBackupVideo struct {
	VideoID       string `json:"videoID"`
	ChannelID     string `json:"channelID,omitempty"`
	ChannelTitle  string `json:"channelTitle,omitempty"`
	Title         string `json:"title,omitempty"`
	FavoritedAt   int64  `json:"favoritedAt"`
	FavoriteTime  string `json:"favoriteTime,omitempty"`
	FavoriteGroup string `json:"favoriteGroup,omitempty"`
	UpdatedAt     int64  `json:"updatedAt"`
}

type favoritesBackup struct {
	SchemaVersion int                    `json:"schemaVersion"`
	ExportedAt    string                 `json:"exportedAt,omitempty"`
	Favorites     []favoritesBackupVideo `json:"favorites"`
}

func ExportFavorites(state core.SyncState, path string, now time.Time) error {
	backup := favoritesBackup{
		SchemaVersion: 1,
		ExportedAt:    now.UTC().Format(time.RFC3339),
		Favorites:     make([]favoritesBackupVideo, 0, len(state.Favorites)),
	}
	for _, fav := range state.Favorites {
		entry := favoritesBackupVideo{
			VideoID:      fav.VideoID,
			FavoritedAt:  fav.FavoritedAt,
			FavoriteTime: core.FormatRFC3339(time.UnixMilli(fav.FavoritedAt)),
			UpdatedAt:    fav.UpdatedAt,
		}
		if fav.Video != nil {
			entry.ChannelID = fav.Video.ChannelID
			entry.ChannelTitle = fav.Video.ChannelTitle
			entry.Title = fav.Video.Title
			entry.FavoriteGroup = fav.Video.FavoriteChannel
		}
		backup.Favorites = append(backup.Favorites, entry)
	}
	sort.Slice(backup.Favorites, func(i, j int) bool {
		if backup.Favorites[i].FavoritedAt != backup.Favorites[j].FavoritedAt {
			return backup.Favorites[i].FavoritedAt > backup.Favorites[j].FavoritedAt
		}
		return backup.Favorites[i].VideoID < backup.Favorites[j].VideoID
	})
	data, err := json.MarshalIndent(backup, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, data)
}

func (a *App) ExportFavorites() error {
	a.settingsMu.RLock()
	backupPath := a.Settings.BackupPath
	a.settingsMu.RUnlock()
	return ExportFavorites(a.Sync, backupPath, time.Now())
}

func parseFavoritesBackup(data []byte) (favoritesBackup, error) {
	var payload favoritesBackup
	if err := json.Unmarshal(data, &payload); err != nil {
		return favoritesBackup{}, err
	}
	if payload.SchemaVersion != 1 {
		return favoritesBackup{}, fmt.Errorf("unsupported favorites backup version %d", payload.SchemaVersion)
	}
	if payload.Favorites == nil {
		return favoritesBackup{}, fmt.Errorf("favorites backup is missing favorites")
	}
	seen := make(map[string]bool, len(payload.Favorites))
	for i, fav := range payload.Favorites {
		if fav.VideoID == "" {
			return favoritesBackup{}, fmt.Errorf("favorites backup entry %d has no videoID", i)
		}
		if seen[fav.VideoID] {
			return favoritesBackup{}, fmt.Errorf("favorites backup has duplicate videoID %q", fav.VideoID)
		}
		seen[fav.VideoID] = true
	}
	return payload, nil
}

func ApplyFavoritesBackup(state *core.SyncState, data []byte, now int64) (int, error) {
	payload, err := parseFavoritesBackup(data)
	if err != nil {
		return 0, err
	}
	changed := 0
	for _, fav := range payload.Favorites {
		updatedAt := fav.UpdatedAt
		if updatedAt == 0 {
			updatedAt = now
		}
		old, exists := state.Favorites[fav.VideoID]
		if exists && old.UpdatedAt > updatedAt {
			continue
		}
		favoritedAt := fav.FavoritedAt
		if favoritedAt == 0 {
			favoritedAt = fav.FavoriteTimeMillis()
		}
		if exists && old.FavoritedAt != 0 {
			favoritedAt = old.FavoritedAt
		}
		if favoritedAt == 0 {
			favoritedAt = now
		}
		video := core.Video{
			ID:              fav.VideoID,
			ChannelID:       fav.ChannelID,
			ChannelTitle:    fav.ChannelTitle,
			Title:           fav.Title,
			FavoritedAt:     favoritedAt,
			FavoriteChannel: fav.FavoriteGroup,
		}
		state.Favorites[fav.VideoID] = core.FavoriteRecord{
			VideoID:     fav.VideoID,
			FavoritedAt: favoritedAt,
			Video:       &video,
			UpdatedAt:   updatedAt,
		}
		changed++
	}
	if changed > 0 {
		state.UpdatedAt = now
	}
	return changed, nil
}

// FavoriteTimeMillis parses the optional RFC3339 field for older backups that
// predated favoritedAt. A bad value is ignored in favor of the numeric field.
func (f favoritesBackupVideo) FavoriteTimeMillis() int64 {
	if f.FavoriteTime == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339, f.FavoriteTime)
	if err != nil {
		return 0
	}
	return t.UnixMilli()
}

func (a *App) ImportFavorites(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	now := time.Now().UnixMilli()
	state := a.Sync
	changed, err := ApplyFavoritesBackup(&state, data, now)
	if err != nil {
		return 0, err
	}
	if changed == 0 {
		return 0, nil
	}
	a.Sync = state
	if err := a.SaveSync(); err != nil {
		return 0, err
	}
	return changed, nil
}

// refreshBlockedReason says why a refresh cannot run now, or "" when it can.
// A missing key and a quota pause are the two conditions core.RefreshYoutube
// refuses; every refresh surface reads this one answer, so none offers a
// refresh that would return without doing anything.
func (a *App) refreshBlockedReason(now time.Time) string {
	settings := a.settingsSnapshot()
	if settings.APIKey == "" {
		return "Add a YouTube API key in Settings to refresh"
	}
	if until, err := time.Parse(time.RFC3339, settings.QuotaPausedUntil); err == nil && now.Before(until) {
		return "Refresh paused until " + until.Local().Format("2 Jan, 15:04")
	}
	return ""
}

func (a *App) Refresh(force bool) RefreshOutcome {
	ctx := context.Background()
	now := time.Now()
	settings := a.settingsSnapshot()
	interval := time.Duration(settings.RefreshMinutes) * time.Minute
	client := core.YouTube{Key: settings.APIKey}
	outcome := core.RefreshYoutube(ctx, client, settings, a.Sync, a.Store, a.Videos, a.Revision, force, now)
	_, _ = a.updateSettings(func(s *core.Settings) bool {
		if outcome.Changed {
			s.LastRefresh = core.FormatRFC3339(now)
			s.NextRefresh = core.FormatRFC3339(now.Add(interval))
			s.RecentFailures = outcome.Failures
		} else if len(outcome.Failures) > 0 {
			s.RecentFailures = outcome.Failures
		}
		if containsQuotaFailure(outcome.Failures) {
			s.QuotaPausedUntil = core.FormatRFC3339(now.Add(24 * time.Hour))
		}
		return true
	})
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
	a.settingsMu.RLock()
	defer a.settingsMu.RUnlock()
	for _, id := range a.Settings.NotificationIDs {
		if id == channelID {
			return true
		}
	}
	return false
}

func (a *App) EnableNotificationsForSelection() {
	var ids []string
	if a.Selected == "all" {
		for id := range a.Sync.Channels {
			ids = append(ids, id)
		}
	} else if strings.HasPrefix(a.Selected, "group:") {
		if g, ok := a.Sync.Groups[strings.TrimPrefix(a.Selected, "group:")]; ok {
			ids = append(ids, g.ChannelIDs...)
		}
	} else if strings.HasPrefix(a.Selected, "channel:") {
		ids = append(ids, strings.TrimPrefix(a.Selected, "channel:"))
	}
	_, _ = a.updateSettings(func(s *core.Settings) bool {
		known := make(map[string]bool, len(s.NotificationIDs)+len(ids))
		for _, id := range s.NotificationIDs {
			known[id] = true
		}
		changed := false
		for _, id := range ids {
			if id == "" || known[id] {
				continue
			}
			s.NotificationIDs = append(s.NotificationIDs, id)
			known[id] = true
			changed = true
		}
		return changed
	})
}

func (a *App) StartupRefreshIfDue() {
	a.settingsMu.RLock()
	lastRefresh := a.Settings.LastRefresh
	refreshMinutes := a.Settings.RefreshMinutes
	quotaPausedUntil := a.Settings.QuotaPausedUntil
	a.settingsMu.RUnlock()
	if lastRefresh == "" {
		return
	}
	last, err := time.Parse(time.RFC3339, lastRefresh)
	if err != nil || time.Since(last) < time.Duration(refreshMinutes)*time.Minute {
		return
	}
	if paused, err := time.Parse(time.RFC3339, quotaPausedUntil); err == nil && time.Now().Before(paused) {
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
	currentStatus := a.settingsSnapshot()
	refreshInterval := time.Duration(currentStatus.RefreshMinutes) * time.Minute
	if handoff.ID != "" && handoff.ID == currentStatus.LastHandoffID {
		return nil
	}
	fresh := handoffStatusTime(handoff.CreatedAt, handoff.Failures).After(settingsStatusTime(currentStatus))
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
	_, _ = a.updateSettings(func(s *core.Settings) bool {
		if fresh && len(handoff.Failures) > 0 {
			s.RecentFailures = handoff.Failures
			if containsQuotaFailure(handoff.Failures) {
				for _, f := range handoff.Failures {
					if strings.HasPrefix(f.Message, "quota paused") {
						if at, err := time.Parse(time.RFC3339, f.At); err == nil {
							s.QuotaPausedUntil = core.FormatRFC3339(at.Add(24 * time.Hour))
						}
						break
					}
				}
			}
		}
		if fresh && handoff.Result != nil && handoff.CreatedAt != "" {
			s.LastRefresh = handoff.CreatedAt
			if at, err := time.Parse(time.RFC3339, handoff.CreatedAt); err == nil {
				s.NextRefresh = core.FormatRFC3339(at.Add(refreshInterval))
			}
		}
		s.LastHandoffID = handoff.ID
		return true
	})
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
	a.settingsMu.RLock()
	showShorts := a.Settings.ShowShorts
	a.settingsMu.RUnlock()
	for _, v := range a.Videos {
		if !a.videoVisible(v, allowed, showShorts) {
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

// videoVisible is the permanent visibility rule shared by the grid and the
// sidebar counts, so a row's count always equals the videos its destination
// shows once the transient filter bar is cleared. The filter bar's
// ChannelFilter, Search and UnreadOnly are applied on top of a destination
// rather than being part of it, so VisibleVideos applies them itself and the
// sidebar counts leave them out.
func (a *App) videoVisible(v core.Video, allowed map[string]bool, showShorts bool) bool {
	if !showShorts && v.IsShort {
		return false
	}
	return allowed == nil || allowed[v.ID] || allowed[v.ChannelID]
}

func (a *App) HasSelection() bool {
	return a.SelectedVideoID != ""
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
