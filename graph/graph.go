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
	Warnings  []string
}

const cacheVersion = 3

func Build(dir string, excludePrefixes []string, noCache bool) (*Graph, *Stats, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve dir: %w", err)
	}

	if _, err := os.Stat(absDir); err != nil {
		return nil, nil, fmt.Errorf("directory not found: %s", absDir)
	}
	if resolved, err := filepath.EvalSymlinks(absDir); err == nil {
		absDir = resolved
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
	var warnings []string
	errCount := 0
	for _, pkg := range pkgs {
		for _, e := range pkg.Errors {
			errCount++
			if len(warnings) < 20 {
				warnings = append(warnings, fmt.Sprintf("%s: %s", pkg.PkgPath, e))
			}
		}
		if len(pkg.Errors) > 0 {
			continue
		}
		if isProjectPkg(pkg, absDir) {
			projectPkgs++
		}
	}
	if errCount > len(warnings) {
		warnings = append(warnings, fmt.Sprintf("... and %d more package errors", errCount-len(warnings)))
	}

	prog, _ := ssautil.AllPackages(pkgs, ssa.BuilderMode(0))
	prog.Build()

	shortMap := buildShortMap(pkgs)

	sortedPaths := make([]string, 0, len(shortMap))
	for p := range shortMap {
		sortedPaths = append(sortedPaths, p)
	}
	sort.Slice(sortedPaths, func(i, j int) bool {
		return len(sortedPaths[i]) > len(sortedPaths[j])
	})

	displayCache := make(map[string]string)
	excludeCache := make(map[string]bool)

	funcs := ssautil.AllFunctions(prog)

	cg := vta.CallGraph(funcs, nil)

	callTemp := make(map[string]map[string]struct{})
	refTemp := make(map[string]map[string]struct{})
	nameSet := make(map[string]bool)
	edgeCount := 0

	for fn, node := range cg.Nodes {
		if fn == nil {
			continue
		}

		if !isInProject(fn, absDir) {
			continue
		}

		fnStr := fn.String()
		if _, ok := excludeCache[fnStr]; !ok {
			excludeCache[fnStr] = matchesExclude(fnStr, excludePrefixes)
		}
		if excludeCache[fnStr] {
			continue
		}

		if _, ok := displayCache[fnStr]; !ok {
			displayCache[fnStr] = funcDisplayName(fnStr, shortMap, sortedPaths)
		}
		callerName := displayCache[fnStr]
		nameSet[callerName] = true

		for _, edge := range node.Out {
			callee := edge.Callee.Func
			calleeStr := callee.String()
			if _, ok := excludeCache[calleeStr]; !ok {
				excludeCache[calleeStr] = matchesExclude(calleeStr, excludePrefixes)
			}
			if excludeCache[calleeStr] {
				continue
			}

			if _, ok := displayCache[calleeStr]; !ok {
				displayCache[calleeStr] = funcDisplayName(calleeStr, shortMap, sortedPaths)
			}
			calleeName := displayCache[calleeStr]

			if callTemp[callerName] == nil {
				callTemp[callerName] = make(map[string]struct{})
			}
			callTemp[callerName][calleeName] = struct{}{}

			if refTemp[calleeName] == nil {
				refTemp[calleeName] = make(map[string]struct{})
			}
			refTemp[calleeName][callerName] = struct{}{}

			nameSet[calleeName] = true
			edgeCount++
		}
	}

	g := &Graph{
		CallGraph: make(map[string][]string, len(callTemp)),
		RefGraph:  make(map[string][]string, len(refTemp)),
	}

	for caller, callees := range callTemp {
		slice := make([]string, 0, len(callees))
		for c := range callees {
			slice = append(slice, c)
		}
		sort.Strings(slice)
		g.CallGraph[caller] = slice
	}

	for callee, callers := range refTemp {
		slice := make([]string, 0, len(callers))
		for c := range callers {
			slice = append(slice, c)
		}
		sort.Strings(slice)
		g.RefGraph[callee] = slice
	}

	for name := range nameSet {
		g.FullNames = append(g.FullNames, name)
	}
	sort.Strings(g.FullNames)

	stats := &Stats{
		Packages:  projectPkgs,
		Functions: len(nameSet),
		Edges:     edgeCount,
		Warnings:  warnings,
	}

	return g, stats, nil
}

func dirCacheHash(dir string) string {
	sum := sha256.Sum256([]byte(dir))
	return fmt.Sprintf("%x", sum)[:16]
}

