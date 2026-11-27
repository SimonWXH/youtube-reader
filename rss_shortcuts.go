package main

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
)

// The RSS workspace's keyboard reading flow: J and K step the article list, M
// toggles the open article's read state, S toggles its star, Cmd+O opens its
// original page in the default browser, and Shift+Cmd+U marks the whole
// destination read. The keys are declared as window shortcuts while the RSS
// page builds, so no other workspace hears them, and the single-key ones stand
// aside while a field takes typing, so J in the search field is the letter and
// not the next article.
func (w *Window) installRSSShortcuts(c *ui.Context) {
	c.OnWindowShortcut(0, ui.KeyJ, func() { w.rssStepSelection(c, 1) })
	c.OnWindowShortcut(0, ui.KeyK, func() { w.rssStepSelection(c, -1) })
	c.OnWindowShortcut(0, ui.KeyM, func() { w.rssToggleOpenArticleRead(c) })
	c.OnWindowShortcut(0, ui.KeyS, func() { w.rssToggleOpenArticleStar(c) })
	c.OnWindowShortcut(ui.Cmd, ui.KeyO, func() { w.rssOpenArticleInBrowser(c) })
	c.OnWindowShortcut(ui.Cmd|ui.Shift, ui.KeyU, func() { w.rssMarkDestinationRead(c) })
}

// rssSingleKeyReady reports whether a single-key shortcut may act: the RSS page
// is the one showing and no field is taking text. A chord with Cmd needs no
// such guard, because typing never produces it.
func (w *Window) rssSingleKeyReady(c *ui.Context) bool {
	return w.app != nil && w.activeWorkspaceName() == WorkspaceRSS && !c.FocusTakesText()
}

// rssStepSelection moves the selection one entry down (+1) or up (-1) the
// article column as it is filtered now, reusing the arrow keys' move so the
// list scrolls the chosen row into view and the reader loads it.
func (w *Window) rssStepSelection(c *ui.Context, delta int) {
	if !w.rssSingleKeyReady(c) {
		return
	}
	articles := w.app.VisibleRSSArticles()
	if len(articles) == 0 {
		return
	}
	w.moveRSSSelection(articles, delta)
	c.Invalidate()
}

// rssToggleOpenArticleRead flips the open article between read and unread. The
// column header and the sidebar counts follow through the app's own count
// reload.
func (w *Window) rssToggleOpenArticleRead(c *ui.Context) {
	if !w.rssSingleKeyReady(c) || c.ShortcutRepeated() {
		return
	}
	art := w.app.SelectedRSSArticle()
	if art == nil {
		return
	}
	w.toggleRSSArticleRead(c, *art)
}

// rssToggleOpenArticleStar stars or unstars the open article.
func (w *Window) rssToggleOpenArticleStar(c *ui.Context) {
	if !w.rssSingleKeyReady(c) || c.ShortcutRepeated() {
		return
	}
	art := w.app.SelectedRSSArticle()
	if art == nil {
		return
	}
	w.toggleRSSArticleStar(c, *art)
}

// rssMarkDestinationRead marks every article the selected destination covers
// read: a feed's own articles, a folder's feeds' articles, or - at a smart feed
// such as Today, All Unread or Starred, whose filter is a view over the library
// - exactly the articles the column is showing.
func (w *Window) rssMarkDestinationRead(c *ui.Context) {
	if w.app == nil || w.activeWorkspaceName() != WorkspaceRSS || c.ShortcutRepeated() {
		return
	}
	w.markRSSScopeRead(c, w.app.RSSBulkReadScope(), true)
}

// rssOpenArticleInBrowser opens the open article's original page in the
// default browser.
func (w *Window) rssOpenArticleInBrowser(c *ui.Context) {
	if w.app == nil || w.activeWorkspaceName() != WorkspaceRSS || c.ShortcutRepeated() {
		return
	}
	art := w.app.SelectedRSSArticle()
	if art == nil || strings.TrimSpace(art.Link) == "" {
		return
	}
	c.OpenURL(art.Link)
}

// toggleRSSArticleRead writes an article's new read state and repaints the
// page, so the header count and the row's unread dot move together.
func (w *Window) toggleRSSArticleRead(c *ui.Context, art core.RSSArticle) {
	if w.app == nil {
		return
	}
	if err := w.app.MarkRSSArticleRead(art.ID, !art.Read); err != nil {
		w.toast(c, "Couldn't update the read state", true)
		return
	}
	if c != nil {
		c.Invalidate()
	}
}

// toggleRSSArticleStar writes an article's new star state and repaints the
// page, so the header's star, the row's mark and the Starred count agree.
func (w *Window) toggleRSSArticleStar(c *ui.Context, art core.RSSArticle) {
	if w.app == nil {
		return
	}
	if err := w.app.SetRSSArticleStarred(art.ID, !art.Starred); err != nil {
		w.toast(c, "Couldn't update the star", true)
		return
	}
	if c != nil {
		c.Invalidate()
	}
}
