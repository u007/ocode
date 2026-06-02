package server

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/jamesmercstudio/ocode/internal/config"
	"github.com/jamesmercstudio/ocode/internal/plugins"
)

func (h *Handler) HandleListPlugins(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	cfg := h.cfg
	h.mu.Unlock()

	if cfg == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}

	type pluginEntry struct {
		Name        string `json:"name"`
		Source      string `json:"source"`
		Dir         string `json:"dir"`
		Enabled     bool   `json:"enabled"`
		Description string `json:"description,omitempty"`
	}

	loaded := plugins.LoadPlugins(nil)
	descByName := make(map[string]string, len(loaded))
	for _, pl := range loaded {
		descByName[pl.Name] = pl.Description
	}

	out := make([]pluginEntry, 0, len(cfg.Plugins))
	for name, pc := range cfg.Plugins {
		out = append(out, pluginEntry{
			Name:        name,
			Source:      pc.Source,
			Dir:         pc.Dir,
			Enabled:     pc.Enabled,
			Description: descByName[name],
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) HandleGetPlugin(w http.ResponseWriter, r *http.Request, name string) {
	h.mu.Lock()
	if h.cfg == nil {
		h.mu.Unlock()
		writeError(w, http.StatusInternalServerError, "config not loaded")
		return
	}
	pc, ok := h.cfg.Plugins[name]
	h.mu.Unlock()

	if !ok {
		writeError(w, http.StatusNotFound, "plugin not found")
		return
	}

	type pluginDetail struct {
		Name        string   `json:"name"`
		Source      string   `json:"source"`
		Dir         string   `json:"dir"`
		Enabled     bool     `json:"enabled"`
		Description string   `json:"description,omitempty"`
		Tools       []string `json:"tools,omitempty"`
		Commands    []string `json:"commands,omitempty"`
	}

	detail := pluginDetail{Name: name, Source: pc.Source, Dir: pc.Dir, Enabled: pc.Enabled}
	for _, pl := range plugins.LoadPlugins(nil) {
		if pl.Name == name {
			detail.Description = pl.Description
			detail.Tools = pl.Tools
			detail.Commands = pl.Commands
			break
		}
	}
	writeJSON(w, http.StatusOK, detail)
}

func (h *Handler) HandleSetPluginEnabled(w http.ResponseWriter, r *http.Request, name string, enabled bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.cfg.Plugins[name]; !ok {
		writeError(w, http.StatusNotFound, "plugin not found")
		return
	}
	if err := config.SavePluginEnabled(name, enabled); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	p := h.cfg.Plugins[name]
	p.Enabled = enabled
	h.cfg.Plugins[name] = p

	state := "enabled"
	if !enabled {
		state = "disabled"
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": name, "status": state})
}

func (h *Handler) HandleInstallPlugin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Source string `json:"source"`
	}
	if err := readBodyJSON(r, &req); err != nil || req.Source == "" {
		writeError(w, http.StatusBadRequest, "source is required")
		return
	}

	source := req.Source
	ref := ""
	if at := strings.LastIndex(source, "@"); at > 0 {
		ref = source[at+1:]
		source = source[:at]
	}

	installDir, err := plugins.PluginInstallDir()
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("install dir: %v", err))
		return
	}

	pl, dirName, err := plugins.InstallGit(source, installDir, ref)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := plugins.RunOnInstall(dirName, pl); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := plugins.AutoRegisterMCP(dirName, pl); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	pc := config.PluginConfig{Source: req.Source, Dir: dirName, Enabled: true}
	if err := config.SavePlugin(pl.Name, pc); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.mu.Lock()
	if h.cfg.Plugins == nil {
		h.cfg.Plugins = map[string]config.PluginConfig{}
	}
	h.cfg.Plugins[pl.Name] = pc
	h.mu.Unlock()

	writeJSON(w, http.StatusCreated, map[string]string{"name": pl.Name, "dir": dirName, "source": req.Source})
}

func (h *Handler) HandleRemovePlugin(w http.ResponseWriter, r *http.Request, name string) {
	h.mu.Lock()
	if h.cfg == nil || h.cfg.Plugins == nil {
		h.mu.Unlock()
		writeError(w, http.StatusNotFound, "plugin not found")
		return
	}
	pc, ok := h.cfg.Plugins[name]
	h.mu.Unlock()

	if !ok {
		writeError(w, http.StatusNotFound, "plugin not found")
		return
	}
	if err := plugins.Remove(pc.Dir); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := config.RemovePlugin(name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.mu.Lock()
	delete(h.cfg.Plugins, name)
	h.mu.Unlock()

	w.WriteHeader(http.StatusNoContent)
}
