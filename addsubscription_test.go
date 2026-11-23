package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
	mui "github.com/simon/youtube-reader/ui"
)

// addDialogTestApp is the smallest window that shows the sidebar with two
// groups and their channels, so the duplicate cases are all reachable.
func addDialogTestApp(t *testing.T) (*App, *Window) {
	t.Helper()
	dir := t.TempDir()
	a := newTestApp(t, dir)
	t.Cleanup(a.Close)
	group := core.ChannelGroup{ID: "group-fun", Name: "Fun", ChannelIDs: []string{"ch-fun"}, UpdatedAt: 100}
	other := core.ChannelGroup{ID: "group-dev", Name: "GameDev", ChannelIDs: []string{"ch-dev"}, UpdatedAt: 100}
	a.Sync.Groups = map[string]core.ChannelGroup{group.ID: group, other.ID: other}
	a.Sync.Channels = map[string]core.Channel{
		"ch-fun":   {ID: "ch-fun", Title: "Anna Cramling", GroupIDs: []string{group.ID}, UpdatedAt: 100},
		"ch-dev":   {ID: "ch-dev", Title: "Developer", GroupIDs: []string{other.ID}, UpdatedAt: 100},
		"ch-loose": {ID: "ch-loose", Title: "Loose channel", UpdatedAt: 100},
	}
	a.Groups = []core.ChannelGroup{group, other}
	a.Channels = []core.Channel{
		{ID: "ch-fun", Title: "Anna Cramling"},
		{ID: "ch-dev", Title: "Developer"},
		{ID: "ch-loose", Title: "Loose channel"},
	}
	a.Settings.SidebarWidth = defaultSidebarWidth
	a.Settings.APIKey = "test"
	w := &Window{app: a, selectedIndex: new(int)}
	return a, w
}

// addDialogTester builds one frame and then opens the panel for the next.
func addDialogTester(t *testing.T, w *Window, width, height int) *ui.Tester {
	t.Helper()
	tt := ui.NewTester(w.shell, width, height)
	tt.Frame()
	w.openAddDialog()
	tt.Frame()
	return tt
}

func TestAddDialogPanelHeightMatchesTheBoard(t *testing.T) {
	tests := []struct {
		name  string
		state addDialogState
		extra float32
		want  float32
	}{
		{"initial", addDialogInitial, 0, 272},
		{"not found", addDialogNotFound, 0, 286},
		{"resolved", addDialogResolved, 0, 306},
		{"quota or offline", addDialogBlocked, 0, 300},
		{"new group field", addDialogResolved, addDialogExtrasHeight(false, true), 336},
		{"duplicate sentence", addDialogResolved, addDialogExtrasHeight(true, false), 320},
		{"duplicate sentence and new group field", addDialogResolved, addDialogExtrasHeight(true, true), 354},
	}
	if got := addDialogExtrasHeight(false, true); got != 30 {
		t.Fatalf("inline new-group field adds %v pt; want the board's 30", got)
	}
	for _, tt := range tests {
		if got := addDialogPanelHeight(tt.state, tt.extra); got != tt.want {
			t.Errorf("%s panel height = %v pt; want %v", tt.name, got, tt.want)
		}
	}
}

func TestAddDialogFitsTheMinimumWindow(t *testing.T) {
	_, w := addDialogTestApp(t)
	for _, size := range []struct{ w, h int }{{minMainWindowWidth, minMainWindowHeight}, {1280, 808}} {
		tt := addDialogTester(t, w, size.w, size.h)
		panel, ok := tt.AccessByRole(ui.RoleDialog, "Add subscription")
		if !ok {
			t.Fatalf("%dx%d: the dialog panel is not in the frame", size.w, size.h)
		}
		if panel.Bounds.W != addDialogWidth || panel.Bounds.H != 272 {
			t.Fatalf("%dx%d: panel is %vx%v pt; want %vx272", size.w, size.h, panel.Bounds.W, panel.Bounds.H, addDialogWidth)
		}
		if panel.Bounds.X < 0 || panel.Bounds.Y < 0 ||
			panel.Bounds.X+panel.Bounds.W > float32(size.w) ||
			panel.Bounds.Y+panel.Bounds.H > float32(size.h) {
			t.Fatalf("%dx%d: panel at %+v is not inside the window", size.w, size.h, panel.Bounds)
		}
		wantX := (float32(size.w) - addDialogWidth) / 2
		wantY := (float32(size.h) - 272) / 2
		if panel.Bounds.X != wantX || panel.Bounds.Y != wantY {
			t.Fatalf("%dx%d: panel at %v,%v; want centred at %v,%v",
				size.w, size.h, panel.Bounds.X, panel.Bounds.Y, wantX, wantY)
		}
		w.closeAddDialog(nil)
		tt.Frame()
	}
}

