package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
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
	SelectedVideoID string
	Busy            bool
	Revision        int64

	LastRefreshMessage string
	LastNotified       []core.NotificationPlan
	LastSyncWrite      time.Time
	SaveSyncCalls      int

	// BackfillCursors is where each channel's history paging stands, by channel
	// ID: a channel the map does not name has never loaded its history, and an
	// entry whose token is empty has reached the channel's earliest video. It
	// stays in this device's SQLite store and is never synced, because it names
	// a page this device has read rather than something another device should
	// skip.
	BackfillCursors map[string]core.BackfillCursor
	// BackfillBusy is the channel whose history page is in flight, empty when
	// none is, and BackfillErr is the failure that page reported. The next
	// attempt clears the failure, and so does leaving the channel.
	BackfillBusy string
	BackfillErr  string
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
	if s.GroupOrder != nil {
		cloned.GroupOrder = append([]string(nil), s.GroupOrder...)
	}
	if s.WorkspaceOrder != nil {
		cloned.WorkspaceOrder = append([]string(nil), s.WorkspaceOrder...)
	}
	return cloned
}

func cloneSyncState(s core.SyncState) core.SyncState {
	cloned := s
	cloned.Groups = make(map[string]core.ChannelGroup, len(s.Groups))
	for id, group := range s.Groups {
		cloned.Groups[id] = group
	}
	cloned.Channels = make(map[string]core.Channel, len(s.Channels))
	for id, channel := range s.Channels {
		cloned.Channels[id] = channel
	}
	cloned.ReadStates = make(map[string]map[string]core.ReadRecord, len(s.ReadStates))
	for id, reads := range s.ReadStates {
		inner := make(map[string]core.ReadRecord, len(reads))
		for videoID, record := range reads {
			inner[videoID] = record
		}
		cloned.ReadStates[id] = inner
	}
	cloned.Favorites = make(map[string]core.FavoriteRecord, len(s.Favorites))
	for id, favorite := range s.Favorites {
		cloned.Favorites[id] = favorite
	}
	return cloned
}

type refreshSnapshot struct {
	state    core.SyncState
	videos   []core.Video
	revision int64
}

