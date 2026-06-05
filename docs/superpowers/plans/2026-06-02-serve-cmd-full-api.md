# Serve Command Full API Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expose every meaningful TUI slash command as a REST endpoint in the `serve` subcommand so the web UI has full feature parity with the TUI.

**Architecture:** New handler files grouped by domain (`handler_config.go`, `handler_permissions.go`, `handler_mcp.go`, `handler_plugins.go`, `handler_usage.go`, `handler_info.go`); session operations (compact, recap, export, title, share, context) added to the existing `handler.go`; undo/redo added to `handler_files.go`; two sync wrappers (`Compact`, `Recap`) added to `internal/agent/agent.go`; all routes registered in `server.go`.

**Tech Stack:** Go stdlib `net/http`, existing packages: `internal/agent`, `internal/config`, `internal/session`, `internal/snapshot`, `internal/usage`, `internal/github`, `internal/skill`, `internal/commands`, `internal/plugins`

---

## File Structure

**New files:**
- `internal/server/handler_config.go` — GET/PUT model, small-model, advisor, agent mode
- `internal/server/handler_permissions.go` — GET/POST permissions, GET/PUT yolo
- `internal/server/handler_mcp.go` — GET list, PUT enable/disable
- `internal/server/handler_plugins.go` — GET list/info, PUT enable/disable, POST install, DELETE remove
- `internal/server/handler_usage.go` — GET usage summary
- `internal/server/handler_info.go` — GET skills, GET commands, GET github PR/issues, POST init, POST undo, POST redo

**Modified files:**
- `internal/agent/agent.go` — add exported `Compact(messages []Message) (CompactResult, bool)` and `Recap(messages []Message) string`
- `internal/server/handler.go` — add session compact, recap, export, export-claude, share, title, context endpoints
- `internal/server/server.go` — register all new routes

---

## Task 1: Add Sync Agent Wrappers (Compact + Recap)

**Files:**
- Modify: `internal/agent/agent.go` (after line ~702, near RecapAsync)

- [ ] **Step 1: Add exported sync wrappers**

Open `internal/agent/agent.go`. After the `RecapAsync` function (around line 702), add:

```go
// Compact runs context compaction synchronously and returns the result.
// Returns (result, false) if compaction is disabled in config.
func (a *Agent) Compact(messages []Message) (CompactResult, bool) {
	rt := a.resolveCompactRuntime(true)
	if !rt.Enabled {
		return CompactResult{}, false
	}
	return a.runCompact(messages, rt), true
}

// Recap generates a conversation recap synchronously using the small model.
func (a *Agent) Recap(messages []Message) string {
	return a.runRecap(messages)
}
```

- [ ] **Step 2: Build to verify**

```bash
go build ./internal/agent/
```
Expected: no output (clean build).

- [ ] **Step 3: Commit**

```bash
git add internal/agent/agent.go
git commit -m "feat(agent): add exported sync Compact and Recap methods"
```

---

## Task 2: Session Operations — Compact, Recap, Export, Share, Title, Context

**Files:**
- Modify: `internal/server/handler.go`

- [ ] **Step 1: Add helpers and session operation handlers**

Append to `internal/server/handler.go`:

```go
// ── helpers ────────────────────────────────────────────────────────────────

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

// ── compact ────────────────────────────────────────────────────────────────

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

	// Splice the summary into the message list.
	before := as.messages[:result.ReplaceFrom]
	after := as.messages[result.ReplaceTo:]
	compacted := make([]agent.Message, 0, len(before)+1+len(after))
	compacted = append(compacted, before...)
	compacted = append(compacted, result.Summary)
	compacted = append(compacted, after...)
	as.messages = compacted

	_ = session.Save(id, "", as.messages, nil)

	writeJSON(w, http.StatusOK, map[string]any{
		"original_len": result.OriginalLen,
		"compacted_len": len(as.messages),
	})
}

// ── recap ──────────────────────────────────────────────────────────────────

func (h *Handler) HandleRecapSession(w http.ResponseWriter, r *http.Request, id string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	as, err := h.getOrCreateAgentSession(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if len(as.messages) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "no messages to recap")
		return
	}

	// Run recap outside the lock to avoid blocking other requests.
	msgs := make([]agent.Message, len(as.messages))
	copy(msgs, as.messages)
	h.mu.Unlock()

	text := as.agent.Recap(msgs)

	h.mu.Lock()
	writeJSON(w, http.StatusOK, map[string]string{"recap": text})
}

// ── export ─────────────────────────────────────────────────────────────────

func (h *Handler) HandleExportSession(w http.ResponseWriter, r *http.Request, id string) {
	s, err := session.Load(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	var b strings.Builder
	for _, msg := range s.Messages {
		if msg.Role == "user" || msg.Role == "assistant" {
			role := strings.Title(msg.Role)
			b.WriteString(fmt.Sprintf("## %s\n\n%s\n\n", role, msg.Content))
		}
	}

	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="ocode_export_%s.md"`, id))
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, b.String())
}

