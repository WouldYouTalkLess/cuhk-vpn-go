//go:build darwin || linux

package platform

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
)

func ListenIPC() (net.Listener, string, func(), error) {
	d, e := os.MkdirTemp("/tmp", "cuhk-vpn-ipc-")
	if e != nil {
		return nil, "", nil, e
	}
	p := filepath.Join(d, "session.sock")
	l, e := net.Listen("unix", p)
	if e != nil {
		os.RemoveAll(d)
		return nil, "", nil, e
	}
	return l, p, func() { l.Close(); os.RemoveAll(d) }, nil
}
func DialIPC(ctx context.Context, address string) (net.Conn, error) {
	if !filepath.IsAbs(address) || filepath.Base(address) != "session.sock" || !strings.HasPrefix(filepath.Base(filepath.Dir(address)), "cuhk-vpn-ipc-") {
		return nil, errors.New("无效的本机会话 socket")
	}
	return (&net.Dialer{}).DialContext(ctx, "unix", address)
}
