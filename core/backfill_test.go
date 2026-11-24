package core

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// backfillServer answers the two endpoints one history page reads and records the
// queries it was asked for. pages maps a page token (empty for the newest page)
// to the playlist's answer, so a test can drive paging through several pages.
func backfillServer(t *testing.T, pages map[string]string, hydrated string) (*httptest.Server, *[]url.Values) {
	t.Helper()
	requests := &[]url.Values{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		*requests = append(*requests, r.URL.Query())
		switch {
		case strings.HasSuffix(r.URL.Path, "/playlistItems"):
			body, ok := pages[r.URL.Query().Get("pageToken")]
			if !ok {
				t.Errorf("no page for token %q", r.URL.Query().Get("pageToken"))
				w.Write([]byte(`{"items":[]}`))
				return
			}
			w.Write([]byte(body))
		case strings.HasSuffix(r.URL.Path, "/videos"):
			body := hydrated
			if body == "" {
				body = `{"items":[]}`
			}
			w.Write([]byte(body))
		case strings.HasSuffix(r.URL.Path, "/channels"):
			w.Write([]byte(`{"items":[{"id":"ch-1","snippet":{"title":"Rare channel"},"contentDetails":{"relatedPlaylists":{"uploads":"uploads-1"}}}]}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, requests
}

// playlistPageBody is one playlistItems answer: the video IDs it lists, each
// published at published, and the token of the page before it.
func playlistPageBody(published string, next string, ids ...string) string {
	items := make([]string, 0, len(ids))
	for _, id := range ids {
		items = append(items, fmt.Sprintf(`{"contentDetails":{"videoId":%q},"snippet":{"title":"Episode %s","publishedAt":%q}}`, id, id, published))
	}
	body := `{"items":[` + strings.Join(items, ",") + `]}`
	if next != "" {
		body = strings.TrimSuffix(body, "}") + `,"nextPageToken":"` + next + `"}`
	}
	return body
}

// hydratedBody is one videos.list answer: every ID at the given duration.
func hydratedBody(duration string, ids ...string) string {
	items := make([]string, 0, len(ids))
	for _, id := range ids {
		items = append(items, fmt.Sprintf(`{"id":%q,"snippet":{"title":"Episode %s"},"contentDetails":{"duration":%q}}`, id, id, duration))
	}
	return `{"items":[` + strings.Join(items, ",") + `]}`
}

// TestBackfillChannelReadsHistoryPastTheRefreshWindow proves the reason the
// control exists: a page of uploads older than the 30 days a refresh reads comes
// back whole, marked as backfilled, with the token that reads the page before it.
func TestBackfillChannelReadsHistoryPastTheRefreshWindow(t *testing.T) {
	old := time.Now().AddDate(-3, 0, 0).UTC().Format(time.RFC3339)
	ids := make([]string, PlaylistPageSize)
	for i := range ids {
		ids[i] = fmt.Sprintf("v%02d", i)
	}
	srv, requests := backfillServer(t, map[string]string{"": playlistPageBody(old, "page-2", ids...)}, hydratedBody("PT12M0S", ids...))
	client := YouTube{Key: "key", Client: srv.Client(), BaseURL: srv.URL}
	channel := Channel{ID: "ch-1", Title: "Rare channel", AvatarURL: "http://avatar", UploadedAt: "uploads-1"}

	result, err := BackfillChannel(context.Background(), client, channel, "")
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if len(result.Videos) != PlaylistPageSize {
		t.Fatalf("page held %d videos; want the whole %d-video page", len(result.Videos), PlaylistPageSize)
	}
	if result.NextPageToken != "page-2" || result.Complete() {
		t.Fatalf("next token = %q; want page-2", result.NextPageToken)
	}
	for _, v := range result.Videos {
		if !v.IsBackfilled {
			t.Fatalf("video %q is not marked backfilled", v.ID)
		}
		if v.ChannelID != "ch-1" || v.ChannelTitle != "Rare channel" || v.AvatarURL != "http://avatar" {
			t.Fatalf("video %q lost the channel's metadata: %+v", v.ID, v)
		}
		if v.Duration != "PT12M0S" || v.IsShort {
			t.Fatalf("video %q was not hydrated: %+v", v.ID, v)
		}
	}
	if len(*requests) != 2 {
		t.Fatalf("one page cost %d requests; want 2 (playlistItems plus one hydration)", len(*requests))
	}
	listing := (*requests)[0]
	if listing.Get("playlistId") != "uploads-1" {
		t.Fatalf("page read playlist %q; want the channel's uploads", listing.Get("playlistId"))
	}
	if listing.Get("maxResults") != fmt.Sprint(PlaylistPageSize) {
		t.Fatalf("page size = %q; want %d", listing.Get("maxResults"), PlaylistPageSize)
	}
	if listing.Get("pageToken") != "" {
		t.Fatalf("the newest page was asked for with token %q", listing.Get("pageToken"))
	}
}

// TestBackfillChannelReusesTheSavedToken covers the saved position: the token a
// page returns reads the page before it, and a page with no next token is the
// channel's earliest video.
func TestBackfillChannelReusesTheSavedToken(t *testing.T) {
	newest := time.Now().AddDate(0, -6, 0).UTC().Format(time.RFC3339)
	srv, requests := backfillServer(t, map[string]string{
		"":       playlistPageBody(newest, "page-2", "v1", "v2"),
		"page-2": playlistPageBody(newest, "", "v3"),
	}, hydratedBody("PT8M0S", "v1", "v2", "v3"))
	client := YouTube{Key: "key", Client: srv.Client(), BaseURL: srv.URL}
	channel := Channel{ID: "ch-1", Title: "Channel", UploadedAt: "uploads-1"}
	ctx := context.Background()

	first, err := BackfillChannel(ctx, client, channel, "")
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if first.Complete() || first.NextPageToken != "page-2" {
		t.Fatalf("first page next token = %q; want page-2", first.NextPageToken)
	}
	second, err := BackfillChannel(ctx, client, channel, first.NextPageToken)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if !second.Complete() {
		t.Fatalf("the last page is not complete: %+v", second)
	}
	if len(second.Videos) != 1 || second.Videos[0].ID != "v3" {
		t.Fatalf("second page = %+v; want the channel's oldest video", second.Videos)
	}
	listing := (*requests)[2]
	if listing.Get("pageToken") != "page-2" {
		t.Fatalf("the second page was asked for with token %q; want the saved page-2", listing.Get("pageToken"))
	}
}

// TestBackfillChannelKeepsOnlyLongVideos proves the Shorts rule holds for history
// pages too: a Short costs a slot in the page but never reaches the store, and
// only the hydration can tell, so it still runs.
func TestBackfillChannelKeepsOnlyLongVideos(t *testing.T) {
	published := time.Now().AddDate(-1, 0, 0).UTC().Format(time.RFC3339)
	srv, _ := backfillServer(t, map[string]string{"": playlistPageBody(published, "", "v-short", "v-long")},
		`{"items":[
			{"id":"v-short","snippet":{"title":"A short"},"contentDetails":{"duration":"PT0M45S"}},
			{"id":"v-long","snippet":{"title":"An episode"},"contentDetails":{"duration":"PT42M0S"}}]}`)
	client := YouTube{Key: "key", Client: srv.Client(), BaseURL: srv.URL}
	result, err := BackfillChannel(context.Background(), client, Channel{ID: "ch-1", UploadedAt: "uploads-1"}, "")
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if len(result.Videos) != 1 || result.Videos[0].ID != "v-long" {
		t.Fatalf("page kept %+v; want only the long video", result.Videos)
	}
	if !result.Complete() {
		t.Fatal("a page with no next token is not complete")
	}
}

// TestBackfillChannelFailsWithoutLosingThePage: a page whose hydration fails
// reports the failure and nothing else, so the caller keeps its page token and a
// retry reads the same page instead of skipping 50 videos.
func TestBackfillChannelFailsWithoutLosingThePage(t *testing.T) {
	published := time.Now().AddDate(-1, 0, 0).UTC().Format(time.RFC3339)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/playlistItems") {
			w.Write([]byte(playlistPageBody(published, "page-2", "v1")))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"message":"backend error"}}`))
	}))
	defer srv.Close()
	client := YouTube{Key: "key", Client: srv.Client(), BaseURL: srv.URL}
	result, err := BackfillChannel(context.Background(), client, Channel{ID: "ch-1", UploadedAt: "uploads-1"}, "")
	if err == nil {
		t.Fatal("a failed hydration reported success")
	}
	if len(result.Videos) != 0 || result.NextPageToken != "" {
		t.Fatalf("a failed page returned %+v; want nothing to commit", result)
	}
}

