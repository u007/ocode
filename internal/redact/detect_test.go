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

func TestDetectEnvSecretEmptyValue(t *testing.T) {
	// Empty values on secret-named variables must still produce spans.
	// Regression test for: mask fail on .env with variable AGENT48_PASS=
	cases := []struct {
		name   string
		quoted byte
	}{
		{"AGENT48_PASS", 0},
		{"DB_PASSWORD", 0},
		{"API_KEY", 0},
		{"ENCRYPTION_KEY", '"'},
		{"DB_PASSWORD", '\''},
	}

	var lines []string
	for _, c := range cases {
		switch c.quoted {
		case '"':
			lines = append(lines, c.name+`=""`)
		case '\'':
			lines = append(lines, c.name+"=''")
		default:
			lines = append(lines, c.name+"=")
		}
	}
	text := strings.Join(lines, "\n")

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
				// Find the line start for this assignment
				var lineStart int
				switch c.quoted {
				case '"':
					lineStart = strings.Index(text, c.name+`=""`)
				case '\'':
					lineStart = strings.Index(text, c.name+"=''")
				default:
					lineStart = strings.Index(text, c.name+"=")
				}
				if lineStart < 0 {
					t.Fatalf("[%s] could not find line for %s", mode.name, c.name)
				}
				// Value position is after the `=` (for unquoted) or after opening quote (for quoted)
				valPos := lineStart + len(c.name) + 1 // after `=`
				if c.quoted == '"' {
					valPos++ // after `="`
				} else if c.quoted == '\'' {
					valPos++ // after `='`
				}
				// For empty values, the span is zero-length at valPos.
				// For non-empty values, the span covers valPos..valPos+len(value).
				// We just verify there's an env_secret span starting at valPos.
				found := false
				var gotKinds []string
				for _, s := range spans {
					if strings.HasPrefix(s.Kind, "env_secret:") {
						gotKinds = append(gotKinds, s.Kind)
						if s.Start == valPos {
							found = true
						}
					}
				}
				if !found {
					t.Errorf("[%s] expected env_secret span at position %d for %s, got kinds %v spans %v", mode.name, valPos, c.name, gotKinds, spans)
				}
			}
		})
	}
}

func TestDetectEnvSecretNoNewlineLeak(t *testing.T) {
	// Regression test: an empty value must not cause the regex to "leak"
	// into the next line, consuming the next assignment as the value of
	// the current line. Each assignment must produce its own span.
	text := "AGENT48_PASS=\nDB_PASSWORD=actual-secret"

	for _, mode := range []struct {
		name string
		opts DetectOpts
	}{
		{"chat", DetectOpts{FileContent: false}},
		{"file", DetectOpts{FileContent: true}},
	} {
		t.Run(mode.name, func(t *testing.T) {
			spans := Detect(text, nil, mode.opts)
			// Must find exactly 2 env_secret spans (one per line).
			var envSpans []Span
			for _, s := range spans {
				if strings.HasPrefix(s.Kind, "env_secret:") {
					envSpans = append(envSpans, s)
				}
			}
			if len(envSpans) != 2 {
				t.Fatalf("expected 2 env_secret spans, got %d: %v", len(envSpans), spans)
			}
			// First span (AGENT48_PASS) must not extend past position 13
			// (i.e., must not include the newline or the next line).
			if envSpans[0].Start != 13 || envSpans[0].End != 13 {
				t.Errorf("span[0] for AGENT48_PASS should be zero-length at pos 13, got %d:%d", envSpans[0].Start, envSpans[0].End)
			}
			// Second span (DB_PASSWORD) must cover "actual-secret".
			if envSpans[1].Start != 26 || envSpans[1].End != 39 {
				t.Errorf("span[1] for DB_PASSWORD should cover actual-secret at 26:39, got %d:%d", envSpans[1].Start, envSpans[1].End)
			}
			// Verify the second span's kind references DB_PASSWORD.
			if !strings.Contains(envSpans[1].Kind, "DB_PASSWORD") {
				t.Errorf("span[1] Kind should reference DB_PASSWORD, got %q", envSpans[1].Kind)
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

func TestDetectEnvSecretJSONBlobValue(t *testing.T) {
	// Regression test: rclone (and other OAuth-based CLI configs) store the
	// live access/refresh token as a bare JSON object under an INI-style
	// `token = {...}` assignment. The unquoted-value alternative used to stop
	// at the value's first '"', capturing only the opening '{' and leaving the
	// actual access_token/refresh_token unmasked.
	value := `{"access_token":"` + hiEntropy(24) + `","refresh_token":"` + hiEntropy(24) + `","expiry":"2026-01-01T00:00:00Z"}`
	text := "client_id = 12345.apps.googleusercontent.com\ntoken = " + value + "\n"

	for _, mode := range []struct {
		name string
		opts DetectOpts
	}{
		{"chat", DetectOpts{FileContent: false}},
		{"file", DetectOpts{FileContent: true}},
	} {
		t.Run(mode.name, func(t *testing.T) {
			spans := Detect(text, nil, mode.opts)
			if !hasEnvSecretSpan(spans, text, value) {
				t.Errorf("[%s] expected env_secret span covering full JSON blob %q, got %v", mode.name, value, spans)
			}
		})
	}
}
