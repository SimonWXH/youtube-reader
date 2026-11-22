package main

import (
	_ "embed"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/sfsymbols"
	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
	mui "github.com/simon/youtube-reader/ui"
)

// The status surface: one menu bar item and one popover, replacing the status
// copy the window used to repeat in its header and the foot of the sidebar.
// The item carries each state's shape, and the popover carries the copy and
// the two actions a state offers. Refresh feedback lives here alone, so the
// window shows no status text and no toast for a refresh.

// The popover's fixed width and the height its content may grow to before the
// rows scroll: the design board's 320 pt and 420 pt. The arrow is a 12 pt
// token-sized square turned a quarter, so its tip stands 8.5 pt above the
// panel inside a 10 pt strip that points at the menu bar item.
const (
	statusPopoverWidth     float32 = 320
	statusPopoverMaxHeight float32 = 420
	statusArrowStrip       float32 = 10
	statusArrowSide        float32 = 12
)

// statusClockPeriod is how often the item and an open popover are rebuilt while
// nothing else changes, so that "2 min ago" and a due "Next refresh" stay
// true. A minute's copy never needs more than one look a minute.
const statusClockPeriod = 30 * time.Second

// The menu bar item's template images: each state's SF Symbol drawn at 18 pt
// Regular by Scripts/status-icons.swift into 36 px (2x) PNGs of black ink on
// transparency. MyGo's TrayOptions.Icon takes PNG bytes and IconIsTemplate and
// has no SF Symbol API, so the shapes ship as files; macOS tints a template
// image for the menu bar itself, so one file per state serves both appearances.
var (
	//go:embed assets/status/up-to-date.png
	statusIconUpToDate []byte
	//go:embed assets/status/refreshing.png
	statusIconRefreshing []byte
	//go:embed assets/status/paused.png
	statusIconPaused []byte
	//go:embed assets/status/offline.png
	statusIconOffline []byte
	//go:embed assets/status/failed.png
	statusIconFailed []byte
	//go:embed assets/status/setup.png
	statusIconSetup []byte
	//go:embed assets/status/not-refreshed.png
	statusIconNotRefreshed []byte
)

// statusIcon is the menu bar image for one state.
func statusIcon(s statusState) []byte {
	switch s {
	case statusRefreshing:
		return statusIconRefreshing
	case statusPaused:
		return statusIconPaused
	case statusOffline:
		return statusIconOffline
	case statusFailed:
		return statusIconFailed
	case statusNotConfigured:
		return statusIconSetup
	case statusNotRefreshed:
		return statusIconNotRefreshed
	default:
		return statusIconUpToDate
	}
}

// statusSymbol is the SF Symbol a state draws at 18 pt in the popover, the
// same shape the menu bar image was made from.
func statusSymbol(s statusState) string {
	switch s {
	case statusRefreshing:
		return "arrow.triangle.2.circlepath"
	case statusPaused:
		return "pause.circle"
	case statusOffline:
		return "wifi.slash"
	case statusFailed:
		return "exclamationmark.triangle"
	case statusNotConfigured:
		return "key.fill"
	case statusNotRefreshed:
		return "arrow.clockwise"
	default:
		return "checkmark.circle"
	}
}

// statusState is one of the states the menu bar item shows. The shape and the
// words of a state say what it means, so no state depends on colour alone;
// colour is one cue on the popover's glyph and nothing on the item.
type statusState int

const (
	statusUpToDate statusState = iota
	statusRefreshing
	statusPaused
	statusOffline
	statusFailed
	statusNotConfigured
	statusNotRefreshed
)

// statusAction is a button of the popover. The view names each action in the
// state's words, so the model carries the intent rather than a label.
type statusAction int

const (
	statusNoAction statusAction = iota
	statusRefreshNow
	statusRetry
	statusOpenSettings
)

// statusDetail is one label and value row under the popover's heading, with the
// SF Symbol that leads it.
type statusDetail struct {
	Symbol string
	Label  string
	Value  string
}

