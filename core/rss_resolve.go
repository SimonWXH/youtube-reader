package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
)

// ResolvedRSSSource represents the result of discovering and parsing an RSS/Atom/JSON feed.
type ResolvedRSSSource struct {
	Feed         RSSFeed
	ArticleCount int
}

// ResolveRSSSource resolves a feed URL with dual-mode discovery:
// 1. Direct RSS/Atom/JSON feed URL.
// 2. Website homepage URL via HTML <link rel="alternate" type="application/(rss|atom)+xml"> auto-discovery.
//
// It returns the constructed RSSFeed model (with stable ID, title, site URL, and avatar URL)
// along with the count of items currently available in the feed.
func ResolveRSSSource(ctx context.Context, client *http.Client, inputURL string) (*ResolvedRSSSource, error) {
	canonical := CanonicalURL(inputURL)
	if canonical == "" {
		return nil, errors.New("empty URL")
	}

	if client == nil {
		client = &http.Client{Timeout: DefaultRSSTimeout}
	}

	// 1. Attempt feed URL discovery via DiscoverFeedURL
	targetFeedURL, discoverErr := DiscoverFeedURL(ctx, client, canonical)
	candidateURLs := make([]string, 0, 2)
	if discoverErr == nil && targetFeedURL != "" {
		candidateURLs = append(candidateURLs, targetFeedURL)
		if CanonicalURL(targetFeedURL) != canonical {
			candidateURLs = append(candidateURLs, canonical)
		}
	} else {
		candidateURLs = append(candidateURLs, canonical)
	}

	fp := gofeed.NewParser()
	var lastErr error

	for _, tryURL := range candidateURLs {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, tryURL, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("User-Agent", DefaultRSSUserAgent)
		req.Header.Set("Accept", "application/atom+xml, application/rss+xml, application/json, text/xml, application/xml, */*")

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %s for %s", resp.Status, tryURL)
			continue
		}

		bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, MaxFeedBodyBytes))
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}

		parsed, err := fp.Parse(bytes.NewReader(bodyBytes))
		if err != nil {
			lastErr = err
			continue
		}

		// Successfully parsed feed
		feedURL := tryURL
		title := strings.TrimSpace(parsed.Title)
		if title == "" {
			title = feedURL
		}

		siteURL := strings.TrimSpace(parsed.Link)
		if siteURL != "" {
			siteURL = resolveAssetURL(feedURL, siteURL)
		}

		avatarURL := ""
		if parsed.Image != nil && parsed.Image.URL != "" {
			avatarURL = resolveAssetURL(feedURL, parsed.Image.URL)
		}

		feedID := FeedID(CanonicalURL(feedURL))
		now := time.Now().UnixMilli()

		feed := RSSFeed{
			ID:                  feedID,
			FeedURL:             CanonicalURL(feedURL),
			SiteURL:             siteURL,
			Title:               title,
			AvatarURL:           avatarURL,
			RefreshIntervalMins: DefaultRSSRefreshIntervalMins,
			UpdatedAt:           now,
		}

		return &ResolvedRSSSource{
			Feed:         feed,
			ArticleCount: len(parsed.Items),
		}, nil
	}

	if discoverErr != nil && lastErr != nil {
		return nil, fmt.Errorf("feed resolution failed: %w", lastErr)
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no valid RSS/Atom/JSON feed found at %s", canonical)
}
