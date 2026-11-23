package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/sfsymbols"
	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
	mui "github.com/simon/youtube-reader/ui"
)

// The add-subscription dialog is the app's first modal panel drawn in the
// window. A system alert cannot hold a text field and a group picker, so this
// panel is drawn from the shared tokens: the L2 scrim over the window, the
// panel at L2 elevation, and the same 4 pt ladder, corner radii, and type roles
// as the rest of the app. The approved board fixes every number below.
const (
	// addDialogWidth is the panel's width. At the minimum window width of 960
	// pt it leaves 270 pt clear on each side.
	addDialogWidth float32 = 420
	// addDialogPadding is the panel's inner margin on all four sides.
	addDialogPadding float32 = 20
	// addDialogFieldHeight is the height of the link field and of the group
	// picker's trigger.
	addDialogFieldHeight float32 = 28
	// addDialogInputPadding is the field's horizontal text inset.
	addDialogInputPadding float32 = 8
	// addDialogGroupFieldHeight is the inline new-group name field: 26 pt, the
	// height the board agrees inline fields have.
	addDialogGroupFieldHeight float32 = 26
	// addDialogHelperLine is the height of one line of helper or error copy.
	addDialogHelperLine float32 = 14
	// addDialogCardHeight is the resolved channel card's height: a 32 pt
	// avatar with 8 pt above and below.
	addDialogCardHeight float32 = 48
	// addDialogNoticeHeight is the inline quota, offline, or key notice: 11 pt
	// copy wrapped to two lines, or one line beside the action a key notice
	// carries.
	addDialogNoticeHeight float32 = 42
	// addDialogControlHeight is the footer button height, as every control in
	// the app is 28 pt tall.
	addDialogControlHeight float32 = 28
	// addDialogPickerHeight is one row of the group list.
	addDialogPickerHeight float32 = 28
	// addDialogPickerInset is a group row's horizontal text inset.
	addDialogPickerInset float32 = 9
	// addDialogResolveTimeout bounds one lookup, as the board's 8 seconds.
	addDialogResolveTimeout = 8 * time.Second
)

// The dialog's vertical rhythm: the block gaps the board measures, on the 4 pt
// ladder.
const (
	addDialogGapTight   float32 = 4
	addDialogGapControl float32 = 8
	addDialogGapBlock   float32 = 16
)

// addDialogGroupNewSentinel is the group list's last row, which starts a new
// group instead of choosing one. Group IDs are "group-...", so the value never
// names a real group.
const addDialogGroupNewSentinel = "\x00new-group"

// addDialogState is what the panel shows: the lookup's state, or the reason it
// cannot run. The primary button is enabled in every state but resolving and
// blocked, so invalid input is answered by pressing Add rather than by a button
// that explains nothing.
type addDialogState uint8

const (
	// addDialogInitial is the panel before any lookup: the hint, a picker, and
	// an enabled Add.
	addDialogInitial addDialogState = iota
	// addDialogResolving is a lookup in flight: the primary button holds a
	// spinner and Cancel still works.
	addDialogResolving
	// addDialogResolved is a channel the reader can see before adding it.
	addDialogResolved
	// addDialogNotFound is a lookup that found nothing, or a channel YouTube
	// gave no title for.
	addDialogNotFound
	// addDialogInvalid is input the app answered itself: a shape it does not
	// read, a custom link, a timeout, or a network failure.
	addDialogInvalid
	// addDialogBlocked is a lookup that cannot run, or cannot succeed: offline,
	// quota exhausted, or a key YouTube refused. The primary button is disabled
	// and the notice says when to retry.
	addDialogBlocked
)

// addCause names the copy one state shows.
type addCause uint8

const (
	addCauseNone addCause = iota
	addCauseShape
	addCauseCustom
	addCauseNotFound
	addCauseQuota
	addCauseOffline
	addCauseNoKey
	addCauseRejectedKey
	addCauseTimeout
	addCauseNetwork
)

// addDialog is the panel's state. It lives on the window rather than in a frame
// closure, so a lookup that returns after the panel closed can be dropped
// instead of mutating a frame that no longer exists.
type addDialog struct {
	open bool
	// seq numbers lookups. The panel keeps the number of the lookup it waits
	// for, so an answer that arrives after the panel closed, or after a newer
	// lookup started, is discarded.
	seq int
	// input is the link or channel reference the reader types.
	input string
	// ref is what the input parsed as, kept so the channel card can name the
	// handle the reader typed.
	ref core.ChannelRef
	// groupID is the group the subscription goes to, empty for ungrouped. It is
	// also the group picker's value, which the picker sets directly.
	groupID string
	// pickerOpen is the group list's own open flag. The dialog owns it so that
	// Escape closes the list first and the panel second.
	pickerOpen bool
	// newGroupMode is on while the list's last row has started a new group: the
	// inline name field shows and the subscription creates the group with it.
	newGroupMode bool
	// newGroupName is the inline field's text.
	newGroupName string
	// focusInput asks for the keyboard focus on the link field once, as the
	// panel opens.
	focusInput bool
	// focusNewGroup asks for the focus on the inline name field once, as the
	// list's last row starts a new group.
	focusNewGroup bool

	state   addDialogState
	cause   addCause
	channel core.Channel
	// quotaUntil is when the pause that blocks the add ends, so the retry
	// sentence names the same time the rest of the app waits for.
	quotaUntil time.Time
	// cancel ends the lookup in flight, nil when none runs.
	cancel context.CancelFunc
}

// openAddDialog shows the panel, starting on the group the sidebar has
// selected, with an empty link field.
func (w *Window) openAddDialog() {
	a := &w.addDialog
	if a.open {
		return
	}
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
	a.seq++
	a.open = true
	a.input = ""
	a.ref = core.ChannelRef{}
	a.groupID = w.defaultAddGroup()
	a.pickerOpen = false
	a.newGroupMode = false
	a.newGroupName = ""
	a.focusInput = true
	a.focusNewGroup = false
	a.state = addDialogInitial
	a.cause = addCauseNone
	a.channel = core.Channel{}
	a.quotaUntil = time.Time{}
	w.invalidateMainWindow()
}

