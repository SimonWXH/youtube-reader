package ui

import (
	"github.com/egoist/mygo/plugins/sfsymbols"
	"github.com/egoist/mygo/ui"
)

// SidebarRowHeight is the height of a navigation row or a group fold row.
// The 2 pt gap between rows comes from each row's 1 pt vertical margin.
const SidebarRowHeight float32 = 28

// SidebarChannelRowHeight is the taller row a channel gets: a 24 pt avatar
// and a 13 pt label need more room than a navigation glyph or a fold chevron.
// Channel rows still keep the 2 pt visual gap from their 1 pt margin.
const SidebarChannelRowHeight float32 = 34

// SidebarChannelAvatar is the channel avatar's diameter in a sidebar row.
const SidebarChannelAvatar float32 = 24

// ScrollBottomInset is the clear room after the last item of a scrolling
// pane, so the bottom row clears the window's bottom edge at the minimum
// height. The sidebar, grid and detail panes share it.
const ScrollBottomInset float32 = 20

// SidebarRow is one row of the navigation sidebar. A navigation or fold row
// is 28 pt tall; a channel row is 34 pt. Both have an 8 pt horizontal inset,
// a 6 pt radius, and a 2 pt gap to the next row.
//
// A row shows one of three leads: a 16 pt accent glyph (Glyph), a 24 pt
// channel avatar with a monogram fallback (Channel), or a 12 pt disclosure
// chevron that folds the rows below it (Fold). The label is 13 pt Regular,
// or 11 pt Semibold for a fold row, and Count is 11 pt Medium tabular
// figures at the trailing edge.
//
// Hover paints SurfaceAlt. A selected row paints the AccentSoft tint with
// accent text; it never becomes a solid accent fill. MyGo's ButtonBase
// supplies pointer, Return and Space activation, and every row takes the
// keyboard focus with the shared 2 pt ring at the token's 3 pt offset.
type SidebarRow struct {
	Key      string
	Label    string
	Count    string
	Symbol   string // SF Symbol name for the leading navigation glyph
	Channel  bool   // show a channel avatar instead of a glyph
	Avatar   string // the channel's avatar URL; empty draws a monogram
	Fold     bool   // this row folds the rows below it
	Expanded bool   // the fold is open, so the chevron points down
	Selected bool
	OnClick  func()
}

// Build creates the row's element in this frame.
func (r SidebarRow) Build(c *ui.Context, tokens Tokens) ui.Element {
	state := Default
	if r.Selected {
		state = Active
	}
	face := tokens.Face(state, false, true)
	rowHeight := SidebarRowHeight
	if r.Channel {
		rowHeight = SidebarChannelRowHeight
	}
	el := ui.ButtonBase(c).Key(r.Key).
		Height(rowHeight).FillWidth().Gap(Space(2)).Padding(0, Space(2)).
		Radius(RadiusControl).Background(face.Background).TextColor(face.Text).
		Cursor(ui.CursorPointer).Transition(Fade).MarginY(1).
		FocusRingColor(tokens.Focus).FocusRingOffset(FocusRingGap).
		// Name the row by its label alone: without it the avatar's monogram
		// letter and the trailing count would join the accessible name.
		Label(r.Label)
	if r.Fold {
		el = el.Role(ui.RoleDisclosure).Expanded(r.Expanded)
	}
	if el.FocusVisible() {
		// A row paints above its neighbours, so the 3 pt offset of its ring is
		// never clipped by the next row's background.
		el.ZIndex(1)
	}
	countColor := tokens.Muted
	if r.Selected {
		countColor = face.Text
	}
	el.Children(func() {
		switch {
		case r.Fold:
			glyph := "chevron.right"
			if r.Expanded {
				glyph = "chevron.down"
			}
			sfsymbols.Symbol(c, glyph).Size(12).Color(tokens.Muted).Element.Role(ui.RoleNone)
			SidebarGroupLabel.Apply(ui.Text(c, r.Label)).TextColor(tokens.Muted).SingleLine().Grow(1)
		case r.Channel:
			ChannelAvatar(c, tokens, r.Avatar, r.Label, SidebarChannelAvatar)
			Control.Apply(ui.Text(c, r.Label)).FontSize(13).SingleLine().Grow(1)
		case r.Symbol != "":
			sfsymbols.Symbol(c, r.Symbol).Size(16).Color(tokens.Accent).Element.Role(ui.RoleNone)
			Control.Apply(ui.Text(c, r.Label)).SingleLine().Grow(1)
		default:
			Control.Apply(ui.Text(c, r.Label)).SingleLine().Grow(1)
		}
		if r.Count != "" {
			Metadata.Apply(ui.Text(c, r.Count)).TextColor(countColor).SingleLine()
		}
	})
	el.Draw(func(p *ui.Painter, box ui.Rect) {
		switch {
		case el.Pressed() && !r.Selected:
			p.Fill(box, tokens.Face(Active, false, true).Background, RadiusControl)
		case el.Hovered() && !r.Selected:
			p.Fill(box, tokens.Face(Hover, false, true).Background, RadiusControl)
		}
	})
	if r.OnClick != nil && el.Clicked() {
		r.OnClick()
	}
	return el
}
