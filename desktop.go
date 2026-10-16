package main

import (
	"context"
	"cuhk-vpn-go/internal/tray"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"time"
)

type desktopTray interface {
	Active() bool
	CanHide() bool
	Update(tray.Status)
}

type windowActions struct {
	show, hide, quit func(context.Context)
	emit             func(context.Context, View)
}

func nativeWindowActions() windowActions {
	return windowActions{
		show: func(ctx context.Context) { wruntime.WindowShow(ctx); wruntime.WindowUnminimise(ctx) },
		hide: wruntime.WindowHide,
		quit: wruntime.Quit,
		emit: func(ctx context.Context, v View) { wruntime.EventsEmit(ctx, "vpn:update", v) },
	}
}

func (a *App) ShowWindow() {
	a.mu.Lock()
	ctx := a.ctx
	a.mu.Unlock()
	if ctx != nil {
		a.window.show(ctx)
	}
}

func (a *App) toggleFromTray() {
	v := a.snapshot()
	if v.Exiting {
		return
	}
	if v.State != "disconnected" {
		if v.State != "preparing" {
			if err := a.Disconnect(); err != nil {
				a.fail(err)
				a.ShowWindow()
			}
		}
		return
	}
	if !v.Settings.Remember || v.Settings.Username == "" {
		a.ShowWindow()
		a.log("请在主界面输入学校账号和密码后连接")
		return
	}
	if err := a.Connect(v.Settings, ""); err != nil {
		a.fail(err)
		a.ShowWindow()
	}
}

// Quit is distinct from closing the window. It never exits before the owned
// OpenConnect session has completed its existing route cleanup protocol.
func (a *App) Quit() {
	a.mu.Lock()
	if a.exitWaiting || a.ctx == nil {
		a.mu.Unlock()
		return
	}
	a.exitWaiting = true
	a.view.Exiting = true
	ctx, busy, state, done, cancel := a.ctx, a.busy, a.view.State, a.done, a.prepareCancel
	timeout := a.exitTimeout
	if timeout <= 0 {
		timeout = 35 * time.Second
	}
	a.mu.Unlock()
	a.emit()
	if !busy {
		a.window.quit(ctx)
		return
	}
	if state == "preparing" {
		if cancel != nil {
			cancel()
		}
	} else {
		if err := a.Disconnect(); err != nil {
			a.log(err.Error())
		}
	}
	go func() {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case <-done:
			a.window.quit(ctx)
		case <-timer.C:
			a.mu.Lock()
			a.exitWaiting = false
			a.view.Exiting = false
			a.mu.Unlock()
			a.log("仍在正常断开，请稍后从托盘菜单重试退出")
			a.ShowWindow()
		}
	}()
}
