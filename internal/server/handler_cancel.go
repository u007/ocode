package server

import (
	"net/http"
)

// HandleCancelSession handles POST /api/sessions/{id}/cancel: interrupts a
// running turn for the session (and any background sub-agent runs, mirroring
// the TUI's Escape key). It is safe to call when no turn is active — that
// case is a no-op.
//
// The cancellation propagates via Agent.Cancel() (closes the stop channel and
// cancels in-flight LLM streaming) plus Runs().CancelAll() (terminates
// background sub-agent runs for this session, matching TUI Escape's
// agent.Cancel() + Runs().CancelAll() pair).
//
// Stop is intentionally distinct from close (HandleCloseSession): Stop only
// interrupts in-flight work and keeps the resident agent live so Resume can
// reuse it; Close additionally releases the agent.
func (h *Handler) HandleCancelSession(w http.ResponseWriter, r *http.Request, id string) {
	if _, err := h.sessions.Resolve(id); err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	h.interruptSessionWork(id)
	writeJSON(w, http.StatusOK, map[string]any{"cancelled": true})
}

// interruptSessionWork cancels in-flight work for a session: records a
// pendingCancel flag (when the session actually has work to cancel — an
// active turn, or a dispatched-but-not-yet-finished turn job) and cancels the
// agent + background sub-agent runs.
//
// pendingCancel is only recorded when a live job or turn can observe it.
// Recording it otherwise would poison the NEXT turn — executeTurnJob
// consumes pendingCancel at start and would publish a cancelled turn_error
// without running the user's message. In particular PendingCount alone is
// NOT a signal to record: a failed bootstrap can leave persisted-but-unturned
// messages while the session is idle, and a flag set then would swallow the
// first retry. A stale flag left by an earlier cancel is cleared here so
// fire-and-forget cancels (e.g. closing a session tab) are harmless.
func (h *Handler) interruptSessionWork(id string) {
	// Decide whether the session has in-flight work. The dispatch-in-flight
	// marker (turnInFlight) is set by dispatchTurn BEFORE the job goroutine
	// starts, so a cancel that arrives between dispatch and the job's first
	// pendingCancel check is still honored, and a queued second job keeps the
	// session visible as in-flight while the first drains. IsTurnActive comes
	// from the session manager; read it before taking cancelMu to avoid lock
	// inversion (sessions uses its own mutex).
	active := h.sessions.IsTurnActive(id)
	compacting := h.sessions.IsCompacting(id)
	h.cancelMu.Lock()
	inFlight := h.turnInFlight[id] > 0
	if !active && !inFlight {
		// No turn to cancel. Always clear any stale pendingCancel flag so it
		// cannot poison the next turn. A compaction pass is NOT a turn: it has
		// no executeTurnJob waiting to consume pendingCancel, so recording the
		// flag for compaction-only work would swallow the next user message
		// with a cancelled turn_error.
		delete(h.pendingCancel, id)
		h.cancelMu.Unlock()
		if compacting {
			if as := h.lookupAgentSession(id); as != nil && as.agent != nil {
				as.agent.CancelCompaction()
			}
		}
		return
	}
	h.pendingCancel[id] = true
	h.cancelMu.Unlock()

	as := h.lookupAgentSession(id)
	if as == nil || as.agent == nil {
		// No active agent yet (bootstrap in flight or not started) — the flag
		// set above is observed by executeTurnJob's pendingCancel checks and
		// aborts the bootstrap/turn.
		return
	}
	as.agent.Cancel()
	// Stop must also interrupt an in-flight compaction: Agent.Cancel only
	// closes the Step loop's stop channel, which runCompact does not observe.
	as.agent.CancelCompaction()
	if as.agent.Runs() != nil {
		as.agent.Runs().CancelAll()
	}
}

// HandleCancelCompaction handles POST /api/sessions/{id}/compact/cancel:
// interrupts every in-flight compaction pass (manual or automatic) for the
// session. Idempotent — cancelling with nothing in flight returns
// cancelled:false rather than an error, so a stale or double-clicked Cancel
// button cannot surface as a failure. The running pass publishes the terminal
// compaction_done frame itself, so this handler does no lifecycle bookkeeping.
func (h *Handler) HandleCancelCompaction(w http.ResponseWriter, r *http.Request, id string) {
	if _, err := h.sessions.Resolve(id); err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	cancelled := false
	if as := h.lookupAgentSession(id); as != nil && as.agent != nil {
		cancelled = as.agent.CancelCompaction()
	}
	writeJSON(w, http.StatusOK, map[string]any{"cancelled": cancelled})
}

func (h *Handler) consumePendingCancel(id string) bool {
	h.cancelMu.Lock()
	defer h.cancelMu.Unlock()
	if h.pendingCancel[id] {
		delete(h.pendingCancel, id)
		return true
	}
	return false
}

func (h *Handler) isPendingCancel(id string) bool {
	h.cancelMu.Lock()
	defer h.cancelMu.Unlock()
	return h.pendingCancel[id]
}
