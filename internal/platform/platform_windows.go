package platform

import (
	"context"
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var kernel = windows.NewLazySystemDLL("kernel32.dll")

func hide(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}
func ConfigureVPN(cmd *exec.Cmd) { hide(cmd) }
func Privileged() bool {
	t, e := windows.OpenCurrentProcessToken()
	if e != nil {
		return false
	}
	defer t.Close()
	return t.IsElevated()
}
func Elevate(ctx context.Context, args []string) error {
	exe, e := os.Executable()
	if e != nil {
		return e
	}
	return ShellExecute(exe, args)
}
func ShellExecute(exe string, args []string) error {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = syscall.EscapeArg(a)
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, e := windows.UTF16PtrFromString(exe)
	if e != nil {
		return e
	}
	params, e := windows.UTF16PtrFromString(strings.Join(quoted, " "))
	if e != nil {
		return e
	}
	dir, _ := windows.UTF16PtrFromString(filepath.Dir(exe))
	return windows.ShellExecute(0, verb, file, params, dir, windows.SW_HIDE)
}
func processIdentity(pid uint32) (string, uint64, error) {
	h, e := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if e != nil {
		return "", 0, e
	}
	defer windows.CloseHandle(h)
	b := make([]uint16, 32768)
	n := uint32(len(b))
	if e = windows.QueryFullProcessImageName(h, 0, &b[0], &n); e != nil {
		return "", 0, e
	}
	var c, x, k, u windows.Filetime
	if e = windows.GetProcessTimes(h, &c, &x, &k, &u); e != nil {
		return "", 0, e
	}
	return windows.UTF16ToString(b[:n]), uint64(c.HighDateTime)<<32 | uint64(c.LowDateTime), nil
}
func Stop(cmd *exec.Cmd) error {
	_, ticks, e := processIdentity(uint32(cmd.Process.Pid))
	if e != nil {
		return e
	}
	exe, e := os.Executable()
	if e != nil {
		return e
	}
	c := exec.Command(exe, "--vpn-signal", strconv.Itoa(cmd.Process.Pid), strconv.FormatUint(ticks, 10), cmd.Path)
	hide(c)
	if b, e := c.CombinedOutput(); e != nil {
		return CommandError("Ctrl+C", e, b)
	}
	return nil
}
func signalMode(args []string) bool {
	if len(args) == 0 || args[0] != "--vpn-signal" {
		return false
	}
	if len(args) != 4 {
		os.Exit(2)
	}
	pid, e := strconv.ParseUint(args[1], 10, 32)
	if e != nil {
		os.Exit(2)
	}
	ticks, e := strconv.ParseUint(args[2], 10, 64)
	if e != nil {
		os.Exit(2)
	}
	image, created, e := processIdentity(uint32(pid))
	if e != nil || created != ticks || !strings.EqualFold(filepath.Clean(image), filepath.Clean(args[3])) {
		os.Exit(3)
	}
	kernel.NewProc("FreeConsole").Call()
	r, _, _ := kernel.NewProc("AttachConsole").Call(uintptr(pid))
	if r == 0 {
		os.Exit(4)
	}
	kernel.NewProc("SetConsoleCtrlHandler").Call(0, 1)
	r, _, _ = kernel.NewProc("GenerateConsoleCtrlEvent").Call(0, 0)
	if r == 0 {
		os.Exit(5)
	}
	time.Sleep(300 * time.Millisecond)
	kernel.NewProc("FreeConsole").Call()
	os.Exit(0)
	return true
}
func Lease() (func(), error) {
	ready := make(chan error, 1)
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(done)
		name, _ := windows.UTF16PtrFromString("Global\\CUHKOpenConnectLauncher-v1")
		h, e := windows.CreateMutex(nil, false, name)
		if e != nil {
			ready <- e
			return
		}
		defer windows.CloseHandle(h)
		r, e := windows.WaitForSingleObject(h, 0)
		if e != nil {
			ready <- e
			return
		}
		if r != windows.WAIT_OBJECT_0 && r != windows.WAIT_ABANDONED {
			ready <- errors.New("原 VPN 客户端仍在运行，请先通过原客户端退出")
			return
		}
		ready <- nil
		<-release
		windows.ReleaseMutex(h)
	}()
	if e := <-ready; e != nil {
		return nil, e
	}
	return func() { close(release); <-done }, nil
}

// Reject a second unmanaged OpenConnect process instead of interfering with it.
func CheckExisting() error {
	h, e := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if e != nil {
		return e
	}
	defer windows.CloseHandle(h)
	p := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for e = windows.Process32First(h, &p); e == nil; e = windows.Process32Next(h, &p) {
		if strings.EqualFold(windows.UTF16ToString(p.ExeFile[:]), "openconnect.exe") {
			return fmt.Errorf("检测到已有 OpenConnect 进程 %d，请先在原客户端断开", p.ProcessID)
		}
	}
	if e != windows.ERROR_NO_MORE_FILES {
		return e
	}
	return nil
}
