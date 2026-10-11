package server

import (
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/commandctx"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/knowledge"
	"github.com/u007/ocode/internal/memory"
	"github.com/u007/ocode/internal/tool"
)

// ─── Slash-command parity endpoints ────────────────────────────────────────
// These handlers back the web/desktop slash commands that the TUI implements
// locally (/docs, /paths, /mem status, /ban, /autocontinue, /connect,
// /add-dir). Where a TUI command builds an LLM prompt, this file reuses the
// internal/commandctx builders so both surfaces behave identically.

// HandleGetPathsInfo serves GET /api/paths — the /paths report for the
// requested project (byte-identical markdown to the TUI's /paths output).
func (h *Handler) HandleGetPathsInfo(w http.ResponseWriter, r *http.Request) {
	workDir, ok := h.gitProjectDir(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown project")
		return
	}
	if workDir == "" {
		workDir = h.workDir
	}

	h.mu.Lock()
	var extraPaths []string
	uploadDir := ""
	activeOpenCodePath := ""
	if h.cfg != nil {
		extraPaths = h.cfg.Ocode.ExtraAllowedPaths
		uploadDir = h.cfg.Ocode.UploadDir
		if p, err := h.cfg.ActiveConfigPath(); err == nil {
			activeOpenCodePath = p
		}
	}
	h.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"work_dir":             workDir,
		"extra_allowed_paths":  extraPaths,
		"upload_dir":           uploadDir,
		"active_opencode_path": activeOpenCodePath,
		"text":                 commandctx.PathsInfo(workDir, extraPaths, uploadDir, activeOpenCodePath),
	})
}

// HandleGetMemoryStatus serves GET /api/memory/status — the /mem status
// snapshot (which scope files exist, previews) for the requested project.
func (h *Handler) HandleGetMemoryStatus(w http.ResponseWriter, r *http.Request) {
	workDir, ok := h.gitProjectDir(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown project")
		return
	}

	enabled := false
	h.mu.Lock()
	if h.cfg != nil {
		enabled = h.cfg.Ocode.MemoryEnabled
	}
	h.mu.Unlock()

	snap, err := memory.Status(workDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": enabled,
		"scopes": map[string]any{
			"user":    memoryScopeJSON(snap.User),
			"project": memoryScopeJSON(snap.Project),
			"global":  memoryScopeJSON(snap.Global),
		},
	})
}

func memoryScopeJSON(s memory.Scope) map[string]any {
	return map[string]any{
		"path":    s.Path,
		"present": s.Present,
		"preview": s.Preview,
	}
}

// HandleSetBashRule serves POST /api/permissions/bash-rule — the /ban write
// path. Body: {"prefix": "rm -rf", "level": "deny"|"allow"|"ask"}.
// Persists to config and applies to every live agent (mirroring the TUI's
// pm.SetBashPrefixRule + persistPermissions).
func (h *Handler) HandleSetBashRule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Prefix string `json:"prefix"`
		Level  string `json:"level"`
	}
	if err := readBodyJSON(r, &req); err != nil || strings.TrimSpace(req.Prefix) == "" || req.Level == "" {
		writeError(w, http.StatusBadRequest, "prefix and level are required")
		return
	}
	prefix := strings.TrimSpace(req.Prefix)
	level := agent.PermissionLevel(strings.TrimSpace(req.Level))
	// Shared validator, NOT the setter: SetBashPrefixRule silently discards an
	// invalid rule, so a `git`=allow write used to answer 200 while storing
	// nothing. Validate up front and report the reason.
	if err := agent.ValidateBashPrefixRule(prefix, level); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	levelStr := string(level)

	if err := config.SaveSingleBashPrefixRule(prefix, levelStr); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg != nil {
		if h.cfg.Ocode.Permissions.Bash.Prefixes == nil {
			h.cfg.Ocode.Permissions.Bash.Prefixes = map[string]string{}
		}
		h.cfg.Ocode.Permissions.Bash.Prefixes[prefix] = levelStr
	}
	for _, a := range h.allAgents() {
		if pm := a.Permissions(); pm != nil {
			pm.SetBashPrefixRule(prefix, level)
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"prefix": prefix, "level": levelStr})
}

