package core

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/mmcdole/gofeed"
)

const (
	DefaultRSSRefreshIntervalMins  = 30
	DefaultRSSConcurrency          = 4
	DefaultRSSTimeout              = 25 * time.Second
	DefaultRSSMaxRetries           = 2
	DefaultRSSRetentionMaxArticles = 500
	DefaultRSSRetentionMaxAge      = 30 * 24 * time.Hour
	DefaultRSSUserAgent            = "YouTubeReader/1.0 (+https://github.com/SimonWXH/youtube-reader)"
	MaxFeedBodyBytes               = 10 * 1024 * 1024 // 10MB
)

// FetchFeedResult details the outcome of fetching a single RSS feed.
type FetchFeedResult struct {
	FeedID       string
	FeedURL      string
	Title        string
	NotModified  bool
	ItemsFetched int
	ItemsSaved   int
	Error        error
}

// RSSFetcherOption configures the RSSFetcher.
type RSSFetcherOption func(*RSSFetcher)

// WithHTTPClient sets the HTTP client to use for network operations.
func WithHTTPClient(client *http.Client) RSSFetcherOption {
	return func(f *RSSFetcher) {
		if client != nil {
			f.client = client
		}
	}
}

// WithConcurrency sets maximum parallel feed fetching workers.
func WithConcurrency(c int) RSSFetcherOption {
	return func(f *RSSFetcher) {
		if c > 0 {
			f.concurrency = c
		}
	}
}

// WithTimeout sets the request timeout for fetching feeds and articles.
func WithTimeout(d time.Duration) RSSFetcherOption {
	return func(f *RSSFetcher) {
		if d > 0 {
			f.timeout = d
		}
	}
}

// WithMaxRetries sets the maximum retry attempts on transient network errors.
func WithMaxRetries(r int) RSSFetcherOption {
	return func(f *RSSFetcher) {
		if r >= 0 {
			f.maxRetries = r
		}
	}
}

// WithUserAgent configures the User-Agent header for HTTP requests.
func WithUserAgent(ua string) RSSFetcherOption {
	return func(f *RSSFetcher) {
		if ua != "" {
			f.userAgent = ua
		}
	}
}

// WithExtractFullText enables or disables automatic full-text extraction.
func WithExtractFullText(extract bool) RSSFetcherOption {
	return func(f *RSSFetcher) {
		f.extractFullText = extract
	}
}

// WithRetention configures retention cleanup thresholds.
func WithRetention(maxArticles int, maxAge time.Duration) RSSFetcherOption {
	return func(f *RSSFetcher) {
		if maxArticles > 0 {
			f.retentionMaxArticles = maxArticles
		}
		if maxAge > 0 {
			f.retentionMaxAge = maxAge
		}
	}
}

// RSSFetcher coordinates fetching RSS/Atom/JSON feeds, parsing entries,
// conditional requests (ETag/Last-Modified 304), optional full-text extraction, and storage.
type RSSFetcher struct {
	store                *Store
	client               *http.Client
	concurrency          int
	timeout              time.Duration
	maxRetries           int
	userAgent            string
	extractFullText      bool
	retentionMaxArticles int
	retentionMaxAge      time.Duration
}