func (a *App) refreshSnapshot() refreshSnapshot {
	return refreshSnapshot{
		state:    cloneSyncState(a.Sync),
		videos:   append([]core.Video(nil), a.Videos...),
		revision: a.Revision,
	}
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
	if a.Dir == "" {
		return nil
	}
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
		RefreshMinutes:  defaultRefreshMinutes,
		SyncPath:        filepath.Join(dir, "youtube-reader-sync.json"),
		BackupPath:      filepath.Join(dir, "favorites-backup.json"),
		NotificationIDs: []string{},
		RecentFailures:  []core.RefreshFailure{},
		SidebarWidth:    defaultSidebarWidth,
		DetailsWidth:    defaultDetailsWidth,
		MaxGridColumns:  defaultMaxGridColumns,
		ExpandedGroups:  map[string]bool{},
		WorkspaceOrder:  append([]string(nil), DefaultWorkspaceOrder...),
		ActiveWorkspace: WorkspaceVideos,
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
	settings.APIKey = strings.TrimSpace(settings.APIKey)
	// A zero refresh interval is Manual, a choice the Settings window
	// offers, so it is kept as it is. A file without the key keeps the
	// default the struct started with.
	if settings.SyncPath == "" {
		settings.SyncPath = filepath.Join(dir, "youtube-reader-sync.json")
	}
	if settings.BackupPath == "" {
		settings.BackupPath = filepath.Join(dir, "favorites-backup.json")
	}
	if settings.MaxGridColumns < minMaxGridColumns || settings.MaxGridColumns > maxMaxGridColumns {
		settings.MaxGridColumns = defaultMaxGridColumns
	}
	if err := reconcileHelperInterval(settings.RefreshMinutes); err != nil {
		log.Printf("helper schedule: %v", err)
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
	app := &App{Settings: settings, Sync: state, Store: store, Dir: dir, DeviceID: device, Lock: lock, Selected: defaultDestination}
	if err := app.reload(); err != nil {
		store.Close()
		lock.Release()
		return nil, err
	}
	// The destination is restored after the reload, because whether the
	// stored group or channel still exists is a question only the loaded
	// library answers. A notification click that launched the app runs
	// later and overrides what this restored.
	app.Selected = app.restoredDestination(app.settingsSnapshot().LastSelectedDestination)
	return app, nil
}

// defaultDestination is the sidebar entry a launch opens on when no stored
// destination survives, and the value the sidebar itself starts from.
const defaultDestination = "all"

// restoredDestination returns the destination a launch should open on. The
// stored value is accepted only while it still names something the sidebar
// shows: a group or channel that was removed falls back to All videos, so a
// launch never opens on a pane its destination cannot fill.
func (a *App) restoredDestination(stored string) string {
	switch {
	case stored == "all" || stored == "favorites":
		return stored
	case destinationID(stored, "group:") != "":
		id := destinationID(stored, "group:")
		for _, g := range a.Groups {
			if g.ID == id {
				return stored
			}
		}
	case destinationID(stored, "channel:") != "":
		id := destinationID(stored, "channel:")
		for _, ch := range a.Channels {
			if ch.ID == id {
				return stored
			}
		}
	}
	return defaultDestination
}

// SelectDestination is the one place the sidebar destination changes. Every
// entry point goes through it - each sidebar row and the keyboard navigation
// that runs those rows, the empty pane's way back to the library, and the
// notification click's channel jump - so the stored destination always names
// what the reader left selected. It stays in local settings and is never
// synced.
func (a *App) SelectDestination(destination string) {
	if a.Selected != destination {
		// A history page's failure belongs to the channel page it happened on.
		a.BackfillErr = ""
	}
	a.Selected = destination
	_, _ = a.updateSettings(func(s *core.Settings) bool {
		if s.LastSelectedDestination == destination {
			return false
		}
		s.LastSelectedDestination = destination
		return true
	})
}

// channelTitle is the title a group sorts a channel by: the sync state's, or
// the library's for a channel the sync state does not hold yet, as one being
// moved in from the sidebar may not be. An unknown channel answers an empty
// title, which sorts by its ID.
func (a *App) channelTitle(id string) string {
	if ch, ok := a.Sync.Channels[id]; ok {
		return ch.Title
	}
	for _, ch := range a.Channels {
		if ch.ID == id {
			return ch.Title
		}
	}
	return ""
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
	// Groups keep their channels in title order, and state written before this
	// version did is put in that order here, so an existing library shows
	// sorted on its first launch instead of waiting on a migration.
	a.Sync.SortAllGroupChannels()
	a.BackfillCursors, err = a.Store.BackfillCursors(ctx)
	if err != nil {
		return err
	}
	a.Groups = orderGroups(a.Sync.Groups, a.settingsSnapshot().GroupOrder)
	if rev := a.Store.Meta(ctx, "revision"); rev != "" {
		fmt.Sscan(rev, &a.Revision)
	}
	return nil
}

// orderGroups arranges the synced groups the way the sidebar shows them: the
// order the user left them in, then every group that order does not name in
// name order. Stored IDs with no group are ignored, so a group removed
// elsewhere leaves no gap, and a group the stored order has never seen, as
// one another device imported, joins the end alphabetically.
func orderGroups(groups map[string]core.ChannelGroup, stored []string) []core.ChannelGroup {
	ordered := make([]core.ChannelGroup, 0, len(groups))
	placed := make(map[string]bool, len(groups))
	for _, id := range stored {
		if g, ok := groups[id]; ok && !placed[id] {
			placed[id] = true
			ordered = append(ordered, g)
		}
	}
	rest := make([]core.ChannelGroup, 0, len(groups)-len(ordered))
	for id, g := range groups {
		if !placed[id] {
			rest = append(rest, g)
		}
	}
	sort.Slice(rest, func(i, j int) bool {
		if rest[i].Name != rest[j].Name {
			return rest[i].Name < rest[j].Name
		}
		return rest[i].ID < rest[j].ID
	})
	return append(ordered, rest...)
}

// groupIDs names the groups in the order given.
func groupIDs(groups []core.ChannelGroup) []string {
	ids := make([]string, 0, len(groups))
	for _, g := range groups {
		ids = append(ids, g.ID)
	}
	return ids
}

// SetGroupOrder stores the sidebar's group order in local settings. The order
// belongs to this screen, so it stays in settings.json and is never synced.
func (a *App) SetGroupOrder(ids []string) error {
	_, err := a.updateSettings(func(s *core.Settings) bool {
		s.GroupOrder = append([]string(nil), ids...)
		return true
	})
	return err
}

// WorkspaceOrder returns the current workspace order, falling back to the default.
func (a *App) WorkspaceOrder() []string {
	a.settingsMu.RLock()
	defer a.settingsMu.RUnlock()
	return sanitizeWorkspaceOrder(a.Settings.WorkspaceOrder)
}

// SetWorkspaceOrder stores the workspace order in local settings.
func (a *App) SetWorkspaceOrder(order []string) error {
	sanitized := sanitizeWorkspaceOrder(order)
	_, err := a.updateSettings(func(s *core.Settings) bool {
		s.WorkspaceOrder = append([]string(nil), sanitized...)
		return true
	})
	return err
}

// SetActiveWorkspace stores the active workspace in local settings.
func (a *App) SetActiveWorkspace(id string) error {
	_, err := a.updateSettings(func(s *core.Settings) bool {
		if s.ActiveWorkspace == id {
			return false
		}
		s.ActiveWorkspace = id
		return true
	})
	return err
}

// RenameGroup updates a group's name across sync and local state, and persists
// it to the sync file.
func (a *App) RenameGroup(id, newName string) error {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return errors.New("group name cannot be empty")
	}
	now := time.Now().UnixMilli()
	if a.Sync.Groups == nil {
		a.Sync.Groups = map[string]core.ChannelGroup{}
	}
	if !a.Sync.RenameGroup(id, newName, now) {
		found := false
		for _, cg := range a.Groups {
			if cg.ID == id {
				cg.Name = newName
				cg.UpdatedAt = now
				a.Sync.Groups[id] = cg
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("group %q not found", id)
		}
		if a.Sync.UpdatedAt < now {
			a.Sync.UpdatedAt = now
		}
	}
	for i := range a.Groups {
		if a.Groups[i].ID == id {
			a.Groups[i].Name = newName
			a.Groups[i].UpdatedAt = now
			break
		}
	}
	return a.SaveSync()
}

// channelGroupIDs returns the IDs of the groups the channel currently belongs to.
func (a *App) channelGroupIDs(channelID string) []string {
	if a.Sync.Channels != nil {
		if ch, ok := a.Sync.Channels[channelID]; ok && len(ch.GroupIDs) > 0 {
			return append([]string(nil), ch.GroupIDs...)
		}
	}
	for _, ch := range a.Channels {
		if ch.ID == channelID && len(ch.GroupIDs) > 0 {
			return append([]string(nil), ch.GroupIDs...)
		}
	}
	var ids []string
	for _, g := range a.Groups {
		for _, cid := range g.ChannelIDs {
			if cid == channelID {
				ids = append(ids, g.ID)
				break
			}
		}
	}
	return ids
}

// channelOnlyInGroup reports whether channelID belongs solely to groupID.
func (a *App) channelOnlyInGroup(channelID, groupID string) bool {
	gids := a.channelGroupIDs(channelID)
	return len(gids) == 1 && gids[0] == groupID
}

// channelIsUngrouped reports whether channelID belongs to no group.
func (a *App) channelIsUngrouped(channelID string) bool {
	return len(a.channelGroupIDs(channelID)) == 0
}

// MoveChannelToGroup moves a channel to the given target group (or to ungrouped
// if targetGroupID is empty), enforcing single membership. It updates both
// directions of the relationship, bumps UpdatedAt timestamps on modified
// groups, the channel, and sync state, updates the local store, and persists
// the sync file.
// If the channel already belongs solely to targetGroupID (or is already ungrouped
// when targetGroupID is empty), it is a no-op that performs no writes.
func (a *App) MoveChannelToGroup(channelID string, targetGroupID string) (bool, error) {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return false, errors.New("channel ID cannot be empty")
	}

	targetGroupID = strings.TrimSpace(targetGroupID)
	if targetGroupID != "" {
		found := false
		if a.Sync.Groups != nil {
			_, found = a.Sync.Groups[targetGroupID]
		}
		if !found {
			for _, g := range a.Groups {
				if g.ID == targetGroupID {
					found = true
					break
				}
			}
		}
		if !found {
			return false, fmt.Errorf("target group %q not found", targetGroupID)
		}
	}

	// No-op check:
	if targetGroupID != "" && a.channelOnlyInGroup(channelID, targetGroupID) {
		return false, nil
	}
	if targetGroupID == "" && a.channelIsUngrouped(channelID) {
		return false, nil
	}

	now := time.Now().UnixMilli()

	if a.Sync.Groups == nil {
		a.Sync.Groups = make(map[string]core.ChannelGroup)
	}
	if a.Sync.Channels == nil {
		a.Sync.Channels = make(map[string]core.Channel)
	}

	// Ensure all in-memory groups are in a.Sync.Groups
	for _, g := range a.Groups {
		if _, ok := a.Sync.Groups[g.ID]; !ok {
			a.Sync.Groups[g.ID] = g
		}
	}

	// 1. Remove channelID from every group other than targetGroupID.
	for gid, g := range a.Sync.Groups {
		if gid != targetGroupID {
			var newIDs []string
			removed := false
			for _, cid := range g.ChannelIDs {
				if cid == channelID {
					removed = true
				} else {
					newIDs = append(newIDs, cid)
				}
			}
			if removed {
				g.ChannelIDs = newIDs
				g.UpdatedAt = now
				a.Sync.Groups[gid] = g
			}
		}
	}

	// Ensure targetGroupID has channelID
	if targetGroupID != "" {
		tg := a.Sync.Groups[targetGroupID]
		hasChan := false
		for _, cid := range tg.ChannelIDs {
			if cid == channelID {
				hasChan = true
				break
			}
		}
		if !hasChan {
			tg.ChannelIDs = append(tg.ChannelIDs, channelID)
			core.SortChannelIDs(tg.ChannelIDs, a.channelTitle)
		}
		tg.UpdatedAt = now
		a.Sync.Groups[targetGroupID] = tg
	}

	// Synchronize in-memory a.Groups slice
	for i := range a.Groups {
		gid := a.Groups[i].ID
		if g, ok := a.Sync.Groups[gid]; ok {
			a.Groups[i] = g
		}
	}

	// 2. Update channel record in Sync.Channels and a.Channels
	ch, ok := a.Sync.Channels[channelID]
	if !ok {
		for _, c := range a.Channels {
			if c.ID == channelID {
				ch = c
				ok = true
				break
			}
		}
	}
	if !ok {
		ch = core.Channel{ID: channelID, Title: channelID}
	}

	if targetGroupID != "" {
		ch.GroupIDs = []string{targetGroupID}
	} else {
		ch.GroupIDs = nil
	}
	ch.UpdatedAt = now
	a.Sync.Channels[channelID] = ch

	// Synchronize in-memory a.Channels slice
	foundInSlice := false
	for i := range a.Channels {
		if a.Channels[i].ID == channelID {
			a.Channels[i].GroupIDs = ch.GroupIDs
			a.Channels[i].UpdatedAt = now
			foundInSlice = true
			break
		}
	}
	if !foundInSlice {
		a.Channels = append(a.Channels, ch)
	}

	// 3. Update Sync.UpdatedAt
	a.Sync.UpdatedAt = now

	// 4. Update local SQLite store if present
	if a.Store != nil {
		ctx := context.Background()
		_ = a.Store.SetChannelGroupIDs(ctx, channelID, ch.GroupIDs, now)
	}

	// 5. Persist sync state
	if err := a.SaveSync(); err != nil {
		return false, err
	}

	return true, nil
}

