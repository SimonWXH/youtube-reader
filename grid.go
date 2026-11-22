package main

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
	mui "github.com/simon/youtube-reader/ui"
)

// The video grid keeps the framework's virtual scrolling: ui.GridView owns the
// rows and builds only the ones in view. These are the card measurements it is
// given, so a card is a full-bleed 16:9 thumbnail over a fixed text block.
const (
	// gridCardMaxWidth is the widest a card gets: a wider column is split.
	gridCardMaxWidth float32 = 280
	// gridDecorInset is the room each card leaves inside the GridView's clip
	// for its selection halo and focus ring, which paint outside the card.
	// The grid's outer padding is reduced by as much so the cards keep the
	// width the metrics give them.
	gridDecorInset float32 = mui.FocusRingGap + mui.FocusRing
	// gridCardTextBlock is the card's text area below the thumbnail: 8 pt
	// padding, a two-line 13/18 title (36), 4 pt, an 18 pt footer row and
	// 8 pt padding. Two points stay clear at the bottom of the card.
	gridCardTextBlock float32 = 76
	// gridThumbAspect is the thumbnail's height over the card's width.
	gridThumbAspect float32 = 9.0 / 16.0
	// gridCellInset is the clear 2 pt border the framework reserves on every
	// GridView cell, so that a cell's own selection stroke never moves its
	// content. The card cancels it: its width is the column's width and its
	// gutters are the grid's 8 pt gap.
	gridCellInset float32 = 2
	// gridFitEpsilon keeps the framework's float division from rounding a
	// column away when a minimum width divides the pane exactly.
	gridFitEpsilon float32 = 0.05
)

// gridMetrics is how one grid of cards lays out at one content width: the
// columns it takes, and the width and height each card then has.
type gridMetrics struct {
	Columns int
	Width   float32
	Height  float32
}

// gridCardMetrics reads the card metrics for a grid content width (the pane
// less the grid's padding) and the grid's 8 pt gap: as many columns as keep
// every card at most gridCardMaxWidth wide, clamped to the settings cap. Both
// limits hold: when the cap asks for fewer columns than the width rule needs,
// the cards stay at most gridCardMaxWidth wide and the pane's leftover width
// becomes a trailing margin rather than wider cards.
func gridCardMetrics(available, gap float32, maxColumns int) gridMetrics {
	available = max(available, 1)
	gap = max(gap, 0)
	maxColumns = clampInt(maxColumns, minMaxGridColumns, maxMaxGridColumns)
	columns := int(math.Ceil(float64((available + gap) / (gridCardMaxWidth + gap))))
	columns = clampInt(columns, 1, maxColumns)
	usable := min(available, float32(columns)*gridCardMaxWidth+gap*float32(columns-1))
	width := (usable - gap*float32(columns-1)) / float32(columns)
	return gridMetrics{Columns: columns, Width: width, Height: gridCardHeight(width)}
}

// gridCardHeight is the agreed card height: the width times nine sixteenths
// for the thumbnail, plus the text block.
func gridCardHeight(width float32) float32 {
	return width*gridThumbAspect + gridCardTextBlock
}

// gridMinWidth is the minimum item width ui.GridView needs to lay the cards
// out in exactly these columns at this width.
func (m gridMetrics) gridMinWidth(gap float32) float32 {
	return m.Width - gap - gridFitEpsilon
}

// gridWidth is the width the cards and their gaps take, which is the pane's
// content width unless the settings cap leaves a trailing margin.
func (m gridMetrics) gridWidth(gap float32) float32 {
	return float32(m.Columns)*m.Width + gap*float32(m.Columns-1)
}

