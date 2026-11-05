package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/memory"
)

// TestLoadContextInjectsMemoryOfSessionRootNotProcessCwd pins that the memory
// fragment resolves from the session's project root. A web/desktop server hosts
// many projects and its process cwd is unrelated to any of them, so reading the
// cwd injected another project's memory into this session's system prompt.
func TestLoadContextInjectsMemoryOfSessionRootNotProcessCwd(t *testing.T) {
	setHomeTree(t, t.TempDir())

	projectA := t.TempDir()
	projectB := t.TempDir()
	t.Chdir(projectB)

	writeProjectMemory := func(root, body string) {
		t.Helper()
		p, err := memory.ResolvePaths(root)
		if err != nil {
			t.Fatalf("resolve memory paths for %s: %v", root, err)
		}
		if err := os.MkdirAll(filepath.Dir(p.Project), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p.Project, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeProjectMemory(projectA, "memory-marker-for-project-A")
	writeProjectMemory(projectB, "memory-marker-for-project-B")

	ctx := LoadContext(nil, true, false, "", projectA)

	if !strings.Contains(ctx, "memory-marker-for-project-A") {
		t.Fatalf("LoadContext with root A did not inject A's project memory:\n%s", ctx)
	}
	if strings.Contains(ctx, "memory-marker-for-project-B") {
		t.Fatalf("LoadContext with root A injected B's project memory from the process cwd:\n%s", ctx)
	}
}
