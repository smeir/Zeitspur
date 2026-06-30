package main

import (
	"fmt"
	"os"

	"github.com/godbus/dbus/v5"
)

func main() {
    sysConn, err := dbus.ConnectSystemBus()
    if err == nil {
        obj := sysConn.Object("org.freedesktop.login1", "/org/freedesktop/login1")
        var sessionPath dbus.ObjectPath
        err = obj.Call("org.freedesktop.login1.Manager.GetSessionByPID", 0, uint32(os.Getpid())).Store(&sessionPath)
        if err != nil {
            fmt.Println("Fallback ListSessions")
            var sessions [][]interface{}
            if err := obj.Call("org.freedesktop.login1.Manager.ListSessions", 0).Store(&sessions); err == nil {
                uid := uint32(os.Getuid())
                for _, s := range sessions {
                    if s[1].(uint32) == uid {
                        sessionPath = s[4].(dbus.ObjectPath)
                        break
                    }
                }
            }
        }
        fmt.Println("Session path:", sessionPath)
    }
}
