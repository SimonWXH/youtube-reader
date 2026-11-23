package main

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
)

// setupChannelDragTestApp creates an isolated App with a real SQLite store and sync path.
func setupChannelDragTestApp(t *testing.T) (*App, *Window) {
	t.Helper()
	dir := t.TempDir()
	a := newTestApp(t, dir)
	t.Cleanup(func() { a.Close() })

	gA := core.ChannelGroup{ID: "group-a", Name: "Alpha", ChannelIDs: []string{"ch-a"}, UpdatedAt: 100}
	gB := core.ChannelGroup{ID: "group-b", Name: "Bravo", ChannelIDs: []string{"ch-b"}, UpdatedAt: 100}

	chA := core.Channel{ID: "ch-a", Title: "Alpha Channel", GroupIDs: []string{"group-a"}, UpdatedAt: 100}
	chB := core.Channel{ID: "ch-b", Title: "Bravo Channel", GroupIDs: []string{"group-b"}, UpdatedAt: 100}

	a.Sync.Groups = map[string]core.ChannelGroup{"group-a": gA, "group-b": gB}
	a.Sync.Channels = map[string]core.Channel{"ch-a": chA, "ch-b": chB}
	a.Groups = []core.ChannelGroup{gA, gB}
	a.Channels = []core.Channel{chA, chB}
	a.Settings.SidebarWidth = defaultSidebarWidth
	a.Settings.ExpandedGroups = map[string]bool{"group-a": true, "group-b": false}

	ctx := context.Background()
	_ = a.Store.SetChannelGroupIDs(ctx, "ch-a", []string{"group-a"}, 100)
	_ = a.Store.SetChannelGroupIDs(ctx, "ch-b", []string{"group-b"}, 100)
	_ = a.SaveSync()
	a.SaveSyncCalls = 0 // reset counter after initial setup

	w := &Window{app: a, selectedIndex: new(int)}
	return a, w
}

