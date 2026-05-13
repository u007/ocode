package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

func Run(sessionID string, cont bool) error {
	m := newModel(sessionID, cont)
	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if m.config != nil && m.config.TUI.Mouse != nil && *m.config.TUI.Mouse {
		opts = append(opts, tea.WithMouseCellMotion())
	}

	p := tea.NewProgram(m, opts...)
	_, err := p.Run()
	return err
}
