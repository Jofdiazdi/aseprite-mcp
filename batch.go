package asepritemcp

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

type batchDocument struct {
	file  string
	path  string
	dirty bool
}

type Batch struct {
	mu         sync.Mutex
	executable string
	dir        string
	docs       map[string]*batchDocument
	timeout    time.Duration
	ready      bool
	status     json.RawMessage
}

func NewBatch(executable string, timeout time.Duration) (*Batch, error) {
	path, err := exec.LookPath(executable)
	if err != nil {
		return nil, fmt.Errorf("Aseprite executable not found; set --aseprite: %w", err)
	}
	dir, err := os.MkdirTemp("", "aseprite-mcp-")
	if err != nil {
		return nil, err
	}
	for _, name := range []string{"dispatcher.lua", "batch.lua"} {
		data, _ := assets.ReadFile("assets/" + name)
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			os.RemoveAll(dir)
			return nil, err
		}
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Batch{executable: path, dir: dir, docs: map[string]*batchDocument{}, timeout: timeout}, nil
}

func (b *Batch) Mode() string { return "batch" }
func (b *Batch) Close() error { b.mu.Lock(); defer b.mu.Unlock(); return os.RemoveAll(b.dir) }

// limitBuffer bounds diagnostics from a crashing/noisy external process.
type limitBuffer struct{ bytes.Buffer }

func (b *limitBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if b.Len() < 65536 {
		keep := min(len(p), 65536-b.Len())
		b.Buffer.Write(p[:keep])
	}
	return n, nil
}

func (b *Batch) Call(ctx context.Context, op string, args map[string]any) (json.RawMessage, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.ready {
		raw, err := b.run(ctx, "get_status", map[string]any{}, "", "")
		if err != nil {
			return nil, err
		}
		status, err := decode[map[string]any](raw)
		if err != nil {
			return nil, err
		}
		if number(status["api_version"]) < 41 || number(status["protocol"]) != ProtocolVersion {
			return nil, &Failure{Code: "incompatible_version", Message: "requires Aseprite scripting API 41+ and matching bridge protocol"}
		}
		b.ready = true
		b.status = raw
	}
	if op == "undo" || op == "redo" {
		return nil, &Failure{Code: "unsupported_operation", Message: "undo and redo require live mode"}
	}
	if op == "list_documents" {
		out := []Document{}
		for id, d := range b.docs {
			raw, err := b.run(ctx, "get_document", map[string]any{"document_id": id}, d.file, "")
			if err != nil {
				return nil, err
			}
			doc, err := decode[Document](raw)
			if err != nil {
				return nil, err
			}
			doc.Path = d.path
			doc.Modified = d.dirty
			out = append(out, doc)
		}
		return json.Marshal(map[string]any{"documents": out})
	}
	if op == "get_status" {
		return b.status, nil
	}
	id, _ := args["document_id"].(string)
	d := b.docs[id]
	isNew := op == "create_document" || op == "open_document"
	if !isNew && d == nil {
		return nil, &Failure{Code: "document_closed", Message: "document is closed; list or open documents again"}
	}
	if isNew {
		d = &batchDocument{file: filepath.Join(b.dir, id+".aseprite"), dirty: op == "create_document"}
	}
	if op == "close_document" && d.dirty && args["discard"] != true {
		return nil, &Failure{Code: "unsaved_changes", Message: "save or set discard=true"}
	}
	input := ""
	if !isNew {
		input = d.file
	}
	output := ""
	if isNew || op == "apply_edits" {
		output = filepath.Join(b.dir, randomID()+".aseprite")
	}
	raw, err := b.run(ctx, op, args, input, output)
	if err != nil {
		if output != "" {
			os.Remove(output)
		}
		return nil, err
	}
	if output != "" {
		if err := os.Rename(output, d.file); err != nil {
			return nil, err
		}
		if op == "apply_edits" {
			d.dirty = true
		}
	}
	if op == "open_document" {
		d.path, _ = args["path"].(string)
	}
	if op == "save_document" {
		d.path, _ = args["path"].(string)
		d.dirty = false
	}
	if op == "close_document" {
		os.Remove(d.file)
		delete(b.docs, id)
		return raw, nil
	}
	b.docs[id] = d
	if op == "get_document" || isNew || op == "apply_edits" || op == "save_document" {
		doc, err := decode[Document](raw)
		if err != nil {
			return nil, err
		}
		doc.Path = d.path
		doc.Modified = d.dirty
		return json.Marshal(doc)
	}
	return raw, nil
}

func (b *Batch) run(ctx context.Context, op string, args map[string]any, input, output string) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()
	callDir, err := os.MkdirTemp(b.dir, "call-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(callDir)
	requestPath, responsePath := filepath.Join(callDir, "request.json"), filepath.Join(callDir, "response.json")
	req := Request{ID: randomID(), Op: op, Args: args}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(requestPath, data, 0600); err != nil {
		return nil, err
	}
	cli := []string{"--batch", "--script-param", "dispatcher=" + filepath.Join(b.dir, "dispatcher.lua"),
		"--script-param", "request=" + requestPath, "--script-param", "response=" + responsePath}
	if input != "" {
		cli = append(cli, "--script-param", "input="+input)
	}
	if output != "" {
		cli = append(cli, "--script-param", "output="+output)
	}
	cli = append(cli, "--script", filepath.Join(b.dir, "batch.lua"))
	cmd := exec.CommandContext(ctx, b.executable, cli...)
	cmd.WaitDelay = time.Second
	var log limitBuffer
	cmd.Stdout = &log
	cmd.Stderr = &log
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, &Failure{Code: "cancelled", Message: "Aseprite process terminated; inspect before retrying any file write", Uncertain: op == "save_document" || op == "export_gif"}
		}
		return nil, fmt.Errorf("Aseprite failed: %w: %s", err, log.String())
	}
	data, err = os.ReadFile(responsePath)
	if err != nil {
		return nil, fmt.Errorf("Aseprite produced no response (check scripting support): %s: %w", log.String(), err)
	}
	var reply Reply
	if err := json.Unmarshal(data, &reply); err != nil {
		return nil, fmt.Errorf("invalid Lua response: %w", err)
	}
	if reply.ID != req.ID {
		return nil, fmt.Errorf("bridge response id mismatch")
	}
	if !reply.OK {
		if reply.Error == nil {
			return nil, fmt.Errorf("bridge failed without an error")
		}
		return nil, reply.Error
	}
	return reply.Result, nil
}
