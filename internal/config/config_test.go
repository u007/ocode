package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGetProjectConfigPath(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ocode-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)

	configPath := filepath.Join(tmpDir, "opencode.json")
	if err := os.WriteFile(configPath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	path, err := getProjectConfigPath()
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	resolvedExpected, err := filepath.EvalSymlinks(configPath)
	if err != nil {
		t.Fatal(err)
	}
	resolvedGot, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	if resolvedGot != resolvedExpected {
		t.Errorf("expected %s, got %s", resolvedExpected, resolvedGot)
	}
}

func TestLoadCreatesOcodeConfigFiles(t *testing.T) {
	tmpHome, err := os.MkdirTemp("", "ocode-home")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpHome)

	tmpDir, err := os.MkdirTemp("", "ocode-project")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)

	if err := os.WriteFile(filepath.Join(tmpDir, "opencode.json"), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", tmpHome)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Ocode == nil {
		t.Fatal("expected ocode config to load")
	}
	if !cfg.Ocode.Compact.Enabled {
		t.Fatal("expected compact to default to enabled")
	}

	globalPath := filepath.Join(tmpHome, ".config", "opencode", "ocodeconfig.json")
	projectPath := filepath.Join(tmpDir, "ocodeconfig.json")
	for _, path := range []string{globalPath, projectPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to be created: %v", path, err)
		}
	}

	data, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]json.RawMessage
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if _, ok := saved["compact"]; !ok {
		t.Fatal("expected compact section in ocodeconfig.json")
	}
}
