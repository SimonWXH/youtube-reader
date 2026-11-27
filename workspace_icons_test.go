package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/egoist/mygo/ui"
	mui "github.com/simon/youtube-reader/ui"
)

// dockIcons are the four supplied glyphs with the design box each was drawn
// in. The design box is a coordinate space only: WorkspaceIconSize is what the
// dock draws.
var dockIcons = []struct {
	id     string
	kind   mui.WorkspaceIconKind
	name   string
	design float32
}{
	{WorkspaceVideos, mui.WorkspaceIconVideos, "Video", 24},
	{WorkspaceRSS, mui.WorkspaceIconRSS, "RSS", 32},
	{WorkspaceReadLater, mui.WorkspaceIconReadLater, "Later", 16},
	{WorkspaceWeChatNotes, mui.WorkspaceIconWeChatNotes, "WeChat Readings", 1024},
}

// dockIconHexes are the colours the four active glyphs are specified to paint,
// per appearance (0 light, 1 dark), taken from the icon set's spec rather than
// from the theme: the tokens and the painted pixels both answer to these, so a
// swapped or mistyped token cannot satisfy itself.
var dockIconHexes = map[mui.WorkspaceIconKind][2]string{
	mui.WorkspaceIconVideos:      {"#ff0033", "#ff5b74"},
	mui.WorkspaceIconRSS:         {"#000000", "#f2f2f3"},
	mui.WorkspaceIconReadLater:   {"#f46b26", "#ff8b52"},
	mui.WorkspaceIconWeChatNotes: {"#35a8ff", "#6cc3ff"},
}

// dockIdleHexes are the muted grey colours specified for unselected dock
// glyphs, per appearance (0 light, 1 dark).
var dockIdleHexes = [2]string{"#7a7a7a", "#919294"}

// dockIconColour is the spec colour of kind when active in the light or dark appearance.
func dockIconColour(kind mui.WorkspaceIconKind, dark bool) ui.Color {
	hexes, ok := dockIconHexes[kind]
	if !ok {
		return ui.Color{}
	}
	if dark {
		return ui.Hex(hexes[1])
	}
	return ui.Hex(hexes[0])
}

// dockIdleColour is the spec colour of an unselected dock glyph in the light or dark appearance.
func dockIdleColour(dark bool) ui.Color {
	if dark {
		return ui.Hex(dockIdleHexes[1])
	}
	return ui.Hex(dockIdleHexes[0])
}

// dockTitles are the English and Chinese workspace names, which are also the
// only thing the dock's tooltip says.
var dockTitles = map[string][2]string{
	WorkspaceVideos:      {"Videos", "视频"},
	WorkspaceRSS:         {"RSS", "RSS 文章"},
	WorkspaceReadLater:   {"Read Later", "稍后读"},
	WorkspaceWeChatNotes: {"WeChat Notes", "微信读书笔记"},
}

// TestWorkspaceDockIconPathsParse checks that all four supplied paths parse
// into real ink: each has a non-empty box no larger than its design box, so a
// truncated or wrong d attribute fails here rather than drawing nothing.
func TestWorkspaceDockIconPathsParse(t *testing.T) {
	for _, icon := range dockIcons {
		t.Run(icon.name, func(t *testing.T) {
			svg := mui.WorkspaceIconSVG(icon.kind)
			if svg == nil {
				t.Fatal("glyph missing")
			}
			w, h := svg.Size()
			if w <= 0 || h <= 0 {
				t.Fatalf("ink box %gx%g is empty", w, h)
			}
			if w > icon.design+0.5 || h > icon.design+0.5 {
				t.Fatalf("ink box %gx%g exceeds its %g-unit design box", w, h, icon.design)
			}
		})
	}
}

