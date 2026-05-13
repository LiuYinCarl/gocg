package graph

import (
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
}

func Build(dir string, excludePrefixes []string) (*Graph, *Stats, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve dir: %w", err)
	}

	if _, err := os.Stat(absDir); err != nil {
		return nil, nil, fmt.Errorf("directory not found: %s", absDir)
	}

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
