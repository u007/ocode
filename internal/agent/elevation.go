package agent

import (
	"bytes"
	"sync"
)

// ElevationManager holds elevation passwords for pending human permission
// asks, keyed by the ask's request id (the paused tool-call id). It lives on
// one Agent, so a parent and its subagents never share a cache. Secrets are
// copied on Store and zeroed on Take/Clear/ClearAll: at rest the only copy is
// the manager's own, and consuming a secret destroys that copy.
//
// A password must never be placed in a transcript, tool result, SSE frame,
// log line, argv or environment variable. It is handed to the re-executed
// tool only through a getter closure attached to the tool call context (see
// tool.WithElevationSecret).
type ElevationManager struct {
	mu       sync.Mutex
	secrets  map[string][]byte
	failures int // consecutive failed elevation attempts in the current ask-chain
}

func NewElevationManager() *ElevationManager {
	return &ElevationManager{secrets: make(map[string][]byte)}
}

// Store copies secret into the slot for requestID. An empty id or secret is
// rejected. Re-storing an id zeroes the previous value first.
func (m *ElevationManager) Store(requestID string, secret []byte) {
	if requestID == "" || len(secret) == 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if old, ok := m.secrets[requestID]; ok {
		clear(old)
	}
	m.secrets[requestID] = bytes.Clone(secret)
}

// Take returns a copy of the secret for requestID, removes the entry and
// zeroes the stored bytes. A second Take returns nil: each secret is
// single-use, bound to the ask that produced it.
func (m *ElevationManager) Take(requestID string) []byte {
	m.mu.Lock()
	stored, ok := m.secrets[requestID]
	if !ok {
		m.mu.Unlock()
		return nil
	}
	delete(m.secrets, requestID)
	out := bytes.Clone(stored)
	clear(stored)
	m.mu.Unlock()
	return out
}

// Clear zeroes and removes one entry. Clearing an absent id is a no-op.
func (m *ElevationManager) Clear(requestID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if stored, ok := m.secrets[requestID]; ok {
		clear(stored)
		delete(m.secrets, requestID)
	}
}

// ClearAll zeroes and removes every entry. Session reset, rekey and agent
// teardown call it so no secret outlives the conversation that supplied it.
func (m *ElevationManager) ClearAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, stored := range m.secrets {
		clear(stored)
		delete(m.secrets, id)
	}
}

// noteFailure records one failed elevation attempt and returns the new
// consecutive count. resetFailures clears the chain (non-elevation command or
// new human turn); failureCount is a locked read. All are guarded by the same
// mutex as the secrets map, so a concurrent approval cannot race the counter.
func (m *ElevationManager) noteFailure() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failures++
	return m.failures
}

func (m *ElevationManager) failureCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.failures
}

func (m *ElevationManager) resetFailures() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failures = 0
}
