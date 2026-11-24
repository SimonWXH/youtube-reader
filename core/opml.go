package core

import (
	"encoding/xml"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"slices"
	"strings"
	"time"
)

// OPMLDocument represents the root <opml> element for standard OPML 2.0 serialization.
type OPMLDocument struct {
	XMLName xml.Name `xml:"opml"`
	Version string   `xml:"version,attr"`
	Head    OPMLHead `xml:"head"`
	Body    OPMLBody `xml:"body"`
}

// OPMLHead represents metadata in the OPML <head> element.
type OPMLHead struct {
	Title        string `xml:"title,omitempty"`
	DateCreated  string `xml:"dateCreated,omitempty"`
	DateModified string `xml:"dateModified,omitempty"`
	Docs         string `xml:"docs,omitempty"`
}

// OPMLBody represents the OPML <body> element.
type OPMLBody struct {
	Outlines []OPMLOutline `xml:"outline"`
}

// OPMLOutline represents a node in the OPML document tree.
type OPMLOutline struct {
	Text        string        `xml:"text,attr"`
	Title       string        `xml:"title,attr,omitempty"`
	Type        string        `xml:"type,attr,omitempty"`
	XMLURL      string        `xml:"xmlUrl,attr,omitempty"`
	HTMLURL     string        `xml:"htmlUrl,attr,omitempty"`
	Description string        `xml:"description,attr,omitempty"`
	Version     string        `xml:"version,attr,omitempty"`
	Outlines    []OPMLOutline `xml:"outline,omitempty"`
}

// ParsedRSSOPML holds the decoded feeds and groups extracted from an OPML document.
type ParsedRSSOPML struct {
	Title  string
	Groups []RSSGroup
	Feeds  []RSSFeed
}

// StableRSSGroupID generates a deterministic, unique group ID from a group name.
func StableRSSGroupID(name string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(strings.TrimSpace(name)))
	return fmt.Sprintf("rss-group-%016x", h.Sum64())
}

// rawOPMLDoc and rawOPMLOutline provide fault-tolerant XML decoding,
// accepting case-insensitive attributes (xmlUrl/xmlURL, htmlUrl/htmlURL)
// and arbitrary nested outline tags.
type rawOPMLDoc struct {
	XMLName xml.Name    `xml:"opml"`
	Version string      `xml:"version,attr"`
	Head    rawOPMLHead `xml:"head"`
	Body    rawOPMLBody `xml:"body"`
}

type rawOPMLHead struct {
	Title string `xml:"title"`
}

type rawOPMLBody struct {
	Outlines []rawOPMLOutline `xml:"outline"`
}

func (b *rawOPMLBody) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	for {
		tok, err := d.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		switch elem := tok.(type) {
		case xml.StartElement:
			if strings.EqualFold(elem.Name.Local, "outline") {
				var child rawOPMLOutline
				if err := d.DecodeElement(&child, &elem); err != nil {
					return err
				}
				b.Outlines = append(b.Outlines, child)
			} else {
				if err := d.Skip(); err != nil {
					return err
				}
			}
		case xml.EndElement:
			if elem.Name == start.Name {
				return nil
			}
		}
	}
	return nil
}

type rawOPMLOutline struct {
	Text        string
	Title       string
	Type        string
	XMLURL      string
	HTMLURL     string
	Description string
	Version     string
	Children    []rawOPMLOutline
}

func (o *rawOPMLOutline) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	for _, attr := range start.Attr {
		switch strings.ToLower(attr.Name.Local) {
		case "text":
			o.Text = attr.Value
		case "title":
			o.Title = attr.Value
		case "type":
			o.Type = attr.Value
		case "xmlurl":
			o.XMLURL = attr.Value
		case "htmlurl":
			o.HTMLURL = attr.Value
		case "description":
			o.Description = attr.Value
		case "version":
			o.Version = attr.Value
		}
	}
	for {
		tok, err := d.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		switch elem := tok.(type) {
		case xml.StartElement:
			if strings.EqualFold(elem.Name.Local, "outline") {
				var child rawOPMLOutline
				if err := d.DecodeElement(&child, &elem); err != nil {
					return err
				}
				o.Children = append(o.Children, child)
			} else {
				if err := d.Skip(); err != nil {
					return err
				}
			}
		case xml.EndElement:
			if elem.Name == start.Name {
				return nil
			}
		}
	}
	return nil
}

