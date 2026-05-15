package tui

import (
	tea "charm.land/bubbletea/v2"
)

func Run(sessionID string, cont bool) error {
	m := newModel(sessionID, cont)
	p := tea.NewProgram(m)
	_, err := p.Run()
	return err
}
