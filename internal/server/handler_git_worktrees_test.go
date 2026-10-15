package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// worktreeFixture builds a repo with one commit on main and returns its
// resolved path. EvalSymlinks matters on macOS, where t.TempDir sits under
// /var (a symlink to /private/var) and git reports the resolved path.
func worktreeFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	initGitRepo(t, dir)
	writeFile(t, filepath.Join(dir, "tracked.txt"), "one\n")
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-m", "init")
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolve fixture dir: %v", err)
	}
	return resolved
}

func worktreePost(t *testing.T, h *Handler, path string, body any, handler func(http.ResponseWriter, *http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest("POST", path, bytes.NewReader(raw)))
	return rec
}

func TestParseGitWorktreeList(t *testing.T) {
	out := strings.Join([]string{
		"worktree /repo",
		"HEAD aaa",
		"branch refs/heads/main",
		"",
		"worktree /repo/.worktrees/feat",
		"HEAD bbb",
		"branch refs/heads/feat/x",
		"locked testing",
		"",
		"worktree /repo/.worktrees/detached",
		"HEAD ccc",
		"detached",
		"prunable gitdir file points to non-existent location",
		"",
		"worktree /srv/bare.git",
		"bare",
		"",
	}, "\n")
	got := parseGitWorktreeList(out)
	if len(got) != 3 {
		t.Fatalf("want 3 entries (bare skipped), got %d: %+v", len(got), got)
	}
	if !got[0].IsMain || got[0].Path != "/repo" || got[0].Branch != "main" {
		t.Errorf("main entry wrong: %+v", got[0])
	}
	if got[1].IsMain || got[1].Branch != "feat/x" || !got[1].Locked {
		t.Errorf("linked branch entry wrong: %+v", got[1])
	}
	if !got[2].Detached || got[2].Branch != "" || !got[2].Prunable {
		t.Errorf("detached entry wrong: %+v", got[2])
	}
}

func TestParseGitWorktreeListMainIsFirstEvenWhenBare(t *testing.T) {
	// A bare main is skipped, so the first checkout that is listed is linked.
	// IsMain must stay tied to the first record git printed, not to the first
	// entry that survives the bare filter.
	out := "worktree /srv/bare.git\nbare\n\nworktree /work/a\nHEAD aaa\nbranch refs/heads/a\n"
	got := parseGitWorktreeList(out)
	if len(got) != 1 || got[0].IsMain {
		t.Fatalf("want one linked entry with IsMain=false, got %+v", got)
	}
}

func TestGitWorktreeCreateListRemove(t *testing.T) {
	dir := worktreeFixture(t)
	h := gitStashHandler(t, dir)
	want := filepath.Join(dir, ".worktrees", "feature", "x")

	rec := worktreePost(t, h, "/api/git/worktree", gitWorktreeCreateRequest{Branch: "feature/x"}, h.HandleGitWorktreeCreate)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d, body %s", rec.Code, rec.Body.String())
	}
	list := decodeBody[[]GitWorktree](t, rec)
	if len(list) != 2 {
		t.Fatalf("want main + linked, got %+v", list)
	}
	if !list[0].IsMain || !list[0].IsCurrent {
		t.Errorf("main should be first and current: %+v", list[0])
	}
	linked := list[1]
	if linked.Path != want || linked.Branch != "feature/x" || linked.IsMain {
		t.Errorf("linked entry wrong: %+v (want path %s)", linked, want)
	}
	if _, err := os.Stat(filepath.Join(want, "tracked.txt")); err != nil {
		t.Fatalf("worktree checkout missing: %v", err)
	}

	// The checkout dir must not show as untracked in the main repo.
	status := gitOutput(t, dir, "status", "--porcelain")
	if strings.TrimSpace(status) != "" {
		t.Errorf("main repo status should be clean, got %q", status)
	}

	rec = worktreePost(t, h, "/api/git/worktree", gitWorktreeCreateRequest{Branch: "feature/x"}, h.HandleGitWorktreeCreate)
	if rec.Code != http.StatusConflict {
		t.Errorf("duplicate create status = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}

	// Dirty worktree: git refuses, and the checkout must survive.
	writeFile(t, filepath.Join(want, "scratch.txt"), "wip\n")
	rec = worktreePost(t, h, "/api/git/worktree/remove", gitWorktreeRemoveRequest{Path: want}, h.HandleGitWorktreeRemove)
	if rec.Code != http.StatusConflict {
		t.Fatalf("dirty remove status = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(want, "scratch.txt")); err != nil {
		t.Fatalf("dirty worktree was touched: %v", err)
	}

	if err := os.Remove(filepath.Join(want, "scratch.txt")); err != nil {
		t.Fatalf("clean scratch file: %v", err)
	}
	rec = worktreePost(t, h, "/api/git/worktree/remove", gitWorktreeRemoveRequest{Path: want}, h.HandleGitWorktreeRemove)
	if rec.Code != http.StatusOK {
		t.Fatalf("remove status = %d, body %s", rec.Code, rec.Body.String())
	}
	if list := decodeBody[[]GitWorktree](t, rec); len(list) != 1 {
		t.Errorf("after remove want only main, got %+v", list)
	}
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Errorf("removed checkout still on disk: %v", err)
	}
}

func TestGitWorktreeCreateChecksOutExistingBranch(t *testing.T) {
	dir := worktreeFixture(t)
	run(t, dir, "git", "branch", "existing")
	h := gitStashHandler(t, dir)

	rec := worktreePost(t, h, "/api/git/worktree", gitWorktreeCreateRequest{Branch: "existing"}, h.HandleGitWorktreeCreate)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d, body %s", rec.Code, rec.Body.String())
	}
	list := decodeBody[[]GitWorktree](t, rec)
	if len(list) != 2 || list[1].Branch != "existing" {
		t.Fatalf("want linked worktree on existing branch, got %+v", list)
	}
}

