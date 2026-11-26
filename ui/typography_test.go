package ui

import "testing"

// TestArticleTypeLadder pins the reading view's three steps and each step's
// heading ladder: the body sizes the control offers, a line height that keeps
// long-form prose comfortable, three section levels that always step down in
// size and never invert, and a vertical rhythm that grows with the letter. The
// middle step is the published Article tokens so the reader's default page and
// the type scale cannot drift apart.
func TestArticleTypeLadder(t *testing.T) {
	for _, tc := range []struct {
		name       string
		body       TextStyle
		title      TextStyle
		h2, h3, h4 TextStyle
	}{
		{
			name:  "small",
			body:  TextStyle{Size: 13, LineHeight: 21, Weight: Regular},
			title: TextStyle{Size: 21, LineHeight: 27, Weight: Semibold, Tracking: -0.2},
			h2:    TextStyle{Size: 18, LineHeight: 28, Weight: Semibold, Tracking: -0.1},
			h3:    TextStyle{Size: 16, LineHeight: 25, Weight: Semibold, Tracking: -0.1},
			h4:    TextStyle{Size: 15, LineHeight: 22, Weight: Semibold, Tracking: -0.1},
		},
		{
			name:  "default",
			body:  TextStyle{Size: 15, LineHeight: 24, Weight: Regular},
			title: TextStyle{Size: 21, LineHeight: 27, Weight: Semibold, Tracking: -0.2},
			h2:    TextStyle{Size: 20, LineHeight: 28, Weight: Semibold, Tracking: -0.1},
			h3:    TextStyle{Size: 18, LineHeight: 25, Weight: Semibold, Tracking: -0.1},
			h4:    TextStyle{Size: 16, LineHeight: 22, Weight: Semibold, Tracking: -0.1},
		},
		{
			name:  "large",
			body:  TextStyle{Size: 17, LineHeight: 27, Weight: Regular},
			title: TextStyle{Size: 27, LineHeight: 34, Weight: Semibold, Tracking: -0.2},
			h2:    TextStyle{Size: 22, LineHeight: 28, Weight: Semibold, Tracking: -0.1},
			h3:    TextStyle{Size: 20, LineHeight: 25, Weight: Semibold, Tracking: -0.1},
			h4:    TextStyle{Size: 18, LineHeight: 22, Weight: Semibold, Tracking: -0.1},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var scale ArticleType
			found := false
			for _, rung := range articleTypes {
				if rung.Body.Size == tc.body.Size {
					scale, found = rung, true
				}
			}
			if !found {
				t.Fatalf("no rung draws a %v pt body", tc.body.Size)
			}
			// The body keeps the size it shipped with; the three section levels
			// are the roles this change splits, and the headline is hand-set per
			// rung rather than taken from them.
			if scale.Body != tc.body {
				t.Fatalf("body = %+v, want the unchanged %+v", scale.Body, tc.body)
			}
			if scale.Title != tc.title {
				t.Fatalf("title = %+v, want the unchanged %+v", scale.Title, tc.title)
			}
			for _, want := range []struct {
				name  string
				style TextStyle
				level int
			}{
				{"h2", tc.h2, 2},
				{"h3", tc.h3, 3},
				{"h4+", tc.h4, 4},
			} {
				got := scale.HeadingFor(want.level)
				if got != want.style {
					t.Fatalf("%s = %+v, want %+v", want.name, got, want.style)
				}
				if got.Weight != Semibold || got.Family != "" || got.Tracking != -0.1 {
					t.Fatalf("%s: %v/%q/%.2f, want the system face, Semibold, -0.1 tracking",
						want.name, got.Weight, got.Family, got.Tracking)
				}
				if got.LineHeight <= got.Size {
					t.Fatalf("%s: the %v line height clips its %v size", want.name, got.LineHeight, got.Size)
				}
			}
			// Three levels stay strictly ordered, and the page's own headline
			// still sits over them all.
			if !(scale.Title.Size > scale.Heading2.Size && scale.Heading2.Size > scale.Heading3.Size &&
				scale.Heading3.Size > scale.Heading4.Size && scale.Heading4.Size > scale.Body.Size) {
				t.Fatalf("%s: the hierarchy inverts: title %v, h2 %v, h3 %v, h4 %v, body %v", tc.name,
					scale.Title.Size, scale.Heading2.Size, scale.Heading3.Size, scale.Heading4.Size, scale.Body.Size)
			}
			// Long-form prose reads best at 1.5 times its size or looser.
			if scale.Body.LineHeight < scale.Body.Size*1.5 {
				t.Fatalf("%s: %v line height under a comfortable %v", tc.name, scale.Body.LineHeight, scale.Body.Size*1.5)
			}
			if scale.Quote != scale.Body {
				t.Fatalf("%s: quote %v, want the body's own %v", tc.name, scale.Quote, scale.Body)
			}
			if scale.Code.Size >= scale.Body.Size || scale.Code.Family != MonoFont {
				t.Fatalf("%s: code %v (%q), want the mono face under the body", tc.name, scale.Code, scale.Code.Family)
			}
		})
	}

	wantSizes := []float32{13, 15, 17}
	if len(ArticleSizes) != len(wantSizes) {
		t.Fatalf("ArticleSizes = %v, want the three steps %v", ArticleSizes, wantSizes)
	}
	for i, want := range wantSizes {
		if ArticleSizes[i] != want {
			t.Fatalf("ArticleSizes[%d] = %v, want %v", i, ArticleSizes[i], want)
		}
	}
	if DefaultArticleSize != 15 {
		t.Fatalf("DefaultArticleSize = %v, want the 15 pt middle step", DefaultArticleSize)
	}
	if len(articleTypes) != len(wantSizes) {
		t.Fatalf("the ladder has %d rungs for %d sizes", len(articleTypes), len(wantSizes))
	}
	for i, scale := range articleTypes {
		if got := ArticleStep(scale.Body.Size); got != i {
			t.Fatalf("rung %d maps to step %d", i, got)
		}
		if scale.Body.Size != wantSizes[i] {
			t.Fatalf("rung %d: body %v, want %v", i, scale.Body.Size, wantSizes[i])
		}
		if i > 0 {
			prev := articleTypes[i-1]
			if scale.BlockGap <= prev.BlockGap || scale.HeadingGap <= prev.HeadingGap {
				t.Fatalf("rung %d: the gaps do not ladder: %v/%v after %v/%v",
					i, scale.BlockGap, scale.HeadingGap, prev.BlockGap, prev.HeadingGap)
			}
		}
	}
	mid := articleTypes[1]
	if mid.Title != ArticleTitle || mid.Heading2 != ArticleHeading2 || mid.Heading3 != ArticleHeading3 ||
		mid.Heading4 != ArticleHeading4 ||
		mid.Body != ArticleBody || mid.Quote != ArticleQuote || mid.Code != ArticleCode {
		t.Fatalf("the middle rung is not the published Article tokens: %+v", mid)
	}
}

