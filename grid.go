package main

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
	mui "github.com/simon/youtube-reader/ui"
)

func (w *Window) channelLink(c *ui.Context, t mui.Tokens, v core.Video) {
	link := ui.Text(c, v.ChannelTitle).FontSize(13).TextColor(t.Muted).SingleLine().Grow(1)
	if v.ChannelID == "" {
		return
	}
	open := func() { c.OpenURL(channelURL(v.ChannelID)) }
	link.Cursor(ui.CursorPointer).Tooltip("Open "+v.ChannelTitle+" on YouTube").Role(ui.RoleLink).Focusable().OnClick(open).OnShortcut(0, ui.KeyEnter, open)
}

func channelURL(channelID string) string {
	return "https://www.youtube.com/channel/" + channelID
}

func (w *Window) gridArea(c *ui.Context, t mui.Tokens) {
	videos := w.app.VisibleVideos()
	ui.Column(c).Grow(1).Padding(0, 12, 16, 12).Children(func() {
		if len(videos) == 0 {
			if w.app.SelectedVideoID != "" {
				w.app.SelectedVideoID = ""
			}
			none := -1
			w.selectedIndex = &none
			w.emptyState(c, t, "Nothing here yet", "Try another filter or refresh your subscriptions.", "empty-refresh", "Refresh now", func(c *ui.Context) { w.runRefresh(c, true) })
			return
		}
		target := -1
		if w.app.SelectedVideoID != "" {
			for i := range videos {
				if videos[i].ID == w.app.SelectedVideoID {
					target = i
					break
				}
			}
			if target < 0 {
				w.app.SelectedVideoID = ""
			}
		}
		w.selectedIndex = &target
		w.grid.Selected = w.selectedIndex
		minWidth := w.gridMinWidth(c, w.app.settingsSnapshot().MaxGridColumns)
		grid := ui.GridView(c, &w.grid, len(videos), minWidth, 302, func(i int) {
			v := videos[i]
			selected := w.app.SelectedVideoID == v.ID
			card := ui.Column(c).Height(302).Gap(5).Padding(12).Radius(10).Background(t.Surface).Border(1, t.BorderSoft).Transition(mui.Fade).Clip()
			if selected {
				card.Border(1, t.Accent)
			} else if card.Hovered() {
				card.Background(t.SurfaceAlt)
			}
			card.Children(func() {
				ui.Row(c).Height(160).FillWidth().Radius(8).Clip().Children(func() {
					mui.Thumbnail(c, t, v.ThumbnailURL, 0, 0)
				})
				ui.Text(c, v.Title).Bold().FontSize(14).FixedLineHeight(20).Height(40).MaxLines(2).Wrap().Label(v.Title)
				if desc := strings.TrimSpace(v.Description); desc != "" {
					ui.Text(c, desc).FontSize(13).TextColor(t.Muted).FixedLineHeight(18).Height(36).MaxLines(2)
				}
				ui.Row(c).Height(18).Gap(8).AlignItems(ui.Center).Children(func() {
					if !w.app.Sync.IsRead(v.ID) {
						ui.Row(c).Size(6, 6).Radius(3).Background(t.Accent).Label("Unread")
					}
					mui.ChannelAvatar(c, t, v.AvatarURL, v.ChannelTitle, 18)
					w.channelLink(c, t, v)
					if v.IsShort {
						ui.Text(c, "Shorts").FontSize(13).TextColor(t.Muted).SingleLine()
					}
					ui.Text(c, core.RelativeDate(v.PublishedAt, time.Now())).FontSize(13).TextColor(t.Muted).SingleLine()
				})
			})
		}).Grow(1)
		if grid.Changed() {
			if w.grid.Selected != nil && *w.grid.Selected < len(videos) {
				w.app.SelectedVideoID = videos[*w.grid.Selected].ID
			}
		}
	})
}

func (w *Window) gridMinWidth(c *ui.Context, maxColumns int) float32 {
	available := w.gridAvailableWidth
	if available <= 0 {
		available = w.contentWidth
	}
	return gridMinWidthForAvailable(available, c.Theme().Space(2), maxColumns)
}

// gridMinWidthForAvailable uses the width the grid will actually occupy on
// this build, not the split's stale previous-frame width. That keeps the
// first paint and later divider drags within the configured column cap.
func gridMinWidthForAvailable(available, gap float32, maxColumns int) float32 {
	maxColumns = clampInt(maxColumns, minMaxGridColumns, maxMaxGridColumns)
	available -= 24
	return max(float32(200), available/float32(maxColumns)-gap)
}

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
