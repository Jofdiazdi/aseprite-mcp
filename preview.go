package asepritemcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

type PreviewArgs struct {
	DocumentID   string  `json:"document_id"`
	Frames       []int   `json:"frames,omitempty" jsonschema:"Frame numbers; default first frame; up to 64"`
	LayerID      string  `json:"layer_id,omitempty"`
	Region       *Region `json:"region,omitempty"`
	Scale        int     `json:"scale,omitempty" jsonschema:"Integer nearest-neighbor enlargement, 1 to 32; default 4"`
	Grid         bool    `json:"grid,omitempty"`
	Rulers       bool    `json:"rulers,omitempty"`
	Columns      int     `json:"columns,omitempty" jsonschema:"Contact sheet columns; default 4"`
	Before       int     `json:"before,omitempty"`
	After        int     `json:"after,omitempty"`
	GhostOpacity *int    `json:"ghost_opacity,omitempty" jsonschema:"Onion skin opacity from 0 to 255; default 96"`
}

func (s *Service) pixels(ctx context.Context, id, layer string, frame int, region *Region) (Pixels, error) {
	a := map[string]any{"document_id": id, "frame": frame, "composite": true}
	if layer != "" {
		a["layer_id"] = layer
	}
	if region != nil {
		a["region"] = region
	}
	raw, err := s.Backend.Call(ctx, "read_pixels", a)
	if err != nil {
		return Pixels{}, err
	}
	return decode[Pixels](raw)
}

func pixelImage(p Pixels) (*image.NRGBA, error) {
	if p.ColorMode != "rgba" || p.Width < 1 || p.Height < 1 || len(p.Values) != p.Width*p.Height {
		return nil, fmt.Errorf("invalid composite pixels")
	}
	img := image.NewNRGBA(image.Rect(0, 0, p.Width, p.Height))
	for i, v := range p.Values {
		j := i * 4
		img.Pix[j] = byte(v)
		img.Pix[j+1] = byte(v >> 8)
		img.Pix[j+2] = byte(v >> 16)
		img.Pix[j+3] = byte(v >> 24)
	}
	return img, nil
}

func encodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	err := png.Encode(&buf, img)
	return buf.Bytes(), err
}

func (s *Service) Render(ctx context.Context, a PreviewArgs, onion bool) ([]byte, map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.document(ctx, map[string]any{"document_id": a.DocumentID})
	if err != nil {
		return nil, nil, err
	}
	if a.Scale == 0 {
		a.Scale = 4
	}
	if a.Columns == 0 {
		a.Columns = 4
	}
	if a.Scale < 1 || a.Scale > 32 || a.Columns < 1 || a.Columns > 16 {
		return nil, nil, fmt.Errorf("scale must be 1 to 32 and columns 1 to 16")
	}
	if len(a.Frames) == 0 {
		a.Frames = []int{1}
	}
	if len(a.Frames) > 64 {
		return nil, nil, fmt.Errorf("preview at most 64 frames")
	}
	if a.Before < 0 || a.Before > 8 || a.After < 0 || a.After > 8 {
		return nil, nil, fmt.Errorf("onion neighbors must be from 0 to 8")
	}
	if a.Region == nil {
		a.Region = &Region{Width: doc.Width, Height: doc.Height}
	}
	if a.Region.Width < 1 || a.Region.Height < 1 || a.Region.Width*a.Region.Height > 262144 {
		return nil, nil, fmt.Errorf("preview region must contain 1 to 262144 pixels")
	}
	marginX, marginY := 0, 0
	if a.Rulers {
		marginX, marginY = 44, 22
	}
	tileW, tileH := a.Region.Width*a.Scale+marginX, a.Region.Height*a.Scale+marginY+20
	cols := min(a.Columns, len(a.Frames))
	rows := (len(a.Frames) + cols - 1) / cols
	w, h := cols*tileW, rows*tileH
	if int64(w)*int64(h) > 16_000_000 {
		return nil, nil, fmt.Errorf("preview too large; reduce region, scale, or frames")
	}
	result := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(result, result.Bounds(), image.NewUniform(color.NRGBA{28, 28, 32, 255}), image.Point{}, draw.Src)
	ghost := 96
	if a.GhostOpacity != nil {
		ghost = *a.GhostOpacity
	}
	if ghost < 0 || ghost > 255 {
		return nil, nil, fmt.Errorf("ghost_opacity must be 0 to 255")
	}
	for i, f := range a.Frames {
		if f < 1 || f > len(doc.Frames) {
			return nil, nil, fmt.Errorf("frame out of range")
		}
		p, err := s.pixels(ctx, a.DocumentID, a.LayerID, f, a.Region)
		if err != nil {
			return nil, nil, err
		}
		img, err := pixelImage(p)
		if err != nil {
			return nil, nil, err
		}
		if onion {
			composite := image.NewNRGBA(img.Bounds())
			before, after := a.Before, a.After
			if before == 0 && after == 0 {
				before, after = 1, 1
			}
			for n := max(1, f-before); n <= min(len(doc.Frames), f+after); n++ {
				if n == f {
					continue
				}
				p, err := s.pixels(ctx, a.DocumentID, a.LayerID, n, a.Region)
				if err != nil {
					return nil, nil, err
				}
				neighbor, err := pixelImage(p)
				if err != nil {
					return nil, nil, err
				}
				for j := 3; j < len(neighbor.Pix); j += 4 {
					neighbor.Pix[j] = byte(int(neighbor.Pix[j]) * ghost / 255)
				}
				draw.Draw(composite, composite.Bounds(), neighbor, image.Point{}, draw.Over)
			}
			draw.Draw(composite, composite.Bounds(), img, image.Point{}, draw.Over)
			img = composite
		}
		x0, y0 := (i%cols)*tileW, (i/cols)*tileH
		for y := 0; y < img.Bounds().Dy()*a.Scale; y++ {
			for x := 0; x < img.Bounds().Dx()*a.Scale; x++ {
				result.SetNRGBA(x0+marginX+x, y0+marginY+y, img.NRGBAAt(x/a.Scale, y/a.Scale))
			}
		}
		if a.Grid && a.Scale >= 3 {
			gridColor := color.NRGBA{110, 110, 120, 100}
			for y := 0; y < img.Bounds().Dy()*a.Scale; y++ {
				for x := 0; x < img.Bounds().Dx()*a.Scale; x++ {
					if x%a.Scale == 0 || y%a.Scale == 0 {
						result.SetNRGBA(x0+marginX+x, y0+marginY+y, gridColor)
					}
				}
			}
		}
		if a.Rulers {
			step := max(1, 48/a.Scale)
			for x := 0; x < a.Region.Width; x += step {
				label(result, x0+marginX+x*a.Scale, y0+14, fmt.Sprint(a.Region.X+x))
			}
			for y := 0; y < a.Region.Height; y += step {
				label(result, x0+2, y0+marginY+y*a.Scale+12, fmt.Sprint(a.Region.Y+y))
			}
		}
		label(result, x0+3, y0+tileH-5, fmt.Sprintf("Frame %d / %d ms", f, doc.Frames[f-1].DurationMS))
	}
	latest, err := s.document(ctx, map[string]any{"document_id": doc.ID})
	if err != nil {
		return nil, nil, err
	}
	if latest.Revision != doc.Revision {
		return nil, nil, &Failure{Code: "stale_revision", Message: "document changed while rendering; inspect and render again"}
	}
	data, err := encodePNG(result)
	return data, map[string]any{"document_id": doc.ID, "revision": doc.Revision, "frames": a.Frames, "region": a.Region, "scale": a.Scale}, err
}