// closeAddDialog hides the panel and abandons its lookup. The generation moves
// on, so an answer still in flight lands on a closed panel and is dropped
// without touching state or reopening it.
func (w *Window) closeAddDialog(c *ui.Context) {
	a := &w.addDialog
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
	a.seq++
	a.open = false
	a.pickerOpen = false
	a.newGroupMode = false
	a.state = addDialogInitial
	a.cause = addCauseNone
	a.channel = core.Channel{}
	a.quotaUntil = time.Time{}
	w.repaintDialog(c)
}

// repaintDialog repaints after a dialog state change, from a frame or from a
// lookup that returned on the main thread.
func (w *Window) repaintDialog(c *ui.Context) {
	if c != nil && c.Valid() {
		c.Invalidate()
		return
	}
	w.invalidateMainWindow()
}

// defaultAddGroup is the group the panel starts on: the sidebar's selected
// group while that selection still names one, and ungrouped otherwise. All
// videos, Favorites, a channel, and a group another device removed all fall
// back to ungrouped rather than to a group that is not there.
func (w *Window) defaultAddGroup() string {
	id := destinationID(w.app.Selected, "group:")
	if id == "" {
		return ""
	}
	for _, g := range w.app.Groups {
		if g.ID == id {
			return id
		}
	}
	if _, ok := w.app.Sync.Groups[id]; ok {
		return id
	}
	return ""
}

// addDialogPanelHeight is the panel's height in one state, plus the extra rows
// the group area may show. The board fixes these: 272 pt initial, 286 pt not
// found, 306 pt resolved, 300 pt blocked, 30 pt more while the inline
// new-group field shows, and 18 pt more for a duplicate sentence.
func addDialogPanelHeight(state addDialogState, extra float32) float32 {
	height := 2*addDialogPadding +
		20 + addDialogGapBlock + // title
		16 + addDialogGapTight + // field label
		addDialogFieldHeight + addDialogGapTight + // link field
		addDialogHelperHeight(state) + addDialogGapBlock + // help, error, card, or notice
		16 + addDialogGapTight + // group label
		addDialogFieldHeight + addDialogGapControl + // group picker
		addDialogHelperLine + addDialogGapBlock + // notifications stay off
		addDialogControlHeight // footer
	return height + extra
}

// addDialogHelperHeight is the helper slot's height in one state.
func addDialogHelperHeight(state addDialogState) float32 {
	switch state {
	case addDialogResolved:
		return addDialogCardHeight
	case addDialogBlocked:
		return addDialogNoticeHeight
	case addDialogNotFound:
		return 2 * addDialogHelperLine
	}
	return addDialogHelperLine
}

// addDialogExtrasHeight is the height the group area's extra rows add under the
// picker. While either shows, the picker keeps a 4 pt gap instead of its 8 pt
// one; the duplicate sentence then takes 14 pt and its own 4 pt gap, and the
// inline name field 26 pt and its own 8 pt gap.
func addDialogExtrasHeight(duplicate, newGroupField bool) float32 {
	extra := float32(0)
	if duplicate || newGroupField {
		extra -= addDialogGapControl - addDialogGapTight
	}
	if duplicate {
		extra += addDialogHelperLine + addDialogGapTight
	}
	if newGroupField {
		extra += addDialogGroupFieldHeight + addDialogGapControl
	}
	return extra
}

// addDialogCopy is the panel's words in one language. The app speaks English by
// default and 简体中文 when the system asks for it, as the rest of the app does.
type addDialogCopy struct {
	title               string
	fieldLabel          string
	fieldHint           string
	fieldPlaceholder    string
	groupLabelAdd       string
	groupLabelMove      string
	ungrouped           string
	newGroupItem        string
	newGroupValue       string
	newGroupPlaceholder string
	notifyOff           string
	add                 string
	move                string
	cancel              string
	resolving           string
	alreadyIn           string
	moves               string
	inLibrary           string
	duplicateGroup      string
	addedToast          string
	addedUngroupedToast string
	movedToast          string
	errShape            string
	errNotFound         string
	errNotFoundHint     string
	errCustom           string
	errQuota            string
	errQuotaNoTime      string
	errNoKey            string
	errKey              string
	errOffline          string
	errTimeout          string
	errNetwork          string
	errSave             string
	openSettings        string
}

