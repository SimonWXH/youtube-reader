package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
)

func TestChannelContextMenuPresenceAndBilingualLabels(t *testing.T) {
	dir := t.TempDir()
	syncPath := filepath.Join(dir, "youtube-reader-sync.json")
	a := newTestApp(t, dir)
	defer a.Close()
	a.Settings.SyncPath = syncPath

	chGrouped := core.Channel{ID: "ch-grouped", Title: "Veritasium", AvatarURL: "https://example.com/v.jpg"}
	chUngrouped := core.Channel{ID: "ch-ungrouped", Title: "3Blue1Brown", AvatarURL: "https://example.com/3b.jpg"}
	g := core.ChannelGroup{ID: "group-tech", Name: "Tech", ChannelIDs: []string{chGrouped.ID}, UpdatedAt: 100}

	a.Sync.Groups = map[string]core.ChannelGroup{"group-tech": g}
	a.Groups = []core.ChannelGroup{g}
	a.Sync.Channels = map[string]core.Channel{
		chGrouped.ID:   chGrouped,
		chUngrouped.ID: chUngrouped,
	}
	a.Channels = []core.Channel{chGrouped, chUngrouped}
	_ = a.SetGroupCollapsed("group-tech", false)
	if err := a.SaveSync(); err != nil {
		t.Fatalf("save sync: %v", err)
	}

	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.view, 1280, 840)

	// 1. English locale: both grouped and ungrouped rows have "Unsubscribe" as the first/only menu item
	tt.SetLocale("en")
	tt.Frame()

	// Grouped channel row
	if err := tt.RightClick("Veritasium"); err != nil {
		t.Fatalf("right click Veritasium in English: %v", err)
	}
	menu := tt.Menu()
	if len(menu) != 1 || menu[0] != "Unsubscribe" {
		t.Fatalf("English grouped channel menu = %v, want [Unsubscribe]", menu)
	}

	// Ungrouped channel row
	if err := tt.RightClick("3Blue1Brown"); err != nil {
		t.Fatalf("right click 3Blue1Brown in English: %v", err)
	}
	menu = tt.Menu()
	if len(menu) != 1 || menu[0] != "Unsubscribe" {
		t.Fatalf("English ungrouped channel menu = %v, want [Unsubscribe]", menu)
	}

	// 2. Chinese locale: both rows have "取消订阅" as the menu item
	tt.SetLocale("zh-Hans")
	tt.Frame()

	if err := tt.RightClick("Veritasium"); err != nil {
		t.Fatalf("right click Veritasium in Chinese: %v", err)
	}
	menu = tt.Menu()
	if len(menu) != 1 || menu[0] != "取消订阅" {
		t.Fatalf("Chinese grouped channel menu = %v, want [取消订阅]", menu)
	}

	if err := tt.RightClick("3Blue1Brown"); err != nil {
		t.Fatalf("right click 3Blue1Brown in Chinese: %v", err)
	}
	menu = tt.Menu()
	if len(menu) != 1 || menu[0] != "取消订阅" {
		t.Fatalf("Chinese ungrouped channel menu = %v, want [取消订阅]", menu)
	}
}

