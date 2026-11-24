package ui

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

// The channel page's history control. A refresh reads only the last 30 days of
// a channel's uploads, so a channel that posts rarely arrives with few videos or
// none at all. This control reads one page further back on each click, and the
// store keeps what it brought.
//
// The rules it keeps:
//   - It is one quiet outlined button centred under the grid's last card, in the
//     same 28 pt button the empty states use. It is never a primary fill while
//     the channel still has videos above it.
//   - The label names the page: 50 videos read for two quota units, one
//     playlistItems.list call and one videos.list call.
//   - While a page is in flight the button keeps its place, says so, and cannot
//     be pressed again.
//   - Once the channel's earliest video is loaded the button becomes the muted
//     line "All earlier videos loaded", so the end of a channel is named rather
//     than a button that does nothing.
//   - A failure stays readable above the button until the next attempt.

// ChannelHistoryWords is every word the channel page's history control draws, in
// one language: the button, its loading and finished faces, what a failed page
// reports, and the empty channel pane it enables.
type ChannelHistoryWords struct {
	// Load is the button's label, formatted with the page size.
	Load string
	// Hint is the button's tooltip: the page size and its quota cost.
	Hint string
	// Loading is the disabled face of a page in flight.
	Loading string
	// Loaded is the muted line that says the earliest video is loaded.
	Loaded string
	// Action is the empty channel pane's primary action.
	Action string
	// Title and Body are the empty channel pane's headline and sentence before
	// the channel's history is read; BodyDone replaces Body once it is.
	Title    string
	Body     string
	BodyDone string
	// Failed heads a failed page; Quota and NoUploads name the two failures the
	// app can explain, and EmptyPage reports a page that held no long video.
	Failed    string
	Quota     string
	NoUploads string
	EmptyPage string
}

// ChannelHistoryCopy returns the channel history control's words in the
// reader's language, English by default and 简体中文 when the system asks for it,
// as the rest of the app does.
func ChannelHistoryCopy(locale string) ChannelHistoryWords {
	if strings.HasPrefix(strings.ToLower(locale), "zh") {
		return ChannelHistoryWords{
			Load:      "载入更早视频 (%d 条)",
			Hint:      "载入 %d 条更早的视频，消耗 2 个配额单位",
			Loading:   "正在抓取历史视频…",
			Loaded:    "已载入全部历史视频",
			Action:    "立即抓取历史视频",
			Title:     "该频道近期无更新",
			Body:      "这个频道最近没有上传。可以载入它的历史视频，查看更早的内容。",
			BodyDone:  "该频道没有更早的视频了。刷新可查看新的上传。",
			Failed:    "无法载入历史视频",
			Quota:     "YouTube API 配额已用尽，暂停结束后再试。",
			NoUploads: "该频道没有可读取的上传。",
			EmptyPage: "这一页没有长视频，可以继续载入更早的内容。",
		}
	}
	return ChannelHistoryWords{
		Load:      "Load %d earlier videos",
		Hint:      "Reads %d earlier uploads for 2 quota units",
		Loading:   "Loading earlier videos…",
		Loaded:    "All earlier videos loaded",
		Action:    "Load history videos",
		Title:     "No recent videos from this channel",
		Body:      "This channel has not uploaded lately. Load its history to see earlier videos.",
		BodyDone:  "This channel has no earlier videos. Refresh to check for new uploads.",
		Failed:    "Could not load earlier videos",
		Quota:     "YouTube API quota reached. Try again after the pause ends.",
		NoUploads: "This channel has no uploads to read.",
		EmptyPage: "That page held no long videos. Load again for the ones before it.",
	}
}

// ChannelEmptyCopy is the empty channel pane's words: a channel whose uploads
// the refresh window holds none of, which is the channel this control exists
// for. Its action reads the channel's history instead of refreshing again, so an
// inactive channel is not a dead end. Once the earliest video is loaded the pane
// keeps its headline, says there is nothing earlier, and offers no action.
func ChannelEmptyCopy(locale string, complete bool) EmptyStateCopy {
	words := ChannelHistoryCopy(locale)
	copied := EmptyStateCopy{Symbol: "play.rectangle", Title: words.Title, Sentence: words.Body, Action: words.Action}
	if complete {
		copied.Sentence = words.BodyDone
		copied.Action = ""
	}
	return copied
}

// BackfillStage is what the channel page's history control offers.
type BackfillStage uint8

const (
	// BackfillAvailable offers the channel's next page of earlier uploads.
	BackfillAvailable BackfillStage = iota
	// BackfillLoading is the page the reader asked for, in flight.
	BackfillLoading
	// BackfillExhausted names the channel's earliest video: nothing is earlier.
	BackfillExhausted
)

// BackfillStageFor decides the control from the channel's saved position and
// whether its page is in flight.
func BackfillStageFor(complete, loading bool) BackfillStage {
	switch {
	case loading:
		return BackfillLoading
	case complete:
		return BackfillExhausted
	default:
		return BackfillAvailable
	}
}

// BackfillSpec is one channel page's history control.
type BackfillSpec struct {
	// Key names the control; it is stable per channel page.
	Key   string
	Stage BackfillStage
	// PageSize is how many uploads one click reads.
	PageSize int
	Locale   string
	// Width is the grid's content width, so a failure wraps inside the cards.
	Width float32
	// Note is what the last page failed with, redacted for display; empty when
	// it brought nothing to report.
	Note string
	// Disabled and Tooltip hold the control back when no page can be read now:
	// offline, no API key, or a quota pause.
	Disabled bool
	Tooltip  string
	OnLoad   func()
}

// BackfillControl draws one channel page's history control: the quiet outlined
// button under the grid's last card, its disabled loading face, or the muted
// line that says the earliest video is loaded. A failure stays visible above it.
func BackfillControl(c *ui.Context, tokens Tokens, spec BackfillSpec) ui.Element {
	words := ChannelHistoryCopy(spec.Locale)
	width := spec.Width
	if width <= 0 {
		width = Space(80)
	}
	column := ui.Column(c).Width(width).AlignItems(ui.Center).Gap(Space(2)).Shrink(0)
	column.Children(func() {
		if spec.Note != "" {
			note := ui.Row(c).Width(width).AlignItems(ui.Center).Justify(ui.Center).Gap(Space(2))
			note.Children(func() {
				ui.Icon(c, IconAlert).Size(Space(3.5), Space(3.5)).TextColor(tokens.Danger)
				Helper.Apply(ui.Text(c, spec.Note)).TextColor(tokens.Danger).MaxLines(2).Wrap()
			})
		}
		switch spec.Stage {
		case BackfillLoading:
			PaneAction(c, tokens, spec.Key, words.Loading, false, true, words.Loading, nil)
		case BackfillExhausted:
			// The end of a channel is a rest, not a control: muted text in the
			// space the button leaves, so the position never shifts.
			ui.Row(c).Height(Space(7)).AlignItems(ui.Center).Children(func() {
				Secondary.Apply(ui.Text(c, words.Loaded)).TextColor(tokens.Faint).SingleLine()
			})
		default:
			hint := fmt.Sprintf(words.Hint, spec.PageSize)
			if spec.Disabled && spec.Tooltip != "" {
				hint = spec.Tooltip
			}
			PaneAction(c, tokens, spec.Key, fmt.Sprintf(words.Load, spec.PageSize), false, spec.Disabled, hint, spec.OnLoad)
		}
	})
	return column
}
