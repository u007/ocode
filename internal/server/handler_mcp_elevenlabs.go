package server

import (
	"fmt"
	"log"
	"net/http"

	"github.com/u007/ocode/internal/config"
)

// ElevenLabs' hosted MCP server. Verified 2026-11-15: it accepts POST only
// (Streamable HTTP, not the legacy SSE transport), rejects `xi-api-key`, and
// answers 401 with `resource_metadata=...`. Its authorization server has no
// dynamic client registration but advertises
// `client_id_metadata_document_supported: true`, so the OAuth client_id is the
// URL of a Client ID Metadata Document (hosted in this repo) rather than a
// registered client id.
const (
	elevenLabsMCPName = "elevenlabs"
	elevenLabsMCPURL  = "https://api.elevenlabs.io/v1/mcp"
	// jsDelivr serves the repo file as application/json, which a CIMD must be.
	// GitHub's raw host serves text/plain.
	elevenLabsMCPClientMetadataURL = "https://cdn.jsdelivr.net/gh/u007/ocode@main/client-metadata.json"
)

// elevenLabsMCPPreset is the MCP entry the connect flow installs.
func elevenLabsMCPPreset() config.MCPConfig {
	return config.MCPConfig{
		Type:    "http",
		URL:     elevenLabsMCPURL,
		Enabled: true,
		// Generations of images/video/speech can run well past the 20s default.
		Timeout: 60000,
		OAuth: &config.MCPOAuthConfig{
			AuthorizationURL: "https://elevenlabs.io/app/oauth/authorize",
			TokenURL:         "https://api.us.elevenlabs.io/v1/oauth/token",
			ClientID:         elevenLabsMCPClientMetadataURL,
			Scopes: []string{
				"text_to_speech",
				"image_video_generation",
				"flows",
				"voice_generation",
				"speech_history_read",
				"user_read",
			},
		},
	}
}

// mergeElevenLabsPreset keeps the fields a user owns (timeout, extra headers,
// environment) and takes the endpoint, transport and OAuth identity from the
// preset. Connect is the canonical installer for this entry, so a stale,
// placeholder or SSE-era value cannot survive it.
func mergeElevenLabsPreset(existing config.MCPConfig) config.MCPConfig {
	preset := elevenLabsMCPPreset()
	if existing.Timeout > 0 {
		preset.Timeout = existing.Timeout
	}
	if len(existing.Headers) > 0 {
		preset.Headers = existing.Headers
	}
	if len(existing.Environment) > 0 {
		preset.Environment = existing.Environment
	}
	return preset
}

// installElevenLabsMCP writes the canonical preset (merged with user-owned
// fields), enables it, and mirrors the result into the live config. It always
// saves: a placeholder or half-configured entry must not block the flow.
func (h *Handler) installElevenLabsMCP() (config.MCPConfig, error) {
	h.mu.Lock()
	cfg := h.cfg
	existing, exists := config.MCPConfig{}, false
	if cfg != nil {
		existing, exists = cfg.MCP[elevenLabsMCPName]
	}
	h.mu.Unlock()

	if cfg == nil {
		return config.MCPConfig{}, fmt.Errorf("config not loaded")
	}

	preset := elevenLabsMCPPreset()
	if exists {
		preset = mergeElevenLabsPreset(existing)
	}
	if err := config.SaveMCPServer(elevenLabsMCPName, preset); err != nil {
		return config.MCPConfig{}, err
	}

	h.mu.Lock()
	if h.cfg != nil {
		if h.cfg.MCP == nil {
			h.cfg.MCP = make(map[string]config.MCPConfig)
		}
		h.cfg.MCP[elevenLabsMCPName] = preset
	}
	h.mu.Unlock()
	return preset, nil
}

// HandleConnectElevenLabs installs the ElevenLabs MCP preset (once) and starts
// the browser OAuth flow, mirroring POST /api/mcp/{name}/auth: 202 with a job
// id, then GET /api/mcp/auth/{id} carries the outcome. The OAuth callback only
// reaches the machine running ocode, so non-loopback callers are refused.
func (h *Handler) HandleConnectElevenLabs(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		writeError(w, http.StatusForbidden, "MCP OAuth must run on the machine hosting ocode")
		return
	}

	h.mu.Lock()
	fromPlugin := ""
	if h.pluginMCP != nil {
		fromPlugin = h.pluginMCP[elevenLabsMCPName]
	}
	h.mu.Unlock()
	if fromPlugin != "" {
		writeError(w, http.StatusBadRequest, "the elevenlabs MCP server is provided by a plugin")
		return
	}

	preset, err := h.installElevenLabsMCP()
	if err != nil {
		log.Printf("mcp: install elevenlabs preset: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to save MCP server config")
		return
	}

	oauth := preset.OAuth
	if oauth == nil || oauth.AuthorizationURL == "" || oauth.TokenURL == "" || oauth.ClientID == "" {
		writeError(w, http.StatusInternalServerError, "stored elevenlabs MCP entry has no usable OAuth configuration")
		return
	}

	jobID := h.mcpAuthJobs.start(elevenLabsMCPName, &mcpAuthConfig{
		AuthorizationURL: oauth.AuthorizationURL,
		TokenURL:         oauth.TokenURL,
		ClientID:         oauth.ClientID,
		Scopes:           oauth.Scopes,
	})
	writeJSON(w, http.StatusAccepted, mcpAuthStartResponse{
		JobID:    jobID,
		Server:   elevenLabsMCPName,
		Status:   "running",
		Browsers: "A browser window opens for the ElevenLabs sign-in. Approve access, then this page refreshes.",
	})
}
