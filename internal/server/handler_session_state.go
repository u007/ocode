package server

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/session"
)

// HandleSessionState is the reconcile endpoint (Part 03). The frontend derives
// streaming state from it — bootstrap_stage and turn_active — and uses
// last_seq to detect events it may have missed during a reconnect. Reconcile
// is state fetch + transcript refetch for the persisted transcript; the one
// deliberate exception is live_frames, which replays whatever streaming
// text/thinking/tool activity is still buffered from the current turn (see
// appendLiveFrame in session_manager.go) so a mid-turn reload doesn't lose the
// in-progress reply while waiting for turn_done.
//
// A session that exists in no registered project 404s; every other session
// (registered explicitly, resolved from disk, or a bridged TUI session) gets a
// state snapshot.
func (h *Handler) HandleSessionState(w http.ResponseWriter, r *http.Request, id string) {
	if _, err := h.sessions.Resolve(id); err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	state, ok := h.sessions.State(id)
	if !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	resp := sessionStateResponse{SessionState: state}
	// Attach the live pending ask (if any) from the resident agent's trailing
	// tool round. The frontend already derives pending asks from a fetched
	// transcript's sentinels, but that only works when the paused tool result
	// actually reached disk: a session whose in-memory ask outlived a failed /
	// conflicting save (or whose pause post-dated the last write) has no
	// sentinel to recover from, leaving the browser with no dialog while every
	// send is rejected by ErrPermissionPending. Reading the live transcript is
	// the only authoritative source in that case.
	//
	// livePendingAsks takes h.mu only to read the session pointer, releases
	// it, then takes as.mu — preserving the as.mu → h.mu lock order (see
	// agent_session.go).
	resp.PendingAsks = h.livePendingAsks(id)
	writeJSON(w, http.StatusOK, resp)
}

// sessionStateResponse is SessionState plus the live pending-ask payload.
// SessionState is embedded so every existing field serializes exactly as
// before; pending_asks is omitempty so an idle session's body is unchanged.
type sessionStateResponse struct {
	SessionState
	PendingAsks *PendingAsks `json:"pending_asks,omitempty"`
}

// PendingAsks is the unresolved permission/question prompt(s) a live agent
// session is currently paused on. Each entry mirrors the corresponding SSE
// frame (PermissionEvent / QuestionEvent) so the browser can dispatch it
// through the same PERMISSION_REQUEST / QUESTION_REQUEST reducer path a live
// event would take.
type PendingAsks struct {
	Permissions []PermissionEvent `json:"permissions,omitempty"`
	Questions   []QuestionEvent   `json:"questions,omitempty"`
}

// livePendingAsks returns the unresolved asks held in the session's resident
// agent transcript, or nil when the session has no live agent, a turn is in
// flight, or it is not paused on one. A resolved ask has its sentinel replaced
// in place, so this returns nil again once the user answers.
//
// The read is a non-blocking TryLock on purpose: runTurn holds as.mu for the
// whole turn (minutes), and this runs inside the reconcile HTTP handler the
// browser and watchdog poll — a blocking lock would pin an HTTP connection
// behind the turn (the "stuck session" class, see AGENTS). While a turn holds
// the lock it cannot yet be paused on an ask (the pause and the unlock happen
// together when the step returns), so reporting nothing is correct; any ask it
// does raise arrives over SSE.
func (h *Handler) livePendingAsks(id string) *PendingAsks {
	as := h.lookupAgentSession(id)
	if as == nil {
		return nil
	}
	if !as.mu.TryLock() {
		return nil
	}
	defer as.mu.Unlock()
	return pendingAsksFromMessages(as.messages)
}

// pendingAsksFromMessages extracts unresolved asks from the trailing tool
// round of msgs. A single round may pause on more than one ask (parallel
// dispatch runs several calls before the pause check), so the whole trailing
// run is scanned — see trailingToolRunStart.
func pendingAsksFromMessages(msgs []agent.Message) *PendingAsks {
	var out PendingAsks
	for i := trailingToolRunStart(msgs); i < len(msgs); i++ {
		m := msgs[i]
		if m.Role != "tool" {
			continue
		}
		if req, ok := parsePermissionAsk(m.Content); ok {
			out.Permissions = append(out.Permissions, newPermissionEvent(m.ToolID, req))
			continue
		}
		if prompts, ok := parseQuestionAsk(m.Content); ok {
			out.Questions = append(out.Questions, QuestionEvent{RequestID: m.ToolID, Questions: prompts})
		}
	}
	if len(out.Permissions) == 0 && len(out.Questions) == 0 {
		return nil
	}
	return &out
}

