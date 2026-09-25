package server

import (
	"testing"

	"github.com/u007/ocode/internal/session"
)

// TestBootstrapStageTransitions covers Part 03 Task 1: the registry entry
// advances idle → tools → mcp → model → ready, terminal ready clears a prior
// failure, and a backwards jump is error-logged but still recorded (the stage
// is diagnostic state, never a gate). The State snapshot reflects everything.
func TestBootstrapStageTransitions(t *testing.T) {
	mgr := NewSessionManager(defaultSessionIdleTimeout, func() []string { return nil }, nil)
	id := session.NewSessionID()
	mgr.Register(id, t.TempDir())

	state, ok := mgr.State(id)
	if !ok {
		t.Fatal("no state for registered session")
	}
	if state.BootstrapStage != "" || state.TurnActive {
		t.Fatalf("fresh entry state = %+v, want idle/inactive", state)
	}

	// Same order buildAgentSession emits: model → tools → mcp → ready.
	mgr.SetBootstrapStage(id, "model")
	mgr.SetBootstrapStage(id, "tools")
	mgr.SetBootstrapStage(id, "mcp")
	mgr.SetBootstrapStage(id, "ready")
	state, _ = mgr.State(id)
	if state.BootstrapStage != "ready" {
		t.Fatalf("after full bootstrap stage = %q, want ready", state.BootstrapStage)
	}

	// A successful bootstrap clears a prior failure.
	mgr.SetBootstrapFailed(id, "mcp")
	mgr.SetBootstrapError(id, "boom")
	state, _ = mgr.State(id)
	if state.BootstrapStage != "mcp" || state.BootstrapError != "boom" {
		t.Fatalf("failed state = %+v, want stage mcp + error boom", state)
	}
	mgr.SetBootstrapStage(id, "ready")
	state, _ = mgr.State(id)
	if state.BootstrapError != "" {
		t.Fatalf("ready did not clear bootstrap error: %+v", state)
	}

	// Turn-active flag flips and is reported.
	mgr.setTurnActive(id, true)
	state, _ = mgr.State(id)
	if !state.TurnActive {
		t.Fatal("turn_active should be true after setTurnActive(true)")
	}
	mgr.setTurnActive(id, false)
	state, _ = mgr.State(id)
	if state.TurnActive {
		t.Fatal("turn_active should be false after setTurnActive(false)")
	}
}

// TestCompactionLifecycleTracksOverlappingOperations keeps the session-level
// compaction state honest when a manual pass and an automatic pass overlap.
// A single boolean would let the first completion hide the still-running
// operation from /state and from clients reconciling after a missed event.
func TestCompactionLifecycleTracksOverlappingOperations(t *testing.T) {
	mgr := NewSessionManager(defaultSessionIdleTimeout, func() []string { return nil }, nil)
	id := session.NewSessionID()
	mgr.Register(id, t.TempDir())

	ok, startedAt, generation := mgr.BeginCompaction(id)
	if !ok {
		t.Fatal("BeginCompaction rejected a registered session")
	}
	ok, overlappingStartedAt, overlappingGeneration := mgr.BeginCompaction(id)
	if !ok {
		t.Fatal("second BeginCompaction rejected an already active session")
	}
	if overlappingGeneration != generation || !overlappingStartedAt.Equal(startedAt) {
		t.Fatalf("overlapping BeginCompaction metadata = (%s, %d), want shared (%s, %d)", overlappingStartedAt, overlappingGeneration, startedAt, generation)
	}
	state, ok := mgr.State(id)
	if !ok {
		t.Fatal("session not found")
	}
	if !state.Compacting || state.CompactionStartedAt.IsZero() {
		t.Fatalf("overlapping compaction state = %+v, want active with a start time", state)
	}

	idle, wasActive, aggregateErr, generation := mgr.EndCompaction(id, "first pass failed")
	if idle || !wasActive || aggregateErr != "" || generation == 0 {
		t.Fatalf("first EndCompaction = (idle=%v, wasActive=%v, aggregateErr=%q, generation=%d), want another operation still active", idle, wasActive, aggregateErr, generation)
	}
	state, _ = mgr.State(id)
	if !state.Compacting {
		t.Fatalf("state after first completion = %+v, want still compacting", state)
	}

	idle, wasActive, aggregateErr, generation = mgr.EndCompaction(id, "")
	if !idle || !wasActive || aggregateErr != "first pass failed" || generation == 0 {
		t.Fatalf("final EndCompaction = (idle=%v, wasActive=%v, aggregateErr=%q, generation=%d), want idle with first error retained", idle, wasActive, aggregateErr, generation)
	}
	state, _ = mgr.State(id)
	if state.Compacting || !state.CompactionStartedAt.IsZero() {
		t.Fatalf("final compaction state = %+v, want inactive with no start time", state)
	}
}

// TestPendingMessageLifecycle covers the persist-then-202 bookkeeping: pending
// messages are pushed in order, exposed front-to-back, shifted after a turn,
// and the pending count drives the bootstrap strip length.
func TestPendingMessageLifecycle(t *testing.T) {
	mgr := NewSessionManager(defaultSessionIdleTimeout, func() []string { return nil }, nil)
	id := session.NewSessionID()
	mgr.Register(id, t.TempDir())

	if mgr.PendingCount(id) != 0 {
		t.Fatalf("fresh pending count = %d, want 0", mgr.PendingCount(id))
	}
	mgr.PushPending(id, "m1")
	mgr.PushPending(id, "m2")
	if n := mgr.PendingCount(id); n != 2 {
		t.Fatalf("pending count = %d, want 2", n)
	}
	if front, ok := mgr.PendingFront(id); !ok || front != "m1" {
		t.Fatalf("pending front = %q (%v), want m1", front, ok)
	}
	mgr.ShiftPending(id)
	if front, _ := mgr.PendingFront(id); front != "m2" {
		t.Fatalf("pending front after shift = %q, want m2", front)
	}
	mgr.ShiftPending(id)
	if _, ok := mgr.PendingFront(id); ok {
		t.Fatal("pending should be empty after two shifts")
	}
}

// TestSessionStartMarkerSurvivesUntilFirstTurn covers the session_started
// correlation: the marker set at session creation is consumed exactly once by
// the first turn, even when a bootstrap failure delays that turn.
func TestSessionStartMarkerSurvivesUntilFirstTurn(t *testing.T) {
	mgr := NewSessionManager(defaultSessionIdleTimeout, func() []string { return nil }, nil)
	id := session.NewSessionID()
	mgr.Register(id, t.TempDir())

	mgr.SetSessionStart(id, "new-abc")
	if rid, ok := mgr.ConsumeSessionStart(id); !ok || rid != "new-abc" {
		t.Fatalf("first consume = %q (%v), want new-abc", rid, ok)
	}
	if _, ok := mgr.ConsumeSessionStart(id); ok {
		t.Fatal("second consume should be empty (marker consumed once)")
	}
}
