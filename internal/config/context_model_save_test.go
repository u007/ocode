package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestSaveContextModelPersists pins that SaveContextModel actually changes
// the persisted config.
//
// The model-override keys ("context_model", "context_model_enabled",
// "explorer_model", "explorer_model_enabled", "speech_summary_model") are
// NOT in writeOcodeConfigFile's Extra-skip list, so the stale raw value
// preserved in cfg.Extra is written back over the canonical value the setter
// just applied. The save therefore reports success but the file still holds
// the OLD model — which is how the context sub-agent stayed pinned to a
// provider whose key had expired even after the user (or an API call)
// changed it.
func TestSaveContextModelPersists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	// Seed a config file that already carries a context_model, so the
	// stale-raw-value path is exercised.
	dir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	seed := `{"context_model":"opencode-go/old-model","context_model_enabled":true}`
	if err := os.WriteFile(filepath.Join(dir, "ocodeconfig.json"), []byte(seed), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	if err := SaveContextModel("deepseek/deepseek-v4-pro"); err != nil {
		t.Fatalf("SaveContextModel: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "ocodeconfig.json"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	if got["context_model"] != "deepseek/deepseek-v4-pro" {
		t.Fatalf("context_model = %v, want %q — the save did not persist (stale raw value overwrote it)", got["context_model"], "deepseek/deepseek-v4-pro")
	}
}
