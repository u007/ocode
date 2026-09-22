package server

import (
	"net/http"
	"strings"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
)

// HandleRetrySession handles POST /api/sessions/{id}/retry: it re-runs the
// last turn from the session's existing transcript tail WITHOUT appending a
// new user message.
//
// This backs the composer's retry action after a user Stop or an LLM-loop
// error. Re-sending the text through the normal message endpoint would append
// a second user row (see runTurn), duplicating the message; a retry instead
// steps the agent over the rows already in the transcript — the same semantics
// as the TUI's Ctrl+Y retry (model.retryLastLLMError).
//
// Returns 409 when a turn is already active (or the agent is mid-turn / parked
// on an ask) or when the transcript has no user message to re-run. Remote
// (SSH/WSL) sessions reach this through the same /api/remote/{host} reverse
// proxy as every other session-scoped call.
func (h *Handler) HandleRetrySession(w http.ResponseWriter, r *http.Request, id string) {
	entry, err := h.sessions.Resolve(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if h.sessions.IsTurnActive(id) {
		writeError(w, http.StatusConflict, "turn already active")
		return
	}
	// The bridged TUI owns its session (see HandleResetSessionID): a server-side
	// retry would run a second agent against the same transcript.
	if rc := h.RCBridge(); rc != nil && rc.SessionID == id {
		writeError(w, http.StatusConflict, "cannot retry the remote-controlled TUI session; retry in the terminal")
		return
	}
	// The transcript must contain a user turn to re-run; stepping the agent on
	// an empty transcript would send the model no prompt. Prefer the resident
	// agent's in-memory tail (authoritative), falling back to disk when no
	// agent is resident. A held agent lock means a turn/ask is in flight, so
	// refuse rather than block behind it.
	resident := h.lookupAgentSession(id)
	var msgs []agent.Message
	if resident != nil {
		if !resident.mu.TryLock() {
			writeError(w, http.StatusConflict, "turn already active")
			return
		}
		msgs = append([]agent.Message(nil), resident.messages...)
		resident.mu.Unlock()
	} else {
		s, loadErr := session.LoadForDir(entry.ProjectRoot, id)
		if loadErr != nil {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		msgs = s.Messages
	}
	if !hasUserMessage(msgs) {
		writeError(w, http.StatusConflict, "nothing to retry")
		return
	}

	// Per-window profile binding, mirroring HandleSendMessage: a proxied remote
	// retry must run on the desktop window's active profile, and the resident
	// agent is reconciled to it before the turn.
	windowID := r.Header.Get("X-Window-Id")
	if windowID == "" {
		windowID = r.URL.Query().Get("windowId")
	}
	if windowID = strings.TrimSpace(windowID); windowID != "" {
		h.sessions.SetWindowID(id, windowID)
	}
	h.applyProxiedActiveProfile(r, windowID)

	// Async: nothing to persist for a retry (the user row is already durable),
	// so persistAck closes as soon as the job starts; the turn runs on the
	// per-session goroutine with events streamed over the unified bus.
	model := h.effectiveSessionModel(id)
	job, err := h.dispatchTurn(id, model, "", turnOptions{retryLast: true})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	select {
	case <-job.persistAck:
		if job.err != nil {
			writeError(w, http.StatusInternalServerError, job.err.Error())
			return
		}
	case <-r.Context().Done():
		return
	}
	writeJSON(w, http.StatusAccepted, ChatResponse{SessionID: id, Model: model})
}

// hasUserMessage reports whether a transcript contains at least one user row —
// the precondition for a retry re-running the last turn in place.
func hasUserMessage(msgs []agent.Message) bool {
	for _, m := range msgs {
		if m.Role == "user" {
			return true
		}
	}
	return false
}
