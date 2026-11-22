package ui

import (
	"strings"

	"github.com/egoist/mygo/plugins/sfsymbols"
	"github.com/egoist/mygo/ui"
)

// The shared empty, loading, error, and offline states. One pane decides its
// state from PaneInput alone, so the tests drive every state without a window,
// and every surface draws the copy this file defines.
//
// The rules the components keep:
//   - An empty pane is a 32 pt SF Symbol, a 15 pt Semibold title, one 13 pt
//     secondary sentence, and one 28 pt primary action. No illustration and no
//     sample cards.
//   - A sidebar with no channels says "No channels yet" and offers "Add
//     channel..."; it is never blank.
//   - The detail pane's empty sentence is one 13 pt secondary line.
//   - The content pane's first load is a skeleton of 12 neutral cards. The
//     toolbar's refresh control holds the pane's only spinner, so a load never
//     moves its progress indicator and never shows two.
//   - Activity is determinate only with real progress. A refresh reports none,
//     so every activity sign is an unlabeled spinner rather than an invented
//     percentage.
//   - An error says what failed, why when known, and the next action, inside
//     the affected pane, with Retry. Only a critical or irreversible failure
//     (a quota pause) uses an alert.
//   - Offline keeps cached content visible and disables refresh with the "You
//     are offline" tooltip; the status surface keeps its own Retry.
//   - Nothing here animates, so Reduce Motion changes nothing: a skeleton is a
//     rest, not a pulse.

// SkeletonCards is the number of neutral cards the first load shows. The
// board's count is 12, enough to fill the grid at the default window without
// promising how many videos will arrive.
const SkeletonCards = 12

// PaneState is what the content pane shows.
type PaneState uint8

const (
	// PaneContent shows the grid, cached or fresh.
	PaneContent PaneState = iota
	// PaneLoading shows the initial-load skeleton: a refresh is running and
	// the library has no videos to keep on screen.
	PaneLoading
	// PaneOffline shows the offline state: there is no cached video to keep
	// visible, so the pane names the reason instead of an empty library.
	PaneOffline
	// PaneError shows the last refresh's failure inside the pane, because
	// there is no cached content for the status surface to sit behind.
	PaneError
	// PaneEmpty shows the pane's empty state for its destination.
	PaneEmpty
)

// PaneInput is everything the content pane's state depends on. The window
// fills it from the app, and the tests fill it directly, so a state that
// cannot be reached through the app cannot be shipped by accident.
type PaneInput struct {
	HasAPIKey    bool // a YouTube Data API key is stored
	ChannelCount int  // channels in the synced subscription list
	// LibraryVideos is how many videos the library holds at all, before the
	// destination and the filter bar. Zero is what lets an empty pane explain
	// offline or a failed refresh; when the library has videos, an empty
	// destination is simply empty and its own state explains it.
	LibraryVideos int
	// DestinationVideos is how many videos the selected destination holds
	// before the filter bar: All videos, Favorites, a group, or a channel.
	DestinationVideos int
	// VisibleVideos is how many videos the filter bar leaves.
	VisibleVideos int
	Refreshing    bool // a refresh is in flight
	Online        bool // macOS reports a usable network
	// Failure is the newest recorded refresh failure, empty when the last
	// refresh did not fail. The pane reads it for the cause and the reason it
	// shows; the reason is redacted at display time.
	Failure string
	// Selected is the destination: "all", "favorites", "group:<id>", or
	// "channel:<id>".
	Selected string
	// The filter bar, applied within the destination.
	Search        string
	ChannelFilter string
	UnreadOnly    bool
}

// FiltersActive reports whether the filter bar hides anything.
func (in PaneInput) FiltersActive() bool {
	return in.Search != "" || in.ChannelFilter != "" || in.UnreadOnly
}

