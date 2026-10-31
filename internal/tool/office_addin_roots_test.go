package tool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// NormalizeRootPath's one-level fallback is the other half of the Office add-in
// `Available` contract (internal/paths/office_addins.go): a preset is offered
// only when its PARENT exists, because this is how far resolution walks. If the
// fallback ever became resolveExistingAncestor (walks up arbitrarily far),
// presets would be disabled for paths the runtime would happily accept.
func TestNormalizeRootPathMissingLeafResolvesThroughParent(t *testing.T) {
	root := t.TempDir()
	leaf := filepath.Join(root, "a", "b", "wef")

	if _, ok := NormalizeRootPath(leaf); ok {
		t.Fatalf("a missing leaf with no existing parent must not resolve: %q", leaf)
	}
	if err := os.MkdirAll(filepath.Join(root, "a", "b"), 0o755); err != nil {
		t.Fatalf("mkdir parent: %v", err)
	}
	got, ok := NormalizeRootPath(leaf)
	if !ok {
		t.Fatalf("a missing leaf under an existing parent must resolve (Office creates wef on first sideload)")
	}
	if !strings.HasSuffix(got, string(filepath.Separator)+"wef") {
		t.Fatalf("normalized path %q does not end in %q", got, string(filepath.Separator)+"wef")
	}
}
