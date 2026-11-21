package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	mui "github.com/simon/youtube-reader/ui"
)

// The window chrome takes its geometry from the agreed design (board A1/A1.1):
// a 52 pt toolbar drawn over the hidden-inset title bar and a 44 pt list
// filter strip under it. Toolbar controls are 28 pt with 16 pt glyphs and no
// bezel; their spacing comes from the shared 4 pt ladder in ui/theme.go.
const (
	toolbarHeight      float32 = 52
	toolbarControl     float32 = 28
	toolbarGlyph       float32 = 16
	contentStripHeight float32 = 44
	channelMenuWidth   float32 = 160
	channelMenuIcon    float32 = 36
	searchFieldWidth   float32 = 200
	searchFieldMin     float32 = 124
)

// The toolbar glyphs the shared ui/icons.go does not have yet: the sidebar
// toggle, the overflow menu, and the collapsed Unwatched check box. They
// follow the same 24 pt box, 1.5 pt stroke, round joins as the rest of the
// set.
var (
	toolbarIconSidebar     = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="16" rx="3"/><path d="M9 4v16"/></svg>`))
	toolbarIconMore        = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><circle cx="5" cy="12" r="1.6" fill="currentColor"/><circle cx="12" cy="12" r="1.6" fill="currentColor"/><circle cx="19" cy="12" r="1.6" fill="currentColor"/></svg>`))
	toolbarIconUnwatched   = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><rect x="4" y="4" width="16" height="16" rx="3"/></svg>`))
	toolbarIconUnwatchedOn = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><rect x="4" y="4" width="16" height="16" rx="3"/><path d="m8 12 3 3 5-6"/></svg>`))
)

// shell is the window root: the toolbar spans the window, as the title bar it
// replaces does, so the traffic lights sit inside it; the panes sit below.
// Keeping the toolbar here leaves the split layout in detail.go as it is,
// apart from the sidebar-hidden flag it reads.
func (w *Window) shell(c *ui.Context) {
	t := c.Theme()
	tokens := mui.NewTokens(t, t.Dark)
	w.frame = c
	for _, toast := range w.pendingToasts {
		c.AddToast(toast)
	}
	w.pendingToasts = nil
	ui.Column(c).Fill().Background(tokens.App).Children(func() {
		w.toolbar(c, tokens)
		w.view(c)
	})
}

// toolbar draws the 52 pt window toolbar: the sidebar toggle on the leading
// side, the window title centred on the window, and on the trailing side only
// the refresh control and the overflow menu. Every control is borderless so
// the bar reads as part of the window frame.
func (w *Window) toolbar(c *ui.Context, t mui.Tokens) {
	// The traffic lights keep their own room, so the leading control starts
	// clear of them whatever the title bar style reports. Full screen has no
	// controls to clear, so the bar keeps one panel inset instead.
	leading := max(c.TitleBar().Left+mui.Space(2), mui.Space(4))
	bar := ui.Column(c).Height(toolbarHeight).Shrink(0).FillWidth().Background(t.App).DragWindow()
	bar.Draw(func(p *ui.Painter, r ui.Rect) {
		p.Fill(ui.Rect{X: r.X, Y: r.Y + r.H - mui.Hairline, W: r.W, H: mui.Hairline}, t.BorderSoft, 0)
	})
	bar.Children(func() {
		// The controls are one stop of Tab, as a toolbar's are natively; the
		// arrows move between them.
		ui.Row(c).Fill().Padding(0, mui.Space(4), 0, leading).AlignItems(ui.Center).
			Role(ui.RoleToolbar).Label("Toolbar").FocusGroup(ui.Horizontal).Children(func() {
			w.sidebarToggle(c, t)
			ui.Spacer(c)
			ui.Row(c).Gap(mui.Space(2)).AlignItems(ui.Center).Children(func() {
				w.refreshControl(c, t)
				w.overflowControl(c, t)
			})
		})
		// The title centres on the window, not on what the traffic lights and
		// the trailing group leave over.
		ui.Row(c).Absolute().Left(0).Right(0).Top(0).Height(toolbarHeight).
			Justify(ui.Center).AlignItems(ui.Center).
			HitTest(func(x, y float32, r ui.Rect) bool { return false }).Children(func() {
			title := ui.Text(c, w.title()).SingleLine()
			if room := windowWidth(c) - 2*(leading+searchFieldWidth); room > 0 {
				title = title.MaxWidth(room)
			}
			mui.ToolbarTitle.Apply(title).TextColor(t.Text)
		})
	})
}

