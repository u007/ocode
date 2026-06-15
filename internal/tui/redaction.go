package tui

import (
	"strings"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/redact"
)

// initRedaction initializes the redaction state from config.
func (m *model) initRedaction() {
	if m.config == nil {
		return
	}
	m.redactionEnabled = m.config.Ocode.Security.Redaction.Enabled
	m.redactionModel = m.config.Ocode.Security.Redaction.Model
}

// toggleRedaction enables/disables redaction and persists the setting.
func (m *model) toggleRedaction() error {
	newState := !m.redactionEnabled
	err := config.SaveSecurityRedaction(func(rc *config.RedactionConfig) {
		rc.Enabled = newState
	})
	if err != nil {
		return err
	}
	
	// If enabling redaction mid-session, scrub existing user messages
	if newState && !m.redactionEnabled {
		if m.redactionRegistry == nil {
			m.redactionRegistry = redact.NewRegistry(redact.NewNonce())
		}
		m.scrubExistingMessages()
	}
	
	m.redactionEnabled = newState
	return nil
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

// redactText applies redaction to a text string using the session registry.
// Returns the redacted text and the registry for later resolution.
func redactText(text string, reg *redact.Registry) string {
	if reg == nil || text == "" {
		return text
	}
	spans := redact.Detect(text, nil, redact.DetectOpts{FileContent: false})
	if len(spans) == 0 {
		return text
	}
	for _, span := range spans {
		value := text[span.Start:span.End]
		reg.GetOrAssign(value, span.Kind, "tui")
	}
	return reg.Substitute(text)
}


// buildLLMScanner creates a tier-2 LLM scanner that calls a local model server.
// Returns nil when no base URL or model is configured.
func buildLLMScanner(baseURL, model string) *redact.LLMScanner {
	if baseURL == "" || model == "" {
		return nil
	}
	if !redact.IsLocalEndpoint(baseURL) {
		agent.DebugAppendf("REDACT", "tier-2 scanner: base_url %q is not a local endpoint; skipping (security policy)", baseURL)
		return nil
	}
	return &redact.LLMScanner{BaseURL: baseURL, Model: model}
}

// applyTier2Scan runs the tier-2 LLM scanner on the most recent user message
// in agentMsgs. Any newly identified secrets are registered into reg so the
// tier-1 NetHook will substitute them before the content reaches the LLM.
// MUTATES: overwrites msg.Content for the scanned message with token-substituted text.
func applyTier2Scan(agentMsgs []agent.Message, scanner redact.Scanner, reg *redact.Registry) {
	if scanner == nil {
		return
	}
	// Find the last user message.
	for i := len(agentMsgs) - 1; i >= 0; i-- {
		msg := &agentMsgs[i]
		if msg.Role != "user" || strings.TrimSpace(msg.Content) == "" {
			continue
		}
		// Apply tier-1 to get the masked text for the scanner.
		masked := redactText(msg.Content, reg)
		spans, err := scanner.Scan(masked)
		if err != nil {
			agent.DebugAppendf("REDACT", "tier-2 scan error: %v", err)
			return
		}
		for _, span := range spans {
			val := masked[span.Start:span.End]
			if !redact.TokenPattern.MatchString(val) {
				reg.GetOrAssign(val, "model", "scanner")
			}
		}
		// Re-substitute this message with the now-expanded registry.
		msg.Content = reg.Substitute(msg.Content)
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
