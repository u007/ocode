package tui

import (
	tea "charm.land/bubbletea/v2"
)

func Run(sessionID string, cont bool, yolo bool) error {
	m := newModel(sessionID, cont, yolo)
	p := tea.NewProgram(m)
	_, err := p.Run()
	return err
}