// CreateGroup adds an empty group the reader made in the app, at the end of the
// sidebar's order. The ID is fresh rather than derived from the name, so two
// groups of one name never share an ID and overwrite each other.
func (a *App) CreateGroup(name string) (core.ChannelGroup, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return core.ChannelGroup{}, errors.New("group name cannot be empty")
	}
	now := time.Now().UnixMilli()
	group := core.ChannelGroup{ID: a.freshGroupID(), Name: name, UpdatedAt: now}
	if a.Sync.Groups == nil {
		a.Sync.Groups = map[string]core.ChannelGroup{}
	}
	a.Sync.Groups[group.ID] = group
	if a.Sync.UpdatedAt < now {
		a.Sync.UpdatedAt = now
	}
	if err := a.SaveSync(); err != nil {
		delete(a.Sync.Groups, group.ID)
		return core.ChannelGroup{}, err
	}
	// The new group joins the order at its end, so it appears where the reader
	// just looked for it and the stored order keeps naming every group.
	_, _ = a.updateSettings(func(s *core.Settings) bool {
		s.GroupOrder = append(groupIDs(a.Groups), group.ID)
		return true
	})
	if err := a.reload(); err != nil {
		return group, err
	}
	return group, nil
}

// freshGroupID returns a group ID no group in the sync state uses.
func (a *App) freshGroupID() string {
	for i := 0; i < 8; i++ {
		id := core.NewGroupID()
		if _, exists := a.Sync.Groups[id]; !exists {
			return id
		}
	}
	return core.NewGroupID()
}