func TestAddDialogEscapeClosesThePickerFirst(t *testing.T) {
	_, w := addDialogTestApp(t)
	tt := addDialogTester(t, w, 1280, 808)
	w.addDialog.pickerOpen = true
	tt.Frame()

	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if !w.addDialog.open {
		t.Fatal("Escape with the group list open closed the whole dialog")
	}
	if w.addDialog.pickerOpen {
		t.Fatal("Escape with the group list open left the list open")
	}

	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if w.addDialog.open {
		t.Fatal("Escape on the dialog without its list open left the dialog open")
	}
}

func TestAddDialogPickerDefaultsToTheSidebarGroup(t *testing.T) {
	a, w := addDialogTestApp(t)
	tests := []struct {
		selected string
		want     string
	}{
		{"group:group-fun", "group-fun"},
		{"all", ""},
		{"favorites", ""},
		{"channel:ch-fun", ""},
		{"group:group-gone", ""},
	}
	for _, tt := range tests {
		a.Selected = tt.selected
		if got := w.defaultAddGroup(); got != tt.want {
			t.Errorf("default group for %q = %q; want %q", tt.selected, got, tt.want)
		}
	}
}

func TestAddDialogInvalidInputIssuesNoRequest(t *testing.T) {
	_, w := addDialogTestApp(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		rw.Header().Set("Content-Type", "application/json")
		rw.Write([]byte(`{"items":[]}`))
	}))
	defer srv.Close()
	previous := appYouTubeClient
	appYouTubeClient = func(apiKey string) core.YouTube {
		return core.YouTube{Key: apiKey, Client: srv.Client(), BaseURL: srv.URL}
	}
	defer func() { appYouTubeClient = previous }()

	words := addDialogCopyIn("en")
	tt := addDialogTester(t, w, 1280, 808)
	inputs := []string{
		"",
		"   ",
		"not a channel at all",
		"https://example.com/watch?v=dQw4w9WgXcQ",
		"youtube.com/playlist?list=PL123",
		"youtube.com/c/SomeCustomName",
		"youtube.com/channel/UCtoo-short",
		"https://www.youtube.com/@a",
	}
	for _, input := range inputs {
		w.addDialog.input = input
		w.addDialog.state = addDialogInitial
		w.submitAddDialog(nil)
		tt.Frame()
		if hits.Load() != 0 {
			t.Fatalf("input %q issued %d requests; invalid input must cost none", input, hits.Load())
		}
		if w.addDialog.state != addDialogInvalid {
			t.Fatalf("input %q left state %v; want the invalid state", input, w.addDialog.state)
		}
		if !tt.HasText(words.errShape) && !tt.HasText(words.errCustom) {
			t.Fatalf("input %q showed no explanation", input)
		}
	}
}

