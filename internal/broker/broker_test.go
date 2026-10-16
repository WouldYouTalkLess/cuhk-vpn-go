package broker

import (
	"context"
	"cuhk-vpn-go/internal/protocol"
	"strings"
	"testing"
	"time"
)

func TestPinnedAuthenticatedSession(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	clientDone := make(chan error, 1)
	w, e := open(ctx, func(_ context.Context, args []string) error {
		// A different ephemeral certificate must not be trusted.
		bad, e := Dial(args[1], strings.Repeat("00", 32), args[3])
		if bad != nil {
			bad.Close()
		}
		if e == nil {
			t.Error("accepted incorrect certificate pin")
		}
		client, e := Dial(args[1], args[2], args[3])
		if e != nil {
			clientDone <- e
			return e
		}
		defer client.Close()
		m, e := client.Receive()
		if e == nil && m.Text != "private request" {
			t.Error(m)
		}
		if e == nil {
			e = client.Send(protocol.Message{Type: "state", State: "connected"})
		}
		clientDone <- e
		return e
	})
	if e != nil {
		t.Fatal(e)
	}
	defer w.Close()
	if e = w.Send(protocol.Message{Type: "log", Text: "private request"}); e != nil {
		t.Fatal(e)
	}
	m, e := w.Receive()
	if e != nil || m.State != "connected" {
		t.Fatalf("%+v %v", m, e)
	}
	if e = <-clientDone; e != nil {
		t.Fatal(e)
	}
}
func TestCancelledElevation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, e := open(ctx, func(ctx context.Context, _ []string) error { <-ctx.Done(); return ctx.Err() })
	if e == nil {
		t.Fatal("cancelled connection accepted")
	}
}
func TestRemoteHelperRejected(t *testing.T) {
	if w, e := Dial("192.0.2.1:443", strings.Repeat("00", 32), strings.Repeat("00", 32)); e == nil {
		w.Close()
		t.Fatal("accepted remote endpoint")
	}
}
