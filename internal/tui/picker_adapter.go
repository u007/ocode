package tui

import (
	tea "charm.land/bubbletea/v2"
)

// pickerModal adapts the existing picker into a Modal interface for ModalStack.
// This is a transitional adapter — it delegates to the model's existing picker
// methods during migration.
type pickerModal struct {
	m *model
}

func (p *pickerModal) Handle(msg tea.Msg) bool {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return false
	}

	keyStr := keyMsg.String()

	// Route picker keyboard input through the existing handler.
	switch keyStr {
	case "up", "k":
		if p.m.pickerIndex > 0 {
			p.m.pickerIndex--
		}
		return true
	case "down", "j":
		items, _ := p.m.pickerVisibleItems()
		if p.m.pickerIndex < len(items)-1 {
			p.m.pickerIndex++
		}
		return true
	case "enter":
		// Delegate to existing picker confirm logic.
		return false // let the existing handler deal with it
	case "esc", "ctrl+c":
		p.m.showPicker = false
		return true
	case "pgup":
		p.m.pickerIndex -= 10
		if p.m.pickerIndex < 0 {
			p.m.pickerIndex = 0
		}
		return true
	case "pgdown":
		items, _ := p.m.pickerVisibleItems()
		p.m.pickerIndex += 10
		if p.m.pickerIndex >= len(items) {
			p.m.pickerIndex = len(items) - 1
		}
		if p.m.pickerIndex < 0 {
			p.m.pickerIndex = 0
		}
		return true
	}

	return false
}

func (p *pickerModal) Render() string {
	if !p.m.showPicker {
		return ""
	}
	return p.m.renderPicker()
}

func (p *pickerModal) Bounds() Rect {
	width := p.m.width - 4
	if width < 40 {
		width = 40
	}
	items, _ := p.m.pickerVisibleItems()
	height := len(items) + 4 // header + filter + items + hint
	if height > 20 {
		height = 20
	}
	return Rect{X: 0, Y: 0, Width: width, Height: height}
}

// pushPickerModal pushes the picker adapter onto the modal stack.
func (m *model) pushPickerModal() {
	if m.modalStack == nil {
		return
	}
	if _, ok := m.modalStack.Top().(*pickerModal); !ok {
		m.modalStack.Push(&pickerModal{m: m})
	}
}

// popPickerModal pops the picker from the modal stack.
func (m *model) popPickerModal() {
	if m.modalStack == nil {
		return
	}
	if m.modalStack.Top() != nil {
		if _, ok := m.modalStack.Top().(*pickerModal); ok {
			m.modalStack.Pop()
		}
	}
}
