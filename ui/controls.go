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

// SidebarRenameFieldHeight is the height of the inline rename field in a fold
// row. A 13 pt face lays out an 18 pt line, descenders included, so the field
// keeps a 24 pt content box inside its 1 pt border: the whole glyph box shows,
// vertically centred, with room above and below in the 28 pt fold row.
const SidebarRenameFieldHeight float32 = 26

// ScrollBottomInset is the clear room after the last item of a scrolling
// pane, so the bottom row clears the window's bottom edge at the minimum
// height. The sidebar, grid and detail panes share it.
const ScrollBottomInset float32 = 20

// SidebarRow is one row of the navigation sidebar. A navigation or fold row
// is 28 pt tall; a channel row is 34 pt. Both have an 8 pt horizontal inset,
// a 6 pt radius, and a 2 pt gap to the next row.
//
// A row shows one of three leads: a 16 pt accent glyph (Symbol), a 24 pt
// channel avatar with a monogram fallback (Channel), or a 12 pt disclosure
// chevron that folds the rows below it (Fold). A fold row separates chevron
// toggle from row selection and supports inline renaming. The label is 13 pt
// Regular, or 11 pt Semibold for a fold row, and Count is 11 pt Medium tabular
// figures at the trailing edge.
//
// Hover paints SurfaceAlt. A selected row paints the AccentSoft tint with
// accent text; it never becomes a solid accent fill. MyGo's ButtonBase
// supplies pointer, Return and Space activation, and every row takes the
// keyboard focus with the shared 2 pt ring at the token's 3 pt offset.
//
// A fold row being renamed drops the tint and keeps the sidebar surface: the
// rename field is a control of its own, SidebarRenameFieldHeight tall on the
// row's centre, with the whole 13 pt line box visible inside its border.
type SidebarRow struct {
	Key            string
	Label          string
	Count          string
	Symbol         string // SF Symbol name for the leading navigation glyph
	Channel        bool   // show a channel avatar instead of a glyph
	Avatar         string // the channel's avatar URL; empty draws a monogram
	Fold           bool   // this row folds the rows below it
	Expanded       bool   // the fold is open, so the chevron points down
	Selected       bool
	Renaming       bool                 // row is currently being renamed inline
	RenameValue    *string              // value buffer for the rename text input
	OnRename       func(newName string) // callback on rename submit
	OnRenameCancel func()               // callback on rename cancel
	OnClick        func()
	OnFoldClick    func()
	OnContextMenu  func(m *ui.Menu)
}

