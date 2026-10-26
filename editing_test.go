package asepritemcp

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRealDrawingAndLayerOperations(t *testing.T) {
	s := integrationService(t)
	doc := documentResult(t, execute(t, s, "create_document", CreateArgs{Width: 8, Height: 8}))
	red := Color{RGBA: []int{255, 0, 0, 255}}
	pos := 1
	opacity := 128
	visible := false
	doc = mutate(t, s, doc,
		Edit{Op: "layer_create", Name: "Shapes", Ref: "shapes"},
		Edit{Op: "layer_update", LayerID: "shapes", Position: &pos, Opacity: &opacity},
		Edit{Op: "rectangle", LayerID: "shapes", Frame: 1, X: 1, Y: 1, Width: 6, Height: 6, Color: &red, Filled: true},
		Edit{Op: "erase", LayerID: "shapes", Frame: 1, X: 2, Y: 2, Width: 4, Height: 4},
		Edit{Op: "fill", LayerID: "shapes", Frame: 1, X: 3, Y: 3, Color: &red})
	layer := doc.Layers[0].ID
	if doc.Layers[0].Name != "Shapes" || doc.Layers[0].Opacity != 128 {
		t.Fatalf("layer update failed: %+v", doc.Layers)
	}
	p := read(t, s, doc, layer, 1)
	count := 0
	for _, v := range p.Values {
		if v != 0 {
			count++
		}
	}
	if count != 36 {
		t.Fatalf("rectangle/fill wrong: %d", count)
	}
	doc = mutate(t, s, doc, Edit{Op: "erase", LayerID: layer, Frame: 1, Width: 8, Height: 8},
		Edit{Op: "line", LayerID: layer, Frame: 1, X: 0, Y: 0, X2: 7, Y2: 7, Color: &red})
	p = read(t, s, doc, layer, 1)
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			if (p.Values[y*8+x] != 0) != (x == y) {
				t.Fatalf("line wrong at %d,%d", x, y)
			}
		}
	}
	doc = mutate(t, s, doc, Edit{Op: "cel_flip", LayerID: layer, Frame: 1, Axis: "horizontal"})
	p = read(t, s, doc, layer, 1)
	if p.Values[7] == 0 || p.Values[0] != 0 {
		t.Fatal("flip failed")
	}
	doc = mutate(t, s, doc, Edit{Op: "erase", LayerID: layer, Frame: 1, Width: 8, Height: 8},
		Edit{Op: "ellipse", LayerID: layer, Frame: 1, X: 1, Y: 2, Width: 6, Height: 4, Color: &red, Filled: true},
		Edit{Op: "stroke", LayerID: layer, Frame: 1, Color: &red, Points: []Point{{X: 0, Y: 0}, {X: 7, Y: 0}, {X: 7, Y: 7}}})
	p = read(t, s, doc, layer, 1)
	if p.Values[0] == 0 || p.Values[7] == 0 || p.Values[7*8+7] == 0 || p.Values[3*8+3] == 0 {
		t.Fatal("ellipse or stroke missing")
	}
	doc = mutate(t, s, doc, Edit{Op: "layer_update", LayerID: layer, Visible: &visible})
	composite := read(t, s, doc, "", 1)
	for _, v := range composite.Values {
		if v>>24 != 0 {
			t.Fatal("hidden layer rendered")
		}
	}
	doc = mutate(t, s, doc, Edit{Op: "layer_delete", LayerID: layer})
	if len(doc.Layers) != 1 {
		t.Fatal("layer delete failed")
	}
}

