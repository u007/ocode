package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/bundled"
)

func TestModelMatchesTuned(t *testing.T) {
	cases := []struct {
		name        string
		activeModel string
		tunedFor    string
		want        bool
	}{
		{"exact", "tencent/hy3", "tencent/hy3", true},
		{"provider prefix novita", "novita-ai/tencent/hy3", "tencent/hy3", true},
		{"provider prefix openrouter", "openrouter/tencent/hy3", "tencent/hy3", true},
		{"openrouter free variant", "openrouter/tencent/hy3:free", "tencent/hy3", true},
		{"openrouter nitro variant", "openrouter/tencent/hy3:nitro", "tencent/hy3", true},
		{"bare id with free variant", "tencent/hy3:free", "tencent/hy3", true},
		{"case-insensitive", "NOVITA/Tencent/HY3", "tencent/hy3", true},
		{"bare id no slash boundary", "hy3", "tencent/hy3", false},
		{"suffix without slash boundary", "xtencent/hy3", "tencent/hy3", false},
		{"different model", "anthropic/claude-opus-4-8", "tencent/hy3", false},
		{"different model free variant", "openrouter/other/model:free", "tencent/hy3", false},
		{"empty active", "", "tencent/hy3", false},
		{"empty tuned", "novita/tencent/hy3", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := modelMatchesTuned(tc.activeModel, tc.tunedFor); got != tc.want {
				t.Fatalf("modelMatchesTuned(%q, %q) = %v, want %v", tc.activeModel, tc.tunedFor, got, tc.want)
			}
		})
	}
}