// ── export-claude ──────────────────────────────────────────────────────────

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

// ── share ──────────────────────────────────────────────────────────────────

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
	fmt.Fprintf(&b, "Session ID: `%s`  \n", id)
	fmt.Fprintf(&b, "Created: %s  \n\n", s.CreatedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "---\n\n")

	for _, msg := range s.Messages {
		if msg.Role != "user" && msg.Role != "assistant" {
			continue
		}
		if msg.Content == "" {
			continue
		}
		role := strings.Title(msg.Role)
		fmt.Fprintf(&b, "**%s:** %s\n\n", role, msg.Content)
	}

	writeJSON(w, http.StatusOK, map[string]string{"markdown": b.String()})
}

// ── title ──────────────────────────────────────────────────────────────────

func (h *Handler) HandleSetSessionTitle(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Title string `json:"title"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
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

// ── context ────────────────────────────────────────────────────────────────

func (h *Handler) HandleSessionContext(w http.ResponseWriter, r *http.Request, id string) {
	s, err := session.Load(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	var totalChars int
	for _, msg := range s.Messages {
		totalChars += len(msg.Content)
		totalChars += len(msg.ReasoningContent)
		for _, tc := range msg.ToolCalls {
			totalChars += len(tc.Function.Arguments)
		}
	}
	estimatedTokens := totalChars / 4

	writeJSON(w, http.StatusOK, map[string]any{
		"session_id":       id,
		"message_count":    len(s.Messages),
		"estimated_tokens": estimatedTokens,
	})
}
```

- [ ] **Step 2: Add required imports to handler.go**

In `internal/server/handler.go`, ensure the import block includes `"strings"` and `"time"`:

```go
import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/tool"
)
```

- [ ] **Step 3: Build**

```bash
go build ./internal/server/
```
Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add internal/server/handler.go
git commit -m "feat(server): session compact, recap, export, share, title, context endpoints"
```

---

## Task 3: Undo / Redo Endpoints

**Files:**
- Modify: `internal/server/handler_files.go`

- [ ] **Step 1: Add undo/redo handlers**

Append to `internal/server/handler_files.go`:

```go
func (h *Handler) HandleUndo(w http.ResponseWriter, r *http.Request) {
	path, err := snapshot.Undo()
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": path, "action": "undo"})
}

func (h *Handler) HandleRedo(w http.ResponseWriter, r *http.Request) {
	path, err := snapshot.Redo()
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": path, "action": "redo"})
}
```

- [ ] **Step 2: Add import to handler_files.go**

Ensure `"github.com/u007/ocode/internal/snapshot"` is in the import block of `handler_files.go`.

- [ ] **Step 3: Build**

```bash
go build ./internal/server/
```

- [ ] **Step 4: Commit**

```bash
git add internal/server/handler_files.go
git commit -m "feat(server): undo/redo file change endpoints"
```

---

## Task 4: Config Endpoints (model, small-model, advisor, agent)

**Files:**
- Create: `internal/server/handler_config.go`

- [ ] **Step 1: Write handler_config.go**

```go
package server

import (
	"net/http"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
)

// ── model ──────────────────────────────────────────────────────────────────

func (h *Handler) HandleGetModel(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	model := ""
	if h.cfg != nil {
		model = h.cfg.Model
	}
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

// ── small-model ────────────────────────────────────────────────────────────

func (h *Handler) HandleGetSmallModel(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	current := ""
	if h.cfg != nil {
		current = h.cfg.Ocode.SmallModel
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"model":    current,
		"priority": agent.SmallModelPriority,
	})
}

func (h *Handler) HandleSetSmallModel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string `json:"model"` // "auto" clears override
	}
	if err := readBodyJSON(r, &req); err != nil || req.Model == "" {
		writeError(w, http.StatusBadRequest, "model is required (use \"auto\" to clear)")
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

// ── advisor ────────────────────────────────────────────────────────────────

func (h *Handler) HandleGetAdvisor(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	model := ""
	if h.cfg != nil {
		model = h.cfg.Ocode.AdvisorModel
	}
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
	h.cfg.Ocode.AdvisorModel = req.Model
	writeJSON(w, http.StatusOK, map[string]string{"model": req.Model})
}

// ── agent ──────────────────────────────────────────────────────────────────

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
```

- [ ] **Step 2: Build**

```bash
go build ./internal/server/
```

- [ ] **Step 3: Commit**

```bash
git add internal/server/handler_config.go
git commit -m "feat(server): config endpoints for model, small-model, advisor, agent"
```

---

## Task 5: Permissions Endpoints

**Files:**
- Create: `internal/server/handler_permissions.go`

- [ ] **Step 1: Write handler_permissions.go**

```go
package server

import (
	"net/http"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
)

func (h *Handler) HandleGetPermissions(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.cfg == nil {
		writeError(w, http.StatusInternalServerError, "config not loaded")
		return
	}

	pm := agent.NewPermissionManager()
	pm.LoadFromOcode(h.cfg.Ocode.Permissions)

	type ruleEntry struct {
		Tool  string `json:"tool"`
		Level string `json:"level"`
	}
	var rules []ruleEntry
	for tool, level := range pm.Rules() {
		rules = append(rules, ruleEntry{Tool: tool, Level: string(level)})
	}
	var bashRules []ruleEntry
	for prefix, level := range pm.BashPrefixRules() {
		bashRules = append(bashRules, ruleEntry{Tool: prefix, Level: string(level)})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"mode":       string(pm.Mode()),
		"auto_allow": pm.AutoPermissionEnabled(),
		"rules":      rules,
		"bash_rules": bashRules,
	})
}

