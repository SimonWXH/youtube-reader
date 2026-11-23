package main

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/simon/youtube-reader/core"
)

func TestCreateGroupGivesEveryGroupItsOwnID(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	defer a.Close()

	first, err := a.CreateGroup("Fun")
	if err != nil {
		t.Fatalf("create the first group: %v", err)
	}
	second, err := a.CreateGroup("Fun")
	if err != nil {
		t.Fatalf("create the second group: %v", err)
	}
	if first.ID == second.ID {
		t.Fatalf("two groups named Fun share the ID %q", first.ID)
	}
	if first.ID == core.StableGroupID("Fun") {
		t.Fatal("a group the reader creates reused the name-derived OPML ID")
	}
	if len(a.Sync.Groups) != 2 {
		t.Fatalf("groups = %d; want 2", len(a.Sync.Groups))
	}
	if _, ok := a.Sync.Groups[first.ID]; !ok {
		t.Fatal("the first group is not in the sync state")
	}
	if len(a.Groups) != 2 || a.Groups[0].ID != first.ID || a.Groups[1].ID != second.ID {
		t.Fatalf("the sidebar's order = %+v; want the created order", a.Groups)
	}
	if got := a.settingsSnapshot().GroupOrder; len(got) != 2 || got[0] != first.ID || got[1] != second.ID {
		t.Fatalf("stored group order = %q; want %q", got, []string{first.ID, second.ID})
	}
}

func TestCreateGroupRefusesAnEmptyName(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	defer a.Close()
	if _, err := a.CreateGroup("   "); err == nil {
		t.Fatal("an unnamed group was created")
	}
	if len(a.Sync.Groups) != 0 {
		t.Fatalf("groups = %d; want none", len(a.Sync.Groups))
	}
}

func TestDeleteGroupOnlyRemovesAnEmptyGroup(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	defer a.Close()
	empty, err := a.CreateGroup("Empty")
	if err != nil {
		t.Fatal(err)
	}
	used, err := a.CreateGroup("Used")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.AddSubscription(core.Channel{ID: "ch-1", Title: "Channel"}, used.ID); err != nil {
		t.Fatalf("add the channel: %v", err)
	}
	if err := a.DeleteGroup(used.ID); err != nil {
		t.Fatalf("delete the used group: %v", err)
	}
	if _, ok := a.Sync.Groups[used.ID]; !ok {
		t.Fatal("a group that holds a channel was deleted")
	}
	if err := a.DeleteGroup(empty.ID); err != nil {
		t.Fatalf("delete the empty group: %v", err)
	}
	if _, ok := a.Sync.Groups[empty.ID]; ok {
		t.Fatal("the empty group was not deleted")
	}
	for _, id := range a.settingsSnapshot().GroupOrder {
		if id == empty.ID {
			t.Fatal("the deleted group is still in the stored order")
		}
	}
}

func TestAddSubscriptionWritesTheChannelIntoTheLibrary(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	defer a.Close()
	group, err := a.CreateGroup("Fun")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.AddSubscription(core.Channel{ID: "ch-1", Title: "Channel"}, group.ID); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, ok := a.Sync.Channels["ch-1"]; !ok {
		t.Fatal("the channel is not in the sync state")
	}
	found := false
	for _, channel := range a.Channels {
		if channel.ID == "ch-1" && channel.Title == "Channel" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the channel is not in the library the sidebar reads: %+v", a.Channels)
	}
	if !containsString(a.Sync.Groups[group.ID].ChannelIDs, "ch-1") {
		t.Fatalf("group membership = %q; want the new channel", a.Sync.Groups[group.ID].ChannelIDs)
	}
	if got := a.Sync.Channels["ch-1"].GroupIDs; len(got) != 1 || got[0] != group.ID {
		t.Fatalf("channel groups = %q; want only %q", got, group.ID)
	}
}

func TestAddSubscriptionRefusesAChannelWithoutATitle(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	defer a.Close()
	if err := a.AddSubscription(core.Channel{ID: "ch-1"}, ""); err == nil {
		t.Fatal("a channel without a title was added")
	}
	if len(a.Sync.Channels) != 0 {
		t.Fatalf("channels = %d; want none", len(a.Sync.Channels))
	}
}

func TestAddSubscriptionRefusesADuplicate(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	defer a.Close()
	if err := a.AddSubscription(core.Channel{ID: "ch-1", Title: "Channel"}, ""); err != nil {
		t.Fatal(err)
	}
	if err := a.AddSubscription(core.Channel{ID: "ch-1", Title: "Channel"}, ""); err == nil {
		t.Fatal("the same channel was subscribed twice")
	}
}