// status is everything the popover shows. deriveStatus builds it from
// statusInput alone, so the tests drive every state, its copy, and its actions
// without a window or a menu bar.
type status struct {
	State statusState
	// Title is the 13 pt Semibold heading, Body the 12 pt line under it.
	Title string
	Body  string
	// Details are the key/value rows between the heading and the actions.
	Details []statusDetail
	// Primary is the filled button and Secondary the outlined one, if the state
	// offers them. A state with no primary shows no filled button.
	Primary   statusAction
	Secondary statusAction
	// PrimaryDisabled draws the primary in its disabled face: a refresh action
	// stays visible while a quota pause makes it impossible, so the pause is
	// what explains it rather than the action vanishing.
	PrimaryDisabled bool
	// PausedUntil is when a quota pause ends, zero when refreshes run. A pause
	// is raised once as an alert when it begins, and the popover shows its
	// resume time.
	PausedUntil time.Time
}

// statusInput is everything the status surface reads. The clock is an input so
// the same settings can be read at any time in a test.
type statusInput struct {
	Now        time.Time
	Online     bool
	Refreshing bool
	Started    time.Time // when the refresh in flight began, zero when none is
	Settings   core.Settings
	Channels   int
	Videos     []core.Video
}

// deriveStatus reads the app's state into the popover's model. A refresh in
// flight wins over offline, because the attempt the user just started must be
// the one the surface reports; a missing API key and a reader that has never
// refreshed are setup conditions, so neither is reported as healthy or as a
// failed refresh, and setup owns the panel until a key exists: a pause in
// force adds no row there and raises no alert.
func deriveStatus(in statusInput) status {
	settings := in.Settings
	last, hasLast := stampTime(settings.LastRefresh)
	pausedUntil, paused := stampTime(settings.QuotaPausedUntil)
	paused = paused && in.Now.Before(pausedUntil)

	m := status{State: statusUpToDate, Primary: statusRefreshNow, Secondary: statusOpenSettings}
	if paused {
		m.PausedUntil = pausedUntil
	}
	pauseShown := false

	switch {
	case settings.APIKey == "":
		m.State = statusNotConfigured
		m.Title = "Set up YouTube Reader"
		m.Body = "Add a YouTube Data API key in Settings to get started."
		m.Primary = statusOpenSettings
		m.Secondary = statusNoAction
		pauseShown = true
		m.Details = append(m.Details, statusDetail{Symbol: "arrow.triangle.2.circlepath", Label: "Channels", Value: fmt.Sprintf("%d", in.Channels)})

	case in.Refreshing:
		m.State = statusRefreshing
		m.Title = "Refreshing"
		m.Body = fmt.Sprintf("%d channel%s", in.Channels, plural(in.Channels))
		m.Secondary = statusNoAction
		if !in.Started.IsZero() {
			m.Details = append(m.Details, statusDetail{Symbol: "clock", Label: "Started", Value: in.Started.Local().Format("15:04")})
		}
		m.Details = append(m.Details, statusDetail{Symbol: "arrow.triangle.2.circlepath", Label: "Channels", Value: fmt.Sprintf("%d", in.Channels)})

	case paused:
		m.State = statusPaused
		m.Title = "Refresh paused"
		m.Body = "YouTube API quota reached. Refresh resumes on " + pausedUntil.Local().Format("2 Jan, 15:04") + "."
		m.Primary = statusRefreshNow
		m.Secondary = statusOpenSettings
		pauseShown = true
		if hasLast {
			m.Details = append(m.Details, statusDetail{Symbol: "clock", Label: "Last refresh", Value: relativeTime(in.Now, last)})
		}
		m.Details = append(m.Details, statusDetail{Symbol: "arrow.triangle.2.circlepath", Label: "Channels", Value: fmt.Sprintf("%d", in.Channels)})

	case !in.Online:
		m.State = statusOffline
		m.Title = "Offline"
		m.Body = "Showing cached videos"
		if hasLast {
			m.Body += " from " + last.Local().Format("15:04")
		}
		m.Primary = statusRetry
		if hasLast {
			m.Details = append(m.Details, statusDetail{Symbol: "clock", Label: "Last refresh", Value: relativeTime(in.Now, last)})
		}
		m.Details = append(m.Details, statusDetail{Symbol: "arrow.triangle.2.circlepath", Label: "Channels", Value: fmt.Sprintf("%d", in.Channels)})

	case len(settings.RecentFailures) > 0:
		m.State = statusFailed
		m.Title = "Couldn't refresh subscriptions"
		message := statusFailureText(settings.RecentFailures[0].Message)
		m.Body = message
		m.Primary = statusRetry
		at, hasAt := stampTime(settings.RecentFailures[0].At)
		switch {
		case strings.HasPrefix(message, "quota paused"):
			// The stored quota failure carries a machine stamp; the pause has
			// ended, so the row keeps the human sentence instead.
			m.Body = "YouTube API quota reached. Retry to refresh again."
		case rejectedKey(message):
			// The API answered, so the connection is not what to check.
			m.Body = "The YouTube API rejected the key. Check it in Settings, then try again."
		}
		if hasAt {
			m.Details = append(m.Details, statusDetail{Symbol: "clock", Label: "Failed", Value: relativeTime(in.Now, at)})
		}
		m.Details = append(m.Details, statusDetail{Symbol: "arrow.triangle.2.circlepath", Label: "Channels", Value: fmt.Sprintf("%d", in.Channels)})

	case !hasLast:
		m.State = statusNotRefreshed
		m.Title = "Not refreshed yet"
		m.Body = "Refresh to fetch your subscriptions and videos."
		m.Details = append(m.Details, statusDetail{Symbol: "arrow.triangle.2.circlepath", Label: "Channels", Value: fmt.Sprintf("%d", in.Channels)})

	default:
		m.Title = "Up to date"
		m.Body = "Last refresh " + relativeTime(in.Now, last)
		// Manual schedules no automatic refresh, so naming a next one would
		// promise a run that never comes.
		if settings.RefreshMinutes > 0 {
			next := last.Add(time.Duration(settings.RefreshMinutes) * time.Minute)
			m.Details = append(m.Details, statusDetail{Symbol: "clock", Label: "Next refresh", Value: remainingTime(in.Now, next)})
		}
		m.Details = append(m.Details, statusDetail{Symbol: "arrow.triangle.2.circlepath", Label: "Channels", Value: fmt.Sprintf("%d", in.Channels)})
		m.Details = append(m.Details, statusDetail{Symbol: "play.rectangle", Label: "New videos today", Value: fmt.Sprintf("%d", videosToday(in.Videos, in.Now))})
	}

	if paused {
		if !pauseShown {
			m.Details = append([]statusDetail{{Symbol: "clock", Label: "Next refresh", Value: "Paused until " + pausedUntil.Local().Format("2 Jan, 15:04")}}, m.Details...)
		}
		if m.Primary == statusRefreshNow || m.Primary == statusRetry {
			m.PrimaryDisabled = true
		}
	}
	return m
}

