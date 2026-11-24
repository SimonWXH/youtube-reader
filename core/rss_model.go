package core

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// BlockType identifies the structured content element type in an article.
type BlockType string

const (
	BlockHeading    BlockType = "heading"
	BlockParagraph  BlockType = "paragraph"
	BlockBlockquote BlockType = "blockquote"
	BlockCode       BlockType = "code"
	BlockImage      BlockType = "image"
	BlockLink       BlockType = "link"
	BlockList       BlockType = "list"
)

// ArticleBlock represents a structured block of content within an article,
// used for native reading view rendering and paragraph-level note anchoring.
type ArticleBlock struct {
	Type     BlockType `json:"type"`
	Text     string    `json:"text,omitempty"`
	HTML     string    `json:"html,omitempty"`
	URL      string    `json:"url,omitempty"`      // Image src or link href
	Alt      string    `json:"alt,omitempty"`      // Image alt text
	Caption  string    `json:"caption,omitempty"`  // Image caption
	Level    int       `json:"level,omitempty"`    // Heading level (1-6)
	Language string    `json:"language,omitempty"` // Code block language
	Hash     string    `json:"hash,omitempty"`     // Stable content hash for paragraph notes
}

// RSSFeed represents an RSS/Atom/JSON Feed subscription source.
// It is completely decoupled from YouTube Channel records.
type RSSFeed struct {
	ID                  string   `json:"id"`                            // Stable global ID: "feed:" + sha256(canonicalURL)[:16]
	FeedURL             string   `json:"feedUrl"`                       // Feed XML/JSON URL
	SiteURL             string   `json:"siteUrl,omitempty"`             // Site home URL
	Title               string   `json:"title"`                         // Fetched title
	CustomTitle         string   `json:"customTitle,omitempty"`         // User custom title
	GroupIDs            []string `json:"groupIds,omitempty"`            // Belonging group IDs
	AvatarURL           string   `json:"avatarUrl,omitempty"`           // Favicon / feed avatar
	ETag                string   `json:"etag,omitempty"`                // HTTP conditional request ETag
	LastModified        string   `json:"lastModified,omitempty"`        // HTTP conditional request Last-Modified
	LastFetchedAt       int64    `json:"lastFetchedAt,omitempty"`       // Last fetch timestamp (msec)
	FailureCount        int      `json:"failureCount,omitempty"`        // Consecutive failures
	LastError           string   `json:"lastError,omitempty"`           // Last error message
	LastErrorAt         int64    `json:"lastErrorAt,omitempty"`         // Last error timestamp (msec)
	RefreshIntervalMins int      `json:"refreshIntervalMins,omitempty"` // Refresh interval in minutes (default 30)
	UpdatedAt           int64    `json:"updatedAt"`                     // Millisecond timestamp
	Deleted             bool     `json:"deleted,omitempty"`             // Tombstone flag for deletion propagation
	DeletedAt           int64    `json:"deletedAt,omitempty"`           // Deletion millisecond timestamp
}

// DisplayName returns the user-chosen title if available, falling back to feed title or URL.
func (f RSSFeed) DisplayName() string {
	if f.CustomTitle != "" {
		return f.CustomTitle
	}
	if f.Title != "" {
		return f.Title
	}
	return f.FeedURL
}

// RSSGroup represents a folder / category grouping RSS subscription feeds.
// It is completely decoupled from YouTube ChannelGroup records.
type RSSGroup struct {
	ID        string   `json:"id"`                  // Stable group ID, e.g. "rss-group-" + id
	Name      string   `json:"name"`                // Group name
	FeedIDs   []string `json:"feedIds,omitempty"`   // Feeds assigned to this group
	SortOrder int      `json:"sortOrder,omitempty"` // User-specified sort order
	UpdatedAt int64    `json:"updatedAt"`           // Millisecond timestamp
	Deleted   bool     `json:"deleted,omitempty"`   // Tombstone flag
	DeletedAt int64    `json:"deletedAt,omitempty"` // Deletion millisecond timestamp
}

