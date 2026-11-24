package core

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const sampleRSS20 = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/">
<channel>
  <title>The Go Blog</title>
  <link>https://go.dev/blog</link>
  <description>The Go Programming Language Blog</description>
  <item>
    <title>Go 1.24 is released</title>
    <link>https://go.dev/blog/go1.24</link>
    <guid>https://go.dev/blog/go1.24-unique-guid</guid>
    <pubDate>Tue, 11 Feb 2026 12:00:00 GMT</pubDate>
    <description>Go 1.24 introduces generic type aliases and tool directives.</description>
    <content:encoded><![CDATA[<p>Go 1.24 introduces generic type aliases and tool directives.</p><h2>Language changes</h2><p>Here are the key improvements in Go 1.24.</p>]]></content:encoded>
  </item>
</channel>
</rss>`

const sampleAtom = `<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Example Atom Feed</title>
  <link href="https://example.org/"/>
  <updated>2026-02-11T12:00:00Z</updated>
  <id>tag:example.org,2026:feed</id>
  <entry>
    <title>Atom Release Notes</title>
    <link href="https://example.org/atom-entry-1"/>
    <id>tag:example.org,2026:entry-1</id>
    <updated>2026-02-11T12:00:00Z</updated>
    <summary>Short atom summary</summary>
    <content type="html"><![CDATA[<p>Atom content body with details.</p>]]></content>
  </entry>
</feed>`

const sampleJSONFeed = `{
  "version": "https://jsonfeed.org/version/1.1",
  "title": "JSON Feed News",
  "home_page_url": "https://example.com/",
  "feed_url": "https://example.com/feed.json",
  "items": [
    {
      "id": "json-post-1",
      "url": "https://example.com/json-post-1",
      "title": "JSON Feed Article",
      "content_html": "<p>Hello from JSON Feed format.</p>",
      "date_published": "2026-02-11T12:00:00Z"
    }
  ]
}`

func TestFetchRSS20(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	feed := RSSFeed{
		ID:        "feed:goblog",
		FeedURL:   "https://go.dev/blog/feed.atom",
		UpdatedAt: time.Now().UnixMilli(),
	}
	if err := store.SaveRSSFeed(ctx, feed); err != nil {
		t.Fatal(err)
	}

	client := &http.Client{
		Transport: &mockTransport{
			roundTripFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header: http.Header{
						"ETag":          []string{`"etag-1234"`},
						"Last-Modified": []string{"Tue, 11 Feb 2026 12:00:00 GMT"},
						"Content-Type":  []string{"application/rss+xml"},
					},
					Body: io.NopCloser(strings.NewReader(sampleRSS20)),
				}, nil
			},
		},
	}

	fetcher := NewRSSFetcher(store, WithHTTPClient(client), WithExtractFullText(false))
	res, err := fetcher.FetchFeed(ctx, feed)
	if err != nil {
		t.Fatalf("FetchFeed RSS 2.0 failed: %v", err)
	}

	if res.ItemsFetched != 1 || res.ItemsSaved != 1 {
		t.Fatalf("expected 1 item fetched and saved, got fetched=%d, saved=%d", res.ItemsFetched, res.ItemsSaved)
	}

	// Verify feed metadata updated
	updatedFeed, _ := store.GetRSSFeed(ctx, feed.ID)
	if updatedFeed.Title != "The Go Blog" || updatedFeed.ETag != `"etag-1234"` {
		t.Errorf("feed metadata not updated: %+v", updatedFeed)
	}

	// Verify article saved
	articles, _ := store.ListRSSArticles(ctx, RSSArticleFilter{FeedID: feed.ID})
	if len(articles) != 1 {
		t.Fatalf("expected 1 article in store, got %d", len(articles))
	}
	art := articles[0]
	if art.Title != "Go 1.24 is released" || art.GUID != "https://go.dev/blog/go1.24-unique-guid" {
		t.Errorf("article fields unexpected: %+v", art)
	}
	if len(art.Blocks) == 0 {
		t.Errorf("article blocks should be populated from feed content")
	}
}

const sampleRSSUnparseableDate = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
<channel>
  <title>Assets Blog</title>
  <link>https://example.com</link>
  <description>Blog about assets</description>
  <item>
    <title>Post with relative image</title>
    <link>https://example.com/post/1</link>
    <guid>post-relative-assets</guid>
    <pubDate>not a real date</pubDate>
    <description><![CDATA[<p>Body text here.</p><img src="/img/cover.png" alt="cover">]]></description>
  </item>
</channel>
</rss>`