// ContentPaneState is the pane's one state decision. Cached content always
// wins over a refresh, a failure, or offline, so nothing reloads a pane that
// already has something to show: that activity belongs to the toolbar control
// and the status surface. A refresh with an empty library shows the skeleton,
// even while offline, because the attempt the user just started is the one the
// pane reports. A destination whose videos the filter bar hides is empty in
// its own right, so offline and a failed refresh do not replace it: the
// library still has cached content, and only the pane's destinations with
// nothing cached at all get those two states.
func ContentPaneState(in PaneInput) PaneState {
	switch {
	case !in.HasAPIKey, in.ChannelCount == 0:
		return PaneEmpty
	case in.Refreshing && in.LibraryVideos == 0:
		return PaneLoading
	case in.VisibleVideos > 0:
		return PaneContent
	case in.DestinationVideos > 0:
		return PaneEmpty
	case in.LibraryVideos == 0 && !in.Online:
		return PaneOffline
	case in.LibraryVideos == 0 && in.Failure != "":
		return PaneError
	default:
		return PaneEmpty
	}
}

// EmptyVariant names the empty state a pane shows, so the copy fits the
// situation instead of repeating one sentence everywhere.
type EmptyVariant uint8

const (
	// EmptySetup is the first launch: no API key yet.
	EmptySetup EmptyVariant = iota
	// EmptyImportChannels is a key with no subscriptions imported.
	EmptyImportChannels
	// EmptyNoVideos is a destination with no videos and no filter hiding any.
	EmptyNoVideos
	// EmptyChannel is one channel with no videos in the retained window.
	EmptyChannel
	// EmptyFavorites is Favorites with nothing favorited.
	EmptyFavorites
	// EmptyFiltered is a destination with videos that the filter bar hides.
	EmptyFiltered
)

// EmptyVariantFor picks the empty state the pane's situation calls for.
func EmptyVariantFor(in PaneInput) EmptyVariant {
	switch {
	case !in.HasAPIKey:
		return EmptySetup
	case in.ChannelCount == 0:
		return EmptyImportChannels
	case in.DestinationVideos == 0 && in.Selected == "favorites":
		return EmptyFavorites
	case in.DestinationVideos == 0 && strings.HasPrefix(in.Selected, "channel:"):
		return EmptyChannel
	case in.DestinationVideos == 0:
		return EmptyNoVideos
	case in.FiltersActive():
		return EmptyFiltered
	default:
		return EmptyNoVideos
	}
}

// EmptyStateCopy is one empty state's words. Symbol is the SF Symbol name the
// state draws at 32 pt; Action is the 28 pt primary button's label, or empty
// for a state that has no next step.
type EmptyStateCopy struct {
	Symbol   string
	Title    string
	Sentence string
	Action   string
}

// The empty states' copy. Each states the situation, says what to do next, and
// never blames the reader.
var (
	emptyCopySetup = EmptyStateCopy{
		Symbol:   "key.fill",
		Title:    "Set up YouTube Reader",
		Sentence: "Add a YouTube Data API key in Settings to start following channels.",
		Action:   "Open Settings",
	}
	emptyCopyImportChannels = EmptyStateCopy{
		Symbol:   "tray.and.arrow.down",
		Title:    "Import your channels",
		Sentence: "Choose an OPML file from your Mac to import channel groups.",
		Action:   "Import OPML",
	}
	emptyCopyNoVideos = EmptyStateCopy{
		Symbol:   "play.rectangle",
		Title:    "No videos yet",
		Sentence: "Refresh to check your subscriptions for new videos.",
		Action:   "Refresh now",
	}
	emptyCopyChannel = EmptyStateCopy{
		Symbol:   "play.rectangle",
		Title:    "No videos from this channel yet",
		Sentence: "Refresh to check for new videos, or choose another channel.",
		Action:   "Refresh now",
	}
	emptyCopyFavorites = EmptyStateCopy{
		Symbol:   "star",
		Title:    "No favorites yet",
		Sentence: "Choose Favorite on a video to keep it here.",
		Action:   "Browse videos",
	}
	emptyCopyFiltered = EmptyStateCopy{
		Symbol:   "magnifyingglass",
		Title:    "No videos match your filters",
		Sentence: "Clear the search and filters to see every video here.",
		Action:   "Clear filters",
	}
)

// CopyForEmpty returns one empty state's words.
func CopyForEmpty(v EmptyVariant) EmptyStateCopy {
	switch v {
	case EmptySetup:
		return emptyCopySetup
	case EmptyImportChannels:
		return emptyCopyImportChannels
	case EmptyChannel:
		return emptyCopyChannel
	case EmptyFavorites:
		return emptyCopyFavorites
	case EmptyFiltered:
		return emptyCopyFiltered
	default:
		return emptyCopyNoVideos
	}
}