// statusFailureQuery matches a request URL's path and leaves out its query,
// which carries only request parameters. A transport failure's reason comes
// after the URL, so dropping the query keeps that reason inside the popover's
// body instead of letting the URL ellipsize it.
var statusFailureQuery = regexp.MustCompile(`(https?://[^\s"?]+)\?[^\s"]*`)

// statusFailureText is the popover's wording of one recorded failure: the
// request keeps its scheme, host and path and loses its query, and any key that
// still reached the disk is redacted before display.
func statusFailureText(raw string) string {
	return core.RedactKey(statusFailureQuery.ReplaceAllString(raw, "$1"))
}

// rejectedKey reports whether the API's own words name the key as the problem,
// where the repair is in Settings rather than in the connection.
func rejectedKey(message string) bool {
	m := strings.ToLower(message)
	return strings.Contains(m, "api key") || strings.Contains(m, "keyinvalid")
}

// stampTime reads one of the settings' RFC3339 stamps. A stamp that does not
// parse is no stamp: the surface never shows a raw timestamp.
func stampTime(raw string) (time.Time, bool) {
	if raw == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// relativeTime renders a past instant in the status surface's one wording:
// "just now", "5 min ago", "3 hr ago", "2 days ago", then a date. Every status
// call site reads this function, so the tests assert this copy.
func relativeTime(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hr ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		days := int(d.Hours() / 24)
		return fmt.Sprintf("%d day%s ago", days, plural(days))
	default:
		return "on " + t.Local().Format("2 Jan 2006")
	}
}