func (h *Handler) HandleSetPermission(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Tool  string `json:"tool"`  // "bash:prefix" for bash prefix rules
		Level string `json:"level"` // "allow", "deny", "ask"
	}
	if err := readBodyJSON(r, &req); err != nil || req.Tool == "" || req.Level == "" {
		writeError(w, http.StatusBadRequest, "tool and level are required")
		return
	}

	level := agent.PermissionLevel(req.Level)
	if level != agent.PermAllow && level != agent.PermDeny && level != agent.PermAsk {
		writeError(w, http.StatusBadRequest, "level must be allow, deny, or ask")
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.cfg == nil {
		writeError(w, http.StatusInternalServerError, "config not loaded")
		return
	}

	perms := h.cfg.Ocode.Permissions
	if perms.Tools == nil {
		perms.Tools = map[string]string{}
	}
	perms.Tools[req.Tool] = req.Level
	h.cfg.Ocode.Permissions = perms
	if err := config.SaveOcodePermissions(perms); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Apply to all live agent sessions.
	for _, as := range h.agents {
		if pm := as.agent.Permissions(); pm != nil {
			pm.SetRule(req.Tool, level)
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"tool": req.Tool, "level": req.Level})
}

func (h *Handler) HandleGetYolo(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	enabled := false
	for _, as := range h.agents {
		if pm := as.agent.Permissions(); pm != nil {
			enabled = pm.IsYolo()
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"yolo": enabled})
}

func (h *Handler) HandleSetYolo(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	for _, as := range h.agents {
		if pm := as.agent.Permissions(); pm != nil {
			pm.SetYolo(req.Enabled)
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"yolo": req.Enabled})
}
```

**Note:** `PermissionManager` needs `IsYolo()` and `SetYolo(bool)` methods. Check if they exist:

```bash
grep -n "IsYolo\|SetYolo\|yolo" /Users/james/www/ocode/internal/agent/permissions.go | head -10
```

If not present, add to `internal/agent/permissions.go`:

```go
func (pm *PermissionManager) IsYolo() bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.yolo
}

func (pm *PermissionManager) SetYolo(enabled bool) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.yolo = enabled
}
```

And add `yolo bool` to the `PermissionManager` struct if not present.

- [ ] **Step 2: Build**

```bash
go build ./internal/server/
```

- [ ] **Step 3: Commit**

```bash
git add internal/server/handler_permissions.go internal/agent/permissions.go
git commit -m "feat(server): permissions and yolo endpoints"
```

---

## Task 6: MCP Endpoints

**Files:**
- Create: `internal/server/handler_mcp.go`

- [ ] **Step 1: Write handler_mcp.go**

```go
package server

