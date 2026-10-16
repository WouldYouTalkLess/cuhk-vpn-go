package main

import (
	"context"
	"cuhk-vpn-go/internal/broker"
	"cuhk-vpn-go/internal/config"
	"cuhk-vpn-go/internal/core"
	"cuhk-vpn-go/internal/credentials"
	"cuhk-vpn-go/internal/platform"
	"cuhk-vpn-go/internal/protocol"
	"cuhk-vpn-go/internal/vpn"
	"errors"
	"fmt"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"strings"
	"sync"
	"time"
)

type View struct {
	Settings  config.Settings `json:"settings"`
	Core      core.Info       `json:"core"`
	State     string          `json:"state"`
	Phase     string          `json:"phase"`
	Error     string          `json:"error"`
	IP        string          `json:"ip"`
	Interface string          `json:"interface"`
	PID       int             `json:"pid"`
	Logs      []string        `json:"logs"`
}
type App struct {
	ctx           context.Context
	mu            sync.Mutex
	view          View
	wire          *protocol.Wire
	cancel        context.CancelFunc
	done          chan struct{}
	busy          bool
	prepareCancel context.CancelFunc
}

func NewApp() *App {
	s, e := config.Load()
	a := &App{view: View{Settings: s, State: "disconnected", Logs: []string{}}}
	if e != nil {
		a.view.Error = "读取设置失败：" + e.Error()
	}
	return a
}
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if a.snapshot().Settings.CorePath == "" {
		go a.PrepareCore()
	}
}
func (a *App) emit() {
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "vpn:update", a.snapshot())
	}
}
func (a *App) snapshot() View {
	a.mu.Lock()
	defer a.mu.Unlock()
	v := a.view
	v.Logs = append([]string{}, v.Logs...)
	v.Settings.CIDRs = append([]string{}, v.Settings.CIDRs...)
	return v
}
func (a *App) GetView() View { return a.snapshot() }

