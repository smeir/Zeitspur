package activity

import (
	"context"
	"errors"
	"os"

	"github.com/godbus/dbus/v5"
)

type logindChecker struct {
	sysConn     *dbus.Conn
	sessionPath dbus.ObjectPath
}

func newLogindChecker() (*logindChecker, error) {
	sysConn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, err
	}

	obj := sysConn.Object("org.freedesktop.login1", "/org/freedesktop/login1")
	var sessionPath dbus.ObjectPath
	err = obj.Call("org.freedesktop.login1.Manager.GetSessionByPID", 0, uint32(os.Getpid())).Store(&sessionPath)
	if err != nil {
		// Fallback to finding the first session for the current user
		var sessions [][]interface{}
		if err := obj.Call("org.freedesktop.login1.Manager.ListSessions", 0).Store(&sessions); err == nil {
			uid := uint32(os.Getuid())
			for _, s := range sessions {
				if len(s) >= 5 && s[1].(uint32) == uid {
					sessionPath = s[4].(dbus.ObjectPath)
					break
				}
			}
		}
	}

	if sessionPath == "" {
		sysConn.Close()
		return nil, errors.New("no logind session found")
	}

	return &logindChecker{
		sysConn:     sysConn,
		sessionPath: sessionPath,
	}, nil
}

func (c *logindChecker) isLocked(ctx context.Context) (bool, error) {
	if c == nil || c.sysConn == nil {
		return false, errors.New("closed")
	}

	sessionObj := c.sysConn.Object("org.freedesktop.login1", c.sessionPath)
	v, err := sessionObj.GetProperty("org.freedesktop.login1.Session.LockedHint")
	if err != nil {
		return false, err
	}

	if locked, ok := v.Value().(bool); ok {
		return locked, nil
	}
	return false, nil
}

func (c *logindChecker) Close() error {
	if c != nil && c.sysConn != nil {
		return c.sysConn.Close()
	}
	return nil
}

// sleepWatcher subscribes to logind's PrepareForSleep signal so the engine
// learns about suspend/resume the moment they happen, instead of only
// inferring them after the fact from a gap between poll ticks. It is a
// best-effort addition: if the system bus or logind is unavailable,
// newSleepWatcher returns an error and callers fall back to the polling-based
// gap detection in Engine.tick, which still catches missed or undelivered
// signals.
type sleepWatcher struct {
	conn *dbus.Conn
	c    chan bool
}

// newSleepWatcher connects to the system bus and subscribes to
// org.freedesktop.login1.Manager.PrepareForSleep. The signal fires with
// argument true shortly before the system suspends and false right after it
// resumes.
func newSleepWatcher() (*sleepWatcher, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, err
	}

	err = conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.login1.Manager"),
		dbus.WithMatchMember("PrepareForSleep"),
	)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}

	// Buffered so a burst (e.g. a quick sleep/wake or a duplicate signal)
	// never blocks the D-Bus dispatch goroutine below.
	signals := make(chan *dbus.Signal, 8)
	conn.Signal(signals)

	w := &sleepWatcher{conn: conn, c: make(chan bool, 8)}
	go w.forward(signals)
	return w, nil
}

// forward decodes PrepareForSleep signals and republishes them on w.c. Godbus
// closes the signals channel when conn is closed, which ends this goroutine.
func (w *sleepWatcher) forward(signals chan *dbus.Signal) {
	for sig := range signals {
		if sig.Name != "org.freedesktop.login1.Manager.PrepareForSleep" || len(sig.Body) == 0 {
			continue
		}
		aboutToSleep, ok := sig.Body[0].(bool)
		if !ok {
			continue
		}
		select {
		case w.c <- aboutToSleep:
		default:
			// Nobody is reading anymore (engine shutting down); drop rather
			// than block forever.
		}
	}
}

// Events returns true shortly before the system suspends and false right
// after it resumes.
func (w *sleepWatcher) Events() <-chan bool { return w.c }

// Close stops watching and releases the D-Bus connection.
func (w *sleepWatcher) Close() error {
	if w == nil || w.conn == nil {
		return nil
	}
	return w.conn.Close()
}