// toolbarControl is one borderless 28 pt toolbar control. It takes the
// sidebar, refresh, and overflow faces from the tokens and shows the focus
// ring 3 pt outside its bounds.
type toolbarControlSpec struct {
	key      string
	label    string
	tooltip  string
	on       bool     // drawn as chosen, in the accent tint
	disabled bool     // no pointer, no keyboard activation
	face     ui.Color // glyph colour; zero keeps the token default
	content  func()   // the glyph, or a spinner while the control works
	onClick  func()
}

func (w *Window) toolbarControl(c *ui.Context, t mui.Tokens, spec toolbarControlSpec) ui.Element {
	face := t.Muted
	switch {
	case spec.disabled:
		face = t.Faint
	case spec.on:
		face = t.Accent
	case spec.face.A > 0:
		face = spec.face
	}
	var bg ui.Color
	if spec.on {
		bg = t.AccentSoft
	}
	el := ui.ButtonBase(c).Key(spec.key).Size(toolbarControl, toolbarControl).Padding(0).
		Radius(mui.RadiusControl).Background(bg).TextColor(face).Label(spec.label).
		Tooltip(spec.tooltip).Cursor(ui.CursorPointer).Transition(mui.Fade).
		FocusRingColor(t.Focus).FocusRingOffset(mui.FocusRingGap)
	if spec.disabled {
		el = el.Disabled(true)
	}
	el.Children(spec.content)
	el.Draw(func(p *ui.Painter, r ui.Rect) {
		if spec.disabled {
			return
		}
		if el.Pressed() || (el.Hovered() && !spec.on) {
			p.Fill(r, t.SurfaceAlt, mui.RadiusControl)
		}
	})
	if !spec.disabled && el.Clicked() && spec.onClick != nil {
		spec.onClick()
	}
	return el
}

// sidebarToggle shows and hides the sidebar. It stays lit while the sidebar
// is hidden so the frame says where the navigation went.
func (w *Window) sidebarToggle(c *ui.Context, t mui.Tokens) {
	w.toolbarControl(c, t, toolbarControlSpec{
		key:     "sidebar-toggle",
		label:   w.sidebarMenuLabel(),
		tooltip: w.sidebarMenuLabel(),
		on:      w.sidebarHidden,
		content: func() { ui.Icon(c, toolbarIconSidebar).Size(toolbarGlyph, toolbarGlyph) },
		onClick: func() { w.setSidebarHidden(!w.sidebarHidden) },
	})
}

// refreshControl refreshes, and is the app's one visible sign that a refresh
// is running: a spinner replaces the glyph in the same 28 pt box, and repeat
// activation is disabled until the refresh returns.
func (w *Window) refreshControl(c *ui.Context, t mui.Tokens) {
	spec := toolbarControlSpec{
		key:     "refresh",
		label:   "Refresh",
		tooltip: "Refresh",
		content: func() { ui.Icon(c, mui.IconRefresh).Size(toolbarGlyph, toolbarGlyph) },
		onClick: func() { w.runRefresh(true) },
	}
	if reason := w.app.refreshBlockedReason(time.Now()); reason != "" {
		spec.tooltip, spec.disabled = reason, true
	}
	if w.app.Busy {
		spec.label, spec.tooltip, spec.disabled = "Refreshing", "Refreshing", true
		spec.content = func() { ui.Spinner(c).Size(toolbarGlyph, toolbarGlyph).TextColor(t.Muted) }
	}
	w.toolbarControl(c, t, spec)
}

