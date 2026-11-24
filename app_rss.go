package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/simon/youtube-reader/core"
)

// orderRSSGroups arranges the groups in the order specified by storedOrder,
// followed by any groups not in storedOrder sorted by SortOrder and name.
func orderRSSGroups(groups []core.RSSGroup, storedOrder []string) []core.RSSGroup {
	if len(groups) <= 1 {
		return groups
	}
	byID := make(map[string]core.RSSGroup, len(groups))
	for _, g := range groups {
		byID[g.ID] = g
	}
	ordered := make([]core.RSSGroup, 0, len(groups))
	seen := make(map[string]bool, len(groups))
	for _, id := range storedOrder {
		if g, ok := byID[id]; ok && !seen[id] {
			ordered = append(ordered, g)
			seen[id] = true
		}
	}
	var remaining []core.RSSGroup
	for _, g := range groups {
		if !seen[g.ID] {
			remaining = append(remaining, g)
		}
	}
	sort.SliceStable(remaining, func(i, j int) bool {
		if remaining[i].SortOrder != remaining[j].SortOrder {
			return remaining[i].SortOrder < remaining[j].SortOrder
		}
		return strings.ToLower(remaining[i].Name) < strings.ToLower(remaining[j].Name)
	})
	return append(ordered, remaining...)
}

// orderRSSFeeds orders feeds based on storedOrder, followed by any remaining feeds sorted by display name.
func orderRSSFeeds(feeds []core.RSSFeed, storedOrder []string) []core.RSSFeed {
	if len(feeds) <= 1 {
		return feeds
	}
	byID := make(map[string]core.RSSFeed, len(feeds))
	for _, f := range feeds {
		byID[f.ID] = f
	}
	ordered := make([]core.RSSFeed, 0, len(feeds))
	seen := make(map[string]bool, len(feeds))
	for _, id := range storedOrder {
		if f, ok := byID[id]; ok && !seen[id] {
			ordered = append(ordered, f)
			seen[id] = true
		}
	}
	var remaining []core.RSSFeed
	for _, f := range feeds {
		if !seen[f.ID] {
			remaining = append(remaining, f)
		}
	}
	sort.SliceStable(remaining, func(i, j int) bool {
		return strings.ToLower(remaining[i].DisplayName()) < strings.ToLower(remaining[j].DisplayName())
	})
	return append(ordered, remaining...)
}

// ReloadRSS refreshes all in-memory RSS feeds, groups, and counts from SQLite.
func (a *App) ReloadRSS() error {
	if a.Store == nil {
		return nil
	}
	ctx := context.Background()
	feeds, err := a.Store.ListRSSFeeds(ctx, false)
	if err != nil {
		return err
	}
	groups, err := a.Store.ListRSSGroups(ctx, false)
	if err != nil {
		return err
	}

	unreadMap, _ := a.Store.RSSUnreadCounts(ctx)
	if unreadMap == nil {
		unreadMap = make(map[string]int)
	}
	totalUnread, _ := a.Store.RSSTotalUnreadCount(ctx)
	starred, _ := a.Store.RSSStarredCount(ctx)

	a.RSSFeeds = feeds
	a.RSSGroups = orderRSSGroups(groups, a.settingsSnapshot().RSSGroupOrder)
	a.RSSUnreadMap = unreadMap
	a.RSSTotalUnread = totalUnread
	a.RSSStarredTotal = starred
	return nil
}

// RSSGroupCollapsed reports whether an RSS group is folded in the sidebar.
func (a *App) RSSGroupCollapsed(id string) bool {
	a.settingsMu.RLock()
	defer a.settingsMu.RUnlock()
	return a.Settings.RSSExpandedGroups != nil && a.Settings.RSSExpandedGroups[id]
}

// SetRSSGroupCollapsed stores the fold state of an RSS group in local settings.
func (a *App) SetRSSGroupCollapsed(id string, collapsed bool) error {
	_, err := a.updateSettings(func(s *core.Settings) bool {
		if s.RSSExpandedGroups == nil {
			s.RSSExpandedGroups = make(map[string]bool)
		}
		if s.RSSExpandedGroups[id] == collapsed {
			return false
		}
		s.RSSExpandedGroups[id] = collapsed
		return true
	})
	return err
}