func cachePath(dir string, excludePrefixes []string) (string, error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}

	h := sha256.New()
	fmt.Fprintf(h, "v%d\n", cacheVersion)
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

	hash := fmt.Sprintf("%x", h.Sum(nil))[:32]
	cacheDir := filepath.Join(root, "gocg")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, dirCacheHash(dir)+"-"+hash+".json"), nil
}

type cachePayload struct {
	CallGraph map[string][]string `json:"callgraph"`
	RefGraph  map[string][]string `json:"refgraph"`
	FullNames []string            `json:"fullnames"`
	Packages  int                 `json:"packages"`
	Functions int                 `json:"functions"`
	Edges     int                 `json:"edges"`
	Warnings  []string            `json:"warnings,omitempty"`
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
		Warnings:  p.Warnings,
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
		Warnings:  stats.Warnings,
	}
	b, err := json.Marshal(p)
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
	if resolved, err := filepath.EvalSymlinks(absDir); err == nil {
		absDir = resolved
	}

	removed := 0
	if root, err := os.UserCacheDir(); err == nil {
		cacheDir := filepath.Join(root, "gocg")
		if entries, err := os.ReadDir(cacheDir); err == nil {
			prefix := dirCacheHash(absDir) + "-"
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), prefix) && strings.HasSuffix(e.Name(), ".json") {
					if os.Remove(filepath.Join(cacheDir, e.Name())) == nil {
						removed++
					}
				}
			}
		}
	}

	legacy := filepath.Join(absDir, ".gocg-cache")
	if entries, err := os.ReadDir(legacy); err == nil {
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".json") {
				removed++
			}
		}
		os.RemoveAll(legacy)
	}
	return removed, nil
}

func buildShortMap(pkgs []*packages.Package) map[string]string {
	pathToName := make(map[string]string)
	seen := make(map[string]bool)
	var collect func(pkg *packages.Package)
	collect = func(pkg *packages.Package) {
		if pkg == nil || seen[pkg.ID] {
			return
		}
		seen[pkg.ID] = true
		if pkg.PkgPath != "" && pkg.Name != "" {
			pathToName[pkg.PkgPath] = pkg.Name
		}
		for _, imp := range pkg.Imports {
			collect(imp)
		}
	}
	for _, pkg := range pkgs {
		collect(pkg)
	}

	m := make(map[string]string)
	byName := make(map[string][]string)
	for path, name := range pathToName {
		byName[name] = append(byName[name], path)
	}
	for name, paths := range byName {
		if len(paths) == 1 {
			m[paths[0]] = name
			continue
		}
		resolveAmbiguous(m, name, paths)
	}
	return m
}

func resolveAmbiguous(m map[string]string, name string, paths []string) {
	partsOf := make(map[string][]string, len(paths))
	maxSeg := 0
	for _, p := range paths {
		parts := strings.Split(p, "/")
		partsOf[p] = parts
		if len(parts) > maxSeg {
			maxSeg = len(parts)
		}
	}
	candidate := func(p string, level int) string {
		parts := partsOf[p]
		switch level {
		case 0:
			if len(parts) >= 2 {
				return parts[len(parts)-2] + "/" + name
			}
			return p
		case 1:
			return parts[len(parts)-1] + "/" + name
		default:
			k := level
			if k > len(parts) {
				k = len(parts)
			}
			return strings.Join(parts[len(parts)-k:], "/")
		}
	}
	assigned := make(map[string]bool)
	unresolved := paths
	for level := 0; len(unresolved) > 0 && level <= maxSeg; level++ {
		count := make(map[string]int)
		cand := make(map[string]string)
		for _, p := range unresolved {
			c := candidate(p, level)
			cand[p] = c
			count[c]++
		}
		var next []string
		for _, p := range unresolved {
			if count[cand[p]] == 1 && !assigned[cand[p]] {
				m[p] = cand[p]
				assigned[cand[p]] = true
			} else {
				next = append(next, p)
			}
		}
		unresolved = next
	}
	for _, p := range unresolved {
		m[p] = p
	}
}

func funcDisplayName(full string, shortMap map[string]string, sortedPaths []string) string {
	for _, path := range sortedPaths {
		full = strings.ReplaceAll(full, path, shortMap[path])
	}
	return full
}

func pathInDir(dir, file string) bool {
	rel, err := filepath.Rel(dir, file)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func isProjectPkg(pkg *packages.Package, dir string) bool {
	for _, f := range pkg.GoFiles {
		abs, err := filepath.Abs(f)
		if err != nil {
			continue
		}
		if pathInDir(dir, abs) {
			return true
		}
	}
	return false
}

func isInProject(fn *ssa.Function, dir string) bool {
	pos := fn.Prog.Fset.Position(fn.Pos())
	return pos.Filename != "" && pathInDir(dir, pos.Filename)
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
