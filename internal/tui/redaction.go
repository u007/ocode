package tui

import (
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
	m.redactionEnabled = newState
	return nil
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
