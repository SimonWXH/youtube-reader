package main

import (
	"context"
	"testing"
	"time"

	"github.com/simon/youtube-reader/core"
)

// TestHelperHandoffMakesTheAnnouncedVideoClickable drives the app side of the
// helper's hand-off: the refresh the helper committed to the shared store
// while this app was running is read, so a click on its notification opens the
// channel already showing the videos it announced.
func TestHelperHandoffMakesTheAnnouncedVideoClickable(t *testing.T) {
	dir := t.TempDir()
	a := newTestApp(t, dir)
	defer a.Close()
	a.Selected = "all"

	channel := core.Channel{ID: "ch-1", Title: "Channel"}
	video := core.Video{
		ID: "v-new", ChannelID: "ch-1", ChannelTitle: "Channel", Title: "Just announced",
		PublishedAt: core.FormatRFC3339(time.Now().Add(-time.Hour)), Duration: "PT10M0S",
	}
	if err := a.Store.CommitRefresh(context.Background(), core.RefreshCommit{
		Channels: []core.Channel{channel},
		Videos:   []core.Video{video},
	}); err != nil {
		t.Fatal(err)
	}
	if got := len(a.Videos); got != 0 {
		t.Fatalf("the app already shows %d videos before the hand-off", got)
	}

	w := &Window{app: a}
	w.reloadHelperRefresh()
	if got := len(a.Channels); got != 1 {
		t.Fatalf("channels after hand-off = %d, want 1", got)
	}

	notificationClickHandler(a)(core.ChannelNotificationID("ch-1"))
	if a.Selected != "channel:ch-1" {
		t.Fatalf("click routed to %q", a.Selected)
	}
	visible := a.VisibleVideos()
	if len(visible) != 1 || visible[0].ID != "v-new" {
		t.Fatalf("the announced video is not in the clicked channel's view: %+v", visible)
	}
}