// overflowControl opens the menu of the app's secondary commands. MyGo draws
// it in the window, so it lines up with the control's trailing edge.
func (w *Window) overflowControl(c *ui.Context, t mui.Tokens) {
	el := w.toolbarControl(c, t, toolbarControlSpec{
		key:     "more",
		label:   "More",
		tooltip: "More",
		content: func() { ui.Icon(c, toolbarIconMore).Size(toolbarGlyph, toolbarGlyph) },
	})
	el.PopoverMenuButton(func(m *ui.Menu) { w.overflowMenu(m) },
		ui.PopoverMenuButtonOptions{At: ui.AnchorBottomEnd, Self: ui.AnchorTopEnd})
}

// overflowMenu is the toolbar's overflow: the commands that also exist as
// toolbar controls or menu commands, then the secondary ones. Anything that
// cannot run right now is dimmed, not hidden.
func (w *Window) overflowMenu(m *ui.Menu) {
	blocked := w.app.refreshBlockedReason(time.Now()) != ""
	if m.Item("Refresh").Shortcut(ui.Cmd, ui.KeyR).Disabled(w.app.Busy || blocked).Chosen() {
		w.runRefresh(true)
	}
	if m.Item("Settings…").Shortcut(ui.Cmd, ui.KeyComma).Chosen() {
		w.openSettings()
	}
	m.Separator()
	if m.Item("Import OPML…").Chosen() {
		w.importOPML(w.frame)
	}
	if m.Item("Export Favorites…").Chosen() {
		w.exportFavorites(w.frame)
	}
	if m.Item("Restore Favorites…").Chosen() {
		w.restoreFavorites(w.frame)
	}
}

func (w *Window) exportFavorites(c *ui.Context) {
	if err := w.app.ExportFavorites(); err != nil {
		w.toast(c, "Export failed: "+err.Error(), true)
	} else {
		w.toast(c, "Favorites exported", false)
	}
}

// contentStrip is the 44 pt list filter: the channel drop-down, the search
// field, the unwatched check box, and the count of what is shown, right
// aligned in tabular figures.
func (w *Window) contentStrip(c *ui.Context, t mui.Tokens) {
	options := []string{"All channels"}
	ids := []string{""}
	for _, ch := range w.app.Channels {
		options = append(options, ch.Title)
		ids = append(ids, ch.ID)
	}
	selected := "All channels"
	for i, id := range ids {
		if id == w.app.ChannelFilter {
			selected = options[i]
		}
	}
	n := len(w.app.VisibleVideos())
	count := fmt.Sprintf("%d video%s", n, plural(n))
	labelWidth, _ := c.MeasureText(0, ui.Span{Text: "Unwatched", Size: mui.Control.Size, Weight: int(mui.Control.Weight)})
	countWidth, _ := c.MeasureText(0, ui.Span{Text: count, Size: mui.Metadata.Size, Weight: int(mui.Metadata.Weight)})
	checkboxWidth := mui.Space(4) + mui.Space(2) + labelWidth
	available := w.contentWidth
	if available <= 0 {
		available = windowWidth(c)
	}
	channelWidth, searchWidth, gap, collapsed := fitFilterStrip(
		max(0, available-2*mui.Space(4)), checkboxWidth, toolbarControl, countWidth+2,
	)
	strip := ui.Row(c).Height(contentStripHeight).Shrink(0).FillWidth().
		Padding(0, mui.Space(4)).AlignItems(ui.Center).Gap(gap).
		Role(ui.RoleGroup).Label("Video filters")
	strip.Draw(func(p *ui.Painter, r ui.Rect) {
		p.Fill(ui.Rect{X: r.X, Y: r.Y + r.H - mui.Hairline, W: r.W, H: mui.Hairline}, t.BorderSoft, 0)
	})
	strip.Children(func() {
		if channelWidth < channelMenuWidth {
			channel := ui.PopoverMenuButton(c.Key("channel-filter"), "", func(m *ui.Menu) {
				for i, label := range options {
					if m.Item(label).Radio(label == selected).Chosen() {
						w.app.ChannelFilter = ids[i]
					}
				}
			})
			channel.Size(channelWidth, toolbarControl).
				MinWidth(channelWidth).MaxWidth(channelWidth).Shrink(0).Padding(0).
				Background(ui.Color{}).Border(0, ui.Color{}).
				Label("Channel filter").Tooltip(selected).
				FocusRingColor(t.Focus).FocusRingOffset(mui.FocusRingGap).Cursor(ui.CursorPointer)
			channel.Draw(func(p *ui.Painter, r ui.Rect) {
				if channel.Pressed() || channel.Hovered() {
					p.Fill(r, t.SurfaceAlt, mui.RadiusControl)
				}
			})
		} else {
			channel := ui.Select(c.Key("channel-filter"), &selected, options).
				Width(channelWidth).MinWidth(channelWidth).MaxWidth(channelWidth).Shrink(0).
				Height(toolbarControl).Padding(0, mui.Space(3)).FontSize(mui.Control.Size).
				Tooltip(selected)
			if channel.Changed() {
				for i, label := range options {
					if label == selected {
						w.app.ChannelFilter = ids[i]
					}
				}
			}
		}
		ui.SearchField(c.Key("search"), &w.app.Search).
			Width(searchWidth).MinWidth(searchWidth).MaxWidth(searchWidth).Shrink(0).
			Height(toolbarControl).FontSize(mui.Control.Size).Label("Search videos")
		if collapsed {
			box := ui.CheckboxBase(c.Key("unwatched"), &w.app.UnreadOnly).
				Size(toolbarControl, toolbarControl).Center().Shrink(0).
				Label("Unwatched").Tooltip("Unwatched").
				FocusRingColor(t.Focus).FocusRingOffset(mui.FocusRingGap).
				Cursor(ui.CursorPointer)
			face, glyph, state := t.Muted, toolbarIconUnwatched, "Unwatched off"
			if w.app.UnreadOnly {
				face, glyph, state = t.Accent, toolbarIconUnwatchedOn, "Unwatched on"
			}
			box.Draw(func(p *ui.Painter, r ui.Rect) {
				if box.Pressed() || box.Hovered() {
					p.Fill(r, t.SurfaceAlt, mui.RadiusControl)
				}
			})
			box.Children(func() {
				ui.Icon(c, glyph).Size(toolbarGlyph, toolbarGlyph).TextColor(face).Label(state)
			})
		} else {
			ui.Checkbox(c.Key("unwatched"), &w.app.UnreadOnly, "Unwatched").
				Shrink(0).FontSize(mui.Control.Size).Label("Unwatched")
		}
		ui.Spacer(c)
		mui.Metadata.Apply(ui.Text(c, count)).
			TextColor(t.Muted).SingleLine().Shrink(0)
	})
}

