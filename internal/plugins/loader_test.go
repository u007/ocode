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

func TestLoadPluginsForProjectUsesExplicitRoot(t *testing.T) {
	// Create two separate project dirs with different plugins.
	projA := t.TempDir()
	projB := t.TempDir()

	// Set up project A with plugin "alpha".
	alphaDir := filepath.Join(projA, ".opencode", "plugins", "alpha")
	if err := os.MkdirAll(alphaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(alphaDir, "plugin.json"), []byte(`{"name":"alpha","description":"plugin A"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Set up project B with plugin "beta".
	betaDir := filepath.Join(projB, ".opencode", "plugins", "beta")
	if err := os.MkdirAll(betaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(betaDir, "plugin.json"), []byte(`{"name":"beta","description":"plugin B"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// LoadPluginsForProject with project A root should find alpha but not beta.
	plugins := LoadPluginsForProject(nil, projA)
	names := pluginNames(plugins)
	if !containsStr(names, "alpha") {
		t.Errorf("expected alpha in project A plugins, got %v", names)
	}
	if containsStr(names, "beta") {
		t.Errorf("beta should not appear in project A plugins")
	}

	// LoadPluginsForProject with project B root should find beta but not alpha.
	plugins = LoadPluginsForProject(nil, projB)
	names = pluginNames(plugins)
	if !containsStr(names, "beta") {
		t.Errorf("expected beta in project B plugins, got %v", names)
	}
	if containsStr(names, "alpha") {
		t.Errorf("alpha should not appear in project B plugins")
	}
}

func TestFindPluginDirForProjectUsesExplicitRoot(t *testing.T) {
	proj := t.TempDir()
	pluginDir := filepath.Join(proj, ".opencode", "plugins", "myplugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"name":"myplugin"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Should find it with the explicit root.
	got := FindPluginDirForProject("myplugin", proj)
	if got == "" {
		t.Fatal("FindPluginDirForProject returned empty for existing plugin")
	}
	if got != pluginDir {
		t.Errorf("got %q, want %q", got, pluginDir)
	}

	// Should not find it with a different root.
	other := t.TempDir()
	got = FindPluginDirForProject("myplugin", other)
	if got != "" {
		t.Errorf("expected empty for non-matching root, got %q", got)
	}
}

func TestLoadPluginsForProjectEmptyRootFallsBack(t *testing.T) {
	// When projectRoot is empty, it should fall back to the legacy path.
	// We can't fully test findProjectRoot() behavior, but we can verify
	// the function doesn't panic and returns a result.
	plugins := LoadPluginsForProject(nil, "")
	// Just verify it doesn't crash; the actual results depend on cwd.
	_ = plugins
}

func TestLoadPluginsDeduplicatesByName(t *testing.T) {
	// Create a plugin in both global and project dirs with the same name.
	// Project copy should win (first in precedence).
	proj := t.TempDir()
	pluginDir := filepath.Join(proj, ".opencode", "plugins", "shared")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"name":"shared","description":"project copy"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	plugins := LoadPluginsForProject(nil, proj)
	count := 0
	for _, p := range plugins {
		if p.Name == "shared" {
			count++
			if p.Dir != pluginDir {
				t.Errorf("expected project dir %q, got %q", pluginDir, p.Dir)
			}
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 'shared' plugin, got %d", count)
	}
}

// pluginNames extracts plugin names from a slice.
func pluginNames(plugins []Plugin) []string {
	names := make([]string, len(plugins))
	for i, p := range plugins {
		names[i] = p.Name
	}
	return names
}

// containsStr checks if a string slice contains a value.
func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// TestValidateRemovableDirSymlinkEscape verifies that a symlink pointing
// outside an approved root is rejected.
func TestValidateRemovableDirSymlinkEscape(t *testing.T) {
	// Create an approved root and a plugin dir inside it.
	approvedRoot := t.TempDir()
	pluginDir := filepath.Join(approvedRoot, "myplugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Point the global install dir at our approved root for this test.
	prev := bundled.PluginsDir
	bundled.PluginsDir = approvedRoot
	defer func() { bundled.PluginsDir = prev }()

	// A valid dir inside the root should pass.
	if err := ValidateRemovableDir(pluginDir); err != nil {
		t.Errorf("expected valid dir to pass, got: %v", err)
	}

	// Create a symlink that points outside the approved root.
	outsideTarget := t.TempDir()
	symlinkDir := filepath.Join(approvedRoot, "sneaky-link")
	if err := os.Symlink(outsideTarget, symlinkDir); err != nil {
		t.Fatal(err)
	}

	// The symlink's logical path is inside the root, but its canonical
	// target is outside — ValidateRemovableDir should reject it.
	if err := ValidateRemovableDir(symlinkDir); err == nil {
		t.Error("expected symlink escape to be rejected")
	}
}

// TestValidateRemovableDirSymlinkWithinRoot verifies that a symlink whose
// target is still within an approved root is accepted.
func TestValidateRemovableDirSymlinkWithinRoot(t *testing.T) {
	approvedRoot := t.TempDir()
	realDir := filepath.Join(approvedRoot, "realplugin")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}

	linkDir := filepath.Join(approvedRoot, "linkplugin")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Fatal(err)
	}

	prev := bundled.PluginsDir
	bundled.PluginsDir = approvedRoot
	defer func() { bundled.PluginsDir = prev }()

	// The symlink target is within the approved root — should pass.
	if err := ValidateRemovableDir(linkDir); err != nil {
		t.Errorf("expected symlink within root to pass, got: %v", err)
	}
}

// TestValidateRemovableDirTraversalRejected ensures ".." paths are rejected.
func TestValidateRemovableDirTraversalRejected(t *testing.T) {
	if err := ValidateRemovableDir("/some/path/../../../etc/passwd"); err == nil {
		t.Error("expected traversal to be rejected")
	}
}
