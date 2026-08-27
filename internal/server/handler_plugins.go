package server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/plugins"
)

func (h *Handler) pluginConfigSnapshot() *config.Config {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg == nil {
		return nil
	}
	c := *h.cfg
	c.Plugins = make(map[string]config.PluginConfig, len(h.cfg.Plugins))
	for name, pc := range h.cfg.Plugins {
		c.Plugins[name] = pc
	}
	return &c
}

func (h *Handler) HandleListPlugins(w http.ResponseWriter, r *http.Request) {
	cfg := h.pluginConfigSnapshot()

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

	// Load all plugins found on disk (project, global, bundled) regardless of
	// enabled state, so the web/desktop UI sees the same set the TUI's agent
	// loader sees. We then merge with cfg.Plugins (source of truth for
	// enabled/source/dir/ref) so unregistered disk plugins still appear
	// (as enabled by default) and stale config entries remain visible.
	loaded := plugins.LoadPluginsForProject(nil, h.workDir)
	loadedByName := make(map[string]plugins.Plugin, len(loaded))
	for _, pl := range loaded {
		loadedByName[pl.Name] = pl
	}

	seen := make(map[string]bool, len(loaded)+len(cfg.Plugins))
	out := make([]pluginEntry, 0, len(loaded)+len(cfg.Plugins))

	// First, emit every plugin discovered on disk.
	for _, pl := range loaded {
		pc, inCfg := cfg.Plugins[pl.Name]
		entry := pluginEntry{
			Name:        pl.Name,
			Description: pl.Description,
		}
		if inCfg {
			entry.Source = pc.Source
			// Prefer the on-disk Dir when available so the UI can act on the
			// real location; fall back to the config Dir for bundled entries
			// where Dir may have been materialized elsewhere.
			if pl.Dir != "" {
				entry.Dir = pl.Dir
			} else {
				entry.Dir = pc.Dir
			}
			entry.Enabled = pc.Enabled
		} else {
			// Disk-only plugin without a config entry: treat as enabled by
			// default (mirrors LoadPlugins(nil) and the agent's enabled=nil
			// path) so it is visible and toggle-able.
			entry.Source = ""
			entry.Dir = pl.Dir
			entry.Enabled = true
		}
		out = append(out, entry)
		seen[pl.Name] = true
	}

	// Then, include any config entries whose directory is missing or not
	// discovered (e.g., plugin removed from disk but still in config, or a
	// freshly installed plugin whose directory hasn't been scanned due to a
	// stale bundled extraction). These remain actionable for remove/toggle.
	for name, pc := range cfg.Plugins {
		if seen[name] {
			continue
		}
		desc := ""
		if pl, ok := loadedByName[name]; ok {
			desc = pl.Description
		}
		out = append(out, pluginEntry{
			Name:        name,
			Source:      pc.Source,
			Dir:         pc.Dir,
			Enabled:     pc.Enabled,
			Description: desc,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) HandleGetPlugin(w http.ResponseWriter, r *http.Request, name string) {
	cfg := h.pluginConfigSnapshot()
	if cfg == nil {
		writeError(w, http.StatusInternalServerError, "config not loaded")
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

	// Prefer config data when present, but fall back to disk discovery so
	// unregistered plugins (e.g., bundled orchestrator without an
	// external_plugins entry) are still inspectable.
	var detail pluginDetail
	detail.Name = name
	if pc, ok := cfg.Plugins[name]; ok {
		detail.Source = pc.Source
		detail.Dir = pc.Dir
		detail.Enabled = pc.Enabled
	} else {
		// Disk-only: synthesize an enabled entry.
		if dir := plugins.FindPluginDirForProject(name, h.workDir); dir != "" {
			detail.Dir = dir
			detail.Enabled = true
		} else {
			writeError(w, http.StatusNotFound, "plugin not found")
			return
		}
	}
	for _, pl := range plugins.LoadPluginsForProject(nil, h.workDir) {
		if pl.Name == name {
			detail.Description = pl.Description
			detail.Tools = pl.Tools
			detail.Commands = pl.Commands
			// Prefer the real on-disk dir when we synthesized it.
			if detail.Dir == "" && pl.Dir != "" {
				detail.Dir = pl.Dir
			} else if pl.Dir != "" {
				detail.Dir = pl.Dir
			}
			break
		}
	}
	writeJSON(w, http.StatusOK, detail)
}

func (h *Handler) HandleSetPluginEnabled(w http.ResponseWriter, r *http.Request, name string, enabled bool) {
	// Take a synchronized snapshot under lock, then release for all I/O.
	// Holding h.mu across config.SavePluginEnabled blocks session polling,
	// agent-run state, and other endpoints.
	cfg := h.pluginConfigSnapshot()

	// Normal path: plugin already has a config entry.
	if _, ok := cfg.Plugins[name]; ok {
		if err := config.SavePluginEnabled(name, enabled); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		h.mu.Lock()
		p := h.cfg.Plugins[name]
		p.Enabled = enabled
		h.cfg.Plugins[name] = p
		h.mu.Unlock()
		agent.ApplyAgentConfig(h.cfg)
		// Refresh resident agents so enable/disable takes effect on the next turn.
		h.refreshAgentSessionsForPluginChange()
		state := "enabled"
		if !enabled {
			state = "disabled"
		}
		writeJSON(w, http.StatusOK, map[string]string{"name": name, "status": state})
		return
	}

	// Disk-only plugin without a config entry: create one on first toggle
	// so enable/disable persists. We need its on-disk directory to populate
	// the new PluginConfig.
	dir := plugins.FindPluginDirForProject(name, h.workDir)
	// Also try the loaded scan which already resolved Dir.
	if dir == "" {
		for _, pl := range plugins.LoadPluginsForProject(nil, h.workDir) {
			if pl.Name == name {
				dir = pl.Dir
				break
			}
		}
	}
	if dir == "" {
		writeError(w, http.StatusNotFound, "plugin not found")
		return
	}
	// Look up source/ref from any existing loaded metadata if available,
	// otherwise leave source empty — the plugin is still toggleable.
	newCfg := config.PluginConfig{Dir: dir, Enabled: enabled}
	// Preserve source if we can infer it from the loaded plugin's origin?
	// The on-disk plugin.json doesn't store source; we keep what we have.
	if err := config.SavePlugin(name, newCfg); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.mu.Lock()
	if h.cfg.Plugins == nil {
		h.cfg.Plugins = map[string]config.PluginConfig{}
	}
	h.cfg.Plugins[name] = newCfg
	h.mu.Unlock()
	agent.ApplyAgentConfig(h.cfg)
	// Refresh resident agents so enable/disable takes effect on the next turn.
	h.refreshAgentSessionsForPluginChange()

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

	// Transactional install: if any post-clone step fails, roll back the
	// clone and any side effects (MCP registration, config persistence).
	rollbackClone := true
	mcpRegistered := false
	defer func() {
		if rollbackClone {
			if mcpRegistered {
				_ = plugins.UnregisterMCP(pl)
			}
			_ = os.RemoveAll(filepath.Join(installDir, dirName))
		}
	}()

	if err := plugins.RunOnInstall(dirName, pl); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("on_install: %v", err))
		return
	}
	if err := plugins.AutoRegisterMCP(dirName, pl); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("auto_register_mcp: %v", err))
		return
	}
	mcpRegistered = (pl.MCP != nil && pl.MCP.AutoRegister)

	pc := config.PluginConfig{Source: req.Source, Dir: dirName, Ref: ref, Enabled: true}
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
	agent.ApplyAgentConfig(h.cfg)

	// Success — prevent deferred rollback.
	rollbackClone = false
	// Refresh resident agent sessions so they pick up the new plugin tools.
	h.refreshAgentSessionsForPluginChange()

	writeJSON(w, http.StatusCreated, map[string]string{"name": pl.Name, "dir": dirName, "source": req.Source})
}