// TestWorkspaceDockIconNormalization checks the measured-ink normalization:
// each glyph is WorkspaceIconSize tall whatever its design box and sits on the
// centre of a 46 pt slot, so the four read as one size and the row does not
// move.
func TestWorkspaceDockIconNormalization(t *testing.T) {
	// The narrowest slot: a 220 pt sidebar, its 12 pt dock padding, and the
	// three 4 pt gaps.
	slot := ui.Rect{X: 6, Y: 4, W: (220 - 12 - 12) / 4, H: 46}
	for _, icon := range dockIcons {
		t.Run(icon.name, func(t *testing.T) {
			svg := mui.WorkspaceIconSVG(icon.kind)
			bw, bh := svg.Size()
			ink := mui.WorkspaceIconRect(svg, slot)
			if math.Abs(float64(ink.H-mui.WorkspaceIconSize)) > 0.5 {
				t.Fatalf("ink height = %g, want %g ± 0.5", ink.H, mui.WorkspaceIconSize)
			}
			if math.Abs(float64(ink.X+ink.W/2-(slot.X+slot.W/2))) > 0.5 {
				t.Fatalf("ink centre x = %g, want the slot's %g ± 0.5", ink.X+ink.W/2, slot.X+slot.W/2)
			}
			if math.Abs(float64(ink.Y+ink.H/2-(slot.Y+slot.H/2))) > 0.5 {
				t.Fatalf("ink centre y = %g, want the slot's %g ± 0.5", ink.Y+ink.H/2, slot.Y+slot.H/2)
			}
			if want := bw / bh; math.Abs(float64(ink.W/ink.H-want)) > 0.001 {
				t.Fatalf("ink aspect = %g, want the glyph's %g", ink.W/ink.H, want)
			}
			if ink.Y < slot.Y || ink.Y+ink.H > slot.Y+slot.H {
				t.Fatalf("ink %+v does not fit the %+v slot", ink, slot)
			}
			if ink.W > slot.W {
				t.Fatalf("ink %g wide overflows the %g wide slot", ink.W, slot.W)
			}
		})
	}
}

// TestWorkspaceDockTooltipIsWorkspaceName checks the dock tooltip is the
// workspace name alone, in both locales, with no digits and no unread wording,
// and that nothing else in the dock row carries text: the numeral is gone.
func TestWorkspaceDockTooltipIsWorkspaceName(t *testing.T) {
	_, w := setupWorkspaceTestApp(t)
	tt := ui.NewTester(w.view, 1280, 808)
	tt.Frame()

	for _, locale := range []struct {
		code  string
		index int
	}{{"en", 0}, {"zh-CN", 1}} {
		t.Run(locale.code, func(t *testing.T) {
			tt.SetLocale(locale.code)
			tt.Frame()
			for _, icon := range dockIcons {
				want := dockTitles[icon.id][locale.index]
				meta := dockMeta(locale.code, icon.id)
				if meta.Title != want {
					t.Fatalf("%s title = %q, want %q", icon.id, meta.Title, want)
				}
				info, ok := tt.Access(want)
				if !ok {
					t.Fatalf("dock item %q is not in the accessibility tree", want)
				}
				if info.Description != want {
					t.Fatalf("%s tooltip = %q, want the workspace name %q", icon.id, info.Description, want)
				}
				if strings.ContainsFunc(info.Description, unicode.IsDigit) {
					t.Fatalf("%s tooltip %q carries a number", icon.id, info.Description)
				}
				for _, word := range []string{"unread", "未读", "unwatched", "未看"} {
					if strings.Contains(info.Description, word) {
						t.Fatalf("%s tooltip %q still counts %q", icon.id, info.Description, word)
					}
				}
			}

			// The dock row shows the four tabs and nothing else, so no
			// numeral, dot or badge is painted there.
			names := map[string]bool{}
			for _, icon := range dockIcons {
				names[dockTitles[icon.id][locale.index]] = true
			}
			row, ok := dockRowBounds(tt)
			if !ok {
				t.Fatal("the dock row is not in the accessibility tree")
			}
			for _, info := range tt.AccessAll() {
				if info.Bounds.W <= 0 || info.Bounds.H <= 0 {
					continue
				}
				if info.Bounds.X < row.X-0.5 || info.Bounds.X+info.Bounds.W > row.X+row.W+0.5 ||
					info.Bounds.Y < row.Y-0.5 || info.Bounds.Y+info.Bounds.H > row.Y+row.H+0.5 {
					continue
				}
				if strings.ContainsFunc(info.Name, unicode.IsDigit) {
					t.Fatalf("the dock row still shows the text %q", info.Name)
				}
				switch info.Role {
				case ui.RoleTabList:
				case ui.RoleTab:
					if !names[info.Name] {
						t.Fatalf("unexpected dock tab %q", info.Name)
					}
				default:
					t.Fatalf("the dock row has a %v %q where only the four tabs belong", info.Role, info.Name)
				}
			}
		})
	}
}

