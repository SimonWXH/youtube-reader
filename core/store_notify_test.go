package core

import (
	"context"
	"testing"
	"time"
)

// TestNotifiedVideosPersistApartFromRetainedVideos proves the announced-video
// records are written with the refresh and read back on the next one, so the
// retained video rows are not the only memory of what was announced.
func TestNotifiedVideosPersistApartFromRetainedVideos(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(ctx, t.TempDir()+"/videos.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Date(2026, 11, 23, 5, 0, 0, 0, time.UTC)
	commit := func(videoID string, at time.Time) {
		t.Helper()
		if err := store.CommitRefresh(ctx, RefreshCommit{
			Revision: 1,
			Notified: []VideoNotification{{VideoID: videoID, NotifiedAt: at.UnixMilli()}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	commit("v-1", now)
	// Re-announcing a video updates its record instead of failing the commit.
	commit("v-1", now)
	commit("v-2", now.Add(time.Hour))

	notified, err := store.NotifiedVideoIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(notified) != 2 || !notified["v-1"] || !notified["v-2"] {
		t.Fatalf("notified = %+v", notified)
	}
	if videos, err := store.Videos(ctx); err != nil || len(videos) != 0 {
		t.Fatalf("videos = %+v, err = %v; want none", videos, err)
	}

	// A record must outlive the 30-day uploads window every refresh reads, or a
	// video retention dropped could be fetched and announced again; a record far
	// older than that can never match a fetched video again and is dropped. The
	// bounds are the uploads window and a generous multiple of it, not the
	// production constant, so a retention that is too short fails here.
	aged := now.Add(-31 * 24 * time.Hour)
	expired := now.Add(-90 * 24 * time.Hour)
	commit("v-old", expired)
	if err := store.CommitRefresh(ctx, RefreshCommit{
		Revision: 2,
		Notified: []VideoNotification{
			{VideoID: "v-aged", NotifiedAt: aged.UnixMilli()},
			{VideoID: "v-now", NotifiedAt: now.UnixMilli()},
		},
	}); err != nil {
		t.Fatal(err)
	}
	notified, err = store.NotifiedVideoIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !notified["v-aged"] || !notified["v-now"] {
		t.Fatalf("records inside the uploads window were pruned: %+v", notified)
	}
	if notified["v-old"] {
		t.Fatalf("a record older than the uploads window was kept: %+v", notified)
	}
}

// TestOpenStoreSeedsAnnouncedVideosFromAnOlderStore proves the upgrade path: a
// store written before announced videos were remembered has videos but no
// notified records, and the first open seeds the table so those videos are not
// announced again.
func TestOpenStoreSeedsAnnouncedVideosFromAnOlderStore(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/videos.sqlite"
	store, err := OpenStore(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CommitRefresh(ctx, RefreshCommit{Videos: []Video{
		{ID: "v-stored", ChannelID: "ch-1", Title: "Stored"},
	}}); err != nil {
		t.Fatal(err)
	}
	// An older build has no notified table at all.
	if _, err := store.DB().Execute(ctx, `DROP TABLE notified`); err != nil {
		t.Fatal(err)
	}
	store.Close()

	reopened, err := OpenStore(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	notified, err := reopened.NotifiedVideoIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !notified["v-stored"] {
		t.Fatalf("stored videos were not seeded as announced: %+v", notified)
	}
}