func TestMoveChannelToGroupKeepsOneMembership(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	defer a.Close()
	from, err := a.CreateGroup("Fun")
	if err != nil {
		t.Fatal(err)
	}
	to, err := a.CreateGroup("GameDev")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.AddSubscription(core.Channel{ID: "ch-1", Title: "Channel"}, from.ID); err != nil {
		t.Fatal(err)
	}
	// A notification choice follows the channel, not the group.
	if _, err := a.updateSettings(func(s *core.Settings) bool {
		s.NotificationIDs = []string{"ch-1"}
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.MoveChannelToGroup("ch-1", to.ID); err != nil {
		t.Fatalf("move: %v", err)
	}
	if containsString(a.Sync.Groups[from.ID].ChannelIDs, "ch-1") {
		t.Fatal("the channel is still in the group it moved out of")
	}
	if !containsString(a.Sync.Groups[to.ID].ChannelIDs, "ch-1") {
		t.Fatal("the channel is not in the group it moved to")
	}
	if got := a.Sync.Channels["ch-1"].GroupIDs; len(got) != 1 || got[0] != to.ID {
		t.Fatalf("channel groups = %q; want only %q", got, to.ID)
	}
	if !a.notificationEnabled("ch-1") {
		t.Fatal("the move dropped the channel's notification choice")
	}

	if _, err := a.MoveChannelToGroup("ch-1", ""); err != nil {
		t.Fatalf("move to ungrouped: %v", err)
	}
	if containsString(a.Sync.Groups[to.ID].ChannelIDs, "ch-1") {
		t.Fatal("the channel moved to ungrouped is still in its group")
	}
	if got := a.Sync.Channels["ch-1"].GroupIDs; len(got) != 0 {
		t.Fatalf("channel groups after moving to ungrouped = %q; want none", got)
	}
}

func TestAddedChannelFetchSkipsQuietlyWhenTheQuotaIsExhausted(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	defer a.Close()
	if _, err := a.updateSettings(func(s *core.Settings) bool {
		s.APIKey = "key"
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.AddSubscription(core.Channel{ID: "ch-1", Title: "Channel"}, ""); err != nil {
		t.Fatal(err)
	}
	a.NoteQuotaPause(time.Now())
	var hits atomic.Int32
	previous := appYouTubeClient
	appYouTubeClient = func(apiKey string) core.YouTube {
		hits.Add(1)
		return core.YouTube{Key: apiKey}
	}
	defer func() { appYouTubeClient = previous }()

	w := &Window{app: a, selectedIndex: new(int)}
	w.fetchAddedChannel(core.Channel{ID: "ch-1", Title: "Channel"})
	if hits.Load() != 0 {
		t.Fatalf("the fetch built %d clients while paused; want none", hits.Load())
	}
	if _, ok := a.Sync.Channels["ch-1"]; !ok {
		t.Fatal("the add was undone by the skipped fetch")
	}
}

func TestApplyAddedChannelFetchCommitsOnlyThatChannel(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	defer a.Close()
	group, err := a.CreateGroup("Fun")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.AddSubscription(core.Channel{ID: "ch-2", Title: "Second"}, group.ID); err != nil {
		t.Fatal(err)
	}
	if err := a.AddSubscription(core.Channel{ID: "ch-1", Title: "First"}, ""); err != nil {
		t.Fatal(err)
	}
	updated := core.Channel{ID: "ch-2", Title: "Second renamed", AvatarURL: "http://avatar", UploadedAt: "uploads-2"}
	videos := []core.Video{{ID: "v2", ChannelID: "ch-2", ChannelTitle: "Second renamed", Title: "Episode", PublishedAt: time.Now().UTC().Format(time.RFC3339), Duration: "PT8M"}}
	if !a.ApplyChannelFetch(updated, videos) {
		t.Fatal("the fetch of the added channel did not commit")
	}
	if got := a.Sync.Channels["ch-2"].Title; got != "Second renamed" {
		t.Fatalf("channel title = %q; want the fetched one", got)
	}
	if got := a.Sync.Channels["ch-2"].GroupIDs; len(got) != 1 || got[0] != group.ID {
		t.Fatalf("the fetch changed the channel's group: %q", got)
	}
	found := false
	for _, video := range a.Videos {
		if video.ID == "v2" && video.ChannelID == "ch-2" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the fetched video is not in the library: %+v", a.Videos)
	}
	if len(a.Videos) != 1 {
		t.Fatalf("videos = %d; the fetch must add only the one channel's", len(a.Videos))
	}
	if _, ok := a.Sync.Channels["ch-1"]; !ok {
		t.Fatal("the fetch dropped the library's other channel")
	}
}

func TestApplyAddedChannelFetchIgnoresAnUnknownChannel(t *testing.T) {
	a := newTestApp(t, t.TempDir())
	defer a.Close()
	if a.ApplyChannelFetch(core.Channel{ID: "ch-gone", Title: "Gone"}, nil) {
		t.Fatal("the fetch committed a channel the library does not hold")
	}
}
