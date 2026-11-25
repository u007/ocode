package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/u007/ocode/internal/session"
)

// workspaceRootTestHandler builds a handler whose only saved roots are the
// given local project paths (the process cwd is cleared so the checkout this
// test binary runs from is not an accidental trust root).
func workspaceRootTestHandler(t *testing.T, saved ...string) *Handler {
	t.Helper()
	resetWorktreeParentCache()
	resetWorkspaceRootCache()
	t.Cleanup(func() {
		resetWorktreeParentCache()
		resetWorkspaceRootCache()
	})
	h := testProjectHandler(t)
	h.workDir = ""
	for _, root := range saved {
		if err := h.projects.Add(root); err != nil {
			t.Fatalf("add saved project %s: %v", root, err)
		}
	}
	return h
}

// linkedWorktreeFixture returns a main checkout and an external linked worktree
// of it, both symlink-resolved (macOS t.TempDir sits under /var).
func linkedWorktreeFixture(t *testing.T) (main, linked string) {
	t.Helper()
	main = worktreeFixture(t)
	linked = filepath.Join(t.TempDir(), "linked")
	run(t, main, "git", "worktree", "add", "-b", "feat", linked)
	resolved, err := filepath.EvalSymlinks(linked)
	if err != nil {
		t.Fatalf("resolve linked worktree: %v", err)
	}
	return main, resolved
}

func TestResolveWorkspaceRootSavedRootAndSubpath(t *testing.T) {
	main := worktreeFixture(t)
	h := workspaceRootTestHandler(t, main)

	root, saved, ok := h.resolveWorkspaceRoot(main)
	if !ok || !samePath(root, main) || !samePath(saved, main) {
		t.Fatalf("saved root = (%q,%q,%v), want main", root, saved, ok)
	}

	sub := filepath.Join(main, "nested", "dir")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	root, saved, ok = h.resolveWorkspaceRoot(sub)
	if !ok || !samePath(root, main) || !samePath(saved, main) {
		t.Fatalf("saved-root subpath = (%q,%q,%v), want main", root, saved, ok)
	}
}

func TestResolveWorkspaceRootLinkedWorktree(t *testing.T) {
	main, linked := linkedWorktreeFixture(t)
	h := workspaceRootTestHandler(t, main)

	root, saved, ok := h.resolveWorkspaceRoot(linked)
	if !ok || !samePath(root, linked) || !samePath(saved, main) {
		t.Fatalf("linked worktree = (%q,%q,%v), want (%q,%q)", root, saved, ok, linked, main)
	}

	// A path inside the worktree resolves to the worktree, not the main
	// checkout (the file tree expands nested directories this way).
	sub := filepath.Join(linked, "nested")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	root, saved, ok = h.resolveWorkspaceRoot(sub)
	if !ok || !samePath(root, linked) || !samePath(saved, main) {
		t.Fatalf("worktree subpath = (%q,%q,%v), want worktree root", root, saved, ok)
	}
}

// ocode's own worktrees live under .worktrees/, i.e. INSIDE the saved main
// checkout. They must keep their own identity rather than collapsing to the
// containing saved root.
func TestResolveWorkspaceRootNestedWorktree(t *testing.T) {
	main := worktreeFixture(t)
	nested := filepath.Join(main, ".worktrees", "feature")
	run(t, main, "git", "worktree", "add", "-b", "feature", nested)
	resolved, err := filepath.EvalSymlinks(nested)
	if err != nil {
		t.Fatalf("resolve nested worktree: %v", err)
	}
	h := workspaceRootTestHandler(t, main)

	root, saved, ok := h.resolveWorkspaceRoot(resolved)
	if !ok || !samePath(root, resolved) || !samePath(saved, main) {
		t.Fatalf("nested worktree = (%q,%q,%v), want (%q,%q)", root, saved, ok, resolved, main)
	}
}

