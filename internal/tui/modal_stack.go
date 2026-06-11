package tui

import (
	tea "charm.land/bubbletea/v2"
)

// Modal is the interface that all modal dialogs must implement.
// Handle processes a message and returns true if the message was consumed.
// Render returns the modal's rendered content.
// Bounds returns the modal's bounding rectangle for mouse hit-testing.
type Modal interface {
	Handle(msg tea.Msg) bool
	Render() string
	Bounds() Rect
}

// ModalStack manages a stack of Modal instances. Only the topmost modal
// receives keyboard messages. Mouse messages are consumed only if they
// fall within the top modal's bounds; otherwise they fall through to the caller.
type ModalStack struct {
	stack []Modal
}

// NewModalStack creates a new empty ModalStack.
func NewModalStack() *ModalStack {
	return &ModalStack{}
}

// Push pushes a modal onto the stack. It becomes the active (top) modal.
func (ms *ModalStack) Push(m Modal) {
	ms.stack = append(ms.stack, m)
}

// Pop removes and returns the top modal. If the stack is empty, this is a no-op.
func (ms *ModalStack) Pop() {
	if len(ms.stack) == 0 {
		return
	}
	ms.stack = ms.stack[:len(ms.stack)-1]
}

// Top returns the topmost modal, or nil if the stack is empty.
func (ms *ModalStack) Top() Modal {
	if len(ms.stack) == 0 {
		return nil
	}
	return ms.stack[len(ms.stack)-1]
}

// Len returns the number of modals in the stack.
func (ms *ModalStack) Len() int {
	return len(ms.stack)
}

// Handle routes a message to the appropriate modal(s).
// Keyboard messages go to the top modal only.
// Mouse messages are consumed if they fall within the top modal's bounds;
// otherwise they fall through (returns false).
// Returns true if the message was consumed.
func (ms *ModalStack) Handle(msg tea.Msg) bool {
	if len(ms.stack) == 0 {
		return false
	}

	top := ms.stack[len(ms.stack)-1]

	// Mouse messages: check bounds before dispatching.
	if mouseMsg, ok := msg.(tea.MouseMsg); ok {
		m := mouseMsg.Mouse()
		bounds := top.Bounds()
		if !bounds.Contains(m.X, m.Y) {
			return false // fall through to caller
		}
	}

	return top.Handle(msg)
}

// Render returns the rendered output of the topmost modal, or "" if empty.
func (ms *ModalStack) Render() string {
	if len(ms.stack) == 0 {
		return ""
	}
	return ms.stack[len(ms.stack)-1].Render()
}