func TestChannelUnsubscribeDialogWordingAndOptions(t *testing.T) {
	origDialog := dialogMessage
	defer func() { dialogMessage = origDialog }()

	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()

	ch := core.Channel{ID: "ch-science", Title: "Kurzgesagt"}
	a.Sync.Channels = map[string]core.Channel{ch.ID: ch}
	a.Channels = []core.Channel{ch}

	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.view, 1280, 840)

	var lastOpts mygo.MessageOptions

	// English check
	tt.SetLocale("en")
	tt.Frame()
	dialogMessage = func(opts mygo.MessageOptions) (mygo.MessageResult, error) {
		lastOpts = opts
		return mygo.MessageResult{Button: 0}, nil // cancel
	}

	if err := tt.RightClick("Kurzgesagt"); err != nil {
		t.Fatalf("right click Kurzgesagt: %v", err)
	}
	if err := tt.ChooseMenuItem("Unsubscribe"); err != nil {
		t.Fatalf("choose Unsubscribe: %v", err)
	}

	if lastOpts.Title != "Unsubscribe from Kurzgesagt?" {
		t.Fatalf("English title = %q, want 'Unsubscribe from Kurzgesagt?'", lastOpts.Title)
	}
	if lastOpts.Message != "Unsubscribe from Kurzgesagt?" {
		t.Fatalf("English message = %q, want 'Unsubscribe from Kurzgesagt?'", lastOpts.Message)
	}
	if lastOpts.Detail != "This channel and its un-favorited videos will be removed from your library." {
		t.Fatalf("English detail = %q", lastOpts.Detail)
	}
	if !slices.Equal(lastOpts.Buttons, []string{"Cancel", "Unsubscribe"}) {
		t.Fatalf("English buttons = %v, want [Cancel Unsubscribe]", lastOpts.Buttons)
	}
	if lastOpts.DefaultButton != 0 {
		t.Fatalf("DefaultButton = %d, want 0 (Cancel)", lastOpts.DefaultButton)
	}
	if lastOpts.CancelButton != 0 {
		t.Fatalf("CancelButton = %d, want 0 (Cancel)", lastOpts.CancelButton)
	}
	if !slices.Equal(lastOpts.DestructiveButtons, []int{1}) {
		t.Fatalf("DestructiveButtons = %v, want [1]", lastOpts.DestructiveButtons)
	}

	// Chinese check
	tt.SetLocale("zh-CN")
	tt.Frame()
	lastOpts = mygo.MessageOptions{}

	if err := tt.RightClick("Kurzgesagt"); err != nil {
		t.Fatalf("right click Kurzgesagt in Chinese: %v", err)
	}
	if err := tt.ChooseMenuItem("取消订阅"); err != nil {
		t.Fatalf("choose 取消订阅: %v", err)
	}

	if lastOpts.Title != "取消订阅 Kurzgesagt？" {
		t.Fatalf("Chinese title = %q, want '取消订阅 Kurzgesagt？'", lastOpts.Title)
	}
	if lastOpts.Message != "取消订阅 Kurzgesagt？" {
		t.Fatalf("Chinese message = %q, want '取消订阅 Kurzgesagt？'", lastOpts.Message)
	}
	if lastOpts.Detail != "该频道及其未收藏的视频将从本地列表中移除。" {
		t.Fatalf("Chinese detail = %q", lastOpts.Detail)
	}
	if !slices.Equal(lastOpts.Buttons, []string{"取消", "取消订阅"}) {
		t.Fatalf("Chinese buttons = %v, want [取消 取消订阅]", lastOpts.Buttons)
	}
	if lastOpts.DefaultButton != 0 {
		t.Fatalf("DefaultButton = %d, want 0 (取消)", lastOpts.DefaultButton)
	}
	if lastOpts.CancelButton != 0 {
		t.Fatalf("CancelButton = %d, want 0 (取消)", lastOpts.CancelButton)
	}
	if !slices.Equal(lastOpts.DestructiveButtons, []int{1}) {
		t.Fatalf("DestructiveButtons = %v, want [1]", lastOpts.DestructiveButtons)
	}
}

func TestChannelUnsubscribeCancelKeepsData(t *testing.T) {
	origDialog := dialogMessage
	defer func() { dialogMessage = origDialog }()

	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()

	ch := core.Channel{ID: "ch-1", Title: "Tech Channel"}
	v1 := core.Video{ID: "v-1", ChannelID: "ch-1", Title: "Video 1"}
	g := core.ChannelGroup{ID: "g-1", Name: "Tech Group", ChannelIDs: []string{"ch-1"}, UpdatedAt: 100}

	a.Sync.Channels = map[string]core.Channel{ch.ID: ch}
	a.Sync.Groups = map[string]core.ChannelGroup{g.ID: g}
	a.Settings.NotificationIDs = []string{"ch-1"}
	_ = a.SaveSync()
	_ = a.SaveSettings()

	ctx := context.Background()
	_ = a.Store.CommitRefresh(ctx, core.RefreshCommit{
		Channels: []core.Channel{ch},
		Videos:   []core.Video{v1},
		Revision: 1,
	})
	_ = a.reload()
	_ = a.SetGroupCollapsed("g-1", false)

	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.view, 1280, 840)
	tt.SetLocale("en")
	tt.Frame()

	// Simulate user clicking Cancel (button 0)
	dialogMessage = func(opts mygo.MessageOptions) (mygo.MessageResult, error) {
		return mygo.MessageResult{Button: 0}, nil
	}

	if err := tt.RightClick("Tech Channel"); err != nil {
		t.Fatalf("right click: %v", err)
	}
	if err := tt.ChooseMenuItem("Unsubscribe"); err != nil {
		t.Fatalf("choose Unsubscribe: %v", err)
	}

	// Verify nothing was changed
	if _, ok := a.Sync.Channels["ch-1"]; !ok {
		t.Fatal("Sync.Channels was modified on Cancel")
	}
	if !slices.Contains(a.Sync.Groups["g-1"].ChannelIDs, "ch-1") {
		t.Fatal("group was modified on Cancel")
	}
	if !slices.Contains(a.Settings.NotificationIDs, "ch-1") {
		t.Fatal("notification preferences modified on Cancel")
	}
	channels, err := a.Store.Channels(ctx)
	if err != nil || len(channels) != 1 {
		t.Fatalf("Store channels modified on Cancel: %v, err=%v", channels, err)
	}
	videos, err := a.Store.Videos(ctx)
	if err != nil || len(videos) != 1 {
		t.Fatalf("Store videos modified on Cancel: %v, err=%v", videos, err)
	}
}

