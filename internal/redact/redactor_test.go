package redact

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedactorDisabled(t *testing.T) {
	r := NewRedactor(RedactorConfig{Enabled: false}, "", nil)
	if r.Enabled() {
		t.Error("Redactor should be disabled")
	}

	text := "my password is AKIAIOSFODNN7EXAMPLE"
	masked, err := r.RedactChat(text)
	if err != nil {
		t.Fatalf("RedactChat error: %v", err)
	}
	if masked != text {
		t.Error("Disabled redactor should not modify text")
	}
}

func TestRedactorChatMode(t *testing.T) {
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "test.vault.json")
	reg := NewRegistry("a3f9c2")

	r := NewRedactor(RedactorConfig{Enabled: true}, vaultPath, nil)
	r.SetRegistry(reg)

	text := "my password is AKIAIOSFODNN7EXAMPLE"
	masked, err := r.RedactChat(text)
	if err != nil {
		t.Fatalf("RedactChat error: %v", err)
	}

	if masked == text {
		t.Error("RedactChat should have modified text")
	}

	// Check vault was persisted
	if _, err := LoadVault(vaultPath); err != nil {
		t.Errorf("Vault should have been persisted: %v", err)
	}

	// Resolve back
	resolved := r.Render(masked)
	if resolved != text {
		t.Errorf("Render mismatch:\n  original: %q\n  resolved: %q", text, resolved)
	}
}

func TestRedactorFileMode(t *testing.T) {
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "test.vault.json")
	reg := NewRegistry("a3f9c2")

	r := NewRedactor(RedactorConfig{Enabled: true}, vaultPath, nil)
	r.SetRegistry(reg)

	// File mode: should detect known formats but not keyword entropy
	text := "file content with AKIAIOSFODNN7EXAMPLE"
	masked, err := r.RedactFile(text)
	if err != nil {
		t.Fatalf("RedactFile error: %v", err)
	}

	if masked == text {
		t.Error("RedactFile should have modified text")
	}

	// Vault should be persisted
	if _, err := LoadVault(vaultPath); err != nil {
		t.Errorf("Vault should have been persisted: %v", err)
	}
}

func TestRedactorRenderDisabled(t *testing.T) {
	r := NewRedactor(RedactorConfig{Enabled: false}, "", nil)

	text := "[[OCSEC:a3f9c2:1]]"
	resolved := r.Render(text)
	if resolved != text {
		t.Error("Render on disabled redactor should return text unchanged")
	}
}

func TestRedactorInit(t *testing.T) {
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "test.vault.json")

	r := NewRedactor(RedactorConfig{Enabled: true}, vaultPath, nil)
	if err := r.Init(); err != nil {
		t.Fatalf("Init error: %v", err)
	}

	if r.Registry() == nil {
		t.Fatal("Registry should be initialized")
	}

	// Vault should exist
	if _, err := LoadVault(vaultPath); err != nil {
		t.Errorf("Vault should exist after Init: %v", err)
	}
}

func TestRedactorSameSecretReuse(t *testing.T) {
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "test.vault.json")
	reg := NewRegistry("a3f9c2")

	r := NewRedactor(RedactorConfig{Enabled: true}, vaultPath, nil)
	r.SetRegistry(reg)

	// Same secret appears multiple times
	text := "first AKIAIOSFODNN7EXAMPLE and second AKIAIOSFODNN7EXAMPLE"
	masked, err := r.RedactChat(text)
	if err != nil {
		t.Fatalf("RedactChat error: %v", err)
	}

	// Both occurrences should be replaced with same token
	tokens := TokenPattern.FindAllString(masked, -1)
	if len(tokens) != 2 {
		t.Fatalf("Expected 2 tokens, got %d: %v", len(tokens), tokens)
	}
	if tokens[0] != tokens[1] {
		t.Errorf("Same secret should use same token: %q != %q", tokens[0], tokens[1])
	}
}