import (
	"net/http"

	"github.com/u007/ocode/internal/config"
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
```

- [ ] **Step 2: Build**

```bash
go build ./internal/server/
```

- [ ] **Step 3: Commit**

```bash
git add internal/server/handler_mcp.go
git commit -m "feat(server): MCP list/enable/disable endpoints"
```

---

## Task 7: Plugin Endpoints

**Files:**
- Create: `internal/server/handler_plugins.go`

- [ ] **Step 1: Write handler_plugins.go**

```go
package server

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/plugins"
)

func (h *Handler) HandleListPlugins(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

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

	out := make([]pluginEntry, 0, len(h.cfg.Plugins))
	for name, pc := range h.cfg.Plugins {
		out = append(out, pluginEntry{
			Name:        name,
			Source:      pc.Source,
			Dir:         pc.Dir,
			Enabled:     pc.Enabled,
			Description: descByName[name],
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) HandleGetPlugin(w http.ResponseWriter, r *http.Request, name string) {
	h.mu.Lock()
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
		Source string `json:"source"` // github.com/user/repo[@ref]
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
```

- [ ] **Step 2: Build**

```bash
go build ./internal/server/
```

- [ ] **Step 3: Commit**

```bash
git add internal/server/handler_plugins.go
git commit -m "feat(server): plugin list/info/enable/disable/install/remove endpoints"
```

---

## Task 8: Usage Endpoint

**Files:**
- Create: `internal/server/handler_usage.go`

- [ ] **Step 1: Write handler_usage.go**

```go
package server

import (
	"net/http"

	"github.com/u007/ocode/internal/usage"
)

func (h *Handler) HandleGetUsage(w http.ResponseWriter, r *http.Request) {
	rangeLabel := r.URL.Query().Get("range")
	if rangeLabel == "" {
		rangeLabel = "day"
	}

	// Map shorthand labels to DateRange entries.
	labelMap := map[string]string{
		"hour":         "Last hour",
		"day":          "Today",
		"week":         "This week (last 7 days)",
		"month":        "This month (last 30 days)",
		"last-month":   "Last month",
		"last-3-month": "Last 3 months",
		"all":          "All time",
	}
	fullLabel, ok := labelMap[rangeLabel]
	if !ok {
		writeError(w, http.StatusBadRequest, "range must be one of: hour, day, week, month, last-month, last-3-month, all")
		return
	}

	var from, to interface{ IsZero() bool }
	var records []usage.Record
	for _, dr := range usage.DateRanges {
		if dr.Label == fullLabel {
			f, t := dr.From()
			recs, err := usage.Query(f, t)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			records = recs
			_ = from
			_ = to
			break
		}
	}

	summary := usage.Summarize(records)
	writeJSON(w, http.StatusOK, summary)
}
```

- [ ] **Step 2: Fix the unused variable pattern** (simplify the loop):

```go
package server

import (
	"net/http"

	"github.com/u007/ocode/internal/usage"
)

func (h *Handler) HandleGetUsage(w http.ResponseWriter, r *http.Request) {
	rangeLabel := r.URL.Query().Get("range")
	if rangeLabel == "" {
		rangeLabel = "day"
	}

	labelMap := map[string]string{
		"hour":         "Last hour",
		"day":          "Today",
		"week":         "This week (last 7 days)",
		"month":        "This month (last 30 days)",
		"last-month":   "Last month",
		"last-3-month": "Last 3 months",
		"all":          "All time",
	}
	fullLabel, ok := labelMap[rangeLabel]
	if !ok {
		writeError(w, http.StatusBadRequest, "range must be one of: hour, day, week, month, last-month, last-3-month, all")
		return
	}

	var records []usage.Record
	for _, dr := range usage.DateRanges {
		if dr.Label == fullLabel {
			f, t := dr.From()
			recs, err := usage.Query(f, t)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			records = recs
			break
		}
	}

	writeJSON(w, http.StatusOK, usage.Summarize(records))
}
```

- [ ] **Step 3: Build**

```bash
go build ./internal/server/
```

- [ ] **Step 4: Commit**

```bash
git add internal/server/handler_usage.go
git commit -m "feat(server): usage summary endpoint"
```

---

## Task 9: Info Endpoints (skills, commands, github, init)

**Files:**
- Create: `internal/server/handler_info.go`

- [ ] **Step 1: Write handler_info.go**

```go
package server

import (
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/u007/ocode/internal/commands"
	"github.com/u007/ocode/internal/github"
	"github.com/u007/ocode/internal/skill"
)

// ── skills ─────────────────────────────────────────────────────────────────

func (h *Handler) HandleListSkills(w http.ResponseWriter, r *http.Request) {
	statuses, err := skill.GetSkillStatus()
	if err != nil {
		// Fall back to basic list if status check fails.
		basic := skill.LoadSkills()
		type entry struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		out := make([]entry, len(basic))
		for i, s := range basic {
			out[i] = entry{Name: s.Name, Description: s.Description}
		}
		writeJSON(w, http.StatusOK, out)
		return
	}

	type entry struct {
		Name   string `json:"name"`
		Status string `json:"status"`
		Source string `json:"source,omitempty"`
	}
	out := make([]entry, len(statuses))
	for i, s := range statuses {
		out[i] = entry{Name: s.Name, Status: string(s.Status), Source: s.Source}
	}
	writeJSON(w, http.StatusOK, out)
}

// ── commands ───────────────────────────────────────────────────────────────

func (h *Handler) HandleListCommands(w http.ResponseWriter, r *http.Request) {
	cmds := commands.LoadCommands(nil)
	type entry struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
	}
	out := make([]entry, len(cmds))
	for i, c := range cmds {
		out[i] = entry{Name: c.Name, Description: c.Description}
	}
	writeJSON(w, http.StatusOK, out)
}

// ── github ─────────────────────────────────────────────────────────────────

func (h *Handler) HandleGitHubPR(w http.ResponseWriter, r *http.Request, owner, repo string, prNumber int) {
	pr, err := github.GetPR(owner, repo, prNumber)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	diff, _ := github.GetPRDiff(owner, repo, prNumber)
	writeJSON(w, http.StatusOK, map[string]any{
		"pr":   pr,
		"diff": diff,
	})
}

func (h *Handler) HandleGitHubIssues(w http.ResponseWriter, r *http.Request, owner, repo string) {
	state := r.URL.Query().Get("state")
	if state == "" {
		state = "open"
	}
	issues, err := github.ListIssues(owner, repo, state)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, issues)
}

// ── init ───────────────────────────────────────────────────────────────────

func (h *Handler) HandleInit(w http.ResponseWriter, r *http.Request) {
	// Generate a minimal AGENTS.md by scanning the working directory.
	// This mirrors what /init does in the TUI (bootstraps the file for
	// the user to refine; the agent fills in the details on first use).
	const agentsFile = "AGENTS.md"

	if _, err := os.Stat(agentsFile); err == nil {
		writeJSON(w, http.StatusOK, map[string]string{
			"path":   agentsFile,
			"status": "already exists",
		})
		return
	}

	content := fmt.Sprintf("# Project\n\n> Auto-generated by ocode /init. Edit this file to describe your project.\n\n## Working Directory\n\n%s\n", mustGetwd())
	if err := os.WriteFile(agentsFile, []byte(content), 0644); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"path": agentsFile, "status": "created"})
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

// ── route helpers ──────────────────────────────────────────────────────────

// parseGitHubPRRoute extracts owner/repo/number from path like
// /api/github/pr/{owner}/{repo}/{number}
func parseGitHubPRRoute(r *http.Request) (owner, repo string, number int, ok bool) {
	owner = r.PathValue("owner")
	repo = r.PathValue("repo")
	numStr := r.PathValue("number")
	n, err := strconv.Atoi(numStr)
	if owner == "" || repo == "" || err != nil {
		return "", "", 0, false
	}
	return owner, repo, n, true
}
```

- [ ] **Step 2: Build**

```bash
go build ./internal/server/
```

- [ ] **Step 3: Commit**

```bash
git add internal/server/handler_info.go
git commit -m "feat(server): skills, commands, github, init endpoints"
```

---

## Task 10: Register All Routes in server.go

**Files:**
- Modify: `internal/server/server.go`

- [ ] **Step 1: Update registerRoutes and WithCORS in server.go**

Replace the `registerRoutes` method with:

```go
func (s *Server) registerRoutes() {
	// Chat
	s.mux.HandleFunc("POST /api/chat", s.authMiddleware(s.handleChat))
	s.mux.HandleFunc("GET /api/chat/stream", s.authMiddleware(s.handleChatStream))

	// Sessions
	s.mux.HandleFunc("GET /api/sessions", s.authMiddleware(s.handleListSessions))
	s.mux.HandleFunc("GET /api/sessions/{id}", s.authMiddleware(s.handleGetSession))
	s.mux.HandleFunc("POST /api/sessions/{id}/message", s.authMiddleware(s.handleSendMessage))
	s.mux.HandleFunc("POST /api/sessions/{id}/compact", s.authMiddleware(s.handleCompactSession))
	s.mux.HandleFunc("GET /api/sessions/{id}/recap", s.authMiddleware(s.handleRecapSession))
	s.mux.HandleFunc("GET /api/sessions/{id}/export", s.authMiddleware(s.handleExportSession))
	s.mux.HandleFunc("GET /api/sessions/{id}/export-claude", s.authMiddleware(s.handleExportClaudeSession))
	s.mux.HandleFunc("GET /api/sessions/{id}/share", s.authMiddleware(s.handleShareSession))
	s.mux.HandleFunc("PUT /api/sessions/{id}/title", s.authMiddleware(s.handleSetSessionTitle))
	s.mux.HandleFunc("GET /api/sessions/{id}/context", s.authMiddleware(s.handleSessionContext))

	// Files
	s.mux.HandleFunc("GET /api/files/tree", s.authMiddleware(s.handleFileTree))
	s.mux.HandleFunc("GET /api/files/content", s.authMiddleware(s.handleFileContent))
	s.mux.HandleFunc("POST /api/files/undo", s.authMiddleware(s.handleUndo))
	s.mux.HandleFunc("POST /api/files/redo", s.authMiddleware(s.handleRedo))

	// Config
	s.mux.HandleFunc("GET /api/models", s.authMiddleware(s.handleListModels))
	s.mux.HandleFunc("GET /api/config/model", s.authMiddleware(s.handleGetModel))
	s.mux.HandleFunc("PUT /api/config/model", s.authMiddleware(s.handleSetModel))
	s.mux.HandleFunc("GET /api/config/small-model", s.authMiddleware(s.handleGetSmallModel))
	s.mux.HandleFunc("PUT /api/config/small-model", s.authMiddleware(s.handleSetSmallModel))
	s.mux.HandleFunc("GET /api/config/advisor", s.authMiddleware(s.handleGetAdvisor))
	s.mux.HandleFunc("PUT /api/config/advisor", s.authMiddleware(s.handleSetAdvisor))
	s.mux.HandleFunc("GET /api/config/agents", s.authMiddleware(s.handleListAgents))
	s.mux.HandleFunc("PUT /api/config/agent", s.authMiddleware(s.handleSetAgent))

	// Permissions
	s.mux.HandleFunc("GET /api/permissions", s.authMiddleware(s.handleGetPermissions))
	s.mux.HandleFunc("POST /api/permissions", s.authMiddleware(s.handleSetPermission))
	s.mux.HandleFunc("GET /api/permissions/yolo", s.authMiddleware(s.handleGetYolo))
	s.mux.HandleFunc("PUT /api/permissions/yolo", s.authMiddleware(s.handleSetYolo))

	// MCP
	s.mux.HandleFunc("GET /api/mcp", s.authMiddleware(s.handleListMCP))
	s.mux.HandleFunc("PUT /api/mcp/{name}/enable", s.authMiddleware(s.handleEnableMCP))
	s.mux.HandleFunc("PUT /api/mcp/{name}/disable", s.authMiddleware(s.handleDisableMCP))

	// Plugins
	s.mux.HandleFunc("GET /api/plugins", s.authMiddleware(s.handleListPlugins))
	s.mux.HandleFunc("GET /api/plugins/{name}", s.authMiddleware(s.handleGetPlugin))
	s.mux.HandleFunc("PUT /api/plugins/{name}/enable", s.authMiddleware(s.handleEnablePlugin))
	s.mux.HandleFunc("PUT /api/plugins/{name}/disable", s.authMiddleware(s.handleDisablePlugin))
	s.mux.HandleFunc("POST /api/plugins", s.authMiddleware(s.handleInstallPlugin))
	s.mux.HandleFunc("DELETE /api/plugins/{name}", s.authMiddleware(s.handleRemovePlugin))

	// Usage
	s.mux.HandleFunc("GET /api/usage", s.authMiddleware(s.handleGetUsage))

	// Info
	s.mux.HandleFunc("GET /api/skills", s.authMiddleware(s.handleListSkills))
	s.mux.HandleFunc("GET /api/commands", s.authMiddleware(s.handleListCommands))
	s.mux.HandleFunc("GET /api/github/pr/{owner}/{repo}/{number}", s.authMiddleware(s.handleGitHubPR))
	s.mux.HandleFunc("GET /api/github/issues/{owner}/{repo}", s.authMiddleware(s.handleGitHubIssues))
	s.mux.HandleFunc("POST /api/init", s.authMiddleware(s.handleInit))

	// Git
	s.mux.HandleFunc("GET /api/git/status", s.authMiddleware(s.handleGitStatus))

	// Web UI
	s.mux.Handle("/", spaHandler(s.webFS))
}
```

- [ ] **Step 2: Add shim methods in server.go** that route to handler methods:

```go
// ── session shims ──────────────────────────────────────────────────────────
func (s *Server) handleCompactSession(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleCompactSession(w, r, r.PathValue("id"))
}
func (s *Server) handleRecapSession(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleRecapSession(w, r, r.PathValue("id"))
}
func (s *Server) handleExportSession(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleExportSession(w, r, r.PathValue("id"))
}
func (s *Server) handleExportClaudeSession(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleExportClaudeSession(w, r, r.PathValue("id"))
}
func (s *Server) handleShareSession(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleShareSession(w, r, r.PathValue("id"))
}
func (s *Server) handleSetSessionTitle(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetSessionTitle(w, r, r.PathValue("id"))
}
func (s *Server) handleSessionContext(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSessionContext(w, r, r.PathValue("id"))
}

// ── file shims ─────────────────────────────────────────────────────────────
func (s *Server) handleUndo(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleUndo(w, r)
}
func (s *Server) handleRedo(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleRedo(w, r)
}

// ── config shims ───────────────────────────────────────────────────────────
func (s *Server) handleGetModel(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetModel(w, r)
}
func (s *Server) handleSetModel(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetModel(w, r)
}
func (s *Server) handleGetSmallModel(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetSmallModel(w, r)
}
func (s *Server) handleSetSmallModel(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetSmallModel(w, r)
}
func (s *Server) handleGetAdvisor(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetAdvisor(w, r)
}
func (s *Server) handleSetAdvisor(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetAdvisor(w, r)
}
func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleListAgents(w, r)
}
func (s *Server) handleSetAgent(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetAgent(w, r)
}

