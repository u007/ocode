# Configuration API Backend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add REST GET/PUT endpoints for every `OcodeConfig` field that currently has no HTTP API, and extend two existing endpoints (Advisor, Mask/Redaction) to cover their remaining subfields, per the field→group→endpoint table in `docs/superpowers/specs/2026-08-11-configuration-ui-design.md` section 3. This is Plan 1 of 2 for the Configuration UI feature; Plan 2 (frontend Settings tab) consumes these endpoints and is written separately once this plan is implemented and reviewed.

**Architecture:** Every task follows the existing two-layer handler pattern already used by every config route in this codebase: a `Handle...` method on `*Handler` in `internal/server/handler_config.go` holds the logic, a thin `*Server` wrapper method in `internal/server/server.go` delegates to it, and two `s.mux.HandleFunc("METHOD /path", s.authMiddleware(s.handle...))` lines register the route. Every write goes through `internal/config`'s `withOcodeConfigLock(func(cfg *OcodeConfig) error {...})` helper (cross-process lock + fresh load-from-disk + mutate + atomic write) — **never** save a stale in-memory snapshot, since concurrent ocode sessions can write the same file.

**Tech Stack:** Go stdlib `net/http`, `encoding/json`; tests use stdlib `testing` + `net/http/httptest` (no testify, no mocking framework — this codebase doesn't use either for handler tests).

## Global Constraints

- Every new config mutation MUST go through `config.withOcodeConfigLock` (directly, or via a new `config.SaveOcode...` setter that itself wraps every mutation in `withOcodeConfigLock`). Never call `config.SaveOcodeConfig` with a hand-built or previously-loaded `*OcodeConfig` — that reintroduces the concurrent-write clobber bug this pattern exists to prevent.
- All new JSON field names are `snake_case`, matching every existing field in `OcodeConfig`'s JSON representation.
- Handler methods live in `internal/server/handler_config.go`; thin `*Server` wrappers and route registrations live in `internal/server/server.go`, inserted into the existing `// Config` route block (`server.go:158-185`).
- Every new/extended endpoint is wrapped in `s.authMiddleware(...)`, exactly like every other `/api/config/*` route.
- Tests use the `testHandlerWithConfig(t *testing.T) *Handler` constructor pattern from `internal/server/handler_tui_status_test.go:9-21` (build `*Handler`, set `h.cfg` directly under `h.mu.Lock()`), call the `Handle...` method directly with `httptest.NewRecorder()`/`httptest.NewRequest(...)` (bypassing the mux), and assert on `w.Code` + `json.Unmarshal(w.Body.Bytes(), &target)`.
- No new third-party dependencies.
- New endpoints that map to a struct field which already has full JSON struct tags (e.g. `CompactConfig`, `TUIConfig`, `ImageGenConfig`) use those tags as-is for the request/response body — do not invent different key names.
- New endpoints for scalar `OcodeConfig` fields with no struct tag (they're mapped by hand in `writeOcodeConfigFile`/`loadOcodeConfigFile`) use the JSON key names already established by that hand-mapping, confirmed by prior research: `RecapModel`→`recap_model`, `RecapModelEnabled`→`recap_model_enabled`, `CommitMsgModel`→`commit_msg_model`, `CommitMsgPrompt`→`commit_msg_prompt`, `Editor`→`editor`, `EditorMode`→`editor_mode`, `IDEMode`→`ide_mode`, `ExtraAllowedPaths`→`extra_allowed_paths`, `MemoryEnabled`→`memory_enabled`, `DocPromptEnabled`→`doc_prompt_enabled`.

---

### Task 1: Recap settings endpoint

**Files:**
- Modify: `internal/config/ocodeconfig.go` (add `SaveOcodeRecapConfig`, near the other `Save*` setters around line 1358)
- Modify: `internal/server/handler_config.go` (add `HandleGetRecapConfig` / `HandleSetRecapConfig`)
- Modify: `internal/server/server.go` (add wrapper methods + route registration in the `// Config` block, `server.go:158-185`)
- Test: `internal/server/handler_config_test.go` (new file — no `handler_config_test.go` exists yet; create it, package `server`, following the imports/style of `internal/server/handler_tui_status_test.go:1-28`)

**Interfaces:**
- Produces: `config.SaveOcodeRecapConfig(model string, enabled bool, timeoutSeconds int) error`; routes `GET/PUT /api/config/ocode/recap`.

- [x] **Step 1: Write the failing test**

```go
// internal/server/handler_config_test.go
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

func testConfigHandler(t *testing.T) *Handler {
	t.Helper()
	h := NewHandler()
	h.mu.Lock()
	h.cfg = &config.Config{
		Ocode: config.OcodeConfig{},
	}
	h.mu.Unlock()
	return h
}

func TestHandleGetRecapConfigDefaults(t *testing.T) {
	h := testConfigHandler(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/config/ocode/recap", nil)
	h.HandleGetRecapConfig(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp struct {
		RecapModel          string `json:"recap_model"`
		RecapModelEnabled   bool   `json:"recap_model_enabled"`
		RecapTimeoutSeconds int    `json:"recap_timeout_seconds"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.RecapModel != "" || resp.RecapModelEnabled {
		t.Errorf("expected zero-value defaults, got %+v", resp)
	}
}

func TestHandleSetRecapConfigPersists(t *testing.T) {
	h := testConfigHandler(t)

	body := `{"recap_model":"gpt-4o-mini","recap_model_enabled":true,"recap_timeout_seconds":90}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/ocode/recap", strings.NewReader(body))
	h.HandleSetRecapConfig(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}

	h.mu.Lock()
	got := h.cfg.Ocode
	h.mu.Unlock()
	if got.RecapModel != "gpt-4o-mini" || !got.RecapModelEnabled || got.RecapTimeoutSeconds != 90 {
		t.Errorf("in-memory cfg not updated: %+v", got)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/... -run TestHandleGetRecapConfigDefaults -v`
Expected: FAIL — `h.HandleGetRecapConfig` undefined (compile error).

- [x] **Step 3: Add the setter in `internal/config/ocodeconfig.go`**

Add directly below `SaveMaxSteps` (around line 1365):

```go
// SaveOcodeRecapConfig persists the /recap model selection and timeout.
func SaveOcodeRecapConfig(model string, enabled bool, timeoutSeconds int) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.RecapModel = model
		cfg.RecapModelEnabled = enabled
		cfg.RecapTimeoutSeconds = timeoutSeconds
		return nil
	})
}
```

Before moving on, run `grep -n '"recap_model"\|"recap_model_enabled"\|"recap_timeout_seconds"' internal/config/ocodeconfig.go`. If any of these three keys is missing from the `payload` map inside `writeOcodeConfigFile` (around `ocodeconfig.go:1226-1298`), add it there following the exact style of its neighboring entries (e.g. `payload["recap_model"] = cfg.RecapModel`), so the new setter's writes actually round-trip to disk.

- [x] **Step 4: Add the handler in `internal/server/handler_config.go`**

```go
// HandleGetRecapConfig reports the /recap model selection and timeout.
func (h *Handler) HandleGetRecapConfig(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	model, enabled, timeout := "", false, 0
	if h.cfg != nil {
		model = h.cfg.Ocode.RecapModel
		enabled = h.cfg.Ocode.RecapModelEnabled
		timeout = h.cfg.Ocode.RecapTimeoutSeconds
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"recap_model":           model,
		"recap_model_enabled":   enabled,
		"recap_timeout_seconds": timeout,
	})
}

// HandleSetRecapConfig persists the /recap model selection and timeout.
func (h *Handler) HandleSetRecapConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RecapModel          string `json:"recap_model"`
		RecapModelEnabled   bool   `json:"recap_model_enabled"`
		RecapTimeoutSeconds int    `json:"recap_timeout_seconds"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := config.SaveOcodeRecapConfig(req.RecapModel, req.RecapModelEnabled, req.RecapTimeoutSeconds); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
		return
	}

	h.mu.Lock()
	if h.cfg != nil {
		h.cfg.Ocode.RecapModel = req.RecapModel
		h.cfg.Ocode.RecapModelEnabled = req.RecapModelEnabled
		h.cfg.Ocode.RecapTimeoutSeconds = req.RecapTimeoutSeconds
	}
	h.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"recap_model":           req.RecapModel,
		"recap_model_enabled":   req.RecapModelEnabled,
		"recap_timeout_seconds": req.RecapTimeoutSeconds,
	})
}
```

- [x] **Step 5: Add the `*Server` wrappers and route registration in `internal/server/server.go`**

Add wrappers near `handleGetTerminalConfig`/`handleSetTerminalConfig` (`server.go:667-672`):

```go
func (s *Server) handleGetRecapConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetRecapConfig(w, r)
}
func (s *Server) handleSetRecapConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetRecapConfig(w, r)
}
```

Add routes inside the `// Config` block (`server.go:158-185`), next to the terminal routes:

```go
s.mux.HandleFunc("GET /api/config/ocode/recap", s.authMiddleware(s.handleGetRecapConfig))
s.mux.HandleFunc("PUT /api/config/ocode/recap", s.authMiddleware(s.handleSetRecapConfig))
```

