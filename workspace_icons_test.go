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

// dockTitles are the English and Chinese workspace names, which are also the
// only thing the dock's tooltip says.
var dockTitles = map[string][2]string{
	WorkspaceVideos:      {"Videos", "视频"},
	WorkspaceRSS:         {"RSS", "RSS 文章"},
	WorkspaceReadLater:   {"Read Later", "稍后读"},
	WorkspaceWeChatNotes: {"WeChat Notes", "微信读书笔记"},
}

// TestWorkspaceDockIconPathsParse checks that all four supplied paths parse
// into real ink: each has a non-empty box inside its design box, so a
// truncated or wrong d attribute fails here rather than drawing nothing.
func TestWorkspaceDockIconPathsParse(t *testing.T) {
	for _, icon := range dockIcons {
		t.Run(icon.name, func(t *testing.T) {
			path := mui.WorkspaceIconPath(icon.kind)
			if path == nil {
				t.Fatal("path missing")
			}
			b := path.Bounds()
			if b.W <= 0 || b.H <= 0 {
				t.Fatalf("ink box %+v is empty", b)
			}
			if b.W > icon.design+0.5 || b.H > icon.design+0.5 {
				t.Fatalf("ink box %+v exceeds its %g-unit design box", b, icon.design)
			}
			if b.X < -0.5 || b.Y < -0.5 {
				t.Fatalf("ink box %+v starts outside its design box", b)
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
			path := mui.WorkspaceIconPath(icon.kind)
			b := path.Bounds()
			ink := mui.WorkspaceIconRect(path, slot)
			if math.Abs(float64(ink.H-mui.WorkspaceIconSize)) > 0.5 {
				t.Fatalf("ink height = %g, want %g ± 0.5", ink.H, mui.WorkspaceIconSize)
			}
			if math.Abs(float64(ink.X+ink.W/2-(slot.X+slot.W/2))) > 0.5 {
				t.Fatalf("ink centre x = %g, want the slot's %g ± 0.5", ink.X+ink.W/2, slot.X+slot.W/2)
			}
			if math.Abs(float64(ink.Y+ink.H/2-(slot.Y+slot.H/2))) > 0.5 {
				t.Fatalf("ink centre y = %g, want the slot's %g ± 0.5", ink.Y+ink.H/2, slot.Y+slot.H/2)
			}
			if want := b.W / b.H; math.Abs(float64(ink.W/ink.H-want)) > 0.001 {
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
				meta := w.dockMeta(locale.code, icon.id)
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
	if got, want := mui.NewTokens(nil, false).WorkspaceRSS, ui.Hex("#000000"); got != want {
		t.Fatalf("light RSS token = %v, want black %v", got, want)
	}
	dark := mui.NewTokens(nil, true)
	if got, want := dark.WorkspaceRSS, ui.Hex("#F2F2F3"); got != want {
		t.Fatalf("dark RSS token = %v, want %v", got, want)
	}
	for _, tc := range []struct {
		name string
		ink  ui.Color
		bg   ui.Color
	}{
		{"dark", dark.WorkspaceRSS, dark.Sidebar},
		{"dark selected", dark.WorkspaceRSS, dark.AccentSoft},
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
// appearances every slot paints its own glyph 30 dp tall and centred in the
// slot, and the selected slot keeps that colour over its AccentSoft
// background instead of tinting the glyph. A placeholder workspace is coloured
// like the others: its placeholder state is content, not ink.
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
		t.Run(appearance.name, func(t *testing.T) {
			tt.SetDark(appearance.dark)
			// The appearance change animates colours, and the Tester's
			// clock stands still until it moves.
			tt.Advance(mui.MotionInteraction + 20*time.Millisecond)
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
				// Every glyph, a placeholder workspace's included,
				// shows its own colour in every state.
				colour := appearance.tok.WorkspaceIconColor(icon.kind)
				ink, count := dockInkOf(img, slot, scale)
				if count < 100 {
					t.Fatalf("%s glyph painted %d pixels, want a real glyph", icon.name, count)
				}
				if !dockShows(img, ink, colour) {
					t.Fatalf("%s glyph is not painted in %v", icon.name, colour)
				}
				b := mui.WorkspaceIconPath(icon.kind).Bounds()
				gotH := float32(ink.Dy()) / scale
				gotW := float32(ink.Dx()) / scale
				heights[icon.name] = gotH
				if math.Abs(float64(gotH-mui.WorkspaceIconSize)) > 0.75 {
					t.Fatalf("%s glyph is %g dp tall, want %g ± 0.75", icon.name, gotH, mui.WorkspaceIconSize)
				}
				if want := float64(b.W / b.H); math.Abs(float64(gotW/gotH)-want) > 0.06 {
					t.Fatalf("%s glyph aspect = %g, want the glyph's %g", icon.name, gotW/gotH, want)
				}
				cx := (float32(ink.Min.X+ink.Max.X)/2 + 0.5) / scale
				cy := (float32(ink.Min.Y+ink.Max.Y)/2 + 0.5) / scale
				if math.Abs(float64(cx-(slot.X+slot.W/2))) > 1 {
					t.Fatalf("%s glyph centre x = %g, want the slot's %g ± 1", icon.name, cx, slot.X+slot.W/2)
				}
				if math.Abs(float64(cy-(slot.Y+slot.H/2))) > 1 {
					t.Fatalf("%s glyph centre y = %g, want the slot's %g ± 1", icon.name, cy, slot.Y+slot.H/2)
				}
			}
			// The four glyphs read as one size: their painted heights
			// agree within half a device pixel.
			var lo, hi float32 = math.MaxFloat32, 0
			for _, h := range heights {
				lo, hi = min(lo, h), max(hi, h)
			}
			if hi-lo > 0.5 {
				t.Fatalf("glyph heights %v differ by %g dp, want them equal within 0.5", heights, hi-lo)
			}
		})
	}
}

// dockInkOf returns the device-pixel box of the pixels inside the slot that
// differ from its background, and how many they are. The slot's rounded
// corners are skipped: they show the dock behind the selection fill.
func dockInkOf(img *image.RGBA, r ui.Rect, scale float32) (image.Rectangle, int) {
	// The slot's own rim is antialiased into the dock behind it, so the
	// scan keeps 1.5 dp clear of it; no glyph comes that close.
	const rim = 1.5
	x0 := int(math.Round(float64((r.X + rim) * scale)))
	y0 := int(math.Round(float64((r.Y + rim) * scale)))
	x1 := int(math.Round(float64((r.X + r.W - rim) * scale)))
	y1 := int(math.Round(float64((r.Y + r.H - rim) * scale)))
	// The slot's leading edge at its middle, clear of every glyph, holds
	// the background the glyph is painted over.
	bg := img.RGBAAt(x0+int(3*scale), y0+int(r.H/2*scale))
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