// ── permissions shims ──────────────────────────────────────────────────────
func (s *Server) handleGetPermissions(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetPermissions(w, r)
}
func (s *Server) handleSetPermission(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetPermission(w, r)
}
func (s *Server) handleGetYolo(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetYolo(w, r)
}
func (s *Server) handleSetYolo(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetYolo(w, r)
}

// ── MCP shims ──────────────────────────────────────────────────────────────
func (s *Server) handleListMCP(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleListMCP(w, r)
}
func (s *Server) handleEnableMCP(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetMCPEnabled(w, r, r.PathValue("name"), true)
}
func (s *Server) handleDisableMCP(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetMCPEnabled(w, r, r.PathValue("name"), false)
}

// ── plugin shims ───────────────────────────────────────────────────────────
func (s *Server) handleListPlugins(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleListPlugins(w, r)
}
func (s *Server) handleGetPlugin(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetPlugin(w, r, r.PathValue("name"))
}
func (s *Server) handleEnablePlugin(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetPluginEnabled(w, r, r.PathValue("name"), true)
}
func (s *Server) handleDisablePlugin(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetPluginEnabled(w, r, r.PathValue("name"), false)
}
func (s *Server) handleInstallPlugin(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleInstallPlugin(w, r)
}
func (s *Server) handleRemovePlugin(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleRemovePlugin(w, r, r.PathValue("name"))
}