// RSSArticle represents an article fetched from an RSS feed.
// It is stored exclusively in the local SQLite database.
//
// CRITICAL SYNCHRONIZATION BARRIER:
// Full article content (ContentHTML, Snippet, Blocks) MUST NEVER be placed into
// cross-device synchronization payloads (such as sync.json). Full text is kept
// local to each device. Cross-platform clients (e.g. Android) fetch content
// directly from the feed source and store it in their own local SQLite databases.
type RSSArticle struct {
	ID          string         `json:"id"`                  // Stable global ID: "art:" + feedID + ":" + sha256(dedupKey)[:16]
	FeedID      string         `json:"feedId"`              // Associated feed ID
	Title       string         `json:"title"`               // Article title
	Link        string         `json:"link"`                // Web link
	GUID        string         `json:"guid,omitempty"`      // Item GUID / ID from feed
	Author      string         `json:"author,omitempty"`    // Author name
	PublishedAt string         `json:"publishedAt"`         // ISO8601 published date
	Snippet     string         `json:"snippet"`             // 2-line plain-text snippet for list views
	ContentHTML string         `json:"contentHtml"`         // Full HTML content (local SQLite only)
	IsFullText  bool           `json:"isFullText"`          // True if full text was extracted; false if degraded to summary
	HeroImage   string         `json:"heroImage,omitempty"` // Hero/lead image URL
	Blocks      []ArticleBlock `json:"blocks,omitempty"`    // Structured blocks for native reading view
	Read        bool           `json:"read"`                // Local read state
	ReadAt      int64          `json:"readAt,omitempty"`    // Timestamp when marked read (msec)
	Starred     bool           `json:"starred"`             // Local star / favorite state
	StarredAt   int64          `json:"starredAt,omitempty"` // Timestamp when starred (msec)
	UpdatedAt   int64          `json:"updatedAt"`           // Millisecond timestamp
}

// RSSReadRecord holds cross-device read status for an article.
type RSSReadRecord struct {
	ArticleID string `json:"articleId"`
	DeviceID  string `json:"deviceId"`
	Read      bool   `json:"read"`
	UpdatedAt int64  `json:"updatedAt"`
}

// RSSFavoriteRecord holds cross-device star/favorite status for an article.
type RSSFavoriteRecord struct {
	ArticleID   string `json:"articleId"`
	FavoritedAt int64  `json:"favoritedAt"`
	UpdatedAt   int64  `json:"updatedAt"`
	Deleted     bool   `json:"deleted,omitempty"`
	DeletedAt   int64  `json:"deletedAt,omitempty"`
}

// ArticleNoteRecord holds paragraph-level notes or annotations on an article.
type ArticleNoteRecord struct {
	ID             string `json:"id"`
	ArticleID      string `json:"articleId"`
	ParagraphIndex int    `json:"paragraphIndex"`
	ParagraphHash  string `json:"paragraphHash,omitempty"` // Hash of paragraph text for resilient anchoring
	SelectedText   string `json:"selectedText,omitempty"`
	NoteText       string `json:"noteText"`
	UpdatedAt      int64  `json:"updatedAt"`
	Deleted        bool   `json:"deleted,omitempty"`
	DeletedAt      int64  `json:"deletedAt,omitempty"`
}

// ======================================================================================
// SYNC ARCHITECTURE & METADATA DECOUPLING (同步分层与正文解耦)
// ======================================================================================
// RSSSyncState represents the lightweight synchronization payload shared across devices
// (macOS and future Android clients via Syncthing or cloud storage).
//
// CORE CONSTRAINTS:
//  1. Only lightweight metadata (feed configurations, group memberships, read state vectors,
//     favorites, and article notes) may be serialized into this synchronization payload.
//  2. ARTICLE FULL TEXT (HTML, snippets, and parsed blocks) MUST NEVER BE WRITTEN TO SYNC DATA.
//     Article bodies live strictly in each device's local SQLite store.
//  3. When Device A subscribes to a feed or marks an article as read, Device B receives
//     the updated metadata. Device B's own background fetch engine independently retrieves
//     the feed and articles from the web, populating its local SQLite database.
//  4. This separation ensures sync files stay tiny (kilobytes, not dozens of megabytes),
//     prevents Syncthing write conflicts, eliminates OOM risks on mobile, and guarantees
//     fast, smooth cross-platform sync.
type RSSSyncState struct {
	SchemaVersion int                                 `json:"schemaVersion"`
	DeviceID      string                              `json:"deviceId"`
	UpdatedAt     int64                               `json:"updatedAt"`
	Feeds         map[string]RSSFeed                  `json:"feeds"`
	Groups        map[string]RSSGroup                 `json:"groups"`
	ReadStates    map[string]map[string]RSSReadRecord `json:"readStates"`      // articleID -> deviceID -> record
	Favorites     map[string]RSSFavoriteRecord        `json:"favorites"`       // articleID -> record
	Notes         map[string]ArticleNoteRecord        `json:"notes,omitempty"` // noteID -> record
}