// HasRSSFeed reports whether a feed with feedID is already subscribed.
func (a *App) HasRSSFeed(feedID string) bool {
	for _, f := range a.RSSFeeds {
		if f.ID == feedID {
			return true
		}
	}
	return false
}

// RSSFeedByID returns the feed matching feedID, or an empty RSSFeed if not found.
func (a *App) RSSFeedByID(feedID string) core.RSSFeed {
	for _, f := range a.RSSFeeds {
		if f.ID == feedID {
			return f
		}
	}
	return core.RSSFeed{}
}

// RSSGroupByID returns the group matching groupID, or an empty RSSGroup if not found.
func (a *App) RSSGroupByID(groupID string) core.RSSGroup {
	for _, g := range a.RSSGroups {
		if g.ID == groupID {
			return g
		}
	}
	return core.RSSGroup{}
}

// CreateRSSGroup creates a new RSS group with the given name, appends it to the group order,
// and persists it to the local store.
func (a *App) CreateRSSGroup(name string) (core.RSSGroup, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return core.RSSGroup{}, errors.New("group name cannot be empty")
	}

	groupID := "rss-group-" + core.NewGroupID()
	now := time.Now().UnixMilli()
	group := core.RSSGroup{
		ID:        groupID,
		Name:      name,
		SortOrder: len(a.RSSGroups),
		UpdatedAt: now,
	}

	if a.Store != nil {
		ctx := context.Background()
		if err := a.Store.SaveRSSGroup(ctx, group); err != nil {
			return core.RSSGroup{}, err
		}
	}

	// Update stored group order
	var newOrder []string
	for _, g := range a.RSSGroups {
		newOrder = append(newOrder, g.ID)
	}
	newOrder = append(newOrder, groupID)
	_ = a.SetRSSGroupOrder(newOrder)

	_ = a.ReloadRSS()
	return group, nil
}

// RenameRSSGroup updates an RSS group's name in SQLite and in-memory state.
func (a *App) RenameRSSGroup(id, newName string) error {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return errors.New("group name cannot be empty")
	}

	if a.Store != nil {
		ctx := context.Background()
		g, err := a.Store.GetRSSGroup(ctx, id)
		if err != nil {
			return err
		}
		if g == nil {
			return fmt.Errorf("group %q not found", id)
		}
		g.Name = newName
		g.UpdatedAt = time.Now().UnixMilli()
		if err := a.Store.SaveRSSGroup(ctx, *g); err != nil {
			return err
		}
	}

	return a.ReloadRSS()
}

// DeleteRSSGroup removes an RSS group from the store and ungroups any feeds that belonged to it.
func (a *App) DeleteRSSGroup(id string) error {
	if a.Store != nil {
		ctx := context.Background()
		g, err := a.Store.GetRSSGroup(ctx, id)
		if err == nil && g != nil {
			// Ungroup any feeds in this group
			for _, feedID := range g.FeedIDs {
				if feed, fErr := a.Store.GetRSSFeed(ctx, feedID); fErr == nil && feed != nil {
					newGroups := make([]string, 0, len(feed.GroupIDs))
					for _, gid := range feed.GroupIDs {
						if gid != id {
							newGroups = append(newGroups, gid)
						}
					}
					feed.GroupIDs = newGroups
					feed.UpdatedAt = time.Now().UnixMilli()
					_ = a.Store.SaveRSSFeed(ctx, *feed)
				}
			}
		}

		if err := a.Store.DeleteRSSGroup(ctx, id, false); err != nil {
			return err
		}
	}

	// Remove from Settings.RSSGroupOrder
	_, _ = a.updateSettings(func(s *core.Settings) bool {
		var kept []string
		for _, gid := range s.RSSGroupOrder {
			if gid != id {
				kept = append(kept, gid)
			}
		}
		s.RSSGroupOrder = kept
		return true
	})

	return a.ReloadRSS()
}

// SetRSSGroupOrder persists the sidebar's RSS group order to settings and updates group sort orders in SQLite.
func (a *App) SetRSSGroupOrder(ids []string) error {
	_, err := a.updateSettings(func(s *core.Settings) bool {
		s.RSSGroupOrder = append([]string(nil), ids...)
		return true
	})
	if a.Store != nil {
		ctx := context.Background()
		_ = a.Store.UpdateRSSGroupSortOrder(ctx, ids)
	}
	a.RSSGroups = orderRSSGroups(a.RSSGroups, ids)
	return err
}

