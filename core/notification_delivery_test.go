package core

import (
	"context"
	"net"
	"os"
	"testing"
	"time"
)

// notificationTestDir is a data directory whose socket path fits the platform's
// Unix socket limit; a test runner's temporary directory can be far longer than
// a real app's data directory.
func notificationTestDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "ytreader")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// notificationPostRecorder collects the plans a serving app was asked to show.
func notificationBatchRecorder() (func([]NotificationPlan), <-chan []NotificationPlan) {
	shown := make(chan []NotificationPlan, 8)
	return func(plans []NotificationPlan) { shown <- plans }, shown
}

// TestDeliverNotificationsReachesTheServingApp proves the helper's hand-off
// path: the plans a background refresh produced reach the running app, which
// owns the notification and its click routing.
func TestDeliverNotificationsReachesTheServingApp(t *testing.T) {
	dir := notificationTestDir(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	show, shown := notificationBatchRecorder()
	served := make(chan error, 1)
	go func() { served <- ServeNotifications(ctx, dir, show) }()

	plans := []NotificationPlan{
		{ChannelID: "ch-1", ChannelName: "One", Count: 2, NewestTitle: "New"},
		{ChannelID: "ch-2", ChannelName: "Two", Count: 1},
	}
	if err := waitForNotificationSocket(dir); err != nil {
		t.Fatal(err)
	}
	if err := DeliverNotifications(dir, plans); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-shown:
		if len(got) != len(plans) || got[0] != plans[0] || got[1] != plans[1] {
			t.Fatalf("shown = %+v, want %+v", got, plans)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("plans %+v were never shown", plans)
	}
	cancel()
	if err := <-served; err != nil {
		t.Fatalf("serve: %v", err)
	}
}

// TestDeliverNotificationsWithoutListeningApp proves a closed app is reported
// as an error rather than waited for: the helper then posts plainly.
func TestDeliverNotificationsWithoutListeningApp(t *testing.T) {
	dir := notificationTestDir(t)
	if err := DeliverNotifications(dir, []NotificationPlan{{ChannelID: "ch-1"}}); err == nil {
		t.Fatal("delivery without a listening app reported success")
	}
	if _, err := os.Stat(NotificationSocketPath(dir)); !os.IsNotExist(err) {
		t.Fatalf("delivery created a socket file: %v", err)
	}
	if err := DeliverNotifications(dir, nil); err != nil {
		t.Fatalf("empty delivery: %v", err)
	}
}

// TestServeNotificationsReplacesStaleSocket proves an app that crashed before
// closing its socket does not lock out the next launch.
func TestServeNotificationsReplacesStaleSocket(t *testing.T) {
	dir := notificationTestDir(t)
	listener, err := net.Listen("unix", NotificationSocketPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	listener.(*net.UnixListener).SetUnlinkOnClose(false)
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(NotificationSocketPath(dir)); err != nil {
		t.Fatalf("stale socket missing: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	show, shown := notificationBatchRecorder()
	served := make(chan error, 1)
	go func() { served <- ServeNotifications(ctx, dir, show) }()
	if err := waitForNotificationSocket(dir); err != nil {
		t.Fatal(err)
	}
	if err := DeliverNotifications(dir, []NotificationPlan{{ChannelID: "ch-1", Count: 1}}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-shown:
		if len(got) != 1 || got[0].ChannelID != "ch-1" {
			t.Fatalf("shown = %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stale socket took over the listener")
	}
	cancel()
	<-served
	if _, err := os.Stat(NotificationSocketPath(dir)); !os.IsNotExist(err) {
		t.Fatalf("socket outlived the listener: %v", err)
	}
}

// TestServeNotificationsRefusesLiveSocket proves a second listener on the same
// path is refused instead of stealing the running app's deliveries.
func TestServeNotificationsRefusesLiveSocket(t *testing.T) {
	dir := notificationTestDir(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	show, _ := notificationBatchRecorder()
	go func() { _ = ServeNotifications(ctx, dir, show) }()
	if err := waitForNotificationSocket(dir); err != nil {
		t.Fatal(err)
	}
	if err := ServeNotifications(ctx, dir, show); err == nil {
		t.Fatal("second listener took the live socket")
	}
}

// waitForNotificationSocket waits until a serving app answers on the socket,
// since the listener starts on its own goroutine and a stale socket file from
// a crashed app answers nothing.
func waitForNotificationSocket(dir string) error {
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := net.DialTimeout("unix", NotificationSocketPath(dir), 50*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return os.ErrDeadlineExceeded
		}
		time.Sleep(5 * time.Millisecond)
	}
}
