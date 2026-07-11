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
