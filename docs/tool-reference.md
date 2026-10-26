# Tools and editing reference

[Back to the setup guide](../README.md). The MCP client discovers the current input schemas through `tools/list`; this guide explains their behavior and conventions.


| Tools | Purpose |
|---|---|
| `get_status`, `list_documents`, `get_document` | Connection, capabilities, current handles/revisions, layer hierarchy, frames, palettes, tags |
| `create_document`, `open_document`, `close_document` | Create, open, or close; closing unsaved work requires `discard=true` |
| `apply_edits` | One atomic edit collection and one native undo transaction |
| `read_pixels`, `read_rows` | Raw layer pixels or the visible RGBA composite, optionally cropped |
| `render_preview` | Selected frames as PNG image content; nearest-neighbor scale, grid, rulers, and contact sheets |
| `render_onion_skin` | PNG image content over ghosted neighboring frames |
| `compare_frames` | Changed-pixel count, percentage, and canvas-global bounds; bounds omitted when identical |
| `list_palette_presets`, `generate_color_ramp` | Non-mutating palette discovery and dark-to-light RGBA ramps |
| `undo`, `redo` | Native live history; unsupported in batch mode |
| `save_document`, `export_document` | Explicit native save, PNG sequence, GIF, or sheet plus JSON |

Coordinates are canvas-global and start at zero. Frames and layer stack positions start at one. Durations are milliseconds. Document and layer IDs are opaque; batch layer IDs persist in the working copy through Aseprite's layer UUID support. A live editor restart invalidates its handles.

| Edit `op` | Fields beyond `op` |
|---|---|
| `pixels` | `layer_id`, `frame`, `pixels: [{x,y,color}]` |
| `rows` | `layer_id`, `frame`, `x`, `y`, `rows`, `legend` |
| `stroke` | `layer_id`, `frame`, `points: [{x,y}]`, `color`; one-pixel polyline |
| `line` | `layer_id`, `frame`, `x`, `y`, `x2`, `y2`, `color` |
| `rectangle`, `ellipse` | `layer_id`, `frame`, `x`, `y`, `width`, `height`, `color`, optional `filled` |
| `fill` | `layer_id`, `frame`, `x`, `y`, `color`; exact-color, four-connected fill |
| `erase` | `layer_id`, `frame`, `x`, `y`, `width`, `height` |
| `layer_create` | `name`, optional `ref` used as a temporary `layer_id` later in this collection |
| `layer_update` | `layer_id`, optional `name`, `visible`, `opacity` (0–255), `position` |
| `layer_delete` | `layer_id` |
| `frame_create` | Append an empty frame; optional `copy_from`, `duration_ms` |
| `frame_delete` | `frame`; cannot remove the last frame |
| `frame_move` | `frame`, `to_frame`; moves contents and timing, preserving tag timeline ranges |
| `frame_duration` | `frame`, `duration_ms` |
| `cel_copy` | `layer_id`, source `frame`, `to_frame`, optional inclusive `end_frame` |
| `cel_move` | `layer_id`, `frame`, absolute cel position `x`, `y` |
| `cel_flip` | `layer_id`, `frame`, `axis`: `horizontal` or `vertical` |
| `tag_set` | `name`, `frame`, `end_frame`, optional `direction`: `forward`, `reverse`, `pingpong`, `pingpong_reverse` |
| `tag_delete` | `name` |
| `palette_set` | `colors`: array of RGBA colors; replaces the document's base palette |
| `palette_preset` | `preset`: ID from `list_palette_presets`; preserves pixel indices, does not quantize artwork |

Colors contain exactly one representation: `{"rgba":[r,g,b,a]}`, `{"gray_alpha":[gray,alpha]}`, or `{"index":n}`. Drawing requires the representation matching the document color mode. Palette entries and generated ramps use RGBA. Indexed palette shrinking is rejected if it would remove a used or transparent index.

`read_pixels.values` is row-major: RGBA packed as `r | g<<8 | b<<16 | a<<24`, grayscale as `gray | alpha<<8`, or a palette index. Omitting `layer_id` returns the visible RGBA composite. Specify `composite=true` with a layer ID to render that layer in isolation.

Preview crops retain the original canvas coordinates. Frame labels include durations. Preview limits are 64 frames and 16 million output pixels; raw reads are limited to 262,144 pixels. Drawing is hard-edged without antialiasing. Off-canvas drawing clips; fill requires an in-canvas origin.

Exports require the current revision. `format` is `png`, `gif`, or `sheet`. PNG exports multiple frames as `name-0001.png`, etc.; one selected frame uses the exact path. `sheet` uses configurable `columns` and writes an adjacent `.json` with source frame durations. GIF exports the complete timeline with native Aseprite timing; select frames using PNG or sheet exports. GIF timing follows the format's centisecond resolution.

## Saving and failure handling

Batch mode never edits the source during drawing. It stores private temporary `.aseprite` working copies and normalizes revisions after serialization. Closing or stopping the server discards unsaved working copies. Live mode leaves edits in the editor's undo history until saved.

Every mutation requires the latest revision. Revisions cover image bytes, dimensions, layers, palettes, frame durations, and tags; live change counters also detect undo/redo. A stale revision requires inspection and a fresh call. Failed edit collections roll back. Edited cels are detached from links so other frames remain unchanged. Locked layers and their parent groups are respected.

`save_document` accepts `.aseprite` or `.ase`. Existing destinations require `overwrite=true`; a source changed externally cannot be overwritten from the stale session even with that flag. Reopen it or save elsewhere. Workspace-relative and absolute paths are allowed only inside the configured workspace; escaping symlinks are rejected. Saves and exported files use rooted filesystem access and staged replacement. After a live save, the bridge acknowledges the exact saved revision in the editor. If the editor changes or disconnects before that acknowledgement, the tool reports that the snapshot was saved but the editor's saved state could not be confirmed.

Export destinations are checked before committing outputs. Each output is replaced individually; a filesystem failure can leave a partially written sequence, which is reported. Exports do not mark artwork as saved. A disconnect or timeout after a live mutation is sent reports an uncertain outcome; the server never retries automatically. Reconnect, inspect, and decide the next edit. Once native Lua starts on the UI thread it cannot be interrupted safely; live timeouts disconnect the bridge rather than claiming cancellation.

Only ordinary image layers are editable. Existing groups can be inspected; tilemap editing and frame moves involving tilemaps are rejected. This release does not provide arbitrary code execution, text rendering, tweening, advanced selections/effects, remote hosting, or simultaneous agent clients.
