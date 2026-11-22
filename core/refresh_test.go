package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRefreshYoutubeUsesSameFetchPipeline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "channels"):
			w.Write([]byte(`{"items":[{"id":"ch-1","snippet":{"title":"Channel","thumbnails":{"default":{"url":"http://avatar"}}},"contentDetails":{"relatedPlaylists":{"uploads":"uploads-1"}}}]}`))
		case strings.Contains(r.URL.Path, "playlistItems"):
			w.Write([]byte(`{"items":[{"contentDetails":{"videoId":"v1"},"snippet":{"title":"Video","description":"","thumbnails":{"default":{"url":"http://thumb"}},"publishedAt":"2026-11-20T10:00:00Z"}}]}`))
		case strings.Contains(r.URL.Path, "videos"):
			w.Write([]byte(`{"items":[{"id":"v1","contentDetails":{"duration":"PT10M0S"},"snippet":{"title":"Video"}}]}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	client := YouTube{Key: "key", Client: srv.Client(), BaseURL: srv.URL + "/youtube/v3"}
	state := SyncState{
		SchemaVersion: 1,
		Channels:      map[string]Channel{"ch-1": {ID: "ch-1", Title: "Old", UpdatedAt: 1}},
		ReadStates:    map[string]map[string]ReadRecord{},
		Favorites:     map[string]FavoriteRecord{},
	}
	settings := Settings{APIKey: "key", NotificationIDs: []string{"ch-1"}, RefreshMinutes: 60}
	result := RefreshYoutube(context.Background(), client, settings, state, nil, nil, 0, true, time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC))
	if !result.Changed || len(result.Videos) != 1 || len(result.Notified) != 1 {
		b, _ := json.Marshal(result)
		t.Fatalf("result=%s", b)
	}
	if result.Videos[0].ChannelID != "ch-1" || result.Videos[0].ChannelTitle != "Channel" || result.Notified[0].ChannelName != "Channel" {
		t.Fatalf("video=%+v notify=%+v", result.Videos[0], result.Notified[0])
	}
	if result.Videos[0].IsShort {
		t.Fatal("10 minute landscape video classified as Short")
	}
	if state.Channels["ch-1"].UploadedAt != "uploads-1" {
		t.Fatalf("channel metadata not updated: %+v", state.Channels["ch-1"])
	}
}

func TestRefreshYoutubeQuotaFailurePauses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":{"message":"quota exceeded"}}`))
	}))
	defer srv.Close()
	client := YouTube{Key: "key", Client: srv.Client(), BaseURL: srv.URL + "/youtube/v3"}
	state := SyncState{Channels: map[string]Channel{"ch-1": {ID: "ch-1", Title: "Channel"}}, ReadStates: map[string]map[string]ReadRecord{}, Favorites: map[string]FavoriteRecord{}}
	settings := Settings{APIKey: "key", RefreshMinutes: 60}
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	result := RefreshYoutube(context.Background(), client, settings, state, nil, nil, 0, true, now)
	if result.Changed || len(result.Failures) != 1 || !strings.HasPrefix(result.Failures[0].Message, "quota paused until ") {
		t.Fatalf("result=%+v", result)
	}
	if !strings.Contains(result.Failures[0].Message, "2026-11-21T12:00:00Z") {
		t.Fatalf("quota pause message=%q", result.Failures[0].Message)
	}
}

func refreshTestState() SyncState {
	return SyncState{
		SchemaVersion: 1,
		Channels:      map[string]Channel{"ch-1": {ID: "ch-1", Title: "Channel"}},
		ReadStates:    map[string]map[string]ReadRecord{},
		Favorites:     map[string]FavoriteRecord{},
	}
}

// abortBody drops the connection mid-response, so the client returns the
// request error a real transport failure gives, with the full URL.
func abortBody(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	conn, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		t.Errorf("hijack: %v", err)
		return
	}
	conn.Close()
}

func TestRefreshYoutubeRedactsAPIKeyFromFailedRefresh(t *testing.T) {
	const key = "AIzaSyD-transport-failure-key"
	srv := httptest.NewServer(http.NotFoundHandler())
	baseURL := srv.URL + "/youtube/v3"
	srv.Close()
	client := YouTube{Key: key, Client: srv.Client(), BaseURL: baseURL}
	settings := Settings{APIKey: key, RefreshMinutes: 60}
	result := RefreshYoutube(context.Background(), client, settings, refreshTestState(), nil, nil, 0, true, time.Now())
	if len(result.Failures) != 1 {
		t.Fatalf("failures=%+v", result.Failures)
	}
	message := result.Failures[0].Message
	if strings.Contains(message, key) || strings.Contains(result.Description, key) {
		t.Fatalf("failure leaks the API key: message=%q description=%q", message, result.Description)
	}
	if !strings.Contains(message, "/channels?") {
		t.Fatalf("failure lost the request context: %q", message)
	}
}

func TestRefreshYoutubeRedactsAPIKeyFromChannelFailure(t *testing.T) {
	const key = "AIzaSyD-channel-failure-key"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "channels") {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"items":[{"id":"ch-1","snippet":{"title":"Channel"},"contentDetails":{"relatedPlaylists":{"uploads":"uploads-1"}}}]}`))
			return
		}
		abortBody(t, w)
	}))
	defer srv.Close()
	client := YouTube{Key: key, Client: srv.Client(), BaseURL: srv.URL + "/youtube/v3"}
	settings := Settings{APIKey: key, RefreshMinutes: 60}
	result := RefreshYoutube(context.Background(), client, settings, refreshTestState(), nil, nil, 0, true, time.Now())
	if len(result.Failures) != 1 {
		t.Fatalf("failures=%+v", result.Failures)
	}
	message := result.Failures[0].Message
	if strings.Contains(message, key) {
		t.Fatalf("failure leaks the API key: %q", message)
	}
	if !strings.HasPrefix(message, "Channel: ") {
		t.Fatalf("failure lost the channel prefix: %q", message)
	}
}

func TestRefreshYoutubeRedactsAPIKeyFromHydrateFailure(t *testing.T) {
	const key = "AIzaSyD-hydrate-failure-key"
	published := time.Now().UTC().Format(time.RFC3339)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "channels"):
			w.Write([]byte(`{"items":[{"id":"ch-1","snippet":{"title":"Channel"},"contentDetails":{"relatedPlaylists":{"uploads":"uploads-1"}}}]}`))
		case strings.Contains(r.URL.Path, "playlistItems"):
			w.Write([]byte(`{"items":[{"contentDetails":{"videoId":"v1"},"snippet":{"title":"Video","publishedAt":"` + published + `"}}]}`))
		default:
			abortBody(t, w)
		}
	}))
	defer srv.Close()
	client := YouTube{Key: key, Client: srv.Client(), BaseURL: srv.URL + "/youtube/v3"}
	settings := Settings{APIKey: key, RefreshMinutes: 60}
	result := RefreshYoutube(context.Background(), client, settings, refreshTestState(), nil, nil, 0, true, time.Now())
	if len(result.Failures) != 1 {
		t.Fatalf("failures=%+v", result.Failures)
	}
	message := result.Failures[0].Message
	if strings.Contains(message, key) {
		t.Fatalf("failure leaks the API key: %q", message)
	}
	if !strings.HasPrefix(message, "hydrate: ") {
		t.Fatalf("failure lost the hydrate prefix: %q", message)
	}
}
