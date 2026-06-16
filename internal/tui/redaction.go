package tui

import (
	"fmt"
	"strings"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/redact"
)

// Tier-2 scanner defaults.
const (
	// defaultScanSkipLLMIfClean skips the expensive LLM tier-2 scan when
	// the fast regex pre-pass finds no obvious secrets. This avoids doubling
	// LLM latency on every normal chat message. The legacy config key
	// skip_llm_if_clean is deprecated in favour of mode ("lenient"/"full").
	// fail_mode is orthogonal — it only controls behaviour on scanner error.
	defaultScanSkipLLMIfClean = true
)

// initRedaction initializes the redaction state from config.
func (m *model) initRedaction() {
	if m.config == nil {
		return
	}
	m.redactionEnabled = m.config.Ocode.Security.Redaction.Enabled
	m.redactionModel = normalizeRedactionModelName(m.config.Ocode.Security.Redaction.Model, m.config.Ocode.Security.Redaction.BaseURL)
}

// toggleRedaction enables/disables redaction and persists the setting.
func (m *model) toggleRedaction() error {
	return m.setRedactionEnabled(!m.redactionEnabled)
}

// scrubExistingMessages applies redaction to all existing user messages in the session.
// This is called when redaction is enabled mid-session.
func (m *model) scrubExistingMessages() {
	if m.redactionRegistry == nil {
		return
	}
	for i := range m.messages {
		if m.messages[i].role == roleUser && m.messages[i].text != "" {
			// Check if already redacted (contains OCSEC tokens)
			if !redact.TokenPattern.MatchString(m.messages[i].text) {
				m.messages[i].text = redactText(m.messages[i].text, m.redactionRegistry)
			}
		}
	}
}

// isRedactionEnabled returns whether redaction is currently enabled.
func (m *model) isRedactionEnabled() bool {
	return m.redactionEnabled
}

// getRedactionModel returns the configured local model for tier-2 scanning.
func (m *model) getRedactionModel() string {
	return m.redactionModel
}

// rebuildRedactionScanner refreshes the tier-2 scanner from the current config.
func (m *model) rebuildRedactionScanner() {
	if m == nil || m.config == nil {
		m.llmScanner = nil
		return
	}
	rc := m.config.Ocode.Security.Redaction
	if m.redactionModel == "" || rc.BaseURL == "" {
		m.llmScanner = nil
		return
	}
	m.llmScanner = buildLLMScanner(rc.BaseURL, m.redactionModel, rc.AllowRemoteTier2)
}

// syncRedactionRuntime applies the current redaction state to the live agent.
func (m *model) syncRedactionRuntime() {
	if m == nil || m.agent == nil {
		return
	}
	m.agent.SetRedactionEnabled(m.redactionEnabled)
	m.agent.SetRedactionRegistry(m.redactionRegistry)
	if m.redactionEnabled && m.redactionRegistry != nil {
		m.agent.SetRedactionHook(redact.NetHookEnabled(m.redactionRegistry))
	} else {
		m.agent.SetRedactionHook(nil)
	}
	if m.redactionEnabled {
		m.agent.SetRedactionScanner(m.llmScanner)
	} else {
		m.agent.SetRedactionScanner(nil)
	}
}

// setRedactionEnabled persists the enabled flag and updates live state.
func (m *model) setRedactionEnabled(enabled bool) error {
	if err := config.SaveSecurityRedaction(func(rc *config.RedactionConfig) {
		rc.Enabled = enabled
	}); err != nil {
		return err
	}
	prev := m.redactionEnabled
	if enabled && !prev {
		if m.redactionRegistry == nil {
			m.redactionRegistry = redact.NewRegistry(redact.NewNonce())
		}
		m.scrubExistingMessages()
		m.rebuildRedactionScanner()
	}
	m.redactionEnabled = enabled
	m.syncRedactionRuntime()
	return nil
}

// setRedactionMode persists the tier-2 aggressiveness mode.
func (m *model) setRedactionMode(mode string) error {
	if mode != "lenient" && mode != "full" {
		return fmt.Errorf("Invalid mode. Use: lenient or full")
	}
	if err := config.SaveSecurityRedaction(func(rc *config.RedactionConfig) {
		rc.Mode = mode
	}); err != nil {
		return err
	}
	m.redactMode = mode
	return nil
}

// defaultRedactionBaseURL returns the default base URL for a model if its
// provider is a known local server (e.g. lmstudio → http://localhost:1234/v1).
// Returns "" when the provider has no local default.
func defaultRedactionBaseURL(modelName string) string {
	if modelName == "" {
		return ""
	}
	// Extract provider prefix: "lmstudio/ternary-bonsai-8b-mlx" → "lmstudio"
	provider, _ := config.SplitProviderModel(modelName)
	switch provider {
	case "lmstudio":
		return "http://localhost:1234/v1"
	}
	return ""
}

