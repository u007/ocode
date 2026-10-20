package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/bundled"
	"github.com/u007/ocode/internal/config"
)

// disabledSkillEnv isolates HOME and the bundled dirs so only the fixtures a
// test writes are discoverable, and clears both skill caches (a stale cached
// listing or disabled set would make these assertions meaningless).
func disabledSkillEnv(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	prevSkills, prevPlugins := bundled.SkillsDir, bundled.PluginsDir
	bundled.SkillsDir, bundled.PluginsDir = "", ""
	t.Cleanup(func() { bundled.SkillsDir, bundled.PluginsDir = prevSkills, prevPlugins })
	InvalidateSkillCache()
	t.Cleanup(InvalidateSkillCache)
}

func writeDiscoverableSkill(t *testing.T, root, name string) {
	t.Helper()
	dir := filepath.Join(root, "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: " + name + " does things.\n---\n\n# " + name + "\nbody.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeDiscoverableKaizenSkill(t *testing.T, root, name string, tunedFor string) {
	t.Helper()
	dir := filepath.Join(root, "skills", "kaizen", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// No `stack:` line: an empty stack is universally active, so the fixture does
	// not depend on stack detection of the (empty) temp repo.
	body := "---\nname: " + name + "\ndescription: tuned.\ntuned_for: " + tunedFor + "\n---\n\n" +
		"# " + name + "\n" + digestStart + "\nHARD RULE " + name + "\n" + digestEnd + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// hasSkill reports whether a listing contains a skill by name. Named apart from
// the package's existing containsName (which takes []string) to avoid a collision.
func hasSkill(skills []Skill, name string) bool {
	return containsName(skillNames(skills), name)
}

// TestDisabledSkillIsNotDiscoverable is the core contract: a disabled skill
// vanishes from every advertisement surface while staying resolvable through the
// RAW loader (so an already-attached body is never retracted).
func TestDisabledSkillIsNotDiscoverable(t *testing.T) {
	disabledSkillEnv(t)
	root := t.TempDir()
	writeDiscoverableSkill(t, root, "alpha")
	writeDiscoverableSkill(t, root, "beta")

	// Sanity: both are advertised before the toggle.
	if cat := BuildCatalogForModel(root, "test/model"); !strings.Contains(cat, "alpha") || !strings.Contains(cat, "beta") {
		t.Fatalf("precondition failed, catalog:\n%s", cat)
	}

	if err := config.SaveSkillEnabled(root, "alpha", false); err != nil {
		t.Fatalf("disable alpha: %v", err)
	}
	InvalidateSkillCache()

	if !IsDisabledForRoot(root, "alpha") {
		t.Fatal("IsDisabledForRoot(alpha) = false, want true")
	}
	if IsDisabledForRoot(root, "beta") {
		t.Fatal("IsDisabledForRoot(beta) = true, want false")
	}

	// 1. Base-prompt catalog.
	cat := BuildCatalogForModel(root, "test/model")
	if strings.Contains(cat, "alpha") {
		t.Fatalf("disabled skill still advertised in the catalog:\n%s", cat)
	}
	if !strings.Contains(cat, "beta") {
		t.Fatalf("enabled skill missing from the catalog:\n%s", cat)
	}

	// 2. The model-aware listing (TUI slash commands, context budget).
	listed := LoadSkillsForModel(root, "test/model")
	if hasSkill(listed, "alpha") {
		t.Fatalf("disabled skill still in LoadSkillsForModel: %v", skillNames(listed))
	}
	if !hasSkill(listed, "beta") {
		t.Fatalf("enabled skill missing from LoadSkillsForModel: %v", skillNames(listed))
	}

	// 3. Raw listing stays complete: this is what the manage endpoint lists, so
	// a disabled skill can be switched back on.
	if !hasSkill(LoadSkillsForRoot(root), "alpha") {
		t.Fatal("raw listing lost the disabled skill; it could never be re-enabled")
	}

	// 4. The tool path refuses it...
	if s, err := LoadEnabledSkillForRoot("alpha", root); err != nil || s != nil {
		t.Fatalf("LoadEnabledSkillForRoot(alpha) = (%v, %v), want (nil, nil)", s, err)
	}
	// ...but the raw resolver still returns it, which is the path an in-flight
	// auto-injected skill uses to materialize its body.
	if s, err := LoadSkillForRoot("alpha", root); err != nil || s == nil {
		t.Fatalf("raw resolver must still find the already-attached skill: (%v, %v)", s, err)
	}
}

// TestDisabledKaizenSkillIsExcluded covers the model-specific tuning skills:
// they are gated by model+stack AND must additionally respect the user toggle.
func TestDisabledKaizenSkillIsExcluded(t *testing.T) {
	disabledSkillEnv(t)
	root := t.TempDir()
	writeDiscoverableKaizenSkill(t, root, "tuned-go", "test/model")

	if got := KaizenSkillsForModel(root, "test/model"); !hasSkill(got, "tuned-go") {
		t.Fatalf("precondition: tuned skill not admitted: %v", skillNames(got))
	}
	if !KaizenDigestAdmittedForModels(root, []string{"test/model"})["test/model"] {
		t.Fatal("precondition: digest not admitted")
	}
	if digest := KaizenDigestBlock(root, "test/model"); !strings.Contains(digest, "HARD RULE tuned-go") {
		t.Fatalf("precondition: digest block missing:\n%s", digest)
	}

	if err := config.SaveSkillEnabled(root, "tuned-go", false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	InvalidateSkillCache()

	if got := KaizenSkillsForModel(root, "test/model"); hasSkill(got, "tuned-go") {
		t.Fatalf("disabled Kaizen skill still admitted: %v", skillNames(got))
	}
	if KaizenDigestAdmittedForModels(root, []string{"test/model"})["test/model"] {
		t.Fatal("disabled Kaizen skill still badges its digest as admitted")
	}
	if digest := KaizenDigestBlock(root, "test/model"); strings.Contains(digest, "HARD RULE tuned-go") {
		t.Fatalf("disabled Kaizen digest still force-injected:\n%s", digest)
	}
}

// TestProjectEnableOverridesGlobalDisable is the layering end-to-end through the
// real loader: a project can rescue a globally disabled skill.
func TestProjectEnableOverridesGlobalDisable(t *testing.T) {
	disabledSkillEnv(t)
	root := t.TempDir()
	writeDiscoverableSkill(t, root, "alpha")

	if err := config.SaveSkillEnabled("", "alpha", false); err != nil {
		t.Fatalf("disable globally: %v", err)
	}
	InvalidateSkillCache()
	if !IsDisabledForRoot(root, "alpha") {
		t.Fatal("global disable did not reach the project")
	}

	if err := config.SaveSkillEnabled(root, "alpha", true); err != nil {
		t.Fatalf("enable in project: %v", err)
	}
	InvalidateSkillCache()
	if IsDisabledForRoot(root, "alpha") {
		t.Fatal("project enable did not override the global disable")
	}
	// A different project still sees the global disable.
	other := t.TempDir()
	writeDiscoverableSkill(t, other, "alpha")
	if !IsDisabledForRoot(other, "alpha") {
		t.Fatal("the project override leaked into another project")
	}
}

func TestExcludeDisabledForRootDoesNotMutateInput(t *testing.T) {
	disabledSkillEnv(t)
	root := t.TempDir()
	writeDiscoverableSkill(t, root, "alpha")
	writeDiscoverableSkill(t, root, "beta")
	if err := config.SaveSkillEnabled(root, "alpha", false); err != nil {
		t.Fatal(err)
	}
	InvalidateSkillCache()

	in := LoadSkillsForRoot(root)
	before := len(in)
	out := ExcludeDisabledForRoot(root, in)
	if len(in) != before {
		t.Fatalf("input slice was mutated: %d -> %d", before, len(in))
	}
	if hasSkill(out, "alpha") {
		t.Fatalf("ExcludeDisabledForRoot kept alpha: %v", skillNames(out))
	}
	if !hasSkill(out, "beta") {
		t.Fatalf("ExcludeDisabledForRoot dropped beta: %v", skillNames(out))
	}
}
