package plugins

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/bundled"
)

// TestLoadPluginsIncludesBundled verifies the embedded (bundled) plugin is
// surfaced by LoadPlugins, and that LoadBundledPluginAgentsDirPaths returns
// its agents/ directory in the lowest-precedence position.
func TestLoadPluginsIncludesBundled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// Neutral cwd so no on-disk project plugin is discovered.
	t.Chdir(t.TempDir())

	bundledDir := t.TempDir()
	pdir := filepath.Join(bundledDir, "embeddedfallback")
	os.MkdirAll(filepath.Join(pdir, "agents"), 0o755)
	if err := os.WriteFile(filepath.Join(pdir, "plugin.json"), []byte(`{"name":"embeddedfallback","version":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pdir, "agents", "embeddedfallback.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	prev := bundled.PluginsDir
	bundled.PluginsDir = bundledDir
	defer func() { bundled.PluginsDir = prev }()

	plugins := LoadPlugins(nil)
	found := false
	for _, p := range plugins {
		if p.Name == "embeddedfallback" {
			found = true
		}
	}
	if !found {
		t.Fatal("bundled plugin not returned by LoadPlugins")
	}

	agentDirs := LoadBundledPluginAgentsDirPaths(nil)
	if len(agentDirs) != 1 {
		t.Fatalf("expected 1 bundled agent dir, got %d", len(agentDirs))
	}
	if filepath.Base(filepath.Dir(agentDirs[0])) != "embeddedfallback" {
		t.Fatalf("unexpected bundled agent dir %q", agentDirs[0])
	}
}