func TestFetchNormalizesPublishedAtAndResolvesAssetURLs(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	feed := RSSFeed{
		ID:        "feed:assets",
		FeedURL:   "https://example.com/feed.xml",
		UpdatedAt: time.Now().UnixMilli(),
	}
	if err := store.SaveRSSFeed(ctx, feed); err != nil {
		t.Fatal(err)
	}

	client := &http.Client{
		Transport: &mockTransport{
			roundTripFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{"Content-Type": []string{"application/rss+xml"}},
					Body:       io.NopCloser(strings.NewReader(sampleRSSUnparseableDate)),
				}, nil
			},
		},
	}

	before := time.Now().UTC()
	fetcher := NewRSSFetcher(store, WithHTTPClient(client))
	if _, err := fetcher.FetchFeed(ctx, feed); err != nil {
		t.Fatalf("FetchFeed failed: %v", err)
	}

	articles, _ := store.ListRSSArticles(ctx, RSSArticleFilter{FeedID: feed.ID})
	if len(articles) != 1 {
		t.Fatalf("expected 1 article, got %d", len(articles))
	}
	art := articles[0]

	// published_at is persisted as RFC3339 so retention and ordering compare chronologically.
	published, err := time.Parse(time.RFC3339, art.PublishedAt)
	if err != nil {
		t.Fatalf("published_at must always be RFC3339, got %q", art.PublishedAt)
	}
	if published.Before(before.Add(-time.Minute)) || published.After(before.Add(time.Hour)) {
		t.Fatalf("unparseable feed date must fall back to fetch time, got %q", art.PublishedAt)
	}

	const wantImage = "https://example.com/img/cover.png"
	if art.HeroImage != wantImage {
		t.Fatalf("hero image must be absolute, got %q; want %q", art.HeroImage, wantImage)
	}
	var imageBlockURL string
	for _, b := range art.Blocks {
		if b.Type == BlockImage {
			imageBlockURL = b.URL
		}
	}
	if imageBlockURL != wantImage {
		t.Fatalf("image block URL must be absolute, got %q; want %q", imageBlockURL, wantImage)
	}
}

func TestFetchFeedDefaultDoesNotRequestArticlePages(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	feed := RSSFeed{
		ID:        "feed:nofulltext",
		FeedURL:   "https://go.dev/blog/feed.atom",
		UpdatedAt: time.Now().UnixMilli(),
	}
	if err := store.SaveRSSFeed(ctx, feed); err != nil {
		t.Fatal(err)
	}

	var requested []string
	client := &http.Client{
		Transport: &mockTransport{
			roundTripFunc: func(req *http.Request) (*http.Response, error) {
				requested = append(requested, req.URL.String())
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{"Content-Type": []string{"application/rss+xml"}},
					Body:       io.NopCloser(strings.NewReader(sampleRSS20)),
				}, nil
			},
		},
	}

	fetcher := NewRSSFetcher(store, WithHTTPClient(client))
	res, err := fetcher.FetchFeed(ctx, feed)
	if err != nil {
		t.Fatalf("FetchFeed failed: %v", err)
	}
	if res.ItemsSaved != 1 {
		t.Fatalf("expected 1 item saved, got %d", res.ItemsSaved)
	}
	if len(requested) != 1 || requested[0] != feed.FeedURL {
		t.Fatalf("feed refresh must only request the feed itself, requested %v", requested)
	}

	articles, _ := store.ListRSSArticles(ctx, RSSArticleFilter{FeedID: feed.ID})
	if len(articles) != 1 {
		t.Fatalf("expected 1 article, got %d", len(articles))
	}
	if articles[0].IsFullText {
		t.Fatalf("default feed refresh must not mark articles as extracted full text")
	}
	if !strings.Contains(articles[0].ContentHTML, "Go 1.24 introduces generic type aliases") {
		t.Fatalf("article must fall back to feed-provided content, got %q", articles[0].ContentHTML)
	}
}

func TestFetchAtom(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	feed := RSSFeed{
		ID:        "feed:atom",
		FeedURL:   "https://example.org/atom.xml",
		UpdatedAt: time.Now().UnixMilli(),
	}
	_ = store.SaveRSSFeed(ctx, feed)

	client := &http.Client{
		Transport: &mockTransport{
			roundTripFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{"Content-Type": []string{"application/atom+xml"}},
					Body:       io.NopCloser(strings.NewReader(sampleAtom)),
				}, nil
			},
		},
	}

	fetcher := NewRSSFetcher(store, WithHTTPClient(client), WithExtractFullText(false))
	res, err := fetcher.FetchFeed(ctx, feed)
	if err != nil {
		t.Fatalf("FetchFeed Atom failed: %v", err)
	}
	if res.ItemsSaved != 1 {
		t.Fatalf("expected 1 item saved, got %d", res.ItemsSaved)
	}

	articles, _ := store.ListRSSArticles(ctx, RSSArticleFilter{FeedID: feed.ID})
	if len(articles) != 1 || articles[0].Title != "Atom Release Notes" {
		t.Fatalf("unexpected article: %+v", articles)
	}
}

