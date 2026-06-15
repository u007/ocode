package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRedactionConfigLoad(t *testing.T) {
	// Create a config file with security.redaction section
	configJSON := `{
  "compact": {
    "enabled": true
  },
		"security": {
		  "redaction": {
		    "enabled": true,
		    "model": "lmstudio/x",
		    "base_url": "http://localhost:11434",
		    "fail_mode": "block",
		    "allow_remote_tier2": true,
		    "custom_words": ["word1", "word2"]
		  }
		}
	}`

	dir := t.TempDir()
	configPath := filepath.Join(dir, "ocode.json")
	if err := os.WriteFile(configPath, []byte(configJSON), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// Load config
	cfg := &Config{}
	cfg.Ocode = defaultOcodeConfig()

	if err := loadOcodeConfigFile(configPath, &cfg.Ocode); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify redaction config
	if !cfg.Ocode.Security.Redaction.Enabled {
		t.Error("Expected redaction to be enabled")
	}
	if cfg.Ocode.Security.Redaction.Model != "lmstudio/x" {
		t.Errorf("Expected model 'lmstudio/x', got %q", cfg.Ocode.Security.Redaction.Model)
	}
	if cfg.Ocode.Security.Redaction.BaseURL != "http://localhost:11434" {
		t.Errorf("Expected base_url 'http://localhost:11434', got %q", cfg.Ocode.Security.Redaction.BaseURL)
	}
	if cfg.Ocode.Security.Redaction.FailMode != "block" {
		t.Errorf("Expected fail_mode 'block', got %q", cfg.Ocode.Security.Redaction.FailMode)
	}
	if !cfg.Ocode.Security.Redaction.AllowRemoteTier2 {
		t.Error("Expected allow_remote_tier2 to load as true")
	}
	if len(cfg.Ocode.Security.Redaction.CustomWords) != 2 {
		t.Errorf("Expected 2 custom words, got %d", len(cfg.Ocode.Security.Redaction.CustomWords))
	}
}

func TestRedactionConfigDefaults(t *testing.T) {
	// Load config without security section
	configJSON := `{
  "compact": {
    "enabled": true
  }
}`

	dir := t.TempDir()
	configPath := filepath.Join(dir, "ocode.json")
	if err := os.WriteFile(configPath, []byte(configJSON), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	cfg := &Config{}
	cfg.Ocode = defaultOcodeConfig()

	if err := loadOcodeConfigFile(configPath, &cfg.Ocode); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify defaults
	if cfg.Ocode.Security.Redaction.Enabled {
		t.Error("Expected redaction to be disabled by default")
	}
	if cfg.Ocode.Security.Redaction.FailMode != "block" {
		t.Errorf("Expected fail_mode 'block', got %q", cfg.Ocode.Security.Redaction.FailMode)
	}
}

func TestRedactionConfigPreservesOtherKeys(t *testing.T) {
	// Create config with multiple sections
	configJSON := `{
  "compact": {
    "enabled": true
  },
  "permissions": {
    "mode": "normal"
  },
  "tui": {
    "theme": "dark"
  }
}`

	dir := t.TempDir()
	configPath := filepath.Join(dir, "ocode.json")
	if err := os.WriteFile(configPath, []byte(configJSON), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	cfg := &Config{}
	cfg.Ocode = defaultOcodeConfig()

	if err := loadOcodeConfigFile(configPath, &cfg.Ocode); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify other sections are preserved
	if cfg.Ocode.Permissions.Mode != "normal" {
		t.Errorf("Expected permissions.mode 'normal', got %q", cfg.Ocode.Permissions.Mode)
	}
	if cfg.Ocode.TUI.Theme != "dark" {
		t.Errorf("Expected tui.theme 'dark', got %q", cfg.Ocode.TUI.Theme)
	}
}

func TestRedactionConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "ocode.json")

	// Create initial config
	initialJSON := `{
  "compact": {
    "enabled": true
  },
  "permissions": {
    "mode": "normal"
  }
}`
	if err := os.WriteFile(configPath, []byte(initialJSON), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// Load config
	cfg := &Config{}
	cfg.Ocode = defaultOcodeConfig()

	if err := loadOcodeConfigFile(configPath, &cfg.Ocode); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Modify redaction config
	cfg.Ocode.Security.Redaction.Enabled = true
	cfg.Ocode.Security.Redaction.Model = "lmstudio/x"
	cfg.Ocode.Security.Redaction.Mode = "full"
	cfg.Ocode.Security.Redaction.AllowRemoteTier2 = true

	// Save config
	if err := writeOcodeConfigFile(configPath, &cfg.Ocode); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	// Load again and verify
	cfg2 := &Config{}
	cfg2.Ocode = defaultOcodeConfig()

	if err := loadOcodeConfigFile(configPath, &cfg2.Ocode); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if !cfg2.Ocode.Security.Redaction.Enabled {
		t.Error("Expected redaction to be enabled after round-trip")
	}
	if cfg2.Ocode.Security.Redaction.Model != "lmstudio/x" {
		t.Errorf("Expected model 'lmstudio/x', got %q", cfg2.Ocode.Security.Redaction.Model)
	}
	if cfg2.Ocode.Security.Redaction.Mode != "full" {
		t.Errorf("Expected mode 'full' after round-trip, got %q", cfg2.Ocode.Security.Redaction.Mode)
	}
	if !cfg2.Ocode.Security.Redaction.AllowRemoteTier2 {
		t.Error("Expected allow_remote_tier2 to round-trip as true")
	}

	// Verify other sections are preserved
	if cfg2.Ocode.Permissions.Mode != "normal" {
		t.Errorf("Expected permissions.mode 'normal', got %q", cfg2.Ocode.Permissions.Mode)
	}
}
