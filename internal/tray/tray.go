// Package tray adds a native system tray without taking over Wails' event loop.
package tray

import (
	"runtime"
	"sync"
	"sync/atomic"

	"fyne.io/systray"
)

type Status struct {
	State, Phase                string
	CoreReady, UIReady, Exiting bool
}

type Actions struct {
	Show, Toggle, Quit, Ready func()
}

type Controller struct {
	actions        Actions
	icon, template []byte
	mu             sync.Mutex
	status         Status
	updates        chan Status
	done           chan struct{}
	active         atomic.Bool
	stop           func()
	stopOnce       sync.Once
}

func New(icon, template []byte, actions Actions) *Controller {
	return &Controller{icon: icon, template: template, actions: actions, updates: make(chan Status, 1), done: make(chan struct{})}
}

// Start is called on the main OS thread, before Wails starts its native loop.
// Windows uses a dedicated locked message-pump thread; Cocoa shares Wails' loop.
func (c *Controller) Start() error {
	stop, err := startNative(c.onReady)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.stop = stop
	c.mu.Unlock()
	return nil
}

func (c *Controller) Active() bool  { return c.active.Load() }
func (c *Controller) CanHide() bool { return c.Active() && hostAvailable() }

func (c *Controller) Update(status Status) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if status == c.status {
		return
	}
	c.status = status
	select {
	case <-c.updates:
	default:
	}
	select {
	case c.updates <- status:
	default:
	}
}

func (c *Controller) Stop() {
	c.stopOnce.Do(func() {
		c.mu.Lock()
		c.active.Store(false)
		close(c.done)
		stop := c.stop
		c.mu.Unlock()
		if stop != nil {
			stop()
		}
	})
}

func (c *Controller) onReady() {
	if runtime.GOOS == "darwin" {
		systray.SetTemplateIcon(c.template, c.icon)
	} else {
		systray.SetIcon(c.icon)
	}
	systray.SetTooltip("CUHK VPN · 正在启动")
	systray.SetOnTapped(func() { go c.actions.Show() })
	statusItem := systray.AddMenuItem("状态：正在启动", "当前 VPN 状态")
	statusItem.Disable()
	showItem := systray.AddMenuItem("显示主界面", "打开 CUHK VPN")
	toggleItem := systray.AddMenuItem("连接 VPN", "连接、取消或断开")
	toggleItem.Disable()
	systray.AddSeparator()
	quitItem := systray.AddMenuItem("退出 CUHK VPN", "正常断开并清理路由后退出")
	c.mu.Lock()
	select {
	case <-c.done:
		c.mu.Unlock()
		return
	default:
		c.active.Store(true)
	}
	c.mu.Unlock()
	if c.actions.Ready != nil {
		c.actions.Ready()
	}
	c.mu.Lock()
	current := c.status
	c.mu.Unlock()
	apply := func(s Status) {
		status, action, enabled := presentation(s)
		statusItem.SetTitle("状态：" + status)
		systray.SetTooltip("CUHK VPN · " + status)
		toggleItem.SetTitle(action)
		if enabled {
			toggleItem.Enable()
		} else {
			toggleItem.Disable()
		}
		if s.Exiting {
			quitItem.SetTitle("正在退出…")
			quitItem.Disable()
		} else {
			quitItem.SetTitle("退出 CUHK VPN")
			quitItem.Enable()
		}
	}
	apply(current)
	for {
		select {
		case <-c.done:
			return
		case s := <-c.updates:
			apply(s)
		case <-showItem.ClickedCh:
			go c.actions.Show()
		case <-toggleItem.ClickedCh:
			go c.actions.Toggle()
		case <-quitItem.ClickedCh:
			go c.actions.Quit()
		}
	}
}

func presentation(s Status) (status, action string, enabled bool) {
	if s.Exiting {
		return "正在断开并退出", "正在退出…", false
	}
	if !s.UIReady {
		return "正在启动", "连接 VPN", false
	}
	switch s.State {
	case "preparing":
		return "正在准备核心", "正在准备核心…", false
	case "connecting":
		status = "正在连接"
		if s.Phase == "authorize" {
			status = "等待系统授权"
		}
		return status, "取消连接", true
	case "connected":
		return "已连接 · 分流已启用", "断开 VPN", true
	case "disconnecting":
		return "正在断开", "重试断开", true
	default:
		return "未连接", "连接 VPN", s.CoreReady
	}
}