func TestRealFrameReorderingAndPalette(t *testing.T) {
	s := integrationService(t)
	doc := documentResult(t, execute(t, s, "create_document", CreateArgs{Width: 4, Height: 4, ColorMode: "indexed"}))
	layer := doc.Layers[0].ID
	index := 3
	c := Color{Index: &index}
	doc = mutate(t, s, doc, Edit{Op: "palette_preset", Preset: "pico-8"},
		Edit{Op: "pixels", LayerID: layer, Frame: 1, Pixels: []Pixel{{X: 1, Y: 1, Color: c}}},
		Edit{Op: "frame_duration", Frame: 1, DurationMS: 70},
		Edit{Op: "frame_create", DurationMS: 130}, Edit{Op: "frame_create", DurationMS: 200},
		Edit{Op: "cel_copy", LayerID: layer, Frame: 1, ToFrame: 2, EndFrame: 3},
		Edit{Op: "erase", LayerID: layer, Frame: 2, X: 1, Y: 1, Width: 1, Height: 1},
		Edit{Op: "frame_move", Frame: 1, ToFrame: 3})
	if doc.Frames[0].DurationMS != 130 || doc.Frames[1].DurationMS != 200 || doc.Frames[2].DurationMS != 70 {
		t.Fatalf("durations not reordered: %+v", doc.Frames)
	}
	if read(t, s, doc, layer, 1).Values[5] != 0 || read(t, s, doc, layer, 3).Values[5] != 3 {
		t.Fatal("contents not reordered")
	}
	if len(doc.Palettes[0].Colors) != 16 {
		t.Fatal("palette preset not applied")
	}
	before := doc.Revision
	a, _ := argumentMap(ApplyArgs{DocumentID: doc.ID, Revision: doc.Revision, Edits: []Edit{{Op: "palette_set", Colors: []Color{{RGBA: []int{0, 0, 0, 255}}}}}})
	if _, err := s.Execute(context.Background(), "apply_edits", a); err == nil {
		t.Fatal("palette removed used index")
	}
	if latest := documentResult(t, execute(t, s, "get_document", DocumentArgs{doc.ID})); latest.Revision != before {
		t.Fatal("palette failure changed document")
	}
	doc = mutate(t, s, doc, Edit{Op: "tag_set", Name: "test", Frame: 1, EndFrame: 3, Direction: "reverse"}, Edit{Op: "tag_delete", Name: "test"}, Edit{Op: "frame_delete", Frame: 2})
	if len(doc.Frames) != 2 || len(doc.Tags) != 0 {
		t.Fatal("tag/frame deletion failed")
	}
}

func TestRealRowsValidationAndDocumentClosing(t *testing.T) {
	s := integrationService(t)
	doc := documentResult(t, execute(t, s, "create_document", CreateArgs{Width: 4, Height: 4}))
	layer := doc.Layers[0].ID
	red := Color{RGBA: []int{255, 0, 0, 255}}
	doc = mutate(t, s, doc, Edit{Op: "rows", LayerID: layer, Frame: 1, Rows: []string{"AA", "A."}, Legend: map[string]Color{"A": red}})
	raw := execute(t, s, "read_rows", ReadArgs{DocumentID: doc.ID, LayerID: layer, Frame: 1})
	var result struct {
		Rows   []string         `json:"rows"`
		Legend map[string]Color `json:"legend"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	before := read(t, s, doc, layer, 1)
	doc = mutate(t, s, doc, Edit{Op: "rows", LayerID: layer, Frame: 1, Rows: result.Rows, Legend: result.Legend})
	after := read(t, s, doc, layer, 1)
	for i, v := range before.Values {
		if v != after.Values[i] {
			t.Fatal("row round trip lost pixels")
		}
	}
	for _, rows := range [][]string{{"bad"}, {"A", "AA"}} {
		a, _ := argumentMap(ApplyArgs{DocumentID: doc.ID, Revision: doc.Revision, Edits: []Edit{{Op: "rows", LayerID: layer, Frame: 1, Rows: rows, Legend: map[string]Color{"A": red}}}})
		if _, err := s.Execute(context.Background(), "apply_edits", a); err == nil {
			t.Fatalf("accepted invalid rows %v", rows)
		}
	}
	a, _ := argumentMap(CloseArgs{DocumentID: doc.ID, Revision: doc.Revision})
	if _, err := s.Execute(context.Background(), "close_document", a); err == nil {
		t.Fatal("closed unsaved artwork without discard")
	}
	execute(t, s, "close_document", CloseArgs{DocumentID: doc.ID, Revision: doc.Revision, Discard: true})
	a, _ = argumentMap(DocumentArgs{doc.ID})
	if _, err := s.Execute(context.Background(), "get_document", a); err == nil {
		t.Fatal("closed handle remains usable")
	}
}
