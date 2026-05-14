# AGENTS.md — gocg

A Go call graph analyzer using `golang.org/x/tools` (VTA/SSA) with an interactive REPL. Single binary, module name `gocg`.

## Build / Test / Run

```bash
go build .          # compile
go install .        # install to $GOPATH/bin
go vet ./...        # static analysis (passes clean)
```

No test files exist. No Makefile, no CI config.

## Architecture

```
main.go          — CLI entry point: argument parsing, wires graph → query → repl
graph/graph.go   — call graph construction (VTA), caching, short-name mapping
query/query.go   — search, tree-format call graph printing (call/filter/ignore/ref modes)
repl/repl.go     — interactive REPL with readline tab completion
```

## Package `graph` — Core Call Graph

**`Build(dir, excludePrefixes, noCache)`** does everything:
1. Resolves absolute directory path
2. Checks cache (`.gocg-cache/<sha256>.json`) — keyed on SHA256 of all `.go` file mtimes/sizes + cacheVersion + exclude prefixes
3. On cache miss: loads packages via `go/packages`, builds SSA, runs VTA call graph
4. Simultaneously populates **`CallGraph`** (`caller → []callees`) and **`RefGraph`** (`callee → []callers`) plus sorted **`FullNames`**
5. Writes cache

**Key types:**
```go
type Graph struct {
    CallGraph map[string][]string  // caller → sorted callee names
    RefGraph  map[string][]string  // callee → sorted caller names
    FullNames []string             // all function names, sorted
}

type Stats struct {
    Packages  int
    Functions int
    Edges     int
    Cached    bool
}
```

**Function name shortening** (`funcDisplayName`):
- Accepts pre-sorted paths (longest-first) to avoid repeated sorting per call
- Replaces full import paths with short package names
- If only one package uses a given name → just `pkgname`
- If multiple share the same name → `parent/pkgname`

**Gotchas:**
- Only functions with source files under the project directory are expanded as **callers**. External callees are still included as leaf nodes — they aren't filtered from other callers' out-edges.
- `matchesExclude(name, prefixes)` does **substring** matching against the full `ssa.Function.String()` output, not just the import path. Any substring match in the full function name triggers exclusion.
- Cache stores the serialized `cachePayload` JSON. Cache version is hardcoded (`const cacheVersion = 1`). Bumping it invalidates all existing caches.
- `refgraph` is populated for **every** edge (not just project-internal callees), so reverse lookups can show external callers.

**Performance notes:**
- `buildFromSource` caches `funcDisplayName` and `matchesExclude` results using maps to avoid redundant computation across edges.
- Edge collection uses `map[string]map[string]struct{}` (temp maps) then converts to sorted `[]string` slices — avoids O(n) `appendUnique` lookups on growing slices.
- Short map paths are sorted once in `buildFromSource`; `funcDisplayName` receives the pre-sorted slice.

## Package `query` — Printing & Search

All output is tree-formatted with ANSI color escapes:
- **Green** (`\033[32m`, `ctrlGreen`): tree branches and root function names in call graph output
- **Red** (`\033[31m`, `ctrlRed`): tree branches in reverse reference output (`printRefs`)
- **Yellow** (`\033[33m`, `ctrlYellow`): search result headers ("matching list:")

**Modes** (called from repl/main):
| Function | Purpose |
|---|---|
| `PrintCallGraph` | Exact match → full call tree; no exact match → auto-select if single search result, else show list |
| `PrintFilterCallGraph` | Only show branches containing any filter keyword; same auto-select behavior |
| `PrintIgnoreCallGraph` | Skip branches containing any ignore keyword; same auto-select behavior |
| `PrintRefGraph` | Reverse: who calls this function; same auto-select behavior |
| `Search` | Substring search over `FullNames` |

Cycle detection: all recursive printer functions track `seen []string` to avoid infinite loops on recursive calls.

Auto-select behavior: `resolveName(g, check, input)` first tries exact match in `check`, then falls back to `Search` — if exactly one result, uses it; otherwise returns empty, triggering the match list display.

## Package `repl` — Interactive Shell

Uses `github.com/chzyer/readline` for line editing and tab completion.

**`State`** holds:
- `FilterSet`/`IgnoreSet` (`map[string]bool`) — persistent filter/ignore keywords
- `PrintDepth` — user-configurable max depth (default 15), clamps at `MaxDepth`
- `MaxDepth` — hard cap (default 15), not user-changeable via `@ depth`

**REPL commands:**
```
funcname          → call graph (exact match) or search list (partial match)
? funcname        → filter mode: branches containing filter keywords
! funcname        → ignore mode: skip branches matching ignore keywords
& funcname        → reverse references (who calls this)
@ filter kw1 kw2  → add filter keywords
@ ignore kw1 kw2  → add ignore keywords
@ del_fi kw       → remove filter keyword
@ del_ig kw       → remove ignore keyword
@ depth N         → set print depth (clamped to MaxDepth)
@ show            → display current config
@ reset           → clear all config to defaults
```

Tab completion works for:
- Function names (prefix match, first 100 matches)
- `@` subcommands (`filter`, `ignore`, `del_fi`, `del_ig`, `depth`, `show`, `reset`)
- Mode prefixes (`?`, `!`, `&`) followed by function names

## CLI Flags (parsed in `main.go`)

| Flag | Behavior |
|---|---|
| `<directory>` | Go project to analyze (default: `.`) |
| `-x p1,p2,...` | Exclude functions containing any of these substrings |
| `--lookup func` | Single lookup, print and exit (no REPL) |
| `--no-cache` | Skip cache read/write |
| `--clear-cache` | Remove cached files and exit |

Error output goes to **stderr**. Summary line goes to **stdout**.

## Dependency Graph (imports)

```
main.go  → graph, query, repl
repl.go  → graph, query, readline
query.go → graph
graph.go → vta, ssa, ssautil, packages (all from golang.org/x/tools)
```

`query` and `repl` never import each other — they're independent consumers of `graph`. `main` wires them together.

## Style Notes

- No comments in source code (by convention — the project is self-documenting)
- Sorted output everywhere: `sort.Strings()` on callee/ref lists, file lists for cache keys
- All packages use the `gocg/` module prefix for imports
- Uses `slices.Contains` (stdlib) instead of handwritten `contains` helper
