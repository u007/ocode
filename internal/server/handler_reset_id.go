package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/u007/ocode/internal/session"
)

// HandleResetSessionID handles POST /api/sessions/{id}/reset-id: it re-keys a
// session to a brand-new `ses_…` id, preserving the transcript, title,
// metadata and undo journal, then deletes the old id. This exists because the
// `X-Opencode-Session` (opencode / opencode-go) and `x-session-id`
// (OpenRouter) provider headers are derived from the session id itself
// (Agent.OpenCodeSessionID). Provider-side prompt caching, rate-limit buckets
// and sticky routing group by that value, so a chat whose provider grouping
// has gone stale (bad cache affinity, exhausted rate bucket, wedged sticky
// route) can get a fresh identity without losing the conversation.
//
// Lifecycle mirrors HandleTruncateSession/HandleCloseSession:
//
//  1. Resolve the session and refuse while a turn is in flight (a mid-turn
//     transcript copy would race the running step, and a queued live write
//     could resurrect the deleted old id).
//  2. Drain queued live writes for the old id, then release the resident
//     agent so no straggler write can recreate it.
//  3. Re-key on disk (session.RekeyForDir) and move the registry entry to the
//     new id. The agent itself is released (rebuilt from the rekeyed
//     transcript on the next turn) because releasing is what guarantees no
//     writer still targets the deleted id.
//  4. Publish `session_rekeyed` so every open web/desktop tab (and other
//     windows) rebinds the session id it is tracking.
func (h *Handler) HandleResetSessionID(w http.ResponseWriter, r *http.Request, id string) {
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing session id")
		return
	}
	entry, err := h.sessions.Resolve(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if h.sessions.IsTurnActive(id) {
		writeError(w, http.StatusConflict, "cannot reset session id while a turn is active")
		return
	}
	if rc := h.RCBridge(); rc != nil && rc.SessionID == id {
		// The bridged TUI owns its session id; mutating it from the server
		// side would desync the bridge (SessionID is read unlocked in ~20
		// places). Run /reset-id in the terminal instead.
		writeError(w, http.StatusConflict, "cannot reset the session id of the remote-controlled TUI session; run /reset-id in the terminal")
		return
	}

	projectRoot := entry.ProjectRoot

	// Quiesce: drain queued live snapshots, then release the resident agent so
	// no queued live write can land on the old id after the delete. A failed
	// drain is not fatal (we still hold the transcript), but log-and-continue
	// would risk the resurrection race, so refuse instead.
	if err := session.FlushForDir(projectRoot, id, 10*time.Second); err != nil {
		writeError(w, http.StatusInternalServerError, "could not drain pending writes: "+err.Error())
		return
	}
	as := h.lookupAgentSession(id)
	if as != nil && !h.sessions.ReleaseAgent(id) {
		// A resolve (permission/question) can pin the agent; refuse rather
		// than copy a transcript with an unresolved ask round.
		writeError(w, http.StatusConflict, "cannot reset session id while an ask is pending")
		return
	}
	// ReleaseAgent's onEvict hook already dropped h.agents[id], deleted
	// h.turnLocks[id], and shut the agent down, so only the registry entry
	// still needs moving.

	newID, err := session.RekeyForDir(projectRoot, id)
	if err != nil {
		if errors.Is(err, session.ErrNoStoredSession) {
			writeError(w, http.StatusNotFound, "session has no stored transcript to re-key")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to reset session id: "+err.Error())
		return
	}

	// Move the registry entry to the new id so every session-scoped lookup
	// (status, transcript, next turn) resolves under it.
	if h.sessions.Rekey(id, newID, projectRoot) == nil {
		writeError(w, http.StatusInternalServerError, "failed to rekey session registry entry")
		return
	}

	h.publishBusEvent("session_rekeyed", newID, map[string]string{
		"session_id": newID,
		"old_id":     id,
	})

	writeJSON(w, http.StatusOK, map[string]string{
		"old_id": id,
		"new_id": newID,
	})
}