// fitFilterStrip gives the filter controls their widths for the room the grid
// pane has: the search field shrinks first, but never below the width its
// contents need, then the channel menu collapses to its glyph form, then the
// Unwatched check box collapses to its icon, and then the gaps tighten. The
// count always stays visible as text. The row never clips its controls or
// runs under the detail pane.
func fitFilterStrip(inner, checkbox, checkboxIcon, count float32) (channel, search, gap float32, collapsed bool) {
	channel, search, gap = channelMenuWidth, searchFieldWidth, mui.Space(2)
	need := func() float32 {
		box := checkbox
		if collapsed {
			box = checkboxIcon
		}
		return channel + search + box + count + 4*gap
	}
	deficit := need() - inner
	if deficit <= 0 {
		return channel, search, gap, false
	}
	shrink := func(deficit float32, value *float32, floor float32) float32 {
		d := min(deficit, *value-floor)
		*value -= d
		return deficit - d
	}
	deficit = shrink(deficit, &search, searchFieldMin)
	if deficit > 0 {
		channel = channelMenuIcon
		deficit = need() - inner
	}
	if deficit > 0 {
		collapsed = true
		deficit = need() - inner
	}
	if deficit > 0 {
		d := min(deficit, 4*(gap-mui.Space(1)))
		gap -= d / 4
		deficit -= d
	}
	return channel, search, gap, collapsed
}

func (w *Window) title() string {
	if w.app.SettingsOpen {
		return "Settings"
	}
	switch {
	case w.app.Selected == "all":
		return "All videos"
	case w.app.Selected == "favorites":
		return "Favorites"
	case strings.HasPrefix(w.app.Selected, "group:"):
		gid := strings.TrimPrefix(w.app.Selected, "group:")
		if g, ok := w.app.Sync.Groups[gid]; ok {
			return g.Name
		}
	case strings.HasPrefix(w.app.Selected, "channel:"):
		ch := w.channelByID(strings.TrimPrefix(w.app.Selected, "channel:"))
		return ch.Title
	}
	return "YouTube Reader"
}

