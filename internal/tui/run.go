package tui

import (
	"logscanner/internal/analyzer"

	tea "github.com/charmbracelet/bubbletea"
)

func Run(files []string, updates <-chan analyzer.Event, cfg Config) error {
	m := initialModel(files, updates, cfg)
	p := tea.NewProgram(m) // Windows/VSCode에서 안정적으로 AltScreen OFF
	_, err := p.Run()
	return err
}