// NewRSSFetcher creates and initializes a new RSSFetcher.
func NewRSSFetcher(store *Store, opts ...RSSFetcherOption) *RSSFetcher {
	f := &RSSFetcher{
		store:                store,
		client:               &http.Client{Timeout: DefaultRSSTimeout},
		concurrency:          DefaultRSSConcurrency,
		timeout:              DefaultRSSTimeout,
		maxRetries:           DefaultRSSMaxRetries,
		userAgent:            DefaultRSSUserAgent,
		extractFullText:      false,
		retentionMaxArticles: DefaultRSSRetentionMaxArticles,
		retentionMaxAge:      DefaultRSSRetentionMaxAge,
	}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

// FetchFeed fetches a single RSS/Atom/JSON feed with conditional request headers,
// extracts articles, saves them to the store (preserving read states), and applies retention.
func (f *RSSFetcher) FetchFeed(ctx context.Context, feed RSSFeed) (*FetchFeedResult, error) {
	if feed.FeedURL == "" {
		return nil, errors.New("empty feed URL")
	}

	res := &FetchFeedResult{
		FeedID:  feed.ID,
		FeedURL: feed.FeedURL,
		Title:   feed.DisplayName(),
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feed.FeedURL, nil)
	if err != nil {
		f.recordFeedFailure(ctx, &feed, err.Error())
		res.Error = err
		return res, err
	}

	req.Header.Set("User-Agent", f.userAgent)
	req.Header.Set("Accept", "application/atom+xml, application/rss+xml, application/json, text/xml, application/xml, */*")

	// Conditional request headers (If-None-Match / If-Modified-Since)
	if feed.ETag != "" {
		req.Header.Set("If-None-Match", feed.ETag)
	}
	if feed.LastModified != "" {
		req.Header.Set("If-Modified-Since", feed.LastModified)
	}

	// Execute with retries
	var resp *http.Response
	var reqErr error
	for attempt := 0; attempt <= f.maxRetries; attempt++ {
		resp, reqErr = f.client.Do(req)
		if reqErr == nil {
			break
		}
		if attempt < f.maxRetries {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(50*(1<<attempt)) * time.Millisecond):
			}
		}
	}

	if reqErr != nil {
		f.recordFeedFailure(ctx, &feed, reqErr.Error())
		res.Error = reqErr
		return res, reqErr
	}
	defer resp.Body.Close()

	now := time.Now().UnixMilli()

	// ==================================================================================
	// 304 NOT MODIFIED: Conditional request hit - ZERO re-parsing, ZERO data written
	// ==================================================================================
	if resp.StatusCode == http.StatusNotModified {
		feed.FailureCount = 0
		feed.LastError = ""
		feed.LastFetchedAt = now
		_ = f.store.SaveRSSFeed(ctx, feed)
		res.NotModified = true
		return res, nil
	}

	// Non-200 HTTP status
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		statusErr := fmt.Sprintf("HTTP %s", resp.Status)
		f.recordFeedFailure(ctx, &feed, statusErr)
		err = errors.New(statusErr)
		res.Error = err
		return res, err
	}

	// Parse feed stream using gofeed
	limitReader := io.LimitReader(resp.Body, MaxFeedBodyBytes)
	parser := gofeed.NewParser()
	parsed, parseErr := parser.Parse(limitReader)
	if parseErr != nil {
		f.recordFeedFailure(ctx, &feed, fmt.Sprintf("parse error: %v", parseErr))
		res.Error = parseErr
		return res, parseErr
	}

	// Update feed metadata
	if newETag := getHeader(resp.Header, "ETag"); newETag != "" {
		feed.ETag = newETag
	}
	if newLastMod := getHeader(resp.Header, "Last-Modified"); newLastMod != "" {
		feed.LastModified = newLastMod
	}
	if feed.Title == "" && parsed.Title != "" {
		feed.Title = parsed.Title
	}
	if feed.SiteURL == "" && parsed.Link != "" {
		feed.SiteURL = parsed.Link
	}
	feedBase := feed.FeedURL
	if feed.SiteURL != "" {
		feed.SiteURL = resolveAssetURL(feed.FeedURL, feed.SiteURL)
		feedBase = feed.SiteURL
	}
	if feed.AvatarURL == "" && parsed.Image != nil && parsed.Image.URL != "" {
		feed.AvatarURL = resolveAssetURL(feedBase, parsed.Image.URL)
	}
	feed.FailureCount = 0
	feed.LastError = ""
	feed.LastFetchedAt = now
	feed.UpdatedAt = now
	_ = f.store.SaveRSSFeed(ctx, feed)

	res.Title = feed.DisplayName()
	res.ItemsFetched = len(parsed.Items)

	// Transform parsed items into RSSArticle models
	articles := make([]RSSArticle, 0, len(parsed.Items))
	for _, item := range parsed.Items {
		articleLink := resolveAssetURL(feedBase, item.Link)
		articleBase := articleLink
		if articleBase == "" {
			articleBase = feedBase
		}
		dedupKey := ArticleDeduplicationKey(item.GUID, articleLink, item.Title)
		artID := ArticleID(feed.ID, dedupKey)

		author := ""
		if item.Author != nil && item.Author.Name != "" {
			author = item.Author.Name
		} else if len(item.Authors) > 0 && item.Authors[0].Name != "" {
			author = item.Authors[0].Name
		}

		publishedAt := time.UnixMilli(now).UTC().Format(time.RFC3339)
		if item.PublishedParsed != nil {
			publishedAt = item.PublishedParsed.UTC().Format(time.RFC3339)
		} else if item.UpdatedParsed != nil {
			publishedAt = item.UpdatedParsed.UTC().Format(time.RFC3339)
		}

		feedContent := item.Content
		if feedContent == "" {
			feedContent = item.Description
		}
		snippet := CleanSnippet(item.Description, item.Content, 200)

		heroImage := ""
		if item.Image != nil && item.Image.URL != "" {
			heroImage = item.Image.URL
		}

		var contentHTML string
		var blocks []ArticleBlock
		var isFullText bool

		if f.extractFullText && articleLink != "" {
			extractedHTML, parsedBlocks, fullTextOK, extractedHero, _ := ExtractFullText(ctx, f.client, articleLink, feedContent, snippet)
			contentHTML = extractedHTML
			blocks = parsedBlocks
			isFullText = fullTextOK
			if heroImage == "" && extractedHero != "" {
				heroImage = extractedHero
			}
		} else {
			contentHTML = feedContent
			if strings.TrimSpace(contentHTML) == "" {
				contentHTML = "<p>" + html.EscapeString(snippet) + "</p>"
			}
			blocks = ParseHTMLBlocks(contentHTML)
			if heroImage == "" {
				heroImage = ExtractHeroImage(contentHTML)
			}
			isFullText = false
		}

		heroImage = resolveAssetURL(articleBase, heroImage)
		for i := range blocks {
			if blocks[i].Type == BlockImage {
				blocks[i].URL = resolveAssetURL(articleBase, blocks[i].URL)
			}
		}

		articles = append(articles, RSSArticle{
			ID:          artID,
			FeedID:      feed.ID,
			Title:       item.Title,
			Link:        articleLink,
			GUID:        item.GUID,
			Author:      author,
			PublishedAt: publishedAt,
			Snippet:     snippet,
			ContentHTML: contentHTML,
			IsFullText:  isFullText,
			HeroImage:   heroImage,
			Blocks:      blocks,
			Read:        false, // Will not overwrite if already read in DB
			Starred:     false, // Will not overwrite if already starred in DB
			UpdatedAt:   now,
		})
	}

	// Persist articles with idempotency preserving read and starred states
	if err := f.store.SaveRSSArticles(ctx, articles); err != nil {
		res.Error = err
		return res, err
	}
	res.ItemsSaved = len(articles)

	// Clean up old read articles according to retention limits
	_, _ = f.store.CleanOldRSSArticles(ctx, feed.ID, f.retentionMaxArticles, f.retentionMaxAge, time.Now())

	return res, nil
}

