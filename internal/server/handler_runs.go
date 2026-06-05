package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jamesmercstudio/ocode/internal/agent"
)

// runToolCallDTO is a single tool call inside an assistant message.
type runToolCallDTO struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// runMessageDTO is one transcript entry of an agent run, serialised for the web.
type runMessageDTO struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []runToolCallDTO `json:"toolCalls,omitempty"`
	ToolID    string           `json:"toolCallId,omitempty"`
}

// agentRunDTO mirrors a *agent.AgentRun for the web "agent preview" strip,
// including the run's transcript and its nested sub-agent runs (recursively).
type agentRunDTO struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Status       string          `json:"status"`
	Result       string          `json:"result,omitempty"`
	Err          string          `json:"err,omitempty"`
	Model        string          `json:"model,omitempty"`
	StartedAt    time.Time       `json:"startedAt"`
	EndedAt      *time.Time      `json:"endedAt,omitempty"`
	InputTokens  int64           `json:"inputTokens"`
	OutputTokens int64           `json:"outputTokens"`
	Messages     []runMessageDTO `json:"messages"`
	Children     []agentRunDTO   `json:"children"`
}

// buildRunDTO converts a run (and its nested sub-agent runs) into a serialisable
// DTO. Children come from the sub-agent's own run registry, the same source the
// TUI's agentRunChildren uses, so nesting is identical across surfaces.
func buildRunDTO(r *agent.AgentRun) agentRunDTO {
	in, out := r.Usage()
	dto := agentRunDTO{
		ID:           r.ID,
		Name:         r.Name,
		Status:       string(r.Status),
		Result:       r.Result,
		Err:          r.Err,
		Model:        r.ModelLabel(),
		StartedAt:    r.StartedAt,
		InputTokens:  in,
		OutputTokens: out,
		Messages:     []runMessageDTO{},
		Children:     []agentRunDTO{},
	}
	if !r.EndedAt.IsZero() {
		ended := r.EndedAt
		dto.EndedAt = &ended
	}
	for _, m := range r.TranscriptPublic() {
		if m.Content == "" && len(m.ToolCalls) == 0 {
			continue
		}
		md := runMessageDTO{Role: m.Role, Content: m.Content, ToolID: m.ToolID}
		for _, tc := range m.ToolCalls {
			md.ToolCalls = append(md.ToolCalls, runToolCallDTO{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			})
		}
		dto.Messages = append(dto.Messages, md)
	}
	if r.Sub != nil && r.Sub.Runs() != nil {
		// Children are kept in registry (chronological) order to mirror the TUI
		// strip; the list is small and bounded by active sub-agents.
		for _, c := range r.Sub.Runs().Snapshot() {
			dto.Children = append(dto.Children, buildRunDTO(c))
		}
	}
	return dto
}

// activeAgentForRuns resolves which agent's run registry to expose. In /rc mode
// the live TUI agent wins; otherwise the per-session server agent is used.
// Returns nil when no agent is active (a legitimate empty state — no runs yet),
// which callers render as an empty list rather than an error.
func (h *Handler) activeAgentForRuns(sessionID string) *agent.Agent {
	h.mu.Lock()
	rc := h.rc
	h.mu.Unlock()
	if rc != nil {
		if a := rc.Agent(); a != nil {
			return a
		}
	}
	if sessionID == "" {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if as, ok := h.agents[sessionID]; ok {
		return as.agent
	}
	return nil
}

// runsSnapshot builds the top-level run DTOs for the active agent, in registry
// (chronological) order. Returns an empty slice when no agent/runs are active.
func (h *Handler) runsSnapshot(sessionID string) []agentRunDTO {
	ag := h.activeAgentForRuns(sessionID)
	if ag == nil || ag.Runs() == nil {
		return []agentRunDTO{}
	}
	runs := ag.Runs().Snapshot()
	out := make([]agentRunDTO, 0, len(runs))
	for _, r := range runs {
		out = append(out, buildRunDTO(r))
	}
	return out
}

// HandleListRuns returns the current agent-run tree as JSON.
func (h *Handler) HandleListRuns(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.runsSnapshot(r.URL.Query().Get("session")))
}

// HandleRunsStream pushes the agent-run tree over SSE whenever it changes. The
// registry has no change notification, so the snapshot is polled on a short
// interval and emitted only when the serialised tree differs from the last sent
// frame (keeping the connection quiet while runs are idle).
func (h *Handler) HandleRunsStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	sessionID := r.URL.Query().Get("session")
	ctx := r.Context()
	ticker := time.NewTicker(750 * time.Millisecond)
	defer ticker.Stop()

	var last string
	emit := func() {
		data, err := json.Marshal(h.runsSnapshot(sessionID))
		if err != nil {
			// Marshalling our own DTOs should never fail; log and keep the
			// stream alive rather than dropping the client.
			fmt.Fprintf(w, "event: error\ndata: %s\n\n", `{"error":"failed to encode runs"}`)
			flusher.Flush()
			return
		}
		if string(data) == last {
			flusher.Flush() // keepalive
			return
		}
		last = string(data)
		fmt.Fprintf(w, "event: runs\ndata: %s\n\n", data)
		flusher.Flush()
	}

	emit() // send initial snapshot immediately
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			emit()
		}
	}
}