func TestAddDialogOfflineAndQuotaBlockTheAdd(t *testing.T) {
	a, w := addDialogTestApp(t)
	words := addDialogCopyIn("en")
	tt := addDialogTester(t, w, 1280, 808)

	w.offline = true
	w.addDialog.input = "youtube.com/@AnnaCramling"
	w.submitAddDialog(nil)
	tt.Frame()
	if w.addDialog.state != addDialogBlocked || w.addDialog.cause != addCauseOffline {
		t.Fatalf("offline add left state %v cause %v; want the offline notice", w.addDialog.state, w.addDialog.cause)
	}
	if !tt.HasText(words.errOffline) {
		t.Fatal("the offline notice is not on screen")
	}
	primary, ok := tt.AccessByRole(ui.RoleButton, "Add")
	if !ok || !primary.Disabled {
		t.Fatalf("offline primary button = %+v, found %v; want a disabled Add", primary, ok)
	}

	w.offline = false
	a.NoteQuotaPause(time.Now())
	if _, ok := a.QuotaRetryAt(time.Now()); !ok {
		t.Fatal("the quota pause was not armed")
	}
	w.addDialog.state = addDialogInitial
	w.addDialog.cause = addCauseNone
	w.submitAddDialog(nil)
	tt.Frame()
	if w.addDialog.state != addDialogBlocked || w.addDialog.cause != addCauseQuota {
		t.Fatalf("quota-paused add left state %v cause %v; want the quota notice", w.addDialog.state, w.addDialog.cause)
	}
	if w.addDialog.quotaUntil.IsZero() {
		t.Fatal("the quota notice has no retry time to name")
	}
	if want := fmt.Sprintf(words.errQuota, w.addDialog.quotaUntil.Local().Format("15:04")); !tt.HasText(want) {
		t.Fatalf("the quota notice %q is not on screen", want)
	}
}

func TestAddDialogLateAnswerIsDiscarded(t *testing.T) {
	_, w := addDialogTestApp(t)
	tt := addDialogTester(t, w, 1280, 808)
	// The panel is waiting for the lookup this generation started.
	w.addDialog.input = "youtube.com/@AnnaCramling"
	w.addDialog.state = addDialogResolving
	w.addDialog.seq++
	seq := w.addDialog.seq
	tt.Frame()

	w.closeAddDialog(nil)
	tt.Frame()
	w.applyAddResolve(seq, core.Channel{ID: "ch-new", Title: "Anna Cramling"}, nil)
	tt.Frame()
	if w.addDialog.open {
		t.Fatal("a lookup that landed after the dialog closed reopened it")
	}
	if w.addDialog.state != addDialogInitial || w.addDialog.channel.ID != "" {
		t.Fatalf("late answer changed the closed dialog: state %v channel %q", w.addDialog.state, w.addDialog.channel.ID)
	}
}

func TestAddDialogDuplicateOutcomes(t *testing.T) {
	groups := []core.ChannelGroup{
		{ID: "group-fun", Name: "Fun", ChannelIDs: []string{"ch-fun", "ch-multi"}},
		{ID: "group-dev", Name: "GameDev", ChannelIDs: []string{"ch-multi"}},
	}
	tests := []struct {
		name      string
		id        string
		target    string
		inLibrary bool
		action    addDialogAction
		from      string
	}{
		{"in another group", "ch-fun", "group-dev", true, addDialogMove, "Fun"},
		{"already in the chosen group", "ch-fun", "group-fun", true, addDialogAlreadyThere, "Fun"},
		{"already in ungrouped", "ch-loose", "", true, addDialogAlreadyThere, ""},
		{"in the library, in no group", "ch-loose", "group-dev", true, addDialogMoveUngrouped, ""},
		{"not in the library", "ch-new", "group-dev", false, addDialogAdd, ""},
		{"multi-group matches target", "ch-multi", "group-dev", true, addDialogAlreadyThere, "GameDev"},
		{"multi-group moves to third group", "ch-multi", "group-other", true, addDialogMove, "Fun"},
		{"target new group moves existing", "ch-fun", addDialogGroupNewSentinel, true, addDialogMove, "Fun"},
		{"target new group moves ungrouped", "ch-loose", addDialogGroupNewSentinel, true, addDialogMoveUngrouped, ""},
	}
	for _, tt := range tests {
		plan := addDialogPlanFor(tt.id, groups, tt.target, tt.inLibrary)
		if plan.action != tt.action || plan.from != tt.from {
			t.Errorf("%s: plan = %+v; want action %v from %q", tt.name, plan, tt.action, tt.from)
		}
	}
}

