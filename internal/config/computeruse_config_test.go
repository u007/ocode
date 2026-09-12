package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestComputerUseConfig_DefaultDisabled(t *testing.T) {
	cfg := DefaultComputerUseConfig()
	if cfg.Enabled {
		t.Fatalf("DefaultComputerUseConfig().Enabled = %v, want false", cfg.Enabled)
	}
}

func TestComputerUseConfig_RoundTrip(t *testing.T) {
	chdirTempForConfigTest(t)

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// Seed the config with an unrelated enabled field so we can verify
	// it survives the targeted save.
	seed := defaultOcodeConfig()
	seed.Ocr.Enabled = true
	if err := SaveOcodeConfig(&seed); err != nil {
		t.Fatalf("SaveOcodeConfig failed: %v", err)
	}

	// Now save the computer use config via its targeted saver.
	if err := SaveComputerUseConfig(ComputerUseConfig{Enabled: true}); err != nil {
		t.Fatalf("SaveComputerUseConfig failed: %v", err)
	}

	// Verify on disk.
	configPath := filepath.Join(tmp, ".config", "opencode", "ocodeconfig.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config failed: %v", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse config failed: %v", err)
	}
	cu, ok := parsed["computer_use"].(map[string]interface{})
	if !ok {
		t.Fatalf("computer_use not found in saved config: %v", parsed)
	}
	if got, want := cu["enabled"], true; got != want {
		t.Fatalf("saved computer_use.enabled = %v, want %v", got, want)
	}

	// Reload via the package loader and assert both fields survive.
	var cfg Config
	if err := LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("LoadOcodeConfig failed: %v", err)
	}
	if !cfg.Ocode.ComputerUse.Enabled {
		t.Fatalf("loaded Ocode.ComputerUse.Enabled = false, want true")
	}
	if !cfg.Ocode.Ocr.Enabled {
		t.Fatalf("unrelated Ocode.Ocr.Enabled = false after round-trip, want true")
	}
}