// TestChannelDragGroupToGroup drags a channel from group A to group B:
// channel is removed from group A, added to group B, group B auto-expands,
// timestamps bump, persistence happens exactly once, and data in sync/store converges.
func TestChannelDragGroupToGroup(t *testing.T) {
	a, w := setupChannelDragTestApp(t)
	tt := ui.NewTester(w.view, 1280, 808)
	tt.Frame()

	initCalls := a.SaveSyncCalls
	alphaChanBox := rowBox(t, tt, "Alpha Channel")
	bravoBox := rowBox(t, tt, "Bravo")

	// Start dragging Alpha Channel
	sx, sy := alphaChanBox.X+20, alphaChanBox.Y+alphaChanBox.H/2
	tt.Press(sx, sy)
	tt.Move(sx, sy-10)
	tt.Move(bravoBox.X+20, bravoBox.Y+bravoBox.H/2)
	tt.Frame()

	// While hovering Bravo, verify the "Move here" callout badge is visible
	if !tt.HasText("Move here") {
		t.Fatal("hovering target group Bravo did not show 'Move here' bubble")
	}

	// Release over Bravo
	tt.Release(bravoBox.X+20, bravoBox.Y+bravoBox.H/2)
	tt.Frame()

	// Assertions on group membership
	gA := a.Sync.Groups["group-a"]
	if slices.Contains(gA.ChannelIDs, "ch-a") {
		t.Fatalf("group-a still contains ch-a: %+v", gA.ChannelIDs)
	}
	gB := a.Sync.Groups["group-b"]
	if !slices.Contains(gB.ChannelIDs, "ch-a") {
		t.Fatalf("group-b does not contain ch-a: %+v", gB.ChannelIDs)
	}

	// Assert single membership on channel
	chA := a.Sync.Channels["ch-a"]
	if !slices.Equal(chA.GroupIDs, []string{"group-b"}) {
		t.Fatalf("ch-a groupIDs = %v, want [group-b]", chA.GroupIDs)
	}
	chInApp := w.channelByID("ch-a")
	if !slices.Equal(chInApp.GroupIDs, []string{"group-b"}) {
		t.Fatalf("in-memory ch-a groupIDs = %v, want [group-b]", chInApp.GroupIDs)
	}

	// Target group auto-expanded
	if a.GroupCollapsed("group-b") {
		t.Fatal("target group-b was not auto-expanded after drop")
	}

	// Timestamps bumped
	if gA.UpdatedAt <= 100 || gB.UpdatedAt <= 100 || chA.UpdatedAt <= 100 {
		t.Fatalf("timestamps not bumped: gA=%d, gB=%d, chA=%d", gA.UpdatedAt, gB.UpdatedAt, chA.UpdatedAt)
	}

	// Persistence asserted: exactly once
	if a.SaveSyncCalls-initCalls != 1 {
		t.Fatalf("SaveSyncCalls delta = %d, want 1", a.SaveSyncCalls-initCalls)
	}

	// Verify local SQLite store
	ctx := context.Background()
	channels, err := a.Store.Channels(ctx)
	if err != nil {
		t.Fatalf("read store channels: %v", err)
	}
	var storedCh *core.Channel
	for _, c := range channels {
		if c.ID == "ch-a" {
			storedCh = &c
			break
		}
	}
	if storedCh == nil || !slices.Equal(storedCh.GroupIDs, []string{"group-b"}) {
		t.Fatalf("stored channel = %+v, want groupIDs=[group-b]", storedCh)
	}

	// Verify persisted sync.json
	data, err := os.ReadFile(a.Settings.SyncPath)
	if err != nil {
		t.Fatalf("read sync file: %v", err)
	}
	var loadedSync core.SyncState
	if err := json.Unmarshal(data, &loadedSync); err != nil {
		t.Fatalf("unmarshal sync file: %v", err)
	}
	if slices.Contains(loadedSync.Groups["group-a"].ChannelIDs, "ch-a") {
		t.Fatal("persisted group-a still contains ch-a")
	}
	if !slices.Contains(loadedSync.Groups["group-b"].ChannelIDs, "ch-a") {
		t.Fatal("persisted group-b does not contain ch-a")
	}
	if !slices.Equal(loadedSync.Channels["ch-a"].GroupIDs, []string{"group-b"}) {
		t.Fatalf("persisted ch-a groupIDs = %v, want [group-b]", loadedSync.Channels["ch-a"].GroupIDs)
	}
}

// TestChannelDragGroupToUngrouped drags a grouped channel onto the ungrouped area
// when ungrouped channels already exist: channel is removed from all groups and joins ungrouped.
func TestChannelDragGroupToUngrouped(t *testing.T) {
	a, w := setupChannelDragTestApp(t)

	// Add an existing ungrouped channel so the section is already rendered
	lone := core.Channel{ID: "ch-lone", Title: "Lone Channel", UpdatedAt: 100}
	a.Channels = append(a.Channels, lone)
	a.Sync.Channels["ch-lone"] = lone
	_ = a.SaveSync()
	a.SaveSyncCalls = 0

	tt := ui.NewTester(w.view, 1280, 808)
	tt.Frame()

	initCalls := a.SaveSyncCalls
	alphaChanBox := rowBox(t, tt, "Alpha Channel")
	loneBox := rowBox(t, tt, "Lone Channel")

	sx, sy := alphaChanBox.X+20, alphaChanBox.Y+alphaChanBox.H/2
	tt.Press(sx, sy)
	tt.Move(sx, sy-10)
	tt.Move(loneBox.X+20, loneBox.Y+loneBox.H/2)
	tt.Frame()

	// Hovering ungrouped block shows "Move here"
	if !tt.HasText("Move here") {
		t.Fatal("hovering ungrouped area did not show 'Move here'")
	}

	tt.Release(loneBox.X+20, loneBox.Y+loneBox.H/2)
	tt.Frame()

	// Channel removed from group-a
	gA := a.Sync.Groups["group-a"]
	if slices.Contains(gA.ChannelIDs, "ch-a") {
		t.Fatalf("group-a still contains ch-a: %+v", gA.ChannelIDs)
	}

	// Channel membership is empty
	chA := a.Sync.Channels["ch-a"]
	if len(chA.GroupIDs) != 0 {
		t.Fatalf("ch-a groupIDs = %v, want empty", chA.GroupIDs)
	}

	// Exactly one persistence call
	if a.SaveSyncCalls-initCalls != 1 {
		t.Fatalf("SaveSyncCalls delta = %d, want 1", a.SaveSyncCalls-initCalls)
	}

	// Local SQLite store
	ctx := context.Background()
	channels, err := a.Store.Channels(ctx)
	if err != nil {
		t.Fatalf("read store: %v", err)
	}
	for _, c := range channels {
		if c.ID == "ch-a" && len(c.GroupIDs) != 0 {
			t.Fatalf("store ch-a groupIDs = %v, want empty", c.GroupIDs)
		}
	}
}

