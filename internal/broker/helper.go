package broker

import (
	"context"
	"cuhk-vpn-go/internal/platform"
	"cuhk-vpn-go/internal/protocol"
	"cuhk-vpn-go/internal/vpn"
	"errors"
	"time"
)

func Helper(args []string) error {
	if len(args) != 3 || !platform.Privileged() {
		return errors.New("助手需要系统授权和有效会话参数")
	}
	w, e := Dial(args[0], args[1], args[2])
	if e != nil {
		return e
	}
	defer w.Close()
	// A dead/cancelled GUI cannot leave an idle privileged helper behind.
	incoming := make(chan protocol.Message, 8)
	closed := make(chan struct{})
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		defer close(closed)
		for {
			m, e := w.Receive()
			if e != nil {
				return
			}
			select {
			case incoming <- m:
			case <-finished:
				return
			}
		}
	}()
	var session *vpn.Session
	emit := func(m protocol.Message) { w.Send(m) }
	idle := time.NewTimer(20 * time.Second)
	defer idle.Stop()
	select {
	case m := <-incoming:
		switch m.Type {
		case "start":
			session, e = vpn.Start(m.Settings, m.Password, emit)
			m.Password = ""
			if e != nil {
				emit(protocol.Message{Type: "error", Text: e.Error()})
				return nil
			}
		default:
			return errors.New("不支持的助手请求")
		}
	case <-closed:
		return nil
	case <-idle.C:
		return errors.New("助手未收到连接请求")
	}
	requested := false
	for {
		select {
		case m := <-incoming:
			if m.Type != "stop" {
				continue
			}
			requested = true
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			e = session.Stop(ctx)
			cancel()
			if e != nil {
				emit(protocol.Message{Type: "error", Text: e.Error()})
			}
		case <-session.Done:
			if e = session.Err(); e != nil && !requested {
				emit(protocol.Message{Type: "error", Text: "OpenConnect 退出：" + e.Error()})
			} else {
				emit(protocol.Message{Type: "state", State: "disconnected"})
			}
			return nil
		case <-closed:
			// The GUI exited unexpectedly. Keep ownership until OpenConnect has restored
			// the network; never kill it merely because the controlling socket closed.
			for {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				e = session.Stop(ctx)
				cancel()
				if e == nil {
					return nil
				}
				time.Sleep(time.Second)
			}
		}
	}
}