func TestAddDialogDuplicateCopyAndPrimaryLabel(t *testing.T) {
	tests := []struct {
		name        string
		channelID   string
		target      string
		newGroup    bool
		newName     string
		wantText    string
		wantPrimary string
		disabled    bool
	}{
		{
			name: "moves from another group", channelID: "ch-fun", target: "group-dev",
			wantText:    "This moves Anna Cramling from Fun to GameDev.",
			wantPrimary: "Move",
		},
		{
			name: "already in the chosen group", channelID: "ch-fun", target: "group-fun",
			wantText:    "Anna Cramling is already in Fun.",
			wantPrimary: "Add", disabled: true,
		},
		{
			name: "already in ungrouped", channelID: "ch-loose", target: "",
			wantText:    "Loose channel is already in Ungrouped.",
			wantPrimary: "Add", disabled: true,
		},
		{
			name: "moves out of ungrouped", channelID: "ch-loose", target: "group-fun",
			wantText:    "This moves Loose channel from Ungrouped to Fun.",
			wantPrimary: "Move",
		},
		{
			name: "moves into new group with custom name", channelID: "ch-fun", target: "group-fun",
			newGroup: true, newName: "Chess",
			wantText:    "This moves Anna Cramling from Fun to Chess.",
			wantPrimary: "Move",
		},
		{
			name: "moves into new group with default name", channelID: "ch-fun", target: "group-fun",
			newGroup: true,
			wantText:    "This moves Anna Cramling from Fun to New Group.",
			wantPrimary: "Move",
		},
	}
	for _, tt := range tests {
		_, w := addDialogTestApp(t)
		frame := addDialogTester(t, w, 1280, 808)
		w.addDialog.state = addDialogResolved
		w.addDialog.channel = core.Channel{ID: tt.channelID, Title: channelTitleOf(w, tt.channelID)}
		w.addDialog.groupID = tt.target
		w.addDialog.newGroupMode = tt.newGroup
		w.addDialog.newGroupName = tt.newName
		frame.Frame()
		if !frame.HasText(tt.wantText) {
			t.Fatalf("%s: %q is not on screen", tt.name, tt.wantText)
		}
		primary, ok := frame.AccessByRole(ui.RoleButton, tt.wantPrimary)
		if !ok {
			t.Fatalf("%s: no %q button in the frame", tt.name, tt.wantPrimary)
		}
		if primary.Disabled != tt.disabled {
			t.Fatalf("%s: %q disabled = %v; want %v", tt.name, tt.wantPrimary, primary.Disabled, tt.disabled)
		}
		if !frame.HasText("Already in your library") {
			t.Fatalf("%s: the card does not say the channel is already in the library", tt.name)
		}
	}
}

func TestAddDialogNewGroupMoveSubmission(t *testing.T) {
	a, w := addDialogTestApp(t)
	tt := addDialogTester(t, w, 1280, 808)
	w.addDialog.state = addDialogResolved
	w.addDialog.channel = core.Channel{ID: "ch-fun", Title: "Anna Cramling"}
	w.addDialog.groupID = "group-fun"
	w.addDialog.newGroupMode = true
	w.addDialog.newGroupName = "Chess"
	tt.Frame()
	if err := tt.Click("Move"); err != nil {
		t.Fatalf("click Move: %v", err)
	}
	tt.Frame()
	if w.addDialog.open {
		t.Fatal("the dialog stayed open after moving into a new group")
	}
	var newGroupID string
	for id, group := range a.Sync.Groups {
		if group.Name == "Chess" {
			newGroupID = id
			break
		}
	}
	if newGroupID == "" {
		t.Fatal("the new group Chess was not created")
	}
	if containsString(a.Sync.Groups["group-fun"].ChannelIDs, "ch-fun") {
		t.Fatal("ch-fun was not removed from group-fun")
	}
	if !containsString(a.Sync.Groups[newGroupID].ChannelIDs, "ch-fun") {
		t.Fatal("ch-fun was not added to the new group")
	}
}

// channelTitleOf names a channel the test app holds.
func channelTitleOf(w *Window, id string) string {
	for _, channel := range w.app.Sync.Channels {
		if channel.ID == id {
			return channel.Title
		}
	}
	return id
}

