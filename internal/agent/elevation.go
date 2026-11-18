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

// pendingElevationSecrets is the in-process handoff for a human-typed password
// on a SUB-AGENT elevation ask. The host approval handler (TUI/web) calls
// SetPendingElevationSecret with the ask's ElevationKey (the child's paused
// tool-call id) BEFORE delivering the approval; the child's tool-call path
// moves the secret into its OWN ElevationManager with
// takePendingElevationSecret. This is deliberately NOT a field of
// PermissionResponse (or any shared/decision/persisted struct): the answer is
// copied into tool results, SSE frames and stored objects, and a future %+v
// debug line or marshal path would leak a root password. The map is keyed by
// the ask's unique tool-call id and every value is copied on store, single-use
// on take and zeroed on clear, so one child's secret never reaches another ask.
// Entries are in-process only — never serialized, persisted or logged.
var pendingElevationSecrets = struct {
	mu sync.Mutex
	m  map[string][]byte
}{m: make(map[string][]byte)}

// SetPendingElevationSecret stages a human-typed elevation secret for the child
// ask identified by requestID (PermissionRequest.ElevationKey). The host must
// call it only after an explicit approval and only for an elevation ask; it is
// a no-op on an empty id or secret, so denying or approving without typing
// stages nothing and the re-executing tool fails closed. The bytes are copied
// and any previous value for the id is zeroed first.
func SetPendingElevationSecret(requestID string, secret []byte) {
	if requestID == "" || len(secret) == 0 {
		return
	}
	pendingElevationSecrets.mu.Lock()
	defer pendingElevationSecrets.mu.Unlock()
	if old := pendingElevationSecrets.m[requestID]; len(old) > 0 {
		clear(old)
	}
	pendingElevationSecrets.m[requestID] = bytes.Clone(secret)
}

// takePendingElevationSecret returns and removes the staged secret for
// requestID, zeroing the stored copy. A second take returns nil: the handoff is
// single-use, bound to the one ask that produced it.
func takePendingElevationSecret(requestID string) []byte {
	pendingElevationSecrets.mu.Lock()
	defer pendingElevationSecrets.mu.Unlock()
	stored, ok := pendingElevationSecrets.m[requestID]
	if !ok {
		return nil
	}
	delete(pendingElevationSecrets.m, requestID)
	out := bytes.Clone(stored)
	clear(stored)
	return out
}

// clearPendingElevationSecret drops a staged secret that was never consumed
// (e.g. the child was denied or cancelled after the host staged it), so no
// secret outlives its ask.
func clearPendingElevationSecret(requestID string) {
	pendingElevationSecrets.mu.Lock()
	defer pendingElevationSecrets.mu.Unlock()
	if stored := pendingElevationSecrets.m[requestID]; len(stored) > 0 {
		clear(stored)
	}
	delete(pendingElevationSecrets.m, requestID)
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
