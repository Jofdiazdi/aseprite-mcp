package asepritemcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

type savingBackend struct {
	doc        Document
	change     bool
	disconnect bool
}

func (*savingBackend) Mode() string { return "live" }
func (*savingBackend) Close() error { return nil }
func (b *savingBackend) Call(_ context.Context, op string, a map[string]any) (json.RawMessage, error) {
	switch op {
	case "get_document":
		return json.Marshal(b.doc)
	case "snapshot":
		if err := os.WriteFile(stringArg(a, "path"), []byte("snapshot"), 0600); err != nil {
			return nil, err
		}
		if b.change {
			b.doc.Revision = "newer"
		}
		return json.Marshal(b.doc)
	case "ack_save":
		if stringArg(a, "path") == stringArg(a, "display_path") {
			return nil, fmt.Errorf("native bridge must not write directly to destination")
		}
		if b.disconnect {
			return nil, &Failure{Code: "connection_lost", Message: "disconnected", Uncertain: true}
		}
		b.doc.Path = stringArg(a, "display_path")
		b.doc.Modified = false
		return json.Marshal(b.doc)
	}
	return nil, fmt.Errorf("unexpected operation %s", op)
}

func TestLiveSaveCommitAndAcknowledgement(t *testing.T) {
	for _, tt := range []struct {
		name               string
		change, disconnect bool
		code               string
		written            bool
	}{
		{"saved and acknowledged", false, false, "", true},
		{"changed during snapshot", true, false, "stale_revision", false},
		{"disconnected after commit", false, true, "save_acknowledgement_failed", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w, err := OpenWorkspace(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer w.Close()
			backend := &savingBackend{doc: Document{ID: "doc", Revision: "r1", Modified: true}, change: tt.change, disconnect: tt.disconnect}
			s := NewService(backend, w)
			a, _ := argumentMap(SaveArgs{DocumentID: "doc", Revision: "r1", Path: "art.aseprite"})
			raw, err := s.Execute(context.Background(), "save_document", a)
			if tt.code == "" {
				if err != nil {
					t.Fatal(err)
				}
				doc := documentResult(t, raw)
				if doc.Modified {
					t.Fatal("save was not acknowledged")
				}
			} else {
				var failure *Failure
				if !errors.As(err, &failure) || failure.Code != tt.code {
					t.Fatalf("unexpected error: %v", err)
				}
			}
			data, readErr := os.ReadFile(filepath.Join(w.path, "art.aseprite"))
			if tt.written {
				if readErr != nil || string(data) != "snapshot" {
					t.Fatalf("snapshot not committed: %q %v", data, readErr)
				}
			} else if !os.IsNotExist(readErr) {
				t.Fatal("stale snapshot was committed")
			}
		})
	}
}
