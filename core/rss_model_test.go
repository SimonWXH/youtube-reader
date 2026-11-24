package core

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCanonicalURL(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"  https://Example.COM/feed/  ", "https://example.com/feed"},
		{"http://example.com:80/rss", "http://example.com/rss"},
		{"https://example.com:443/atom.xml", "https://example.com/atom.xml"},
		{"https://example.com/feed/?utm_source=twitter&utm_medium=social&key=value#section", "https://example.com/feed?key=value"},
		{"https://example.com/blog/?fbclid=xyz123", "https://example.com/blog"},
		{"https://example.com/", "https://example.com/"},
	}
	for _, c := range cases {
		got := CanonicalURL(c.input)
		if got != c.want {
			t.Errorf("CanonicalURL(%q) = %q; want %q", c.input, got, c.want)
		}
	}
}

func TestFeedIDAndArticleIDDeterministic(t *testing.T) {
	url1 := "https://example.com/feed.xml"
	url2 := "https://EXAMPLE.COM/feed.xml/"
	id1 := FeedID(CanonicalURL(url1))
	id2 := FeedID(CanonicalURL(url2))
	if id1 != id2 {
		t.Fatalf("FeedID should be deterministic for normalized URLs, got %q vs %q", id1, id2)
	}
	if !strings.HasPrefix(id1, "feed:") {
		t.Fatalf("FeedID must have 'feed:' prefix, got %q", id1)
	}

	dedupKey := "https://example.com/item/1"
	artID1 := ArticleID(id1, dedupKey)
	artID2 := ArticleID(id1, dedupKey)
	if artID1 != artID2 {
		t.Fatalf("ArticleID must be deterministic, got %q vs %q", artID1, artID2)
	}
	if !strings.HasPrefix(artID1, "art:feed:") {
		t.Fatalf("ArticleID must have 'art:feed:' prefix, got %q", artID1)
	}
}

func TestArticleDeduplicationKey(t *testing.T) {
	// GUID prioritized over link
	k1 := ArticleDeduplicationKey("unique-guid-123", "https://example.com/post/1", "Title 1")
	if k1 != "unique-guid-123" {
		t.Fatalf("dedup key should prioritize GUID, got %q", k1)
	}

	// Fallback to link when GUID is empty
	k2 := ArticleDeduplicationKey("", "https://EXAMPLE.COM/post/1/?utm_source=rss", "Title 1")
	if k2 != "https://example.com/post/1" {
		t.Fatalf("dedup key should fallback to canonical link, got %q", k2)
	}

	// Fallback to title when link and GUID are empty
	k3 := ArticleDeduplicationKey("", "", "Unique Title Here")
	if k3 != "Unique Title Here" {
		t.Fatalf("dedup key should fallback to title, got %q", k3)
	}

	// Final fallback
	k4 := ArticleDeduplicationKey("", "", "")
	if k4 != "empty" {
		t.Fatalf("dedup key should fallback to 'empty', got %q", k4)
	}
}

func TestParagraphHash(t *testing.T) {
	h1 := ParagraphHash(BlockParagraph, "Hello world", "")
	h2 := ParagraphHash(BlockParagraph, "Hello world", "")
	h3 := ParagraphHash(BlockParagraph, "Different text", "")
	h4 := ParagraphHash(BlockHeading, "Hello world", "")

	if h1 != h2 {
		t.Fatalf("ParagraphHash must be deterministic, got %q != %q", h1, h2)
	}
	if h1 == h3 {
		t.Fatalf("ParagraphHash must differ for different text, got %q == %q", h1, h3)
	}
	if h1 == h4 {
		t.Fatalf("ParagraphHash must differ for different block types, got %q == %q", h1, h4)
	}
}

func TestRSSSyncStateMonotonicReadAbsorption(t *testing.T) {
	now := time.Now().UnixMilli()

	// Device 1 marked article-1 as read
	s1 := NewRSSSyncState("device-mac")
	s1.MarkRead("art-1", now)

	// Device 2 has article-1 unread with a newer timestamp
	s2 := NewRSSSyncState("device-android")
	s2.ReadStates["art-1"] = map[string]RSSReadRecord{
		"device-android": {
			ArticleID: "art-1",
			DeviceID:  "device-android",
			Read:      false,
			UpdatedAt: now + 5000,
		},
	}

	merged := MergeRSSSync(s1, s2)
	if !merged.IsRead("art-1") {
		t.Fatalf("article must remain read under monotonic absorption, even if device 2 has newer unread record")
	}
}

func TestRSSSyncStateTombstoneLWW(t *testing.T) {
	t0 := int64(1000)
	t1 := int64(2000)

	// Device A created a feed at t0
	sA := NewRSSSyncState("devA")
	sA.Feeds["feed-1"] = RSSFeed{
		ID:        "feed-1",
		FeedURL:   "https://example.com/rss",
		Title:     "Old Title",
		UpdatedAt: t0,
		Deleted:   false,
	}

	// Device B deleted feed-1 at t1 with a tombstone
	sB := NewRSSSyncState("devB")
	sB.Feeds["feed-1"] = RSSFeed{
		ID:        "feed-1",
		FeedURL:   "https://example.com/rss",
		Title:     "Old Title",
		UpdatedAt: t1,
		Deleted:   true,
		DeletedAt: t1,
	}

	merged := MergeRSSSync(sA, sB)
	feed, ok := merged.Feeds["feed-1"]
	if !ok {
		t.Fatalf("tombstone feed should exist in merged state")
	}
	if !feed.Deleted || feed.DeletedAt != t1 {
		t.Fatalf("feed tombstone should be respected: deleted=%v, deletedAt=%d", feed.Deleted, feed.DeletedAt)
	}

	// Reverse order merge gives identical result
	mergedRev := MergeRSSSync(sB, sA)
	feedRev := mergedRev.Feeds["feed-1"]
	if !feedRev.Deleted || feedRev.DeletedAt != t1 {
		t.Fatalf("merge order must not affect tombstone resolution")
	}
}

func TestRSSSyncStateExcludesArticleContent(t *testing.T) {
	// Verify that RSSSyncState only contains lightweight metadata and cannot serialize article bodies
	syncState := NewRSSSyncState("device-mac")
	syncState.Feeds["feed-1"] = RSSFeed{
		ID:        "feed-1",
		FeedURL:   "https://example.com/rss",
		Title:     "Tech Blog",
		UpdatedAt: 1000,
	}
	syncState.MarkRead("art-1", 1000)
	syncState.SetFavorite("art-1", true, 1000)

	raw, err := json.Marshal(syncState)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	jsonStr := string(raw)
	// Article content fields MUST NEVER appear in sync payload
	for _, forbidden := range []string{"contentHtml", "blocks", "snippet", "paragraphs"} {
		if strings.Contains(jsonStr, `"`+forbidden+`"`) {
			t.Fatalf("sync payload contains forbidden article body field: %q in %s", forbidden, jsonStr)
		}
	}
}
