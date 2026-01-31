package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"logscanner/internal/analyzer"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type focusArea int

const (
	focusTable focusArea = iota
	focusTail
)

var (
	cTitle = lipgloss.NewStyle().Bold(true)
	cDim   = lipgloss.NewStyle().Faint(true)

	box = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(0, 1)

	headerBar = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, true, false).
			Padding(0, 1)

	badgeOK = lipgloss.NewStyle().
		Foreground(lipgloss.Color("10")).
		Bold(true)

	badgeRun = lipgloss.NewStyle().
			Foreground(lipgloss.Color("12")).
			Bold(true)

	badgeWarn = lipgloss.NewStyle().
			Foreground(lipgloss.Color("11")).
			Bold(true)

	badgeErr = lipgloss.NewStyle().
			Foreground(lipgloss.Color("9")).
			Bold(true)

	keyHint = lipgloss.NewStyle().Faint(true)
)

type model struct {
	width  int
	height int

	started time.Time

	prog progress.Model
	spin spinner.Model
	tab  table.Model
	tail viewport.Model

	cfg Config

	updates <-chan analyzer.Event

	// totals
	filesTotal int
	filesDone  int
	linesTotal int64
	matches    int64

	done bool
	err  error

	// file -> row index
	rowIndexByFile map[string]int

	// tail buffer
	tailLines []string

	focus focusArea
}

func initialModel(files []string, updates <-chan analyzer.Event, cfg Config) model {
	p := progress.New(progress.WithDefaultGradient())
	p.Width = 40

	s := spinner.New()
	s.Spinner = spinner.Dot

	sorted := append([]string(nil), files...)
	sort.Strings(sorted)

	cols := []table.Column{
		{Title: "File", Width: 44},
		{Title: "Lines", Width: 10},
		{Title: "Matches", Width: 10},
		{Title: "Status", Width: 8},
	}

	rows := make([]table.Row, 0, len(sorted))
	rowIndex := make(map[string]int, len(sorted))

	for i, f := range sorted {
		rows = append(rows, table.Row{f, "-", "-", "WAIT"})
		rowIndex[f] = i
	}

	t := table.New(table.WithColumns(cols), table.WithRows(rows), table.WithFocused(true))
	t.SetHeight(minInt(12, len(rows)+1))

	// ✅ 테이블 스타일 살짝 업그레이드
	st := table.DefaultStyles()
	st.Header = st.Header.Bold(true)
	st.Selected = st.Selected.Bold(true)
	t.SetStyles(st)

	vp := viewport.New(80, 8)
	vp.SetContent("")

	return model{
		started:        time.Now(),
		prog:           p,
		spin:           s,
		tab:            t,
		tail:           vp,
		cfg:            cfg,
		updates:        updates,
		filesTotal:     len(files),
		rowIndexByFile: rowIndex,
		tailLines:      make([]string, 0, cfg.TailMax),
		focus:          focusTable,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		m.spin.Tick,
		waitEvent(m.updates),
	)
}

