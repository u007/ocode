package server

import (
	"log"
	"sync"
	"time"

	"github.com/u007/ocode/internal/session"
)

// SessionManager is the single authority for session ID → project root +
// agent lifecycle on the server. Every session-scoped handler resolves
// through it, so sessions belonging to any registered project — not just the
// server's own workdir — load and run (this is what killed the cross-project
// 404s: session *listing* was per-project but session *loading* resolved
// against the server's own workdir).
//
// Thread-safety: the manager has its own mutex and must never be called with
// h.mu held. Resolution performs disk I/O (LoadForDir per candidate project);
// holding a map lock across it would serialize unrelated endpoints.
type SessionManager struct {
	mu sync.Mutex
	// entries is the registry, keyed by session id. Entries are created by
	// Register (explicit binding) or by Resolve (implicit binding once the
	// owning project is found on disk) and survive agent eviction.
	entries map[string]*sessionEntry

	// idleTimeout is how long a built agent session may sit without an
	// active turn before EvictIdle releases it. Registry entry and on-disk
	// session remain; the agent rebuilds on the next message.
	idleTimeout time.Duration

	// projectRoots returns the current search space for unknown session IDs:
	// the registered project store's roots. It is a func so the manager has
	// no dependency on Handler internals and picks up store changes live.
	projectRoots func() []string

	// onEvict is invoked (outside the manager lock) with a session id whose
	// agent was just released, so the Handler can drop the mirror entry in
	// h.agents. May be nil.
	onEvict func(sessionID string)
}

// sessionEntry is one registry row. Bootstrap/turn state fields are
// placeholders in Part 01 and become real in Part 03 (async bootstrap, turn
// lifecycle).
type sessionEntry struct {
	SessionID   string
	ProjectRoot string

	// agent is the lazily-built in-memory agent session. Nil until the first
	// turn; released by EvictIdle. Mirrored in Handler.agents for the legacy
	// lookup paths.
	agent *agentSession

	// bootstrapStage is one of "", "tools", "mcp", "model", "ready"
	// (Part 03 fills these transitions).
	bootstrapStage string
	// turnActive is true while a turn is running on this session's agent.
	turnActive bool
	// lastSeq is the last event-bus sequence observed for this session
	// (Part 02/03).
	lastSeq uint64

	// lastActivity is the last time the entry had an active turn or was
	// touched. Used by EvictIdle.
	lastActivity time.Time

	// building is true while a bootstrap goroutine is constructing the agent
	// for this entry (single-flight per session, Part 03).
	building bool
}

// NewSessionManager builds an empty registry. idleTimeout is the idle-agent
// eviction threshold; projectRoots returns the candidate project roots for
// resolving unknown session ids; onEvict (optional) is called with a session
// id after EvictIdle releases its agent.
func NewSessionManager(idleTimeout time.Duration, projectRoots func() []string, onEvict func(string)) *SessionManager {
	return &SessionManager{
		entries:     make(map[string]*sessionEntry),
		idleTimeout: idleTimeout,
		projectRoots: projectRoots,
		onEvict:     onEvict,
	}
}

// Register binds sessionID to projectRoot. If the session is already bound,
// the binding is updated (an explicit project_path from the client wins over
// an earlier implicit resolution). An agent that is already built is kept.
func (m *SessionManager) Register(sessionID, projectRoot string) *sessionEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.entries[sessionID]
	if !ok {
		entry = &sessionEntry{SessionID: sessionID, ProjectRoot: projectRoot}
		m.entries[sessionID] = entry
	} else if projectRoot != "" {
		entry.ProjectRoot = projectRoot
	}
	entry.lastActivity = time.Now()
	return entry
}

// Lookup returns the registry entry for sessionID if it is already known
// (registered or resolved), without touching the disk. Returns nil when the
// session is not yet known.
func (m *SessionManager) Lookup(sessionID string) *sessionEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.entries[sessionID]
}

// Resolve returns the registry entry for sessionID, resolving an unknown id
// by checking each registered project's storage dir (LoadForDir) and caching
// the owning project root. It errors only when the session exists in no
// registered project. Results are cached, so a later Resolve of the same id
// never touches the disk again.
func (m *SessionManager) Resolve(sessionID string) (*sessionEntry, error) {
	if e := m.Lookup(sessionID); e != nil {
		return e, nil
	}

	// Search the registered project roots for the session's on-disk home.
	// The first project whose storage dir contains the session wins.
	var root string
	for _, candidate := range m.projectRoots() {
		if candidate == "" {
			continue
		}
		if _, err := session.LoadForDir(candidate, sessionID); err == nil {
			root = candidate
			break
		}
	}
	if root == "" {
		return nil, errSessionNotFound
	}
	return m.Register(sessionID, root), nil
}

// errSessionNotFound is returned by Resolve when the session exists in no
// registered project.
var errSessionNotFound = &sessionNotFoundError{}

type sessionNotFoundError struct{}

func (*sessionNotFoundError) Error() string { return "session not found" }

// defaultSessionIdleTimeout is how long a built agent session may sit without
// an active turn before the periodic eviction pass releases it (default 30
// minutes, per the design spec). Tests may construct a manager with a shorter
// threshold.
const defaultSessionIdleTimeout = 30 * time.Minute

// setAgent attaches (or detaches, with nil) the built agent to the entry.
func (m *SessionManager) setAgent(sessionID string, as *agentSession) {
	m.mu.Lock()
	if e := m.entries[sessionID]; e != nil {
		e.agent = as
		if as != nil {
			e.lastActivity = time.Now()
		}
	}
	m.mu.Unlock()
}

// touch updates the entry's last-activity stamp (used at turn start).
func (m *SessionManager) touch(sessionID string) {
	m.mu.Lock()
	if e := m.entries[sessionID]; e != nil {
		e.lastActivity = time.Now()
	}
	m.mu.Unlock()
}

// setTurnActive marks whether a turn is currently running on the session.
// Active turns exempt the entry from idle eviction.
func (m *SessionManager) setTurnActive(sessionID string, active bool) {
	m.mu.Lock()
	if e := m.entries[sessionID]; e != nil {
		e.turnActive = active
		e.lastActivity = time.Now()
	}
	m.mu.Unlock()
}

// EvictIdle releases the built agent of every entry that has sat idle longer
// than the threshold with no active turn. The registry entry and the on-disk
// session remain; the agent rebuilds on the next message. Returns the ids of
// the evicted sessions so the caller can drop mirror state.
func (m *SessionManager) EvictIdle() []string {
	var evicted []string
	m.mu.Lock()
	for id, e := range m.entries {
		if e.agent == nil || e.turnActive {
			continue
		}
		if time.Since(e.lastActivity) <= m.idleTimeout {
			continue
		}
		e.agent = nil
		evicted = append(evicted, id)
	}
	m.mu.Unlock()

	for _, id := range evicted {
		log.Printf("session manager: evicting idle agent for session %s", id)
		if m.onEvict != nil {
			m.onEvict(id)
		}
	}
	return evicted
}

// Snapshot returns a copy of the current entries for diagnostics/tests.
func (m *SessionManager) Snapshot() []*sessionEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*sessionEntry, 0, len(m.entries))
	for _, e := range m.entries {
		cp := *e
		out = append(out, &cp)
	}
	return out
}