// TestChannelDragGroupToEmptyUngrouped verifies that when no channels are ungrouped,
// starting a channel drag reveals the 32pt "Ungrouped" drop strip at the bottom,
// and dropping on it moves the channel to ungrouped.
func TestChannelDragGroupToEmptyUngrouped(t *testing.T) {
	a, w := setupChannelDragTestApp(t)
	tt := ui.NewTester(w.view, 1280, 808)
	tt.Frame()

	// Before drag, no ungrouped channels exist, so "Ungrouped" is not rendered
	if tt.HasText("Ungrouped") {
		t.Fatal("Ungrouped section visible before drag when all channels are grouped")
	}

	initCalls := a.SaveSyncCalls
	alphaChanBox := rowBox(t, tt, "Alpha Channel")

	// Start dragging Alpha Channel
	sx, sy := alphaChanBox.X+20, alphaChanBox.Y+alphaChanBox.H/2
	tt.Press(sx, sy)
	tt.Move(sx, sy-10)
	tt.Frame()

	// The drop strip is now dynamically shown at the bottom
	if !tt.HasText("Ungrouped") {
		t.Fatal("empty Ungrouped drop strip was not shown while dragging channel")
	}

	// Locate the drop strip box
	stripBox, ok := tt.Find("Ungrouped")
	if !ok {
		t.Fatal("could not find Ungrouped drop strip box")
	}

	// Move over the drop strip
	tt.Move(stripBox.X+20, stripBox.Y+stripBox.H/2)
	tt.Frame()

	// While hovering, "Move here" bubble appears
	if !tt.HasText("Move here") {
		t.Fatal("hovering empty Ungrouped drop strip did not show 'Move here'")
	}

	// Release over drop strip
	tt.Release(stripBox.X+20, stripBox.Y+stripBox.H/2)
	tt.Frame()

	// Channel removed from group-a
	if slices.Contains(a.Sync.Groups["group-a"].ChannelIDs, "ch-a") {
		t.Fatal("group-a still contains ch-a")
	}
	if len(a.Sync.Channels["ch-a"].GroupIDs) != 0 {
		t.Fatalf("ch-a groupIDs = %v, want empty", a.Sync.Channels["ch-a"].GroupIDs)
	}

	// Exactly one persistence call
	if a.SaveSyncCalls-initCalls != 1 {
		t.Fatalf("SaveSyncCalls delta = %d, want 1", a.SaveSyncCalls-initCalls)
	}

	// Now that ch-a is ungrouped, the frame shows Alpha Channel in the ungrouped section
	if !tt.HasText("Alpha Channel") {
		t.Fatal("Alpha Channel not visible in sidebar after being ungrouped")
	}
}

