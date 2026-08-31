package server

import (
	"sync"
	"time"
)

// terminalDetachTTL is how long a shell whose websocket went away is kept
// alive waiting for the same terminal_id to reattach. It covers page reloads
// and transient socket drops; a browser tab that is simply closed in web mode
// still gets reaped once the window elapses. Desktop app exit sweeps every
// shell immediately via shutdownTerminals regardless of this timer.
const terminalDetachTTL = 30 * time.Minute

// terminalSessionTable owns every live pty shell keyed by the frontend's
// terminal id, so a reconnecting socket can find and reattach to its shell
// instead of spawning a new one. Anonymous sockets (no terminal_id) are never
// stored: with nothing to reattach to, their shell dies with the socket.
type terminalSessionTable struct {
	mu        sync.Mutex
	sessions  map[string]*terminalSession
	detachTTL time.Duration
}

func newTerminalSessionTable() *terminalSessionTable {
	return &terminalSessionTable{
		sessions:  make(map[string]*terminalSession),
		detachTTL: terminalDetachTTL,
	}
}

// lookup returns the resumable session for a frontend terminal id, or nil.
// Anonymous sessions live in the table (so kill/shutdown can reach them) but
// are never handed out for reattach.
func (t *terminalSessionTable) lookup(id string) *terminalSession {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.sessions[id]
	if s == nil || !s.resumable {
		return nil
	}
	return s
}

func (t *terminalSessionTable) put(id string, s *terminalSession) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sessions[id] = s
}

// remove drops id only if it still maps to s, so a fresh shell that reused
// the id after a late exit of the old one is never evicted by mistake.
func (t *terminalSessionTable) remove(id string, s *terminalSession) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.sessions[id] == s {
		delete(t.sessions, id)
	}
}

func (t *terminalSessionTable) count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.sessions)
}