// dockRowBounds returns the box of the dock's tab list, which is the row the
// four entrances live in.
func dockRowBounds(tt *ui.Tester) (ui.Rect, bool) {
	for _, info := range tt.AccessAll() {
		if info.Role == ui.RoleTabList && info.Name == "Workspaces" {
			return info.Bounds, true
		}
	}
	return ui.Rect{}, false
}

// TestWorkspaceDockRSSColourSurvivesDarkMode checks the RSS glyph leaves black
// for near-white ink in the dark appearance, on the sidebar it sits on.
func TestWorkspaceDockRSSColourSurvivesDarkMode(t *testing.T) {
	if got, want := mui.NewTokens(nil, false).WorkspaceRSS, dockIconColour(mui.WorkspaceIconRSS, false); got != want {
		t.Fatalf("light RSS token = %v, want black %v", got, want)
	}
	dark := mui.NewTokens(nil, true)
	if got, want := dark.WorkspaceRSS, dockIconColour(mui.WorkspaceIconRSS, true); got != want {
		t.Fatalf("dark RSS token = %v, want %v", got, want)
	}
	for _, tc := range []struct {
		name string
		ink  ui.Color
		bg   ui.Color
	}{
		{"dark", dark.WorkspaceRSS, dark.Sidebar},
	} {
		if ratio := dockContrast(tc.ink, tc.bg); ratio < 3 {
			t.Fatalf("%s RSS glyph contrast on its background = %.2f:1, want at least 3:1", tc.name, ratio)
		}
	}
}

// dockContrast is the WCAG 2.1 contrast ratio of two opaque sRGB colours.
func dockContrast(fg, bg ui.Color) float64 {
	lum := func(c ui.Color) float64 {
		channel := func(v uint8) float64 {
			s := float64(v) / 255
			if s <= 0.04045 {
				return s / 12.92
			}
			return math.Pow((s+0.055)/1.055, 2.4)
		}
		return 0.2126*channel(c.R) + 0.7152*channel(c.G) + 0.0722*channel(c.B)
	}
	a, b := lum(fg), lum(bg)
	if a < b {
		a, b = b, a
	}
	return (a + 0.05) / (b + 0.05)
}

