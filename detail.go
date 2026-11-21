package main

import (
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
	mui "github.com/simon/youtube-reader/ui"
)

func (w *Window) view(c *ui.Context) {
	t := c.Theme()
	tokens := mui.NewTokens(t, t.Dark)
	settings := w.app.settingsSnapshot()

	total := windowWidth(c)
	selected := w.app.HasSelection()
	w.sidebarWidth, w.detailsWidth = fitPaneWidths(
		settings.SidebarWidth, settings.DetailsWidth,
		selected, total,
	)
	// Zone A hook: the toolbar's sidebar toggle folds this pane away, so it and
	// its divider take no room; w.sidebarWidth keeps what it shows with again.
	layoutSidebar := w.sidebarWidth
	outerDivider := splitDividerWidth()
	if w.sidebarHidden {
		layoutSidebar = 0
		outerDivider = 0
	}
	innerDivider := splitDividerWidth()
	w.contentWidth = total - layoutSidebar - outerDivider
	if selected {
		w.contentWidth -= w.detailsWidth + innerDivider
	}
	// MyGo's Split clamps only to its own 40 DIP minimum while a drag is
	// active. Keep the width it reports within the same bounds used on the
	// next build, so a drag cannot save a value that immediately snaps back.
	w.sidebarWidth = boundedSidebarWidth(w.sidebarWidth, selected, total)
	w.contentWidth = max(minGridWidth, w.contentWidth)
	if selected {
		w.contentWidth = min(w.contentWidth, total-layoutSidebar-outerDivider-innerDivider-minDetailsWidth)
	}
	var contentSplit, outerSplit ui.Element
	panes := func() {
		if !selected {
			w.gridAvailableWidth = w.contentWidth
			w.content(c, tokens)
			return
		}
		contentSplit = ui.Split(c, &w.contentWidth, func() {
			w.gridAvailableWidth = w.contentWidth - splitDividerWidth()
			w.content(c, tokens)
		}, func() {
			w.drawer(c, tokens)
		}).Fill()
	}
	// Zone A hook: a folded sidebar has no split to drag.
	if w.sidebarHidden {
		panes()
	} else {
		outerSplit = ui.Split(c, &w.sidebarWidth, func() {
			w.sidebar(c, tokens)
		}, panes).Fill()
	}
	outerChanged := outerSplit.Changed()
	contentChanged := contentSplit.Changed()
	if outerChanged || contentChanged {
		_, _ = w.app.updateSettings(func(s *core.Settings) bool {
			if outerChanged {
				s.SidebarWidth = boundedSidebarWidth(w.sidebarWidth, selected, total)
			}
			if details, hasDetails := persistedDetailsWidth(total, layoutSidebar, w.contentWidth, outerDivider, selected, contentChanged); hasDetails {
				s.DetailsWidth = details
			}
			return true
		})
	}
}

// persistedDetailsWidth recovers the inner split's new size only when that
// split exists and reported a change. Without this guard, an outer split
// drag computes a nonsensical details width from the no-details content pane.
func persistedDetailsWidth(total, layoutSidebar, content, outerDivider float32, selected, contentChanged bool) (float32, bool) {
	if !selected || !contentChanged {
		return 0, false
	}
	details := total - layoutSidebar - content - outerDivider - splitDividerWidth()
	if details < minDetailsWidth {
		return 0, false
	}
	return clamp(details, minDetailsWidth, maxDetailsWidth), true
}

func fitPaneWidths(sidebar, details float32, selected bool, total float32) (float32, float32) {
	if sidebar <= 0 {
		sidebar = defaultSidebarWidth
	}
	if details <= 0 {
		details = defaultDetailsWidth
	}
	sidebar = clamp(sidebar, minSidebarWidth, maxSidebarWidth)
	details = clamp(details, minDetailsWidth, maxDetailsWidth)
	if !selected {
		if total <= 0 {
			return sidebar, details
		}
		return clamp(sidebar, minSidebarWidth, min(maxSidebarWidth, total-2*splitDividerWidth()-minGridWidth)), details
	}
	if total <= 0 {
		return sidebar, details
	}
	divider := splitDividerWidth()
	maxTotal := total - 2*divider
	if maxTotal < minSidebarWidth+minGridWidth+minDetailsWidth {
		return minSidebarWidth, minDetailsWidth
	}
	if sidebar+details+minGridWidth > maxTotal {
		excess := sidebar + details + minGridWidth - maxTotal
		room := sidebar + details - minSidebarWidth - minDetailsWidth
		sidebarShare := float32(0.5)
		if room > 0 {
			sidebarShare = (sidebar - minSidebarWidth) / room
		}
		sidebar -= excess * sidebarShare
		details -= excess * (1 - sidebarShare)
	}
	return sidebar, details
}

