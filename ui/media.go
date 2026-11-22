package ui

import (
	"github.com/egoist/mygo/plugins/sfsymbols"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/netimage"
)

// ChannelAvatar renders a real channel avatar once its picture has loaded and
// a deterministic monogram in every other state: while it comes, when it
// fails, and when no URL is supplied. Callers pass the row's avatar size, so
// the reserved circular slot stays fixed and never shows a blank disc.
func ChannelAvatar(c *ui.Context, tokens Tokens, url, title string, size float32) {
	if url != "" {
		opts := netimage.Options{MaxSide: int(size * 2)}
		if src, _ := netimage.Load(c, url, opts); src != nil {
			netimage.Image(c, url, opts).Size(size, size).Radius(size / 2)
			return
		}
	}
	initial := "?"
	if title != "" {
		initial = string([]rune(title)[0])
	}
	palette := []ui.Color{tokens.Accent, tokens.Success, tokens.Warning, tokens.Danger, tokens.Muted}
	index := 0
	for _, r := range title {
		index += int(r)
	}
	bg := palette[index%len(palette)].Alpha(0.18)
	ui.Row(c).Size(size, size).Radius(size/2).Background(bg).Border(Hairline, tokens.BorderSoft).Center().Children(func() {
		Metadata.Apply(ui.Text(c, initial)).TextColor(tokens.Text)
	})
}

// Thumbnail fills the 16:9 frame its caller gives it with the video's
// thumbnail, or with a placeholder that states the frame's state: a neutral
// frame while the picture comes, and one that names the failure when it
// cannot load. Neither a missing URL nor a failed load leaves a gap. The
// caller clips the frame to the card's corners, so the frame carries no
// radius of its own.
func Thumbnail(c *ui.Context, tokens Tokens, url string, w, h float32) {
	if url != "" {
		src, err := netimage.Load(c, url, netimage.Options{MaxSide: thumbMaxSide(w)})
		if src != nil {
			thumbnailFrame(netimage.Image(c, url, netimage.Options{MaxSide: thumbMaxSide(w)}), w, h)
			return
		}
		if err != nil {
			thumbnailFrame(thumbnailPlaceholder(c, tokens, "Thumbnail unavailable", true), w, h)
			return
		}
	}
	thumbnailFrame(thumbnailPlaceholder(c, tokens, "Thumbnail loading", false), w, h)
}

// thumbnailFrame sizes a picture to the frame its caller gives it, or fills
// the frame when the caller passes no size.
func thumbnailFrame(el ui.Element, w, h float32) {
	if w <= 0 || h <= 0 {
		el.Fill()
		return
	}
	el.Size(w, h)
}

// thumbnailPlaceholder is the frame a thumbnail shows before it loads, and
// the one it keeps when it cannot: the neutral surface, and on a failure the
// play glyph and the reason, so a card never shows a gap or a broken image.
func thumbnailPlaceholder(c *ui.Context, tokens Tokens, label string, failed bool) ui.Element {
	frame := ui.Row(c).Background(tokens.SurfaceAlt).Role(ui.RoleImage).Label(label)
	if !failed {
		return frame
	}
	return frame.Tooltip(label).Center().Children(func() {
		sfsymbols.Symbol(c, "play.fill").Size(28).Color(tokens.Muted).Element.Role(ui.RoleNone)
	})
}

// thumbMaxSide bounds the bitmap kept for a frame w wide, at about three
// pixels per point.
func thumbMaxSide(w float32) int {
	if w <= 0 {
		return 640
	}
	return int(w * 3)
}
