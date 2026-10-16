//go:build darwin || linux

package platform

import (
	"context"
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
	"runtime"
	"syscall"
)

func hide(cmd *exec.Cmd) {}
func Privileged() bool   { return os.Geteuid() == 0 }
func Elevate(ctx context.Context, args []string) error {
	exe, e := os.Executable()
	if e != nil {
		return e
	}
	argv := append([]string{exe}, args...)
	if Privileged() {
		return exec.CommandContext(ctx, exe, args...).Run()
	}
	if runtime.GOOS == "darwin" {
		// Arguments are first quoted for /bin/sh, then escaped as an AppleScript string.
		command := QuoteCommand(argv)
		return exec.CommandContext(ctx, "/usr/bin/osascript", "-e", "do shell script "+appleQuote(command)+" with administrator privileges").Run()
	}
	pk, e := exec.LookPath("pkexec")
	if e != nil {
		return errors.New("未找到 pkexec，请安装 polkit 和桌面授权代理")
	}
	return exec.CommandContext(ctx, pk, argv...).Run()
}
func appleQuote(s string) string {
	b := []byte{'"'}
	for _, c := range []byte(s) {
		if c == '\\' || c == '"' {
			b = append(b, '\\')
		}
		b = append(b, c)
	}
	return string(append(b, '"'))
}
func ConfigureVPN(cmd *exec.Cmd)    { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
func Stop(cmd *exec.Cmd) error      { return cmd.Process.Signal(syscall.SIGINT) }
func signalMode(args []string) bool { return false }
func Lease() (func(), error) {
	fd, e := unix.Open("/var/run/cuhk-vpn-go.lock", unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if e != nil {
		return nil, e
	}
	if e = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); e != nil {
		unix.Close(fd)
		return nil, errors.New("另一个校园 VPN 会话正在运行")
	}
	return func() { unix.Flock(fd, unix.LOCK_UN); unix.Close(fd) }, nil
}
