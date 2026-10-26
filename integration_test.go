package asepritemcp

import (
	"bytes"
	"context"
	"encoding/json"
	"image/gif"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func asepriteExecutable(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("real Aseprite integration test")
	}
	path := os.Getenv("ASEPRITE_PATH")
	if path == "" {
		path, _ = exec.LookPath("aseprite")
	}
	if path == "" {
		t.Skip("set ASEPRITE_PATH to enable real Aseprite integration tests")
	}
	return path
}

func integrationService(t *testing.T) *Service {
	t.Helper()
	b, err := NewBatch(asepriteExecutable(t), 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close() })
	w, err := OpenWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.root.Close() })
	return NewService(b, w)
}

func execute(t *testing.T, s *Service, op string, input any) json.RawMessage {
	t.Helper()
	a, err := argumentMap(input)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := s.Execute(context.Background(), op, a)
	if err != nil {
		t.Fatalf("%s: %v", op, err)
	}
	return raw
}
func documentResult(t *testing.T, raw json.RawMessage) Document {
	t.Helper()
	doc, err := decode[Document](raw)
	if err != nil {
		t.Fatalf("%s: %v", raw, err)
	}
	return doc
}
func mutate(t *testing.T, s *Service, doc Document, edits ...Edit) Document {
	t.Helper()
	return documentResult(t, execute(t, s, "apply_edits", ApplyArgs{DocumentID: doc.ID, Revision: doc.Revision, Edits: edits}))
}
func read(t *testing.T, s *Service, doc Document, layer string, f int) Pixels {
	t.Helper()
	raw := execute(t, s, "read_pixels", ReadArgs{DocumentID: doc.ID, LayerID: layer, Frame: f})
	p, err := decode[Pixels](raw)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRealArtworkModesAndExplicitSave(t *testing.T) {
	for _, tt := range []struct {
		mode   string
		color  Color
		packed uint32
	}{
		{"rgba", Color{RGBA: []int{255, 0, 0, 128}}, 0x800000ff},
		{"grayscale", Color{GrayAlpha: []int{125, 200}}, 0xc87d},
		{"indexed", Color{Index: func() *int { v := 3; return &v }()}, 3},
	} {
		t.Run(tt.mode, func(t *testing.T) {
			s := integrationService(t)
			doc := documentResult(t, execute(t, s, "create_document", CreateArgs{Width: 4, Height: 4, ColorMode: tt.mode}))
			layer := doc.Layers[0].ID
			doc = mutate(t, s, doc, Edit{Op: "rows", LayerID: layer, Frame: 1, Rows: []string{"AA", "A."}, Legend: map[string]Color{"A": tt.color}})
			p := read(t, s, doc, layer, 1)
			if p.Values[0] != tt.packed || p.Values[1] != tt.packed || p.Values[4] != tt.packed || p.Values[5] != 0 {
				t.Fatalf("wrong pixels: %+v", p)
			}
			entries, err := os.ReadDir(s.Workspace.path)
			if err != nil || len(entries) != 0 {
				t.Fatalf("unsaved artwork wrote source files: %v %v", entries, err)
			}
			// Invalid later edits must roll back an earlier valid pixel edit.
			a, _ := argumentMap(ApplyArgs{DocumentID: doc.ID, Revision: doc.Revision, Edits: []Edit{
				{Op: "erase", LayerID: layer, Frame: 1, Width: 1, Height: 1},
				{Op: "tag_delete", Name: "does-not-exist"},
			}})
			if _, err := s.Execute(context.Background(), "apply_edits", a); err == nil {
				t.Fatal("invalid edit succeeded")
			}
			if got := read(t, s, doc, layer, 1).Values[0]; got != tt.packed {
				t.Fatalf("rollback lost pixel: %x", got)
			}
			doc = mutate(t, s, doc, Edit{Op: "rows", LayerID: layer, Frame: 1, Rows: []string{".~"}})
			if p := read(t, s, doc, layer, 1); p.Values[0] != tt.packed || p.Values[1] != 0 {
				t.Fatalf("skip/erase wrong: %+v", p)
			}
			path := filepath.Join(s.Workspace.path, "art.aseprite")
			doc = documentResult(t, execute(t, s, "save_document", SaveArgs{DocumentID: doc.ID, Revision: doc.Revision, Path: path}))
			if doc.Modified {
				t.Fatal("saved document is dirty")
			}
			reopened := documentResult(t, execute(t, s, "open_document", OpenArgs{Path: path}))
			if reopened.ColorMode != tt.mode || read(t, s, reopened, reopened.Layers[0].ID, 1).Values[0] != tt.packed {
				t.Fatal("save/reopen lost artwork")
			}
			a, _ = argumentMap(SaveArgs{DocumentID: doc.ID, Revision: doc.Revision, Path: path})
			if _, err := s.Execute(context.Background(), "save_document", a); err == nil {
				t.Fatal("overwrote without explicit flag")
			}
			if err := os.WriteFile(path, []byte("external change"), 0600); err != nil {
				t.Fatal(err)
			}
			a["overwrite"] = true
			if _, err := s.Execute(context.Background(), "save_document", a); err == nil {
				t.Fatal("overwrote externally changed source")
			}
		})
	}
}

func TestRealAnimationPreviewAndExports(t *testing.T) {
	s := integrationService(t)
	doc := documentResult(t, execute(t, s, "create_document", CreateArgs{Width: 8, Height: 8}))
	layer := doc.Layers[0].ID
	red := Color{RGBA: []int{255, 0, 0, 255}}
	doc = mutate(t, s, doc,
		Edit{Op: "pixels", LayerID: layer, Frame: 1, Pixels: []Pixel{{X: 2, Y: 3, Color: red}}},
		Edit{Op: "frame_duration", Frame: 1, DurationMS: 80},
		Edit{Op: "frame_create", CopyFrom: 1, DurationMS: 120},
		Edit{Op: "cel_move", LayerID: layer, Frame: 2, X: 1, Y: 0},
		Edit{Op: "tag_set", Name: "walk", Frame: 1, EndFrame: 2, Direction: "forward"})
	diffRaw := execute(t, s, "compare_frames", CompareArgs{DocumentID: doc.ID, Frame: 1, OtherFrame: 2})
	diff, _ := decode[struct {
		Changed int    `json:"changed_pixels"`
		Bounds  Region `json:"changed_bounds"`
	}](diffRaw)
	if diff.Changed != 2 || diff.Bounds.X != 2 || diff.Bounds.Y != 3 || diff.Bounds.Width != 2 {
		t.Fatalf("wrong diff: %s", diffRaw)
	}
	for _, onion := range []bool{false, true} {
		data, _, err := s.Render(context.Background(), PreviewArgs{DocumentID: doc.ID, Frames: []int{1, 2}, Region: &Region{X: 1, Y: 2, Width: 5, Height: 4}, Scale: 4, Grid: true, Rulers: true}, onion)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = png.Decode(bytes.NewReader(data)); err != nil {
			t.Fatal(err)
		}
	}
	if latest := documentResult(t, execute(t, s, "get_document", DocumentArgs{doc.ID})); latest.Revision != doc.Revision {
		t.Fatal("preview changed document")
	}
	for _, format := range []string{"png", "sheet", "gif"} {
		path := filepath.Join(s.Workspace.path, format+".png")
		if format == "gif" {
			path = filepath.Join(s.Workspace.path, "animation.gif")
		}
		execute(t, s, "export_document", ExportArgs{DocumentID: doc.ID, Revision: doc.Revision, Path: path, Format: format, Columns: 2})
		if format == "gif" {
			file, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			animation, err := gif.DecodeAll(file)
			file.Close()
			if err != nil {
				t.Fatal(err)
			}
			if len(animation.Image) != 2 || animation.Delay[0] != 8 || animation.Delay[1] != 12 {
				t.Fatalf("wrong GIF timing: %+v", animation.Delay)
			}
		}
		if format == "sheet" {
			data, _ := os.ReadFile(filepath.Join(s.Workspace.path, "sheet.json"))
			var metadata struct {
				Frames []struct {
					Duration int `json:"duration"`
				} `json:"frames"`
			}
			if err := json.Unmarshal(data, &metadata); err != nil {
				t.Fatal(err)
			}
			if len(metadata.Frames) != 2 || metadata.Frames[0].Duration != 80 || metadata.Frames[1].Duration != 120 {
				t.Fatalf("wrong sheet timing: %s", data)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(s.Workspace.path, "art.aseprite")); !os.IsNotExist(err) {
		t.Fatal("export saved source")
	}
	// Old revisions may not edit the new state.
	old := doc
	doc = mutate(t, s, doc, Edit{Op: "frame_duration", Frame: 2, DurationMS: 150})
	a, _ := argumentMap(ApplyArgs{DocumentID: old.ID, Revision: old.Revision, Edits: []Edit{{Op: "frame_delete", Frame: 2}}})
	if _, err := s.Execute(context.Background(), "apply_edits", a); err == nil {
		t.Fatal("stale edit accepted")
	}
	if doc.Revision == old.Revision {
		t.Fatal("revision unchanged")
	}
}

func TestNativeUndoLinkedCelsAndManualChanges(t *testing.T) {
	exe := asepriteExecutable(t)
	dir := t.TempDir()
	data, _ := assets.ReadFile("assets/dispatcher.lua")
	path := filepath.Join(dir, "dispatcher.lua")
	os.WriteFile(path, data, 0600)
	script := `
local bridge=dofile(app.params.dispatcher)
bridge.batch=false
local function call(op,args)
  local reply=bridge.dispatch{id="test",op=op,args=args}
  assert(reply.ok,reply.error and reply.error.message)
  return reply.result
end
local s=Sprite(4,4)
s.cels[1].image:drawPixel(1,1,app.pixelColor.rgba(255,0,0,255))
app.command.NewFrame{content="cellinked"}
assert(s.layers[1]:cel(1).image.id==s.layers[1]:cel(2).image.id,"fixture is not linked")
bridge.attach("doc",s)
s.layers[1]:cel(1).data="preserve text"
s.layers[1]:cel(1).properties("example.extension",{answer=42})
local before=call("get_document",{document_id="doc"})
local edited=call("apply_edits",{document_id="doc",revision=before.revision,edits={
 {op="pixels",layer_id=before.layers[1].layer_id,frame=2,pixels={{x=1,y=1,color={rgba={0,255,0,255}}}}},
 {op="layer_update",layer_id=before.layers[1].layer_id,name="Edited"}}})
assert(s.layers[1]:cel(1).image:getPixel(1,1)==app.pixelColor.rgba(255,0,0,255),"linked source changed")
assert(s.layers[1]:cel(2).image:getPixel(1,1)==app.pixelColor.rgba(0,255,0,255),"target not edited")
assert(s.layers[1]:cel(2).data=="preserve text","cel text lost")
assert(s.layers[1]:cel(2).properties("example.extension").answer==42,"extension data lost")
local undone=call("undo",{document_id="doc",revision=edited.revision})
assert(s.layers[1].name==before.layers[1].name,"one undo did not roll back entire collection")
assert(s.layers[1]:cel(1).image.id==s.layers[1]:cel(2).image.id,"undo did not restore links")
local redone=call("redo",{document_id="doc",revision=undone.revision})
assert(s.layers[1].name=="Edited")
-- A direct pixel change must be detected even without a transaction event.
s.layers[1]:cel(2).image:drawPixel(0,0,app.pixelColor.rgba(0,0,255,255))
local stale=bridge.dispatch{id="stale",op="undo",args={document_id="doc",revision=redone.revision}}
assert(not stale.ok and stale.error.code=="stale_revision","manual edit not detected")
local latest=call("get_document",{document_id="doc"})
call("snapshot",{document_id="doc",revision=latest.revision,path=app.params.saved})
assert(s.isModified,"snapshot marked document saved")
local saved=call("ack_save",{document_id="doc",revision=latest.revision,path=app.params.saved,display_path=app.params.visible})
assert(not s.isModified and s.filename==app.params.visible,"save acknowledgement failed")
-- Edits must keep hidden off-canvas pixels intact.
local c=s.layers[1]:cel(2);c.position=Point(-1,0)
c.image:drawPixel(0,0,app.pixelColor.rgba(12,34,56,255))
latest=call("get_document",{document_id="doc"})
call("apply_edits",{document_id="doc",revision=latest.revision,edits={
 {op="pixels",layer_id=latest.layers[1].layer_id,frame=2,pixels={{x=2,y=2,color={rgba={255,255,255,255}}}}}}})
c=s.layers[1]:cel(2)
assert(c.position.x==-1 and c.image:getPixel(0,0)==app.pixelColor.rgba(12,34,56,255),"off-canvas pixels lost")
latest=call("get_document",{document_id="doc"})
call("apply_edits",{document_id="doc",revision=latest.revision,edits={{op="frame_move",frame=2,to_frame=1}}})
assert(s.layers[1]:cel(1).properties("example.extension").answer==42,"frame move lost extension data")
latest=call("get_document",{document_id="doc"})
call("apply_edits",{document_id="doc",revision=latest.revision,edits={{op="frame_move",frame=1,to_frame=2}}})
assert(s.layers[1]:cel(2).position.x==-1,"reverse frame move lost cel offset")
s.layers[1].isEditable=false
local locked=bridge.dispatch{id="locked",op="apply_edits",args={document_id="doc",revision=call("get_document",{document_id="doc"}).revision,
 edits={{op="erase",layer_id=latest.layers[1].layer_id,frame=1,width=1,height=1}}}}
assert(not locked.ok,"locked layer accepted")
print("MCP_NATIVE_OK")
`
	scriptPath := filepath.Join(dir, "test.lua")
	os.WriteFile(scriptPath, []byte(script), 0600)
	cmd := exec.Command(exe, "--batch", "--script-param", "dispatcher="+path,
		"--script-param", "saved="+filepath.Join(dir, "snapshot.aseprite"),
		"--script-param", "visible="+filepath.Join(dir, "art.aseprite"), "--script", scriptPath)
	output, err := cmd.CombinedOutput()
	if err != nil || !bytes.Contains(output, []byte("MCP_NATIVE_OK")) {
		t.Fatalf("native integration: %v\n%s", err, output)
	}
}
