package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"hash/fnv"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Channel struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	GroupIDs   []string `json:"groupIDs"`
	AvatarURL  string   `json:"avatarURL,omitempty"`
	UploadedAt string   `json:"uploadedAt,omitempty"`
	UpdatedAt  int64    `json:"updatedAt"`
}

type ChannelGroup struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	ChannelIDs []string `json:"channelIDs"`
	UpdatedAt  int64    `json:"updatedAt"`
}

type Video struct {
	ID              string `json:"id"`
	ChannelID       string `json:"channelID"`
	ChannelTitle    string `json:"channelTitle"`
	Title           string `json:"title"`
	Description     string `json:"description"`
	ThumbnailURL    string `json:"thumbnailURL"`
	AvatarURL       string `json:"avatarURL,omitempty"`
	PublishedAt     string `json:"publishedAt"`
	Duration        string `json:"duration,omitempty"`
	Width           int    `json:"width,omitempty"`
	Height          int    `json:"height,omitempty"`
	IsShort         bool   `json:"isShort"`
	FavoritedAt     int64  `json:"favoritedAt,omitempty"`
	FavoriteChannel string `json:"favoriteChannel,omitempty"`
}

type RefreshFailure struct {
	At      string `json:"at"`
	Message string `json:"message"`
}

type Settings struct {
	APIKey           string           `json:"apiKey"`
	RefreshMinutes   int              `json:"refreshMinutes"`
	SyncPath         string           `json:"syncPath"`
	BackupPath       string           `json:"backupPath"`
	ShowShorts       bool             `json:"showShorts"`
	NotificationIDs  []string         `json:"notificationChannelIDs"`
	LastRefresh      string           `json:"lastRefresh,omitempty"`
	NextRefresh      string           `json:"nextRefresh,omitempty"`
	RecentFailures   []RefreshFailure `json:"recentFailures"`
	QuotaPausedUntil string           `json:"quotaPausedUntil,omitempty"`
	LastHandoffID    string           `json:"lastHandoffID,omitempty"`
	HelperInstalled  bool             `json:"helperInstalled,omitempty"`

	// Local UI preferences. These stay in settings.json and are never synced.
	SidebarWidth   float32          `json:"sidebarWidth,omitempty"`
	DetailsWidth   float32          `json:"detailsWidth,omitempty"`
	MaxGridColumns int              `json:"maxGridColumns,omitempty"`
	ExpandedGroups map[string]bool `json:"expandedGroups,omitempty"`
}

type FavoriteRecord struct {
	VideoID     string `json:"videoID"`
	FavoritedAt int64  `json:"favoritedAt"`
	Video       *Video `json:"video,omitempty"`
	UpdatedAt   int64  `json:"updatedAt"`
}

type ReadRecord struct {
	VideoID   string `json:"videoID"`
	DeviceID  string `json:"deviceID"`
	Read      bool   `json:"read"`
	UpdatedAt int64  `json:"updatedAt"`
}

type SyncState struct {
	SchemaVersion int                              `json:"schemaVersion"`
	DeviceID      string                           `json:"deviceID"`
	UpdatedAt     int64                            `json:"updatedAt"`
	Groups        map[string]ChannelGroup          `json:"groups"`
	Channels      map[string]Channel               `json:"channels"`
	ReadStates    map[string]map[string]ReadRecord `json:"readStates"`
	Favorites     map[string]FavoriteRecord        `json:"favorites"`
}

func newSyncState(deviceID string) SyncState {
	return SyncState{SchemaVersion: 1, DeviceID: deviceID, Groups: map[string]ChannelGroup{}, Channels: map[string]Channel{}, ReadStates: map[string]map[string]ReadRecord{}, Favorites: map[string]FavoriteRecord{}}
}

func normalizeSync(s *SyncState) {
	if s.Groups == nil {
		s.Groups = map[string]ChannelGroup{}
	}
	if s.Channels == nil {
		s.Channels = map[string]Channel{}
	}
	if s.ReadStates == nil {
		s.ReadStates = map[string]map[string]ReadRecord{}
	}
	if s.Favorites == nil {
		s.Favorites = map[string]FavoriteRecord{}
	}
	if s.SchemaVersion == 0 {
		s.SchemaVersion = 1
	}
}