// TestWorkspaceDockIconsRender checks the app's own pixels: in both
// appearances every slot paints its own glyph 26 dp tall and centred in the
// slot, the selected slot paints in its workspace brand colour (including dark
// mode's near-white RSS), and the remaining three paint in the muted idle grey.
// A placeholder workspace follows the same selection colour rule: its
// placeholder state is content, not ink.
func TestWorkspaceDockIconsRender(t *testing.T) {
	_, w := setupWorkspaceTestApp(t)
	const width, height = 1280, 808
	const scale float32 = 2
	tt := ui.NewTester(w.view, width, height)
	tt.SetScale(scale)

	for _, appearance := range []struct {
		name string
		dark bool
		tok  mui.Tokens
	}{
		{"light", false, mui.NewTokens(nil, false)},
		{"dark", true, mui.NewTokens(nil, true)},
	} {
		idleColour := dockIdleColour(appearance.dark)
		for _, icon := range dockIcons {
			colour := dockIconColour(icon.kind, appearance.dark)
			if got := appearance.tok.WorkspaceIconColor(icon.kind, true); got != colour {
				t.Fatalf("%s active %s token = %v, want the spec colour %v", appearance.name, icon.name, got, colour)
			}
			if got := appearance.tok.WorkspaceIconColor(icon.kind, false); got != idleColour {
				t.Fatalf("%s idle %s token = %v, want the spec idle colour %v", appearance.name, icon.name, got, idleColour)
			}
		}
		t.Run(appearance.name, func(t *testing.T) {
			tt.SetDark(appearance.dark)
			// The appearance change animates colours, and the Tester's
			// clock stands still until it moves.
			tt.Advance(mui.MotionInteraction + 20*time.Millisecond)

			for _, active := range dockIcons {
				w.selectWorkspace(active.id)
				tt.Frame()
				img := tt.Image()
				if img == nil {
					t.Fatal("no frame rendered")
				}
				heights := map[string]float32{}
				for _, icon := range dockIcons {
					want := dockTitles[icon.id][0]
					slot, ok := tt.Find(want)
					if !ok {
						t.Fatalf("dock slot %q not found", want)
					}
					expectedColour := idleColour
					if icon.id == active.id {
						expectedColour = dockIconColour(icon.kind, appearance.dark)
					}
					ink, count := dockInkOf(img, slot, scale)
					if count < 80 {
						t.Fatalf("active=%s %s glyph painted %d pixels, want a real glyph", active.name, icon.name, count)
					}
					if !dockShows(img, ink, expectedColour) {
						t.Fatalf("active=%s %s glyph is not painted in %v", active.name, icon.name, expectedColour)
					}
					if icon.id != active.id {
						brandColour := dockIconColour(icon.kind, appearance.dark)
						if brandColour != expectedColour && dockShows(img, ink, brandColour) {
							t.Fatalf("active=%s unselected %s glyph still paints brand colour %v instead of %v", active.name, icon.name, brandColour, expectedColour)
						}
					}
					bw, bh := mui.WorkspaceIconSVG(icon.kind).Size()
					gotH := float32(ink.Dy()) / scale
					gotW := float32(ink.Dx()) / scale
					heights[icon.name] = gotH
					if math.Abs(float64(gotH-mui.WorkspaceIconSize)) > 0.75 {
						t.Fatalf("active=%s %s glyph is %g dp tall, want %g ± 0.75", active.name, icon.name, gotH, mui.WorkspaceIconSize)
					}
					if want := float64(bw / bh); math.Abs(float64(gotW/gotH)-want) > 0.06 {
						t.Fatalf("active=%s %s glyph aspect = %g, want the glyph's %g", active.name, icon.name, gotW/gotH, want)
					}
					cx := (float32(ink.Min.X+ink.Max.X)/2 + 0.5) / scale
					cy := (float32(ink.Min.Y+ink.Max.Y)/2 + 0.5) / scale
					if math.Abs(float64(cx-(slot.X+slot.W/2))) > 1 {
						t.Fatalf("active=%s %s glyph centre x = %g, want the slot's %g ± 1", active.name, icon.name, cx, slot.X+slot.W/2)
					}
					if math.Abs(float64(cy-(slot.Y+slot.H/2))) > 1 {
						t.Fatalf("active=%s %s glyph centre y = %g, want the slot's %g ± 1", active.name, icon.name, cy, slot.Y+slot.H/2)
					}
				}
				// The four glyphs read as one size: their painted heights
				// agree within half a device pixel.
				var lo, hi float32 = math.MaxFloat32, 0
				for _, h := range heights {
					lo, hi = min(lo, h), max(hi, h)
				}
				if hi-lo > 0.5 {
					t.Fatalf("active=%s glyph heights %v differ by %g dp, want them equal within 0.5", active.name, heights, hi-lo)
				}
			}
			// Restore initial Videos selection before leaving the subtest.
			w.selectWorkspace(WorkspaceVideos)
			tt.Frame()
		})
	}
}

