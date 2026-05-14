package query

import (
	"slices"
	"sort"
	"strings"

	"gocg/graph"
)

const (
	ctrlGreen  = "\033[32m"
	ctrlRed    = "\033[31m"
	ctrlYellow = "\033[33m"
	ctrlReset  = "\033[0m"
)

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
	var buf []string
	name := resolveName(g, g.CallGraph, fun)
	if name != "" {
		buf = append(buf, colorFunc(name))
		printCalls(g, name, []string{}, 0, maxDepth, &buf)
	} else {
		buf = append(buf, ctrlYellow+"matching list:"+ctrlReset)
		matches := Search(g, fun)
		for _, m := range matches {
			buf = append(buf, colorFunc(m))
		}
	}
	return buf
}

func PrintFilterCallGraph(g *graph.Graph, fun string, filterKwds []string, maxDepth int) []string {
	var buf []string
	name := resolveName(g, g.CallGraph, fun)
	if name != "" {
		buf = append(buf, colorFunc(name))
		filterCalls(g, name, []string{}, []string{}, filterKwds, 0, maxDepth, &buf)
	}
	return buf
}

func PrintIgnoreCallGraph(g *graph.Graph, fun string, ignoreKwds []string, maxDepth int) []string {
	var buf []string
	name := resolveName(g, g.CallGraph, fun)
	if name != "" {
		buf = append(buf, colorFunc(name))
		ignoreCalls(g, name, []string{}, ignoreKwds, 0, maxDepth, &buf)
	}
	return buf
}

func PrintRefGraph(g *graph.Graph, fun string, maxDepth int) []string {
	var buf []string
	name := resolveName(g, g.RefGraph, fun)
	if name != "" {
		buf = append(buf, name)
		printRefs(g, name, []string{}, 0, maxDepth, &buf)
	}
	return buf
}

func colorFunc(name string) string {
	return ctrlGreen + name + ctrlReset
}

func printCalls(g *graph.Graph, fun string, seen []string, depth, maxDepth int, buf *[]string) {
	if depth >= maxDepth {
		return
	}
	callees, ok := g.CallGraph[fun]
	if !ok {
		return
	}
	sort.Strings(callees)
	for _, c := range callees {
		prefix := strings.Repeat(ctrlGreen+"|"+ctrlReset+"  ", depth) + ctrlGreen+"|--"+ctrlReset
		line := prefix + c
		*buf = append(*buf, line)

		if slices.Contains(seen, c) {
			continue
		}
		seen = append(seen, c)
		if _, ok := g.CallGraph[c]; ok {
			printCalls(g, c, seen, depth+1, maxDepth, buf)
		}
	}
}

func filterCalls(g *graph.Graph, fun string, stack, seen, filterKwds []string, depth, maxDepth int, buf *[]string) {
	if depth >= maxDepth {
		return
	}
	callees, ok := g.CallGraph[fun]
	if !ok {
		return
	}
	sort.Strings(callees)
	for _, c := range callees {
		prefix := strings.Repeat(ctrlGreen+"|"+ctrlReset+"  ", depth) + ctrlGreen+"|--"+ctrlReset
		line := prefix + c
		stack = append(stack, line)

		hit := false
		for _, kw := range filterKwds {
			if strings.Contains(c, kw) {
				hit = true
				break
			}
		}
		if hit {
			for _, s := range stack {
				*buf = append(*buf, s)
			}
		}

		if slices.Contains(seen, c) {
			stack = stack[:len(stack)-1]
			continue
		}
		seen = append(seen, c)
		if _, ok := g.CallGraph[c]; ok {
			filterCalls(g, c, stack, seen, filterKwds, depth+1, maxDepth, buf)
		}
		stack = stack[:len(stack)-1]
	}
}

func ignoreCalls(g *graph.Graph, fun string, seen []string, ignoreKwds []string, depth, maxDepth int, buf *[]string) {
	if depth >= maxDepth {
		return
	}
	callees, ok := g.CallGraph[fun]
	if !ok {
		return
	}
	sort.Strings(callees)
	for _, c := range callees {
		hit := false
		for _, kw := range ignoreKwds {
			if strings.Contains(c, kw) {
				hit = true
				break
			}
		}
		if hit {
			continue
		}

		prefix := strings.Repeat(ctrlGreen+"|"+ctrlReset+"  ", depth) + ctrlGreen+"|--"+ctrlReset
		line := prefix + c
		*buf = append(*buf, line)

		if slices.Contains(seen, c) {
			continue
		}
		seen = append(seen, c)
		if _, ok := g.CallGraph[c]; ok {
			ignoreCalls(g, c, seen, ignoreKwds, depth+1, maxDepth, buf)
		}
	}
}

func printRefs(g *graph.Graph, fun string, seen []string, depth, maxDepth int, buf *[]string) {
	if depth >= maxDepth {
		return
	}
	refs, ok := g.RefGraph[fun]
	if !ok {
		return
	}
	sort.Strings(refs)
	for _, r := range refs {
		prefix := strings.Repeat(ctrlRed+"|"+ctrlReset+"  ", depth) + ctrlRed+"|--"+ctrlReset
		line := prefix + r
		*buf = append(*buf, line)

		if slices.Contains(seen, r) {
			continue
		}
		seen = append(seen, r)
		if _, ok := g.RefGraph[r]; ok {
			printRefs(g, r, seen, depth+1, maxDepth, buf)
		}
	}
}