// FetchAllFeeds fetches all subscribed feeds with bounded concurrency.
// If force is false, feeds not yet due for refresh are skipped.
func (f *RSSFetcher) FetchAllFeeds(ctx context.Context, force bool) ([]FetchFeedResult, error) {
	feeds, err := f.store.ListRSSFeeds(ctx, false)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	var toFetch []RSSFeed
	for _, feed := range feeds {
		if force || f.isDueForFetch(feed, now) {
			toFetch = append(toFetch, feed)
		}
	}

	if len(toFetch) == 0 {
		return nil, nil
	}

	results := make([]FetchFeedResult, len(toFetch))
	sem := make(chan struct{}, f.concurrency)
	var wg sync.WaitGroup

	for i, feed := range toFetch {
		wg.Add(1)
		go func(idx int, target RSSFeed) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			res, _ := f.FetchFeed(ctx, target)
			if res != nil {
				results[idx] = *res
			}
		}(i, feed)
	}

	wg.Wait()
	return results, nil
}

func (f *RSSFetcher) isDueForFetch(feed RSSFeed, now time.Time) bool {
	if feed.LastFetchedAt == 0 {
		return true
	}
	interval := feed.RefreshIntervalMins
	if interval <= 0 {
		interval = DefaultRSSRefreshIntervalMins
	}
	return now.Sub(time.UnixMilli(feed.LastFetchedAt)) >= time.Duration(interval)*time.Minute
}

func (f *RSSFetcher) recordFeedFailure(ctx context.Context, feed *RSSFeed, errMsg string) {
	now := time.Now().UnixMilli()
	feed.FailureCount++
	feed.LastError = errMsg
	feed.LastErrorAt = now
	feed.LastFetchedAt = now
	_ = f.store.SaveRSSFeed(ctx, *feed)
}

func getHeader(h http.Header, key string) string {
	if v := h.Get(key); v != "" {
		return v
	}
	for k, vals := range h {
		if strings.EqualFold(k, key) && len(vals) > 0 {
			return vals[0]
		}
	}
	return ""
}

// DiscoverFeedURL probes a website's HTML for RSS/Atom/JSON Feed links.
// It returns the canonical feed URL, or an error when the probe fails or the
// page declares no feed.
func DiscoverFeedURL(ctx context.Context, client *http.Client, targetURL string) (string, error) {
	canonical := CanonicalURL(targetURL)
	if canonical == "" {
		return "", errors.New("empty URL")
	}

	requestURL := canonical
	baseURL, err := url.Parse(canonical)
	if err != nil {
		return "", err
	}
	if input, inputErr := url.Parse(strings.TrimSpace(targetURL)); inputErr == nil && strings.HasSuffix(input.Path, "/") {
		baseURL.Path += "/"
		requestURL = baseURL.String()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", DefaultRSSUserAgent)

	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("feed discovery failed for %s: HTTP %s", requestURL, resp.Status)
	}

	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(contentType, "xml") || strings.Contains(contentType, "json") {
		return canonical, nil
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", fmt.Errorf("feed discovery failed for %s: %w", requestURL, err)
	}

	var foundFeed string
	doc.Find(`link[rel="alternate"]`).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		t, _ := s.Attr("type")
		t = strings.ToLower(t)
		if strings.Contains(t, "rss") || strings.Contains(t, "atom") || strings.Contains(t, "json") {
			href, exists := s.Attr("href")
			if exists && href != "" {
				parsedHref, err := url.Parse(href)
				if err == nil {
					foundFeed = baseURL.ResolveReference(parsedHref).String()
					return false // stop searching
				}
			}
		}
		return true
	})

	if foundFeed != "" {
		return CanonicalURL(foundFeed), nil
	}
	return "", fmt.Errorf("no RSS/Atom/JSON feed found at %s", requestURL)
}
