package server

import (
	"log"
	"net/http"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
)

// sessionGoalResponse is the body of GET and PUT /api/sessions/{id}/goal.
// Active reports whether the session has a goal at all; Status says whether
// the judge still evaluates it. All fields are zero when there is no goal.
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

func goalResponse(a *agent.Agent) sessionGoalResponse {
	gs, ok := a.Goal()
	if !ok {
		return sessionGoalResponse{}
	}
	return sessionGoalResponse{
		Active:     true,
		Condition:  gs.Condition,
		Status:     string(gs.Status),
		Turns:      gs.Turns,
		LastReason: gs.LastReason,
		StatusText: a.GoalStatusText(),
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

// HandleGetSessionGoal reports the session's /goal state.
func (h *Handler) HandleGetSessionGoal(w http.ResponseWriter, r *http.Request, id string) {
	as, _, ok := h.goalSession(w, id)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, goalResponse(as.agent))
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
		sessionGoalResponse: goalResponse(as.agent),
		KickoffPrompt:       as.agent.GoalKickoffPrompt(),
	})
}

// HandleClearSessionGoal removes the session's goal.
func (h *Handler) HandleClearSessionGoal(w http.ResponseWriter, r *http.Request, id string) {
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
