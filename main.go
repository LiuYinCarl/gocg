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
		fmt.Fprintf(os.Stderr, "usage: %s <directory> [-x prefix1,prefix2] [--lookup func]\n", os.Args[0])
		os.Exit(1)
	}

	dir := "."
	excludePrefixes := []string{}
	lookup := ""

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
		default:
			if !strings.HasPrefix(args[i], "-") && dir == "." {
				dir = args[i]
			}
		}
		i++
	}

	fmt.Fprintf(os.Stderr, "loading Go packages from %s...\n", dir)
	start := time.Now()

	g, stats, err := graph.Build(dir, excludePrefixes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	elapsed := time.Since(start)
	fmt.Printf("load summary: packages=%d, functions=%d, edges=%d, seconds=%.3f\n",
		stats.Packages, stats.Functions, stats.Edges, elapsed.Seconds())

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
