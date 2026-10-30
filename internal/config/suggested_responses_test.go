package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSuggestedResponsesEnabledDefaultsOn pins the default-ON contract: a
// config that never mentions the setting still has it enabled.
//
// The default matters because the feature is a presentation affordance the
// user asked for; it should work out of the box, not need a first-run toggle.
func TestSuggestedResponsesEnabledDefaultsOn(t *testing.T) {
	cfg := defaultOcodeConfig()
	if !cfg.SuggestedResponsesEnabled {
		t.Fatal("defaultOcodeConfig().SuggestedResponsesEnabled = false, want true")
	}
}

// TestSuggestedResponsesEnabledRoundTripsThroughDisk pins that the setting
// survives a save/load cycle under its own JSON key.
//
// Uses the package's setHomeTree helper (never a bare HOME override): ocode's
// path resolution is platform-split, and a bare HOME leaves the XDG variables
// pointing at the package-wide temp dir on Linux — which is the path CI runs.
func TestSuggestedResponsesEnabledRoundTripsThroughDisk(t *testing.T) {
	tmp := t.TempDir()
	setHomeTree(t, tmp)
	if err := os.MkdirAll(filepath.Join(tmp, ".config", "opencode"), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}

	if err := SaveOcodeFeatures(true, false, false, true); err != nil {
		t.Fatalf("SaveOcodeFeatures failed: %v", err)
	}
	var cfg Config
	if err := LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("LoadOcodeConfig failed: %v", err)
	}
	if cfg.Ocode.SuggestedResponsesEnabled {
		t.Fatal("suggested responses should persist as disabled after saving false")
	}

	if err := SaveOcodeFeatures(true, false, true, true); err != nil {
		t.Fatalf("SaveOcodeFeatures failed: %v", err)
	}
	var cfg2 Config
	if err := LoadOcodeConfig(&cfg2); err != nil {
		t.Fatalf("LoadOcodeConfig failed: %v", err)
	}
	if !cfg2.Ocode.SuggestedResponsesEnabled {
		t.Fatal("suggested responses should persist as enabled after saving true")
	}
}

// TestSuggestedResponsesEnabledWrittenToConfigFile pins that the value lands
// in ocodeconfig.json under the documented key, so a user can hand-edit it.
func TestSuggestedResponsesEnabledWrittenToConfigFile(t *testing.T) {
	tmp := t.TempDir()
	setHomeTree(t, tmp)
	if err := os.MkdirAll(filepath.Join(tmp, ".config", "opencode"), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}

	if err := SaveOcodeFeatures(true, false, false, true); err != nil {
		t.Fatalf("SaveOcodeFeatures failed: %v", err)
	}
	path, err := getGlobalOcodeConfigPath()
	if err != nil {
		t.Fatalf("getGlobalOcodeConfigPath: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(raw), "suggested_responses_enabled") {
		t.Fatalf("config file has no suggested_responses_enabled key:\n%s", raw)
	}
}
