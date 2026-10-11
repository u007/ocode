package skill

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/bundled"
)

func TestParseSkillMetadataFromFrontmatter(t *testing.T) {
	content := `---
name: "Deploy Helper"
description: Guides deployment verification and rollback checks.
when_to_use: Use when shipping infra or app changes.
---

# Ignored title
`

	skill := parseSkillMetadata(content)
	if skill.Name != "Deploy Helper" {
		t.Fatalf("expected name from frontmatter, got %q", skill.Name)
	}
	if skill.Description != "Guides deployment verification and rollback checks." {
		t.Fatalf("unexpected description %q", skill.Description)
	}
	if skill.WhenToUse != "Use when shipping infra or app changes." {
		t.Fatalf("unexpected when-to-use %q", skill.WhenToUse)
	}
}

func TestParseSkillMetadataFallsBackToHeadingsAndBody(t *testing.T) {
	content := `# Incident Response

Purpose: Coordinate triage, mitigation, and follow-up for production incidents.
When to use: During outages, severe degradations, or time-sensitive customer impact.

## Steps
- assess
`

	skill := parseSkillMetadata(content)
	if skill.Name != "Incident Response" {
		t.Fatalf("expected heading-derived name, got %q", skill.Name)
	}
	if skill.Description != "Coordinate triage, mitigation, and follow-up for production incidents." {
		t.Fatalf("unexpected description %q", skill.Description)
	}
	if skill.WhenToUse != "During outages, severe degradations, or time-sensitive customer impact." {
		t.Fatalf("unexpected when-to-use %q", skill.WhenToUse)
	}
}

func TestBuildCatalogIncludesCompactEntries(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	project := t.TempDir()
	t.Chdir(project)

	skillDir := project + "/skills/release-checks"
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := `---
name: Release Checks
description: Verify readiness before deployment.
when_to_use: Before cutting a release or promoting a build.
---

# Release Checks
Full content here.
`
	if err := os.WriteFile(skillDir+"/SKILL.md", []byte(content), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	catalog := BuildCatalog()
	for _, want := range []string{
		"--- Skill Catalog ---",
		"Release Checks: Verify readiness before deployment.",
		"When to use: Before cutting a release or promoting a build.",
		"load full SKILL.md contents on demand",
	} {
		if !strings.Contains(catalog, want) {
			t.Fatalf("expected %q in catalog, got:\n%s", want, catalog)
		}
	}
	if strings.Contains(catalog, "Full content here") {
		t.Fatalf("catalog should not include full skill content, got:\n%s", catalog)
	}
}

func writeNested(t *testing.T, parts ...string) string {
	t.Helper()
	p := filepath.Join(parts...)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func findSkill(skills []Skill, name string) *Skill {
	for i := range skills {
		if skills[i].Name == name {
			return &skills[i]
		}
	}
	return nil
}

// TestBundledSkillsFallbackAndDiskWins checks two things:
//  1. When no disk copy exists, the embedded (bundled) skill is served.
//  2. When a disk copy exists, it overrides the bundled copy (bundled is the
//     lowest-precedence path, appended last in SkillSearchPathsForRoot).
func TestBundledSkillsFallbackAndDiskWins(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	bundledDir := t.TempDir()
	writeNested(t, bundledDir, "orchestrator", "SKILL.md")
	if err := os.WriteFile(filepath.Join(bundledDir, "orchestrator", "SKILL.md"), []byte("BUNDLED"), 0o644); err != nil {
		t.Fatal(err)
	}

	diskRoot := t.TempDir()
	writeNested(t, diskRoot, "skills", "orchestrator", "SKILL.md")
	if err := os.WriteFile(filepath.Join(diskRoot, "skills", "orchestrator", "SKILL.md"), []byte("DISK"), 0o644); err != nil {
		t.Fatal(err)
	}

	prev := bundled.SkillsDir
	bundled.SkillsDir = bundledDir
	defer func() { bundled.SkillsDir = prev }()

	// Disk copy present -> disk wins.
	skills := LoadSkillsForRoot(diskRoot)
	got := findSkill(skills, "orchestrator")
	if got == nil {
		t.Fatal("orchestrator skill not found")
	}
	if got.Content != "DISK" {
		t.Fatalf("disk should override bundled, got %q", got.Content)
	}

	// No disk copy -> bundled fallback used.
	skills2 := LoadSkillsForRoot(t.TempDir())
	got2 := findSkill(skills2, "orchestrator")
	if got2 == nil {
		t.Fatal("bundled orchestrator skill not found when disk absent")
	}
	if got2.Content != "BUNDLED" {
		t.Fatalf("expected bundled fallback, got %q", got2.Content)
	}
}

// TestSymlinkedSkillDirIsDiscovered pins that a skill directory installed as a
// SYMLINK is still found on the search path.
//
// This is not hypothetical: os.ReadDir reports a symlink-to-directory with
// DirEntry.IsDir() == false (its Type() carries ModeSymlink, not ModeDir), so a
// loader that trusts IsDir() alone silently SKIPS every symlinked skill. The
// skill then vanishes from the catalog with no error anywhere — the model simply
// stops being offered it.
//
// The layout this guards is the standard one for sharing one skill checkout
// between a plugin install and a search-path root: the real files live in a
// plugin/versioned checkout, and the search-path entry is a symlink into it (see
// the using-git-worktrees skill, symlinked from ~/.agents/skills into the
// u007-superpowers plugin clone). Losing the stat-through-the-link fallback
// would therefore break discovery for real, so this test fails loudly instead.
func TestSymlinkedSkillDirIsDiscovered(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevation on Windows")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)

	// The real skill lives outside the search path, as a versioned checkout would.
	checkout := t.TempDir()
	realSkill := filepath.Join(checkout, "linked-skill")
	if err := os.MkdirAll(realSkill, 0o755); err != nil {
		t.Fatal(err)
	}
	const body = "LINKED-CONTENT"
	if err := os.WriteFile(filepath.Join(realSkill, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	// Link it into ~/.agents/skills, which SkillSearchPathsForRoot includes.
	agentsSkills := filepath.Join(home, ".agents", "skills")
	if err := os.MkdirAll(agentsSkills, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(agentsSkills, "linked-skill")
	if err := os.Symlink(realSkill, link); err != nil {
		t.Fatal(err)
	}

	skills := LoadSkillsForRoot(t.TempDir())
	got := findSkill(skills, "linked-skill")
	if got == nil {
		t.Fatal("symlinked skill dir was skipped by discovery; a symlink-to-directory " +
			"must be stat'ed through before deciding it is not a dir")
	}
	if got.Content != body {
		t.Fatalf("content = %q, want %q", got.Content, body)
	}
	// The catalog entry must point at the search path, so a later install or edit
	// resolves the same file the user sees listed.
	if got.Source != filepath.Join(link, "SKILL.md") {
		t.Fatalf("Source = %q, want %q", got.Source, filepath.Join(link, "SKILL.md"))
	}
}
