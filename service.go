package asepritemcp

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type source struct{ path, hash string }

type Service struct {
	mu        sync.Mutex
	Backend   Backend
	Workspace *Workspace
	sources   map[string]source
}

func NewService(backend Backend, workspace *Workspace) *Service {
	return &Service{Backend: backend, Workspace: workspace, sources: map[string]source{}}
}

func (s *Service) remember(doc Document) {
	if _, ok := s.sources[doc.ID]; ok {
		return
	}
	if doc.Path != "" {
		p, err := s.Workspace.Resolve(doc.Path)
		if err == nil {
			hash, err := s.Workspace.Hash(p)
			if err == nil {
				s.sources[doc.ID] = source{p, hash}
			}
		}
	}
}

// Execute serializes the complete operation, including validation and exports.
// The Lua dispatcher also checks revisions on the UI thread, after queueing.
func (s *Service) Execute(ctx context.Context, op string, a map[string]any) (json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch op {
	case "get_status":
		raw, err := s.Backend.Call(ctx, op, a)
		if err != nil {
			return nil, err
		}
		status, err := decode[map[string]any](raw)
		if err != nil {
			return nil, err
		}
		status["mode"] = s.Backend.Mode()
		status["workspace"] = s.Workspace.path
		status["capabilities"] = map[string]bool{"undo": s.Backend.Mode() == "live", "redo": s.Backend.Mode() == "live", "explicit_save": true}
		return json.Marshal(status)
	case "list_palette_presets":
		return json.Marshal(map[string]any{"presets": PalettePresets()})
	case "generate_color_ramp":
		return colorRamp(a)
	case "create_document":
		a["document_id"] = "doc-" + randomID()
	case "open_document":
		return s.open(ctx, a)
	case "save_document":
		return s.save(ctx, a)
	case "export_document":
		return s.export(ctx, a)
	case "read_rows":
		raw, err := s.Backend.Call(ctx, "read_pixels", a)
		if err != nil {
			return nil, err
		}
		p, err := decode[Pixels](raw)
		if err != nil {
			return nil, err
		}
		return rows(p)
	case "compare_frames":
		return s.compare(ctx, a)
	case "apply_edits":
		data, err := json.Marshal(a["edits"])
		if err != nil {
			return nil, err
		}
		var edits []Edit
		if err = json.Unmarshal(data, &edits); err != nil {
			return nil, err
		}
		for i := range edits {
			if edits[i].Op == "palette_preset" {
				p, ok := palettePreset(edits[i].Preset)
				if !ok {
					return nil, fmt.Errorf("unknown palette preset %q; call list_palette_presets", edits[i].Preset)
				}
				edits[i].Colors = p.Colors
			}
		}
		a["edits"] = edits
	}
	raw, err := s.Backend.Call(ctx, op, a)
	if err != nil {
		return nil, err
	}
	if op == "get_document" || op == "open_document" || op == "create_document" {
		doc, err := decode[Document](raw)
		if err != nil {
			return nil, err
		}
		s.remember(doc)
	}
	if op == "list_documents" {
		list, err := decode[struct {
			Documents []Document `json:"documents"`
		}](raw)
		if err != nil {
			return nil, err
		}
		for _, doc := range list.Documents {
			s.remember(doc)
		}
	}
	if op == "close_document" {
		delete(s.sources, stringArg(a, "document_id"))
	}
	return raw, nil
}

func stringArg(a map[string]any, key string) string { v, _ := a[key].(string); return v }

func (s *Service) open(ctx context.Context, a map[string]any) (json.RawMessage, error) {
	path, err := s.Workspace.Resolve(stringArg(a, "path"))
	if err != nil {
		return nil, err
	}
	a["path"] = path
	a["document_id"] = "doc-" + randomID()
	var initialHash string
	if b, ok := s.Backend.(*Batch); ok {
		data, err := s.Workspace.Read(path)
		if err != nil {
			return nil, err
		}
		initialHash = fmt.Sprintf("%x", sha256.Sum256(data))
		dir, err := os.MkdirTemp("", "aseprite-mcp-open-")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(dir)
		stage := filepath.Join(dir, "input"+filepath.Ext(path))
		if err = os.WriteFile(stage, data, 0600); err != nil {
			return nil, err
		}
		a["path"] = stage
		raw, err := b.Call(ctx, "open_document", a)
		if err != nil {
			return nil, err
		}
		doc, err := decode[Document](raw)
		if err != nil {
			return nil, err
		}
		b.mu.Lock()
		b.docs[doc.ID].path = path
		b.mu.Unlock()
		doc.Path = path
		s.sources[doc.ID] = source{path, initialHash}
		return json.Marshal(doc)
	}
	initialHash, err = s.Workspace.Hash(path)
	if err != nil {
		return nil, err
	}
	raw, err := s.Backend.Call(ctx, "open_document", a)
	if err != nil {
		return nil, err
	}
	doc, err := decode[Document](raw)
	if err != nil {
		return nil, err
	}
	s.sources[doc.ID] = source{path, initialHash}
	return raw, nil
}

func (s *Service) document(ctx context.Context, a map[string]any) (Document, error) {
	raw, err := s.Backend.Call(ctx, "get_document", map[string]any{"document_id": a["document_id"]})
	if err != nil {
		return Document{}, err
	}
	return decode[Document](raw)
}

