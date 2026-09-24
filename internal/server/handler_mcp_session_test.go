package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/config"
)

// mcpSessionTestHandler builds a handler whose h.cfg has two MCP servers, with
// HOME + the process workdir pointed at throwaway dirs that contain an
// opencode.json holding the same servers. This isolates config.SaveMCPEnabled
// (which resolves the project/global opencode.json) from the developer's real
// config, and gives the handler a valid config to mutate.
func mcpSessionTestHandler(t *testing.T) (*Handler, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())

	dir := t.TempDir()
	cfgJSON := `{"mcp":{"alpha":{"command":["echo","a"]},"beta":{"command":["echo","b"]}}}`
	if err := os.WriteFile(filepath.Join(dir, "opencode.json"), []byte(cfgJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	origWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origWd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	// config caches the workdir, so point it at the temp dir too.
	config.SetWorkDir(dir)

	h := NewHandler()
	h.mu.Lock()
	h.cfg = &config.Config{
		Ocode: config.OcodeConfig{},
		MCP: map[string]config.MCPConfig{
			"alpha": {Type: "local", Command: []string{"echo", "a"}, Enabled: true},
			"beta":  {Type: "local", Command: []string{"echo", "b"}, Enabled: false},
		},
	}
	h.mu.Unlock()
	return h, dir
}

// TestHandleSetMCPEnabledSessionScopedOverride proves the core per-chat
// behavior: a toggle carrying session_id records an override for THAT session
// only, persists the process-wide config (matching /mcp), and leaves other
// sessions reading the process-wide value.
func TestHandleSetMCPEnabledSessionScopedOverride(t *testing.T) {
	h, _ := mcpSessionTestHandler(t)
	const sid = "ses_test_chat"

	req := httptest.NewRequest("PUT", "/api/mcp/alpha/disable?session_id="+sid, nil)
	rec := httptest.NewRecorder()
	h.HandleSetMCPEnabled(rec, req, "alpha", false)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["session_id"] != sid {
		t.Errorf("response session_id = %q, want %q", resp["session_id"], sid)
	}
	if resp["status"] != "disabled" {
		t.Errorf("response status = %q, want disabled", resp["status"])
	}

	// The chat's override is recorded...
	if enabled, ok := h.mcpSessionOverride(sid, "alpha"); !ok || enabled {
		t.Errorf("session override = (%v, %v), want (false, true)", enabled, ok)
	}
	// ...but another session has none, so it follows the process-wide config.
	if _, ok := h.mcpSessionOverride("ses_other", "alpha"); ok {
		t.Error("unrelated session unexpectedly gained an override")
	}
	// The process-wide config was still persisted (durable, /mcp semantics).
	h.mu.Lock()
	global := h.cfg.MCP["alpha"].Enabled
	h.mu.Unlock()
	if global {
		t.Error("process-wide config not updated to disabled")
	}
}

// TestHandleListMCPSessionScoped proves GET /api/mcp?session_id= reports the
// chat's overridden state while the un-scoped list keeps the process-wide
// state.
func TestHandleListMCPSessionScoped(t *testing.T) {
	h, _ := mcpSessionTestHandler(t)
	const sid = "ses_list_chat"

	// Toggle beta ON for this chat only (process default is off).
	req := httptest.NewRequest("PUT", "/api/mcp/beta/enable?session_id="+sid, nil)
	rec := httptest.NewRecorder()
	h.HandleSetMCPEnabled(rec, req, "beta", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("set status = %d", rec.Code)
	}

	entryFor := func(raw []byte, name string) (bool, bool) {
		var entries []struct {
			Name    string `json:"name"`
			Enabled bool   `json:"enabled"`
		}
		if err := json.Unmarshal(raw, &entries); err != nil {
			t.Fatalf("unmarshal list: %v", err)
		}
		for _, e := range entries {
			if e.Name == name {
				return e.Enabled, true
			}
		}
		return false, false
	}

	// Session-scoped list reflects the override.
	req = httptest.NewRequest("GET", "/api/mcp?session_id="+sid, nil)
	rec = httptest.NewRecorder()
	h.HandleListMCP(rec, req)
	if enabled, ok := entryFor(rec.Body.Bytes(), "beta"); !ok || !enabled {
		t.Errorf("session-scoped list beta = (%v, %v), want (true, true)", enabled, ok)
	}

	// Un-scoped list keeps the process-wide config. (The process-wide config was
	// also persisted to enabled by the same call, so re-seed it to off to show
	// the two paths are independent.)
	h.mu.Lock()
	beta := h.cfg.MCP["beta"]
	beta.Enabled = false
	h.cfg.MCP["beta"] = beta
	h.mu.Unlock()
	req = httptest.NewRequest("GET", "/api/mcp", nil)
	rec = httptest.NewRecorder()
	h.HandleListMCP(rec, req)
	if enabled, ok := entryFor(rec.Body.Bytes(), "beta"); !ok || enabled {
		t.Errorf("unscoped list beta = (%v, %v), want (false, true)", enabled, ok)
	}
}

// TestApplyMCPSessionOverridesDoesNotMutateSharedConfig is the safety
// invariant: per-session overrides must be applied to a COPY, never the shared
// process-wide h.cfg.MCP map (which every other session reads).
func TestApplyMCPSessionOverridesDoesNotMutateSharedConfig(t *testing.T) {
	h, _ := mcpSessionTestHandler(t)
	const sid = "ses_copy_chat"

	h.setMCPSessionOverride(sid, "alpha", boolPtr(false))

	var cfg *config.Config
	h.mu.Lock()
	cfg = h.cfg
	h.mu.Unlock()

	eff := h.applyMCPSessionOverrides(cfg, sid)
	if eff.MCP["alpha"].Enabled {
		t.Error("effective copy should have alpha disabled")
	}
	if !cfg.MCP["alpha"].Enabled {
		t.Fatal("shared process-wide config was mutated by applyMCPSessionOverrides")
	}
	if eff == cfg {
		t.Fatal("expected a cloned config, got the shared pointer")
	}
}

// TestMCPToolsForSessionCacheFastPath proves a session without overrides
// returns nil (so buildAgentSession falls back to the process-wide cache)
// instead of triggering a fresh enumeration.
func TestMCPToolsForSessionCacheFastPath(t *testing.T) {
	h, _ := mcpSessionTestHandler(t)

	var cfg *config.Config
	h.mu.Lock()
	cfg = h.cfg
	h.mu.Unlock()

	tools, errs := h.mcpToolsForSession(cfg, "ses_no_override")
	if tools != nil || errs != nil {
		t.Errorf("expected nil result for override-less session, got tools=%v errs=%v", tools, errs)
	}

	// With an override the fast path is skipped (the call proceeds to the real
	// enumeration; with unreachable echo servers it returns whatever it got,
	// possibly empty — the point is only that it did NOT take the nil shortcut
	// blindly). We assert the override is visible to hasMCPSessionOverrides.
	h.setMCPSessionOverride("ses_has_override", "alpha", boolPtr(false))
	if !h.hasMCPSessionOverrides("ses_has_override") {
		t.Error("expected hasMCPSessionOverrides to report the recorded override")
	}
	if h.hasMCPSessionOverrides("ses_no_override") {
		t.Error("override-less session reported as having overrides")
	}
}

// TestClearMCPSessionOverrides proves the release path drops the map entry so
// it cannot leak one per session id.
func TestClearMCPSessionOverrides(t *testing.T) {
	h, _ := mcpSessionTestHandler(t)
	const sid = "ses_release_chat"

	h.setMCPSessionOverride(sid, "alpha", boolPtr(false))
	if !h.hasMCPSessionOverrides(sid) {
		t.Fatal("override not recorded")
	}
	h.clearMCPSessionOverrides(sid)
	if h.hasMCPSessionOverrides(sid) {
		t.Error("override survived clearMCPSessionOverrides")
	}
}
