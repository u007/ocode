package redact

import (
	"testing"
)

func TestDetectKnownFormats(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected []string // expected kind values
	}{
		{"AWS key", "AKIAIOSFODNN7EXAMPLE", []string{"aws_key"}},
		{"GitHub PAT", "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdef1234", []string{"github_token"}},
		{"GitHub OAuth", "gho_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdef1234", []string{"github_token"}},
		{"Slack bot", "xoxb-1234567890-1234567890123-AbCdEfGhIjKlMnOpQrStUvWx", []string{"slack_token"}},
		{"Stripe live", "sk_live_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij", []string{"stripe_key"}},
		{"JWT", "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U", []string{"jwt"}},
		{"OpenAI key", "sk-abcdefghijklmnopqrstuvwxyz1234567890AB", []string{"openai_key"}},
		{"Anthropic key", "sk-ant-api03abcdefghijklmnopqrstuvwxyz1234567890ABCD", []string{"anthropic_key"}},
		{"PEM key", "-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA...\n-----END RSA PRIVATE KEY-----", []string{"pem_key"}},
		{"URL creds", "https://user:password123@example.com/api", []string{"url_credentials"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spans := Detect(tt.text, nil, DetectOpts{})
			if len(spans) == 0 {
				t.Errorf("Detect(%q) returned no spans", tt.text)
				return
			}
			for _, expectedKind := range tt.expected {
				found := false
				for _, s := range spans {
					if s.Kind == expectedKind {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Detect(%q) missing kind %q, got %v", tt.text, expectedKind, spans)
				}
			}
		})
	}
}

func TestDetectFalsePositives(t *testing.T) {
	// False positive guard: these should NOT match
	text := "commit SHA: " + "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2" +
		"\nsha512-abc123def456abc123def456abc123def456abc123def456abc123def456abc123def456abc123def456abc123def456abc123def456abc123def456abc123def456abc123def456"

	spans := Detect(text, nil, DetectOpts{})
	for _, s := range spans {
		if s.Kind == "aws_key" || s.Kind == "github_token" || s.Kind == "slack_token" ||
			s.Kind == "stripe_key" || s.Kind == "jwt" || s.Kind == "openai_key" ||
			s.Kind == "anthropic_key" || s.Kind == "pem_key" || s.Kind == "url_credentials" {
			// Check if it overlaps with the safe spans
			if s.Start >= len("commit SHA: ") && s.End <= len(text) {
				// This is in the safe region - shouldn't have matched
				t.Errorf("False positive: %q (kind=%s) at [%d:%d]", text[s.Start:s.End], s.Kind, s.Start, s.End)
			}
		}
	}
}

func TestDetectKeywordEntropyChatMode(t *testing.T) {
	// High-entropy string adjacent to keyword should match in chat mode
	text := "password = AbC123456789012345678901234567890"
	spans := Detect(text, nil, DetectOpts{FileContent: false})
	
	keywordSpan := false
	for _, s := range spans {
		if s.Kind == "keyword_entropy: password" {
			keywordSpan = true
			break
		}
	}
	if !keywordSpan {
		t.Errorf("Expected keyword_entropy span in chat mode, got %v", spans)
	}

	// In file mode, keyword entropy should NOT match
	spans = Detect(text, nil, DetectOpts{FileContent: true})
	for _, s := range spans {
		if s.Kind == "keyword_entropy: password" {
			t.Error("keyword_entropy should not match in file mode")
		}
	}
}

func TestDetectCustomWords(t *testing.T) {
	text := "my-secret-value is here"
	spans := Detect(text, []string{"my-secret-value"}, DetectOpts{})
	
	if len(spans) != 1 {
		t.Errorf("Expected 1 span for custom word, got %d: %v", len(spans), spans)
		return
	}
	if spans[0].Kind != "custom" {
		t.Errorf("Expected kind 'custom', got %q", spans[0].Kind)
	}
	// "my-secret-value" is 15 characters
	if spans[0].Start != 0 || spans[0].End != 15 {
		t.Errorf("Expected span [0:15], got [%d:%d]", spans[0].Start, spans[0].End)
	}
}