// DeleteGroup removes a group the reader gave up on while naming it. It only
// removes an empty group: the naming path's undo is not a destructive command,
// so a group that holds channels is left as it is.
func (a *App) DeleteGroup(id string) error {
	group, ok := a.Sync.Groups[id]
	if !ok || len(group.ChannelIDs) > 0 {
		return nil
	}
	delete(a.Sync.Groups, id)
	for channelID, channel := range a.Sync.Channels {
		if !slices.Contains(channel.GroupIDs, id) {
			continue
		}
		kept := make([]string, 0, len(channel.GroupIDs))
		for _, groupID := range channel.GroupIDs {
			if groupID != id {
				kept = append(kept, groupID)
			}
		}
		channel.GroupIDs = kept
		a.Sync.Channels[channelID] = channel
	}
	a.Sync.UpdatedAt = time.Now().UnixMilli()
	if err := a.SaveSync(); err != nil {
		return err
	}
	_, _ = a.updateSettings(func(s *core.Settings) bool {
		order := make([]string, 0, len(s.GroupOrder))
		for _, groupID := range s.GroupOrder {
			if groupID != id {
				order = append(order, groupID)
			}
		}
		s.GroupOrder = order
		return true
	})
	return a.reload()
}

// AddSubscription writes a channel the reader added into the library and into
// the group they put it in, and commits its row so the sidebar shows it before
// any video arrives. A channel without a title is refused: the library never
// holds a subscription it cannot name.
func (a *App) AddSubscription(channel core.Channel, groupID string) error {
	channel.ID = strings.TrimSpace(channel.ID)
	channel.Title = strings.TrimSpace(channel.Title)
	if channel.ID == "" || channel.Title == "" {
		return errors.New("channel has no title")
	}
	if _, exists := a.Sync.Channels[channel.ID]; exists {
		return fmt.Errorf("channel %q is already subscribed", channel.ID)
	}
	if groupID != "" {
		if _, ok := a.Sync.Groups[groupID]; !ok {
			return fmt.Errorf("group %q not found", groupID)
		}
	}
	now := time.Now().UnixMilli()
	channel.UpdatedAt = now
	channel.GroupIDs = nil
	if groupID != "" {
		channel.GroupIDs = []string{groupID}
	}
	if a.Sync.Channels == nil {
		a.Sync.Channels = map[string]core.Channel{}
	}
	a.Sync.Channels[channel.ID] = channel
	if groupID != "" {
		group := a.Sync.Groups[groupID]
		if !slices.Contains(group.ChannelIDs, channel.ID) {
			group.ChannelIDs = append(group.ChannelIDs, channel.ID)
			core.SortChannelIDs(group.ChannelIDs, a.channelTitle)
		}
		group.UpdatedAt = now
		a.Sync.Groups[groupID] = group
	}
	if a.Sync.UpdatedAt < now {
		a.Sync.UpdatedAt = now
	}
	if err := a.SaveSync(); err != nil {
		delete(a.Sync.Channels, channel.ID)
		if groupID != "" {
			group := a.Sync.Groups[groupID]
			kept := make([]string, 0, len(group.ChannelIDs))
			for _, id := range group.ChannelIDs {
				if id != channel.ID {
					kept = append(kept, id)
				}
			}
			group.ChannelIDs = kept
			a.Sync.Groups[groupID] = group
		}
		return err
	}
	if err := a.commitChannelRows(channel); err != nil {
		return err
	}
	return a.reload()
}