func (h *Handler) HandleRemovePlugin(w http.ResponseWriter, r *http.Request, name string) {
	// Take a snapshot of config under lock, then release before any I/O.
	cfg := h.pluginConfigSnapshot()
	if cfg == nil {
		writeError(w, http.StatusInternalServerError, "config not loaded")
		return
	}

	// Resolve the directory to remove: prefer config entry, fall back to
	// on-disk discovery so bundled or manually-placed plugins remain
	// removable even without an external_plugins entry.
	dir := ""
	if pc, ok := cfg.Plugins[name]; ok {
		dir = pc.Dir
	} else {
		dir = plugins.FindPluginDirForProject(name, h.workDir)
		if dir == "" {
			for _, pl := range plugins.LoadPluginsForProject(nil, h.workDir) {
				if pl.Name == name {
					dir = pl.Dir
					break
				}
			}
		}
	}
	if dir == "" {
		writeError(w, http.StatusNotFound, "plugin not found")
		return
	}

	// Path-containment check: the resolved dir must be inside the approved
	// plugin install root. A crafted config entry with an absolute path or
	// traversal sequences could otherwise delete arbitrary directories.
	if err := plugins.ValidateRemovableDirForProject(dir, h.workDir); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Bundled/embedded plugins ship inside the binary; their directory is
	// read-only and must never be deleted. Reject removal with a clear error
	// instead of attempting (and failing) the delete.
	if plugins.IsBundledPluginDir(dir) {
		writeError(w, http.StatusBadRequest, "bundled plugins cannot be removed")
		return
	}

	// Capture plugin metadata for MCP cleanup before deleting the directory
	// (which destroys the plugin.json we'd need to read).
	var loadedPlugin plugins.Plugin
	for _, pl := range plugins.LoadPluginsForProject(nil, h.workDir) {
		if pl.Name == name {
			loadedPlugin = pl
			break
		}
	}

	if err := plugins.Remove(dir); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Best-effort: remove MCP auto-registration and config entry.
	if loadedPlugin.MCP != nil {
		_ = plugins.UnregisterMCP(loadedPlugin)
	}

	if _, ok := cfg.Plugins[name]; ok {
		if err := config.RemovePlugin(name); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		h.mu.Lock()
		delete(h.cfg.Plugins, name)
		h.mu.Unlock()
		agent.ApplyAgentConfig(h.cfg)
	}

	// Refresh resident agent sessions so they drop the removed plugin's tools.
	h.refreshAgentSessionsForPluginChange()

	w.WriteHeader(http.StatusNoContent)
}

