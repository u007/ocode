package server

import (
	"fmt"
	"log"
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
	agent.ApplyAgentConfig(cfg)
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
			sid = session.NewSessionID()
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

		tools, _ := tool.LoadBuiltins(h.cfg)
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
		log.Printf("serve error: agent step: %v", err)
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

		tools, _ := tool.LoadBuiltins(h.cfg)
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
		log.Printf("serve error: agent step: %v", err)
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

// getOrCreateAgentSession returns the in-memory agent session for id,
// creating one from the saved session if it does not exist yet.
// Must be called with h.mu held.
func (h *Handler) getOrCreateAgentSession(id string) (*agentSession, error) {
	if as, ok := h.agents[id]; ok {
		return as, nil
	}
	s, err := session.Load(id)
	if err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
	}
	model := h.cfg.Model
	if model == "" {
		return nil, fmt.Errorf("no model configured")
	}
	client := agent.NewClient(h.cfg, model)
	if client == nil {
		return nil, fmt.Errorf("failed to create LLM client")
	}
	tools, _ := tool.LoadBuiltins(h.cfg)
	ag := agent.NewAgent(client, tools, h.cfg)
	ag.LoadExternalTools(h.cfg)
	as := &agentSession{agent: ag, messages: s.Messages, model: model}
	h.agents[id] = as
	return as, nil
}

func (h *Handler) HandleCompactSession(w http.ResponseWriter, r *http.Request, id string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	as, err := h.getOrCreateAgentSession(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	result, enabled := as.agent.Compact(as.messages)
	if !enabled {
		writeError(w, http.StatusUnprocessableEntity, "compaction disabled in config")
		return
	}
	if !result.OK {
		if result.Err != nil {
			writeError(w, http.StatusInternalServerError, result.Err.Error())
			return
		}
		writeError(w, http.StatusUnprocessableEntity, "nothing to compact")
		return
	}

	before := as.messages[:result.ReplaceFrom]
	after := as.messages[result.ReplaceTo:]
	compacted := make([]agent.Message, 0, len(before)+1+len(after))
	compacted = append(compacted, before...)
	compacted = append(compacted, result.Summary)
	compacted = append(compacted, after...)
	as.messages = compacted

	_ = session.Save(id, "", as.messages, nil)

	writeJSON(w, http.StatusOK, map[string]any{
		"original_len":  result.OriginalLen,
		"compacted_len": len(as.messages),
	})
}

func (h *Handler) HandleRecapSession(w http.ResponseWriter, r *http.Request, id string) {
	h.mu.Lock()

	as, err := h.getOrCreateAgentSession(id)
	if err != nil {
		h.mu.Unlock()
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if len(as.messages) == 0 {
		h.mu.Unlock()
		writeError(w, http.StatusUnprocessableEntity, "no messages to recap")
		return
	}

	msgs := make([]agent.Message, len(as.messages))
	copy(msgs, as.messages)
	ag := as.agent
	h.mu.Unlock()

	text := ag.Recap(msgs)

	writeJSON(w, http.StatusOK, map[string]string{"recap": text})
}

func (h *Handler) HandleExportSession(w http.ResponseWriter, r *http.Request, id string) {
	s, err := session.Load(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	var b strings.Builder
	for _, msg := range s.Messages {
		if msg.Role == "user" || msg.Role == "assistant" {
			role := strings.ToUpper(msg.Role[:1]) + msg.Role[1:]
			b.WriteString(fmt.Sprintf("## %s\n\n%s\n\n", role, msg.Content))
		}
	}

	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="ocode_export_%s.md"`, id))
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, b.String())
}

func (h *Handler) HandleExportClaudeSession(w http.ResponseWriter, r *http.Request, id string) {
	s, err := session.Load(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if len(s.Messages) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "no messages to export")
		return
	}

	path, err := session.AppendClaudeSession(id, s.Messages)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": path})
}

func (h *Handler) HandleShareSession(w http.ResponseWriter, r *http.Request, id string) {
	s, err := session.Load(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	var b strings.Builder
	title := s.Title
	if title == "" {
		title = "ocode session " + id
	}
	fmt.Fprintf(&b, "# %s\n\n", title)
	fmt.Fprintf(&b, "Session ID: `%s`  \nCreated: %s\n\n---\n\n", id, s.CreatedAt.Format(time.RFC3339))

	for _, msg := range s.Messages {
		if msg.Role != "user" && msg.Role != "assistant" {
			continue
		}
		if msg.Content == "" {
			continue
		}
		role := strings.ToUpper(msg.Role[:1]) + msg.Role[1:]
		fmt.Fprintf(&b, "**%s:** %s\n\n", role, msg.Content)
	}

	writeJSON(w, http.StatusOK, map[string]string{"markdown": b.String()})
}

func (h *Handler) HandleSetSessionTitle(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Title string `json:"title"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title cannot be empty")
		return
	}

	s, err := session.Load(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	if err := session.Save(id, req.Title, s.Messages, nil); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"title": req.Title})
}

func (h *Handler) HandleSessionContext(w http.ResponseWriter, r *http.Request, id string) {
	s, err := session.Load(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	var totalChars int
	for _, msg := range s.Messages {
		totalChars += len(msg.Content) + len(msg.ReasoningContent)
		for _, tc := range msg.ToolCalls {
			totalChars += len(tc.Function.Arguments)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"session_id":       id,
		"message_count":    len(s.Messages),
		"estimated_tokens": totalChars / 4,
	})
}