- [x] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/server/... -run TestHandleGetRecapConfigDefaults -v` — expect PASS
Run: `go test ./internal/server/... -run TestHandleSetRecapConfigPersists -v` — expect PASS
Run: `go build ./...` — expect no errors

- [x] **Step 7: Commit**

```bash
git add internal/config/ocodeconfig.go internal/server/handler_config.go internal/server/server.go internal/server/handler_config_test.go
git commit -m "feat(server): add /api/config/ocode/recap endpoint"
```

---

### Task 2: Commit message settings endpoint

**Files:**
- Modify: `internal/config/ocodeconfig.go` (add `SaveOcodeCommitMsgConfig`)
- Modify: `internal/server/handler_config.go` (add `HandleGetCommitMsgConfig` / `HandleSetCommitMsgConfig`)
- Modify: `internal/server/server.go` (wrappers + routes)
- Test: `internal/server/handler_config_test.go`

**Interfaces:**
- Consumes: `testConfigHandler(t)` from Task 1.
- Produces: `config.SaveOcodeCommitMsgConfig(model, prompt string) error`; routes `GET/PUT /api/config/ocode/commit-msg`.

- [x] **Step 1: Write the failing test**

```go
func TestHandleSetCommitMsgConfigPersists(t *testing.T) {
	h := testConfigHandler(t)

	body := `{"commit_msg_model":"claude-sonnet-5","commit_msg_prompt":"Write a concise commit message."}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/ocode/commit-msg", strings.NewReader(body))
	h.HandleSetCommitMsgConfig(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	h.mu.Lock()
	got := h.cfg.Ocode
	h.mu.Unlock()
	if got.CommitMsgModel != "claude-sonnet-5" || got.CommitMsgPrompt != "Write a concise commit message." {
		t.Errorf("in-memory cfg not updated: %+v", got)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/... -run TestHandleSetCommitMsgConfigPersists -v`
Expected: FAIL — `h.HandleSetCommitMsgConfig` undefined.

- [x] **Step 3: Add the setter**

```go
// SaveOcodeCommitMsgConfig persists the commit-message generation model and prompt.
func SaveOcodeCommitMsgConfig(model, prompt string) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		cfg.CommitMsgModel = model
		cfg.CommitMsgPrompt = prompt
		return nil
	})
}
```

Run `grep -n '"commit_msg_model"\|"commit_msg_prompt"' internal/config/ocodeconfig.go`; add missing keys to the `writeOcodeConfigFile` payload map following neighboring entries, same as Task 1 Step 3.

- [x] **Step 4: Add the handler**

```go
func (h *Handler) HandleGetCommitMsgConfig(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	model, prompt := "", ""
	if h.cfg != nil {
		model = h.cfg.Ocode.CommitMsgModel
		prompt = h.cfg.Ocode.CommitMsgPrompt
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"commit_msg_model":  model,
		"commit_msg_prompt": prompt,
	})
}

func (h *Handler) HandleSetCommitMsgConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CommitMsgModel  string `json:"commit_msg_model"`
		CommitMsgPrompt string `json:"commit_msg_prompt"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := config.SaveOcodeCommitMsgConfig(req.CommitMsgModel, req.CommitMsgPrompt); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
		return
	}
	h.mu.Lock()
	if h.cfg != nil {
		h.cfg.Ocode.CommitMsgModel = req.CommitMsgModel
		h.cfg.Ocode.CommitMsgPrompt = req.CommitMsgPrompt
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"commit_msg_model":  req.CommitMsgModel,
		"commit_msg_prompt": req.CommitMsgPrompt,
	})
}
```

- [x] **Step 5: Add wrappers and routes**

```go
func (s *Server) handleGetCommitMsgConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetCommitMsgConfig(w, r)
}
func (s *Server) handleSetCommitMsgConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetCommitMsgConfig(w, r)
}
```

```go
s.mux.HandleFunc("GET /api/config/ocode/commit-msg", s.authMiddleware(s.handleGetCommitMsgConfig))
s.mux.HandleFunc("PUT /api/config/ocode/commit-msg", s.authMiddleware(s.handleSetCommitMsgConfig))
```

- [x] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/server/... -run TestHandleSetCommitMsgConfigPersists -v` — expect PASS
Run: `go build ./...` — expect no errors

- [x] **Step 7: Commit**

```bash
git add internal/config/ocodeconfig.go internal/server/handler_config.go internal/server/server.go internal/server/handler_config_test.go
git commit -m "feat(server): add /api/config/ocode/commit-msg endpoint"
```

---

### Task 3: Compact settings endpoint

**Files:**
- Modify: `internal/config/ocodeconfig.go` (add `SaveOcodeCompactConfig`)
- Modify: `internal/server/handler_config.go` (add `HandleGetCompactConfig` / `HandleSetCompactConfig`)
- Modify: `internal/server/server.go` (wrappers + routes)
- Test: `internal/server/handler_config_test.go`

**Interfaces:**
- Produces: `config.SaveOcodeCompactConfig(cfg config.CompactConfig) error`; routes `GET/PUT /api/config/ocode/compact`.

- [x] **Step 1: Write the failing test**

```go
func TestHandleSetCompactConfigPersists(t *testing.T) {
	h := testConfigHandler(t)

	body := `{"enabled":true,"summary_provider":"anthropic","summary_model":"claude-haiku-4-5",` +
		`"token_threshold":0.8,"keep_recent_turns":4,"keep_recent_tokens":2000,"min_messages":6,` +
		`"summary_timeout_seconds":30,"summary_max_retries":2,"max_summary_input_tokens":50000}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/ocode/compact", strings.NewReader(body))
	h.HandleSetCompactConfig(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	h.mu.Lock()
	got := h.cfg.Ocode.Compact
	h.mu.Unlock()
	if !got.Enabled || got.SummaryModel != "claude-haiku-4-5" || got.KeepRecentTurns != 4 {
		t.Errorf("in-memory cfg not updated: %+v", got)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/... -run TestHandleSetCompactConfigPersists -v`
Expected: FAIL — `h.HandleSetCompactConfig` undefined.

- [x] **Step 3: Add the setter**

```go
// SaveOcodeCompactConfig persists the auto-compact settings block.
func SaveOcodeCompactConfig(cfg CompactConfig) error {
	return withOcodeConfigLock(func(c *OcodeConfig) error {
		c.Compact = cfg
		return nil
	})
}
```

`Compact` is already written wholesale as `payload["compact"] = cfg.Compact` in `writeOcodeConfigFile` (confirmed at `ocodeconfig.go:1226-1298`), and `CompactConfig`'s fields already carry full JSON tags, so no payload-map change is needed for this task.

- [x] **Step 4: Add the handler**

```go
func (h *Handler) HandleGetCompactConfig(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	cfg := config.CompactConfig{}
	if h.cfg != nil {
		cfg = h.cfg.Ocode.Compact
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, cfg)
}

func (h *Handler) HandleSetCompactConfig(w http.ResponseWriter, r *http.Request) {
	var req config.CompactConfig
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := config.SaveOcodeCompactConfig(req); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
		return
	}
	h.mu.Lock()
	if h.cfg != nil {
		h.cfg.Ocode.Compact = req
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, req)
}
```

- [x] **Step 5: Add wrappers and routes**

```go
func (s *Server) handleGetCompactConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetCompactConfig(w, r)
}
func (s *Server) handleSetCompactConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetCompactConfig(w, r)
}
```

```go
s.mux.HandleFunc("GET /api/config/ocode/compact", s.authMiddleware(s.handleGetCompactConfig))
s.mux.HandleFunc("PUT /api/config/ocode/compact", s.authMiddleware(s.handleSetCompactConfig))
```

- [x] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/server/... -run TestHandleSetCompactConfigPersists -v` — expect PASS
Run: `go build ./...` — expect no errors

- [x] **Step 7: Commit**

```bash
git add internal/config/ocodeconfig.go internal/server/handler_config.go internal/server/server.go internal/server/handler_config_test.go
git commit -m "feat(server): add /api/config/ocode/compact endpoint"
```

---

### Task 4: Permissions auto-approval settings endpoint

**Files:**
- Modify: `internal/config/ocodeconfig.go` (add `SaveOcodeAutoPermissionConfig`, reusing existing granular setters if present)
- Modify: `internal/server/handler_config.go` (add `HandleGetAutoPermissionConfig` / `HandleSetAutoPermissionConfig`)
- Modify: `internal/server/server.go` (wrappers + routes)
- Test: `internal/server/handler_config_test.go`

**Interfaces:**
- Produces: `config.SaveOcodeAutoPermissionConfig(cfg config.AutoPermissionConfig) error`; routes `GET/PUT /api/config/ocode/permissions-auto`.

- [x] **Step 1: Before writing code, discover existing granular setters**

Run: `grep -n 'func Save.*Auto\|func SavePermissionModel' internal/config/ocodeconfig.go`

