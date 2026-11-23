package core

import "testing"

func TestParseChannelRef(t *testing.T) {
	tests := []struct {
		name  string
		input string
		kind  ChannelRefKind
		value string
	}{
		{"channel id", "UCXuqSBlHAE6Xw-yeJA0Tunw", ChannelRefID, "UCXuqSBlHAE6Xw-yeJA0Tunw"},
		{"channel id with spaces", "  UCXuqSBlHAE6Xw-yeJA0Tunw\n", ChannelRefID, "UCXuqSBlHAE6Xw-yeJA0Tunw"},
		{"channel link", "https://www.youtube.com/channel/UCXuqSBlHAE6Xw-yeJA0Tunw", ChannelRefID, "UCXuqSBlHAE6Xw-yeJA0Tunw"},
		{"channel link without scheme", "youtube.com/channel/UCXuqSBlHAE6Xw-yeJA0Tunw/videos", ChannelRefID, "UCXuqSBlHAE6Xw-yeJA0Tunw"},
		{"channel link with query", "https://m.youtube.com/channel/UCXuqSBlHAE6Xw-yeJA0Tunw?si=abc", ChannelRefID, "UCXuqSBlHAE6Xw-yeJA0Tunw"},
		{"handle", "@AnnaCramling", ChannelRefHandle, "AnnaCramling"},
		{"handle link", "https://youtube.com/@AnnaCramling", ChannelRefHandle, "AnnaCramling"},
		{"handle link with tab", "https://www.youtube.com/@AnnaCramling/videos", ChannelRefHandle, "AnnaCramling"},
		{"bare name", "AnnaCramling", ChannelRefName, "AnnaCramling"},
		{"bare name with dots", "Anna.Cramling_1", ChannelRefName, "Anna.Cramling_1"},
		{"legacy username link", "https://www.youtube.com/user/AnnaCramling", ChannelRefUsername, "AnnaCramling"},
		{"watch link", "https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=42s", ChannelRefVideo, "dQw4w9WgXcQ"},
		{"watch link without scheme", "youtube.com/watch?v=dQw4w9WgXcQ", ChannelRefVideo, "dQw4w9WgXcQ"},
		{"short link", "https://youtu.be/dQw4w9WgXcQ", ChannelRefVideo, "dQw4w9WgXcQ"},
		{"shorts link", "https://www.youtube.com/shorts/dQw4w9WgXcQ", ChannelRefVideo, "dQw4w9WgXcQ"},
		{"live link", "https://www.youtube.com/live/dQw4w9WgXcQ", ChannelRefVideo, "dQw4w9WgXcQ"},
		{"custom link", "https://www.youtube.com/c/SomeCustomName", ChannelRefCustom, "SomeCustomName"},
	}
	for _, tt := range tests {
		ref := ParseChannelRef(tt.input)
		if ref.Kind != tt.kind || ref.Value != tt.value {
			t.Errorf("%s: ParseChannelRef(%q) = %+v; want %v %q", tt.name, tt.input, ref, tt.kind, tt.value)
		}
	}
}

func TestParseChannelRefRejectsWhatTheAPICannotLookUp(t *testing.T) {
	rejected := []string{
		"",
		"   ",
		"not a channel at all",
		"https://example.com/@AnnaCramling",
		"https://www.youtube.com/playlist?list=PL123",
		"https://www.youtube.com/results?search_query=chess",
		"https://www.youtube.com/",
		"https://www.youtube.com/channel/UCtoo-short",
		"https://www.youtube.com/watch?v=tooshort",
		"https://youtu.be/",
		"https://www.youtube.com/@a",
		"@",
		"@@handle",
		"https://www.youtube.com/@handle with spaces",
	}
	for _, input := range rejected {
		if ref := ParseChannelRef(input); ref.Kind != ChannelRefInvalid {
			t.Errorf("ParseChannelRef(%q) = %+v; want the invalid kind", input, ref)
		}
	}
}
