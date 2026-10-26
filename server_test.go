package asepritemcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type previewBackend struct{}

func TestMain(m *testing.M) {
	if os.Getenv("ASEPRITE_MCP_SLEEP_HELPER") == "1" {
		time.Sleep(10 * time.Second)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func (previewBackend) Mode() string { return "batch" }
func (previewBackend) Close() error { return nil }
func (previewBackend) Call(_ context.Context, op string, a map[string]any) (json.RawMessage, error) {
	switch op {
	case "get_status":
		return json.Marshal(map[string]any{"protocol": 1})
	case "get_document":
		return json.Marshal(Document{ID: "doc", Revision: "r1", Width: 2, Height: 2, Frames: []Frame{{Number: 1, DurationMS: 100}}})
	case "read_pixels":
		return json.Marshal(Pixels{ColorMode: "rgba", Width: 2, Height: 2, Values: []uint32{0xff0000ff, 0, 0, 0}})
	default:
		return nil, &Failure{Code: "test_failure", Message: "deliberate failure"}
	}
}

func TestMCPProtocolToolsErrorsAndImage(t *testing.T) {
	w, err := OpenWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.root.Close()
	s := NewService(previewBackend{}, w)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := NewServer(s).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "integration-client", Version: "1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 18 {
		t.Fatalf("expected 18 tools, got %d", len(tools.Tools))
	}
	for _, tool := range tools.Tools {
		if tool.InputSchema == nil || tool.OutputSchema == nil {
			t.Fatalf("missing schema: %s", tool.Name)
		}
	}
	status, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_status", Arguments: map[string]any{}})
	if err != nil || status.IsError || status.StructuredContent == nil {
		t.Fatalf("status: %+v %v", status, err)
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_document", Arguments: map[string]any{"document_id": "doc"}})
	if err != nil || result.IsError {
		t.Fatalf("document: %+v %v", result, err)
	}
	preview, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "render_preview", Arguments: map[string]any{"document_id": "doc", "scale": 2}})
	if err != nil || preview.IsError {
		t.Fatalf("preview: %+v %v", preview, err)
	}
	found := false
	for _, content := range preview.Content {
		if img, ok := content.(*mcp.ImageContent); ok {
			found = true
			if img.MIMEType != "image/png" {
				t.Fatal(img.MIMEType)
			}
			if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !found {
		t.Fatal("no image returned")
	}
	failed, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "undo", Arguments: map[string]any{"document_id": "doc", "revision": "r1"}})
	if err != nil || !failed.IsError || failed.StructuredContent == nil {
		t.Fatalf("tool failure was not structured: %+v %v", failed, err)
	}
}

func TestBatchCancellation(t *testing.T) {
	// The test process itself is a portable fake executable, avoiding shell-only
	// scripts in the cross-platform process cancellation test.
	if testing.Short() {
		t.Skip("external process")
	}
	t.Setenv("ASEPRITE_MCP_SLEEP_HELPER", "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewBatch(exe, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	start := time.Now()
	_, err = b.Call(context.Background(), "get_status", map[string]any{})
	var failure *Failure
	if !errors.As(err, &failure) || failure.Code != "cancelled" || time.Since(start) > 3*time.Second {
		t.Fatalf("process not cancelled: %v", err)
	}
}

func TestExtensionArchiveMatchesRuntime(t *testing.T) {
	path := t.TempDir() + "/agent-mcp.aseprite-extension"
	if err := PackageExtension(path, ""); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"package.json", "plugin.lua", "dispatcher.lua"} {
		expected, _ := assets.ReadFile("assets/" + name)
		actual, err := os.ReadFile("extension/agent-mcp/" + name)
		if err != nil || !bytes.Equal(expected, actual) {
			t.Fatalf("extension source drift in %s; run go generate ./...: %v", name, err)
		}
	}
}

func TestPreviewPixelCoordinates(t *testing.T) {
	w, err := OpenWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.root.Close()
	s := NewService(previewBackend{}, w)
	data, _, err := s.Render(context.Background(), PreviewArgs{DocumentID: "doc", Scale: 3}, false)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 3; y++ {
		for x := 0; x < 3; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			if r != 65535 || g != 0 || b != 0 || a != 65535 {
				t.Fatal(fmt.Sprint("nearest-neighbor pixel mismatch at ", x, ",", y))
			}
		}
	}
}