func (w *Window) gridArea(c *ui.Context, t mui.Tokens) {
	videos := w.app.VisibleVideos()
	gap := c.Theme().Space(2)
	padding := mui.Space(4)
	ui.Column(c).Grow(1).Padding(padding - gridDecorInset).Children(func() {
		if len(videos) == 0 {
			if w.app.SelectedVideoID != "" {
				w.app.SelectedVideoID = ""
			}
			none := -1
			w.selectedIndex = &none
			state := mui.Default
			if w.app.refreshBlocked(time.Now()) {
				state = mui.Disabled
			}
			w.emptyState(c, t, "Nothing here yet", "Try another filter or refresh your subscriptions.", "empty-refresh", "Refresh now", state, func(c *ui.Context) { w.runRefresh(true) })
			return
		}
		target := -1
		if w.app.SelectedVideoID != "" {
			for i := range videos {
				if videos[i].ID == w.app.SelectedVideoID {
					target = i
					break
				}
			}
			if target < 0 {
				w.app.SelectedVideoID = ""
			}
		}
		lastIndex := 0
		if w.grid.Selected != nil {
			lastIndex = max(*w.grid.Selected, 0)
		}
		w.selectedIndex = &target
		w.grid.Selected = w.selectedIndex
		metrics := gridCardMetrics(w.gridContentWidth(padding), gap, w.app.settingsSnapshot().MaxGridColumns)
		focusIndex := max(target, 0)
		// The ring belongs to the chosen card, or to the first card while the
		// grid has the keyboard focus and nothing is chosen yet.
		grid := ui.GridView(c, &w.grid, len(videos), metrics.gridMinWidth(gap), metrics.Height, func(i int) {
			w.videoCard(c, t, i, focusIndex, videos[i], metrics)
		}).Grow(1).Width(metrics.gridWidth(gap) + 2*gridDecorInset).Padding(gridDecorInset).FocusRing(false)
		w.trackGridFocus(c, grid, focusIndex, lastIndex)
		if grid.Changed() {
			if w.grid.Selected != nil && *w.grid.Selected < len(videos) {
				w.app.SelectedVideoID = videos[*w.grid.Selected].ID
			}
		}
	})
}

// trackGridFocus keeps whether the grid shows the keyboard focus, which the
// cards read while they build, before the grid itself exists in the pass, and
// brings the ring's card into view when the grid takes the focus or the card
// under the ring changes. A change asks for the pass that paints it.
func (w *Window) trackGridFocus(c *ui.Context, grid ui.Element, focusIndex, lastIndex int) {
	focused := grid.FocusVisible()
	if gridFocusScroll(focused, w.gridKeyboardFocus, focusIndex, lastIndex) {
		w.grid.ScrollIntoView(focusIndex)
	}
	if focused == w.gridKeyboardFocus {
		return
	}
	w.gridKeyboardFocus = focused
	c.AnimationFrame()
}

// gridFocusScroll reports whether the ring's card must be scrolled into view:
// when the grid takes the keyboard focus, and when the card under the ring
// changes as it keeps it, as a filter moves the chosen card. Neither moves for
// the user's own scrolling, which the ring leaves alone.
func gridFocusScroll(focused, wasFocused bool, focusIndex, lastIndex int) bool {
	return focused && (!wasFocused || focusIndex != lastIndex)
}

// gridContentWidth is the width the cards may use: the grid pane on this
// frame, less the grid's own padding.
func (w *Window) gridContentWidth(padding float32) float32 {
	available := w.gridAvailableWidth
	if available <= 0 {
		available = w.contentWidth
	}
	return available - 2*padding
}

func (w *Window) videoCard(c *ui.Context, t mui.Tokens, index, focusIndex int, v core.Video, m gridMetrics) {
	selected := w.app.SelectedVideoID == v.ID
	face := t.Surface
	card := ui.Column(c).Height(m.Height).Margin(-gridCellInset).Radius(mui.RadiusCard).Background(face).Clip().Transition(mui.Fade)
	hovered := card.Hovered()
	if hovered && !selected {
		face = t.SurfaceAlt
		card.Background(face)
	}
	card.Children(func() {
		ui.Row(c).Height(m.Height - gridCardTextBlock).FillWidth().Children(func() {
			mui.Thumbnail(c, t, v.ThumbnailURL, 0, 0)
		})
		ui.Column(c).Grow(1).Padding(mui.Space(2)).Gap(mui.Space(1)).Children(func() {
			mui.CardTitle.Apply(ui.Text(c, v.Title)).TextColor(cardTitleColor(t, selected)).
				Height(mui.CardTitle.LineHeight * 2).MaxLines(2).Wrap().Label(v.Title)
			w.videoCardFooter(c, t, v)
		})
	})
	stroke := t.BorderSoft
	if selected {
		stroke = t.Accent
	}
	// The keyboard focus ring is the grid's, so it shows only once the grid
	// was reached from the keyboard. It sits outside the selection stroke and
	// its halo, and marks the first card while nothing is chosen, where the
	// arrows start from.
	focused := index == focusIndex && w.gridKeyboardFocus
	// The framework paints the cell's own hover and selection faces under the
	// card. Cover the corners its smaller radius leaves open so only this card's
	// faces show.
	card.Draw(func(p *ui.Painter, r ui.Rect) {
		if hovered || selected {
			coverCardCorners(p, r, mui.RadiusCard, t.App, face)
		}
	})
	card.DrawOver(func(p *ui.Painter, r ui.Rect) {
		p.Stroke(r, stroke, mui.RadiusCard, mui.Hairline)
		if selected {
			p.Stroke(outsetRect(r, mui.FocusRing), t.Accent.Alpha(0.24), mui.RadiusCard+mui.FocusRing, mui.FocusRing)
		}
		if focused {
			// The ring's inner edge sits FocusRingGap outside the card, clear
			// of the selection stroke and its halo.
			offset := mui.FocusRingGap + mui.FocusRing
			p.Stroke(outsetRect(r, offset), t.Focus, mui.RadiusCard+offset, mui.FocusRing)
		}
	})
}

