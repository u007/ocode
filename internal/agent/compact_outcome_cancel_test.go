package agent

import (
	"context"
	"errors"
	"testing"
)

func TestRecordCompactOutcomeIgnoresCancel(t *testing.T) {
	a := &Agent{}
	a.recordCompactOutcome(CompactResult{Err: context.Canceled})
	if a.CompactFailed() {
		t.Fatal("cancelled compaction must not latch auto-compaction off")
	}
	a.recordCompactOutcome(CompactResult{Err: errors.New("provider 500")})
	if !a.CompactFailed() {
		t.Fatal("real compaction error must latch")
	}
}