// UnsubscribeChannel removes a channel from the library: its sync record,
// its presence in every group, its ordinary videos in the local store, and its
// notification consent. Favorited videos are permanently kept. If the channel
// was selected in the sidebar, navigation falls back to All videos.
func (a *App) UnsubscribeChannel(channelID string) error {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return errors.New("channel id is empty")
	}

	// 1. Snapshot sync state for safe rollback on failure.
	oldSync := cloneSyncState(a.Sync)

	// 2. Identify favorite videos that must be preserved.
	keepIDs := make(map[string]bool)
	for id, fav := range a.Sync.Favorites {
		if fav.Video != nil && fav.Video.ChannelID == channelID {
			keepIDs[id] = true
		}
	}
	for _, v := range a.Videos {
		if v.ChannelID == channelID {
			if v.FavoritedAt != 0 || a.Sync.IsFavorite(v.ID) {
				keepIDs[v.ID] = true
			}
		}
	}
	keepList := make([]string, 0, len(keepIDs))
	for id := range keepIDs {
		keepList = append(keepList, id)
	}

	// 3. Mutate sync state: delete from Sync.Channels and filter from every group.
	now := time.Now().UnixMilli()
	delete(a.Sync.Channels, channelID)
	for gid, group := range a.Sync.Groups {
		if slices.Contains(group.ChannelIDs, channelID) {
			kept := make([]string, 0, len(group.ChannelIDs)-1)
			for _, id := range group.ChannelIDs {
				if id != channelID {
					kept = append(kept, id)
				}
			}
			group.ChannelIDs = kept
			group.UpdatedAt = now
			a.Sync.Groups[gid] = group
		}
	}
	if a.Sync.UpdatedAt < now {
		a.Sync.UpdatedAt = now
	}

	// 4. Persist sync state.
	if err := a.SaveSync(); err != nil {
		a.Sync = oldSync
		return err
	}

	// 5. Clean local SQLite store: remove channel and un-favorited videos.
	if a.Store != nil {
		ctx := context.Background()
		if err := a.Store.DeleteChannel(ctx, channelID, keepList); err != nil {
			a.Sync = oldSync
			_ = a.SaveSync()
			return err
		}
	}

	// 6. Remove channel from notification preferences.
	_, _ = a.updateSettings(func(s *core.Settings) bool {
		if !slices.Contains(s.NotificationIDs, channelID) {
			return false
		}
		kept := make([]string, 0, len(s.NotificationIDs)-1)
		for _, id := range s.NotificationIDs {
			if id != channelID {
				kept = append(kept, id)
			}
		}
		s.NotificationIDs = kept
		return true
	})

	// 7. Reset selection if current selection or filter is this channel.
	if a.Selected == "channel:"+channelID {
		a.SelectDestination("all")
	}
	if a.ChannelFilter == channelID {
		a.ChannelFilter = ""
	}
	if a.SelectedVideoID != "" {
		if _, ok := keepIDs[a.SelectedVideoID]; !ok {
			for _, v := range a.Videos {
				if v.ID == a.SelectedVideoID && v.ChannelID == channelID {
					a.SelectedVideoID = ""
					break
				}
			}
		}
	}

	// 8. Reload local memory state from store and sync.
	return a.reload()
}

