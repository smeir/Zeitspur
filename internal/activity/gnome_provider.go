package activity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/godbus/dbus/v5"
)

// dbusProbeTimeout bounds the construction-time capability probe so startup
// cannot hang on an unresponsive D-Bus service.
const dbusProbeTimeout = 2 * time.Second

// GNOMEProvider queries GNOME/Mutter over D-Bus for idle and lock state.
type GNOMEProvider struct {
	conn          *dbus.Conn
	idleThreshold time.Duration
}

// NewGNOMEProvider connects to the session bus and returns a provider.
// idleThreshold is the duration after which the user is considered idle.
//
// Connecting to the session bus succeeds on virtually every desktop, so it is
// not a usable signal on its own. We probe the GNOME/Mutter idle interface and
// return an error when it is absent, letting callers fall back to another
// provider (e.g. on KDE).
func NewGNOMEProvider(idleThreshold time.Duration) (*GNOMEProvider, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("connect session bus: %w", err)
	}
	p := &GNOMEProvider{conn: conn, idleThreshold: idleThreshold}

	ctx, cancel := context.WithTimeout(context.Background(), dbusProbeTimeout)
	defer cancel()
	if _, err := p.idleTime(ctx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("gnome idle monitor unavailable: %w", err)
	}
	return p, nil
}

// Name returns the provider name.
func (g *GNOMEProvider) Name() string { return "gnome" }

// CurrentState returns the current activity state.
func (g *GNOMEProvider) CurrentState(ctx context.Context) (ActivityState, error) {
	if g.conn == nil {
		return ActivityUnknown, errors.New("dbus connection not available")
	}

	// Check screen lock via org.gnome.ScreenSaver or org.freedesktop.ScreenSaver.
	locked, err := g.isLocked(ctx)
	if err == nil && locked {
		return ActivityLocked, nil
	}

	// Query idle time via org.gnome.Mutter.IdleMonitor.
	idleMs, err := g.idleTime(ctx)
	if err != nil {
		return ActivityUnknown, fmt.Errorf("idle query: %w", err)
	}

	if time.Duration(idleMs)*time.Millisecond >= g.idleThreshold {
		return ActivityIdle, nil
	}
	return ActivityActive, nil
}

func (g *GNOMEProvider) isLocked(ctx context.Context) (bool, error) {
	obj := g.conn.Object("org.gnome.ScreenSaver", "/org/gnome/ScreenSaver")
	var locked bool
	err := obj.CallWithContext(ctx, "org.gnome.ScreenSaver.GetActive", 0).Store(&locked)
	if err == nil {
		return locked, nil
	}

	// Fallback to freedesktop screensaver.
	obj = g.conn.Object("org.freedesktop.ScreenSaver", "/org/freedesktop/ScreenSaver")
	err = obj.CallWithContext(ctx, "org.freedesktop.ScreenSaver.GetActive", 0).Store(&locked)
	return locked, err
}

func (g *GNOMEProvider) idleTime(ctx context.Context) (uint64, error) {
	obj := g.conn.Object("org.gnome.Mutter.IdleMonitor", "/org/gnome/Mutter/IdleMonitor/Core")
	var idleTime uint64
	err := obj.CallWithContext(ctx, "org.gnome.Mutter.IdleMonitor.GetIdletime", 0).Store(&idleTime)
	return idleTime, err
}

// Close closes the D-Bus connection.
func (g *GNOMEProvider) Close() error {
	if g.conn != nil {
		return g.conn.Close()
	}
	return nil
}