func TestGitWorktreeCreateRejectsInvalidBranch(t *testing.T) {
	dir := worktreeFixture(t)
	h := gitStashHandler(t, dir)
	for _, branch := range []string{"", "-foo", "@{-1}", "a..b", "bad name"} {
		rec := worktreePost(t, h, "/api/git/worktree", gitWorktreeCreateRequest{Branch: branch}, h.HandleGitWorktreeCreate)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("branch %q: status = %d, want 400 (body %s)", branch, rec.Code, rec.Body.String())
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".worktrees")); !os.IsNotExist(err) {
		t.Errorf("rejected create must not create .worktrees/: %v", err)
	}
}

func TestGitWorktreeRemoveRefusesMainCurrentAndUnknown(t *testing.T) {
	dir := worktreeFixture(t)
	h := gitStashHandler(t, dir)
	linked := filepath.Join(dir, ".worktrees", "keep")
	if rec := worktreePost(t, h, "/api/git/worktree", gitWorktreeCreateRequest{Branch: "keep"}, h.HandleGitWorktreeCreate); rec.Code != http.StatusOK {
		t.Fatalf("setup create status = %d, body %s", rec.Code, rec.Body.String())
	}

	rec := worktreePost(t, h, "/api/git/worktree/remove", gitWorktreeRemoveRequest{Path: dir}, h.HandleGitWorktreeRemove)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("main remove status = %d, want 400", rec.Code)
	}
	rec = worktreePost(t, h, "/api/git/worktree/remove", gitWorktreeRemoveRequest{Path: filepath.Join(dir, "nope")}, h.HandleGitWorktreeRemove)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("unknown path status = %d, want 400", rec.Code)
	}

	// Opening the linked worktree as the project makes it the current
	// checkout, and the server must refuse to remove the open project.
	open := gitStashHandler(t, linked)
	rec = worktreePost(t, open, "/api/git/worktree/remove", gitWorktreeRemoveRequest{Path: linked}, open.HandleGitWorktreeRemove)
	if rec.Code != http.StatusConflict {
		t.Errorf("current worktree remove status = %d, want 409", rec.Code)
	}
}

func TestGitWorktreeRemoteHostIsNotImplemented(t *testing.T) {
	dir := worktreeFixture(t)
	h := gitStashHandler(t, dir)
	rec := httptest.NewRecorder()
	h.HandleGitWorktreeList(rec, httptest.NewRequest("GET", "/api/git/worktrees?host=devbox", nil))
	if rec.Code != http.StatusNotImplemented {
		t.Errorf("remote list status = %d, want 501", rec.Code)
	}
}

func TestEnsureWorktreesDirExcludedIsIdempotent(t *testing.T) {
	dir := worktreeFixture(t)
	for i := 0; i < 2; i++ {
		if err := ensureWorktreesDirExcluded(dir); err != nil {
			t.Fatalf("pass %d: %v", i, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, ".git", "info", "exclude"))
	if err != nil {
		t.Fatalf("read exclude: %v", err)
	}
	if n := strings.Count(string(data), ".worktrees/\n"); n != 1 {
		t.Errorf("want exactly one .worktrees/ entry, got %d in %q", n, data)
	}
}

func TestGitWorktreeMutationsRejectUnregisteredProject(t *testing.T) {
	dir := worktreeFixture(t)
	other := worktreeFixture(t)
	h := gitStashHandler(t, dir)
	query := "?project=" + url.QueryEscape(other)

	rec := worktreePost(t, h, "/api/git/worktree"+query, gitWorktreeCreateRequest{Branch: "sneaky"}, h.HandleGitWorktreeCreate)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("create in unregistered repo: status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(other, ".worktrees")); !os.IsNotExist(err) {
		t.Errorf("rejected create touched the unregistered repo: %v", err)
	}

	rec = worktreePost(t, h, "/api/git/worktree/remove"+query, gitWorktreeRemoveRequest{Path: other}, h.HandleGitWorktreeRemove)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("remove in unregistered repo: status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestGitWorktreeCreateRefusesWhenMainIsBare(t *testing.T) {
	// A bare clone with one linked checkout: the bare entry is skipped by the
	// parser, so the first listed checkout is not the main one. Create must
	// refuse rather than place .worktrees/ inside the linked checkout.
	src := worktreeFixture(t)
	bare := filepath.Join(t.TempDir(), "bare.git")
	run(t, filepath.Dir(bare), "git", "clone", "--bare", "-q", src, bare)
	linked := filepath.Join(t.TempDir(), "linked")
	run(t, bare, "git", "worktree", "add", "-q", linked, "main")
	h := gitStashHandler(t, linked)

	rec := worktreePost(t, h, "/api/git/worktree", gitWorktreeCreateRequest{Branch: "nested"}, h.HandleGitWorktreeCreate)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "bare") {
		t.Errorf("create with bare main: status = %d, want 400 naming the bare main (body %s)", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(linked, ".worktrees")); !os.IsNotExist(err) {
		t.Errorf("refused create still made .worktrees/ inside the linked checkout: %v", err)
	}
}
