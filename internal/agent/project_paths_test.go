package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/paths"
)

func TestProjectScopedPathsFollowSymlinkAlias(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	alias := filepath.Join(root, "alias")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, alias); err != nil {
		t.Skipf("symlink test unavailable: %v", err)
	}

	base, err := paths.GlobalDataDir()
	if err != nil {
		t.Fatal(err)
	}
	wantSlug := paths.ProjectSlug(real)
	if got := paths.ProjectSlug(alias); got != wantSlug {
		t.Fatalf("paths.ProjectSlug(%q) = %q, want %q", alias, got, wantSlug)
	}

	a := &Agent{workDir: alias}
	wantSnapshots := filepath.Join(base, "project", wantSlug, "snapshots")
	if got := a.projectSnapshotsDir(); got != wantSnapshots {
		t.Fatalf("projectSnapshotsDir() = %q, want %q", got, wantSnapshots)
	}

	wantCache := filepath.Join(base, "project", wantSlug, "md-summaries.json")
	if got := mdSummaryCachePath(alias); got != wantCache {
		t.Fatalf("mdSummaryCachePath(%q) = %q, want %q", alias, got, wantCache)
	}
}
