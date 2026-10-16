// Package broker connects the ordinary Wails process to a short-lived privileged
// copy of the same executable. The OS-protected local pipe uses a pinned ephemeral TLS
// certificate and a random token, both discarded when the session ends.
package broker

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"cuhk-vpn-go/internal/platform"
	"cuhk-vpn-go/internal/protocol"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"time"
)

func Open(ctx context.Context) (*protocol.Wire, error) {
	return open(ctx, platform.Elevate)
}

func open(ctx context.Context, elevate func(context.Context, []string) error) (*protocol.Wire, error) {
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		return nil, e
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(2 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, e := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if e != nil {
		return nil, e
	}
	digest := sha256.Sum256(der)
	raw, address, cleanup, e := platform.ListenIPC()
	if e != nil {
		return nil, e
	}
	defer cleanup()
	listener := tls.NewListener(raw, &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS13})
	defer listener.Close()
	secret := make([]byte, 32)
	if _, e = rand.Read(secret); e != nil {
		return nil, e
	}
	token := hex.EncodeToString(secret)
	launchCtx, launchCancel := context.WithCancel(context.Background())
	live := false
	defer func() {
		if !live {
			launchCancel()
		}
	}()
	// Do not cancel a live helper on completion of the initial handshake.
	launched := make(chan error, 1)
	go func() {
		defer launchCancel()
		launched <- elevate(launchCtx, []string{"--vpn-helper", address, hex.EncodeToString(digest[:]), token})
	}()
	accepted := make(chan *protocol.Wire)
	acceptCtx, acceptCancel := context.WithCancel(ctx)
	defer acceptCancel()
	acceptErr := make(chan error, 1)
	go func() {
		for {
			c, e := listener.Accept()
			if e != nil {
				acceptErr <- e
				return
			}
			c.SetDeadline(time.Now().Add(5 * time.Second))
			w := protocol.New(c)
			m, e := w.Receive()
			if e != nil || m.Type != "hello" || subtle.ConstantTimeCompare([]byte(m.Token), []byte(token)) != 1 {
				w.Close()
				continue
			}
			c.SetDeadline(time.Time{})
			select {
			case accepted <- w:
			case <-acceptCtx.Done():
				w.Close()
			}
			return
		}
	}()
	timeout := time.NewTimer(90 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case w := <-accepted:
			live = true
			return w, nil
		case e := <-launched:
			if e != nil {
				launchCancel()
				return nil, fmt.Errorf("系统授权失败或被取消：%w", e)
			}
			launched = nil
		case e := <-acceptErr:
			launchCancel()
			return nil, e
		case <-ctx.Done():
			launchCancel()
			return nil, ctx.Err()
		case <-timeout.C:
			launchCancel()
			return nil, errors.New("等待系统授权超时，请重试")
		}
	}
}

func Dial(address, pin, token string) (*protocol.Wire, error) {
	expected, e := hex.DecodeString(pin)
	if e != nil || len(expected) != sha256.Size || len(token) != 64 {
		return nil, errors.New("无效的助手会话")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conf := &tls.Config{MinVersion: tls.VersionTLS13,
		// Trust exactly this GUI process's ephemeral certificate, rather than a public CA.
		InsecureSkipVerify: true, VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) != 1 {
				return errors.New("无效的会话证书")
			}
			sum := sha256.Sum256(cs.PeerCertificates[0].Raw)
			if subtle.ConstantTimeCompare(sum[:], expected) != 1 {
				return errors.New("助手会话证书不匹配")
			}
			return nil
		}}
	raw, e := platform.DialIPC(ctx, address)
	if e != nil {
		return nil, e
	}
	c := tls.Client(raw, conf)
	if e = c.HandshakeContext(ctx); e != nil {
		c.Close()
		return nil, e
	}
	w := protocol.New(c)
	if e = w.Send(protocol.Message{Type: "hello", Token: token}); e != nil {
		w.Close()
		return nil, e
	}
	return w, nil
}
