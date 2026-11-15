package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/u007/ocode/internal/config"
)

func TestElevenLabsMCPPresetUsesStreamableCIMD(t *testing.T) {
	preset := elevenLabsMCPPreset()
	if preset.Type != "http" {
		t.Errorf("Type = %q, want http (Streamable HTTP)", preset.Type)
	}
	if preset.URL != "https://api.elevenlabs.io/v1/mcp" {
		t.Errorf("URL = %q", preset.URL)
	}
	if !preset.Enabled {
		t.Error("preset should be enabled")
	}
	if preset.OAuth == nil {
		t.Fatal("preset has no OAuth config")
	}
	if preset.OAuth.AuthorizationURL != "https://elevenlabs.io/app/oauth/authorize" {
		t.Errorf("AuthorizationURL = %q", preset.OAuth.AuthorizationURL)
	}
	if preset.OAuth.TokenURL != "https://api.us.elevenlabs.io/v1/oauth/token" {
		t.Errorf("TokenURL = %q", preset.OAuth.TokenURL)
	}
	if preset.OAuth.ClientID != elevenLabsMCPClientMetadataURL {
		t.Errorf("ClientID = %q, want the CIMD URL", preset.OAuth.ClientID)
	}
	if len(preset.OAuth.Scopes) == 0 {
		t.Error("preset should request scopes")
	}
}

// isolateServerConfig points HOME/XDG at a temp tree AND moves the process into
// a temp working directory. The chdir is load-bearing: ActiveConfigPath prefers
// a project opencode.json found by walking up from cwd, so without it these
// tests write into the repository's own (gitignored) config.
func isolateServerConfig(t *testing.T) {
	t.Helper()
	setHomeTree(t, t.TempDir())
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
}

func TestInstallElevenLabsMCPWritesAndEnables(t *testing.T) {
	isolateServerConfig(t)

	h := testHandlerWithConfig(t)
	preset, err := h.installElevenLabsMCP()
	if err != nil {
		t.Fatalf("installElevenLabsMCP: %v", err)
	}
	if !preset.Enabled || preset.Type != "http" {
		t.Fatalf("installed = %+v", preset)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, ok := cfg.MCP["elevenlabs"]
	if !ok {
		t.Fatal("elevenlabs was not persisted")
	}
	if got.URL != elevenLabsMCPURL || got.Type != "http" || !got.Enabled {
		t.Errorf("persisted = %+v", got)
	}
	if got.OAuth == nil || got.OAuth.ClientID != elevenLabsMCPClientMetadataURL {
		t.Errorf("persisted OAuth = %+v", got.OAuth)
	}

	// A second install stays canonical.
	again, err := h.installElevenLabsMCP()
	if err != nil {
		t.Fatalf("second install: %v", err)
	}
	if again.OAuth == nil || again.OAuth.ClientID != elevenLabsMCPClientMetadataURL {
		t.Errorf("second install = %+v", again)
	}
}

// A placeholder entry (authorization_url "a", token_url "b", client_id "c") is
// exactly what a half-finished manual config looks like, and it used to survive
// Connect. The preset must replace it.
func TestInstallElevenLabsMCPReplacesPlaceholderEntry(t *testing.T) {
	isolateServerConfig(t)

	h := testHandlerWithConfig(t)
	h.mu.Lock()
	h.cfg.MCP = map[string]config.MCPConfig{
		"elevenlabs": {
			Type:    "remote",
			URL:     "https://api.us.elevenlabs.io/v1/mcp",
			Enabled: false,
			OAuth:   &config.MCPOAuthConfig{AuthorizationURL: "a", TokenURL: "b", ClientID: "c"},
		},
	}
	h.mu.Unlock()

	got, err := h.installElevenLabsMCP()
	if err != nil {
		t.Fatalf("installElevenLabsMCP: %v", err)
	}
	if got.Type != "http" || got.URL != elevenLabsMCPURL {
		t.Errorf("placeholder transport survived: %+v", got)
	}
	if got.OAuth == nil || got.OAuth.AuthorizationURL != "https://elevenlabs.io/app/oauth/authorize" ||
		got.OAuth.TokenURL != "https://api.us.elevenlabs.io/v1/oauth/token" ||
		got.OAuth.ClientID != elevenLabsMCPClientMetadataURL {
		t.Errorf("placeholder OAuth survived: %+v", got.OAuth)
	}
	if !got.Enabled {
		t.Error("connect did not enable the entry")
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if persisted := cfg.MCP["elevenlabs"]; persisted.OAuth == nil || persisted.OAuth.ClientID != elevenLabsMCPClientMetadataURL {
		t.Errorf("persisted entry = %+v", persisted)
	}
}

// User-owned fields survive a reconnect; endpoint and OAuth identity do not.
func TestInstallElevenLabsMCPPreservesUserFields(t *testing.T) {
	isolateServerConfig(t)

	h := testHandlerWithConfig(t)
	h.mu.Lock()
	h.cfg.MCP = map[string]config.MCPConfig{
		"elevenlabs": {
			Type:    "http",
			URL:     "https://eu.api.elevenlabs.io/v1/mcp",
			Enabled: false,
			Timeout: 45000,
			Headers: map[string]string{"X-Custom": "1"},
			OAuth:   &config.MCPOAuthConfig{AuthorizationURL: "x", TokenURL: "y", ClientID: "z"},
		},
	}
	h.mu.Unlock()

	got, err := h.installElevenLabsMCP()
	if err != nil {
		t.Fatalf("installElevenLabsMCP: %v", err)
	}
	if got.Timeout != 45000 {
		t.Errorf("timeout = %d, want the user's 45000", got.Timeout)
	}
	if got.Headers["X-Custom"] != "1" {
		t.Errorf("headers = %v, want the user's X-Custom", got.Headers)
	}
	if got.URL != elevenLabsMCPURL {
		t.Errorf("URL = %q, want the canonical endpoint", got.URL)
	}
	if got.OAuth == nil || got.OAuth.ClientID != elevenLabsMCPClientMetadataURL {
		t.Errorf("OAuth = %+v, want the canonical CIMD identity", got.OAuth)
	}
	if !got.Enabled {
		t.Error("connect did not enable the entry")
	}
}

func TestConnectElevenLabsRefusesRemoteCaller(t *testing.T) {
	h := testHandlerWithConfig(t)
	req := httptest.NewRequest(http.MethodPost, "/api/mcp/elevenlabs/connect", nil)
	req.RemoteAddr = "203.0.113.5:1234"
	rec := httptest.NewRecorder()

	h.HandleConnectElevenLabs(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestConnectElevenLabsRefusesPluginProvidedServer(t *testing.T) {
	h := testHandlerWithConfig(t)
	h.mu.Lock()
	h.pluginMCP = map[string]string{"elevenlabs": "some-plugin"}
	h.mu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/mcp/elevenlabs/connect", nil)
	req.RemoteAddr = "127.0.0.1:5555"
	rec := httptest.NewRecorder()

	h.HandleConnectElevenLabs(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}
