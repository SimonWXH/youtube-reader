package ui

import (
	"fmt"
	"strings"
	"testing"
	"unicode"
)

// TestChannelHistoryCopyIsComplete walks both languages: every word the control
// draws is there, so a half-translated control cannot ship.
func TestChannelHistoryCopyIsComplete(t *testing.T) {
	for _, locale := range []string{"en", "zh-Hans"} {
		words := ChannelHistoryCopy(locale)
		fields := map[string]string{
			"Load": words.Load, "Hint": words.Hint, "Loading": words.Loading, "Loaded": words.Loaded,
			"Action": words.Action, "Title": words.Title, "Body": words.Body, "BodyDone": words.BodyDone,
			"Failed": words.Failed, "Quota": words.Quota, "NoUploads": words.NoUploads, "EmptyPage": words.EmptyPage,
		}
		for name, value := range fields {
			if strings.TrimSpace(value) == "" {
				t.Errorf("%s: %s is empty", locale, name)
			}
		}
		if !strings.Contains(words.Load, "%d") || !strings.Contains(words.Hint, "%d") {
			t.Errorf("%s: the label and its hint must name the page size", locale)
		}
		if !strings.Contains(words.Hint, "2") {
			t.Errorf("%s: the hint must name the page's two quota units: %q", locale, words.Hint)
		}
	}
	if ChannelHistoryCopy("zh-Hans").Load == ChannelHistoryCopy("en").Load {
		t.Fatal("the control speaks one language in both locales")
	}
	if got, want := fmt.Sprintf(ChannelHistoryCopy("zh-Hans").Load, 50), "载入更早视频 (50 条)"; got != want {
		t.Fatalf("the Chinese label = %q; want %q", got, want)
	}
	if got, want := fmt.Sprintf(ChannelHistoryCopy("en").Load, 50), "Load 50 earlier videos"; got != want {
		t.Fatalf("the English label = %q; want %q", got, want)
	}
}

// TestChannelHistoryCopyFollowsTheCopyRules holds the control's words to the same
// rules the shared state copy keeps: no em dash, no exclamation.
func TestChannelHistoryCopyFollowsTheCopyRules(t *testing.T) {
	for _, locale := range []string{"en", "zh-Hans"} {
		words := ChannelHistoryCopy(locale)
		for _, text := range []string{
			words.Load, words.Hint, words.Loading, words.Loaded, words.Action, words.Title,
			words.Body, words.BodyDone, words.Failed, words.Quota, words.NoUploads, words.EmptyPage,
		} {
			if strings.ContainsRune(text, '\u2014') || strings.ContainsRune(text, '!') || strings.ContainsRune(text, '！') {
				t.Errorf("%s: %q breaks the copy rules", locale, text)
			}
			if text != strings.TrimSpace(text) {
				t.Errorf("%s: %q carries padding", locale, text)
			}
			for _, r := range text {
				if r > unicode.MaxASCII && !unicode.Is(unicode.Han, r) && !unicode.IsPunct(r) {
					t.Errorf("%s: %q holds an unexpected character %q", locale, text, r)
				}
			}
		}
	}
}

// TestChannelEmptyCopyNamesTheEnd covers the empty channel pane in both of its
// positions: it offers the history before the first page, and names the end of
// the channel after it instead of a button that cannot do anything.
func TestChannelEmptyCopyNamesTheEnd(t *testing.T) {
	for _, locale := range []string{"en", "zh-Hans"} {
		words := ChannelHistoryCopy(locale)
		before := ChannelEmptyCopy(locale, false)
		if before.Action != words.Action || before.Sentence != words.Body || before.Title != words.Title {
			t.Fatalf("%s: the empty pane before the first page = %+v", locale, before)
		}
		if before.Symbol == "" {
			t.Fatalf("%s: the empty pane carries no symbol", locale)
		}
		after := ChannelEmptyCopy(locale, true)
		if after.Action != "" {
			t.Fatalf("%s: a channel with nothing earlier still offers %q", locale, after.Action)
		}
		if after.Sentence != words.BodyDone || after.Title != words.Title {
			t.Fatalf("%s: the empty pane at the end = %+v", locale, after)
		}
	}
}

// TestBackfillStageFor covers the control's three states: a page to read, the
// page in flight, and the channel's earliest video loaded.
func TestBackfillStageFor(t *testing.T) {
	cases := []struct {
		name     string
		complete bool
		loading  bool
		want     BackfillStage
	}{
		{"a page is available", false, false, BackfillAvailable},
		{"a page is in flight", false, true, BackfillLoading},
		{"the channel's earliest video is loaded", true, false, BackfillExhausted},
		// A page in flight wins: the control names the work, not the end it may
		// reach when it returns.
		{"in flight on the last page", true, true, BackfillLoading},
	}
	for _, c := range cases {
		if got := BackfillStageFor(c.complete, c.loading); got != c.want {
			t.Errorf("%s: stage = %v; want %v", c.name, got, c.want)
		}
	}
}