This surfaces any existing per-field setters for `AutoPermissionConfig` (the earlier design research found references to `SaveAutoPermissionEnabled` and a `/permissions model` command implying a `SavePermissionModel`-style setter). Read each match's full body. `internal/config/ocodeconfig.go:1326-1356` (`SaveOcodePermissions`) documents that the auto-permission `Model` field is **exclusively** owned by the model-setting setter and must never be overwritten by a general permissions write — the new endpoint in this task must preserve that invariant: it edits `Enabled`, `AllowDestructive`, `Prompt`, `MaxContextBytes`, `MaxContextSources`, `MaxContextLinesPerSource`, `MinConfidence`, and `Grants`, but does **not** touch `Model` (that stays under whatever existing model-setting path already owns it).

- [x] **Step 2: Write the failing test**

```go
func TestHandleSetAutoPermissionConfigPersists(t *testing.T) {
	h := testConfigHandler(t)
	h.mu.Lock()
	h.cfg.Ocode.Permissions.Auto = &config.AutoPermissionConfig{Model: "existing-model"}
	h.mu.Unlock()

	body := `{"enabled":true,"allow_destructive":false,"prompt":"custom prompt",` +
		`"max_context_bytes":4096,"max_context_sources":3,"max_context_lines_per_source":50,` +
		`"min_confidence":0.9,"grants":[]}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/ocode/permissions-auto", strings.NewReader(body))
	h.HandleSetAutoPermissionConfig(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	h.mu.Lock()
	got := h.cfg.Ocode.Permissions.Auto
	h.mu.Unlock()
	if got == nil || !got.Enabled || got.MaxContextBytes != 4096 {
		t.Errorf("in-memory cfg not updated: %+v", got)
	}
	if got.Model != "existing-model" {
		t.Errorf("Model must be preserved, got %q", got.Model)
	}
}
```

- [x] **Step 3: Run test to verify it fails**

Run: `go test ./internal/server/... -run TestHandleSetAutoPermissionConfigPersists -v`
Expected: FAIL — `h.HandleSetAutoPermissionConfig` undefined.

- [x] **Step 4: Add the setter**

```go
// SaveOcodeAutoPermissionConfig persists the auto-approval block, preserving
// Model (owned exclusively by the /permissions model setter) from whatever
// is currently on disk.
func SaveOcodeAutoPermissionConfig(cfg AutoPermissionConfig) error {
	return withOcodeConfigLock(func(c *OcodeConfig) error {
		preservedModel := ""
		if c.Permissions.Auto != nil {
			preservedModel = c.Permissions.Auto.Model
		}
		cfg.Model = preservedModel
		c.Permissions.Auto = &cfg
		return nil
	})
}
```

- [x] **Step 5: Add the handler**

```go
func (h *Handler) HandleGetAutoPermissionConfig(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	cfg := config.AutoPermissionConfig{}
	if h.cfg != nil && h.cfg.Ocode.Permissions.Auto != nil {
		cfg = *h.cfg.Ocode.Permissions.Auto
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, cfg)
}

func (h *Handler) HandleSetAutoPermissionConfig(w http.ResponseWriter, r *http.Request) {
	var req config.AutoPermissionConfig
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := config.SaveOcodeAutoPermissionConfig(req); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
		return
	}
	h.mu.Lock()
	preservedModel := ""
	if h.cfg != nil {
		if h.cfg.Ocode.Permissions.Auto != nil {
			preservedModel = h.cfg.Ocode.Permissions.Auto.Model
		}
		req.Model = preservedModel
		h.cfg.Ocode.Permissions.Auto = &req
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, req)
}
```

- [x] **Step 6: Add wrappers and routes**

```go
func (s *Server) handleGetAutoPermissionConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetAutoPermissionConfig(w, r)
}
func (s *Server) handleSetAutoPermissionConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetAutoPermissionConfig(w, r)
}
```

```go
s.mux.HandleFunc("GET /api/config/ocode/permissions-auto", s.authMiddleware(s.handleGetAutoPermissionConfig))
s.mux.HandleFunc("PUT /api/config/ocode/permissions-auto", s.authMiddleware(s.handleSetAutoPermissionConfig))
```

- [x] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/server/... -run TestHandleSetAutoPermissionConfigPersists -v` — expect PASS
Run: `go build ./...` — expect no errors

- [x] **Step 8: Commit**

```bash
git add internal/config/ocodeconfig.go internal/server/handler_config.go internal/server/server.go internal/server/handler_config_test.go
git commit -m "feat(server): add /api/config/ocode/permissions-auto endpoint"
```

---

### Task 5: Discovery settings endpoint

**Files:**
- Modify: `internal/config/ocodeconfig.go` (add `SaveOcodeDiscoveryConfig`)
- Modify: `internal/server/handler_config.go` (add `HandleGetDiscoveryConfig` / `HandleSetDiscoveryConfig`)
- Modify: `internal/server/server.go` (wrappers + routes)
- Test: `internal/server/handler_config_test.go`

**Interfaces:**
- Produces: `config.SaveOcodeDiscoveryConfig(cfg config.DiscoveryConfig) error`; routes `GET/PUT /api/config/ocode/discovery`.

- [x] **Step 1: Before writing code, confirm the on-disk key names**

`DiscoveryConfig` has no Go struct tags — it's hand-mapped into a `discoveryMap` inside `writeOcodeConfigFile`. Run:
`grep -n 'discoveryMap' internal/config/ocodeconfig.go`
and read the surrounding ~20 lines to get the exact key string used for each field (`Enabled`, `EmbeddingModel`, `EmbeddingBackend`, `LocalModelStatus`, `LocalServerURL`, `PinnedSkills`, `IgnorePaths`). Use those exact keys as the JSON tags on the request/response DTO struct below — if any differs from the snake_case guess used here, adjust the DTO tags to match, not the other way around.

- [x] **Step 2: Write the failing test**

```go
func TestHandleSetDiscoveryConfigPersists(t *testing.T) {
	h := testConfigHandler(t)

	body := `{"enabled":true,"embedding_model":"bge-m3","embedding_backend":"local",` +
		`"local_model_status":"ready","local_server_url":"","pinned_skills":["foo"],"ignore_paths":["dist/"]}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/ocode/discovery", strings.NewReader(body))
	h.HandleSetDiscoveryConfig(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	h.mu.Lock()
	got := h.cfg.Ocode.Discovery
	h.mu.Unlock()
	if !got.Enabled || got.EmbeddingModel != "bge-m3" || len(got.PinnedSkills) != 1 {
		t.Errorf("in-memory cfg not updated: %+v", got)
	}
}
```

- [x] **Step 3: Run test to verify it fails**

Run: `go test ./internal/server/... -run TestHandleSetDiscoveryConfigPersists -v`
Expected: FAIL — `h.HandleSetDiscoveryConfig` undefined.

- [x] **Step 4: Add the setter**

```go
// SaveOcodeDiscoveryConfig persists the discovery-based skill/MCP retrieval settings.
func SaveOcodeDiscoveryConfig(cfg DiscoveryConfig) error {
	return withOcodeConfigLock(func(c *OcodeConfig) error {
		c.Discovery = cfg
		return nil
	})
}
```

- [x] **Step 5: Add the handler**

Use a DTO with explicit tags (adjust to match Step 1's findings if they differ):

```go
type discoveryConfigDTO struct {
	Enabled          bool     `json:"enabled"`
	EmbeddingModel   string   `json:"embedding_model"`
	EmbeddingBackend string   `json:"embedding_backend"`
	LocalModelStatus string   `json:"local_model_status"`
	LocalServerURL   string   `json:"local_server_url"`
	PinnedSkills     []string `json:"pinned_skills"`
	IgnorePaths      []string `json:"ignore_paths"`
}

func (h *Handler) HandleGetDiscoveryConfig(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	d := config.DiscoveryConfig{}
	if h.cfg != nil {
		d = h.cfg.Ocode.Discovery
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, discoveryConfigDTO{
		Enabled: d.Enabled, EmbeddingModel: d.EmbeddingModel, EmbeddingBackend: d.EmbeddingBackend,
		LocalModelStatus: d.LocalModelStatus, LocalServerURL: d.LocalServerURL,
		PinnedSkills: d.PinnedSkills, IgnorePaths: d.IgnorePaths,
	})
}

func (h *Handler) HandleSetDiscoveryConfig(w http.ResponseWriter, r *http.Request) {
	var req discoveryConfigDTO
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	cfg := config.DiscoveryConfig{
		Enabled: req.Enabled, EmbeddingModel: req.EmbeddingModel, EmbeddingBackend: req.EmbeddingBackend,
		LocalModelStatus: req.LocalModelStatus, LocalServerURL: req.LocalServerURL,
		PinnedSkills: req.PinnedSkills, IgnorePaths: req.IgnorePaths,
	}
	if err := config.SaveOcodeDiscoveryConfig(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
		return
	}
	h.mu.Lock()
	if h.cfg != nil {
		h.cfg.Ocode.Discovery = cfg
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, req)
}
```

- [x] **Step 6: Add wrappers and routes**

```go
func (s *Server) handleGetDiscoveryConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetDiscoveryConfig(w, r)
}
func (s *Server) handleSetDiscoveryConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetDiscoveryConfig(w, r)
}
```

```go
s.mux.HandleFunc("GET /api/config/ocode/discovery", s.authMiddleware(s.handleGetDiscoveryConfig))
s.mux.HandleFunc("PUT /api/config/ocode/discovery", s.authMiddleware(s.handleSetDiscoveryConfig))
```

- [x] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/server/... -run TestHandleSetDiscoveryConfigPersists -v` — expect PASS
Run: `go build ./...` — expect no errors

