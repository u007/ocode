package server

import (
	"errors"
	"testing"
)

// A synchronous turn counted by beginSyncTurn must make a pulse (refuse-if-busy)
// dispatch fail rather than start behind it.
func TestPulseDispatchRefusedBehindSyncTurn(t *testing.T) {
	h := NewHandler()
	const id = "ses_sync"

	release := h.beginSyncTurn(id)
	_, err := h.dispatchTurn(id, "", "hello", turnOptions{refuseIfBusy: true})
	if !errors.Is(err, errTurnInFlight) {
		t.Fatalf("pulse dispatch behind a sync turn: err = %v, want errTurnInFlight", err)
	}
	release()

	job, err := h.dispatchTurn(id, "", "hello", turnOptions{refuseIfBusy: true})
	if err != nil {
		t.Fatalf("pulse dispatch after the sync turn ended: %v", err)
	}
	<-job.persistAck
}

// Releasing the last synchronous turn must drop a Stop flag that nothing else
// would consume, so the next message is not cancelled by it.
func TestSyncTurnReleaseClearsStaleCancel(t *testing.T) {
	h := NewHandler()
	const id = "ses_stale"

	release := h.beginSyncTurn(id)
	h.cancelMu.Lock()
	h.pendingCancel[id] = true
	h.cancelMu.Unlock()
	release()

	h.cancelMu.Lock()
	defer h.cancelMu.Unlock()
	if h.pendingCancel[id] {
		t.Fatal("pendingCancel survived the end of a sync turn; the next turn would be cancelled")
	}
	if h.turnInFlight[id] != 0 {
		t.Fatalf("turnInFlight[%s] = %d after release, want 0", id, h.turnInFlight[id])
	}
}
