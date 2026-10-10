package server

import (
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
)

// The Settings save handler calls this synchronously; a running turn holds
// as.mu for its whole duration, so it must never take that lock.
func TestApplyLimitsToLiveSessionsDoesNotBlockOnRunningTurn(t *testing.T) {
	h := NewHandler()
	ag := agent.NewAgent(nil, nil, nil, nil)
	busy := &agentSession{agent: ag}
	busy.mu.Lock()
	defer busy.mu.Unlock()
	h.agents["busy"] = busy

	done := make(chan struct{})
	go func() {
		h.applyLimitsToLiveSessions(42, 3)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("applyLimitsToLiveSessions blocked on a session whose turn holds as.mu")
	}
	if got := ag.GetMaxSteps(); got != 42 {
		t.Fatalf("GetMaxSteps = %d, want 42", got)
	}
}
