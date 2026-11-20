package ui

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/netimage"
)

// ChannelAvatar renders a real channel avatar when one is available and a
// deterministic monogram otherwise. Every row has the same 32px visual
// anchor whether or not the image has loaded yet.
func ChannelAvatar(c *ui.Context, tokens Tokens, url, title string, size float32) {
	if url != "" {
		netimage.Image(c, url, netimage.Options{MaxSide: int(size * 2)}).Size(size, size).Radius(size / 2)
		return
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
	ui.Row(c).Size(size, size).Radius(size/2).Background(bg).Border(1, tokens.BorderSoft).Center().Children(func() {
		ui.Text(c, fmt.Sprintf("%s", initial)).FontSize(13).Bold().TextColor(tokens.Text)
	})
}

// Thumbnail keeps the grid's 16:9 frames identical even if a network image
// fails to load or is still in flight. A zero width or height fills its parent,
// which is how the responsive grid keeps thumbnails edge to edge.
func Thumbnail(c *ui.Context, tokens Tokens, url string, w, h float32) {
	fill := w <= 0 || h <= 0
	if url != "" {
		maxSide := int(w)
		if maxSide <= 0 {
			maxSide = 640
		}
		image := netimage.Image(c, url, netimage.Options{MaxSide: maxSide}).Radius(8)
		if fill {
			image.Fill()
		} else {
			image.Size(w, h)
		}
		return
	}
	placeholder := ui.Row(c).Radius(8).Background(tokens.SurfaceAlt)
	if fill {
		placeholder.Fill()
	} else {
		placeholder.Size(w, h)
	}
	placeholder.Center().Children(func() {
		ui.Icon(c, IconPlay).Size(28, 28)
	})
}