func after(a, b int64) bool { return a >= b }

func MergeSync(states ...SyncState) SyncState {
	var device string
	var merged SyncState
	has := false
	for _, s := range states {
		normalizeSync(&s)
		if !has {
			merged = newSyncState(s.DeviceID)
			device = s.DeviceID
			has = true
		}
		if s.UpdatedAt > merged.UpdatedAt {
			merged.UpdatedAt = s.UpdatedAt
		}
		for id, g := range s.Groups {
			if old, ok := merged.Groups[id]; !ok || after(g.UpdatedAt, old.UpdatedAt) {
				merged.Groups[id] = g
			}
		}
		for id, ch := range s.Channels {
			if old, ok := merged.Channels[id]; !ok || after(ch.UpdatedAt, old.UpdatedAt) {
				merged.Channels[id] = ch
			}
		}
		for id, fav := range s.Favorites {
			if old, ok := merged.Favorites[id]; !ok || after(fav.UpdatedAt, old.UpdatedAt) {
				merged.Favorites[id] = fav
			}
		}
		for videoID, byDevice := range s.ReadStates {
			for dev, rec := range byDevice {
				if merged.ReadStates[videoID] == nil {
					merged.ReadStates[videoID] = map[string]ReadRecord{}
				}
				old, ok := merged.ReadStates[videoID][dev]
				if !ok || (rec.Read && !old.Read) || (rec.Read == old.Read && after(rec.UpdatedAt, old.UpdatedAt)) {
					merged.ReadStates[videoID][dev] = rec
				}
			}
		}
	}
	if device == "" {
		device = "unknown"
	}
	merged.DeviceID = device
	return merged
}

func (s *SyncState) IsRead(videoID string) bool {
	for _, r := range s.ReadStates[videoID] {
		if r.Read {
			return true
		}
	}
	return false
}

func (s *SyncState) MarkRead(videoID string, now int64) {
	normalizeSync(s)
	if s.ReadStates[videoID] == nil {
		s.ReadStates[videoID] = map[string]ReadRecord{}
	}
	s.ReadStates[videoID][s.DeviceID] = ReadRecord{VideoID: videoID, DeviceID: s.DeviceID, Read: true, UpdatedAt: now}
	s.UpdatedAt = now
}

func (s *SyncState) SetFavorite(v Video, favorite bool, now int64) {
	normalizeSync(s)
	if !favorite {
		delete(s.Favorites, v.ID)
		s.UpdatedAt = now
		return
	}
	old := s.Favorites[v.ID]
	at := now
	if old.FavoritedAt != 0 {
		at = old.FavoritedAt
	}
	v.FavoritedAt = at
	s.Favorites[v.ID] = FavoriteRecord{VideoID: v.ID, FavoritedAt: at, Video: &v, UpdatedAt: now}
	s.UpdatedAt = now
}