// TestChannelDragUngroupedToGroup drags an ungrouped channel onto a group:
// channel joins the group, group auto-expands, and persistence happens exactly once.
func TestChannelDragUngroupedToGroup(t *testing.T) {
	a, w := setupChannelDragTestApp(t)

	// Add an ungrouped channel
	lone := core.Channel{ID: "ch-lone", Title: "Lone Channel", UpdatedAt: 100}
	a.Channels = append(a.Channels, lone)
	a.Sync.Channels["ch-lone"] = lone
	_ = a.SaveSync()
	a.SaveSyncCalls = 0

	tt := ui.NewTester(w.view, 1280, 808)
	tt.Frame()

	initCalls := a.SaveSyncCalls
	loneBox := rowBox(t, tt, "Lone Channel")
	bravoBox := rowBox(t, tt, "Bravo")

	sx, sy := loneBox.X+20, loneBox.Y+loneBox.H/2
	tt.Press(sx, sy)
	tt.Move(sx, sy-10)
	tt.Move(bravoBox.X+20, bravoBox.Y+bravoBox.H/2)
	tt.Frame()

	if !tt.HasText("Move here") {
		t.Fatal("hovering Bravo from ungrouped channel did not show 'Move here'")
	}

	tt.Release(bravoBox.X+20, bravoBox.Y+bravoBox.H/2)
	tt.Frame()

	gB := a.Sync.Groups["group-b"]
	if !slices.Contains(gB.ChannelIDs, "ch-lone") {
		t.Fatalf("group-b does not contain ch-lone: %+v", gB.ChannelIDs)
	}
	if !slices.Equal(a.Sync.Channels["ch-lone"].GroupIDs, []string{"group-b"}) {
		t.Fatalf("ch-lone groupIDs = %v, want [group-b]", a.Sync.Channels["ch-lone"].GroupIDs)
	}
	if a.GroupCollapsed("group-b") {
		t.Fatal("group-b was not auto-expanded")
	}

	// Exactly 1 persistence call
	if a.SaveSyncCalls-initCalls != 1 {
		t.Fatalf("SaveSyncCalls delta = %d, want 1", a.SaveSyncCalls-initCalls)
	}
}

// TestChannelDragSameGroupNoOp drags a channel onto the group it already belongs to:
// no highlight is shown, drop does nothing, timestamps remain unchanged, and 0 persistence calls.
func TestChannelDragSameGroupNoOp(t *testing.T) {
	a, w := setupChannelDragTestApp(t)
	tt := ui.NewTester(w.view, 1280, 808)
	tt.Frame()

	initCalls := a.SaveSyncCalls
	alphaChanBox := rowBox(t, tt, "Alpha Channel")
	alphaBox := rowBox(t, tt, "Alpha")

	tAlphaGroup := a.Sync.Groups["group-a"].UpdatedAt
	tAlphaChan := a.Sync.Channels["ch-a"].UpdatedAt

	sx, sy := alphaChanBox.X+20, alphaChanBox.Y+alphaChanBox.H/2
	tt.Press(sx, sy)
	tt.Move(sx, sy-10)
	tt.Move(alphaBox.X+20, alphaBox.Y+alphaBox.H/2)
	tt.Frame()

	// Hovering own group should NOT show "Move here"
	if tt.HasText("Move here") {
		t.Fatal("hovering own group Alpha showed 'Move here', want no highlight")
	}

	tt.Release(alphaBox.X+20, alphaBox.Y+alphaBox.H/2)
	tt.Frame()

	// Assert unchanged
	if a.Sync.Groups["group-a"].UpdatedAt != tAlphaGroup {
		t.Fatalf("group-a timestamp modified on same-group no-op: got %d, want %d", a.Sync.Groups["group-a"].UpdatedAt, tAlphaGroup)
	}
	if a.Sync.Channels["ch-a"].UpdatedAt != tAlphaChan {
		t.Fatalf("ch-a timestamp modified on same-group no-op: got %d, want %d", a.Sync.Channels["ch-a"].UpdatedAt, tAlphaChan)
	}
	if a.SaveSyncCalls != initCalls {
		t.Fatalf("SaveSyncCalls changed on same-group no-op: got %d, want %d", a.SaveSyncCalls, initCalls)
	}
}

