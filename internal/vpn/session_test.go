package vpn

import (
	"bufio"
	"context"
	"cuhk-vpn-go/internal/platform"
	"cuhk-vpn-go/internal/protocol"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	platform.RunSignalMode(os.Args[1:])
	if len(os.Args) > 1 && os.Args[1] == "--fake-core" {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt)
		p, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		fmt.Printf("echo %s", p)
		fmt.Println("COOKIE=do-not-log")
		marker := os.Args[2]
		os.WriteFile(marker, []byte("connect|10.20.1.2|test"), 0600)
		<-signals
		os.WriteFile(marker, []byte("disconnect||test"), 0600)
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func TestOwnedProcessGracefulStop(t *testing.T) {
	d := t.TempDir()
	marker := filepath.Join(d, "marker")
	exe, _ := os.Executable()
	cmd := exec.Command(exe, "--fake-core", marker)
	platform.ConfigureVPN(cmd)
	var mu sync.Mutex
	events := []protocol.Message{}
	ready := make(chan struct{}, 1)
	cleaned := make(chan struct{}, 1)
	emit := func(m protocol.Message) {
		mu.Lock()
		events = append(events, m)
		mu.Unlock()
		if m.State == "connected" {
			select {
			case ready <- struct{}{}:
			default:
			}
		}
	}
	s, e := launch(cmd, marker, "secret-test-password", emit, func() error { cleaned <- struct{}{}; return nil })
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		select {
		case <-s.Done:
		default:
			cmd.Process.Kill()
		}
	}()
	select {
	case <-ready:
	case <-time.After(8 * time.Second):
		t.Fatal("fake tunnel never became ready")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if e = s.Stop(ctx); e != nil {
		t.Fatal(e)
	}
	if e = s.Err(); e != nil {
		t.Fatal(e)
	}
	select {
	case <-cleaned:
	default:
		t.Fatal("cleanup not called")
	}
	b, e := os.ReadFile(marker)
	if e != nil || string(b) != "disconnect||test" {
		t.Fatalf("graceful cleanup marker: %s %v", b, e)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, m := range events {
		if strings.Contains(m.Text, "secret-test-password") || strings.Contains(m.Text, "do-not-log") {
			t.Fatalf("secret leaked: %s", m.Text)
		}
	}
}
func TestStartFailureCleansUp(t *testing.T) {
	calls := 0
	_, e := launch(exec.Command(filepath.Join(t.TempDir(), "missing-core")), "", "password", func(protocol.Message) {}, func() error { calls++; return nil })
	if e == nil || calls != 1 {
		t.Fatalf("err=%v cleanup=%d", e, calls)
	}
}
