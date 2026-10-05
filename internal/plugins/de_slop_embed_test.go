package plugins_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/plugins"
	"github.com/u007/ocode/internal/skill"
)

// TestDeSlopEmbeddedPluginDiscoverable verifies the repo-embedded de-slop
// plugin is discovered from the project root and its skill loads as
// "de-slop:de-slop" (ocode's Plugin:skill namespacing).
func TestDeSlopEmbeddedPluginDiscoverable(t *testing.T) {
	// Repo root = ../.. from internal/plugins/.
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, ".opencode", "plugins", "de-slop", "plugin.json")
	if _, err := os.Stat(manifest); err != nil {
		t.Fatalf("de-slop plugin manifest not found at %s: %v", manifest, err)
	}

	// Plugin discovery.
	var found *plugins.Plugin
	for _, p := range plugins.LoadAllPluginsForProject(root) {
		if p.Name == "de-slop" {
			pp := p
			found = &pp
		}
	}
	if found == nil {
		t.Fatal("de-slop plugin not discovered from project root")
	}
	if len(found.SkillDirs) == 0 {
		t.Fatal("de-slop plugin has no skill dirs")
	}

	// Skill loading through the skill loader.
	skills := skill.LoadSkillsForRoot(root)
	var slop *skill.Skill
	for _, s := range skills {
		if s.Name == "de-slop:de-slop" {
			ss := s
			slop = &ss
		}
	}
	if slop == nil {
		names := make([]string, 0, len(skills))
		for _, s := range skills {
			names = append(names, s.Name)
		}
		t.Fatalf("de-slop:de-slop skill not loaded; got %v", names)
	}
	if slop.Plugin != "de-slop" {
		t.Fatalf("expected Plugin=de-slop, got %q", slop.Plugin)
	}
}