// NewRSSSyncState initializes an empty RSSSyncState for the specified device ID.
func NewRSSSyncState(deviceID string) RSSSyncState {
	return RSSSyncState{
		SchemaVersion: 1,
		DeviceID:      deviceID,
		UpdatedAt:     time.Now().UnixMilli(),
		Feeds:         make(map[string]RSSFeed),
		Groups:        make(map[string]RSSGroup),
		ReadStates:    make(map[string]map[string]RSSReadRecord),
		Favorites:     make(map[string]RSSFavoriteRecord),
		Notes:         make(map[string]ArticleNoteRecord),
	}
}

// Normalize ensures map fields are initialized and valid.
func (s *RSSSyncState) Normalize() {
	if s.SchemaVersion == 0 {
		s.SchemaVersion = 1
	}
	if s.Feeds == nil {
		s.Feeds = make(map[string]RSSFeed)
	}
	if s.Groups == nil {
		s.Groups = make(map[string]RSSGroup)
	}
	if s.ReadStates == nil {
		s.ReadStates = make(map[string]map[string]RSSReadRecord)
	}
	if s.Favorites == nil {
		s.Favorites = make(map[string]RSSFavoriteRecord)
	}
	if s.Notes == nil {
		s.Notes = make(map[string]ArticleNoteRecord)
	}
}

// IsRead reports whether the given article has been marked read on any device.
func (s *RSSSyncState) IsRead(articleID string) bool {
	for _, rec := range s.ReadStates[articleID] {
		if rec.Read {
			return true
		}
	}
	return false
}

// MarkRead marks an article as read for the local device.
func (s *RSSSyncState) MarkRead(articleID string, now int64) {
	s.Normalize()
	if s.ReadStates[articleID] == nil {
		s.ReadStates[articleID] = make(map[string]RSSReadRecord)
	}
	s.ReadStates[articleID][s.DeviceID] = RSSReadRecord{
		ArticleID: articleID,
		DeviceID:  s.DeviceID,
		Read:      true,
		UpdatedAt: now,
	}
	if now > s.UpdatedAt {
		s.UpdatedAt = now
	}
}

// SetFavorite marks or unmarks an article as favorite.
func (s *RSSSyncState) SetFavorite(articleID string, favorite bool, now int64) {
	s.Normalize()
	if !favorite {
		s.Favorites[articleID] = RSSFavoriteRecord{
			ArticleID: articleID,
			UpdatedAt: now,
			Deleted:   true,
			DeletedAt: now,
		}
	} else {
		favAt := now
		if old, exists := s.Favorites[articleID]; exists && old.FavoritedAt != 0 {
			favAt = old.FavoritedAt
		}
		s.Favorites[articleID] = RSSFavoriteRecord{
			ArticleID:   articleID,
			FavoritedAt: favAt,
			UpdatedAt:   now,
			Deleted:     false,
		}
	}
	if now > s.UpdatedAt {
		s.UpdatedAt = now
	}
}

// MergeRSSSync merges multiple RSSSyncState instances using Last-Write-Wins (LWW)
// and monotonic absorption for read records.
func MergeRSSSync(states ...RSSSyncState) RSSSyncState {
	var merged RSSSyncState
	var primaryDevice string
	initialized := false

	for _, s := range states {
		s.Normalize()
		if !initialized {
			primaryDevice = s.DeviceID
			merged = NewRSSSyncState(primaryDevice)
			initialized = true
		}
		if s.UpdatedAt > merged.UpdatedAt {
			merged.UpdatedAt = s.UpdatedAt
		}

		// Merge Feeds (LWW with tombstone awareness)
		for id, feed := range s.Feeds {
			old, exists := merged.Feeds[id]
			if !exists || feed.UpdatedAt >= old.UpdatedAt {
				merged.Feeds[id] = feed
			}
		}

		// Merge Groups (LWW with tombstone awareness)
		for id, grp := range s.Groups {
			old, exists := merged.Groups[id]
			if !exists || grp.UpdatedAt >= old.UpdatedAt {
				merged.Groups[id] = grp
			}
		}

		// Merge ReadStates (monotonic absorption: once read, stays read)
		for artID, byDevice := range s.ReadStates {
			if merged.ReadStates[artID] == nil {
				merged.ReadStates[artID] = make(map[string]RSSReadRecord)
			}
			for dev, rec := range byDevice {
				old, exists := merged.ReadStates[artID][dev]
				if !exists || (rec.Read && !old.Read) || (rec.Read == old.Read && rec.UpdatedAt >= old.UpdatedAt) {
					merged.ReadStates[artID][dev] = rec
				}
			}
		}

		// Merge Favorites (LWW with tombstone awareness)
		for artID, fav := range s.Favorites {
			old, exists := merged.Favorites[artID]
			if !exists || fav.UpdatedAt >= old.UpdatedAt {
				merged.Favorites[artID] = fav
			}
		}

		// Merge Notes (LWW with tombstone awareness)
		for noteID, note := range s.Notes {
			old, exists := merged.Notes[noteID]
			if !exists || note.UpdatedAt >= old.UpdatedAt {
				merged.Notes[noteID] = note
			}
		}
	}

	if primaryDevice == "" {
		primaryDevice = "local"
	}
	merged.DeviceID = primaryDevice
	return merged
}