// The sidebar's and detail pane's empty copy, and the strings the loading and
// offline states share.
const (
	// EmptySidebarTitle heads the sidebar when no channel is imported.
	EmptySidebarTitle = "No channels yet"
	// EmptySidebarSentence says how channels get here.
	EmptySidebarSentence = "Import an OPML file to add the channels you follow."
	// EmptySidebarAction opens the OPML importer.
	EmptySidebarAction = "Add channel..."
	// EmptyDetailSentence is the detail pane's fallback when no video is
	// chosen; it is one 13 pt secondary line, with no title or action.
	EmptyDetailSentence = "Select a video to see details"
	// LoadingCancelLabel ends a refresh that has nothing on screen yet.
	LoadingCancelLabel = "Cancel"
	// LoadingGridLabel names the skeleton for assistive technology. The cards
	// themselves are decoration: the toolbar control already says the refresh
	// is running, so a screen reader hears one group, not twelve placeholders.
	LoadingGridLabel = "Loading videos"
	// OfflineTitle heads the content pane when it has no cached video.
	OfflineTitle = "You are offline"
	// OfflineSentence explains what survives without a network.
	OfflineSentence = "Videos you loaded before stay available. Reconnect to refresh."
	// OfflineTooltip is the refresh control's reason while offline.
	OfflineTooltip = "You are offline"
	// OfflineActionLabel is the pane's retry face while offline. It stays
	// visible and disabled, so the way back is named even when it cannot run;
	// the status surface keeps the action that works.
	OfflineActionLabel = "Retry"
)

// ErrorCause is why a refresh failed, as far as the app can tell. The pane
// turns it into the sentence that says what to check, and the raw recorded
// message is shown beside it when it adds information.
type ErrorCause uint8

const (
	// CauseConnection is a transport failure or an unknown cause.
	CauseConnection ErrorCause = iota
	// CauseRejectedKey is the API rejecting the stored key.
	CauseRejectedKey
	// CauseQuota is the API quota that paused refreshes.
	CauseQuota
)

// RefreshErrorCopy is the inline error's words: what failed, and the next
// action. The recorded reason travels separately, so the sentence stays short
// and never quotes a full request URL.
func RefreshErrorCopy(cause ErrorCause) (title, sentence string) {
	title = "Could not refresh subscriptions"
	switch cause {
	case CauseRejectedKey:
		sentence = "The YouTube API rejected the key. Check it in Settings, then try again."
	case CauseQuota:
		sentence = "YouTube API quota reached. Try again after the pause ends."
	default:
		sentence = "Check your connection, then try again."
	}
	return title, sentence
}

// StateCopy is every word this file draws. The tests walk it, so a new state
// cannot ship an em dash, a fake percentage, or an exclamation.
func StateCopy() []string {
	out := []string{
		EmptySidebarTitle, EmptySidebarSentence, EmptySidebarAction,
		EmptyDetailSentence, LoadingCancelLabel,
		OfflineTitle, OfflineSentence, OfflineTooltip, OfflineActionLabel,
	}
	for _, v := range []EmptyVariant{EmptySetup, EmptyImportChannels, EmptyNoVideos, EmptyChannel, EmptyFavorites, EmptyFiltered} {
		c := CopyForEmpty(v)
		out = append(out, c.Title, c.Sentence, c.Action)
	}
	for _, cause := range []ErrorCause{CauseConnection, CauseRejectedKey, CauseQuota} {
		title, sentence := RefreshErrorCopy(cause)
		out = append(out, title, sentence)
	}
	return out
}

// EmptySpec is one empty pane to draw. Copy carries the words, so a caller
// never retypes them; OnAction is the 28 pt primary action, or nil for a state
// that has no next step.
type EmptySpec struct {
	Key            string
	Copy           EmptyStateCopy
	ActionDisabled bool
	ActionTooltip  string
	OnAction       func()
}

