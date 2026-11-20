package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	LastSyncWrite   time.Time
}

type RefreshOutcome struct {
	Videos      []core.Video
	Channels    []core.Channel
	Failures    []core.RefreshFailure
	Notified    []NotificationPlan
	Description string
	Changed     bool
}

type NotificationPlan struct {
	ChannelID   string
	ChannelName string
	Count       int
	NewestTitle string
}

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
	data, err := json.MarshalIndent(a.Settings, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(a.Dir, "settings.json"), data)
}

func (a *App) SaveSync() error {
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
	if a.Settings.APIKey == "" {
		return RefreshOutcome{Description: "Add a YouTube API key in Settings to refresh."}
	}
	if !force && a.Settings.LastRefresh != "" {
		last, err := time.Parse(time.RFC3339, a.Settings.LastRefresh)
		if err == nil && time.Since(last) < time.Duration(a.Settings.RefreshMinutes)*time.Minute {
			return RefreshOutcome{Description: "Up to date"}
		}
	}
	ctx := context.Background()
	client := core.YouTube{Key: a.Settings.APIKey}
	ids := make([]string, 0, len(a.Sync.Channels))
	for id := range a.Sync.Channels {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	infos, err := client.ChannelInfo(ctx, ids)
	if err != nil {
		return a.refreshFailed(err)
	}
	byID := map[string]core.Channel{}
	for _, ch := range infos {
		byID[ch.ID] = ch
	}
	all := []core.Video{}
	var failures []core.RefreshFailure
	notifications := []NotificationPlan{}
	for id, ch := range a.Sync.Channels {
		uploads := ch.UploadedAt
		if info, ok := byID[id]; ok {
			uploads = info.UploadedAt
			ch.AvatarURL = info.AvatarURL
			ch.Title = info.Title
			a.Sync.Channels[id] = ch
		}
		if uploads == "" {
			failures = append(failures, core.RefreshFailure{At: nowRFC3339(), Message: "no uploads playlist for " + ch.Title})
			continue
		}
		videos, err := client.PlaylistVideos(ctx, uploads, 3)
		if err != nil {
			if errors.Is(err, core.ErrQuota) {
				return a.refreshFailed(err)
			}
			failures = append(failures, core.RefreshFailure{At: nowRFC3339(), Message: ch.Title + ": " + err.Error()})
			continue
		}
		existing := map[string]bool{}
		for _, v := range a.Videos {
			if v.ChannelID == id {
				existing[v.ID] = true
			}
		}
		newCount := 0
		newest := ""
		for _, v := range videos {
			v.ChannelID = id
			v.ChannelTitle = ch.Title
			v.AvatarURL = ch.AvatarURL
			v.IsShort = core.IsShort(v.Duration, v.Width, v.Height)
			if !existing[v.ID] {
				newCount++
				if newest == "" {
					newest = v.Title
				}
			}
			all = append(all, v)
		}
		if newCount > 0 && a.notificationEnabled(id) {
			notifications = append(notifications, NotificationPlan{ChannelID: id, ChannelName: ch.Title, Count: newCount, NewestTitle: newest})
		}
	}
	all = core.DedupeVideos(all)
	missing := []core.Video{}
	for _, v := range all {
		if v.Duration == "" {
			missing = append(missing, v)
		}
	}
	if len(missing) > 0 {
		hydrated, err := client.Hydrate(ctx, missing)
		if err != nil {
			if errors.Is(err, core.ErrQuota) {
				return a.refreshFailed(err)
			}
			failures = append(failures, core.RefreshFailure{At: nowRFC3339(), Message: "hydrate: " + err.Error()})
		} else {
			byID := map[string]core.Video{}
			for _, v := range hydrated {
				byID[v.ID] = v
			}
			for i, v := range all {
				if hv, ok := byID[v.ID]; ok {
					hv.ChannelID = v.ChannelID
					hv.ChannelTitle = v.ChannelTitle
					hv.AvatarURL = v.AvatarURL
					if hv.ThumbnailURL == "" {
						hv.ThumbnailURL = v.ThumbnailURL
					}
					all[i] = hv
				}
			}
		}
	}
	protected := func(v core.Video) bool { return !a.Sync.IsRead(v.ID) || a.Sync.IsFavorite(v.ID) }
	all = core.ApplyRetention(all, protected, time.Now(), core.DefaultRetention())
	channelList := make([]core.Channel, 0, len(a.Sync.Channels))
	for _, ch := range a.Sync.Channels {
		channelList = append(channelList, ch)
	}
	if err := a.Store.CommitRefresh(ctx, core.RefreshCommit{Channels: channelList, Videos: all, Revision: a.Revision + 1}); err != nil {
		return a.refreshFailed(err)
	}
	now := time.Now().UTC()
	a.Settings.LastRefresh = core.FormatRFC3339(now)
	a.Settings.NextRefresh = core.FormatRFC3339(now.Add(time.Duration(a.Settings.RefreshMinutes) * time.Minute))
	a.Settings.RecentFailures = failures
	_ = a.SaveSettings()
	_ = a.reload()
	return RefreshOutcome{Videos: all, Channels: a.Channels, Failures: failures, Notified: notifications, Changed: true, Description: fmt.Sprintf("Refreshed %d videos", len(all))}
}

func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }

func (a *App) refreshFailed(err error) RefreshOutcome {
	msg := err.Error()
	if errors.Is(err, core.ErrQuota) {
		a.Settings.QuotaPausedUntil = time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
		msg = "quota paused until " + a.Settings.QuotaPausedUntil
	}
	f := core.RefreshFailure{At: nowRFC3339(), Message: msg}
	a.Settings.RecentFailures = append([]core.RefreshFailure{f}, a.Settings.RecentFailures...)
	if len(a.Settings.RecentFailures) > 5 {
		a.Settings.RecentFailures = a.Settings.RecentFailures[:5]
	}
	_ = a.SaveSettings()
	return RefreshOutcome{Failures: a.Settings.RecentFailures, Description: msg}
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
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			continue
		}
		data, _ := json.Marshal(payload["state"])
		var state core.SyncState
		if err := json.Unmarshal(data, &state); err != nil {
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
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &handoff); err != nil {
		return nil
	}
	if handoff.ID != "" && handoff.ID == a.Settings.LastHandoffID {
		return nil
	}
	a.Settings.LastHandoffID = handoff.ID
	_ = a.SaveSettings()
	_ = os.Remove(path)
	return nil
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