// HandleSessionStatus returns the per-session status snapshot: a superset of
// today's GET /api/tui-status snapshot (model, advisor, OCR, cwd, LSP servers)
// plus the context data that previously lived only on GET /api/sessions/:id/
// context (context_current_tokens, context_max_tokens, context_model) and the
// session identity itself. Unlike the process-global tui-status snapshot, the
// cwd is the session's owning project root and session_id is populated, so a
// multi-project frontend can render each tab's status independently.
func (h *Handler) HandleSessionStatus(w http.ResponseWriter, r *http.Request, id string) {
	entry, err := h.sessions.Resolve(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	snap := h.buildStatusSnapshot()
	// Per-session model override takes precedence over the process-wide config
	// model, so each chat tab shows and runs its own model instead of a single
	// global value reflected across every open session.
	snap.MainModel = h.effectiveSessionModel(id)
	// The bridged TUI session is driven by the TUI itself: its live snapshot
	// wins over the persisted/default model so the web mirrors what the TUI is
	// actually running right now.
	if rc := h.RCBridge(); rc != nil && rc.SessionID == id {
		if live := rc.TUIStatus(); live.MainModel != "" {
			snap.MainModel = live.MainModel
		}
	}
	snap.SessionID = id
	if entry.ProjectRoot != "" {
		snap.CWD = entry.ProjectRoot
	}
	// Populate persisted session title so the web tab bar shows the
	// authoritative title (auto fallback or LLM-generated) immediately,
	// not just after a generated-title status broadcast.
	if s, err := session.LoadForDir(entry.ProjectRoot, id); err == nil {
		snap.SessionTitle = s.Title
		if !s.CreatedAt.IsZero() {
			snap.SessionCreatedAt = s.CreatedAt.UTC().Format(time.RFC3339Nano)
		}
	}
	h.applySessionContext(&snap, id)
	h.applyTurnTiming(&snap, id)
	snap.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)

	writeJSON(w, http.StatusOK, snap)
}

// applySessionContext fills snap's context-window fields (context_current_
// tokens, context_max_tokens, context_model) for the session id.
//
// context_current_tokens is ALWAYS the backend's provider-reported value, never
// a character-count estimate over the persisted transcript:
//   - the bridged TUI session uses the TUI's live ContextCurrentTokens, which
//     it captured from the provider's last response;
//   - a headless (web/desktop) session uses its live agent's LastInputTokens,
//     captured from resp.Usage on the most recent LLM call.
//
// When neither is available (no live agent yet — e.g. a freshly restored
// session before its first turn) the field stays 0 and is omitted from the wire
// snapshot, so the gauge renders as unknown instead of a fabricated number.
//
// Every session-tagged status snapshot must go through this before it is
// broadcast or returned: buildStatusSnapshot() alone omits all three fields,
// and because they are omitempty on the wire, a snapshot without them makes
// the web sidebar's Context gauge drop to zero ("not reflected") until the
// next per-session fetch.
func (h *Handler) applySessionContext(snap *TUIStatus, id string) {
	model := ""
	maxTokens := 0
	var current int64

	if rc := h.RCBridge(); rc != nil && id == rc.SessionID {
		if live := rc.TUIStatus(); live.ContextModel != "" {
			model = live.ContextModel
			maxTokens = live.ContextMaxTokens
			current = int64(live.ContextCurrentTokens)
		}
	}
	if current == 0 {
		if as := h.lookupAgentSession(id); as != nil && as.agent != nil {
			current = as.agent.LastInputTokens()
		}
	}
	if model == "" && h.cfg != nil {
		model = h.effectiveSessionModel(id)
	}
	if maxTokens == 0 {
		maxTokens = int(agent.ModelWindow(model))
	}
	snap.ContextCurrentTokens = int(current)
	snap.ContextMaxTokens = maxTokens
	snap.ContextModel = model
}

