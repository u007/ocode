package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/shell/sandbox"
	"github.com/u007/ocode/internal/tool"
)

// TestAllowedRootsClassified_CacheRoots pins the sandbox classification of the
// managed cache dirs: the truncated tool-results cache must be writable (a
// nested ocode or go test inside sandboxed bash writes there), while the
// cloned-repo cache stays read-only.
//
// Test contract (keep when editing): ACTUAL PATH — assert against the roots
// the production resolvers return, not hard-coded strings; HARMLESS — both
// roots are redirected to a temp dir via XDG_STATE_HOME so nothing under the
// user's real state dir is read or created; CROSS-PLATFORM — XDG_STATE_HOME
// is honored before any OS-specific branch, so the same test runs unchanged
// on darwin/linux/windows.
func TestAllowedRootsClassified_CacheRoots(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv("OPENCODE_REPO_CACHE", "")

	pm := NewPermissionManager()
	pm.SetWorkDir(t.TempDir())

	toolResults, ok := tool.ToolResultCacheRoot()
	if !ok {
		t.Fatal("ToolResultCacheRoot unresolvable")
	}
	repoCache, ok := tool.RepoCacheRoot()
	if !ok {
		t.Fatal("RepoCacheRoot unresolvable")
	}
	if filepath.Dir(toolResults) != filepath.Dir(repoCache) {
		t.Fatalf("cache roots must share the XDG state parent: %q vs %q", toolResults, repoCache)
	}

	got := map[string]bool{}
	for _, spec := range pm.AllowedRootsClassified() {
		got[spec.Path] = spec.Writable
	}
	w, found := got[toolResults]
	if !found {
		t.Fatalf("tool-results root %q missing from classified set", toolResults)
	}
	if !w {
		t.Errorf("tool-results root %q must be writable under sandbox", toolResults)
	}
	// The repo cache is never granted as writable. Under t.TempDir it sits
	// inside the always-writable temp root, so it is deliberately omitted
	// (see the ...DoNotExpand test below); elsewhere it appears read-only.
	if w, found := got[repoCache]; found && w {
		t.Errorf("repo cache root %q must never be writable under sandbox", repoCache)
	}
}

// TestAllowedRootsClassified_CacheRootsUnderWritableTempDoNotExpand guards a
// production failure mode: the read-only repo cache nested inside a writable
// root (here $TMPDIR, which is always writable under sandbox) used to carve
// that root into every child entry, producing a seatbelt profile so large
// that sandbox-exec aborted ("diff <= INSTR_JUMP_NE_MAX_LENGTH") and every
// sandboxed bash command failed. Same contract as above: ACTUAL PATH (asserts
// the resolved os.TempDir root survives intact in the OS root set), HARMLESS
// (XDG_STATE_HOME redirected to a temp dir, nothing created), CROSS-PLATFORM.
func TestAllowedRootsClassified_CacheRootsUnderWritableTempDoNotExpand(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("OPENCODE_REPO_CACHE", "")

	pm := NewPermissionManager()
	pm.SetWorkDir(t.TempDir())
	rs := sandbox.NewRootSet(pm.AllowedRootsClassified())

	tmp, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tmp = filepath.Clean(tmp)
	found := false
	for _, r := range rs.WritableRoots {
		if r == tmp {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("temp root %q was expanded away (repo cache carve-out leaked); %d writable roots", tmp, len(rs.WritableRoots))
	}
	repo, ok := tool.RepoCacheRoot()
	if !ok {
		t.Fatal("RepoCacheRoot unresolvable")
	}
	for _, spec := range pm.AllowedRootsClassified() {
		if spec.Path == repo && !spec.Writable {
			t.Fatalf("repo cache %q must not be a read-only carve-out inside writable %q", repo, tmp)
		}
	}
}
