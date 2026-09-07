package query

import (
	"slices"
	"strings"

	"github.com/muesli/termenv"

	"github.com/LiuYinCarl/gocg/graph"
)

var (
	ctrlGreen  = "\033[32m"
	ctrlRed    = "\033[31m"
	ctrlYellow = "\033[33m"
	ctrlReset  = "\033[0m"
)

func init() {
	if termenv.EnvColorProfile() == termenv.Ascii {
		ctrlGreen, ctrlRed, ctrlYellow, ctrlReset = "", "", "", ""
	}
}

func Search(g *graph.Graph, keyword string) []string {
	var matches []string
	for _, name := range g.FullNames {
		if strings.Contains(name, keyword) {
			matches = append(matches, name)
		}
	}
	return matches
}

func resolveName(g *graph.Graph, check map[string][]string, input string) string {
	if _, ok := check[input]; ok {
		return input
	}
	matches := Search(g, input)
	if len(matches) == 1 {
		return matches[0]
	}
	return ""
}

func PrintCallGraph(g *graph.Graph, fun string, maxDepth int) []string {
	name := resolveName(g, g.CallGraph, fun)
	if name == "" {
		return matchList(g, fun)
	}
	buf := []string{colorFunc(name)}
	printCalls(g, name, nil, nil, 0, maxDepth, &buf)
	return buf
}

func PrintFilterCallGraph(g *graph.Graph, fun string, filterKwds []string, maxDepth int) []string {
	name := resolveName(g, g.CallGraph, fun)
	if name == "" {
		return matchList(g, fun)
	}
	keep := make(map[string]bool)
	markFilter(g, name, filterKwds, nil, 0, maxDepth, keep)
	buf := []string{colorFunc(name)}
	printFiltered(g, name, keep, nil, nil, 0, maxDepth, &buf)
	return buf
}

func PrintIgnoreCallGraph(g *graph.Graph, fun string, ignoreKwds []string, maxDepth int) []string {
	name := resolveName(g, g.CallGraph, fun)
	if name == "" {
		return matchList(g, fun)
	}
	buf := []string{colorFunc(name)}
	printIgnored(g, name, ignoreKwds, nil, nil, 0, maxDepth, &buf)
	return buf
}

func PrintRefGraph(g *graph.Graph, fun string, maxDepth int) []string {
	name := resolveName(g, g.RefGraph, fun)
	if name == "" {
		return matchList(g, fun)
	}
	buf := []string{name}
	printRefs(g, name, nil, nil, 0, maxDepth, &buf)
	return buf
}

func matchList(g *graph.Graph, fun string) []string {
	buf := []string{ctrlYellow + "matching list:" + ctrlReset}
	for _, m := range Search(g, fun) {
		buf = append(buf, colorFunc(m))
	}
	return buf
}

func colorFunc(name string) string {
	return ctrlGreen + name + ctrlReset
}

func treePrefix(last []bool, isLast bool, color string) string {
	var b strings.Builder
	for _, l := range last {
		if l {
			b.WriteString("   ")
		} else {
			b.WriteString(color + "|" + ctrlReset + "  ")
		}
	}
	if isLast {
		b.WriteString(color + "\\--" + ctrlReset)
	} else {
		b.WriteString(color + "|--" + ctrlReset)
	}
	return b.String()
}

func containsAny(s string, kwds []string) bool {
	for _, kw := range kwds {
		if strings.Contains(s, kw) {
			return true
		}
	}
	return false
}

func printCalls(g *graph.Graph, fun string, seen []string, last []bool, depth, maxDepth int, buf *[]string) {
	if depth >= maxDepth {
		return
	}
	callees, ok := g.CallGraph[fun]
	if !ok {
		return
	}
	for i, c := range callees {
		isLast := i == len(callees)-1
		*buf = append(*buf, treePrefix(last, isLast, ctrlGreen)+c)
		if slices.Contains(seen, c) {
			continue
		}
		seen = append(seen, c)
		if _, ok := g.CallGraph[c]; ok {
			printCalls(g, c, seen, append(last, isLast), depth+1, maxDepth, buf)
		}
	}
}

func markFilter(g *graph.Graph, fun string, kwds, seen []string, depth, maxDepth int, keep map[string]bool) bool {
	if depth >= maxDepth {
		return false
	}
	callees, ok := g.CallGraph[fun]
	if !ok {
		return false
	}
	any := false
	for _, c := range callees {
		if slices.Contains(seen, c) {
			continue
		}
		seen = append(seen, c)
		hit := containsAny(c, kwds)
		sub := markFilter(g, c, kwds, seen, depth+1, maxDepth, keep)
		if hit || sub {
			keep[c] = true
			any = true
		}
	}
	return any
}

func printFiltered(g *graph.Graph, fun string, keep map[string]bool, seen []string, last []bool, depth, maxDepth int, buf *[]string) {
	if depth >= maxDepth {
		return
	}
	callees, ok := g.CallGraph[fun]
	if !ok {
		return
	}
	vis := make([]string, 0, len(callees))
	for _, c := range callees {
		if keep[c] {
			vis = append(vis, c)
		}
	}
	for i, c := range vis {
		isLast := i == len(vis)-1
		*buf = append(*buf, treePrefix(last, isLast, ctrlGreen)+c)
		if slices.Contains(seen, c) {
			continue
		}
		seen = append(seen, c)
		printFiltered(g, c, keep, seen, append(last, isLast), depth+1, maxDepth, buf)
	}
}

func printIgnored(g *graph.Graph, fun string, ignoreKwds, seen []string, last []bool, depth, maxDepth int, buf *[]string) {
	if depth >= maxDepth {
		return
	}
	callees, ok := g.CallGraph[fun]
	if !ok {
		return
	}
	vis := make([]string, 0, len(callees))
	for _, c := range callees {
		if !containsAny(c, ignoreKwds) {
			vis = append(vis, c)
		}
	}
	for i, c := range vis {
		isLast := i == len(vis)-1
		*buf = append(*buf, treePrefix(last, isLast, ctrlGreen)+c)
		if slices.Contains(seen, c) {
			continue
		}
		seen = append(seen, c)
		if _, ok := g.CallGraph[c]; ok {
			printIgnored(g, c, ignoreKwds, seen, append(last, isLast), depth+1, maxDepth, buf)
		}
	}
}

func printRefs(g *graph.Graph, fun string, seen []string, last []bool, depth, maxDepth int, buf *[]string) {
	if depth >= maxDepth {
		return
	}
	refs, ok := g.RefGraph[fun]
	if !ok {
		return
	}
	for i, r := range refs {
		isLast := i == len(refs)-1
		*buf = append(*buf, treePrefix(last, isLast, ctrlRed)+r)
		if slices.Contains(seen, r) {
			continue
		}
		seen = append(seen, r)
		if _, ok := g.RefGraph[r]; ok {
			printRefs(g, r, seen, append(last, isLast), depth+1, maxDepth, buf)
		}
	}
}
