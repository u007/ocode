package agent

import (
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

// ResolveToolArgs resolves OCSEC tokens in tool call arguments.
func (sr *SessionRedactor) ResolveToolArgs(args string) (string, []redact.SecretRef) {
	if sr == nil || sr.redactor == nil || !sr.redactor.Enabled() {
		return args, nil
	}

	registry := sr.redactor.Registry()
	if registry == nil {
		return args, nil
	}

	// Find all tokens in the args
	var refs []redact.SecretRef
	resolved := args

	// Use a regex to find and replace tokens
	for _, token := range redact.TokenPattern.FindAllString(args, -1) {
		// Check if this token belongs to our session
		tokens, indexes := redact.TokensForNonce(token, sr.nonce)
		if len(tokens) == 0 {
			continue
		}

		// Resolve the token
		entry, ok := registry.Lookup(indexes[0])
		if !ok {
			continue
		}

		// Create masked preview for the ref
		preview := redact.MaskedPreview(entry.Value)
		refs = append(refs, redact.SecretRef{
			Index: indexes[0],
			Kind:  entry.Kind,
			Value: preview,
		})

		// Replace in resolved string
		resolved = strings.ReplaceAll(resolved, token, entry.Value)
	}

	return resolved, refs
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
