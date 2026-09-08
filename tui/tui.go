package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/LiuYinCarl/gocg/graph"
	"github.com/LiuYinCarl/gocg/query"
)

const (
	defaultPrintDepth = 15
	maxDepth          = 15
	maxMatches        = 200
	hScrollStep       = 8
)

type uiState int

const (
	stateLoading uiState = iota
	stateReady
	stateFailed
)

type buildResult struct {
	g       *graph.Graph
	stats   *graph.Stats
	elapsed time.Duration
	err     error
}

type model struct {
	dir     string
	exclude []string
	noCache bool

	st       uiState
	sp       spinner.Model
	input    textinput.Model
	vp       viewport.Model
	vpFocus  bool
	focusCmd tea.Cmd

	g       *graph.Graph
	stats   *graph.Stats
	elapsed time.Duration
	err     error

	matches    []string
	lowerNames []string
	selected   int
	listOffset int

	filterSet  map[string]bool
	ignoreSet  map[string]bool
	printDepth int

	locked     string
	lockedMode string
	status     string

	treeLines    []string
	hOffset      int
	maxTreeWidth int

	width  int
	height int
}

var (
	borderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("10"))
	activeBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("12"))
	statusStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	errStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	selectMarker = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render("> ")
)

func Run(dir string, exclude []string, noCache bool) error {
	sp := spinner.New(spinner.WithSpinner(spinner.Dot))
	in := textinput.New()
	in.Placeholder = "type to filter functions (? ! & @ prefixes supported)"
	focusCmd := in.Focus()

	m := &model{
		dir:        dir,
		exclude:    exclude,
		noCache:    noCache,
		st:         stateLoading,
		sp:         sp,
		input:      in,
		focusCmd:   focusCmd,
		filterSet:  make(map[string]bool),
		ignoreSet:  make(map[string]bool),
		printDepth: defaultPrintDepth,
		width:      100,
		height:     30,
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.sp.Tick, m.buildCmd(), m.focusCmd)
}

func (m *model) buildCmd() tea.Cmd {
	dir, exclude, noCache := m.dir, m.exclude, m.noCache
	return func() tea.Msg {
		start := time.Now()
		g, stats, err := graph.Build(dir, exclude, noCache)
		return buildResult{g: g, stats: stats, elapsed: time.Since(start), err: err}
	}
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeViewport()
		return m, nil

	case buildResult:
		if msg.err != nil {
			m.st = stateFailed
			m.err = msg.err
			return m, nil
		}
		m.st = stateReady
		m.g = msg.g
		m.stats = msg.stats
		m.elapsed = msg.elapsed
		if len(msg.stats.Warnings) > 0 {
			m.status = "warning: " + msg.stats.Warnings[0]
		}
		m.lowerNames = make([]string, len(m.g.FullNames))
		for i, name := range m.g.FullNames {
			m.lowerNames[i] = strings.ToLower(name)
		}
		m.recomputeMatches()
		m.resizeViewport()
		return m, nil

	case spinner.TickMsg:
		if m.st == stateLoading {
			var cmd tea.Cmd
			m.sp, cmd = m.sp.Update(msg)
			return m, cmd
		}
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		switch m.st {
		case stateLoading:
			return m, nil
		case stateFailed:
			if msg.String() == "q" {
				return m, tea.Quit
			}
			return m, nil
		}
		return m.handleKey(msg)
	}

	if m.st == stateReady && !m.vpFocus {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if m.vpFocus {
		switch key {
		case "q":
			return m, tea.Quit
		case "tab", "/", "i":
			m.vpFocus = false
			return m, m.input.Focus()
		case "left", "h":
			m.scrollH(-hScrollStep)
			return m, nil
		case "right", "l":
			m.scrollH(hScrollStep)
			return m, nil
		case "y":
			m.copyTree()
			return m, nil
		}
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}

	switch key {
	case "tab", "esc":
		m.vpFocus = true
		m.input.Blur()
		return m, nil
	case "up":
		m.moveSelection(-1)
		return m, nil
	case "down":
		m.moveSelection(1)
		return m, nil
	case "pgup", "pgdown":
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	case "enter":
		m.handleEnter()
		return m, nil
	}

	before := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() != before {
		m.recomputeMatches()
	}
	return m, cmd
}

func (m *model) moveSelection(delta int) {
	if len(m.matches) == 0 {
		return
	}
	m.selected += delta
	if m.selected < 0 {
		m.selected = 0
	}
	if m.selected >= len(m.matches) {
		m.selected = len(m.matches) - 1
	}
	m.clampListOffset()
}

