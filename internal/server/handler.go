package server

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/tool"
)

type Handler struct {
	mu     sync.Mutex
	agents map[string]*agentSession
	cfg    *config.Config
	rc     *RCBridge // set when proxying to a TUI session
	// advisorEnabled is the runtime gate for the advisor tool, shared by all
	// agents this handler creates. Seeded from config, flipped from the web
	// sidebar, never persisted back to config.
	advisorEnabled bool
	workDir        string // override for git commands in tests
}

type agentSession struct {
	agent    *agent.Agent
	messages []agent.Message
	model    string
	mu       sync.Mutex
}

func NewHandler() *Handler {
	cfg, _ := config.Load()
	agent.ApplyAgentConfig(cfg)
	advisorEnabled := cfg == nil || cfg.Ocode.Advisor.Enabled
	return &Handler{
		agents:         make(map[string]*agentSession),
		cfg:            cfg,
		advisorEnabled: advisorEnabled,
	}
}

// SetWorkDir sets the working directory for git commands (used in tests).
func (h *Handler) SetWorkDir(dir string) {
	h.workDir = dir
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

	model := req.Model
	if model == "" {
		if h.cfg != nil && h.cfg.Model != "" {
			model = h.cfg.Model
		} else {
			writeError(w, http.StatusBadRequest, "no model configured")
			return
		}
	}

	h.mu.Lock()
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
			h.mu.Unlock()
			writeError(w, http.StatusInternalServerError, "failed to create LLM client")
			return
		}

		tools, lspMgr := tool.LoadBuiltins(h.cfg)
		ag := agent.NewAgent(client, tools, h.cfg, lspMgr)
		ag.LoadExternalTools(h.cfg)
		ag.SetAdvisorEnabled(h.advisorEnabled)

		as = &agentSession{
			agent:    ag,
			messages: messages,
			model:    model,
		}
		h.agents[sid] = as
		req.SessionID = sid
	}
	h.mu.Unlock()

	as.mu.Lock()
	defer as.mu.Unlock()

	as.messages = append(as.messages, agent.Message{Role: "user", Content: req.Content})
	messages := append([]agent.Message(nil), as.messages...)

	resp, err := as.agent.Step(messages)
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
	h.mu.Lock()
	rc := h.rc
	h.mu.Unlock()

	// Parse optional pagination params: limit (max messages from end) and
	// offset (skip this many from the end, for loading older messages).
	limit := 0 // 0 means return all
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	paginate := func(all []agent.Message) []agent.Message {
		total := len(all)
		if limit == 0 || limit >= total-offset {
			// Return everything up to the offset point.
			end := total - offset
			if end < 0 {
				end = 0
			}
			return all[:end]
		}
		start := total - offset - limit
		if start < 0 {
			start = 0
		}
		return all[start : total-offset]
	}

	// If this is the RC session, return in-memory messages from the bridge.
	if rc != nil && rc.SessionID == id {
		all := rc.GetMessages()
		msgs := paginate(all)
		writeJSON(w, http.StatusOK, SessionDetail{
			SessionInfo: SessionInfo{
				ID:        rc.SessionID,
				Title:     "",
				CreatedAt: time.Now().Format(time.RFC3339),
				UpdatedAt: time.Now().Format(time.RFC3339),
			},
			Messages: msgs,
			Total:    len(all),
		})
		return
	}

	s, err := session.Load(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	msgs := paginate(s.Messages)
	writeJSON(w, http.StatusOK, SessionDetail{
		SessionInfo: SessionInfo{
			ID:        s.ID,
			Title:     s.Title,
			CreatedAt: s.CreatedAt.Format(time.RFC3339),
			UpdatedAt: s.UpdatedAt.Format(time.RFC3339),
		},
		Messages: msgs,
		Total:    len(s.Messages),
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

	// If we have an RC bridge, forward to the TUI's agent instead of using our own.
	if h.rc != nil {
		rc := h.rc
		h.mu.Unlock()

		resultCh := make(chan RCResult, 1)
		select {
		case rc.RcCh <- RCRequest{Content: req.Content, ResultCh: resultCh}:
		case <-time.After(5 * time.Second):
			writeError(w, http.StatusServiceUnavailable, "TUI is busy, try again")
			return
		}

		select {
		case result := <-resultCh:
			if result.Error != nil {
				writeError(w, http.StatusInternalServerError, fmt.Sprintf("agent error: %v", result.Error))
				return
			}
			var content strings.Builder
			for _, m := range result.Messages {
				if m.Role == "assistant" && m.Content != "" {
					content.WriteString(m.Content)
				}
			}
			writeJSON(w, http.StatusOK, ChatResponse{
				Content:   content.String(),
				SessionID: rc.SessionID,
				Model:     rc.Model,
			})
		case <-time.After(5 * time.Minute):
			writeError(w, http.StatusGatewayTimeout, "agent response timed out")
		}
		return
	}

	as, ok := h.agents[id]
	if !ok {
		s, err := session.Load(id)
		if err != nil {
			h.mu.Unlock()
			writeError(w, http.StatusNotFound, "session not found")
			return
		}

		model := h.cfg.Model
		if model == "" {
			h.mu.Unlock()
			writeError(w, http.StatusBadRequest, "no model configured")
			return
		}

		client := agent.NewClient(h.cfg, model)
		if client == nil {
			h.mu.Unlock()
			writeError(w, http.StatusInternalServerError, "failed to create LLM client")
			return
		}

		tools, lspMgr := tool.LoadBuiltins(h.cfg)
		ag := agent.NewAgent(client, tools, h.cfg, lspMgr)
		ag.LoadExternalTools(h.cfg)
		ag.SetAdvisorEnabled(h.advisorEnabled)

		as = &agentSession{
			agent:    ag,
			messages: s.Messages,
			model:    model,
		}
		h.agents[id] = as
	}
	h.mu.Unlock()

	as.mu.Lock()
	defer as.mu.Unlock()

	as.messages = append(as.messages, agent.Message{Role: "user", Content: req.Content})
	messages := append([]agent.Message(nil), as.messages...)

	resp, err := as.agent.Step(messages)
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

	// Mark the currently configured model as active.
	currentModel := ""
	if h.cfg != nil {
		currentModel = h.cfg.Model
	}

	// Load all models from the models.dev registry.
	allModels := agent.AllProviderModels()
	for _, id := range allModels {
		provider, modelName, ok := splitModelID(id)
		if !ok {
			provider = "other"
			modelName = id
		}
		models = append(models, ModelInfo{
			Name:     id,
			Model:    modelName,
			Provider: provider,
			Active:   id == currentModel,
		})
	}

	// If registry is empty, fall back to configured model + provider keys.
	if len(models) == 0 && h.cfg != nil {
		if currentModel != "" {
			models = append(models, ModelInfo{
				Name:     currentModel,
				Model:    currentModel,
				Provider: "configured",
				Active:   true,
			})
		}
		for name := range h.cfg.Provider {
			models = append(models, ModelInfo{
				Name:     name,
				Model:    name,
				Provider: name,
			})
		}
	}

	writeJSON(w, http.StatusOK, models)
}

// splitModelID splits "provider/model" into provider and model parts.
func splitModelID(id string) (provider, model string, ok bool) {
	for i := 0; i < len(id); i++ {
		if id[i] == '/' {
			return id[:i], id[i+1:], true
		}
	}
	return "", "", false
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
	tools, lspMgr := tool.LoadBuiltins(h.cfg)
	ag := agent.NewAgent(client, tools, h.cfg, lspMgr)
	ag.LoadExternalTools(h.cfg)
	ag.SetAdvisorEnabled(h.advisorEnabled)
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

	text := ag.Recap(msgs, "")

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
