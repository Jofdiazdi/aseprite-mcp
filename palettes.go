package asepritemcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

type Preset struct {
	ID          string  `json:"id"`
	Attribution string  `json:"attribution"`
	Colors      []Color `json:"colors"`
}

func PalettePresets() []Preset {
	entries, _ := assets.ReadDir("assets/palettes")
	result := []Preset{}
	for _, entry := range entries {
		data, _ := assets.ReadFile("assets/palettes/" + entry.Name())
		p := Preset{ID: strings.TrimSuffix(entry.Name(), ".gpl"), Colors: []Color{}}
		var attribution []string
		scan := bufio.NewScanner(strings.NewReader(string(data)))
		for scan.Scan() {
			line := scan.Text()
			var r, g, b int
			if _, err := fmt.Sscanf(line, "%d %d %d", &r, &g, &b); err == nil {
				p.Colors = append(p.Colors, Color{RGBA: []int{r, g, b, 255}})
			} else if strings.HasPrefix(line, "#") && strings.TrimSpace(strings.TrimPrefix(line, "#")) != "" {
				attribution = append(attribution, strings.TrimSpace(strings.TrimPrefix(line, "#")))
			}
		}
		p.Attribution = strings.Join(attribution, "; ")
		result = append(result, p)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func palettePreset(id string) (Preset, bool) {
	for _, p := range PalettePresets() {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}

func colorRamp(a map[string]any) (json.RawMessage, error) {
	data, _ := json.Marshal(a["base"])
	var base Color
	if err := json.Unmarshal(data, &base); err != nil {
		return nil, err
	}
	if len(base.RGBA) != 4 {
		return nil, fmt.Errorf("base requires rgba with four channels")
	}
	for _, v := range base.RGBA {
		if v < 0 || v > 255 {
			return nil, fmt.Errorf("channels must be from 0 to 255")
		}
	}
	steps := number(a["steps"])
	if steps < 2 || steps > 16 {
		return nil, fmt.Errorf("steps must be from 2 to 16")
	}
	shift := number(a["hue_shift"])
	if shift < -60 || shift > 60 {
		return nil, fmt.Errorf("hue_shift must be from -60 to 60 degrees")
	}
	r, g, b := float64(base.RGBA[0])/255, float64(base.RGBA[1])/255, float64(base.RGBA[2])/255
	hi, lo := max(r, g, b), min(r, g, b)
	delta := hi - lo
	l := (hi + lo) / 2
	h, sat := 0.0, 0.0
	if delta > 0 {
		sat = delta / (1 - math.Abs(2*l-1))
		switch hi {
		case r:
			h = math.Mod((g-b)/delta, 6)
		case g:
			h = (b-r)/delta + 2
		default:
			h = (r-g)/delta + 4
		}
		h *= 60
	}
	colors := make([]Color, steps)
	for i := range colors {
		t := float64(i) / float64(steps-1)
		light := max(0.0, min(1.0, l+(t-0.5)*0.65))
		hue := math.Mod(h+(t-0.5)*2*float64(shift)+720, 360)
		c := (1 - math.Abs(2*light-1)) * sat
		x := c * (1 - math.Abs(math.Mod(hue/60, 2)-1))
		m := light - c/2
		var rr, gg, bb float64
		switch {
		case hue < 60:
			rr, gg = c, x
		case hue < 120:
			rr, gg = x, c
		case hue < 180:
			gg, bb = c, x
		case hue < 240:
			gg, bb = x, c
		case hue < 300:
			rr, bb = x, c
		default:
			rr, bb = c, x
		}
		colors[i] = Color{RGBA: []int{int(math.Round((rr + m) * 255)), int(math.Round((gg + m) * 255)), int(math.Round((bb + m) * 255)), base.RGBA[3]}}
	}
	return json.Marshal(map[string]any{"colors": colors})
}