// applyTurnTiming fills snap's turn timing fields from the SessionManager's
// authoritative turn lifecycle (setTurnActive). Used for every per-session
// status snapshot so the web can show current-input elapsed and last-took
// without an extra round-trip.
func (h *Handler) applyTurnTiming(snap *TUIStatus, id string) {
	if startedAt, endedAt := h.sessions.TurnTiming(id); !startedAt.IsZero() {
		snap.TurnStartedAt = startedAt.UTC().Format(time.RFC3339Nano)
		if h.sessions.IsTurnActive(id) {
			snap.TurnElapsedMs = time.Since(startedAt).Milliseconds()
		}
		if !endedAt.IsZero() {
			snap.TurnEndedAt = endedAt.UTC().Format(time.RFC3339Nano)
			snap.TurnTookMs = endedAt.Sub(startedAt).Milliseconds()
		}
	}
	// SessionCreatedAt is populated by the caller when it has the session
	// metadata; if not yet set, try to load it here as fallback.
	if snap.SessionCreatedAt == "" {
		if entry, err := h.sessions.Resolve(id); err == nil {
			if s, err := session.LoadForDir(entry.ProjectRoot, id); err == nil && !s.CreatedAt.IsZero() {
				snap.SessionCreatedAt = s.CreatedAt.UTC().Format(time.RFC3339Nano)
			}
		}
	}
}

// publishTurnStatusSnapshot broadcasts a fresh session-tagged "status" event
// whose context fields were resolved from the backend's provider-reported
// usage (the just-finished turn's agent LastInputTokens). Called right after a
// headless turn completes so the web/desktop sidebar's Context gauge moves with
// every turn instead of only on tab activation or reconnect. No-op when an RC
// bridge is attached — the TUI owns the status feed for its sessions and pushes
// its own snapshots.
func (h *Handler) publishTurnStatusSnapshot(sessionID string) {
	if h.RCBridge() != nil {
		return
	}
	snap := h.buildStatusSnapshot()
	snap.SessionID = sessionID
	var projectRoot string
	if entry, err := h.sessions.Resolve(sessionID); err == nil {
		projectRoot = entry.ProjectRoot
		if projectRoot != "" {
			snap.CWD = projectRoot
		}
	}
	h.applySessionContext(&snap, sessionID)
	if projectRoot != "" {
		if s, err := session.LoadForDir(projectRoot, sessionID); err == nil && !s.CreatedAt.IsZero() {
			snap.SessionCreatedAt = s.CreatedAt.UTC().Format(time.RFC3339Nano)
		}
	}
	h.applyTurnTiming(&snap, sessionID)
	// Reflect the session's effective (override-or-default) model so the
	// sidebar's Context gauge and Model row stay in sync per session.
	snap.MainModel = h.effectiveSessionModel(sessionID)
	snap.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	h.broadcastEvent(SSEEvent{SessionID: sessionID, Event: "status", Data: snap})
}

// effectiveSessionModel returns the model that should drive the agent for the
// given session. A per-session override persisted in the session transcript's
// metadata (metadata["model"]) wins over the process-wide config model, which
// is the fallback for sessions that have never picked their own. This is what
// makes the sidebar's model a per-chat-session setting rather than one global
// value mirrored across every open tab.
func (h *Handler) effectiveSessionModel(id string) string {
	if id != "" {
		if entry, err := h.sessions.Resolve(id); err == nil {
			if s, err := session.LoadForDir(entry.ProjectRoot, id); err == nil && s.Metadata != nil {
				if m, ok := s.Metadata["model"].(string); ok && m != "" {
					return m
				}
			}
		}
	}
	// SetWorkDir replaces h.cfg under h.mu. Keep the lock limited to this
	// in-memory read; session resolution and transcript loading above must stay
	// outside the handler map lock.
	h.mu.Lock()
	model := ""
	if h.cfg != nil {
		model = h.cfg.Model
	}
	h.mu.Unlock()
	return model
}

