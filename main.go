package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type statsSnapshot struct {
	FilesTotal   int
	FilesDone    int
	LinesTotal   int64
	MatchesTotal int64
	Done         bool
	Err          error
}

type statsMsg statsSnapshot
type doneMsg struct{}
type errMsg struct{ err error }

type model struct {
	width  int
	height int

	prog    progress.Model
	spin    spinner.Model
	started time.Time

	// UI state
	filesTotal int
	filesDone  int
	linesTotal int64
	matches    int64

	done bool
	err  error

	updates <-chan statsSnapshot
}

func main() {
	path := flag.String("path", "./logs/*.log", "로그 파일 경로 (와일드카드 지원)")
	keyword := flag.String("keyword", "ERROR", "검색할 키워드/정규식")
	concurrent := flag.Int("concurrent", runtime.NumCPU(), "Consumer 워커 수")
	flag.Parse()

	files, err := filepath.Glob(*path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "glob 실패:", err)
		os.Exit(1)
	}
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "로그 파일을 찾지 못했습니다:", *path)
		os.Exit(1)
	}

	re, err := regexp.Compile(*keyword)
	if err != nil {
		fmt.Fprintln(os.Stderr, "정규식 오류:", err)
		os.Exit(1)
	}
	if *concurrent <= 0 {
		*concurrent = 1
	}

	updates := startAnalyzer(files, re, *concurrent)

	m := initialModel(len(files), updates)
	p := tea.NewProgram(m)

	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "TUI 실행 실패:", err)
		os.Exit(1)
	}
}

func initialModel(filesTotal int, updates <-chan statsSnapshot) model {
	p := progress.New(progress.WithDefaultGradient())
	p.Width = 40

	s := spinner.New()
	s.Spinner = spinner.Dot

	return model{
		prog:       p,
		spin:       s,
		started:    time.Now(),
		filesTotal: filesTotal,
		updates:    updates,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		m.spin.Tick,
		waitUpdate(m.updates),
	)
}

func waitUpdate(ch <-chan statsSnapshot) tea.Cmd {
	return func() tea.Msg {
		snap, ok := <-ch
		if !ok {
			return doneMsg{}
		}
		if snap.Err != nil {
			return errMsg{err: snap.Err}
		}
		if snap.Done {
			return statsMsg(snap)
		}
		return statsMsg(snap)
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		// 진행바는 화면 너비에 맞춰 적당히 조절
		m.prog.Width = clamp(m.width-10, 20, 80)
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.done = true
			return m, tea.Quit
		}
		return m, nil

	case statsMsg:
		s := statsSnapshot(msg)
		m.filesDone = s.FilesDone
		m.linesTotal = s.LinesTotal
		m.matches = s.MatchesTotal

		// 진행률 업데이트(파일 기준)
		var percent float64
		if m.filesTotal > 0 {
			percent = float64(m.filesDone) / float64(m.filesTotal)
		}
		cmd := m.prog.SetPercent(percent)

		if s.Done {
			m.done = true
			return m, tea.Batch(cmd, tea.Quit)
		}

		// 다음 업데이트 기다리기
		return m, tea.Batch(cmd, waitUpdate(m.updates))

	case errMsg:
		m.err = msg.err
		m.done = true
		return m, tea.Quit

	case doneMsg:
		m.done = true
		return m, tea.Quit

	default:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	}
}

func (m model) View() string {
	title := lipgloss.NewStyle().
		Bold(true).
		Render("Go-LogScanner TUI")

	elapsed := time.Since(m.started).Truncate(100 * time.Millisecond)

	percent := 0.0
	if m.filesTotal > 0 {
		percent = float64(m.filesDone) / float64(m.filesTotal)
	}

	bar := m.prog.ViewAs(percent)

	stats := fmt.Sprintf(
		"Files: %d/%d   Lines: %d   Matches: %d   Elapsed: %s",
		m.filesDone, m.filesTotal, m.linesTotal, m.matches, elapsed,
	)

	hint := "Keys: q = quit"

	if m.err != nil {
		errBox := lipgloss.NewStyle().
			Foreground(lipgloss.Color("9")).
			Bold(true).
			Render("ERROR: " + m.err.Error())
		return joinLines(title, "", errBox, "", hint)
	}

	if m.done {
		doneBox := lipgloss.NewStyle().
			Foreground(lipgloss.Color("10")).
			Bold(true).
			Render("DONE ✅")
		return joinLines(title, "", doneBox, stats, "", hint)
	}

	working := m.spin.View() + " Scanning..."
	return joinLines(title, "", working, bar, stats, "", hint)
}

func joinLines(lines ...string) string {
	out := ""
	for i, s := range lines {
		out += s
		if i != len(lines)-1 {
			out += "\n"
		}
	}
	return out
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

//
// Analyzer (백그라운드) : Step 6 파이프라인을 "업데이트 채널"로 흘려보내기
//

func startAnalyzer(files []string, re *regexp.Regexp, concurrent int) <-chan statsSnapshot {
	updates := make(chan statsSnapshot, 64)

	go func() {
		defer close(updates)

		var filesDone int64
		var linesTotal int64
		var matchesTotal int64

		linesCh := make(chan LineEvent, 4096)

		// Producer
		go func() {
			for _, path := range files {
				if err := readFileLines(path, linesCh); err != nil {
					updates <- statsSnapshot{Err: fmt.Errorf("파일 읽기 실패(%s): %w", path, err)}
					close(linesCh)
					return
				}
				atomic.AddInt64(&filesDone, 1)
			}
			close(linesCh)
		}()

		// Consumers
		var consWg sync.WaitGroup
		consWg.Add(concurrent)
		for i := 0; i < concurrent; i++ {
			go func() {
				defer consWg.Done()
				for ev := range linesCh {
					atomic.AddInt64(&linesTotal, 1)
					if re.MatchString(ev.Line) {
						atomic.AddInt64(&matchesTotal, 1)
					}
				}
			}()
		}

		// UI 스냅샷(200ms마다)
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()

		done := make(chan struct{})
		go func() {
			consWg.Wait()
			close(done)
		}()

		for {
			select {
			case <-ticker.C:
				updates <- statsSnapshot{
					FilesTotal:   len(files),
					FilesDone:    int(atomic.LoadInt64(&filesDone)),
					LinesTotal:   atomic.LoadInt64(&linesTotal),
					MatchesTotal: atomic.LoadInt64(&matchesTotal),
				}
			case <-done:
				updates <- statsSnapshot{
					FilesTotal:   len(files),
					FilesDone:    int(atomic.LoadInt64(&filesDone)),
					LinesTotal:   atomic.LoadInt64(&linesTotal),
					MatchesTotal: atomic.LoadInt64(&matchesTotal),
					Done:         true,
				}
				return
			}
		}
	}()

	return updates
}

type LineEvent struct {
	File string
	Line string
}

type MatchEvent struct {
	File    string
	Matched bool
}

func readFileLines(path string, linesCh chan<- LineEvent) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)

	const maxCapacity = 1024 * 1024 * 8 // 8MB
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, maxCapacity)

	for scanner.Scan() {
		linesCh <- LineEvent{File: path, Line: scanner.Text()}
	}
	return scanner.Err()
}