// remainingTime renders a future instant as the time left, the popover's "Next
// refresh" value. It rounds up, so a refresh due in 40 seconds reads "1 min"
// rather than "0 min".
func remainingTime(now, t time.Time) string {
	d := t.Sub(now)
	if d <= 0 {
		return "now"
	}
	minutes := int((d + time.Minute - 1) / time.Minute)
	if minutes < 60 {
		return fmt.Sprintf("%d min", minutes)
	}
	hours := int((d + time.Hour - 1) / time.Hour)
	if hours < 24 {
		return fmt.Sprintf("%d hr", hours)
	}
	days := int((d + 24*time.Hour - 1) / (24 * time.Hour))
	return fmt.Sprintf("%d day%s", days, plural(days))
}

// videosToday counts the videos published on the local day of now, the
// popover's "New videos today" row.
func videosToday(videos []core.Video, now time.Time) int {
	year, day := now.Local().Year(), now.Local().YearDay()
	count := 0
	for _, v := range videos {
		published, ok := stampTime(v.PublishedAt)
		if !ok {
			continue
		}
		published = published.Local()
		if published.Year() == year && published.YearDay() == day {
			count++
		}
	}
	return count
}

// statusActionLabel names an action in the words of the state that offers it.
// "Settings…" opens the app's settings from a healthy or offline state; a
// failure names them in full as the repair it suggests.
func statusActionLabel(a statusAction, state statusState) string {
	switch a {
	case statusRefreshNow:
		return "Refresh now"
	case statusRetry:
		return "Retry"
	case statusOpenSettings:
		if state == statusFailed || state == statusNotConfigured {
			return "Open Settings…"
		}
		return "Settings…"
	}
	return ""
}

// statusItemTitle is the text the menu bar shows beside the icon: none while
// the reader is working, and the state's short name when the user has to know
// without clicking. It keeps offline and failed legible without colour, and
// keeps the item's width steady while a refresh runs.
func statusItemTitle(s statusState) string {
	switch s {
	case statusPaused:
		return "Paused"
	case statusOffline:
		return "Offline"
	case statusFailed:
		return "Refresh failed"
	case statusNotConfigured:
		return "Set up"
	case statusNotRefreshed:
		return "Not refreshed"
	}
	return ""
}

// statusTooltip is the item's hover text: the popover's own heading and body,
// so the pointer reads the same copy as the click.
func statusTooltip(m status) string {
	if m.Body == "" {
		return m.Title
	}
	return m.Title + " - " + m.Body
}

// pausedAlert is the warning a quota pause raises once when it begins; the
// popover shows its resume time. It says what stopped, when refreshes resume,
// and the one action that helps.
func pausedAlert(until time.Time) (title, message, detail string) {
	return "Refresh paused",
		"YouTube API quota reached, so automatic refresh has stopped.",
		"Refresh resumes on " + until.Local().Format("2 Jan at 15:04") + "."
}

