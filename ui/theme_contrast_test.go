package ui

import (
	"math"
	"testing"

	muic "github.com/egoist/mygo/ui"
)

// contrastRatio is the WCAG 2.1 contrast ratio for two opaque sRGB colors
// (https://www.w3.org/TR/WCAG21/#dfn-contrast-ratio). It is computed here from
// the published formula instead of using any value from the token layer.
func contrastRatio(fg, bg muic.Color) float64 {
	l1 := relativeLuminance(fg)
	l2 := relativeLuminance(bg)
	if l1 < l2 {
		l1, l2 = l2, l1
	}
	return (l1 + 0.05) / (l2 + 0.05)
}

func relativeLuminance(c muic.Color) float64 {
	channel := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.04045 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(c.R) + 0.7152*channel(c.G) + 0.0722*channel(c.B)
}

// TestTokenContrastContracts checks the contrast promises the token comments
// make. The minimum is WCAG 2.1 AA for normal text, 4.5:1, and the expected
// colors come from the agreed design board, not from the implementations:
// light Faint #6f7278 and Warning #8a6224, dark Faint #9a9ea6.
func TestTokenContrastContracts(t *testing.T) {
	light := NewTokens(nil, false)
	dark := NewTokens(nil, true)
	const min = 4.5

	for _, tc := range []struct {
		name string
		fg   muic.Color
		bg   muic.Color
	}{
		{"light Faint on Surface", light.Faint, light.Surface},
		{"light Muted on SurfaceAlt", light.Muted, light.SurfaceAlt},
		{"light Muted on Sidebar", light.Muted, light.Sidebar},
		{"light Muted on App", light.Muted, light.App},
		{"light Warning on Surface", light.Warning, light.Surface},
		{"light Warning on SurfaceAlt", light.Warning, light.SurfaceAlt},
		{"light Warning on App", light.Warning, light.App},
		{"dark Faint on Surface", dark.Faint, dark.Surface},
		{"dark Faint on SurfaceAlt", dark.Faint, dark.SurfaceAlt},
		{"dark Faint on Sidebar", dark.Faint, dark.Sidebar},
		{"dark Faint on App", dark.Faint, dark.App},
		{"dark Muted on SurfaceAlt", dark.Muted, dark.SurfaceAlt},
		{"dark Warning on Surface", dark.Warning, dark.Surface},
		{"dark Warning on SurfaceAlt", dark.Warning, dark.SurfaceAlt},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := contrastRatio(tc.fg, tc.bg)
			if got < min {
				t.Fatalf("contrast ratio %.2f:1, want at least %.1f:1", got, min)
			}
		})
	}

	// The pre-fix light values failed the AA floor; guard the correction.
	if got := contrastRatio(muic.Hex("#8b8e95"), light.Surface); got >= min {
		t.Fatalf("old light Faint contrast ratio %.2f:1 unexpectedly passes; the test no longer proves the correction", got)
	}
	if got := contrastRatio(muic.Hex("#956c2e"), light.SurfaceAlt); got >= min {
		t.Fatalf("old light Warning contrast ratio %.2f:1 unexpectedly passes; the test no longer proves the correction", got)
	}
}

// TestDisabledFaceUsesAgreedTokens checks the agreed disabled state: the
// SurfaceAlt fill and the Faint label are unchanged, and the edge is Border
// (the gap table's "底改 SurfaceAlt + Border"), not the softer card rule.
func TestDisabledFaceUsesAgreedTokens(t *testing.T) {
	tokens := NewTokens(nil, false)
	face := tokens.Face(Disabled, false, false)
	if face.Background != tokens.SurfaceAlt {
		t.Fatalf("disabled background = %+v, want SurfaceAlt %+v", face.Background, tokens.SurfaceAlt)
	}
	if face.Border != tokens.Border {
		t.Fatalf("disabled border = %+v, want Border %+v", face.Border, tokens.Border)
	}
	if face.Text != tokens.Faint {
		t.Fatalf("disabled text = %+v, want Faint %+v", face.Text, tokens.Faint)
	}
}
