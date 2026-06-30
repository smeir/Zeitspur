package main

import (
	"fmt"
	"os"

	"github.com/godbus/dbus/v5"
)

func main() {
	sysConn, err := dbus.ConnectSystemBus()
	if err != nil {
		fmt.Println("Error connecting to system bus:", err)
		os.Exit(1)
	}
	defer sysConn.Close()

    // try to get current user's session
    obj := sysConn.Object("org.freedesktop.login1", "/org/freedesktop/login1")
    var sessions [][]interface{}
    err = obj.Call("org.freedesktop.login1.Manager.ListSessions", 0).Store(&sessions)
    if err != nil {
        fmt.Println("Error ListSessions:", err)
        os.Exit(1)
    }
    
    uid := uint32(os.Getuid())
    for _, s := range sessions {
        sessionID := s[0].(string)
        sessionUID := s[1].(uint32)
        sessionPath := s[4].(dbus.ObjectPath)
        
        if sessionUID == uid {
            fmt.Printf("Found session %s for user %d at %s\n", sessionID, sessionUID, sessionPath)
            sessionObj := sysConn.Object("org.freedesktop.login1", sessionPath)
            v, err := sessionObj.GetProperty("org.freedesktop.login1.Session.LockedHint")
            if err == nil {
                fmt.Println("LockedHint:", v.Value().(bool))
            }
        }
    }
}
