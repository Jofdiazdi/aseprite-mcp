# Contributing to Aseprite MCP

Help is welcome with reproducible bug reports, documentation, live editor verification, portability, and focused artwork-tool improvements. Start with the [README](README.md) to connect the server and understand its current scope.

## Set up a development environment

Install Go 1.25+ and, for native integration work, Aseprite with scripting API 41+. Clone your fork and build from the repository root:

```sh
git clone https://github.com/YOUR_USERNAME/aseprite-mcp.git
cd aseprite-mcp
git switch -c improve-your-feature
go build -o dist/aseprite-mcp ./cmd/aseprite-mcp
go generate ./...
go test ./...
```

Use `dist/aseprite-mcp.exe` as the build output on Windows. Aseprite is a separate runtime dependency; no Aseprite source checkout is needed for ordinary Go development.

`go generate ./...` creates `dist/agent-mcp.aseprite-extension` and synchronizes `extension/agent-mcp/` from the canonical `assets/` sources. Install the archive in Aseprite when testing live mode. Rebuild the server and reinstall the extension after bridge changes so both sides use the same protocol.

## Choose a focused change

For a bug, an issue with a small reproducer is useful; a focused fix can also go straight to a pull request. Discuss larger changes first, especially new edit operations, API changes, dependencies, or scope additions.

Preserve the existing boundaries:

- MCP tools use typed arguments and structured results. Return actionable errors rather than treating tool failures as successful output.
- Live and batch modes share `assets/dispatcher.lua`. Keep editing semantics consistent; report explicitly when a backend cannot support an operation.
- Every mutation requires a current revision. Validate edit collections before applying them and preserve atomic rollback and native undo behavior.
- Workspace checks, explicit overwrite, source-change detection, locked-layer behavior, and uncertain-outcome reporting belong to the supported workflow.
- Rendering and inspection should leave document contents and editor preferences unchanged.
- Do not add arbitrary Lua or shell tools as a shortcut for a defined operation.

Keep code readable and follow the existing Go and Lua style. Run `gofmt` on changed Go files. Change canonical extension files under `assets/`, then generate their copies; do not hand-edit `extension/agent-mcp/`.

## Run the relevant checks

For ordinary Go changes:

```sh
go test ./...
go vet ./...
```

For shared-state, connection, or cancellation changes, also run the race detector on a supported system:

```sh
go test -race ./...
```

Native tests skip when no Aseprite executable is available, or when `-short` is used. Include real execution for Lua, drawing, document lifecycle, saving, or export changes:

```sh
ASEPRITE_PATH=/absolute/path/to/aseprite go test -v ./...
```

PowerShell equivalent:

```powershell
$env:ASEPRITE_PATH = 'C:\path\to\aseprite.exe'
go test -v ./...
```

The test suite can also discover `aseprite` on `PATH`. An explicitly configured but invalid executable should be fixed rather than interpreted as successful native validation.

| Area changed | Useful verification |
|---|---|
| Tool schema or result | Exercise it through the official MCP SDK client; check error and image content where relevant. |
| Drawing or palette | Assert exact pixels, transparency, and the correct RGBA/indexed/grayscale representation. |
| Cels, layers, or frames | Check offset and linked cels, locked layers, unaffected frames, metadata, and rollback. |
| History or revisions | Check a single undo/redo step, manual changes, stale requests, and closed documents. |
| Previews | Check canvas coordinates, crops, grids, image output, and that inspection is non-mutating. |
| Saving or exporting | Check untouched source files before Save, overwrite/source conflicts, reopen fidelity, timing, and sheet metadata. |
| Live connection | Check authentication, version mismatch, serialization, disconnects, and uncertain mutation outcomes. |

Use temporary directories for test files and keep external-command tests skippable. Prefer tests of observable behavior over tests that repeat the implementation.

## Exercise the complete workflow

Build the server and run the real stdio animation client into a fresh directory:

```sh
go build -o dist/aseprite-mcp ./cmd/aseprite-mcp
go run ./cmd/example --server ./dist/aseprite-mcp --aseprite /path/to/aseprite --workspace ./dist/contribution-demo
```

Inspect the native sprite, preview, GIF, and sprite sheet. The example deliberately uses public MCP tools rather than private service functions. It fails on existing outputs unless you choose a fresh destination.

For live bridge changes, also pair a visible Aseprite editor and verify the menu status, edits, manual-edit revision rejection, undo/redo, Save, and Disconnect. Automated WebSocket tests and headless native tests do not replace this GUI check. Report exactly which platform and Aseprite version you exercised.

## Open a pull request

Target the default `main` branch. A useful description states:

- The concrete problem and resulting behavior, with a short before/after example.
- The verification commands and their results, including skipped native or GUI coverage.
- Any compatibility or protocol change, limitations, and documentation updates.

Keep tests with the behavior they verify and documentation with user-visible changes. Split unrelated changes into separate pull requests. Include generated extension copies when their canonical source changes; leave ignored `dist/` binaries, local pairing files, and temporary artwork out of commits.

If adding a committed artwork example, include its editable source, dimensions, timing, and a compact preview. Preserve resource attribution and remove local usernames, absolute paths, tokens, and private source material from creation records.

CI runs Go tests and builds on Linux, macOS, and Windows; Linux also runs race detection and builds the pinned upstream Aseprite baseline for native artwork tests. A passing unit-only run is useful but does not establish native editing compatibility.

## Report a bug or propose a feature

Use the [issue tracker](https://github.com/Jofdiazdi/aseprite-mcp/issues). For a bug, include your OS/architecture, server commit, Aseprite version/API, live or batch mode, minimal tool arguments, expected behavior, and the actual error. A small synthetic `.aseprite` reproducer is preferable to private project files. Remove pairing tokens and machine-specific paths from logs.

For a feature, explain the artwork workflow it would enable, whether it needs live or batch mode, and an example tool call or expected result. Check the README's deferred scope first.

## Licensing and attribution

Project contributions are made under the [MIT license](LICENSE). Preserve third-party notices and clearly identify any added external resources and their license. See [NOTICE.md](NOTICE.md) for existing palette credits. Aseprite remains a separate product with its own licensing.