func TestFetchJSONFeed(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	feed := RSSFeed{
		ID:        "feed:json",
		FeedURL:   "https://example.com/feed.json",
		UpdatedAt: time.Now().UnixMilli(),
	}
	_ = store.SaveRSSFeed(ctx, feed)

	client := &http.Client{
		Transport: &mockTransport{
			roundTripFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(sampleJSONFeed)),
				}, nil
			},
		},
	}

	fetcher := NewRSSFetcher(store, WithHTTPClient(client), WithExtractFullText(false))
	res, err := fetcher.FetchFeed(ctx, feed)
	if err != nil {
		t.Fatalf("FetchFeed JSON Feed failed: %v", err)
	}
	if res.ItemsSaved != 1 {
		t.Fatalf("expected 1 item saved, got %d", res.ItemsSaved)
	}

	articles, _ := store.ListRSSArticles(ctx, RSSArticleFilter{FeedID: feed.ID})
	if len(articles) != 1 || articles[0].Title != "JSON Feed Article" {
		t.Fatalf("unexpected article: %+v", articles)
	}
}

func TestFetch304NotModifiedNoParsing(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	feed := RSSFeed{
		ID:           "feed:conditional",
		FeedURL:      "https://example.com/conditional.xml",
		Title:        "Cached Feed",
		ETag:         `"etag-cache-99"`,
		LastModified: "Wed, 21 Oct 2025 07:28:00 GMT",
		FailureCount: 2,
		LastError:    "previous error",
		UpdatedAt:    time.Now().UnixMilli(),
	}
	_ = store.SaveRSSFeed(ctx, feed)

	requestChecked := false
	client := &http.Client{
		Transport: &mockTransport{
			roundTripFunc: func(req *http.Request) (*http.Response, error) {
				// Verify conditional headers are passed accurately
				if req.Header.Get("If-None-Match") != `"etag-cache-99"` {
					t.Errorf("If-None-Match missing or wrong: %q", req.Header.Get("If-None-Match"))
				}
				if req.Header.Get("If-Modified-Since") != "Wed, 21 Oct 2025 07:28:00 GMT" {
					t.Errorf("If-Modified-Since missing or wrong: %q", req.Header.Get("If-Modified-Since"))
				}
				requestChecked = true
				return &http.Response{
					StatusCode: http.StatusNotModified,
					Status:     "304 Not Modified",
					Header:     http.Header{},
					Body:       io.NopCloser(strings.NewReader("")),
				}, nil
			},
		},
	}

	fetcher := NewRSSFetcher(store, WithHTTPClient(client))
	res, err := fetcher.FetchFeed(ctx, feed)
	if err != nil {
		t.Fatalf("304 should not return error, got: %v", err)
	}

	if !requestChecked {
		t.Fatalf("request was not dispatched to transport")
	}
	if !res.NotModified {
		t.Fatalf("res.NotModified must be true on HTTP 304")
	}
	if res.ItemsFetched != 0 || res.ItemsSaved != 0 {
		t.Fatalf("304 must perform ZERO re-parsing, got fetched=%d, saved=%d", res.ItemsFetched, res.ItemsSaved)
	}

	// Verify store was updated: failureCount cleared, lastFetchedAt updated
	updatedFeed, _ := store.GetRSSFeed(ctx, feed.ID)
	if updatedFeed.FailureCount != 0 || updatedFeed.LastError != "" {
		t.Errorf("304 should clear failure count and error, got count=%d, err=%q", updatedFeed.FailureCount, updatedFeed.LastError)
	}
	if updatedFeed.LastFetchedAt == 0 {
		t.Errorf("lastFetchedAt should be updated on 304")
	}
}

