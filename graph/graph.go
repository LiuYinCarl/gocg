package graph

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/callgraph/vta"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

type Graph struct {
	CallGraph map[string][]string
	RefGraph  map[string][]string
	FullNames []string
}

type Stats struct {
	Packages  int
	Functions int
	Edges     int
	Cached    bool
}

const cacheVersion = 1

func Build(dir string, excludePrefixes []string, noCache bool) (*Graph, *Stats, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve dir: %w", err)
	}

	if _, err := os.Stat(absDir); err != nil {
		return nil, nil, fmt.Errorf("directory not found: %s", absDir)
	}

	cacheFile, _ := cachePath(absDir, excludePrefixes)
	if !noCache && cacheFile != "" {
		if g, stats, err := loadCache(cacheFile); err == nil {
			stats.Cached = true
			return g, stats, nil
		}
	}

	g, stats, err := buildFromSource(absDir, excludePrefixes)
	if err != nil {
		return nil, nil, err
	}

	stats.Cached = false
	if !noCache && cacheFile != "" {
		saveCache(cacheFile, g, stats)
	}

	return g, stats, nil
}

func buildFromSource(absDir string, excludePrefixes []string) (*Graph, *Stats, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedImports | packages.NeedDeps | packages.NeedTypes |
			packages.NeedTypesInfo | packages.NeedSyntax,
		Dir: absDir,
	}

	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load packages: %w", err)
	}

	projectPkgs := 0
	for _, pkg := range pkgs {
		if len(pkg.Errors) > 0 {
			continue
		}
		if isProjectPkg(pkg, absDir) {
			projectPkgs++
		}
	}

	prog, _ := ssautil.AllPackages(pkgs, ssa.BuilderMode(0))
	prog.Build()

	shortMap := buildShortMap(pkgs)

	funcs := make(map[*ssa.Function]bool)
	for _, pkg := range prog.AllPackages() {
		for _, member := range pkg.Members {
			if fn, ok := member.(*ssa.Function); ok {
				funcs[fn] = true
			}
		}
	}

	cg := vta.CallGraph(funcs, nil)

	g := &Graph{
		CallGraph: make(map[string][]string),
		RefGraph:  make(map[string][]string),
	}

	for fn, node := range cg.Nodes {
		caller := fn
		if caller == nil {
			continue
		}

		if !isInProject(caller, absDir) {
			continue
		}

		if matchesExclude(caller.String(), excludePrefixes) {
			continue
		}

		callerName := funcDisplayName(caller, shortMap)

		for _, edge := range node.Out {
			callee := edge.Callee.Func
			if matchesExclude(callee.String(), excludePrefixes) {
				continue
			}

			calleeName := funcDisplayName(callee, shortMap)

			g.CallGraph[callerName] = appendUnique(g.CallGraph[callerName], calleeName)
			g.RefGraph[calleeName] = appendUnique(g.RefGraph[calleeName], callerName)
		}
	}

	nameSet := make(map[string]bool)
	for k := range g.CallGraph {
		nameSet[k] = true
	}
	for _, callees := range g.CallGraph {
		for _, c := range callees {
			nameSet[c] = true
		}
	}
	for name := range nameSet {
		g.FullNames = append(g.FullNames, name)
	}
	sort.Strings(g.FullNames)

	edgeCount := 0
	for _, callees := range g.CallGraph {
		edgeCount += len(callees)
	}

	stats := &Stats{
		Packages:  projectPkgs,
		Functions: len(nameSet),
		Edges:     edgeCount,
	}

	return g, stats, nil
}

func cachePath(dir string, excludePrefixes []string) (string, error) {
	h := sha256.New()
	fmt.Fprintf(h, "v%d\n", cacheVersion)
	fmt.Fprintf(h, "%s\n", dir)
	for _, p := range excludePrefixes {
		fmt.Fprintf(h, "x:%s\n", p)
	}

	type fileInfo struct {
		path  string
		size  int64
		mtime int64
	}
	var files []fileInfo
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if strings.HasPrefix(base, ".") && path != dir {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") {
			files = append(files, fileInfo{path, info.Size(), info.ModTime().UnixNano()})
		}
		return nil
	})

	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	for _, fi := range files {
		fmt.Fprintf(h, "%s:%d:%d\n", fi.path, fi.size, fi.mtime)
	}

	hash := fmt.Sprintf("%x", h.Sum(nil))
	cacheDir := filepath.Join(dir, ".gocg-cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, hash+".json"), nil
}

