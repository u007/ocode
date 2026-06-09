package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A directory target must return a listing (with an explicit "not a reason to
// refuse" note), not the opaque os.ReadFile "is a directory" error that weaker
// permission models misread as a validation failure and deny on.
func TestExecutePermissionReadFile_Directory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "viewport.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	out := executePermissionReadFile(dir, 0, 0)

	if strings.Contains(strings.ToLower(out), "error") {
		t.Fatalf("directory read returned an error-shaped result: %q", out)
	}
	if !strings.Contains(out, "is a directory") || !strings.Contains(out, "not a reason to refuse") {
		t.Fatalf("missing directory guidance note: %q", out)
	}
	if !strings.Contains(out, "viewport.go") {
		t.Fatalf("listing missing file entry: %q", out)
	}
	if !strings.Contains(out, "sub/") {
		t.Fatalf("listing missing dir entry with trailing slash: %q", out)
	}
}