- [x] **Step 8: Commit**

```bash
git add internal/config/ocodeconfig.go internal/server/handler_config.go internal/server/server.go internal/server/handler_config_test.go
git commit -m "feat(server): add /api/config/ocode/discovery endpoint"
```

---

### Task 6: TUI settings endpoint

**Files:**
- Modify: `internal/config/ocodeconfig.go` (add `SaveOcodeTUIConfig`)
- Modify: `internal/server/handler_config.go` (add `HandleGetTUIConfigSection` / `HandleSetTUIConfigSection` — named to avoid clashing with any existing `TUIStatus`-related handler)
- Modify: `internal/server/server.go` (wrappers + routes)
- Test: `internal/server/handler_config_test.go`

**Interfaces:**
- Produces: `config.SaveOcodeTUIConfig(cfg config.TUIConfig) error`; routes `GET/PUT /api/config/ocode/tui`.

- [x] **Step 1: Write the failing test**

```go
func TestHandleSetTUIConfigSectionPersists(t *testing.T) {
	h := testConfigHandler(t)

	body := `{"theme":"dracula","mouse":true,"scroll_speed":2.5,"keybinds":{"quit":"ctrl+c"},` +
		`"leader_timeout":1000,"branchless":false}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/ocode/tui", strings.NewReader(body))
	h.HandleSetTUIConfigSection(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	h.mu.Lock()
	got := h.cfg.Ocode.TUI
	h.mu.Unlock()
	if got.Theme != "dracula" || got.Mouse == nil || !*got.Mouse || got.Keybinds["quit"] != "ctrl+c" {
		t.Errorf("in-memory cfg not updated: %+v", got)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/... -run TestHandleSetTUIConfigSectionPersists -v`
Expected: FAIL — `h.HandleSetTUIConfigSection` undefined.

- [x] **Step 3: Add the setter**

```go
// SaveOcodeTUIConfig persists the TUI theme/input/keybind settings.
func SaveOcodeTUIConfig(cfg TUIConfig) error {
	return withOcodeConfigLock(func(c *OcodeConfig) error {
		c.TUI = cfg
		return nil
	})
}
```

`TUI` is already conditionally written as `payload["tui"] = cfg.TUI` in `writeOcodeConfigFile` and `TUIConfig` has full JSON tags — no payload-map change needed.

- [x] **Step 4: Add the handler**

```go
func (h *Handler) HandleGetTUIConfigSection(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	cfg := config.TUIConfig{}
	if h.cfg != nil {
		cfg = h.cfg.Ocode.TUI
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, cfg)
}

func (h *Handler) HandleSetTUIConfigSection(w http.ResponseWriter, r *http.Request) {
	var req config.TUIConfig
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := config.SaveOcodeTUIConfig(req); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
		return
	}
	h.mu.Lock()
	if h.cfg != nil {
		h.cfg.Ocode.TUI = req
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, req)
}
```

- [x] **Step 5: Add wrappers and routes**

```go
func (s *Server) handleGetTUIConfigSection(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetTUIConfigSection(w, r)
}
func (s *Server) handleSetTUIConfigSection(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetTUIConfigSection(w, r)
}
```

```go
s.mux.HandleFunc("GET /api/config/ocode/tui", s.authMiddleware(s.handleGetTUIConfigSection))
s.mux.HandleFunc("PUT /api/config/ocode/tui", s.authMiddleware(s.handleSetTUIConfigSection))
```

- [x] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/server/... -run TestHandleSetTUIConfigSectionPersists -v` — expect PASS
Run: `go build ./...` — expect no errors

- [x] **Step 7: Commit**

```bash
git add internal/config/ocodeconfig.go internal/server/handler_config.go internal/server/server.go internal/server/handler_config_test.go
git commit -m "feat(server): add /api/config/ocode/tui endpoint"
```

---

### Task 7: Editor mode settings endpoint

**Files:**
- Modify: `internal/config/ocodeconfig.go` (add `SaveOcodeEditorConfig`)
- Modify: `internal/server/handler_config.go` (add `HandleGetEditorConfig` / `HandleSetEditorConfig`)
- Modify: `internal/server/server.go` (wrappers + routes)
- Test: `internal/server/handler_config_test.go`

**Interfaces:**
- Produces: `config.SaveOcodeEditorConfig(editor, editorMode, ideMode string) error`; routes `GET/PUT /api/config/ocode/editor`.

- [x] **Step 1: Write the failing test**

```go
func TestHandleSetEditorConfigPersists(t *testing.T) {
	h := testConfigHandler(t)

	body := `{"editor":"vim","editor_mode":"monaco","ide_mode":"split"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/ocode/editor", strings.NewReader(body))
	h.HandleSetEditorConfig(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	h.mu.Lock()
	got := h.cfg.Ocode
	h.mu.Unlock()
	if got.Editor != "vim" || got.EditorMode != "monaco" || got.IDEMode != "split" {
		t.Errorf("in-memory cfg not updated: %+v", got)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/... -run TestHandleSetEditorConfigPersists -v`
Expected: FAIL — `h.HandleSetEditorConfig` undefined.

- [x] **Step 3: Add the setter**

```go
// SaveOcodeEditorConfig persists the default editor and editor/IDE mode settings.
func SaveOcodeEditorConfig(editor, editorMode, ideMode string) error {
	return withOcodeConfigLock(func(c *OcodeConfig) error {
		c.Editor = editor
		c.EditorMode = editorMode
		c.IDEMode = ideMode
		return nil
	})
}
```

Run `grep -n '"editor"\|"editor_mode"\|"ide_mode"' internal/config/ocodeconfig.go`; add any missing key to the `writeOcodeConfigFile` payload map following neighboring entries.

- [x] **Step 4: Add the handler**

```go
func (h *Handler) HandleGetEditorConfig(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	editor, editorMode, ideMode := "", "", ""
	if h.cfg != nil {
		editor, editorMode, ideMode = h.cfg.Ocode.Editor, h.cfg.Ocode.EditorMode, h.cfg.Ocode.IDEMode
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"editor": editor, "editor_mode": editorMode, "ide_mode": ideMode,
	})
}

func (h *Handler) HandleSetEditorConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Editor     string `json:"editor"`
		EditorMode string `json:"editor_mode"`
		IDEMode    string `json:"ide_mode"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := config.SaveOcodeEditorConfig(req.Editor, req.EditorMode, req.IDEMode); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
		return
	}
	h.mu.Lock()
	if h.cfg != nil {
		h.cfg.Ocode.Editor, h.cfg.Ocode.EditorMode, h.cfg.Ocode.IDEMode = req.Editor, req.EditorMode, req.IDEMode
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"editor": req.Editor, "editor_mode": req.EditorMode, "ide_mode": req.IDEMode,
	})
}
```

- [x] **Step 5: Add wrappers and routes**

```go
func (s *Server) handleGetEditorConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetEditorConfig(w, r)
}
func (s *Server) handleSetEditorConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetEditorConfig(w, r)
}
```

```go
s.mux.HandleFunc("GET /api/config/ocode/editor", s.authMiddleware(s.handleGetEditorConfig))
s.mux.HandleFunc("PUT /api/config/ocode/editor", s.authMiddleware(s.handleSetEditorConfig))
```

- [x] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/server/... -run TestHandleSetEditorConfigPersists -v` — expect PASS
Run: `go build ./...` — expect no errors

- [x] **Step 7: Commit**

```bash
git add internal/config/ocodeconfig.go internal/server/handler_config.go internal/server/server.go internal/server/handler_config_test.go
git commit -m "feat(server): add /api/config/ocode/editor endpoint"
```

---

### Task 8: Image generation settings endpoint

**Files:**
- Modify: `internal/server/handler_config.go` (add `HandleGetImageGenConfig` / `HandleSetImageGenConfig`)
- Modify: `internal/server/server.go` (wrappers + routes)
- Test: `internal/server/handler_config_test.go`

**Interfaces:**
- Consumes: existing `config.SaveImageGenConfig` (or equivalent) from `internal/config/imagegen_config.go:22-64` — read it first (see Step 1); no new setter should be needed for this task.
- Produces: routes `GET/PUT /api/config/ocode/imagegen`.

- [x] **Step 1: Before writing code, confirm the existing setter's exact signature**

Run: `grep -n 'func Save' internal/config/imagegen_config.go` and read the full body of the matching function(s) at `internal/config/imagegen_config.go:22-64`. `ImageGenConfig` already has full JSON tags (`enabled`, `provider`, `model`, `output_path`, `timeout`) and setters already exist (per prior research: `SaveImageGenConfig`, `SaveImageGenEnabled`, `SaveImageGenModel`) — this task should call whichever full-object setter exists (expected `SaveImageGenConfig(cfg ImageGenConfig) error`, matching the `SaveOcodeCompactConfig`/`SaveOcodeTUIConfig` shape used elsewhere in this plan). If the exact name or signature differs from that expectation, use what you actually find — do not add a duplicate setter.

- [x] **Step 2: Write the failing test**

```go
func TestHandleSetImageGenConfigPersists(t *testing.T) {
	h := testConfigHandler(t)

	body := `{"enabled":true,"provider":"gemini","model":"gemini-3.1-flash-image",` +
		`"output_path":"","timeout":60}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/ocode/imagegen", strings.NewReader(body))
	h.HandleSetImageGenConfig(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	h.mu.Lock()
	got := h.cfg.Ocode.ImageGen
	h.mu.Unlock()
	if !got.Enabled || got.Provider != "gemini" || got.Timeout != 60 {
		t.Errorf("in-memory cfg not updated: %+v", got)
	}
}
```

- [x] **Step 3: Run test to verify it fails**

Run: `go test ./internal/server/... -run TestHandleSetImageGenConfigPersists -v`
Expected: FAIL — `h.HandleSetImageGenConfig` undefined.

- [x] **Step 4: Add the handler, calling the existing setter found in Step 1**

```go
func (h *Handler) HandleGetImageGenConfig(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	cfg := config.ImageGenConfig{}
	if h.cfg != nil {
		cfg = h.cfg.Ocode.ImageGen
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, cfg)
}

func (h *Handler) HandleSetImageGenConfig(w http.ResponseWriter, r *http.Request) {
	var req config.ImageGenConfig
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := config.SaveImageGenConfig(req); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
		return
	}
	h.mu.Lock()
	if h.cfg != nil {
		h.cfg.Ocode.ImageGen = req
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, req)
}
```

(If Step 1 found a different setter name, use that name in place of `config.SaveImageGenConfig` above.)

- [x] **Step 5: Add wrappers and routes**

```go
func (s *Server) handleGetImageGenConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetImageGenConfig(w, r)
}
func (s *Server) handleSetImageGenConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetImageGenConfig(w, r)
}
```

```go
s.mux.HandleFunc("GET /api/config/ocode/imagegen", s.authMiddleware(s.handleGetImageGenConfig))
s.mux.HandleFunc("PUT /api/config/ocode/imagegen", s.authMiddleware(s.handleSetImageGenConfig))
```

- [x] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/server/... -run TestHandleSetImageGenConfigPersists -v` — expect PASS
Run: `go build ./...` — expect no errors

- [x] **Step 7: Commit**

```bash
git add internal/server/handler_config.go internal/server/server.go internal/server/handler_config_test.go
git commit -m "feat(server): add /api/config/ocode/imagegen endpoint"
```

---

### Task 9: Paths & uploads settings endpoint

**Files:**
- Modify: `internal/config/ocodeconfig.go` (add `SaveOcodePathsConfig`)
- Modify: `internal/server/handler_config.go` (add `HandleGetPathsConfig` / `HandleSetPathsConfig`)
- Modify: `internal/server/server.go` (wrappers + routes)
- Test: `internal/server/handler_config_test.go`

**Interfaces:**
- Produces: `config.SaveOcodePathsConfig(extraAllowedPaths []string, uploadDir string) error`; routes `GET/PUT /api/config/ocode/paths`.

- [x] **Step 1: Write the failing test**

```go
func TestHandleSetPathsConfigPersists(t *testing.T) {
	h := testConfigHandler(t)

	body := `{"extra_allowed_paths":["/tmp/scratch","/data"],"upload_dir":"/data/uploads"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/ocode/paths", strings.NewReader(body))
	h.HandleSetPathsConfig(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	h.mu.Lock()
	got := h.cfg.Ocode
	h.mu.Unlock()
	if len(got.ExtraAllowedPaths) != 2 || got.UploadDir != "/data/uploads" {
		t.Errorf("in-memory cfg not updated: %+v", got)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/... -run TestHandleSetPathsConfigPersists -v`
Expected: FAIL — `h.HandleSetPathsConfig` undefined.

- [x] **Step 3: Add the setter**

```go
// SaveOcodePathsConfig persists the extra-allowed-paths allowlist and upload directory.
func SaveOcodePathsConfig(extraAllowedPaths []string, uploadDir string) error {
	return withOcodeConfigLock(func(c *OcodeConfig) error {
		c.ExtraAllowedPaths = extraAllowedPaths
		c.UploadDir = uploadDir
		return nil
	})
}
```

Run `grep -n '"extra_allowed_paths"\|"upload_dir"' internal/config/ocodeconfig.go`; `UploadDir` already has a `json:"upload_dir,omitempty"` struct tag and is likely already payload-mapped since it's a tagged field consumed elsewhere (`/api/uploads`) — confirm it's present in the `writeOcodeConfigFile` payload map and add it if missing, following neighboring entries. Do the same check for `extra_allowed_paths`.

- [x] **Step 4: Add the handler**

```go
func (h *Handler) HandleGetPathsConfig(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	paths := []string{}
	uploadDir := ""
	if h.cfg != nil {
		paths = h.cfg.Ocode.ExtraAllowedPaths
		uploadDir = h.cfg.Ocode.UploadDir
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"extra_allowed_paths": paths, "upload_dir": uploadDir,
	})
}

func (h *Handler) HandleSetPathsConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ExtraAllowedPaths []string `json:"extra_allowed_paths"`
		UploadDir         string   `json:"upload_dir"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := config.SaveOcodePathsConfig(req.ExtraAllowedPaths, req.UploadDir); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
		return
	}
	h.mu.Lock()
	if h.cfg != nil {
		h.cfg.Ocode.ExtraAllowedPaths = req.ExtraAllowedPaths
		h.cfg.Ocode.UploadDir = req.UploadDir
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"extra_allowed_paths": req.ExtraAllowedPaths, "upload_dir": req.UploadDir,
	})
}
```

- [x] **Step 5: Add wrappers and routes**

```go
func (s *Server) handleGetPathsConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetPathsConfig(w, r)
}
func (s *Server) handleSetPathsConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetPathsConfig(w, r)
}
```

```go
s.mux.HandleFunc("GET /api/config/ocode/paths", s.authMiddleware(s.handleGetPathsConfig))
s.mux.HandleFunc("PUT /api/config/ocode/paths", s.authMiddleware(s.handleSetPathsConfig))
```

- [x] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/server/... -run TestHandleSetPathsConfigPersists -v` — expect PASS
Run: `go build ./...` — expect no errors

