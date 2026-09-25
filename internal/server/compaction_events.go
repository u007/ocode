package server

import "time"

// compactionStartedEvent is the session-scoped signal that a compaction pass
// has begun. started_at is shared by overlapping passes: the session manager
// keeps the first active timestamp so clients do not reset their elapsed timer
// when another pass joins the group.
type compactionStartedEvent struct {
	StartedAt  string `json:"started_at"`
	Generation uint64 `json:"generation,omitzero"`
}

// compactionDoneEvent is emitted only when the last overlapping pass finishes.
// ok is false only for a real summarization failure; disabled/no-op compaction
// completes without an error banner.
type compactionDoneEvent struct {
	OK         bool   `json:"ok"`
	Error      string `json:"error,omitempty"`
	Generation uint64 `json:"generation,omitzero"`
}

// beginCompaction records and publishes the start of one pass. The manager is
// updated before the event so a client that reacts immediately can reconcile
// against an already-active /state snapshot.
func (h *Handler) beginCompaction(sessionID string) bool {
	ok, startedAt, generation := h.sessions.BeginCompaction(sessionID)
	if !ok {
		// A few legacy/internal callers install an agentSession directly in
		// h.agents without first registering its lifecycle row. Preserve that
		// supported path while still giving the operation authoritative state.
		h.sessions.Register(sessionID, "")
		ok, startedAt, generation = h.sessions.BeginCompaction(sessionID)
		if !ok {
			return false
		}
	}
	startedAtText := ""
	if !startedAt.IsZero() {
		startedAtText = startedAt.UTC().Format(time.RFC3339Nano)
	}
	h.broadcastEvent(SSEEvent{
		SessionID: sessionID,
		Event:     "compaction_started",
		Data:      compactionStartedEvent{StartedAt: startedAtText, Generation: generation},
	})
	return true
}

// finishCompaction retires one pass and publishes the terminal event only
// after the overlap count reaches zero. aggregateErr is the first real error
// observed in the group, so a later successful pass cannot erase an earlier
// failure. Call this after releasing as.mu: compaction_done is critical and
// may wait for a slow bus subscriber.
func (h *Handler) finishCompaction(sessionID, compactionErr string) {
	idle, wasActive, aggregateErr, generation := h.sessions.EndCompaction(sessionID, compactionErr)
	if !wasActive || !idle {
		return
	}
	h.broadcastEvent(SSEEvent{
		SessionID: sessionID,
		Event:     "compaction_done",
		Data: compactionDoneEvent{
			OK:         aggregateErr == "",
			Error:      aggregateErr,
			Generation: generation,
		},
	})
}