// addDialogCopyIn returns the panel's words in the reader's language.
func addDialogCopyIn(locale string) addDialogCopy {
	if strings.HasPrefix(strings.ToLower(locale), "zh") {
		return addDialogCopy{
			title:               "新增订阅",
			fieldLabel:          "频道链接或 ID",
			fieldHint:           "粘贴频道链接、@handle 或频道 ID。",
			fieldPlaceholder:    "youtube.com/@handle",
			groupLabelAdd:       "添加到分组",
			groupLabelMove:      "移动到分组",
			ungrouped:           "未分组",
			newGroupItem:        "新建分组…",
			newGroupValue:       "新建分组",
			newGroupPlaceholder: "分组名称",
			notifyOff:           "新加入的频道默认不发送通知。",
			add:                 "添加",
			move:                "移动",
			cancel:              "取消",
			resolving:           "正在查询频道…",
			alreadyIn:           "%s 已经在 %s 分组中。",
			moves:               "这会把 %s 从 %s 移动到 %s。",
			inLibrary:           "已在你的频道库中",
			duplicateGroup:      "已经有一个叫 %s 的分组。",
			addedToast:          "已将 %s 添加到 %s。",
			addedUngroupedToast: "已添加 %s。",
			movedToast:          "已将 %s 移动到 %s。",
			errShape:            "这不像一个频道链接或 ID。",
			errNotFound:         "没有找到对应频道。",
			errNotFoundHint:     "请确认 handle 与频道页显示的一致。",
			errCustom:           "无法通过该自定义链接查询。请改为粘贴频道页地址栏里的链接。",
			errQuota:            "YouTube API 配额已用尽。请在 %s 配额恢复后再试。",
			errQuotaNoTime:      "YouTube API 配额已用尽。请在配额恢复后再试。",
			errNoKey:            "请先在设置中添加 YouTube API 密钥。",
			errKey:              "YouTube API 拒绝了该密钥。请在设置中检查后重试。",
			errOffline:          "当前离线。请联网后再添加这个频道。",
			errTimeout:          "YouTube 没有响应。请重试。",
			errNetwork:          "无法连接 YouTube。请检查网络后重试。",
			errSave:             "无法保存这个订阅。请重试。",
			openSettings:        "打开设置…",
		}
	}
	return addDialogCopy{
		title:               "Add subscription",
		fieldLabel:          "Channel URL or ID",
		fieldHint:           "Paste a channel link, an @handle, or a channel ID.",
		fieldPlaceholder:    "youtube.com/@handle",
		groupLabelAdd:       "Add to group",
		groupLabelMove:      "Move to group",
		ungrouped:           "Ungrouped",
		newGroupItem:        "New Group…",
		newGroupValue:       "New Group",
		newGroupPlaceholder: "Group name",
		notifyOff:           "Notifications stay off for new channels.",
		add:                 "Add",
		move:                "Move",
		cancel:              "Cancel",
		resolving:           "Looking up the channel…",
		alreadyIn:           "%s is already in %s.",
		moves:               "This moves %s from %s to %s.",
		inLibrary:           "Already in your library",
		duplicateGroup:      "A group named %s already exists.",
		addedToast:          "Added %s to %s.",
		addedUngroupedToast: "Added %s.",
		movedToast:          "Moved %s to %s.",
		errShape:            "That does not look like a channel link or ID.",
		errNotFound:         "No channel found for that link.",
		errNotFoundHint:     "Check that the handle is spelled the way the channel shows it.",
		errCustom:           "This custom link cannot be looked up. Paste the channel's own URL instead.",
		errQuota:            "YouTube API quota reached. Try again after the pause ends, at %s.",
		errQuotaNoTime:      "YouTube API quota reached. Try again after the pause ends.",
		errNoKey:            "Add a YouTube API key in Settings first.",
		errKey:              "The YouTube API rejected the key. Check it in Settings, then try again.",
		errOffline:          "You are offline. Reconnect to add this channel.",
		errTimeout:          "YouTube did not answer. Try again.",
		errNetwork:          "Could not reach YouTube. Check your connection, then try again.",
		errSave:             "Could not save the subscription. Try again.",
		openSettings:        "Open Settings…",
	}
}

// addDialogStateCopy is every word the panel can draw, rendered. The copy
// contract test walks it: no em dash, no percentage, no exclamation, and no
// state that only calls itself an error.
func addDialogStateCopy() []string {
	out := []string{}
	for _, locale := range []string{"en", "zh-Hans"} {
		words := addDialogCopyIn(locale)
		out = append(out,
			words.title, words.fieldLabel, words.fieldHint, words.fieldPlaceholder,
			words.groupLabelAdd, words.groupLabelMove, words.ungrouped, words.newGroupItem,
			words.newGroupValue, words.newGroupPlaceholder, words.notifyOff, words.add, words.move, words.cancel,
			words.resolving,
			fmt.Sprintf(words.alreadyIn, "Channel", words.ungrouped),
			fmt.Sprintf(words.moves, "Channel", words.ungrouped, words.ungrouped),
			words.inLibrary,
			fmt.Sprintf(words.duplicateGroup, "Group"),
			fmt.Sprintf(words.addedToast, "Channel", words.ungrouped),
			fmt.Sprintf(words.addedUngroupedToast, "Channel"),
			fmt.Sprintf(words.movedToast, "Channel", words.ungrouped),
			words.errShape, words.errNotFound, words.errNotFoundHint, words.errCustom,
			fmt.Sprintf(words.errQuota, "14:32"), words.errQuotaNoTime,
			words.errNoKey, words.errKey, words.errOffline, words.errTimeout,
			words.errNetwork, words.errSave, words.openSettings,
		)
	}
	return out
}

// addDialogAction is what pressing the primary button does for the channel in
// the panel.
type addDialogAction uint8

const (
	// addDialogAdd subscribes to a channel the library does not hold.
	addDialogAdd addDialogAction = iota
	// addDialogMove puts a channel the library holds in another group into the
	// chosen group.
	addDialogMove
	// addDialogMoveUngrouped puts a channel the library holds in no group into
	// the chosen group.
	addDialogMoveUngrouped
	// addDialogAlreadyThere is a channel already in the chosen group: nothing
	// moves, so the primary button is disabled.
	addDialogAlreadyThere
)

// addDialogPlan is what the panel will do, and where the channel is now.
type addDialogPlan struct {
	action addDialogAction
	fromID string
	from   string // the current group's name, empty when the channel is in no group
}

// addDialogPlanFor works out what adding the channel to the chosen group means.
// A channel's membership is single, so a channel the library already holds in
// another group moves rather than being added twice, and the panel says so
// before the reader presses the button. inLibrary is the library before this
// add, so a channel held in no group is still a move.
func addDialogPlanFor(channelID string, groups []core.ChannelGroup, targetID string, inLibrary bool) addDialogPlan {
	var fromID, fromName string
	found := false
	for _, group := range groups {
		for _, id := range group.ChannelIDs {
			if id != channelID {
				continue
			}
			if group.ID == targetID {
				return addDialogPlan{action: addDialogAlreadyThere, fromID: group.ID, from: group.Name}
			}
			if !found {
				found = true
				fromID = group.ID
				fromName = group.Name
			}
			break
		}
	}
	if found {
		return addDialogPlan{action: addDialogMove, fromID: fromID, from: fromName}
	}
	if inLibrary {
		if targetID == "" {
			return addDialogPlan{action: addDialogAlreadyThere}
		}
		return addDialogPlan{action: addDialogMoveUngrouped}
	}
	return addDialogPlan{action: addDialogAdd}
}

func (w *Window) addDialogTargetID() string {
	if w.addDialog.newGroupMode {
		return addDialogGroupNewSentinel
	}
	return w.addDialog.groupID
}

