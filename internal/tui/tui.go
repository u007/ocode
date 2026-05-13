package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

func Run(sessionID string, cont bool) error {
	p := tea.NewProgram(newModel(sessionID, cont), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
