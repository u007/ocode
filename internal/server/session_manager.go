package server

import (
	"encoding/json"
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

// sessionEntry is one registry row. Bootstrap/turn state fields became real in
// Part 03 (async bootstrap with observable stages, turn lifecycle).
type sessionEntry struct {
	SessionID   string
	ProjectRoot string
	WindowID    string

	// agent is the lazily-built in-memory agent session. Nil until the first
	// turn; released by EvictIdle. Mirrored in Handler.agents for the legacy
	// lookup paths.
	agent *agentSession

	// bootstrapStage is the current bootstrap stage: "" (idle/not started),
	// "tools", "mcp", "model", or "ready" (terminal). On a failed bootstrap it
	// stays at the failing stage and bootstrapErr carries the error.
	bootstrapStage string
	// bootstrapErr is the error message of the last failed bootstrap ("" when
	// the last bootstrap succeeded or none ran). A later successful bootstrap
	// clears it.
	bootstrapErr string
	// turnActive is true while a turn is running on this session's agent.
	turnActive bool
	// turnStartedAt is set when the current turn becomes active; cleared when
	// it completes (turnEndedAt captures the end time and TurnTookMs is derived
	// from the pair). These drive the web's current-input and last-took timers.
	turnStartedAt time.Time
	turnEndedAt   time.Time
	// lastSeq is the last event-bus sequence observed for this session, the
	// reconcile watermark returned by GET /api/sessions/:id/state.
	lastSeq uint64

	// pending holds user message contents persisted to the session's on-disk
	// transcript but whose turns have not started yet (Part 03 persist-then-
	// 202). Bootstrap strips exactly len(pending) trailing messages from the
	// disk transcript so runTurn's in-memory append does not duplicate them.
	// Turn jobs shift one entry per successfully started turn.
	pending []string

	// pendingStartRequestID correlates the session_started frame to the tab
	// that created a brand-new session. Set when the session is created and
	// consumed (cleared) by the first turn that runs, so the frame survives a
	// bootstrap failure that delays the first turn.
	pendingStartRequestID string

	// lastActivity is the last time the entry had an active turn or was
	// touched. Used by EvictIdle.
	lastActivity time.Time

	// building is true while a bootstrap goroutine is constructing the agent
	// for this entry (single-flight per session, Part 03).
	building bool

	// liveFrames buffers the streaming SSE frames (text/thinking/tool_*)
	// broadcastEvent emits while turnActive is true, so a client that reloads
	// mid-turn can replay what already streamed instead of losing it — see
	// appendLiveFrame and GET /api/sessions/:id/state. Cleared on every
	// turnActive transition (fresh turn, or turn/continuation finished —
	// the final `messages` broadcast is authoritative from then on).
	// liveFramesSize tracks the summed byte length of liveFrames' Data so the
	// cap below can head-truncate without re-marshaling on every append.
	liveFrames     []liveFrame
	liveFramesSize int
}

// liveFrame is one buffered SSE frame from an in-progress turn. Field names
// mirror Envelope's wire shape (event_bus.go) so the JSON serializes directly
// into the frontend's BusEnvelope-shaped replay path.
type liveFrame struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
	Seq   uint64          `json:"seq"`
	size  int             // len(Data), cached for the byte-cap sweep below
}

// liveFrameEvents is the fixed set of event types buffered for mid-turn
// replay: the token-level streaming activity a reload actually loses.
// Deliberately excludes question/permission asks — those are written into
// the persisted transcript as a sentinel before the turn pauses (see
// tailIsPermissionAsk), so a reload already recovers them from disk.
var liveFrameEvents = map[string]bool{
	"text":        true,
	"thinking":    true,
	"tool_start":  true,
	"tool_output": true,
	"tool_result": true,
}

// liveFramesByteCap bounds a single session's buffer so a very long reply
// (or a runaway tool_output stream) can't grow it unbounded; older frames are
// dropped head-first once the cap is exceeded. Generous for one turn's worth
// of streaming text.
const liveFramesByteCap = 256 * 1024

// NewSessionManager builds an empty registry. idleTimeout is the idle-agent
// eviction threshold; projectRoots returns the candidate project roots for
// resolving unknown session ids; onEvict (optional) is called with a session
// id after EvictIdle releases its agent.
func NewSessionManager(idleTimeout time.Duration, projectRoots func() []string, onEvict func(string)) *SessionManager {
	return &SessionManager{
		entries:      make(map[string]*sessionEntry),
		idleTimeout:  idleTimeout,
		projectRoots: projectRoots,
		onEvict:      onEvict,
	}
}

