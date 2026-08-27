package server

import (
	"errors"
	"sort"
	"strings"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/tool"
)

// ErrPermissionPending is returned by runTurn when a session's transcript
// tail is still an unresolved PERMISSION_ASK: the turn is refused outright
// (no message appended, no Step call) rather than stepping the agent on top
// of a decision the user hasn't made yet. Every turn entry point (HandleChat,
// HandleSendMessage, the async turn-job queue) funnels through runTurn, so
// this is the single choke point that stops a second request — another
// browser tab, the desktop shell, a scheduler/Telegram dispatch — from
// resuming the agent loop while a permission dialog is still up.
var ErrPermissionPending = errors.New("a permission decision is pending for this session; resolve it before sending a new message")

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
			status := r.CurrentStatus()
			out = append(out, RunState{
				SessionID: sessionID,
				ID:        r.ID,
				Name:      r.Name,
				Ended:     status.IsTerminal(),
				Failed:    status == agent.RunFailed,
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
// exactly when its trailing tool-call round still contains one.
//
// Each session's messages are read under its own as.mu (not h.mu): a running
// turn mutates as.messages concurrently, so a snapshot taken under h.mu alone
// would race — see findPendingSession's comment for the same hazard.
func (h *Handler) PendingPermissionAsks() int {
	h.mu.Lock()
	sessions := make([]*agentSession, 0, len(h.agents))
	for _, as := range h.agents {
		sessions = append(sessions, as)
	}
	h.mu.Unlock()

	count := 0
	for _, as := range sessions {
		as.mu.Lock()
		pending := tailIsPermissionAsk(as.messages)
		as.mu.Unlock()
		if pending {
			count++
		}
	}
	return count
}

// trailingToolRunStart returns the index of the first message in the run of
// consecutive tool-role messages at the end of msgs — the results of the
// most recent assistant tool-call round. A single round can pause on more
// than one unresolved ask when the model dispatches several tool calls that
// each require a decision (parallel/group dispatch runs them concurrently
// before any pause check), so callers must search this whole run instead of
// assuming the literal last message is the only — or the right — one.
func trailingToolRunStart(msgs []agent.Message) int {
	i := len(msgs)
	for i > 0 && msgs[i-1].Role == "tool" {
		i--
	}
	return i
}

// tailIsPermissionAsk reports whether the most recent tool-call round still
// has an unresolved permission ask anywhere in it (not just as the literal
// last message — see trailingToolRunStart). A resolved ask has its sentinel
// content replaced in place, so once every ask in the round is answered this
// returns false again.
func tailIsPermissionAsk(msgs []agent.Message) bool {
	for i := trailingToolRunStart(msgs); i < len(msgs); i++ {
		if strings.HasPrefix(msgs[i].Content, tool.SentinelPermissionAsk) {
			return true
		}
	}
	return false
}

// PendingPermissionAsks exposes the pending-prompt count at the Server level
// for in-process consumers (the desktop shell badge).
func (s *Server) PendingPermissionAsks() int {
	return s.handler.PendingPermissionAsks()
}
