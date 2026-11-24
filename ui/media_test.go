package ui

import "testing"

// TestThumbMaxSideSamplesAtTwoPixelsPerPoint pins the density a thumbnail is
// decoded at: two pixels per point, the density every Mac retina screen draws,
// rounded up to the cache's step so a resize asks for a handful of sizes.
// Three - an iPhone's - kept 2.25 times the pixels the screen shows.
func TestThumbMaxSideSamplesAtTwoPixelsPerPoint(t *testing.T) {
	const (
		retinaDensity = 2
		step          = 64
	)
	cases := []struct {
		name string
		w    float32
		want int
	}{
		{"narrow card", 160, 320},
		{"default card", 280, 576},
		{"wide card", 332, 704},
		{"fractional width rounds up", 280.5, 576},
	}
	for _, c := range cases {
		got := thumbMaxSide(c.w)
		if got != c.want {
			t.Errorf("%s: thumbMaxSide(%v) = %d, want %d", c.name, c.w, got, c.want)
		}
		want := float64(c.w) * retinaDensity
		if float64(got) < want || float64(got) >= want+step {
			t.Errorf("%s: thumbMaxSide(%v) = %d, want a 2x side in [%v, %v)", c.name, c.w, got, want, want+step)
		}
		if got%step != 0 {
			t.Errorf("%s: thumbMaxSide(%v) = %d, not on the %d px step", c.name, c.w, got, step)
		}
	}
}

// TestThumbMaxSideDefaultsToTheWidestCardAtTwoX pins the cap for a frame whose
// width its caller leaves to the layout: the grid's widest card at two pixels
// per point.
func TestThumbMaxSideDefaultsToTheWidestCardAtTwoX(t *testing.T) {
	const wantDefault = 640
	for _, w := range []float32{0, -1} {
		if got := thumbMaxSide(w); got != wantDefault {
			t.Errorf("thumbMaxSide(%v) = %d, want the 2x default %d", w, got, wantDefault)
		}
	}
}
