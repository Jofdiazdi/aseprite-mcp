# Attribution and third-party resources

The original Aseprite MCP server, Lua bridge, documentation, and example artwork in this repository are provided under the [MIT license](LICENSE). Third-party resources and dependencies retain their own notices and licensing; the project license does not relicense them.

## Aseprite

[Aseprite](https://www.aseprite.org/) is an external runtime, maintained in [aseprite/aseprite](https://github.com/aseprite/aseprite). Its C++ source and application binary are not included in this repository. CI checks out and builds an upstream revision separately for integration testing. See the upstream repository for Aseprite's licensing terms.

This is an independent integration and is not an official Aseprite project.

## Bundled palettes

The files under `assets/palettes/` were copied from Aseprite's bundled extensions at commit `b3e34acbae812b8a1d814c2e6285343b351c8945`. Their original comments and attribution remain in the files.

| Resource | Attribution | Original reference |
|---|---|---|
| `pico-8.gpl` | Joseph White; [PICO-8](https://www.pico-8.com/) | [`data/extensions/pico8-palette/pico-8.gpl`](https://github.com/aseprite/aseprite/blob/b3e34acbae812b8a1d814c2e6285343b351c8945/data/extensions/pico8-palette/pico-8.gpl) |
| `db16.gpl` | Richard “DawnBringer” Fhager; [original post](https://pixeljoint.com/forum/forum_posts.asp?TID=12795) | [`data/extensions/dawnbringer-palettes/db16.gpl`](https://github.com/aseprite/aseprite/blob/b3e34acbae812b8a1d814c2e6285343b351c8945/data/extensions/dawnbringer-palettes/db16.gpl) |
| `db32.gpl` | Richard “DawnBringer” Fhager; [original post](https://pixeljoint.com/forum/forum_posts.asp?TID=16247) | [`data/extensions/dawnbringer-palettes/db32.gpl`](https://github.com/aseprite/aseprite/blob/b3e34acbae812b8a1d814c2e6285343b351c8945/data/extensions/dawnbringer-palettes/db32.gpl) |
| `gameboy.gpl` | Bundled Game Boy hardware color palette | [`data/extensions/hardware-palettes/gameboy.gpl`](https://github.com/aseprite/aseprite/blob/b3e34acbae812b8a1d814c2e6285343b351c8945/data/extensions/hardware-palettes/gameboy.gpl) |

## Go dependencies

Direct dependencies are the official [Go MCP SDK](https://github.com/modelcontextprotocol/go-sdk), [coder/websocket](https://github.com/coder/websocket), and [golang.org/x/image](https://pkg.go.dev/golang.org/x/image). `go.mod` and `go.sum` record the pinned direct and transitive dependency versions. Their respective source distributions contain their license notices.

## Related work

The [diivi/aseprite-mcp](https://github.com/diivi/aseprite-mcp) Python project was considered during the initial design comparison. No Python server source from that project is included here.