func (s *SyncState) MergeImported(groups []ChannelGroup, channels []Channel, now int64) {
	normalizeSync(s)
	if s.Groups == nil {
		s.Groups = map[string]ChannelGroup{}
	}
	if s.Channels == nil {
		s.Channels = map[string]Channel{}
	}
	for _, g := range groups {
		g.UpdatedAt = now
		old := s.Groups[g.ID]
		seen := map[string]bool{}
		ids := append([]string{}, old.ChannelIDs...)
		for _, id := range g.ChannelIDs {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
		g.ChannelIDs = ids
		s.Groups[g.ID] = g
	}
	for _, ch := range channels {
		ch.UpdatedAt = now
		old := s.Channels[ch.ID]
		if old.Title != "" {
			ch.Title = old.Title
		}
		if old.AvatarURL != "" {
			ch.AvatarURL = old.AvatarURL
		}
		if old.UploadedAt != "" {
			ch.UploadedAt = old.UploadedAt
		}
		seen := map[string]bool{}
		ids := append([]string{}, old.GroupIDs...)
		for _, id := range ch.GroupIDs {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
		ch.GroupIDs = ids
		s.Channels[ch.ID] = ch
	}
	s.UpdatedAt = now
}

func (s *SyncState) ReplaceGroups(groups []ChannelGroup, channels []Channel, now int64) {
	normalizeSync(s)
	s.Groups = map[string]ChannelGroup{}
	s.Channels = map[string]Channel{}
	for _, g := range groups {
		g.UpdatedAt = now
		s.Groups[g.ID] = g
	}
	for _, ch := range channels {
		ch.UpdatedAt = now
		s.Channels[ch.ID] = ch
	}
	s.UpdatedAt = now
}

func (s *SyncState) FavoritesSorted() []Video {
	var out []Video
	for _, f := range s.Favorites {
		if f.Video != nil {
			out = append(out, *f.Video)
		} else {
			out = append(out, Video{ID: f.VideoID, FavoritedAt: f.FavoritedAt})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FavoritedAt > out[j].FavoritedAt })
	return out
}

func StableGroupID(name string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(name))
	return fmt.Sprintf("group-%x", h.Sum64())
}

type opmlOutline struct {
	Text     string        `xml:"text,attr"`
	Title    string        `xml:"title,attr"`
	Type     string        `xml:"type,attr"`
	XMLURL   string        `xml:"xmlUrl,attr"`
	Children []opmlOutline `xml:"outline"`
}

type opmlDocument struct {
	Body struct {
		Outlines []opmlOutline `xml:"outline"`
	} `xml:"body"`
}

func ParseOPMLFile(path string) ([]ChannelGroup, []Channel, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	return ParseOPML(data)
}

func ParseOPML(data []byte) ([]ChannelGroup, []Channel, error) {
	var doc opmlDocument
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, nil, err
	}
	var groups []ChannelGroup
	var channels []Channel
	seenGroups := map[string]int{}
	seenChannels := map[string]int{}
	var walk func([]opmlOutline, []string, []string)
	walk = func(outlines []opmlOutline, parents []string, groupIDs []string) {
		for _, o := range outlines {
			name := o.Title
			if name == "" {
				name = o.Text
			}
			if o.XMLURL == "" {
				if name == "" {
					continue
				}
				path := append(append([]string{}, parents...), name)
				id := StableGroupID(name)
				if _, ok := seenGroups[id]; !ok {
					groups = append(groups, ChannelGroup{ID: id, Name: name})
					seenGroups[id] = len(groups) - 1
				}
				walk(o.Children, path, append(append([]string{}, groupIDs...), id))
				for _, gid := range groupIDs {
					for i := range groups {
						if groups[i].ID != gid {
							continue
						}
						for _, childID := range groups[len(groups)-1].ChannelIDs {
							dup := false
							for _, id := range groups[i].ChannelIDs {
								if id == childID {
									dup = true
									break
								}
							}
							if !dup {
								groups[i].ChannelIDs = append(groups[i].ChannelIDs, childID)
							}
						}
					}
				}
				continue
			}
			channelID := youTubeChannelID(o.XMLURL)
			if channelID == "" {
				continue
			}
			idx, ok := seenChannels[channelID]
			if !ok {
				channels = append(channels, Channel{ID: channelID, Title: name})
				idx = len(channels) - 1
				seenChannels[channelID] = idx
			}
			for _, gid := range groupIDs {
				found := false
				for _, existing := range channels[idx].GroupIDs {
					if existing == gid {
						found = true
						break
					}
				}
				if !found {
					channels[idx].GroupIDs = append(channels[idx].GroupIDs, gid)
				}
				for i := range groups {
					if groups[i].ID == gid {
						dup := false
						for _, id := range groups[i].ChannelIDs {
							if id == channelID {
								dup = true
								break
							}
						}
						if !dup {
							groups[i].ChannelIDs = append(groups[i].ChannelIDs, channelID)
						}
					}
				}
			}
			walk(o.Children, parents, groupIDs)
		}
	}
	walk(doc.Body.Outlines, nil, nil)
	return groups, channels, nil
}

func youTubeChannelID(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Host != "www.youtube.com" && u.Host != "youtube.com") {
		return ""
	}
	return u.Query().Get("channel_id")
}

func DedupeVideos(videos []Video) []Video {
	byID := map[string]Video{}
	for _, v := range videos {
		if old, ok := byID[v.ID]; !ok || v.PublishedAt > old.PublishedAt {
			byID[v.ID] = v
		}
	}
	out := make([]Video, 0, len(byID))
	for _, v := range byID {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PublishedAt > out[j].PublishedAt })
	return out
}