func (w *Window) addDialogTargetName(words addDialogCopy) string {
	a := &w.addDialog
	if a.newGroupMode {
		name := strings.TrimSpace(a.newGroupName)
		if name == "" {
			return words.newGroupValue
		}
		return name
	}
	return addDialogGroupName(w.app.Groups, a.groupID, words.ungrouped)
}

// addDialogIsMove reports whether the plan changes an existing subscription's
// group rather than subscribing to a new channel.
func addDialogIsMove(plan addDialogPlan) bool {
	return plan.action == addDialogMove || plan.action == addDialogMoveUngrouped
}

// addDialogGroupName names a group by its ID, with the ungrouped label for none.
func addDialogGroupName(groups []core.ChannelGroup, id, ungrouped string) string {
	if id == "" {
		return ungrouped
	}
	for _, group := range groups {
		if group.ID == id {
			return group.Name
		}
	}
	return ungrouped
}

// addDialogGroupNameExists reports whether a group already has the name, so the
// inline field can say so while still allowing it.
func addDialogGroupNameExists(groups []core.ChannelGroup, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, group := range groups {
		if strings.EqualFold(strings.TrimSpace(group.Name), name) {
			return true
		}
	}
	return false
}

// beginAddResolve answers the input the app can answer alone, and starts one
// lookup for what only YouTube can settle. Obviously invalid input, a custom
// link, offline, exhausted quota, and a missing key cost no request: those
// answers come from the parser, the network state, and the settings.
func (w *Window) beginAddResolve(c *ui.Context) {
	a := &w.addDialog
	if a.state == addDialogResolving || a.state == addDialogBlocked {
		return
	}
	ref := core.ParseChannelRef(a.input)
	a.ref = ref
	switch ref.Kind {
	case core.ChannelRefInvalid:
		a.state, a.cause = addDialogInvalid, addCauseShape
		w.repaintDialog(c)
		return
	case core.ChannelRefCustom:
		a.state, a.cause = addDialogInvalid, addCauseCustom
		w.repaintDialog(c)
		return
	}
	if w.offline {
		a.state, a.cause = addDialogBlocked, addCauseOffline
		w.repaintDialog(c)
		return
	}
	settings := w.app.settingsSnapshot()
	if settings.APIKey == "" {
		a.state, a.cause = addDialogBlocked, addCauseNoKey
		w.repaintDialog(c)
		return
	}
	if until, paused := w.app.QuotaRetryAt(time.Now()); paused {
		a.state, a.cause, a.quotaUntil = addDialogBlocked, addCauseQuota, until
		w.repaintDialog(c)
		return
	}
	if a.cancel != nil {
		a.cancel()
	}
	ctx, cancel := context.WithTimeout(context.Background(), addDialogResolveTimeout)
	a.cancel = cancel
	a.state, a.cause = addDialogResolving, addCauseNone
	a.channel = core.Channel{}
	a.quotaUntil = time.Time{}
	a.seq++
	seq := a.seq
	client := appYouTubeClient(settings.APIKey)
	go func() {
		channel, err := client.ResolveChannelRef(ctx, ref)
		mygo.RunOnMain(func() { w.applyAddResolve(seq, channel, err) })
	}()
	w.repaintDialog(c)
}

// applyAddResolve takes the answer to one lookup. An answer for a closed panel,
// or for a lookup a newer one replaced, is dropped: it neither reopens the panel
// nor changes the state the reader is looking at.
func (w *Window) applyAddResolve(seq int, channel core.Channel, err error) {
	a := &w.addDialog
	if !a.open || a.seq != seq {
		return
	}
	a.cancel = nil
	switch {
	case err == nil && strings.TrimSpace(channel.Title) == "":
		a.state, a.cause = addDialogNotFound, addCauseNotFound
	case err == nil:
		a.state, a.cause = addDialogResolved, addCauseNone
		a.channel = channel
	case errors.Is(err, core.ErrChannelNotFound):
		a.state, a.cause = addDialogNotFound, addCauseNotFound
	case errors.Is(err, core.ErrQuota):
		now := time.Now()
		w.app.NoteQuotaPause(now)
		a.state, a.cause = addDialogBlocked, addCauseQuota
		if until, ok := w.app.QuotaRetryAt(now); ok {
			a.quotaUntil = until
		}
	case errors.Is(err, context.DeadlineExceeded):
		a.state, a.cause = addDialogInvalid, addCauseTimeout
	case rejectedKey(err.Error()):
		a.state, a.cause = addDialogBlocked, addCauseRejectedKey
	default:
		a.state, a.cause = addDialogInvalid, addCauseNetwork
	}
	w.invalidateMainWindow()
}

// submitAddDialog is the panel's Return and its primary button: with nothing
// resolved it starts a lookup, and with a resolved channel it adds or moves it.
func (w *Window) submitAddDialog(c *ui.Context) {
	a := &w.addDialog
	switch a.state {
	case addDialogResolving, addDialogBlocked:
		return
	case addDialogResolved:
	default:
		w.beginAddResolve(c)
		return
	}
	locale := ""
	if c != nil {
		locale = c.Locale()
	}
	words := addDialogCopyIn(locale)
	targetID := w.addDialogTargetID()
	plan := addDialogPlanFor(a.channel.ID, w.app.Groups, targetID, w.channelInLibrary(a.channel.ID))
	if plan.action == addDialogAlreadyThere {
		return
	}
	channel, channelName := a.channel, a.channel.Title
	createdID := ""
	if a.newGroupMode {
		name := strings.TrimSpace(a.newGroupName)
		if name == "" {
			name = words.newGroupValue
		}
		group, err := w.app.CreateGroup(name)
		if err != nil {
			w.toast(c, words.errSave, true)
			return
		}
		createdID, targetID = group.ID, group.ID
	}
	var err error
	if plan.action == addDialogAdd {
		err = w.app.AddSubscription(channel, targetID)
	} else {
		_, err = w.app.MoveChannelToGroup(channel.ID, targetID)
	}
	if err != nil {
		// The subscription is what the reader asked for: a group this dialog
		// created for it goes away again rather than being left empty.
		if createdID != "" {
			_ = w.app.DeleteGroup(createdID)
		}
		w.toast(c, words.errSave, true)
		return
	}
	if targetID != "" {
		_ = w.app.SetGroupCollapsed(targetID, false)
	}
	targetName := addDialogGroupName(w.app.Groups, targetID, words.ungrouped)
	w.closeAddDialog(c)
	switch plan.action {
	case addDialogAdd:
		if targetID == "" {
			w.toast(c, fmt.Sprintf(words.addedUngroupedToast, channelName), false)
		} else {
			w.toast(c, fmt.Sprintf(words.addedToast, channelName, targetName), false)
		}
		// The subscription is saved; its first fetch is a convenience that runs
		// behind the panel and never reverses the add.
		w.fetchAddedChannel(channel)
	default:
		w.toast(c, fmt.Sprintf(words.movedToast, channelName, targetName), false)
	}
}

