package ui

import (
	"math"

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
//
// frameWidth is the frame's width in points, which sets the bitmap's sampling:
// the picture is kept at what the frame shows at two pixels per point, so a
// card as wide as a narrow column holds a narrow bitmap. A frame width of zero
// asks for the default cap, the widest card at that density.
func Thumbnail(c *ui.Context, tokens Tokens, url string, frameWidth float32) {
	maxSide := netimage.Options{MaxSide: thumbMaxSide(frameWidth)}
	if url != "" {
		src, err := netimage.Load(c, url, maxSide)
		if src != nil {
			netimage.Image(c, url, maxSide).Fill()
			return
		}
		if err != nil {
			thumbnailPlaceholder(c, tokens, "Thumbnail unavailable", true).Fill()
			return
		}
	}
	thumbnailPlaceholder(c, tokens, "Thumbnail loading", false).Fill()
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

// thumbnailPixelsPerPoint is the sampling density a thumbnail is decoded at:
// two pixels per point, which is what every Mac retina screen draws. Three - an
// iPhone's density - keeps 2.25 times the pixels the screen shows for no
// sharper a picture.
const thumbnailPixelsPerPoint = 2

// thumbnailSampleStep is the step a frame's sampling width is rounded up to.
// netimage keeps one decoded bitmap per size asked for, so an unrounded width
// would ask for a new one at every pixel of a window drag and hold them all;
// rounding up to a step asks for a handful across the whole drag, and never
// samples below what the frame shows.
const thumbnailSampleStep = 64

// defaultThumbMaxSide bounds the bitmap for a frame whose width its caller
// leaves to the layout: a 320 pt card at two pixels per point need not be
// exceeded by a caller that cannot say how wide its frame is.
const defaultThumbMaxSide = 640

// thumbMaxSide bounds the bitmap kept for a frame w wide, at two pixels per
// point, the density every Mac retina screen draws.
func thumbMaxSide(w float32) int {
	if w <= 0 {
		return defaultThumbMaxSide
	}
	side := int(math.Ceil(float64(w) * thumbnailPixelsPerPoint))
	return (side + thumbnailSampleStep - 1) / thumbnailSampleStep * thumbnailSampleStep
}
