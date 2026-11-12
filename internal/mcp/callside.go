package mcp

import "sync"

// maxCallSideEntries bounds each per-call side channel. An entry is removed when
// its call's taker reads it. The cap only matters for a call whose taker never
// runs: then the oldest entry is evicted when a newer one is stored.
const maxCallSideEntries = 64

// boundedCallMap holds per-call side-channel values keyed by tool call ID. One
// client is shared by every session that uses its server, so the key is the
// call, never the tool name.
type boundedCallMap[V any] struct {
	mu    sync.Mutex
	items map[string]V
	order []string // the keys of items, oldest first
}

// put stores v under id and evicts the oldest entries beyond the cap.
func (m *boundedCallMap[V]) put(id string, v V) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.items == nil {
		m.items = make(map[string]V)
	}
	if _, held := m.items[id]; held {
		m.removeOrderLocked(id)
	}
	m.items[id] = v
	m.order = append(m.order, id)
	for len(m.items) > maxCallSideEntries {
		oldest := m.order[0]
		m.order = m.order[1:]
		delete(m.items, oldest)
	}
}

// take returns and removes the value stored under id.
func (m *boundedCallMap[V]) take(id string) (V, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.items[id]
	if ok {
		delete(m.items, id)
		m.removeOrderLocked(id)
	}
	return v, ok
}

// removeOrderLocked drops id from order. Caller holds m.mu.
func (m *boundedCallMap[V]) removeOrderLocked(id string) {
	for i, held := range m.order {
		if held == id {
			m.order = append(m.order[:i], m.order[i+1:]...)
			return
		}
	}
}
