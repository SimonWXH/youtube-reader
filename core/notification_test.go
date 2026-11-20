package core

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestChannelNotificationIDRoundTrip(t *testing.T) {
	channelID := "UCabc-123_X"
	id := ChannelNotificationID(channelID)
	got, ok := ChannelFromNotificationID(id)
	if !ok || got != channelID {
		t.Fatalf("decode(%q) = %q, %v", id, got, ok)
	}
	for _, bad := range []string{"", "ytreader-channel-", "ytreader-UCabc", "ytreader-channel-UCabc/extra", "ytreader-channel-UC abc"} {
		if got, ok := ChannelFromNotificationID(bad); ok {
			t.Fatalf("decode(%q) = %q, true; want false", bad, got)
		}
	}
}

func TestNewNotificationClicksOnePerChannelPlan(t *testing.T) {
	at := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	plans := []NotificationPlan{
		{ChannelID: "ch-1", ChannelName: "One", Count: 2, NewestTitle: "New"},
		{ChannelID: "ch-2", ChannelName: "Two", Count: 4, NewestTitle: "Newer"},
		{ChannelName: "No channel", Count: 1},
	}
	clicks := NewNotificationClicks(plans, at)
	want := []NotificationClick{
		{ID: "ytreader-channel-ch-1", ChannelID: "ch-1", Timestamp: "2026-03-01T12:00:00Z"},
		{ID: "ytreader-channel-ch-2", ChannelID: "ch-2", Timestamp: "2026-03-01T12:00:00Z"},
	}
	if !reflect.DeepEqual(clicks, want) {
		t.Fatalf("clicks = %+v, want %+v", clicks, want)
	}
	if a, b := NotificationClickKey(clicks[0]), NotificationClickKey(clicks[1]); a == b {
		t.Fatalf("click keys collide: %q", a)
	}
}

func TestLatestNotificationClickPicksNewestAndLeavesOlderEntries(t *testing.T) {
	queue := NotificationClicks{Clicks: []NotificationClick{
		{ID: "ytreader-channel-ch-1", ChannelID: "ch-1", Timestamp: "2026-03-01T11:00:00Z"},
		{ID: "ytreader-channel-ch-2", ChannelID: "ch-2", Timestamp: "2026-03-01T12:00:00Z"},
	}}
	latest, tail, err := LatestNotificationClick(queue)
	if err != nil {
		t.Fatal(err)
	}
	if latest.ID != "ytreader-channel-ch-2" {
		t.Fatalf("latest = %+v", latest)
	}
	if len(tail.Clicks) != 1 || tail.Clicks[0].ID != "ytreader-channel-ch-1" {
		t.Fatalf("tail = %+v", tail)
	}
	empty, tail, err := LatestNotificationClick(NotificationClicks{})
	if err != nil {
		t.Fatal(err)
	}
	if empty.ID != "" || len(tail.Clicks) != 0 {
		t.Fatalf("empty queue = %+v, %+v", empty, tail)
	}
}

func TestNotificationClickKeyComparesTimestampBeforeID(t *testing.T) {
	older := NotificationClick{ID: "ytreader-channel-ch-2", ChannelID: "ch-2", Timestamp: "2026-03-01T11:00:00Z"}
	newer := NotificationClick{ID: "ytreader-channel-ch-1", ChannelID: "ch-1", Timestamp: "2026-03-01T12:00:00Z"}
	key := NotificationClickKey(older)
	if !strings.HasPrefix(key, older.Timestamp) {
		t.Fatalf("key %q does not begin with timestamp", key)
	}
	if NotificationClickKey(newer) <= key {
		t.Fatal("timestamp ordering is not lexical")
	}
	queue := NotificationClicks{Clicks: []NotificationClick{newer, older}}
	latest, tail, err := LatestNotificationClick(queue)
	if err != nil {
		t.Fatal(err)
	}
	if latest.ID != newer.ID {
		t.Fatalf("latest = %+v", latest)
	}
	if len(tail.Clicks) != 1 || tail.Clicks[0].ID != older.ID {
		t.Fatalf("tail = %+v", tail.Clicks)
	}
}

func TestNotificationClicksRejectCorruptQueue(t *testing.T) {
	for name, queue := range map[string]NotificationClicks{
		"bad ID":   {Clicks: []NotificationClick{{ID: "unrelated", ChannelID: "ch-1", Timestamp: "2026-03-01T12:00:00Z"}}},
		"mismatch": {Clicks: []NotificationClick{{ID: "ytreader-channel-ch-2", ChannelID: "ch-1", Timestamp: "2026-03-01T12:00:00Z"}}},
		"bad time": {Clicks: []NotificationClick{{ID: "ytreader-channel-ch-1", ChannelID: "ch-1", Timestamp: "bad"}}},
		"duplicate": {Clicks: []NotificationClick{
			{ID: "ytreader-channel-ch-1", ChannelID: "ch-1", Timestamp: "2026-03-01T12:00:00Z"},
			{ID: "ytreader-channel-ch-1", ChannelID: "ch-1", Timestamp: "2026-03-01T12:00:00Z"},
		}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := LatestNotificationClick(queue); err == nil {
				t.Fatal("corrupt queue was accepted")
			}
		})
	}
}
