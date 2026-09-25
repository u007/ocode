package server

import "log"

// dispatchAskContinuation runs an answered-ask continuation — the agent re-Step
// triggered by answering a `question` prompt or resolving a PERMISSION_ASK — on
// its own goroutine, so the resolve/answer HTTP handlers can acknowledge with a
// 202 and release the connection immediately instead of holding it open for the
// whole round.
//
// WHY THIS EXISTS: `HandleAnswerQuestion` and `HandleResolvePermission` used to
// run `as.agent.Step` (and, for permissions, the approved tool itself) inline on
// the request goroutine. Step can run for minutes, so the POST stayed pending
// for minutes: the browser's `await` never settled, the dialog's local
// answer echo + dismissal were gated on it, and the request held one of the
// ~6 connections per origin the whole time (the exact starvation the async send
// path was fixed for — see web/src/api/client.ts:1433 `async: true` and
// web/src/hooks/useChat.ts:162). The TUI never had this problem: resolving a
// dialog just resumes its single in-process loop.
//
// LOCK OWNERSHIP: the caller MUST hold as.mu (findPendingSession returns it
// locked) and MUST NOT unlock it on the success path. This function transfers
// the lock to the callback goroutine, which unlocks it when fn returns. A
// caller whose validation can fail after findPendingSession must unlock
// explicitly on those error paths (there is no handler-level defer anymore).
//
// Shutdown admission mirrors dispatchTurn: once shutdown has begun the
// continuation runs INLINE on the caller's goroutine (as.mu unlocked before
// returning) rather than spawning an unregistered goroutine that could write
// after the join.
func (h *Handler) dispatchAskContinuation(sessionID string, as *agentSession, fn func()) {
	h.shutdownMu.Lock()
	if h.shutdownStarted {
		h.shutdownMu.Unlock()
		defer as.mu.Unlock()
		fn()
		return
	}
	h.turnJobsWG.Add(1)
	h.shutdownMu.Unlock()

	// Register BEFORE the goroutine starts, exactly like dispatchTurn, so a
	// Stop that races the dispatch still sees the session as in-flight (and so
	// shutdownAgentSessions interrupts it).
	h.cancelMu.Lock()
	if h.turnInFlight == nil {
		h.turnInFlight = make(map[string]int)
	}
	h.turnInFlight[sessionID]++
	h.cancelMu.Unlock()

	go func() {
		// Defer order (LIFO after fn): unlock as.mu, recover, drop the
		// in-flight count, release the shutdown join.
		defer h.turnJobsWG.Done()
		defer func() {
			h.cancelMu.Lock()
			if n := h.turnInFlight[sessionID] - 1; n <= 0 {
				delete(h.turnInFlight, sessionID)
			} else {
				h.turnInFlight[sessionID] = n
			}
			h.cancelMu.Unlock()
		}()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("serve error: ask continuation panic for %s: %v", sessionID, r)
			}
		}()
		defer as.mu.Unlock()
		fn()
	}()
}
