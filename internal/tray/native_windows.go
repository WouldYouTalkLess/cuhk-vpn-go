package tray

import (
	"fyne.io/systray"
	"runtime"
)

func hostAvailable() bool { return true }

func startNative(ready func()) (func(), error) {
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		systray.Run(ready, nil)
	}()
	return systray.Quit, nil
}