// channelInLibrary reports whether the library already holds the channel, so a
// channel in no group is still a move rather than a new subscription.
func (w *Window) channelInLibrary(channelID string) bool {
	if channelID == "" {
		return false
	}
	_, ok := w.app.Sync.Channels[channelID]
	return ok
}

// fetchAddedChannel reads the one channel a reader just added, so the new
// subscription shows its videos without waiting for the next refresh. Every
// failure is dropped: the subscription is already saved, and a quota that runs
// out at this moment must not undo the add. The lookup runs behind the panel and
// applies on the main thread, as a refresh does, and a test can record the
// request instead of starting a lookup that outlives it.
func (w *Window) fetchAddedChannel(channel core.Channel) {
	if w.onAddedChannel != nil {
		w.onAddedChannel(channel)
		return
	}
	if w.app.refreshBlocked(time.Now()) {
		return
	}
	settings := w.app.settingsSnapshot()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		updated, videos, err := core.FetchChannel(ctx, appYouTubeClient(settings.APIKey), channel)
		if err != nil || strings.TrimSpace(updated.Title) == "" {
			return
		}
		mygo.RunOnMain(func() {
			w.app.ApplyChannelFetch(updated, videos)
			w.invalidateMainWindow()
		})
	}()
}

// addDialogModal draws the panel while it is open. DialogBase supplies the
// window's modal surface: a scrim the app paints, the focus kept inside the
// panel, Tab going round its controls, and the window behind it inert.
func (w *Window) addDialogModal(c *ui.Context, t mui.Tokens) {
	a := &w.addDialog
	if !a.open {
		return
	}
	words := addDialogCopyIn(c.Locale())
	// DialogBase's own open flag stays local: Escape is the panel's to route,
	// so it closes the group list first and the panel second, and the dialog
	// closes when the panel says it was dismissed.
	open := true
	panel := ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		backdrop.Background(t.ModalScrim)
		// The outline is painted rather than bordered, so that the panel's
		// height is the board's 272 pt and not 2 pt of border above it. The
		// minimum restates the board's number: an unexpected long group name
		// may grow the panel, but no state draws it shorter than the board.
		_, duplicate := w.addDialogRows()
		panel.Width(addDialogWidth).Shrink(0).Padding(addDialogPadding).
			MinHeight(addDialogPanelHeight(a.state, addDialogExtrasHeight(duplicate, a.newGroupMode))).
			Radius(mui.RadiusWindow).Background(t.Surface).Label(words.title)
		panel.Shadow(0, mui.ElevationModalOffsetY, mui.ElevationModalBlur, mui.ElevationModalSpread, t.ModalShadow)
		panel.Draw(func(p *ui.Painter, r ui.Rect) {
			p.Stroke(r, t.Border, mui.RadiusWindow, mui.Hairline)
		})
		panel.Children(func() {
			w.addDialogBody(c, t, words)
		})
	})
	// Escape closes the group list first and the panel second, so the nested
	// popover never takes the panel down with it. The panel's shortcut sits on
	// the focused element's own chain, which MyGo routes before the overlay on
	// top.
	panel.OnShortcut(0, ui.KeyEscape, func() {
		if a.pickerOpen {
			a.pickerOpen = false
		} else {
			w.closeAddDialog(c)
		}
		c.Invalidate()
	})
	if panel.Dismissed() {
		w.closeAddDialog(c)
	}
}

// addDialogRows works out what the panel shows for the resolved channel: what
// pressing the primary button would do, and whether the channel is already in
// the library, which adds the duplicate sentence and changes the wording.
func (w *Window) addDialogRows() (addDialogPlan, bool) {
	a := &w.addDialog
	plan := addDialogPlan{action: addDialogAdd}
	if a.state == addDialogResolved && a.channel.ID != "" {
		plan = addDialogPlanFor(a.channel.ID, w.app.Groups, w.addDialogTargetID(), w.channelInLibrary(a.channel.ID))
	}
	return plan, a.state == addDialogResolved && plan.action != addDialogAdd
}

// addDialogBody lays the panel's content out in the board's order.
func (w *Window) addDialogBody(c *ui.Context, t mui.Tokens, words addDialogCopy) {
	a := &w.addDialog
	groups := w.app.Groups
	plan, duplicate := w.addDialogRows()

	mui.DetailTitle.Apply(ui.Text(c, words.title)).TextColor(t.Text).SingleLine().
		Height(20).Margin(0, 0, addDialogGapBlock, 0).Role(ui.RoleHeading).Level(2)
	mui.Secondary.Apply(ui.Text(c, words.fieldLabel)).TextColor(t.Text).SingleLine().
		Height(16).Margin(0, 0, addDialogGapTight, 0)

	w.addDialogLinkField(c, t, words)

	w.addDialogHelper(c, t, words).
		Margin(0, 0, addDialogGapBlock, 0)

	groupLabelText := words.groupLabelAdd
	if addDialogIsMove(plan) || plan.action == addDialogAlreadyThere {
		groupLabelText = words.groupLabelMove
	}
	mui.Secondary.Apply(ui.Text(c, groupLabelText)).TextColor(t.Text).SingleLine().
		Height(16).Margin(0, 0, addDialogGapTight, 0)

	picker := w.addDialogGroupPicker(c, t, words, groups)
	if a.newGroupMode {
		picker.Margin(0, 0, addDialogGapTight, 0)
	} else if duplicate {
		picker.Margin(0, 0, addDialogGapTight, 0)
	} else {
		picker.Margin(0, 0, addDialogGapControl, 0)
	}
	if duplicate {
		w.addDialogDuplicateLine(c, t, words, plan).Margin(0, 0, addDialogGapTight, 0)
	}
	if a.newGroupMode {
		w.addDialogNewGroupField(c, t, words).Margin(0, 0, addDialogGapControl, 0)
	}

	mui.Helper.Apply(ui.Text(c, words.notifyOff)).TextColor(t.Muted).SingleLine().
		Height(addDialogHelperLine).Margin(0, 0, addDialogGapBlock, 0)

	footer := ui.Row(c).Key("add-dialog-footer").Height(addDialogControlHeight).FillWidth().
		Justify(ui.End).AlignItems(ui.Center).Gap(addDialogGapControl)
	footer.Children(func() {
		w.addDialogCancelButton(c, t, words)
		primary := words.add
		if addDialogIsMove(plan) {
			primary = words.move
		}
		w.addDialogPrimaryButton(c, t, words, primary, plan.action == addDialogAlreadyThere)
	})
}

