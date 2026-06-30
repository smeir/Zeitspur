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
