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

	// Context usage from the session's transcript: estimated tokens from
	// message characters, max tokens from the model window. Prefer the live
	// bridged model name (the running model) when this session is the TUI's.
	var totalChars int
	if s, err := session.LoadForDir(entry.ProjectRoot, id); err == nil {
		for _, msg := range s.Messages {
			totalChars += len(msg.Content) + len(msg.ReasoningContent)
			for _, tc := range msg.ToolCalls {
				totalChars += len(tc.Function.Arguments)
			}
		}
	}
	model := ""
	maxTokens := 0
	if rc := h.RCBridge(); rc != nil && id == rc.SessionID {
		if live := rc.TUIStatus(); live.ContextModel != "" {
			model = live.ContextModel
			maxTokens = live.ContextMaxTokens
		}
	}
	if model == "" && h.cfg != nil {
		model = h.cfg.Model
	}
	if maxTokens == 0 {
		maxTokens = int(agent.ModelWindow(model))
	}
	snap.ContextCurrentTokens = totalChars / 4
	snap.ContextMaxTokens = maxTokens
	snap.ContextModel = model
	snap.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)

	writeJSON(w, http.StatusOK, snap)
}