func TestMaskedPreview(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"short", "***"},           // Short string: just asterisks
		{"12345678", "123***78"},   // Exactly 8 chars: first 3 + *** + last 2
		{"abcdefghij", "abc***ij"}, // 10 chars: first 3 + *** + last 2
		{"a", "***"},               // Single char: just asterisks
		{"ab", "***"},              // Two chars: just asterisks
		{"1234567", "***"},         // 7 chars: just asterisks
	}

	for _, tt := range tests {
		got := MaskedPreview(tt.input)
		if got != tt.expected {
			t.Errorf("MaskedPreview(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestRedactorCustomWords(t *testing.T) {
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "test.vault.json")
	reg := NewRegistry("a3f9c2")

	r := NewRedactor(RedactorConfig{Enabled: true, CustomWords: []string{"my-secret"}}, vaultPath, nil)
	r.SetRegistry(reg)

	text := "the secret is my-secret and it is important"
	masked, err := r.RedactChat(text)
	if err != nil {
		t.Fatalf("RedactChat error: %v", err)
	}

	// Custom word should be masked
	resolved := r.Render(masked)
	if resolved != text {
		t.Errorf("Custom word should be resolved:\n  original: %q\n  resolved: %q", text, resolved)
	}
}

// TestRedactorChatMode_Tier2 verifies that RedactChat correctly invokes
// the tier-2 scanner on the tier-1 masked text and registers new findings.
func TestRedactorChatMode_Tier2(t *testing.T) {
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "test.vault.json")
	reg := NewRegistry("a3f9c2")

	// Novel secret that tier-1 regex won't catch (no known prefix, no keyword)
	novelSecret := "xC7kQ9mBw2pL"
	text := "my api key is " + novelSecret

	// Mock scanner that finds the novel secret at its position in the
	// tier-1 masked text (since tier-1 didn't catch it, it's still plaintext).
	scanner := &mockScanner{
		spans: []Span{{Start: 14, End: 26, Kind: "custom"}},
	}

	r := NewRedactor(RedactorConfig{Enabled: true}, vaultPath, scanner)
	r.SetRegistry(reg)

	masked, err := r.RedactChat(text)
	if err != nil {
		t.Fatalf("RedactChat error: %v", err)
	}

	// The novel secret must be masked
	if strings.Contains(masked, novelSecret) {
		t.Errorf("novel secret not masked in output: %q", masked)
	}

	// The masked output should contain an OCSEC token
	if !TokenPattern.MatchString(masked) {
		t.Errorf("expected OCSEC token in output, got: %q", masked)
	}

	// Resolve back — should match original
	resolved := r.Render(masked)
	if resolved != text {
		t.Errorf("Render mismatch:\n  original: %q\n  resolved: %q", text, resolved)
	}

	// Vault should be persisted
	if _, err := LoadVault(vaultPath); err != nil {
		t.Errorf("Vault should have been persisted: %v", err)
	}
}

// TestRedactorChatMode_Tier2_ScannerError verifies that RedactChat returns
// ErrScannerUnavailable when the tier-2 scanner fails, but still returns
// the tier-1 masked text.
func TestRedactorChatMode_Tier2_ScannerError(t *testing.T) {
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "test.vault.json")
	reg := NewRegistry("a3f9c2")

	scanner := &mockScanner{
		err: fmt.Errorf("connection refused"),
	}

	r := NewRedactor(RedactorConfig{Enabled: true}, vaultPath, scanner)
	r.SetRegistry(reg)

	text := "my password is AKIAIOSFODNN7EXAMPLE"
	masked, err := r.RedactChat(text)

	// Should return ErrScannerUnavailable
	if !IsScannerUnavailable(err) {
		t.Errorf("expected ErrScannerUnavailable, got: %v", err)
	}

	// Should still return tier-1 masked text
	if masked == text {
		t.Error("RedactChat should have modified text (tier-1)")
	}

	// Vault should still be persisted
	if _, err := LoadVault(vaultPath); err != nil {
		t.Errorf("Vault should have been persisted: %v", err)
	}
}

func TestScannerUnavailableError(t *testing.T) {
	err := ErrScannerUnavailable
	if !IsScannerUnavailable(err) {
		t.Error("Should detect ScannerError")
	}

	nonScannerErr := &ScannerError{Err: nil}
	if !IsScannerUnavailable(nonScannerErr) {
		t.Error("Should detect pointer ScannerError")
	}
}
