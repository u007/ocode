package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// listWorktrees is a GET of the worktree endpoint, the same route the web Git
// tab and the session chip use.
func listWorktrees(t *testing.T, h *Handler, query string) []GitWorktree {
	t.Helper()
	rec := httptest.NewRecorder()
	h.HandleGitWorktreeList(rec, httptest.NewRequest("GET", "/api/git/worktrees"+query, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body %s", rec.Code, rec.Body.String())
	}
	return decodeBody[[]GitWorktree](t, rec)
}

func createWorktree(t *testing.T, h *Handler, branch string) string {
	t.Helper()
	rec := worktreePost(t, h, "/api/git/worktree", gitWorktreeCreateRequest{Branch: branch}, h.HandleGitWorktreeCreate)
	if rec.Code != http.StatusOK {
		t.Fatalf("create %s status = %d, body %s", branch, rec.Code, rec.Body.String())
	}
	return filepath.Join(h.workDir, worktreesDirName, filepath.FromSlash(branch))
}

func findByPath(t *testing.T, list []GitWorktree, path string) GitWorktree {
	t.Helper()
	for _, wt := range list {
		if wt.Path == path {
			return wt
		}
	}
	t.Fatalf("worktree %s not in %+v", path, list)
	return GitWorktree{}
}

// TestGitWorktreeListDerivesState pins the derived fields: the UI's whole job
// is to stop guessing, so a linked checkout with work in it must report its
// own dirtiness and divergence, while the main checkout must NOT report a
// meaningless "ahead of its own branch".
func TestGitWorktreeListDerivesState(t *testing.T) {
	dir := worktreeFixture(t)
	h := gitStashHandler(t, dir)
	linkedPath := createWorktree(t, h, "feat-state")

	writeFile(t, filepath.Join(linkedPath, "tracked.txt"), "two\n")
	run(t, linkedPath, "git", "add", "-A")
	run(t, linkedPath, "git", "commit", "-m", "second commit")
	writeFile(t, filepath.Join(linkedPath, "scratch.txt"), "wip\n")

	list := listWorktrees(t, h, "")
	if len(list) != 2 {
		t.Fatalf("want main + linked, got %+v", list)
	}

	main, linked := list[0], list[1]
	if !main.IsMain || !linked.IsCurrent && linked.IsMain {
		t.Fatalf("ordering broken: %+v", list)
	}
	if !main.Exists || main.Dirty != 0 {
		t.Errorf("main should exist and be clean: %+v", main)
	}
	// "Ahead of the base branch" is meaningless for the main checkout.
	if main.Ahead != -1 || main.Behind != -1 || main.ContainedInBase != nil {
		t.Errorf("main must report unknown divergence, got ahead=%d behind=%d contained=%v",
			main.Ahead, main.Behind, main.ContainedInBase)
	}

	if !linked.Exists {
		t.Errorf("linked checkout should exist: %+v", linked)
	}
	if linked.Dirty != 1 {
		t.Errorf("linked dirty = %d, want 1 (scratch.txt)", linked.Dirty)
	}
	if linked.Ahead != 1 || linked.Behind != 0 {
		t.Errorf("linked ahead/behind = %d/%d, want 1/0", linked.Ahead, linked.Behind)
	}
	if linked.ContainedInBase == nil || *linked.ContainedInBase {
		t.Errorf("unmerged branch must be not-contained: %v", linked.ContainedInBase)
	}
	if !linked.BaseKnown || linked.BaseRef != "main" {
		t.Errorf("base = %q known=%v, want main/true", linked.BaseRef, linked.BaseKnown)
	}
	if linked.LastCommit == "" {
		t.Errorf("linked checkout should report a last commit date")
	}
	if linked.OwnerSessions == nil {
		t.Errorf("owner_sessions must be an empty slice, never nil")
	}
	if linked.OwnerSessionCount != 0 {
		t.Errorf("no session has used %s, got %d owners", linkedPath, linked.OwnerSessionCount)
	}
}

// TestGitWorktreeListMarksContainedBranch covers the "already merged" case:
// containment is reported as containment (an ancestor check), which is the
// only claim the server can honestly make.
func TestGitWorktreeListMarksContainedBranch(t *testing.T) {
	dir := worktreeFixture(t)
	h := gitStashHandler(t, dir)
	linkedPath := createWorktree(t, h, "feat-done")

	writeFile(t, filepath.Join(linkedPath, "tracked.txt"), "merged\n")
	run(t, linkedPath, "git", "commit", "-am", "work to merge")
	run(t, dir, "git", "merge", "--ff-only", "feat-done")

	linked := findByPath(t, listWorktrees(t, h, ""), linkedPath)
	if linked.ContainedInBase == nil || !*linked.ContainedInBase {
		t.Fatalf("merged branch should be contained: %v", linked.ContainedInBase)
	}
	if linked.Ahead != 0 || linked.Behind != 0 {
		t.Errorf("after fast-forward, ahead/behind = %d/%d, want 0/0", linked.Ahead, linked.Behind)
	}
	if !linked.BaseKnown || linked.BaseRef != "main" {
		t.Errorf("base = %q known=%v", linked.BaseRef, linked.BaseKnown)
	}
}

// TestGitWorktreeListReportsMissingCheckout pins the honest "missing" state:
// git keeps listing a checkout whose directory was deleted, and the old
// zero-values would have reported it as clean and present.
func TestGitWorktreeListReportsMissingCheckout(t *testing.T) {
	dir := worktreeFixture(t)
	h := gitStashHandler(t, dir)
	linkedPath := createWorktree(t, h, "feat-gone")

	if err := os.RemoveAll(linkedPath); err != nil {
		t.Fatalf("remove checkout: %v", err)
	}

	linked := findByPath(t, listWorktrees(t, h, ""), linkedPath)
	if linked.Exists {
		t.Errorf("deleted checkout must report exists=false: %+v", linked)
	}
	if linked.Dirty != -1 {
		t.Errorf("dirty = %d, want -1 (unknown, never 'clean')", linked.Dirty)
	}
	if linked.Ahead != -1 || linked.Behind != -1 {
		t.Errorf("ahead/behind = %d/%d, want -1/-1", linked.Ahead, linked.Behind)
	}
	if linked.ContainedInBase != nil {
		t.Errorf("containment must stay unknown, got %v", *linked.ContainedInBase)
	}
}

// TestGitWorktreeListPaginates pins ?limit/?offset, applied AFTER the existing
// main-first/path sort and BEFORE the per-checkout git probes.
func TestGitWorktreeListPaginates(t *testing.T) {
	dir := worktreeFixture(t)
	h := gitStashHandler(t, dir)
	first := createWorktree(t, h, "feat-a")
	_ = createWorktree(t, h, "feat-b")

	all := listWorktrees(t, h, "")
	if len(all) != 3 {
		t.Fatalf("want main + 2 linked, got %+v", all)
	}

	page := listWorktrees(t, h, "?limit=1&offset=1")
	if len(page) != 1 || page[0].Path != first {
		t.Fatalf("limit=1&offset=1 = %+v, want only %s", page, first)
	}

	// A malformed limit must not hide checkouts.
	if got := listWorktrees(t, h, "?limit=nope&offset=-3"); len(got) != 3 {
		t.Errorf("malformed pagination returned %d entries, want 3", len(got))
	}
	// Offset past the end yields an empty page, not an error.
	if got := listWorktrees(t, h, "?offset=99"); len(got) != 0 {
		t.Errorf("offset past end returned %d entries, want 0", len(got))
	}
}

// TestGitWorktreeListReportsOwnerSessions is the "which chat is here" signal:
// live sessions in this process count as owners of the checkout they are
// rooted at, and nobody else does.
func TestGitWorktreeListReportsOwnerSessions(t *testing.T) {
	dir := worktreeFixture(t)
	h := gitStashHandler(t, dir)
	linkedPath := createWorktree(t, h, "feat-owned")

	h.sessions = NewSessionManager(defaultSessionIdleTimeout, func() []string { return nil }, nil)
	h.sessions.Register("ses_owner", linkedPath)
	h.sessions.Register("ses_main", dir)

	list := listWorktrees(t, h, "")
	linked := findByPath(t, list, linkedPath)
	if linked.OwnerSessionCount != 1 || len(linked.OwnerSessions) != 1 {
		t.Fatalf("linked owners = %+v (count %d), want exactly ses_owner", linked.OwnerSessions, linked.OwnerSessionCount)
	}
	if linked.OwnerSessions[0].SessionID != "ses_owner" {
		t.Errorf("linked owner = %q, want ses_owner", linked.OwnerSessions[0].SessionID)
	}

	main := findByPath(t, list, list[0].Path)
	if main.IsMain && main.OwnerSessionCount != 1 {
		t.Errorf("main owners = %+v, want exactly ses_main", main.OwnerSessions)
	}
}
