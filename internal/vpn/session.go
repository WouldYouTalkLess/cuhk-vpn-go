package vpn

import (
	"bufio"
	"context"
	"cuhk-vpn-go/internal/config"
	"cuhk-vpn-go/internal/core"
	"cuhk-vpn-go/internal/platform"
	"cuhk-vpn-go/internal/protocol"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var secretLine = regexp.MustCompile(`(?i)(cookie\s*[:=]|set-cookie|authorization\s*:|\b(password|passwd|token)\s*[:=]|webvpn=)`)

func Sanitize(line string) string {
	if secretLine.MatchString(line) {
		return "[已隐藏认证数据]"
	}
	r := []rune(line)
	if len(r) > 2000 {
		return string(r[:2000]) + "…"
	}
	return line
}

type Session struct {
	cmd      *exec.Cmd
	Done     chan struct{}
	exited   chan struct{}
	err      error
	stopMu   sync.Mutex
	marker   string
	emit     func(protocol.Message)
	secret   string
	stopping atomic.Bool
}

func Arguments(s config.Settings, script string) []string {
	a := []string{"--protocol=anyconnect", "--non-inter", "--no-proxy", "--disable-ipv6", "--passwd-on-stdin", "--script", script, "--user", s.Username}
	if s.Group != "" {
		a = append(a, "--authgroup", s.Group)
	}
	return append(a, s.Server)
}

func Start(s config.Settings, password string, emit func(protocol.Message)) (*Session, error) {
	s, e := s.Validate(true)
	if e != nil {
		return nil, e
	}
	if password == "" || strings.ContainsAny(password, "\x00\r\n") {
		return nil, errors.New("密码为空或包含无效字符")
	}
	info := core.Find(s.CorePath)
	if !info.Ready {
		return nil, errors.New(info.Error)
	}
	release, e := platform.Lease()
	if e != nil {
		return nil, e
	}
	if e = platform.CheckExisting(); e != nil {
		release()
		return nil, e
	}
	dir, e := os.MkdirTemp("", "cuhk-vpn-session-")
	if e != nil {
		release()
		return nil, e
	}
	script, marker, e := WriteScripts(dir, info.Script, s)
	if e != nil {
		os.RemoveAll(dir)
		release()
		return nil, e
	}
	prepareStart := time.Now()
	extra, env, cleanup, e := prepare(s, dir)
	emit(protocol.Message{Type: "log", Text: fmt.Sprintf("登录前网络准备耗时 %.3f 秒", time.Since(prepareStart).Seconds())})
	if e != nil {
		cleanupUntilDone(cleanup, emit)
		os.RemoveAll(dir)
		release()
		return nil, e
	}
	args := Arguments(s, script)
	args = append(append(args[:len(args)-1], extra...), s.Server)
	cmd := exec.Command(info.Path, args...)
	platform.ConfigureVPN(cmd)
	// Do not inherit injected proxy or OpenConnect debug/logging environment settings.
	cmd.Env = []string{}
	for _, v := range os.Environ() {
		k := strings.ToUpper(strings.SplitN(v, "=", 2)[0])
		if strings.HasPrefix(k, "CISCO_") || strings.HasPrefix(k, "INTERNAL_IP") || strings.HasPrefix(k, "LOG") || k == "REASON" {
			continue
		}
		cmd.Env = append(cmd.Env, v)
	}
	cmd.Env = append(cmd.Env, "LC_ALL=C", "LANG=C")
	cmd.Env = append(cmd.Env, "PATH="+core.ToolsDir(info)+string(os.PathListSeparator)+os.Getenv("PATH"))
	cmd.Env = append(cmd.Env, env...)
	finish := func() error {
		e := cleanup()
		if e == nil {
			os.RemoveAll(dir)
			release()
		}
		return e
	}
	return launch(cmd, marker, password, emit, finish)
}

func launch(cmd *exec.Cmd, marker, password string, emit func(protocol.Message), cleanup func() error) (*Session, error) {
	stdin, e := cmd.StdinPipe()
	if e != nil {
		cleanupUntilDone(cleanup, emit)
		return nil, e
	}
	r, w, e := os.Pipe()
	if e != nil {
		stdin.Close()
		cleanupUntilDone(cleanup, emit)
		return nil, e
	}
	cmd.Stdout = w
	cmd.Stderr = w
	if e = cmd.Start(); e != nil {
		stdin.Close()
		r.Close()
		w.Close()
		cleanupUntilDone(cleanup, emit)
		return nil, e
	}
	w.Close()
	session := &Session{cmd: cmd, Done: make(chan struct{}), exited: make(chan struct{}), marker: marker, emit: emit, secret: password}
	emit(protocol.Message{Type: "state", State: "connecting", PID: cmd.Process.Pid})
	var reads sync.WaitGroup
	reads.Add(1)
	go func() { defer reads.Done(); defer r.Close(); session.readLogs(r) }()
	go func() {
		err := cmd.Wait()
		close(session.exited)
		reads.Wait()
		cleanupUntilDone(cleanup, emit)
		session.err = err
		close(session.Done)
	}()
	go session.monitor()
	// A pipe failure after Start still leaves an owned child: request graceful cleanup.
	_, e = io.WriteString(stdin, password+"\n")
	stdin.Close()
	if e != nil {
		emit(protocol.Message{Type: "log", Text: "无法写入登录信息，正在清理连接"})
		go func() {
			ctx, c := context.WithTimeout(context.Background(), 30*time.Second)
			defer c()
			session.Stop(ctx)
		}()
	}
	return session, nil
}

func cleanupUntilDone(cleanup func() error, emit func(protocol.Message)) {
	for {
		e := cleanup()
		if e == nil {
			return
		}
		emit(protocol.Message{Type: "error", Text: "网关路由清理失败，助手将继续重试：" + e.Error()})
		time.Sleep(3 * time.Second)
	}
}
func (s *Session) readLogs(r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), 128*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if s.secret != "" {
			line = strings.ReplaceAll(line, s.secret, "[已隐藏密码]")
		}
		s.emit(protocol.Message{Type: "log", Text: Sanitize(line)})
	}
	if e := scanner.Err(); e != nil {
		s.emit(protocol.Message{Type: "log", Text: "日志读取中断：" + e.Error()})
	}
}
func (s *Session) monitor() {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	last := ""
	for {
		select {
		case <-s.Done:
			return
		case <-s.exited:
			return
		case <-t.C:
			if s.stopping.Load() {
				continue
			}
			b, e := os.ReadFile(s.marker)
			if e != nil {
				continue
			}
			value := string(b)
			if value == last {
				continue
			}
			last = value
			p := strings.Split(value, "|")
			if len(p) != 3 {
				continue
			}
			if p[0] == "connect" || p[0] == "reconnect" {
				if a, e := netip.ParseAddr(p[1]); e == nil && a.Is4() {
					s.emit(protocol.Message{Type: "state", State: "connected", IP: a.String(), Interface: p[2], PID: s.cmd.Process.Pid})
				}
			}
		}
	}
}
func (s *Session) Stop(ctx context.Context) error {
	s.stopMu.Lock()
	defer s.stopMu.Unlock()
	select {
	case <-s.Done:
		return nil
	default:
	}
	s.emit(protocol.Message{Type: "state", State: "disconnecting"})
	s.stopping.Store(true)
	select {
	case <-s.exited: // The core has exited, but route cleanup may still be running.
	default:
		if e := platform.Stop(s.cmd); e != nil {
			select {
			case <-s.exited:
			default:
				return fmt.Errorf("正常断开信号发送失败：%w", e)
			}
		}
	}
	select {
	case <-s.Done:
		return nil
	case <-ctx.Done():
		return errors.New("OpenConnect 尚未完成正常断开，保留助手以继续清理，请重试")
	}
}
func (s *Session) Err() error { <-s.Done; return s.err }
