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

// TestPendingNotificationsLifecycle verifies writing, appending, reading, and
// atomic consuming of pending notification plans.
func TestPendingNotificationsLifecycle(t *testing.T) {
	dir := t.TempDir()

	// Empty write is no-op
	if err := WritePendingNotifications(dir, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(PendingNotificationsPath(dir)); !os.IsNotExist(err) {
		t.Fatal("empty write created a handshake file")
	}

	// Plans with empty ChannelID are filtered out
	emptyPlan := []NotificationPlan{{ChannelID: "", ChannelName: "Invalid"}}
	if err := WritePendingNotifications(dir, emptyPlan); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(PendingNotificationsPath(dir)); !os.IsNotExist(err) {
		t.Fatal("empty ChannelID write created a handshake file")
	}

	// Consume empty returns nil
	consumed, err := ConsumePendingNotifications(dir)
	if err != nil {
		t.Fatal(err)
	}
	if consumed != nil {
		t.Fatalf("consume on empty = %+v, want nil", consumed)
	}

	batch1 := []NotificationPlan{
		{ChannelID: "ch-1", ChannelName: "One", Count: 2, NewestTitle: "Video 1"},
	}
	if err := WritePendingNotifications(dir, batch1); err != nil {
		t.Fatal(err)
	}

	// Check file mode 0600
	fi, err := os.Stat(PendingNotificationsPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %o, want 0600", fi.Mode().Perm())
	}

	// Read without consuming
	read, err := ReadPendingNotifications(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(read) != 1 || read[0].ChannelID != "ch-1" {
		t.Fatalf("read = %+v, want %+v", read, batch1)
	}

	// Write batch 2 appends
	batch2 := []NotificationPlan{
		{ChannelID: "ch-2", ChannelName: "Two", Count: 1, NewestTitle: "Video 2"},
	}
	if err := WritePendingNotifications(dir, batch2); err != nil {
		t.Fatal(err)
	}

	// Consume returns both batches and deletes file
	consumed, err = ConsumePendingNotifications(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(consumed) != 2 || consumed[0].ChannelID != "ch-1" || consumed[1].ChannelID != "ch-2" {
		t.Fatalf("consumed = %+v, want 2 plans", consumed)
	}

	if _, err := os.Stat(PendingNotificationsPath(dir)); !os.IsNotExist(err) {
		t.Fatal("handshake file was not removed after consume")
	}

	// Subsequent consume returns nil
	consumedAgain, err := ConsumePendingNotifications(dir)
	if err != nil {
		t.Fatal(err)
	}
	if consumedAgain != nil {
		t.Fatalf("subsequent consume = %+v, want nil", consumedAgain)
	}

	// Unmarshaled plans with empty ChannelID are filtered upon read and consume
	rawPayload := `{"plans": [{"channelId": ""}, {"channelId": "ch-valid", "channelName": "Valid", "count": 1}]}`
	if err := os.WriteFile(PendingNotificationsPath(dir), []byte(rawPayload), 0o600); err != nil {
		t.Fatal(err)
	}
	readPlans, err := ReadPendingNotifications(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(readPlans) != 1 || readPlans[0].ChannelID != "ch-valid" {
		t.Fatalf("read plans = %+v, want 1 valid plan", readPlans)
	}
	consumedPlans, err := ConsumePendingNotifications(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(consumedPlans) != 1 || consumedPlans[0].ChannelID != "ch-valid" {
		t.Fatalf("consumed plans = %+v, want 1 valid plan", consumedPlans)
	}
}