// foldRowSurface is the fill behind a fold row's content. A row being renamed
// hands the surface back to the sidebar: the inline field draws its own
// bordered surface, so the selected tint behind it would read as a second,
// overlapping control.
func (r SidebarRow) foldRowSurface(tokens Tokens) ui.Color {
	if r.Renaming {
		return tokens.Sidebar
	}
	state := Default
	if r.Selected {
		state = Active
	}
	return tokens.Face(state, false, true).Background
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
	countColor := tokens.Muted
	if r.Selected {
		countColor = face.Text
	}

	if r.Fold {
		container := ui.Row(c).Key(r.Key+":container").
			Height(rowHeight).FillWidth().Gap(0).Padding(0, Space(2)).
			Radius(RadiusControl).AlignItems(ui.Center).
			Background(r.foldRowSurface(tokens)).TextColor(face.Text).
			Transition(Fade).MarginY(1)

		glyph := "chevron.right"
		if r.Expanded {
			glyph = "chevron.down"
		}
		chevronColor := tokens.Muted
		if r.Selected {
			chevronColor = face.Text
		}

		var chevron ui.Element
		var body ui.Element

		container.Children(func() {
			chevron = ui.ButtonBase(c).Key(r.Key + ":fold").
				Width(16).Height(rowHeight).
				Radius(RadiusControl).
				Cursor(ui.CursorPointer).
				Label("Toggle " + r.Label).
				Role(ui.RoleDisclosure).Expanded(r.Expanded)
			chevron.Children(func() {
				sfsymbols.Symbol(c, glyph).Size(12).Color(chevronColor).Element.Role(ui.RoleNone)
			})
			if r.OnFoldClick != nil && chevron.Clicked() {
				r.OnFoldClick()
			}

			if r.Renaming {
				// The field owns its surface and stays at one height, so the
				// 13 pt line keeps its descenders and centres in the row.
				input := ui.TextInput(c.Key(r.Key+":rename-input"), r.RenameValue).
					Height(SidebarRenameFieldHeight).Grow(1).
					Padding(0, Space(2)).Radius(RadiusControl).
					Background(tokens.Surface).Border(Hairline, tokens.Border).
					TextColor(tokens.Text).Label("Rename " + r.Label).
					FocusRingColor(tokens.Focus).FocusRingOffset(FocusRingGap)
				Control.Apply(input).FontSize(13)
				// MyGo paints a focused input's outline with its theme accent,
				// which is not the app's accent: the field's outline is the focus
				// token, the same colour as the ring around it.
				input.DrawOver(func(p *ui.Painter, box ui.Rect) {
					if input.Focused() {
						p.Stroke(box, tokens.Focus, RadiusControl, Hairline)
					}
				})
				input.Focus()
				input.OnShortcut(0, ui.KeyEnter, func() {
					if r.OnRename != nil && r.RenameValue != nil {
						r.OnRename(*r.RenameValue)
					}
				})
				input.OnShortcut(0, ui.KeyEscape, func() {
					if r.OnRenameCancel != nil {
						r.OnRenameCancel()
					}
				})
				input.OnSubmit(func() {
					if r.OnRename != nil && r.RenameValue != nil {
						r.OnRename(*r.RenameValue)
					}
				})
				body = input
			} else {
				btn := ui.ButtonBase(c).Key(r.Key).
					Height(rowHeight).Grow(1).Gap(Space(2)).Padding(0, Space(1)).
					Radius(RadiusControl).TextColor(face.Text).
					Cursor(ui.CursorPointer).Transition(Fade).
					FocusRingColor(tokens.Focus).FocusRingOffset(FocusRingGap).
					Label(r.Label)

				if btn.Focused() || btn.FocusVisible() {
					btn.ZIndex(1)
					container.ZIndex(1)
				}
				if r.OnContextMenu != nil {
					btn.ContextMenu(r.OnContextMenu)
				}
				btn.Children(func() {
					textColor := tokens.Muted
					if r.Selected {
						textColor = face.Text
					}
					SidebarGroupLabel.Apply(ui.Text(c, r.Label)).TextColor(textColor).SingleLine().Grow(1)
					if r.Count != "" {
						Metadata.Apply(ui.Text(c, r.Count)).TextColor(countColor).SingleLine()
					}
				})
				if r.OnClick != nil && btn.Clicked() {
					r.OnClick()
				}
				body = btn
			}
		})

		container.Draw(func(p *ui.Painter, box ui.Rect) {
			if r.Renaming {
				return
			}
			switch {
			case (body.Pressed() || chevron.Pressed()) && !r.Selected:
				p.Fill(box, tokens.Face(Active, false, true).Background, RadiusControl)
			case (container.Hovered() || body.Hovered() || chevron.Hovered()) && !r.Selected:
				p.Fill(box, tokens.Face(Hover, false, true).Background, RadiusControl)
			}
		})

		return body
	}

	el := ui.ButtonBase(c).Key(r.Key).
		Height(rowHeight).FillWidth().Gap(Space(2)).Padding(0, Space(2)).
		Radius(RadiusControl).Background(face.Background).TextColor(face.Text).
		Cursor(ui.CursorPointer).Transition(Fade).MarginY(1).
		FocusRingColor(tokens.Focus).FocusRingOffset(FocusRingGap).
		Label(r.Label)
	if el.FocusVisible() {
		el.ZIndex(1)
	}
	if r.OnContextMenu != nil {
		el.ContextMenu(r.OnContextMenu)
	}
	el.Children(func() {
		switch {
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
	if r.OnClick != nil && el.Clicked() && !r.Renaming {
		r.OnClick()
	}
	return el
}
