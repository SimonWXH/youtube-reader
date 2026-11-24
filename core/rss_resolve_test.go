package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResolveRSSSourceDirectRSS(t *testing.T) {
	rssContent := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
<channel>
	<title>Go Blog</title>
	<link>https://go.dev/blog</link>
	<description>The Go Programming Language Blog</description>
	<item>
		<title>Go 1.24 is released</title>
		<link>https://go.dev/blog/go1.24</link>
		<guid>https://go.dev/blog/go1.24</guid>
		<description>New features in Go 1.24</description>
	</item>
	<item>
		<title>Range over function types</title>
		<link>https://go.dev/blog/range-functions</link>
		<guid>https://go.dev/blog/range-functions</guid>
		<description>Iterators in Go</description>
	</item>
</channel>
</rss>`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
		_, _ = w.Write([]byte(rssContent))
	}))
	defer ts.Close()

	ctx := context.Background()
	res, err := ResolveRSSSource(ctx, ts.Client(), ts.URL)
	if err != nil {
		t.Fatalf("ResolveRSSSource failed: %v", err)
	}

	if res.Feed.Title != "Go Blog" {
		t.Errorf("feed title = %q, want Go Blog", res.Feed.Title)
	}
	if res.ArticleCount != 2 {
		t.Errorf("article count = %d, want 2", res.ArticleCount)
	}
	if res.Feed.FeedURL != CanonicalURL(ts.URL) {
		t.Errorf("feed url = %q, want %q", res.Feed.FeedURL, CanonicalURL(ts.URL))
	}
}

func TestResolveRSSSourceDirectAtom(t *testing.T) {
	atomContent := `<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
	<title>Rust Blog</title>
	<link href="https://blog.rust-lang.org/"/>
	<id>https://blog.rust-lang.org/</id>
	<updated>2026-01-01T00:00:00Z</updated>
	<entry>
		<title>Announcing Rust 1.85.0</title>
		<link href="https://blog.rust-lang.org/2026/01/01/Rust-1.85.0.html"/>
		<id>https://blog.rust-lang.org/2026/01/01/Rust-1.85.0.html</id>
		<updated>2026-01-01T00:00:00Z</updated>
		<summary>Rust 2024 Edition stabilized</summary>
	</entry>
</feed>`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
		_, _ = w.Write([]byte(atomContent))
	}))
	defer ts.Close()

	ctx := context.Background()
	res, err := ResolveRSSSource(ctx, ts.Client(), ts.URL)
	if err != nil {
		t.Fatalf("ResolveRSSSource failed: %v", err)
	}

	if res.Feed.Title != "Rust Blog" {
		t.Errorf("feed title = %q, want Rust Blog", res.Feed.Title)
	}
	if res.ArticleCount != 1 {
		t.Errorf("article count = %d, want 1", res.ArticleCount)
	}
}

func TestResolveRSSSourceHTMLAutoDiscovery(t *testing.T) {
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/", "/index.html":
			html := `<!DOCTYPE html>
<html>
<head>
	<title>Example Site</title>
	<link rel="alternate" type="application/rss+xml" title="Example RSS Feed" href="/feeds/news.xml">
</head>
<body><h1>Welcome</h1></body>
</html>`
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(html))
		case "/feeds/news.xml":
			rss := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
<channel>
	<title>Discovered News Feed</title>
	<link>` + ts.URL + `</link>
	<description>Discovered RSS</description>
	<item>
		<title>Headline 1</title>
		<link>` + ts.URL + `/post/1</link>
	</item>
</channel>
</rss>`
			w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
			_, _ = w.Write([]byte(rss))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	ctx := context.Background()
	// Pass the homepage URL
	res, err := ResolveRSSSource(ctx, ts.Client(), ts.URL)
	if err != nil {
		t.Fatalf("ResolveRSSSource auto-discovery failed: %v", err)
	}

	if res.Feed.Title != "Discovered News Feed" {
		t.Errorf("feed title = %q, want Discovered News Feed", res.Feed.Title)
	}
	expectedFeedURL := CanonicalURL(ts.URL + "/feeds/news.xml")
	if res.Feed.FeedURL != expectedFeedURL {
		t.Errorf("feed URL = %q, want %q", res.Feed.FeedURL, expectedFeedURL)
	}
	if res.ArticleCount != 1 {
		t.Errorf("article count = %d, want 1", res.ArticleCount)
	}
}

func TestResolveRSSSourceNoFeedFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `<!DOCTYPE html><html><head><title>No Feeds</title></head><body>No feeds here</body></html>`
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(html))
	}))
	defer ts.Close()

	ctx := context.Background()
	_, err := ResolveRSSSource(ctx, ts.Client(), ts.URL)
	if err == nil {
		t.Fatal("expected error when resolving page without feeds, got nil")
	}
}

func TestResolveRSSSource404(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer ts.Close()

	ctx := context.Background()
	_, err := ResolveRSSSource(ctx, ts.Client(), ts.URL+"/nonexistent")
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
}

func TestResolveRSSSourceEmptyURL(t *testing.T) {
	ctx := context.Background()
	_, err := ResolveRSSSource(ctx, nil, "   ")
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("expected empty URL error, got %v", err)
	}
}