func waitEvent(ch <-chan analyzer.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return analyzer.Totals{Done: true}
		}
		return ev
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.prog.Width = clamp(m.width-12, 20, 90)

		// 2컬럼 레이아웃 기준으로 컬럼 너비 튜닝
		leftW := clamp(m.width/2, 40, 90)
		fileColWidth := clamp(leftW-26, 20, 80)

		cols := m.tab.Columns()
		cols[0].Width = fileColWidth
		m.tab.SetColumns(cols)

		// tail viewport
		rightW := maxInt(30, m.width-leftW-3)
		m.tail.Width = clamp(rightW-4, 26, 120)

		// 세로 공간이 넉넉하면 tail 높이 살짝 증가
		if m.height > 32 {
			m.tail.Height = 10
		} else {
			m.tail.Height = 8
		}

		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.done = true
			return m, tea.Quit
		case "tab":
			if m.focus == focusTable {
				m.focus = focusTail
			} else {
				m.focus = focusTable
			}
			return m, nil
		default:
			// route arrows to focused widget
			if m.focus == focusTable {
				var cmd tea.Cmd
				m.tab, cmd = m.tab.Update(msg)
				return m, cmd
			}
			var cmd tea.Cmd
			m.tail, cmd = m.tail.Update(msg)
			return m, cmd
		}

	// analyzer events
	case analyzer.FileUpdate:
		u := msg

		idx, ok := m.rowIndexByFile[u.File]
		if ok {
			rows := m.tab.Rows()
			lines := "-"
			matches := "-"
			if u.Status != "WAIT" {
				lines = fmt.Sprintf("%d", u.Lines)
				matches = fmt.Sprintf("%d", u.Matches)
			}
			rows[idx] = table.Row{u.File, lines, matches, u.Status}
			m.tab.SetRows(rows)
		}
		return m, tea.Batch(waitEvent(m.updates))

	case analyzer.MatchLine:
		line := fmt.Sprintf("[%s] %s", msg.File, msg.Line)
		m.tailLines = append(m.tailLines, line)
		if len(m.tailLines) > m.cfg.TailMax {
			m.tailLines = m.tailLines[len(m.tailLines)-m.cfg.TailMax:]
		}

		// ✅ ERROR/WARN 하이라이트 적용
		pretty := make([]string, 0, len(m.tailLines))
		for _, ln := range m.tailLines {
			pretty = append(pretty, highlight(ln))
		}
		m.tail.SetContent(strings.Join(pretty, "\n"))
		m.tail.GotoBottom()

		return m, tea.Batch(waitEvent(m.updates))

	case analyzer.Totals:
		s := msg
		if s.Err != nil {
			m.err = s.Err
			m.done = true
			return m, tea.Quit
		}

		m.filesDone = s.FilesDone
		m.linesTotal = s.LinesTotal
		m.matches = s.MatchesTotal

		var percent float64
		if m.filesTotal > 0 {
			percent = float64(m.filesDone) / float64(m.filesTotal)
		}
		cmd := m.prog.SetPercent(percent)

		if s.Done {
			m.done = true
			return m, tea.Batch(cmd, tea.Quit)
		}
		return m, tea.Batch(cmd, waitEvent(m.updates))

	default:
		return m, nil
	}
}

func (m model) View() string {
	// 상태 배지
	statusBadge := badgeRun.Render(" SCANNING ")
	if m.done {
		statusBadge = badgeOK.Render(" DONE ")
	}

	// 헤더
	headLeft := cTitle.Render("Go-LogScanner") + " " + statusBadge
	headRight := cDim.Render(fmt.Sprintf("keyword=%s  workers=%d  tail=%d", m.cfg.Keyword, m.cfg.Concurrent, m.cfg.TailMax))
	header := headerBar.Width(maxInt(0, m.width-2)).Render(headLeft + "\n" + headRight)

	// 진행률/통계
	elapsed := time.Since(m.started).Truncate(100 * time.Millisecond)
	percent := 0.0
	if m.filesTotal > 0 {
		percent = float64(m.filesDone) / float64(m.filesTotal)
	}
	bar := m.prog.ViewAs(percent)

	stats := fmt.Sprintf("Files %d/%d  Lines %d  Matches %d  Elapsed %s",
		m.filesDone, m.filesTotal, m.linesTotal, m.matches, elapsed)

	top := joinLines(header, "", bar, cDim.Render(stats))

	// 좌/우 컬럼 레이아웃
	leftW := clamp(m.width/2, 40, 90)
	rightW := maxInt(30, m.width-leftW-3)

	// 테이블 박스
	tableTitle := cTitle.Render("Files")
	tableBox := box.Width(leftW).Render(tableTitle + "\n" + m.tab.View())

	// tail 박스
	tailTitle := cTitle.Render("Recent Matches")
	tailBox := box.Width(rightW).Render(tailTitle + "\n" + m.tail.View())

	row := lipgloss.JoinHorizontal(lipgloss.Top, tableBox, " ", tailBox)

	// 포커스/키 힌트
	focusTag := ""
	if m.focus == focusTable {
		focusTag = cDim.Render("Focus: TABLE (tab to switch)")
	} else {
		focusTag = cDim.Render("Focus: TAIL (tab to switch)")
	}

	hint := keyHint.Render("Keys: tab focus | ↑/↓ scroll (focused) | q quit")

	if m.err != nil {
		return joinLines(top, "", badgeErr.Render("ERROR: "+m.err.Error()))
	}

	return joinLines(
		top,
		"",
		focusTag,
		"",
		row,
		"",
		hint,
	)
}

func highlight(line string) string {
	if strings.Contains(line, "ERROR") {
		return badgeErr.Render(line)
	}
	if strings.Contains(line, "WARN") {
		return badgeWarn.Render(line)
	}
	return line
}

func joinLines(lines ...string) string { return strings.Join(lines, "\n") }

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
