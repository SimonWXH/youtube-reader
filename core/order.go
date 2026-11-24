package core

import (
	"sort"
	"strings"
)

// ChannelSortKey is the key a group keeps its channels in: the channel title
// lowercased, or the channel ID lowercased when the title is blank.
//
// Lowercasing makes the order case-insensitive, so "gameDev" and "GameDev" sort
// as one name. It leaves Chinese and every other uncased script untouched, so
// those titles keep Unicode code point order: the standard order for Han
// characters, and the reason no pinyin table is needed here. Digits and symbols
// have code points below the Latin letters, so a title that starts with one
// sorts ahead of a lettered title in that same code point order instead of
// being moved into a bucket of its own. The ID fallback keeps a title-less
// channel in one fixed place rather than wherever it happened to be appended,
// which also keeps the order identical on every device.
func ChannelSortKey(title, id string) string {
	if title = strings.ToLower(strings.TrimSpace(title)); title != "" {
		return title
	}
	return strings.ToLower(strings.TrimSpace(id))
}

// SortChannelIDs puts ids in the order a group keeps its channels - the order
// the sidebar renders - and reports whether it moved anything. titleOf answers
// a channel's title, and may answer with an empty title for a channel the
// caller does not know: that channel sorts by its ID alone. Equal keys fall
// back to the ID, so the order never depends on the order the IDs arrived in.
func SortChannelIDs(ids []string, titleOf func(id string) string) bool {
	if len(ids) < 2 {
		return false
	}
	ordered := append([]string(nil), ids...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return channelSortsBefore(ordered[i], ordered[j], titleOf)
	})
	changed := false
	for i := range ordered {
		if ordered[i] != ids[i] {
			changed = true
			break
		}
	}
	if changed {
		copy(ids, ordered)
	}
	return changed
}

func channelSortsBefore(a, b string, titleOf func(id string) string) bool {
	keyA, keyB := ChannelSortKey(titleOf(a), a), ChannelSortKey(titleOf(b), b)
	if keyA != keyB {
		return keyA < keyB
	}
	return a < b
}

// SortGroupChannels puts one group's channel list in that order and reports
// whether it moved anything. The group's UpdatedAt is left alone, because the
// order follows the titles rather than a choice the reader made.
func (s *SyncState) SortGroupChannels(groupID string) bool {
	group, ok := s.Groups[groupID]
	if !ok || !SortChannelIDs(group.ChannelIDs, s.channelTitle) {
		return false
	}
	s.Groups[groupID] = group
	return true
}

// SortAllGroupChannels puts every group's channel list in that order and
// reports how many groups moved. It runs as the library loads, so state written
// before groups kept a sorted order shows correctly on the first launch of this
// version without a separate migration.
func (s *SyncState) SortAllGroupChannels() int {
	moved := 0
	for id := range s.Groups {
		if s.SortGroupChannels(id) {
			moved++
		}
	}
	return moved
}

func (s *SyncState) channelTitle(id string) string {
	return s.Channels[id].Title
}
