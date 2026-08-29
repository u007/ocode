package redact

import (
	"strings"
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

// hasEnvSecretSpan reports whether Detect produced an env_secret span covering
// the given value substring.
func hasEnvSecretSpan(spans []Span, text, value string) bool {
	vi := strings.Index(text, value)
	if vi < 0 {
		return false
	}
	vEnd := vi + len(value)
	for _, s := range spans {
		if strings.HasPrefix(s.Kind, "env_secret:") && s.Start <= vi && s.End >= vEnd {
			return true
		}
	}
	return false
}

// hiEntropy returns a deterministic high-entropy-looking alphanumeric string of
// length n. It is used to build test fixtures without embedding literal secret
// values in the source (which the redactor would otherwise mask). The charset is
// built with a loop so the source never contains a long uppercase run that the
// redactor would mask.
func hiEntropy(n int) string {
	var chars []byte
	for c := byte('A'); c <= 'Z'; c++ {
		chars = append(chars, c)
	}
	for c := byte('a'); c <= 'z'; c++ {
		chars = append(chars, c)
	}
	for c := byte('0'); c <= '9'; c++ {
		chars = append(chars, c)
	}
	var b strings.Builder
	b.Grow(n)
	for i := 0; i < n; i++ {
		b.WriteByte(chars[(i*7+3)%len(chars)])
	}
	return b.String()
}

func TestDetectEnvSecretAssignments(t *testing.T) {
	// Real-world .env leak cases from the bug report. Values are generated at
	// runtime (hiEntropy) so the source contains no literal secrets. quoted marks
	// assignments whose value is wrapped in quotes and may contain spaces.
	cases := []struct {
		name   string
		value  string
		quoted byte // 0 = unquoted, '"' = double-quoted, '\'' = single-quoted
	}{
		{"ENCRYPTION_KEY", hiEntropy(64), 0},
		{"AWS_ACCESS_KEY_ID", hiEntropy(20), 0},
		{"AWS_SECRET_ACCESS_KEY", hiEntropy(40), 0},
		{"NEXTAUTH_SECRET", hiEntropy(32), 0},
		{"CHUTES_API_KEY", hiEntropy(20), 0},
		{"export OPENAI_API_KEY", hiEntropy(24), 0},
		{"DB_PASSWORD", hiEntropy(16), 0},
		{"WEBHOOK_SECRET", hiEntropy(32) + "===", 0},
		// Short (4-7 char) strong-name values must still be redacted.
		{"PASSWORD", hiEntropy(7), 0},
		{"API_KEY", hiEntropy(6), 0},
		// Quoted values (with spaces) must be captured in full.
		{"ENCRYPTION_KEY", hiEntropy(8) + " " + hiEntropy(6) + " " + hiEntropy(5), '"'},
		{"SSH_KEY", hiEntropy(7) + " " + hiEntropy(7), '\''},
		{"DB_PASSWORD", hiEntropy(9), '"'},
	}

	var lines []string
	for _, c := range cases {
		switch c.quoted {
		case '"':
			lines = append(lines, c.name+`="`+c.value+`"`)
		case '\'':
			lines = append(lines, c.name+"='"+c.value+"'")
		default:
			lines = append(lines, c.name+"="+c.value)
		}
	}
	text := strings.Join(lines, "\n")

	// Must catch all of these in BOTH chat and file mode.
	for _, mode := range []struct {
		name string
		opts DetectOpts
	}{
		{"chat", DetectOpts{FileContent: false}},
		{"file", DetectOpts{FileContent: true}},
	} {
		t.Run(mode.name, func(t *testing.T) {
			spans := Detect(text, nil, mode.opts)
			for _, c := range cases {
				if !hasEnvSecretSpan(spans, text, c.value) {
					t.Errorf("[%s] expected env_secret span for %s, got spans %v", mode.name, c.name, spans)
				}
			}
		})
	}
}

func TestDetectEnvSecretFalsePositives(t *testing.T) {
	// These must NOT be redacted: non-secret names, low-entropy weak ids,
	// and prose containing "KEY" as a non-secret word.
	text := strings.Join([]string{
		`AWS_REGION=us-east-1`,
		`DB_HOST=localhost`,
		`DATABASE_PORT=5432`,
		`APP_URL=http://localhost:3201`,
		`DEPLOY_USER=mercstudio`,
		`SSH_HOST=s12.mercstudio.com`,
		`PROJECT_ID=prod`,
		`CLIENT_ID=1234567890`,
		`MONKEY=notasecretvalue`,
		`RESEND_FROM=onboarding@hub.mercstudio.com`,
		`the monkey held the key and opened the door`,
		`DEPLOY_USER='mercstudio'`,
		`DB_HOST="db.local"`,
		`APP_URL="http://localhost:3201"`,
	}, "\n")

	spans := Detect(text, nil, DetectOpts{})
	for _, c := range []string{
		"us-east-1", "localhost", "5432", "http://localhost:3201",
		"mercstudio", "s12.mercstudio.com", "prod", "1234567890",
		"notasecretvalue", "onboarding@hub.mercstudio.com", "db.local",
	} {
		if hasEnvSecretSpan(spans, text, c) {
			t.Errorf("false positive: %q should not be redacted, got spans %v", c, spans)
		}
	}
}

func TestDetectEnvSecretPaddedBase64AndPunct(t *testing.T) {
	// Values with base64 padding must be fully captured (the value class
	// includes '=', so the trailing padding is part of the span).
	value := hiEntropy(32) + "==="
	text := "WEBHOOK_SECRET=" + value
	spans := Detect(text, nil, DetectOpts{})
	if !hasEnvSecretSpan(spans, text, value) {
		t.Errorf("expected env_secret span covering padded base64 value %q, got %v", value, spans)
	}
}
