package redact

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestVaultPath(t *testing.T) {
	// VaultPath(base, slug, sessionID) = <base>/project/<slug>/secrets/<ses_id>.vault.json
	expected := filepath.Join("base", "project", "myslug", "secrets", "ses_2026-01-01.vault.json")
	got := VaultPath("base", "myslug", "ses_2026-01-01")
	if got != expected {
		t.Errorf("VaultPath = %q, want %q", got, expected)
	}
}

func TestDefaultVaultBase(t *testing.T) {
	base, err := DefaultVaultBase()
	if err != nil {
		t.Fatalf("DefaultVaultBase() error: %v", err)
	}

	switch runtime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		expected := filepath.Join(home, ".local", "share", "opencode")
		if base != expected {
			t.Errorf("DefaultVaultBase() = %q, want %q", base, expected)
		}
	case "windows":
		// Just check it doesn't error and returns something reasonable
		if base == "" {
			t.Error("DefaultVaultBase() returned empty on Windows")
		}
	}
}

func TestSaveLoadVault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.vault.json")

	reg := NewRegistry("a3f9c2")
	reg.GetOrAssign("hunter2", "password", "test")
	reg.GetOrAssign("sk-abc123xyz", "api_key", "test")

	// Save
	if err := SaveVault(path, reg); err != nil {
		t.Fatalf("SaveVault() error: %v", err)
	}

	// Load
	loaded, err := LoadVault(path)
	if err != nil {
		t.Fatalf("LoadVault() error: %v", err)
	}

	if loaded.Nonce() != "a3f9c2" {
		t.Errorf("Loaded nonce = %q, want %q", loaded.Nonce(), "a3f9c2")
	}

	// Check entries
	entry, ok := loaded.Lookup(1)
	if !ok {
		t.Fatal("Lookup(1) not found in loaded vault")
	}
	if entry.Value != "hunter2" {
		t.Errorf("entry.Value = %q, want %q", entry.Value, "hunter2")
	}
	if entry.Kind != "password" {
		t.Errorf("entry.Kind = %q, want %q", entry.Kind, "password")
	}

	// Substitute/Resolve round trip
	text := "my password is hunter2"
	substituted := loaded.Substitute(text)
	resolved := loaded.Resolve(substituted)
	if resolved != text {
		t.Errorf("Resolve mismatch after load:\n  original:  %q\n  resolved:  %q", text, resolved)
	}
}

func TestVaultAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.vault.json")

	reg := NewRegistry("a3f9c2")
	reg.GetOrAssign("secret1", "test", "test")

	// Save twice - second should overwrite cleanly
	if err := SaveVault(path, reg); err != nil {
		t.Fatalf("First SaveVault() error: %v", err)
	}

	// Add more entries
	reg.GetOrAssign("secret2", "test", "test")
	if err := SaveVault(path, reg); err != nil {
		t.Fatalf("Second SaveVault() error: %v", err)
	}

	// Load and verify
	loaded, err := LoadVault(path)
	if err != nil {
		t.Fatalf("LoadVault() error: %v", err)
	}

	entries := loaded.All()
	if len(entries) != 2 {
		t.Errorf("Expected 2 entries after second save, got %d", len(entries))
	}

	// No temp file should exist
	tmpPath := path + ".tmp"
	if _, err := os.Stat(tmpPath); err == nil {
		t.Error("Temp file still exists after SaveVault")
	}
}

func TestDeleteVault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.vault.json")

	reg := NewRegistry("a3f9c2")
	if err := SaveVault(path, reg); err != nil {
		t.Fatalf("SaveVault() error: %v", err)
	}

	// Delete
	if err := DeleteVault(path); err != nil {
		t.Fatalf("DeleteVault() error: %v", err)
	}

	// Verify gone
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("Vault file should not exist after delete")
	}

	// Delete non-existent should not error
	if err := DeleteVault(path); err != nil {
		t.Errorf("DeleteVault on non-existent should not error: %v", err)
	}
}

func TestVaultDirPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "test.vault.json")

	reg := NewRegistry("a3f9c2")
	if err := SaveVault(path, reg); err != nil {
		t.Fatalf("SaveVault() error: %v", err)
	}

	// Check dir permissions
	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("Stat dir error: %v", err)
	}
	if info.Mode().Perm() != 0700 {
		t.Errorf("Dir permissions = %o, want 0700", info.Mode().Perm())
	}

	// Check file permissions
	info, err = os.Stat(path)
	if err != nil {
		t.Fatalf("Stat file error: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("File permissions = %o, want 0600", info.Mode().Perm())
	}
}