// ======================================================================================
// IDENTIFIERS & NORMALIZATION HELPERS
// ======================================================================================

// CanonicalURL normalizes a URL by trimming whitespace, prepending a default https://
// scheme when missing, lowercasing scheme and host, removing default ports, removing
// trailing slashes, stripping tracking query parameters, and discarding fragments.
func CanonicalURL(rawURL string) string {
	raw := strings.TrimSpace(rawURL)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	} else if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	// Strip default ports
	if parsed.Scheme == "http" && strings.HasSuffix(parsed.Host, ":80") {
		parsed.Host = strings.TrimSuffix(parsed.Host, ":80")
	} else if parsed.Scheme == "https" && strings.HasSuffix(parsed.Host, ":443") {
		parsed.Host = strings.TrimSuffix(parsed.Host, ":443")
	}
	// Normalize path trailing slash
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	// Strip common marketing / tracking query parameters
	if parsed.RawQuery != "" {
		values := parsed.Query()
		for key := range values {
			k := strings.ToLower(key)
			if strings.HasPrefix(k, "utm_") || k == "fbclid" || k == "gclid" || k == "msclkid" {
				values.Del(key)
			}
		}
		parsed.RawQuery = values.Encode()
	}
	parsed.Fragment = ""
	return parsed.String()
}

// resolveAssetURL resolves a relative asset reference (e.g. an image src) against base.
// Absolute references and empty inputs are returned unchanged.
func resolveAssetURL(base, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" || base == "" {
		return ref
	}
	parsedRef, err := url.Parse(ref)
	if err != nil || parsedRef.IsAbs() {
		return ref
	}
	parsedBase, err := url.Parse(base)
	if err != nil || parsedBase.Scheme == "" {
		return ref
	}
	return parsedBase.ResolveReference(parsedRef).String()
}

// FeedID produces a deterministic, collision-resistant feed identifier from a canonical feed URL.
// Example: "feed:a1b2c3d4e5f60718"
func FeedID(canonicalFeedURL string) string {
	h := sha256.Sum256([]byte(canonicalFeedURL))
	return fmt.Sprintf("feed:%x", h[:8])
}

// ArticleDeduplicationKey determines the primary deduplication key for an item,
// prioritizing the feed's unique GUID, falling back to canonical Link, then Title.
func ArticleDeduplicationKey(guid, link, title string) string {
	g := strings.TrimSpace(guid)
	if g != "" {
		return g
	}
	l := strings.TrimSpace(link)
	if l != "" {
		return CanonicalURL(l)
	}
	t := strings.TrimSpace(title)
	if t != "" {
		return t
	}
	return "empty"
}

// ArticleID generates a deterministic, stable global identifier for an article.
// Example: "art:feed:a1b2c3d4e5f60718:9f8e7d6c5b4a3210"
func ArticleID(feedID, dedupKey string) string {
	h := sha256.Sum256([]byte(dedupKey))
	return fmt.Sprintf("art:%s:%x", feedID, h[:8])
}

// ParagraphHash generates a short content hash for an ArticleBlock to anchor notes.
func ParagraphHash(blockType BlockType, text, u string) string {
	h := sha256.Sum256([]byte(string(blockType) + ":" + strings.TrimSpace(text) + ":" + strings.TrimSpace(u)))
	return fmt.Sprintf("%x", h[:8])
}

// ParseBlocksJSON deserializes JSON-encoded blocks.
func ParseBlocksJSON(raw string) []ArticleBlock {
	if raw == "" || raw == "[]" {
		return nil
	}
	var blocks []ArticleBlock
	if err := json.Unmarshal([]byte(raw), &blocks); err != nil {
		return nil
	}
	return blocks
}

// EncodeBlocksJSON serializes blocks into a JSON string.
func EncodeBlocksJSON(blocks []ArticleBlock) string {
	if len(blocks) == 0 {
		return ""
	}
	data, err := json.Marshal(blocks)
	if err != nil {
		return ""
	}
	return string(data)
}
