package agent

import (
	"fmt"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/redact"
)

// recordingScanner is a test Scanner that masks a single known novel value
// (one the tier-1 regex cannot match) and records how many times it is called.
type recordingScanner struct {
	target string
	calls  int
}

func (s *recordingScanner) Scan(maskedText string) ([]redact.Span, error) {
	s.calls++
	idx := strings.Index(maskedText, s.target)
	if idx < 0 {
		return nil, nil
	}
	return []redact.Span{{Start: idx, End: idx + len(s.target), Kind: "scanner"}}, nil
}

type failingScanner struct{ err error }

func (s *failingScanner) Scan(string) ([]redact.Span, error) { return nil, s.err }

func TestScanToolResult(t *testing.T) {
	const novel = "zzqxNovelSecretValue9999" // no known format, no keyword → tier-1 misses it

	t.Run("sensitive file read runs LLM scanner and masks in-place", func(t *testing.T) {
		sc := &recordingScanner{target: novel}
		a := &Agent{}
		a.SetRedactionEnabled(true)
		a.SetRedactionRegistry(redact.NewRegistry("nonce1"))
		a.SetRedactionScanner(sc)

		content := "API_BASE=https://api.example.com\nCUSTOM=" + novel + "\n"
		out := a.scanToolResult("read", `{"path":"/proj/.env.local"}`, content)

		if sc.calls != 1 {
			t.Fatalf("expected scanner called once, got %d", sc.calls)
		}
		if strings.Contains(out, novel) {
			t.Errorf("novel secret not masked in output: %q", out)
		}
	})

	t.Run("non-sensitive read does not call LLM and leaves keywordless value", func(t *testing.T) {
		sc := &recordingScanner{target: novel}
		a := &Agent{}
		a.SetRedactionEnabled(true)
		a.SetRedactionRegistry(redact.NewRegistry("nonce2"))
		a.SetRedactionScanner(sc)

		content := "var token = \"" + novel + "\""
		out := a.scanToolResult("read", `{"path":"/proj/main.go"}`, content)

		if sc.calls != 0 {
			t.Errorf("scanner must not run for non-sensitive read, got %d calls", sc.calls)
		}
		// chat-mode regex may mask the keyworded assignment, but must never invoke the LLM.
		_ = out
	})

	t.Run("bash output with high-entropy password masked by chat-mode regex (no LLM)", func(t *testing.T) {
		sc := &recordingScanner{target: novel}
		a := &Agent{}
		a.SetRedactionEnabled(true)
		a.SetRedactionRegistry(redact.NewRegistry("nonce3"))
		a.SetRedactionScanner(sc)

		// High-entropy value after a keyword is caught by chat-mode Detect.
		// (Low-entropy/dictionary passwords are a documented gap — see PLAN.)
		secret := "aK39fjZ20vQ81mLpWx"
		content := "connecting with password=" + secret + " to db"
		out := a.scanToolResult("bash", `{"command":"psql ..."}`, content)

		if sc.calls != 0 {
			t.Errorf("bash output must not invoke the LLM scanner, got %d calls", sc.calls)
		}
		if strings.Contains(out, secret) {
			t.Errorf("high-entropy password not masked in bash output: %q", out)
		}
	})

	t.Run("nil registry is a no-op", func(t *testing.T) {
		a := &Agent{}
		content := "password=hunter2secret"
		if out := a.scanToolResult("bash", `{}`, content); out != content {
			t.Errorf("expected no-op with nil registry, got %q", out)
		}
	})

	t.Run("sensitive file scan error falls back to masked output", func(t *testing.T) {
		a := &Agent{}
		a.SetRedactionEnabled(true)
		a.SetRedactionRegistry(redact.NewRegistry("a3f9c2"))
		a.SetRedactionScanner(&failingScanner{err: fmt.Errorf("scanner unavailable")})

		rawSecret := "sk-ant-12345678901234567890"
		content := "API_KEY=" + rawSecret + "\n"
		out := a.scanToolResult("read", `{"path":"/proj/.env"}`, content)

		if strings.Contains(out, rawSecret) {
			t.Fatalf("expected masked fallback on scanner error, got %q", out)
		}
		if !redact.TokenPattern.MatchString(out) {
			t.Fatalf("expected tokenized fallback on scanner error, got %q", out)
		}
	})
}

func TestSessionRedactorDisabled(t *testing.T) {
	sr := &SessionRedactor{
		redactor: redact.NewRedactor(redact.RedactorConfig{Enabled: false}, "", nil),
		nonce:    "test123",
	}

	if sr.Enabled() {
		t.Error("Expected redactor to be disabled")
	}

	text := "my password is AKIAIOSFODNN7EXAMPLE"
	masked, err := sr.RedactChat(text)
	if err != nil {
		t.Fatalf("RedactChat error: %v", err)
	}

	if masked != text {
		t.Error("Disabled redactor should not modify text")
	}
}

func TestIsEgressCommand(t *testing.T) {
	tests := []struct {
		cmd      string
		expected bool
	}{
		{"curl https://example.com", true},
		{"wget http://example.com/file", true},
		{"ssh user@host", true},
		{"scp file user@host:/path", true},
		{"nc host 1234", true},
		{"ls -la", false},
		{"cat file.txt", false},
		{"go test ./...", false},
		{"git status", false},
	}

	for _, tt := range tests {
		got := IsEgressCommand(tt.cmd)
		if got != tt.expected {
			t.Errorf("IsEgressCommand(%q) = %v, want %v", tt.cmd, got, tt.expected)
		}
	}
}

func TestRedactMessage(t *testing.T) {
	sr := &SessionRedactor{
		redactor: redact.NewRedactor(redact.RedactorConfig{Enabled: true}, "", nil),
		nonce:    "a3f9c2",
	}
	sr.redactor.SetRegistry(redact.NewRegistry("a3f9c2"))

	// User message should be redacted
	msg := Message{Role: "user", Content: "my password is AKIAIOSFODNN7EXAMPLE"}
	redacted, err := RedactMessage(msg, sr)
	if err != nil {
		t.Fatalf("RedactMessage error: %v", err)
	}

	if redacted.Content == msg.Content {
		t.Error("User message should have been redacted")
	}

	// Disabled redactor should not modify
	sr2 := &SessionRedactor{
		redactor: redact.NewRedactor(redact.RedactorConfig{Enabled: false}, "", nil),
		nonce:    "a3f9c2",
	}

	redacted2, err := RedactMessage(msg, sr2)
	if err != nil {
		t.Fatalf("RedactMessage error: %v", err)
	}

	if redacted2.Content != msg.Content {
		t.Error("Disabled redactor should not modify message")
	}

	// Nil redactor should not modify
	redacted3, err := RedactMessage(msg, nil)
	if err != nil {
		t.Fatalf("RedactMessage error: %v", err)
	}

	if redacted3.Content != msg.Content {
		t.Error("Nil redactor should not modify message")
	}
}