func TestChannelUnsubscribeCascadeAndPreservesFavorites(t *testing.T) {
	origDialog := dialogMessage
	defer func() { dialogMessage = origDialog }()

	dir := t.TempDir()
	syncPath := filepath.Join(dir, "youtube-reader-sync.json")
	a := newTestApp(t, dir)
	defer a.Close()
	a.Settings.SyncPath = syncPath

	ch1 := core.Channel{ID: "ch-1", Title: "Physics Channel"}
	ch2 := core.Channel{ID: "ch-2", Title: "Math Channel"}

	vNormal1 := core.Video{ID: "v-normal-1", ChannelID: "ch-1", Title: "Quantum Physics", PublishedAt: "2026-01-01T00:00:00Z"}
	vNormal2 := core.Video{ID: "v-normal-2", ChannelID: "ch-1", Title: "Relativity", PublishedAt: "2026-01-02T00:00:00Z"}
	vFav1 := core.Video{ID: "v-fav-1", ChannelID: "ch-1", Title: "Favorite Quantum Experiment", PublishedAt: "2026-01-03T00:00:00Z"}
	vOther := core.Video{ID: "v-other", ChannelID: "ch-2", Title: "Linear Algebra", PublishedAt: "2026-01-04T00:00:00Z"}

	g1 := core.ChannelGroup{ID: "g-science", Name: "Science", ChannelIDs: []string{"ch-1", "ch-2"}, UpdatedAt: 100}

	a.Sync.Channels = map[string]core.Channel{ch1.ID: ch1, ch2.ID: ch2}
	a.Sync.Groups = map[string]core.ChannelGroup{g1.ID: g1}
	a.Settings.NotificationIDs = []string{"ch-1", "ch-2"}

	// Favorite v-fav-1
	a.Sync.SetFavorite(vFav1, true, 2000)
	if err := a.SaveSync(); err != nil {
		t.Fatalf("save initial sync: %v", err)
	}
	if err := a.SaveSettings(); err != nil {
		t.Fatalf("save initial settings: %v", err)
	}

	ctx := context.Background()
	if err := a.Store.CommitRefresh(ctx, core.RefreshCommit{
		Channels: []core.Channel{ch1, ch2},
		Videos:   []core.Video{vNormal1, vNormal2, vFav1, vOther},
		Revision: 1,
	}); err != nil {
		t.Fatalf("store commit: %v", err)
	}

	_ = a.reload()
	_ = a.SetGroupCollapsed("g-science", false)
	a.Selected = "channel:ch-1"
	a.ChannelFilter = "ch-1"
	a.SelectedVideoID = "v-normal-1"

	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.view, 1280, 840)
	tt.SetLocale("en")
	tt.Frame()

	// Simulate user clicking Unsubscribe (button 1)
	dialogMessage = func(opts mygo.MessageOptions) (mygo.MessageResult, error) {
		return mygo.MessageResult{Button: 1}, nil
	}

	if err := tt.RightClick("Physics Channel"); err != nil {
		t.Fatalf("right click Physics Channel: %v", err)
	}
	if err := tt.ChooseMenuItem("Unsubscribe"); err != nil {
		t.Fatalf("choose Unsubscribe: %v", err)
	}

	// 1. Channel removed from Sync.Channels
	if _, ok := a.Sync.Channels["ch-1"]; ok {
		t.Fatal("ch-1 still exists in Sync.Channels")
	}
	if _, ok := a.Sync.Channels["ch-2"]; !ok {
		t.Fatal("ch-2 missing from Sync.Channels")
	}

	// 2. Channel removed from group ChannelIDs and UpdatedAt updated
	gAfter := a.Sync.Groups["g-science"]
	if slices.Contains(gAfter.ChannelIDs, "ch-1") {
		t.Fatalf("group still contains ch-1: %v", gAfter.ChannelIDs)
	}
	if !slices.Contains(gAfter.ChannelIDs, "ch-2") {
		t.Fatalf("group missing ch-2: %v", gAfter.ChannelIDs)
	}
	if gAfter.UpdatedAt <= 100 {
		t.Fatalf("group UpdatedAt not bumped: got %d, want > 100", gAfter.UpdatedAt)
	}

	// 3. Removed from notification IDs
	if slices.Contains(a.Settings.NotificationIDs, "ch-1") {
		t.Fatalf("NotificationIDs still has ch-1: %v", a.Settings.NotificationIDs)
	}
	if !slices.Contains(a.Settings.NotificationIDs, "ch-2") {
		t.Fatalf("NotificationIDs missing ch-2: %v", a.Settings.NotificationIDs)
	}

	// 4. Selection degraded to "all", ChannelFilter cleared, SelectedVideoID cleared
	if a.Selected != "all" {
		t.Fatalf("Selected = %q, want 'all'", a.Selected)
	}
	if a.ChannelFilter != "" {
		t.Fatalf("ChannelFilter = %q, want empty", a.ChannelFilter)
	}
	if a.SelectedVideoID != "" {
		t.Fatalf("SelectedVideoID = %q, want empty", a.SelectedVideoID)
	}

	// 5. In SQLite Store:
	// - Channel row removed
	storeChans, err := a.Store.Channels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range storeChans {
		if c.ID == "ch-1" {
			t.Fatal("Store.Channels still contains ch-1")
		}
	}

	// - Non-favorited videos deleted, favorited video preserved!
	storeVids, err := a.Store.Videos(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var foundNormal1, foundNormal2, foundFav1, foundOther bool
	for _, v := range storeVids {
		switch v.ID {
		case "v-normal-1":
			foundNormal1 = true
		case "v-normal-2":
			foundNormal2 = true
		case "v-fav-1":
			foundFav1 = true
		case "v-other":
			foundOther = true
		}
	}
	if foundNormal1 {
		t.Fatal("v-normal-1 was not deleted from SQLite")
	}
	if foundNormal2 {
		t.Fatal("v-normal-2 was not deleted from SQLite")
	}
	if !foundFav1 {
		t.Fatal("RED LINE VIOLATION: favorited video v-fav-1 was deleted from SQLite!")
	}
	if !foundOther {
		t.Fatal("v-other of other channel was deleted from SQLite")
	}

	// 6. Memory a.Videos and VisibleVideos accessibility
	if !a.Sync.IsFavorite("v-fav-1") {
		t.Fatal("v-fav-1 missing from Sync.Favorites")
	}
	video, ok := a.VideoByID("v-fav-1")
	if !ok || video.ID != "v-fav-1" {
		t.Fatalf("VideoByID('v-fav-1') = %v, found=%v", video, ok)
	}

	// Visible in favorites
	a.Selected = "favorites"
	favVids := a.VisibleVideos()
	var favVisible bool
	for _, v := range favVids {
		if v.ID == "v-fav-1" {
			favVisible = true
		}
	}
	if !favVisible {
		t.Fatal("favorited video v-fav-1 not visible in favorites destination")
	}

	// Visible in All videos
	a.Selected = "all"
	allVids := a.VisibleVideos()
	var allVisible bool
	for _, v := range allVids {
		if v.ID == "v-fav-1" {
			allVisible = true
		}
	}
	if !allVisible {
		t.Fatal("favorited video v-fav-1 not visible in All destination")
	}

	// 7. Check persisted sync file on disk
	data, err := os.ReadFile(syncPath)
	if err != nil {
		t.Fatal(err)
	}
	var loadedSync core.SyncState
	if err := json.Unmarshal(data, &loadedSync); err != nil {
		t.Fatal(err)
	}
	if _, ok := loadedSync.Channels["ch-1"]; ok {
		t.Fatal("persisted sync file still has ch-1")
	}
	if slices.Contains(loadedSync.Groups["g-science"].ChannelIDs, "ch-1") {
		t.Fatal("persisted sync file group still has ch-1")
	}
	if _, ok := loadedSync.Favorites["v-fav-1"]; !ok {
		t.Fatal("persisted sync file missing v-fav-1 favorite")
	}
}

