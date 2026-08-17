package copilot

import (
	"context"
	"log/slog"

	"github.com/godbus/dbus/v5"
)

// Notifier sends a desktop notification. Implementations must be safe to call
// from the fetcher goroutine.
type Notifier interface {
	// Notify shows a notification with the given title and body.
	Notify(ctx context.Context, title, body string) error
	// Close releases any resources held by the notifier (e.g. a D-Bus conn).
	Close() error
}

// FreedesktopNotifier sends notifications over the freedesktop D-Bus
// Notifications interface (org.freedesktop.Notifications), the standard on
// GNOME, KDE, and most Linux desktops. It needs no external binary.
type FreedesktopNotifier struct {
	conn *dbus.Conn
	app  string
}

// NewFreedesktopNotifier connects to the session bus and returns a notifier.
// The caller must Close it on shutdown.
func NewFreedesktopNotifier(app string) (*FreedesktopNotifier, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, err
	}
	return &FreedesktopNotifier{conn: conn, app: app}, nil
}

// Notify implements Notifier. A replaces_id of 0 lets the server create a new
// notification; a timeout of -1 lets the server pick its default expiry.
func (n *FreedesktopNotifier) Notify(ctx context.Context, title, body string) error {
	obj := n.conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications")
	var id uint32
	err := obj.CallWithContext(ctx, "org.freedesktop.Notifications.Notify", 0,
		n.app, uint32(0), "", title, body, []string{}, map[string]dbus.Variant{}, int32(-1),
	).Store(&id)
	return err
}

// Close implements Notifier.
func (n *FreedesktopNotifier) Close() error {
	if n.conn != nil {
		return n.conn.Close()
	}
	return nil
}

// LogNotifier is a Notifier that only logs the alert. It is the fallback when
// the D-Bus session bus is unavailable, and the stub used in tests.
type LogNotifier struct{}

// Notify implements Notifier.
func (LogNotifier) Notify(ctx context.Context, title, body string) error {
	slog.Info("copilot credit alert", "title", title, "body", body)
	return nil
}

// Close implements Notifier.
func (LogNotifier) Close() error { return nil }