// Register binds sessionID to projectRoot. If the session is already bound,
// the binding is updated (an explicit project_path from the client wins over
// an earlier implicit resolution). An agent that is already built is kept.
func (m *SessionManager) Register(sessionID, projectRoot string) *sessionEntry {
	return m.RegisterWithWindow(sessionID, projectRoot, "")
}

// RegisterWithWindow is Register that also binds a windowId for per-window
// profile isolation. WindowID is sticky: first window wins, later calls with
// a different non-empty windowId update it (e.g. user moved tab), empty
// windowId leaves existing value intact.
func (m *SessionManager) RegisterWithWindow(sessionID, projectRoot, windowID string) *sessionEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.entries[sessionID]
	if !ok {
		entry = &sessionEntry{SessionID: sessionID, ProjectRoot: projectRoot, WindowID: windowID}
		m.entries[sessionID] = entry
	} else {
		if projectRoot != "" {
			entry.ProjectRoot = projectRoot
		}
		if windowID != "" {
			entry.WindowID = windowID
		}
	}
	entry.lastActivity = time.Now()
	return entry
}

// SetWindowID updates the window binding for an existing session. No-op if
// session unknown or windowID empty.
func (m *SessionManager) SetWindowID(sessionID, windowID string) {
	if windowID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.entries[sessionID]; ok {
		e.WindowID = windowID
		e.lastActivity = time.Now()
	}
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
// Active turns exempt the entry from idle eviction. Clears liveFrames on
// every transition: turning on starts a fresh turn's buffer, turning off
// means the turn (or continuation) just finished and the authoritative
// `messages` broadcast/persisted transcript supersedes whatever was buffered.
func (m *SessionManager) setTurnActive(sessionID string, active bool) {
	now := time.Now()
	m.mu.Lock()
	if e := m.entries[sessionID]; e != nil {
		e.turnActive = active
		e.lastActivity = now
		e.liveFrames = nil
		e.liveFramesSize = 0
		if active {
			e.turnStartedAt = now
			e.turnEndedAt = time.Time{}
		} else {
			if !e.turnStartedAt.IsZero() {
				e.turnEndedAt = now
			}
		}
	}
	m.mu.Unlock()
}

// appendLiveFrame buffers one streaming SSE frame for mid-turn replay if its
// event type is in liveFrameEvents and the session currently has an active
// turn. seq is the bus sequence the frame was published at (broadcastEvent
// passes h.bus.LastSeq() right after publishing), so a client can filter out
// frames it already received live during a reconnect race.
func (m *SessionManager) appendLiveFrame(sessionID, event string, data any, seq uint64) {
	if !liveFrameEvents[event] {
		return
	}
	raw, err := json.Marshal(data)
	if err != nil {
		log.Printf("session manager: marshal live frame %q for session %s: %v", event, sessionID, err)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.entries[sessionID]
	if e == nil || !e.turnActive {
		return
	}
	e.liveFrames = append(e.liveFrames, liveFrame{Event: event, Data: raw, Seq: seq, size: len(raw)})
	e.liveFramesSize += len(raw)
	for e.liveFramesSize > liveFramesByteCap && len(e.liveFrames) > 1 {
		e.liveFramesSize -= e.liveFrames[0].size
		e.liveFrames = e.liveFrames[1:]
	}
}

// IsTurnActive reports whether a turn is currently running on the session.
// Used by profile reconciliation to avoid tearing down an agent mid-turn.
func (m *SessionManager) IsTurnActive(sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.entries[sessionID]; e != nil {
		return e.turnActive
	}
	return false
}

// TurnTiming returns the stored turn start/end for a session. Zero times mean
// no turn has run yet (or the entry does not exist).
func (m *SessionManager) TurnTiming(sessionID string) (startedAt, endedAt time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.entries[sessionID]; e != nil {
		return e.turnStartedAt, e.turnEndedAt
	}
	return time.Time{}, time.Time{}
}

// EvictIdle releases the built agent of every entry that has sat idle longer
// than the threshold with no active turn. The registry entry and the on-disk
// session remain; the agent rebuilds on the next message. Returns the ids of
// the evicted sessions so the caller can drop mirror state.
//
// A session paused on an unresolved permission ask is exempt even past the
// timeout: evicting it would delete it from the handler's session map, so a
// later resolve request 404s ("no pending permission found") instead of
// completing, and the dialog is left on screen with no way to actually
// dismiss it. The turn only "goes idle" (lastActivity stamped, turnActive
// cleared) the instant it pauses on the sentinel, so a long-unanswered dialog
// is exactly the case this sweep would otherwise catch.
func (m *SessionManager) EvictIdle() []string {
	type candidate struct {
		id string
		as *agentSession
	}

	m.mu.Lock()
	var candidates []candidate
	for id, e := range m.entries {
		if e.agent == nil || e.turnActive {
			continue
		}
		if time.Since(e.lastActivity) <= m.idleTimeout {
			continue
		}
		// Capture the agent pointer itself while m.mu is held — re-reading
		// e.agent later, unsynchronized, would race a concurrent setAgent
		// (e.g. a session rebuild) and could see it go nil, panicking on
		// as.mu.Lock() below.
		candidates = append(candidates, candidate{id, e.agent})
	}
	m.mu.Unlock()

	// Checking for a pending ask needs the agent session's own lock (as.mu),
	// not m.mu — the same lock-order hazard findPendingSession documents:
	// a running turn takes as.mu first and m.mu (via setTurnActive) second,
	// so this must not hold m.mu while taking as.mu.
	var evicted []string
	for _, c := range candidates {
		c.as.mu.Lock()
		pending := tailIsPermissionAsk(c.as.messages)
		c.as.mu.Unlock()
		if pending {
			continue
		}

		m.mu.Lock()
		if e := m.entries[c.id]; e != nil && e.agent == c.as {
			e.agent = nil
			evicted = append(evicted, c.id)
		}
		m.mu.Unlock()
	}

	for _, id := range evicted {
		log.Printf("session manager: evicting idle agent for session %s", id)
		if m.onEvict != nil {
			m.onEvict(id)
		}
	}
	return evicted
}

// ReleaseAgent releases the built agent for one session immediately,
// regardless of idle time — the explicit counterpart of EvictIdle for the
// session-close path (web/desktop UI closing a session tab). Like EvictIdle,
// it never releases an agent while a turn is active, and a session paused on
// an unanswered permission/question ask is exempt (releasing it would make
// the pending resolve 404). The registry entry and the on-disk session
// remain; a later message rebuilds the agent. Returns true when a resident
// agent was released.
//
// Lock order mirrors EvictIdle: m.mu is never held while taking as.mu. A
// running turn takes as.mu first and then m.mu (via setTurnActive), so
// holding both in the opposite order here would deadlock a close racing a
// turn start. The pending-ask check happens under as.mu alone, after
// releasing m.mu; the entry is then re-verified under m.mu before clearing.
func (m *SessionManager) ReleaseAgent(sessionID string) bool {
	// Phase 1: snapshot under m.mu only. Capture the agent pointer itself —
	// re-reading e.agent later, unsynchronized, would race a concurrent
	// setAgent (e.g. a session rebuild) and could see it go nil.
	m.mu.Lock()
	e := m.entries[sessionID]
	if e == nil || e.agent == nil || e.turnActive {
		m.mu.Unlock()
		return false
	}
	as := e.agent
	release := false
	m.mu.Unlock()

	// Phase 2: permission/question-ask check under the session lock (as.mu),
	// not m.mu — the same lock-order hazard findPendingSession documents.
	// Exempting unresolved asks mirrors EvictIdle: releasing here would 404
	// the pending resolve and strand the dialog.
	as.mu.Lock()
	pending := tailIsPermissionAsk(as.messages)
	as.mu.Unlock()
	if pending {
		return false
	}

	// Phase 3: re-verify under m.mu that the entry still owns this exact
	// agent (a rebuild or new turn may have raced in) and that no turn
	// started since the snapshot, then detach it. onEvict runs outside m.mu.
	m.mu.Lock()
	if e2 := m.entries[sessionID]; e2 != nil && e2.agent == as && !e2.turnActive {
		e2.agent = nil
		e2.lastActivity = time.Now()
		release = true
	}
	m.mu.Unlock()

	if release && m.onEvict != nil {
		m.onEvict(sessionID)
	}
	return release
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

// Bootstrap stage order, indexed for transition validation. "" is idle (no
// bootstrap attempted); "ready" is terminal.
// Order matches the actual emission sequence in buildAgentSession:
// model → tools → mcp → ready.
var bootstrapStageOrder = map[string]int{
	"":      0,
	"model": 1,
	"tools": 2,
	"mcp":   3,
	"ready": 4,
}

// SetBootstrapStage advances the entry's bootstrap stage. Illegal transitions
// (jumping backwards, or mutating a finished bootstrap) are error-logged but
// the stage is still recorded — the stage is diagnostic state that the state
// endpoint reports, never a gate on agent construction. A transition to ""
// resets a failed bootstrap for a retry.
func (m *SessionManager) SetBootstrapStage(sessionID, stage string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.entries[sessionID]
	if e == nil {
		log.Printf("session manager: setBootstrapStage(%q) for unknown session", sessionID)
		return
	}
	if !validBootstrapTransition(e.bootstrapStage, stage) {
		log.Printf("session manager: ERROR illegal bootstrap transition %q -> %q for session %s", e.bootstrapStage, stage, sessionID)
	}
	e.bootstrapStage = stage
	if stage == "ready" {
		// A successful bootstrap clears any prior failure so the state
		// endpoint does not keep reporting a stale error.
		e.bootstrapErr = ""
	}
}

// validBootstrapTransition reports whether moving from one stage to another
// is a forward progression. Jumping ahead (tools -> ready) is tolerated; only
// backwards jumps and mutations of a terminal stage are illegal.
func validBootstrapTransition(from, to string) bool {
	f, okF := bootstrapStageOrder[from]
	t, okT := bootstrapStageOrder[to]
	if !okF || !okT {
		return false
	}
	return t > f
}

// SetBootstrapFailed records a bootstrap failure at the failing stage so the
// state endpoint can report which stage broke.
func (m *SessionManager) SetBootstrapFailed(sessionID, stage string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.entries[sessionID]; e != nil {
		e.bootstrapStage = stage
	}
}

// SetBootstrapError records the failure message on the entry (used by the
// state endpoint's bootstrap_error field).
func (m *SessionManager) SetBootstrapError(sessionID, errMsg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.entries[sessionID]; e != nil {
		e.bootstrapErr = errMsg
	}
}

// SetLastSeq records the event-bus sequence watermark for the session (the
// reconcile last_seq). The bus sequence is global and monotonic, so the
// latest observed value is simply stored per session.
func (m *SessionManager) SetLastSeq(sessionID string, seq uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.entries[sessionID]; e != nil {
		e.lastSeq = seq
	}
}

// PushPending records a user message content that has been persisted to disk
// but whose turn has not started yet.
func (m *SessionManager) PushPending(sessionID, content string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.entries[sessionID]; e != nil {
		e.pending = append(e.pending, content)
	}
}

// PendingFront returns the oldest persisted-but-unturned message content.
func (m *SessionManager) PendingFront(sessionID string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.entries[sessionID]
	if e == nil || len(e.pending) == 0 {
		return "", false
	}
	return e.pending[0], true
}

// ShiftPending drops the oldest pending message after its turn has started
// (the message has been appended to the in-memory transcript by runTurn).
func (m *SessionManager) ShiftPending(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.entries[sessionID]; e != nil && len(e.pending) > 0 {
		e.pending = e.pending[1:]
	}
}

// PendingCount reports how many messages are persisted but not yet turned.
// Bootstrap strips exactly this many trailing messages from the on-disk
// transcript.
func (m *SessionManager) PendingCount(sessionID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.entries[sessionID]; e != nil {
		return len(e.pending)
	}
	return 0
}

// SetSessionStart records that a brand-new session was created and its first
// turn must emit session_started (correlated to the creating tab's
// requestID), even if that first turn runs after a bootstrap failure.
func (m *SessionManager) SetSessionStart(sessionID, requestID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.entries[sessionID]; e != nil {
		e.pendingStartRequestID = requestID
	}
}

// ConsumeSessionStart returns and clears the pending session_started marker.
// The bool reports whether a marker was set.
func (m *SessionManager) ConsumeSessionStart(sessionID string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.entries[sessionID]
	if e == nil || e.pendingStartRequestID == "" {
		return "", false
	}
	rid := e.pendingStartRequestID
	e.pendingStartRequestID = ""
	return rid, true
}

// SessionState is the wire shape of GET /api/sessions/:id/state — the
// reconcile endpoint. The frontend derives streaming state from it
// (turn_active) and detects events it may have missed via last_seq.
// LiveFrames carries whatever of the current turn's streaming activity is
// still buffered (nil/empty once the turn ends), so a client reconnecting
// mid-turn can replay it instead of only seeing it start from a blank slate.
type SessionState struct {
	SessionID      string      `json:"session_id"`
	BootstrapStage string      `json:"bootstrap_stage"`
	BootstrapError string      `json:"bootstrap_error,omitempty"`
	TurnActive     bool        `json:"turn_active"`
	LastSeq        uint64      `json:"last_seq"`
	LiveFrames     []liveFrame `json:"live_frames,omitempty"`
}

// State returns the session's reconcile state. The bool reports whether the
// session is known to the registry (false for sessions in no registered
// project).
func (m *SessionManager) State(sessionID string) (SessionState, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.entries[sessionID]
	if e == nil {
		return SessionState{}, false
	}
	var frames []liveFrame
	if len(e.liveFrames) > 0 {
		frames = append([]liveFrame(nil), e.liveFrames...)
	}
	return SessionState{
		SessionID:      e.SessionID,
		BootstrapStage: e.bootstrapStage,
		BootstrapError: e.bootstrapErr,
		TurnActive:     e.turnActive,
		LastSeq:        e.lastSeq,
		LiveFrames:     frames,
	}, true
}