func TestAddDialogStateCopyContract(t *testing.T) {
	copy := addDialogStateCopy()
	for _, locale := range []string{"en", "zh-Hans"} {
		menu := sidebarMenuCopy(locale)
		copy = append(copy, menu.newGroup, menu.addSubscription, menu.groupSaveFailed)
	}
	for _, text := range copy {
		if strings.TrimSpace(text) == "" {
			t.Fatal("the dialog's copy table holds an empty string")
		}
		if strings.Contains(text, "—") {
			t.Fatalf("dialog copy uses an em dash: %q", text)
		}
		if strings.Contains(text, "%") {
			t.Fatalf("dialog copy shows a percentage: %q", text)
		}
		if strings.Contains(text, "!") {
			t.Fatalf("dialog copy shouts: %q", text)
		}
		switch text {
		case "Error", "Failed", "error", "failed":
			t.Fatalf("dialog copy only names the failure: %q", text)
		}
	}
}

func TestAddDialogResolvedAndNotFoundStates(t *testing.T) {
	words := addDialogCopyIn("en")
	_, w := addDialogTestApp(t)
	tt := addDialogTester(t, w, 1280, 808)

	w.addDialog.state = addDialogResolved
	w.addDialog.channel = core.Channel{ID: "UCabcdefghijklmnopqrstuv", Title: "Anna Cramling"}
	w.addDialog.ref = core.ChannelRef{Kind: core.ChannelRefHandle, Value: "AnnaCramling"}
	tt.Frame()
	panel, _ := tt.AccessByRole(ui.RoleDialog, words.title)
	if panel.Bounds.H != 306 {
		t.Fatalf("resolved panel height = %v pt; want 306", panel.Bounds.H)
	}
	if !tt.HasText("Anna Cramling") || !tt.HasText("@AnnaCramling · UC…stuv") {
		t.Fatal("the resolved channel card is not on screen with its handle")
	}
	if !tt.HasText(words.notifyOff) {
		t.Fatal("the notifications-are-off line is not on screen")
	}

	w.addDialog.state = addDialogNotFound
	w.addDialog.cause = addCauseNotFound
	w.addDialog.channel = core.Channel{}
	tt.Frame()
	panel, _ = tt.AccessByRole(ui.RoleDialog, words.title)
	if panel.Bounds.H != 286 {
		t.Fatalf("not-found panel height = %v pt; want 286", panel.Bounds.H)
	}
	if !tt.HasText(words.errNotFound) || !tt.HasText(words.errNotFoundHint) {
		t.Fatal("the not-found copy is not on screen")
	}
}

func TestAddDialogAddCreatesTheSubscriptionInTheChosenGroup(t *testing.T) {
	a, w := addDialogTestApp(t)
	var fetched []string
	w.onAddedChannel = func(channel core.Channel) { fetched = append(fetched, channel.ID) }
	tt := addDialogTester(t, w, 1280, 808)
	w.addDialog.state = addDialogResolved
	w.addDialog.channel = core.Channel{ID: "ch-new", Title: "Newcomer"}
	w.addDialog.groupID = "group-fun"
	tt.Frame()
	if err := tt.Click("Add"); err != nil {
		t.Fatalf("click Add: %v", err)
	}
	tt.Frame()

	if _, ok := a.Sync.Channels["ch-new"]; !ok {
		t.Fatal("the channel was not subscribed")
	}
	if !containsString(a.Sync.Groups["group-fun"].ChannelIDs, "ch-new") {
		t.Fatalf("the channel is not in the chosen group: %v", a.Sync.Groups["group-fun"].ChannelIDs)
	}
	if w.addDialog.open {
		t.Fatal("the dialog stayed open after a successful add")
	}
	found := false
	for _, channel := range a.Channels {
		if channel.ID == "ch-new" {
			found = true
		}
	}
	if !found {
		t.Fatal("the added channel is not in the library the sidebar reads")
	}
	if len(fetched) != 1 || fetched[0] != "ch-new" {
		t.Fatalf("the add fetched %q; want only the added channel", fetched)
	}
}