// statusPanelView builds the popover's content for one model. arrow is where
// the arrow's centre sits from the panel's left edge; a negative arrow draws a
// panel without one, as the layout test measures. The actions are passed in,
// so the view is a pure function of the model and the tests render both
// appearances of every state without a menu bar.
func statusPanelView(m status, actions statusActions, arrow float32) func(*ui.Context) {
	return func(c *ui.Context) {
		theme := c.Theme()
		tokens := mui.NewTokens(theme, theme.Dark)
		// The window is clear and the panel and its arrow are the only paint:
		// their union is the one shape the system's window shadow takes, with
		// its hairline, and nothing fills the strip or the corners. The arrow's
		// diamond covers the panel's border where the two meet.
		c.Root().Background(ui.Transparent)
		strip := statusArrowStrip
		if arrow < 0 {
			strip = 0
		}
		root := ui.Column(c).Key("status-panel").Width(statusPopoverWidth)
		root.Draw(func(p *ui.Painter, box ui.Rect) {
			panel := ui.Rect{X: box.X, Y: box.Y + strip, W: box.W, H: box.H - strip}
			p.Fill(panel, tokens.Surface, mui.RadiusWindow)
			p.Stroke(panel, tokens.Border, mui.RadiusWindow, mui.Hairline)
			if arrow < 0 {
				return
			}
			cx, cy := box.X+arrow, panel.Y
			half := statusArrowSide / 2
			p.Transform(ui.Rotation(45), cx, cy, func() {
				// The smallest token corner keeps the tip from reading as a
				// spike, as the system's own arrow is rounded.
				p.Fill(ui.Rect{X: cx - half, Y: cy - half, W: statusArrowSide, H: statusArrowSide}, tokens.Surface, mui.RadiusCheck)
			})
		})
		root.Children(func() {
			if strip > 0 {
				ui.Box(c).Height(strip).Shrink(0)
			}
			ui.Column(c).MaxHeight(statusPopoverMaxHeight).Children(func() {
				// Heading: the state's glyph beside its title, with the body copy
				// aligned under the title.
				ui.Column(c).Padding(mui.Space(3)).Children(func() {
					ui.Row(c).Gap(mui.Space(2)).AlignItems(ui.Start).Children(func() {
						// The glyph is decorative: the title carries the state, so it
						// stays out of the accessible tree.
						glyph := sfsymbols.Symbol(c, statusSymbol(m.State)).Size(18).
							Color(statusGlyphColor(m.State, tokens)).Element.Role(ui.RoleNone)
						if m.State == statusRefreshing && !c.Preferences().ReduceMotion {
							// The shape turns while the refresh runs, and rests where
							// the desktop asks for less motion: the words still say so.
							glyph = glyph.Rotate(glyph.Loop("status-refreshing", time.Second, ui.Linear) * 360)
						}
						// The text column shrinks to the width the row gives it:
						// without MinWidth(0) a long unbroken failure URL holds
						// its full intrinsic width and the body paints past the
						// panel's edge instead of wrapping.
						ui.Column(c).Grow(1).MinWidth(0).Children(func() {
							mui.PopoverTitle.Apply(ui.Text(c, m.Title)).TextColor(tokens.Text).SingleLine()
							// A transport failure quotes the request it failed and
							// the reason at the end of that sentence; the six lines
							// keep the reason visible instead of ellipsizing it
							// behind the URL.
							mui.PopoverBody.Apply(ui.Text(c, m.Body)).TextColor(tokens.Muted).MaxLines(6).Wrap()
						})
					})
				})
				ui.Row(c).Height(mui.Hairline).FillWidth().Background(tokens.BorderSoft).MarginY(mui.Space(2))
				if len(m.Details) > 0 {
					// The rows scroll only once the panel reaches its cap, which is
					// what a status of many failures would need.
					ui.Scroll(c).Grow(1).Children(func() {
						ui.Column(c).Padding(0, mui.Space(3), 0).Children(func() {
							for _, d := range m.Details {
								statusDetailRow(c, tokens, d)
							}
						})
					})
				}
				ui.Row(c).Gap(mui.Space(2)).Padding(mui.Space(3)).Children(func() {
					statusActionButtons(c, tokens, m, actions)
				})
			})
		})
	}
}

