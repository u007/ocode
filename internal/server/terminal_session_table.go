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
// instead of spawning a new one. Anonymous sockets (no terminal_id) ARE
// stored under generated anon-N keys so the kill/shutdown paths can reach
// them; lookup deliberately hides them since they can never reattach.
type terminalSessionTable struct {
	mu       sync.Mutex
	sessions map[string]*terminalSession
	// creating tracks terminal ids whose shell is mid-spawn. reserve() installs
	// a channel here before the caller runs pty.Start, and completeCreate
	// closes it after the shell is published (or the spawn failed), so
	// concurrent sockets for the same brand-new id serialize on one spawn.
	creating map[string]chan struct{}
	// sealed, once set by sealForShutdown, refuses all new admissions
	// (reserve/put). Winners that already hold a reservation still publish
	// via completeCreate so shutdown's second snapshot can terminate them.
	sealed    bool
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
// are never handed out for reattach. lookup also reports nil while the table
// is sealed for shutdown so no new reattach can resurrect a session that
// shutdown is about to terminate.
func (t *terminalSessionTable) lookup(id string) *terminalSession {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.sealed {
		return nil
	}
	s := t.sessions[id]
	if s == nil || !s.resumable {
		return nil
	}
	return s
}

// reserve atomically claims the right to create the shell for id, settling
// the lookup/create race under one lock:
//
//   - table sealed for shutdown → (nil, false, nil): caller must refuse with
//     a retryable "server is shutting down" error, mirroring pty.Start
//     failure, and must NOT spawn;
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
	if t.sealed {
		return nil, false, nil
	}
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
//
// It reports whether the session was accepted into the table. After the table
// is sealed for shutdown it always reports false and stores nothing — even a
// successfully spawned session — so a pty.Start that raced the seal can never
// escape termination. The reservation owner must then immediately terminate
// the just-spawned shell itself and return a retryable "server is shutting
// down" error (mirroring the anonymous put-refusal path). Waiters still wake
// via the closed done channel and observe a nil lookup, i.e. a retryable
// spawn failure.
func (t *terminalSessionTable) completeCreate(id string, sess *terminalSession) bool {
	t.mu.Lock()
	done, ok := t.creating[id]
	delete(t.creating, id)
	if sess != nil && !t.sealed {
		t.sessions[id] = sess
	}
	accepted := sess == nil || !t.sealed
	t.mu.Unlock()
	if ok {
		close(done)
	}
	return accepted
}

// put publishes an anonymous session. It reports false (and stores nothing)
// when the table is sealed for shutdown, so a pty.Start that raced the seal
// is torn down by the caller instead of escaping termination.
func (t *terminalSessionTable) put(id string, s *terminalSession) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.sealed {
		return false
	}
	t.sessions[id] = s
	return true
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

// sealForShutdown refuses all future admissions (reserve/put) and returns the
// current live sessions. Shutdown calls this FIRST so no pty spawned after
// the seal can enter the table and escape termination. A reservation winner
// whose pty.Start raced the seal is refused by completeCreate (which stores
// nothing post-seal); the owner tears that shell down itself, so shutdown
// needs no second snapshot and never depends on the shutdown context still
// being live when a reservation publishes.
func (t *terminalSessionTable) sealForShutdown() (live []*terminalSession) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sealed = true
	live = make([]*terminalSession, 0, len(t.sessions))
	for _, s := range t.sessions {
		if s != nil {
			live = append(live, s)
		}
	}
	return live
}
