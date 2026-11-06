package plugins_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/plugins"
	"github.com/u007/ocode/internal/skill"
)

// TestAgentDesktopEmbeddedPluginDiscoverable verifies the repo-embedded
// agent-desktop plugin is discovered from the project root and its skill loads
// as "agent-desktop:agent-desktop" (ocode's Plugin:skill namespacing), so the
// bundled copy ships without extra wiring.
func TestAgentDesktopEmbeddedPluginDiscoverable(t *testing.T) {
	root := repoRoot(t)
	if _, err := os.Stat(filepath.Join(root, ".opencode", "plugins", "agent-desktop", "plugin.json")); err != nil {
		t.Fatalf("agent-desktop plugin manifest not found: %v", err)
	}

	var found *plugins.Plugin
	for _, p := range plugins.LoadAllPluginsForProject(root) {
		if p.Name == "agent-desktop" {
			pp := p
			found = &pp
		}
	}
	if found == nil {
		t.Fatal("agent-desktop plugin not discovered from project root")
	}
	if got, want := found.Dir, filepath.Join(root, ".opencode", "plugins", "agent-desktop"); got != want {
		t.Fatalf("agent-desktop plugin resolved to %q, want the repo copy %q", got, want)
	}
	if len(found.SkillDirs) == 0 {
		t.Fatal("agent-desktop plugin has no skill dirs")
	}
	if !found.DefaultEnabled {
		t.Fatal("bundled agent-desktop plugin should be enabled by default")
	}

	skills := skill.LoadSkillsForRoot(root)
	var agentDesktop *skill.Skill
	for _, s := range skills {
		if s.Name == "agent-desktop:agent-desktop" {
			ss := s
			agentDesktop = &ss
		}
	}
	if agentDesktop == nil {
		t.Fatal("agent-desktop:agent-desktop skill not loaded")
	}
	if agentDesktop.Plugin != "agent-desktop" {
		t.Fatalf("expected Plugin=agent-desktop, got %q", agentDesktop.Plugin)
	}

	// The ocode host adaptation is the part that is not upstream text: without
	// it the skill would install the binary itself or drive the person's
	// pointer, and it would chase reference files this plugin does not ship.
	for _, want := range []string{
		"## In ocode",
		"Never install it yourself",
		"computer",
		"agent-desktop skills get desktop <reference>",
	} {
		if !strings.Contains(agentDesktop.Content, want) {
			t.Fatalf("agent-desktop skill is missing %q", want)
		}
	}
	if strings.Contains(agentDesktop.Content, "references/") {
		t.Fatal("agent-desktop skill points at reference files this plugin does not ship")
	}
	if !strings.Contains(agentDesktop.ForModel(), "Plugin root:") {
		t.Fatal("plugin skill should tell the model its plugin root")
	}
}

// TestAgentDesktopPluginDisableToggle verifies the plugin is switchable: the
// ocode config entry turns it off and on without touching the bundled copy.
func TestAgentDesktopPluginDisableToggle(t *testing.T) {
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

	if !contains(names(nil), "agent-desktop") {
		t.Fatal("agent-desktop should load when the config has no entry for it")
	}
	if contains(names(map[string]bool{"agent-desktop": false}), "agent-desktop") {
		t.Fatal("agent-desktop should not load when disabled")
	}
	if !contains(names(map[string]bool{"agent-desktop": true}), "agent-desktop") {
		t.Fatal("agent-desktop should load when enabled")
	}
}
