// Package hook provides a simple multi-listener hook for callbacks.
// It is used by config.OnConfigSaved and auth.OnCredentialsSaved to allow
// multiple independent listeners (e.g. sync.Watcher and server auth flow)
// to coexist without clobbering each other.
package hook

import "sync"

// Hooks manages a set of callback functions. Safe for concurrent use.
// The zero value is ready to use.
type Hooks struct {
	mu   sync.Mutex
	fns  []func()
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