// boundedSidebarWidth matches the Split's drag value to the bounds that the
// next build's fitPaneWidths would enforce, so a drag cannot persist a width
// that immediately snaps back.
func boundedSidebarWidth(width float32, selected bool, total float32) float32 {
	max := maxSidebarWidth
	if total > 0 {
		maxGridShare := total - 2*splitDividerWidth() - minGridWidth
		if selected {
			maxGridShare -= minDetailsWidth
		}
		max = min(max, maxGridShare)
	}
	return clamp(width, minSidebarWidth, max)
}

func splitDividerWidth() float32 {
	return 1 // The divider line itself; the drag handle overlays both panes.
}

func windowWidth(c *ui.Context) float32 {
	w, _ := c.Size()
	return w
}

func clamp(v, min, max float32) float32 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func (w *Window) drawer(c *ui.Context, t mui.Tokens) {
	drawer := ui.Column(c).Fill().Padding(24, 20).Gap(16).Background(t.App).
		Role(ui.RoleGroup).Label("Video details")
	drawer.Draw(func(p *ui.Painter, r ui.Rect) {
		p.Fill(ui.Rect{X: r.X, Y: r.Y, W: 1, H: r.H}, t.Border, 0)
	})
	drawer.Children(func() {
		if w.app.SelectedVideoID == "" {
			ui.Column(c).Grow(1).Center().AlignItems(ui.Center).Gap(12).Children(func() {
				ui.Icon(c, mui.IconInfo).Size(28, 28)
				ui.Text(c, "Select a video to see details.").FontSize(14).TextColor(t.Muted).TextAlign(ui.Center)
			})
			return
		}
		v, ok := w.app.VideoByID(w.app.SelectedVideoID)
		if !ok {
			ui.Text(c, "Video not found.").FontSize(14).TextColor(t.Muted)
			return
		}
		ui.Text(c, v.Title).Bold().FontSize(20).FixedLineHeight(26).MaxLines(3).Wrap()
		ui.Text(c, v.ChannelTitle+"  -  "+core.RelativeDate(v.PublishedAt, time.Now())).FontSize(13).TextColor(t.Muted)
		if v.ThumbnailURL != "" {
			ui.Row(c).Height(158).FillWidth().Radius(10).Clip().Children(func() {
				mui.Thumbnail(c, t, v.ThumbnailURL, 0, 0)
			})
		}
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Text(c, v.Description).FontSize(14).FixedLineHeight(21).TextColor(t.Text)
		})
		ui.Column(c).Gap(8).Children(func() {
			mui.StatefulButton(c, t, "open-youtube", "Open in YouTube", mui.IconExternal, true, mui.Default, func() {
				w.app.MarkRead(v)
				c.OpenURL("https://www.youtube.com/watch?v=" + v.ID)
			})
			label, key := "Favorite", "favorite"
			if w.app.Sync.IsFavorite(v.ID) {
				label, key = "Unfavorite", "unfavorite"
			}
			mui.StatefulButton(c, t, key, label, mui.IconStar, false, mui.Default, func() {
				w.app.ToggleFavorite(v)
				if w.app.Sync.IsFavorite(v.ID) {
					w.toast(c, "Added to favorites", false)
				} else {
					w.toast(c, "Removed from favorites", false)
				}
			})
			if w.app.Sync.IsRead(v.ID) {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, mui.IconInfo).Size(16, 16)
					ui.Text(c, "Read").FontSize(13).TextColor(t.Success)
				})
			}
		})
	})
}
