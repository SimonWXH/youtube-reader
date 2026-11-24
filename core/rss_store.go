package core

import (
	"context"
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

// SaveRSSFeed inserts or updates an RSS subscription feed in the local store.
func (s *Store) SaveRSSFeed(ctx context.Context, f RSSFeed) error {
	groupJSON, _ := json.Marshal(f.GroupIDs)
	sql := `INSERT INTO rss_feeds(
		id, feed_url, site_url, title, custom_title, group_ids, avatar_url,
		etag, last_modified, last_fetched_at, failure_count, last_error, last_error_at,
		refresh_interval_mins, updated_at, deleted, deleted_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	ON CONFLICT(id) DO UPDATE SET
		feed_url=excluded.feed_url,
		site_url=COALESCE(NULLIF(excluded.site_url, ''), rss_feeds.site_url),
		title=COALESCE(NULLIF(excluded.title, ''), rss_feeds.title),
		custom_title=excluded.custom_title,
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
		deleted=excluded.deleted,
		deleted_at=excluded.deleted_at`

	_, err := s.db.Execute(ctx, sql,
		f.ID, f.FeedURL, f.SiteURL, f.Title, f.CustomTitle, string(groupJSON), f.AvatarURL,
		f.ETag, f.LastModified, f.LastFetchedAt, f.FailureCount, f.LastError, f.LastErrorAt,
		f.RefreshIntervalMins, f.UpdatedAt, boolInt(f.Deleted), f.DeletedAt,
	)
	return err
}

// GetRSSFeed returns a feed by its stable ID, or nil if not found.
func (s *Store) GetRSSFeed(ctx context.Context, id string) (*RSSFeed, error) {
	rows, err := s.db.Query(ctx, `SELECT
		id, feed_url, site_url, title, custom_title, group_ids, avatar_url,
		etag, last_modified, last_fetched_at, failure_count, last_error, last_error_at,
		refresh_interval_mins, updated_at, deleted, deleted_at
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
		refresh_interval_mins, updated_at, deleted, deleted_at
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
		content_html, is_full_text, hero_image, blocks_json,
		read, read_at, starred, starred_at, updated_at, deleted, deleted_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
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
				a.ContentHTML, boolInt(a.IsFullText), a.HeroImage, blocksJSON,
				boolInt(a.Read), a.ReadAt, boolInt(a.Starred), a.StarredAt, a.UpdatedAt,
				boolInt(a.Deleted), a.DeletedAt,
			},
		})
	}

	_, err := s.db.Transaction(ctx, stmts)
	return err
}

// GetRSSArticle returns an article by its ID.
func (s *Store) GetRSSArticle(ctx context.Context, id string) (*RSSArticle, error) {
	rows, err := s.db.Query(ctx, `SELECT
		id, feed_id, title, link, guid, author, published_at, snippet,
		content_html, is_full_text, hero_image, blocks_json,
		read, read_at, starred, starred_at, updated_at, deleted, deleted_at
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

// ListRSSArticles returns articles matching the specified filter, ordered by published_at DESC.
func (s *Store) ListRSSArticles(ctx context.Context, filter RSSArticleFilter) ([]RSSArticle, error) {
	var where []string
	var args []any

	where = append(where, "deleted = 0")

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

	query := `SELECT
		id, feed_id, title, link, guid, author, published_at, snippet,
		content_html, is_full_text, hero_image, blocks_json,
		read, read_at, starred, starred_at, updated_at, deleted, deleted_at
	FROM rss_articles`
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

// DeleteRSSArticle deletes an article. If hard is false, marks deleted with a tombstone.
func (s *Store) DeleteRSSArticle(ctx context.Context, id string, hard bool) error {
	now := time.Now().UnixMilli()
	if hard {
		_, err := s.db.Execute(ctx, `DELETE FROM rss_articles WHERE id = ?`, id)
		return err
	}
	_, err := s.db.Execute(ctx, `UPDATE rss_articles SET deleted = 1, deleted_at = ?, updated_at = ? WHERE id = ?`, now, now, id)
	return err
}

// CleanOldRSSArticles cleans up old articles for a feed according to retention limits.
//
// ABSOLUTE RETENTION SAFETY INVARIANTS:
// 1. UNREAD articles (`read = 0`) are PERMANENTLY RETAINED and NEVER deleted.
// 2. STARRED articles (`starred = 1`) are PERMANENTLY RETAINED and NEVER deleted.
// 3. ONLY articles matching `read = 1 AND starred = 0 AND deleted = 0` are eligible.
func (s *Store) CleanOldRSSArticles(ctx context.Context, feedID string, maxReadArticles int, olderThan time.Duration, now time.Time) (int64, error) {
	// First, check how many eligible read articles exist for this feed
	countQuery := `SELECT COUNT(*) FROM rss_articles WHERE feed_id = ? AND read = 1 AND starred = 0 AND deleted = 0`
	rows, err := s.db.Query(ctx, countQuery, feedID)
	if err != nil || len(rows.Values) == 0 {
		return 0, err
	}
	eligibleCount, _ := int64FromAny(rows.Values[0][0])
	if eligibleCount == 0 {
		return 0, nil
	}

	cutoffDate := ""
	if olderThan > 0 {
		cutoffDate = now.Add(-olderThan).Format(time.RFC3339)
	}

	var deleteIDs []string

	// Check condition A: older than cutoff
	if cutoffDate != "" {
		ageQuery := `SELECT id FROM rss_articles
		WHERE feed_id = ? AND read = 1 AND starred = 0 AND deleted = 0 AND published_at < ?`
		ageRows, err := s.db.Query(ctx, ageQuery, feedID, cutoffDate)
		if err == nil {
			for _, r := range ageRows.Values {
				if id, ok := r[0].(string); ok && id != "" {
					deleteIDs = append(deleteIDs, id)
				}
			}
		}
	}

	// Check condition B: exceeding maxReadArticles count
	if maxReadArticles > 0 && int(eligibleCount) > maxReadArticles {
		excessQuery := fmt.Sprintf(`SELECT id FROM rss_articles
		WHERE feed_id = ? AND read = 1 AND starred = 0 AND deleted = 0
		ORDER BY published_at DESC, id DESC
		LIMIT -1 OFFSET %d`, maxReadArticles)
		excessRows, err := s.db.Query(ctx, excessQuery, feedID)
		if err == nil {
			for _, r := range excessRows.Values {
				if id, ok := r[0].(string); ok && id != "" {
					deleteIDs = append(deleteIDs, id)
				}
			}
		}
	}

	if len(deleteIDs) == 0 {
		return 0, nil
	}

	// Deduplicate IDs
	unique := make(map[string]bool, len(deleteIDs))
	var uniqueIDs []string
	for _, id := range deleteIDs {
		if !unique[id] {
			unique[id] = true
			uniqueIDs = append(uniqueIDs, id)
		}
	}

	// Execute deletion in a transaction
	placeholders := make([]string, len(uniqueIDs))
	args := make([]any, len(uniqueIDs))
	for i, id := range uniqueIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	delQuery := fmt.Sprintf(`DELETE FROM rss_articles WHERE id IN (%s) AND read = 1 AND starred = 0`, strings.Join(placeholders, ","))
	res, err := s.db.Execute(ctx, delQuery, args...)
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
	a.Deleted = intFromAnyBool(r[17])
	a.DeletedAt, _ = int64FromAny(r[18])
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
