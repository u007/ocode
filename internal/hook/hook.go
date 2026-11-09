// Package hook provides a simple multi-listener hook for callbacks.
// It is used by config.OnConfigSaved and auth.OnCredentialsSaved to allow
// multiple independent listeners (e.g. sync.Watcher and server auth flow)
// to coexist without clobbering each other.
package hook

import "sync"

// Hooks manages a set of callback functions. Safe for concurrent use.
// The zero value is ready to use.
type Hooks struct {
	mu  sync.Mutex
	fns []func()
}

// Add registers fn and returns a remove function. Calling remove is
// idempotent beyond the first call.
func (h *Hooks) Add(fn func()) (remove func()) {
	h.mu.Lock()
	idx := len(h.fns)
	h.fns = append(h.fns, fn)
	h.mu.Unlock()
	return func() {
		h.mu.Lock()
		if idx < len(h.fns) {
			h.fns[idx] = nil // leave nil slot; indices stay stable
		}
		h.mu.Unlock()
	}
}

// Len reports how many callbacks are currently registered. Removed callbacks
// leave nil slots and are not counted. Tests use it to pin who registers what
// on the process-global hook lists (e.g. a Server must not install its
// config/auth sync hooks until it actually serves).
func (h *Hooks) Len() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, fn := range h.fns {
		if fn != nil {
			n++
		}
	}
	return n
}

// Fire invokes every registered callback. Nil entries (from removed
// callbacks) are skipped. Safe to call from within a callback (no
// reentrancy deadlock — we snapshot under lock).
func (h *Hooks) Fire() {
	h.mu.Lock()
	fns := make([]func(), len(h.fns))
	copy(fns, h.fns)
	h.mu.Unlock()
	for _, fn := range fns {
		if fn != nil {
			fn()
		}
	}
}
