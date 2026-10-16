package tray

import "fyne.io/systray"

func hostAvailable() bool { return true }

func startNative(ready func()) (func(), error) {
	start, stop := systray.RunWithExternalLoop(ready, nil)
	start()
	return stop, nil
}
