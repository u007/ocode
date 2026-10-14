package server

import (
	"fmt"
	"log"
	"net/http"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
)

// sessionGoalResponse is the body of GET and PUT /api/sessions/{id}/goal.
// Active reports whether the goal is still being pursued (the same rule as
// Agent.GoalEvaluable); a met or impossible goal keeps its Status and
// StatusText but reports Active=false. All fields are zero when there is no goal.
type sessionGoalResponse struct {
	Active     bool   `json:"active"`
	Condition  string `json:"condition"`
	Status     string `json:"status"`
	Turns      int    `json:"turns"`
	LastReason string `json:"last_reason"`
	StatusText string `json:"status_text"`
}

// sessionGoalPutResponse adds the first user turn the client should send.
type sessionGoalPutResponse struct {
	sessionGoalResponse
	KickoffPrompt string `json:"kickoff_prompt"`
}

// liveGoalResponse describes the goal of a session with a live agent.
func liveGoalResponse(a *agent.Agent) sessionGoalResponse {
	gs, ok := a.Goal()
	if !ok {
		return sessionGoalResponse{}
	}
	return goalResponse(gs, a.GoalStatusText())
}

// livePollGoalResponse is liveGoalResponse without the judge line. GET is polled,
// and resolving the judge rebuilds a decision client on every call while none is
// cached, so a poll must not resolve it.
func livePollGoalResponse(a *agent.Agent) sessionGoalResponse {
	gs, ok := a.Goal()
	if !ok {
		return sessionGoalResponse{}
	}
	return goalResponse(gs, agent.GoalStatusTextFor(gs, ""))
}

// goalResponse describes gs. Active follows the same rule as Agent.GoalEvaluable.
func goalResponse(gs agent.GoalState, statusText string) sessionGoalResponse {
	return sessionGoalResponse{
		Active:     gs.Status == agent.GoalActive || gs.Status == agent.GoalPaused,
		Condition:  gs.Condition,
		Status:     string(gs.Status),
		Turns:      gs.Turns,
		LastReason: gs.LastReason,
		StatusText: statusText,
	}
}

// persistSessionGoal writes the agent's current goal into the session metadata,
// or removes the key when there is none. It takes only the metadata file lock,
// so callers must not hold h.mu.
func persistSessionGoal(projectRoot, id string, a *agent.Agent) error {
	gs, ok := a.Goal()
	return session.UpdateMetadataForDir(projectRoot, id, func(md map[string]any) {
		if !ok {
			delete(md, agent.GoalMetadataKey)
			return
		}
		md[agent.GoalMetadataKey] = agent.GoalStateToMetadata(gs)[agent.GoalMetadataKey]
	})
}

// goalSession resolves the session's live agent for a goal endpoint. It writes
// the error response and returns false when the session cannot be resolved.
func (h *Handler) goalSession(w http.ResponseWriter, id string) (*agentSession, string, bool) {
	entry, err := h.sessions.Resolve(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return nil, "", false
	}
	as, err := h.getOrCreateAgentSession(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return nil, "", false
	}
	return as, entry.ProjectRoot, true
}

// bridgedGoalAgent returns the TUI's agent when id is the /rc-bridged session.
// The TUI owns that session's agent and its persistence, so the goal endpoints
// act on the bridged agent directly and never build a second one. ok is false
// for every other session. The agent is nil when the TUI has none yet.
func (h *Handler) bridgedGoalAgent(id string) (ag *agent.Agent, ok bool) {
	rc := h.RCBridge()
	if rc == nil || rc.SessionID != id {
		return nil, false
	}
	return rc.Agent(), true
}

// HandleGetSessionGoal reports the session's /goal state. It is polled, so it
// never builds an agent: a live agent answers, otherwise the persisted metadata.
func (h *Handler) HandleGetSessionGoal(w http.ResponseWriter, r *http.Request, id string) {
	if ag, bridged := h.bridgedGoalAgent(id); bridged && ag != nil {
		writeJSON(w, http.StatusOK, livePollGoalResponse(ag))
		return
	}
	if as := h.lookupAgentSession(id); as != nil {
		writeJSON(w, http.StatusOK, livePollGoalResponse(as.agent))
		return
	}
	entry, err := h.sessions.Resolve(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	s, err := session.LoadForDir(entry.ProjectRoot, id)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("session not found: %v", err))
		return
	}
	gs, ok := agent.GoalStateFromMetadata(s.Metadata)
	if !ok {
		writeJSON(w, http.StatusOK, sessionGoalResponse{})
		return
	}
	writeJSON(w, http.StatusOK, goalResponse(gs, agent.GoalStatusTextFor(gs, "")))
}

// HandleSetSessionGoal sets the session's goal, replacing any previous one.
// Body is {"condition": "..."}. An empty or overlong condition is a 400.
func (h *Handler) HandleSetSessionGoal(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Condition string `json:"condition"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if ag, bridged := h.bridgedGoalAgent(id); bridged {
		if ag == nil {
			writeError(w, http.StatusConflict, "the bridged TUI session has no live agent")
			return
		}
		if err := ag.SetGoal(req.Condition); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		// The TUI persists its own session, so the metadata is not written here.
		writeJSON(w, http.StatusOK, sessionGoalPutResponse{
			sessionGoalResponse: liveGoalResponse(ag),
			KickoffPrompt:       ag.GoalKickoffPrompt(),
		})
		return
	}
	as, projectRoot, ok := h.goalSession(w, id)
	if !ok {
		return
	}
	if err := as.agent.SetGoal(req.Condition); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := persistSessionGoal(projectRoot, id, as.agent); err != nil {
		log.Printf("serve: persist goal for %s: %v", id, err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, sessionGoalPutResponse{
		sessionGoalResponse: liveGoalResponse(as.agent),
		KickoffPrompt:       as.agent.GoalKickoffPrompt(),
	})
}

// HandleClearSessionGoal removes the session's goal.
func (h *Handler) HandleClearSessionGoal(w http.ResponseWriter, r *http.Request, id string) {
	if ag, bridged := h.bridgedGoalAgent(id); bridged {
		if ag == nil {
			writeError(w, http.StatusConflict, "the bridged TUI session has no live agent")
			return
		}
		ag.ClearGoal()
		w.WriteHeader(http.StatusNoContent)
		return
	}
	as, projectRoot, ok := h.goalSession(w, id)
	if !ok {
		return
	}
	as.agent.ClearGoal()
	if err := persistSessionGoal(projectRoot, id, as.agent); err != nil {
		log.Printf("serve: persist goal for %s: %v", id, err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
