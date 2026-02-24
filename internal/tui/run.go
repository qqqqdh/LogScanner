package tui

import (
	"logscanner/internal/analyzer"

	tea "github.com/charmbracelet/bubbletea"
)

func Run(files []string, updates <-chan analyzer.Event, cfg Config, pauseFn func(bool)) error {
	// 에러 해결: initialModel을 InitialModel(대문자)로 수정
	m := InitialModel(files, updates, cfg, pauseFn)

	// 개선: tea.WithAltScreen()을 추가하여 전체 화면 모드를 사용합니다.
	// 이렇게 해야 q, p, tab 등의 단축키 입력이 터미널 환경에 구애받지 않고 정확히 전달됩니다.
	p := tea.NewProgram(m, tea.WithAltScreen())

	_, err := p.Run()
	return err
}
