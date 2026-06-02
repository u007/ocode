package server

import (
	"net/http"
	"sort"

	"github.com/jamesmercstudio/ocode/internal/config"
)

func (h *Handler) HandleListMCP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.cfg == nil || len(h.cfg.MCP) == 0 {
		writeJSON(w, http.StatusOK, []any{})
		return
	}

	type mcpEntry struct {
		Name    string `json:"name"`
		Type    string `json:"type"`
		Enabled bool   `json:"enabled"`
	}
	out := make([]mcpEntry, 0, len(h.cfg.MCP))
	for name, mc := range h.cfg.MCP {
		out = append(out, mcpEntry{Name: name, Type: mc.Type, Enabled: mc.Enabled})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) HandleSetMCPEnabled(w http.ResponseWriter, r *http.Request, name string, enabled bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.cfg == nil {
		writeError(w, http.StatusInternalServerError, "config not loaded")
		return
	}
	mc, ok := h.cfg.MCP[name]
	if !ok {
		writeError(w, http.StatusNotFound, "MCP server not found")
		return
	}
	if err := config.SaveMCPEnabled(name, enabled); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	mc.Enabled = enabled
	h.cfg.MCP[name] = mc

	state := "enabled"
	if !enabled {
		state = "disabled"
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": name, "status": state})
}
