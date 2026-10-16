package tray

import (
	"errors"
	"fyne.io/systray"
	"github.com/godbus/dbus/v5"
)

// Do not hide the only window on a desktop that cannot display a tray icon.
func hostAvailable() bool {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return false
	}
	defer conn.Close()
	value, err := conn.Object("org.kde.StatusNotifierWatcher", "/StatusNotifierWatcher").GetProperty("org.kde.StatusNotifierWatcher.IsStatusNotifierHostRegistered")
	if err != nil {
		return false
	}
	available, _ := value.Value().(bool)
	return available
}

func startNative(ready func()) (func(), error) {
	if !hostAvailable() {
		return nil, errors.New("桌面未提供可用的 StatusNotifier 托盘服务，保留关闭窗口退出行为")
	}
	start, stop := systray.RunWithExternalLoop(ready, nil)
	start()
	return stop, nil
}