// addDialogLinkField is the 28 pt link field, with the 2 pt focus ring and a
// danger outline while the input is the problem.
func (w *Window) addDialogLinkField(c *ui.Context, t mui.Tokens, words addDialogCopy) {
	a := &w.addDialog
	border := t.Border
	switch a.state {
	case addDialogInvalid, addDialogNotFound:
		border = t.Danger
	}
	field := ui.TextInput(c.Key("add-dialog-input"), &a.input).Height(addDialogFieldHeight).
		PaddingX(addDialogInputPadding).Radius(mui.RadiusControl).
		Background(t.Surface).Border(mui.Hairline, border).TextColor(t.Text).
		Label(words.fieldLabel).Placeholder(words.fieldPlaceholder).MinWidth(0).
		FocusRingColor(t.Focus).FocusRingOffset(mui.FocusRingGap)
	mui.Control.Apply(field)
	field.Margin(0, 0, addDialogGapTight, 0)
	if a.focusInput {
		a.focusInput = false
		field.Focus()
	}
	field.OnSubmit(func() { w.submitAddDialog(c) })
	if field.Changed() && a.state != addDialogInitial {
		// Editing the input starts the panel over: the resolved channel, the
		// error, and the blocked notice all describe what was typed before, and
		// a lookup in flight answers a reference that is no longer here.
		if a.cancel != nil {
			a.cancel()
			a.cancel = nil
		}
		// The generation moves on, so an answer for the reference that was
		// here before is dropped.
		a.seq++
		a.state, a.cause, a.channel = addDialogInitial, addCauseNone, core.Channel{}
		c.Invalidate()
	}
}

// addDialogHelper draws the slot under the link field: one line of help, two of
// a not-found error, the resolved channel's card, or the notice that blocked
// the add.
func (w *Window) addDialogHelper(c *ui.Context, t mui.Tokens, words addDialogCopy) ui.Element {
	a := &w.addDialog
	switch a.state {
	case addDialogResolved:
		return w.addDialogChannelCard(c, t, words, w.channelInLibrary(a.channel.ID))
	case addDialogNotFound:
		box := ui.Column(c).Key("add-dialog-helper").Height(2 * addDialogHelperLine).Gap(0)
		box.Children(func() {
			mui.Helper.Apply(ui.Text(c, words.errNotFound)).TextColor(t.Danger).SingleLine().
				Height(addDialogHelperLine)
			mui.Helper.Apply(ui.Text(c, words.errNotFoundHint)).TextColor(t.Danger).SingleLine().
				Height(addDialogHelperLine)
		})
		return box
	case addDialogInvalid:
		return mui.Helper.Apply(ui.Text(c, w.addDialogErrorCopy(words))).TextColor(t.Danger).SingleLine().
			Height(addDialogHelperLine)
	case addDialogBlocked:
		return w.addDialogNotice(c, t, words)
	case addDialogResolving:
		return mui.Helper.Apply(ui.Text(c, words.resolving)).TextColor(t.Muted).SingleLine().
			Height(addDialogHelperLine)
	default:
		return mui.Helper.Apply(ui.Text(c, words.fieldHint)).TextColor(t.Muted).SingleLine().
			Height(addDialogHelperLine)
	}
}

// addDialogErrorCopy is the sentence that explains an input the app or YouTube
// refused.
func (w *Window) addDialogErrorCopy(words addDialogCopy) string {
	switch w.addDialog.cause {
	case addCauseCustom:
		return words.errCustom
	case addCauseTimeout:
		return words.errTimeout
	case addCauseNetwork:
		return words.errNetwork
	default:
		return words.errShape
	}
}

// addDialogChannelCard shows which channel the input resolved to, so the reader
// sees who they are about to follow before they choose a group.
func (w *Window) addDialogChannelCard(c *ui.Context, t mui.Tokens, words addDialogCopy, inLibrary bool) ui.Element {
	channel := w.addDialog.channel
	card := ui.Row(c).Key("add-dialog-channel").Height(addDialogCardHeight).FillWidth().
		AlignItems(ui.Center).Gap(mui.Space(2)).Padding(0, mui.Space(2)).
		Radius(mui.RadiusNotice).Background(t.SurfaceAlt).Border(mui.Hairline, t.BorderSoft)
	card.Children(func() {
		mui.ChannelAvatar(c, t, channel.AvatarURL, channel.Title, 32)
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			mui.SectionHeader.Apply(ui.Text(c, channel.Title)).TextColor(t.Text).SingleLine()
			mui.Metadata.Apply(ui.Text(c, w.addDialogChannelSubtitle(words, inLibrary))).TextColor(t.Muted).SingleLine()
		})
	})
	return card
}