// EmptyPane draws an empty state in the pane it belongs to: one symbol, one
// title, one sentence, one action, centered in a 320 pt measure.
func EmptyPane(c *ui.Context, tokens Tokens, spec EmptySpec) ui.Element {
	return statePane(c, tokens, statePaneSpec{
		Key:            spec.Key,
		Symbol:         spec.Copy.Symbol,
		SymbolColor:    tokens.Muted,
		Title:          spec.Copy.Title,
		Sentence:       spec.Copy.Sentence,
		Action:         spec.Copy.Action,
		ActionDisabled: spec.ActionDisabled,
		ActionTooltip:  spec.ActionTooltip,
		OnAction:       spec.OnAction,
	})
}

// OfflineSpec is the content pane's offline state.
type OfflineSpec struct {
	Key string
}

// OfflinePane explains an empty pane while offline. Cached content is kept by
// the callers that have it, so this state only appears when there is nothing
// to keep: the refresh face stays visible and disabled, with the reason, while
// the status surface holds the Retry that can run.
func OfflinePane(c *ui.Context, tokens Tokens, spec OfflineSpec) ui.Element {
	return statePane(c, tokens, statePaneSpec{
		Key:            spec.Key,
		Symbol:         "wifi.slash",
		SymbolColor:    tokens.Warning,
		Title:          OfflineTitle,
		Sentence:       OfflineSentence,
		Action:         OfflineActionLabel,
		ActionDisabled: true,
		ActionTooltip:  OfflineTooltip,
	})
}

// ErrorSpec is an inline error in the pane it concerns.
type ErrorSpec struct {
	Key            string
	Cause          ErrorCause
	Reason         string // the recorded failure, redacted; empty when it adds nothing
	ActionDisabled bool
	ActionTooltip  string
	OnRetry        func()
}

// ErrorPane draws an error inline: the symbol and title say what failed, the
// sentence says what to do, the reason box shows the recorded failure when it
// helps, and Retry is the next action. It is a live region, so assistive
// technology reads it when it appears; it is not an alert dialog, because a
// refresh failure loses nothing.
func ErrorPane(c *ui.Context, tokens Tokens, spec ErrorSpec) ui.Element {
	title, sentence := RefreshErrorCopy(spec.Cause)
	return statePane(c, tokens, statePaneSpec{
		Key:            spec.Key,
		Symbol:         "exclamationmark.triangle",
		SymbolColor:    tokens.Danger,
		Title:          title,
		Sentence:       sentence,
		Reason:         spec.Reason,
		Action:         "Retry",
		ActionDisabled: spec.ActionDisabled,
		ActionTooltip:  spec.ActionTooltip,
		OnAction:       spec.OnRetry,
		Role:           ui.RoleAlert,
	})
}

// statePaneSpec is the one layout the empty, offline, and error states share.
type statePaneSpec struct {
	Key            string
	Symbol         string
	SymbolColor    ui.Color
	Title          string
	Sentence       string
	Reason         string
	Action         string
	ActionDisabled bool
	ActionTooltip  string
	OnAction       func()
	Role           ui.Role
}

// statePane centers a state in its pane: the symbol, the title, the sentence,
// the optional reason box, and the optional action, in a 320 pt measure so no
// sentence runs the width of the window.
func statePane(c *ui.Context, tokens Tokens, spec statePaneSpec) ui.Element {
	pane := ui.Column(c).Grow(1).Center().AlignItems(ui.Center).Padding(Space(8))
	if spec.Role != ui.RoleAuto {
		pane = pane.Role(spec.Role)
	}
	pane.Children(func() {
		ui.Column(c).Width(Space(80)).AlignItems(ui.Center).Gap(Space(3)).Children(func() {
			if spec.Symbol != "" {
				// The symbol is decoration: the title carries the state, so it
				// stays out of the accessible tree.
				sfsymbols.Symbol(c, spec.Symbol).Size(Space(8)).Color(spec.SymbolColor).Element.Role(ui.RoleNone)
			}
			if spec.Title != "" {
				EmptyTitle.Apply(ui.Text(c, spec.Title)).TextColor(tokens.Text).
					TextAlign(ui.Center).Wrap().Role(ui.RoleHeading).Level(2)
			}
			if spec.Sentence != "" {
				EmptyBody.Apply(ui.Text(c, spec.Sentence)).TextColor(tokens.Muted).
					TextAlign(ui.Center).Wrap()
			}
			if spec.Reason != "" {
				ui.Column(c).Padding(Space(2), Space(3)).Radius(RadiusNotice).
					Background(tokens.SurfaceAlt).Children(func() {
					Secondary.Apply(ui.Text(c, spec.Reason)).TextColor(tokens.Muted).
						MaxLines(3).Wrap()
				})
			}
			if spec.Action != "" {
				PaneAction(c, tokens, spec.Key+"-action", spec.Action, true, spec.ActionDisabled, spec.ActionTooltip, spec.OnAction)
			}
		})
	})
	return pane
}