// commitChannelRows writes the channel rows the library now holds into the
// local store, so a channel added or moved shows with the title and avatar the
// lookup found before any video refreshes it. The refresh revision is left as
// it is: adding a subscription is not a refresh.
func (a *App) commitChannelRows(channels ...core.Channel) error {
	if a.Store == nil {
		return nil
	}
	return a.Store.CommitRefresh(context.Background(), core.RefreshCommit{Channels: channels, Revision: a.Revision})
}

// QuotaRetryAt reports when the pause a quota failure started ends. It is the
// one time every surface names, so the dialog's retry sentence and the status
// item never disagree.
func (a *App) QuotaRetryAt(now time.Time) (time.Time, bool) {
	settings := a.settingsSnapshot()
	until, err := time.Parse(time.RFC3339, settings.QuotaPausedUntil)
	if err != nil || !now.Before(until) {
		return time.Time{}, false
	}
	return until, true
}

// NoteQuotaPause arms the pause a quota failure starts, 24 hours long as the
// refresh arms it, so the add-subscription dialog's retry sentence names the
// same time the rest of the app waits for.
func (a *App) NoteQuotaPause(now time.Time) {
	until := now.Add(24 * time.Hour)
	_, _ = a.updateSettings(func(s *core.Settings) bool {
		if existing, err := time.Parse(time.RFC3339, s.QuotaPausedUntil); err == nil && !existing.Before(until) {
			return false
		}
		s.QuotaPausedUntil = core.FormatRFC3339(until)
		return true
	})
}

// appYouTubeClient builds the YouTube client the app reads through: the
// dialog's lookup and the fetch that follows an add. The tests replace it to
// count and answer requests without a network.
var appYouTubeClient = func(apiKey string) core.YouTube {
	return core.YouTube{Key: apiKey}
}

