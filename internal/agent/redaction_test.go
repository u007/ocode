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