- [x] **Step 7: Commit**

```bash
git add internal/config/ocodeconfig.go internal/server/handler_config.go internal/server/server.go internal/server/handler_config_test.go
git commit -m "feat(server): add /api/config/ocode/paths endpoint"
```

---

### Task 10: Limits settings endpoint

**Files:**
- Modify: `internal/config/ocodeconfig.go` (add `SaveOcodeLimits`)
- Modify: `internal/server/handler_config.go` (add `HandleGetLimitsConfig` / `HandleSetLimitsConfig`)
- Modify: `internal/server/server.go` (wrappers + routes)
- Test: `internal/server/handler_config_test.go`

**Interfaces:**
- Produces: `config.SaveOcodeLimits(maxSteps, maxImageDim, maxConcurrentAgents, undoMaxAgeDelta int) error`; routes `GET/PUT /api/config/ocode/limits`.
- Note: `MaxSteps` and `MaxConcurrentAgents` already have individual setters (`SaveMaxSteps`, `SaveMaxConcurrentAgents`) used elsewhere (e.g. TUI commands). This task adds one combined setter so the Settings form's single save action is one atomic `withOcodeConfigLock` call instead of four sequential lock/unlock round trips; it does not remove or replace the existing individual setters.

- [x] **Step 1: Write the failing test**

