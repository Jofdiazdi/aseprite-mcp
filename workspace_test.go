package asepritemcp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceContainment(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	w, err := OpenWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer w.root.Close()
	if err := os.WriteFile(filepath.Join(outside, "source.aseprite"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	symlinks := os.Symlink(outside, filepath.Join(dir, "escape")) == nil
	for _, tt := range []struct {
		name, path string
		reject     bool
	}{
		{"relative output", "new.aseprite", false},
		{"absolute output", filepath.Join(dir, "new.aseprite"), false},
		{"parent escape", filepath.Join("..", filepath.Base(outside), "new.aseprite"), true},
		{"absolute escape", filepath.Join(outside, "source.aseprite"), true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := w.Resolve(tt.path)
			if (err != nil) != tt.reject {
				t.Fatalf("Resolve: %v", err)
			}
		})
	}
	if symlinks {
		for _, path := range []string{"escape/source.aseprite", "escape/new.aseprite"} {
			if _, err := w.Resolve(path); err == nil {
				t.Fatalf("accepted symlink escape %s", path)
			}
		}
	}
	path, err := w.Resolve("art.aseprite")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Write(path, []byte("first")); err != nil {
		t.Fatal(err)
	}
	first, err := w.Hash(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Write(path, []byte("second")); err != nil {
		t.Fatal(err)
	}
	second, _ := w.Hash(path)
	if first == second {
		t.Fatal("hash did not detect replacement")
	}
	data, err := w.Read(path)
	if err != nil || string(data) != "second" {
		t.Fatalf("read: %q, %v", data, err)
	}
}

func TestRowsPreserveTransparencyAndMode(t *testing.T) {
	for _, tt := range []struct {
		mode   string
		values []uint32
	}{
		{"rgba", []uint32{0, 0xff0000ff, 0, 0xff0000ff}},
		{"grayscale", []uint32{0, 0x8080, 0, 0x8080}},
		{"indexed", []uint32{0, 3, 0, 3}},
	} {
		t.Run(tt.mode, func(t *testing.T) {
			raw, err := rows(Pixels{ColorMode: tt.mode, Width: 2, Height: 2, Values: tt.values})
			if err != nil {
				t.Fatal(err)
			}
			result, err := decode[struct {
				Rows   []string         `json:"rows"`
				Legend map[string]Color `json:"legend"`
			}](raw)
			if err != nil {
				t.Fatal(err)
			}
			if result.Rows[0] != "AB" || result.Rows[1] != "AB" || len(result.Legend) != 2 {
				t.Fatalf("incorrect rows: %s", raw)
			}
			if _, ok := result.Legend["."]; ok {
				t.Fatal("transparent pixels must use an explicit color, not skip")
			}
		})
	}
}

func TestRampAndPresets(t *testing.T) {
	for _, p := range PalettePresets() {
		if len(p.Colors) == 0 {
			t.Errorf("empty preset %s", p.ID)
		}
	}
	p, ok := palettePreset("pico-8")
	if !ok || len(p.Colors) != 16 || p.Attribution == "" {
		t.Fatalf("preset attribution missing: %+v", p)
	}
	raw, err := colorRamp(map[string]any{"base": Color{RGBA: []int{100, 150, 200, 128}}, "steps": 4, "hue_shift": 15})
	if err != nil {
		t.Fatal(err)
	}
	ramp, _ := decode[struct {
		Colors []Color `json:"colors"`
	}](raw)
	if len(ramp.Colors) != 4 || ramp.Colors[0].RGBA[3] != 128 || ramp.Colors[0].RGBA[0] >= ramp.Colors[3].RGBA[0] {
		t.Fatalf("invalid ramp: %s", raw)
	}
}
