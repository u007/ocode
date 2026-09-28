package skill

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/bundled"
)

// isolateBundledSkills points bundled.SkillsDir at an empty directory for the
// duration of the test so bundled skills cannot mask a search-path regression.
func isolateBundledSkills(t *testing.T) {
	t.Helper()
	prev := bundled.SkillsDir
	bundled.SkillsDir = t.TempDir()
	t.Cleanup(func() { bundled.SkillsDir = prev })
}

func writeHomeSkill(t *testing.T, home, dir, name, description string) {
	t.Helper()
	root := filepath.Join(home, dir, name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", root, err)
	}
	content := "---\nname: " + name + "\ndescription: " + description +
		"\nwhen_to_use: Whenever this skill is relevant.\n---\n\n# " + name + "\nBody.\n"
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
}

// TestSkillSearchPathsIncludeHomeClaudeSkills guards the user-global Claude
// Code skills directory. Claude Code installs user-wide skills into
// ~/.claude/skills; ocode scanned only ~/.config/opencode/skills and
// ~/.agents/skills at home level, so every skill living solely in
// ~/.claude/skills was invisible. Project-local <root>/.claude/skills was
// already supported, which made the omission an asymmetry rather than a
// deliberate policy.
func TestSkillSearchPathsIncludeHomeClaudeSkills(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := t.TempDir()
	t.Chdir(project)

	paths := SkillSearchPathsForRoot(project)

	for _, want := range []string{
		filepath.Join(home, ".claude", "skills"),
		// The kaizen/ subtree is appended per base root, so the new root must
		// get one too or tuned skills under ~/.claude/skills/kaizen never load.
		filepath.Join(home, ".claude", "skills", "kaizen"),
	} {
		if !slices.Contains(paths, want) {
			t.Errorf("SkillSearchPathsForRoot missing %q\ngot: %v", want, paths)
		}
	}
}

// TestSkillSearchPathPrecedence pins the ordering contract: the ocode-native
// global dir stays first (it is globalSkillsDir, the installer's write target),
// then ~/.agents/skills, then the Claude Code user-global dir, then project
// roots. loadSkillsFromPaths is first-wins-on-directory-name, so a reordering
// here silently changes which copy of a duplicated skill is served.
func TestSkillSearchPathPrecedence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := t.TempDir()
	t.Chdir(project)
	isolateBundledSkills(t)

	paths := SkillSearchPathsForRoot(project)
	idx := func(p string) int { return slices.Index(paths, p) }
	for _, p := range []string{
		filepath.Join(home, ".config", "opencode", "skills"),
		filepath.Join(home, ".agents", "skills"),
		filepath.Join(home, ".claude", "skills"),
		filepath.Join(project, ".claude", "skills"),
	} {
		if idx(p) < 0 {
			t.Fatalf("%q not in search paths: %v", p, paths)
		}
	}
	// Explicit relative order: config/opencode (installer target) < .agents <
	// .claude < project roots. First-wins-on-directory-name means this order
	// decides which copy of a duplicated skill is served.
	if !(idx(filepath.Join(home, ".config", "opencode", "skills")) <
		idx(filepath.Join(home, ".agents", "skills")) &&
		idx(filepath.Join(home, ".agents", "skills")) <
			idx(filepath.Join(home, ".claude", "skills")) &&
		idx(filepath.Join(home, ".claude", "skills")) <
			idx(filepath.Join(project, ".claude", "skills"))) {
		t.Fatalf("unexpected home/project ordering: %v", paths)
	}
}

// TestLoadSkillsReadsHomeClaudeSkills is the behavioural proof: a skill that
// exists only in ~/.claude/skills must appear in the loaded set.
func TestLoadSkillsReadsHomeClaudeSkills(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := t.TempDir()
	t.Chdir(project)
	isolateBundledSkills(t)

	writeHomeSkill(t, home, filepath.Join(".claude", "skills"), "productize",
		"Productise a deliverable before shipping it.")

	skills := LoadSkillsForRoot(project)
	got := findSkill(skills, "productize")
	if got == nil {
		t.Fatalf("skill under ~/.claude/skills not discovered; loaded: %v", skillNames(skills))
	}
	if want := "Productise a deliverable before shipping it."; got.Description != want {
		t.Fatalf("description = %q, want %q", got.Description, want)
	}
	if want := filepath.Join(home, ".claude", "skills", "productize", "SKILL.md"); got.Source != want {
		t.Fatalf("source = %q, want %q", got.Source, want)
	}
}

// TestLoadSkillsFollowsHomeClaudeSkillsSymlink mirrors the real layout, where
// ~/.claude/skills entries are frequently symlinks into a shared ~/.agents
// tree or a per-project checkout. loadSkillsFromPaths already stats through
// symlinks; this test proves that holds for the new root too.
func TestLoadSkillsFollowsHomeClaudeSkillsSymlink(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := t.TempDir()
	t.Chdir(project)
	isolateBundledSkills(t)

	real := filepath.Join(t.TempDir(), "shared-skills", "git-commit-push")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(real, "SKILL.md"),
		[]byte("---\nname: git-commit-push\ndescription: Commit and push with updated docs.\n---\n\n# git-commit-push\n"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	linkDir := filepath.Join(home, ".claude", "skills")
	if err := os.MkdirAll(linkDir, 0o755); err != nil {
		t.Fatalf("mkdir link dir: %v", err)
	}
	if err := os.Symlink(real, filepath.Join(linkDir, "git-commit-push")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	got := findSkill(LoadSkillsForRoot(project), "git-commit-push")
	if got == nil {
		t.Fatal("symlinked skill under ~/.claude/skills not discovered")
	}
	if !strings.Contains(got.Description, "Commit and push") {
		t.Fatalf("unexpected description %q", got.Description)
	}
}

// TestBuildCatalogAdvertisesHomeClaudeSkills covers the other consumer: the
// catalog rendered into the system prompt, which is what actually makes a
// skill loadable by the model.
func TestBuildCatalogAdvertisesHomeClaudeSkills(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := t.TempDir()
	t.Chdir(project)
	isolateBundledSkills(t)

	writeHomeSkill(t, home, filepath.Join(".claude", "skills"), "laya-integration",
		"Integrate with the Laya platform.")

	catalog := BuildCatalog()
	if !strings.Contains(catalog, "laya-integration") {
		t.Fatalf("catalog missing skill from ~/.claude/skills:\n%s", catalog)
	}
}

// TestHomeClaudeSkillsDoNotDisplaceNativeGlobalSkills guards precedence end to
// end: a same-named skill in ~/.config/opencode/skills (the installer's
// upgrade target) must keep winning over ~/.claude/skills.
func TestHomeClaudeSkillsDoNotDisplaceNativeGlobalSkills(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := t.TempDir()
	t.Chdir(project)
	isolateBundledSkills(t)

	writeHomeSkill(t, home, filepath.Join(".config", "opencode", "skills"), "docx",
		"NATIVE copy from the installer target.")
	writeHomeSkill(t, home, filepath.Join(".claude", "skills"), "docx",
		"CLAUDE copy from the Claude Code user dir.")

	got := findSkill(LoadSkillsForRoot(project), "docx")
	if got == nil {
		t.Fatal("docx not discovered")
	}
	if !strings.Contains(got.Description, "NATIVE") {
		t.Fatalf("~/.config/opencode/skills must win; got %q from %s", got.Description, got.Source)
	}
}

func skillNames(skills []Skill) []string {
	out := make([]string, 0, len(skills))
	for _, s := range skills {
		out = append(out, s.Name)
	}
	return out
}
