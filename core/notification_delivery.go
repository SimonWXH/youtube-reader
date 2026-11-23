package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"
)

// notificationSocketName is the local socket a running app listens on for the
// notifications the background helper must display itself. A notification only
// macOS attribute means, and only lets a click route back to, the process that
// posted it, so the helper hands its plans to the app instead of posting them
// through osascript, which Notification Center attributes to Script Editor.
const notificationSocketName = "notifications.sock"

// notificationDeliveryTimeout bounds the local socket handshake. Both ends run
// on this Mac, so a background refresh never waits on a notification: the
// helper only has to hand the plans over, not to see them displayed.
const notificationDeliveryTimeout = 2 * time.Second

// maxNotificationSocketPath is the longest usable socket path. macOS keeps a
// Unix socket path in a 104-byte sun_path; a longer data directory falls back
// to AppleScript notifications instead of failing the app's socket.
const maxNotificationSocketPath = 104

// maxNotificationDeliveryBytes caps one delivery read off the socket: a plan
// per notified channel is a few hundred bytes at most.
const maxNotificationDeliveryBytes = 1 << 20

// NotificationDelivery is one batch of notification plans the helper hands the
// running app. The app owns the display and its click routing from then on.
type NotificationDelivery struct {
	Plans []NotificationPlan `json:"plans"`
}

// NotificationSocketPath is where the running app listens for the helper's
// notification plans and where the helper hands them over.
func NotificationSocketPath(dir string) string {
	return filepath.Join(dir, notificationSocketName)
}

// DeliverNotifications hands plans to the running app, which shows them as
// YouTube Reader and routes a click through its own notification IDs. It
// reports an error when no app is listening, and never launches the app: a
// closed app keeps the plain AppleScript path so nothing pulls its window up.
func DeliverNotifications(dir string, plans []NotificationPlan) error {
	if len(plans) == 0 {
		return nil
	}
	data, err := json.Marshal(NotificationDelivery{Plans: plans})
	if err != nil {
		return err
	}
	conn, err := net.DialTimeout("unix", NotificationSocketPath(dir), notificationDeliveryTimeout)
	if err != nil {
		return fmt.Errorf("notification socket: %w", err)
	}
	// Closing the connection ends the delivery: the app reads everything
	// written and shows it, and the helper leaves without waiting.
	defer conn.Close()
	if err := conn.SetWriteDeadline(time.Now().Add(notificationDeliveryTimeout)); err != nil {
		return err
	}
	if _, err := conn.Write(data); err != nil {
		return fmt.Errorf("notification socket: %w", err)
	}
	return nil
}

// ServeNotifications listens on the local notification socket and shows every
// delivery the app receives through show, one call per hand-off, until ctx
// ends. The listener is removed with the socket when it closes.
func ServeNotifications(ctx context.Context, dir string, show func([]NotificationPlan)) error {
	path := NotificationSocketPath(dir)
	if len(path) > maxNotificationSocketPath {
		return fmt.Errorf("notification socket path too long: %q", path)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	listener, err := listenNotificationSocket(path)
	if err != nil {
		return err
	}
	defer listener.Close()
	go func() {
		<-ctx.Done()
		listener.Close()
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		showNotificationDelivery(conn, show)
	}
}

// listenNotificationSocket takes the notification socket, replacing a socket
// file an app that crashed left behind. A socket another instance answers on is
// refused instead of taken over.
func listenNotificationSocket(path string) (net.Listener, error) {
	listener, err := net.Listen("unix", path)
	if err == nil {
		return takeNotificationSocket(listener, path)
	}
	if conn, dialErr := net.DialTimeout("unix", path, notificationDeliveryTimeout); dialErr == nil {
		conn.Close()
		return nil, fmt.Errorf("notification socket in use: %q", path)
	}
	if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
		return nil, err
	}
	listener, err = net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	return takeNotificationSocket(listener, path)
}

// takeNotificationSocket keeps the socket private to this user, like the data
// directory it lives in.
func takeNotificationSocket(listener net.Listener, path string) (net.Listener, error) {
	if err := os.Chmod(path, 0o600); err != nil && !errors.Is(err, os.ErrNotExist) {
		listener.Close()
		return nil, err
	}
	return listener, nil
}

// showNotificationDelivery reads one delivery off an accepted connection and
// passes its plans on in order. A connection that carries nothing, or
// something unreadable, shows nothing: it is a helper that quit while handing
// over. Showing a plan is the app's business, including a refusal it records.
func showNotificationDelivery(conn net.Conn, show func([]NotificationPlan)) {
	defer conn.Close()
	if err := conn.SetReadDeadline(time.Now().Add(notificationDeliveryTimeout)); err != nil {
		return
	}
	data, err := io.ReadAll(io.LimitReader(conn, maxNotificationDeliveryBytes))
	if err != nil || len(data) == 0 {
		return
	}
	var delivery NotificationDelivery
	if err := json.Unmarshal(data, &delivery); err != nil {
		return
	}
	plans := make([]NotificationPlan, 0, len(delivery.Plans))
	for _, plan := range delivery.Plans {
		if plan.ChannelID != "" {
			plans = append(plans, plan)
		}
	}
	if len(plans) > 0 {
		show(plans)
	}
}
