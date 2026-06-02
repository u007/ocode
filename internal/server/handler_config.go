package server

import (
	"net/http"

	"github.com/jamesmercstudio/ocode/internal/agent"
	"github.com/jamesmercstudio/ocode/internal/config"
)

func (h *Handler) HandleGetModel(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	model := ""
	if h.cfg != nil {
		model = h.cfg.Model
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"model": model})
}

func (h *Handler) HandleSetModel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string `json:"model"`
	}
	if err := readBodyJSON(r, &req); err != nil || req.Model == "" {
		writeError(w, http.StatusBadRequest, "model is required")
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.cfg == nil {
		writeError(w, http.StatusInternalServerError, "config not loaded")
		return
	}
	h.cfg.Model = req.Model
	if err := config.SaveLastModel(req.Model); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"model": req.Model})
}

func (h *Handler) HandleGetSmallModel(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	current := ""
	if h.cfg != nil {
		current = h.cfg.Ocode.SmallModel
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"model":    current,
		"priority": agent.SmallModelPriority,
	})
}

func (h *Handler) HandleSetSmallModel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string `json:"model"`
	}
	if err := readBodyJSON(r, &req); err != nil || req.Model == "" {
		writeError(w, http.StatusBadRequest, `model is required (use "auto" to clear)`)
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.cfg == nil {
		writeError(w, http.StatusInternalServerError, "config not loaded")
		return
	}

	if req.Model == "auto" {
		h.cfg.Ocode.SmallModel = ""
		resolved := agent.ResolveSmallModel(h.cfg)
		if err := config.SaveSmallModel(resolved); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		h.cfg.Ocode.SmallModel = resolved
		writeJSON(w, http.StatusOK, map[string]string{"model": resolved, "source": "auto"})
		return
	}

	if err := config.SaveSmallModel(req.Model); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.cfg.Ocode.SmallModel = req.Model
	writeJSON(w, http.StatusOK, map[string]string{"model": req.Model, "source": "manual"})
}

func (h *Handler) HandleGetAdvisor(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	model := ""
	if h.cfg != nil {
		model = h.cfg.Ocode.Advisor.Model
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"model": model})
}

func (h *Handler) HandleSetAdvisor(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string `json:"model"`
	}
	if err := readBodyJSON(r, &req); err != nil || req.Model == "" {
		writeError(w, http.StatusBadRequest, "model is required")
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.cfg == nil {
		writeError(w, http.StatusInternalServerError, "config not loaded")
		return
	}
	if err := config.SaveAdvisorModel(req.Model); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.cfg.Ocode.Advisor.Model = req.Model
	writeJSON(w, http.StatusOK, map[string]string{"model": req.Model})
}

func (h *Handler) HandleListAgents(w http.ResponseWriter, r *http.Request) {
	specs := agent.PrimaryAgentSpecs()
	type agentInfo struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	out := make([]agentInfo, len(specs))
	for i, s := range specs {
		out[i] = agentInfo{Name: s.Name, Description: s.Description}
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) HandleSetAgent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string `json:"name"`
		SessionID string `json:"session_id,omitempty"`
	}
	if err := readBodyJSON(r, &req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	spec := agent.FindAgentSpec(req.Name)
	if spec == nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	if req.SessionID != "" {
		h.mu.Lock()
		if as, ok := h.agents[req.SessionID]; ok {
			as.agent.SetSpec(spec)
		}
		h.mu.Unlock()
	}

	writeJSON(w, http.StatusOK, map[string]string{"name": spec.Name, "description": spec.Description})
}
