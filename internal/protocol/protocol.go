// Package protocol is the private, per-session GUI/helper protocol. It is never an HTTP server.
package protocol

import (
	"bufio"
	"cuhk-vpn-go/internal/config"
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

type Message struct {
	Type      string          `json:"type"`
	Token     string          `json:"token,omitempty"`
	Settings  config.Settings `json:"settings,omitempty"`
	Password  string          `json:"password,omitempty"`
	Text      string          `json:"text,omitempty"`
	State     string          `json:"state,omitempty"`
	IP        string          `json:"ip,omitempty"`
	Interface string          `json:"interface,omitempty"`
	PID       int             `json:"pid,omitempty"`
}
type Wire struct {
	conn net.Conn
	scan *bufio.Scanner
	mu   sync.Mutex
}

func New(c net.Conn) *Wire {
	s := bufio.NewScanner(c)
	s.Buffer(make([]byte, 4096), 256*1024)
	return &Wire{conn: c, scan: s}
}
func (w *Wire) Send(m Message) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	defer w.conn.SetWriteDeadline(time.Time{})
	return json.NewEncoder(w.conn).Encode(m)
}
func (w *Wire) Receive() (Message, error) {
	var m Message
	if !w.scan.Scan() {
		if e := w.scan.Err(); e != nil {
			return m, e
		}
		return m, io.EOF
	}
	if e := json.Unmarshal(w.scan.Bytes(), &m); e != nil {
		return m, e
	}
	if m.Type == "" {
		return m, errors.New("missing message type")
	}
	return m, nil
}
func (w *Wire) Close() error { return w.conn.Close() }