// ── usage shims ────────────────────────────────────────────────────────────
func (s *Server) handleGetUsage(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetUsage(w, r)
}

// ── info shims ─────────────────────────────────────────────────────────────
func (s *Server) handleListSkills(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleListSkills(w, r)
}
func (s *Server) handleListCommands(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleListCommands(w, r)
}
func (s *Server) handleGitHubPR(w http.ResponseWriter, r *http.Request) {
	owner, repo, number, ok := parseGitHubPRRoute(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid github PR path")
		return
	}
	s.handler.HandleGitHubPR(w, r, owner, repo, number)
}
func (s *Server) handleGitHubIssues(w http.ResponseWriter, r *http.Request) {
	owner := r.PathValue("owner")
	repo := r.PathValue("repo")
	if owner == "" || repo == "" {
		writeError(w, http.StatusBadRequest, "owner and repo are required")
		return
	}
	s.handler.HandleGitHubIssues(w, r, owner, repo)
}
func (s *Server) handleInit(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleInit(w, r)
}
```

- [ ] **Step 3: Also update WithCORS to include all new routes** (replace the WithCORS method body to wrap the new mux instead of re-registering each route individually):

```go
func (s *Server) WithCORS() *Server {
	original := s.mux
	wrapped := http.NewServeMux()
	wrapped.Handle("/", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		original.ServeHTTP(w, r)
	}))
	s.mux = wrapped
	return s
}
```

- [ ] **Step 4: Check for any missing yolo methods on PermissionManager**

```bash
grep -n "IsYolo\|SetYolo\|yolo\b" /Users/james/www/ocode/internal/agent/permissions.go | head -10
```

If `IsYolo` / `SetYolo` are not found, add them. Find the `PermissionManager` struct and add a `yolo bool` field, then add the methods shown in Task 5.

- [ ] **Step 5: Full build**

```bash
go build ./...
```
Expected: no output.

- [ ] **Step 6: Run server tests if any**

```bash
go test ./internal/server/ -v 2>&1 | tail -20
```

- [ ] **Step 7: Commit**

```bash
git add internal/server/server.go
git commit -m "feat(server): register all new API routes"
```

---

## Task 11: Smoke Test the New Endpoints

- [ ] **Step 1: Start the server in the background**

```bash
go run . serve --port 4097 &
SERVER_PID=$!
sleep 1
```

- [ ] **Step 2: Test a sampling of endpoints**

```bash
# Skills list
curl -s http://localhost:4097/api/skills | head -c 200