// normalizeRedactionModelName returns the canonical persisted/display name for
// the tier-2 model. Bare LM Studio ids are normalized to lmstudio/<name> when
// the configured base_url points at the default LM Studio endpoint.
func normalizeRedactionModelName(modelName, baseURL string) string {
	if modelName == "" {
		return ""
	}
	if strings.Contains(modelName, "/") {
		return modelName
	}
	switch strings.TrimRight(baseURL, "/") {
	case "http://localhost:1234/v1", "http://localhost:1234":
		return "lmstudio/" + modelName
	}
	return modelName
}

// scannerRequestModelName returns the model id to send to the tier-2 scanner.
// LM Studio expects the bare model name, even when the persisted/display name
// is lmstudio/<name>.
func scannerRequestModelName(modelName string) string {
	if strings.HasPrefix(modelName, "lmstudio/") {
		return strings.TrimPrefix(modelName, "lmstudio/")
	}
	return modelName
}

// setRedactionModel persists the tier-2 model and refreshes the runtime scanner.
// If the model's provider has a known local default base URL (e.g. lmstudio)
// and no base_url is currently configured, the base_url is auto-set.
func (m *model) setRedactionModel(modelName string) error {
	// Compute the new base_url and normalized model from the current in-memory config.
	// SaveSecurityRedaction reads from disk, so we must derive from m.config first,
	// then persist the result.
	baseURL := ""
	if m.config != nil {
		baseURL = m.config.Ocode.Security.Redaction.BaseURL
	}
	if baseURL == "" {
		if def := defaultRedactionBaseURL(modelName); def != "" {
			agent.DebugAppendf("REDACT", "auto-set tier-2 scanner base_url to %q for model %q", def, modelName)
			baseURL = def
		}
	}
	normalized := normalizeRedactionModelName(modelName, baseURL)

	if err := config.SaveSecurityRedaction(func(rc *config.RedactionConfig) {
		rc.BaseURL = baseURL
		rc.Model = normalized
	}); err != nil {
		return err
	}
	// Update in-memory config to match what was persisted.
	if m.config != nil {
		m.config.Ocode.Security.Redaction.BaseURL = baseURL
		m.config.Ocode.Security.Redaction.Model = normalized
	}
	m.redactionModel = normalized
	m.rebuildRedactionScanner()
	m.syncRedactionRuntime()
	return nil
}

// redactText applies redaction to a text string using the session registry.
// Returns the redacted text and the registry for later resolution.
func redactText(text string, reg *redact.Registry) string {
	if reg == nil || text == "" {
		return text
	}
	spans := redact.Detect(text, nil, redact.DetectOpts{FileContent: false})
	agent.DebugAppendf("REDACT", "redactText: Detect found %d spans in %q", len(spans), text)
	if len(spans) == 0 {
		return text
	}
	for _, span := range spans {
		value := text[span.Start:span.End]
		agent.DebugAppendf("REDACT", "redactText: registering %q kind=%q", value, span.Kind)
		reg.GetOrAssign(value, span.Kind, "tui")
	}
	result := reg.Substitute(text)
	agent.DebugAppendf("REDACT", "redactText: substituted → %q", result)
	return result
}

// buildLLMScanner creates a tier-2 LLM scanner that calls a local model server.
// Returns nil when no base URL or model is configured.
// allowRemote overrides the local-endpoint security check for users running
// tier-2 scanning through a Docker bridge, Tailscale tunnel, or LAN proxy.
func buildLLMScanner(baseURL, model string, allowRemote bool) *redact.LLMScanner {
	if baseURL == "" || model == "" {
		return nil
	}
	if !allowRemote && !redact.IsLocalEndpoint(baseURL) {
		agent.DebugAppendf("REDACT", "tier-2 scanner: base_url %q is not a local endpoint; skipping (security policy — set security.redaction.allow_remote_tier2=true to allow)", baseURL)
		return nil
	}
	if !allowRemote {
		agent.DebugAppendf("REDACT", "tier-2 scanner: base_url %q accepted (local endpoint)", baseURL)
	} else {
		agent.DebugAppendf("REDACT", "tier-2 scanner: base_url %q accepted (remote endpoints allowed by config)", baseURL)
	}

	// Fetch API key from auth store for providers that require authentication.
	// The model may have a provider prefix (e.g. "lmstudio/local-scan").
	var apiKey string
	provider := extractProvider(model)
	if provider != "" {
		if key := auth.ResolveKey(provider); key != "" {
			apiKey = key
			agent.DebugAppendf("REDACT", "tier-2 scanner: resolved API key for provider %q", provider)
		} else if cred, ok := auth.Get(provider); ok && cred.Key != "" {
			apiKey = cred.Key
			agent.DebugAppendf("REDACT", "tier-2 scanner: using stored API key for provider %q", provider)
		}
	}

	return &redact.LLMScanner{BaseURL: baseURL, Model: scannerRequestModelName(model), AllowRemote: allowRemote, APIKey: apiKey}
}

