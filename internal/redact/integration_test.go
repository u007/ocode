package redact

import (
	"testing"
)

func TestEndToEndRedactionFlow(t *testing.T) {
	// 1. Create registry with nonce
	nonce := NewNonce()
	if len(nonce) != 6 {
		t.Fatalf("Expected 6-char nonce, got %d chars", len(nonce))
	}
	reg := NewRegistry(nonce)

	// 2. Simulate detecting secrets in user input
	text := "My AWS key is AKIAIOSFODNN7EXAMPLE and my GitHub token is ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdef1234"
	spans := Detect(text, nil, DetectOpts{FileContent: false})
	if len(spans) < 2 {
		t.Fatalf("Expected at least 2 spans, got %d", len(spans))
	}

	// 3. Register secrets
	for _, span := range spans {
		value := text[span.Start:span.End]
		idx := reg.GetOrAssign(value, span.Kind, "test")
		if idx <= 0 {
			t.Errorf("Expected positive index for %s", value)
		}
	}

	// 4. Substitute secrets with tokens
	redacted := reg.Substitute(text)
	if redacted == text {
		t.Error("Expected text to be modified after substitution")
	}

	// 5. Verify tokens are in the redacted text
	if !TokenPattern.MatchString(redacted) {
		t.Error("Expected OCSEC tokens in redacted text")
	}

	// 6. Resolve tokens back to original
	resolved := reg.Resolve(redacted)
	if resolved != text {
		t.Errorf("Resolve mismatch:\n  original: %q\n  resolved: %q", text, resolved)
	}

	// 7. Verify all entries are registered
	entries := reg.All()
	if len(entries) < 2 {
		t.Errorf("Expected at least 2 entries, got %d", len(entries))
	}
}

func TestEndToEndNetHookFlow(t *testing.T) {
	// 1. Create NetHook
	nonce := NewNonce()
	reg := NewRegistry(nonce)
	nh := NetHookEnabled(reg)

	// 2. Simulate system prompt with secrets
	systemPrompt := "You are a helpful assistant. AWS credentials: AKIAIOSFODNN7EXAMPLE"
	redactedPrompt := nh.ScanText(systemPrompt)

	if redactedPrompt == systemPrompt {
		t.Error("Expected system prompt to be redacted")
	}

	// 3. Verify secret was registered
	entries := reg.All()
	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}

	if entries[0].Kind != "aws_key" {
		t.Errorf("Expected aws_key kind, got %s", entries[0].Kind)
	}

	// 4. Simulate tool args with token
	toolArgs := "curl -H \"Authorization: [[OCSEC:" + nonce + ":1]]\" https://api.example.com"
	resolved := reg.Resolve(toolArgs)

	if resolved == toolArgs {
		t.Error("Expected tool args to be resolved")
	}

	if !contains(resolved, "AKIAIOSFODNN7EXAMPLE") {
		t.Error("Expected resolved args to contain original secret")
	}
}

func TestEndToEndVaultFlow(t *testing.T) {
	// 1. Create registry
	nonce := NewNonce()
	reg := NewRegistry(nonce)

	// 2. Register some secrets
	reg.GetOrAssign("AKIAIOSFODNN7EXAMPLE", "aws_key", "test")
	reg.GetOrAssign("ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdef1234", "github_token", "test")

	// 3. Save vault
	sessionID := "test-session-123"
	vaultPath := VaultPath("/tmp", "test-project", sessionID)
	if err := SaveVault(vaultPath, reg); err != nil {
		t.Fatalf("Failed to save vault: %v", err)
	}

	// 4. Load vault
	loadedReg, err := LoadVault(vaultPath)
	if err != nil {
		t.Fatalf("Failed to load vault: %v", err)
	}

	// 5. Verify loaded registry matches original
	if loadedReg.Nonce() != reg.Nonce() {
		t.Errorf("Nonce mismatch: got %s, want %s", loadedReg.Nonce(), reg.Nonce())
	}

	// 6. Verify entries
	loadedEntries := loadedReg.All()
	originalEntries := reg.All()
	if len(loadedEntries) != len(originalEntries) {
		t.Errorf("Entry count mismatch: got %d, want %d", len(loadedEntries), len(originalEntries))
	}

	// 7. Clean up
	if err := DeleteVault(vaultPath); err != nil {
		t.Errorf("Failed to delete vault: %v", err)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
