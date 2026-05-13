package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gocg/graph"
	"gocg/query"
	"gocg/repl"
)

func main() {
	args := os.Args[1:]
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "usage: %s <directory> [-x prefix1,prefix2] [--lookup func] [--no-cache] [--clear-cache]\n", os.Args[0])
		os.Exit(1)
	}

	dir := "."
	excludePrefixes := []string{}
	lookup := ""
	noCache := false
	clearCache := false

	i := 0
	for i < len(args) {
		switch args[i] {
		case "-x":
			i++
			if i < len(args) {
				for _, p := range strings.Split(args[i], ",") {
					p = strings.TrimSpace(p)
					if p != "" {
						excludePrefixes = append(excludePrefixes, p)
					}
				}
			}
		case "--lookup":
			i++
			if i < len(args) {
				lookup = args[i]
			}
		case "--no-cache":
			noCache = true
		case "--clear-cache":
			clearCache = true
		default:
			if !strings.HasPrefix(args[i], "-") && dir == "." {
				dir = args[i]
			}
		}
		i++
	}

	if clearCache {
		removed, err := graph.ClearCache(dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("cleared cache files: %d\n", removed)
		return
	}

	fmt.Fprintf(os.Stderr, "loading Go packages from %s...\n", dir)
	start := time.Now()

	g, stats, err := graph.Build(dir, excludePrefixes, noCache)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	elapsed := time.Since(start)
	cacheStatus := "no"
	if stats.Cached {
		cacheStatus = "yes"
	}
	fmt.Printf("load summary: packages=%d, functions=%d, edges=%d, seconds=%.3f, cache=%s\n",
		stats.Packages, stats.Functions, stats.Edges, elapsed.Seconds(), cacheStatus)

	if lookup != "" {
		lines := query.PrintCallGraph(g, lookup, 15)
		fmt.Println()
		for _, l := range lines {
			fmt.Println(l)
		}
		return
	}

	s := repl.NewState(g)
	repl.Run(s)
}
