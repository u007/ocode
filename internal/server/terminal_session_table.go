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
	mu       sync.Mutex
	sessions map[string]*terminalSession
	// creating tracks terminal ids whose shell is mid-spawn. reserve() installs
	// a channel here before the caller runs pty.Start, and completeCreate
	// closes it after the shell is published (or the spawn failed), so
	// concurrent sockets for the same brand-new id serialize on one spawn.
	creating  map[string]chan struct{}
	detachTTL time.Duration
}

func newTerminalSessionTable() *terminalSessionTable {
	return &terminalSessionTable{
		sessions:  make(map[string]*terminalSession),
		creating:  make(map[string]chan struct{}),
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

// reserve atomically claims the right to create the shell for id, settling
// the lookup/create race under one lock:
//
//   - a live session already exists → returned as existing (caller reattaches,
//     never spawns);
//   - another goroutine is mid-create → created=false plus a done channel the
//     caller waits on, then re-looks-up and reattaches to the winner;
//   - id is free → created=true: THIS caller must run pty.Start and then
//     publish the session via completeCreate (which closes done for waiters).
//
// Because the reservation is installed before pty.Start, N concurrent sockets
// for the same never-seen id still spawn exactly one shell; losers never own
// a process to clean up.
func (t *terminalSessionTable) reserve(id string) (existing *terminalSession, created bool, done <-chan struct{}) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if s := t.sessions[id]; s != nil {
		if !s.hasExited() {
			return s, false, nil
		}
		// Stale entry: the shell exited and exit() is still tearing down
		// (terminalExited → remove runs after the process is reaped). Claim
		// the slot fresh — the stale teardown's remove() is identity-checked,
		// so it cannot clobber the session we publish here.
	}
	if d, ok := t.creating[id]; ok {
		return nil, false, d
	}
	d := make(chan struct{})
	t.creating[id] = d
	return nil, true, d
}

// completeCreate publishes the freshly spawned session for id (or marks the
// spawn as failed with a nil session) and wakes every goroutine waiting on
// the reservation channel. It is only called by the reservation owner.
func (t *terminalSessionTable) completeCreate(id string, sess *terminalSession) {
	t.mu.Lock()
	done, ok := t.creating[id]
	delete(t.creating, id)
	if sess != nil {
		t.sessions[id] = sess
	}
	t.mu.Unlock()
	if ok {
		close(done)
	}
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