func TestChannelUnsubscribePreservesSelectedVideoIfFavorited(t *testing.T) {
	origDialog := dialogMessage
	defer func() { dialogMessage = origDialog }()

	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()

	ch := core.Channel{ID: "ch-fav", Title: "Fav Channel"}
	vFav := core.Video{ID: "v-fav-selected", ChannelID: "ch-fav", Title: "Saved Forever"}

	a.Sync.Channels = map[string]core.Channel{ch.ID: ch}
	a.Sync.SetFavorite(vFav, true, 5000)
	_ = a.SaveSync()

	ctx := context.Background()
	_ = a.Store.CommitRefresh(ctx, core.RefreshCommit{
		Channels: []core.Channel{ch},
		Videos:   []core.Video{vFav},
		Revision: 1,
	})
	_ = a.reload()

	a.Selected = "channel:ch-fav"
	a.SelectedVideoID = "v-fav-selected"

	dialogMessage = func(opts mygo.MessageOptions) (mygo.MessageResult, error) {
		return mygo.MessageResult{Button: 1}, nil
	}

	w := &Window{app: a, selectedIndex: new(int)}
	tt := ui.NewTester(w.view, 1280, 840)
	tt.SetLocale("en")
	tt.Frame()

	if err := tt.RightClick("Fav Channel"); err != nil {
		t.Fatalf("right click: %v", err)
	}
	if err := tt.ChooseMenuItem("Unsubscribe"); err != nil {
		t.Fatalf("choose Unsubscribe: %v", err)
	}

	// Channel destination degraded to all, but the favorited video remains selected
	if a.Selected != "all" {
		t.Fatalf("Selected = %q, want 'all'", a.Selected)
	}
	if a.SelectedVideoID != "v-fav-selected" {
		t.Fatalf("SelectedVideoID = %q, want 'v-fav-selected'", a.SelectedVideoID)
	}
}

