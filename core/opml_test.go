package core

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestParseRSSOPMLSingleLayer verifies parsing standalone top-level feeds without groups.
func TestParseRSSOPMLSingleLayer(t *testing.T) {
	opmlData := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <head>
    <title>Standalone Feeds</title>
  </head>
  <body>
    <outline type="rss" text="Hacker News" title="Hacker News" xmlUrl="https://news.ycombinator.com/rss" htmlUrl="https://news.ycombinator.com"/>
    <outline type="rss" text="Daring Fireball" title="Daring Fireball" xmlUrl="https://daringfireball.net/feeds/main" htmlUrl="https://daringfireball.net"/>
  </body>
</opml>`)

	parsed, err := ParseRSSOPML(opmlData)
	if err != nil {
		t.Fatalf("ParseRSSOPML failed: %v", err)
	}

	if parsed.Title != "Standalone Feeds" {
		t.Errorf("Title = %q; want %q", parsed.Title, "Standalone Feeds")
	}
	if len(parsed.Groups) != 0 {
		t.Errorf("len(Groups) = %d; want 0", len(parsed.Groups))
	}
	if len(parsed.Feeds) != 2 {
		t.Fatalf("len(Feeds) = %d; want 2", len(parsed.Feeds))
	}

	f1 := parsed.Feeds[0]
	if f1.Title != "Hacker News" || f1.FeedURL != "https://news.ycombinator.com/rss" || f1.SiteURL != "https://news.ycombinator.com" {
		t.Errorf("f1 = %+v; want Hacker News", f1)
	}
	if len(f1.GroupIDs) != 0 {
		t.Errorf("f1.GroupIDs = %v; want empty", f1.GroupIDs)
	}

	f2 := parsed.Feeds[1]
	if f2.Title != "Daring Fireball" || f2.FeedURL != "https://daringfireball.net/feeds/main" {
		t.Errorf("f2 = %+v; want Daring Fireball", f2)
	}
}

// TestParseRSSOPMLNetNewsWireStyle verifies parsing standard NetNewsWire exported OPML documents.
func TestParseRSSOPMLNetNewsWireStyle(t *testing.T) {
	opmlData := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<opml version="1.0">
	<head>
		<title>Subscriptions-NetNewsWire</title>
	</head>
	<body>
		<outline text="Daring Fireball" title="Daring Fireball" description="" type="rss" version="RSS" htmlUrl="https://daringfireball.net/" xmlUrl="https://daringfireball.net/feeds/main"/>
		<outline text="Technology" title="Technology">
			<outline text="Ars Technica" title="Ars Technica" description="" type="rss" version="RSS" htmlUrl="https://arstechnica.com" xmlUrl="https://feeds.arstechnica.com/arstechnica/index"/>
			<outline text="The Verge" title="The Verge" description="" type="rss" version="RSS" htmlUrl="https://theverge.com" xmlUrl="https://www.theverge.com/rss/index.xml"/>
		</outline>
		<outline text="Comics" title="Comics">
			<outline text="xkcd" title="xkcd" description="" type="rss" version="RSS" htmlUrl="https://xkcd.com" xmlUrl="https://xkcd.com/rss.xml"/>
		</outline>
	</body>
</opml>`)

	parsed, err := ParseRSSOPML(opmlData)
	if err != nil {
		t.Fatalf("ParseRSSOPML failed: %v", err)
	}

	if len(parsed.Groups) != 2 {
		t.Fatalf("len(Groups) = %d; want 2", len(parsed.Groups))
	}
	if parsed.Groups[0].Name != "Technology" {
		t.Errorf("Groups[0].Name = %q; want Technology", parsed.Groups[0].Name)
	}
	if parsed.Groups[1].Name != "Comics" {
		t.Errorf("Groups[1].Name = %q; want Comics", parsed.Groups[1].Name)
	}

	if len(parsed.Feeds) != 4 {
		t.Fatalf("len(Feeds) = %d; want 4", len(parsed.Feeds))
	}

	// Ungrouped feed
	df := parsed.Feeds[0]
	if df.Title != "Daring Fireball" || len(df.GroupIDs) != 0 {
		t.Errorf("Daring Fireball should be ungrouped, got GroupIDs: %v", df.GroupIDs)
	}

	// Grouped feeds
	ars := parsed.Feeds[1]
	if ars.Title != "Ars Technica" || !slices.Contains(ars.GroupIDs, parsed.Groups[0].ID) {
		t.Errorf("Ars Technica missing Technology group ID, got: %v", ars.GroupIDs)
	}

	verge := parsed.Feeds[2]
	if verge.Title != "The Verge" || !slices.Contains(verge.GroupIDs, parsed.Groups[0].ID) {
		t.Errorf("The Verge missing Technology group ID, got: %v", verge.GroupIDs)
	}

	// Check group's FeedIDs
	techGroup := parsed.Groups[0]
	if len(techGroup.FeedIDs) != 2 || !slices.Contains(techGroup.FeedIDs, ars.ID) || !slices.Contains(techGroup.FeedIDs, verge.ID) {
		t.Errorf("Technology group FeedIDs = %v; want ars and verge", techGroup.FeedIDs)
	}
}