// TestChannelDragMultiGroupConvergence tests that dropping a channel that currently
// belongs to multiple groups onto group B converges its membership to exactly group B.
func TestChannelDragMultiGroupConvergence(t *testing.T) {
	a, w := setupChannelDragTestApp(t)

	// Make ch-a belong to BOTH group-a and group-b (e.g. from OPML import)
	gA := a.Sync.Groups["group-a"]
	gA.ChannelIDs = []string{"ch-a", "ch-other"}
	gB := a.Sync.Groups["group-b"]
	gB.ChannelIDs = []string{"ch-a", "ch-b"}
	a.Sync.Groups["group-a"] = gA
	a.Sync.Groups["group-b"] = gB
	a.Groups = []core.ChannelGroup{gA, gB}

	chA := a.Sync.Channels["ch-a"]
	chA.GroupIDs = []string{"group-a", "group-b"}
	a.Sync.Channels["ch-a"] = chA
	for i := range a.Channels {
		if a.Channels[i].ID == "ch-a" {
			a.Channels[i].GroupIDs = []string{"group-a", "group-b"}
		}
	}
	_ = a.SaveSync()
	a.SaveSyncCalls = 0

	tt := ui.NewTester(w.view, 1280, 808)
	tt.Frame()

	initCalls := a.SaveSyncCalls
	// Because ch-a appears only once in sidebar (under group-a), locate its row
	alphaChanBox := rowBox(t, tt, "Alpha Channel")
	bravoBox := rowBox(t, tt, "Bravo")

	sx, sy := alphaChanBox.X+20, alphaChanBox.Y+alphaChanBox.H/2
	tt.Press(sx, sy)
	tt.Move(sx, sy-10)
	tt.Move(bravoBox.X+20, bravoBox.Y+bravoBox.H/2)
	tt.Frame()

	// Since it's in multiple groups, group B is a valid target to converge to single membership
	if !tt.HasText("Move here") {
		t.Fatal("multi-group channel dragged over Bravo did not show 'Move here'")
	}

	tt.Release(bravoBox.X+20, bravoBox.Y+bravoBox.H/2)
	tt.Frame()

	// Convergence: ch-a must be removed from group-a, and present in group-b
	if slices.Contains(a.Sync.Groups["group-a"].ChannelIDs, "ch-a") {
		t.Fatal("group-a still contains ch-a after convergence drop")
	}
	if !slices.Contains(a.Sync.Groups["group-b"].ChannelIDs, "ch-a") {
		t.Fatal("group-b does not contain ch-a after convergence drop")
	}
	if !slices.Equal(a.Sync.Channels["ch-a"].GroupIDs, []string{"group-b"}) {
		t.Fatalf("ch-a groupIDs = %v, want strictly [group-b]", a.Sync.Channels["ch-a"].GroupIDs)
	}

	// Exactly 1 persistence call
	if a.SaveSyncCalls-initCalls != 1 {
		t.Fatalf("SaveSyncCalls delta = %d, want 1", a.SaveSyncCalls-initCalls)
	}
}

// TestChannelDragChineseLocale verifies bilingual labels "移入此组" and "未分组".
func TestChannelDragChineseLocale(t *testing.T) {
	a, w := setupChannelDragTestApp(t)
	tt := ui.NewTester(w.view, 1280, 808)
	tt.SetLocale("zh-CN")
	tt.Frame()

	alphaChanBox := rowBox(t, tt, "Alpha Channel")
	bravoBox := rowBox(t, tt, "Bravo")

	sx, sy := alphaChanBox.X+20, alphaChanBox.Y+alphaChanBox.H/2
	tt.Press(sx, sy)
	tt.Move(sx, sy-10)
	tt.Move(bravoBox.X+20, bravoBox.Y+bravoBox.H/2)
	tt.Frame()

	if !tt.HasText("移入此组") {
		t.Fatal("Chinese locale did not show '移入此组' bubble while hovering target group")
	}

	tt.Release(bravoBox.X+20, bravoBox.Y+bravoBox.H/2)
	tt.Frame()

	if !slices.Contains(a.Sync.Groups["group-b"].ChannelIDs, "ch-a") {
		t.Fatal("group-b does not contain ch-a after Chinese locale drop")
	}
}
