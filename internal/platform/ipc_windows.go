package platform

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
	"net"
	"strings"
)

const pipePrefix = `\\.\pipe\CuhkVpnGo-`

func ListenIPC() (net.Listener, string, func(), error) {
	t, e := windows.OpenCurrentProcessToken()
	if e != nil {
		return nil, "", nil, e
	}
	defer t.Close()
	user, e := t.GetTokenUser()
	if e != nil {
		return nil, "", nil, e
	}
	b := make([]byte, 24)
	if _, e = rand.Read(b); e != nil {
		return nil, "", nil, e
	}
	p := pipePrefix + hex.EncodeToString(b)
	// go-winio also sets FILE_PIPE_REJECT_REMOTE_CLIENTS. Only this user,
	// administrators (including an alternate UAC identity), and SYSTEM may connect.
	l, e := winio.ListenPipe(p, &winio.PipeConfig{SecurityDescriptor: "D:P(A;;GA;;;" + user.User.Sid.String() + ")(A;;GA;;;BA)(A;;GA;;;SY)", InputBufferSize: 65536, OutputBufferSize: 65536})
	if e != nil {
		return nil, "", nil, e
	}
	return l, p, func() { l.Close() }, nil
}
func DialIPC(ctx context.Context, address string) (net.Conn, error) {
	if !strings.HasPrefix(address, pipePrefix) {
		return nil, errors.New("助手只接受本机命名管道")
	}
	suffix := strings.TrimPrefix(address, pipePrefix)
	if len(suffix) != 48 {
		return nil, errors.New("无效的助手管道")
	}
	if _, e := hex.DecodeString(suffix); e != nil {
		return nil, e
	}
	return winio.DialPipeContext(ctx, address)
}