// TestParseRSSOPMLFeedlyStyle verifies parsing standard Feedly exported OPML documents.
func TestParseRSSOPMLFeedlyStyle(t *testing.T) {
	opmlData := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<opml version="1.0">
    <head>
        <title>feedly Cloud Subscriptions</title>
    </head>
    <body>
        <outline text="Tech News" title="Tech News">
            <outline type="rss" text="TechCrunch" title="TechCrunch" xmlUrl="https://techcrunch.com/feed/" htmlUrl="https://techcrunch.com"/>
            <outline type="rss" text="Wired" title="Wired" xmlUrl="https://www.wired.com/feed/rss" htmlUrl="https://www.wired.com"/>
        </outline>
    </body>
</opml>`)

	parsed, err := ParseRSSOPML(opmlData)
	if err != nil {
		t.Fatalf("ParseRSSOPML failed: %v", err)
	}

	if len(parsed.Groups) != 1 || parsed.Groups[0].Name != "Tech News" {
		t.Fatalf("unexpected groups: %+v", parsed.Groups)
	}
	if len(parsed.Feeds) != 2 {
		t.Fatalf("len(Feeds) = %d; want 2", len(parsed.Feeds))
	}
}

// TestParseRSSOPMLNestedGroups verifies parsing multi-level nested folders.
func TestParseRSSOPMLNestedGroups(t *testing.T) {
	opmlData := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <head>
    <title>Nested Subscriptions</title>
  </head>
  <body>
    <outline text="Technology" title="Technology">
      <outline type="rss" text="Top Tech" title="Top Tech" xmlUrl="https://example.com/toptech.xml"/>
      <outline text="Mobile" title="Mobile">
        <outline text="Apple" title="Apple">
          <outline type="rss" text="9to5Mac" title="9to5Mac" xmlUrl="https://9to5mac.com/feed/" htmlUrl="https://9to5mac.com"/>
        </outline>
        <outline type="rss" text="Android Police" title="Android Police" xmlUrl="https://androidpolice.com/feed/"/>
      </outline>
    </outline>
  </body>
</opml>`)

	parsed, err := ParseRSSOPML(opmlData)
	if err != nil {
		t.Fatalf("ParseRSSOPML failed: %v", err)
	}

	// Should extract groups: Technology, Mobile, Apple
	if len(parsed.Groups) != 3 {
		t.Fatalf("len(Groups) = %d; want 3", len(parsed.Groups))
	}
	groupNames := []string{parsed.Groups[0].Name, parsed.Groups[1].Name, parsed.Groups[2].Name}
	for _, expected := range []string{"Technology", "Mobile", "Apple"} {
		if !slices.Contains(groupNames, expected) {
			t.Errorf("missing group %q, got: %v", expected, groupNames)
		}
	}

	if len(parsed.Feeds) != 3 {
		t.Fatalf("len(Feeds) = %d; want 3", len(parsed.Feeds))
	}

	var macFeed, apFeed, topFeed *RSSFeed
	for i := range parsed.Feeds {
		switch parsed.Feeds[i].Title {
		case "9to5Mac":
			macFeed = &parsed.Feeds[i]
		case "Android Police":
			apFeed = &parsed.Feeds[i]
		case "Top Tech":
			topFeed = &parsed.Feeds[i]
		}
	}

	if macFeed == nil || apFeed == nil || topFeed == nil {
		t.Fatalf("missing expected feeds: %+v", parsed.Feeds)
	}

	// 9to5Mac is in Technology, Mobile, and Apple
	techID := StableRSSGroupID("Technology")
	mobileID := StableRSSGroupID("Mobile")
	appleID := StableRSSGroupID("Apple")

	if !slices.Contains(macFeed.GroupIDs, techID) || !slices.Contains(macFeed.GroupIDs, mobileID) || !slices.Contains(macFeed.GroupIDs, appleID) {
		t.Errorf("9to5Mac group IDs = %v; want Technology, Mobile, and Apple", macFeed.GroupIDs)
	}

	// Android Police is in Technology and Mobile
	if !slices.Contains(apFeed.GroupIDs, techID) || !slices.Contains(apFeed.GroupIDs, mobileID) {
		t.Errorf("Android Police group IDs = %v; want Technology and Mobile", apFeed.GroupIDs)
	}

	// Top Tech is only in Technology
	if len(topFeed.GroupIDs) != 1 || topFeed.GroupIDs[0] != techID {
		t.Errorf("Top Tech group IDs = %v; want only Technology", topFeed.GroupIDs)
	}
}

