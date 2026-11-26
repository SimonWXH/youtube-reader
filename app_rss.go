package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
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

// ReloadRSS refreshes all in-memory RSS feeds, groups, articles, and counts from SQLite.
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
	articles, err := a.Store.ListRSSArticleSummaries(ctx, core.RSSArticleFilter{})
	if err != nil {
		return err
	}

	a.RSSFeeds = feeds
	a.RSSGroups = orderRSSGroups(groups, a.settingsSnapshot().RSSGroupOrder)
	a.RSSArticles = articles
	a.selectedRSSCache = nil
	a.reloadRSSCounts()
	return nil
}

// reloadRSSCounts reloads the unread and starred totals the RSS sidebar shows.
// Every writer of an article's read or starred state calls it so those counts
// never disagree with the article column.
func (a *App) reloadRSSCounts() {
	if a.Store == nil {
		return
	}
	ctx := context.Background()
	unreadMap, _ := a.Store.RSSUnreadCounts(ctx)
	if unreadMap == nil {
		unreadMap = make(map[string]int)
	}
	a.RSSUnreadMap = unreadMap
	a.RSSTotalUnread, _ = a.Store.RSSTotalUnreadCount(ctx)
	a.RSSStarredTotal, _ = a.Store.RSSStarredCount(ctx)
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

// uniqueRSSGroupName returns name, or name with the first free numeric suffix,
// so no two active groups share a name case-insensitively. The names come from
// the store, so a stale in-memory snapshot cannot let a duplicate through.
func (a *App) uniqueRSSGroupName(ctx context.Context, name, exceptID string) (string, error) {
	name = strings.TrimSpace(name)
	var groups []core.RSSGroup
	if a.Store != nil {
		stored, err := a.Store.ListRSSGroups(ctx, false)
		if err != nil {
			return "", err
		}
		groups = stored
	} else {
		groups = a.RSSGroups
	}
	taken := make(map[string]bool, len(groups))
	for _, g := range groups {
		if g.ID != exceptID {
			taken[strings.ToLower(strings.TrimSpace(g.Name))] = true
		}
	}
	candidate := name
	for n := 2; taken[strings.ToLower(candidate)]; n++ {
		candidate = fmt.Sprintf("%s %d", name, n)
	}
	return candidate, nil
}

// activeRSSGroupByName returns the active group whose name matches name
// case-insensitively, read from the store.
func (a *App) activeRSSGroupByName(ctx context.Context, name string) (*core.RSSGroup, error) {
	groups, err := a.Store.ListRSSGroups(ctx, false)
	if err != nil {
		return nil, err
	}
	for i := range groups {
		if strings.EqualFold(strings.TrimSpace(groups[i].Name), strings.TrimSpace(name)) {
			return &groups[i], nil
		}
	}
	return nil, nil
}

// CreateRSSGroup creates a new RSS group with the given name, appends it to the group order,
// and persists it to the local store.
func (a *App) CreateRSSGroup(name string) (core.RSSGroup, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return core.RSSGroup{}, errors.New("group name cannot be empty")
	}
	ctx := context.Background()
	unique, err := a.uniqueRSSGroupName(ctx, name, "")
	if err != nil {
		return core.RSSGroup{}, err
	}
	name = unique

	groupID := "rss-group-" + core.NewGroupID()
	now := time.Now().UnixMilli()
	group := core.RSSGroup{
		ID:        groupID,
		Name:      name,
		SortOrder: len(a.RSSGroups),
		UpdatedAt: now,
	}

	if a.Store != nil {
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
		unique, err := a.uniqueRSSGroupName(ctx, newName, id)
		if err != nil {
			return err
		}
		g.Name = unique
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

var sharedRSSHTTPClient = &http.Client{Timeout: core.DefaultRSSTimeout}

// rssArticleClient is the client the reader's full-text fetch reads article
// pages with.
func (a *App) rssArticleClient() *http.Client {
	if a.rssHTTPClient != nil {
		return a.rssHTTPClient
	}
	return sharedRSSHTTPClient
}

// RefetchRSSArticleContent re-extracts one article's full text from its
// original page, keeping the feed's own content as the fallback when the
// extraction fails, and saves it back without touching read or starred state.
func (a *App) RefetchRSSArticleContent(ctx context.Context, articleID string) (core.RSSArticle, error) {
	if a.Store == nil {
		return core.RSSArticle{}, errors.New("store not initialized")
	}
	art, err := a.Store.GetRSSArticle(ctx, articleID)
	if err != nil {
		return core.RSSArticle{}, err
	}
	if art == nil {
		return core.RSSArticle{}, fmt.Errorf("article %q not found", articleID)
	}
	if strings.TrimSpace(art.Link) == "" {
		return *art, errors.New("article has no link to fetch")
	}

	contentHTML, blocks, isFullText, hero, _ := core.ExtractFullText(ctx, a.rssArticleClient(), art.Link, art.ContentHTML, art.Snippet)
	if strings.TrimSpace(contentHTML) == "" {
		return *art, errors.New("the article page returned no content")
	}

	art.ContentHTML = contentHTML
	art.Blocks = blocks
	art.IsFullText = isFullText
	if art.HeroImage == "" && hero != "" {
		art.HeroImage = hero
	}
	art.UpdatedAt = time.Now().UnixMilli()
	if err := a.Store.SaveRSSArticles(ctx, []core.RSSArticle{*art}); err != nil {
		return core.RSSArticle{}, err
	}
	// Read back what the store kept: the upsert never downgrades an article
	// that already holds full text, so the caller's feedback follows the
	// stored state rather than this attempt's.
	if stored, err := a.Store.GetRSSArticle(ctx, articleID); err == nil && stored != nil {
		return *stored, nil
	}
	return *art, nil
}

// SetRSSFeedAutoFullText turns one feed's automatic full-text fetching on or
// off and saves it beside the feed's other preferences. A feed that has never
// had the choice stored fetches its articles' text, which is the default.
func (a *App) SetRSSFeedAutoFullText(feedID string, fetch bool) error {
	if a.Store == nil {
		return errors.New("store not initialized")
	}
	ctx := context.Background()
	feed, err := a.Store.GetRSSFeed(ctx, feedID)
	if err != nil {
		return err
	}
	if feed == nil {
		return fmt.Errorf("feed %q not found", feedID)
	}
	feed.FetchFullText = &fetch
	feed.UpdatedAt = time.Now().UnixMilli()
	if err := a.Store.SaveRSSFeed(ctx, *feed); err != nil {
		return err
	}
	return a.ReloadRSS()
}

// RSSOPMLImportResult summarizes the outcome of importing one or more OPML files.
// GroupsImported counts the groups the import created or brought back from a
// tombstone, so a restore is reported rather than silently passing as no change.
type RSSOPMLImportResult struct {
	FeedsImported  int
	FeedsSkipped   int
	GroupsImported int
	NewFeedIDs     []string
}

// ImportRSSOPMLPaths batch imports RSS subscriptions and folders from one or more OPML files.
// Existing feeds with identical canonical URLs are automatically deduplicated.
// It is safe to run off the main thread: it writes only the SQLite store and
// settings, and the caller reloads on the main thread so the frames that read
// the app never see a half-applied library.
func (a *App) ImportRSSOPMLPaths(ctx context.Context, paths []string) (RSSOPMLImportResult, error) {
	if a.Store == nil {
		return RSSOPMLImportResult{}, errors.New("store not initialized")
	}

	var allGroups []core.RSSGroup
	var allFeeds []core.RSSFeed

	for _, p := range paths {
		parsed, err := core.ParseRSSOPMLFile(p)
		if err != nil {
			return RSSOPMLImportResult{}, fmt.Errorf("failed to parse OPML %s: %w", p, err)
		}
		allGroups = append(allGroups, parsed.Groups...)
		allFeeds = append(allFeeds, parsed.Feeds...)
	}

	if len(allFeeds) == 0 && len(allGroups) == 0 {
		return RSSOPMLImportResult{}, nil
	}

	now := time.Now().UnixMilli()
	var result RSSOPMLImportResult

	// 1. Fetch current groups and map them by normalized name
	existingGroups, err := a.Store.ListRSSGroups(ctx, true)
	if err != nil {
		return result, err
	}
	groupByName := make(map[string]*core.RSSGroup)
	activeGroupCount := 0
	for i := range existingGroups {
		if existingGroups[i].Deleted {
			continue
		}
		groupByName[strings.ToLower(strings.TrimSpace(existingGroups[i].Name))] = &existingGroups[i]
		activeGroupCount++
	}
	for i := range existingGroups {
		if !existingGroups[i].Deleted {
			continue
		}
		normName := strings.ToLower(strings.TrimSpace(existingGroups[i].Name))
		if _, exists := groupByName[normName]; !exists {
			groupByName[normName] = &existingGroups[i]
		}
	}

	// Map parsed group IDs to existing or newly created group IDs
	groupIDMap := make(map[string]string)
	var newGroupIDs []string

	for _, g := range allGroups {
		normName := strings.ToLower(strings.TrimSpace(g.Name))
		if normName == "" {
			continue
		}
		existing, exists := groupByName[normName]
		if !exists || existing.Deleted {
			raced, err := a.activeRSSGroupByName(ctx, g.Name)
			if err != nil {
				return result, err
			}
			if raced != nil {
				groupByName[normName] = raced
				groupIDMap[g.ID] = raced.ID
				continue
			}
		}
		if exists {
			// Reuse existing group and revive if soft-deleted. Deleting a group
			// removes it from every feed but leaves its own FeedIDs behind, so a
			// revived group starts from the memberships this import carries.
			if existing.Deleted {
				existing.Deleted = false
				existing.DeletedAt = 0
				existing.FeedIDs = nil
				existing.UpdatedAt = now
				if err := a.Store.SaveRSSGroup(ctx, *existing); err != nil {
					return result, err
				}
				result.GroupsImported++
			}
			groupIDMap[g.ID] = existing.ID
		} else {
			// Create new group
			newID := g.ID
			if newID == "" {
				newID = "rss-group-" + core.NewGroupID()
			}
			if oldG, _ := a.Store.GetRSSGroup(ctx, newID); oldG != nil {
				newID = "rss-group-" + core.NewGroupID()
			}
			newGroup := core.RSSGroup{
				ID:        newID,
				Name:      g.Name,
				SortOrder: activeGroupCount,
				UpdatedAt: now,
			}
			if err := a.Store.SaveRSSGroup(ctx, newGroup); err != nil {
				return result, err
			}
			activeGroupCount++
			groupByName[normName] = &newGroup
			groupIDMap[g.ID] = newID
			newGroupIDs = append(newGroupIDs, newID)
			result.GroupsImported++
		}
	}

	if len(newGroupIDs) > 0 {
		_, _ = a.updateSettings(func(s *core.Settings) bool {
			s.RSSGroupOrder = append(s.RSSGroupOrder, newGroupIDs...)
			return true
		})
	}

	// 2. Fetch existing feeds to check for duplicates
	existingFeeds, err := a.Store.ListRSSFeeds(ctx, true)
	if err != nil {
		return result, err
	}
	existingFeedMap := make(map[string]core.RSSFeed)
	for _, f := range existingFeeds {
		existingFeedMap[f.ID] = f
		existingFeedMap[core.CanonicalURL(f.FeedURL)] = f
	}

	importOrder := make([]string, 0, len(allFeeds))
	importFeeds := make(map[string]*core.RSSFeed)
	for _, f := range allFeeds {
		canonicalURL := core.CanonicalURL(f.FeedURL)
		if canonicalURL == "" {
			continue
		}
		entry, ok := importFeeds[canonicalURL]
		if !ok {
			merged := f
			merged.FeedURL = canonicalURL
			entry = &merged
			importFeeds[canonicalURL] = entry
			importOrder = append(importOrder, canonicalURL)
		}
		for _, gid := range f.GroupIDs {
			if gid != "" && !slices.Contains(entry.GroupIDs, gid) {
				entry.GroupIDs = append(entry.GroupIDs, gid)
			}
		}
	}

	for _, canonicalURL := range importOrder {
		f := *importFeeds[canonicalURL]
		fid := f.ID
		if fid == "" {
			fid = core.FeedID(canonicalURL)
		}

		var mappedGroupIDs []string
		for _, gid := range f.GroupIDs {
			targetGID := gid
			if mapped, ok := groupIDMap[gid]; ok {
				targetGID = mapped
			}
			if targetGID != "" && !slices.Contains(mappedGroupIDs, targetGID) {
				mappedGroupIDs = append(mappedGroupIDs, targetGID)
			}
		}

		existing, subscribed := existingFeedMap[fid]
		if !subscribed {
			existing, subscribed = existingFeedMap[canonicalURL]
		}
		if subscribed && !existing.Deleted {
			result.FeedsSkipped++
			if err := a.addRSSFeedToGroups(ctx, existing, mappedGroupIDs, now); err != nil {
				return result, err
			}
			continue
		}

		feedToSave := core.RSSFeed{
			ID:                  fid,
			FeedURL:             canonicalURL,
			SiteURL:             f.SiteURL,
			Title:               f.Title,
			GroupIDs:            mappedGroupIDs,
			RefreshIntervalMins: core.DefaultRSSRefreshIntervalMins,
			UpdatedAt:           now,
		}

		if err := a.Store.SubscribeRSSFeed(ctx, feedToSave); err != nil {
			return result, err
		}
		if err := a.addRSSFeedToGroups(ctx, feedToSave, mappedGroupIDs, now); err != nil {
			return result, err
		}

		result.NewFeedIDs = append(result.NewFeedIDs, fid)
		result.FeedsImported++
	}

	return result, nil
}

// addRSSFeedToGroups makes feed a member of every group in groupIDs on both
// membership records, the feed's GroupIDs and each group's FeedIDs, so no
// active group lists a feed whose own GroupIDs omit it.
func (a *App) addRSSFeedToGroups(ctx context.Context, feed core.RSSFeed, groupIDs []string, now int64) error {
	if len(groupIDs) == 0 {
		return nil
	}

	feedChanged := false
	for _, gid := range groupIDs {
		if !slices.Contains(feed.GroupIDs, gid) {
			feed.GroupIDs = append(feed.GroupIDs, gid)
			feedChanged = true
		}
	}
	if feedChanged {
		feed.UpdatedAt = now
		if err := a.Store.SaveRSSFeed(ctx, feed); err != nil {
			return err
		}
	}

	for _, gid := range groupIDs {
		grp, err := a.Store.GetRSSGroup(ctx, gid)
		if err != nil {
			return err
		}
		if grp == nil || slices.Contains(grp.FeedIDs, feed.ID) {
			continue
		}
		grp.FeedIDs = append(grp.FeedIDs, feed.ID)
		grp.UpdatedAt = now
		if err := a.Store.SaveRSSGroup(ctx, *grp); err != nil {
			return err
		}
	}
	return nil
}

// ExportRSSOPMLToFile exports all active RSS subscriptions and groups to a local OPML file.
func (a *App) ExportRSSOPMLToFile(path string) error {
	if a.Store == nil {
		return errors.New("store not initialized")
	}
	ctx := context.Background()
	groups, err := a.Store.ListRSSGroups(ctx, false)
	if err != nil {
		return err
	}
	feeds, err := a.Store.ListRSSFeeds(ctx, false)
	if err != nil {
		return err
	}
	orderedGroups := orderRSSGroups(groups, a.settingsSnapshot().RSSGroupOrder)
	return core.ExportRSSOPMLFile(path, orderedGroups, feeds, "YouTube Reader RSS Subscriptions")
}
