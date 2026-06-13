package tui

import (
	"strings"

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

// resolveTokens resolves all OCSEC tokens in text using the session registry.
func resolveTokens(text string, reg *redact.Registry) string {
	if reg == nil || text == "" {
		return text
	}
	return reg.Resolve(text)
}

// redactMessage redacts a message's text and stores the original for later resolution.
func (m *model) redactMessage(msg *message, reg *redact.Registry) {
	if reg == nil || !m.redactionEnabled || msg.text == "" {
		return
	}
	// Only redact user messages
	if msg.role != roleUser {
		return
	}
	msg.text = redactText(msg.text, reg)
}

// appendUserMessage adds a user message with optional redaction.
func (m *model) appendUserMessage(text string) {
	msg := message{role: roleUser, text: text}
	if m.redactionEnabled && m.redactionRegistry != nil {
		m.redactMessage(&msg, m.redactionRegistry)
	}
	m.messages = append(m.messages, msg)
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
	// Find all tokens and replace with masked previews
	for _, match := range redact.TokenPattern.FindAllString(text, -1) {
		// Parse token to get index
		parts := redact.TokenPattern.FindStringSubmatch(match)
		if len(parts) < 1 {
			continue
		}
		// Extract index from token [[OCSEC:nonce:idx]]
		idxStr := match[len("[[OCSEC:")+6 : len(match)-3] // skip nonce, get :idx]
		idxStr = idxStr[strings.Index(idxStr, ":")+1:]     // remove nonce part
		idx := 0
		for _, c := range idxStr {
			if c >= '0' && c <= '9' {
				idx = idx*10 + int(c-'0')
			}
		}

		if entry, ok := reg.Lookup(idx); ok {
			preview := redact.MaskedPreview(entry.Value)
			result = strings.ReplaceAll(result, match, preview)
		}
	}
	return result
}
