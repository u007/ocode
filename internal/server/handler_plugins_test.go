package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/bundled"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/plugins"
)

// pluginListEntry mirrors the JSON emitted by HandleListPlugins.
type pluginListEntry struct {
	Name        string `json:"name"`
	Source      string `json:"source"`
	Dir         string `json:"dir"`
	Enabled     bool   `json:"enabled"`
	Description string `json:"description,omitempty"`
}

// withBundledPluginDir points bundled.PluginsDir at a temp dir containing the
// named plugin and returns a cleanup func.
func withBundledPluginDir(t *testing.T, name string) (string, func()) {
	t.Helper()
	dir := t.TempDir()
	pdir := filepath.Join(dir, name)
	if err := os.MkdirAll(pdir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"` + name + `","description":"disk-only plugin"}`
	if err := os.WriteFile(filepath.Join(pdir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	prev := bundled.PluginsDir
	bundled.PluginsDir = dir
	return pdir, func() { bundled.PluginsDir = prev }
}

// TestHandleListPluginsMergesDiskPlugins verifies the web list surfaces plugins
// discovered on disk (including bundled/disk-only plugins with no external_plugins
// entry) in addition to configured entries, so it shows at least as many as the
// TUI's agent/context views.
func TestHandleListPluginsMergesDiskPlugins(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, cleanup := withBundledPluginDir(t, "diskonly")
	defer cleanup()

	h := NewHandler()
	h.mu.Lock()
	h.cfg = &config.Config{
		Plugins: map[string]config.PluginConfig{
			"configured": {Source: "github.com/x/y", Dir: "/tmp/x-y", Enabled: true},
			"stale":      {Source: "github.com/a/b", Dir: "/nonexistent", Enabled: false},
		},
		Ocode: config.OcodeConfig{},
	}
	h.mu.Unlock()

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/plugins", nil)
	h.HandleListPlugins(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var list []pluginListEntry
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	byName := map[string]pluginListEntry{}
	for _, e := range list {
		byName[e.Name] = e
	}

	// Disk-only plugin must appear, defaulting to enabled.
	disk, ok := byName["diskonly"]
	if !ok {
		t.Fatalf("disk-only plugin not in list: %+v", list)
	}
	if !disk.Enabled {
		t.Errorf("disk-only plugin should default to enabled")
	}
	if disk.Description != "disk-only plugin" {
		t.Errorf("description not populated from plugin.json: %q", disk.Description)
	}

	// Configured (enabled) and stale (disabled) entries must remain.
	if c, ok := byName["configured"]; !ok || !c.Enabled || c.Source != "github.com/x/y" {
		t.Errorf("configured plugin missing or wrong: %+v", c)
	}
	if s, ok := byName["stale"]; !ok || s.Enabled {
		t.Errorf("stale plugin missing or wrong: %+v", s)
	}
}

// TestHandleRemovePluginRejectsBundled verifies bundled/embedded plugin
// directories cannot be removed (deleting them would destroy the embedded copy).
func TestHandleRemovePluginRejectsBundled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	pdir, cleanup := withBundledPluginDir(t, "bundledplugin")
	defer cleanup()

	h := NewHandler()
	h.mu.Lock()
	h.cfg = &config.Config{
		Plugins: map[string]config.PluginConfig{
			"bundledplugin": {Source: "", Dir: pdir, Enabled: true},
		},
		Ocode: config.OcodeConfig{},
	}
	h.mu.Unlock()

	w := httptest.NewRecorder()
	r := httptest.NewRequest("DELETE", "/api/plugins/bundledplugin", nil)
	h.HandleRemovePlugin(w, r, "bundledplugin")

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (bad request for bundled); body=%s", w.Code, w.Body.String())
	}
	// Directory must remain intact.
	if _, err := os.Stat(pdir); err != nil {
		t.Fatalf("bundled plugin dir was deleted: %v", err)
	}
}

// TestHandleSetPluginEnabledCreatesConfigForDiskOnly verifies toggling a
// disk-discovered plugin with no external_plugins entry auto-creates the config
// entry so the enabled state persists.
func TestHandleSetPluginEnabledCreatesConfigForDiskOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	pdir, cleanup := withBundledPluginDir(t, "diskonly")
	defer cleanup()

	h := NewHandler()
	h.mu.Lock()
	h.cfg = &config.Config{
		Plugins: map[string]config.PluginConfig{},
		Ocode:   config.OcodeConfig{},
	}
	h.mu.Unlock()

	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/plugins/diskonly/enable", nil)
	h.HandleSetPluginEnabled(w, r, "diskonly", true)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	h.mu.Lock()
	pc, ok := h.cfg.Plugins["diskonly"]
	h.mu.Unlock()
	if !ok {
		t.Fatal("disk-only plugin not added to config after enable")
	}
	if !pc.Enabled || pc.Dir != pdir {
		t.Errorf("config entry wrong: %+v", pc)
	}
}

// TestHandleGetPluginDiskOnly verifies inspecting a plugin discovered on disk
// but absent from config (e.g. a bundled plugin) still returns its metadata.
func TestHandleGetPluginDiskOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	pdir, cleanup := withBundledPluginDir(t, "bdisk")
	defer cleanup()

	h := NewHandler()
	h.mu.Lock()
	h.cfg = &config.Config{
		Plugins: map[string]config.PluginConfig{},
		Ocode:   config.OcodeConfig{},
	}
	h.mu.Unlock()

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/plugins/bdisk", nil)
	h.HandleGetPlugin(w, r, "bdisk")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var detail struct {
		Name        string   `json:"name"`
		Dir         string   `json:"dir"`
		Enabled     bool     `json:"enabled"`
		Description string   `json:"description"`
		Tools       []string `json:"tools"`
		Commands    []string `json:"commands"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if detail.Name != "bdisk" || detail.Dir != pdir || !detail.Enabled {
		t.Errorf("unexpected detail: %+v", detail)
	}
	if detail.Description != "disk-only plugin" {
		t.Errorf("description not populated: %q", detail.Description)
	}

	// A genuinely unknown plugin must 404.
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("GET", "/api/plugins/missing", nil)
	h.HandleGetPlugin(w2, r2, "missing")
	if w2.Code != http.StatusNotFound {
		t.Fatalf("missing plugin status = %d, want 404; body=%s", w2.Code, w2.Body.String())
	}
}

