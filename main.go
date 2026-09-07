package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/LiuYinCarl/gocg/graph"
	"github.com/LiuYinCarl/gocg/query"
	"github.com/LiuYinCarl/gocg/repl"
	"github.com/LiuYinCarl/gocg/tui"
)

func main() {
	args := os.Args[1:]
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "usage: %s <directory> [-x prefix1,prefix2] [--lookup func] [--tui] [--no-cache] [--clear-cache]\n", os.Args[0])
		os.Exit(1)
	}

	dir := "."
	dirSet := false
	excludePrefixes := []string{}
	lookup := ""
	noCache := false
	clearCache := false
	tuiMode := false

	i := 0
	for i < len(args) {
		switch args[i] {
		case "-x":
			i++
			if i >= len(args) {
				fmt.Fprintf(os.Stderr, "error: -x requires a value\n")
				os.Exit(1)
			}
			for p := range strings.SplitSeq(args[i], ",") {
				p = strings.TrimSpace(p)
				if p != "" {
					excludePrefixes = append(excludePrefixes, p)
				}
			}
		case "--lookup":
			i++
			if i >= len(args) {
				fmt.Fprintf(os.Stderr, "error: --lookup requires a value\n")
				os.Exit(1)
			}
			lookup = args[i]
		case "--no-cache":
			noCache = true
		case "--clear-cache":
			clearCache = true
		case "--tui":
			tuiMode = true
		default:
			if strings.HasPrefix(args[i], "-") {
				fmt.Fprintf(os.Stderr, "error: unknown flag %s\n", args[i])
				os.Exit(1)
			}
			if dirSet {
				fmt.Fprintf(os.Stderr, "error: multiple directories given (%s, %s)\n", dir, args[i])
				os.Exit(1)
			}
			dir = args[i]
			dirSet = true
		}
		i++
	}

	if tuiMode && lookup != "" {
		fmt.Fprintf(os.Stderr, "error: --tui and --lookup cannot be used together\n")
		os.Exit(1)
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

	if tuiMode {
		if err := tui.Run(dir, excludePrefixes, noCache); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
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
	for _, w := range stats.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}
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
