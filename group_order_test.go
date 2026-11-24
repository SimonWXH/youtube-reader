package main

import (
	"context"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
)

const gameDevGroupID = "group-gamedev"

// gameDevGroupApp is the screenshot's GameDev group: the two subscriptions the
// reader already had, in the order the group held them before groups kept a
// title order. The rows are in the library store and in the sync state, so the
// sidebar renders them the way a launch does.
func gameDevGroupApp(t *testing.T) *App {
	t.Helper()
	a := newTestApp(t, t.TempDir())
	t.Cleanup(func() { a.Close() })
	channels := []core.Channel{
		{ID: "ch-howard", Title: "Howard Wimshurst Animation", GroupIDs: []string{gameDevGroupID}, UpdatedAt: 100},
		{ID: "ch-logos", Title: "Logos By Nick", GroupIDs: []string{gameDevGroupID}, UpdatedAt: 100},
	}
	a.Sync.Channels = map[string]core.Channel{}
	for _, ch := range channels {
		a.Sync.Channels[ch.ID] = ch
	}
	a.Sync.Groups = map[string]core.ChannelGroup{
		gameDevGroupID: {ID: gameDevGroupID, Name: "GameDev", ChannelIDs: []string{"ch-howard", "ch-logos"}, UpdatedAt: 100},
	}
	a.Settings.SidebarWidth = defaultSidebarWidth
	a.Settings.DetailsWidth = defaultDetailsWidth
	a.Settings.ExpandedGroups = map[string]bool{gameDevGroupID: true}
	if err := a.Store.CommitRefresh(context.Background(), core.RefreshCommit{Channels: channels, Revision: 1}); err != nil {
		t.Fatalf("commit the library channels: %v", err)
	}
	if err := a.reload(); err != nil {
		t.Fatalf("load the library: %v", err)
	}
	return a
}

// TestAddSubscriptionPlacesTheChannelInTitleOrder is the screenshot's case: a
// new subscription used to be appended after every channel already in the
// group, and now lands where its title sorts.
func TestAddSubscriptionPlacesTheChannelInTitleOrder(t *testing.T) {
	a := gameDevGroupApp(t)
	if err := a.AddSubscription(core.Channel{ID: "ch-juniper", Title: "Juniper Dev"}, gameDevGroupID); err != nil {
		t.Fatalf("add the channel: %v", err)
	}
	want := []string{"ch-howard", "ch-juniper", "ch-logos"}
	if got := a.Sync.Groups[gameDevGroupID].ChannelIDs; !slices.Equal(got, want) {
		t.Fatalf("sync group order = %q; want %q", got, want)
	}
	if got := a.Groups[0].ChannelIDs; !slices.Equal(got, want) {
		t.Fatalf("sidebar group order = %q; want %q", got, want)
	}

	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.view, 1280, 808)
	tt.Frame()
	howard := rowBox(t, tt, "Howard Wimshurst Animation")
	juniper := rowBox(t, tt, "Juniper Dev")
	logos := rowBox(t, tt, "Logos By Nick")
	if !(howard.Y < juniper.Y && juniper.Y < logos.Y) {
		t.Fatalf("rendered rows at y = %v, %v, %v; want Howard, Juniper, Logos from top",
			howard.Y, juniper.Y, logos.Y)
	}
}

// TestAddSubscriptionOrdersSymbolAndChineseTitlesStably adds a title that starts
// with a symbol and one in Chinese: neither panics, both land where the order
// rule puts them, and a second load leaves the order where it was.
func TestAddSubscriptionOrdersSymbolAndChineseTitlesStably(t *testing.T) {
	a := gameDevGroupApp(t)
	for _, ch := range []core.Channel{
		{ID: "ch-han", Title: "科技频道"},
		{ID: "ch-hash", Title: "#Shorts Studio"},
	} {
		if err := a.AddSubscription(ch, gameDevGroupID); err != nil {
			t.Fatalf("add %q: %v", ch.Title, err)
		}
	}
	want := []string{"ch-hash", "ch-howard", "ch-logos", "ch-han"}
	if got := a.Sync.Groups[gameDevGroupID].ChannelIDs; !slices.Equal(got, want) {
		t.Fatalf("group order = %q; want %q", got, want)
	}
	if err := a.reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := a.Sync.Groups[gameDevGroupID].ChannelIDs; !slices.Equal(got, want) {
		t.Fatalf("group order after a reload moved to %q; want %q", got, want)
	}
	if got := a.Groups[0].ChannelIDs; !slices.Equal(got, want) {
		t.Fatalf("sidebar group order = %q; want %q", got, want)
	}
}

// TestMoveChannelToGroupPlacesItInTitleOrder moves an ungrouped channel into a
// group that already holds a title on either side of it.
func TestMoveChannelToGroupPlacesItInTitleOrder(t *testing.T) {
	a := gameDevGroupApp(t)
	if err := a.AddSubscription(core.Channel{ID: "ch-juniper", Title: "Juniper Dev"}, ""); err != nil {
		t.Fatalf("add the ungrouped channel: %v", err)
	}
	moved, err := a.MoveChannelToGroup("ch-juniper", gameDevGroupID)
	if err != nil {
		t.Fatalf("move the channel: %v", err)
	}
	if !moved {
		t.Fatal("the move reported no change")
	}
	want := []string{"ch-howard", "ch-juniper", "ch-logos"}
	if got := a.Sync.Groups[gameDevGroupID].ChannelIDs; !slices.Equal(got, want) {
		t.Fatalf("sync group order = %q; want %q", got, want)
	}
	if got := a.Groups[0].ChannelIDs; !slices.Equal(got, want) {
		t.Fatalf("sidebar group order = %q; want %q", got, want)
	}
}

// TestReloadSortsGroupsWrittenBeforeTheOrder loads state the way a launch does:
// the sync file an older build wrote, whose group lists end in whatever was
// added last. The reload that follows must arrange them without a migration.
func TestReloadSortsGroupsWrittenBeforeTheOrder(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	a.Sync.Channels = map[string]core.Channel{
		"ch-howard":  {ID: "ch-howard", Title: "Howard Wimshurst Animation", GroupIDs: []string{gameDevGroupID}, UpdatedAt: 100},
		"ch-juniper": {ID: "ch-juniper", Title: "Juniper Dev", GroupIDs: []string{gameDevGroupID}, UpdatedAt: 100},
		"ch-logos":   {ID: "ch-logos", Title: "Logos By Nick", GroupIDs: []string{gameDevGroupID}, UpdatedAt: 100},
	}
	a.Sync.Groups = map[string]core.ChannelGroup{
		gameDevGroupID: {ID: gameDevGroupID, Name: "GameDev",
			ChannelIDs: []string{"ch-howard", "ch-logos", "ch-juniper"}, UpdatedAt: 100},
	}
	if err := a.SaveSync(); err != nil {
		t.Fatalf("save the legacy state: %v", err)
	}

	loaded, err := core.LoadSyncWithConflicts(a.Settings.SyncPath, "test-device")
	if err != nil {
		t.Fatalf("load the saved state: %v", err)
	}
	a.Sync = loaded
	if err := a.reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	want := []string{"ch-howard", "ch-juniper", "ch-logos"}
	if got := a.Sync.Groups[gameDevGroupID].ChannelIDs; !slices.Equal(got, want) {
		t.Fatalf("healed sync group order = %q; want %q", got, want)
	}
	if len(a.Groups) != 1 || !slices.Equal(a.Groups[0].ChannelIDs, want) {
		t.Fatalf("healed sidebar groups = %+v; want %q", a.Groups, want)
	}
}