// SubscribeRSSFeed inserts an RSS feed into SQLite, associates it with groupID (if non-empty),
// and updates in-memory state.
func (a *App) SubscribeRSSFeed(feed core.RSSFeed, groupID string) error {
	feed.ID = strings.TrimSpace(feed.ID)
	if feed.ID == "" {
		feed.ID = core.FeedID(feed.FeedURL)
	}
	feed.Title = strings.TrimSpace(feed.Title)
	if feed.Title == "" {
		feed.Title = feed.FeedURL
	}
	now := time.Now().UnixMilli()
	feed.UpdatedAt = now
	groupID = strings.TrimSpace(groupID)
	if groupID != "" {
		feed.GroupIDs = []string{groupID}
	} else {
		feed.GroupIDs = nil
	}

	if a.Store != nil {
		ctx := context.Background()
		if feed.CustomTitle == "" {
			if existing, err := a.Store.GetRSSFeed(ctx, feed.ID); err == nil && existing != nil {
				feed.CustomTitle = existing.CustomTitle
			}
		}
		if err := a.Store.SubscribeRSSFeed(ctx, feed); err != nil {
			return err
		}
		groups, _ := a.Store.ListRSSGroups(ctx, false)
		for _, g := range groups {
			if g.ID != groupID && slices.Contains(g.FeedIDs, feed.ID) {
				var kept []string
				for _, fid := range g.FeedIDs {
					if fid != feed.ID {
						kept = append(kept, fid)
					}
				}
				g.FeedIDs = kept
				g.UpdatedAt = now
				_ = a.Store.SaveRSSGroup(ctx, g)
			}
		}
		if groupID != "" {
			if g, err := a.Store.GetRSSGroup(ctx, groupID); err == nil && g != nil {
				if !slices.Contains(g.FeedIDs, feed.ID) {
					g.FeedIDs = append(g.FeedIDs, feed.ID)
					g.UpdatedAt = now
					_ = a.Store.SaveRSSGroup(ctx, *g)
				}
			}
		}
	}

	return a.ReloadRSS()
}

// RenameRSSFeed sets a feed's custom display title and persists it to SQLite.
func (a *App) RenameRSSFeed(id, newName string) error {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return errors.New("feed name cannot be empty")
	}

	if a.Store != nil {
		ctx := context.Background()
		f, err := a.Store.GetRSSFeed(ctx, id)
		if err != nil {
			return err
		}
		if f == nil {
			return fmt.Errorf("feed %q not found", id)
		}
		f.CustomTitle = newName
		f.UpdatedAt = time.Now().UnixMilli()
		if err := a.Store.SaveRSSFeed(ctx, *f); err != nil {
			return err
		}
	}

	return a.ReloadRSS()
}

// DeleteRSSFeed soft-deletes a feed from the local store and removes it from its group's FeedIDs.
func (a *App) DeleteRSSFeed(id string) error {
	if a.Store != nil {
		ctx := context.Background()
		f, err := a.Store.GetRSSFeed(ctx, id)
		if err == nil && f != nil {
			for _, gid := range f.GroupIDs {
				if g, gErr := a.Store.GetRSSGroup(ctx, gid); gErr == nil && g != nil {
					var kept []string
					for _, fid := range g.FeedIDs {
						if fid != id {
							kept = append(kept, fid)
						}
					}
					g.FeedIDs = kept
					g.UpdatedAt = time.Now().UnixMilli()
					_ = a.Store.SaveRSSGroup(ctx, *g)
				}
			}
		}

		if err := a.Store.DeleteRSSFeed(ctx, id, false); err != nil {
			return err
		}
	}

	// Remove from Settings.RSSUngroupedOrder
	_, _ = a.updateSettings(func(s *core.Settings) bool {
		var kept []string
		for _, fid := range s.RSSUngroupedOrder {
			if fid != id {
				kept = append(kept, fid)
			}
		}
		s.RSSUngroupedOrder = kept
		return true
	})

	return a.ReloadRSS()
}

