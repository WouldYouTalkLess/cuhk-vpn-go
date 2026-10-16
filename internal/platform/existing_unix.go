//go:build darwin || linux

package platform

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

func CheckExisting() error {
	if runtime.GOOS == "linux" {
		entries, e := os.ReadDir("/proc")
		if e != nil {
			return e
		}
		for _, entry := range entries {
			if _, e := strconv.Atoi(entry.Name()); e != nil {
				continue
			}
			b, e := os.ReadFile(filepath.Join("/proc", entry.Name(), "comm"))
			if e == nil && strings.TrimSpace(string(b)) == "openconnect" {
				return errors.New("已有 OpenConnect 会话，请先通过原客户端断开")
			}
		}
		return nil
	}
	p, e := exec.LookPath("pgrep")
	if e != nil {
		return errors.New("缺少 pgrep，无法检查是否已有 OpenConnect 会话")
	}
	c := exec.Command(p, "-x", "openconnect")
	b, e := c.Output()
	if e == nil && len(b) > 0 {
		return errors.New("已有 OpenConnect 会话，请先通过原客户端断开")
	}
	if ex, ok := e.(*exec.ExitError); ok && ex.ExitCode() == 1 {
		return nil
	}
	return e
}
