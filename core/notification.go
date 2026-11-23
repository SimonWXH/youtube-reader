package core

import "strings"

// channelNotificationIDPrefix makes notification IDs self-describing. MyGo
// passes the ID back to App.OnNotificationClick, including after a cold
// launch, so the app can route a click to the channel that produced it.
const channelNotificationIDPrefix = "ytreader-channel-"

// ChannelNotificationID returns the stable notification ID for a channel.
func ChannelNotificationID(channelID string) string {
	return channelNotificationIDPrefix + channelID
}

// ChannelFromNotificationID decodes a notification ID produced by
// ChannelNotificationID. It rejects unrelated or malformed IDs.
func ChannelFromNotificationID(id string) (string, bool) {
	if !strings.HasPrefix(id, channelNotificationIDPrefix) {
		return "", false
	}
	channelID := strings.TrimPrefix(id, channelNotificationIDPrefix)
	if channelID == "" || strings.ContainsAny(channelID, "/\\\n\r\t ") {
		return "", false
	}
	return channelID, true
}