// setSessionModelOverride persists (or clears, when model == "") a per-session
// model override in the session transcript metadata so the choice survives
// restart and resume. It returns the model
// that is now in effect for the session.
func (h *Handler) setSessionModelOverride(id, model string) (string, error) {
	entry, err := h.sessions.Resolve(id)
	if err != nil {
		return "", err
	}
	// Metadata-only write: a full load→save of the transcript conflicts
	// whenever the stored file holds rows the loader filters out (see
	// session.UpdateMetadataForDir), which made every model pick 404.
	err = session.UpdateMetadataForDir(entry.ProjectRoot, id, func(md map[string]any) {
		if model == "" {
			delete(md, "model")
		} else {
			md["model"] = model
		}
	})
	if err != nil {
		return "", err
	}
	return h.effectiveSessionModel(id), nil
}

// pushSessionStatusSnapshot broadcasts a session-tagged status snapshot whose
// model reflects the session's effective (override-or-default) model. Used
// after a web-initiated per-session model change so that tab's sidebar updates
// immediately without touching any other session. Skipped only for the
// bridged TUI's own session — the TUI owns that session's status feed and
// would clobber a web override on its next snapshot; every other session
// (which gets no TUI-side snapshots) relies on this push to update live.
func (h *Handler) pushSessionStatusSnapshot(id string) {
	if rc := h.RCBridge(); rc != nil && rc.SessionID == id {
		return
	}
	snap := h.buildStatusSnapshot()
	snap.SessionID = id
	snap.MainModel = h.effectiveSessionModel(id)
	if entry, err := h.sessions.Resolve(id); err == nil && entry.ProjectRoot != "" {
		snap.CWD = entry.ProjectRoot
	}
	h.applySessionContext(&snap, id)
	h.applyTurnTiming(&snap, id)
	if snap.SessionCreatedAt == "" {
		if entry, err := h.sessions.Resolve(id); err == nil {
			if s, err := session.LoadForDir(entry.ProjectRoot, id); err == nil && !s.CreatedAt.IsZero() {
				snap.SessionCreatedAt = s.CreatedAt.UTC().Format(time.RFC3339Nano)
			}
		}
	}
	snap.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	h.broadcastEvent(SSEEvent{SessionID: id, Event: "status", Data: snap})
}

// HandleSetSessionModel sets a per-session model override for id. It validates
// the model id the same way the global config-model setter does (a bare id
// without a provider prefix can't be resolved back to a provider), persists
// it in the session transcript metadata, and pushes a fresh per-session status
// snapshot so the web sidebar updates at once.
func (h *Handler) HandleSetSessionModel(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Model string `json:"model"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !strings.Contains(req.Model, "/") && !strings.Contains(req.Model, ":") {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("model %q has no provider prefix; use a \"provider/model\" id", req.Model))
		return
	}
	h.mu.Lock()
	cfgNil := h.cfg == nil
	h.mu.Unlock()
	if cfgNil {
		writeError(w, http.StatusInternalServerError, "config not loaded")
		return
	}
	effective, err := h.setSessionModelOverride(id, req.Model)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	// Mirror the TUI's finishModelSwitch and the global config-model setter
	// (handler_config.go): every model pick lands in the shared recent list so
	// the picker's "Recently Used" section stays in sync across TUI, web and
	// desktop regardless of which surface made the switch.
	if strings.Contains(req.Model, "/") {
		if err := config.SaveRecentModel(req.Model); err != nil {
			log.Printf("save recent model: %v", err)
		}
	}
	h.pushSessionStatusSnapshot(id)
	writeJSON(w, http.StatusOK, map[string]string{"model": effective, "session_id": id})
}

// HandleClearSessionModel removes a per-session model override for id, so the
// session falls back to the process-wide config model again.
func (h *Handler) HandleClearSessionModel(w http.ResponseWriter, r *http.Request, id string) {
	effective, err := h.setSessionModelOverride(id, "")
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	h.pushSessionStatusSnapshot(id)
	writeJSON(w, http.StatusOK, map[string]string{"model": effective, "session_id": id})
}
