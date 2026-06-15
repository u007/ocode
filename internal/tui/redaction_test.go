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
	if got := buildLLMScanner("", "model", false); got != nil {
		t.Errorf("expected nil for empty baseURL, got %v", got)
	}
}

func TestBuildLLMScanner_EmptyModel(t *testing.T) {
	if got := buildLLMScanner("http://localhost:11434", "", false); got != nil {
		t.Errorf("expected nil for empty model, got %v", got)
	}
}

func TestBuildLLMScanner_NonLocalURL(t *testing.T) {
	if got := buildLLMScanner("https://api.openai.com", "gpt-4", false); got != nil {
		t.Errorf("expected nil for non-local URL, got %v", got)
	}
}

func TestBuildLLMScanner_ValidLocalURL(t *testing.T) {
	got := buildLLMScanner("http://localhost:11434", "llama3", false)
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

func TestBuildLLMScanner_RemoteAllowed(t *testing.T) {
	got := buildLLMScanner("https://api.openai.com", "gpt-4", true)
	if got == nil {
		t.Fatal("expected non-nil scanner when remote endpoints are allowed")
	}
	if !got.AllowRemote {
		t.Fatal("expected AllowRemote to be preserved on scanner")
	}
}

func TestBuildLLMScanner_LoopbackIP(t *testing.T) {
	got := buildLLMScanner("http://127.0.0.1:8080", "model", false)
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
	applyTier2Scan(msgs, nil, reg, "block", "full")
}

func TestApplyTier2Scan_EmptyMessages(t *testing.T) {
	reg := redact.NewRegistry(redact.NewNonce())
	scanner := &mockScanner{}
	msgs := []agent.Message{}
	applyTier2Scan(msgs, scanner, reg, "block", "full")
	// No crash, no scan calls.
}

func TestApplyTier2Scan_NoUserMessages(t *testing.T) {
	reg := redact.NewRegistry(redact.NewNonce())
	scanner := &mockScanner{spans: []redact.Span{{Start: 0, End: 5, Kind: "test"}}}
	msgs := []agent.Message{
		{Role: "assistant", Content: "hello"},
		{Role: "system", Content: "you are helpful"},
	}
	applyTier2Scan(msgs, scanner, reg, "block", "full")
	// No user message found, so no scan should occur.
}

func TestApplyTier2Scan_WhitespaceOnlyUserMessage(t *testing.T) {
	reg := redact.NewRegistry(redact.NewNonce())
	scanner := &mockScanner{}
	msgs := []agent.Message{
		{Role: "user", Content: "   \t\n  "},
	}
	applyTier2Scan(msgs, scanner, reg, "block", "full")
	// Whitespace-only message is skipped.
}

func TestApplyTier2Scan_ScannerErrorWarnMode(t *testing.T) {
	reg := redact.NewRegistry(redact.NewNonce())
	scanner := &mockScanner{err: fmt.Errorf("connection refused")}
	msgs := []agent.Message{
		{Role: "user", Content: "my key is AKIAIOSFODNN7EXAMPLE"},
	}
	// "warn" mode: error is logged, call returns nil.
	if err := applyTier2Scan(msgs, scanner, reg, "warn", "full"); err != nil {
		t.Errorf("expected nil error in warn mode, got %v", err)
	}
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
	if err := applyTier2Scan(msgs, scanner, reg, "block", "full"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
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
	if err := applyTier2Scan(msgs, scanner, reg, "block", "full"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
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
	if err := applyTier2Scan(msgs, scanner, reg, "block", "full"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
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

func TestApplyTier2Scan_BlockModeReturnsError(t *testing.T) {
	reg := redact.NewRegistry(redact.NewNonce())
	scanner := &mockScanner{err: fmt.Errorf("scanner unavailable")}
	msgs := []agent.Message{
		{Role: "user", Content: "my api key is sk-test12345678901234567890"},
	}
	// "block" mode: error should be propagated.
	if err := applyTier2Scan(msgs, scanner, reg, "block", "full"); err == nil {
		t.Error("expected error in block mode, got nil")
	}
}

func TestApplyTier2Scan_LenientSkipsScannerOnCleanText(t *testing.T) {
	reg := redact.NewRegistry(redact.NewNonce())
	callCount := 0
	scanner := &countingScanner{
		inner: &mockScanner{spans: []redact.Span{}},
		count: &callCount,
	}
	msgs := []agent.Message{
		{Role: "user", Content: "hello, this is a normal message with no secrets"},
	}
	// With lenient mode and no sensitive keywords/patterns, scanner is not called.
	if err := applyTier2Scan(msgs, scanner, reg, "block", "lenient"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if callCount != 0 {
		t.Errorf("expected 0 scan calls (skipped), got %d", callCount)
	}
}

func TestApplyTier2Scan_FullStillScansCleanText(t *testing.T) {
	reg := redact.NewRegistry(redact.NewNonce())
	callCount := 0
	scanner := &countingScanner{
		inner: &mockScanner{spans: []redact.Span{}},
		count: &callCount,
	}
	msgs := []agent.Message{
		{Role: "user", Content: "this also has no secrets but mode is full"},
	}
	// With full mode, scanner runs even on clean text.
	if err := applyTier2Scan(msgs, scanner, reg, "block", "full"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 scan call, got %d", callCount)
	}
}

func TestApplyTier2Scan_LenientSkipsAlreadyMaskedMessage(t *testing.T) {
	reg := redact.NewRegistry(redact.NewNonce())
	callCount := 0
	scanner := &countingScanner{
		inner: &mockScanner{spans: []redact.Span{}},
		count: &callCount,
	}
	// Message with sensitive keyword but already masked with an OCSEC token.
	// The masked text won't trigger WarrantsLLMScan so scanner is skipped.
	msgs := []agent.Message{
		{Role: "user", Content: "my api key is sk-test12345678901234567890"},
	}
	if err := applyTier2Scan(msgs, scanner, reg, "block", "lenient"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if callCount != 0 {
		t.Errorf("expected 0 scan calls (already masked), got %d", callCount)
	}
}
