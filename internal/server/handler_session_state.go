package server

import (
	"net/http"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
)

// HandleSessionState is the reconcile endpoint (Part 03). The frontend derives
// streaming state from it — bootstrap_stage and turn_active — and uses
// last_seq to detect events it may have missed during a reconnect. Reconcile
// is state fetch + transcript refetch, never event replay.
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
	snap.SessionID = id
	if entry.ProjectRoot != "" {
		snap.CWD = entry.ProjectRoot
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
		model = h.cfg.Model
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
	snap.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	h.broadcastEvent(SSEEvent{SessionID: sessionID, Event: "status", Data: snap})
}
