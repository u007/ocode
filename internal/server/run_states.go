package server

import (
	"sort"

	"github.com/u007/ocode/internal/agent"
)

// RunState is a minimal cross-package view of one top-level agent run.
// The desktop shell polls this for dock-badge counts and finished-run
// notifications; it deliberately excludes transcripts (see agentRunDTO for
// the full web-facing shape).
type RunState struct {
	SessionID string // "" for the /rc-bridged TUI agent
	ID        string
	Name      string
	Ended     bool
	Failed    bool
}

// RunStates returns one entry per top-level run across the /rc agent (if
// any) and every per-session server agent, in registry (chronological)
// order per agent. Session keys iterate sorted so output is stable.
// Unpaginated: bounded small set (active agents only).
//
// Reading run.Status without the run's own mutex matches buildRunDTO's
// existing access pattern (the locked accessor is unexported in package
// agent).
func (h *Handler) RunStates() []RunState {
	h.mu.Lock()
	rc := h.rc
	sessionIDs := make([]string, 0, len(h.agents))
	agents := make(map[string]*agent.Agent, len(h.agents))
	for id, as := range h.agents {
		sessionIDs = append(sessionIDs, id)
		agents[id] = as.agent
	}
	h.mu.Unlock()

	sort.Strings(sessionIDs)

	out := []RunState{}
	appendRuns := func(sessionID string, ag *agent.Agent) {
		if ag == nil || ag.Runs() == nil {
			return
		}
		for _, r := range ag.Runs().Snapshot() {
			out = append(out, RunState{
				SessionID: sessionID,
				ID:        r.ID,
				Name:      r.Name,
				Ended:     r.Status != agent.RunRunning,
				Failed:    r.Status == agent.RunFailed,
			})
		}
	}

	if rc != nil {
		appendRuns("", rc.Agent())
	}
	for _, id := range sessionIDs {
		appendRuns(id, agents[id])
	}
	return out
}

// RunStates exposes the handler snapshot at the Server level for in-process
// consumers (the desktop shell).
func (s *Server) RunStates() []RunState {
	return s.handler.RunStates()
}
