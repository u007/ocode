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
	writeJSON(w, http.StatusOK, state)
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
	}
	h.applySessionContext(&snap, id)
	snap.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)

	writeJSON(w, http.StatusOK, snap)
}

// applySessionContext fills snap's context-window fields (context_current_
// tokens, context_max_tokens, context_model) for the session id from its
// persisted transcript: current tokens are estimated from message characters
// (chars/4), max tokens come from the model window. When the TUI bridge is
// live and this is the bridged session, the bridge's provider-reported values
// win over the estimate.
//
// Every session-tagged status snapshot must go through this before it is
// broadcast or returned: buildStatusSnapshot() alone omits all three fields,
// and because they are omitempty on the wire, a snapshot without them makes
// the web sidebar's Context gauge drop to zero ("not reflected") until the
// next per-session fetch.
func (h *Handler) applySessionContext(snap *TUIStatus, id string) {
	root := ""
	if entry, err := h.sessions.Resolve(id); err == nil {
		root = entry.ProjectRoot
	}
	var totalChars int
	if s, err := session.LoadForDir(root, id); err == nil {
		for _, msg := range s.Messages {
			totalChars += len(msg.Content) + len(msg.ReasoningContent)
			for _, tc := range msg.ToolCalls {
				totalChars += len(tc.Function.Arguments)
			}
		}
	}
	current := totalChars / 4
	model := ""
	maxTokens := 0
	if rc := h.RCBridge(); rc != nil && id == rc.SessionID {
		if live := rc.TUIStatus(); live.ContextModel != "" {
			model = live.ContextModel
			maxTokens = live.ContextMaxTokens
			if live.ContextCurrentTokens > 0 {
				current = live.ContextCurrentTokens
			}
		}
	}
	if model == "" && h.cfg != nil {
		model = h.effectiveSessionModel(id)
	}
	if maxTokens == 0 {
		maxTokens = int(agent.ModelWindow(model))
	}
	snap.ContextCurrentTokens = current
	snap.ContextMaxTokens = maxTokens
	snap.ContextModel = model
}

// publishTurnStatusSnapshot broadcasts a fresh session-tagged "status" event
// whose context fields were computed from the just-persisted transcript.
// Called right after a headless turn completes so the web/desktop sidebar's
// Context gauge moves with every turn instead of only on tab activation or
// reconnect. No-op when an RC bridge is attached — the TUI owns the status
// feed for its sessions and pushes its own snapshots.
func (h *Handler) publishTurnStatusSnapshot(sessionID string) {
	if h.RCBridge() != nil {
		return
	}
	snap := h.buildStatusSnapshot()
	snap.SessionID = sessionID
	if entry, err := h.sessions.Resolve(sessionID); err == nil && entry.ProjectRoot != "" {
		snap.CWD = entry.ProjectRoot
	}
	h.applySessionContext(&snap, sessionID)
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
// model override in the session transcript metadata, then re-saves the
// transcript so the choice survives restart and resume. It returns the model
// that is now in effect for the session.
func (h *Handler) setSessionModelOverride(id, model string) (string, error) {
	entry, err := h.sessions.Resolve(id)
	if err != nil {
		return "", err
	}
	s, err := session.LoadForDir(entry.ProjectRoot, id)
	if err != nil {
		return "", err
	}
	if s.Metadata == nil {
		s.Metadata = map[string]any{}
	}
	if model == "" {
		delete(s.Metadata, "model")
	} else {
		s.Metadata["model"] = model
	}
	if err := session.SaveForDir(entry.ProjectRoot, id, s.Title, s.Messages, s.Metadata); err != nil {
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
