package activity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/godbus/dbus/v5"
)

// FreedesktopProvider queries standard D-Bus screensaver/idle interfaces.
// It works with any desktop that exposes org.freedesktop.ScreenSaver,
// including KDE (org.kde.screensaver).
type FreedesktopProvider struct {
	conn          *dbus.Conn
	idleThreshold time.Duration
	name          string
	services      []string
	queryIdleTime func(context.Context) (uint32, error)
	queryLocked   func(context.Context) (bool, error)
}

// NewFreedesktopProvider connects to the session bus and returns a provider
// that tries common freedesktop-compatible screensaver services.
func NewFreedesktopProvider(idleThreshold time.Duration) (*FreedesktopProvider, error) {
	return newFreedesktopProvider(
		"freedesktop",
		[]string{
			"org.freedesktop.ScreenSaver",
			"org.kde.screensaver",
		},
		idleThreshold,
	)
}

func newFreedesktopProvider(name string, services []string, idleThreshold time.Duration) (*FreedesktopProvider, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("connect session bus: %w", err)
	}
	p := &FreedesktopProvider{
		conn:          conn,
		idleThreshold: idleThreshold,
		name:          name,
		services:      services,
	}
	p.queryIdleTime = p.defaultIdleTime
	p.queryLocked = p.defaultLocked

	// Probe the idle interface: CurrentState treats a failed idle query as a
	// hard error, so a provider whose services are absent is unusable and must
	// signal that here to let callers fall back.
	ctx, cancel := context.WithTimeout(context.Background(), dbusProbeTimeout)
	defer cancel()
	if _, err := p.queryIdleTime(ctx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%s idle service unavailable: %w", name, err)
	}
	return p, nil
}

// Name returns the provider name.
func (f *FreedesktopProvider) Name() string { return f.name }

// CurrentState returns the current activity state.
func (f *FreedesktopProvider) CurrentState(ctx context.Context) (ActivityState, error) {
	locked, err := f.queryLocked(ctx)
	if err == nil && locked {
		return ActivityLocked, nil
	}

	idleSecs, err := f.queryIdleTime(ctx)
	if err != nil {
		return ActivityUnknown, fmt.Errorf("idle query: %w", err)
	}

	// org.freedesktop.ScreenSaver.GetSessionIdleTime returns seconds (the
	// interface declares the out argument as "seconds"), unlike GNOME Mutter's
	// GetIdletime which returns milliseconds.
	if time.Duration(idleSecs)*time.Second >= f.idleThreshold {
		return ActivityIdle, nil
	}
	return ActivityActive, nil
}

func (f *FreedesktopProvider) defaultLocked(ctx context.Context) (bool, error) {
	paths := []dbus.ObjectPath{"/ScreenSaver", "/org/freedesktop/ScreenSaver"}
	for _, svc := range f.services {
		for _, p := range paths {
			obj := f.conn.Object(svc, p)
			var active bool
			err := obj.CallWithContext(ctx, "org.freedesktop.ScreenSaver.GetActive", 0).Store(&active)
			if err == nil {
				return active, nil
			}
		}
	}
	return false, errors.New("no screensaver service available")
}

// defaultIdleTime returns the session idle time in seconds.
func (f *FreedesktopProvider) defaultIdleTime(ctx context.Context) (uint32, error) {
	paths := []dbus.ObjectPath{"/ScreenSaver", "/org/freedesktop/ScreenSaver"}
	for _, svc := range f.services {
		for _, p := range paths {
			obj := f.conn.Object(svc, p)
			var idleSecs uint32
			err := obj.CallWithContext(ctx, "org.freedesktop.ScreenSaver.GetSessionIdleTime", 0).Store(&idleSecs)
			if err == nil {
				return idleSecs, nil
			}
		}
	}
	return 0, errors.New("no idle service available")
}

// Close closes the D-Bus connection.
func (f *FreedesktopProvider) Close() error {
	if f.conn != nil {
		return f.conn.Close()
	}
	return nil
}