// statusGlyphColor is the one colour cue of a state: success on the happy mark,
// accent while refreshing, the warning ink offline and the danger ink after a
// failed refresh. Shape and words carry the meaning if it is not seen.
func statusGlyphColor(s statusState, tokens mui.Tokens) ui.Color {
	switch s {
	case statusRefreshing:
		return tokens.Accent
	case statusPaused:
		return tokens.Warning
	case statusOffline:
		return tokens.Warning
	case statusFailed:
		return tokens.Danger
	case statusNotConfigured:
		return tokens.Warning
	case statusNotRefreshed:
		return tokens.Accent
	default:
		return tokens.Success
	}
}

// statusActionButtons draws the state's buttons side by side, each half the
// panel. A refresh in flight keeps its button visible but disabled, so the
// user sees that the action exists and cannot run twice.
func statusActionButtons(c *ui.Context, tokens mui.Tokens, m status, actions statusActions) {
	if m.Primary != statusNoAction {
		disabled := m.State == statusRefreshing || m.PrimaryDisabled
		statusButton(c, tokens, "status-primary", statusActionLabel(m.Primary, m.State), true, disabled, func() {
			switch m.Primary {
			case statusRefreshNow, statusRetry:
				actions.refresh()
			case statusOpenSettings:
				actions.settings()
			}
		})
	}
	if m.Secondary != statusNoAction {
		statusButton(c, tokens, "status-secondary", statusActionLabel(m.Secondary, m.State), false, false, func() {
			if m.Secondary == statusOpenSettings {
				actions.settings()
			}
		})
	}
}

// statusDetailRow is one key/value row: a 14 pt glyph, the label, and the value
// in tabular figures at the trailing edge.
func statusDetailRow(c *ui.Context, tokens mui.Tokens, d statusDetail) {
	ui.Row(c).Height(mui.Space(5)).Gap(mui.Space(2)).AlignItems(ui.Center).Children(func() {
		sfsymbols.Symbol(c, d.Symbol).Size(14).Color(tokens.Faint).Role(ui.RoleNone)
		mui.PopoverBody.Apply(ui.Text(c, d.Label)).TextColor(tokens.Muted).SingleLine().Grow(1)
		statusValue.Apply(ui.Text(c, d.Value)).TextColor(tokens.Text).SingleLine()
	})
}

// statusValue is the detail rows' value type: the popover's body size and line
// height, one weight step up and on tabular figures, so values line up and
// digits keep their width as they change.
var statusValue = func() mui.TextStyle {
	style := mui.PopoverBody
	style.Weight = mui.Medium
	style.Tabular = true
	return style
}()

// statusButton is the popover's action control: the native 28 pt height, the
// token faces for hover, press and disabled, and the shared 2 pt focus ring at
// the token's 3 pt offset.
func statusButton(c *ui.Context, tokens mui.Tokens, key, label string, primary, disabled bool, action func()) ui.Element {
	state := mui.Default
	if disabled {
		state = mui.Disabled
	}
	face := tokens.Face(state, primary, false)
	el := ui.ButtonBase(c).Key(key).Height(28).Grow(1).AlignItems(ui.Center).
		Center().Border(mui.Hairline, face.Border).Background(face.Background).
		TextColor(face.Text).Radius(mui.RadiusControl).Cursor(ui.CursorPointer).
		Transition(mui.Fade).FocusRingColor(tokens.Focus).FocusRingOffset(mui.FocusRingGap)
	if disabled {
		el = el.Disabled(true)
	}
	el.Children(func() {
		text := mui.Control
		if primary {
			text.Weight = mui.Medium
		}
		text.Apply(ui.Text(c, label)).SingleLine()
	})
	el.Draw(func(p *ui.Painter, box ui.Rect) {
		if disabled {
			return
		}
		switch {
		case el.Pressed():
			p.Fill(box, tokens.Face(mui.Active, primary, false).Background, mui.RadiusControl)
		case el.Hovered():
			p.Fill(box, tokens.Face(mui.Hover, primary, false).Background, mui.RadiusControl)
		}
	})
	if el.Clicked() && action != nil {
		action()
	}
	return el
}

// statusActions is what the popover's buttons do. The view never reaches into
// the window, so a test renders the buttons with nothing behind them.
type statusActions struct {
	refresh  func()
	settings func()
}

