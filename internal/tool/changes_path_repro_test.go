package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/changes"
	"github.com/u007/ocode/internal/snapshot"
)

// TestChangesTabReflectsFullPathWhenProcessCwdDiffersFromWorkDir reproduces
// the reported bug: when ocode's process working directory is NOT the project
// root (a common case — launched from a parent dir, a multi-project/desktop
// session, or after /cd), a write made with an ABSOLUTE (full) path inside the
// project was wrongly rejected by confinedPath (which confined against the
// process cwd) and therefore never reached the changes tab. The fix threads
// the agent's workDir through the tool context so confinedPath confines
// against the project root.
func TestChangesTabReflectsFullPathWhenProcessCwdDiffersFromWorkDir(t *testing.T) {
	workDir := t.TempDir() // the project root; intentionally NOT the process cwd
	absPath := filepath.Join(workDir, "full-path.md")

	// Process cwd stays as the test's package dir (NOT workDir), mimicking a
	// launch from outside the project. If confinedPath used os.Getwd() as the
	// root, absPath (which is outside the process cwd) would be rejected.
	store := snapshot.NewStore("main", filepath.Join(workDir, ".snapshots"))
	reg := changes.NewRegistry()
	if err := reg.AttachSnapshotStore("main", store); err != nil {
		t.Fatal(err)
	}

	ctx := snapshot.WithStore(context.Background(), store)
	ctx = snapshot.WithToolCallID(ctx, "tc1")
	ctx = WithWorkDir(ctx, workDir) // the fix: project root carried in context

	args, _ := json.Marshal(map[string]interface{}{"path": absPath, "content": "hello"})
	if _, err := (WriteTool{}).ExecuteCtx(ctx, args); err != nil {
		t.Fatalf("absolute-path write failed (would not reflect on changes tab): %v", err)
	}

	list := reg.List()
	if len(list) != 1 {
		t.Fatalf("expected exactly 1 changes-tab entry, got %d: %+v", len(list), list)
	}
	if list[0].OriginalPath != absPath {
		t.Errorf("changes-tab entry path = %q, want %q", list[0].OriginalPath, absPath)
	}
}

// TestConfinedPathFallsBackToProcessCwd preserves pre-fix behavior for
// non-agent callers (no workDir in context): it still confines against the
// process working directory.
func TestConfinedPathFallsBackToProcessCwd(t *testing.T) {
	origWd, _ := os.Getwd()
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWd) //nolint:errcheck

	if _, err := confinedPath(context.Background(), "rel.md"); err != nil {
		t.Fatalf("relative path should be allowed via process cwd fallback: %v", err)
	}
}
