package core

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// notifyUploadsServer serves an uploads playlist of `pages` items for every
// channel it is asked about, plus the videos.list hydration those items need.
// Each video's publishedAt comes from notifyPublishedAt.
func notifyUploadsServer(t *testing.T, channels int, videosPerChannel int, now time.Time) *httptest.Server {
	t.Helper()
	playlists := map[string][]string{}
	published := map[string]string{}
	durations := map[string]string{}
	for c := 1; c <= channels; c++ {
		id := "uploads-" + strconv.Itoa(c)
		for v := 0; v < videosPerChannel; v++ {
			videoID := fmt.Sprintf("v-%d-%d", c, v)
			// The one videos per channel list is ordered newest first, and the
			// channels newest first too, so retention's video cap drops the
			// last channel's videos.
			index := (c-1)*videosPerChannel + v
			playlists[id] = append(playlists[id], videoID)
			published[videoID] = FormatRFC3339(now.Add(-time.Duration(index) * time.Minute))
			durations[videoID] = "PT10M0S"
		}
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "channels"):
			var items []string
			for c := 1; c <= channels; c++ {
				items = append(items, fmt.Sprintf(`{"id":"ch-%d","snippet":{"title":"Channel %d"},"contentDetails":{"relatedPlaylists":{"uploads":"uploads-%d"}}}`, c, c, c))
			}
			fmt.Fprintf(w, `{"items":[%s]}`, strings.Join(items, ","))
		case strings.Contains(r.URL.Path, "playlistItems"):
			playlist := r.URL.Query().Get("playlistId")
			var items []string
			for _, videoID := range playlists[playlist] {
				items = append(items, fmt.Sprintf(`{"contentDetails":{"videoId":"%s"},"snippet":{"title":"Video %s","publishedAt":"%s"}}`, videoID, videoID, published[videoID]))
			}
			fmt.Fprintf(w, `{"items":[%s]}`, strings.Join(items, ","))
		case strings.Contains(r.URL.Path, "videos"):
			var items []string
			for _, videoID := range strings.Split(r.URL.Query().Get("id"), ",") {
				if _, ok := published[videoID]; !ok {
					continue
				}
				items = append(items, fmt.Sprintf(`{"id":"%s","snippet":{"title":"Video %s","publishedAt":"%s"},"contentDetails":{"duration":"%s"}}`, videoID, videoID, published[videoID], durations[videoID]))
			}
			fmt.Fprintf(w, `{"items":[%s]}`, strings.Join(items, ","))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
}

// testRefreshNow is the refresh clock these fixtures run at: the wall clock,
// truncated so published times stay stable through one test.
func testRefreshNow() time.Time { return time.Now().UTC().Truncate(time.Second) }

// TestRefreshDoesNotRenotifyRetentionDroppedVideos is the captain's hourly
// repeat: retention keeps only the newest 400 videos, so a video it drops was
// never committed and the next refresh counts it as new again. The notified
// records live beside the refresh, not in the retained video rows.
func TestRefreshDoesNotRenotifyRetentionDroppedVideos(t *testing.T) {
	const channels, perChannel = 9, 50 // 450 fetched; retention keeps 400
	// PlaylistVideos drops uploads older than 30 days by the wall clock, so the
	// fixtures are laid out from now rather than from a fixed date.
	now := testRefreshNow()
	srv := notifyUploadsServer(t, channels, perChannel, now)
	defer srv.Close()
	client := YouTube{Key: "key", Client: srv.Client(), BaseURL: srv.URL + "/youtube/v3"}

	ctx := context.Background()
	store, err := OpenStore(ctx, t.TempDir()+"/videos.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	state := SyncState{
		SchemaVersion: 1,
		Channels:      map[string]Channel{},
		ReadStates:    map[string]map[string]ReadRecord{},
		Favorites:     map[string]FavoriteRecord{},
	}
	for c := 1; c <= channels; c++ {
		state.Channels["ch-"+strconv.Itoa(c)] = Channel{ID: "ch-" + strconv.Itoa(c), Title: "Channel"}
	}
	// The channel whose videos the cap drops is the one the reader opted in
	// for, which is how the captain saw the same videos alert every hour.
	settings := Settings{APIKey: "key", RefreshMinutes: 60, NotificationIDs: []string{"ch-9"}}

	existing, err := store.Videos(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first := RefreshYoutube(ctx, client, settings, state, store, existing, 0, true, now)
	if len(first.Notified) != 1 || first.Notified[0].Count != perChannel {
		t.Fatalf("first refresh notifications = %+v, want one plan with %d videos", first.Notified, perChannel)
	}

	existing, err = store.Videos(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(existing) != DefaultRetention().MaxVideos {
		t.Fatalf("stored videos = %d, want the retained %d", len(existing), DefaultRetention().MaxVideos)
	}
	settings.LastRefresh = FormatRFC3339(now)
	second := RefreshYoutube(ctx, client, settings, state, store, existing, first.Revision, false, now.Add(time.Hour))
	if len(second.Notified) != 0 {
		t.Fatalf("second refresh re-notified already notified videos: %+v", second.Notified)
	}
}

// TestRefreshDoesNotNotifyReadVideos proves a video the reader has already
// read never counts as new, even while retention still holds its row.
func TestRefreshDoesNotNotifyReadVideos(t *testing.T) {
	now := testRefreshNow()
	srv := notifyUploadsServer(t, 1, 1, now)
	defer srv.Close()
	client := YouTube{Key: "key", Client: srv.Client(), BaseURL: srv.URL + "/youtube/v3"}

	state := SyncState{
		SchemaVersion: 1,
		Channels:      map[string]Channel{"ch-1": {ID: "ch-1", Title: "Channel"}},
		ReadStates:    map[string]map[string]ReadRecord{"v-1-0": {"device": {VideoID: "v-1-0", DeviceID: "device", Read: true, UpdatedAt: now.UnixMilli()}}},
		Favorites:     map[string]FavoriteRecord{},
	}
	settings := Settings{APIKey: "key", RefreshMinutes: 60, NotificationIDs: []string{"ch-1"}}
	result := RefreshYoutube(context.Background(), client, settings, state, nil, nil, 0, true, now)
	if len(result.Notified) != 0 {
		t.Fatalf("read video was notified: %+v", result.Notified)
	}
}
