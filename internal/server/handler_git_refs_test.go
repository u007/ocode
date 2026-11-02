package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// refActionPost posts one ref action against the handler's work directory.
func refActionPost(t *testing.T, h *Handler, query string, body any) *httptest.ResponseRecorder {
	t.Helper()
	return worktreePost(t, h, "/api/git/ref-action"+query, body, h.HandleGitRefAction)
}

func currentBranch(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(runOut(t, dir, "git", "rev-parse", "--abbrev-ref", "HEAD"))
}

func TestGitRefActionRejectsBadRequests(t *testing.T) {
	dir := worktreeFixture(t)
	hash := headHash(t, dir)
	run(t, dir, "git", "branch", "cafe")
	h := NewHandler()
	h.SetWorkDir(dir)

	cases := []struct {
		name     string
		body     any
		wantCode int
		wantText string
	}{
		{"unknown action", map[string]string{"action": "push", "target": hash}, http.StatusBadRequest, "push is not a supported ref action"},
		{"dash-prefixed target", map[string]string{"action": "checkout", "target": "-x"}, http.StatusBadRequest, "target must be"},
		{"empty target", map[string]string{"action": "checkout", "target": ""}, http.StatusBadRequest, "target must be"},
		{"bad branch name", map[string]string{"action": "branch", "target": hash, "name": "bad..name"}, http.StatusBadRequest, "invalid branch name"},
		{"dash-prefixed name", map[string]string{"action": "tag", "target": hash, "name": "-x"}, http.StatusBadRequest, "invalid name"},
		{"missing name", map[string]string{"action": "tag", "target": hash}, http.StatusBadRequest, "name is required"},
		{"bad tag name", map[string]string{"action": "tag", "target": hash, "name": "a b"}, http.StatusBadRequest, "invalid tag name"},
		{"name on an action that takes none", map[string]string{"action": "checkout", "target": hash, "name": "x"}, http.StatusBadRequest, "does not take a name"},
		{"branch-delete with a hash", map[string]string{"action": "branch-delete", "target": hash}, http.StatusBadRequest, "needs a branch name"},
		{"cherry-pick with a branch name", map[string]string{"action": "cherry-pick", "target": "main"}, http.StatusBadRequest, "needs a commit hash"},
		{"unknown hash", map[string]any{"action": "reset-hard", "target": "deadbeef", "confirm": true}, http.StatusBadRequest, "does not resolve to a commit"},
		{"reset-hard without confirm", map[string]string{"action": "reset-hard", "target": hash}, http.StatusBadRequest, "confirm:true"},
		{"reset-hard with confirm false", map[string]any{"action": "reset-hard", "target": hash, "confirm": false}, http.StatusBadRequest, "confirm:true"},
		{"reset-hard with string confirm", map[string]any{"action": "reset-hard", "target": hash, "confirm": "true"}, http.StatusBadRequest, "invalid request body"},
		{"cherry-pick with a hex-looking branch name", map[string]string{"action": "cherry-pick", "target": "cafe"}, http.StatusBadRequest, "needs a commit hash"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := refActionPost(t, h, "", tc.body)
			if rec.Code != tc.wantCode {
				t.Fatalf("status %d, want %d: %s", rec.Code, tc.wantCode, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.wantText) {
				t.Errorf("body %q does not contain %q", rec.Body.String(), tc.wantText)
			}
		})
	}
}

