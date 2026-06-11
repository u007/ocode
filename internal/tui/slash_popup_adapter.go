package tui

import (
	tea "charm.land/bubbletea/v2"
)

// slashPopupModal adapts the existing slash popup into a Modal interface.
type slashPopupModal struct {
	m *model
}

func (s *slashPopupModal) Handle(msg tea.Msg) bool {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return false
	}

	keyStr := keyMsg.String()

	switch keyStr {
	case "up", "shift+tab":
		if s.m.slashPopupIndex > 0 {
			s.m.slashPopupIndex--
		}
		return true
	case "down", "tab":
		if s.m.slashPopupIndex < len(s.m.slashPopupItems)-1 {
			s.m.slashPopupIndex++
		}
		return true
	case "esc":
		s.m.showSlashPopup = false
		return true
	}

	return false
}

func (s *slashPopupModal) Render() string {
	if !s.m.showSlashPopup {
		return ""
	}
	return s.m.renderSlashPopup()
}

func (s *slashPopupModal) Bounds() Rect {
	width := s.m.panelWidth() - 2
	if width < 40 {
		width = 40
	}
	height := len(s.m.slashPopupItems) + 2
	if height > 15 {
		height = 15
	}
	return Rect{X: 0, Y: 0, Width: width, Height: height}
}

// pushSlashPopupModal pushes the slash popup adapter onto the modal stack.
func (m *model) pushSlashPopupModal() {
	if m.modalStack == nil {
		return
	}
	if _, ok := m.modalStack.Top().(*slashPopupModal); !ok {
		m.modalStack.Push(&slashPopupModal{m: m})
	}
}

// popSlashPopupModal pops the slash popup from the modal stack.
func (m *model) popSlashPopupModal() {
	if m.modalStack == nil {
		return
	}
	if m.modalStack.Top() != nil {
		if _, ok := m.modalStack.Top().(*slashPopupModal); ok {
			m.modalStack.Pop()
		}
	}
}