// ApplyChannelFetch commits what the fetch of one just-added channel found: its
// refreshed metadata, and the videos it has now. The window calls it on the main
// thread, as a refresh applies its channels, so the maps a frame reads are never
// written from a goroutine. It reports whether it committed anything.
func (a *App) ApplyChannelFetch(channel core.Channel, videos []core.Video) bool {
	stored, ok := a.Sync.Channels[channel.ID]
	if !ok || strings.TrimSpace(channel.Title) == "" {
		return false
	}
	// The group the reader chose is the add's, not the fetch's: the lookup only
	// brings metadata and videos.
	channel.GroupIDs = stored.GroupIDs
	channel.UpdatedAt = time.Now().UnixMilli()
	a.Sync.Channels[channel.ID] = channel
	if err := a.SaveSync(); err != nil {
		return false
	}
	if a.Store != nil {
		_ = a.Store.CommitRefresh(context.Background(), core.RefreshCommit{
			Channels: []core.Channel{channel},
			Videos:   videos,
			Revision: a.Revision + 1,
		})
	}
	return a.reload() == nil
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
	a.SaveSyncCalls++
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
	known := make(map[string]bool, len(a.Sync.Groups))
	for id := range a.Sync.Groups {
		known[id] = true
	}
	now := time.Now().UnixMilli()
	a.Sync.MergeImported(mergedGroups, mergedChannels, now)
	if err := a.SaveSync(); err != nil {
		return err
	}
	// Remember the order the groups now show in: the ones already here keep
	// the order the user left them in, and a group this import adds for the
	// first time joins the end in the order its OPML file lists it.
	_, _ = a.updateSettings(func(s *core.Settings) bool {
		existing := make(map[string]core.ChannelGroup, len(known))
		for id := range known {
			existing[id] = a.Sync.Groups[id]
		}
		order := groupIDs(orderGroups(existing, s.GroupOrder))
		for _, g := range mergedGroups {
			if known[g.ID] {
				continue
			}
			known[g.ID] = true
			order = append(order, g.ID)
		}
		s.GroupOrder = order
		return true
	})
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
	// A replace starts the groups over, so the OPML document order is their
	// first-seen order and the sidebar shows it until the user reorders.
	_, _ = a.updateSettings(func(s *core.Settings) bool {
		s.GroupOrder = groupIDs(groups)
		return true
	})
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

// refreshBlocked reports whether a refresh cannot run now: a missing key or a
// quota pause are the two conditions core.RefreshYoutube refuses. Every refresh
// surface reads this one answer, so none offers a refresh that would return
// without doing anything.
func (a *App) refreshBlocked(now time.Time) bool {
	settings := a.settingsSnapshot()
	if settings.APIKey == "" {
		return true
	}
	if until, err := time.Parse(time.RFC3339, settings.QuotaPausedUntil); err == nil && now.Before(until) {
		return true
	}
	return false
}

func (a *App) Refresh(force bool, snapshot refreshSnapshot) RefreshOutcome {
	outcome, _ := a.RefreshContext(context.Background(), force, snapshot)
	return outcome
}

// RefreshContext performs one refresh under a caller's context, so the window
// can offer Cancel while the initial-load skeleton is the only thing on screen.
// It reports whether the reader ended the attempt before its result was applied.
// A cancelled attempt records no failure and no refresh stamp: the reader
// ended it, and the next refresh must not inherit a failure they did not cause.
func (a *App) RefreshContext(ctx context.Context, force bool, snapshot refreshSnapshot) (RefreshOutcome, bool) {
	now := time.Now()
	settings := a.settingsSnapshot()
	client := core.YouTube{Key: settings.APIKey}
	// RefreshYoutube commits refreshed channel metadata into the state's
	// channel map; the snapshot must come back holding the records the
	// refresh started from, so the writeback guard can still tell a
	// concurrent edit apart from the refresh's own update.
	outcome := core.RefreshYoutube(ctx, client, settings, cloneSyncState(snapshot.state), a.Store, snapshot.videos, snapshot.revision, force, now)
	if ctx.Err() != nil && !outcome.Changed {
		return RefreshOutcome{Description: "Cancelled"}, true
	}
	_, _ = a.updateSettings(func(s *core.Settings) bool {
		if outcome.Changed {
			s.LastRefresh = core.FormatRFC3339(now)
			s.RecentFailures = outcome.Failures
		} else if len(outcome.Failures) > 0 {
			s.RecentFailures = outcome.Failures
		}
		if core.FailedOnQuota(outcome.Failures) {
			s.QuotaPausedUntil = core.FormatRFC3339(now.Add(24 * time.Hour))
		}
		return true
	})
	return outcome, false
}

// applyRefreshedChannels writes the channel metadata a refresh produced into
// the sync state and persists it, so the Notifications pane and the sync file
// carry the titles and avatars YouTube has now. A refresh refreshes the
// subscriptions the sync state holds; it never adds or removes one, and a
// record that changed since the refresh snapshot is left alone: the writeback
// lands only while the stored record is still the one the snapshot copied.
func (a *App) applyRefreshedChannels(channels []core.Channel, snapshotChannels map[string]core.Channel) {
	if len(channels) == 0 {
		return
	}
	for _, channel := range channels {
		stored, ok := a.Sync.Channels[channel.ID]
		if !ok {
			continue
		}
		base, ok := snapshotChannels[channel.ID]
		if !ok || !sameChannelRecord(stored, base) {
			continue
		}
		a.Sync.Channels[channel.ID] = channel
	}
	_ = a.SaveSync()
}

// sameChannelRecord reports whether two records hold the same fields, so a
// record's identity never rests on its UpdatedAt alone.
func sameChannelRecord(a, b core.Channel) bool {
	return a.Title == b.Title &&
		a.AvatarURL == b.AvatarURL &&
		a.UploadedAt == b.UploadedAt &&
		a.UpdatedAt == b.UpdatedAt &&
		slices.Equal(a.GroupIDs, b.GroupIDs)
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

// channelSnapshot copies the subscriptions the sync state holds, so a pane or
// action never iterates the live map a refresh could touch.
func (a *App) channelSnapshot() []core.Channel {
	channels := make([]core.Channel, 0, len(a.Sync.Channels))
	for _, channel := range a.Sync.Channels {
		channels = append(channels, channel)
	}
	return channels
}

// EnableAllNotifications turns on notifications for every subscription in the
// sync state, whichever main-window view happens to be selected. It reports
// whether any consent was newly recorded.
func (a *App) EnableAllNotifications() bool {
	channels := a.channelSnapshot()
	changed, _ := a.updateSettings(func(s *core.Settings) bool {
		known := make(map[string]bool, len(s.NotificationIDs)+len(channels))
		for _, id := range s.NotificationIDs {
			known[id] = true
		}
		changed := false
		for _, channel := range channels {
			id := channel.ID
			if id == "" || known[id] {
				continue
			}
			s.NotificationIDs = append(s.NotificationIDs, id)
			known[id] = true
			changed = true
		}
		return changed
	})
	return changed
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
		ID          string                  `json:"id"`
		CreatedAt   string                  `json:"createdAt"`
		Description string                  `json:"description"`
		Result      *core.RefreshResult     `json:"result"`
		Notified    []core.NotificationPlan `json:"notified"`
		Failures    []core.RefreshFailure   `json:"failures"`
	}
	if err := json.Unmarshal(data, &handoff); err != nil {
		return nil
	}
	currentStatus := a.settingsSnapshot()
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
	_, _ = a.updateSettings(func(s *core.Settings) bool {
		if fresh && len(handoff.Failures) > 0 {
			s.RecentFailures = handoff.Failures
			if core.FailedOnQuota(handoff.Failures) {
				if at, err := time.Parse(time.RFC3339, handoff.Failures[0].At); err == nil {
					s.QuotaPausedUntil = core.FormatRFC3339(at.Add(24 * time.Hour))
				}
			}
		}
		if fresh && handoff.Result != nil && handoff.CreatedAt != "" {
			s.LastRefresh = handoff.CreatedAt
		}
		s.LastHandoffID = handoff.ID
		return true
	})
	_ = os.Remove(path)
	return a.reload()
}

// SelectChannel routes the UI to a channel's filtered view, expands its
// owning group if collapsed, and clears transient search/unread filters so
// the destination is unambiguous.
func (a *App) SelectChannel(channelID string) bool {
	if channelID == "" {
		return false
	}
	a.SelectDestination("channel:" + channelID)
	a.ChannelFilter = ""
	a.Search = ""
	a.UnreadOnly = false
	a.SelectedVideoID = ""
	a.expandGroupForChannel(channelID)
	return true
}

func (a *App) expandGroupForChannel(channelID string) {
	for _, g := range a.Groups {
		if slices.Contains(g.ChannelIDs, channelID) {
			if a.GroupCollapsed(g.ID) {
				_ = a.SetGroupCollapsed(g.ID, false)
			}
		}
	}
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
		gid := strings.TrimPrefix(selected, "group:")
		if g, ok := a.Sync.Groups[gid]; ok {
			allowed = map[string]bool{}
			for _, id := range g.ChannelIDs {
				allowed[id] = true
			}
		} else {
			for _, g := range a.Groups {
				if g.ID == gid {
					allowed = map[string]bool{}
					for _, id := range g.ChannelIDs {
						allowed[id] = true
					}
					break
				}
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
