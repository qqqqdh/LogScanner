package tui

import (
	"fmt"
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

	// 선이 여러 개인 느낌을 주는 DoubleBorder 적용
	box        = lipgloss.NewStyle().Border(lipgloss.DoubleBorder()).Padding(0, 1)
	focusedBox = box.Copy().BorderForeground(lipgloss.Color("6"))

	// 상세 보기용 팝업 스타일
	modalBox = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(lipgloss.Color("5")). // 보라색 테두리
			Padding(1, 2).
			Background(lipgloss.Color("0"))

	headerBar = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, false, true, false).Padding(0, 1)

	badgeOK    = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	badgeRun   = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	badgePause = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	badgeWarn  = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	badgeErr   = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)

	selectedLineStyle = lipgloss.NewStyle().Background(lipgloss.Color("57")).Foreground(lipgloss.Color("229")).Bold(true)
	keyHint           = lipgloss.NewStyle().Faint(true)
)

type model struct {
	width  int
	height int

	started time.Time
	prog    progress.Model
	spin    spinner.Model
	tab     table.Model

	cfg     Config
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
	tailItems      []tailItem
	focus          focusArea

	// 페이지네이션 및 상세 보기 제어
	tailPageIndex     int
	tailSelectedIndex int
	autoPageFollow    bool
	showDetail        bool // [추가] 상세 보기 모드 여부

	panelHeight     int
	innerHeight     int
	tailPanelHeight int
	tailPanelWidth  int
	lastTableUpdate time.Time
}

func InitialModel(files []string, updates <-chan analyzer.Event, cfg Config, pauseFn func(bool)) model {
	p := progress.New(progress.WithDefaultGradient())
	p.Width = 40
	s := spinner.New()
	s.Spinner = spinner.Dot

	cols := []table.Column{
		{Title: "File", Width: 40},
		{Title: "Lines", Width: 10},
		{Title: "Matches", Width: 10},
		{Title: "Status", Width: 10},
	}

	rows := make([]table.Row, 0, len(files))
	rowIndex := make(map[string]int, len(files))
	for i, f := range files {
		rows = append(rows, table.Row{f, "0", "0", "WAIT"})
		rowIndex[f] = i
	}

	t := table.New(table.WithColumns(cols), table.WithRows(rows), table.WithFocused(false))
	st := table.DefaultStyles()
	st.Header = st.Header.Bold(true)
	st.Selected = st.Selected.Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57")).Bold(true)
	t.SetStyles(st)

	return model{
		started:         time.Now(),
		prog:            p,
		spin:            s,
		tab:             t,
		cfg:             cfg,
		updates:         updates,
		pauseFn:         pauseFn,
		filesTotal:      len(files),
		rowIndexByFile:  rowIndex,
		tailItems:       make([]tailItem, 0),
		focus:           focusTail,
		autoPageFollow:  true,
		lastTableUpdate: time.Now(),
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.spin.Tick, waitEvent(m.updates))
}