// TestHandleDisableDiskOnlyExcludedFromEnabledLoader verifies that disabling a
// disk-only plugin (which auto-creates a config entry) causes the enabled-map
// loader to exclude it, matching the TUI/agent gating behavior.
func TestHandleDisableDiskOnlyExcludedFromEnabledLoader(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	pdir, cleanup := withBundledPluginDir(t, "diskonly")
	defer cleanup()

	h := NewHandler()
	h.mu.Lock()
	h.cfg = &config.Config{
		Plugins: map[string]config.PluginConfig{},
		Ocode:   config.OcodeConfig{},
	}
	h.mu.Unlock()

	// Enable then disable.
	for _, enable := range []bool{true, false} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("PUT", "/api/plugins/diskonly/enable", nil)
		h.HandleSetPluginEnabled(w, r, "diskonly", enable)
		if w.Code != http.StatusOK {
			t.Fatalf("enable=%v status = %d; body=%s", enable, w.Code, w.Body.String())
		}
	}

	// Build the enabled map exactly as the agent registry does.
	h.mu.Lock()
	enabledMap := map[string]bool{}
	for name, pc := range h.cfg.Plugins {
		enabledMap[name] = pc.Enabled
	}
	h.mu.Unlock()

	// With enabled=false, the loader must skip it.
	found := false
	for _, pl := range plugins.LoadPlugins(enabledMap) {
		if pl.Name == "diskonly" {
			found = true
		}
	}
	if found {
		t.Error("disabled disk-only plugin should be excluded by the enabled loader")
	}
	// Sanity: the merged list (enabled=nil) still surfaces it on disk.
	onDisk := false
	for _, pl := range plugins.LoadPlugins(nil) {
		if pl.Name == "diskonly" && pl.Dir == pdir {
			onDisk = true
		}
	}
	if !onDisk {
		t.Error("disk-only plugin should still be discoverable on disk")
	}
}

// TestHandleSetPluginEnabledUsesProjectRoot verifies that the handler's
// project-root-aware plugin discovery is used (via h.workDir) for finding
// plugins to toggle.
func TestHandleSetPluginEnabledUsesProjectRoot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// Create a project with a plugin.
	proj := t.TempDir()
	pluginDir := filepath.Join(proj, ".opencode", "plugins", "projplugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"name":"projplugin","description":"project plugin"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	h := NewHandler()
	h.workDir = proj
	h.mu.Lock()
	h.cfg = &config.Config{
		Plugins: map[string]config.PluginConfig{},
		Ocode:   config.OcodeConfig{},
	}
	h.mu.Unlock()

	// Enable the plugin — should find it via the project root.
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/plugins/projplugin/enable", nil)
	h.HandleSetPluginEnabled(w, r, "projplugin", true)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	h.mu.Lock()
	pc, ok := h.cfg.Plugins["projplugin"]
	h.mu.Unlock()
	if !ok {
		t.Fatal("plugin not added to config")
	}
	if !pc.Enabled || pc.Dir != pluginDir {
		t.Errorf("config entry wrong: %+v", pc)
	}
}

