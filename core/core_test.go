package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDedupeVideos(t *testing.T) {
	v := []Video{
		{ID: "a", Title: "old", PublishedAt: "2026-01-01T00:00:00Z"},
		{ID: "b", Title: "other", PublishedAt: "2026-01-03T00:00:00Z"},
		{ID: "a", Title: "new", PublishedAt: "2026-01-02T00:00:00Z"},
	}
	got := DedupeVideos(v)
	if len(got) != 2 || got[0].Title != "other" || got[1].Title != "new" {
		t.Fatalf("dedupe: %+v", got)
	}
}

func TestApplyRetention(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	v := []Video{
		{ID: "old", PublishedAt: "2026-01-01T00:00:00Z"},
		{ID: "new", PublishedAt: "2026-03-01T00:00:00Z"},
		{ID: "protected", PublishedAt: "2026-01-01T00:00:00Z"},
	}
	got := ApplyRetention(v, func(v Video) bool { return v.ID == "protected" }, now, DefaultRetention())
	if len(got) != 2 || got[0].ID != "new" || got[1].ID != "protected" {
		t.Fatalf("retention: %+v", got)
	}
}

func TestIsShortAndDuration(t *testing.T) {
	cases := []struct {
		d    string
		w, h int
		want bool
	}{
		{"PT2M30S", 1920, 1080, true},
		{"PT4M0S", 1920, 1080, false},
		{"PT10M0S", 1080, 1920, true},
		{"", 1080, 1920, true},
	}
	for _, c := range cases {
		if got := IsShort(c.d, c.w, c.h); got != c.want {
			t.Errorf("IsShort(%s,%d,%d)=%v want %v", c.d, c.w, c.h, got, c.want)
		}
	}
	if s, ok := DurationSeconds("PT1H2M3S"); !ok || s != 3723 {
		t.Fatalf("duration %d %v", s, ok)
	}
}

func TestRefreshQuota(t *testing.T) {
	if got := RefreshQuota(89, 2, 3); got != 272 {
		t.Fatalf("quota = %d", got)
	}
	if got := RefreshQuota(1, 0, 0); got != 2 {
		t.Fatalf("quota = %d", got)
	}
}

func TestRelativeDate(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct{ raw, want string }{
		{"2026-03-01T11:59:30Z", "just now"},
		{"2026-03-01T11:58:00Z", "2m ago"},
		{"2026-03-01T09:00:00Z", "3h ago"},
		{"2026-02-27T12:00:00Z", "2d ago"},
		{"2026-01-20T12:00:00Z", "5w ago"},
	}
	for _, c := range cases {
		if got := RelativeDate(c.raw, now); got != c.want {
			t.Errorf("relative(%s)=%s want %s", c.raw, got, c.want)
		}
	}
}

func TestParseOPML(t *testing.T) {
	data := []byte(`<?xml version="1.0"?><opml><body>
	<outline text="News"><outline text="Tech" title="Tech"><outline text="Ch A" title="Ch A" type="rss" xmlUrl="https://www.youtube.com/feeds/videos.xml?channel_id=UC_A"/></outline></outline>
	<outline text="Other"><outline text="Ch A" title="Ch A" type="rss" xmlUrl="https://www.youtube.com/feeds/videos.xml?channel_id=UC_A"/></outline>
	</body></opml>`)
	groups, channels, err := ParseOPML(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 3 {
		t.Fatalf("groups: %+v", groups)
	}
	if len(channels) != 1 || channels[0].ID != "UC_A" || len(channels[0].GroupIDs) != 3 {
		t.Fatalf("channels: %+v", channels)
	}
	for _, g := range groups {
		if g.Name == "News" {
			continue
		}
		if len(g.ChannelIDs) != 1 || g.ChannelIDs[0] != "UC_A" {
			t.Fatalf("group membership: %+v", g)
		}
	}

}

func TestSyncMerge(t *testing.T) {
	a := newSyncState("A")
	b := newSyncState("B")
	a.Groups["g"] = ChannelGroup{ID: "g", Name: "old", UpdatedAt: 10}
	b.Groups["g"] = ChannelGroup{ID: "g", Name: "new", UpdatedAt: 20}
	a.ReadStates["v"] = map[string]ReadRecord{"A": {Read: false, UpdatedAt: 30}}
	b.ReadStates["v"] = map[string]ReadRecord{"B": {Read: true, UpdatedAt: 5}}
	merged := MergeSync(a, b)
	if merged.Groups["g"].Name != "new" {
		t.Fatalf("group LWW: %+v", merged.Groups)
	}
	if !merged.IsRead("v") {
		t.Fatalf("any device read wins: %+v", merged.ReadStates)
	}
}

func TestSyncRenameGroup(t *testing.T) {
	s := newSyncState("A")
	s.Groups["g"] = ChannelGroup{ID: "g", Name: "Tech", ChannelIDs: []string{"UC_1"}, UpdatedAt: 10}
	if !s.RenameGroup("g", "Technology", 50) {
		t.Fatal("rename existing group should return true")
	}
	if s.Groups["g"].Name != "Technology" || s.Groups["g"].UpdatedAt != 50 {
		t.Fatalf("renamed group state: %+v", s.Groups["g"])
	}
	if s.UpdatedAt != 50 {
		t.Fatalf("sync UpdatedAt = %d, want 50", s.UpdatedAt)
	}
	if s.RenameGroup("nonexistent", "Tech", 60) {
		t.Fatal("rename nonexistent group should return false")
	}
	if s.RenameGroup("g", "   ", 60) {
		t.Fatal("rename group to blank should return false")
	}
	if s.Groups["g"].Name != "Technology" {
		t.Fatalf("blank rename modified group name: %+v", s.Groups["g"])
	}
}

func TestSyncAtomicAndConflictMerge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sync.json")
	s := newSyncState("A")
	s.Groups["g"] = ChannelGroup{ID: "g", Name: "main", UpdatedAt: 10}
	if err := SaveSyncAtomic(path, s); err != nil {
		t.Fatal(err)
	}
	conflict := newSyncState("B")
	conflict.Groups["h"] = ChannelGroup{ID: "h", Name: "conflict", UpdatedAt: 20}
	data, _ := json.Marshal(conflict)
	if err := os.WriteFile(path+".sync-conflict-1", data, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSyncWithConflicts(path, "A")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Groups) != 2 {
		t.Fatalf("merged groups: %+v", got.Groups)
	}
	cleaned := false
	CleanupSyncConflicts(path, func(p string) bool { cleaned = strings.Contains(p, "sync-conflict"); return cleaned })
	if !cleaned {
		t.Fatal("conflict copy cleanup not invoked")
	}
}

func TestFavoritePreservesTime(t *testing.T) {
	s := newSyncState("A")
	v := Video{ID: "v", Title: "one"}
	now := time.Now().UnixMilli()
	s.SetFavorite(v, true, now)
	first := s.Favorites["v"].FavoritedAt
	v.Title = "two"
	s.SetFavorite(v, true, now+100)
	if s.Favorites["v"].FavoritedAt != first || s.Favorites["v"].Video.Title != "two" {
		t.Fatalf("favorite upsert: %+v", s.Favorites["v"])
	}
}

func TestStableGroupID(t *testing.T) {
	if !strings.HasPrefix(StableGroupID("News"), "group-") || StableGroupID("News") == StableGroupID("Sports") {
		t.Fatal("unstable group id")
	}
	if !reflect.DeepEqual(StableGroupID("News"), StableGroupID("News")) {
		t.Fatal("nondeterministic group id")
	}
}