```go
func TestHandleSetLimitsConfigPersists(t *testing.T) {
	h := testConfigHandler(t)

	body := `{"max_steps":150,"max_image_dim":2500,"max_concurrent_agents":4,"undo_max_age_delta":8}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/ocode/limits", strings.NewReader(body))
	h.HandleSetLimitsConfig(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	h.mu.Lock()
	got := h.cfg.Ocode
	h.mu.Unlock()
	if got.MaxSteps != 150 || got.MaxImageDim != 2500 || got.MaxConcurrentAgents != 4 || got.UndoMaxAgeDelta != 8 {
		t.Errorf("in-memory cfg not updated: %+v", got)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/... -run TestHandleSetLimitsConfigPersists -v`
Expected: FAIL — `h.HandleSetLimitsConfig` undefined.

- [x] **Step 3: Add the setter**

```go
// SaveOcodeLimits persists MaxSteps, MaxImageDim, MaxConcurrentAgents, and
// UndoMaxAgeDelta in a single atomic write. n <= 0 for MaxConcurrentAgents
// means unlimited (matches SaveMaxConcurrentAgents's existing normalization).
func SaveOcodeLimits(maxSteps, maxImageDim, maxConcurrentAgents, undoMaxAgeDelta int) error {
	if maxConcurrentAgents < 0 {
		maxConcurrentAgents = 0
	}
	return withOcodeConfigLock(func(c *OcodeConfig) error {
		c.MaxSteps = maxSteps
		c.MaxImageDim = maxImageDim
		c.MaxConcurrentAgents = maxConcurrentAgents
		c.UndoMaxAgeDelta = undoMaxAgeDelta
		return nil
	})
}
```

`MaxSteps`, `MaxImageDim`, `MaxConcurrentAgents` already have JSON tags and — since `MaxSteps`/`MaxConcurrentAgents` already round-trip via the existing individual setters — are presumably already in the `writeOcodeConfigFile` payload map. Run `grep -n '"undo_max_age_delta"\|"image_max_dim"' internal/config/ocodeconfig.go` and add `UndoMaxAgeDelta`/`MaxImageDim` to the payload map if either is missing, following neighboring entries (e.g. the existing `if cfg.MaxSteps > 0 { payload["max_steps"] = cfg.MaxSteps }` conditional style).

- [x] **Step 4: Add the handler**

```go
func (h *Handler) HandleGetLimitsConfig(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	maxSteps, maxImageDim, maxConcurrentAgents, undoMaxAgeDelta := 0, 0, 0, 0
	if h.cfg != nil {
		maxSteps = h.cfg.Ocode.MaxSteps
		maxImageDim = h.cfg.Ocode.MaxImageDim
		maxConcurrentAgents = h.cfg.Ocode.MaxConcurrentAgents
		undoMaxAgeDelta = h.cfg.Ocode.UndoMaxAgeDelta
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"max_steps": maxSteps, "max_image_dim": maxImageDim,
		"max_concurrent_agents": maxConcurrentAgents, "undo_max_age_delta": undoMaxAgeDelta,
	})
}

func (h *Handler) HandleSetLimitsConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MaxSteps            int `json:"max_steps"`
		MaxImageDim         int `json:"max_image_dim"`
		MaxConcurrentAgents int `json:"max_concurrent_agents"`
		UndoMaxAgeDelta     int `json:"undo_max_age_delta"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := config.SaveOcodeLimits(req.MaxSteps, req.MaxImageDim, req.MaxConcurrentAgents, req.UndoMaxAgeDelta); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
		return
	}
	h.mu.Lock()
	if h.cfg != nil {
		h.cfg.Ocode.MaxSteps = req.MaxSteps
		h.cfg.Ocode.MaxImageDim = req.MaxImageDim
		h.cfg.Ocode.MaxConcurrentAgents = req.MaxConcurrentAgents
		h.cfg.Ocode.UndoMaxAgeDelta = req.UndoMaxAgeDelta
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"max_steps": req.MaxSteps, "max_image_dim": req.MaxImageDim,
		"max_concurrent_agents": req.MaxConcurrentAgents, "undo_max_age_delta": req.UndoMaxAgeDelta,
	})
}
```

- [x] **Step 5: Add wrappers and routes**

```go
func (s *Server) handleGetLimitsConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetLimitsConfig(w, r)
}
func (s *Server) handleSetLimitsConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetLimitsConfig(w, r)
}
```

```go
s.mux.HandleFunc("GET /api/config/ocode/limits", s.authMiddleware(s.handleGetLimitsConfig))
s.mux.HandleFunc("PUT /api/config/ocode/limits", s.authMiddleware(s.handleSetLimitsConfig))
```

- [x] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/server/... -run TestHandleSetLimitsConfigPersists -v` — expect PASS
Run: `go build ./...` — expect no errors

- [x] **Step 7: Commit**

```bash
git add internal/config/ocodeconfig.go internal/server/handler_config.go internal/server/server.go internal/server/handler_config_test.go
git commit -m "feat(server): add /api/config/ocode/limits endpoint"
```

---

### Task 11: Features settings endpoint

**Files:**
- Modify: `internal/config/ocodeconfig.go` (add `SaveOcodeFeatures`)
- Modify: `internal/server/handler_config.go` (add `HandleGetFeaturesConfig` / `HandleSetFeaturesConfig`)
- Modify: `internal/server/server.go` (wrappers + routes)
- Test: `internal/server/handler_config_test.go`

**Interfaces:**
- Produces: `config.SaveOcodeFeatures(memoryEnabled, docPromptEnabled bool) error`; routes `GET/PUT /api/config/ocode/features`.

- [x] **Step 1: Write the failing test**

```go
func TestHandleSetFeaturesConfigPersists(t *testing.T) {
	h := testConfigHandler(t)

	body := `{"memory_enabled":true,"doc_prompt_enabled":false}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/ocode/features", strings.NewReader(body))
	h.HandleSetFeaturesConfig(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	h.mu.Lock()
	got := h.cfg.Ocode
	h.mu.Unlock()
	if !got.MemoryEnabled || got.DocPromptEnabled {
		t.Errorf("in-memory cfg not updated: %+v", got)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/... -run TestHandleSetFeaturesConfigPersists -v`
Expected: FAIL — `h.HandleSetFeaturesConfig` undefined.

- [x] **Step 3: Add the setter**

```go
// SaveOcodeFeatures persists the memory and doc-prompt injection toggles.
func SaveOcodeFeatures(memoryEnabled, docPromptEnabled bool) error {
	return withOcodeConfigLock(func(c *OcodeConfig) error {
		c.MemoryEnabled = memoryEnabled
		c.DocPromptEnabled = docPromptEnabled
		return nil
	})
}
```

Run `grep -n '"memory_enabled"\|"doc_prompt_enabled"' internal/config/ocodeconfig.go`; add any missing key to the `writeOcodeConfigFile` payload map following neighboring entries.

- [x] **Step 4: Add the handler**

```go
func (h *Handler) HandleGetFeaturesConfig(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	memoryEnabled, docPromptEnabled := false, false
	if h.cfg != nil {
		memoryEnabled = h.cfg.Ocode.MemoryEnabled
		docPromptEnabled = h.cfg.Ocode.DocPromptEnabled
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"memory_enabled": memoryEnabled, "doc_prompt_enabled": docPromptEnabled,
	})
}

func (h *Handler) HandleSetFeaturesConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MemoryEnabled    bool `json:"memory_enabled"`
		DocPromptEnabled bool `json:"doc_prompt_enabled"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := config.SaveOcodeFeatures(req.MemoryEnabled, req.DocPromptEnabled); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
		return
	}
	h.mu.Lock()
	if h.cfg != nil {
		h.cfg.Ocode.MemoryEnabled = req.MemoryEnabled
		h.cfg.Ocode.DocPromptEnabled = req.DocPromptEnabled
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"memory_enabled": req.MemoryEnabled, "doc_prompt_enabled": req.DocPromptEnabled,
	})
}
```

- [x] **Step 5: Add wrappers and routes**

```go
func (s *Server) handleGetFeaturesConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetFeaturesConfig(w, r)
}
func (s *Server) handleSetFeaturesConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetFeaturesConfig(w, r)
}
```

```go
s.mux.HandleFunc("GET /api/config/ocode/features", s.authMiddleware(s.handleGetFeaturesConfig))
s.mux.HandleFunc("PUT /api/config/ocode/features", s.authMiddleware(s.handleSetFeaturesConfig))
```

- [x] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/server/... -run TestHandleSetFeaturesConfigPersists -v` — expect PASS
Run: `go build ./...` — expect no errors

- [x] **Step 7: Commit**

```bash
git add internal/config/ocodeconfig.go internal/server/handler_config.go internal/server/server.go internal/server/handler_config_test.go
git commit -m "feat(server): add /api/config/ocode/features endpoint"
```

---

### Task 12: Plugins-enabled and local-models endpoints

**Files:**
- Modify: `internal/config/ocodeconfig.go` (add `SaveOcodePluginsConfig`, `SaveOcodeLocalModels`)
- Modify: `internal/server/handler_config.go` (add `HandleGetPluginsEnabledConfig` / `HandleSetPluginsEnabledConfig`, `HandleGetLocalModelsConfig` / `HandleSetLocalModelsConfig`)
- Modify: `internal/server/server.go` (wrappers + routes)
- Test: `internal/server/handler_config_test.go`

**Interfaces:**
- Produces: `config.SaveOcodePluginsConfig(cfg config.PluginsConfig) error`, `config.SaveOcodeLocalModels(models map[string]config.LocalModelConfig) error`; routes `GET/PUT /api/config/ocode/plugins-enabled`, `GET/PUT /api/config/ocode/local-models`.
- Note: this is distinct from `ExternalPlugins`, already served by the existing `/api/plugins*` routes — do not touch those.

- [x] **Step 1: Write the failing tests**

```go
func TestHandleSetPluginsEnabledConfigPersists(t *testing.T) {
	h := testConfigHandler(t)

	body := `{"ast":true}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/ocode/plugins-enabled", strings.NewReader(body))
	h.HandleSetPluginsEnabledConfig(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	h.mu.Lock()
	got := h.cfg.Ocode.Plugins
	h.mu.Unlock()
	if !got.AST {
		t.Errorf("in-memory cfg not updated: %+v", got)
	}
}

