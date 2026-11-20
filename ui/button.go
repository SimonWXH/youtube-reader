package ui

import (
	"github.com/egoist/mygo/ui"
)

// StatefulButton has the same behavior as a native button but paints all
// eight defined states from the shared token system. MyGo's DrawOver hook
// runs at paint time, so one element reacts to hover, press, keyboard focus,
// disabled state, and app-driven loading/error/success states.
func StatefulButton(c *ui.Context, tokens Tokens, key, label string, glyph *ui.SVG, primary bool, state State, onClick func()) ui.Element {
	face := tokens.Face(state, primary, false)
	el := ui.ButtonBase(c).Key(key).Height(34).Gap(Space(2)).Padding(0, Space(3)).Radius(6).Border(1, face.Border).Background(face.Background).TextColor(face.Text).FontSize(14).Cursor(ui.CursorPointer).Transition(Fade).FocusRingColor(tokens.Focus).FocusRingOffset(2)
	if state == Disabled {
		el = el.Disabled(true)
	}
	el.Children(func() {
		if glyph != nil {
			ui.Icon(c, glyph).Size(18, 18)
		}
		if label != "" {
			ui.Text(c, label).FontSize(14).SingleLine()
		}
	})
	el.Draw(func(p *ui.Painter, r ui.Rect) {
		switch {
		case state == Disabled:
			return
		case el.Pressed():
			p.Fill(ui.Rect{X: r.X - 1, Y: r.Y - 1, W: r.W + 2, H: r.H + 2}, tokens.Face(Active, primary, false).Background, 6)
		case el.Hovered():
			p.Fill(ui.Rect{X: r.X - 1, Y: r.Y - 1, W: r.W + 2, H: r.H + 2}, tokens.Face(Hover, primary, false).Background, 6)
		}
	})
	if el.Clicked() && onClick != nil {
		onClick()
	}
	return el
}