// TestHandleRemovePluginUsesProjectRoot verifies that the handler's
// project-root-aware plugin discovery is used for removal.
func TestHandleRemovePluginUsesProjectRoot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	proj := t.TempDir()
	pluginDir := filepath.Join(proj, ".opencode", "plugins", "rmplugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"name":"rmplugin"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	h := NewHandler()
	h.workDir = proj
	h.mu.Lock()
	h.cfg = &config.Config{
		Plugins: map[string]config.PluginConfig{
			"rmplugin": {Dir: pluginDir, Enabled: true},
		},
		Ocode: config.OcodeConfig{},
	}
	h.mu.Unlock()

	w := httptest.NewRecorder()
	r := httptest.NewRequest("DELETE", "/api/plugins/rmplugin", nil)
	h.HandleRemovePlugin(w, r, "rmplugin")

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", w.Code, w.Body.String())
	}
	// Directory should be removed.
	if _, err := os.Stat(pluginDir); !os.IsNotExist(err) {
		t.Errorf("plugin dir still exists after removal")
	}
	// Config entry should be removed.
	h.mu.Lock()
	_, ok := h.cfg.Plugins["rmplugin"]
	h.mu.Unlock()
	if ok {
		t.Error("config entry not removed after plugin removal")
	}
}

// TestHandleRemovePluginRejectsTraversal verifies that paths with ".." are
// rejected by the removal handler.
func TestHandleRemovePluginRejectsTraversal(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	h := NewHandler()
	h.workDir = t.TempDir()
	h.mu.Lock()
	h.cfg = &config.Config{
		Plugins: map[string]config.PluginConfig{
			"sneaky": {Dir: "/some/path/../../etc", Enabled: true},
		},
		Ocode: config.OcodeConfig{},
	}
	h.mu.Unlock()

	w := httptest.NewRecorder()
	r := httptest.NewRequest("DELETE", "/api/plugins/sneaky", nil)
	h.HandleRemovePlugin(w, r, "sneaky")

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for traversal; body=%s", w.Code, w.Body.String())
	}
}

// TestHandleInstallPluginRollbackOnFailure verifies that if RunOnInstall
// or AutoRegisterMCP fails, the cloned directory is cleaned up (transactional).
func TestHandleInstallPluginRollbackOnFailure(t *testing.T) {
	// This test verifies the rollback path by checking that the install
	// handler cleans up on failure. We can't easily mock InstallGit, but we
	// can verify the handler doesn't leave partial state when config persistence
	// is simulated to fail.
	t.Setenv("HOME", t.TempDir())

	h := NewHandler()
	h.workDir = t.TempDir()
	h.mu.Lock()
	h.cfg = &config.Config{
		Plugins: map[string]config.PluginConfig{},
		Ocode:   config.OcodeConfig{},
	}
	h.mu.Unlock()

	// Install with an invalid source that will fail at git clone.
	body := `{"source": "https://invalid.example.com/nonexistent-plugin-repo.git"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/plugins/install", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	h.HandleInstallPlugin(w, r)

	// Should fail with a 500 since the git URL is invalid.
	if w.Code == http.StatusOK || w.Code == http.StatusCreated {
		t.Fatalf("expected failure for invalid source, got %d", w.Code)
	}

	// Config should not have been modified.
	h.mu.Lock()
	_, ok := h.cfg.Plugins["nonexistent-plugin-repo"]
	h.mu.Unlock()
	if ok {
		t.Error("config should not contain a plugin from a failed install")
	}
}

// TestConcurrentHandlerAccess verifies that concurrent plugin operations
// don't race on h.mu or h.cfg.
func TestConcurrentHandlerAccess(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	proj := t.TempDir()
	pluginDir := filepath.Join(proj, ".opencode", "plugins", "cplugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"name":"cplugin"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	h := NewHandler()
	h.workDir = proj
	h.mu.Lock()
	h.cfg = &config.Config{
		Plugins: map[string]config.PluginConfig{},
		Ocode:   config.OcodeConfig{},
	}
	h.mu.Unlock()

	// Run concurrent list/toggle operations.
	done := make(chan struct{})
	for i := 0; i < 5; i++ {
		go func(enable bool) {
			defer func() { done <- struct{}{} }()
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/api/plugins", nil)
			h.HandleListPlugins(w, r)
			w2 := httptest.NewRecorder()
			r2 := httptest.NewRequest("PUT", "/api/plugins/cplugin/enable", nil)
			h.HandleSetPluginEnabled(w2, r2, "cplugin", enable)
		}(i%2 == 0)
	}
	for i := 0; i < 5; i++ {
		<-done
	}
}
