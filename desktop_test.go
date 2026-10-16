package main

import (
	"context"
	"cuhk-vpn-go/internal/tray"
	"sync/atomic"
	"testing"
	"time"
)

type fakeTray struct{ active bool }

func (t fakeTray) Active() bool     { return t.active }
func (t fakeTray) CanHide() bool    { return t.active }
func (fakeTray) Update(tray.Status) {}

func TestCloseHidesConnectedSession(t *testing.T) {
	var hidden, stopped atomic.Int32
	a := &App{ctx: context.Background(), tray: fakeTray{true}, busy: true, view: View{State: "connected"}, cancel: func() { stopped.Add(1) }, window: windowActions{hide: func(context.Context) { hidden.Add(1) }}}
	if !a.beforeClose(a.ctx) {
		t.Fatal("closing with a working tray should keep the app alive")
	}
	if hidden.Load() != 1 || stopped.Load() != 0 || a.snapshot().State != "connected" {
		t.Fatal("hiding interrupted the VPN session")
	}
}

func TestCloseWithoutTrayExitsIdle(t *testing.T) {
	a := &App{ctx: context.Background(), tray: fakeTray{false}, view: View{State: "disconnected"}}
	if a.beforeClose(a.ctx) {
		t.Fatal("unavailable tray must not hide the only window")
	}
}

func TestQuitWaitsForConnectionCleanup(t *testing.T) {
	var stopped atomic.Int32
	quit := make(chan struct{}, 1)
	done := make(chan struct{})
	a := &App{ctx: context.Background(), tray: fakeTray{true}, busy: true, done: done, view: View{State: "connected"}, cancel: func() { stopped.Add(1) }, window: windowActions{quit: func(context.Context) { quit <- struct{}{} }}}
	a.Quit()
	a.Quit() // Repeated tray clicks must not schedule another exit.
	if stopped.Load() != 1 || !a.snapshot().Exiting {
		t.Fatal("graceful disconnect was not requested exactly once")
	}
	select {
	case <-quit:
		t.Fatal("exited before cleanup")
	default:
	}
	a.mu.Lock()
	a.busy = false
	a.view.State = "disconnected"
	a.mu.Unlock()
	close(done)
	select {
	case <-quit:
	case <-time.After(time.Second):
		t.Fatal("did not exit after cleanup")
	}
	if a.beforeClose(a.ctx) {
		t.Fatal("explicit exit was hidden back to the tray")
	}
	select {
	case <-quit:
		t.Fatal("duplicate exit")
	default:
	}
}

func TestQuitCancelsDownloadAndWaits(t *testing.T) {
	var cancelled atomic.Int32
	quit := make(chan struct{}, 1)
	done := make(chan struct{})
	a := &App{ctx: context.Background(), busy: true, done: done, view: View{State: "preparing"}, prepareCancel: func() { cancelled.Add(1) }, window: windowActions{quit: func(context.Context) { quit <- struct{}{} }}}
	a.Quit()
	if cancelled.Load() != 1 {
		t.Fatal("core preparation was not cancelled")
	}
	select {
	case <-quit:
		t.Fatal("exited before download cleanup")
	default:
	}
	close(done)
	select {
	case <-quit:
	case <-time.After(time.Second):
		t.Fatal("exit did not complete")
	}
}

func TestQuitTimeoutKeepsOwnedSession(t *testing.T) {
	shown := make(chan struct{}, 1)
	var quits atomic.Int32
	a := &App{ctx: context.Background(), busy: true, done: make(chan struct{}), view: View{State: "connected"}, cancel: func() {}, exitTimeout: 10 * time.Millisecond, window: windowActions{show: func(context.Context) { shown <- struct{}{} }, quit: func(context.Context) { quits.Add(1) }}}
	a.Quit()
	select {
	case <-shown:
	case <-time.After(time.Second):
		t.Fatal("timeout did not restore the window")
	}
	if quits.Load() != 0 || !a.busy || a.snapshot().Exiting {
		t.Fatal("timeout abandoned the still-owned VPN")
	}
}

func TestTrayConnectWithoutSavedPasswordShowsWindow(t *testing.T) {
	var shown atomic.Int32
	a := &App{ctx: context.Background(), view: View{State: "disconnected"}, window: windowActions{show: func(context.Context) { shown.Add(1) }}}
	a.toggleFromTray()
	if shown.Load() != 1 || a.busy {
		t.Fatal("missing credentials should open the GUI without starting a helper")
	}
}
