package server

import (
	"log"
	"sync"

	"github.com/u007/ocode/internal/session"
)

// sessionInterrupted reports whether the session has SETTLED on an unfinished
// turn — the server half of the chat's "The previous reply was interrupted"
// notice. It is true only when all of these hold:
//
//   - no active turn;
//   - no turn job in flight — the per-session turn lock can be taken.
//     executeTurnJob holds that lock for its whole duration (persist →
//     bootstrap → turn), so it is the probe that covers the pre-turnActive
//     window, where the user row is already stored but no agent is resident;
//   - not parked on an ask — the per-session agent lock can be taken, or no
//     resident agent exists. The lock is what the permission/question-answer
//     continuation holds across the answer persist;
//   - the last transcript row is unfinished (see
//     session.TranscriptTailUnfinished) — an answered ask, a user row, a
//     tool_calls-only assistant. A landed reply or an unanswered ask is not an
//     interruption.
//
// Every probe is non-blocking: this runs inside the /state poll that every
// open tab hits, so a blocking lock would pin an HTTP connection behind a turn
// (the same reason livePendingAsks uses TryLock). It is deliberately
// FAIL-OPEN — an absent store, a legacy/undecodable format, or a lock we could
// not inspect reports false. Never invent an interruption.
//
// A session with a message left pending by a failed bootstrap reads as
// interrupted on purpose: the pending queue does not retry on its own, so the
// failed message sits until the next turn. Continue is exactly that next turn —
// executeTurnJob drains the pending message first, then the Continue — so the
// retry cannot double-queue ahead of it.
func (h *Handler) sessionInterrupted(id, projectRoot string) bool {
	if projectRoot == "" {
		// Bridged / in-memory session: no stored transcript to classify.
		return false
	}
	if h.sessions.IsTurnActive(id) {
		return false
	}

	as := h.lookupAgentSession(id)
	if as != nil {
		// A resident agent owns the authoritative tail (free, and more
		// authoritative than disk). TryLock distinguishes "idle at a stable
		// tail" from "mid-turn or parked on an ask".
		if !as.mu.TryLock() {
			return false
		}
		unfinished := session.TranscriptTailUnfinished(as.messages)
		as.mu.Unlock()
		// The agent lock is free, but a job with an existing agent persists a
		// fresh user row before runTurn re-takes the lock and refreshes
		// as.messages. If a job holds the turn lock, the session is not settled.
		if h.sessionTurnInFlight(id) {
			return false
		}
		return unfinished
	}

	// No resident agent (post-restart / idle-evicted — the reported case): the
	// stored transcript is the only source. Hold the turn lock across the read
	// so a job cannot persist a user row between the probe and the read. A nil
	// lock means no job has ever created one for this session — the map entry
	// is created by executeTurnJob, never by this read.
	lock := h.sessionTurnLockIfExists(id)
	if lock != nil {
		if !lock.TryLock() {
			return false
		}
		defer lock.Unlock()
	}
	_, tail, err := session.StoredTranscriptStateForDir(projectRoot, id)
	if err != nil {
		// Unreadable store: fail open rather than guess (logged, not swallowed).
		log.Printf("serve: session %s interrupted: stored transcript state: %v", id, err)
		return false
	}
	return session.TranscriptTailUnfinished(tail)
}

// sessionTurnLockIfExists returns the per-session turn lock WITHOUT creating
// it, so a read-only probe never leaves a map entry behind. nil means no turn
// job has ever run for the session.
func (h *Handler) sessionTurnLockIfExists(id string) *sync.Mutex {
	h.turnMu.Lock()
	defer h.turnMu.Unlock()
	return h.turnLocks[id]
}

// sessionTurnInFlight reports whether a turn job currently holds id's turn
// lock. Non-blocking (TryLock) and non-creating, so it is safe on the /state
// poll path.
func (h *Handler) sessionTurnInFlight(id string) bool {
	lock := h.sessionTurnLockIfExists(id)
	if lock == nil {
		return false
	}
	if !lock.TryLock() {
		return true
	}
	lock.Unlock()
	return false
}
