package tui

import (
	"fmt"
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/redact"
)

// mockScanner is a test double for the redact.Scanner interface.
type mockScanner struct {
	spans []redact.Span
	err   error
}

func (m *mockScanner) Scan(maskedText string) ([]redact.Span, error) {
	return m.spans, m.err
}

func TestBuildLLMScanner_EmptyBaseURL(t *testing.T) {
	if got := buildLLMScanner("", "model"); got != nil {
		t.Errorf("expected nil for empty baseURL, got %v", got)
	}
}

func TestBuildLLMScanner_EmptyModel(t *testing.T) {
	if got := buildLLMScanner("http://localhost:11434", ""); got != nil {
		t.Errorf("expected nil for empty model, got %v", got)
	}
}

func TestBuildLLMScanner_NonLocalURL(t *testing.T) {
	if got := buildLLMScanner("https://api.openai.com", "gpt-4"); got != nil {
		t.Errorf("expected nil for non-local URL, got %v", got)
	}
}

func TestBuildLLMScanner_ValidLocalURL(t *testing.T) {
	got := buildLLMScanner("http://localhost:11434", "llama3")
	if got == nil {
		t.Fatal("expected non-nil scanner for valid local URL")
	}
	if got.BaseURL != "http://localhost:11434" {
		t.Errorf("BaseURL = %q, want %q", got.BaseURL, "http://localhost:11434")
	}
	if got.Model != "llama3" {
		t.Errorf("Model = %q, want %q", got.Model, "llama3")
	}
}

func TestBuildLLMScanner_LoopbackIP(t *testing.T) {
	got := buildLLMScanner("http://127.0.0.1:8080", "model")
	if got == nil {
		t.Fatal("expected non-nil scanner for 127.0.0.1")
	}
}

func TestApplyTier2Scan_NilScanner(t *testing.T) {
	reg := redact.NewRegistry(redact.NewNonce())
	msgs := []agent.Message{
		{Role: "user", Content: "my key is AKIAIOSFODNN7EXAMPLE"},
	}
	// Should not panic.
	applyTier2Scan(msgs, nil, reg)
}

func TestApplyTier2Scan_EmptyMessages(t *testing.T) {
	reg := redact.NewRegistry(redact.NewNonce())
	scanner := &mockScanner{}
	msgs := []agent.Message{}
	applyTier2Scan(msgs, scanner, reg)
	// No crash, no scan calls.
}

func TestApplyTier2Scan_NoUserMessages(t *testing.T) {
	reg := redact.NewRegistry(redact.NewNonce())
	scanner := &mockScanner{spans: []redact.Span{{Start: 0, End: 5, Kind: "test"}}}
	msgs := []agent.Message{
		{Role: "assistant", Content: "hello"},
		{Role: "system", Content: "you are helpful"},
	}
	applyTier2Scan(msgs, scanner, reg)
	// No user message found, so no scan should occur.
}

func TestApplyTier2Scan_WhitespaceOnlyUserMessage(t *testing.T) {
	reg := redact.NewRegistry(redact.NewNonce())
	scanner := &mockScanner{}
	msgs := []agent.Message{
		{Role: "user", Content: "   \t\n  "},
	}
	applyTier2Scan(msgs, scanner, reg)
	// Whitespace-only message is skipped.
}

func TestApplyTier2Scan_ScannerError(t *testing.T) {
	reg := redact.NewRegistry(redact.NewNonce())
	scanner := &mockScanner{err: fmt.Errorf("connection refused")}
	msgs := []agent.Message{
		{Role: "user", Content: "my key is AKIAIOSFODNN7EXAMPLE"},
	}
	// Should not panic; error is logged via DebugAppendf.
	applyTier2Scan(msgs, scanner, reg)
}

func TestApplyTier2Scan_ScannerReturnsSpans(t *testing.T) {
	reg := redact.NewRegistry(redact.NewNonce())
	// Simulate scanner finding a span that tier-1 wouldn't catch.
	input := "password is my-secret-value-123"
	// "my-secret-value-123" starts at index 12, length 19 → end 31.
	scanner := &mockScanner{
		spans: []redact.Span{{Start: 12, End: 31, Kind: "custom"}},
	}
	msgs := []agent.Message{
		{Role: "user", Content: input},
	}
	applyTier2Scan(msgs, scanner, reg)
	// The original message should now be substituted with a token.
	if msgs[0].Content == input {
		t.Error("expected message content to be modified with OCSEC token")
	}
}

func TestApplyTier2Scan_AlreadyTokenizedSpanSkipped(t *testing.T) {
	reg := redact.NewRegistry(redact.NewNonce())
	// Pre-register a value so it gets a token.
	preExisting := "AKIAIOSFODNN7EXAMPLE"
	reg.GetOrAssign(preExisting, "aws_key", "test")
	// After substitution, the text contains an OCSEC token.
	masked := reg.Substitute("key: " + preExisting)

	scanner := &mockScanner{
		// Scanner tries to report the token itself as a span — should be skipped.
		spans: []redact.Span{{Start: 0, End: len(masked), Kind: "token"}},
	}
	msgs := []agent.Message{
		{Role: "user", Content: masked},
	}
	applyTier2Scan(msgs, scanner, reg)
	// No panic, token is not double-registered.
}

func TestApplyTier2Scan_OnlyScansLastUserMessage(t *testing.T) {
	reg := redact.NewRegistry(redact.NewNonce())
	callCount := 0
	var scanner redact.Scanner = &countingScanner{
		inner: &mockScanner{spans: []redact.Span{}},
		count: &callCount,
	}

	msgs := []agent.Message{
		{Role: "user", Content: "first message with AKIAIOSFODNN7EXAMPLE"},
		{Role: "assistant", Content: "response"},
		{Role: "user", Content: "second message"},
	}
	applyTier2Scan(msgs, scanner, reg)
	// Should scan exactly once (the last user message).
	if callCount != 1 {
		t.Errorf("expected 1 scan call, got %d", callCount)
	}
}

// countingScanner wraps a scanner and counts Scan calls.
type countingScanner struct {
	inner redact.Scanner
	count *int
}

func (c *countingScanner) Scan(maskedText string) ([]redact.Span, error) {
	*c.count++
	return c.inner.Scan(maskedText)
}