type cachePayload struct {
	CallGraph map[string][]string `json:"callgraph"`
	RefGraph  map[string][]string `json:"refgraph"`
	FullNames []string            `json:"fullnames"`
	Packages  int                 `json:"packages"`
	Functions int                 `json:"functions"`
	Edges     int                 `json:"edges"`
}

func loadCache(path string) (*Graph, *Stats, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var p cachePayload
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, nil, err
	}
	return &Graph{
		CallGraph: p.CallGraph,
		RefGraph:  p.RefGraph,
		FullNames: p.FullNames,
	}, &Stats{
		Packages:  p.Packages,
		Functions: p.Functions,
		Edges:     p.Edges,
	}, nil
}

func saveCache(path string, g *Graph, stats *Stats) error {
	p := cachePayload{
		CallGraph: g.CallGraph,
		RefGraph:  g.RefGraph,
		FullNames: g.FullNames,
		Packages:  stats.Packages,
		Functions: stats.Functions,
		Edges:     stats.Edges,
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0644)
}

func ClearCache(dir string) (int, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return 0, err
	}
	cacheDir := filepath.Join(absDir, ".gocg-cache")
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return 0, nil
	}
	removed := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			os.Remove(filepath.Join(cacheDir, e.Name()))
			removed++
		}
	}
	return removed, nil
}

func buildShortMap(pkgs []*packages.Package) map[string]string {
	pathToName := make(map[string]string)
	nameCount := make(map[string]int)
	seen := make(map[string]bool)
	var collect func(pkg *packages.Package)
	collect = func(pkg *packages.Package) {
		if pkg == nil || seen[pkg.ID] {
			return
		}
		seen[pkg.ID] = true
		if pkg.PkgPath != "" && pkg.Name != "" {
			pathToName[pkg.PkgPath] = pkg.Name
			nameCount[pkg.Name]++
		}
		for _, imp := range pkg.Imports {
			collect(imp)
		}
	}
	for _, pkg := range pkgs {
		collect(pkg)
	}

	m := make(map[string]string)
	for path, name := range pathToName {
		if nameCount[name] > 1 {
			parts := strings.Split(path, "/")
			if len(parts) >= 2 {
				m[path] = parts[len(parts)-2] + "/" + name
			} else {
				m[path] = path
			}
		} else {
			m[path] = name
		}
	}
	return m
}

func funcDisplayName(fn *ssa.Function, shortMap map[string]string) string {
	if fn == nil {
		return ""
	}
	full := fn.String()
	// Sort paths longest-first to avoid partial replacements
	paths := make([]string, 0, len(shortMap))
	for p := range shortMap {
		paths = append(paths, p)
	}
	sort.Slice(paths, func(i, j int) bool {
		return len(paths[i]) > len(paths[j])
	})
	for _, path := range paths {
		full = strings.ReplaceAll(full, path, shortMap[path])
	}
	return full
}

func isProjectPkg(pkg *packages.Package, dir string) bool {
	for _, f := range pkg.GoFiles {
		abs, err := filepath.Abs(f)
		if err != nil {
			continue
		}
		if strings.HasPrefix(abs, dir) {
			return true
		}
	}
	return false
}

func isInProject(fn *ssa.Function, dir string) bool {
	pos := fn.Prog.Fset.Position(fn.Pos())
	return strings.HasPrefix(pos.Filename, dir)
}

func matchesExclude(name string, prefixes []string) bool {
	for _, p := range prefixes {
		if p == "" {
			continue
		}
		if strings.Contains(name, p) {
			return true
		}
	}
	return false
}

func appendUnique(slice []string, item string) []string {
	for _, s := range slice {
		if s == item {
			return slice
		}
	}
	return append(slice, item)
}