// videoCardFooter is the card's 18 pt footer: the unwatched dot, the channel
// avatar and link, and the relative time, in the 11 pt footer role.
func (w *Window) videoCardFooter(c *ui.Context, t mui.Tokens, v core.Video) {
	ui.Row(c).Height(18).Gap(mui.Space(2)).AlignItems(ui.Center).Children(func() {
		if !w.app.Sync.IsRead(v.ID) {
			ui.Row(c).Size(6, 6).Radius(mui.RadiusCheck).Background(t.Accent).Label("Unwatched")
		}
		mui.ChannelAvatar(c, t, v.AvatarURL, v.ChannelTitle, 18)
		w.channelLink(c, t, v)
		if v.IsShort {
			mui.Metadata.Apply(ui.Text(c, "Shorts")).TextColor(t.Muted).SingleLine()
		}
		mui.Metadata.Apply(ui.Text(c, core.RelativeDate(v.PublishedAt, time.Now()))).TextColor(t.Muted).SingleLine()
	})
}

func (w *Window) channelLink(c *ui.Context, t mui.Tokens, v core.Video) {
	link := mui.Metadata.Apply(ui.Text(c, v.ChannelTitle)).TextColor(t.Muted).SingleLine().Grow(1)
	if v.ChannelID == "" {
		return
	}
	open := func() { c.OpenURL(channelURL(v.ChannelID)) }
	link.Cursor(ui.CursorPointer).Tooltip("Open "+v.ChannelTitle+" on YouTube").Role(ui.RoleLink).Focusable().OnClick(open).OnShortcut(0, ui.KeyEnter, open)
}

func channelURL(channelID string) string {
	return "https://www.youtube.com/channel/" + channelID
}

// cardTitleColor is the card title's colour: the accent marks the chosen card
// along with its stroke and halo, and every other title keeps the body colour.
func cardTitleColor(t mui.Tokens, selected bool) ui.Color {
	if selected {
		return t.Accent
	}
	return t.Text
}

// coverCardCorners paints behind over the square corners a card's radius cuts
// away, then puts the card's own rounded face back inside them. The framework
// paints a plain background corner there, and this leaves only the card's.
func coverCardCorners(p *ui.Painter, r ui.Rect, radius float32, behind, face ui.Color) {
	for _, corner := range [4][2]bool{{false, false}, {true, false}, {false, true}, {true, true}} {
		right, bottom := corner[0], corner[1]
		x, y := r.X, r.Y
		if right {
			x = r.X + r.W - radius
		}
		if bottom {
			y = r.Y + r.H - radius
		}
		square := ui.Rect{X: x, Y: y, W: radius, H: radius}
		disc := ui.Rect{X: x, Y: y, W: 2 * radius, H: 2 * radius}
		if right {
			disc.X -= radius
		}
		if bottom {
			disc.Y -= radius
		}
		p.Clip(square, 0, func() { p.Fill(square, behind, 0) })
		p.Clip(square, 0, func() { p.Fill(disc, face, radius) })
	}
}

// outsetRect grows r by d on every side.
func outsetRect(r ui.Rect, d float32) ui.Rect {
	return ui.Rect{X: r.X - d, Y: r.Y - d, W: r.W + 2*d, H: r.H + 2*d}
}

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
