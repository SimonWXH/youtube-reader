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

	// A record far older than the uploads window every refresh reads cannot
	// match a fetched video again, so the next commit drops it.
	later := now.Add(notifiedVideoRetention + 2*time.Hour)
	commit("v-3", later)
	notified, err = store.NotifiedVideoIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(notified) != 1 || !notified["v-3"] {
		t.Fatalf("notified after pruning = %+v", notified)
	}
}
