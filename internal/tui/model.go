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
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type focusArea int

const (
	focusTable focusArea = iota
	focusTail
)

type tailItem struct {
	Seq  uint64
	Text string
}

var (
	cTitle = lipgloss.NewStyle().Bold(true)
	cDim   = lipgloss.NewStyle().Faint(true)

	box = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(0, 1)

	headerBar = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, true, false).
			Padding(0, 1)

	badgeOK    = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	badgeRun   = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	badgePause = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)

	badgeWarn = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	badgeErr  = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)

	keyHint = lipgloss.NewStyle().Faint(true)
)

type model struct {
	width  int
	height int

	started time.Time

	prog progress.Model
	spin spinner.Model
	tab  table.Model

	cfg Config

	updates <-chan analyzer.Event
	pauseFn func(bool)

	filesTotal int
	filesDone  int
	linesTotal int64
	matches    int64

	done   bool
	paused bool
	err    error

	rowIndexByFile map[string]int

	tailItems []tailItem
	focus     focusArea

	// panel sizing (content area)
	panelHeight     int // 박스 전체 높이
	innerHeight     int // 박스 내부 콘텐츠 높이(타이틀 제외)
	tailPanelHeight int // 오른쪽에 표시할 라인 수 (= innerHeight)
	tailPanelWidth  int // 오른쪽 내부폭(줄을 끝까지 채우기)
}

func initialModel(files []string, updates <-chan analyzer.Event, cfg Config, pauseFn func(bool)) model {
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
	// 초기값은 적당히. 실제 높이/폭은 WindowSizeMsg에서 맞춤.
	t.SetHeight(minInt(12, len(rows)+1))

	st := table.DefaultStyles()
	st.Header = st.Header.Bold(true)
	st.Selected = st.Selected.Bold(true)
	t.SetStyles(st)

	return model{
		started:        time.Now(),
		prog:           p,
		spin:           s,
		tab:            t,
		cfg:            cfg,
		updates:        updates,
		pauseFn:        pauseFn,
		filesTotal:     len(files),
		rowIndexByFile: rowIndex,
		tailItems:      make([]tailItem, 0, cfg.TailMax),
		focus:          focusTable,

		panelHeight:     12,
		innerHeight:     9,
		tailPanelHeight: 9,
		tailPanelWidth:  40,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.spin.Tick, waitEvent(m.updates))
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

		// View()에서 찍는 줄 수와 맞춰서 content 영역 높이 계산
		// header(2) + blank(1) + bar(1) + stats(1) = 5
		// 그리고 joinLines에서 top, "", row, "", hint => 빈줄 2 + hint 1
		// 합: 5 + 3 = 8
		topAndBottom := 8

		contentH := m.height - topAndBottom
		if contentH < 10 {
			contentH = 10
		}
		m.panelHeight = contentH

		// box는 border 상하 2줄 + title 1줄이 들어가므로 내부 콘텐츠 높이:
		innerH := contentH - 3
		if innerH < 3 {
			innerH = 3
		}
		m.innerHeight = innerH
		m.tailPanelHeight = innerH

		// layout widths (View와 동일)
		leftW := clamp(m.width/2, 40, 90)
		rightW := maxInt(30, m.width-leftW-3)

		// box 내부폭 = width - (border2 + padding2) = width - 4
		leftInnerW := maxInt(1, leftW-4)
		rightInnerW := maxInt(1, rightW-4)
		m.tailPanelWidth = rightInnerW

		// ✅ 테이블 폭을 box 내부폭에 맞춤 (wrap 방지의 시작)
		m.tab.SetWidth(leftInnerW)

		// ✅ 컬럼 폭을 "테이블 폭" 기준으로 재계산해서 wrap 안 나게
		// (구분자/패딩 여유를 조금 잡아줌)
		linesW := 8
		matchesW := 8
		statusW := 8
		gutter := 6 // 컬럼 사이 공백/구분자 여유

		fileW := leftInnerW - (linesW + matchesW + statusW + gutter)
		fileW = clamp(fileW, 20, 120) // 너무 작아져서 또 wrap 나지 않게 최소 20

		cols := m.tab.Columns()
		if len(cols) >= 4 {
			cols[0].Width = fileW
			cols[1].Width = linesW
			cols[2].Width = matchesW
			cols[3].Width = statusW
			m.tab.SetColumns(cols)
		}

		// ✅ 테이블 높이도 내부 콘텐츠 높이에 맞춤 (아래 빈칸 방지)
		m.tab.SetHeight(innerH)

		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "tab":
			if m.focus == focusTable {
				m.focus = focusTail
			} else {
				m.focus = focusTable
			}
			return m, nil
		case "p":
			m.paused = !m.paused
			if m.pauseFn != nil {
				m.pauseFn(m.paused)
			}
			return m, nil
		default:
			if m.focus == focusTable {
				var cmd tea.Cmd
				m.tab, cmd = m.tab.Update(msg)
				return m, cmd
			}
			return m, nil
		}

	case analyzer.FileUpdate:
		u := msg
		if idx, ok := m.rowIndexByFile[u.File]; ok {
			rows := m.tab.Rows()
			lines := "-"
			matches := "-"
			status := u.Status
			if status == "" {
				status = "WAIT"
			}
			if status != "WAIT" {
				lines = fmt.Sprintf("%d", u.Lines)
				matches = fmt.Sprintf("%d", u.Matches)
			}
			rows[idx] = table.Row{u.File, lines, matches, status}
			m.tab.SetRows(rows)
		}
		return m, tea.Batch(waitEvent(m.updates))

	case analyzer.MatchLine:
		text := fmt.Sprintf("[%s] %s", msg.File, msg.Line)
		m.tailItems = append(m.tailItems, tailItem{Seq: msg.Seq, Text: text})

		sort.Slice(m.tailItems, func(i, j int) bool { return m.tailItems[i].Seq < m.tailItems[j].Seq })
		if len(m.tailItems) > m.cfg.TailMax {
			m.tailItems = m.tailItems[len(m.tailItems)-m.cfg.TailMax:]
		}
		return m, tea.Batch(waitEvent(m.updates))

	case analyzer.Totals:
		s := msg
		if s.Err != nil {
			m.err = s.Err
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
			return m, cmd
		}
		return m, tea.Batch(cmd, waitEvent(m.updates))

	default:
		return m, nil
	}
}