// MoveRSSFeedToGroup moves feedID to targetGroupID (or to ungrouped if targetGroupID is empty).
// It updates both the feed's GroupIDs and the groups' FeedIDs, persisting all changes.
func (a *App) MoveRSSFeedToGroup(feedID string, targetGroupID string) error {
	feedID = strings.TrimSpace(feedID)
	targetGroupID = strings.TrimSpace(targetGroupID)
	if feedID == "" {
		return errors.New("empty feed ID")
	}

	if a.Store != nil {
		ctx := context.Background()
		feed, err := a.Store.GetRSSFeed(ctx, feedID)
		if err != nil {
			return err
		}
		if feed == nil {
			return fmt.Errorf("feed %q not found", feedID)
		}

		now := time.Now().UnixMilli()

		// Remove from old groups
		groups, _ := a.Store.ListRSSGroups(ctx, false)
		for _, g := range groups {
			if g.ID != targetGroupID && slices.Contains(g.FeedIDs, feedID) {
				var kept []string
				for _, fid := range g.FeedIDs {
					if fid != feedID {
						kept = append(kept, fid)
					}
				}
				g.FeedIDs = kept
				g.UpdatedAt = now
				_ = a.Store.SaveRSSGroup(ctx, g)
			}
		}

		// Add to target group
		if targetGroupID != "" {
			tg, err := a.Store.GetRSSGroup(ctx, targetGroupID)
			if err != nil {
				return err
			}
			if tg == nil {
				return fmt.Errorf("target group %q not found", targetGroupID)
			}
			if !slices.Contains(tg.FeedIDs, feedID) {
				tg.FeedIDs = append(tg.FeedIDs, feedID)
				tg.UpdatedAt = now
				_ = a.Store.SaveRSSGroup(ctx, *tg)
			}
			feed.GroupIDs = []string{targetGroupID}
		} else {
			feed.GroupIDs = nil
		}

		feed.UpdatedAt = now
		if err := a.Store.SaveRSSFeed(ctx, *feed); err != nil {
			return err
		}
	}

	return a.ReloadRSS()
}

// ReorderRSSFeedsInGroup updates the ordered FeedIDs of a group.
func (a *App) ReorderRSSFeedsInGroup(groupID string, feedIDs []string) error {
	if a.Store != nil {
		ctx := context.Background()
		g, err := a.Store.GetRSSGroup(ctx, groupID)
		if err != nil {
			return err
		}
		if g == nil {
			return fmt.Errorf("group %q not found", groupID)
		}
		g.FeedIDs = append([]string(nil), feedIDs...)
		g.UpdatedAt = time.Now().UnixMilli()
		if err := a.Store.SaveRSSGroup(ctx, *g); err != nil {
			return err
		}
	}
	return a.ReloadRSS()
}

// SetRSSUngroupedOrder stores the order of ungrouped feeds in settings.
func (a *App) SetRSSUngroupedOrder(feedIDs []string) error {
	_, err := a.updateSettings(func(s *core.Settings) bool {
		s.RSSUngroupedOrder = append([]string(nil), feedIDs...)
		return true
	})
	return err
}

// FetchRSSFeed fetches a single feed from the web, parses entries, saves them to SQLite,
// and updates the feed's failure/error status.
func (a *App) FetchRSSFeed(ctx context.Context, feedID string) (*core.FetchFeedResult, error) {
	if a.Store == nil {
		return nil, errors.New("store not initialized")
	}
	feed, err := a.Store.GetRSSFeed(ctx, feedID)
	if err != nil {
		return nil, err
	}
	if feed == nil {
		return nil, fmt.Errorf("feed %q not found", feedID)
	}

	fetcher := core.NewRSSFetcher(a.Store)
	return fetcher.FetchFeed(ctx, *feed)
}

// FetchAllRSSFeeds fetches all subscribed feeds with bounded concurrency.
func (a *App) FetchAllRSSFeeds(ctx context.Context, force bool) ([]core.FetchFeedResult, error) {
	if a.Store == nil {
		return nil, errors.New("store not initialized")
	}
	fetcher := core.NewRSSFetcher(a.Store)
	return fetcher.FetchAllFeeds(ctx, force)
}