func checkRevision(doc Document, a map[string]any) error {
	if stringArg(a, "revision") == "" || stringArg(a, "revision") != doc.Revision {
		return &Failure{Code: "stale_revision", Message: "get_document and use its current revision"}
	}
	return nil
}

func (s *Service) destination(path string, overwrite bool) (string, error) {
	p, err := s.Workspace.Resolve(path)
	if err != nil {
		return "", err
	}
	hash, err := s.Workspace.Hash(p)
	if err != nil {
		return "", err
	}
	if hash != "" && !overwrite {
		return "", &Failure{Code: "overwrite_required", Message: "destination exists; set overwrite=true explicitly"}
	}
	return p, nil
}

func (s *Service) save(ctx context.Context, a map[string]any) (json.RawMessage, error) {
	doc, err := s.document(ctx, a)
	if err != nil {
		return nil, err
	}
	if err = checkRevision(doc, a); err != nil {
		return nil, err
	}
	path, err := s.destination(stringArg(a, "path"), a["overwrite"] == true)
	if err != nil {
		return nil, err
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".aseprite" && ext != ".ase" {
		return nil, fmt.Errorf("save requires .aseprite or .ase; use export_document")
	}
	if src, ok := s.sources[doc.ID]; ok && src.path == path {
		hash, err := s.Workspace.Hash(path)
		if err != nil {
			return nil, err
		}
		if hash != src.hash {
			return nil, &Failure{Code: "source_changed", Message: "source changed outside this session; reopen it or save to another path"}
		}
	}
	dir, err := os.MkdirTemp("", "aseprite-mcp-save-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	stage := filepath.Join(dir, "saved.aseprite")
	_, err = s.Backend.Call(ctx, "snapshot", map[string]any{"document_id": doc.ID, "revision": doc.Revision, "path": stage})
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(stage)
	if err != nil {
		return nil, err
	}
	latest, err := s.document(ctx, a)
	if err != nil {
		return nil, err
	}
	if latest.Revision != doc.Revision {
		return nil, &Failure{Code: "stale_revision", Message: "document changed while preparing save; no source file written"}
	}
	// Recheck the destination after snapshotting, which can take time in the UI.
	if _, err = s.destination(path, a["overwrite"] == true); err != nil {
		return nil, err
	}
	if src, ok := s.sources[doc.ID]; ok && src.path == path {
		hash, err := s.Workspace.Hash(path)
		if err != nil {
			return nil, err
		}
		if hash != src.hash {
			return nil, &Failure{Code: "source_changed", Message: "source changed while preparing save; save to another path"}
		}
	}
	if err = s.Workspace.Write(path, data); err != nil {
		return nil, err
	}
	s.sources[doc.ID] = source{path, fmt.Sprintf("%x", sha256.Sum256(data))}
	if batch, ok := s.Backend.(*Batch); ok {
		batch.mu.Lock()
		batch.docs[doc.ID].path = path
		batch.docs[doc.ID].dirty = false
		batch.mu.Unlock()
		doc.Path = path
		doc.Modified = false
	} else {
		raw, err := s.Backend.Call(ctx, "ack_save", map[string]any{"document_id": doc.ID, "revision": doc.Revision, "path": stage, "display_path": path})
		if err != nil {
			return nil, &Failure{Code: "save_acknowledgement_failed", Message: fmt.Sprintf("snapshot %s was saved to %s; native saved state could not be confirmed: %v; inspect before retrying", doc.Revision, path, err), Uncertain: true}
		}
		doc, err = decode[Document](raw)
		if err != nil {
			return nil, err
		}
	}
	hash, err := s.Workspace.Hash(path)
	if err != nil {
		return nil, err
	}
	if hash == "" {
		return nil, fmt.Errorf("Aseprite did not write the destination")
	}
	s.sources[doc.ID] = source{path, hash}
	return json.Marshal(doc)
}

func rows(p Pixels) (json.RawMessage, error) {
	const symbols = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789!#$%&()*+,-/:;<=>?@[]^_{}|"
	if len(p.Values) != p.Width*p.Height {
		return nil, fmt.Errorf("invalid pixel response dimensions")
	}
	legend := map[string]Color{}
	seen := map[uint32]string{}
	out := make([]string, p.Height)
	for y := 0; y < p.Height; y++ {
		var row strings.Builder
		for x := 0; x < p.Width; x++ {
			v := p.Values[y*p.Width+x]
			ch, ok := seen[v]
			if !ok {
				if len(seen) >= len(symbols) {
					return nil, fmt.Errorf("region has too many colors for character rows; use read_pixels")
				}
				ch = string(symbols[len(seen)])
				seen[v] = ch
				legend[ch] = unpackColor(p.ColorMode, v)
			}
			row.WriteString(ch)
		}
		out[y] = row.String()
	}
	return json.Marshal(map[string]any{"x": p.X, "y": p.Y, "color_mode": p.ColorMode, "rows": out, "legend": legend, "skip_symbol": ".", "erase_symbol": "~"})
}

func unpackColor(mode string, v uint32) Color {
	switch mode {
	case "indexed":
		i := int(v)
		return Color{Index: &i}
	case "grayscale":
		return Color{GrayAlpha: []int{int(v & 255), int(v >> 8 & 255)}}
	}
	return Color{RGBA: []int{int(v & 255), int(v >> 8 & 255), int(v >> 16 & 255), int(v >> 24 & 255)}}
}
