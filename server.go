package asepritemcp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const Instructions = `Create pixel art in passes: plan a small palette and silhouette, organize parts into layers, then refine and shade with one light direction. Inspect each pass with render_preview. Use read_rows and the rows edit for compact pixel detail: '.' leaves a pixel unchanged, '~' erases it, and other ASCII characters map to mode-specific colors in legend. Read document metadata for document_id, layer_id and revision; supply the latest revision for every mutation. Temporary layer refs created in one apply_edits call can be used by later edits in that call. Coordinates are canvas-global and zero-based; frames are one-based; durations are milliseconds. Build animation key poses before in-betweens; use onion skins and compare_frames to verify motion and timing. Read composite pixels for visible output and raw layer pixels for exact color-mode values. Edits are unsaved until save_document. Export does not save the source. Undo/redo require live mode. If a mutation outcome is uncertain, reconnect and inspect before retrying. Never infer success from a file path alone.`

type EmptyArgs struct{}
type DocumentArgs struct {
	DocumentID string `json:"document_id"`
}
type MutationArgs struct {
	DocumentID string `json:"document_id"`
	Revision   string `json:"revision"`
}
type OpenArgs struct {
	Path string `json:"path" jsonschema:"Workspace-relative or absolute artwork path inside the workspace"`
}
type CreateArgs struct {
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	ColorMode string `json:"color_mode,omitempty" jsonschema:"rgba (default), indexed, or grayscale"`
}
type CloseArgs struct {
	DocumentID string `json:"document_id"`
	Revision   string `json:"revision"`
	Discard    bool   `json:"discard,omitempty"`
}
type ReadArgs struct {
	DocumentID string  `json:"document_id"`
	LayerID    string  `json:"layer_id,omitempty" jsonschema:"Omit to read the visible RGBA composite"`
	Frame      int     `json:"frame,omitempty" jsonschema:"One-based frame; default 1"`
	Region     *Region `json:"region,omitempty"`
	Composite  bool    `json:"composite,omitempty" jsonschema:"Render an isolated layer as RGBA rather than returning its raw pixels"`
}
type ApplyArgs struct {
	DocumentID string `json:"document_id"`
	Revision   string `json:"revision"`
	Label      string `json:"label,omitempty"`
	Edits      []Edit `json:"edits"`
}
type SaveArgs struct {
	DocumentID string `json:"document_id"`
	Revision   string `json:"revision"`
	Path       string `json:"path"`
	Overwrite  bool   `json:"overwrite,omitempty"`
}
type ExportArgs struct {
	DocumentID string `json:"document_id"`
	Revision   string `json:"revision"`
	Path       string `json:"path"`
	Format     string `json:"format" jsonschema:"png, gif, or sheet"`
	Frames     []int  `json:"frames,omitempty" jsonschema:"PNG/sheet frames; default all; GIF requires the full timeline"`
	Columns    int    `json:"columns,omitempty" jsonschema:"Sheet columns; default a horizontal strip"`
	Overwrite  bool   `json:"overwrite,omitempty"`
}
type CompareArgs struct {
	DocumentID string  `json:"document_id"`
	Frame      int     `json:"frame"`
	OtherFrame int     `json:"other_frame"`
	LayerID    string  `json:"layer_id,omitempty"`
	Region     *Region `json:"region,omitempty"`
}
type RampArgs struct {
	Base     Color `json:"base"`
	Steps    int   `json:"steps"`
	HueShift int   `json:"hue_shift,omitempty"`
}

func argumentMap(v any) (map[string]any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var a map[string]any
	err = json.Unmarshal(data, &a)
	return a, err
}

func register[In any](server *mcp.Server, s *Service, name, description string, readOnly bool) {
	mcp.AddTool(server, &mcp.Tool{Name: name, Description: description, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: readOnly}},
		func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, map[string]any, error) {
			a, err := argumentMap(in)
			if err != nil {
				return toolError(err)
			}
			raw, err := s.Execute(ctx, name, a)
			if err != nil {
				return toolError(err)
			}
			out, err := decode[map[string]any](raw)
			if err != nil {
				return toolError(err)
			}
			return nil, out, nil
		})
}

func toolError(err error) (*mcp.CallToolResult, map[string]any, error) {
	failure := &Failure{Code: "operation_failed", Message: err.Error()}
	var specific *Failure
	if errors.As(err, &specific) {
		failure = specific
	}
	out := map[string]any{"error": failure}
	data, _ := json.Marshal(out)
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, out, nil
}

func NewServer(s *Service) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "aseprite-mcp", Version: "0.1.0"}, &mcp.ServerOptions{Instructions: Instructions})
	register[EmptyArgs](server, s, "get_status", "Check backend, connection, Aseprite version and supported capabilities.", true)
	register[EmptyArgs](server, s, "list_documents", "List open artwork documents with handles, revisions, layers and frames.", true)
	register[DocumentArgs](server, s, "get_document", "Inspect current document metadata and revision before editing.", true)
	register[OpenArgs](server, s, "open_document", "Open artwork inside the workspace. Batch mode opens a private working copy.", false)
	register[CreateArgs](server, s, "create_document", "Create unsaved artwork in rgba, indexed, or grayscale color mode.", false)
	register[CloseArgs](server, s, "close_document", "Close artwork; unsaved changes require discard=true.", false)
	register[ReadArgs](server, s, "read_pixels", "Read canvas-global pixels as packed integers: RGBA low-to-high bytes, grayscale gray/alpha bytes, or palette indices.", true)
	register[ReadArgs](server, s, "read_rows", "Read a region as ASCII rows and a mode-specific color legend, ready for a rows edit.", true)
	register[ApplyArgs](server, s, "apply_edits", "Apply an ordered collection of defined edits atomically. See the edits schema and server instructions; each call is one undo step in live mode.", false)
	register[MutationArgs](server, s, "undo", "Undo one native transaction in live mode, using the latest revision.", false)
	register[MutationArgs](server, s, "redo", "Redo one native transaction in live mode, using the latest revision.", false)
	register[SaveArgs](server, s, "save_document", "Explicitly save .aseprite or .ase inside the workspace; overwrite=true is required for an existing destination.", false)
	register[ExportArgs](server, s, "export_document", "Export PNG frames, a native animated GIF, or a PNG sprite sheet plus Aseprite-style JSON without saving the source.", false)
	register[CompareArgs](server, s, "compare_frames", "Compare visible frame pixels; return changed count, percentage and canvas-global bounds (omitted if identical).", true)
	register[EmptyArgs](server, s, "list_palette_presets", "List bundled palette presets, original attribution and RGBA colors.", true)
	register[RampArgs](server, s, "generate_color_ramp", "Generate 2 to 16 dark-to-light RGBA shading colors, optionally shifting hue. Does not edit artwork.", true)
	for _, onion := range []bool{false, true} {
		name, desc := "render_preview", "View selected frames or a cropped region as a labeled PNG contact sheet. Optional pixel grid and canvas coordinate rulers."
		if onion {
			name, desc = "render_onion_skin", "View frames over translucent neighboring frames without changing artwork or editor preferences."
		}
		mcp.AddTool(server, &mcp.Tool{Name: name, Description: desc, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}},
			func(ctx context.Context, _ *mcp.CallToolRequest, a PreviewArgs) (*mcp.CallToolResult, map[string]any, error) {
				data, out, err := s.Render(ctx, a, onion)
				if err != nil {
					return toolError(err)
				}
				metadata, _ := json.Marshal(out)
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.ImageContent{Data: data, MIMEType: "image/png"}, &mcp.TextContent{Text: string(metadata)}}}, out, nil
			})
	}
	return server
}