func TestResolveWorkspaceRootRejectsUnrelatedRepo(t *testing.T) {
	savedRoot := worktreeFixture(t)
	unrelated := worktreeFixture(t)
	h := workspaceRootTestHandler(t, savedRoot)

	if root, saved, ok := h.resolveWorkspaceRoot(unrelated); ok {
		t.Fatalf("unrelated repo accepted as (%q,%q)", root, saved)
	}
	// Its subdirectory is just as unrelated.
	sub := filepath.Join(unrelated, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if root, saved, ok := h.resolveWorkspaceRoot(sub); ok {
		t.Fatalf("unrelated repo subpath accepted as (%q,%q)", root, saved)
	}
}

func TestResolveWorkspaceRootRejectsMissingPath(t *testing.T) {
	main := worktreeFixture(t)
	h := workspaceRootTestHandler(t, main)

	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if root, saved, ok := h.resolveWorkspaceRoot(missing); ok {
		t.Fatalf("missing path accepted as (%q,%q)", root, saved)
	}
}

func TestResolveWorkspaceRootSymlinkSpelling(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	main, linked := linkedWorktreeFixture(t)
	h := workspaceRootTestHandler(t, main)

	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(linked, alias); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	root, saved, ok := h.resolveWorkspaceRoot(alias)
	if !ok || !samePath(root, linked) || !samePath(saved, main) {
		t.Fatalf("symlinked worktree = (%q,%q,%v), want (%q,%q)", root, saved, ok, linked, main)
	}

	// A symlinked spelling of the saved root itself resolves to that root.
	mainAlias := filepath.Join(t.TempDir(), "main-alias")
	if err := os.Symlink(main, mainAlias); err != nil {
		t.Fatalf("symlink main: %v", err)
	}
	root, saved, ok = h.resolveWorkspaceRoot(mainAlias)
	if !ok || !samePath(root, main) || !samePath(saved, main) {
		t.Fatalf("symlinked saved root = (%q,%q,%v), want main", root, saved, ok)
	}
}

func TestResolveWorkspaceRootRejectsFailedProbe(t *testing.T) {
	main := worktreeFixture(t)
	h := workspaceRootTestHandler(t, main)

	candidate := filepath.Join(t.TempDir(), "candidate")
	if err := os.MkdirAll(candidate, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(candidate, ".git"), "gitdir: "+filepath.Join(main, ".git", "worktrees", "gone")+"\n")
	stubWorktreeGit(t, "#!/bin/sh\necho 'fatal: not a git repository' >&2\nexit 128\n")
	resetWorkspaceRootCache()

	if root, saved, ok := h.resolveWorkspaceRoot(candidate); ok {
		t.Fatalf("failed probe accepted as (%q,%q)", root, saved)
	}
}

func TestResolveWorkspaceRootRejectsForgedGitFile(t *testing.T) {
	savedRoot := worktreeFixture(t)
	_, otherLinked := linkedWorktreeFixture(t)
	h := workspaceRootTestHandler(t, savedRoot)

	forged := filepath.Join(t.TempDir(), "forged-worktree")
	if err := os.MkdirAll(forged, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(otherLinked, ".git"))
	if err != nil {
		t.Fatalf("read other worktree .git: %v", err)
	}
	writeFile(t, filepath.Join(forged, ".git"), string(data))

	if root, saved, ok := h.resolveWorkspaceRoot(forged); ok {
		t.Fatalf("forged .git accepted as (%q,%q)", root, saved)
	}
	// The containment check itself rejects a `.git` file aimed outside the
	// saved repository's worktrees directory.
	if worktreeGitFileWithin(forged, savedRoot) {
		t.Fatal("forged .git file counted as inside the saved repository")
	}
	if worktreeGitFileWithin(otherLinked, savedRoot) {
		t.Fatal("another repo's worktree counted as inside the saved repository")
	}
}

func TestWorktreeGitFileWithinAcceptsRealWorktree(t *testing.T) {
	main, linked := linkedWorktreeFixture(t)
	if !worktreeGitFileWithin(linked, main) {
		t.Fatalf("%s should point inside %s/.git/worktrees", linked, main)
	}
}

// Git reads and mutations must accept a validated linked worktree but keep
// rejecting an unrelated repository.
func TestWorkspaceRootGitProjectResolution(t *testing.T) {
	main, linked := linkedWorktreeFixture(t)
	h := gitStashHandler(t, main)

	req := func(p string) *http.Request {
		return httptest.NewRequest(http.MethodGet, "/api/git/status?project="+url.QueryEscape(p), nil)
	}
	dir, ok := h.gitProjectDir(req(linked))
	if !ok || !samePath(dir, linked) {
		t.Fatalf("gitProjectDir(linked) = (%q,%v), want %q", dir, ok, linked)
	}
	dir, ok = h.mutationProjectDir(req(linked))
	if !ok || !samePath(dir, linked) {
		t.Fatalf("mutationProjectDir(linked) = (%q,%v), want %q", dir, ok, linked)
	}

	unrelated := worktreeFixture(t)
	if _, ok := h.gitProjectDir(req(unrelated)); ok {
		t.Fatal("gitProjectDir accepted an unrelated repository")
	}
	if _, ok := h.mutationProjectDir(req(unrelated)); ok {
		t.Fatal("mutationProjectDir accepted an unrelated repository")
	}
}

// Project session rows must carry the checkout root, and a worktree's sessions
// (keyed by that checkout's own slug) must list there, not under the main
// checkout.
func TestListProjectSessionsWorktreeRootAndSlug(t *testing.T) {
	main, linked := linkedWorktreeFixture(t)
	h := workspaceRootTestHandler(t, main)

	mainID := session.NewSessionID()
	if err := session.SaveForDir(main, mainID, "main session", nil, nil); err != nil {
		t.Fatalf("save main session: %v", err)
	}
	linkedID := session.NewSessionID()
	if err := session.SaveForDir(linked, linkedID, "linked session", nil, nil); err != nil {
		t.Fatalf("save linked session: %v", err)
	}

	list := func(p string) []SessionInfo {
		t.Helper()
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/projects/sessions?path="+url.QueryEscape(p), nil)
		h.HandleListProjectSessions(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("list %s status=%d body=%s", p, rr.Code, rr.Body.String())
		}
		return decodeBody[[]SessionInfo](t, rr)
	}

	rows := list(linked)
	if len(rows) != 1 || rows[0].ID != linkedID {
		t.Fatalf("linked listing = %+v, want only %s (main slug must not leak in)", rows, linkedID)
	}
	if !samePath(rows[0].ProjectPath, linked) {
		t.Fatalf("linked row project_path = %q, want %q", rows[0].ProjectPath, linked)
	}

	rows = list(main)
	if len(rows) != 1 || rows[0].ID != mainID {
		t.Fatalf("main listing = %+v, want only %s", rows, mainID)
	}
	if !samePath(rows[0].ProjectPath, main) {
		t.Fatalf("main row project_path = %q, want %q", rows[0].ProjectPath, main)
	}
}