// TestArticleHeadingLevels pins which role each heading level draws with: the
// article's own headline at level 1, the three section levels at 2, 3 and 4,
// and the smallest section level for a deeper 5 or 6 so a body never loses a
// heading's styling.
func TestArticleHeadingLevels(t *testing.T) {
	scale := ArticleTypeForSize(DefaultArticleSize)
	for _, tc := range []struct {
		level int
		want  TextStyle
	}{
		{1, scale.Title},
		{2, scale.Heading2},
		{3, scale.Heading3},
		{4, scale.Heading4},
		{5, scale.Heading4},
		{6, scale.Heading4},
	} {
		if got := scale.HeadingFor(tc.level); got != tc.want {
			t.Errorf("level %d draws with %+v, want %+v", tc.level, got, tc.want)
		}
	}
	// A scale carries the whole page, so no role is left at zero.
	for _, scale := range articleTypes {
		if scale.Title.Size == 0 || scale.Heading2.Size == 0 || scale.Heading3.Size == 0 ||
			scale.Heading4.Size == 0 || scale.Body.Size == 0 || scale.Quote.Size == 0 ||
			scale.Code.Size == 0 || scale.BlockGap == 0 || scale.HeadingGap == 0 {
			t.Fatalf("a rung leaves a role unset: %+v", scale)
		}
	}
}

// TestArticleTypeForSize pins which step a stored size reads as: any offered
// size keeps its own rung, a size between two takes the nearer one and a tie
// the larger, and a size that names nothing, as a first launch stores, takes
// the middle.
func TestArticleTypeForSize(t *testing.T) {
	for _, tc := range []struct {
		name  string
		size  float32
		index int
		want  float32
	}{
		{"first launch", 0, 1, 15},
		{"smallest", 13, 0, 13},
		{"just under a step", 12, 0, 13},
		{"tie rounds up", 14, 1, 15},
		{"middle", 15, 1, 15},
		{"tie above the middle", 16, 2, 17},
		{"largest", 17, 2, 17},
		{"past the top", 21, 2, 17},
		{"below the floor", 4, 0, 13},
		{"a negative size", -3, 1, 15},
	} {
		scale := ArticleTypeForSize(tc.size)
		if scale.Body.Size != tc.want || ArticleStep(scale.Body.Size) != tc.index {
			t.Errorf("%s: ArticleTypeForSize(%v) = step %d at %v, want step %d at %v",
				tc.name, tc.size, ArticleStep(scale.Body.Size), scale.Body.Size, tc.index, tc.want)
		}
		if got := NormalizeArticleSize(tc.size); got != tc.want {
			t.Errorf("%s: NormalizeArticleSize(%v) = %v, want %v", tc.name, tc.size, got, tc.want)
		}
	}
}