func TestGitRefActionRefusesFileNameTarget(t *testing.T) {
	dir := worktreeFixture(t)
	writeFile(t, filepath.Join(dir, "tracked.txt"), "local edit\n")
	h := NewHandler()
	h.SetWorkDir(dir)

	rec := refActionPost(t, h, "", map[string]string{"action": "checkout", "target": "tracked.txt"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", rec.Code, rec.Body.String())
	}
	// The file must not have been restored from the index over the edit.
	content, err := os.ReadFile(filepath.Join(dir, "tracked.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "local edit\n" {
		t.Errorf("tracked.txt = %q, want the local edit kept", content)
	}
}

func TestGitRefActionBranchAndTag(t *testing.T) {
	dir := worktreeFixture(t)
	hash := headHash(t, dir)
	h := NewHandler()
	h.SetWorkDir(dir)

	if rec := refActionPost(t, h, "", map[string]string{"action": "branch", "target": hash, "name": "newbr"}); rec.Code != http.StatusOK {
		t.Fatalf("branch: status %d: %s", rec.Code, rec.Body.String())
	}
	if !gitRefResolves(dir, "refs/heads/newbr") {
		t.Error("branch action did not create refs/heads/newbr")
	}
	if currentBranch(t, dir) != "main" {
		t.Error("branch action must not switch HEAD")
	}

	if rec := refActionPost(t, h, "", map[string]string{"action": "tag", "target": hash, "name": "v9"}); rec.Code != http.StatusOK {
		t.Fatalf("tag: status %d: %s", rec.Code, rec.Body.String())
	}
	if !gitRefResolves(dir, "refs/tags/v9") {
		t.Error("tag action did not create refs/tags/v9")
	}
}

func TestGitRefActionBranchCheckoutSwitchesHEAD(t *testing.T) {
	dir := worktreeFixture(t)
	hash := headHash(t, dir)
	h := NewHandler()
	h.SetWorkDir(dir)

	rec := refActionPost(t, h, "", map[string]string{"action": "branch-checkout", "target": hash, "name": "sw"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if got := currentBranch(t, dir); got != "sw" {
		t.Errorf("HEAD branch = %q, want sw", got)
	}
	var res GitRefActionResult
	if err := json.NewDecoder(rec.Body).Decode(&res); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if res.Workspace.Status.Branch != "sw" {
		t.Errorf("workspace branch = %q, want sw", res.Workspace.Status.Branch)
	}
}

func TestGitRefActionCheckoutRemoteTrackingUsesTrack(t *testing.T) {
	dir := worktreeFixture(t)
	origin := t.TempDir()
	run(t, origin, "git", "init", "--bare", "-b", "main")
	run(t, dir, "git", "remote", "add", "origin", origin)
	run(t, dir, "git", "push", "origin", "main:refs/heads/feat")
	run(t, dir, "git", "fetch", "origin")
	if gitRefResolves(dir, "refs/heads/feat") {
		t.Fatal("fixture precondition: local feat must not exist yet")
	}

	h := NewHandler()
	h.SetWorkDir(dir)
	rec := refActionPost(t, h, "", map[string]string{"action": "checkout", "target": "origin/feat"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if got := currentBranch(t, dir); got != "feat" {
		t.Errorf("HEAD branch = %q, want feat (a --track checkout, not a detached HEAD)", got)
	}
	if upstream := strings.TrimSpace(runOut(t, dir, "git", "config", "branch.feat.remote")); upstream != "origin" {
		t.Errorf("branch.feat.remote = %q, want origin", upstream)
	}
}

func TestGitRefActionResetHardMovesHEAD(t *testing.T) {
	dir := worktreeFixture(t)
	first := headHash(t, dir)
	writeFile(t, filepath.Join(dir, "tracked.txt"), "two\n")
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-m", "second")

	h := NewHandler()
	h.SetWorkDir(dir)
	rec := refActionPost(t, h, "", map[string]any{"action": "reset-hard", "target": first, "confirm": true})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if got := headHash(t, dir); got != first {
		t.Errorf("HEAD = %s, want %s", got, first)
	}
	if content := runOut(t, dir, "cat", "tracked.txt"); content != "one\n" {
		t.Errorf("tracked.txt = %q, want the reset content", content)
	}
}

func TestGitRefActionCherryPickApplies(t *testing.T) {
	dir := worktreeFixture(t)
	run(t, dir, "git", "checkout", "-b", "feat")
	writeFile(t, filepath.Join(dir, "new.txt"), "new\n")
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-m", "add new")
	picked := headHash(t, dir)
	run(t, dir, "git", "checkout", "main")

	h := NewHandler()
	h.SetWorkDir(dir)
	rec := refActionPost(t, h, "", map[string]string{"action": "cherry-pick", "target": picked})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if content := runOut(t, dir, "cat", "new.txt"); content != "new\n" {
		t.Errorf("new.txt = %q, want the cherry-picked content", content)
	}
}

func TestGitRefActionBranchDeleteUnmergedConflicts(t *testing.T) {
	dir := worktreeFixture(t)
	run(t, dir, "git", "checkout", "-b", "side")
	writeFile(t, filepath.Join(dir, "side.txt"), "side\n")
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-m", "side work")
	run(t, dir, "git", "checkout", "main")

	h := NewHandler()
	h.SetWorkDir(dir)
	rec := refActionPost(t, h, "", map[string]string{"action": "branch-delete", "target": "side"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "not fully merged") {
		t.Errorf("body %q does not carry git's refusal text", rec.Body.String())
	}
	if !gitRefResolves(dir, "refs/heads/side") {
		t.Error("refused branch-delete must keep the branch")
	}
}

func TestGitRefActionRemoteHostNotImplemented(t *testing.T) {
	dir := worktreeFixture(t)
	h := NewHandler()
	h.SetWorkDir(dir)
	rec := refActionPost(t, h, "?host=ci.local&project="+dir, map[string]string{"action": "reset-hard", "target": "HEAD"})
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status %d, want 501: %s", rec.Code, rec.Body.String())
	}
}

func TestGitRefActionHexLookingBranchNamesAreBranches(t *testing.T) {
	dir := worktreeFixture(t)
	run(t, dir, "git", "branch", "dead")
	run(t, dir, "git", "branch", "cafe")
	h := NewHandler()
	h.SetWorkDir(dir)

	if rec := refActionPost(t, h, "", map[string]string{"action": "branch-delete", "target": "dead"}); rec.Code != http.StatusOK {
		t.Fatalf("branch-delete dead: status %d: %s", rec.Code, rec.Body.String())
	}
	if gitRefResolves(dir, "refs/heads/dead") {
		t.Error("branch-delete did not delete refs/heads/dead")
	}

	if rec := refActionPost(t, h, "", map[string]string{"action": "checkout", "target": "cafe"}); rec.Code != http.StatusOK {
		t.Fatalf("checkout cafe: status %d: %s", rec.Code, rec.Body.String())
	}
	if got := currentBranch(t, dir); got != "cafe" {
		t.Errorf("HEAD branch = %q, want cafe", got)
	}
}

func TestGitRefActionRefusesWorktreeWritesDuringTurn(t *testing.T) {
	dir := worktreeFixture(t)
	first := headHash(t, dir)
	writeFile(t, filepath.Join(dir, "tracked.txt"), "two\n")
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-m", "second")

	h := NewHandler()
	h.SetWorkDir(dir)
	const sessionID = "ses_refguard"
	h.sessions.Register(sessionID, dir)
	h.sessions.setTurnActive(sessionID, true)

	refused := []map[string]any{
		{"action": "reset-hard", "target": first, "confirm": true},
		{"action": "merge", "target": first},
		{"action": "cherry-pick", "target": first},
		{"action": "revert", "target": first},
	}
	for _, body := range refused {
		rec := refActionPost(t, h, "", body)
		if rec.Code != http.StatusConflict {
			t.Fatalf("%v during a turn: status %d, want 409: %s", body["action"], rec.Code, rec.Body.String())
		}
	}
	if got := headHash(t, dir); got == first {
		t.Fatal("a refused ref action still moved HEAD")
	}

	h.sessions.setTurnActive(sessionID, false)
	rec := refActionPost(t, h, "", map[string]any{"action": "reset-hard", "target": first, "confirm": true})
	if rec.Code != http.StatusOK {
		t.Fatalf("reset-hard after the turn: status %d: %s", rec.Code, rec.Body.String())
	}
	if got := headHash(t, dir); got != first {
		t.Errorf("HEAD = %s, want %s", got, first)
	}
}

func TestGitRefActionConflictCarriesWorkspace(t *testing.T) {
	dir := worktreeFixture(t)
	run(t, dir, "git", "checkout", "-b", "feat")
	writeFile(t, filepath.Join(dir, "tracked.txt"), "feat\n")
	run(t, dir, "git", "commit", "-am", "feat change")
	picked := headHash(t, dir)
	run(t, dir, "git", "checkout", "main")
	writeFile(t, filepath.Join(dir, "tracked.txt"), "main\n")
	run(t, dir, "git", "commit", "-am", "main change")

	h := NewHandler()
	h.SetWorkDir(dir)
	rec := refActionPost(t, h, "", map[string]string{"action": "cherry-pick", "target": picked})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status %d, want 409: %s", rec.Code, rec.Body.String())
	}
	var res GitRefActionConflict
	if err := json.NewDecoder(rec.Body).Decode(&res); err != nil {
		t.Fatalf("decode conflict body: %v", err)
	}
	if res.Error == "" {
		t.Error("conflict body carries no git error text")
	}
	if res.Workspace.Status.Operation == nil || res.Workspace.Status.Operation.Kind != "cherry-pick" {
		t.Errorf("workspace operation = %+v, want a halted cherry-pick", res.Workspace.Status.Operation)
	}
	if len(res.Workspace.Status.Conflicts) == 0 {
		t.Error("workspace reports no conflicted paths")
	}
}