type RetentionPolicy struct {
	Hours     int
	MaxVideos int
}

func DefaultRetention() RetentionPolicy { return RetentionPolicy{Hours: 30, MaxVideos: 400} }

func ApplyRetention(videos []Video, protected func(Video) bool, now time.Time, p RetentionPolicy) []Video {
	cutoff := now.Add(-time.Duration(p.Hours) * time.Hour)
	out := make([]Video, 0, len(videos))
	for _, v := range videos {
		t, err := time.Parse(time.RFC3339, v.PublishedAt)
		if protected(v) || err == nil && !t.Before(cutoff) {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PublishedAt > out[j].PublishedAt })
	if len(out) > p.MaxVideos {
		out = out[:p.MaxVideos]
	}
	return out
}

func RefreshQuota(channelCount, pagesPerChannel, hydrationRequests int) int {
	if channelCount < 0 {
		channelCount = 0
	}
	if pagesPerChannel < 0 {
		pagesPerChannel = 0
	}
	if hydrationRequests < 0 {
		hydrationRequests = 0
	}
	return (channelCount+49)/50 + channelCount*(1+maxInt(pagesPerChannel, 0)) + hydrationRequests
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func IsShort(duration string, width, height int) bool {
	if height > width && height > 0 {
		return true
	}
	sec, ok := DurationSeconds(duration)
	return ok && sec <= 180
}

func DurationSeconds(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	s = strings.TrimPrefix(s, "PT")
	total := 0.0
	num := ""
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9' || r == '.':
			num += string(r)
		case r == 'H' || r == 'M' || r == 'S':
			if num == "" {
				return 0, false
			}
			n, err := strconv.ParseFloat(num, 64)
			if err != nil {
				return 0, false
			}
			switch r {
			case 'H':
				total += n * 3600
			case 'M':
				total += n * 60
			case 'S':
				total += n
			}
			num = ""
		default:
			return 0, false
		}
	}
	if num != "" {
		return 0, false
	}
	return int(math.Floor(total)), true
}

func RelativeDate(raw string, now time.Time) string {
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return ""
	}
	d := now.Sub(t)
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	days := int(d.Hours() / 24)
	if days < 35 {
		return fmt.Sprintf("%dd ago", days)
	}
	if days < 365 {
		return fmt.Sprintf("%dw ago", days/7)
	}
	months := days / 30
	if months < 12 {
		return fmt.Sprintf("%dmo ago", months)
	}
	return fmt.Sprintf("%dy ago", days/365)
}

func StableDeviceID() string {
	host, _ := os.Hostname()
	sum := sha256.Sum256([]byte(host + "|" + os.Getenv("USER")))
	return hex.EncodeToString(sum[:8])
}

type SyncLock struct {
	fd   *os.File
	path string
}

func AcquireLock(dir string) (*SyncLock, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, ".youtube-reader.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := flockExclusive(f, true); err != nil {
		f.Close()
		return nil, err
	}
	return &SyncLock{fd: f, path: path}, nil
}

func (l *SyncLock) Release() error {
	if l == nil || l.fd == nil {
		return nil
	}
	err := flockRelease(l.fd)
	cerr := l.fd.Close()
	l.fd = nil
	if err != nil {
		return err
	}
	return cerr
}

func SaveSyncAtomic(path string, state SyncState) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".youtube-reader-sync-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func LoadSyncWithConflicts(path string, deviceID string) (SyncState, error) {
	states := []SyncState{}
	paths := []string{path}
	matches, _ := filepath.Glob(path + ".sync-conflict-*")
	paths = append(paths, matches...)
	base, _ := filepath.Glob(filepath.Dir(path) + "/base~*.json")
	paths = append(paths, base...)
	found := false
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var s SyncState
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}
		normalizeSync(&s)
		if s.DeviceID == "" {
			s.DeviceID = deviceID
		}
		states = append(states, s)
		found = true
	}
	if !found {
		return newSyncState(deviceID), nil
	}
	merged := MergeSync(states...)
	merged.DeviceID = deviceID
	return merged, nil
}

func CleanupSyncConflicts(path string, appGenerated func(string) bool) {
	matches, _ := filepath.Glob(path + ".sync-conflict-*")
	for _, p := range matches {
		if appGenerated(p) {
			os.Remove(p)
		}
	}
}