func waitEvent(ch <-chan analyzer.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return ev
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.prog.Width = clamp(m.width-12, 20, 90)
		topAndBottom := 10
		m.panelHeight = maxInt(10, m.height-topAndBottom)
		m.innerHeight = maxInt(3, m.panelHeight-2)
		m.tailPanelHeight = m.innerHeight

		leftW := clamp(m.width/2, 40, 90)
		rightW := maxInt(30, m.width-leftW-3)

		m.tailPanelWidth = leftW - 4
		m.tab.SetWidth(rightW - 4)
		m.tab.SetHeight(m.innerHeight - 1)
		return m, nil

	case tea.KeyMsg:
		// [추가] 상세 보기 모드일 때 닫기 로직
		if m.showDetail {
			switch msg.String() {
			case "esc", "enter", "q":
				m.showDetail = false
				return m, nil
			}
			return m, nil
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "tab":
			if m.focus == focusTail {
				m.focus = focusTable
				m.tab.Focus()
			} else {
				m.focus = focusTail
				m.tab.Blur()
			}
			return m, nil
		case "enter":
			// [추가] 로그 창에서 엔터 누르면 전문 보기 활성화
			if m.focus == focusTail && len(m.tailItems) > 0 {
				m.showDetail = true
			}
			return m, nil
		case "p":
			m.paused = !m.paused
			if m.pauseFn != nil {
				m.pauseFn(m.paused)
			}
			return m, nil
		case "left", "right", "up", "down":
			if m.focus == focusTail {
				m.autoPageFollow = false
				pageSize := m.tailPanelHeight
				total := len(m.tailItems)
				if total == 0 {
					return m, nil
				}
				totalPages := (total + pageSize - 1) / maxInt(1, pageSize)

				switch msg.String() {
				case "left":
					if m.tailPageIndex > 0 {
						m.tailPageIndex--
					}
				case "right":
					if m.tailPageIndex < totalPages-1 {
						m.tailPageIndex++
					}
				case "up":
					if m.tailSelectedIndex > 0 {
						m.tailSelectedIndex--
						if m.tailSelectedIndex < m.tailPageIndex*pageSize {
							m.tailPageIndex--
						}
					}
				case "down":
					if m.tailSelectedIndex < total-1 {
						m.tailSelectedIndex++
						if m.tailSelectedIndex >= (m.tailPageIndex+1)*pageSize {
							m.tailPageIndex++
						}
					}
				}
				return m, nil
			}
		case "f":
			m.autoPageFollow = true
			return m, nil
		}

		if m.focus == focusTable {
			var cmd tea.Cmd
			m.tab, cmd = m.tab.Update(msg)
			return m, cmd
		}

	case analyzer.MatchLine:
		text := fmt.Sprintf("[%s] %s", msg.File, msg.Line)
		m.tailItems = append(m.tailItems, tailItem{Seq: msg.Seq, Text: text})
		m.matches++
		pageSize := m.tailPanelHeight
		totalLogs := len(m.tailItems)
		lastLogPage := (totalLogs - 1) / maxInt(1, pageSize)
		if m.autoPageFollow {
			m.tailPageIndex = lastLogPage
			m.tailSelectedIndex = totalLogs - 1
		}
		return m, waitEvent(m.updates)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case analyzer.FileUpdate:
		u := msg
		if idx, ok := m.rowIndexByFile[u.File]; ok {
			if time.Since(m.lastTableUpdate) > 100*time.Millisecond || u.Status == "DONE" {
				rows := m.tab.Rows()
				rows[idx] = table.Row{u.File, fmt.Sprintf("%d", u.Lines), fmt.Sprintf("%d", u.Matches), u.Status}
				m.tab.SetRows(rows)
				m.lastTableUpdate = time.Now()
			}
		}
		return m, waitEvent(m.updates)

	case analyzer.Totals:
		s := msg
		if s.Err != nil {
			m.err = s.Err
		}
		m.filesDone, m.linesTotal = s.FilesDone, s.LinesTotal
		if s.MatchesTotal > 0 {
			m.matches = s.MatchesTotal
		}
		percent := float64(m.filesDone) / float64(maxInt(1, m.filesTotal))
		cmd := m.prog.SetPercent(percent)
		if s.Done {
			m.done = true
		} else {
			cmd = tea.Batch(cmd, waitEvent(m.updates))
		}
		return m, cmd
	}
	return m, nil
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
	header := headerBar.Width(maxInt(0, m.width-2)).Render(headLeft + "\n" + cDim.Render(fmt.Sprintf("keyword=%s  workers=%d", m.cfg.Keyword, m.cfg.Concurrent)))

	bar := m.prog.ViewAs(float64(m.filesDone) / float64(maxInt(1, m.filesTotal)))
	stats := cDim.Render(fmt.Sprintf("Files %d/%d  Lines %d  Matches %d", m.filesDone, m.filesTotal, m.linesTotal, m.matches))
	top := joinLines(header, "", bar, stats)

	leftW := clamp(m.width/2, 40, 90)
	rightW := maxInt(30, m.width-leftW-3)

	tStyle, rStyle := box, box
	if m.focus == focusTable {
		tStyle = focusedBox
	}
	if m.focus == focusTail {
		rStyle = focusedBox
	}

	pageSize := m.tailPanelHeight
	totalLogs := len(m.tailItems)
	totalPages := (totalLogs + pageSize - 1) / maxInt(1, pageSize)
	if totalPages == 0 {
		totalPages = 1
	}

	followStatus := ""
	if m.autoPageFollow {
		followStatus = " [FOLLOW]"
	}
	tailTitle := fmt.Sprintf("Matches (Page %d/%d)%s", m.tailPageIndex+1, totalPages, followStatus)
	tailContent := strings.Join(m.renderTailLines(pageSize), "\n")
	tailBox := rStyle.Width(leftW).Height(m.panelHeight).Render(cTitle.Render(tailTitle) + "\n" + tailContent)

	tableBox := tStyle.Width(rightW).Height(m.panelHeight).Render(cTitle.Render("Files") + "\n" + m.tab.View())

	row := lipgloss.JoinHorizontal(lipgloss.Top, tailBox, " ", tableBox)
	hint := keyHint.Render("Enter: Full Detail | Tab: Focus | Arrows: Page/Select | F: Follow | Q: Quit")

	mainView := joinLines(top, "", row, "", hint)

	// [추가] 상세 보기 팝업 렌더링
	if m.showDetail && len(m.tailItems) > 0 {
		selectedLog := m.tailItems[m.tailSelectedIndex].Text
		// 팝업 가로 길이를 전체 화면의 80%로 설정
		modalWidth := int(float64(m.width) * 0.8)
		detailView := modalBox.Width(modalWidth).Render(
			cTitle.Render("--- FULL LOG DETAIL ---") + "\n\n" +
				lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Render(selectedLog) + "\n\n" +
				keyHint.Render("Press Enter or Esc to close"),
		)

		// 화면 정중앙에 배치 (단순 조인으로 처리)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, detailView)
	}

	return mainView
}

func (m model) renderTailLines(height int) []string {
	if height <= 0 {
		return []string{}
	}
	start := m.tailPageIndex * height
	end := start + height
	if end > len(m.tailItems) {
		end = len(m.tailItems)
	}

	out := make([]string, 0, height)
	if start < len(m.tailItems) {
		for i := start; i < end; i++ {
			it := m.tailItems[i]
			lineText := it.Text
			// 목록에서는 텍스트를 자르지만, 상세 보기에서는 전문을 보여줄 예정
			if len(lineText) > m.tailPanelWidth {
				lineText = lineText[:m.tailPanelWidth-3] + "..."
			}

			var styledLine string
			if i == m.tailSelectedIndex && m.focus == focusTail {
				styledLine = selectedLineStyle.Width(m.tailPanelWidth).Render(lineText)
			} else {
				styledLine = m.highlightLine(lineText)
			}
			out = append(out, styledLine)
		}
	}
	for len(out) < height {
		out = append(out, lipgloss.NewStyle().Width(m.tailPanelWidth).Render(""))
	}
	return out
}

func (m model) highlightLine(line string) string {
	style := lipgloss.NewStyle().Width(m.tailPanelWidth).MaxWidth(m.tailPanelWidth)
	if strings.Contains(line, "ERROR") {
		return badgeErr.Inherit(style).Render(line)
	}
	if strings.Contains(line, "WARN") {
		return badgeWarn.Inherit(style).Render(line)
	}
	return style.Render(line)
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
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
