package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/netimage"
	"github.com/simon/youtube-reader/core"
	mui "github.com/simon/youtube-reader/ui"
)

// detailsDividerGrip is the width of the divider's hit area: the 1 pt line
// plus mui.Space(1) over either side. It is the board's 9 pt value, not a
// spacing step, so it lives beside the divider rather than in the token layer.
const detailsDividerGrip float32 = 9

// detailKeyStep is the pane width an arrow key asks for: the board's 8 pt.
var detailKeyStep = mui.Space(2)

// detailState is the detail pane's own state: where its width came from,
// where the open/close animation is, the video it shows while it closes,
// and the interaction state of the divider.
type detailState struct {
	seeded bool
	width  float32 // the user's width, always within minDetailsWidth..maxDetailsWidth

	started   bool
	openState bool
	from      float32
	shown     float32
	start     time.Time
	animating bool

	dragging  bool
	overshoot float32

	videoID      string
	video        core.Video
	hasVideo     bool
	errID        string
	errDismissed bool
	more         bool
	content      detailContent
	pane         ui.Handle
	close        ui.Handle

	returnFocus bool
	returnIndex int
}

type detailContent uint8

const (
	detailContentNone detailContent = iota
	detailContentVideo
	detailContentError
)

func (d *detailState) contentKind() detailContent {
	switch {
	case d.errID != "":
		return detailContentError
	case d.hasVideo:
		return detailContentVideo
	}
	return detailContentNone
}

