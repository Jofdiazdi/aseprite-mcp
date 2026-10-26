package asepritemcp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestLivePairingSerializationAndUncertainOutcome(t *testing.T) {
	l, err := NewLive(2 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	data, err := os.ReadFile(l.PairingFile)
	if err != nil {
		t.Fatal(err)
	}
	var pairing struct {
		URL, Token string
		Protocol   int
	}
	if err := json.Unmarshal(data, &pairing); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if conn, _, err := websocket.Dial(ctx, pairing.URL+"?token=wrong", nil); err == nil {
		conn.CloseNow()
		t.Fatal("unauthenticated connection accepted")
	}
	c, _, err := websocket.Dial(ctx, pairing.URL+"?token="+pairing.Token, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	hello, _ := json.Marshal(map[string]any{"protocol": 1, "api_version": 41, "aseprite_version": "test"})
	if err := c.Write(ctx, websocket.MessageText, hello); err != nil {
		t.Fatal(err)
	}
	// Wait for the handshake, bounded by the test context.
	for {
		raw, _ := l.Call(ctx, "get_status", map[string]any{})
		status, _ := decode[map[string]any](raw)
		if status["connected"] == true {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
	if duplicate, _, err := websocket.Dial(ctx, pairing.URL+"?token="+pairing.Token, nil); err == nil {
		duplicate.CloseNow()
		t.Fatal("second editor accepted")
	}
	done := make(chan error, 1)
	go func() {
		_, data, err := c.Read(ctx)
		if err != nil {
			done <- err
			return
		}
		var req Request
		if err = json.Unmarshal(data, &req); err != nil {
			done <- err
			return
		}
		result, _ := json.Marshal(map[string]any{"document_id": "doc", "revision": "r"})
		reply, _ := json.Marshal(Reply{ID: req.ID, OK: true, Result: result})
		if err = c.Write(ctx, websocket.MessageText, reply); err != nil {
			done <- err
			return
		}
		// Read the next mutation, then disconnect without acknowledging it.
		_, _, err = c.Read(ctx)
		c.CloseNow()
		done <- err
	}()
	raw, err := l.Call(ctx, "get_document", map[string]any{"document_id": "doc"})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := decode[Document](raw)
	if err != nil || doc.ID != "doc" {
		t.Fatalf("reply: %s %v", raw, err)
	}
	_, err = l.Call(ctx, "apply_edits", map[string]any{"document_id": "doc", "revision": "r"})
	var failure *Failure
	if !errors.As(err, &failure) || !failure.Uncertain {
		t.Fatalf("mutation must report uncertain outcome: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestLiveRejectsIncompatibleExtension(t *testing.T) {
	l, err := NewLive(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	data, _ := os.ReadFile(l.PairingFile)
	var pairing struct{ URL, Token string }
	json.Unmarshal(data, &pairing)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, pairing.URL+"?token="+pairing.Token, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	c.Write(ctx, websocket.MessageText, []byte(`{"protocol":99,"api_version":41}`))
	if _, _, err = c.Read(ctx); err == nil {
		t.Fatal("incompatible connection kept open")
	}
	raw, err := l.Call(ctx, "get_status", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	status, _ := decode[map[string]any](raw)
	if status["connected"] != false || status["connection_error"] == nil {
		t.Fatalf("no compatibility diagnostic: %s", raw)
	}
}