// TestBackfillChannelResolvesAnUnknownUploadsPlaylist covers the channel a
// refresh never fetched: its uploads playlist is one extra lookup away, and its
// history is still readable.
func TestBackfillChannelResolvesAnUnknownUploadsPlaylist(t *testing.T) {
	published := time.Now().AddDate(-2, 0, 0).UTC().Format(time.RFC3339)
	srv, requests := backfillServer(t, map[string]string{"": playlistPageBody(published, "", "v1")}, hydratedBody("PT6M0S", "v1"))
	client := YouTube{Key: "key", Client: srv.Client(), BaseURL: srv.URL}
	result, err := BackfillChannel(context.Background(), client, Channel{ID: "ch-1"}, "")
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if len(result.Videos) != 1 {
		t.Fatalf("page = %+v; want the one video", result.Videos)
	}
	if len(*requests) != 3 {
		t.Fatalf("an unknown playlist cost %d requests; want the lookup and the page", len(*requests))
	}
	if got := (*requests)[0].Get("id"); got != "ch-1" {
		t.Fatalf("lookup asked for channels %q; want ch-1", got)
	}
}

// TestBackfillChannelReportsQuota stops the page at the API's quota refusal, so
// the app can arm its pause instead of retrying a page that cannot be read.
func TestBackfillChannelReportsQuota(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":{"message":"quota exceeded"}}`))
	}))
	defer srv.Close()
	client := YouTube{Key: "key", Client: srv.Client(), BaseURL: srv.URL}
	if _, err := BackfillChannel(context.Background(), client, Channel{ID: "ch-1", UploadedAt: "uploads-1"}, ""); !errors.Is(err, ErrQuota) {
		t.Fatalf("quota failure = %v; want ErrQuota", err)
	}
}
