// The example uses a real stdio MCP client, not private server functions.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	ase "github.com/Jofdiazdi/aseprite-mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	server := flag.String("server", "./dist/aseprite-mcp", "built MCP executable")
	executable := flag.String("aseprite", "aseprite", "Aseprite executable")
	workspace := flag.String("workspace", "./dist/demo", "new demo directory")
	flag.Parse()
	path, err := filepath.Abs(*workspace)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(path, 0755); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "aseprite-example", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: exec.Command(*server, "serve", "--mode", "batch", "--workspace", path, "--aseprite", *executable)}, nil)
	if err != nil {
		return err
	}
	defer session.Close()
	call := func(name string, args any) (*mcp.CallToolResult, error) {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			return nil, err
		}
		if result.IsError {
			return nil, fmt.Errorf("%s: %v", name, result.Content)
		}
		return result, nil
	}
	result, err := call("create_document", ase.CreateArgs{Width: 16, Height: 16, ColorMode: "rgba"})
	if err != nil {
		return err
	}
	decodeDoc := func(result *mcp.CallToolResult) (ase.Document, error) {
		data, err := json.Marshal(result.StructuredContent)
		if err != nil {
			return ase.Document{}, err
		}
		var doc ase.Document
		err = json.Unmarshal(data, &doc)
		return doc, err
	}
	doc, err := decodeDoc(result)
	if err != nil {
		return err
	}
	green := ase.Color{RGBA: []int{70, 185, 95, 255}}
	highlight := ase.Color{RGBA: []int{145, 235, 125, 255}}
	dark := ase.Color{RGBA: []int{25, 50, 35, 255}}
	rows := []string{"..GGGG..", ".GHHGGG.", "GGGGGGGG", "GGKGGKGG", "GGGGGGGG", ".GGGGGG.", "..GGGG.."}
	edits := []ase.Edit{{Op: "rows", LayerID: doc.Layers[0].ID, Frame: 1, X: 4, Y: 5, Rows: rows, Legend: map[string]ase.Color{"G": green, "H": highlight, "K": dark}},
		{Op: "frame_duration", Frame: 1, DurationMS: 120},
		{Op: "frame_create", CopyFrom: 1, DurationMS: 100}, {Op: "cel_move", LayerID: doc.Layers[0].ID, Frame: 2, X: 0, Y: -2},
		{Op: "frame_create", CopyFrom: 1, DurationMS: 160}, {Op: "tag_set", Name: "bounce", Frame: 1, EndFrame: 3, Direction: "pingpong"}}
	result, err = call("apply_edits", ase.ApplyArgs{DocumentID: doc.ID, Revision: doc.Revision, Label: "Draw and animate a slime", Edits: edits})
	if err != nil {
		return err
	}
	doc, err = decodeDoc(result)
	if err != nil {
		return err
	}
	result, err = call("render_preview", ase.PreviewArgs{DocumentID: doc.ID, Frames: []int{1, 2, 3}, Scale: 8, Grid: true, Rulers: true})
	if err != nil {
		return err
	}
	for _, content := range result.Content {
		if img, ok := content.(*mcp.ImageContent); ok {
			if err = os.WriteFile(filepath.Join(path, "preview.png"), img.Data, 0644); err != nil {
				return err
			}
		}
	}
	if _, err = call("save_document", ase.SaveArgs{DocumentID: doc.ID, Revision: doc.Revision, Path: "slime.aseprite"}); err != nil {
		return err
	}
	for _, format := range []string{"gif", "sheet"} {
		out := "slime.png"
		if format == "gif" {
			out = "slime.gif"
		}
		if _, err = call("export_document", ase.ExportArgs{DocumentID: doc.ID, Revision: doc.Revision, Path: out, Format: format, Columns: 3}); err != nil {
			return err
		}
	}
	fmt.Println("Created editable artwork, preview, GIF and sprite sheet in", path)
	return nil
}
