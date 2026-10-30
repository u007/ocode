package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCommunicationStyleEnabledDefaultsOn pins the default-ON contract: a config
// that never mentions the setting still has the communication standard enabled.
func TestCommunicationStyleEnabledDefaultsOn(t *testing.T) {
	if !defaultOcodeConfig().CommunicationStyleEnabled {
		t.Fatal("defaultOcodeConfig().CommunicationStyleEnabled = false, want true")
	}
}

// TestCommunicationStyleEnabledRoundTripsThroughDisk pins that the setting
// survives a save/load cycle under its own JSON key, in both directions.
func TestCommunicationStyleEnabledRoundTripsThroughDisk(t *testing.T) {
	tmp := t.TempDir()
	setHomeTree(t, tmp)
	if err := os.MkdirAll(filepath.Join(tmp, ".config", "opencode"), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}

	if err := SaveOcodeFeatures(true, false, true, false); err != nil {
		t.Fatalf("SaveOcodeFeatures failed: %v", err)
	}
	var cfg Config
	if err := LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("LoadOcodeConfig failed: %v", err)
	}
	if cfg.Ocode.CommunicationStyleEnabled {
		t.Fatal("communication style should persist as disabled after saving false")
	}
	path, err := getGlobalOcodeConfigPath()
	if err != nil {
		t.Fatalf("getGlobalOcodeConfigPath: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(raw), "communication_style_enabled") {
		t.Fatalf("config file has no communication_style_enabled key:\n%s", raw)
	}

	if err := SaveOcodeFeatures(true, false, true, true); err != nil {
		t.Fatalf("SaveOcodeFeatures failed: %v", err)
	}
	var cfg2 Config
	if err := LoadOcodeConfig(&cfg2); err != nil {
		t.Fatalf("LoadOcodeConfig failed: %v", err)
	}
	if !cfg2.Ocode.CommunicationStyleEnabled {
		t.Fatal("communication style should persist as enabled after saving true")
	}
}

// TestCommunicationStyleEnabledStaysOnForConfigWithoutTheKey pins the upgrade
// path: an existing ocodeconfig.json written before this setting existed must
// load with it ENABLED, not with the zero value false.
func TestCommunicationStyleEnabledStaysOnForConfigWithoutTheKey(t *testing.T) {
	tmp := t.TempDir()
	setHomeTree(t, tmp)
	dir := filepath.Join(tmp, ".config", "opencode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	// Seed a config that carries the sibling toggles but not the new key.
	if err := SaveOcodeFeatures(true, false, true, true); err != nil {
		t.Fatalf("SaveOcodeFeatures failed: %v", err)
	}
	path, err := getGlobalOcodeConfigPath()
	if err != nil {
		t.Fatalf("getGlobalOcodeConfigPath: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"memory_enabled":true,"suggested_responses_enabled":true}`), 0o644); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}
	var cfg Config
	if err := LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("LoadOcodeConfig failed: %v", err)
	}
	if !cfg.Ocode.CommunicationStyleEnabled {
		t.Fatal("a legacy config without communication_style_enabled must load with it ENABLED")
	}
}