func TestFetchFailureRecordsError(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	feed := RSSFeed{
		ID:        "feed:failing",
		FeedURL:   "https://example.com/fail.xml",
		UpdatedAt: time.Now().UnixMilli(),
	}
	_ = store.SaveRSSFeed(ctx, feed)

	// Mock returning 503
	client := &http.Client{
		Transport: &mockTransport{
			roundTripFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusServiceUnavailable,
					Status:     "503 Service Unavailable",
					Body:       io.NopCloser(strings.NewReader("Unavailable")),
				}, nil
			},
		},
	}

	fetcher := NewRSSFetcher(store, WithHTTPClient(client), WithMaxRetries(0))
	res, err := fetcher.FetchFeed(ctx, feed)
	if err == nil {
		t.Fatalf("expected error on 503")
	}
	_ = res

	// Check visible error status recorded on feed
	storedFeed, _ := store.GetRSSFeed(ctx, feed.ID)
	if storedFeed.FailureCount != 1 {
		t.Fatalf("expected FailureCount=1, got %d", storedFeed.FailureCount)
	}
	if !strings.Contains(storedFeed.LastError, "503") {
		t.Fatalf("expected LastError to mention 503, got %q", storedFeed.LastError)
	}
	if storedFeed.LastErrorAt == 0 {
		t.Fatalf("expected LastErrorAt to be recorded")
	}

	// Second failure increments count
	_, _ = fetcher.FetchFeed(ctx, *storedFeed)
	secondFeed, _ := store.GetRSSFeed(ctx, feed.ID)
	if secondFeed.FailureCount != 2 {
		t.Fatalf("expected FailureCount=2, got %d", secondFeed.FailureCount)
	}
}

func TestFetchDeduplicationIdempotency(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	feed := RSSFeed{
		ID:        "feed:dedup",
		FeedURL:   "https://go.dev/blog/feed.atom",
		UpdatedAt: time.Now().UnixMilli(),
	}
	_ = store.SaveRSSFeed(ctx, feed)

	client := &http.Client{
		Transport: &mockTransport{
			roundTripFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{"Content-Type": []string{"application/rss+xml"}},
					Body:       io.NopCloser(strings.NewReader(sampleRSS20)),
				}, nil
			},
		},
	}

	fetcher := NewRSSFetcher(store, WithHTTPClient(client), WithExtractFullText(false))

	// First fetch
	_, err := fetcher.FetchFeed(ctx, feed)
	if err != nil {
		t.Fatal(err)
	}

	// Second fetch of identical feed
	_, err = fetcher.FetchFeed(ctx, feed)
	if err != nil {
		t.Fatal(err)
	}

	// Verify only 1 article exists in DB (idempotent, no duplicate entries)
	articles, _ := store.ListRSSArticles(ctx, RSSArticleFilter{FeedID: feed.ID})
	if len(articles) != 1 {
		t.Fatalf("expected 1 unique article in DB, got %d", len(articles))
	}
}

func TestFetchAllFeedsConcurrency(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	for i := 1; i <= 3; i++ {
		feed := RSSFeed{
			ID:        FeedID(CanonicalURL(strings.Repeat("a", i))),
			FeedURL:   "https://example.com/feed" + string(rune('0'+i)),
			Title:     "Feed " + string(rune('0'+i)),
			UpdatedAt: time.Now().UnixMilli(),
		}
		_ = store.SaveRSSFeed(ctx, feed)
	}

	client := &http.Client{
		Transport: &mockTransport{
			roundTripFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(sampleJSONFeed)),
				}, nil
			},
		},
	}

	fetcher := NewRSSFetcher(store, WithHTTPClient(client), WithConcurrency(2), WithExtractFullText(false))
	results, err := fetcher.FetchAllFeeds(ctx, true)
	if err != nil {
		t.Fatalf("FetchAllFeeds failed: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
}

func TestDiscoverFeedURL(t *testing.T) {
	ctx := context.Background()

	siteHTML := `<!DOCTYPE html>
<html>
<head>
	<link rel="alternate" type="application/rss+xml" title="Blog RSS" href="/rss.xml" />
</head>
<body><h1>My Blog</h1></body>
</html>`

	client := &http.Client{
		Transport: &mockTransport{
			roundTripFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
					Body:       io.NopCloser(strings.NewReader(siteHTML)),
				}, nil
			},
		},
	}

	discovered, err := DiscoverFeedURL(ctx, client, "https://example.com/blog/")
	if err != nil {
		t.Fatalf("DiscoverFeedURL failed: %v", err)
	}
	if discovered != "https://example.com/rss.xml" {
		t.Fatalf("expected discovered URL 'https://example.com/rss.xml', got %q", discovered)
	}

	discoveredSchemeless, err := DiscoverFeedURL(ctx, client, "example.com/blog/")
	if err != nil {
		t.Fatalf("DiscoverFeedURL with schemeless input failed: %v", err)
	}
	if discoveredSchemeless != "https://example.com/rss.xml" {
		t.Fatalf("expected discovered URL 'https://example.com/rss.xml' for schemeless input, got %q", discoveredSchemeless)
	}
}