func TestHandleSetLocalModelsConfigPersists(t *testing.T) {
	h := testConfigHandler(t)

	body := `{"local/bonsai-8b-1bit":{"enabled":true,"max_parallel":2}}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/ocode/local-models", strings.NewReader(body))
	h.HandleSetLocalModelsConfig(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	h.mu.Lock()
	got := h.cfg.Ocode.LocalModels
	h.mu.Unlock()
	entry, ok := got["local/bonsai-8b-1bit"]
	if !ok || !entry.Enabled || entry.MaxParallel != 2 {
		t.Errorf("in-memory cfg not updated: %+v", got)
	}
}
```

- [x] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server/... -run TestHandleSetPluginsEnabledConfigPersists -v`
Run: `go test ./internal/server/... -run TestHandleSetLocalModelsConfigPersists -v`
Expected: both FAIL — handlers undefined.

- [x] **Step 3: Add the setters**

```go
// SaveOcodePluginsConfig persists the opt-in builtin-tool toggles (e.g. AST).
func SaveOcodePluginsConfig(cfg PluginsConfig) error {
	return withOcodeConfigLock(func(c *OcodeConfig) error {
		c.Plugins = cfg
		return nil
	})
}

// SaveOcodeLocalModels persists the full set of registered local model instances.
func SaveOcodeLocalModels(models map[string]LocalModelConfig) error {
	return withOcodeConfigLock(func(c *OcodeConfig) error {
		c.LocalModels = models
		return nil
	})
}
```

`Plugins` is already conditionally written (`if cfg.Plugins.AST { payload["plugins"] = cfg.Plugins }`) — no payload-map change needed there. Run `grep -n '"local_models"' internal/config/ocodeconfig.go`; add `LocalModels` to the payload map if missing, following neighboring entries.

- [x] **Step 4: Add the handlers**

```go
func (h *Handler) HandleGetPluginsEnabledConfig(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	cfg := config.PluginsConfig{}
	if h.cfg != nil {
		cfg = h.cfg.Ocode.Plugins
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, cfg)
}

func (h *Handler) HandleSetPluginsEnabledConfig(w http.ResponseWriter, r *http.Request) {
	var req config.PluginsConfig
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := config.SaveOcodePluginsConfig(req); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
		return
	}
	h.mu.Lock()
	if h.cfg != nil {
		h.cfg.Ocode.Plugins = req
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, req)
}

func (h *Handler) HandleGetLocalModelsConfig(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	models := map[string]config.LocalModelConfig{}
	if h.cfg != nil && h.cfg.Ocode.LocalModels != nil {
		models = h.cfg.Ocode.LocalModels
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, models)
}

func (h *Handler) HandleSetLocalModelsConfig(w http.ResponseWriter, r *http.Request) {
	var req map[string]config.LocalModelConfig
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := config.SaveOcodeLocalModels(req); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
		return
	}
	h.mu.Lock()
	if h.cfg != nil {
		h.cfg.Ocode.LocalModels = req
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, req)
}
```

- [x] **Step 5: Add wrappers and routes**

```go
func (s *Server) handleGetPluginsEnabledConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetPluginsEnabledConfig(w, r)
}
func (s *Server) handleSetPluginsEnabledConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetPluginsEnabledConfig(w, r)
}
func (s *Server) handleGetLocalModelsConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGetLocalModelsConfig(w, r)
}
func (s *Server) handleSetLocalModelsConfig(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetLocalModelsConfig(w, r)
}
```

```go
s.mux.HandleFunc("GET /api/config/ocode/plugins-enabled", s.authMiddleware(s.handleGetPluginsEnabledConfig))
s.mux.HandleFunc("PUT /api/config/ocode/plugins-enabled", s.authMiddleware(s.handleSetPluginsEnabledConfig))
s.mux.HandleFunc("GET /api/config/ocode/local-models", s.authMiddleware(s.handleGetLocalModelsConfig))
s.mux.HandleFunc("PUT /api/config/ocode/local-models", s.authMiddleware(s.handleSetLocalModelsConfig))
```

- [x] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/server/... -run TestHandleSetPluginsEnabledConfigPersists -v` — expect PASS
Run: `go test ./internal/server/... -run TestHandleSetLocalModelsConfigPersists -v` — expect PASS
Run: `go build ./...` — expect no errors

- [x] **Step 7: Commit**

```bash
git add internal/config/ocodeconfig.go internal/server/handler_config.go internal/server/server.go internal/server/handler_config_test.go
git commit -m "feat(server): add /api/config/ocode/plugins-enabled and /local-models endpoints"
```

---

### Task 13: Extend the Advisor endpoint to cover Provider/ClaudeCode/Checkpoints

**Files:**
- Modify: `internal/server/handler_config.go` (extend `HandleGetAdvisor` / `HandleSetAdvisor`, `handler_config.go:160-197`)
- Test: `internal/server/handler_config_test.go`

**Interfaces:**
- Consumes: existing `HandleGetAdvisor`/`HandleSetAdvisor` at `internal/server/handler_config.go:160,170` — read the current implementation in full before editing (it currently reads/writes `Advisor.Model` only).
- Produces: same routes `GET/PUT /api/config/advisor`, response/request body grows to include `provider`, `claude_code`, `checkpoints` alongside the existing `model` field. Partial-update-safe: a PUT that omits `provider`/`claude_code`/`checkpoints` must not clear them (existing callers may still send `{"model": "..."}` only).

- [x] **Step 1: Read the current implementation before editing**

Run: `sed -n '155,200p' internal/server/handler_config.go` and read the full existing `HandleGetAdvisor`/`HandleSetAdvisor` bodies plus whatever `config.Save*` function they currently call for `Advisor.Model`, so the extension below matches its existing style exactly (field names, error handling, whether it already uses pointer types).

- [x] **Step 2: Write the failing test**

