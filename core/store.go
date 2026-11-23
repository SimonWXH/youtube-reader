package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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
	for _, ddl := range []string{
		`CREATE TABLE IF NOT EXISTS channels (id TEXT PRIMARY KEY, title TEXT, group_ids TEXT, avatar_url TEXT, uploads TEXT, updated_at INTEGER)`,
		`CREATE TABLE IF NOT EXISTS videos (id TEXT PRIMARY KEY, channel_id TEXT, channel_title TEXT, title TEXT, description TEXT, thumbnail_url TEXT, avatar_url TEXT, published_at TEXT, duration TEXT, width INTEGER, height INTEGER, is_short INTEGER)`,
		`CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT)`,
		`CREATE TABLE IF NOT EXISTS queue (id INTEGER PRIMARY KEY AUTOINCREMENT, payload TEXT)`,
		`CREATE TABLE IF NOT EXISTS notified (video_id TEXT PRIMARY KEY, notified_at INTEGER NOT NULL)`,
	} {
		if _, err := db.Execute(ctx, ddl); err != nil {
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
	rows, err := s.db.Query(ctx, `SELECT id,channel_id,channel_title,title,description,thumbnail_url,avatar_url,published_at,duration,width,height,is_short FROM videos`)
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
		stmts = append(stmts, sqlite.Statement{SQL: `INSERT INTO videos(id,channel_id,channel_title,title,description,thumbnail_url,avatar_url,published_at,duration,width,height,is_short) VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET channel_id=excluded.channel_id,channel_title=excluded.channel_title,title=excluded.title,description=excluded.description,thumbnail_url=COALESCE(NULLIF(excluded.thumbnail_url,''),videos.thumbnail_url),avatar_url=COALESCE(NULLIF(excluded.avatar_url,''),videos.avatar_url),published_at=excluded.published_at,duration=COALESCE(NULLIF(excluded.duration,''),videos.duration),width=excluded.width,height=excluded.height,is_short=excluded.is_short`, Args: []any{v.ID, v.ChannelID, v.ChannelTitle, v.Title, v.Description, v.ThumbnailURL, v.AvatarURL, v.PublishedAt, v.Duration, v.Width, v.Height, boolInt(v.IsShort)}})
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

func (s *Store) Count(ctx context.Context, table string) int {
	if table != "videos" && table != "channels" {
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