// HandleSetBashRules serves PUT /api/permissions/bash-rules — the batch write
// behind the Settings → Permissions rule editor. Body:
//
//	{"set": {"git push": "deny"}, "remove": ["sed"]}
//
// The payload is a DELTA, not a replacement map, and that is deliberate: a
// full-map PUT would clobber a rule another surface added in the meantime (the
// TUI /ban handler, POST /api/permissions/bash-rule) between the editor's load
// and its save. Each entry is applied with its own targeted load-modify-write,
// so a concurrent writer's unrelated rule survives.
//
// Validation is complete BEFORE anything is written, so one bad entry produces a
// 400 and no partial write. SetBashPrefixRule silently discards an invalid rule,
// which is why the shared validator runs first here rather than relying on the
// setter to refuse.
func (h *Handler) HandleSetBashRules(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Set    map[string]string `json:"set"`
		Remove []string          `json:"remove"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// Validate the whole payload first: normalize keys, then reject.
	type write struct {
		prefix string
		level  agent.PermissionLevel
	}
	sets := make([]write, 0, len(req.Set))
	for rawPrefix, rawLevel := range req.Set {
		prefix := strings.TrimSpace(rawPrefix)
		level := agent.PermissionLevel(strings.TrimSpace(rawLevel))
		if err := agent.ValidateBashPrefixRule(prefix, level); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("%q: %s", prefix, err.Error()))
			return
		}
		sets = append(sets, write{prefix: prefix, level: level})
	}
	// Deterministic order so two clients sending the same delta persist the same
	// bytes to the config file (map iteration order is random).
	sort.Slice(sets, func(i, j int) bool { return sets[i].prefix < sets[j].prefix })

	removes := make([]string, 0, len(req.Remove))
	seenRemove := make(map[string]bool, len(req.Remove))
	for _, raw := range req.Remove {
		prefix := strings.TrimSpace(raw)
		if prefix == "" || seenRemove[prefix] {
			continue
		}
		seenRemove[prefix] = true
		removes = append(removes, prefix)
	}
	sort.Strings(removes)

	if len(sets) == 0 && len(removes) == 0 {
		writeError(w, http.StatusBadRequest, "no rule changes supplied")
		return
	}

	// Persist first, one targeted load-modify-write per entry, so a config
	// failure leaves the on-disk state untouched by the entries that follow.
	// A prefix present in both maps resolves to the set (the editor's rename
	// sends the new value plus the old key's removal; apply the set last).
	for _, rm := range removes {
		if _, also := req.Set[rm]; also {
			continue
		}
		if err := config.DeleteBashPrefixRule(rm); err != nil {
			writeError(w, http.StatusInternalServerError, "remove "+rm+": "+err.Error())
			return
		}
	}
	for _, s := range sets {
		if err := config.SaveSingleBashPrefixRule(s.prefix, string(s.level)); err != nil {
			writeError(w, http.StatusInternalServerError, "set "+s.prefix+": "+err.Error())
			return
		}
	}

	// Apply to live agents and the in-memory config under h.mu (same order as
	// HandleSetBashRule). Plain map operations on the copy-on-write rule store,
	// so nothing here can fail after the config is written.
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg != nil {
		prefixes := h.cfg.Ocode.Permissions.Bash.Prefixes
		if prefixes == nil {
			prefixes = map[string]string{}
			h.cfg.Ocode.Permissions.Bash.Prefixes = prefixes
		}
		for _, rm := range removes {
			delete(prefixes, rm)
		}
		for _, s := range sets {
			prefixes[s.prefix] = string(s.level)
		}
	}
	for _, a := range h.allAgents() {
		pm := a.Permissions()
		if pm == nil {
			continue
		}
		for _, rm := range removes {
			pm.RemoveBashPrefixRule(rm)
		}
		for _, s := range sets {
			pm.SetBashPrefixRule(s.prefix, s.level)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"bash_rules": h.bashRulesFromConfigLocked(),
	})
}

// bashRulesFromConfigLocked renders the user bash prefix rules sorted by prefix,
// in the same {tool, level} shape GET /api/permissions reports, so the editor can
// reload from its own save response and show server truth. The caller MUST hold
// h.mu — it reads h.cfg, which every endpoint mutates.
func (h *Handler) bashRulesFromConfigLocked() []map[string]string {
	rules := make([]map[string]string, 0)
	if h.cfg == nil {
		return rules
	}
	for prefix, level := range h.cfg.Ocode.Permissions.Bash.Prefixes {
		if agent.InternalBashPrefix(prefix) {
			// Internal in-root auto-allow keys are not user rules and are not
			// editable; PermissionManager.BashPrefixRules filters them too.
			continue
		}
		rules = append(rules, map[string]string{"tool": prefix, "level": level})
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i]["tool"] < rules[j]["tool"] })
	return rules
}

// HandleGetAutoContinue serves GET /api/config/ocode/autocontinue.
func (h *Handler) HandleGetAutoContinue(w http.ResponseWriter, r *http.Request) {
	enabled := false
	model := ""
	h.mu.Lock()
	if h.cfg != nil {
		enabled = h.cfg.Ocode.AutoContinueEnabled
		model = h.cfg.Ocode.AutoContinueModel
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"enabled": enabled, "model": model})
}

// HandleSetAutoContinue serves PUT /api/config/ocode/autocontinue — the
// /autocontinue on|off|model [name] write path.
func (h *Handler) HandleSetAutoContinue(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled *bool  `json:"enabled"`
		Model   string `json:"model"`
		Clear   bool   `json:"clear"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Enabled == nil && !req.Clear && req.Model == "" {
		writeError(w, http.StatusBadRequest, "nothing to update: provide enabled, model, or clear")
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg == nil {
		writeError(w, http.StatusInternalServerError, "config not loaded")
		return
	}

	if req.Enabled != nil {
		if err := config.SaveAutoContinueEnabled(*req.Enabled); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		h.cfg.Ocode.AutoContinueEnabled = *req.Enabled
	}
	if req.Clear {
		if err := config.SaveAutoContinueModel(""); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		h.cfg.Ocode.AutoContinueModel = ""
	} else if req.Model != "" {
		// Validate the model resolves before persisting (mirrors the TUI's
		// handleAutoContinueModelSub agent.NewClient probe).
		if agent.NewClient(h.cfg, req.Model) == nil {
			writeError(w, http.StatusBadRequest, "unknown provider or missing configuration for model "+req.Model)
			return
		}
		if err := config.SaveAutoContinueModel(req.Model); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		h.cfg.Ocode.AutoContinueModel = req.Model
	}

	// Broadcast a fresh status snapshot so a connected web sidebar's
	// auto-continue row (model + on/off) reflects the change immediately —
	// same reasoning as HandleSetSmallModel's pushStatusSnapshot.
	h.pushStatusSnapshot()

	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": h.cfg.Ocode.AutoContinueEnabled,
		"model":   h.cfg.Ocode.AutoContinueModel,
	})
}

