package graph

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/callgraph/cha"
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

	cg := cha.CallGraph(prog)

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

		callerName := caller.String()
		if matchesExclude(callerName, excludePrefixes) {
			continue
		}

		for _, edge := range node.Out {
			callee := edge.Callee.Func
			calleeName := callee.String()

			if matchesExclude(calleeName, excludePrefixes) {
				continue
			}

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
		if strings.HasPrefix(name, p) {
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
