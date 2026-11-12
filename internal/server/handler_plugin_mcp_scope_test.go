package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/plugins"
)

// writeProjectLocalPluginProject writes a project with a project-local plugin
// (under .opencode/plugins) that declares one MCP server, and an explicit enable
// for that plugin in the ocode config of the test home (home must already be
// set up with setHomeTree). An enable is what admits a project-local plugin's
// servers; opencode.json's plugins key is not read for that.
func writeProjectLocalPluginProject(t *testing.T, home, proj string) {
	t.Helper()
	dir := filepath.Join(proj, ".opencode", "plugins", "projplug")
	enable := filepath.Join(home, ".config", "opencode", "ocodeconfig.json")
	files := map[string]string{
		filepath.Join(dir, "plugin.json"): `{"name":"projplug"}`,
		filepath.Join(dir, ".mcp.json"):   `{"mcpServers":{"plugin-srv":{"command":"/nonexistent/plugin-srv-binary"}}}`,
		enable:                            `{"external_plugins":{"projplug":{"source":"local","dir":"` + dir + `","enabled":true}}}`,
	}
	for path, body := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestSharedConfigExcludesProjectLocalPluginServers pins that the process-wide
// config of a server whose default project has an explicitly enabled
// project-local plugin does not carry that plugin's server.
func TestSharedConfigExcludesProjectLocalPluginServers(t *testing.T) {
	home := t.TempDir()
	setHomeTree(t, home)
	proj := t.TempDir()
	writeProjectLocalPluginProject(t, home, proj)

	h := NewHandler()
	h.SetWorkDir(proj)
	h.mu.Lock()
	shared := h.cfg
	h.mu.Unlock()

	if _, ok := shared.MCP["plugin-srv"]; ok {
		t.Fatal("the process-wide config carries the project-local plugin server")
	}
	// Precondition: the project's own merge does see the enabled plugin, so the
	// assertion above is not passing for want of a setup.
	own := *shared
	if got := plugins.MergeMCPServers(&own, proj); got["plugin-srv"] != "projplug" {
		t.Fatalf("precondition failed: the project's own merge = %v, want plugin-srv from projplug", got)
	}
}

// TestProjectLocalPluginServerReachesOnlyItsProjectSessions pins that a session
// in the plugin's project loads the server through its own config, and a
// session in another project neither gets it nor takes the per-session path.
func TestProjectLocalPluginServerReachesOnlyItsProjectSessions(t *testing.T) {
	home := t.TempDir()
	setHomeTree(t, home)
	proj := t.TempDir()
	other := t.TempDir()
	writeProjectLocalPluginProject(t, home, proj)

	h := NewHandler()
	h.SetWorkDir(proj)

	// A session in the plugin's project: the merge includes the server, and the
	// server is not in the shared config, so the session takes its own path.
	inProj := &config.Config{Plugins: map[string]config.PluginConfig{"projplug": {Enabled: true}}}
	mergedProj := plugins.MergeMCPServers(inProj, proj)
	if mergedProj["plugin-srv"] != "projplug" {
		t.Fatalf("project session merge = %v, want plugin-srv", mergedProj)
	}
	if !h.hasSessionOnlyPluginMCP(mergedProj) {
		t.Fatal("the project's plugin server is not seen as session-only")
	}
	_, errs, own := h.mcpToolsForSession(inProj, "ses_in_project", true)
	if !own {
		t.Fatal("the project session did not take the per-session path")
	}
	if !containsSubstring(errs, "plugin-srv") {
		t.Fatalf("per-session errors = %v, want the plugin-srv load attempt from its own config", errs)
	}

	// A session in another project: no project-local server, shared path.
	inOther := &config.Config{Plugins: map[string]config.PluginConfig{"projplug": {Enabled: true}}}
	mergedOther := plugins.MergeMCPServers(inOther, other)
	if _, ok := mergedOther["plugin-srv"]; ok {
		t.Fatalf("a session in another project got the plugin server: %v", mergedOther)
	}
	if h.hasSessionOnlyPluginMCP(mergedOther) {
		t.Fatal("another project's session is treated as having its own plugin servers")
	}
	if _, _, own := h.mcpToolsForSession(inOther, "ses_other_project", false); own {
		t.Fatal("another project's session took the per-session path")
	}
}

func containsSubstring(list []string, sub string) bool {
	for _, s := range list {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
