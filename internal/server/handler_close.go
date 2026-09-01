package server

import (
	"net/http"
)

// HandleCloseSession handles POST /api/sessions/{id}/close: terminates the
// backend for a session the user has closed (closed a session tab in the
// web/desktop UI). It is the headless-server counterpart of the TUI's session
// teardown / /new:
//
//  1. Cancel any in-flight turn and background sub-agent runs (same as Stop).
//  2. Release the resident agent (Agent.Shutdown + removal from the registry)
//     so maintenance workers, streaming deltas and the snapshot store don't
//     linger for a session nobody is viewing. The registry entry and on-disk
//     transcript remain — the session can be reopened and rebuilds its agent
//     from history on the next message.
//
// The stop/close distinction: Stop (HandleCancelSession) only interrupts
// in-flight work and keeps the resident agent live so Resume can reuse it;
// Close additionally tears the agent down. Both are safe to call on an idle
// session (no-op / cancelled=true).
//
// When the agent cannot be released immediately — a turn is still in flight,
// or the agent is mid-bootstrap and not registered yet — the session is
// marked close-pending and executeTurnJob releases it the moment the turn (or
// bootstrap) unwinds, so close never waits for the idle-eviction loop.
func (h *Handler) HandleCloseSession(w http.ResponseWriter, r *http.Request, id string) {
	if _, err := h.sessions.Resolve(id); err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	// 1. Interrupt in-flight work exactly like Stop (pendingCancel flag +
	// agent.Cancel + sub-agent CancelAll).
	h.interruptSessionWork(id)

	// 2. Release the resident agent now if possible. The agent must go away
	// even when a turn is in flight or bootstrap is still registering it, so
	// if this release cannot run, mark the session close-pending: the turn
	// job that owns the agent will drain the marker after its final step and
	// perform the release then.
	if !h.sessions.ReleaseAgent(id) {
		h.cancelMu.Lock()
		if h.closePending == nil {
			h.closePending = make(map[string]bool)
		}
		h.closePending[id] = true
		h.cancelMu.Unlock()
	}

	writeJSON(w, http.StatusOK, map[string]any{"cancelled": true})
}

// drainPendingClose releases a close-pending session's agent. executeTurnJob
// and the synchronous turn/resolve paths call it after the turn (or
// continuation) has unwound, so an agent that was busy (turn in flight) or
// not yet registered (bootstrap) when the close arrived is torn down as soon
// as it is no longer owned. Safe to call for any session: it only acts when
// the marker is set and the agent is releasable.
//
// The marker stays set when the agent is still busy (a release raced a turn
// start and returned false), so the next job drain — or the idle-eviction
// sweep — finishes the teardown rather than orphaning it. It is cleared when
// the release succeeds or there is nothing left to release.
func (h *Handler) drainPendingClose(id string) {
	h.cancelMu.Lock()
	pending := h.closePending[id]
	h.cancelMu.Unlock()
	if !pending {
		return
	}
	if h.sessions.ReleaseAgent(id) {
		h.cancelMu.Lock()
		delete(h.closePending, id)
		h.cancelMu.Unlock()
		return
	}
	// ReleaseAgent refused: either a turn is still active (leave the marker
	// for the next drain/eviction pass) or the entry has no agent at all
	// (nothing to release — clear the marker so it does not linger).
	if h.lookupAgentSession(id) == nil {
		h.cancelMu.Lock()
		delete(h.closePending, id)
		h.cancelMu.Unlock()
	}
}
