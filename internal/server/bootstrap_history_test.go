package server

import (
	"testing"

	"github.com/u007/ocode/internal/agent"
)

// The bootstrap strip is the ONLY place a freshly built agent can end up with
// fewer messages than the stored transcript. A resident agent shorter than disk
// is unrecoverable in place: every live snapshot is dropped ("stored rows are
// not a prefix of the snapshot") and every turn-end save conflicts, so each
// turn's output is discarded and the session settles forever on an unanswered
// user row — which the /state endpoint then reports as `interrupted`.
//
// Stripping by len(pending) alone is unsafe: the pending queue is in-memory and
// a cancel deliberately RETAINS its message "for retry after Resume", so it can
// hold entries with no matching trailing row on disk. These tests pin that only
// genuinely-pending TRAILING rows are withheld.
func TestPendingStripLen(t *testing.T) {
	history := []agent.Message{
		{Role: "user", Content: "one"},
		{Role: "assistant", Content: "two"},
		{Role: "user", Content: "three"},
	}

	t.Run("stale pending entries strip nothing", func(t *testing.T) {
		// Pending contents that are NOT the trailing stored rows. The old
		// count-based strip removed 2 settled messages and left the agent
		// holding 1 of 3 — the wedge.
		if n := pendingStripLen(history, []string{"ghost-a", "ghost-b"}); n != 0 {
			t.Fatalf("pendingStripLen = %d, want 0: pending entries with no matching trailing row must not strip settled history", n)
		}
	})

	t.Run("matching trailing pending row is stripped", func(t *testing.T) {
		if n := pendingStripLen(history, []string{"three"}); n != 1 {
			t.Fatalf("pendingStripLen = %d, want 1", n)
		}
	})

	t.Run("non-trailing pending row strips nothing", func(t *testing.T) {
		// "two" is not the tail, so nothing may be withheld.
		if n := pendingStripLen(history, []string{"two"}); n != 0 {
			t.Fatalf("pendingStripLen = %d, want 0", n)
		}
	})

	t.Run("contiguous matching suffix is stripped", func(t *testing.T) {
		// Two user rows typed back to back, neither turned yet. Oldest-first
		// pending order, so the last entry is the tail row.
		twoPending := []agent.Message{
			{Role: "user", Content: "one"},
			{Role: "assistant", Content: "reply"},
			{Role: "user", Content: "two"},
			{Role: "user", Content: "three"},
		}
		if n := pendingStripLen(twoPending, []string{"two", "three"}); n != 2 {
			t.Fatalf("pendingStripLen = %d, want 2", n)
		}
	})

	t.Run("mismatch stops the strip", func(t *testing.T) {
		// "three" matches the tail but "ghost" does not match the row before
		// it, so only the contiguous tail is withheld.
		if n := pendingStripLen(history, []string{"ghost", "three"}); n != 1 {
			t.Fatalf("pendingStripLen = %d, want 1", n)
		}
	})

	t.Run("inflated stale queue strips only the trailing unturned rows", func(t *testing.T) {
		// ses_2026-09-29-144236-b0b55a6a: a long settled transcript ending in
		// four typed-but-unturned "continue" rows, while repeated cancels had
		// left the in-memory queue holding 40 "continue" entries. The old
		// count-based strip removed 40 rows (all the settled tail), leaving the
		// agent far shorter than disk so every save was rejected.
		long := make([]agent.Message, 0, 300)
		for i := 0; i < 148; i++ {
			long = append(long,
				agent.Message{Role: "assistant", Content: "reply"},
				agent.Message{Role: "user", Content: "step"},
			)
		}
		long = append(long, agent.Message{Role: "assistant", Content: "done"})
		for i := 0; i < 4; i++ {
			long = append(long, agent.Message{Role: "user", Content: "continue"})
		}
		stale := make([]string, 40)
		for i := range stale {
			stale[i] = "continue"
		}
		n := pendingStripLen(long, stale)
		if n != 4 {
			t.Fatalf("pendingStripLen = %d, want 4 (only the trailing unturned rows)", n)
		}
		if got := len(long) - n; got != len(long)-4 {
			t.Fatalf("resident history = %d msgs, want %d", got, len(long)-4)
		}
	})

	t.Run("never strips more than the history", func(t *testing.T) {
		if n := pendingStripLen(history, []string{"one", "two", "three", "one", "two", "three"}); n > len(history) {
			t.Fatalf("pendingStripLen = %d, must never exceed history length %d", n, len(history))
		}
	})
}

// A structural conflict at turn end (stored rows are not a prefix of ours)
// leaves the resident agent holding a view that can never save again. Syncing
// memory to disk is not enough: the next turn re-derives its base from the
// resident agent, so the session must be scheduled for release and re-bootstrap
// from disk.
func TestTurnEndConflictSchedulesAgentRelease(t *testing.T) {
	proj := t.TempDir()
	const id = "ses-conflict-release"

	stored := []agent.Message{
		{Role: "user", Content: "one"},
		{Role: "assistant", Content: "two"},
		{Role: "user", Content: "three"},
	}
	seedSessionForHandler(t, proj, id, stored)

	h := NewHandler()
	h.SetWorkDir(proj)
	h.sessions.Register(id, proj)

	// A resident view that DIVERGES in content from disk at the same position
	// (e.g. a compaction result that was never persisted): same length,
	// different row 1. This is the exact shape that yields ErrTranscriptConflict.
	divergent := []agent.Message{
		{Role: "user", Content: "one"},
		{Role: "assistant", Content: "SUMMARY-INSTEAD"},
		{Role: "user", Content: "three"},
	}
	as := &agentSession{messages: divergent}

	// baseLen is the turn's base length, as runTurn passes it: everything
	// before it is already durable, so the save compares the base rows
	// against what is stored and conflicts on the divergence.
	h.persistTurnTranscript(id, as, len(divergent), "turn-end")

	h.cancelMu.Lock()
	released := h.closePending[id]
	h.cancelMu.Unlock()
	if !released {
		t.Fatal("turn-end conflict did not schedule the agent for release; the resident view can never save again and the session stays wedged as interrupted")
	}
}