// addDialogChannelSubtitle is the card's second line: the handle the reader
// typed beside the channel's ID tail, or the note that the library already
// holds the channel.
func (w *Window) addDialogChannelSubtitle(words addDialogCopy, inLibrary bool) string {
	a := &w.addDialog
	if inLibrary {
		return words.inLibrary
	}
	channel := a.channel
	id := channel.ID
	if len(id) > 4 {
		id = "UC…" + id[len(id)-4:]
	}
	switch a.ref.Kind {
	case core.ChannelRefHandle:
		return "@" + a.ref.Value + " · " + id
	case core.ChannelRefName, core.ChannelRefUsername:
		return a.ref.Value + " · " + id
	}
	return id
}

// addDialogGroupPicker is the 28 pt group selector and its popup list: every
// group, then Ungrouped as an explicit choice, then the row that starts a new
// group. MyGo's Select supplies the keyboard model, and the dialog owns the
// list's open flag so Escape can close it before the panel.
func (w *Window) addDialogGroupPicker(c *ui.Context, t mui.Tokens, words addDialogCopy, groups []core.ChannelGroup) ui.Element {
	a := &w.addDialog
	before := a.groupID
	values := make([]string, 0, len(groups)+2)
	for _, group := range groups {
		values = append(values, group.ID)
	}
	values = append(values, "", addDialogGroupNewSentinel)
	parts := ui.SelectBaseOpen(c, &a.groupID, &a.pickerOpen)
	label := addDialogGroupName(groups, a.groupID, words.ungrouped)
	if a.newGroupMode {
		// The subscription goes to the group being named, not to the one the
		// picker showed before, so the picker says so.
		label = words.newGroupValue
	}
	trigger := parts.Trigger.Height(addDialogFieldHeight).FillWidth().Shrink(0).
		PaddingX(addDialogInputPadding).Justify(ui.SpaceBetween).AlignItems(ui.Center).
		Label(label).Radius(mui.RadiusControl).Background(t.Surface).
		Border(mui.Hairline, t.Border).FocusRingColor(t.Focus).FocusRingOffset(mui.FocusRingGap)
	trigger.Children(func() {
		mui.Control.Apply(ui.Text(c, label)).TextColor(t.Text).SingleLine()
		ui.Icon(c, mui.IconChevronDown).Size(10, 10).TextColor(t.Muted)
	})
	trigger.Draw(func(p *ui.Painter, r ui.Rect) {
		if face, ok := buttonPressFace(trigger, t); ok {
			p.Fill(ui.Rect{X: r.X - 1, Y: r.Y - 1, W: r.W + 2, H: r.H + 2}, face, mui.RadiusControl)
		}
	})
	parts.Popup(func(panel ui.Element) {
		panel.MinWidth(addDialogWidth-2*addDialogPadding).Padding(mui.Space(1)).
			Radius(mui.RadiusNotice).Background(t.Surface).Border(mui.Hairline, t.Border).
			Shadow(0, mui.ElevationOffsetY, mui.ElevationBlur, mui.ElevationSpread, t.Shadow)
		for _, value := range values {
			if value == "" {
				// Ungrouped is an explicit choice, under the groups and above
				// the new-group row, so "no group" is never "nothing chosen".
				ui.Box(c).Height(mui.Hairline).FillWidth().Background(t.BorderSoft).
					Margin(mui.Space(1), 0, mui.Space(1), 0)
			}
			item := parts.Item(value).Height(addDialogPickerHeight).
				Padding(0, addDialogPickerInset).Radius(mui.RadiusControl).
				Justify(ui.Start).AlignItems(ui.Center).Gap(mui.Space(1))
			current := value == a.groupID && !a.newGroupMode
			switch {
			case item.Highlighted():
				item.Background(t.AccentSoft)
			case current:
				item.Background(t.SurfaceAlt)
			}
			name := words.newGroupItem
			switch value {
			case addDialogGroupNewSentinel:
			case "":
				name = words.ungrouped
			default:
				name = addDialogGroupName(groups, value, words.ungrouped)
			}
			item.Children(func() {
				color := t.Text
				if item.Highlighted() {
					color = t.Accent
				}
				// Every row keeps the same 16 pt slot before its name, so a
				// checked row's name lines up with the others.
				slot := ui.Box(c).Size(mui.Space(4), mui.Space(4)).Center()
				slot.Children(func() {
					switch {
					case value == addDialogGroupNewSentinel:
						sfsymbols.Symbol(c, "plus").Size(mui.Space(3)).Color(t.Muted).Element.Role(ui.RoleNone)
					case current:
						sfsymbols.Symbol(c, "checkmark").Size(mui.Space(3)).Color(color).Element.Role(ui.RoleNone)
					}
				})
				mui.Control.Apply(ui.Text(c, name)).TextColor(color).SingleLine()
			})
		}
	})
	if trigger.Changed() {
		if a.groupID == addDialogGroupNewSentinel {
			// The list's last row is a command, not a group: put the picker back
			// on the group it showed, keep the pending name empty with its
			// placeholder showing, and take the focus for it.
			a.groupID = before
			a.newGroupMode = true
			a.newGroupName = ""
			a.focusNewGroup = true
		} else {
			// Choosing a group is choosing where the subscription goes: a group
			// being named no longer applies.
			a.newGroupMode = false
			a.newGroupName = ""
		}
		c.Invalidate()
	}
	return trigger
}

// addDialogNewGroupField is the inline 26 pt name field under the picker.
func (w *Window) addDialogNewGroupField(c *ui.Context, t mui.Tokens, words addDialogCopy) ui.Element {
	a := &w.addDialog
	box := ui.Column(c).Key("add-dialog-new-group").Shrink(0)
	box.Children(func() {
		field := ui.TextInput(c.Key("add-dialog-new-group-name"), &a.newGroupName).
			Height(addDialogGroupFieldHeight).PaddingX(addDialogInputPadding).Radius(mui.RadiusControl).
			Background(t.Surface).Border(mui.Hairline, t.Border).TextColor(t.Text).
			Label(words.newGroupPlaceholder).Placeholder(words.newGroupPlaceholder).MinWidth(0).
			FocusRingColor(t.Focus).FocusRingOffset(mui.FocusRingGap)
		mui.Control.Apply(field)
		if a.focusNewGroup {
			a.focusNewGroup = false
			field.Focus()
		}
		field.OnSubmit(func() { w.submitAddDialog(c) })
		if addDialogGroupNameExists(w.app.Groups, a.newGroupName) {
			mui.Helper.Apply(ui.Text(c, fmt.Sprintf(words.duplicateGroup, strings.TrimSpace(a.newGroupName)))).
				TextColor(t.Muted).SingleLine().Height(addDialogHelperLine).
				Margin(addDialogGapTight, 0, 0, 0)
		}
	})
	return box
}

