package redact

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// IsLocalEndpoint checks if a URL points to a local endpoint (localhost/127.0.0.1/::1).
func IsLocalEndpoint(baseURL string) bool {
	u, err := url.Parse(baseURL)
	if err != nil {
		return false
	}

	hostname := u.Hostname()
	if hostname == "" {
		return false
	}

	// Check for localhost
	if hostname == "localhost" {
		return true
	}

	// Check for IP addresses
	ip := parseIP(hostname)
	if ip == nil {
		return false
	}

	// Check for loopback addresses
	return ip.IsLoopback()
}

// QuickSecretPatterns returns a compiled regex matching common secret formats.
// Used as a fast pre-pass before the expensive LLM tier-2 scan. When the regex
// finds nothing the message is almost certainly clean, and the LLM scan can be
// skipped (configurable via skip_llm_if_clean).
var QuickSecretPatterns = regexp.MustCompile(`(?i)` +
	// AWS keys (AKIA..., ASIA..., etc.)
	`(?:AKIA|ASIA|ABIA|ACCA|APKA|AIDA|AIPA|ANPA|ANVA|APKA)[0-9A-Z]{16}` + `|` +
	// OpenAI / Anthropic / generic API keys
	`sk-[A-Za-z0-9]{20,}` + `|` +
	`sk-ant-[A-Za-z0-9]{20,}` + `|` +
	// GitHub tokens (ghp_, gho_, ghu_, ghs_, ghf_)
	`gh[posuf]_[A-Za-z0-9]{36,}` + `|` +
	// GitLab tokens
	`glpat-[A-Za-z0-9\-]{20,}` + `|` +
	// Slack tokens
	`(?:xox[bprsa]-[A-Za-z0-9\-]{10,})` + `|` +
	// Google API keys
	`AIza[0-9A-Za-z\-_]{35}` + `|` +
	// Heroku API keys
	`[hH][eE][rR][oO][kK][uU].*[0-9A-F]{8}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{12}` + `|` +
	// Generic "password", "secret", "token" assignments in code
	`["\']?(?:password|passwd|pwd|secret|token|api[_-]?key)["\']?\s*[:=]\s*["\'][^"\']{8,}["\']` + `|` +
	// PEM / SSH private key blocks
	`-----BEGIN (?:RSA |EC |DSA )?PRIVATE KEY-----` + `|` +
	// JWT tokens (three base64url segments separated by dots)
	`eyJ[A-Za-z0-9\-_]{10,}\.[A-Za-z0-9\-_]{10,}\.[A-Za-z0-9\-_]{10,}` + `|` +
	// Stripe live/test keys
	`(?:rk|sk)_(?:live|test)_[A-Za-z0-9]{10,}` + `|` +
	// Twilio / SendGrid / generic alphanumeric with prefix patterns
	`(?:SG\.[A-Za-z0-9\-_]{20,}|SK[A-Za-z0-9]{32,})`,
)

// QuickScan runs a fast regex-based check for common secret patterns.
// Returns true if any potential secret is found, indicating the message
// warrants a more expensive LLM tier-2 scan (or manual review).
// False means the message passed the quick check and is almost certainly clean.
func QuickScan(text string) bool {
	return QuickSecretPatterns.MatchString(text)
}

// parseIP parses an IP address string.
func parseIP(s string) net.IP {
	ip := net.ParseIP(s)
	return ip
}

// LLMScanner scans text using a local LLM.
type LLMScanner struct {
	BaseURL     string
	Model       string
	AllowRemote bool
}

// scanRequest is the request payload for the OpenAI-compatible API.
type scanRequest struct {
	Model    string        `json:"model"`
	Messages []scanMessage `json:"messages"`
}

type scanMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// scanResponse is the response from the OpenAI-compatible API.
type scanResponse struct {
	Choices []scanChoice `json:"choices"`
}

type scanChoice struct {
	Message scanMessage `json:"message"`
}

// Scan sends the masked text to a local LLM for tier-2 scanning.
func (s *LLMScanner) Scan(maskedText string) ([]Span, error) {
	if s == nil {
		return nil, fmt.Errorf("scanner: nil scanner")
	}
	if !s.AllowRemote && !IsLocalEndpoint(s.BaseURL) {
		return nil, fmt.Errorf("scanner: endpoint %q is not local (security policy)", s.BaseURL)
	}

	systemPrompt := `You are a secret detection assistant. Analyze the following text for secrets, API keys, passwords, or other sensitive information that may have been partially masked.

Return a JSON array of exact secret substrings found in the text. If no secrets are found, return an empty array [].

Rules:
- Return ONLY verbatim substrings from the input text
- Do NOT return tokens or placeholders (like [[OCSEC:...]])
- Do NOT return any text that is already masked with ***
- Do NOT hallucinate or guess secrets
- Return raw JSON array, no markdown formatting`

	req := scanRequest{
		Model: s.Model,
		Messages: []scanMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: maskedText},
		},
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("scanner: marshal request: %w", err)
	}

	endpoint := strings.TrimRight(s.BaseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequest("POST", endpoint, bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("scanner: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("scanner: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("scanner: unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var scanResp scanResponse
	if err := json.NewDecoder(resp.Body).Decode(&scanResp); err != nil {
		return nil, fmt.Errorf("scanner: decode response: %w", err)
	}

	if len(scanResp.Choices) == 0 {
		return nil, fmt.Errorf("scanner: no choices in response")
	}

	content := scanResp.Choices[0].Message.Content
	return parseScannerOutput(content, maskedText)
}

// parseScannerOutput parses the JSON array from the scanner and returns verified spans.
func parseScannerOutput(output, input string) ([]Span, error) {
	// Try to extract JSON array from markdown code blocks if present
	output = strings.TrimSpace(output)
	if strings.HasPrefix(output, "```") {
		lines := strings.Split(output, "\n")
		var jsonLines []string
		inCodeBlock := false
		for _, line := range lines {
			if strings.HasPrefix(line, "```") {
				inCodeBlock = !inCodeBlock
				continue
			}
			if inCodeBlock {
				jsonLines = append(jsonLines, line)
			}
		}
		output = strings.TrimSpace(strings.Join(jsonLines, "\n"))
	}

	var secrets []string
	if err := json.Unmarshal([]byte(output), &secrets); err != nil {
		return nil, fmt.Errorf("scanner: parse output: %w", err)
	}

	// Verify each secret is actually present in the input (no hallucinations)
	var spans []Span
	for _, secret := range secrets {
		if secret == "" {
			continue
		}

		// Skip if it looks like a token or placeholder
		if strings.Contains(secret, "[[OCSEC:") || strings.Contains(secret, "***") {
			log.Printf("scanner: dropped token/placeholder: %s", maskString(secret))
			continue
		}

		// Find in input
		idx := strings.Index(input, secret)
		if idx == -1 {
			log.Printf("scanner: dropped hallucinated span at offset %d (not in input)", len(spans))
			continue
		}

		spans = append(spans, Span{
			Start: idx,
			End:   idx + len(secret),
			Kind:  "model",
		})
	}

	return spans, nil
}

// maskString masks a string for logging (never log raw secrets).
func maskString(s string) string {
	if len(s) <= 6 {
		return "***"
	}
	return s[:3] + "***" + s[len(s)-3:]
}
