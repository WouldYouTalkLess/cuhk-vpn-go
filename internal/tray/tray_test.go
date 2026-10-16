package tray

import "testing"

func TestTrayActionsFollowConnectionLifecycle(t *testing.T) {
	cases := []struct {
		status  Status
		action  string
		enabled bool
	}{
		{Status{}, "连接 VPN", false},
		{Status{State: "disconnected", UIReady: true, CoreReady: true}, "连接 VPN", true},
		{Status{State: "preparing", UIReady: true}, "正在准备核心…", false},
		{Status{State: "connecting", Phase: "authorize", UIReady: true}, "取消连接", true},
		{Status{State: "connected", UIReady: true}, "断开 VPN", true},
		{Status{State: "disconnecting", UIReady: true}, "重试断开", true},
		{Status{State: "connected", UIReady: true, Exiting: true}, "正在退出…", false},
	}
	for _, c := range cases {
		_, action, enabled := presentation(c.status)
		if action != c.action || enabled != c.enabled {
			t.Fatalf("%+v -> %s %v", c.status, action, enabled)
		}
	}
}