// statusBar owns the menu bar item and its popover: one item, one popover, and
// one clock that keeps their copy true. Every field is read and changed on the
// main thread, by the tray's callbacks, the network watcher, the clock, and the
// view.
type statusBar struct {
	w      *Window
	tray   *mygo.Tray
	panel  *mygo.Window // the open popover, or nil
	online bool
	// started is when the refresh in flight began; it labels the popover's
	// "Started" row and clears when the refresh returns.
	started time.Time
	// arrowX is where the popover's arrow points: its centre from the panel's
	// left edge, kept on the item as the panel is placed and slid back into a
	// display's work area.
	arrowX float32
	// shownIcon and shownTitle are what the item shows, so a rebuild that
	// changes neither leaves the menu bar alone.
	shownIcon  statusState
	shownTitle string
	// paused remembers whether a quota pause was in force, so the warning is
	// raised once when it begins and not on every attempt while it lasts.
	paused bool
	// blocked remembers whether refresh actions were disabled, so the clock
	// repaints the window when a pause expires and they become usable again.
	blocked bool
	// stop ends the clock goroutine.
	stop chan struct{}
}

// newStatusBar adds the menu bar item and starts the network watcher and the
// clock. It must run after the application is ready.
func newStatusBar(w *Window) *statusBar {
	s := &statusBar{w: w, online: true, shownIcon: -1, stop: make(chan struct{})}
	s.arrowX = statusPopoverWidth / 2
	if item, err := mygo.NewTray(mygo.TrayOptions{
		ID:             "io.github.simon.youtubereader.status",
		Icon:           statusIcon(statusUpToDate),
		IconIsTemplate: true,
	}); err != nil {
		// Without an item the window still works; say once why the status
		// surface is missing instead of taking the app down.
		fmt.Printf("status item: %v\n", err)
		return s
	} else {
		s.tray = item
	}
	s.online = mygo.Network.Status().Online
	s.tray.OnClick(s.toggle)
	// A pause already in force at launch is shown by the Paused state; only a
	// pause that begins while the app runs raises the alert.
	s.paused = !s.model(time.Now()).PausedUntil.IsZero()
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		s.stopClock()
		if s.tray != nil {
			s.tray.Destroy()
		}
	})
	mygo.Network.OnChanged(func(state mygo.NetworkStatus) {
		s.online = state.Online
		s.refresh()
		s.invalidate()
	})
	s.refresh()
	s.startClock()
	return s
}

// model reads the app's state at now into the popover's model.
func (s *statusBar) model(now time.Time) status {
	in := statusInput{
		Now:        now,
		Online:     s.online,
		Refreshing: s.w.app.Busy,
		Started:    s.started,
		Settings:   s.w.app.settingsSnapshot(),
		Channels:   len(s.w.app.Sync.Channels),
		Videos:     s.w.app.Videos,
	}
	return deriveStatus(in)
}

// actions is what the popover's buttons run.
func (s *statusBar) actions() statusActions {
	return statusActions{
		refresh: func() {
			// The panel stays open: it is where the refresh reports itself,
			// showing Refreshing and then the result.
			s.w.runRefresh(true)
		},
		settings: func() {
			s.close()
			s.w.openSettingsWindow()
		},
	}
}

// refresh brings the menu bar item up to date and raises the quota warning when
// a pause begins. It runs on the main thread.
func (s *statusBar) refresh() {
	m := s.model(time.Now())
	s.w.syncRefreshMenu()
	if blocked := s.w.app.refreshBlocked(time.Now()); blocked != s.blocked {
		s.blocked = blocked
		if s.w.window != nil {
			s.w.window.Invalidate()
		}
	}
	paused := !m.PausedUntil.IsZero()
	if paused && !s.paused {
		s.raisePaused(m.PausedUntil)
	}
	s.paused = paused
	if s.tray == nil {
		return
	}
	if m.State != s.shownIcon {
		if err := s.tray.SetIcon(statusIcon(m.State), true); err == nil {
			s.shownIcon = m.State
		}
	}
	if title := statusItemTitle(m.State); title != s.shownTitle {
		s.tray.SetTitle(title)
		s.shownTitle = title
	}
	s.tray.SetToolTip(statusTooltip(m))
}

