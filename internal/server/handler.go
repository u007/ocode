package server

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jamesmercstudio/ocode/internal/agent"
	"github.com/jamesmercstudio/ocode/internal/config"
	"github.com/jamesmercstudio/ocode/internal/session"
	"github.com/jamesmercstudio/ocode/internal/tool"
)

type Handler struct {
	mu      sync.Mutex
	agents  map[string]*agentSession
	cfg     *config.Config
}

type agentSession struct {
	agent   *agent.Agent
	messages []agent.Message
	model   string
}

func NewHandler() *Handler {
	cfg, _ := config.Load()
	return &Handler{
		agents: make(map[string]*agentSession),
		cfg:    cfg,
	}
}

func (h *Handler) HandleChat(w http.ResponseWriter, r *http.Request) {
	var req ChatRequest
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	model := req.Model
	if model == "" {
		if h.cfg != nil && h.cfg.Model != "" {
			model = h.cfg.Model
		} else {
			writeError(w, http.StatusBadRequest, "no model configured")
			return
		}
	}

	var as *agentSession
	if req.SessionID != "" {
		as = h.agents[req.SessionID]
	}

	if as == nil {
		sid := req.SessionID
		if sid == "" {
			sid = time.Now().Format("2006-01-02-150405")
		}

		var messages []agent.Message
		if req.SessionID != "" {
			s, err := session.Load(req.SessionID)
			if err == nil {
				messages = s.Messages
			}
		}

		client := agent.NewClient(h.cfg, model)
		if client == nil {
			writeError(w, http.StatusInternalServerError, "failed to create LLM client")
			return
		}

		tools := tool.LoadBuiltins()
		ag := agent.NewAgent(client, tools, h.cfg)
		ag.LoadExternalTools(h.cfg)

		as = &agentSession{
			agent:    ag,
			messages: messages,
			model:    model,
		}
		h.agents[sid] = as
		req.SessionID = sid
	}

	as.messages = append(as.messages, agent.Message{Role: "user", Content: req.Content})

	resp, err := as.agent.Step(as.messages)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("agent error: %v", err))
		return
	}

	as.messages = append(as.messages, resp...)

	var content strings.Builder
	for _, m := range resp {
		if m.Role == "assistant" && m.Content != "" {
			content.WriteString(m.Content)
		}
	}

	_ = session.Save(req.SessionID, "", as.messages, nil)

	writeJSON(w, http.StatusOK, ChatResponse{
		Content:   content.String(),
		SessionID: req.SessionID,
		Model:     as.model,
	})
}

func (h *Handler) HandleListSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := session.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list sessions: %v", err))
		return
	}

	result := make([]SessionInfo, 0, len(sessions))
	for _, s := range sessions {
		result = append(result, SessionInfo{
			ID:        s.ID,
			Title:     s.Title,
			CreatedAt: s.CreatedAt.Format(time.RFC3339),
			UpdatedAt: s.UpdatedAt.Format(time.RFC3339),
		})
	}

	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) HandleGetSession(w http.ResponseWriter, r *http.Request, id string) {
	s, err := session.Load(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	writeJSON(w, http.StatusOK, SessionInfo{
		ID:        s.ID,
		Title:     s.Title,
		CreatedAt: s.CreatedAt.Format(time.RFC3339),
		UpdatedAt: s.UpdatedAt.Format(time.RFC3339),
	})
}

func (h *Handler) HandleSendMessage(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Content string `json:"content"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	as, ok := h.agents[id]
	if !ok {
		s, err := session.Load(id)
		if err != nil {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}

		model := h.cfg.Model
		if model == "" {
			writeError(w, http.StatusBadRequest, "no model configured")
			return
		}

		client := agent.NewClient(h.cfg, model)
		if client == nil {
			writeError(w, http.StatusInternalServerError, "failed to create LLM client")
			return
		}

		tools := tool.LoadBuiltins()
		ag := agent.NewAgent(client, tools, h.cfg)
		ag.LoadExternalTools(h.cfg)

		as = &agentSession{
			agent:    ag,
			messages: s.Messages,
			model:    model,
		}
		h.agents[id] = as
	}

	as.messages = append(as.messages, agent.Message{Role: "user", Content: req.Content})

	resp, err := as.agent.Step(as.messages)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("agent error: %v", err))
		return
	}

	as.messages = append(as.messages, resp...)

	var content strings.Builder
	for _, m := range resp {
		if m.Role == "assistant" && m.Content != "" {
			content.WriteString(m.Content)
		}
	}

	_ = session.Save(id, "", as.messages, nil)

	writeJSON(w, http.StatusOK, ChatResponse{
		Content:   content.String(),
		SessionID: id,
		Model:     as.model,
	})
}

func (h *Handler) HandleListModels(w http.ResponseWriter, r *http.Request) {
	models := []ModelInfo{}

	if h.cfg != nil && h.cfg.Model != "" {
		models = append(models, ModelInfo{
			Name:     h.cfg.Model,
			Provider: "configured",
		})
	}

	if h.cfg != nil {
		for name := range h.cfg.Provider {
			models = append(models, ModelInfo{
				Name:     name,
				Provider: name,
			})
		}
	}

	writeJSON(w, http.StatusOK, models)
}
