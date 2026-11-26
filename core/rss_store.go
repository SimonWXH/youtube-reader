package core

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
)

// ======================================================================================
// RSS FEEDS STORAGE
// ======================================================================================

// SaveRSSFeed inserts or updates RSS feed metadata. It never clears an existing
// tombstone; re-subscribing goes through SubscribeRSSFeed.
func (s *Store) SaveRSSFeed(ctx context.Context, f RSSFeed) error {
	groupJSON, _ := json.Marshal(f.GroupIDs)
	sql := `INSERT INTO rss_feeds(
		id, feed_url, site_url, title, custom_title, group_ids, avatar_url,
		etag, last_modified, last_fetched_at, failure_count, last_error, last_error_at,
		refresh_interval_mins, updated_at, deleted, deleted_at, fetch_full_text
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	ON CONFLICT(id) DO UPDATE SET
		feed_url=excluded.feed_url,
		site_url=COALESCE(NULLIF(excluded.site_url, ''), rss_feeds.site_url),
		title=COALESCE(NULLIF(excluded.title, ''), rss_feeds.title),
		custom_title=COALESCE(NULLIF(excluded.custom_title, ''), rss_feeds.custom_title),
		group_ids=excluded.group_ids,
		avatar_url=COALESCE(NULLIF(excluded.avatar_url, ''), rss_feeds.avatar_url),
		etag=COALESCE(NULLIF(excluded.etag, ''), rss_feeds.etag),
		last_modified=COALESCE(NULLIF(excluded.last_modified, ''), rss_feeds.last_modified),
		last_fetched_at=excluded.last_fetched_at,
		failure_count=excluded.failure_count,
		last_error=excluded.last_error,
		last_error_at=excluded.last_error_at,
		refresh_interval_mins=excluded.refresh_interval_mins,
		updated_at=excluded.updated_at,
		fetch_full_text=COALESCE(excluded.fetch_full_text, rss_feeds.fetch_full_text)`

	_, err := s.db.Execute(ctx, sql,
		f.ID, f.FeedURL, f.SiteURL, f.Title, f.CustomTitle, string(groupJSON), f.AvatarURL,
		f.ETag, f.LastModified, f.LastFetchedAt, f.FailureCount, f.LastError, f.LastErrorAt,
		f.RefreshIntervalMins, f.UpdatedAt, boolInt(f.Deleted), f.DeletedAt, feedFetchFullText(f),
	)
	return err
}

// feedFetchFullText is the column value for the feed's answer to automatic
// full-text fetching: NULL while no answer is stored, so the column keeps the
// default of fetching the article's text.
func feedFetchFullText(f RSSFeed) any {
	if f.FetchFullText == nil {
		return nil
	}
	return boolInt(*f.FetchFullText)
}

// SubscribeRSSFeed records a user subscription, reviving a previously soft-deleted feed.
func (s *Store) SubscribeRSSFeed(ctx context.Context, f RSSFeed) error {
	f.Deleted = false
	f.DeletedAt = 0
	if err := s.SaveRSSFeed(ctx, f); err != nil {
		return err
	}
	_, err := s.db.Execute(ctx, `UPDATE rss_feeds SET deleted = 0, deleted_at = 0 WHERE id = ?`, f.ID)
	return err
}

