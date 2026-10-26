package asepritemcp

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"os"
	"path/filepath"
	"strings"
)

func (s *Service) export(ctx context.Context, a map[string]any) (json.RawMessage, error) {
	doc, err := s.document(ctx, a)
	if err != nil {
		return nil, err
	}
	if err = checkRevision(doc, a); err != nil {
		return nil, err
	}
	format := stringArg(a, "format")
	path := stringArg(a, "path")
	overwrite := a["overwrite"] == true
	data, _ := json.Marshal(a["frames"])
	var frames []int
	if err = json.Unmarshal(data, &frames); err != nil {
		return nil, err
	}
	if len(frames) == 0 {
		for _, f := range doc.Frames {
			frames = append(frames, f.Number)
		}
	}
	if len(frames) > 1024 {
		return nil, fmt.Errorf("export at most 1024 frames")
	}
	for _, f := range frames {
		if f < 1 || f > len(doc.Frames) {
			return nil, fmt.Errorf("frame out of range")
		}
	}
	files := map[string][]byte{}
	order := []string{}
	add := func(name string, content []byte) error {
		resolved, err := s.destination(name, overwrite)
		if err != nil {
			return err
		}
		if strings.EqualFold(filepath.Ext(resolved), ".aseprite") || strings.EqualFold(filepath.Ext(resolved), ".ase") {
			return fmt.Errorf("export cannot replace a source document")
		}
		files[resolved] = content
		order = append(order, resolved)
		return nil
	}
	switch format {
	case "gif":
		if strings.ToLower(filepath.Ext(path)) != ".gif" {
			return nil, fmt.Errorf("GIF path must end in .gif")
		}
		if len(frames) != len(doc.Frames) {
			return nil, fmt.Errorf("GIF exports the complete timeline; omit frames or use PNG")
		}
		for i, f := range frames {
			if f != i+1 {
				return nil, fmt.Errorf("GIF frames must be the complete timeline in order")
			}
		}
		if _, err = s.destination(path, overwrite); err != nil {
			return nil, err
		}
		dir, err := os.MkdirTemp("", "aseprite-mcp-export-")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(dir)
		stage := filepath.Join(dir, "animation.gif")
		_, err = s.Backend.Call(ctx, "export_gif", map[string]any{"document_id": doc.ID, "revision": doc.Revision, "path": stage})
		if err != nil {
			return nil, err
		}
		content, err := os.ReadFile(stage)
		if err != nil {
			return nil, err
		}
		if err = add(path, content); err != nil {
			return nil, err
		}
	case "png", "sheet":
		if strings.ToLower(filepath.Ext(path)) != ".png" {
			return nil, fmt.Errorf("PNG path must end in .png")
		}
		if int64(doc.Width)*int64(doc.Height)*int64(len(frames)) > 16_000_000 {
			return nil, fmt.Errorf("export too large; select fewer frames")
		}
		images := make([]*image.NRGBA, len(frames))
		for i, f := range frames {
			p, err := s.pixels(ctx, doc.ID, "", f, nil)
			if err != nil {
				return nil, err
			}
			images[i], err = pixelImage(p)
			if err != nil {
				return nil, err
			}
		}
		if format == "png" {
			for i, img := range images {
				name := path
				if len(frames) > 1 {
					name = strings.TrimSuffix(path, filepath.Ext(path)) + fmt.Sprintf("-%04d.png", frames[i])
				}
				content, err := encodePNG(img)
				if err != nil {
					return nil, err
				}
				if err = add(name, content); err != nil {
					return nil, err
				}
			}
		} else {
			columns := number(a["columns"])
			if columns == 0 {
				columns = len(frames)
			}
			if columns < 1 || columns > 1024 {
				return nil, fmt.Errorf("columns must be 1 to 1024")
			}
			columns = min(columns, len(frames))
			rows := (len(frames) + columns - 1) / columns
			if int64(doc.Width)*int64(columns)*int64(doc.Height)*int64(rows) > 16_000_000 {
				return nil, fmt.Errorf("sheet too large")
			}
			sheet := image.NewNRGBA(image.Rect(0, 0, columns*doc.Width, rows*doc.Height))
			entries := []any{}
			for i, img := range images {
				x, y := (i%columns)*doc.Width, (i/columns)*doc.Height
				draw.Draw(sheet, image.Rect(x, y, x+doc.Width, y+doc.Height), img, image.Point{}, draw.Src)
				entries = append(entries, map[string]any{"filename": fmt.Sprintf("frame-%04d", frames[i]), "frame": map[string]int{"x": x, "y": y, "w": doc.Width, "h": doc.Height}, "rotated": false, "trimmed": false,
					"spriteSourceSize": map[string]int{"x": 0, "y": 0, "w": doc.Width, "h": doc.Height}, "sourceSize": map[string]int{"w": doc.Width, "h": doc.Height}, "duration": doc.Frames[frames[i]-1].DurationMS})
			}
			content, err := encodePNG(sheet)
			if err != nil {
				return nil, err
			}
			if err = add(path, content); err != nil {
				return nil, err
			}
			// Tags refer to sheet frame indices. Omit tags whose source range is
			// not present contiguously, rather than writing misleading metadata.
			tags := []any{}
			for _, tag := range doc.Tags {
				for i, f := range frames {
					if f != tag.From {
						continue
					}
					valid := i+tag.To-tag.From < len(frames)
					for j := 0; valid && j <= tag.To-tag.From; j++ {
						valid = frames[i+j] == tag.From+j
					}
					if valid {
						tags = append(tags, map[string]any{"name": tag.Name, "from": i, "to": i + tag.To - tag.From, "direction": tag.Direction})
					}
				}
			}
			metadata, _ := json.MarshalIndent(map[string]any{"frames": entries, "meta": map[string]any{"app": "aseprite-mcp", "image": filepath.Base(path), "format": "RGBA8888", "size": map[string]int{"w": sheet.Bounds().Dx(), "h": sheet.Bounds().Dy()}, "scale": "1", "frameTags": tags}}, "", "  ")
			if err = add(strings.TrimSuffix(path, filepath.Ext(path))+".json", metadata); err != nil {
				return nil, err
			}
		}
	default:
		return nil, fmt.Errorf("format must be png, gif, or sheet")
	}
	latest, err := s.document(ctx, a)
	if err != nil {
		return nil, err
	}
	if latest.Revision != doc.Revision {
		return nil, &Failure{Code: "stale_revision", Message: "document changed during export; no output files committed"}
	}
	written := []string{}
	for _, name := range order {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := s.Workspace.Write(name, files[name]); err != nil {
			return nil, fmt.Errorf("export failed after writing %v: %w", written, err)
		}
		written = append(written, name)
	}
	return json.Marshal(map[string]any{"files": written, "revision": doc.Revision})
}
