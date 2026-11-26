package ui

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

// WorkspaceIconKind names one glyph in the workspace dock.
type WorkspaceIconKind uint8

const (
	WorkspaceIconVideos WorkspaceIconKind = iota
	WorkspaceIconRSS
	WorkspaceIconReadLater
	WorkspaceIconWeChatNotes
	workspaceIconKindCount
)

// WorkspaceIconSize is every dock glyph's measured ink height in dp. The four
// source paths fill their design boxes to very different degrees (Video is
// about 59% tall, 微信读书 about 62%, RSS and 稍后读 100%), so a glyph is
// normalized by its own bounds instead of by the design viewBox: otherwise the
// four would look visibly unequal and one of them would grow past the slot.
const WorkspaceIconSize float32 = 30

// The four d attributes, verbatim from the open-source icon set the captain
// supplied.
const (
	workspaceVideoPathD  = "M6.989 4.89a64 64 0 0 1 10.022 0l2.24.176a2.725 2.725 0 0 1 2.476 2.268c.517 3.09.517 6.243 0 9.332a2.725 2.725 0 0 1-2.475 2.268l-2.24.175a64 64 0 0 1-10.023 0l-2.24-.175a2.725 2.725 0 0 1-2.476-2.268a28.3 28.3 0 0 1 0-9.332a2.725 2.725 0 0 1 2.475-2.268zM10 14.47V9.53a.3.3 0 0 1 .454-.257l4.117 2.47a.3.3 0 0 1 0 .514l-4.117 2.47A.3.3 0 0 1 10 14.47"
	workspaceRSSPathD    = "M25.599 32C25.599 17.958 14.042 6.401 0 6.401V0c17.552 0 32 14.448 32 32zM4.385 23.219a4.404 4.404 0 0 1 4.396 4.396C8.781 30.032 6.797 32 4.375 32C1.958 32 0 30.036 0 27.615s1.969-4.396 4.385-4.396M21.214 32H14.99c0-8.224-6.766-14.995-14.99-14.995v-6.219c11.635 0 21.214 9.578 21.214 21.214"
	workspaceLaterPathD  = "M15 3.604H1v1.891h14v-1.89ZM1 7.208V16l7-3.926L15 16V7.208zM15 0H1v1.89h14z"
	workspaceWeChatPathD = "M928 161H699.2c-49.1 0-97.1 14.1-138.4 40.7L512 233l-48.8-31.3A255.2 255.2 0 0 0 324.8 161H96c-17.7 0-32 14.3-32 32v568c0 17.7 14.3 32 32 32h228.8c49.1 0 97.1 14.1 138.4 40.7l44.4 28.6c1.3.8 2.8 1.3 4.3 1.3s3-.4 4.3-1.3l44.4-28.6C602 807.1 650.1 793 699.2 793H928c17.7 0 32-14.3 32-32V193c0-17.7-14.3-32-32-32M404 553.5c0 4.1-3.2 7.5-7.1 7.5H211.1c-3.9 0-7.1-3.4-7.1-7.5v-45c0-4.1 3.2-7.5 7.1-7.5h185.7c3.9 0 7.1 3.4 7.1 7.5v45zm0-140c0 4.1-3.2 7.5-7.1 7.5H211.1c-3.9 0-7.1-3.4-7.1-7.5v-45c0-4.1 3.2-7.5 7.1-7.5h185.7c3.9 0 7.1 3.4 7.1 7.5v45zm416 140c0 4.1-3.2 7.5-7.1 7.5H627.1c-3.9 0-7.1-3.4-7.1-7.5v-45c0-4.1 3.2-7.5 7.1-7.5h185.7c3.9 0 7.1 3.4 7.1 7.5v45zm0-140c0 4.1-3.2 7.5-7.1 7.5H627.1c-3.9 0-7.1-3.4-7.1-7.5v-45c0-4.1 3.2-7.5 7.1-7.5h185.7c3.9 0 7.1 3.4 7.1 7.5v45z"
)

