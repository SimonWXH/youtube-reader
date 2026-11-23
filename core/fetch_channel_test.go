package core

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// resolveServer answers the channels and videos lookups the resolver makes and
// records the paths it was asked for.
func resolveServer(t *testing.T, seen *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = append(*seen, r.URL.Path+"?"+r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/channels"):
			switch {
			case r.URL.Query().Get("id") == "UCXuqSBlHAE6Xw-yeJA0Tunw":
				w.Write([]byte(`{"items":[{"id":"UCXuqSBlHAE6Xw-yeJA0Tunw","snippet":{"title":"Linus Tech Tips","thumbnails":{"default":{"url":"http://avatar"}}},"contentDetails":{"relatedPlaylists":{"uploads":"uploads-1"}}}]}`))
			case r.URL.Query().Get("forHandle") == "AnnaCramling":
				w.Write([]byte(`{"items":[{"id":"UC-h","snippet":{"title":"Anna Cramling"},"contentDetails":{"relatedPlaylists":{"uploads":"uploads-h"}}}]}`))
			case r.URL.Query().Get("id") == "UC-v":
				w.Write([]byte(`{"items":[{"id":"UC-v","snippet":{"title":"Video channel"},"contentDetails":{"relatedPlaylists":{"uploads":"uploads-v"}}}]}`))
			case r.URL.Query().Get("forUsername") == "legacyuser":
				w.Write([]byte(`{"items":[{"id":"UC-u","snippet":{"title":"Legacy user"},"contentDetails":{"relatedPlaylists":{"uploads":"uploads-u"}}}]}`))
			default:
				w.Write([]byte(`{"items":[]}`))
			}
		case strings.HasSuffix(r.URL.Path, "/videos"):
			w.Write([]byte(`{"items":[{"id":"dQw4w9WgXcQ","snippet":{"channelId":"UC-v","title":"A video"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/playlistItems"):
			w.Write([]byte(`{"items":[{"contentDetails":{"videoId":"v1"},"snippet":{"title":"Episode","publishedAt":"` + time.Now().UTC().Format(time.RFC3339) + `"}}]}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
}

func TestResolveChannelRefPaths(t *testing.T) {
	var seen []string
	srv := resolveServer(t, &seen)
	defer srv.Close()
	client := YouTube{Key: "key", Client: srv.Client(), BaseURL: srv.URL}
	ctx := context.Background()

	tests := []struct {
		name    string
		ref     ChannelRef
		wantID  string
		wantRef string
	}{
		{"id", ChannelRef{Kind: ChannelRefID, Value: "UCXuqSBlHAE6Xw-yeJA0Tunw"}, "UCXuqSBlHAE6Xw-yeJA0Tunw", "id=UCXuqSBlHAE6Xw-yeJA0Tunw"},
		{"handle", ChannelRef{Kind: ChannelRefHandle, Value: "AnnaCramling"}, "UC-h", "forHandle=AnnaCramling"},
		{"username", ChannelRef{Kind: ChannelRefUsername, Value: "legacyuser"}, "UC-u", "forUsername=legacyuser"},
		{"name falls back to the username", ChannelRef{Kind: ChannelRefName, Value: "legacyuser"}, "UC-u", "forUsername=legacyuser"},
		{"video link", ChannelRef{Kind: ChannelRefVideo, Value: "dQw4w9WgXcQ"}, "UC-v", "id=UC-v"},
	}
	for _, tt := range tests {
		seen = nil
		channel, err := client.ResolveChannelRef(ctx, tt.ref)
		if err != nil {
			t.Fatalf("%s: resolve: %v", tt.name, err)
		}
		if channel.ID != tt.wantID {
			t.Errorf("%s: channel = %+v; want id %q", tt.name, channel, tt.wantID)
		}
		found := false
		for _, request := range seen {
			if strings.Contains(request, tt.wantRef) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: requests %q never used %q", tt.name, seen, tt.wantRef)
		}
	}
}

func TestResolveChannelRefNotFoundAndUnresolvable(t *testing.T) {
	var seen []string
	srv := resolveServer(t, &seen)
	defer srv.Close()
	client := YouTube{Key: "key", Client: srv.Client(), BaseURL: srv.URL}
	ctx := context.Background()

	if _, err := client.ResolveChannelRef(ctx, ChannelRef{Kind: ChannelRefHandle, Value: "nobody"}); !errors.Is(err, ErrChannelNotFound) {
		t.Fatalf("unknown handle error = %v; want ErrChannelNotFound", err)
	}
	seen = nil
	if _, err := client.ResolveChannelRef(ctx, ChannelRef{Kind: ChannelRefCustom, Value: "custom"}); !errors.Is(err, ErrChannelRefUnresolvable) {
		t.Fatalf("custom link error = %v; want ErrChannelRefUnresolvable", err)
	}
	if len(seen) != 0 {
		t.Fatalf("a custom link issued %d requests; it must issue none", len(seen))
	}
}

func TestResolveChannelRefQuota(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":{"message":"quota exceeded"}}`))
	}))
	defer srv.Close()
	client := YouTube{Key: "key", Client: srv.Client(), BaseURL: srv.URL}
	if _, err := client.ResolveChannelRef(context.Background(), ChannelRef{Kind: ChannelRefHandle, Value: "AnnaCramling"}); !errors.Is(err, ErrQuota) {
		t.Fatalf("quota error = %v; want ErrQuota", err)
	}
}