func (m model) View() string {
	statusBadge := badgeRun.Render(" SCANNING ")
	if m.paused {
		statusBadge = badgePause.Render(" PAUSED ")
	}
	if m.done {
		statusBadge = badgeOK.Render(" DONE ")
	}

	headLeft := cTitle.Render("Go-LogScanner") + " " + statusBadge
	headRight := cDim.Render(fmt.Sprintf("keyword=%s  workers=%d  tail=%d", m.cfg.Keyword, m.cfg.Concurrent, m.cfg.TailMax))
	header := headerBar.Width(maxInt(0, m.width-2)).Render(headLeft + "\n" + headRight)

	elapsed := time.Since(m.started).Truncate(100 * time.Millisecond)
	percent := 0.0
	if m.filesTotal > 0 {
		percent = float64(m.filesDone) / float64(m.filesTotal)
	}
	bar := m.prog.ViewAs(percent)

	stats := fmt.Sprintf("Files %d/%d  Lines %d  Matches %d  Elapsed %s",
		m.filesDone, m.filesTotal, m.linesTotal, m.matches, elapsed)

	top := joinLines(header, "", bar, cDim.Render(stats))

	// layout widths
	leftW := clamp(m.width/2, 40, 90)
	rightW := maxInt(30, m.width-leftW-3)

	// ✅ 박스 높이를 content 높이로 고정해서 아래 빈칸을 없앰
	tableBox := box.Width(leftW).Height(m.panelHeight).Render(
		cTitle.Render("Files") + "\n" + m.tab.View(),
	)

	tailLines := m.renderTailLines(m.tailPanelHeight)
	tailContent := strings.Join(tailLines, "\n")
	tailBox := box.Width(rightW).Height(m.panelHeight).Render(
		cTitle.Render("Recent Matches") + "\n" + tailContent,
	)

	row := lipgloss.JoinHorizontal(lipgloss.Top, tableBox, " ", tailBox)

	hint := ""
	if m.done {
		hint = keyHint.Render("Done. Press q to quit | tab focus | ↑/↓ table | p pause/resume")
	} else {
		hint = keyHint.Render("Keys: q quit | tab focus | ↑/↓ table | p pause/resume")
	}

	if m.err != nil {
		return joinLines(top, "", badgeErr.Render("ERROR: "+m.err.Error()), "", row, "", hint)
	}
	return joinLines(top, "", row, "", hint)
}

func (m model) renderTailLines(height int) []string {
	if height <= 0 {
		return []string{}
	}

	start := 0
	if len(m.tailItems) > height {
		start = len(m.tailItems) - height
	}

	out := make([]string, 0, height)
	for _, it := range m.tailItems[start:] {
		out = append(out, m.highlightLine(it.Text))
	}

	// 항상 height 줄 채우기 (폭도 내부폭으로)
	for len(out) < height {
		out = append(out, lipgloss.NewStyle().Width(m.tailPanelWidth).Render(""))
	}
	return out
}

// 오른쪽 패널 줄을 내부 폭만큼 강제로 늘려서 "오른쪽으로 빈칸"이 보기 좋게
func (m model) highlightLine(line string) string {
	w := m.tailPanelWidth
	if w <= 0 {
		w = 1
	}

	styled := line
	if strings.Contains(line, "ERROR") {
		styled = badgeErr.Render(line)
	} else if strings.Contains(line, "WARN") {
		styled = badgeWarn.Render(line)
	}

	return lipgloss.NewStyle().
		Width(w).
		MaxWidth(w).
		Render(styled)
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
