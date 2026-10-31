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

	if _, err := SaveOcodeFeatures(OcodeFeaturesUpdate{
		MemoryEnabled: boolp(true), DocPromptEnabled: boolp(false),
		SuggestedResponsesEnabled: boolp(true), CommunicationStyleEnabled: boolp(false),
	}); err != nil {
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

	if _, err := SaveOcodeFeatures(OcodeFeaturesUpdate{
		MemoryEnabled: boolp(true), DocPromptEnabled: boolp(false),
		SuggestedResponsesEnabled: boolp(true), CommunicationStyleEnabled: boolp(true),
	}); err != nil {
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
	if _, err := SaveOcodeFeatures(OcodeFeaturesUpdate{
		MemoryEnabled: boolp(true), DocPromptEnabled: boolp(false),
		SuggestedResponsesEnabled: boolp(true), CommunicationStyleEnabled: boolp(true),
	}); err != nil {
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

// boolp returns a pointer to a copy of v, for the optional fields of
// OcodeFeaturesUpdate.
func boolp(v bool) *bool { return &v }

// TestSaveOcodeFeaturesNilFieldKeepsStoredValue pins the partial-update
// contract: a nil CommunicationStyleEnabled keeps the value already on disk
// (here false) while the other toggles are still written, and the returned
// merged values mirror what was persisted.
func TestSaveOcodeFeaturesNilFieldKeepsStoredValue(t *testing.T) {
	tmp := t.TempDir()
	setHomeTree(t, tmp)
	if err := os.MkdirAll(filepath.Join(tmp, ".config", "opencode"), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}

	if _, err := SaveOcodeFeatures(OcodeFeaturesUpdate{
		MemoryEnabled: boolp(true), DocPromptEnabled: boolp(false),
		SuggestedResponsesEnabled: boolp(true), CommunicationStyleEnabled: boolp(false),
	}); err != nil {
		t.Fatalf("SaveOcodeFeatures (seed) failed: %v", err)
	}

	got, err := SaveOcodeFeatures(OcodeFeaturesUpdate{
		MemoryEnabled: boolp(true), DocPromptEnabled: boolp(true),
		SuggestedResponsesEnabled: boolp(false),
		// CommunicationStyleEnabled omitted: must keep the stored false.
	})
	if err != nil {
		t.Fatalf("SaveOcodeFeatures (partial) failed: %v", err)
	}
	want := OcodeFeatures{Memory: true, DocPrompt: true, SuggestedResponses: false, CommunicationStyle: false}
	if got != want {
		t.Fatalf("returned features = %+v, want %+v", got, want)
	}

	var cfg Config
	if err := LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("LoadOcodeConfig failed: %v", err)
	}
	if cfg.Ocode.CommunicationStyleEnabled {
		t.Fatal("omitted communication_style_enabled was reset; a nil field must keep the stored false")
	}
	if !cfg.Ocode.DocPromptEnabled || cfg.Ocode.SuggestedResponsesEnabled {
		t.Fatalf("explicitly set fields not persisted: doc=%v suggested=%v",
			cfg.Ocode.DocPromptEnabled, cfg.Ocode.SuggestedResponsesEnabled)
	}
}
