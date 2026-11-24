package ui

import "testing"

// TestArticleTypeLadder pins the reading view's three steps: the body sizes the
// control offers, a line height that keeps long-form prose comfortable, a
// hierarchy that never inverts, and a vertical rhythm that grows with the
// letter. The middle step is the published Article tokens so the reader's
// default page and the type scale cannot drift apart.
func TestArticleTypeLadder(t *testing.T) {
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
		if got := ArticleStep(scale.Size); got != i {
			t.Fatalf("rung %d maps to step %d", i, got)
		}
		if scale.Size != wantSizes[i] || scale.Body.Size != wantSizes[i] {
			t.Fatalf("rung %d: size %v, body %v, want %v", i, scale.Size, scale.Body.Size, wantSizes[i])
		}
		if scale.Body.Weight != Regular || scale.Body.Family != "" {
			t.Fatalf("rung %d: body %v/%q, want the system face at Regular", i, scale.Body.Weight, scale.Body.Family)
		}
		// Long-form prose reads best at 1.5 times its size or looser.
		if scale.Body.LineHeight < scale.Body.Size*1.5 {
			t.Fatalf("rung %d: %v line height under a comfortable %v", i, scale.Body.LineHeight, scale.Body.Size*1.5)
		}
		if scale.Title.Size <= scale.Heading.Size || scale.Heading.Size <= scale.Body.Size {
			t.Fatalf("rung %d: the hierarchy inverts: title %v, heading %v, body %v",
				i, scale.Title.Size, scale.Heading.Size, scale.Body.Size)
		}
		if scale.Title.LineHeight <= scale.Title.Size || scale.Heading.LineHeight <= scale.Heading.Size {
			t.Fatalf("rung %d: a heading's line clips its own size", i)
		}
		if scale.Quote != scale.Body {
			t.Fatalf("rung %d: quote %v, want the body's own %v", i, scale.Quote, scale.Body)
		}
		if scale.Code.Size >= scale.Body.Size || scale.Code.Family != MonoFont {
			t.Fatalf("rung %d: code %v (%q), want the mono face under the body", i, scale.Code, scale.Code.Family)
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
	if mid.Title != ArticleTitle || mid.Heading != ArticleHeading ||
		mid.Body != ArticleBody || mid.Quote != ArticleQuote || mid.Code != ArticleCode {
		t.Fatalf("the middle rung is not the published Article tokens: %+v", mid)
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
		if scale.Size != tc.want || ArticleStep(scale.Size) != tc.index {
			t.Errorf("%s: ArticleTypeForSize(%v) = step %d at %v, want step %d at %v",
				tc.name, tc.size, ArticleStep(scale.Size), scale.Size, tc.index, tc.want)
		}
		if got := NormalizeArticleSize(tc.size); got != tc.want {
			t.Errorf("%s: NormalizeArticleSize(%v) = %v, want %v", tc.name, tc.size, got, tc.want)
		}
	}
	// A scale carries the whole page, so no role is left at zero.
	for _, scale := range articleTypes {
		if scale.Title.Size == 0 || scale.Heading.Size == 0 || scale.Body.Size == 0 ||
			scale.Quote.Size == 0 || scale.Code.Size == 0 || scale.BlockGap == 0 || scale.HeadingGap == 0 {
			t.Fatalf("a rung leaves a role unset: %+v", scale)
		}
	}
}
