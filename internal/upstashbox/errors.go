package upstashbox

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// redacted replaces any secret in an error message.
const redacted = "[REDACTED]"

// maxErrorMessage caps the server message kept in an APIError.
const maxErrorMessage = 512

var (
	// urlPattern matches any URL in free text. A URL may carry a token, so the
	// whole URL is replaced rather than parsed.
	urlPattern = regexp.MustCompile(`(?i)\b(?:https?|wss?)://\S+`)
	// tokenPattern matches a bare token= fragment outside a URL.
	tokenPattern = regexp.MustCompile(`(?i)token=\S+`)
)

// APIError is a non-2xx response from the Upstash Box API. Callers test for it
// with errors.As. Message is the server's "error" field, with secrets removed.
type APIError struct {
	StatusCode int
	Method     string
	// Path is the request path without its query string.
	Path string
	// Message is the body's "error" string, or "request failed with status <code>"
	// when the body is not JSON or has no such field.
	Message string
}

// Error reports the method, path, status and message. It never includes the
// API key or any URL.
func (e *APIError) Error() string {
	return fmt.Sprintf("upstashbox: %s %s: HTTP %d: %s", e.Method, e.Path, e.StatusCode, e.Message)
}

// apiError builds an APIError from a non-2xx body. The message is the body's
// "error" string. A body that is not JSON, or has no non-empty string "error"
// field, gets "request failed with status <code>".
func (c *Client) apiError(method, path string, status int, body []byte) error {
	var payload struct {
		Error string `json:"error"`
	}
	msg := ""
	if err := json.Unmarshal(body, &payload); err == nil && payload.Error != "" {
		msg = c.redact(payload.Error)
	}
	if msg == "" {
		msg = fmt.Sprintf("request failed with status %d", status)
	}
	if runes := []rune(msg); len(runes) > maxErrorMessage {
		msg = string(runes[:maxErrorMessage])
	}
	return &APIError{StatusCode: status, Method: method, Path: path, Message: msg}
}

// redact removes the API key, every URL and every token= value from s.
func (c *Client) redact(s string) string {
	if c.apiKey != "" {
		s = strings.ReplaceAll(s, c.apiKey, redacted)
	}
	s = urlPattern.ReplaceAllString(s, "<url>")
	return tokenPattern.ReplaceAllString(s, "token="+redacted)
}
