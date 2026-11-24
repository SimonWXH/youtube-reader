package core

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
)

// openOldStore writes a store with the schema a build before the history control
// shipped used, holding one channel and one video, and returns its path.
func openOldStore(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "videos.sqlite")
	ctx := context.Background()
	db, err := sqlite.Open(ctx, path, sqlite.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, ddl := range []string{
		`CREATE TABLE channels (id TEXT PRIMARY KEY, title TEXT, group_ids TEXT, avatar_url TEXT, uploads TEXT, updated_at INTEGER)`,
		`CREATE TABLE videos (id TEXT PRIMARY KEY, channel_id TEXT, channel_title TEXT, title TEXT, description TEXT, thumbnail_url TEXT, avatar_url TEXT, published_at TEXT, duration TEXT, width INTEGER, height INTEGER, is_short INTEGER)`,
		`INSERT INTO channels(id,title,group_ids,avatar_url,uploads,updated_at) VALUES('ch-1','Channel','[]','http://avatar','uploads-1',7)`,
		`INSERT INTO videos(id,channel_id,channel_title,title,description,thumbnail_url,avatar_url,published_at,duration,width,height,is_short) VALUES('v1','ch-1','Channel','Episode','','','', '2026-01-01T00:00:00Z','PT10M0S',0,0,0)`,
	} {
		if _, err := db.Execute(ctx, ddl); err != nil {
			t.Fatalf("%s: %v", ddl, err)
		}
	}
	return path
}

// TestStoreOpensAStoreWrittenBeforeTheHistoryFields is the upgrade path: a store
// an earlier build wrote keeps every row it holds and gains the two new fields.
func TestStoreOpensAStoreWrittenBeforeTheHistoryFields(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(ctx, openOldStore(t))
	if err != nil {
		t.Fatalf("open an older store: %v", err)
	}
	defer store.Close()

	channels, err := store.Channels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 1 || channels[0].ID != "ch-1" || channels[0].UploadedAt != "uploads-1" {
		t.Fatalf("channels after the upgrade = %+v; want the stored channel", channels)
	}
	videos, err := store.Videos(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(videos) != 1 || videos[0].ID != "v1" || videos[0].IsBackfilled {
		t.Fatalf("videos after the upgrade = %+v; want the stored video, not backfilled", videos)
	}
	cursors, err := store.BackfillCursors(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(cursors) != 0 {
		t.Fatalf("cursors after the upgrade = %+v; want none", cursors)
	}
	// Both new fields are usable, so the upgrade is complete and not just a
	// successful open.
	if err := store.SetBackfillCursor(ctx, "ch-1", "page-2"); err != nil {
		t.Fatal(err)
	}
	if got := cursorOf(t, store)["ch-1"]; got != (BackfillCursor{Started: true, Token: "page-2"}) {
		t.Fatalf("cursor = %+v; want the saved page", got)
	}
}

func cursorOf(t *testing.T, store *Store) map[string]BackfillCursor {
	t.Helper()
	cursors, err := store.BackfillCursors(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return cursors
}

// TestBackfillCursorRoundTrip covers the three positions a channel can be in: its
// history was never read, its next page is saved, and its earliest video is
// loaded.
func TestBackfillCursorRoundTrip(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(ctx, filepath.Join(t.TempDir(), "videos.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if got := cursorOf(t, store)["ch-1"]; got.Started {
		t.Fatalf("a channel nobody paged reads as started: %+v", got)
	}
	if err := store.SetBackfillCursor(ctx, "ch-1", "page-2"); err != nil {
		t.Fatal(err)
	}
	got := cursorOf(t, store)["ch-1"]
	if !got.Started || got.Complete() || got.Token != "page-2" {
		t.Fatalf("saved cursor = %+v; want the second page", got)
	}
	if err := store.SetBackfillCursor(ctx, "ch-1", ""); err != nil {
		t.Fatal(err)
	}
	if got := cursorOf(t, store)["ch-1"]; !got.Complete() {
		t.Fatalf("cursor = %+v after the last page; want complete", got)
	}
	// The cursor survives a reopen, which is what keeps a second app launch from
	// paying for the pages the first one read.
	path := store.Path()
	store.Close()
	reopened, err := OpenStore(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got := cursorOf(t, reopened)["ch-1"]; !got.Complete() {
		t.Fatalf("cursor after a reopen = %+v; want complete", got)
	}
}

// TestRefreshCommitKeepsABackfilledVideoMarked is the flush guard: a refresh that
// stores the same video again, or commits without the mark, must not put a
// backfilled video back inside retention's reach.
func TestRefreshCommitKeepsABackfilledVideoMarked(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(ctx, filepath.Join(t.TempDir(), "videos.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	video := Video{ID: "v1", ChannelID: "ch-1", Title: "Old episode", PublishedAt: "2024-01-01T00:00:00Z", IsBackfilled: true}
	if err := store.CommitRefresh(ctx, RefreshCommit{Videos: []Video{video}}); err != nil {
		t.Fatal(err)
	}
	// A refresh reads the video again and stores it without the mark, then the
	// history page reads the same video once more: one row, still marked.
	if err := store.CommitRefresh(ctx, RefreshCommit{Videos: []Video{{ID: "v1", ChannelID: "ch-1", Title: "Old episode", PublishedAt: "2024-01-01T00:00:00Z"}}}); err != nil {
		t.Fatal(err)
	}
	if err := store.CommitRefresh(ctx, RefreshCommit{Videos: []Video{video}}); err != nil {
		t.Fatal(err)
	}
	videos, err := store.Videos(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(videos) != 1 {
		t.Fatalf("the store holds %d rows for one video; want 1", len(videos))
	}
	if !videos[0].IsBackfilled {
		t.Fatal("a refresh unmarked the backfilled video, so retention would drop it")
	}
}

// TestBackfilledVideosSurviveRetention is the protection the whole feature rests
// on: a retained pass keeps every backfilled video, whatever its upload date, and
// the video cap counts only the videos a refresh brings.
func TestBackfilledVideosSurviveRetention(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	old := "2023-05-01T00:00:00Z"
	history := make([]Video, 30)
	for i := range history {
		history[i] = Video{ID: "history-" + string(rune('a'+i)), PublishedAt: old, IsBackfilled: true}
	}
	fresh := make([]Video, 5)
	for i := range fresh {
		fresh[i] = Video{ID: "fresh-" + string(rune('a'+i)), PublishedAt: "2026-03-01T00:00:00Z"}
	}
	all := append(append([]Video{}, history...), fresh...)
	kept := ApplyRetention(all, func(Video) bool { return false }, now, RetentionPolicy{Hours: 30, MaxVideos: 3})
	backfilled, recent := 0, 0
	for _, v := range kept {
		switch {
		case v.IsBackfilled:
			backfilled++
		case v.PublishedAt == old:
			t.Fatalf("video %q survived the window without the backfilled mark", v.ID)
		default:
			recent++
		}
	}
	if backfilled != len(history) {
		t.Fatalf("retention kept %d of the %d backfilled videos", backfilled, len(history))
	}
	if recent != 3 {
		t.Fatalf("retention kept %d refreshed videos; want the 3-video cap", recent)
	}
	// The same video, unmarked, is dropped by the same policy: the mark is what
	// protects it, not its date.
	unmarked := ApplyRetention([]Video{{ID: "history-a", PublishedAt: old}}, func(Video) bool { return false }, now, DefaultRetention())
	if len(unmarked) != 0 {
		t.Fatalf("an unmarked 2023 video survived: %+v", unmarked)
	}
}
