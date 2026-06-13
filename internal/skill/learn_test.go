package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadProjectLearnInventoryListsProjectRootSkillsOnly(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	writeLearnFile(t, filepath.Join(root, "skills", "ocode-tui", "SKILL.md"), `---
name: ocode-tui
description: TUI architecture guide.
when_to_use: Use when working on internal/tui.
---

# ocode-tui
`)
	writeLearnFile(t, filepath.Join(root, ".opencode", "skills", "release-checks", "SKILL.md"), `---
name: release-checks
description: Release checklist.
when_to_use: Before release.
---

# release-checks
`)

	inventory, err := LoadProjectLearnInventory(root)
	if err != nil {
		t.Fatalf("LoadProjectLearnInventory: %v", err)
	}
	if len(inventory.ProjectSkills) != 2 {
		t.Fatalf("expected 2 project skills, got %d", len(inventory.ProjectSkills))
	}
	for _, got := range inventory.ProjectSkills {
		if strings.Contains(got.Source, ".config/opencode") || strings.Contains(got.Source, ".agents/skills") {
			t.Fatalf("expected only project-root skills, got source %q", got.Source)
		}
	}
}

func TestBuildLearnContextIsInventoryOnly(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	writeLearnFile(t, filepath.Join(root, "skills", "auth-guide", "SKILL.md"), `---
name: auth-guide
description: Covers auth flows.
when_to_use: Use when changing auth.
---

# auth-guide
`)

	inventory, err := LoadProjectLearnInventory(root)
	if err != nil {
		t.Fatalf("LoadProjectLearnInventory: %v", err)
	}
	ctx := BuildLearnContext(inventory, "auth reliability")
	for _, want := range []string{
		"User focus: auth reliability",
		"Project-root skills",
		"auth-guide",
		"did not inspect repo modules or do gap discovery",
	} {
		if !strings.Contains(ctx, want) {
			t.Fatalf("expected %q in context, got:\n%s", want, ctx)
		}
	}
	for _, unwanted := range []string{"High-value module gaps", "Significant modules/sections discovered"} {
		if strings.Contains(ctx, unwanted) {
			t.Fatalf("did not expect %q in context, got:\n%s", unwanted, ctx)
		}
	}
}

func writeLearnFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
