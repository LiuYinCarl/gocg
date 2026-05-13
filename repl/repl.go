package repl

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/chzyer/readline"

	"gocg/graph"
	"gocg/query"
)

const (
	ctrlGreen = "\033[32m"
	ctrlReset = "\033[0m"
)

type State struct {
	FilterSet  map[string]bool
	IgnoreSet  map[string]bool
	MaxDepth   int
	PrintDepth int
	G          *graph.Graph
}

const defaultMaxDepth = 15

func NewState(g *graph.Graph) *State {
	return &State{
		FilterSet:  make(map[string]bool),
		IgnoreSet:  make(map[string]bool),
		MaxDepth:   defaultMaxDepth,
		PrintDepth: defaultMaxDepth,
		G:          g,
	}
}

func Run(s *State) {
	rl, err := readline.New(">>> ")
	if err != nil {
		return
	}
	defer rl.Close()

	rl.Config.AutoComplete = readline.NewPrefixCompleter(
		readline.PcItem("@",
			readline.PcItem("filter"),
			readline.PcItem("ignore"),
			readline.PcItem("del_fi"),
			readline.PcItem("del_ig"),
			readline.PcItem("depth"),
			readline.PcItem("show"),
			readline.PcItem("reset"),
		),
		readline.PcItem("?", readline.PcItemDynamic(listFunctions(s))),
		readline.PcItem("!", readline.PcItemDynamic(listFunctions(s))),
		readline.PcItem("&", readline.PcItemDynamic(listFunctions(s))),
		readline.PcItemDynamic(listFunctions(s)),
	)

	for {
		line, err := rl.Readline()
		if err != nil {
			if err == io.EOF {
				break
			}
			break
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		handle(s, line)
	}
}

func listFunctions(s *State) func(string) []string {
	return func(line string) []string {
		var matches []string
		lower := strings.ToLower(line)
		for _, name := range s.G.FullNames {
			if strings.HasPrefix(strings.ToLower(name), lower) {
				matches = append(matches, name)
			}
			if len(matches) >= 100 {
				break
			}
		}
		return matches
	}
}

var usageMsg = `
Usage:
    @ ignore keyword1 [keyword2] ...    add ignore keywords
    @ filter keyword1 [keyword2] ...    add filter keywords
    @ del_ig keyword1 [keyword2] ...    del ignore keywords
    @ del_fi keyword1 [keyword2] ...    del filter keywords
    @ depth  n                          set max print depth
    @ show                              show query config
    @ reset                             reset query config
    ? complete_function_name            show call graph to function contain 'filter' keywords
    ! complete_function_name            show call graph without 'ignore' keywords
    & complete_function_name            show reference of function
`

func handle(s *State, line string) {
	switch {
	case line == "@":
		fmt.Print(ctrlGreen, usageMsg, ctrlReset)
	case line == "@ show":
		fmt.Printf("%sfilter set: %v%s\n", ctrlGreen, setKeys(s.FilterSet), ctrlReset)
		fmt.Printf("%signore set: %v%s\n", ctrlGreen, setKeys(s.IgnoreSet), ctrlReset)
		fmt.Printf("%sprint depth: %d%s\n", ctrlGreen, s.PrintDepth, ctrlReset)
		fmt.Printf("%smax print depth: %d%s\n", ctrlGreen, s.MaxDepth, ctrlReset)
	case line == "@ reset":
		s.FilterSet = make(map[string]bool)
		s.IgnoreSet = make(map[string]bool)
		s.PrintDepth = defaultMaxDepth
		fmt.Println("reset finish")
	case strings.HasPrefix(line, "@ filter "):
		args := strings.Fields(line)
		for _, kw := range args[2:] {
			s.FilterSet[kw] = true
		}
		fmt.Printf("update filter set:%s %v%s\n", ctrlGreen, setKeys(s.FilterSet), ctrlReset)
	case strings.HasPrefix(line, "@ ignore "):
		args := strings.Fields(line)
		for _, kw := range args[2:] {
			s.IgnoreSet[kw] = true
		}
		fmt.Printf("update ignore set:%s %v%s\n", ctrlGreen, setKeys(s.IgnoreSet), ctrlReset)
	case strings.HasPrefix(line, "@ del_fi "):
		args := strings.Fields(line)
		for _, kw := range args[2:] {
			delete(s.FilterSet, kw)
		}
	case strings.HasPrefix(line, "@ del_ig "):
		args := strings.Fields(line)
		for _, kw := range args[2:] {
			delete(s.IgnoreSet, kw)
		}
	case strings.HasPrefix(line, "@ depth "):
		args := strings.Fields(line)
		if len(args) < 3 {
			return
		}
		d, err := strconv.Atoi(args[2])
		if err != nil || d <= 0 || d > s.MaxDepth {
			return
		}
		s.PrintDepth = d
	case strings.HasPrefix(line, "? "):
		parts := strings.SplitN(line, " ", 2)
		if len(parts) < 2 {
			return
		}
		fun := parts[1]
		filterKwds := setKeys(s.FilterSet)
		lines := query.PrintFilterCallGraph(s.G, fun, filterKwds, s.PrintDepth)
		fmt.Println()
		for _, l := range lines {
			fmt.Println(l)
		}
	case strings.HasPrefix(line, "! "):
		parts := strings.SplitN(line, " ", 2)
		if len(parts) < 2 {
			return
		}
		fun := parts[1]
		ignoreKwds := setKeys(s.IgnoreSet)
		lines := query.PrintIgnoreCallGraph(s.G, fun, ignoreKwds, s.PrintDepth)
		fmt.Println()
		for _, l := range lines {
			fmt.Println(l)
		}
	case strings.HasPrefix(line, "& "):
		parts := strings.SplitN(line, " ", 2)
		if len(parts) < 2 {
			return
		}
		fun := parts[1]
		lines := query.PrintRefGraph(s.G, fun, s.PrintDepth)
		fmt.Println()
		for _, l := range lines {
			fmt.Println(l)
		}
	default:
		lines := query.PrintCallGraph(s.G, line, s.PrintDepth)
		fmt.Println()
		for _, l := range lines {
			fmt.Println(l)
		}
	}
}

func setKeys(m map[string]bool) []string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