// refreshAgentSessionsForPluginChange rebuilds every resident agent session
// that shares this handler's project root, so plugin tool changes take effect
// on the next turn. The rebuild runs outside h.mu (buildAgentSession touches
// the filesystem and may spawn plugin/MCP processes). We snapshot the session
// IDs under h.mu, then rebuild each one individually.
//
// Lock order: agentSession.mu -> Handler.mu is respected because we never
// hold h.mu while calling buildAgentSession or replaceAgentSession. We only
// hold h.mu briefly to snapshot IDs and to call replaceAgentSession (which
// acquires h.mu for the map write).
func (h *Handler) refreshAgentSessionsForPluginChange() {
	if h.workDir == "" {
		return
	}
	h.mu.Lock()
	ids := make([]string, 0, len(h.agents))
	for id, as := range h.agents {
		if as == nil {
			continue
		}
		ids = append(ids, id)
	}
	h.mu.Unlock()

	for _, id := range ids {
		as := h.lookupAgentSession(id)
		if as == nil {
			continue
		}
		// Only refresh sessions whose project root matches this handler's.
		entry := h.sessions.Lookup(id)
		if entry == nil {
			continue
		}
		if entry.ProjectRoot != h.workDir {
			continue
		}
		// Best-effort rebuild: skip if the session entry shows an active
		// turn — the in-flight turn finishes on the old agent, and the
		// next turn picks up the new plugin set.
		if entry.turnActive {
			continue
		}
		newAs, _, err := h.buildAgentSession(id, as.model, as.messages, entry.ProjectRoot)
		if err != nil {
			continue // best-effort; stale agent is acceptable
		}
		h.replaceAgentSession(id, newAs)
	}
}
