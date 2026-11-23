package core

import (
	"net/url"
	"regexp"
	"strings"
)

// ChannelRefKind is what a pasted channel reference names, as far as the app
// can tell without asking YouTube. The kinds are the lookups the Data API
// offers: a channel ID, a handle, a legacy username, or a video whose channel
// the reference really means.
type ChannelRefKind uint8

const (
	// ChannelRefInvalid is input the app rejects on its own, before it spends
	// a quota unit on it.
	ChannelRefInvalid ChannelRefKind = iota
	// ChannelRefID is a channel ID: the UC-prefixed value channels.list takes
	// as id.
	ChannelRefID
	// ChannelRefHandle is a handle, with or without its @, which
	// channels.list takes as forHandle.
	ChannelRefHandle
	// ChannelRefUsername is a legacy username, which channels.list takes as
	// forUsername.
	ChannelRefUsername
	// ChannelRefName is a bare name that may be either a handle or a legacy
	// username: it is resolved as a handle first, then as a username.
	ChannelRefName
	// ChannelRefVideo is a video link whose channel the app subscribes to.
	ChannelRefVideo
	// ChannelRefCustom is a custom /c/ link, which no Data API parameter
	// resolves reliably. It carries its own error copy rather than a guess.
	ChannelRefCustom
)

// ChannelRef is one parsed channel reference. Value holds the part the lookup
// needs: the channel ID, the handle without its @, the username, or the video
// ID for ChannelRefVideo.
type ChannelRef struct {
	Kind  ChannelRefKind
	Value string
}

// channelIDPattern is the shape of a YouTube channel ID: UC followed by 22
// characters of the ID alphabet.
var channelIDPattern = regexp.MustCompile(`^UC[0-9A-Za-z_-]{22}$`)

// refNamePattern is the alphabet handles and legacy usernames use. A bare
// token outside it is not a channel name the API can resolve.
var refNamePattern = regexp.MustCompile(`^[0-9A-Za-z._-]+$`)

// videoIDPattern is the shape of a YouTube video ID.
var videoIDPattern = regexp.MustCompile(`^[0-9A-Za-z_-]{11}$`)

// handlePattern is the shape of a handle: the name alphabet, 3 to 30
// characters, which is what YouTube allows.
var handlePattern = regexp.MustCompile(`^[0-9A-Za-z._-]{3,30}$`)

// ParseChannelRef reads what the reader pasted into the add-subscription
// field. It answers locally, so obviously invalid input, and a custom link the
// API cannot look up, costs no quota unit: an invalid reference is the app's
// answer alone, and only the kinds above are ever sent to YouTube.
//
// It accepts a channel link (youtube.com/channel/UC..., /@handle, /user/Name),
// an @handle, a bare handle or username, a UC channel ID, and a video link
// (watch?v=, youtu.be/, shorts), which resolves to the video's channel.
func ParseChannelRef(raw string) ChannelRef {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ChannelRef{}
	}
	switch {
	case channelIDPattern.MatchString(text):
		return ChannelRef{Kind: ChannelRefID, Value: text}
	case strings.HasPrefix(text, "@"):
		name := strings.TrimPrefix(text, "@")
		if !handlePattern.MatchString(name) {
			return ChannelRef{}
		}
		return ChannelRef{Kind: ChannelRefHandle, Value: name}
	}
	if ref, ok := channelRefFromURL(text); ok {
		return ref
	}
	if refNamePattern.MatchString(text) && handlePattern.MatchString(text) {
		return ChannelRef{Kind: ChannelRefName, Value: text}
	}
	return ChannelRef{}
}

// channelRefFromURL reads a YouTube link, with or without its scheme. Links
// the app cannot look up - a play list, a channel-less page, a custom /c/
// handle - report what they are instead of spending a request on a guess.
func channelRefFromURL(text string) (ChannelRef, bool) {
	candidate := text
	lower := strings.ToLower(candidate)
	if !strings.Contains(lower, "://") {
		// A host without a scheme is still a link: youtube.com/@handle.
		if !strings.HasPrefix(lower, "youtube.com/") && !strings.HasPrefix(lower, "www.youtube.com/") &&
			!strings.HasPrefix(lower, "m.youtube.com/") && !strings.HasPrefix(lower, "youtu.be/") {
			return ChannelRef{}, false
		}
		candidate = "https://" + candidate
	}
	u, err := url.Parse(candidate)
	if err != nil || u.Host == "" {
		return ChannelRef{}, false
	}
	host := strings.ToLower(strings.TrimPrefix(u.Host, "www."))
	path := strings.TrimSuffix(u.Path, "/")
	segments := []string{}
	for _, segment := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if segment != "" {
			segments = append(segments, segment)
		}
	}
	switch host {
	case "youtu.be":
		// A short link is the video's ID alone.
		if len(segments) == 1 && videoIDPattern.MatchString(segments[0]) {
			return ChannelRef{Kind: ChannelRefVideo, Value: segments[0]}, true
		}
		return ChannelRef{}, false
	case "youtube.com", "m.youtube.com", "music.youtube.com":
	default:
		return ChannelRef{}, false
	}
	if len(segments) == 0 {
		return ChannelRef{}, false
	}
	switch segments[0] {
	case "channel":
		if len(segments) > 1 && channelIDPattern.MatchString(segments[1]) {
			return ChannelRef{Kind: ChannelRefID, Value: segments[1]}, true
		}
		return ChannelRef{}, false
	case "user":
		if len(segments) > 1 && refNamePattern.MatchString(segments[1]) {
			return ChannelRef{Kind: ChannelRefUsername, Value: segments[1]}, true
		}
		return ChannelRef{}, false
	case "c":
		return ChannelRef{Kind: ChannelRefCustom, Value: segments[1]}, true
	case "watch":
		if id := u.Query().Get("v"); videoIDPattern.MatchString(id) {
			return ChannelRef{Kind: ChannelRefVideo, Value: id}, true
		}
		return ChannelRef{}, false
	case "shorts", "live", "embed":
		if len(segments) > 1 && videoIDPattern.MatchString(segments[1]) {
			return ChannelRef{Kind: ChannelRefVideo, Value: segments[1]}, true
		}
		return ChannelRef{}, false
	}
	if handle := strings.TrimPrefix(segments[0], "@"); strings.HasPrefix(segments[0], "@") && handlePattern.MatchString(handle) {
		return ChannelRef{Kind: ChannelRefHandle, Value: handle}, true
	}
	return ChannelRef{}, false
}
