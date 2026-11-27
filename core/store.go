package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
)

type Store struct {
	db   *sqlite.DB
	path string
}

func OpenStore(ctx context.Context, path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sqlite.Open(ctx, path, sqlite.OpenOptions{BusyTimeout: 5 * time.Second})
	if err != nil {
		return nil, err
	}
	s := &Store{db: db, path: path}
	// A store written before announced videos were remembered has videos but no
	// notified records. The first process to open it seeds the table from the
	// videos that build already stored, so they are not announced again.
	newNotified, err := s.missingTable(ctx, "notified")
	if err != nil {
		db.Close()
		return nil, err
	}
	for _, ddl := range []string{
		`CREATE TABLE IF NOT EXISTS channels (id TEXT PRIMARY KEY, title TEXT, group_ids TEXT, avatar_url TEXT, uploads TEXT, updated_at INTEGER, next_page_token TEXT)`,
		`CREATE TABLE IF NOT EXISTS videos (id TEXT PRIMARY KEY, channel_id TEXT, channel_title TEXT, title TEXT, description TEXT, thumbnail_url TEXT, avatar_url TEXT, published_at TEXT, duration TEXT, width INTEGER, height INTEGER, is_short INTEGER, is_backfilled INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT)`,
		`CREATE TABLE IF NOT EXISTS queue (id INTEGER PRIMARY KEY AUTOINCREMENT, payload TEXT)`,
		`CREATE TABLE IF NOT EXISTS notified (video_id TEXT PRIMARY KEY, notified_at INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS rss_feeds (id TEXT PRIMARY KEY, feed_url TEXT NOT NULL, site_url TEXT, title TEXT, custom_title TEXT, group_ids TEXT, avatar_url TEXT, etag TEXT, last_modified TEXT, last_fetched_at INTEGER NOT NULL DEFAULT 0, failure_count INTEGER NOT NULL DEFAULT 0, last_error TEXT, last_error_at INTEGER NOT NULL DEFAULT 0, refresh_interval_mins INTEGER NOT NULL DEFAULT 30, updated_at INTEGER NOT NULL, deleted INTEGER NOT NULL DEFAULT 0, deleted_at INTEGER NOT NULL DEFAULT 0, fetch_full_text INTEGER)`,
		`CREATE TABLE IF NOT EXISTS rss_groups (id TEXT PRIMARY KEY, name TEXT NOT NULL, feed_ids TEXT, sort_order INTEGER NOT NULL DEFAULT 0, updated_at INTEGER NOT NULL, deleted INTEGER NOT NULL DEFAULT 0, deleted_at INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE IF NOT EXISTS rss_articles (id TEXT PRIMARY KEY, feed_id TEXT NOT NULL, title TEXT NOT NULL, link TEXT, guid TEXT, author TEXT, published_at TEXT, snippet TEXT, content_html TEXT, is_full_text INTEGER NOT NULL DEFAULT 0, hero_image TEXT, blocks_json TEXT, read INTEGER NOT NULL DEFAULT 0, read_at INTEGER NOT NULL DEFAULT 0, starred INTEGER NOT NULL DEFAULT 0, starred_at INTEGER NOT NULL DEFAULT 0, updated_at INTEGER NOT NULL, feed_content_html TEXT)`,
		`CREATE INDEX IF NOT EXISTS idx_rss_articles_feed_id ON rss_articles(feed_id)`,
		`CREATE INDEX IF NOT EXISTS idx_rss_articles_published ON rss_articles(published_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_rss_articles_read ON rss_articles(read)`,
		`CREATE INDEX IF NOT EXISTS idx_rss_articles_starred ON rss_articles(starred)`,
		`CREATE TABLE IF NOT EXISTS rss_notes (id TEXT PRIMARY KEY, article_id TEXT NOT NULL, paragraph_index INTEGER NOT NULL, paragraph_hash TEXT, selected_text TEXT, note_text TEXT, updated_at INTEGER NOT NULL, deleted INTEGER NOT NULL DEFAULT 0, deleted_at INTEGER NOT NULL DEFAULT 0)`,
		`CREATE INDEX IF NOT EXISTS idx_rss_notes_article_id ON rss_notes(article_id)`,
		`CREATE TABLE IF NOT EXISTS rss_read_states (article_id TEXT NOT NULL, device_id TEXT NOT NULL, read INTEGER NOT NULL, updated_at INTEGER NOT NULL, PRIMARY KEY(article_id, device_id))`,
	} {
		if _, err := db.Execute(ctx, ddl); err != nil {
			db.Close()
			return nil, err
		}
	}
	// A store an earlier build wrote keeps every row it holds and gains the
	// fields those builds did not have: the page token that remembers how far a
	// channel's history paging got, the mark that keeps a video the reader
	// loaded that way out of retention's reach, and the feed's answer to the
	// reader's automatic full-text fetch, which a NULL column leaves at the
	// default of fetching it.
	for _, column := range [][3]string{
		{"channels", "next_page_token", "next_page_token TEXT"},
		{"videos", "is_backfilled", "is_backfilled INTEGER NOT NULL DEFAULT 0"},
		{"rss_feeds", "fetch_full_text", "fetch_full_text INTEGER"},
		// The feed's own body kept beside the extracted full text, for the
		// reader's original/full-text toggle. Only ever added, and a store
		// written before it holds rows whose column is NULL, which reads as
		// an empty value and leaves those articles on their summary.
		{"rss_articles", "feed_content_html", "feed_content_html TEXT"},
	} {
		if err := s.ensureColumn(ctx, column[0], column[1], column[2]); err != nil {
			db.Close()
			return nil, err
		}
	}
	if newNotified {
		if _, err := db.Execute(ctx, `INSERT OR IGNORE INTO notified(video_id, notified_at) SELECT id, ? FROM videos`, time.Now().UnixMilli()); err != nil {
			db.Close()
			return nil, err
		}
	}
	if _, err := db.Execute(ctx, `PRAGMA journal_mode=WAL`); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// missingTable reports whether the store does not have the named table yet, so
// a caller can tell a fresh store from one an older build wrote.
func (s *Store) missingTable(ctx context.Context, name string) (bool, error) {
	rows, err := s.db.Query(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name=?`, name)
	if err != nil {
		return false, err
	}
	return len(rows.Values) == 0, nil
}

// ensureColumn adds a column an older store does not have, so opening a store a
// previous build wrote keeps its rows instead of failing on the missing field.
// The table names are this file's own DDL, never a caller's.
func (s *Store) ensureColumn(ctx context.Context, table, column, definition string) error {
	rows, err := s.db.Query(ctx, `SELECT name FROM pragma_table_info('`+table+`')`)
	if err != nil {
		return err
	}
	for _, row := range rows.Values {
		if name, _ := row[0].(string); name == column {
			return nil
		}
	}
	_, err = s.db.Execute(ctx, `ALTER TABLE `+table+` ADD COLUMN `+definition)
	return err
}

func (s *Store) Close() error   { return s.db.Close() }
func (s *Store) DB() *sqlite.DB { return s.db }

func (s *Store) Meta(ctx context.Context, key string) string {
	rows, err := s.db.Query(ctx, `SELECT value FROM meta WHERE key = ?`, key)
	if err != nil || len(rows.Values) == 0 {
		return ""
	}
	v, _ := rows.Values[0][0].(string)
	return v
}

func (s *Store) SetMeta(ctx context.Context, key, value string) error {
	_, err := s.db.Execute(ctx, `INSERT INTO meta(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

func (s *Store) Channels(ctx context.Context) ([]Channel, error) {
	rows, err := s.db.Query(ctx, `SELECT id,title,group_ids,avatar_url,uploads,updated_at FROM channels`)
	if err != nil {
		return nil, err
	}
	out := make([]Channel, 0, len(rows.Values))
	for _, r := range rows.Values {
		var c Channel
		c.ID, _ = r[0].(string)
		c.Title, _ = r[1].(string)
		if raw, _ := r[2].(string); raw != "" {
			_ = json.Unmarshal([]byte(raw), &c.GroupIDs)
		}
		c.AvatarURL, _ = r[3].(string)
		c.UploadedAt, _ = r[4].(string)
		c.UpdatedAt, _ = r[5].(int64)
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Title < out[j].Title })
	return out, nil
}

func (s *Store) Videos(ctx context.Context) ([]Video, error) {
	rows, err := s.db.Query(ctx, `SELECT id,channel_id,channel_title,title,description,thumbnail_url,avatar_url,published_at,duration,width,height,is_short,is_backfilled FROM videos`)
	if err != nil {
		return nil, err
	}
	out := make([]Video, 0, len(rows.Values))
	for _, r := range rows.Values {
		v := rowToVideo(r)
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PublishedAt > out[j].PublishedAt })
	return out, nil
}

// BackfillCursors returns where each channel's history paging stands, by channel
// ID. A channel the map does not name has never loaded its history; a channel it
// names with an empty token reached its earliest video.
func (s *Store) BackfillCursors(ctx context.Context) (map[string]BackfillCursor, error) {
	rows, err := s.db.Query(ctx, `SELECT id,next_page_token FROM channels WHERE next_page_token IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	out := make(map[string]BackfillCursor, len(rows.Values))
	for _, r := range rows.Values {
		id, _ := r[0].(string)
		if id == "" {
			continue
		}
		token, _ := r[1].(string)
		out[id] = BackfillCursor{Started: true, Token: token}
	}
	return out, nil
}

// SetBackfillCursor records the page token a channel's next "load more" click
// reads, empty once its earliest video is loaded. Recording the token after the
// page's videos are committed is what makes a failed or interrupted click read
// the same page again rather than skip it; the videos' primary key makes that
// repeat harmless.
func (s *Store) SetBackfillCursor(ctx context.Context, channelID, token string) error {
	if channelID == "" {
		return errors.New("a backfill cursor names a channel")
	}
	_, err := s.db.Execute(ctx, `INSERT INTO channels(id, next_page_token) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET next_page_token=excluded.next_page_token`, channelID, token)
	return err
}

func rowToVideo(r []any) Video {
	var v Video
	v.ID, _ = r[0].(string)
	v.ChannelID, _ = r[1].(string)
	v.ChannelTitle, _ = r[2].(string)
	v.Title, _ = r[3].(string)
	v.Description, _ = r[4].(string)
	v.ThumbnailURL, _ = r[5].(string)
	v.AvatarURL, _ = r[6].(string)
	v.PublishedAt, _ = r[7].(string)
	v.Duration, _ = r[8].(string)
	v.Width, _ = intFromAny(r[9])
	v.Height, _ = intFromAny(r[10])
	v.IsShort = intFromAnyBool(r[11])
	v.IsBackfilled = intFromAnyBool(r[12])
	return v
}

func intFromAny(v any) (int, bool) {
	switch n := v.(type) {
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case string:
		x, err := strconv.Atoi(n)
		return x, err == nil
	}
	return 0, false
}
func intFromAnyBool(v any) bool { n, _ := intFromAny(v); return n != 0 }

// NotifiedVideoIDs returns the videos whose arrival was already announced.
// They are kept apart from the retained videos so neither retention's window
// nor its video cap can make an announced video look new again.
func (s *Store) NotifiedVideoIDs(ctx context.Context) (map[string]bool, error) {
	rows, err := s.db.Query(ctx, `SELECT video_id FROM notified`)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(rows.Values))
	for _, r := range rows.Values {
		if id, ok := r[0].(string); ok && id != "" {
			out[id] = true
		}
	}
	return out, nil
}

type RefreshCommit struct {
	Channels  []Channel
	Videos    []Video
	DeleteIDs []string
	Revision  int64
	// Notified are the videos whose arrival this refresh announced. They are
	// written in the same transaction as the refresh, so an announced video
	// is known even when retention drops its row from Videos at once.
	Notified []VideoNotification
}

func (s *Store) CommitRefresh(ctx context.Context, c RefreshCommit) error {
	stmts := []sqlite.Statement{}
	for _, ch := range c.Channels {
		groups, _ := json.Marshal(ch.GroupIDs)
		stmts = append(stmts, sqlite.Statement{SQL: `INSERT INTO channels(id,title,group_ids,avatar_url,uploads,updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET title=excluded.title,group_ids=excluded.group_ids,avatar_url=COALESCE(NULLIF(excluded.avatar_url,''),channels.avatar_url),uploads=COALESCE(NULLIF(excluded.uploads,''),channels.uploads),updated_at=excluded.updated_at`, Args: []any{ch.ID, ch.Title, string(groups), ch.AvatarURL, ch.UploadedAt, ch.UpdatedAt}})
	}
	for _, v := range c.Videos {
		// is_backfilled only ever rises: a refresh re-storing a video the reader
		// loaded earlier must not put that video back inside retention's reach.
		stmts = append(stmts, sqlite.Statement{SQL: `INSERT INTO videos(id,channel_id,channel_title,title,description,thumbnail_url,avatar_url,published_at,duration,width,height,is_short,is_backfilled) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET channel_id=excluded.channel_id,channel_title=excluded.channel_title,title=excluded.title,description=excluded.description,thumbnail_url=COALESCE(NULLIF(excluded.thumbnail_url,''),videos.thumbnail_url),avatar_url=COALESCE(NULLIF(excluded.avatar_url,''),videos.avatar_url),published_at=excluded.published_at,duration=COALESCE(NULLIF(excluded.duration,''),videos.duration),width=excluded.width,height=excluded.height,is_short=excluded.is_short,is_backfilled=MAX(COALESCE(videos.is_backfilled,0),excluded.is_backfilled)`, Args: []any{v.ID, v.ChannelID, v.ChannelTitle, v.Title, v.Description, v.ThumbnailURL, v.AvatarURL, v.PublishedAt, v.Duration, v.Width, v.Height, boolInt(v.IsShort), boolInt(v.IsBackfilled)}})
	}
	for _, id := range c.DeleteIDs {
		stmts = append(stmts, sqlite.Statement{SQL: `DELETE FROM videos WHERE id = ?`, Args: []any{id}})
	}
	if len(c.Notified) > 0 {
		newest := int64(0)
		for _, n := range c.Notified {
			stmts = append(stmts, sqlite.Statement{SQL: `INSERT INTO notified(video_id,notified_at) VALUES(?,?) ON CONFLICT(video_id) DO UPDATE SET notified_at=excluded.notified_at`, Args: []any{n.VideoID, n.NotifiedAt}})
			newest = max(newest, n.NotifiedAt)
		}
		// Announced videos older than the retention window cannot appear in
		// any later refresh, which reads the last 30 days of uploads.
		stmts = append(stmts, sqlite.Statement{SQL: `DELETE FROM notified WHERE notified_at < ?`, Args: []any{newest - int64(notifiedVideoRetention/time.Millisecond)}})
	}
	stmts = append(stmts, sqlite.Statement{SQL: `INSERT INTO meta(key,value) VALUES('revision',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, Args: []any{strconv.FormatInt(c.Revision, 10)}})
	_, err := s.db.Transaction(ctx, stmts)
	return err
}

// SetChannelGroupIDs updates a channel's group memberships and timestamp in the local store.
func (s *Store) SetChannelGroupIDs(ctx context.Context, channelID string, groupIDs []string, updatedAt int64) error {
	var raw string
	if groupIDs != nil {
		data, err := json.Marshal(groupIDs)
		if err != nil {
			return err
		}
		raw = string(data)
	}
	_, err := s.db.Execute(ctx, `INSERT INTO channels(id, group_ids, updated_at) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET group_ids=excluded.group_ids, updated_at=excluded.updated_at`, channelID, raw, updatedAt)
	return err
}

// DeleteChannel removes a channel from the local store along with its videos,
// preserving any video whose ID appears in keepVideoIDs (e.g. favorited videos).
func (s *Store) DeleteChannel(ctx context.Context, channelID string, keepVideoIDs []string) error {
	stmts := []sqlite.Statement{
		{SQL: `DELETE FROM channels WHERE id = ?`, Args: []any{channelID}},
	}
	if len(keepVideoIDs) == 0 {
		stmts = append(stmts, sqlite.Statement{
			SQL:  `DELETE FROM videos WHERE channel_id = ?`,
			Args: []any{channelID},
		})
	} else {
		placeholders := make([]string, len(keepVideoIDs))
		args := make([]any, 1+len(keepVideoIDs))
		args[0] = channelID
		for i, id := range keepVideoIDs {
			placeholders[i] = "?"
			args[1+i] = id
		}
		stmts = append(stmts, sqlite.Statement{
			SQL:  fmt.Sprintf(`DELETE FROM videos WHERE channel_id = ? AND id NOT IN (%s)`, strings.Join(placeholders, ",")),
			Args: args,
		})
	}
	_, err := s.db.Transaction(ctx, stmts)
	return err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *Store) Queue(ctx context.Context, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = s.db.Execute(ctx, `INSERT INTO queue(payload) VALUES(?)`, string(data))
	return err
}

func (s *Store) DrainQueue(ctx context.Context) ([]json.RawMessage, error) {
	rows, err := s.db.Query(ctx, `SELECT id,payload FROM queue ORDER BY id`)
	if err != nil {
		return nil, err
	}
	out := make([]json.RawMessage, 0, len(rows.Values))
	ids := make([]any, 0, len(rows.Values))
	for _, r := range rows.Values {
		var id int64
		id, _ = r[0].(int64)
		ids = append(ids, id)
		if raw, ok := r[1].(string); ok {
			out = append(out, json.RawMessage(raw))
		}
	}
	for _, id := range ids {
		if _, err := s.db.Execute(ctx, `DELETE FROM queue WHERE id=?`, id); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Count returns the number of rows in the specified table ("videos", "channels", "rss_feeds", or "rss_articles").
func (s *Store) Count(ctx context.Context, table string) int {
	if table != "videos" && table != "channels" && table != "rss_feeds" && table != "rss_articles" {
		return 0
	}
	rows, err := s.db.Query(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s`, table))
	if err != nil || len(rows.Values) == 0 {
		return 0
	}
	n, _ := rows.Values[0][0].(int64)
	return int(n)
}

func (s *Store) Path() string { return s.path }
