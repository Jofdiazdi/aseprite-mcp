package asepritemcp

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/coder/websocket"
)

type Live struct {
	mu          sync.Mutex
	callMu      sync.Mutex
	conn        *websocket.Conn
	hello       map[string]any
	incoming    chan Reply
	lastError   string
	server      *http.Server
	dir         string
	token       string
	PairingFile string
	timeout     time.Duration
}

func NewLive(timeout time.Duration) (*Live, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "aseprite-mcp-live-")
	if err != nil {
		listener.Close()
		return nil, err
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	l := &Live{dir: dir, token: randomID() + randomID(), timeout: timeout, PairingFile: filepath.Join(dir, "pairing.json")}
	data, _ := json.MarshalIndent(map[string]any{"protocol": ProtocolVersion, "url": "ws://" + listener.Addr().String() + "/bridge", "token": l.token}, "", "  ")
	if err := os.WriteFile(l.PairingFile, data, 0600); err != nil {
		listener.Close()
		os.RemoveAll(dir)
		return nil, err
	}
	l.server = &http.Server{Handler: http.HandlerFunc(l.accept), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = l.server.Serve(listener) }()
	return l, nil
}

func (l *Live) Mode() string { return "live" }
func (l *Live) Close() error {
	l.mu.Lock()
	if l.conn != nil {
		l.conn.CloseNow()
		l.conn = nil
	}
	l.mu.Unlock()
	err := l.server.Close()
	os.RemoveAll(l.dir)
	return err
}

func (l *Live) accept(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/bridge" || subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("token")), []byte(l.token)) != 1 {
		http.Error(w, "invalid pairing token", http.StatusUnauthorized)
		return
	}
	l.mu.Lock()
	if l.conn != nil {
		l.mu.Unlock()
		http.Error(w, "one editor connection is already active", http.StatusConflict)
		return
	}
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		l.mu.Unlock()
		return
	}
	c.SetReadLimit(16 << 20)
	// Reserve the connection slot during the handshake too.
	l.conn = c
	l.hello = nil
	l.mu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	_, data, err := c.Read(ctx)
	var hello map[string]any
	if err == nil {
		err = json.Unmarshal(data, &hello)
	}
	if err == nil && (hello["protocol"] != float64(ProtocolVersion) || number(hello["api_version"]) < 41) {
		err = fmt.Errorf("incompatible extension protocol or Aseprite API (requires API 41+)")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err != nil {
		l.lastError = err.Error()
		c.CloseNow()
		if l.conn == c {
			l.conn = nil
		}
		return
	}
	l.hello = hello
	l.lastError = ""
	l.incoming = make(chan Reply, 1)
	go l.receive(c, l.incoming)
}

func (l *Live) receive(c *websocket.Conn, incoming chan Reply) {
	defer close(incoming)
	defer c.CloseNow()
	defer func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.conn == c {
			l.conn = nil
			l.hello = nil
		}
	}()
	for {
		_, data, err := c.Read(context.Background())
		if err != nil {
			return
		}
		var reply Reply
		if json.Unmarshal(data, &reply) != nil {
			return
		}
		select {
		case incoming <- reply:
		default:
			return
		}
	}
}

func number(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}

func (l *Live) Call(ctx context.Context, op string, args map[string]any) (json.RawMessage, error) {
	l.callMu.Lock()
	defer l.callMu.Unlock()
	l.mu.Lock()
	c, hello, incoming, lastError := l.conn, l.hello, l.incoming, l.lastError
	l.mu.Unlock()
	if op == "get_status" {
		result := map[string]any{"connected": c != nil && hello != nil, "protocol": ProtocolVersion, "pairing_file": l.PairingFile}
		if lastError != "" {
			result["connection_error"] = lastError
		}
		for k, v := range hello {
			result[k] = v
		}
		return json.Marshal(result)
	}
	if c == nil || hello == nil {
		return nil, &Failure{Code: "disconnected", Message: "connect the Agent MCP extension using the pairing file"}
	}
	ctx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()
	req := Request{ID: randomID(), Op: op, Args: args}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	var reply Reply
	if err = c.Write(ctx, websocket.MessageText, data); err == nil {
		select {
		case got, ok := <-incoming:
			if !ok {
				err = fmt.Errorf("editor disconnected")
			} else {
				reply = got
			}
		case <-ctx.Done():
			err = ctx.Err()
		}
	}
	if err != nil {
		l.mu.Lock()
		if l.conn == c {
			l.conn = nil
			l.hello = nil
		}
		l.mu.Unlock()
		c.CloseNow()
		return nil, &Failure{Code: "connection_lost", Message: "editor connection lost; reconnect, get_document and inspect before retrying", Uncertain: op != "get_document" && op != "list_documents" && op != "read_pixels"}
	}
	if reply.ID != req.ID {
		l.mu.Lock()
		if l.conn == c {
			l.conn = nil
			l.hello = nil
		}
		l.mu.Unlock()
		c.CloseNow()
		return nil, &Failure{Code: "invalid_reply", Message: "bridge response mismatch; reconnect and inspect before retrying", Uncertain: true}
	}
	if !reply.OK {
		if reply.Error == nil {
			return nil, fmt.Errorf("bridge failed without error")
		}
		return nil, reply.Error
	}
	return reply.Result, nil
}