// PaneAction is a 28 pt button drawn from the tokens: the accent fill for a
// pane's one primary action, a neutral face for a secondary one. It takes the
// keyboard focus with the shared 2 pt ring at 3 pt, and a disabled action
// keeps its label and tooltip so the reason it cannot run stays readable.
func PaneAction(c *ui.Context, tokens Tokens, key, label string, primary, disabled bool, tooltip string, onClick func()) ui.Element {
	state := Default
	if disabled {
		state = Disabled
	}
	face := tokens.Face(state, primary, false)
	el := ui.ButtonBase(c).Key(key).Height(Space(7)).Padding(0, Space(3)).
		Radius(RadiusControl).Border(Hairline, face.Border).
		Background(face.Background).TextColor(face.Text).Label(label).
		Transition(Fade).FocusRingColor(tokens.Focus).FocusRingOffset(FocusRingGap)
	if disabled {
		el = el.Disabled(true)
	} else {
		el = el.Cursor(ui.CursorPointer)
	}
	if tooltip != "" {
		el = el.Tooltip(tooltip)
	}
	el.Children(func() {
		text := Control
		if primary {
			text.Weight = Medium
		}
		text.Apply(ui.Text(c, label)).SingleLine()
	})
	el.Draw(func(p *ui.Painter, r ui.Rect) {
		if disabled {
			return
		}
		// The hover and press faces paint the button's hairline too, so the
		// face changes as one shape instead of leaving a 1 pt ring behind.
		hover := ui.Rect{X: r.X - Hairline, Y: r.Y - Hairline, W: r.W + 2*Hairline, H: r.H + 2*Hairline}
		switch {
		case el.Pressed():
			p.Fill(hover, tokens.Face(Active, primary, false).Background, RadiusControl)
		case el.Hovered():
			p.Fill(hover, tokens.Face(Hover, primary, false).Background, RadiusControl)
		}
	})
	if !disabled && el.Clicked() && onClick != nil {
		onClick()
	}
	return el
}

// statesIconPlus is the sidebar empty state's action glyph. It follows the
// shared 24 pt box, 1.5 pt stroke, round joins in ui/icons.go without adding a
// role to that file.
var statesIconPlus = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M12 5v14"/><path d="M5 12h14"/></svg>`))

// SidebarEmpty fills the sidebar's channel section when no channel is
// imported: a 13 pt Semibold title, one 12 pt secondary sentence, and a 28 pt
// quiet "Add channel..." action, so the sidebar is never a blank column.
func SidebarEmpty(c *ui.Context, tokens Tokens, onAdd func()) ui.Element {
	el := ui.Column(c).Padding(Space(2)).Gap(Space(1))
	el.Children(func() {
		SectionHeader.Apply(ui.Text(c, EmptySidebarTitle)).TextColor(tokens.Text).SingleLine()
		Secondary.Apply(ui.Text(c, EmptySidebarSentence)).TextColor(tokens.Muted).Wrap()
		add := ui.ButtonBase(c).Key("sidebar-add-channel").Height(SidebarRowHeight).
			FillWidth().Gap(Space(2)).Padding(0, Space(2)).Radius(RadiusControl).
			Background(tokens.Sidebar).TextColor(tokens.Accent).Label(EmptySidebarAction).
			Justify(ui.Start).Transition(Fade).FocusRingColor(tokens.Focus).
			FocusRingOffset(FocusRingGap).Cursor(ui.CursorPointer)
		add.Children(func() {
			ui.Icon(c, statesIconPlus).Size(Space(4), Space(4))
			Control.Apply(ui.Text(c, EmptySidebarAction)).SingleLine()
		})
		add.Draw(func(p *ui.Painter, r ui.Rect) {
			switch {
			case add.Pressed():
				p.Fill(r, tokens.Face(Active, false, true).Background, RadiusControl)
			case add.Hovered():
				p.Fill(r, tokens.Face(Hover, false, true).Background, RadiusControl)
			}
		})
		if add.Clicked() && onAdd != nil {
			onAdd()
		}
	})
	return el
}

