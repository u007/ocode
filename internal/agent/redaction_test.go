package agent

import (
	"testing"

	"github.com/u007/ocode/internal/redact"
)

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

func TestSessionRedactorResolveToolArgs(t *testing.T) {
	// Create a redactor with some secrets
	nonce := "a3f9c2"
	reg := redact.NewRegistry(nonce)
	reg.GetOrAssign("hunter2", "password", "test")

	redactor := redact.NewRedactor(redact.RedactorConfig{Enabled: true}, "", nil)
	redactor.SetRegistry(reg)

	sr := &SessionRedactor{
		redactor: redactor,
		nonce:    nonce,
	}

	// Test with token in args
	args := `{"command": "echo [[OCSEC:a3f9c2:1]]"}`
	resolved, refs := sr.ResolveToolArgs(args)

	if len(refs) != 1 {
		t.Fatalf("Expected 1 ref, got %d", len(refs))
	}

	if refs[0].Index != 1 {
		t.Errorf("Expected ref index 1, got %d", refs[0].Index)
	}

	if refs[0].Kind != "password" {
		t.Errorf("Expected ref kind 'password', got %q", refs[0].Kind)
	}

	if resolved != `{"command": "echo hunter2"}` {
		t.Errorf("Expected resolved args, got %q", resolved)
	}
}

func TestSessionRedactorResolveToolArgsForeignNonce(t *testing.T) {
	nonce := "a3f9c2"
	reg := redact.NewRegistry(nonce)

	redactor := redact.NewRedactor(redact.RedactorConfig{Enabled: true}, "", nil)
	redactor.SetRegistry(reg)

	sr := &SessionRedactor{
		redactor: redactor,
		nonce:    nonce,
	}

	// Foreign nonce token should not be resolved
	args := `{"command": "echo [[OCSEC:deadbe:1]]"}`
	resolved, refs := sr.ResolveToolArgs(args)

	if len(refs) != 0 {
		t.Errorf("Expected 0 refs for foreign nonce, got %d", len(refs))
	}

	if resolved != args {
		t.Errorf("Foreign nonce should not be resolved: %q", resolved)
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