// detailCloseIcon is the pane header's close glyph. It follows the shared
// lucide geometry in ui/icons.go without adding a role to that file.
var detailCloseIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="m6 6 12 12"/><path d="M18 6 6 18"/></svg>`))

// clampDetailsWidth keeps a pane width inside the board's 280-480 range.
func clampDetailsWidth(width float32) float32 {
	return clamp(width, minDetailsWidth, maxDetailsWidth)
}

// initialDetailsWidth turns a stored preference into a usable width: zero
// on first launch means the board's 340 default, and older settings that
// stored the removed 560 maximum come back inside the new range.
func initialDetailsWidth(pref float32) float32 {
	if pref <= 0 {
		pref = defaultDetailsWidth
	}
	return clampDetailsWidth(pref)
}

// detailsWidthForKey reports the pane width a divider key asks for, and
// whether the key is one of them. The divider sits left of the pane, so
// Left widens it and Right narrows it; Home and End follow the same
// direction, going to the widest and narrowest bounds.
func detailsWidthForKey(width float32, key ui.Key) (float32, bool) {
	switch key {
	case ui.KeyLeft:
		return clampDetailsWidth(width + detailKeyStep), true
	case ui.KeyRight:
		return clampDetailsWidth(width - detailKeyStep), true
	case ui.KeyHome:
		return maxDetailsWidth, true
	case ui.KeyEnd:
		return minDetailsWidth, true
	}
	return width, false
}

// detailWidthForRoom is the width the layout can show: the pane may use
// up to 480 pt but never take the grid below its minimum.
func detailWidthForRoom(width, rowWidth float32) float32 {
	room := rowWidth - splitDividerWidth() - minGridWidth
	if room < minDetailsWidth {
		room = minDetailsWidth
	}
	return clamp(width, minDetailsWidth, min(maxDetailsWidth, room))
}

func (w *Window) view(c *ui.Context) {
	t := c.Theme()
	tokens := mui.NewTokens(t, t.Dark)
	settings := w.app.settingsSnapshot()
	d := &w.detail

	total := windowWidth(c)
	if !d.seeded {
		d.seeded = true
		d.width = initialDetailsWidth(settings.DetailsWidth)
	}

	selectedID := w.app.SelectedVideoID
	if selectedID != "" {
		if v, ok := w.app.VideoByID(selectedID); ok {
			if d.videoID != selectedID {
				d.more = false
			}
			d.videoID, d.video, d.hasVideo, d.errID = selectedID, v, true, ""
			d.errDismissed = false
		} else {
			d.videoID, d.hasVideo, d.errID = "", false, selectedID
			d.errDismissed = false
		}
	}
	open := selectedID != "" || (d.errID != "" && !d.errDismissed)

	content := d.contentKind()
	if open && d.content != detailContentNone && d.content != content && d.pane.FocusWithin(c) {
		d.close.Focus()
	}
	d.content = content

	fitted := detailWidthForRoom(d.width, w.detailFitWidth(c))
	shown := w.advanceDetails(c, open, fitted)
	w.sidebarWidth = boundedSidebarWidth(settings.SidebarWidth, shown, total)
	sidebar := w.sidebarWidth
	d.shown = shown

	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		// Zone A hook: a folded sidebar has no column, split or grip.
		layoutSidebar := sidebar
		var line ui.Element
		if w.sidebarHidden {
			layoutSidebar = 0
		} else {
			ui.Column(c).Width(sidebar).Shrink(0).Clip().Children(func() {
				w.sidebar(c, tokens)
			})
			line = w.sidebarDivider(c, tokens, sidebar, shown, total)
		}
		ui.Column(c).Grow(1).MinWidth(0).Clip().Children(func() {
			w.mainArea(c, tokens, shown, layoutSidebar)
		})
		if !w.sidebarHidden {
			w.sidebarDividerGrip(c, tokens, line, sidebar, shown, total)
		}
	})

	if d.returnFocus {
		d.returnFocus = false
		if d.returnIndex >= 0 {
			w.grid.ScrollIntoView(d.returnIndex)
		}
		w.grid.Focus(c)
	}
	if !open && shown == 0 {
		d.videoID, d.video, d.hasVideo, d.more = "", core.Video{}, false, false
		d.errID, d.errDismissed = "", false
	}
}

// mainArea lays the grid and the detail pane in the same row, so the pane
// is in flow: the grid gives up exactly the pane's width plus the 1 pt
// divider, and re-columns every frame of the animation.
func (w *Window) mainArea(c *ui.Context, t mui.Tokens, shown, sidebar float32) {
	rowWidth := windowWidth(c) - sidebar
	if !w.sidebarHidden {
		rowWidth -= splitDividerWidth()
	}
	if shown <= 0 {
		w.contentWidth = max(rowWidth, 0)
		w.gridAvailableWidth = w.contentWidth
		w.content(c, t)
		return
	}
	w.contentWidth = max(rowWidth-shown-splitDividerWidth(), 0)
	w.gridAvailableWidth = w.contentWidth

	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			w.content(c, t)
		})
		line := w.detailsDivider(c, t)
		ui.Column(c).Width(max(shown, 0)).Shrink(0).Children(func() {
			w.drawer(c, t)
		})
		w.detailsDividerGrip(c, line, w.contentWidth)
	})
}

// advanceDetails drives the board's 240 ms layout-width transition. Only the
// open and close transitions animate: a drag or a key snaps, a resize in
// flight retargets the transition, and Reduce Motion goes straight to the
// target.
func (w *Window) advanceDetails(c *ui.Context, open bool, fitted float32) float32 {
	d := &w.detail
	target := float32(0)
	if open {
		target = fitted
	}
	if !d.started {
		d.started = true
		d.openState = open
		d.shown = target
		return d.shown
	}
	if open != d.openState {
		d.openState = open
		d.from = d.shown
		d.start = c.Now()
		d.animating = true
		d.overshoot = 0
	}
	if !d.animating {
		d.shown = target
		return d.shown
	}
	if c.Preferences().ReduceMotion {
		d.animating = false
		d.shown = target
		return d.shown
	}
	k := float32(c.Now().Sub(d.start)) / float32(mui.MotionLayout)
	if k >= 1 {
		d.animating = false
		d.shown = target
		return d.shown
	}
	if k < 0 {
		k = 0
	}
	d.shown = d.from + (target-d.from)*mui.Ease(k)
	c.AnimationFrame()
	return d.shown
}

// sidebarDividerGrip is the drag handle's width over the 1 pt line, the
// split's 6 pt value. It is the handle's size, not a spacing step.
const sidebarDividerGrip float32 = 6

// sidebarKeyStep is the sidebar width an arrow key asks for.
var sidebarKeyStep = mui.Space(2.5)

// sidebarDivider is the 1 pt line between the navigation column and the
// content. A press focuses it and the arrows step it. The squeeze from the
// open detail pane bounds the drag, but never the stored width: the drag
// keeps its own overshoot past the bound and writes only widths the user can
// see. The drag handle is committed after the content column so its whole
// area, on either side of the line, receives the pointer.
func (w *Window) sidebarDivider(c *ui.Context, t mui.Tokens, shown, detailShown, total float32) ui.Element {
	line := ui.Box(c).Key("sidebar-divider").Width(splitDividerWidth()).FillHeight().
		Focusable().Role(ui.RoleSplitter).Label("Resize sidebar").
		Range(float64(minSidebarWidth), float64(maxSidebarWidth), float64(shown)).
		Step(float64(sidebarKeyStep)).FocusRingColor(t.Focus).FocusRingOffset(mui.FocusRingGap)
	line.Draw(func(p *ui.Painter, r ui.Rect) {
		p.Fill(r, t.Border, 0)
	})
	if line.Shortcut(0, ui.KeyLeft) {
		w.setSidebarWidth(shown-sidebarKeyStep, detailShown, total)
	}
	if line.Shortcut(0, ui.KeyRight) {
		w.setSidebarWidth(shown+sidebarKeyStep, detailShown, total)
	}
	return line
}

// sidebarDividerGrip carries the drag, the col-resize cursor and the hover
// ink for the line. It is built after the content column, over both sides of
// the seam, so a press 2 pt either side of the line works.
func (w *Window) sidebarDividerGrip(c *ui.Context, t mui.Tokens, line ui.Element, width, detailShown, total float32) {
	handle := ui.Box(c).Absolute().Top(0).FillHeight().Width(sidebarDividerGrip).
		Left(width - (sidebarDividerGrip-splitDividerWidth())/2).Cursor(ui.CursorResizeEW)
	dx, _, held := handle.Dragged()
	switch {
	case held:
		line.Focus()
		if dx != 0 {
			w.dragSidebar(dx, width, detailShown, total)
		}
	case w.sidebarDragging:
		w.sidebarDragging = false
		w.sidebarOvershoot = 0
	}
	handle.Draw(func(p *ui.Painter, r ui.Rect) {
		if handle.Hovered() || w.sidebarDragging {
			x := r.X + (sidebarDividerGrip-splitDividerWidth())/2
			p.Fill(ui.Rect{X: x, Y: r.Y, W: splitDividerWidth(), H: r.H}, t.Accent, 0)
		}
	})
}

// dragSidebar moves the divider by dx DIPs from the width on screen, keeping
// the pointer's blocked travel so coming back moves the divider only once the
// pointer is over it again.
func (w *Window) dragSidebar(dx, shown, detailShown, total float32) {
	if !w.sidebarDragging {
		w.sidebarDragging = true
		w.sidebarOvershoot = 0
	}
	room := boundedSidebarWidth(maxSidebarWidth, detailShown, total)
	want := shown + dx + w.sidebarOvershoot
	next := clamp(want, minSidebarWidth, room)
	w.sidebarOvershoot = want - next
	if next == shown {
		return
	}
	w.sidebarWidth = next
	w.persistSidebarWidth(next)
}

// setSidebarWidth commits a divider key's width, keeping it inside the range
// the split's arrows used: the pane's room bounds the widest it can show.
func (w *Window) setSidebarWidth(width, detailShown, total float32) {
	room := boundedSidebarWidth(maxSidebarWidth, detailShown, total)
	next := clamp(width, minSidebarWidth, room)
	if next == w.sidebarWidth {
		return
	}
	w.sidebarWidth = next
	w.persistSidebarWidth(next)
}

// detailsDivider is the 1 pt separator. Its focusable line sits between
// the grid and the pane in tree order, so Tab goes grid then divider then
// pane. The drag handle is committed after the pane, over both sides of the
// seam, so its whole 9 pt area receives the pointer.
func (w *Window) detailsDivider(c *ui.Context, t mui.Tokens) ui.Element {
	d := &w.detail
	line := ui.Box(c).Key("details-divider").Width(splitDividerWidth()).FillHeight().
		Background(t.Border).Focusable().Role(ui.RoleSplitter).Label("Resize details pane").
		Range(float64(minDetailsWidth), float64(maxDetailsWidth), float64(d.shown)).
		Step(float64(detailKeyStep)).FocusRingColor(t.Focus).FocusRingOffset(mui.FocusRingGap).
		OnShortcut(0, ui.KeyEscape, func() {
			w.closeDetails(c)
		})
	settled := detailWidthForRoom(d.width, w.detailFitWidth(c))
	room := detailWidthForRoom(maxDetailsWidth, w.detailFitWidth(c))
	for _, key := range []ui.Key{ui.KeyLeft, ui.KeyRight, ui.KeyHome, ui.KeyEnd} {
		next, ok := detailsWidthForKey(settled, key)
		if !ok || !line.Shortcut(0, key) {
			continue
		}
		if next > room && d.width > next {
			continue
		}
		w.setDetailsWidth(next, true)
	}
	return line
}

// detailsDividerGrip carries the drag, the col-resize cursor and the
// double-click reset for the line.
func (w *Window) detailsDividerGrip(c *ui.Context, line ui.Element, lineX float32) {
	d := &w.detail
	handle := ui.Box(c).Absolute().Top(0).FillHeight().Width(detailsDividerGrip).
		Left(lineX - (detailsDividerGrip-splitDividerWidth())/2).Cursor(ui.CursorResizeEW)
	dx, _, held := handle.Dragged()
	if held {
		line.Focus()
		if !d.dragging {
			d.dragging = true
			d.overshoot = 0
		}
		if dx != 0 {
			w.dragDetails(dx, w.detailFitWidth(c))
		}
	} else if d.dragging {
		d.dragging = false
		w.persistDetailsWidth()
	}
	if handle.DoubleClicked() {
		w.setDetailsWidth(defaultDetailsWidth, true)
	}
}

// detailFitWidth is the room the grid and pane share at the pane's widest: a
// visible sidebar reserves only its minimum, so the pane may use what a
// divider drag could give it, while a folded sidebar reserves nothing.
func (w *Window) detailFitWidth(c *ui.Context) float32 {
	total := windowWidth(c)
	if w.sidebarHidden {
		return total
	}
	return total - minSidebarWidth - splitDividerWidth()
}

// dragDetails moves the divider by dx DIPs, keeping the pointer on it
// rather than burning the overshoot when it goes past a bound. The sidebar
// yields to its minimum first, so the pane may use all the room that leaves.
func (w *Window) dragDetails(dx, rowWidth float32) {
	d := &w.detail
	room := detailWidthForRoom(maxDetailsWidth, rowWidth)
	settled := detailWidthForRoom(d.width, rowWidth)
	want := settled - dx + d.overshoot
	next := clamp(want, minDetailsWidth, room)
	d.overshoot = want - next
	width := want
	if want > room && d.width > width {
		width = d.width
	}
	d.width = clampDetailsWidth(width)
	d.shown = next
	d.animating = false
}

// setDetailsWidth commits a width from a key or a double click. The frame in
// flight keeps the width it was laid out with, so the pane and the grid share
// one width; advanceDetails shows the new one on the next frame. The choice is
// saved at once because no drag is in flight.
func (w *Window) setDetailsWidth(width float32, persist bool) {
	d := &w.detail
	d.width = clampDetailsWidth(width)
	d.animating = false
	d.overshoot = 0
	if persist {
		w.persistDetailsWidth()
	}
}

// persistDetailsWidth writes the pane width once, when the user lets go.
func (w *Window) persistDetailsWidth() {
	width := w.detail.width
	_, _ = w.app.updateSettings(func(s *core.Settings) bool {
		if s.DetailsWidth == width {
			return false
		}
		s.DetailsWidth = width
		return true
	})
}

// closeDetails hides the pane and gives the keyboard back to the grid.
func (w *Window) closeDetails(c *ui.Context) {
	d := &w.detail
	if w.selectedIndex != nil {
		d.returnIndex = *w.selectedIndex
	}
	if d.dragging {
		// A close in the middle of a drag still saves what the pointer
		// last chose, because no further release frame may come.
		d.dragging = false
		w.persistDetailsWidth()
	}
	if d.errID != "" {
		d.errDismissed = true
	}
	w.app.SelectedVideoID = ""
	d.returnFocus = !w.app.SettingsOpen
}

func (w *Window) drawer(c *ui.Context, t mui.Tokens) {
	d := &w.detail
	pane := ui.Column(c).Fill().Background(t.App).Clip().Bind(&d.pane).
		Role(ui.RoleGroup).Label("Video details").OnShortcut(0, ui.KeyEscape, func() {
		w.closeDetails(c)
	})
	pane.Children(func() {
		w.detailHeader(c, t)
		switch {
		case d.errID != "":
			w.detailError(c, t)
		case d.hasVideo:
			w.detailVideo(c, t, d.video)
		}
	})
}

func (w *Window) detailHeader(c *ui.Context, t mui.Tokens) {
	head := ui.Row(c).Height(mui.Space(10)).Shrink(0).Padding(0, mui.Space(3), 0, mui.Space(4)).
		Gap(mui.Space(2)).AlignItems(ui.Center)
	head.Draw(func(p *ui.Painter, r ui.Rect) {
		p.Fill(ui.Rect{X: r.X, Y: r.Y + r.H - mui.Hairline, W: r.W, H: mui.Hairline}, t.BorderSoft, 0)
	})
	head.Children(func() {
		mui.SectionHeader.Apply(ui.Text(c, "Details")).TextColor(t.Muted).SingleLine().Grow(1)
		close := ui.ButtonBase(c).Key("details-close").Size(mui.Space(7), mui.Space(7)).
			Radius(mui.RadiusControl).Border(0, ui.Color{}).Background(ui.Color{}).
			TextColor(t.Muted).Cursor(ui.CursorPointer).Transition(mui.Fade).
			FocusRingColor(t.Focus).FocusRingOffset(mui.FocusRingGap).
			Role(ui.RoleButton).Label("Close details").Tooltip("Close details").Bind(&w.detail.close)
		close.Children(func() {
			ui.Icon(c, detailCloseIcon).Size(mui.Space(4), mui.Space(4))
		})
		close.Draw(func(p *ui.Painter, r ui.Rect) {
			switch {
			case close.Pressed():
				p.Fill(r, t.Face(mui.Active, false, false).Background, mui.RadiusControl)
			case close.Hovered():
				p.Fill(r, t.Face(mui.Hover, false, false).Background, mui.RadiusControl)
			}
		})
		close.OnClick(func() {
			w.closeDetails(c)
		})
	})
}

func (w *Window) detailError(c *ui.Context, t mui.Tokens) {
	ui.Column(c).Grow(1).Padding(mui.Space(4)).Gap(mui.Space(3)).Children(func() {
		ui.Row(c).Gap(mui.Space(2)).AlignItems(ui.Start).Children(func() {
			ui.Icon(c, mui.IconAlert).Size(mui.Space(4), mui.Space(4)).TextColor(t.Danger)
			mui.SectionHeader.Apply(ui.Text(c, "Couldn't load this video")).TextColor(t.Text).Wrap().Grow(1)
		})
		mui.Body.Apply(ui.Text(c, "It may have been removed, or your list changed. Try again to reload it.")).
			TextColor(t.Muted).Wrap()
		w.detailAction(c, t, "details-retry", "Retry", false, func() {
			w.retryDetails(c)
		})
	})
}

func (w *Window) retryDetails(c *ui.Context) {
	id := w.detail.errID
	if err := w.app.reload(); err != nil {
		w.toast(c, "Couldn't reload videos", true)
		return
	}
	v, ok := w.app.VideoByID(id)
	if !ok {
		w.toast(c, "This video is still unavailable", true)
		return
	}
	w.detail.videoID, w.detail.video, w.detail.hasVideo, w.detail.more = id, v, true, false
	w.detail.errID = ""
	w.app.SelectedVideoID = id
}

func (w *Window) detailVideo(c *ui.Context, t mui.Tokens, v core.Video) {
	d := &w.detail
	innerWidth := max(d.shown-2*mui.Space(4), 0)
	settledWidth := max(detailWidthForRoom(d.width, w.detailFitWidth(c))-2*mui.Space(4), 0)

	body := ui.Scroll(c.Key(v.ID)).Grow(1).MinWidth(0).Padding(mui.Space(4)).Gap(mui.Space(3))
	body.Children(func() {
		w.detailHero(c, t, v, innerWidth)

		mui.DetailTitle.Apply(ui.Text(c, v.Title)).TextColor(t.Text).MaxLines(3).Wrap()

		w.detailMeta(c, t, v)

		desc := strings.TrimSpace(v.Description)
		if desc != "" {
			ui.Column(c).Gap(mui.Space(1)).Children(func() {
				mui.SectionHeader.Apply(ui.Text(c, "Description")).TextColor(t.Text).
					Margin(mui.Space(2), 0, mui.Space(1), 0)
				text := mui.Body.Apply(ui.Text(c, desc)).TextColor(t.Muted).Wrap()
				if !d.more {
					text.MaxLines(6)
				}
				if descriptionOverflows(desc, settledWidth) {
					more := ui.ButtonBase(c).Key("details-description-more").
						Height(mui.Body.LineHeight).Padding(0).Radius(mui.RadiusControl).
						Background(t.App).TextColor(t.Accent).Cursor(ui.CursorPointer).
						Justify(ui.Start).Transition(mui.Fade).FocusRingColor(t.Focus).
						FocusRingOffset(mui.FocusRingGap)
					if d.more {
						more.Label("Show less").Tooltip("Show less")
					} else {
						more.Label("More").Tooltip("Show the full description")
					}
					more.Children(func() {
						label := "More"
						if d.more {
							label = "Show less"
						}
						mui.Body.Apply(ui.Text(c, label)).TextColor(t.Accent).SingleLine()
					})
					more.OnClick(func() {
						d.more = !d.more
					})
				}
			})
		}

		w.detailFacts(c, t, v)
	})

	actions := ui.Column(c).Shrink(0).Padding(mui.Space(3), mui.Space(4), mui.Space(4), mui.Space(4)).
		Gap(mui.Space(2))
	actions.Draw(func(p *ui.Painter, r ui.Rect) {
		p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: mui.Hairline}, t.BorderSoft, 0)
	})
	actions.Children(func() {
		w.detailAction(c, t, "details-open-youtube", "Open on YouTube…", true, func() {
			w.app.MarkRead(v)
			c.OpenURL(youtubeWatchURL(v.ID))
		}).FillWidth()
		ui.Row(c).Gap(mui.Space(2)).Children(func() {
			w.detailAction(c, t, "details-copy-link", "Copy link", false, func() {
				c.WriteClipboardThen(youtubeWatchURL(v.ID), func(err error) {
					if err != nil {
						w.toast(c, "Couldn't copy link", true)
						return
					}
					w.toast(c, "Link copied", false)
				})
			}).Grow(1).Basis(0).MinWidth(0)
			label, key := "Favorite", "details-favorite"
			if w.app.Sync.IsFavorite(v.ID) {
				label = "Unfavorite"
			}
			w.detailAction(c, t, key, label, false, func() {
				w.app.ToggleFavorite(v)
				if w.app.Sync.IsFavorite(v.ID) {
					w.toast(c, "Added to favorites", false)
				} else {
					w.toast(c, "Removed from favorites", false)
				}
			}).Grow(1).Basis(0).MinWidth(0)
		})
	})
}

// detailHeroSide is the hero bitmap's longest side. It is fixed at the
// widest pane's 2x, so the open/close animation, a divider drag and a window
// resize all reuse one decoded image instead of asking netimage for a new
// size on every frame.
const detailHeroSide = int(maxDetailsWidth * 2)

// detailHero is the 16:9 header image. While the network image comes it
// is a neutral surface with one small spinner, so the pane keeps its
// geometry and never shows invented content.
func (w *Window) detailHero(c *ui.Context, t mui.Tokens, v core.Video, width float32) {
	height := width * 9 / 16
	frame := ui.Row(c).Height(height).FillWidth().Radius(mui.RadiusCard).Clip().
		Background(t.SurfaceAlt).Center()
	if v.ThumbnailURL == "" {
		frame.Children(func() {
			ui.Icon(c, mui.IconPlay).Size(mui.Space(7), mui.Space(7)).TextColor(t.Muted)
		})
		return
	}
	src, err := netimage.Load(c, v.ThumbnailURL, netimage.Options{MaxSide: detailHeroSide})
	switch {
	case src != nil:
		frame.Children(func() {
			ui.Image(c, src).Fit(ui.Cover).Fill()
		})
	case err != nil:
		frame.Tooltip("Thumbnail unavailable").Children(func() {
			ui.Icon(c, mui.IconPlay).Size(mui.Space(7), mui.Space(7)).TextColor(t.Muted)
		})
	default:
		frame.Children(func() {
			ui.Spinner(c).Size(mui.Space(4), mui.Space(4)).TextColor(t.Muted).Label("Loading thumbnail")
		})
	}
}

func (w *Window) detailMeta(c *ui.Context, t mui.Tokens, v core.Video) {
	meta := ui.Row(c).Gap(mui.Space(1)).AlignItems(ui.Center).Wrap()
	meta.Children(func() {
		first := true
		add := func(text string, color ui.Color) {
			if !first {
				detailMetaText(c, t, "·").TextColor(t.Faint)
			}
			first = false
			detailMetaText(c, t, text).TextColor(color)
		}
		if v.ChannelTitle != "" {
			add(v.ChannelTitle, t.Muted)
		}
		if stamp := strings.TrimSpace(v.PublishedAt); stamp != "" {
			add(core.RelativeDate(v.PublishedAt, time.Now()), t.Muted)
		}
		if v.Duration != "" {
			add(detailDuration(v.Duration), t.Muted)
		}
		if w.app.Sync.IsRead(v.ID) {
			add("Read", t.Success)
		}
	})
}

// detailFacts is the structured tail: a hairline, then 12 pt labels on the
// left and 13 pt values on the right, 12 pt apart with rows 8 pt apart.
func (w *Window) detailFacts(c *ui.Context, t mui.Tokens, v core.Video) {
	ui.Column(c).Gap(mui.Space(2)).Children(func() {
		ui.Box(c).Height(mui.Hairline).FillWidth().Background(t.BorderSoft)
		detailFact(c, t, "Published", publishedLabel(v.PublishedAt))
		detailFact(c, t, "Duration", detailDuration(v.Duration))
		detailFact(c, t, "Channel", v.ChannelTitle)
		detailFact(c, t, "Video ID", v.ID)
	})
}

func detailFact(c *ui.Context, t mui.Tokens, label, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	ui.Row(c).AlignItems(ui.Baseline).Gap(mui.Space(3)).Children(func() {
		mui.Secondary.Apply(ui.Text(c, label)).TextColor(t.Muted).SingleLine().Shrink(0)
		mui.Control.Apply(ui.Text(c, value)).TextColor(t.Text).FontFeatures("tnum").
			TextAlign(ui.End).SingleLine().Grow(1)
	})
}

// detailAction is a 28 pt action face drawn from the shared tokens. The
// primary is the accent fill; the secondaries are neutral.
func (w *Window) detailAction(c *ui.Context, t mui.Tokens, key, label string, primary bool, onClick func()) ui.Element {
	face := t.Face(mui.Default, primary, false)
	el := ui.ButtonBase(c).Key(key).Height(mui.Space(7)).
		Padding(0, mui.Space(3)).Radius(mui.RadiusControl).Border(mui.Hairline, face.Border).
		Background(face.Background).TextColor(face.Text).Cursor(ui.CursorPointer).
		Transition(mui.Fade).FocusRingColor(t.Focus).FocusRingOffset(mui.FocusRingGap)
	el.Children(func() {
		mui.Control.Apply(ui.Text(c, label)).SingleLine()
	})
	el.Draw(func(p *ui.Painter, r ui.Rect) {
		hover := ui.Rect{X: r.X - mui.Hairline, Y: r.Y - mui.Hairline, W: r.W + 2*mui.Hairline, H: r.H + 2*mui.Hairline}
		switch {
		case el.Pressed():
			p.Fill(hover, t.Face(mui.Active, primary, false).Background, mui.RadiusControl)
		case el.Hovered():
			p.Fill(hover, t.Face(mui.Hover, primary, false).Background, mui.RadiusControl)
		}
	})
	el.OnClick(onClick)
	return el
}

func detailMetaText(c *ui.Context, t mui.Tokens, text string) ui.Element {
	return mui.Secondary.Apply(ui.Text(c, text)).TextColor(t.Muted).FontFeatures("tnum").SingleLine()
}

// descriptionOverflows reports whether the description needs the More
// disclosure: it wraps to more lines than the six the clamped text shows.
// MeasureText cannot tell: it includes hard newlines and its natural line
// height is not the fixed 18 pt the clamped text draws, so the shaped line
// count is the honest measure.
func descriptionOverflows(text string, width float32) bool {
	if width <= 0 {
		return false
	}
	layout := ui.ShapeText(text, ui.Font{
		Family: mui.SFFont,
		Size:   mui.Body.Size,
		Weight: int(mui.Body.Weight),
	}, width)
	return len(layout.Lines()) > 6
}

// detailDuration writes a YouTube ISO 8601 duration as the short clock
// form the board shows: 12:34, or 1:02:03.
func detailDuration(raw string) string {
	sec, ok := core.DurationSeconds(raw)
	if !ok {
		return raw
	}
	h := sec / 3600
	m := (sec % 3600) / 60
	s := sec % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

func publishedLabel(stamp string) string {
	t, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return stamp
	}
	return t.Local().Format("Jan 2, 2006 at 15:04")
}

func youtubeWatchURL(id string) string {
	return "https://www.youtube.com/watch?v=" + id
}

// boundedSidebarWidth fits the sidebar beside the pane width that is
// actually on screen, so the squeeze tracks the open/close animation and the
// divider drag instead of the flag. The grid keeps its minimum while there is
// room to give, and the sidebar never goes below its own minimum.
func boundedSidebarWidth(width, shown, total float32) float32 {
	width = clampSidebarWidth(width)
	limit := maxSidebarWidth
	if total > 0 {
		room := total - 2*splitDividerWidth() - minGridWidth
		if shown > 0 {
			room -= shown
		}
		limit = min(limit, room)
	}
	limit = max(limit, minSidebarWidth)
	return clamp(width, minSidebarWidth, limit)
}

// clampSidebarWidth keeps the user's requested sidebar width inside the
// board's 220-320 range, with zero meaning the 260 default.
func clampSidebarWidth(width float32) float32 {
	if width <= 0 {
		width = defaultSidebarWidth
	}
	return clamp(width, minSidebarWidth, maxSidebarWidth)
}

func (w *Window) persistSidebarWidth(width float32) {
	width = clampSidebarWidth(width)
	_, _ = w.app.updateSettings(func(s *core.Settings) bool {
		if s.SidebarWidth == width {
			return false
		}
		s.SidebarWidth = width
		return true
	})
}

func splitDividerWidth() float32 {
	return 1 // The divider line itself; the drag handle overlays both panes.
}

func windowWidth(c *ui.Context) float32 {
	w, _ := c.Size()
	return w
}

func clamp(v, min, max float32) float32 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
