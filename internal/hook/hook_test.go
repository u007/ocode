package hook

import "testing"

func TestAddFires(t *testing.T) {
	var h Hooks
	var called bool
	remove := h.Add(func() { called = true })
	h.Fire()
	if !called {
		t.Error("expected callback to fire")
	}
	remove()
}

func TestRemovePreventsFire(t *testing.T) {
	var h Hooks
	var count int
	remove := h.Add(func() { count++ })
	remove()
	h.Fire()
	if count != 0 {
		t.Errorf("expected 0 after remove, got %d", count)
	}
}

func TestMultipleListeners(t *testing.T) {
	var h Hooks
	var a, b int
	r1 := h.Add(func() { a++ })
	r2 := h.Add(func() { b++ })
	h.Fire()
	if a != 1 || b != 1 {
		t.Errorf("expected a=1,b=1, got a=%d,b=%d", a, b)
	}
	r1()
	h.Fire()
	if a != 1 || b != 2 {
		t.Errorf("expected a=1,b=2 after r1 remove, got a=%d,b=%d", a, b)
	}
	r2()
	h.Fire()
	if a != 1 || b != 2 {
		t.Errorf("expected unchanged after both removed, got a=%d,b=%d", a, b)
	}
}

func TestFireWithNoListeners(t *testing.T) {
	var h Hooks
	h.Fire() // should not panic
}

func TestRemoveIsIdempotent(t *testing.T) {
	var h Hooks
	var count int
	remove := h.Add(func() { count++ })
	remove()
	remove() // second call must not panic
	h.Fire()
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}
}