// workspaceIconSVGs frame each outline's ink box as an SVG viewBox, drawn in
// the current text colour. The dock draws one at exactly that box's size, so
// the rasterizer works at the display's resolution for the size shown:
// drawing the outline through a transform instead would rasterize it in its
// design units first, which leaves the 24-unit Video and 16-unit 稍后读 glyphs
// blurry at 30 pt, or the 1024-unit 微信读书 one in an oversized mask.
var workspaceIconSVGs = [workspaceIconKindCount]*ui.SVG{
	WorkspaceIconVideos:      workspaceIconSVG(workspaceVideoPathD),
	WorkspaceIconRSS:         workspaceIconSVG(workspaceRSSPathD),
	WorkspaceIconReadLater:   workspaceIconSVG(workspaceLaterPathD),
	WorkspaceIconWeChatNotes: workspaceIconSVG(workspaceWeChatPathD),
}

// workspaceIconSVG frames d's ink box as an SVG's viewBox. The outline is the
// supplied one, unchanged: only the viewBox says which part of its coordinate
// space the icon shows. The paths cut their inner shapes (Video's play triangle)
// out by winding, which only even-odd fills as the icon set draws them; the
// other three glyphs paint the same either way.
func workspaceIconSVG(d string) *ui.SVG {
	b := ui.MustParsePath(d).Bounds()
	return ui.MustParseSVG([]byte(fmt.Sprintf(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="%g %g %g %g" fill-rule="evenodd"><path fill="currentColor" d="%s"/></svg>`,
		b.X, b.Y, b.W, b.H, d)))
}

// WorkspaceIconSVG returns kind's glyph, framed at its ink box, or nil for an
// unknown kind.
func WorkspaceIconSVG(kind WorkspaceIconKind) *ui.SVG {
	if int(kind) >= len(workspaceIconSVGs) {
		return nil
	}
	return workspaceIconSVGs[kind]
}

// WorkspaceIconColor returns the token that colours kind in the dock. The
// glyph keeps its own colour in every interaction state, so selection shows in
// the slot's background instead of tinting the glyph.
func (t Tokens) WorkspaceIconColor(kind WorkspaceIconKind) ui.Color {
	switch kind {
	case WorkspaceIconVideos:
		return t.WorkspaceVideo
	case WorkspaceIconRSS:
		return t.WorkspaceRSS
	case WorkspaceIconReadLater:
		return t.WorkspaceReadLater
	case WorkspaceIconWeChatNotes:
		return t.WorkspaceWeChat
	}
	return t.Muted
}

// WorkspaceIconRect returns where svg's ink box lands when drawn into r: its
// measured ink is WorkspaceIconSize dp tall, its design aspect kept, and its
// centre on r's centre. An empty SVG has an empty box.
func WorkspaceIconRect(svg *ui.SVG, r ui.Rect) ui.Rect {
	if svg == nil {
		return ui.Rect{}
	}
	bw, bh := svg.Size()
	if bw <= 0 || bh <= 0 {
		return ui.Rect{}
	}
	scale := WorkspaceIconSize / bh
	w, h := bw*scale, bh*scale
	return ui.Rect{X: r.X + (r.W-w)/2, Y: r.Y + (r.H-h)/2, W: w, H: h}
}

// WorkspaceIconDraw paints kind's glyph into r in c, sized and centred by
// WorkspaceIconRect. The scale comes from the glyph's own ink box at draw time,
// so the four glyphs read as one size, and nothing is parsed or allocated
// here: the outline and its framed SVG are made once at init.
func WorkspaceIconDraw(p *ui.Painter, r ui.Rect, kind WorkspaceIconKind, c ui.Color) {
	svg := WorkspaceIconSVG(kind)
	ink := WorkspaceIconRect(svg, r)
	if ink.W <= 0 || ink.H <= 0 {
		return
	}
	p.Icon(svg, ink, c)
}