// writeKaizenSkill writes a Kaizen skill at the REAL nested layout
// <root>/skills/kaizen/<name>/SKILL.md — mirroring what sync-derived-skills.py
// produces and what //go:embed ships. This guards the two-level-deep loader gap:
// loadSkillsFromPaths only descends one level, so the kaizen subtree must be its
// own search path or these skills silently never load.
func writeKaizenSkill(t *testing.T, root, name, tunedFor, stack string) {
	t.Helper()
	dir := filepath.Join(root, "skills", "kaizen", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\n" +
		"description: tuned skill for tests.\n" +
		"tuned_for: " + tunedFor + "\n" +
		"stack: " + stack + "\n---\n\n# " + name + "\nbody.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeNormalSkill(t *testing.T, root, name string) {
	t.Helper()
	dir := filepath.Join(root, "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: a normal skill.\n---\n\n# " + name + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// isolate points HOME at a throwaway dir and clears the bundled dir + skill
// cache so a test only sees skills it wrote itself.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	prev := bundled.SkillsDir
	bundled.SkillsDir = ""
	t.Cleanup(func() { bundled.SkillsDir = prev })
	InvalidateSkillCache()
	t.Cleanup(InvalidateSkillCache)
}

func TestLoadSkillsForModel_ConductUniversal(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	writeNormalSkill(t, root, "normal-one")
	writeKaizenSkill(t, root, "conduct-tuning-tencent-hy3", "tencent/hy3", "conduct")

	// Matching model + universal conduct stack -> Kaizen admitted.
	InvalidateSkillCache()
	got := LoadSkillsForModel(root, "novita-ai/tencent/hy3")
	if findSkill(got, "conduct-tuning-tencent-hy3") == nil {
		t.Fatalf("expected conduct Kaizen skill admitted for tencent/hy3, got %v", names(got))
	}
	if findSkill(got, "normal-one") == nil {
		t.Fatalf("normal skill must always be admitted, got %v", names(got))
	}

	// Non-matching model -> Kaizen excluded, normal still present.
	InvalidateSkillCache()
	got = LoadSkillsForModel(root, "anthropic/claude-opus-4-8")
	if findSkill(got, "conduct-tuning-tencent-hy3") != nil {
		t.Fatalf("conduct Kaizen skill must be excluded for a non-matching model, got %v", names(got))
	}
	if findSkill(got, "normal-one") == nil {
		t.Fatalf("normal skill must remain, got %v", names(got))
	}
}

func TestLoadSkillsForModel_StackGated(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	writeKaizenSkill(t, root, "react-tuning-x", "tencent/hy3", "react")

	// react NOT detected (no package.json) -> excluded even though model matches.
	InvalidateSkillCache()
	got := LoadSkillsForModel(root, "novita/tencent/hy3")
	if findSkill(got, "react-tuning-x") != nil {
		t.Fatalf("stack-gated skill must be excluded when its stack is not detected, got %v", names(got))
	}

	// Add a react marker so stackdetect.Detect(root) reports "react".
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"dependencies":{"react":"18"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// react detected AND model matches -> admitted.
	InvalidateSkillCache()
	got = LoadSkillsForModel(root, "novita/tencent/hy3")
	if findSkill(got, "react-tuning-x") == nil {
		t.Fatalf("stack-gated skill must be admitted when react detected and model matches, got %v", names(got))
	}

	// react detected but model does NOT match -> excluded.
	InvalidateSkillCache()
	got = LoadSkillsForModel(root, "anthropic/claude-opus-4-8")
	if findSkill(got, "react-tuning-x") != nil {
		t.Fatalf("stack-gated skill must be excluded when the model does not match, got %v", names(got))
	}
}

// Regression: the ungated BuildCatalog / LoadSkills path must NEVER surface a
// Kaizen skill, whatever the active model.
func TestBuildCatalogExcludesKaizen(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	t.Chdir(root)
	writeNormalSkill(t, root, "normal-one")
	writeKaizenSkill(t, root, "conduct-tuning-tencent-hy3", "tencent/hy3", "conduct")

	InvalidateSkillCache()
	if got := LoadSkills(); findSkill(got, "conduct-tuning-tencent-hy3") != nil {
		t.Fatalf("LoadSkills must not include Kaizen skills, got %v", names(got))
	}

	InvalidateSkillCache()
	cat := BuildCatalog()
	if strings.Contains(cat, "conduct-tuning-tencent-hy3") {
		t.Fatalf("ungated BuildCatalog leaked a Kaizen skill:\n%s", cat)
	}
	if !strings.Contains(cat, "normal-one") {
		t.Fatalf("BuildCatalog dropped the normal skill:\n%s", cat)
	}

	// The model-aware catalog SHOULD surface it for the matching model.
	InvalidateSkillCache()
	cat = BuildCatalogForModel(root, "novita/tencent/hy3")
	if !strings.Contains(cat, "conduct-tuning-tencent-hy3") {
		t.Fatalf("BuildCatalogForModel should include the matching Kaizen skill:\n%s", cat)
	}
}

// LoadSkill (explicit load-by-name for the skill tool) must still resolve a
// Kaizen skill even though it is absent from the ungated catalog.
func TestLoadSkillResolvesKaizenByName(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	t.Chdir(root)
	writeKaizenSkill(t, root, "conduct-tuning-tencent-hy3", "tencent/hy3", "conduct")

	InvalidateSkillCache()
	got, err := LoadSkill("conduct-tuning-tencent-hy3")
	if err != nil {
		t.Fatalf("LoadSkill error: %v", err)
	}
	if got == nil {
		t.Fatal("LoadSkill must resolve a Kaizen skill by exact name (explicit request)")
	}
}

// TestLoadSkillsForModel_BundledKaizen exercises the actual ship path: derived
// skills are embedded and extracted under bundled.SkillsDir/kaizen/<name>/.
// This confirms the `kaizen` subtree search path also covers the bundled root,
// not just project-local skills/.
func TestLoadSkillsForModel_BundledKaizen(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	bundledDir := t.TempDir()
	dir := filepath.Join(bundledDir, "kaizen", "conduct-tuning-tencent-hy3")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: conduct-tuning-tencent-hy3\ndescription: bundled tuned skill.\n" +
		"tuned_for: tencent/hy3\nstack: conduct\n---\n\n# conduct-tuning-tencent-hy3\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	prev := bundled.SkillsDir
	bundled.SkillsDir = bundledDir
	defer func() { bundled.SkillsDir = prev }()
	InvalidateSkillCache()
	defer InvalidateSkillCache()

	got := LoadSkillsForModel(t.TempDir(), "novita/tencent/hy3")
	if findSkill(got, "conduct-tuning-tencent-hy3") == nil {
		t.Fatalf("bundled Kaizen skill must load via bundled.SkillsDir/kaizen, got %v", names(got))
	}
}

func names(skills []Skill) []string {
	out := make([]string, 0, len(skills))
	for _, s := range skills {
		out = append(out, s.Name)
	}
	return out
}
