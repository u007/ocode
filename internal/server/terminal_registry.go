package server

import "sync"

// terminalProcEntry is what the terminal-processes emitter needs to know
// about one open terminal: which project it belongs to (for bus scoping) and
// the OS pid of its top-level shell process (its process tree is walked from
// there).
type terminalProcEntry struct {
	Project string
	PID     int32
	// Cols and Rows are the pty size the browser last reported. They are zero
	// until its first resize. The Pulse card replays output into a screen of
	// this size, so a cursor-addressed program lays out the way it did live.
	Cols uint16
	Rows uint16
}

// terminalRegistry tracks the pid of every currently-open terminal pty,
// keyed by the frontend-generated terminal id (TerminalPanel's `id` prop).
// HandleTerminalWS registers on pty start and unregisters on teardown; the
// terminal-processes emitter (emitters.go) reads a snapshot each tick.
type terminalRegistry struct {
	mu      sync.Mutex
	entries map[string]terminalProcEntry
	// gen counts every change to entries, so a cache keyed on it notices a
	// terminal opened or closed without polling the map itself.
	gen uint64
	// pulseRows memoizes the Pulse terminal walk (pulseTerminalRows). It lives
	// here, not on Handler, because it is keyed by gen.
	pulseRows pulseRowsMemo
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
	r.gen++
}

// setSize records the pty size of a registered terminal. An id that is no
// longer registered is ignored on purpose: a resize frame can arrive just as
// the pty closes, and there is then no entry left to update.
func (r *terminalRegistry) setSize(id string, cols, rows uint16) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.entries[id]
	if !ok {
		// intentionally not logged: the pty closed between the resize and this call
		return
	}
	entry.Cols, entry.Rows = cols, rows
	r.entries[id] = entry
	r.gen++
}

func (r *terminalRegistry) unregister(id string) {
	if id == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.entries[id]; ok {
		delete(r.entries, id)
		r.gen++
	}
}

// generation returns the change counter: it moves on every register and on
// every unregister of a live terminal.
func (r *terminalRegistry) generation() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.gen
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
