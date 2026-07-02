package server

import (
	"sort"
	"strings"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/tool"
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

// PendingPermissionAsks counts sessions currently blocked on a permission
// prompt: the agent pauses a turn after emitting a PERMISSION_ASK: tool
// message (see Agent.Step's pauseAfterResults), so a session is pending
// exactly when its transcript tail is such a message with nothing after it.
func (h *Handler) PendingPermissionAsks() int {
	h.mu.Lock()
	sessions := make([][]agent.Message, 0, len(h.agents))
	for _, as := range h.agents {
		sessions = append(sessions, as.messages)
	}
	h.mu.Unlock()

	count := 0
	for _, msgs := range sessions {
		if tailIsPermissionAsk(msgs) {
			count++
		}
	}
	return count
}

// tailIsPermissionAsk reports whether the newest message is an unanswered
// permission ask. Any later message (user answer, assistant follow-up)
// means the ask was resolved.
func tailIsPermissionAsk(msgs []agent.Message) bool {
	if len(msgs) == 0 {
		return false
	}
	last := msgs[len(msgs)-1]
	return last.Role == "tool" && strings.HasPrefix(last.Content, tool.SentinelPermissionAsk)
}

// PendingPermissionAsks exposes the pending-prompt count at the Server level
// for in-process consumers (the desktop shell badge).
func (s *Server) PendingPermissionAsks() int {
	return s.handler.PendingPermissionAsks()
}