func TestChannelUnsubscribeAndResubscribeSymmetry(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()

	ch := core.Channel{ID: "ch-sym", Title: "Symmetry Channel"}
	vFav := core.Video{ID: "v-sym-fav", ChannelID: "ch-sym", Title: "Sym Fav Video"}
	vOld := core.Video{ID: "v-sym-old", ChannelID: "ch-sym", Title: "Sym Old Video"}

	// 1. Initial subscription
	if err := a.AddSubscription(ch, ""); err != nil {
		t.Fatalf("initial AddSubscription: %v", err)
	}
	a.Sync.SetFavorite(vFav, true, 3000)
	_ = a.SaveSync()

	ctx := context.Background()
	_ = a.Store.CommitRefresh(ctx, core.RefreshCommit{
		Channels: []core.Channel{ch},
		Videos:   []core.Video{vFav, vOld},
		Revision: 1,
	})
	_ = a.reload()

	// 2. Unsubscribe
	if err := a.UnsubscribeChannel(ch.ID); err != nil {
		t.Fatalf("UnsubscribeChannel: %v", err)
	}

	if _, ok := a.Sync.Channels[ch.ID]; ok {
		t.Fatal("channel still in Sync.Channels after unsubscribe")
	}

	// 3. Re-subscribe
	if err := a.AddSubscription(ch, ""); err != nil {
		t.Fatalf("re-subscribing failed: %v", err)
	}

	if _, ok := a.Sync.Channels[ch.ID]; !ok {
		t.Fatal("channel not restored in Sync.Channels after re-subscribing")
	}

	// Favorited video is still intact
	if !a.Sync.IsFavorite(vFav.ID) {
		t.Fatal("v-sym-fav lost favorite status after re-subscribe")
	}
	video, ok := a.VideoByID(vFav.ID)
	if !ok || video.ID != vFav.ID {
		t.Fatalf("v-sym-fav not found after re-subscribe: %v, ok=%v", video, ok)
	}
}
