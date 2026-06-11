package tui

import (
	tea "charm.land/bubbletea/v2"
)

// questionModal adapts the existing question prompt into a Modal interface.
type questionModal struct {
	m *model
}

func (q *questionModal) Handle(msg tea.Msg) bool {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return false
	}
	keyStr := keyMsg.String()

	switch keyStr {
	case "up", "k":
		if q.m.questionTab > 0 {
			q.m.questionTab--
		}
		return true
	case "down", "j":
		if q.m.questionTab < len(q.m.questionPrompts)-1 {
			q.m.questionTab++
		}
		return true
	case "esc":
		q.m.showQuestionDialog = false
		return true
	}
	return false
}

func (q *questionModal) Render() string {
	if !q.m.showQuestionDialog {
		return ""
	}
	return q.m.renderQuestionDialog(q.m.panelWidth() - 2)
}

func (q *questionModal) Bounds() Rect {
	w := q.m.panelWidth() - 2
	if w < 40 {
		w = 40
	}
	return Rect{X: 0, Y: 0, Width: w, Height: 15}
}

func (m *model) pushQuestionModal() {
	if m.modalStack == nil {
		return
	}
	if _, ok := m.modalStack.Top().(*questionModal); !ok {
		m.modalStack.Push(&questionModal{m: m})
	}
}

func (m *model) popQuestionModal() {
	if m.modalStack == nil {
		return
	}
	if m.modalStack.Top() != nil {
		if _, ok := m.modalStack.Top().(*questionModal); ok {
			m.modalStack.Pop()
		}
	}
}

// retryModal adapts the retry dialog into a Modal interface.
type retryModal struct {
	m *model
}

func (r *retryModal) Handle(msg tea.Msg) bool {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return false
	}
	keyStr := keyMsg.String()
	switch keyStr {
	case "esc", "n":
		r.m.showRetryDialog = false
		return true
	}
	return false
}

func (r *retryModal) Render() string {
	if !r.m.showRetryDialog {
		return ""
	}
	return r.m.renderRetryDialog(r.m.panelWidth() - 2)
}

func (r *retryModal) Bounds() Rect {
	w := r.m.panelWidth() - 2
	if w < 40 {
		w = 40
	}
	return Rect{X: 0, Y: 0, Width: w, Height: 5}
}

func (m *model) pushRetryModal() {
	if m.modalStack == nil {
		return
	}
	if _, ok := m.modalStack.Top().(*retryModal); !ok {
		m.modalStack.Push(&retryModal{m: m})
	}
}

func (m *model) popRetryModal() {
	if m.modalStack == nil {
		return
	}
	if m.modalStack.Top() != nil {
		if _, ok := m.modalStack.Top().(*retryModal); ok {
			m.modalStack.Pop()
		}
	}
}

// connectModal adapts the connect dialog into a Modal interface.
type connectModal struct {
	m *model
}

func (c *connectModal) Handle(msg tea.Msg) bool {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return false
	}
	keyStr := keyMsg.String()
	switch keyStr {
	case "esc":
		c.m.showConnect = false
		return true
	}
	return false
}

func (c *connectModal) Render() string {
	if !c.m.showConnect {
		return ""
	}
	return c.m.renderConnect()
}

func (c *connectModal) Bounds() Rect {
	w := c.m.width - 4
	if w < 40 {
		w = 40
	}
	return Rect{X: 0, Y: 0, Width: w, Height: 20}
}

func (m *model) pushConnectModal() {
	if m.modalStack == nil {
		return
	}
	if _, ok := m.modalStack.Top().(*connectModal); !ok {
		m.modalStack.Push(&connectModal{m: m})
	}
}

func (m *model) popConnectModal() {
	if m.modalStack == nil {
		return
	}
	if m.modalStack.Top() != nil {
		if _, ok := m.modalStack.Top().(*connectModal); ok {
			m.modalStack.Pop()
		}
	}
}
