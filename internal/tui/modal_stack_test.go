package tui

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// --- Mock modal for testing ---

type mockModal struct {
	bounds      Rect
	rendered    string
	consumeNext bool // what Handle should return
	handled     bool // set to true when Handle is called
}

func (m *mockModal) Handle(msg tea.Msg) bool {
	m.handled = true
	return m.consumeNext
}

func (m *mockModal) Render() string {
	return m.rendered
}

func (m *mockModal) Bounds() Rect {
	return m.bounds
}

// --- Mock key/mouse messages ---

type mockKeyMsg struct{ code rune }

func (m mockKeyMsg) Key() tea.Key { return tea.Key{Code: m.code} }
func (m mockKeyMsg) String() string {
	return fmt.Sprintf("key:%c", m.code)
}

type mockMouseMsg struct {
	x, y int
	btn  tea.MouseButton
}

func (m mockMouseMsg) Mouse() tea.Mouse {
	return tea.Mouse{X: m.x, Y: m.y, Button: m.btn}
}
func (m mockMouseMsg) String() string {
	return fmt.Sprintf("mouse:%d,%d", m.x, m.y)
}

// --- ModalStack tests ---

func TestModalStackPushPop(t *testing.T) {
	ms := NewModalStack()
	m1 := &mockModal{rendered: "modal1"}
	m2 := &mockModal{rendered: "modal2"}

	ms.Push(m1)
	if ms.Top() != m1 {
		t.Error("top should be m1 after first push")
	}
	if ms.Len() != 1 {
		t.Errorf("expected len 1, got %d", ms.Len())
	}

	ms.Push(m2)
	if ms.Top() != m2 {
		t.Error("top should be m2 after second push")
	}
	if ms.Len() != 2 {
		t.Errorf("expected len 2, got %d", ms.Len())
	}

	ms.Pop()
	if ms.Top() != m1 {
		t.Error("top should be m1 after pop")
	}
	if ms.Len() != 1 {
		t.Errorf("expected len 1 after pop, got %d", ms.Len())
	}

	ms.Pop()
	if ms.Top() != nil {
		t.Error("top should be nil after popping all")
	}
	if ms.Len() != 0 {
		t.Errorf("expected len 0, got %d", ms.Len())
	}
}

func TestModalStackPopEmpty(t *testing.T) {
	ms := NewModalStack()
	ms.Pop() // should not panic
	if ms.Top() != nil {
		t.Error("top should be nil on empty stack")
	}
}

func TestModalStackTopEmpty(t *testing.T) {
	ms := NewModalStack()
	if ms.Top() != nil {
		t.Error("top should be nil on empty stack")
	}
}

func TestModalStackKeyboardToTopOnly(t *testing.T) {
	ms := NewModalStack()
	m1 := &mockModal{rendered: "m1", consumeNext: true}
	m2 := &mockModal{rendered: "m2", consumeNext: true}
	ms.Push(m1)
	ms.Push(m2)

	keyMsg := mockKeyMsg{code: tea.KeyEnter}
	consumed := ms.Handle(keyMsg)

	if !consumed {
		t.Error("key message should be consumed by top modal")
	}
	if !m2.handled {
		t.Error("top modal should have handled the key message")
	}
	if m1.handled {
		t.Error("bottom modal should NOT have handled the key message")
	}
	_ = consumed
}

func TestModalStackMouseInsideConsumed(t *testing.T) {
	ms := NewModalStack()
	m := &mockModal{
		bounds:      Rect{X: 10, Y: 10, Width: 20, Height: 5},
		consumeNext: true,
	}
	ms.Push(m)

	mouseMsg := mockMouseMsg{x: 15, y: 12, btn: tea.MouseLeft}
	consumed := ms.Handle(mouseMsg)

	if !consumed {
		t.Error("mouse inside modal bounds should be consumed")
	}
	if !m.handled {
		t.Error("modal should have handled the mouse message")
	}
}

func TestModalStackMouseOutsideFallThrough(t *testing.T) {
	ms := NewModalStack()
	m := &mockModal{
		bounds:      Rect{X: 10, Y: 10, Width: 20, Height: 5},
		consumeNext: true,
	}
	ms.Push(m)

	// Mouse outside modal bounds
	mouseMsg := mockMouseMsg{x: 50, y: 50, btn: tea.MouseLeft}
	consumed := ms.Handle(mouseMsg)

	if consumed {
		t.Error("mouse outside modal bounds should NOT be consumed")
	}
	if m.handled {
		t.Error("modal should NOT have handled mouse outside its bounds")
	}
}

func TestModalStackEmptyConsumesNothing(t *testing.T) {
	ms := NewModalStack()
	keyMsg := mockKeyMsg{code: tea.KeyEnter}
	consumed := ms.Handle(keyMsg)
	if consumed {
		t.Error("empty stack should not consume any message")
	}
}

func TestModalStackCloseTopRestoresBeneath(t *testing.T) {
	ms := NewModalStack()
	m1 := &mockModal{rendered: "m1", consumeNext: true}
	m2 := &mockModal{rendered: "m2", consumeNext: true}
	ms.Push(m1)
	ms.Push(m2)

	// Close top (m2) — m1 should become active
	ms.Pop()
	if ms.Top() != m1 {
		t.Error("after closing m2, m1 should be top")
	}

	// Key should now go to m1
	keyMsg := mockKeyMsg{code: tea.KeyEsc}
	ms.Handle(keyMsg)
	if !m1.handled {
		t.Error("m1 should handle key after m2 is closed")
	}
}

func TestModalStackRender(t *testing.T) {
	ms := NewModalStack()
	m1 := &mockModal{rendered: "BOTTOM"}
	m2 := &mockModal{rendered: "TOP"}
	ms.Push(m1)
	ms.Push(m2)

	rendered := ms.Render()
	if rendered != "TOP" {
		t.Errorf("Render should return top modal, got %q", rendered)
	}
}

func TestModalStackRenderEmpty(t *testing.T) {
	ms := NewModalStack()
	rendered := ms.Render()
	if rendered != "" {
		t.Errorf("empty stack Render should return empty, got %q", rendered)
	}
}
