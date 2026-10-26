// Package asepritemcp exposes Aseprite artwork through the Model Context Protocol.
package asepritemcp

import (
	"context"
	"encoding/json"
	"fmt"
)

const ProtocolVersion = 1

type Request struct {
	ID   string         `json:"id"`
	Op   string         `json:"op"`
	Args map[string]any `json:"args"`
}

type Reply struct {
	ID     string          `json:"id"`
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Failure        `json:"error,omitempty"`
}

type Failure struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Uncertain bool   `json:"uncertain,omitempty"`
}

func (e *Failure) Error() string { return e.Code + ": " + e.Message }

type Backend interface {
	Call(context.Context, string, map[string]any) (json.RawMessage, error)
	Mode() string
	Close() error
}

type Document struct {
	ID        string    `json:"document_id"`
	Revision  string    `json:"revision"`
	Width     int       `json:"width"`
	Height    int       `json:"height"`
	ColorMode string    `json:"color_mode"`
	Modified  bool      `json:"modified"`
	Path      string    `json:"path,omitempty"`
	Layers    []Layer   `json:"layers"`
	Frames    []Frame   `json:"frames"`
	Palettes  []Palette `json:"palettes"`
	Tags      []Tag     `json:"tags"`
}

type Layer struct {
	ID       string `json:"layer_id"`
	Name     string `json:"name"`
	ParentID string `json:"parent_id,omitempty"`
	Kind     string `json:"kind"`
	Visible  bool   `json:"visible"`
	Locked   bool   `json:"locked"`
	Opacity  int    `json:"opacity"`
}

type Frame struct {
	Number     int `json:"number"`
	DurationMS int `json:"duration_ms"`
}

type Palette struct {
	Frame  int     `json:"frame"`
	Colors [][]int `json:"colors"`
}

type Tag struct {
	Name      string `json:"name"`
	From      int    `json:"from"`
	To        int    `json:"to"`
	Direction string `json:"direction"`
}

type Color struct {
	RGBA      []int `json:"rgba,omitempty" jsonschema:"RGBA channels from 0 to 255; use only in rgba documents"`
	GrayAlpha []int `json:"gray_alpha,omitempty" jsonschema:"Gray and alpha from 0 to 255; use only in grayscale documents"`
	Index     *int  `json:"index,omitempty" jsonschema:"Palette index; use only in indexed documents"`
}

type Point struct {
	X int `json:"x"`
	Y int `json:"y"`
}
type Pixel struct {
	X     int   `json:"x"`
	Y     int   `json:"y"`
	Color Color `json:"color"`
}
type Region struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Edit is a tagged union. The dispatcher validates the fields required by each op.
// Layer handles returned by layer_create can be referenced by ref later in a batch.
type Edit struct {
	Op         string           `json:"op" jsonschema:"One of pixels, rows, stroke, line, rectangle, ellipse, fill, erase, layer_create, layer_update, layer_delete, frame_create, frame_delete, frame_move, frame_duration, cel_copy, cel_move, cel_flip, tag_set, tag_delete, palette_set, palette_preset"`
	LayerID    string           `json:"layer_id,omitempty"`
	Ref        string           `json:"ref,omitempty" jsonschema:"Temporary handle for a newly created layer, usable by later edits"`
	Name       string           `json:"name,omitempty"`
	Frame      int              `json:"frame,omitempty"`
	ToFrame    int              `json:"to_frame,omitempty"`
	EndFrame   int              `json:"end_frame,omitempty"`
	CopyFrom   int              `json:"copy_from,omitempty"`
	DurationMS int              `json:"duration_ms,omitempty"`
	X          int              `json:"x,omitempty"`
	Y          int              `json:"y,omitempty"`
	X2         int              `json:"x2,omitempty"`
	Y2         int              `json:"y2,omitempty"`
	Width      int              `json:"width,omitempty"`
	Height     int              `json:"height,omitempty"`
	Color      *Color           `json:"color,omitempty"`
	Pixels     []Pixel          `json:"pixels,omitempty"`
	Points     []Point          `json:"points,omitempty"`
	Rows       []string         `json:"rows,omitempty"`
	Legend     map[string]Color `json:"legend,omitempty"`
	Filled     bool             `json:"filled,omitempty"`
	Visible    *bool            `json:"visible,omitempty"`
	Opacity    *int             `json:"opacity,omitempty"`
	Position   *int             `json:"position,omitempty" jsonschema:"One-based position in the layer stack"`
	Axis       string           `json:"axis,omitempty"`
	Direction  string           `json:"direction,omitempty"`
	Colors     []Color          `json:"colors,omitempty"`
	Preset     string           `json:"preset,omitempty"`
}

type Pixels struct {
	ColorMode string   `json:"color_mode"`
	X         int      `json:"x"`
	Y         int      `json:"y"`
	Width     int      `json:"width"`
	Height    int      `json:"height"`
	Values    []uint32 `json:"values"`
}

func decode[T any](raw json.RawMessage) (T, error) {
	var v T
	err := json.Unmarshal(raw, &v)
	if err != nil {
		return v, fmt.Errorf("invalid bridge response: %w", err)
	}
	return v, nil
}