// GetRSSFeed returns a feed by its stable ID, or nil if not found.
func (s *Store) GetRSSFeed(ctx context.Context, id string) (*RSSFeed, error) {
	rows, err := s.db.Query(ctx, `SELECT
		id, feed_url, site_url, title, custom_title, group_ids, avatar_url,
		etag, last_modified, last_fetched_at, failure_count, last_error, last_error_at,
		refresh_interval_mins, updated_at, deleted, deleted_at, fetch_full_text
	FROM rss_feeds WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	if len(rows.Values) == 0 {
		return nil, nil
	}
	f := rowToFeed(rows.Values[0])
	return &f, nil
}

// ListRSSFeeds returns all RSS feeds. If includeDeleted is false, tombstones are excluded.
func (s *Store) ListRSSFeeds(ctx context.Context, includeDeleted bool) ([]RSSFeed, error) {
	sql := `SELECT
		id, feed_url, site_url, title, custom_title, group_ids, avatar_url,
		etag, last_modified, last_fetched_at, failure_count, last_error, last_error_at,
		refresh_interval_mins, updated_at, deleted, deleted_at, fetch_full_text
	FROM rss_feeds`
	if !includeDeleted {
		sql += ` WHERE deleted = 0`
	}
	sql += ` ORDER BY title COLLATE NOCASE ASC, id ASC`

	rows, err := s.db.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	out := make([]RSSFeed, 0, len(rows.Values))
	for _, r := range rows.Values {
		out = append(out, rowToFeed(r))
	}
	return out, nil
}

// DeleteRSSFeed deletes a feed from the local store. If hard is false, it records a tombstone.
func (s *Store) DeleteRSSFeed(ctx context.Context, id string, hard bool) error {
	now := time.Now().UnixMilli()
	if hard {
		stmts := []sqlite.Statement{
			{SQL: `DELETE FROM rss_feeds WHERE id = ?`, Args: []any{id}},
			{SQL: `DELETE FROM rss_articles WHERE feed_id = ? AND starred = 0`, Args: []any{id}},
		}
		_, err := s.db.Transaction(ctx, stmts)
		return err
	}
	stmts := []sqlite.Statement{
		{SQL: `UPDATE rss_feeds SET deleted = 1, deleted_at = ?, updated_at = ? WHERE id = ?`, Args: []any{now, now, id}},
	}
	_, err := s.db.Transaction(ctx, stmts)
	return err
}

func rowToFeed(r []any) RSSFeed {
	var f RSSFeed
	f.ID, _ = r[0].(string)
	f.FeedURL, _ = r[1].(string)
	f.SiteURL, _ = r[2].(string)
	f.Title, _ = r[3].(string)
	f.CustomTitle, _ = r[4].(string)
	if raw, _ := r[5].(string); raw != "" {
		_ = json.Unmarshal([]byte(raw), &f.GroupIDs)
	}
	f.AvatarURL, _ = r[6].(string)
	f.ETag, _ = r[7].(string)
	f.LastModified, _ = r[8].(string)
	f.LastFetchedAt, _ = int64FromAny(r[9])
	f.FailureCount, _ = intFromAny(r[10])
	f.LastError, _ = r[11].(string)
	f.LastErrorAt, _ = int64FromAny(r[12])
	f.RefreshIntervalMins, _ = intFromAny(r[13])
	f.UpdatedAt, _ = int64FromAny(r[14])
	f.Deleted = intFromAnyBool(r[15])
	f.DeletedAt, _ = int64FromAny(r[16])
	if fetch, ok := intFromAny(r[17]); ok {
		on := fetch != 0
		f.FetchFullText = &on
	}
	return f
}

// ======================================================================================
// RSS GROUPS STORAGE
// ======================================================================================

// SaveRSSGroup inserts or updates an RSS group in the local store.
func (s *Store) SaveRSSGroup(ctx context.Context, g RSSGroup) error {
	feedJSON, _ := json.Marshal(g.FeedIDs)
	sql := `INSERT INTO rss_groups(id, name, feed_ids, sort_order, updated_at, deleted, deleted_at)
	VALUES(?,?,?,?,?,?,?)
	ON CONFLICT(id) DO UPDATE SET
		name=excluded.name,
		feed_ids=excluded.feed_ids,
		sort_order=excluded.sort_order,
		updated_at=excluded.updated_at,
		deleted=excluded.deleted,
		deleted_at=excluded.deleted_at`

	_, err := s.db.Execute(ctx, sql, g.ID, g.Name, string(feedJSON), g.SortOrder, g.UpdatedAt, boolInt(g.Deleted), g.DeletedAt)
	return err
}

// GetRSSGroup returns an RSS group by ID.
func (s *Store) GetRSSGroup(ctx context.Context, id string) (*RSSGroup, error) {
	rows, err := s.db.Query(ctx, `SELECT id, name, feed_ids, sort_order, updated_at, deleted, deleted_at FROM rss_groups WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	if len(rows.Values) == 0 {
		return nil, nil
	}
	g := rowToGroup(rows.Values[0])
	return &g, nil
}

// ListRSSGroups returns all groups. If includeDeleted is false, tombstones are excluded.
func (s *Store) ListRSSGroups(ctx context.Context, includeDeleted bool) ([]RSSGroup, error) {
	sql := `SELECT id, name, feed_ids, sort_order, updated_at, deleted, deleted_at FROM rss_groups`
	if !includeDeleted {
		sql += ` WHERE deleted = 0`
	}
	sql += ` ORDER BY sort_order ASC, name COLLATE NOCASE ASC`

	rows, err := s.db.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	out := make([]RSSGroup, 0, len(rows.Values))
	for _, r := range rows.Values {
		out = append(out, rowToGroup(r))
	}
	return out, nil
}

// DeleteRSSGroup deletes a group. If hard is false, marks deleted with a tombstone.
func (s *Store) DeleteRSSGroup(ctx context.Context, id string, hard bool) error {
	now := time.Now().UnixMilli()
	if hard {
		_, err := s.db.Execute(ctx, `DELETE FROM rss_groups WHERE id = ?`, id)
		return err
	}
	_, err := s.db.Execute(ctx, `UPDATE rss_groups SET deleted = 1, deleted_at = ?, updated_at = ? WHERE id = ?`, now, now, id)
	return err
}

// UpdateRSSGroupSortOrder updates the sort_order of RSS groups based on their position in groupIDs.
func (s *Store) UpdateRSSGroupSortOrder(ctx context.Context, groupIDs []string) error {
	if len(groupIDs) == 0 {
		return nil
	}
	now := time.Now().UnixMilli()
	stmts := make([]sqlite.Statement, 0, len(groupIDs))
	for i, id := range groupIDs {
		stmts = append(stmts, sqlite.Statement{
			SQL:  `UPDATE rss_groups SET sort_order = ?, updated_at = ? WHERE id = ?`,
			Args: []any{i, now, id},
		})
	}
	_, err := s.db.Transaction(ctx, stmts)
	return err
}

func rowToGroup(r []any) RSSGroup {
	var g RSSGroup
	g.ID, _ = r[0].(string)
	g.Name, _ = r[1].(string)
	if raw, _ := r[2].(string); raw != "" {
		_ = json.Unmarshal([]byte(raw), &g.FeedIDs)
	}
	g.SortOrder, _ = intFromAny(r[3])
	g.UpdatedAt, _ = int64FromAny(r[4])
	g.Deleted = intFromAnyBool(r[5])
	g.DeletedAt, _ = int64FromAny(r[6])
	return g
}

// ======================================================================================
// RSS ARTICLES STORAGE & RETENTION
// ======================================================================================

// SaveRSSArticles inserts or updates articles in the local store.
//
// CRITICAL IDEMPOTENCY & READ-STATE PRESERVATION:
// When an article already exists (by stable ID), its `read`, `read_at`, `starred`,
// and `starred_at` states are strictly preserved and NEVER overwritten or reset.
// This guarantees that repeated background refreshes will never mark read articles as unread.
func (s *Store) SaveRSSArticles(ctx context.Context, articles []RSSArticle) error {
	if len(articles) == 0 {
		return nil
	}
	stmts := make([]sqlite.Statement, 0, len(articles))
	upsertSQL := `INSERT INTO rss_articles(
		id, feed_id, title, link, guid, author, published_at, snippet,
		content_html, feed_content_html, is_full_text, hero_image, blocks_json,
		read, read_at, starred, starred_at, updated_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	ON CONFLICT(id) DO UPDATE SET
		feed_id=excluded.feed_id,
		title=excluded.title,
		link=excluded.link,
		guid=excluded.guid,
		author=excluded.author,
		published_at=excluded.published_at,
		snippet=excluded.snippet,
		content_html=CASE
			WHEN rss_articles.is_full_text = 1 AND excluded.is_full_text = 0 THEN rss_articles.content_html
			ELSE COALESCE(NULLIF(excluded.content_html, ''), rss_articles.content_html)
		END,
		feed_content_html=COALESCE(NULLIF(excluded.feed_content_html, ''), rss_articles.feed_content_html),
		is_full_text=MAX(rss_articles.is_full_text, excluded.is_full_text),
		hero_image=COALESCE(NULLIF(excluded.hero_image, ''), rss_articles.hero_image),
		blocks_json=CASE
			WHEN rss_articles.is_full_text = 1 AND excluded.is_full_text = 0 THEN rss_articles.blocks_json
			ELSE COALESCE(NULLIF(NULLIF(excluded.blocks_json, ''), '[]'), rss_articles.blocks_json)
		END,
		updated_at=excluded.updated_at`

	for _, a := range articles {
		blocksJSON := EncodeBlocksJSON(a.Blocks)
		stmts = append(stmts, sqlite.Statement{
			SQL: upsertSQL,
			Args: []any{
				a.ID, a.FeedID, a.Title, a.Link, a.GUID, a.Author, a.PublishedAt, a.Snippet,
				a.ContentHTML, a.FeedContentHTML, boolInt(a.IsFullText), a.HeroImage, blocksJSON,
				boolInt(a.Read), a.ReadAt, boolInt(a.Starred), a.StarredAt, a.UpdatedAt,
			},
		})
	}

	_, err := s.db.Transaction(ctx, stmts)
	return err
}

// GetRSSArticle returns an article by its ID, its feed's own body included:
// the reader's original/full-text toggle needs both bodies for the one article
// it shows, and the list queries leave the feed's body in the store.
func (s *Store) GetRSSArticle(ctx context.Context, id string) (*RSSArticle, error) {
	rows, err := s.db.Query(ctx, `SELECT
		id, feed_id, title, link, guid, author, published_at, snippet,
		content_html, is_full_text, hero_image, blocks_json,
		read, read_at, starred, starred_at, updated_at, feed_content_html
	FROM rss_articles WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	if len(rows.Values) == 0 {
		return nil, nil
	}
	a := rowToArticle(rows.Values[0])
	return &a, nil
}

// RSSArticleFilter defines criteria for querying articles.
type RSSArticleFilter struct {
	FeedID      string
	UnreadOnly  bool
	StarredOnly bool
	Search      string
	Limit       int
	Offset      int
}

func buildRSSArticleQuery(columns string, filter RSSArticleFilter) (string, []any) {
	var where []string
	var args []any

	if filter.FeedID != "" {
		where = append(where, "feed_id = ?")
		args = append(args, filter.FeedID)
	}
	if filter.UnreadOnly {
		where = append(where, "read = 0")
	}
	if filter.StarredOnly {
		where = append(where, "starred = 1")
	} else {
		where = append(where, "feed_id NOT IN (SELECT id FROM rss_feeds WHERE deleted = 1)")
	}
	if filter.Search != "" {
		where = append(where, "(title LIKE ? OR snippet LIKE ?)")
		pattern := "%" + filter.Search + "%"
		args = append(args, pattern, pattern)
	}

	query := "SELECT " + columns + " FROM rss_articles"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY published_at DESC, id DESC"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
		if filter.Offset > 0 {
			query += fmt.Sprintf(" OFFSET %d", filter.Offset)
		}
	}
	return query, args
}

// ListRSSArticles returns articles matching the specified filter, ordered by published_at DESC.
func (s *Store) ListRSSArticles(ctx context.Context, filter RSSArticleFilter) ([]RSSArticle, error) {
	query, args := buildRSSArticleQuery(`
		id, feed_id, title, link, guid, author, published_at, snippet,
		content_html, is_full_text, hero_image, blocks_json,
		read, read_at, starred, starred_at, updated_at`, filter)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	out := make([]RSSArticle, 0, len(rows.Values))
	for _, r := range rows.Values {
		out = append(out, rowToArticle(r))
	}
	return out, nil
}

// ListRSSArticleSummaries returns article metadata and snippets matching filter
// without loading content_html or blocks_json into memory. Full article bodies
// are loaded on demand via GetRSSArticle.
func (s *Store) ListRSSArticleSummaries(ctx context.Context, filter RSSArticleFilter) ([]RSSArticle, error) {
	query, args := buildRSSArticleQuery(`
		id, feed_id, title, link, guid, author, published_at, snippet,
		CASE WHEN TRIM(COALESCE(snippet, '')) = '' THEN SUBSTR(COALESCE(content_html, ''), 1, 2048) ELSE '' END,
		is_full_text, hero_image, read, read_at, starred, starred_at, updated_at`, filter)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	out := make([]RSSArticle, 0, len(rows.Values))
	for _, r := range rows.Values {
		out = append(out, rowToArticleSummary(r))
	}
	return out, nil
}

// RSSArticleFingerprint holds lightweight change-detection fields for one stored article.
type RSSArticleFingerprint struct {
	Title       string
	Link        string
	PublishedAt string
	IsFullText  bool
	HasBody     bool
	ContentHash [32]byte
}

// ListRSSArticleFingerprints returns change-detection fingerprints for one feed's stored articles.
func (s *Store) ListRSSArticleFingerprints(ctx context.Context, feedID string) (map[string]RSSArticleFingerprint, error) {
	rows, err := s.db.Query(ctx, `SELECT id, title, link, published_at, is_full_text, content_html
	FROM rss_articles WHERE feed_id = ?`, feedID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]RSSArticleFingerprint, len(rows.Values))
	for _, r := range rows.Values {
		id, _ := r[0].(string)
		if id == "" {
			continue
		}
		title, _ := r[1].(string)
		link, _ := r[2].(string)
		pub, _ := r[3].(string)
		isFull := intFromAnyBool(r[4])
		htmlBody, _ := r[5].(string)
		fp := RSSArticleFingerprint{
			Title:       title,
			Link:        link,
			PublishedAt: pub,
			IsFullText:  isFull,
			HasBody:     htmlBody != "",
		}
		if htmlBody != "" {
			fp.ContentHash = sha256.Sum256([]byte(htmlBody))
		}
		out[id] = fp
	}
	return out, nil
}

func rowToArticleSummary(r []any) RSSArticle {
	var a RSSArticle
	a.ID, _ = r[0].(string)
	a.FeedID, _ = r[1].(string)
	a.Title, _ = r[2].(string)
	a.Link, _ = r[3].(string)
	a.GUID, _ = r[4].(string)
	a.Author, _ = r[5].(string)
	a.PublishedAt, _ = r[6].(string)
	a.Snippet, _ = r[7].(string)
	if strings.TrimSpace(a.Snippet) == "" {
		if fallback, _ := r[8].(string); fallback != "" {
			a.Snippet = CleanSnippet(fallback, "", 200)
		}
	}
	a.IsFullText = intFromAnyBool(r[9])
	a.HeroImage, _ = r[10].(string)
	a.Read = intFromAnyBool(r[11])
	a.ReadAt, _ = int64FromAny(r[12])
	a.Starred = intFromAnyBool(r[13])
	a.StarredAt, _ = int64FromAny(r[14])
	a.UpdatedAt, _ = int64FromAny(r[15])
	return a
}

// MarkRSSArticleRead updates the read state of an article.
func (s *Store) MarkRSSArticleRead(ctx context.Context, id string, read bool, now int64) error {
	readAt := int64(0)
	if read {
		readAt = now
	}
	_, err := s.db.Execute(ctx, `UPDATE rss_articles SET read = ?, read_at = ?, updated_at = ? WHERE id = ?`, boolInt(read), readAt, now, id)
	return err
}

// MarkFeedArticlesRead marks all unread articles of a feed as read.
func (s *Store) MarkFeedArticlesRead(ctx context.Context, feedID string, now int64) error {
	_, err := s.db.Execute(ctx, `UPDATE rss_articles SET read = 1, read_at = ?, updated_at = ? WHERE feed_id = ? AND read = 0`, now, now, feedID)
	return err
}

// SetRSSArticleStarred updates the starred/favorite state of an article.
func (s *Store) SetRSSArticleStarred(ctx context.Context, id string, starred bool, now int64) error {
	starredAt := int64(0)
	if starred {
		starredAt = now
	}
	_, err := s.db.Execute(ctx, `UPDATE rss_articles SET starred = ?, starred_at = ?, updated_at = ? WHERE id = ?`, boolInt(starred), starredAt, now, id)
	return err
}

// RSSUnreadCounts returns a map of feedID -> unread article count for non-deleted feeds.
func (s *Store) RSSUnreadCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.Query(ctx, `SELECT feed_id, COUNT(*) FROM rss_articles
	WHERE read = 0 AND feed_id NOT IN (SELECT id FROM rss_feeds WHERE deleted = 1)
	GROUP BY feed_id`)
	if err != nil {
		return nil, err
	}
	counts := make(map[string]int, len(rows.Values))
	for _, r := range rows.Values {
		feedID, _ := r[0].(string)
		cnt, _ := intFromAny(r[1])
		counts[feedID] = cnt
	}
	return counts, nil
}

// RSSTotalUnreadCount returns the total number of unread articles across all active feeds.
func (s *Store) RSSTotalUnreadCount(ctx context.Context) (int, error) {
	rows, err := s.db.Query(ctx, `SELECT COUNT(*) FROM rss_articles
	WHERE read = 0 AND feed_id NOT IN (SELECT id FROM rss_feeds WHERE deleted = 1)`)
	if err != nil || len(rows.Values) == 0 {
		return 0, err
	}
	cnt, _ := intFromAny(rows.Values[0][0])
	return cnt, nil
}

// RSSStarredCount returns the total number of starred articles in active feeds.
func (s *Store) RSSStarredCount(ctx context.Context) (int, error) {
	rows, err := s.db.Query(ctx, `SELECT COUNT(*) FROM rss_articles
	WHERE starred = 1 AND feed_id NOT IN (SELECT id FROM rss_feeds WHERE deleted = 1)`)
	if err != nil || len(rows.Values) == 0 {
		return 0, err
	}
	cnt, _ := intFromAny(rows.Values[0][0])
	return cnt, nil
}

// retentionUnpurged matches article rows whose body is still cached. Purged rows keep
// their metadata and read state, so they must not be counted or purged again.
const retentionUnpurged = `(COALESCE(content_html, '') != '' OR COALESCE(blocks_json, '') != '' OR COALESCE(feed_content_html, '') != '')`

// CleanOldRSSArticles purges the bodies of old read articles for a feed once the feed
// exceeds its read-article cap.
//
// RETENTION POLICY:
// An article is purged only when BOTH hold: the feed has more cached read, unstarred
// articles than maxReadArticles, AND the article was published before the retention
// cutoff. Unread and starred articles are never touched. Purged rows keep their metadata
// and read state so a later refresh cannot resurrect them as unread.
func (s *Store) CleanOldRSSArticles(ctx context.Context, feedID string, maxReadArticles int, olderThan time.Duration, now time.Time) (int64, error) {
	if maxReadArticles <= 0 || olderThan <= 0 {
		return 0, nil
	}

	rows, err := s.db.Query(ctx, `SELECT COUNT(*) FROM rss_articles WHERE feed_id = ? AND read = 1 AND starred = 0 AND `+retentionUnpurged, feedID)
	if err != nil || len(rows.Values) == 0 {
		return 0, err
	}
	excess, _ := int64FromAny(rows.Values[0][0])
	excess -= int64(maxReadArticles)
	if excess <= 0 {
		return 0, nil
	}

	cutoffDate := now.Add(-olderThan).UTC().Format(time.RFC3339)
	purgeQuery := fmt.Sprintf(`UPDATE rss_articles
	SET content_html = '', feed_content_html = '', blocks_json = '', is_full_text = 0
	WHERE id IN (
		SELECT id FROM rss_articles
		WHERE feed_id = ? AND read = 1 AND starred = 0 AND published_at < ? AND %s
		ORDER BY published_at ASC, id ASC
		LIMIT %d
	) AND read = 1 AND starred = 0`, retentionUnpurged, excess)
	res, err := s.db.Execute(ctx, purgeQuery, feedID, cutoffDate)
	if err != nil {
		return 0, err
	}
	return res.Changes, nil
}

func rowToArticle(r []any) RSSArticle {
	var a RSSArticle
	a.ID, _ = r[0].(string)
	a.FeedID, _ = r[1].(string)
	a.Title, _ = r[2].(string)
	a.Link, _ = r[3].(string)
	a.GUID, _ = r[4].(string)
	a.Author, _ = r[5].(string)
	a.PublishedAt, _ = r[6].(string)
	a.Snippet, _ = r[7].(string)
	a.ContentHTML, _ = r[8].(string)
	a.IsFullText = intFromAnyBool(r[9])
	a.HeroImage, _ = r[10].(string)
	if raw, _ := r[11].(string); raw != "" {
		a.Blocks = ParseBlocksJSON(raw)
	}
	a.Read = intFromAnyBool(r[12])
	a.ReadAt, _ = int64FromAny(r[13])
	a.Starred = intFromAnyBool(r[14])
	a.StarredAt, _ = int64FromAny(r[15])
	a.UpdatedAt, _ = int64FromAny(r[16])
	// Only the single-article read selects the feed's own body: the list
	// queries stop at the row's summary fields, so a row without it is a
	// listed article rather than one that never had a body.
	if len(r) > 17 {
		a.FeedContentHTML, _ = r[17].(string)
	}
	return a
}

func int64FromAny(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case float64:
		return int64(n), true
	}
	return 0, false
}

// ======================================================================================
// RSS NOTES STORAGE
// ======================================================================================

// SaveRSSNote inserts or updates an article note in the local store.
func (s *Store) SaveRSSNote(ctx context.Context, note ArticleNoteRecord) error {
	sql := `INSERT INTO rss_notes(id, article_id, paragraph_index, paragraph_hash, selected_text, note_text, updated_at, deleted, deleted_at)
	VALUES(?,?,?,?,?,?,?,?,?)
	ON CONFLICT(id) DO UPDATE SET
		article_id=excluded.article_id,
		paragraph_index=excluded.paragraph_index,
		paragraph_hash=excluded.paragraph_hash,
		selected_text=excluded.selected_text,
		note_text=excluded.note_text,
		updated_at=excluded.updated_at,
		deleted=excluded.deleted,
		deleted_at=excluded.deleted_at`

	_, err := s.db.Execute(ctx, sql,
		note.ID, note.ArticleID, note.ParagraphIndex, note.ParagraphHash,
		note.SelectedText, note.NoteText, note.UpdatedAt, boolInt(note.Deleted), note.DeletedAt,
	)
	return err
}

// ListRSSNotes returns all notes for an article, ordered by paragraph index.
func (s *Store) ListRSSNotes(ctx context.Context, articleID string) ([]ArticleNoteRecord, error) {
	rows, err := s.db.Query(ctx, `SELECT id, article_id, paragraph_index, paragraph_hash, selected_text, note_text, updated_at, deleted, deleted_at
	FROM rss_notes WHERE article_id = ? AND deleted = 0 ORDER BY paragraph_index ASC, id ASC`, articleID)
	if err != nil {
		return nil, err
	}
	out := make([]ArticleNoteRecord, 0, len(rows.Values))
	for _, r := range rows.Values {
		var n ArticleNoteRecord
		n.ID, _ = r[0].(string)
		n.ArticleID, _ = r[1].(string)
		n.ParagraphIndex, _ = intFromAny(r[2])
		n.ParagraphHash, _ = r[3].(string)
		n.SelectedText, _ = r[4].(string)
		n.NoteText, _ = r[5].(string)
		n.UpdatedAt, _ = int64FromAny(r[6])
		n.Deleted = intFromAnyBool(r[7])
		n.DeletedAt, _ = int64FromAny(r[8])
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ParagraphIndex < out[j].ParagraphIndex })
	return out, nil
}
