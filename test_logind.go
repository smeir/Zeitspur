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

	obj := sysConn.Object("org.freedesktop.login1", "/org/freedesktop/login1")
	var sessionPath dbus.ObjectPath
	err = obj.Call("org.freedesktop.login1.Manager.GetSessionByPID", 0, uint32(os.Getpid())).Store(&sessionPath)
	if err != nil {
		fmt.Println("Error GetSessionByPID:", err)
		os.Exit(1)
	}

	sessionObj := sysConn.Object("org.freedesktop.login1", sessionPath)
	v, err := sessionObj.GetProperty("org.freedesktop.login1.Session.LockedHint")
	if err != nil {
		fmt.Println("Error GetProperty:", err)
		os.Exit(1)
	}

	fmt.Println("LockedHint:", v.Value().(bool))
}