func label(img draw.Image, x, y int, text string) {
	d := font.Drawer{Dst: img, Src: image.NewUniform(color.NRGBA{235, 235, 240, 255}), Face: basicfont.Face7x13, Dot: fixed.P(x, y)}
	d.DrawString(text)
}

func (s *Service) compare(ctx context.Context, a map[string]any) (json.RawMessage, error) {
	id := stringArg(a, "document_id")
	doc, err := s.document(ctx, a)
	if err != nil {
		return nil, err
	}
	var region *Region
	if a["region"] != nil {
		data, _ := json.Marshal(a["region"])
		region = &Region{}
		if err := json.Unmarshal(data, region); err != nil {
			return nil, err
		}
	}
	p, err := s.pixels(ctx, id, stringArg(a, "layer_id"), number(a["frame"]), region)
	if err != nil {
		return nil, err
	}
	q, err := s.pixels(ctx, id, stringArg(a, "layer_id"), number(a["other_frame"]), region)
	if err != nil {
		return nil, err
	}
	if p.Width != q.Width || p.Height != q.Height || len(p.Values) != len(q.Values) {
		return nil, fmt.Errorf("frame dimensions differ")
	}
	count := 0
	minX, minY, maxX, maxY := p.Width, p.Height, -1, -1
	for i, v := range p.Values {
		other := q.Values[i]
		// Fully transparent RGB values are visually equivalent.
		if v>>24 == 0 && other>>24 == 0 {
			continue
		}
		if v != other {
			count++
			x, y := i%p.Width, i/p.Width
			minX = min(minX, x)
			minY = min(minY, y)
			maxX = max(maxX, x)
			maxY = max(maxY, y)
		}
	}
	result := map[string]any{"document_id": id, "revision": doc.Revision, "changed_pixels": count, "total_pixels": len(p.Values), "changed_percent": math.Round(float64(count)*10000/float64(len(p.Values))) / 100}
	if count > 0 {
		result["changed_bounds"] = Region{X: p.X + minX, Y: p.Y + minY, Width: maxX - minX + 1, Height: maxY - minY + 1}
	}
	latest, err := s.document(ctx, a)
	if err != nil {
		return nil, err
	}
	if latest.Revision != doc.Revision {
		return nil, &Failure{Code: "stale_revision", Message: "document changed during comparison"}
	}
	return json.Marshal(result)
}
