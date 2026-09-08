# AGENTS.md — gocg

A Go call graph analyzer using `golang.org/x/tools` (VTA/SSA) with an interactive REPL. Single binary, module name `gocg`.

## Build / Test / Run

```bash
go build .          # compile
go install .        # install to $GOPATH/bin
go vet ./...        # static analysis (passes clean)
```

No test files exist. No Makefile. CI/CD lives in `.github/workflows/`: `ci.yml` (gofmt / mod-tidy check / vet / build / test / smoke, matrix over ubuntu/macos/windows × Go 1.25.x+stable) and `release.yml` (tag `v*` → cross-compiled binaries on a GitHub release).

## Architecture

```
main.go          — CLI entry point: argument parsing, wires graph → query → repl/tui
graph/graph.go   — call graph construction (VTA), caching, short-name mapping
query/query.go   — search, tree-format call graph printing (call/filter/ignore/ref modes)
repl/repl.go     — interactive REPL with readline tab completion
tui/tui.go       — bubble tea TUI: filter input, function list, call-tree viewport
```

## Package `graph` — Core Call Graph

**`Build(dir, excludePrefixes, noCache)`** does everything:
1. Resolves absolute directory path
2. Checks cache (`<os.UserCacheDir>/gocg/<dirHash16>-<contentHash32>.json`) — keyed on SHA256 of all `.go` file mtimes/sizes + cacheVersion + exclude prefixes; the filename prefix is a SHA256 of the absolute project dir, so `--clear-cache <dir>` can find and remove all cache variants for that project (and also removes the legacy `<dir>/.gocg-cache/` directory)
3. On cache miss: loads packages via `go/packages`, builds SSA, runs VTA call graph
4. Simultaneously populates **`CallGraph`** (`caller → []callees`) and **`RefGraph`** (`callee → []callers`) plus sorted **`FullNames`**
5. Writes cache (compact `json.Marshal`, not indented)

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
    Warnings  []string             // go/packages load errors, capped at 20 + a count line
}
```

**Function name shortening** (`funcDisplayName`):
- Accepts pre-sorted paths (longest-first) to avoid repeated sorting per call
- Replaces full import paths with short package names
- If only one package uses a given name → just `pkgname`
- If multiple share the same name → `resolveAmbiguous` picks the first unique candidate per path: `parentdir/pkgname` (legacy), then `lastdir/pkgname` (handles versioned dirs like `semconv/v1.37.0`), then progressively longer path suffixes (`crush/internal/client`), finally the full path. Candidates are deduplicated against a global `used` set (groups processed in sorted order, so output is deterministic) — display names are unique across all import paths.

**Gotchas:**
- Only functions with source files under the project directory are expanded as **callers**. External callees are still included as leaf nodes — they aren't filtered from other callers' out-edges.
- The project directory is canonicalized with `filepath.Abs` + `filepath.EvalSymlinks` in both `Build` and `ClearCache`, so symlinked aliases (e.g. macOS `/tmp` → `/private/tmp`) share one cache entry and containment checks stay consistent.
- Project containment (`pathInDir`) uses `filepath.Rel`, not string prefix matching — cross-platform safe (volume names, separators) and immune to `/foo/bar2` matching `/foo/bar`. Note: comparison is case-sensitive, so a differently-cased path spelling on a case-insensitive filesystem (Windows/macOS) won't match.
- Display names are the map keys. Since `resolveAmbiguous`, names are unique per import path; identically-printing generic instantiations (same `fn.String()`) still share one entry, which is harmless — they have identical edges.
- `matchesExclude(name, prefixes)` does **substring** matching against the full `ssa.Function.String()` output, not just the import path. Any substring match in the full function name triggers exclusion.
- Cache stores the serialized `cachePayload` JSON (including `Warnings`, so cached loads still surface them). Cache version is hardcoded (`const cacheVersion = 3`) and participates in the cache filename hash. Bumping it invalidates all existing caches — required whenever graph construction semantics change.
- VTA roots are `ssautil.AllFunctions(prog)` — **not** `pkg.Members`. `vta.CallGraph` only computes out-edges for functions in the root set, and `pkg.Members` omits methods, anonymous functions (`fn$1`), and generic instantiations; using it left most methods as leaf nodes with no call chain. `AllFunctions` covers methods (via method sets), closures (via operand walk), and instantiations.
- `refgraph` is populated for **every** edge (not just project-internal callees), so reverse lookups can show external callers.

**Performance notes:**
- `buildFromSource` caches `funcDisplayName` and `matchesExclude` results using maps to avoid redundant computation across edges.
- Edge collection uses `map[string]map[string]struct{}` (temp maps) then converts to sorted `[]string` slices — avoids O(n) `appendUnique` lookups on growing slices.
- Short map paths are sorted once in `buildFromSource`; `funcDisplayName` receives the pre-sorted slice.

## Package `query` — Printing & Search

All output is tree-formatted with ANSI color escapes, gated by `termenv.EnvColorProfile()` — when the profile is `Ascii` (non-tty, `NO_COLOR`, `TERM=dumb`, legacy Windows console, CI) the color codes become empty strings:
- **Green** (`\033[32m`, `ctrlGreen`): tree branches and root function names in call graph output
- **Red** (`\033[31m`, `ctrlRed`): tree branches in reverse reference output (`printRefs`)
- **Yellow** (`\033[33m`, `ctrlYellow`): search result headers ("matching list:")
- **Cyan** (`\033[36m`, `ctrlCyan`) + **Gray** (`\033[90m`, `ctrlGray`): leaf function names via `colorName` — gray for the path prefix up to the last `/` (usually empty, since display names are short), cyan for the rest

**Modes** (called from repl/main/tui):
| Function | Purpose |
|---|---|
| `PrintCallGraph` | Exact match → full call tree; no exact match → auto-select if single search result, else show list |
| `PrintFilterCallGraph` | Only show branches containing any filter keyword; same auto-select behavior |
| `PrintIgnoreCallGraph` | Skip branches containing any ignore keyword; same auto-select behavior |
| `PrintRefGraph` | Reverse: who calls this function; same auto-select behavior |
| `Search` | Substring search over `FullNames` |

Cycle detection: all recursive printer functions track `seen []string` (accumulates ancestors + previously processed siblings within the current subtree) to avoid infinite loops on recursive calls.

Tree rendering: `treePrefix(last []bool, isLast bool, color)` emits proper branch markers — `|--` for middle children, `\--` for the last child, with continuation (`|  `) or blank (`   `) columns for ancestors. `PrintFilterCallGraph` is two-phase: `markFilter` first collects the set of nodes on hit paths (`keep`), then `printFiltered` prints that set once — no duplicated stack lines. All four `Print*` functions fall back to the yellow "matching list:" when the name doesn't resolve.

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

## Package `tui` — Bubble Tea Interface

Uses `charmbracelet/bubbletea` + `bubbles` (textinput, viewport, spinner) + `lipgloss`. Entered via `--tui`; the call graph is built asynchronously inside the TUI (`tea.Cmd` + spinner), so `main.go` skips its own `graph.Build` call in this mode.

**Model states:** `loading` → `ready` (or `failed`). Layout: input box on top, function list (left, bordered) + call-tree viewport (right, bordered), status bar at bottom.

- Typing filters `FullNames` case-insensitively (substring, capped at 200 matches) against a precomputed lowercased copy, only when the input actually changed; `↑`/`↓` move selection; `enter` renders the tree for the selected function — trees are **not** re-rendered on every cursor move (bounded-cost UX).
- `?`/`!`/`&` input prefixes map to `query.PrintFilterCallGraph` / `PrintIgnoreCallGraph` / `PrintRefGraph`; the mode is captured at lock time (`lockedMode`) so later `@` commands don't reset the tree's mode. `@` commands (`filter`/`ignore`/`del_fi`/`del_ig`/`depth`/`show`/`reset`) mirror the REPL and apply on `enter`, with feedback in the status bar; the tree re-renders only after tree-affecting commands (never after `@ show`).
- Focus: input box by default; `tab`/`esc` moves focus to the tree pane (scrolls with any viewport key, `q` quits); `tab`/`/`/`i` returns to the input. `ctrl+c` quits from anywhere.
- Horizontal scroll: bubbles viewport is vertical-only, so the model keeps the raw `treeLines` plus `hOffset`/`maxTreeWidth`; `syncTree` re-feeds the viewport with each line pre-cut by `ansi.Cut(line, hOffset, hOffset+vp.Width)` (ANSI-aware, colors survive). Tree-pane keys `←`/`→` (or `h`/`l`) scroll by `hScrollStep` (8), the title shows a `←+N` indicator when offset, and `resizeViewport` re-clamps via `scrollH(0)`. `y` copies the whole tree to the clipboard (`atotto/clipboard`) with ANSI codes stripped (`ansi.Strip`), reporting `copied N lines (M bytes)` in the status bar.
- The model keeps its own `filterSet`/`ignoreSet`/`printDepth` (same semantics as `repl.State`, deliberately not shared, including the `maxDepth = 15` clamp on `@ depth`).

`query.Print*` output includes ANSI colors, which the viewport renders as-is (horizontal scrolling cuts them with `charmbracelet/x/ansi`, never bytewise).

## CLI Flags (parsed in `main.go`)

| Flag | Behavior |
|---|---|
| `<directory>` | Go project to analyze (default: `.`) |
| `-x p1,p2,...` | Exclude functions containing any of these substrings |
| `--lookup func` | Single lookup, print and exit (no REPL) |
| `--tui` | Bubble tea TUI instead of the REPL (graph built async inside the TUI) |
| `--no-cache` | Skip cache read/write |
| `--clear-cache` | Remove cached results for the project and exit (also removes legacy `<dir>/.gocg-cache/`) |
| `--version`, `-v` | Print version and exit. Version source: ldflags `-X main.version` (release.yml injects the tag), else `debug.ReadBuildInfo()` — module version for `go install @vX`, short vcs revision + time for checkout builds |
| `--help`, `-h` | Print usage to stdout and exit |

Unknown flags, missing flag values, and multiple positional directories are hard errors (stderr + exit 1). `Stats.Warnings` (go/packages load errors) are printed to stderr after the build.

Error output goes to **stderr**. Summary line goes to **stdout**.

## Dependency Graph (imports)

```
main.go  → graph, query, repl, tui
tui.go   → graph, query, bubbletea, bubbles, lipgloss
repl.go  → graph, query, readline, termenv
query.go → graph, termenv
graph.go → vta, ssa, ssautil, packages (all from golang.org/x/tools)
```

`query`, `repl`, and `tui` never import each other — they're independent consumers of `graph`. `main` wires them together.

## Style Notes

- No comments in source code (by convention — the project is self-documenting)
- Sorted output everywhere: `sort.Strings()` on callee/ref lists, file lists for cache keys
- All packages use the `gocg/` module prefix for imports
- Uses `slices.Contains` (stdlib) instead of handwritten `contains` helper