// normalizeRawURL cleans URL schemes commonly found in OPML files,
// such as feed://, feed:http://, and feed:https://.
func normalizeRawURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "feed:http://") {
		return strings.TrimPrefix(raw, "feed:")
	}
	if strings.HasPrefix(raw, "feed:https://") {
		return strings.TrimPrefix(raw, "feed:")
	}
	if strings.HasPrefix(raw, "feed://") {
		return "http://" + strings.TrimPrefix(raw, "feed://")
	}
	return raw
}

// ParseRSSOPML parses OPML 1.0/2.0 XML data and returns structured groups and feeds.
// It handles single-level feeds, nested folder structures, missing titles or URLs,
// and automatically deduplicates identical feed URLs within the document.
func ParseRSSOPML(data []byte) (*ParsedRSSOPML, error) {
	var doc rawOPMLDoc
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse OPML xml: %w", err)
	}

	now := time.Now().UnixMilli()
	var groups []RSSGroup
	var feeds []RSSFeed
	groupByName := make(map[string]int) // normalized name -> index in groups
	feedByID := make(map[string]int)    // feedID -> index in feeds

	var walk func(outlines []rawOPMLOutline, parentGroupIDs []string)
	walk = func(outlines []rawOPMLOutline, parentGroupIDs []string) {
		for _, o := range outlines {
			rawXMLURL := normalizeRawURL(o.XMLURL)
			hasFeedURL := rawXMLURL != ""

			// Detect folder / group
			groupName := strings.TrimSpace(o.Title)
			if groupName == "" {
				groupName = strings.TrimSpace(o.Text)
			}

			isGroup := !hasFeedURL && !strings.EqualFold(strings.TrimSpace(o.Type), "rss") &&
				strings.TrimSpace(o.HTMLURL) == "" && (len(o.Children) > 0 || groupName != "")
			var currentGroupID string

			if isGroup && groupName != "" {
				normKey := strings.ToLower(groupName)
				if idx, exists := groupByName[normKey]; exists {
					currentGroupID = groups[idx].ID
				} else {
					currentGroupID = StableRSSGroupID(groupName)
					grp := RSSGroup{
						ID:        currentGroupID,
						Name:      groupName,
						SortOrder: len(groups),
						UpdatedAt: now,
					}
					groups = append(groups, grp)
					groupByName[normKey] = len(groups) - 1
				}
			}

			nextParents := parentGroupIDs
			if currentGroupID != "" {
				nextParents = append(slices.Clone(parentGroupIDs), currentGroupID)
			}

			// Process feed entry if xmlUrl is present
			if hasFeedURL {
				canonicalURL := CanonicalURL(rawXMLURL)
				if canonicalURL != "" {
					fid := FeedID(canonicalURL)
					title := strings.TrimSpace(o.Title)
					if title == "" {
						title = strings.TrimSpace(o.Text)
					}
					if title == "" {
						title = canonicalURL
					}
					siteURL := ""
					if rawHTML := strings.TrimSpace(o.HTMLURL); rawHTML != "" {
						siteURL = CanonicalURL(rawHTML)
					}

					if idx, exists := feedByID[fid]; exists {
						// Deduplicate: merge group IDs
						for _, gid := range parentGroupIDs {
							if !slices.Contains(feeds[idx].GroupIDs, gid) {
								feeds[idx].GroupIDs = append(feeds[idx].GroupIDs, gid)
							}
						}
					} else {
						f := RSSFeed{
							ID:        fid,
							FeedURL:   canonicalURL,
							SiteURL:   siteURL,
							Title:     title,
							GroupIDs:  slices.Clone(parentGroupIDs),
							UpdatedAt: now,
						}
						feeds = append(feeds, f)
						feedByID[fid] = len(feeds) - 1
					}

					// Associate feed with all ancestor groups
					for _, gid := range parentGroupIDs {
						for gi := range groups {
							if groups[gi].ID == gid {
								if !slices.Contains(groups[gi].FeedIDs, fid) {
									groups[gi].FeedIDs = append(groups[gi].FeedIDs, fid)
								}
							}
						}
					}
				}
			}

			// Recurse into children
			if len(o.Children) > 0 {
				walk(o.Children, nextParents)
			}
		}
	}

	walk(doc.Body.Outlines, nil)

	return &ParsedRSSOPML{
		Title:  strings.TrimSpace(doc.Head.Title),
		Groups: groups,
		Feeds:  feeds,
	}, nil
}

