package server

import "sync"

// terminalProcEntry is what the terminal-processes emitter needs to know
// about one open terminal: which project it belongs to (for bus scoping) and
// the OS pid of its top-level shell process (its process tree is walked from
// there).
type terminalProcEntry struct {
	Project string
	PID     int32
}

// terminalRegistry tracks the pid of every currently-open terminal pty,
// keyed by the frontend-generated terminal id (TerminalPanel's `id` prop).
// HandleTerminalWS registers on pty start and unregisters on teardown; the
// terminal-processes emitter (emitters.go) reads a snapshot each tick.
type terminalRegistry struct {
	mu      sync.Mutex
	entries map[string]terminalProcEntry
}

func newTerminalRegistry() *terminalRegistry {
	return &terminalRegistry{entries: make(map[string]terminalProcEntry)}
}

func (r *terminalRegistry) register(id string, entry terminalProcEntry) {
	if id == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[id] = entry
}

func (r *terminalRegistry) unregister(id string) {
	if id == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, id)
}

// snapshot returns a copy of the current id -> entry map, safe for the
// caller to range over without holding the registry lock.
func (r *terminalRegistry) snapshot() map[string]terminalProcEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]terminalProcEntry, len(r.entries))
	for id, e := range r.entries {
		out[id] = e
	}
	return out
}
