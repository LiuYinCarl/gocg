# gocg

A Go call graph analyzer inspired by [clang-callgraph](https://github.com/LiuYinCarl/clang-callgraph). Uses `golang.org/x/tools` (VTA, SSA, packages) to build precise call graphs for Go codebases, with an interactive REPL for exploration.

## Installation

```bash
git clone <this-repo>
cd gocg
go install .
```

Requires Go 1.21+.

## Quick Start

```bash
# Analyze a Go project, enter interactive REPL
gocg /path/to/your/go/project

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

Tab completion is supported for function names and `@` subcommands.

## How It Works

1. **Load**: `go/packages` loads the project and all dependencies
2. **SSA**: `go/ssa` builds static single-assignment form
3. **VTA**: `go/callgraph/vta` computes a precise call graph (no false-positive interface calls like CHA)
4. **Filter**: Only functions defined under the project directory are expanded; external calls appear as leaf nodes
5. **Cache**: Results are cached in `.gocg-cache/` under the project directory, keyed by SHA256 of all `.go` file mtimes/sizes

## CLI Flags

| Flag | Description |
|------|-------------|
| `<directory>` | Go project directory to analyze (default: `.`) |
| `-x p1,p2,...` | Exclude functions whose full import path contains any of these substrings |
| `--lookup func` | Print call graph for a function and exit (no REPL) |
| `--no-cache` | Skip reading/writing cache |
| `--clear-cache` | Remove all cached files and exit |

## Comparison with clang-callgraph

| Feature | clang-callgraph | gocg |
|---------|:---:|:---:|
| Language | C/C++ (libclang) | Go (x/tools) |
| Input | `compile_commands.json` / `.cpp` | Go project directory |
| Call graph algorithm | Clang AST walk | VTA (Variable Type Analysis) |
| Interactive REPL | ✅ | ✅ |
| Filter / Ignore / Ref | ✅ | ✅ |
| Tab completion | ✅ | ✅ |
| Cache | ✅ | ✅ |