// addDialogDuplicateLine states a duplicate before the reader presses the
// button: where the channel moves from and to, or that it is already there.
func (w *Window) addDialogDuplicateLine(c *ui.Context, t mui.Tokens, words addDialogCopy, plan addDialogPlan) ui.Element {
	a := &w.addDialog
	target := w.addDialogTargetName(words)
	text := ""
	switch plan.action {
	case addDialogAlreadyThere:
		text = fmt.Sprintf(words.alreadyIn, a.channel.Title, target)
	case addDialogMove:
		text = fmt.Sprintf(words.moves, a.channel.Title, plan.from, target)
	case addDialogMoveUngrouped:
		text = fmt.Sprintf(words.moves, a.channel.Title, words.ungrouped, target)
	}
	return mui.Helper.Apply(ui.Text(c, text)).TextColor(t.Muted).SingleLine().
		Height(addDialogHelperLine)
}

// addDialogNotice is the inline banner that blocks the add: quota, offline, or a
// key YouTube refused. A key notice carries the one action that fixes it.
func (w *Window) addDialogNotice(c *ui.Context, t mui.Tokens, words addDialogCopy) ui.Element {
	a := &w.addDialog
	ink := t.Warning
	text := words.errOffline
	switch a.cause {
	case addCauseQuota:
		text = words.errQuotaNoTime
		if !a.quotaUntil.IsZero() {
			text = fmt.Sprintf(words.errQuota, a.quotaUntil.Local().Format("15:04"))
		}
	case addCauseNoKey:
		ink, text = t.Danger, words.errNoKey
	case addCauseRejectedKey:
		ink, text = t.Danger, words.errKey
	}
	row := ui.Row(c).Key("add-dialog-notice").Height(addDialogNoticeHeight).FillWidth().
		AlignItems(ui.Center).Gap(mui.Space(2)).Padding(0, mui.Space(2)).
		Radius(mui.RadiusNotice).Background(ink.Alpha(0.12)).Border(mui.Hairline, ink.Alpha(0.28))
	row.Children(func() {
		ui.Icon(c, mui.IconAlert).Size(addDialogHelperLine, addDialogHelperLine).TextColor(ink)
		mui.Helper.Apply(ui.Text(c, text)).TextColor(ink).Grow(1).MaxLines(2).Wrap()
		if a.cause == addCauseNoKey || a.cause == addCauseRejectedKey {
			w.addDialogActionButton(c, t, "add-dialog-settings", words.openSettings, false, false, nil, func() {
				w.openSettingsWindow()
			})
		}
	})
	return row
}

// addDialogCancelButton closes the panel and leaves the typing undone.
func (w *Window) addDialogCancelButton(c *ui.Context, t mui.Tokens, words addDialogCopy) {
	w.addDialogActionButton(c, t, "add-dialog-cancel", words.cancel, false, false, nil, func() {
		w.closeAddDialog(c)
	})
}

// addDialogPrimaryButton is Add, or Move for a duplicate. It is disabled while
// the lookup runs, while the add cannot run, and when the channel is already in
// the chosen group.
func (w *Window) addDialogPrimaryButton(c *ui.Context, t mui.Tokens, words addDialogCopy, label string, alreadyThere bool) {
	a := &w.addDialog
	disabled := a.state == addDialogResolving || a.state == addDialogBlocked || alreadyThere
	var content func()
	if a.state == addDialogResolving {
		content = func() {
			ui.Spinner(c).Size(12, 12).TextColor(t.Muted)
		}
	}
	w.addDialogActionButton(c, t, "add-dialog-primary", label, true, disabled, content, func() {
		w.submitAddDialog(c)
	})
}

// addDialogActionButton is the board's 28 pt button: a 6 pt radius, a 1 pt
// outline, 12 pt inner padding, and the 2 pt focus ring at its 3 pt offset. A
// primary button fills with the accent; a secondary one sits on the surface.
func (w *Window) addDialogActionButton(c *ui.Context, t mui.Tokens, key, label string, primary, disabled bool, content func(), onClick func()) ui.Element {
	state := mui.Default
	if disabled {
		state = mui.Disabled
	}
	face := t.Face(state, primary, false)
	button := ui.ButtonBase(c).Key(key).Height(addDialogControlHeight).Shrink(0).AlignItems(ui.Center).
		Padding(0, mui.Space(3)).Radius(mui.RadiusControl).
		Background(face.Background).Border(mui.Hairline, face.Border).
		TextColor(face.Text).Label(label).Cursor(ui.CursorPointer).Transition(mui.Fade).
		FocusRingColor(t.Focus).FocusRingOffset(mui.FocusRingGap)
	if content != nil {
		button.Gap(mui.Space(1))
	}
	if disabled {
		button.Disabled(true)
	}
	button.Children(func() {
		if content != nil {
			content()
		}
		mui.Control.Apply(ui.Text(c, label)).TextColor(face.Text).SingleLine()
	})
	button.Draw(func(p *ui.Painter, r ui.Rect) {
		if disabled {
			return
		}
		switch {
		case button.Pressed():
			p.Fill(ui.Rect{X: r.X - 1, Y: r.Y - 1, W: r.W + 2, H: r.H + 2}, t.Face(mui.Active, primary, false).Background, mui.RadiusControl)
		case button.Hovered():
			p.Fill(ui.Rect{X: r.X - 1, Y: r.Y - 1, W: r.W + 2, H: r.H + 2}, t.Face(mui.Hover, primary, false).Background, mui.RadiusControl)
		}
	})
	if !disabled && button.Clicked() && onClick != nil {
		onClick()
	}
	return button
}