func TestFetchChannelReadsOneChannel(t *testing.T) {
	var seen []string
	srv := resolveServer(t, &seen)
	defer srv.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path+"?"+r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/channels"):
			if r.URL.Query().Get("id") != "UC-h" {
				t.Errorf("fetch asked for channels %q; want only the added one", r.URL.Query().Get("id"))
			}
			w.Write([]byte(`{"items":[{"id":"UC-h","snippet":{"title":"Anna Cramling","thumbnails":{"default":{"url":"http://avatar"}}},"contentDetails":{"relatedPlaylists":{"uploads":"uploads-h"}}}]}`))
		case strings.HasSuffix(r.URL.Path, "/playlistItems"):
			if r.URL.Query().Get("playlistId") != "uploads-h" {
				t.Errorf("fetch read playlist %q; want the added channel's uploads", r.URL.Query().Get("playlistId"))
			}
			w.Write([]byte(`{"items":[{"contentDetails":{"videoId":"v1"},"snippet":{"title":"Episode","publishedAt":"` + time.Now().UTC().Format(time.RFC3339) + `"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/videos"):
			w.Write([]byte(`{"items":[{"id":"v1","snippet":{"title":"Episode"},"contentDetails":{"duration":"PT10M"}}]}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client := YouTube{Key: "key", Client: server.Client(), BaseURL: server.URL}
	channel, videos, err := FetchChannel(context.Background(), client, Channel{ID: "UC-h", Title: "Old name"})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if channel.Title != "Anna Cramling" || channel.UploadedAt != "uploads-h" {
		t.Fatalf("channel = %+v; want the fetched metadata", channel)
	}
	if len(videos) != 1 || videos[0].ChannelID != "UC-h" || videos[0].ChannelTitle != "Anna Cramling" {
		t.Fatalf("videos = %+v; want the channel's one video with its metadata", videos)
	}
	if videos[0].Duration != "PT10M" {
		t.Fatalf("video duration = %q; want the hydrated duration", videos[0].Duration)
	}
	edited := 0
	for _, request := range seen {
		if strings.Contains(request, "id=UC") && !strings.Contains(request, "id=UC-h") {
			edited++
		}
	}
	if edited != 0 {
		t.Fatalf("fetch read another channel: %q", seen)
	}
}

func TestFetchChannelReportsNotFoundForAnEmptyChannel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items":[]}`))
	}))
	defer srv.Close()
	client := YouTube{Key: "key", Client: srv.Client(), BaseURL: srv.URL}
	if _, _, err := FetchChannel(context.Background(), client, Channel{ID: "UC-gone"}); !errors.Is(err, ErrChannelNotFound) {
		t.Fatalf("fetch of a missing channel = %v; want ErrChannelNotFound", err)
	}
}