// invalidate redraws the window and the open popover after a change the status
// surface shows.
func (s *statusBar) invalidate() {
	if s.w.window != nil {
		s.w.window.Invalidate()
	}
	if s.panel != nil {
		s.panel.Invalidate()
	}
}

// startClock rebuilds the item and an open popover every statusClockPeriod, so
// the relative copy stays true while nothing else changes.
func (s *statusBar) startClock() {
	go func() {
		ticker := time.NewTicker(statusClockPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-s.stop:
				return
			case <-ticker.C:
				mygo.RunOnMain(func() {
					s.refresh()
					if s.panel != nil {
						s.panel.Invalidate()
					}
				})
			}
		}
	}()
}

// stopClock ends the clock goroutine once.
func (s *statusBar) stopClock() {
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
}

// toggle opens the popover under the item, or closes the open one, so a second
// click does what an outside click does.
func (s *statusBar) toggle() {
	if s.panel != nil {
		s.close()
		return
	}
	// A focusable flyout rather than an NSPopover: a popover is dismissed by
	// AppKit only while the app is active, and an item's panel takes the
	// keyboard without the app being activated, so a click elsewhere would
	// leave it open. A focusable flyout closes on the system's own outside
	// press and on Escape.
	panel := mygo.NewFlyout(mygo.FlyoutOptions{
		Tray:       s.tray,
		Placement:  mygo.PlacementBottom,
		Gap:        4,
		Focusable:  true,
		Shadow:     true,
		FitContent: true,
		MinWidth:   int(statusPopoverWidth),
		MaxWidth:   int(statusPopoverWidth),
		MaxHeight:  int(statusPopoverMaxHeight + statusArrowStrip),
		Content:    ui.View(s.content),
	})
	s.panel = panel
	panel.OnClosed(func() { s.panel = nil })
	panel.Show()
	s.placeArrow()
	panel.OnMove(s.placeArrow)
	panel.OnResize(s.placeArrow)
}

// placeArrow keeps the arrow on the item: AppKit centres the panel under it
// until the display's edge pushes the panel back, so the arrow's centre is the
// item's centre less the panel's left edge, within the panel's corners.
func (s *statusBar) placeArrow() {
	if s.panel == nil || s.tray == nil {
		return
	}
	item, panel := s.tray.Bounds(), s.panel.Bounds()
	inset := float32(mui.RadiusWindow + mui.Space(1))
	x := clamp(float32(item.X+item.Width/2-panel.X), inset, statusPopoverWidth-inset)
	if x == s.arrowX {
		return
	}
	s.arrowX = x
	s.panel.Invalidate()
}

// content builds one frame of the popover from the state of the moment, so a
// refresh that ends while it is open shows as soon as the frame is asked for.
func (s *statusBar) content(c *ui.Context) {
	statusPanelView(s.model(time.Now()), s.actions(), s.arrowX)(c)
}

func (s *statusBar) close() {
	if s.panel != nil {
		s.panel.Close()
	}
}

// raisePaused raises the quota pause as the alert that announces it when it
// begins; the popover shows its resume time. The dialog blocks, and it is no
// sheet over the panel: the popover is closed first.
func (s *statusBar) raisePaused(until time.Time) {
	s.close()
	title, message, detail := pausedAlert(until)
	go func() {
		result, err := mygo.Dialog.Message(mygo.MessageOptions{
			Type:          mygo.MessageWarning,
			Title:         title,
			Message:       message,
			Detail:        detail,
			Buttons:       []string{"Open Settings…", "OK"},
			DefaultButton: 1,
			CancelButton:  1,
		})
		if err == nil && result.Button == 0 {
			mygo.RunOnMain(s.w.openSettingsWindow)
		}
	}()
}
