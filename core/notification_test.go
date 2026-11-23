package core

import "testing"

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