func TestAddDialogNewGroupRollsBackWhenTheAddFails(t *testing.T) {
	a, w := addDialogTestApp(t)
	tt := addDialogTester(t, w, 1280, 808)
	w.addDialog.state = addDialogResolved
	// A channel without a title is the add the app refuses, which is the
	// failure the rollback exists for.
	w.addDialog.channel = core.Channel{ID: "ch-nameless"}
	w.addDialog.newGroupMode = true
	w.addDialog.newGroupName = "Fresh"
	tt.Frame()
	w.submitAddDialog(nil)
	tt.Frame()
	for _, group := range a.Sync.Groups {
		if group.Name == "Fresh" {
			t.Fatal("the group the failed add created was not rolled back")
		}
	}
	if len(a.Sync.Groups) != 2 {
		t.Fatalf("the failed add left %d groups; want the 2 it started with", len(a.Sync.Groups))
	}
}

func TestAddDialogNewGroupFieldAndDuplicateHint(t *testing.T) {
	_, w := addDialogTestApp(t)
	tt := addDialogTester(t, w, 1280, 808)
	w.addDialog.state = addDialogResolved
	w.addDialog.channel = core.Channel{ID: "ch-new", Title: "Newcomer"}
	w.addDialog.groupID = "group-fun"
	w.addDialog.newGroupMode = true
	w.addDialog.newGroupName = "Fun"
	tt.Frame()
	panel, _ := tt.AccessByRole(ui.RoleDialog, addDialogCopyIn("en").title)
	// The plan adds the channel (it is not in the library), and the group name
	// duplicates an existing group, which adds its own hint line.
	want := float32(306) + addDialogExtrasHeight(false, true) + addDialogHelperLine + addDialogGapTight
	if panel.Bounds.H != want {
		t.Fatalf("new-group panel height = %v pt; want %v", panel.Bounds.H, want)
	}
	if !tt.HasText("A group named Fun already exists.") {
		t.Fatal("the duplicate group name hint is not on screen")
	}
	if _, ok := tt.AccessByRole(ui.RoleTextField, "Group name"); !ok {
		t.Fatal("the inline group name field is not in the frame")
	}
}

func TestAddDialogIsKeyboardReachable(t *testing.T) {
	_, w := addDialogTestApp(t)
	tt := addDialogTester(t, w, 1280, 808)
	if !tt.Focused("Channel URL or ID") {
		t.Fatal("the link field does not take the focus as the dialog opens")
	}
	reached := map[string]bool{}
	for i := 0; i < 6; i++ {
		for _, name := range []string{"Ungrouped", "Add to group", "Cancel", "Add"} {
			if tt.Focused(name) {
				reached[name] = true
			}
		}
		tt.Key(0, ui.KeyTab)
		tt.Frame()
	}
	if !reached["Ungrouped"] || !reached["Cancel"] || !reached["Add"] {
		t.Fatalf("Tab reached %v; want the group picker, Cancel, and Add", reached)
	}
}

func TestAddDialogPickerNewGroupRow(t *testing.T) {
	_, w := addDialogTestApp(t)
	tt := addDialogTester(t, w, 1280, 808)
	w.addDialog.groupID = "group-fun"
	w.addDialog.pickerOpen = true
	tt.Frame()

	if err := tt.Click("New Group…"); err != nil {
		t.Fatalf("click the new-group row: %v", err)
	}
	tt.Frame()
	if !w.addDialog.newGroupMode {
		t.Fatal("the list's last row did not start a new group")
	}
	if w.addDialog.groupID != "group-fun" {
		t.Fatalf("the picker value = %q; want the group it showed before", w.addDialog.groupID)
	}
	if w.addDialog.newGroupName != "" {
		t.Fatalf("the new group name = %q; want it empty with its placeholder", w.addDialog.newGroupName)
	}
	if !tt.Focused("Group name") {
		t.Fatal("the inline name field did not take the focus")
	}
	if w.addDialog.pickerOpen {
		t.Fatal("the group list stayed open")
	}

	// Choosing a group again leaves the new-group mode.
	w.addDialog.pickerOpen = true
	tt.Frame()
	if err := tt.Click("GameDev"); err != nil {
		t.Fatalf("click the group row: %v", err)
	}
	tt.Frame()
	if w.addDialog.newGroupMode {
		t.Fatal("choosing a group left the new-group field showing")
	}
	if w.addDialog.groupID != "group-dev" {
		t.Fatalf("the picker value = %q; want group-dev", w.addDialog.groupID)
	}
}

