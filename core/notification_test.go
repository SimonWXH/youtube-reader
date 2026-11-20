package core

import (
	"reflect"
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
		{ID: "old", Timestamp: "2026-03-01T11:00:00Z"},
		{ID: "new", Timestamp: "2026-03-01T12:00:00Z"},
		{ID: "unknown", Timestamp: "bad"},
	}}
	latest, tail := LatestNotificationClick(queue)
	if latest.ID != "new" {
		t.Fatalf("latest = %+v", latest)
	}
	if len(tail.Clicks) != 2 || tail.Clicks[0].ID != "old" || tail.Clicks[1].ID != "unknown" {
		t.Fatalf("tail = %+v", tail)
	}
	if len(queue.Clicks) != 3 {
		t.Fatalf("input queue was modified: %+v", queue.Clicks)
	}
	empty, tail := LatestNotificationClick(NotificationClicks{})
	if empty.ID != "" || len(tail.Clicks) != 0 {
		t.Fatalf("empty queue = %+v, %+v", empty, tail)
	}
}

func TestNotificationClickAfterComparesTimestampBeforeID(t *testing.T) {
	older := NotificationClick{ID: "ytreader-channel-ch-2", Timestamp: "2026-03-01T11:00:00Z"}
	newer := NotificationClick{ID: "ytreader-channel-ch-1", Timestamp: "2026-03-01T12:00:00Z"}
	watermark := NotificationClickKey(older)
	if !NotificationClickAfter(newer, watermark) {
		t.Fatal("newer post from lexically lower channel was rejected")
	}
	if NotificationClickAfter(older, watermark) {
		t.Fatal("previous post was not deduped")
	}
	sameTimeNewerID := NotificationClick{ID: "ytreader-channel-ch-3", Timestamp: "2026-03-01T11:00:00Z"}
	if !NotificationClickAfter(sameTimeNewerID, watermark) {
		t.Fatal("same-time tie was not broken by ID")
	}
	sameTimeOlderID := NotificationClick{ID: "ytreader-channel-ch-1", Timestamp: "2026-03-01T11:00:00Z"}
	if NotificationClickAfter(sameTimeOlderID, watermark) {
		t.Fatal("same-time lower ID was accepted")
	}
	queue := NotificationClicks{Clicks: []NotificationClick{older, newer}}
	latest, _ := LatestNotificationClick(queue)
	if latest.ID != newer.ID {
		t.Fatalf("latest = %+v", latest)
	}
}