func (a *App) connectionPhase(phase string) {
	a.mu.Lock()
	if a.view.State != "connecting" {
		a.mu.Unlock()
		return
	}
	a.view.Phase = phase
	a.mu.Unlock()
	a.emit()
}
func (a *App) log(line string) {
	if strings.TrimSpace(line) == "" {
		return
	}
	safe := time.Now().Format("15:04:05") + "  " + vpn.Sanitize(line)
	a.mu.Lock()
	a.view.Logs = append(a.view.Logs, safe)
	if len(a.view.Logs) > 500 {
		a.view.Logs = a.view.Logs[len(a.view.Logs)-500:]
	}
	a.mu.Unlock()
	a.emit()
}
func (a *App) fail(err error) {
	a.mu.Lock()
	a.view.Error = err.Error()
	a.mu.Unlock()
	a.log(err.Error())
}
func (a *App) RefreshCore() core.Info {
	a.mu.Lock()
	p := a.view.Settings.CorePath
	a.mu.Unlock()
	i := core.Find(p)
	a.mu.Lock()
	a.view.Core = i
	a.mu.Unlock()
	a.emit()
	return i
}
func (a *App) SaveSettings(s config.Settings) error {
	s, e := s.Validate(false)
	if e != nil {
		return e
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.busy {
		return errors.New("请先断开连接或等待安装完成后再修改设置")
	}
	if e = config.Save(s); e != nil {
		return e
	}
	a.view.Settings = s
	return nil
}
func credentialAccount(s config.Settings) string { return s.Server + "|" + s.Username }

func savePassword(s config.Settings, password string) error {
	if password == "" || len(password) > 16<<10 || strings.ContainsAny(password, "\x00\r\n") {
		return errors.New("请输入有效密码")
	}
	if e := credentials.Set(s.PasswordStorage, credentialAccount(s), password); e != nil {
		if s.PasswordStorage == config.PasswordPortable {
			return fmt.Errorf("本地密码保存失败，请确认软件目录可写：%w", e)
		}
		return fmt.Errorf("系统凭据存储不可用，可改用随软件目录保存：%w", e)
	}
	return nil
}

// SaveProfile also saves a newly entered password without starting a VPN.
// An empty field preserves the existing saved password.
func (a *App) SaveProfile(s config.Settings, password string) error {
	s, e := s.Validate(s.Remember && password != "")
	if e != nil {
		return e
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.busy {
		return errors.New("请先结束当前操作")
	}
	if s.Remember && password != "" {
		if e = savePassword(s, password); e != nil {
			return e
		}
	}
	if e = config.Save(s); e != nil {
		return e
	}
	a.view.Settings = s
	return nil
}

func (a *App) Connect(s config.Settings, password string) error {
	s, e := s.Validate(true)
	if e != nil {
		return e
	}
	a.mu.Lock()
	if a.busy {
		a.mu.Unlock()
		return errors.New("当前操作尚未结束")
	}
	a.busy = true
	a.mu.Unlock()
	undo := func() { a.mu.Lock(); a.busy = false; a.mu.Unlock() }
	if password == "" && s.Remember {
		password, e = credentials.Get(s.PasswordStorage, credentialAccount(s))
		if e != nil {
			undo()
			return fmt.Errorf("无法读取所选保存方式中的密码，请重新输入密码：%w", e)
		}
	}
	if password == "" || len(password) > 16<<10 || strings.ContainsAny(password, "\x00\r\n") {
		undo()
		return errors.New("请输入有效密码")
	}
	if s.Remember {
		if e = savePassword(s, password); e != nil {
			undo()
			return e
		}
	}
	if e = config.Save(s); e != nil {
		undo()
		return e
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	a.mu.Lock()
	a.view.Settings = s
	a.view.Error = ""
	a.view.State = "connecting"
	a.view.Phase = "core"
	a.view.IP = ""
	a.view.Interface = ""
	a.view.PID = 0
	a.cancel = cancel
	a.done = done
	a.mu.Unlock()
	a.emit()
	go a.runConnection(ctx, s, password, done)
	return nil
}
func (a *App) runConnection(ctx context.Context, s config.Settings, password string, done chan struct{}) {
	defer func() {
		a.mu.Lock()
		a.wire = nil
		a.cancel = nil
		a.busy = false
		a.view.PID = 0
		a.view.IP = ""
		a.view.Interface = ""
		a.view.State = "disconnected"
		a.view.Phase = ""
		a.mu.Unlock()
		close(done)
		a.emit()
	}()
	i := core.Find(s.CorePath)
	if !i.Ready && s.CorePath == "" {
		var e error
		i, e = core.Prepare(ctx, a.log)
		if e != nil {
			if !errors.Is(e, context.Canceled) {
				a.fail(e)
			}
			return
		}
	}
	a.mu.Lock()
	a.view.Core = i
	a.mu.Unlock()
	if !i.Ready {
		a.fail(errors.New(i.Error))
		return
	}
	a.connectionPhase("authorize")
	a.log("正在等待系统授权…")
	w, e := broker.Open(ctx)
	if e != nil {
		if !errors.Is(e, context.Canceled) {
			a.fail(e)
		}
		return
	}
	defer w.Close()
	a.mu.Lock()
	// Cancellation before handshake completion must never race a late start request.
	if ctx.Err() != nil {
		a.mu.Unlock()
		return
	}
	s.CorePath = i.Path
	e = w.Send(protocol.Message{Type: "start", Settings: s, Password: password})
	if e == nil {
		a.wire = w
	}
	a.mu.Unlock()
	if e != nil {
		a.fail(e)
		return
	}
	password = ""
	a.connectionPhase("connect")
	go func() {
		select {
		case <-ctx.Done():
			w.Send(protocol.Message{Type: "stop"})
		case <-done:
		}
	}()
	for {
		m, e := w.Receive()
		if e != nil {
			if v := a.snapshot(); v.State != "disconnected" && v.Error == "" {
				a.fail(errors.New("助手连接已结束；若非正常退出，助手会继续正常断开并清理路由"))
			}
			return
		}
		switch m.Type {
		case "log":
			a.log(m.Text)
		case "error":
			a.fail(errors.New(m.Text))
		case "state":
			a.mu.Lock()
			a.view.State = m.State
			switch m.State {
			case "connecting":
				a.view.Phase = "connect"
			case "connected":
				a.view.Phase = "connected"
			case "disconnecting":
				a.view.Phase = "disconnect"
			default:
				a.view.Phase = ""
			}
			if m.PID != 0 {
				a.view.PID = m.PID
			}
			if m.IP != "" {
				a.view.IP = m.IP
				a.view.Interface = m.Interface
			}
			a.mu.Unlock()
			a.emit()
			if m.State == "disconnected" {
				return
			}
		}
	}
}
func (a *App) Disconnect() error {
	a.mu.Lock()
	cancel := a.cancel
	w := a.wire
	state := a.view.State
	if cancel != nil {
		a.view.State = "disconnecting"
		a.view.Phase = "disconnect"
	}
	a.mu.Unlock()
	if state == "preparing" {
		return errors.New("请等待便携核心下载完成")
	}
	if cancel != nil {
		cancel()
	}
	if w != nil {
		if e := w.Send(protocol.Message{Type: "stop"}); e != nil {
			return e
		}
	}
	a.emit()
	return nil
}
func (a *App) ForgetPassword() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.busy {
		return errors.New("请先结束当前操作")
	}
	s := a.view.Settings
	e := credentials.Delete(s.PasswordStorage, credentialAccount(s))
	if e != nil && !errors.Is(e, credentials.ErrNotFound) {
		return e
	}
	s.Remember = false
	if e = config.Save(s); e != nil {
		return e
	}
	a.view.Settings = s
	return nil
}
func (a *App) ClearLogs() { a.mu.Lock(); a.view.Logs = []string{}; a.mu.Unlock(); a.emit() }
func (a *App) PrepareCore() error {
	a.mu.Lock()
	if a.busy {
		a.mu.Unlock()
		return errors.New("请先结束当前操作")
	}
	a.busy = true
	a.view.State = "preparing"
	a.view.Phase = "core"
	a.view.Error = ""
	done := make(chan struct{})
	a.done = done
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	a.prepareCancel = cancel
	a.mu.Unlock()
	a.emit()
	go func() {
		defer cancel()
		defer func() {
			a.RefreshCore()
			a.mu.Lock()
			a.busy = false
			a.prepareCancel = nil
			a.view.State = "disconnected"
			a.view.Phase = ""
			a.mu.Unlock()
			close(done)
			a.emit()
		}()
		_, e := core.Prepare(ctx, a.log)
		if e != nil && !errors.Is(e, context.Canceled) {
			a.fail(e)
		}
	}()
	return nil
}
func (a *App) Probe() (string, error) {
	v := a.snapshot()
	if v.State != "connected" || v.IP == "" {
		return "", errors.New("请先连接 VPN")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	if e := platform.VerifyRoutes(ctx, v.Settings.CIDRs, v.IP, v.Interface, core.ToolsDir(v.Core)); e != nil {
		return "", e
	}
	now := a.snapshot()
	if now.State != "connected" || now.PID != v.PID || now.IP != v.IP {
		return "", errors.New("连接已改变，请重新检测")
	}
	msg := fmt.Sprintf("%d 个分流网段的路由已就绪 · VPN %s", len(v.Settings.CIDRs), v.IP)
	a.log(msg)
	return msg, nil
}
func (a *App) beforeClose(ctx context.Context) bool {
	a.mu.Lock()
	busy := a.busy
	state := a.view.State
	done := a.done
	a.mu.Unlock()
	if !busy {
		return false
	}
	if state == "preparing" {
		a.mu.Lock()
		cancel := a.prepareCancel
		a.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		return false
	}
	a.Disconnect()
	go func() {
		select {
		case <-done:
			wruntime.Quit(ctx)
		case <-time.After(35 * time.Second):
			a.log("仍在正常断开，请稍后重试退出")
		}
	}()
	return true
}
func (a *App) shutdown(ctx context.Context) {
	a.mu.Lock()
	cancel := a.prepareCancel
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	a.Disconnect()
}
