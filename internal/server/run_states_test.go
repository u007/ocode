package server

import (
	"testing"

	"github.com/u007/ocode/internal/agent"
)

func TestRunStatesEmptyWhenNoAgents(t *testing.T) {
	h := NewHandler()
	if states := h.RunStates(); len(states) != 0 {
		t.Fatalf("expected no run states, got %d", len(states))
	}
}

func TestRunStatesReportsSessionRuns(t *testing.T) {
	a := agent.NewAgent(nil, nil, nil, nil)
	running := a.Runs().New("worker")
	done := a.Runs().New("finished-worker")
	done.Status = agent.RunDone
	failed := a.Runs().New("broken-worker")
	failed.Status = agent.RunFailed
	_ = running

	h := NewHandler()
	const sessionID = "sess-1"
	h.agents[sessionID] = &agentSession{agent: a}

	states := h.RunStates()
	if len(states) != 3 {
		t.Fatalf("expected 3 run states, got %d: %+v", len(states), states)
	}
	byName := map[string]RunState{}
	for _, s := range states {
		if s.SessionID != sessionID {
			t.Fatalf("SessionID = %q, want %q", s.SessionID, sessionID)
		}
		byName[s.Name] = s
	}
	if s := byName["worker"]; s.Ended || s.Failed {
		t.Fatalf("running run misreported: %+v", s)
	}
	if s := byName["finished-worker"]; !s.Ended || s.Failed {
		t.Fatalf("done run misreported: %+v", s)
	}
	if s := byName["broken-worker"]; !s.Ended || !s.Failed {
		t.Fatalf("failed run misreported: %+v", s)
	}
}

func TestRunStatesSortsSessions(t *testing.T) {
	h := NewHandler()
	for _, id := range []string{"sess-b", "sess-a"} {
		a := agent.NewAgent(nil, nil, nil, nil)
		a.Runs().New("worker-" + id)
		h.agents[id] = &agentSession{agent: a}
	}

	states := h.RunStates()
	if len(states) != 2 {
		t.Fatalf("expected 2 run states, got %d", len(states))
	}
	if states[0].SessionID != "sess-a" || states[1].SessionID != "sess-b" {
		t.Fatalf("sessions not sorted: %+v", states)
	}
}