// TestWorkspaceDockStateCues checks the dock's state cues are drawn beside the
// ink, never behind it: the active tab carries no edge bar (selection is shown
// by glyph colour alone), hover draws a faint outline and press a stronger one,
// the glyphs keep at least their idle contrast, and each state reads
// differently from the next in both appearances.
func TestWorkspaceDockStateCues(t *testing.T) {
	_, w := setupWorkspaceTestApp(t)
	const scale float32 = 2
	tt := ui.NewTester(w.view, 1280, 808)
	tt.SetScale(scale)

	for _, appearance := range []struct {
		name string
		dark bool
		tok  mui.Tokens
	}{
		{"light", false, mui.NewTokens(nil, false)},
		{"dark", true, mui.NewTokens(nil, true)},
	} {
		t.Run(appearance.name, func(t *testing.T) {
			tok := appearance.tok
			tt.SetDark(appearance.dark)
			tt.Advance(mui.MotionInteraction + 20*time.Millisecond)
			tt.Move(1, 1)
			tt.Frame()

			active, ok := tt.Find(dockTitles[WorkspaceVideos][0])
			if !ok {
				t.Fatal("the active dock slot is not in the accessibility tree")
			}
			idle, ok := tt.Find(dockTitles[WorkspaceRSS][0])
			if !ok {
				t.Fatal("the idle dock slot is not in the accessibility tree")
			}
			pixel := func(img *image.RGBA, r ui.Rect, x, y float32) color.RGBA {
				return img.RGBAAt(
					int(math.Round(float64((r.X+x)*scale))),
					int(math.Round(float64((r.Y+y)*scale))))
			}
			want := func(c ui.Color) color.RGBA { return color.RGBA{R: c.R, G: c.G, B: c.B, A: c.A} }
			asColor := func(p color.RGBA) ui.Color { return ui.Color{R: p.R, G: p.G, B: p.B, A: p.A} }
			// has reports whether the slot's top band holds c, which is
			// where an outline is drawn.
			has := func(img *image.RGBA, r ui.Rect, band float32, c ui.Color) bool {
				x0 := int(math.Round(float64(r.X * scale)))
				x1 := int(math.Round(float64((r.X + r.W) * scale)))
				y0 := int(math.Round(float64(r.Y * scale)))
				y1 := int(math.Round(float64((r.Y + band) * scale)))
				for y := y0; y < y1; y++ {
					for x := x0; x < x1; x++ {
						if img.RGBAAt(x, y) == want(c) {
							return true
						}
					}
				}
				return false
			}

			hoverOutline := tok.WorkspaceDockHoverOutline
			if ratio := dockContrast(hoverOutline, tok.Sidebar); ratio < 1.5 {
				t.Fatalf("hover outline on the sidebar = %.2f:1, want at least 1.5:1", ratio)
			}

			// The slot interior just above the glyph: the background each
			// glyph sits on in that state.
			img := tt.Image()
			idleBG := pixel(img, idle, idle.W/2, 4)
			if idleBG != want(tok.Sidebar) {
				t.Fatalf("idle slot background = %v, want the dock background %v", idleBG, tok.Sidebar)
			}
			activeBG := pixel(img, active, active.W/2, 4)
			if activeBG != idleBG {
				t.Fatalf("active slot background = %v, want the dock background %v: no state paints behind the ink", activeBG, idleBG)
			}
			if got := pixel(img, active, 1, active.H/2); got != activeBG {
				t.Fatalf("active slot edge = %v, want the dock background %v: the edge bar should be gone", got, activeBG)
			}
			if has(img, active, 2, hoverOutline) {
				t.Fatal("the active slot draws a hover outline while idle")
			}
			for _, icon := range dockIcons {
				ink := dockIconColour(icon.kind, appearance.dark)
				if got, want := dockContrast(ink, asColor(activeBG)), dockContrast(ink, asColor(idleBG)); got < want-0.001 {
					t.Fatalf("%s glyph contrast on the active slot = %.2f:1, want at least its idle %.2f:1", icon.name, got, want)
				}
			}

			tt.Move(idle.X+idle.W/2, idle.Y+idle.H/2)
			tt.Frame()
			img = tt.Image()
			if got := pixel(img, idle, idle.W/2, 4); got != idleBG {
				t.Fatalf("hovered slot background = %v, want the dock background %v: hover paints no plate", got, idleBG)
			}
			if !has(img, idle, 2, hoverOutline) {
				t.Fatalf("the hovered slot draws no %v outline", hoverOutline)
			}
			if got := pixel(img, idle, 3, idle.H/2); got != idleBG {
				t.Fatalf("hovered slot edge = %v, want the dock background %v", got, idleBG)
			}

			tt.Press(idle.X+idle.W/2, idle.Y+idle.H/2)
			tt.Frame()
			img = tt.Image()
			if !has(img, idle, 2, tok.Accent) {
				t.Fatalf("the pressed slot draws no accent %v outline", tok.Accent)
			}
			if has(img, idle, 2, hoverOutline) {
				t.Fatalf("the pressed slot keeps the hover outline %v", hoverOutline)
			}
			if ratio := dockContrast(tok.Accent, hoverOutline); ratio < 3 {
				t.Fatalf("pressed outline against the hovered outline = %.2f:1, want at least 3:1", ratio)
			}
			tt.CancelPress()
			tt.Move(1, 1)
			tt.Frame()

			// The active tab still shows its own hover and pressed states
			// with no edge bar.
			tt.Move(active.X+active.W/2, active.Y+active.H/2)
			tt.Frame()
			img = tt.Image()
			if !has(img, active, 2, hoverOutline) {
				t.Fatal("hovering the active slot draws no hover outline")
			}
			if got := pixel(img, active, 1, active.H/2); got != activeBG {
				t.Fatalf("active slot edge while hovered = %v, want the dock background %v", got, activeBG)
			}
			tt.Press(active.X+active.W/2, active.Y+active.H/2)
			tt.Frame()
			img = tt.Image()
			if !has(img, active, 2, tok.Accent) {
				t.Fatal("pressing the active slot draws no press outline")
			}
			if got := pixel(img, active, 1, active.H/2); got != activeBG {
				t.Fatalf("active slot edge while pressed = %v, want the dock background %v", got, activeBG)
			}
			tt.CancelPress()
			tt.Move(1, 1)
			tt.Frame()

			// The insert caret runs the slot's full height on drop targets.
			tt.Press(idle.X+idle.W/2, idle.Y+idle.H/2)
			tt.Move(idle.X+idle.W/2, idle.Y+idle.H/2-5)
			tt.Move(active.X+active.W/4, active.Y+active.H/2)
			tt.Frame()
			img = tt.Image()
			if got := pixel(img, active, 1, 3); got != want(tok.Accent) {
				t.Fatalf("drop caret top = %v, want the accent %v", got, tok.Accent)
			}
			if got := pixel(img, active, 1, active.H-3); got != want(tok.Accent) {
				t.Fatalf("drop caret bottom = %v, want the accent %v", got, tok.Accent)
			}
			tt.CancelPress()
			tt.Move(1, 1)
			tt.Frame()
		})
	}
}