// TestParseRSSOPMLFaultTolerance verifies resilience against missing attributes,
// uppercase xmlURL/htmlURL, feed:// protocols, and duplicate feeds.
func TestParseRSSOPMLFaultTolerance(t *testing.T) {
	opmlData := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<opml version="1.0">
  <head>
    <title>Fault Tolerance Test</title>
  </head>
  <body>
    <!-- Missing title (uses text) -->
    <outline text="Only Text Title" xmlUrl="https://example.com/feed1.xml"/>
    
    <!-- Missing text (uses title) -->
    <outline title="Only Title Attr" xmlUrl="https://example.com/feed2.xml"/>
    
    <!-- Missing both title and text (falls back to URL) -->
    <outline xmlUrl="https://example.com/feed3.xml"/>
    
    <!-- Uppercase xmlURL and htmlURL with feed:// scheme -->
    <outline text="Scheme Test" xmlURL="feed://example.com/feed4.xml" htmlURL="https://example.com"/>

    <!-- feed:https:// scheme -->
    <outline text="Feed HTTPS Scheme" xmlURL="feed:https://example.com/feed5.xml"/>

    <!-- Duplicate feeds (same feed in two different outlines) -->
    <outline text="Group 1">
      <outline text="Dup Feed" xmlUrl="https://example.com/dup.xml"/>
    </outline>
    <outline text="Group 2">
      <outline text="Dup Feed Again" xmlUrl="https://example.com/dup.xml"/>
    </outline>

    <!-- Empty outline (should be safely skipped) -->
    <outline/>
  </body>
</opml>`)

	parsed, err := ParseRSSOPML(opmlData)
	if err != nil {
		t.Fatalf("ParseRSSOPML failed: %v", err)
	}

	// Group 1 and Group 2
	if len(parsed.Groups) != 2 {
		t.Errorf("len(Groups) = %d; want 2", len(parsed.Groups))
	}

	// 6 unique feeds: feed1, feed2, feed3, feed4, feed5, dup
	if len(parsed.Feeds) != 6 {
		t.Fatalf("len(Feeds) = %d; want 6", len(parsed.Feeds))
	}

	feedByTitle := make(map[string]RSSFeed)
	for _, f := range parsed.Feeds {
		feedByTitle[f.Title] = f
	}

	if f, ok := feedByTitle["Only Text Title"]; !ok || f.FeedURL != "https://example.com/feed1.xml" {
		t.Errorf("unexpected f1: %+v", f)
	}

	if f, ok := feedByTitle["Only Title Attr"]; !ok || f.FeedURL != "https://example.com/feed2.xml" {
		t.Errorf("unexpected f2: %+v", f)
	}

	if _, ok := feedByTitle["https://example.com/feed3.xml"]; !ok {
		t.Errorf("missing fallback URL title feed: %+v", parsed.Feeds)
	}

	if f, ok := feedByTitle["Scheme Test"]; !ok || f.FeedURL != "http://example.com/feed4.xml" || f.SiteURL != "https://example.com" {
		t.Errorf("unexpected Scheme Test: %+v", f)
	}

	if f, ok := feedByTitle["Feed HTTPS Scheme"]; !ok || f.FeedURL != "https://example.com/feed5.xml" {
		t.Errorf("unexpected Feed HTTPS Scheme: %+v", f)
	}

	// Check deduplicated feed
	var dupFeed *RSSFeed
	for i := range parsed.Feeds {
		if parsed.Feeds[i].FeedURL == "https://example.com/dup.xml" {
			dupFeed = &parsed.Feeds[i]
			break
		}
	}
	if dupFeed == nil {
		t.Fatal("duplicate feed not found")
	}
	if len(dupFeed.GroupIDs) != 2 {
		t.Errorf("dupFeed.GroupIDs = %v; want 2 group IDs", dupFeed.GroupIDs)
	}
}

// TestExportRSSOPMLRoundTrip verifies exporting subscriptions to OPML and parsing them back.
func TestExportRSSOPMLRoundTrip(t *testing.T) {
	now := int64(1700000000000)

	groups := []RSSGroup{
		{
			ID:        "rss-group-tech",
			Name:      "Tech News",
			FeedIDs:   []string{"feed:theverge", "feed:arstechnica"},
			SortOrder: 0,
			UpdatedAt: now,
		},
		{
			ID:        "rss-group-comics",
			Name:      "Comics",
			FeedIDs:   []string{"feed:xkcd"},
			SortOrder: 1,
			UpdatedAt: now,
		},
		{
			ID:        "rss-group-empty",
			Name:      "Empty Folder",
			FeedIDs:   nil,
			SortOrder: 2,
			UpdatedAt: now,
		},
		{
			ID:        "rss-group-deleted",
			Name:      "Deleted Folder",
			Deleted:   true,
			UpdatedAt: now,
		},
	}

	feeds := []RSSFeed{
		{
			ID:        "feed:theverge",
			FeedURL:   "https://www.theverge.com/rss/index.xml",
			SiteURL:   "https://www.theverge.com",
			Title:     "The Verge",
			GroupIDs:  []string{"rss-group-tech"},
			UpdatedAt: now,
		},
		{
			ID:          "feed:arstechnica",
			FeedURL:     "https://feeds.arstechnica.com/arstechnica/index",
			SiteURL:     "https://arstechnica.com",
			Title:       "Ars Technica",
			CustomTitle: "Ars (Custom)",
			GroupIDs:    []string{"rss-group-tech"},
			UpdatedAt:   now,
		},
		{
			ID:        "feed:xkcd",
			FeedURL:   "https://xkcd.com/rss.xml",
			SiteURL:   "https://xkcd.com",
			Title:     "xkcd",
			GroupIDs:  []string{"rss-group-comics"},
			UpdatedAt: now,
		},
		{
			ID:        "feed:standalone",
			FeedURL:   "https://daringfireball.net/feeds/main",
			SiteURL:   "https://daringfireball.net",
			Title:     "Daring Fireball",
			GroupIDs:  nil,
			UpdatedAt: now,
		},
		{
			ID:        "feed:deleted",
			FeedURL:   "https://example.com/deleted.xml",
			Title:     "Deleted Feed",
			Deleted:   true,
			UpdatedAt: now,
		},
	}

	exportedXML, err := ExportRSSOPML(groups, feeds, "My Subscriptions")
	if err != nil {
		t.Fatalf("ExportRSSOPML failed: %v", err)
	}

	xmlStr := string(exportedXML)
	if !strings.HasPrefix(xmlStr, "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n") {
		t.Errorf("export missing XML declaration: %s", xmlStr[:40])
	}
	if !strings.Contains(xmlStr, `<opml version="2.0">`) {
		t.Errorf("export missing opml version 2.0 tag")
	}
	if strings.Contains(xmlStr, "Deleted Folder") {
		t.Errorf("export contains deleted folder")
	}
	if strings.Contains(xmlStr, "Deleted Feed") {
		t.Errorf("export contains deleted feed")
	}

	// Re-parse the exported OPML
	parsed, err := ParseRSSOPML(exportedXML)
	if err != nil {
		t.Fatalf("ParseRSSOPML on exported XML failed: %v\nXML:\n%s", err, xmlStr)
	}

	if parsed.Title != "My Subscriptions" {
		t.Errorf("Title = %q; want %q", parsed.Title, "My Subscriptions")
	}

	// 3 active groups: Tech News, Comics, Empty Folder
	if len(parsed.Groups) != 3 {
		t.Fatalf("len(Groups) = %d; want 3", len(parsed.Groups))
	}
	if parsed.Groups[0].Name != "Tech News" || parsed.Groups[1].Name != "Comics" || parsed.Groups[2].Name != "Empty Folder" {
		t.Errorf("unexpected parsed group names: %+v", parsed.Groups)
	}

	// 4 active feeds: The Verge, Ars (Custom), xkcd, Daring Fireball
	if len(parsed.Feeds) != 4 {
		t.Fatalf("len(Feeds) = %d; want 4", len(parsed.Feeds))
	}

	byFeedURL := make(map[string]RSSFeed)
	for _, f := range parsed.Feeds {
		byFeedURL[f.FeedURL] = f
	}

	arsParsed, ok := byFeedURL["https://feeds.arstechnica.com/arstechnica/index"]
	if !ok {
		t.Fatal("missing Ars Technica")
	}
	if arsParsed.Title != "Ars (Custom)" {
		t.Errorf("Ars title = %q; want custom title %q", arsParsed.Title, "Ars (Custom)")
	}

	dfParsed, ok := byFeedURL["https://daringfireball.net/feeds/main"]
	if !ok {
		t.Fatal("missing Daring Fireball")
	}
	if len(dfParsed.GroupIDs) != 0 {
		t.Errorf("Daring Fireball should be ungrouped, got: %v", dfParsed.GroupIDs)
	}
}

// TestParseAndExportRSSOPMLFile verifies reading and writing from disk.
func TestParseAndExportRSSOPMLFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.opml")

	groups := []RSSGroup{
		{ID: "g1", Name: "News", FeedIDs: []string{"f1"}, UpdatedAt: 100},
	}
	feeds := []RSSFeed{
		{ID: "f1", FeedURL: "https://example.com/rss", Title: "Example", GroupIDs: []string{"g1"}, UpdatedAt: 100},
	}

	if err := ExportRSSOPMLFile(filePath, groups, feeds, "Disk Test"); err != nil {
		t.Fatalf("ExportRSSOPMLFile failed: %v", err)
	}

	stat, err := os.Stat(filePath)
	if err != nil || stat.Size() == 0 {
		t.Fatalf("file not written properly: %v", err)
	}

	parsed, err := ParseRSSOPMLFile(filePath)
	if err != nil {
		t.Fatalf("ParseRSSOPMLFile failed: %v", err)
	}
	if len(parsed.Feeds) != 1 || parsed.Feeds[0].Title != "Example" {
		t.Fatalf("unexpected parsed result: %+v", parsed)
	}
}