// installMenu builds the native standard menu bar, App through Help, with
// Settings… at Command-Comma in the App menu, the app's document commands and
// Close Window in File, and every toolbar command mirrored, so the toolbar is
// a shortcut surface and never the only way to run a command.
func (w *Window) installMenu() {
	settings := &mygo.MenuItem{Label: "Settings…", Accelerator: "Cmd+,", Click: func(*mygo.MenuItem, *mygo.Window) {
		w.openSettings()
	}}
	w.menuSidebar = &mygo.MenuItem{Label: w.sidebarMenuLabel(), Accelerator: "Ctrl+Cmd+S", Click: func(*mygo.MenuItem, *mygo.Window) {
		w.setSidebarHidden(!w.sidebarHidden)
	}}
	w.menuRefresh = &mygo.MenuItem{Label: "Refresh", Accelerator: "Cmd+R", Click: func(*mygo.MenuItem, *mygo.Window) {
		w.runRefresh(true)
	}}
	appMenu := &mygo.MenuItem{Label: mygo.App.Name(), Submenu: []*mygo.MenuItem{
		{Role: mygo.RoleAbout},
		mygo.Separator(),
		settings,
		mygo.Separator(),
		{Role: mygo.RoleServices},
		mygo.Separator(),
		{Role: mygo.RoleHide},
		{Role: mygo.RoleHideOthers},
		{Role: mygo.RoleUnhide},
		mygo.Separator(),
		{Role: mygo.RoleQuit},
	}}
	fileMenu := &mygo.MenuItem{Label: "File", Submenu: []*mygo.MenuItem{
		{Label: "Import OPML…", Click: func(*mygo.MenuItem, *mygo.Window) { w.importOPML(nil) }},
		{Label: "Export Favorites…", Click: func(*mygo.MenuItem, *mygo.Window) { w.exportFavorites(nil) }},
		{Label: "Restore Favorites…", Click: func(*mygo.MenuItem, *mygo.Window) { w.restoreFavorites(nil) }},
		mygo.Separator(),
		{Role: mygo.RoleClose},
	}}
	// MyGo's Reload, Force Reload, Toggle DevTools and Zoom roles are
	// omitted: this window draws its own content with Content: ui.View, so
	// performRole returns before acting on them, and RoleReload's default
	// accelerator is Command-R, which is already the app's Refresh. A menu
	// command that cannot act is worse than no command.
	viewMenu := &mygo.MenuItem{Label: "View", Submenu: []*mygo.MenuItem{
		w.menuRefresh,
		mygo.Separator(),
		w.menuSidebar,
		mygo.Separator(),
		{Role: mygo.RoleToggleFullScreen},
	}}
	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		appMenu,
		fileMenu,
		{Role: mygo.RoleEditMenu},
		viewMenu,
		{Role: mygo.RoleWindowMenu},
		{Role: mygo.RoleHelp, Submenu: []*mygo.MenuItem{}},
	}))
}

// setSidebarHidden hides or shows the sidebar pane from a toolbar control or
// a menu command, both of which run on the main thread.
func (w *Window) setSidebarHidden(hidden bool) {
	apply := func() {
		w.sidebarHidden = hidden
		if w.menuSidebar != nil {
			w.menuSidebar.SetLabel(w.sidebarMenuLabel())
		}
	}
	if w.window != nil {
		w.window.Update(apply)
		return
	}
	apply()
}

func (w *Window) sidebarMenuLabel() string {
	if w.sidebarHidden {
		return "Show Sidebar"
	}
	return "Hide Sidebar"
}

// syncRefreshMenu dims the View menu's Refresh command while a refresh is
// running or while a quota pause or a missing key makes one impossible, as the
// toolbar control is dimmed.
func (w *Window) syncRefreshMenu() {
	if w.menuRefresh != nil {
		w.menuRefresh.SetEnabled(!w.app.Busy && w.app.refreshBlockedReason(time.Now()) == "")
	}
}
