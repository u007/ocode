package plugins_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