// extractProvider extracts the provider prefix from a model name.
// e.g. "lmstudio/local-scan" → "lmstudio", "gpt-4o" → "".
func extractProvider(model string) string {
	provider, _ := config.SplitProviderModel(model)
	return provider
}

// applyTier2Scan runs the tier-2 LLM scanner on the most recent user message
// in agentMsgs. Any newly identified secrets are registered into reg so the
// tier-1 NetHook will substitute them before the content reaches the LLM.
//
// failMode controls behaviour on scanner error: "block" returns the error to
// the caller (message will not be sent); "warn" logs the error and continues.
// mode controls aggressiveness: "full" always scans; "lenient" scans only when
// WarrantsLLMScan detects a sensitive keyword or value pattern.
//
// MUTATES: overwrites msg.Content for the scanned message with token-substituted text.
// Returns nil on success or when the scanner is skipped; returns an error when
// failMode is "block" and the scanner actually fails.
func applyTier2Scan(agentMsgs []agent.Message, scanner redact.Scanner, reg *redact.Registry, failMode string, mode string) error {
	if scanner == nil {
		return nil
	}
	// Find the last user message.
	for i := len(agentMsgs) - 1; i >= 0; i-- {
		msg := &agentMsgs[i]
		if msg.Role != "user" || strings.TrimSpace(msg.Content) == "" {
			continue
		}
		// Apply tier-1 to get the masked text for the scanner.
		masked := redactText(msg.Content, reg)

		// Mode gate: "lenient" skips the LLM when the message has no
		// sensitive keywords or value patterns; "full" always scans.
		if mode == "lenient" && !redact.WarrantsLLMScan(masked) {
			agent.DebugAppendf("REDACT", "tier-2 scan skipped (lenient mode: no warrant)")
			return nil
		}

		spans, err := scanner.Scan(masked)
		if err != nil {
			agent.DebugAppendf("REDACT", "tier-2 scan error: %v", err)
			if failMode == "block" {
				return fmt.Errorf("tier-2 scanner blocked: %w", err)
			}
			// "warn" mode: log and continue without additional secrets.
			return nil
		}
		for _, span := range spans {
			val := masked[span.Start:span.End]
			if !redact.TokenPattern.MatchString(val) {
				reg.GetOrAssign(val, "model", "scanner")
			}
		}
		// Re-substitute this message with the now-expanded registry.
		msg.Content = reg.Substitute(msg.Content)
		return nil
	}
	return nil
}

// applyTier1UserRedaction runs tier-1 keyword+entropy detection on the last user
// message in agentMsgs. This is called unconditionally before the LLM call (and
// before any tier-2 scan) so that common password/secret patterns are masked
// even when no tier-2 scanner is configured.
//
// MUTATES: overwrites msg.Content for the last user message with token-substituted text.
func applyTier1UserRedaction(agentMsgs []agent.Message, reg *redact.Registry) {
	if reg == nil {
		agent.DebugAppendf("REDACT", "tier-1 skip: registry is nil")
		return
	}
	for i := len(agentMsgs) - 1; i >= 0; i-- {
		msg := &agentMsgs[i]
		if msg.Role != "user" || strings.TrimSpace(msg.Content) == "" {
			continue
		}
		agent.DebugAppendf("REDACT", "tier-1 user scan: %q", msg.Content)
		masked := redactText(msg.Content, reg)
		changed := masked != msg.Content
		if changed {
			agent.DebugAppendf("REDACT", "tier-1 user redacted: %q → %q", msg.Content, masked)
		} else {
			agent.DebugAppendf("REDACT", "tier-1 user: no secrets found in %d chars", len(msg.Content))
		}
		msg.Content = masked
		return
	}
}

// renderSecrets replaces OCSEC tokens in text with masked previews for display.
// The owner can see partial secrets (e.g., "AKIA***7EXAMPLE") while the
// actual value remains in the registry.
func renderSecrets(text string, reg *redact.Registry) string {
	if reg == nil || text == "" {
		return text
	}
	if !redact.TokenPattern.MatchString(text) {
		return text
	}

	result := text
	nonce := reg.Nonce()
	// Find all tokens and replace with masked previews
	for _, match := range redact.TokenPattern.FindAllString(text, -1) {
		// Parse token to get index using TokensForNonce
		_, indexes := redact.TokensForNonce(match, nonce)
		if len(indexes) == 0 {
			continue
		}
		idx := indexes[0]

		if entry, ok := reg.Lookup(idx); ok {
			preview := redact.MaskedPreview(entry.Value)
			result = strings.ReplaceAll(result, match, preview)
		}
	}
	return result
}
