package core

import (
	"slices"
	"testing"
)

func titlesOf(channels ...Channel) func(string) string {
	byID := make(map[string]Channel, len(channels))
	for _, ch := range channels {
		byID[ch.ID] = ch
	}
	return func(id string) string { return byID[id].Title }
}

func TestSortChannelIDsOrdersTitlesCaseInsensitively(t *testing.T) {
	channels := []Channel{
		{ID: "ch-z", Title: "Zulu"},
		{ID: "ch-a", Title: "apple"},
		{ID: "ch-m", Title: "Mike"},
	}
	ids := []string{"ch-z", "ch-a", "ch-m"}
	if !SortChannelIDs(ids, titlesOf(channels...)) {
		t.Fatal("sorting an unordered list reported no change")
	}
	want := []string{"ch-a", "ch-m", "ch-z"}
	if !slices.Equal(ids, want) {
		t.Fatalf("order = %q; want %q", ids, want)
	}
}

func TestSortChannelIDsBreaksTitleTiesByID(t *testing.T) {
	channels := []Channel{
		{ID: "ch-b", Title: "Same"},
		{ID: "ch-a", Title: "same"},
	}
	ids := []string{"ch-b", "ch-a"}
	SortChannelIDs(ids, titlesOf(channels...))
	if want := []string{"ch-a", "ch-b"}; !slices.Equal(ids, want) {
		t.Fatalf("order = %q; want %q", ids, want)
	}
}

func TestSortChannelIDsFallsBackToTheIDWithoutATitle(t *testing.T) {
	channels := []Channel{
		{ID: "ch-z", Title: "Zulu"},
		{ID: "ch-bare"},
	}
	ids := []string{"ch-z", "ch-bare"}
	SortChannelIDs(ids, titlesOf(channels...))
	if want := []string{"ch-bare", "ch-z"}; !slices.Equal(ids, want) {
		t.Fatalf("order = %q; want the title-less channel keyed by its ID", ids)
	}
}

func TestSortChannelIDsSortsAMissingChannelByItsID(t *testing.T) {
	ids := []string{"ch-z", "ch-a"}
	if !SortChannelIDs(ids, func(string) string { return "" }) {
		t.Fatal("a list of unknown channels reported no change")
	}
	if want := []string{"ch-a", "ch-z"}; !slices.Equal(ids, want) {
		t.Fatalf("order = %q; want %q", ids, want)
	}
}

func TestSortChannelIDsReportsAnAlreadySortedList(t *testing.T) {
	ids := []string{"ch-a", "ch-b"}
	if SortChannelIDs(ids, titlesOf(Channel{ID: "ch-a", Title: "Alpha"}, Channel{ID: "ch-b", Title: "Bravo"})) {
		t.Fatal("an already sorted list reported a change")
	}
}

// TestSortChannelIDsKeepsSymbolsNumbersAndChineseStable pins the order rule for
// titles that are not plain English: case-insensitive Unicode code point order,
// so symbols and digits come before letters and Han characters after them, and
// the result never depends on the order the IDs arrived in.
func TestSortChannelIDsKeepsSymbolsNumbersAndChineseStable(t *testing.T) {
	channels := []Channel{
		{ID: "ch-han", Title: "科技日报"},
		{ID: "ch-apple", Title: "Apple Dev"},
		{ID: "ch-hash", Title: "#Shorts"},
		{ID: "ch-three", Title: "3Blue1Brown"},
		{ID: "ch-banana", Title: "banana"},
	}
	ids := []string{"ch-han", "ch-apple", "ch-hash", "ch-three", "ch-banana"}
	SortChannelIDs(ids, titlesOf(channels...))
	want := []string{"ch-hash", "ch-three", "ch-apple", "ch-banana", "ch-han"}
	if !slices.Equal(ids, want) {
		t.Fatalf("order = %q; want %q", ids, want)
	}
	// A second pass over the sorted list moves nothing: the order is total, so
	// it does not jitter between launches or devices.
	if SortChannelIDs(ids, titlesOf(channels...)) {
		t.Fatalf("the sorted order moved on a second pass: %q", ids)
	}
}