// TestWorkspaceDockReportsActiveTab checks the dock tells assistive technology
// which workspace is active, and that the report follows a workspace switch.
func TestWorkspaceDockReportsActiveTab(t *testing.T) {
	_, w := setupWorkspaceTestApp(t)
	tt := ui.NewTester(w.view, 1280, 808)
	tt.Frame()

	selected := func() map[string]bool {
		got := map[string]bool{}
		for _, icon := range dockIcons {
			name := dockTitles[icon.id][0]
			info, ok := tt.AccessByRole(ui.RoleTab, name)
			if !ok {
				t.Fatalf("dock item %q is not a tab in the accessibility tree", name)
			}
			got[icon.id] = info.Selected
		}
		return got
	}

	if got := selected(); !got[WorkspaceVideos] || got[WorkspaceRSS] || got[WorkspaceReadLater] || got[WorkspaceWeChatNotes] {
		t.Fatalf("active dock tabs = %v, want only Videos", got)
	}
	if err := tt.Click(dockTitles[WorkspaceRSS][0]); err != nil {
		t.Fatalf("clicking the RSS dock tab: %v", err)
	}
	tt.Frame()
	if got := selected(); !got[WorkspaceRSS] || got[WorkspaceVideos] || got[WorkspaceReadLater] || got[WorkspaceWeChatNotes] {
		t.Fatalf("active dock tabs after switching to RSS = %v, want only RSS", got)
	}
}

