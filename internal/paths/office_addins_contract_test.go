package paths

import (
	"os"
	"path/filepath"
	"testing"
)

// Available is a CROSS-PACKAGE contract, not a local fact: the handler relies
// on `tool.NormalizeRootPath` resolving a missing leaf through its immediate
// parent, and Available is exactly `parent exists`. Pin the half this package
// owns; internal/tool/office_addin_roots_test.go pins the other half, so the two
// cannot drift (a normalizeRootPath that walked up further would make Available
// narrower than reality and disable presets the runtime would accept).
func TestOfficeAddinAvailableRequiresExistingParent(t *testing.T) {
	root := t.TempDir()
	leaf := filepath.Join(root, "a", "b", "wef")

	p := newOfficeAddinPath("excel", "Excel", leaf, "")
	if p.Exists {
		t.Fatalf("missing leaf reported as existing: %q", leaf)
	}
	if p.Available {
		t.Fatalf("preset offered with no existing parent: %q", leaf)
	}

	if err := os.MkdirAll(filepath.Join(root, "a", "b"), 0o755); err != nil {
		t.Fatalf("mkdir parent: %v", err)
	}
	p = newOfficeAddinPath("excel", "Excel", leaf, "")
	if p.Exists {
		t.Fatalf("missing leaf reported as existing after parent create: %q", leaf)
	}
	if !p.Available {
		t.Fatalf("missing leaf under an existing parent must be grantable (Office creates wef on first sideload): %q", leaf)
	}
}
