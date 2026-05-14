package config

import (
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
	if path != configPath {
		t.Errorf("expected %s, got %s", configPath, path)
	}
}