// dockInkOf returns the device-pixel box of the pixels inside the slot that
// differ from its background, and how many they are. The slot's rounded
// corners and its edge cue are skipped: no glyph comes that close.
func dockInkOf(img *image.RGBA, r ui.Rect, scale float32) (image.Rectangle, int) {
	// A slot's edge carries its state cue, the accent bar or the hover and
	// press outlines, so the scan keeps 4 dp clear of it; no glyph comes
	// that close.
	const rim = 4
	x0 := int(math.Round(float64((r.X + rim) * scale)))
	y0 := int(math.Round(float64((r.Y + rim) * scale)))
	x1 := int(math.Round(float64((r.X + r.W - rim) * scale)))
	y1 := int(math.Round(float64((r.Y + r.H - rim) * scale)))
	// The slot's leading edge at its middle, clear of every glyph and of
	// the state cue, holds the background the glyph is painted over.
	bg := img.RGBAAt(x0+int(3*scale), int(math.Round(float64((r.Y+r.H/2)*scale))))
	var box image.Rectangle
	count := 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if !dockInsideSlot(r, (float32(x)+0.5)/scale, (float32(y)+0.5)/scale) {
				continue
			}
			if !dockDiffers(img.RGBAAt(x, y), bg) {
				continue
			}
			if count == 0 {
				box = image.Rect(x, y, x+1, y+1)
			} else {
				box = box.Union(image.Rect(x, y, x+1, y+1))
			}
			count++
		}
	}
	return box, count
}

// dockInsideSlot reports whether (x, y) is inside r's rounded corners.
func dockInsideSlot(r ui.Rect, x, y float32) bool {
	rad := mui.RadiusControl
	cx := min(max(x, r.X+rad), r.X+r.W-rad)
	cy := min(max(y, r.Y+rad), r.Y+r.H-rad)
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= rad*rad
}

// dockDiffers reports whether p is visibly a different colour than bg.
func dockDiffers(p, bg color.RGBA) bool {
	d := absDiff(int(p.R), int(bg.R))
	d += absDiff(int(p.G), int(bg.G))
	d += absDiff(int(p.B), int(bg.B))
	return d > 24
}

// dockShows reports whether the ink box holds c, so a grey placeholder, a
// tinted selection, or a missing glyph fails.
func dockShows(img *image.RGBA, box image.Rectangle, c ui.Color) bool {
	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			p := img.RGBAAt(x, y)
			if p.R == c.R && p.G == c.G && p.B == c.B && p.A == 255 {
				return true
			}
		}
	}
	return false
}

// TestWorkspaceDockScreenshot writes the dock in both appearances under
// YTREADER_DOCK_SCREENSHOTS, so a design review reads the app's own pixels.
func TestWorkspaceDockScreenshot(t *testing.T) {
	dir := os.Getenv("YTREADER_DOCK_SCREENSHOTS")
	if dir == "" {
		t.Skip("YTREADER_DOCK_SCREENSHOTS is unset")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	_, w := setupWorkspaceTestApp(t)
	tt := ui.NewTester(w.view, 1280, 808)
	tt.SetScale(2)
	for _, dark := range []bool{false, true} {
		tt.SetDark(dark)
		for _, workspace := range []string{WorkspaceVideos, WorkspaceRSS} {
			w.selectWorkspace(workspace)
			tt.Frame()
			tt.Advance(mui.MotionInteraction + 20*time.Millisecond)
			tt.Frame()
			appearance := "light"
			if dark {
				appearance = "dark"
			}
			var buf bytes.Buffer
			if err := png.Encode(&buf, tt.Image()); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "dock-"+workspace+"-"+appearance+".png")
			if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Logf("wrote %s", path)
		}
	}
}