// HandleConnectProvider serves POST /api/auth/connect — the /connect
// provider apikey write path. Stores the credential in auth.json; applies to
// newly built sessions (live agents keep their existing clients until rebuilt,
// same as the TUI before its next client rebuild).
func (h *Handler) HandleConnectProvider(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
		APIKey   string `json:"api_key"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Provider = strings.TrimSpace(req.Provider)
	req.APIKey = strings.TrimSpace(req.APIKey)
	if req.Provider == "" || req.APIKey == "" {
		writeError(w, http.StatusBadRequest, "provider and api_key are required")
		return
	}
	p := auth.FindProvider(req.Provider)
	if p == nil {
		writeError(w, http.StatusBadRequest, "unknown provider: "+req.Provider)
		return
	}
	if err := auth.Set(p.ID, auth.Credential{Kind: auth.KindAPIKey, Key: req.APIKey}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if p.EnvVar != "" {
		os.Setenv(p.EnvVar, req.APIKey)
	}
	masked := req.APIKey
	if len(masked) > 8 {
		masked = masked[:4] + "…" + masked[len(masked)-4:]
	}
	writeJSON(w, http.StatusOK, map[string]string{"provider": p.ID, "key": masked})
}

// ─── /docs knowledge-system endpoints ──────────────────────────────────────

// docsWorkDir resolves the ?project= query against the trust boundary,
// falling back to the server workdir.
func (h *Handler) docsWorkDir(r *http.Request) (string, bool) {
	dir, ok := h.gitProjectDir(r)
	if !ok {
		return "", false
	}
	if dir == "" {
		dir = h.workDir
	}
	return dir, true
}

func (h *Handler) docPromptEnabled() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cfg != nil && h.cfg.Ocode.DocPromptEnabled
}

// HandleDocsStatus serves GET /api/docs/status — the /docs status report.
func (h *Handler) HandleDocsStatus(w http.ResponseWriter, r *http.Request) {
	workDir, ok := h.docsWorkDir(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown project")
		return
	}
	enabled := h.docPromptEnabled()
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": enabled,
		"text":    commandctx.DocsStatus(enabled, workDir),
	})
}

// HandleDocsInit serves POST /api/docs/init?project= — creates the OKF bundle
// marker files (or re-audits an existing bundle). When a new bundle was just
// created, the response carries annotate_prompt which the client should send
// as a normal turn so the context agent classifies existing docs (the TUI
// dispatches it via dispatchContextAgent).
func (h *Handler) HandleDocsInit(w http.ResponseWriter, r *http.Request) {
	workDir, ok := h.docsWorkDir(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown project")
		return
	}
	text, annotatePrompt := commandctx.DocsInit(h.docPromptEnabled(), workDir)
	resp := map[string]any{"result": text}
	if annotatePrompt != "" {
		resp["annotate_prompt"] = annotatePrompt
	}
	writeJSON(w, http.StatusOK, resp)
}

// HandleDocsUpdate serves POST /api/docs/update?project= with body
// {"session_id": "...", "focus": "..."} — queues a forced maintenance pass on
// the named session's live agent (mirroring the TUI's QueueDocMaintenance).
func (h *Handler) HandleDocsUpdate(w http.ResponseWriter, r *http.Request) {
	workDir, ok := h.docsWorkDir(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown project")
		return
	}
	var req struct {
		SessionID string `json:"session_id"`
		Focus     string `json:"focus"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !h.docPromptEnabled() {
		writeError(w, http.StatusConflict, "knowledge system is not enabled. Run /docs on first.")
		return
	}
	if _, detected := knowledge.DetectBundle(workDir); !detected {
		writeJSON(w, http.StatusOK, map[string]any{"result": "No OKF knowledge bundle found. Run /docs init first."})
		return
	}
	h.mu.Lock()
	as := h.agents[req.SessionID]
	h.mu.Unlock()
	if as == nil || as.agent == nil {
		writeError(w, http.StatusNotFound, "no live agent for session "+req.SessionID+" — send a message first, then retry")
		return
	}

	as.agent.QueueDocMaintenance(agent.DocMaintenanceRequest{
		WorkDir: workDir,
		Forced:  true,
		Focus:   req.Focus,
	})

	result := "Maintenance pass queued. Check /docs status for updates."
	if req.Focus != "" {
		result = fmt.Sprintf("Maintenance pass queued with focus: %q. Check /docs status for updates.", req.Focus)
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": result})
}

// HandleDocsCleanup serves POST /api/docs/cleanup?project= with body
// {"confirm": bool} — lists deprecated docs, deleting them only when
// confirm=true (same gate as the TUI's /docs cleanup --yes).
func (h *Handler) HandleDocsCleanup(w http.ResponseWriter, r *http.Request) {
	workDir, ok := h.docsWorkDir(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown project")
		return
	}
	var req struct {
		Confirm bool `json:"confirm"`
	}
	// Empty body is allowed (list-only); malformed JSON is not.
	if r.ContentLength != 0 {
		if err := readBodyJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"result": commandctx.DocsCleanup(workDir, req.Confirm),
	})
}

// applyExtraAllowedPathsToRuntime syncs the tool package's runtime allowlist
// after a paths-config write so web /add-dir takes effect immediately for
// tools executed by the server process (the TUI calls tool.AddExtraAllowedPath
// directly from handleAddDirCmd).
func applyExtraAllowedPathsToRuntime(paths []string) {
	tool.SetPersistentExtraAllowedPaths(paths)
}
