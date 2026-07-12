package skill

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repoRoot() string {
	_, f, _, _ := runtime.Caller(0) // internal/skill/kaizen_deliver_test.go
	return filepath.Clean(filepath.Join(filepath.Dir(f), "..", ".."))
}

func TestKaizenDelivery_hy3_conduct(t *testing.T) {
	const model = "novita-ai/tencent/hy3"
	root := repoRoot() // repo root → ProjectLocalSkillDirs includes skills/kaizen

	// 1. Load path: the kaizen skill must be discoverable at all.
	ks := KaizenSkillsForModel(root, model)
	var names []string
	for _, s := range ks {
		names = append(names, s.Name)
	}
	t.Logf("KaizenSkillsForModel(repoRoot, %s) = %v", model, names)
	found := false
	for _, n := range names {
		if n == "conduct-tuning-tencent-hy3" {
			found = true
		}
	}
	if !found {
		t.Fatalf("conduct tuning skill NOT admitted for %s; got %v", model, names)
	}

	// 2. Catalog: model-aware catalog advertises it (name visible).
	if !strings.Contains(BuildCatalogForModel(root, model), "conduct-tuning-tencent-hy3") {
		t.Fatal("BuildCatalogForModel does not advertise the tuning skill")
	}
	// 3. Ungated (excludeKaizen) set must NOT contain it.
	for _, s := range excludeKaizen(LoadSkillsForRoot(root)) {
		if s.Name == "conduct-tuning-tencent-hy3" {
			t.Fatal("excludeKaizen leaked a Kaizen skill")
		}
	}
	// 4. Wrong model does not admit it.
	for _, s := range KaizenSkillsForModel(root, "anthropic/claude-opus-4-8") {
		if s.Name == "conduct-tuning-tencent-hy3" {
			t.Fatal("wrong model admitted the hy3 skill")
		}
	}
}

// TestKaizenStackGating proves the admission predicate: a STACK-GATED tuning skill
// is admitted only when BOTH the model matches AND its stack is detected, while a
// UNIVERSAL (conduct) skill is admitted on model match alone. Uses synthetic
// skills so it doesn't depend on any particular stack-gated skill being embedded.
func TestKaizenStackGating(t *testing.T) {
	const model = "novita-ai/tencent/hy3"
	stackGated := Skill{Name: "x-tuning-tencent-hy3", TunedFor: "tencent/hy3", Stack: "golang"}
	universal := Skill{Name: "conduct-tuning-tencent-hy3", TunedFor: "tencent/hy3", Stack: "conduct"}

	// Stack-gated: admitted only when its stack is in the detected set.
	if !kaizenAdmitted(stackGated, model, []string{"golang"}) {
		t.Fatal("stack-gated skill should be admitted when its stack is detected")
	}
	if kaizenAdmitted(stackGated, model, nil) {
		t.Fatal("stack-gated skill must NOT be admitted when no stack is detected")
	}
	if kaizenAdmitted(stackGated, model, []string{"react"}) {
		t.Fatal("stack-gated skill must NOT be admitted for a different stack")
	}
	// Universal (conduct): admitted regardless of detected stacks.
	if !kaizenAdmitted(universal, model, nil) {
		t.Fatal("universal conduct skill must be admitted with no stack detected")
	}
	if !kaizenAdmitted(universal, model, []string{"react"}) {
		t.Fatal("universal conduct skill must be admitted regardless of stack")
	}
	// Wrong model → neither admitted, even with the stack present.
	if kaizenAdmitted(stackGated, "anthropic/claude-opus-4-8", []string{"golang"}) {
		t.Fatal("wrong model admitted a stack-gated skill")
	}
	if kaizenAdmitted(universal, "anthropic/claude-opus-4-8", nil) {
		t.Fatal("wrong model admitted a universal skill")
	}
}

func TestExtractDigest(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"present", "intro\n" + digestStart + "\n- rule one\n- rule two\n" + digestEnd + "\ntail", "- rule one\n- rule two"},
		{"absent", "no markers here at all", ""},
		{"unterminated", "x " + digestStart + " - rule with no end", ""},
		{"empty body", digestStart + "\n\n" + digestEnd, ""},
		{"trims surrounding whitespace", digestStart + "\n\n  hello  \n\n" + digestEnd, "hello"},
	}
	for _, c := range cases {
		if got := extractDigest(c.in); got != c.want {
			t.Errorf("%s: extractDigest = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestKaizenDigestBlock_hy3(t *testing.T) {
	root := repoRoot()

	// Matching model: the digest block is present and preserves the two
	// counterintuitive cruxes an overconfident model gets wrong. A lossy digest
	// would be a permanent regression, so assert them explicitly.
	block := KaizenDigestBlock(root, "novita-ai/tencent/hy3")
	if block == "" {
		t.Fatal("expected a digest block for tencent/hy3, got empty")
	}
	for _, crux := range []string{
		"Confidence is NOT an exemption",        // hallucination crux
		"objection is *scope*, not tree-wiping", // bare git reset crux
		"Never overwrite production/remote",     // safety limit 3
	} {
		if !strings.Contains(block, crux) {
			t.Errorf("digest block dropped a required crux: %q\n--- block ---\n%s", crux, block)
		}
	}
	// The parsed embedded skill must actually carry a Digest (guards the sync +
	// marker propagation, not just the renderer).
	for _, s := range KaizenSkillsForModel(root, "novita-ai/tencent/hy3") {
		if s.Name == "conduct-tuning-tencent-hy3" && s.Digest == "" {
			t.Fatal("embedded conduct skill parsed with an empty Digest (markers lost in sync?)")
		}
	}

	// Non-matching model: MUST be exactly "" so the cached base-prompt prefix is
	// byte-identical to a session with no tuning skill (empty-block footgun).
	if got := KaizenDigestBlock(root, "anthropic/claude-opus-4-8"); got != "" {
		t.Fatalf("non-matching model got a non-empty digest block: %q", got)
	}
}
