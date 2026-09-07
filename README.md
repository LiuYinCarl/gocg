# gocg

[![CI](https://github.com/LiuYinCarl/gocg/actions/workflows/ci.yml/badge.svg)](https://github.com/LiuYinCarl/gocg/actions/workflows/ci.yml)

A Go call graph analyzer inspired by [clang-callgraph](https://github.com/LiuYinCarl/clang-callgraph). Uses `golang.org/x/tools` (VTA, SSA, packages) to build precise call graphs for Go codebases, with an interactive REPL for exploration.

## Installation

```bash
go install github.com/LiuYinCarl/gocg@latest
```

Or build from source:

```bash
git clone https://github.com/LiuYinCarl/gocg.git
cd gocg
go install .
```

Requires Go 1.25+.

## Quick Start

```bash
# Analyze a Go project, enter interactive REPL
gocg /path/to/your/go/project

# Interactive TUI (bubble tea): filter list + live call tree pane
gocg /path/to/your/go/project --tui

# Single lookup (non-interactive)
gocg /path/to/your/go/project --lookup 'pkg.FuncName'

# Exclude functions containing certain import paths
gocg . -x 'golang.org/x/tools,github.com/some/dep'

# Disable or clear cache
gocg . --no-cache
gocg . --clear-cache
```

## Interactive REPL

```
>>> createTransport             # exact match → call graph; partial → search list
>>> ? createTransport           # filter mode: only branches containing filter keywords
>>> ! createTransport           # ignore mode: skip branches matching ignore keywords
>>> & createTransport           # reverse references: who calls this function

>>> @ filter keyword1 keyword2  # add filter keywords
>>> @ ignore keyword1 keyword2  # add ignore keywords
>>> @ del_fi keyword            # remove a filter keyword
>>> @ del_ig keyword            # remove an ignore keyword
>>> @ depth 3                   # set max print depth
>>> @ show                      # show current config
>>> @ reset                     # reset all config
```

Tab completion is supported for function names and `@` subcommands. ANSI colors are automatically disabled when output is not a terminal, `NO_COLOR`/`TERM=dumb` is set, or the console lacks VT support (legacy Windows cmd).

## Interactive TUI

Run with `--tui` to get a bubble tea interface: an input box on top, a live-filtered function list on the left, and the call tree on the right.

- Type to filter the function list (case-insensitive substring); `↑`/`↓` move the selection, `enter` renders the tree for the selected function
- `? func`, `! func`, `& func` prefixes and `@ filter/ignore/del_fi/del_ig/depth/show/reset` commands work exactly like the REPL (press `enter` to apply)
- `tab` or `esc` moves focus from the input box to the tree pane; `tab`, `/`, or `i` moves back; scroll the tree with `pgup`/`pgdown` (or any navigation key when the pane is focused)
- `q` quits when the tree pane is focused; `ctrl+c` always quits
- The call graph is built asynchronously with a spinner while loading

## How It Works

1. **Load**: `go/packages` loads the project and all dependencies
2. **SSA**: `go/ssa` builds static single-assignment form
3. **VTA**: `go/callgraph/vta` computes a precise call graph (no false-positive interface calls like CHA)
4. **Filter**: Only functions defined under the project directory are expanded; external calls appear as leaf nodes
5. **Cache**: Results are cached in the OS user cache directory (`~/.cache/gocg/` on Linux, `~/Library/Caches/gocg/` on macOS, `%LocalAppData%\gocg\` on Windows), keyed by SHA256 of the project path, exclude prefixes, and all `.go` file mtimes/sizes. Packages with load errors are reported as warnings on stderr.

## CLI Flags

| Flag | Description |
|------|-------------|
| `<directory>` | Go project directory to analyze (default: `.`) |
| `-x p1,p2,...` | Exclude functions whose full import path contains any of these substrings |
| `--lookup func` | Print call graph for a function and exit (no REPL) |
| `--tui` | Interactive TUI (bubble tea) instead of the REPL |
| `--no-cache` | Skip reading/writing cache |
| `--clear-cache` | Remove cached results for the project (also cleans up the legacy `.gocg-cache/` directory) |

## Comparison with clang-callgraph

| Feature | clang-callgraph | gocg |
|---------|:---:|:---:|
| Language | C/C++ (libclang) | Go (x/tools) |
| Input | `compile_commands.json` / `.cpp` | Go project directory |
| Call graph algorithm | Clang AST walk | VTA (Variable Type Analysis) |
| Interactive REPL | ✅ | ✅ |
| Interactive TUI (bubble tea) | ❌ | ✅ |
| Filter / Ignore / Ref | ✅ | ✅ |
| Tab completion | ✅ | ✅ |
| Cache | ✅ | ✅ |