func (m *model) clampListOffset() {
	rows := m.listRows()
	if m.selected < m.listOffset {
		m.listOffset = m.selected
	}
	if m.selected >= m.listOffset+rows {
		m.listOffset = m.selected - rows + 1
	}
}

func (m *model) listRows() int {
	rows := m.height - 5
	if rows < 1 {
		rows = 1
	}
	return rows
}

func (m *model) resizeViewport() {
	left := m.leftWidth()
	w := m.width - left - 4
	if w < 10 {
		w = 10
	}
	m.vp.Width = w
	m.vp.Height = m.listRows()
	if len(m.treeLines) > 0 {
		m.scrollH(0)
	}
}

func (m *model) leftWidth() int {
	w := m.width / 3
	if w > 40 {
		w = 40
	}
	if w < 20 {
		w = 20
	}
	return w
}

func (m *model) modeAndFilter() (string, string) {
	v := m.input.Value()
	if len(v) >= 2 && (v[0] == '?' || v[0] == '!' || v[0] == '&') && v[1] == ' ' {
		return string(v[0]), strings.TrimSpace(v[2:])
	}
	if strings.HasPrefix(v, "@") {
		return "@", ""
	}
	return "", v
}

func (m *model) recomputeMatches() {
	if m.st != stateReady {
		return
	}
	mode, kw := m.modeAndFilter()
	prompt := ">>> "
	if mode != "" {
		prompt = mode + " >>> "
	}
	m.input.Prompt = prompt
	if mode == "@" {
		m.matches = nil
		m.selected = 0
		m.listOffset = 0
		return
	}
	kw = strings.ToLower(kw)
	var matches []string
	for i, name := range m.g.FullNames {
		if kw == "" || strings.Contains(m.lowerNames[i], kw) {
			matches = append(matches, name)
			if len(matches) >= maxMatches {
				break
			}
		}
	}
	m.matches = matches
	if m.selected >= len(m.matches) {
		m.selected = len(m.matches) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
	m.clampListOffset()
}

func (m *model) handleEnter() {
	mode, _ := m.modeAndFilter()
	if mode == "@" {
		m.handleCommand(m.input.Value())
		m.input.SetValue("")
		m.recomputeMatches()
		return
	}
	if len(m.matches) == 0 {
		return
	}
	m.locked = m.matches[m.selected]
	m.lockedMode = mode
	m.renderTree()
}

func (m *model) renderTree() {
	if m.locked == "" || m.g == nil {
		return
	}
	var lines []string
	switch m.lockedMode {
	case "?":
		lines = query.PrintFilterCallGraph(m.g, m.locked, sortedKeys(m.filterSet), m.printDepth)
	case "!":
		lines = query.PrintIgnoreCallGraph(m.g, m.locked, sortedKeys(m.ignoreSet), m.printDepth)
	case "&":
		lines = query.PrintRefGraph(m.g, m.locked, m.printDepth)
	default:
		lines = query.PrintCallGraph(m.g, m.locked, m.printDepth)
	}
	m.treeLines = lines
	m.hOffset = 0
	m.maxTreeWidth = 0
	for _, l := range lines {
		if w := lipgloss.Width(l); w > m.maxTreeWidth {
			m.maxTreeWidth = w
		}
	}
	m.syncTree(true)
}

func (m *model) syncTree(gotoTop bool) {
	shifted := make([]string, len(m.treeLines))
	right := m.hOffset + m.vp.Width
	for i, l := range m.treeLines {
		shifted[i] = ansi.Cut(l, m.hOffset, right)
	}
	m.vp.SetContent(strings.Join(shifted, "\n"))
	if gotoTop {
		m.vp.GotoTop()
	}
}

func (m *model) scrollH(delta int) {
	if len(m.treeLines) == 0 {
		return
	}
	m.hOffset += delta
	maxOff := m.maxTreeWidth - m.vp.Width
	if maxOff < 0 {
		maxOff = 0
	}
	if m.hOffset > maxOff {
		m.hOffset = maxOff
	}
	if m.hOffset < 0 {
		m.hOffset = 0
	}
	m.syncTree(false)
}

func (m *model) copyTree() {
	if len(m.treeLines) == 0 {
		m.status = "no tree to copy"
		return
	}
	text := ansi.Strip(strings.Join(m.treeLines, "\n"))
	if err := clipboard.WriteAll(text); err != nil {
		m.status = "copy failed: " + err.Error()
		return
	}
	m.status = fmt.Sprintf("copied %d lines (%d bytes)", len(m.treeLines), len(text))
}

func (m *model) handleCommand(line string) {
	args := strings.Fields(line)
	if len(args) < 2 {
		m.status = usageText
		return
	}
	dirty := true
	switch args[1] {
	case "filter":
		for _, kw := range args[2:] {
			m.filterSet[kw] = true
		}
		m.status = fmt.Sprintf("filter set: %v", sortedKeys(m.filterSet))
	case "ignore":
		for _, kw := range args[2:] {
			m.ignoreSet[kw] = true
		}
		m.status = fmt.Sprintf("ignore set: %v", sortedKeys(m.ignoreSet))
	case "del_fi":
		for _, kw := range args[2:] {
			delete(m.filterSet, kw)
		}
		m.status = fmt.Sprintf("filter set: %v", sortedKeys(m.filterSet))
	case "del_ig":
		for _, kw := range args[2:] {
			delete(m.ignoreSet, kw)
		}
		m.status = fmt.Sprintf("ignore set: %v", sortedKeys(m.ignoreSet))
	case "depth":
		if len(args) < 3 {
			m.status = "usage: @ depth N"
			return
		}
		d, err := strconv.Atoi(args[2])
		if err != nil || d <= 0 || d > maxDepth {
			m.status = fmt.Sprintf("invalid depth %q (valid range: 1-%d)", args[2], maxDepth)
			return
		}
		m.printDepth = d
		m.status = fmt.Sprintf("print depth: %d", d)
	case "show":
		dirty = false
		m.status = fmt.Sprintf("filter: %v | ignore: %v | depth: %d",
			sortedKeys(m.filterSet), sortedKeys(m.ignoreSet), m.printDepth)
	case "reset":
		m.filterSet = make(map[string]bool)
		m.ignoreSet = make(map[string]bool)
		m.printDepth = defaultPrintDepth
		m.status = "reset finish"
	default:
		m.status = usageText
		return
	}
	if dirty && m.locked != "" {
		m.renderTree()
	}
}

const usageText = "@ filter|ignore kw... | @ del_fi|del_ig kw... | @ depth N | @ show | @ reset"

func (m *model) View() string {
	switch m.st {
	case stateLoading:
		return fmt.Sprintf("\n  %s building call graph for %s ... (ctrl+c to quit)\n", m.sp.View(), m.dir)
	case stateFailed:
		return fmt.Sprintf("\n  %s\n\n  press q or ctrl+c to quit\n", errStyle.Render("error: "+m.err.Error()))
	}

	left := m.renderList()
	right := m.renderViewport()
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	status := m.statusLine()
	return lipgloss.JoinVertical(lipgloss.Left, m.input.View(), body, status)
}

func (m *model) renderList() string {
	rows := m.listRows()
	w := m.leftWidth() - 2

	var b strings.Builder
	end := m.listOffset + rows
	if end > len(m.matches) {
		end = len(m.matches)
	}
	for i := m.listOffset; i < end; i++ {
		name := truncate(m.matches[i], w-3)
		if i == m.selected {
			b.WriteString(selectMarker)
			b.WriteString(name)
		} else {
			b.WriteString("  ")
			b.WriteString(name)
		}
		if i < end-1 {
			b.WriteByte('\n')
		}
	}
	if len(m.matches) == 0 {
		b.WriteString("  (no match)")
	}

	style := borderStyle
	if !m.vpFocus {
		style = activeBorderStyle
	}
	return style.Width(m.leftWidth() - 2).Height(m.listRows()).Render(b.String())
}

func (m *model) renderViewport() string {
	style := borderStyle
	if m.vpFocus {
		style = activeBorderStyle
	}
	title := ""
	if m.locked != "" {
		title = m.locked
		if m.hOffset > 0 {
			title += fmt.Sprintf("  ←+%d", m.hOffset)
		}
	}
	content := m.vp.View()
	if title != "" {
		content = statusStyle.Render("tree: "+title) + "\n" + content
	}
	return style.Width(m.vp.Width).Height(m.listRows()).Render(content)
}

func (m *model) statusLine() string {
	cache := "no"
	if m.stats != nil && m.stats.Cached {
		cache = "yes"
	}
	left := fmt.Sprintf("funcs:%d edges:%d load:%.2fs cache:%s",
		m.stats.Functions, m.stats.Edges, m.elapsed.Seconds(), cache)
	if n := len(m.stats.Warnings); n > 0 {
		left += fmt.Sprintf(" warn:%d", n)
	}
	left += fmt.Sprintf(" | filter:%v ignore:%v depth:%d",
		sortedKeys(m.filterSet), sortedKeys(m.ignoreSet), m.printDepth)
	right := "tab:pane enter:tree ↑↓/←→:scroll y:copy q:quit"
	line := left + "  |  " + right
	if m.status != "" {
		line = m.status + "  |  " + left
	}
	return statusStyle.Render(truncate(line, m.width-1))
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	return string(r[:w])
}
