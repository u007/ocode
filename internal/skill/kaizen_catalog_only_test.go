package skill

import (
	"strings"
	"testing"
)

// Universal corpora are ADMITTED on model match alone, but only conduct,
// filetools and hallucination are force-injected. pdf and webforms are catalog-only
// (catalogOnlyStacks): their digest is parsed faithfully, but ForceInjected is
// false, so every injection consumer (prompt, TUI notice, web badge and row,
// budget rows) leaves them out of the base prompt while they stay advertised.
func TestKaizenCatalogOnlyStacksAdvertiseWithoutInjecting(t *testing.T) {
	const model = "some-provider/mdl"
	root := t.TempDir()
	cases := []struct {
		stack        string
		wantInjected bool
	}{
		{"conduct", true},
		{"filetools", true},
		{"hallucination", true},
		{"pdf", false},
		{"webforms", false},
	}
	for _, c := range cases {
		writeDigestKaizenSkill(t, root, c.stack+"-tuning-mdl", "mdl", c.stack, strings.ToUpper(c.stack)+"DIGEST")
	}

	block := KaizenDigestBlock(root, model)
	admitted := map[string]Skill{}
	for _, s := range KaizenSkillsForModel(root, model) {
		admitted[s.Name] = s
	}
	for _, c := range cases {
		name := c.stack + "-tuning-mdl"
		s, ok := admitted[name]
		if !ok {
			t.Fatalf("%s not admitted for its tuned model; admitted=%v", name, admitted)
		}
		if s.Digest == "" {
			t.Fatalf("%s lost its digest; Digest must stay faithful to the file", name)
		}
		if got := s.ForceInjected(); got != c.wantInjected {
			t.Fatalf("%s ForceInjected()=%v, want %v", name, got, c.wantInjected)
		}
		marker := "DIGEST-" + strings.ToUpper(c.stack) + "DIGEST"
		if got := strings.Contains(block, marker); got != c.wantInjected {
			t.Fatalf("%s injected=%v, want %v; block=\n%s", marker, got, c.wantInjected, block)
		}
	}

	cat := BuildCatalogForModel(root, model)
	for _, c := range cases {
		name := c.stack + "-tuning-mdl"
		if !strings.Contains(cat, name) {
			t.Fatalf("%s not advertised in the catalog; catalog=\n%s", name, cat)
		}
	}
}