```go
func TestHandleSetAdvisorPreservesUnsetFields(t *testing.T) {
	h := testConfigHandler(t)
	h.mu.Lock()
	h.cfg.Ocode.Advisor = config.AdvisorConfig{
		Model: "old-model", Provider: "anthropic", ClaudeCode: true, Checkpoints: []string{"done"},
	}
	h.mu.Unlock()

	// Only model is sent — provider/claude_code/checkpoints must be preserved.
	body := `{"model":"new-model"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/advisor", strings.NewReader(body))
	h.HandleSetAdvisor(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	h.mu.Lock()
	got := h.cfg.Ocode.Advisor
	h.mu.Unlock()
	if got.Model != "new-model" {
		t.Errorf("Model = %q, want new-model", got.Model)
	}
	if got.Provider != "anthropic" || !got.ClaudeCode || len(got.Checkpoints) != 1 {
		t.Errorf("unset fields were clobbered: %+v", got)
	}
}

func TestHandleSetAdvisorUpdatesNewFields(t *testing.T) {
	h := testConfigHandler(t)

	body := `{"model":"m","provider":"claude-code","claude_code":true,"checkpoints":["done","plan"]}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/advisor", strings.NewReader(body))
	h.HandleSetAdvisor(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	h.mu.Lock()
	got := h.cfg.Ocode.Advisor
	h.mu.Unlock()
	if got.Provider != "claude-code" || !got.ClaudeCode || len(got.Checkpoints) != 2 {
		t.Errorf("new fields not applied: %+v", got)
	}
}
```

- [x] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/server/... -run TestHandleSetAdvisorPreservesUnsetFields -v`
Run: `go test ./internal/server/... -run TestHandleSetAdvisorUpdatesNewFields -v`
Expected: both FAIL (current handler only reads/writes `model`, so `Provider`/`ClaudeCode`/`Checkpoints` stay zero-valued regardless of input — the preserve test fails because `got.Provider` becomes `""` instead of staying `"anthropic"`, and the update test fails because `got.Provider` stays `""` instead of becoming `"claude-code"`).

- [x] **Step 4: Extend the handler**

Replace the request struct and body of `HandleSetAdvisor` (keep `HandleGetAdvisor`'s existing shape but add the three new fields to its response) so that unset request fields fall back to the current on-disk/in-memory value rather than zeroing:

```go
func (h *Handler) HandleGetAdvisor(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	adv := config.AdvisorConfig{}
	if h.cfg != nil {
		adv = h.cfg.Ocode.Advisor
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"model":       adv.Model,
		"provider":    adv.Provider,
		"claude_code": adv.ClaudeCode,
		"checkpoints": adv.Checkpoints,
	})
}

func (h *Handler) HandleSetAdvisor(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model       *string  `json:"model"`
		Provider    *string  `json:"provider"`
		ClaudeCode  *bool    `json:"claude_code"`
		Checkpoints []string `json:"checkpoints"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	h.mu.Lock()
	current := config.AdvisorConfig{}
	if h.cfg != nil {
		current = h.cfg.Ocode.Advisor
	}
	h.mu.Unlock()

	next := current
	if req.Model != nil {
		next.Model = *req.Model
	}
	if req.Provider != nil {
		next.Provider = *req.Provider
	}
	if req.ClaudeCode != nil {
		next.ClaudeCode = *req.ClaudeCode
	}
	if req.Checkpoints != nil {
		next.Checkpoints = req.Checkpoints
	}

	if err := config.SaveOcodeAdvisorConfig(next); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
		return
	}

	h.mu.Lock()
	if h.cfg != nil {
		h.cfg.Ocode.Advisor = next
	}
	h.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"model": next.Model, "provider": next.Provider,
		"claude_code": next.ClaudeCode, "checkpoints": next.Checkpoints,
	})
}
```

If Step 1 found the existing handler already calls a `config.Save...` function for `Model` alone, add a new `internal/config/ocodeconfig.go` setter `SaveOcodeAdvisorConfig(cfg AdvisorConfig) error` (same `withOcodeConfigLock` shape as `SaveOcodeCompactConfig` in Task 3) and switch the handler to call it with the full merged `next` value, rather than trying to reuse the narrower existing setter.

- [x] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/server/... -run TestHandleSetAdvisorPreservesUnsetFields -v` — expect PASS
Run: `go test ./internal/server/... -run TestHandleSetAdvisorUpdatesNewFields -v` — expect PASS
Run: `go build ./...` — expect no errors
Run: `go test ./internal/server/...` (full package) — expect no regressions in any pre-existing advisor test

- [x] **Step 6: Commit**

```bash
git add internal/config/ocodeconfig.go internal/server/handler_config.go internal/server/handler_config_test.go
git commit -m "feat(server): extend /api/config/advisor to cover provider, claude_code, checkpoints"
```

---

### Task 14: Extend the Mask/Redaction endpoint to cover BaseURL/FailMode/AllowRemoteTier2/SkipLLMIfClean/CustomWords

**Files:**
- Modify: `internal/server/handler_config.go` (extend `HandleGetMaskConfig` / `HandleSetMaskConfig` — note: per prior research there may not be a combined `HandleSetMaskConfig`; the existing PUT surface is three separate endpoints, `/api/config/mask/enabled`, `/mode`, `/model`. Read `handler_config.go:423-500` in full before editing.)
- Test: `internal/server/handler_config_test.go`

**Interfaces:**
- Consumes: existing `HandleGetMaskConfig` (`handler_config.go:423`), `HandleSetMaskEnabled`/`HandleSetMaskMode`/`HandleSetMaskModel` (`handler_config.go:442,462,482`).
- Produces: `GET /api/config/mask` grows to include `base_url`, `fail_mode`, `allow_remote_tier2`, `skip_llm_if_clean`, `custom_words`; a new `PUT /api/config/mask/advanced` covers those five fields (added rather than overloading the three existing narrow PUT routes, which stay as-is for backward compatibility).

- [x] **Step 1: Read the current implementation before editing**

Run: `sed -n '420,500p' internal/server/handler_config.go` and read `HandleGetMaskConfig`, `HandleSetMaskEnabled`, `HandleSetMaskMode`, `HandleSetMaskModel` in full, plus whatever `config.Save*` functions they call, so the extension matches existing style.

- [x] **Step 2: Write the failing tests**

```go
func TestHandleGetMaskConfigIncludesAdvancedFields(t *testing.T) {
	h := testConfigHandler(t)
	h.mu.Lock()
	h.cfg.Ocode.Security.Redaction = config.RedactionConfig{
		Enabled: true, Model: "m", BaseURL: "http://localhost:11434", FailMode: "block",
		AllowRemoteTier2: true, CustomWords: []string{"secret1"},
	}
	h.mu.Unlock()

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/config/mask", nil)
	h.HandleGetMaskConfig(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp struct {
		BaseURL          string   `json:"base_url"`
		FailMode         string   `json:"fail_mode"`
		AllowRemoteTier2 bool     `json:"allow_remote_tier2"`
		CustomWords      []string `json:"custom_words"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.BaseURL != "http://localhost:11434" || resp.FailMode != "block" || !resp.AllowRemoteTier2 || len(resp.CustomWords) != 1 {
		t.Errorf("advanced fields missing from GET response: %+v", resp)
	}
}

func TestHandleSetMaskAdvancedPersists(t *testing.T) {
	h := testConfigHandler(t)
	h.mu.Lock()
	h.cfg.Ocode.Security.Redaction = config.RedactionConfig{Enabled: true, Mode: "lenient", Model: "m"}
	h.mu.Unlock()

	body := `{"base_url":"http://localhost:1234","fail_mode":"warn","allow_remote_tier2":true,"custom_words":["foo","bar"]}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/mask/advanced", strings.NewReader(body))
	h.HandleSetMaskAdvanced(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	h.mu.Lock()
	got := h.cfg.Ocode.Security.Redaction
	h.mu.Unlock()
	if got.BaseURL != "http://localhost:1234" || got.FailMode != "warn" || !got.AllowRemoteTier2 || len(got.CustomWords) != 2 {
		t.Errorf("advanced fields not updated: %+v", got)
	}
	// Enabled/Mode/Model set by the other endpoints must survive untouched.
	if !got.Enabled || got.Mode != "lenient" || got.Model != "m" {
		t.Errorf("unrelated fields were clobbered: %+v", got)
	}
}
```

- [x] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/server/... -run TestHandleGetMaskConfigIncludesAdvancedFields -v`
Run: `go test ./internal/server/... -run TestHandleSetMaskAdvancedPersists -v`
Expected: first FAILs on the assertion (fields come back zero-valued from the current narrower response); second FAILs to compile (`h.HandleSetMaskAdvanced` undefined).

- [x] **Step 4: Extend `HandleGetMaskConfig`'s response and add `HandleSetMaskAdvanced`**

Add the four new fields to whatever response map/struct `HandleGetMaskConfig` currently builds (found in Step 1), alongside its existing `enabled`/`mode`/`model`:

```go
// (inside HandleGetMaskConfig, added to the existing response construction)
"base_url":           redaction.BaseURL,
"fail_mode":           redaction.FailMode,
"allow_remote_tier2":  redaction.AllowRemoteTier2,
"custom_words":        redaction.CustomWords,
```

Add a new setter and handler, following `SaveOcodePermissions`'s load-preserve-merge shape (Task 4's pattern) since this endpoint must not clobber `Enabled`/`Mode`/`Model`, which are owned by the three existing narrow endpoints:

```go
// internal/config/ocodeconfig.go — new setter near the other Security setters
// SaveOcodeRedactionAdvanced persists BaseURL/FailMode/AllowRemoteTier2/
// CustomWords, preserving Enabled/Mode/Model (owned by the mask/enabled,
// mask/mode, mask/model endpoints) from whatever is currently on disk.
func SaveOcodeRedactionAdvanced(baseURL, failMode string, allowRemoteTier2 bool, customWords []string) error {
	return withOcodeConfigLock(func(c *OcodeConfig) error {
		c.Security.Redaction.BaseURL = baseURL
		c.Security.Redaction.FailMode = failMode
		c.Security.Redaction.AllowRemoteTier2 = allowRemoteTier2
		c.Security.Redaction.CustomWords = customWords
		return nil
	})
}
```

```go
// internal/server/handler_config.go
func (h *Handler) HandleSetMaskAdvanced(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BaseURL          string   `json:"base_url"`
		FailMode         string   `json:"fail_mode"`
		AllowRemoteTier2 bool     `json:"allow_remote_tier2"`
		CustomWords      []string `json:"custom_words"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := config.SaveOcodeRedactionAdvanced(req.BaseURL, req.FailMode, req.AllowRemoteTier2, req.CustomWords); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
		return
	}
	h.mu.Lock()
	if h.cfg != nil {
		h.cfg.Ocode.Security.Redaction.BaseURL = req.BaseURL
		h.cfg.Ocode.Security.Redaction.FailMode = req.FailMode
		h.cfg.Ocode.Security.Redaction.AllowRemoteTier2 = req.AllowRemoteTier2
		h.cfg.Ocode.Security.Redaction.CustomWords = req.CustomWords
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"base_url": req.BaseURL, "fail_mode": req.FailMode,
		"allow_remote_tier2": req.AllowRemoteTier2, "custom_words": req.CustomWords,
	})
}
```

- [x] **Step 5: Add the wrapper and route for the new PUT endpoint**

```go
func (s *Server) handleSetMaskAdvanced(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleSetMaskAdvanced(w, r)
}
```

```go
s.mux.HandleFunc("PUT /api/config/mask/advanced", s.authMiddleware(s.handleSetMaskAdvanced))
```

(No new GET route — the extended `GET /api/config/mask` from Step 4 already covers reads.)

- [x] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/server/... -run TestHandleGetMaskConfigIncludesAdvancedFields -v` — expect PASS
Run: `go test ./internal/server/... -run TestHandleSetMaskAdvancedPersists -v` — expect PASS
Run: `go build ./...` — expect no errors
Run: `go test ./internal/server/...` (full package) — expect no regressions in any pre-existing mask test

- [x] **Step 7: Commit**

```bash
git add internal/config/ocodeconfig.go internal/server/handler_config.go internal/server/server.go internal/server/handler_config_test.go
git commit -m "feat(server): extend mask config with base_url, fail_mode, allow_remote_tier2, custom_words"
```

---

## Final verification (after all 14 tasks)

- [x] Run the full server test suite: `go test ./internal/server/... -v` — expect all PASS, no regressions.
- [x] Run the full config package test suite: `go test ./internal/config/... -v` — expect all PASS.
- [x] Run `go build ./...` and `go vet ./...` — expect no errors.
- [x] Grep for every new route path to confirm all 20 new/extended endpoints are registered exactly once: `grep -n '/api/config/ocode/\|/api/config/mask/advanced' internal/server/server.go`.
- [x] Cross-check the resulting route list against the "new"/"extend" rows of section 3 in `docs/superpowers/specs/2026-08-11-configuration-ui-design.md` — every row must have a matching route.
