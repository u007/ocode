package plugins_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/plugins"
	"github.com/u007/ocode/internal/skill"
)

// repoRoot resolves the repository root from internal/plugins/.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// TestAtlasEmbeddedPluginDiscoverable verifies the repo-embedded atlas plugin
// is discovered from the project root and its skill loads as "atlas:atlas"
// (ocode's Plugin:skill namespacing), so the bundled copy ships without extra
// wiring.
func TestAtlasEmbeddedPluginDiscoverable(t *testing.T) {
	root := repoRoot(t)
	if _, err := os.Stat(filepath.Join(root, ".opencode", "plugins", "atlas", "plugin.json")); err != nil {
		t.Fatalf("atlas plugin manifest not found: %v", err)
	}

	var found *plugins.Plugin
	for _, p := range plugins.LoadAllPluginsForProject(root) {
		if p.Name == "atlas" {
			pp := p
			found = &pp
		}
	}
	if found == nil {
		t.Fatal("atlas plugin not discovered from project root")
	}
	if got, want := found.Dir, filepath.Join(root, ".opencode", "plugins", "atlas"); got != want {
		t.Fatalf("atlas plugin resolved to %q, want the repo copy %q", got, want)
	}
	if len(found.SkillDirs) == 0 {
		t.Fatal("atlas plugin has no skill dirs")
	}
	if !found.DefaultEnabled {
		t.Fatal("bundled atlas plugin should be enabled by default")
	}

	skills := skill.LoadSkillsForRoot(root)
	var atlas *skill.Skill
	for _, s := range skills {
		if s.Name == "atlas:atlas" {
			ss := s
			atlas = &ss
		}
	}
	if atlas == nil {
		t.Fatal("atlas:atlas skill not loaded")
	}
	if atlas.Plugin != "atlas" {
		t.Fatalf("expected Plugin=atlas, got %q", atlas.Plugin)
	}

	// The ocode host adaptation is the part that is not upstream text: without
	// it the skill would tell the agent to call tools ocode does not have.
	for _, want := range []string{"## In ocode", "ATLAS_SESSION=ocode", "atlas monitor"} {
		if !strings.Contains(atlas.Content, want) {
			t.Fatalf("atlas skill is missing %q", want)
		}
	}
	if strings.Contains(atlas.Content, "references/commands.md") {
		t.Fatal("atlas skill points at upstream reference files this plugin does not ship")
	}
	if !strings.Contains(atlas.ForModel(), "Plugin root:") {
		t.Fatal("plugin skill should tell the model its plugin root")
	}
}

// TestAtlasPluginDeclaresStdioMCPServer verifies the repo atlas plugin ships
// its .mcp.json, so the atlas MCP server (the tools the SKILL.md "In ocode"
// section names) is declared as a stdio server started through /bin/sh.
func TestAtlasPluginDeclaresStdioMCPServer(t *testing.T) {
	root := repoRoot(t)
	want := filepath.Join(root, ".opencode", "plugins", "atlas")
	var atlas *plugins.Plugin
	for _, p := range plugins.LoadAllPluginsForProject(root) {
		if p.Name == "atlas" && p.Dir == want {
			pp := p
			atlas = &pp
		}
	}
	if atlas == nil {
		t.Fatalf("repo atlas plugin not discovered at %q", want)
	}
	mc, ok := atlas.MCPServers["atlas"]
	if !ok {
		t.Fatalf("atlas plugin declares no atlas MCP server: %+v", atlas.MCPServers)
	}
	if len(mc.Command) == 0 || mc.Command[0] != "/bin/sh" {
		t.Fatalf("atlas MCP command = %q, want to start with /bin/sh", mc.Command)
	}
	if mc.Type != "local" || !mc.Enabled {
		t.Fatalf("atlas MCP server should be an enabled stdio (local) server: %+v", mc)
	}
	// The repo copy is project-local, so its server starts only after an
	// explicit enable, never from DefaultEnabled alone.
	if !atlas.ProjectScoped {
		t.Fatal("repo atlas plugin lives in the project and must be ProjectScoped")
	}
	if got := plugins.MergeMCPServers(&config.Config{}, root); got["atlas"] != "" {
		t.Fatalf("atlas MCP server merged without an explicit enable: %v", got)
	}
	cfg := &config.Config{Plugins: map[string]config.PluginConfig{"atlas": {Enabled: true}}}
	plugins.MergeMCPServers(cfg, root)
	if got := cfg.MCP["atlas"].Command; len(got) == 0 || got[0] != "/bin/sh" {
		t.Fatalf("explicitly enabled atlas MCP command = %q, want to start with /bin/sh", got)
	}
}

// TestAtlasPluginDisableToggle verifies the plugin is enable-able: the ocode
// config entry turns it off and on without touching the bundled copy.
func TestAtlasPluginDisableToggle(t *testing.T) {
	root := repoRoot(t)

	names := func(enabled map[string]bool) []string {
		var out []string
		for _, p := range plugins.LoadPluginsForProject(enabled, root) {
			out = append(out, p.Name)
		}
		return out
	}
	contains := func(list []string, name string) bool {
		for _, v := range list {
			if v == name {
				return true
			}
		}
		return false
	}

	if !contains(names(nil), "atlas") {
		t.Fatal("atlas should load when the config has no entry for it")
	}
	if contains(names(map[string]bool{"atlas": false}), "atlas") {
		t.Fatal("atlas should not load when disabled")
	}
	if !contains(names(map[string]bool{"atlas": true}), "atlas") {
		t.Fatal("atlas should load when enabled")
	}
}