func TestAddDialogDrawsTheScrim(t *testing.T) {
	_, w := addDialogTestApp(t)
	tt := addDialogTester(t, w, 1280, 808)
	tokens := mui.NewTokens(nil, false)
	image := tt.Image()
	// A point over the sidebar, outside the panel.
	r, g, b, a := image.At(100, 700).RGBA()
	if a>>8 != 255 {
		t.Fatalf("the scrim left the window translucent: alpha %d", a>>8)
	}
	scrim := tokens.ModalScrim
	want := func(base, ink uint8, alpha float32) int {
		return int(float32(base)*(1-alpha) + float32(ink)*alpha + 0.5)
	}
	alpha := float32(scrim.A) / 255
	if got, wantR := int(r>>8), want(tokens.Sidebar.R, scrim.R, alpha); absDiff(got, wantR) > 2 {
		t.Fatalf("scrim red = %d; want %d over the sidebar", got, wantR)
	}
	if got, wantG := int(g>>8), want(tokens.Sidebar.G, scrim.G, alpha); absDiff(got, wantG) > 2 {
		t.Fatalf("scrim green = %d; want %d over the sidebar", got, wantG)
	}
	if got, wantB := int(b>>8), want(tokens.Sidebar.B, scrim.B, alpha); absDiff(got, wantB) > 2 {
		t.Fatalf("scrim blue = %d; want %d over the sidebar", got, wantB)
	}
	// The panel stays the surface the reader reads on.
	if r, _, _, _ := image.At(640, 400).RGBA(); r>>8 < 200 {
		t.Fatalf("the panel is not the surface on the scrim: red %d", r>>8)
	}
}

func TestAddDialogBackdropClosesIt(t *testing.T) {
	a, w := addDialogTestApp(t)
	tt := addDialogTester(t, w, 1280, 808)
	panel, _ := tt.AccessByRole(ui.RoleDialog, "Add subscription")

	tt.ClickAt(panel.Bounds.X+panel.Bounds.W/2, panel.Bounds.Y+10)
	tt.Frame()
	if !w.addDialog.open {
		t.Fatal("a click inside the panel closed it")
	}

	// A press over the sidebar lands on the scrim: it closes the dialog, and
	// the row under the scrim is never selected.
	before := a.Selected
	tt.ClickAt(60, panel.Bounds.Y+panel.Bounds.H+40)
	tt.Frame()
	if w.addDialog.open {
		t.Fatal("a click on the scrim left the dialog open")
	}
	if a.Selected != before {
		t.Fatalf("the press went through the scrim and selected %q", a.Selected)
	}
}

func TestAddDialogEditingWhileResolvingStartsOver(t *testing.T) {
	_, w := addDialogTestApp(t)
	tt := addDialogTester(t, w, 1280, 808)
	w.addDialog.input = "youtube.com/@AnnaCramling"
	tt.Frame()
	w.addDialog.state = addDialogResolving
	w.addDialog.seq++
	seq := w.addDialog.seq
	tt.Frame()

	tt.Type("x")
	tt.Frame()
	if w.addDialog.state != addDialogInitial {
		t.Fatalf("editing during the lookup left state %v; want the initial state", w.addDialog.state)
	}
	w.applyAddResolve(seq, core.Channel{ID: "ch-new", Title: "Anna Cramling"}, nil)
	tt.Frame()
	if w.addDialog.channel.ID != "" || w.addDialog.state != addDialogInitial {
		t.Fatalf("the abandoned lookup still landed: state %v channel %q", w.addDialog.state, w.addDialog.channel.ID)
	}
}