// ParseRSSOPMLFile reads and parses an OPML file from the filesystem.
func ParseRSSOPMLFile(path string) (*ParsedRSSOPML, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseRSSOPML(data)
}

// ExportRSSOPML serializes RSS groups and feeds into a standards-compliant OPML 2.0 XML byte slice.
// Each group becomes one folder outline in the order given and holds its own feeds, feeds in no
// group follow at the top level in title order, and every feed outline carries its title, xmlUrl
// and htmlUrl. Soft-deleted records are skipped, and the output is formatted with an XML
// declaration and indentation.
func ExportRSSOPML(groups []RSSGroup, feeds []RSSFeed, title string) ([]byte, error) {
	if title == "" {
		title = "RSS Subscriptions"
	}
	nowRFC := time.Now().UTC().Format(time.RFC1123)
	doc := OPMLDocument{
		Version: "2.0",
		Head: OPMLHead{
			Title:        title,
			DateCreated:  nowRFC,
			DateModified: nowRFC,
			Docs:         "http://opml.org/spec2.opml",
		},
	}

	activeFeeds := make(map[string]RSSFeed, len(feeds))
	for _, f := range feeds {
		if !f.Deleted {
			activeFeeds[f.ID] = f
		}
	}

	exportedFeedIDs := make(map[string]bool)

	// 1. Export groups (skipping deleted)
	for _, g := range groups {
		if g.Deleted {
			continue
		}
		groupOutline := OPMLOutline{
			Text:  g.Name,
			Title: g.Name,
		}

		var groupFeeds []RSSFeed
		seenInGroup := make(map[string]bool)
		for _, fid := range g.FeedIDs {
			if f, ok := activeFeeds[fid]; ok && !seenInGroup[fid] {
				groupFeeds = append(groupFeeds, f)
				seenInGroup[fid] = true
				exportedFeedIDs[fid] = true
			}
		}
		for _, f := range activeFeeds {
			if slices.Contains(f.GroupIDs, g.ID) && !seenInGroup[f.ID] {
				groupFeeds = append(groupFeeds, f)
				seenInGroup[f.ID] = true
				exportedFeedIDs[f.ID] = true
			}
		}

		for _, f := range groupFeeds {
			groupOutline.Outlines = append(groupOutline.Outlines, OPMLOutline{
				Type:    "rss",
				Text:    f.DisplayName(),
				Title:   f.DisplayName(),
				XMLURL:  f.FeedURL,
				HTMLURL: f.SiteURL,
			})
		}
		doc.Body.Outlines = append(doc.Body.Outlines, groupOutline)
	}

	// 2. Export ungrouped feeds
	var ungrouped []RSSFeed
	for _, f := range activeFeeds {
		if !exportedFeedIDs[f.ID] {
			ungrouped = append(ungrouped, f)
		}
	}
	slices.SortStableFunc(ungrouped, func(a, b RSSFeed) int {
		return strings.Compare(strings.ToLower(a.DisplayName()), strings.ToLower(b.DisplayName()))
	})

	for _, f := range ungrouped {
		doc.Body.Outlines = append(doc.Body.Outlines, OPMLOutline{
			Type:    "rss",
			Text:    f.DisplayName(),
			Title:   f.DisplayName(),
			XMLURL:  f.FeedURL,
			HTMLURL: f.SiteURL,
		})
	}

	data, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal OPML: %w", err)
	}

	res := append([]byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n"), data...)
	res = append(res, '\n')
	return res, nil
}

// ExportRSSOPMLFile serializes RSS subscriptions and writes them to a file.
func ExportRSSOPMLFile(path string, groups []RSSGroup, feeds []RSSFeed, title string) error {
	data, err := ExportRSSOPML(groups, feeds, title)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