func TestDiscoverFeedURLResolvesRelativeFeedLink(t *testing.T) {
	ctx := context.Background()

	siteHTML := `<!DOCTYPE html>
<html>
<head>
	<link rel="alternate" type="application/atom+xml" href="rss.xml" />
</head>
<body><h1>My Blog</h1></body>
</html>`

	var requested []string
	client := &http.Client{
		Transport: &mockTransport{
			roundTripFunc: func(req *http.Request) (*http.Response, error) {
				requested = append(requested, req.URL.String())
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
					Body:       io.NopCloser(strings.NewReader(siteHTML)),
				}, nil
			},
		},
	}

	discovered, err := DiscoverFeedURL(ctx, client, "https://example.com/blog/")
	if err != nil {
		t.Fatalf("DiscoverFeedURL failed: %v", err)
	}
	if discovered != "https://example.com/blog/rss.xml" {
		t.Fatalf("relative feed link must resolve against the input directory, got %q", discovered)
	}
	if len(requested) != 1 || requested[0] != "https://example.com/blog/" {
		t.Fatalf("expected a single probe of the input directory URL, got %v", requested)
	}
}

func TestDiscoverFeedURLRejectsFailedProbes(t *testing.T) {
	ctx := context.Background()

	notFound := &http.Client{Transport: &mockTransport{
		roundTripFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Status:     "404 Not Found",
				Header:     http.Header{"Content-Type": []string{"text/html"}},
				Body:       io.NopCloser(strings.NewReader("<html><body>not found</body></html>")),
			}, nil
		},
	}}
	if found, err := DiscoverFeedURL(ctx, notFound, "https://example.com/blog/"); err == nil {
		t.Fatalf("404 response must not be reported as a discovered feed, got %q", found)
	}

	noFeed := &http.Client{Transport: &mockTransport{
		roundTripFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
				Body:       io.NopCloser(strings.NewReader("<html><head><title>Plain page</title></head><body>No feed here</body></html>")),
			}, nil
		},
	}}
	if found, err := DiscoverFeedURL(ctx, noFeed, "https://example.com/blog/"); err == nil {
		t.Fatalf("a page without a declared feed must return an error, got %q", found)
	}
}

const sampleRSSRelativeLinks = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
<channel>
  <title>Relative Links Blog</title>
  <link>https://example.com/blog/</link>
  <description>Blog with relative links</description>
  <item>
    <title>Post with relative link</title>
    <link>/blog/post/1</link>
    <pubDate>Tue, 11 Feb 2026 12:00:00 GMT</pubDate>
    <description><![CDATA[<p>Body text.</p><img src="cover.png" alt="cover">]]></description>
  </item>
</channel>
</rss>`

func TestFetchResolvesRelativeArticleLinks(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	feed := RSSFeed{
		ID:        "feed:relativelinks",
		FeedURL:   "https://example.com/feed.xml",
		UpdatedAt: time.Now().UnixMilli(),
	}
	if err := store.SaveRSSFeed(ctx, feed); err != nil {
		t.Fatal(err)
	}

	client := &http.Client{
		Transport: &mockTransport{
			roundTripFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{"Content-Type": []string{"application/rss+xml"}},
					Body:       io.NopCloser(strings.NewReader(sampleRSSRelativeLinks)),
				}, nil
			},
		},
	}

	fetcher := NewRSSFetcher(store, WithHTTPClient(client))
	if _, err := fetcher.FetchFeed(ctx, feed); err != nil {
		t.Fatalf("FetchFeed failed: %v", err)
	}

	articles, _ := store.ListRSSArticles(ctx, RSSArticleFilter{FeedID: feed.ID})
	if len(articles) != 1 {
		t.Fatalf("expected 1 article, got %d", len(articles))
	}
	art := articles[0]

	const wantLink = "https://example.com/blog/post/1"
	if art.Link != wantLink {
		t.Fatalf("relative article link must resolve against the site URL, got %q; want %q", art.Link, wantLink)
	}
	if wantID := ArticleID(feed.ID, wantLink); art.ID != wantID {
		t.Fatalf("article identity must be derived from the resolved link: got %q; want %q", art.ID, wantID)
	}

	const wantImage = "https://example.com/blog/post/cover.png"
	if art.HeroImage != wantImage {
		t.Fatalf("hero image must resolve against the resolved article link, got %q; want %q", art.HeroImage, wantImage)
	}
	var imageBlockURL string
	for _, b := range art.Blocks {
		if b.Type == BlockImage {
			imageBlockURL = b.URL
		}
	}
	if imageBlockURL != wantImage {
		t.Fatalf("image block URL must resolve against the resolved article link, got %q; want %q", imageBlockURL, wantImage)
	}
}