// DetailEmpty is the detail pane's fallback when the pane is open with no
// video: one 13 pt secondary sentence. The pane is normally absent instead of
// empty, so this state says what to do rather than leaving a blank column.
func DetailEmpty(c *ui.Context, tokens Tokens) ui.Element {
	return ui.Column(c).Grow(1).Center().AlignItems(ui.Center).Padding(Space(8)).Children(func() {
		EmptyBody.Apply(ui.Text(c, EmptyDetailSentence)).TextColor(tokens.Muted).
			TextAlign(ui.Center).Wrap()
	})
}

// SkeletonSpec is the skeleton grid's geometry. The caller passes the same
// card metrics the real grid computes, so the skeleton and the cards that
// replace it share one layout and the pane does not jump.
type SkeletonSpec struct {
	Count       int     // cards; zero means SkeletonCards
	Columns     int     // columns; at least one
	CardWidth   float32 // one card's width
	CardHeight  float32 // one card's height, thumbnail plus text block
	ThumbHeight float32 // the 16:9 thumbnail's height
	Gap         float32 // the grid's gap
}

// SkeletonGrid draws the first load: 12 neutral cards with the real cards'
// geometry. It carries no spinner and no animation. The toolbar's refresh
// control is the pane's one activity sign, and a static skeleton respects
// Reduce Motion by resting rather than pulsing.
func SkeletonGrid(c *ui.Context, tokens Tokens, spec SkeletonSpec) ui.Element {
	count := spec.Count
	if count <= 0 {
		count = SkeletonCards
	}
	columns := max(spec.Columns, 1)
	rows := (count + columns - 1) / columns
	grid := ui.Column(c).Gap(spec.Gap).Shrink(0).Role(ui.RoleGroup).Label(LoadingGridLabel)
	grid.Children(func() {
		for row := 0; row < rows; row++ {
			ui.Row(c).Gap(spec.Gap).Shrink(0).Children(func() {
				for col := 0; col < columns; col++ {
					if row*columns+col >= count {
						break
					}
					skeletonCard(c, tokens, spec)
				}
			})
		}
	})
	return grid
}

// skeletonCard is one neutral card: a 16:9 surface where the thumbnail will
// be, and the text block's two lines as neutral bars.
func skeletonCard(c *ui.Context, tokens Tokens, spec SkeletonSpec) ui.Element {
	card := ui.Column(c).Width(spec.CardWidth).Height(spec.CardHeight).Shrink(0).
		Radius(RadiusCard).Background(tokens.Surface).Clip()
	// The outline paints over the thumbnail band, so the card keeps one rounded
	// silhouette instead of the band's square corners cutting through it.
	card.DrawOver(func(p *ui.Painter, r ui.Rect) {
		p.Stroke(r, tokens.BorderSoft, RadiusCard, Hairline)
	})
	card.Children(func() {
		ui.Row(c).Height(spec.ThumbHeight).FillWidth().Background(tokens.SurfaceAlt)
		ui.Column(c).Grow(1).Padding(Space(2)).Gap(Space(1)).Children(func() {
			skeletonBar(c, tokens, spec.CardWidth-2*Space(2), Space(2))
			skeletonBar(c, tokens, (spec.CardWidth-2*Space(2))*0.6, Space(2))
		})
	})
	return card
}

// skeletonBar is one neutral text line.
func skeletonBar(c *ui.Context, tokens Tokens, width, height float32) ui.Element {
	return ui.Row(c).Width(width).Height(height).Shrink(0).
		Radius(RadiusCheck).Background(tokens.SurfaceAlt)
}
