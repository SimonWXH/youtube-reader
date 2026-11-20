package ui

import (
	"github.com/egoist/mygo/ui"
)

// NavRow is a 34px sidebar row. Selected rows use the accent tint; MyGo's
// native button behavior supplies pointer and keyboard interaction, while
// the shared token palette supplies the faces for all eight states.
func NavRow(c *ui.Context, tokens Tokens, key string, glyph *ui.SVG, label string, selected bool, onClick func()) {
	row(c, tokens, key, glyph, "", label, selected, onClick)
}

// ChannelNavRow is a sidebar row with a channel avatar and monogram fallback.
func ChannelNavRow(c *ui.Context, tokens Tokens, key, label, avatarURL string, selected bool, onClick func()) {
	row(c, tokens, key, nil, avatarURL, label, selected, onClick)
}

func row(c *ui.Context, tokens Tokens, key string, glyph *ui.SVG, avatarURL, label string, selected bool, onClick func()) {
	state := Default
	if selected {
		state = Active
	}
	face := tokens.Face(state, false, true)
	el := ui.ButtonBase(c).Key(key).Height(34).FillWidth().Gap(Space(3)).Padding(0, Space(3)).Radius(6).Border(1, face.Border).Background(face.Background).TextColor(face.Text).FontSize(14).Cursor(ui.CursorPointer).Transition(Fade).MarginY(1).FocusRingColor(tokens.Focus).FocusRingOffset(2)
	el.Children(func() {
		if avatarURL != "" || (glyph == nil && key != "status") {
			ChannelAvatar(c, tokens, avatarURL, label, 22)
		} else if glyph != nil {
			ui.Icon(c, glyph).Size(18, 18)
		}
		ui.Text(c, label).FontSize(14).SingleLine().Grow(1)
	})
	el.Draw(func(p *ui.Painter, r ui.Rect) {
		switch {
		case el.Pressed():
			p.Fill(ui.Rect{X: r.X - 1, Y: r.Y - 1, W: r.W + 2, H: r.H + 2}, tokens.Face(Active, false, true).Background, 6)
		case el.Hovered() && !selected:
			p.Fill(ui.Rect{X: r.X - 1, Y: r.Y - 1, W: r.W + 2, H: r.H + 2}, tokens.Face(Hover, false, true).Background, 6)
		}
	})
	if el.Clicked() {
		onClick()
	}
}