func TestSortGroupChannelsLeavesTheGroupTimestampAlone(t *testing.T) {
	state := SyncState{
		Channels: map[string]Channel{
			"ch-z": {ID: "ch-z", Title: "Zulu"},
			"ch-a": {ID: "ch-a", Title: "Alpha"},
		},
		Groups: map[string]ChannelGroup{
			"g": {ID: "g", Name: "Group", ChannelIDs: []string{"ch-z", "ch-a"}, UpdatedAt: 42},
		},
	}
	if !state.SortGroupChannels("g") {
		t.Fatal("an unordered group reported no change")
	}
	group := state.Groups["g"]
	if want := []string{"ch-a", "ch-z"}; !slices.Equal(group.ChannelIDs, want) {
		t.Fatalf("order = %q; want %q", group.ChannelIDs, want)
	}
	if group.UpdatedAt != 42 {
		t.Fatalf("the group timestamp = %d; want it unchanged", group.UpdatedAt)
	}
	if state.SortGroupChannels("g") {
		t.Fatal("a sorted group reported a change")
	}
	if state.SortGroupChannels("missing") {
		t.Fatal("a group that does not exist reported a change")
	}
}

func TestSortAllGroupChannelsHealsEveryGroup(t *testing.T) {
	state := SyncState{
		Channels: map[string]Channel{
			"ch-z": {ID: "ch-z", Title: "Zulu"},
			"ch-a": {ID: "ch-a", Title: "Alpha"},
		},
		Groups: map[string]ChannelGroup{
			"g1": {ID: "g1", ChannelIDs: []string{"ch-z", "ch-a"}},
			"g2": {ID: "g2", ChannelIDs: []string{"ch-a", "ch-z"}},
		},
	}
	if moved := state.SortAllGroupChannels(); moved != 1 {
		t.Fatalf("groups moved = %d; want 1", moved)
	}
	for _, id := range []string{"g1", "g2"} {
		if want := []string{"ch-a", "ch-z"}; !slices.Equal(state.Groups[id].ChannelIDs, want) {
			t.Fatalf("%s order = %q; want %q", id, state.Groups[id].ChannelIDs, want)
		}
	}
}

func TestMergeImportedSortsTheMergedGroup(t *testing.T) {
	state := SyncState{
		Channels: map[string]Channel{
			"ch-m": {ID: "ch-m", Title: "Mike"},
			"ch-z": {ID: "ch-z", Title: "Zulu"},
		},
		Groups: map[string]ChannelGroup{
			"g": {ID: "g", Name: "Group", ChannelIDs: []string{"ch-m", "ch-z"}},
		},
	}
	state.MergeImported(
		[]ChannelGroup{{ID: "g", Name: "Group", ChannelIDs: []string{"ch-a"}}},
		[]Channel{{ID: "ch-a", Title: "Alpha"}},
		10,
	)
	if want := []string{"ch-a", "ch-m", "ch-z"}; !slices.Equal(state.Groups["g"].ChannelIDs, want) {
		t.Fatalf("merged order = %q; want %q", state.Groups["g"].ChannelIDs, want)
	}
}

func TestReplaceGroupsSortsEveryGroup(t *testing.T) {
	state := SyncState{}
	state.ReplaceGroups(
		[]ChannelGroup{{ID: "g", Name: "Group", ChannelIDs: []string{"ch-z", "ch-a"}}},
		[]Channel{{ID: "ch-a", Title: "Alpha"}, {ID: "ch-z", Title: "Zulu"}},
		10,
	)
	if want := []string{"ch-a", "ch-z"}; !slices.Equal(state.Groups["g"].ChannelIDs, want) {
		t.Fatalf("replaced order = %q; want %q", state.Groups["g"].ChannelIDs, want)
	}
}