# Commands list
curl -s http://localhost:4097/api/commands | head -c 200

# Usage (day)
curl -s "http://localhost:4097/api/usage?range=day" | head -c 200

# Config model
curl -s http://localhost:4097/api/config/model | head -c 100

# Permissions
curl -s http://localhost:4097/api/permissions | head -c 200

# MCP
curl -s http://localhost:4097/api/mcp | head -c 200

# Plugins
curl -s http://localhost:4097/api/plugins | head -c 200
```

Each should return a JSON response (not an HTML error page).

- [ ] **Step 3: Stop server**

```bash
kill $SERVER_PID
```

- [ ] **Step 4: Final commit if any fixes were needed**

```bash
git add -p
git commit -m "fix(server): smoke-test fixes"
```

---

## Self-Review

**Spec coverage check:**

| Requirement | Task |
|---|---|
| Compact session | Task 2 |
| Recap session | Task 2 |
| Export markdown | Task 2 |
| Export Claude JSONL | Task 2 |
| Share session | Task 2 |
| Set title | Task 2 |
| Context token info | Task 2 |
| Undo / Redo | Task 3 |
| GET/PUT model | Task 4 |
| GET/PUT small-model | Task 4 |
| GET/PUT advisor | Task 4 |
| GET agents / PUT agent | Task 4 |
| GET/POST permissions | Task 5 |
| GET/PUT yolo | Task 5 |
| GET/enable/disable MCP | Task 6 |
| GET/info/enable/disable/install/remove plugins | Task 7 |
| GET usage | Task 8 |
| GET skills | Task 9 |
| GET commands | Task 9 |
| GET GitHub PR / issues | Task 9 |
| POST init | Task 9 |
| Route registration | Task 10 |
| Sync Compact/Recap on agent | Task 1 |

**Gaps:** `/mcp-auth` (OAuth browser flow — server-side equivalent needs external OAuth redirect, skipped intentionally). `/export-claude` requires `session.AppendClaudeSession` which is already exported.

**Placeholder scan:** None found — all steps contain complete code.

**Type consistency:** `agentSession`, `Handler`, `writeJSON`, `writeError`, `readBodyJSON` used consistently throughout. `agent.PermissionLevel`, `agent.PermAllow/PermDeny/PermAsk`, `agent.NewPermissionManager()`, `agent.FindAgentSpec()` all exist in the agent package.
