package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/u007/ocode/internal/redact"
)

// SessionRedactor holds the redactor state for a session.
type SessionRedactor struct {
	redactor *redact.Redactor
	nonce    string
}

// NewSessionRedactor creates a new session redactor.
func NewSessionRedactor(cfg *redact.RedactorConfig, sessionID, projectSlug, vaultBase string) (*SessionRedactor, error) {
	r, err := redact.NewSessionRedactor(*cfg, sessionID, projectSlug, vaultBase)
	if err != nil {
		return nil, fmt.Errorf("create session redactor: %w", err)
	}

	return &SessionRedactor{
		redactor: r,
		nonce:    r.Registry().Nonce(),
	}, nil
}

// RedactChat applies chat-mode redaction to text.
func (sr *SessionRedactor) RedactChat(text string) (string, error) {
	return sr.redactor.RedactChat(text)
}

// RedactFile applies file-mode redaction to text.
func (sr *SessionRedactor) RedactFile(text string) (string, error) {
	return sr.redactor.RedactFile(text)
}

// Render resolves tokens back to real values for display.
func (sr *SessionRedactor) Render(text string) string {
	return sr.redactor.Render(text)
}

// Enabled returns whether redaction is enabled.
func (sr *SessionRedactor) Enabled() bool {
	return sr.redactor.Enabled()
}

// SetEnabled enables or disables redaction at runtime.
func (sr *SessionRedactor) SetEnabled(enabled bool) {
	sr.redactor.SetEnabled(enabled)
}

// Registry returns the underlying registry.
func (sr *SessionRedactor) Registry() *redact.Registry {
	return sr.redactor.Registry()
}

// Redactor returns the underlying redactor.
func (sr *SessionRedactor) Redactor() *redact.Redactor {
	return sr.redactor
}

// IsEgressCommand checks if a command might exfiltrate data.
func IsEgressCommand(cmd string) bool {
	// Common exfiltration tools
	egressTools := []string{
		"curl", "wget", "nc", "ncat", "netcat",
		"ssh", "scp", "rsync",
		"telnet", "socat",
	}

	// Check for URL patterns
	if strings.Contains(cmd, "http://") || strings.Contains(cmd, "https://") {
		// Check if it's a GET/POST to external URL
		if strings.Contains(cmd, "curl") || strings.Contains(cmd, "wget") {
			return true
		}
	}

	// Check for exfiltration tools
	cmdLower := strings.ToLower(cmd)
	for _, tool := range egressTools {
		if strings.Contains(cmdLower, tool+" ") || strings.HasPrefix(cmdLower, tool+" ") {
			return true
		}
	}

	return false
}

// RedactMessage applies redaction to a message based on its role.
func RedactMessage(msg Message, sr *SessionRedactor) (Message, error) {
	if sr == nil || !sr.Enabled() {
		return msg, nil
	}

	var redacted Message = msg
	var err error

	switch msg.Role {
	case "user", "tool":
		// Chat mode for user messages and tool results
		redacted.Content, err = sr.RedactChat(msg.Content)
	case "system":
		// File mode for system messages (context files, etc.)
		redacted.Content, err = sr.RedactFile(msg.Content)
	case "assistant":
		// Assistant messages should already be redacted by the LLM
		// But we check anyway for safety
		redacted.Content, err = sr.RedactChat(msg.Content)
	}

	return redacted, err
}

// ResolveToolArgs is a standalone function that resolves OCSEC tokens in tool
// arguments using a registry. This is used by the agent's executeToolCall.
func ResolveToolArgs(args json.RawMessage, registry *redact.Registry) json.RawMessage {
	if registry == nil || len(args) == 0 {
		return args
	}

	// Check if there are any tokens in the args
	argsStr := string(args)
	if !redact.TokenPattern.MatchString(argsStr) {
		return args
	}

	// Resolve the tokens
	resolved := registry.Resolve(argsStr)
	return json.RawMessage(resolved)
}
