package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/remote"
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

// remoteRefFixture creates a real repo served through the fake ssh shim and a
// handler that has it registered as a remote project. It returns the handler,
// the repo path, HEAD and the ssh invocation log path.
func remoteRefFixture(t *testing.T) (h *Handler, repo, head, sshLog string) {
	t.Helper()
	sshLog = installCountingFakeSSH(t)
	repo = t.TempDir()
	initGitRepo(t, repo)
	writeFile(t, filepath.Join(repo, "f.txt"), "one\n")
	run(t, repo, "git", "add", ".")
	run(t, repo, "git", "commit", "-m", "first")
	head = strings.TrimSpace(runOut(t, repo, "git", "rev-parse", "HEAD"))
	h = newTestHandlerWithRemote(t, "ci.local", repo)
	return h, repo, head, sshLog
}

// refActionError decodes the {"error": ...} body a validation failure returns.
func refActionError(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body %q: %v", rec.Body.String(), err)
	}
	return body.Error
}

func TestGitRefActionRemoteNowSupported(t *testing.T) {
	h, repo, head, sshLog := remoteRefFixture(t)
	rec := refActionPost(t, h, "?host=ci.local&project="+repo, map[string]any{"action": "branch", "target": head, "name": "newbr"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if !gitRefResolves(repo, "refs/heads/newbr") {
		t.Error("remote branch action must create the branch on the host")
	}
	if inv := readSSHInvocations(t, sshLog); !strings.Contains(inv, "'newbr'") {
		t.Errorf("branch name must reach the host single-quoted; invocations: %q", inv)
	}

	// checkout reports success on stderr, so the remote path must capture it
	// like the local CombinedOutput does.
	run(t, repo, "git", "branch", "side")
	// checkout rewrites the worktree, so it runs the remote turn probe, which
	// needs a connected host.
	connectedTurnHost(t, h, "ci.local", func() bool { return false }, 0)
	rec = refActionPost(t, h, "?host=ci.local&project="+repo, map[string]any{"action": "checkout", "target": "side"})
	if rec.Code != http.StatusOK {
		t.Fatalf("checkout status %d: %s", rec.Code, rec.Body.String())
	}
	var res GitRefActionResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if !strings.Contains(res.Output, "Switched to branch") {
		t.Errorf("output %q does not carry git's stderr", res.Output)
	}
}

// TestGitRefActionRemoteProjectPathWithSpaces proves the quoting added for
// safety did not turn the working directory into a literal: the cd path is
// shell-quoted by shellQuotePathPOSIX, so a path with spaces still runs. (~ and
// spaces in that path are covered by internal/remote's
// TestShellQuotePathPreservesTildeExpansion and
// TestShellQuotePathActuallyExpandsUnderSh.)
func TestGitRefActionRemoteProjectPathWithSpaces(t *testing.T) {
	installCountingFakeSSH(t)
	repo := filepath.Join(t.TempDir(), "repo with spaces")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	initGitRepo(t, repo)
	writeFile(t, filepath.Join(repo, "f.txt"), "one\n")
	run(t, repo, "git", "add", ".")
	run(t, repo, "git", "commit", "-m", "first")
	head := strings.TrimSpace(runOut(t, repo, "git", "rev-parse", "HEAD"))
	h := newTestHandlerWithRemote(t, "ci.local", repo)
	rec := refActionPost(t, h, "?host=ci.local&project="+url.QueryEscape(repo),
		map[string]any{"action": "branch", "target": head, "name": "spacebr"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if !gitRefResolves(repo, "refs/heads/spacebr") {
		t.Error("branch action must run in a project path containing spaces")
	}
}

func TestGitRefActionRemoteCheckoutTracksRemoteBranch(t *testing.T) {
	h, repo, head, _ := remoteRefFixture(t)
	// --track only accepts a real remote-tracking ref, which git recognises
	// through the remote's fetch config.
	run(t, repo, "git", "config", "remote.origin.url", "https://example.invalid/repo.git")
	run(t, repo, "git", "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	run(t, repo, "git", "update-ref", "refs/remotes/origin/feature", head)
	connectedTurnHost(t, h, "ci.local", func() bool { return false }, 0)
	rec := refActionPost(t, h, "?host=ci.local&project="+repo, map[string]any{"action": "checkout", "target": "origin/feature"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if got := strings.TrimSpace(runOut(t, repo, "git", "rev-parse", "--abbrev-ref", "HEAD")); got != "feature" {
		t.Errorf("HEAD = %q, want feature (checkout must track the remote branch)", got)
	}
}

// connectedTurnHost wires a live workspace for host whose server answers
// GET /api/git/turn-active with active(). A non-zero status makes the endpoint
// fail instead (404 models an older host's server).
func connectedTurnHost(t *testing.T, h *Handler, host string, active func() bool, status int) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/git/turn-active", func(w http.ResponseWriter, r *http.Request) {
		if status != 0 && status != http.StatusOK {
			http.Error(w, `{"error":"no such endpoint"}`, status)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"turn_active": active()})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	if h.remoteHosts == nil {
		h.remoteHosts = newTestRegistry(nil)
	}
	reg := h.remoteHosts
	reg.mu.Lock()
	e := &remoteHostEntry{}
	e.connecting.L = &e.mu
	e.workspace = &fakeWorkspace{apiURL: srv.URL, token: "tok-turn-test"}
	e.connected = true
	reg.byConn[host] = e
	reg.mu.Unlock()
}

// twoCommits adds a second commit and returns (HEAD, HEAD~1) for a reset target.
func twoCommits(t *testing.T, repo string) (head, parent string) {
	t.Helper()
	parent = strings.TrimSpace(runOut(t, repo, "git", "rev-parse", "HEAD"))
	writeFile(t, filepath.Join(repo, "second.txt"), "two\n")
	run(t, repo, "git", "add", ".")
	run(t, repo, "git", "commit", "-m", "second")
	head = strings.TrimSpace(runOut(t, repo, "git", "rev-parse", "HEAD"))
	return head, parent
}

func headOf(t *testing.T, repo string) string {
	t.Helper()
	return strings.TrimSpace(runOut(t, repo, "git", "rev-parse", "HEAD"))
}

func TestGitTurnActiveReportsProjectTurn(t *testing.T) {
	dir := worktreeFixture(t)
	h := NewHandler()
	h.SetWorkDir(dir)
	h.sessions.Register("ses_turn", dir)

	call := func() bool {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/git/turn-active?project="+url.QueryEscape(dir), nil)
		h.HandleGitTurnActive(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
		var body struct {
			TurnActive bool `json:"turn_active"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return body.TurnActive
	}

	if call() {
		t.Error("a project with no running turn reported active")
	}
	h.sessions.setTurnActive("ses_turn", true)
	if !call() {
		t.Error("a running turn was not reported")
	}
	h.sessions.setTurnActive("ses_turn", false)
	if call() {
		t.Error("an idle turn was still reported active")
	}

	rec := httptest.NewRecorder()
	h.HandleGitTurnActive(rec, httptest.NewRequest("GET", "/api/git/turn-active", nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("missing project status %d, want 400", rec.Code)
	}
}

func TestGitRefActionRemoteTurnGuardRefusesWhileATurnRuns(t *testing.T) {
	h, repo, _, _ := remoteRefFixture(t)
	head, parent := twoCommits(t, repo)
	connectedTurnHost(t, h, "ci.local", func() bool { return true }, 0)

	rec := refActionPost(t, h, "?host=ci.local&project="+repo,
		map[string]any{"action": "reset-hard", "target": parent, "confirm": true})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "a chat turn is running") {
		t.Errorf("body %q is not the turn-guard refusal", rec.Body.String())
	}
	if got := headOf(t, repo); got != head {
		t.Error("reset-hard ran while a chat turn was active")
	}
}

func TestGitRefActionRemoteTurnGuardAllowsWhenIdle(t *testing.T) {
	h, repo, _, _ := remoteRefFixture(t)
	head, parent := twoCommits(t, repo)
	connectedTurnHost(t, h, "ci.local", func() bool { return false }, 0)

	rec := refActionPost(t, h, "?host=ci.local&project="+repo,
		map[string]any{"action": "reset-hard", "target": parent, "confirm": true})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if got := headOf(t, repo); got != parent {
		t.Errorf("HEAD = %q, want %q after an idle reset-hard", got, parent)
	}
	if head == parent {
		t.Fatal("fixture did not create two commits")
	}
}

func TestGitRefActionRemoteTurnGuardFailsClosed(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, h *Handler)
		want  string
	}{
		{
			name:  "host not connected",
			setup: func(*testing.T, *Handler) {},
			want:  "connect the host",
		},
		{
			name: "older host has no endpoint",
			setup: func(t *testing.T, h *Handler) {
				connectedTurnHost(t, h, "ci.local", func() bool { return false }, http.StatusNotFound)
			},
			want: "cannot report turn state",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, repo, _, _ := remoteRefFixture(t)
			head, parent := twoCommits(t, repo)
			tc.setup(t, h)

			rec := refActionPost(t, h, "?host=ci.local&project="+repo,
				map[string]any{"action": "reset-hard", "target": parent, "confirm": true})
			if rec.Code != http.StatusConflict {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.want) {
				t.Errorf("body %q does not mention %q", rec.Body.String(), tc.want)
			}
			if got := headOf(t, repo); got != head {
				t.Error("an unverifiable turn state must not let reset-hard run")
			}
		})
	}
}

func TestGitRefActionRemoteQuotesShellMetacharacters(t *testing.T) {
	h, repo, head, _ := remoteRefFixture(t)
	marker := filepath.Join(t.TempDir(), "pwned")

	payload := "HEAD; touch " + marker + " #"
	rec := refActionPost(t, h, "?host=ci.local&project="+repo, map[string]any{"action": "checkout", "target": payload})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatalf("target injection executed on the remote host: %s", marker)
	}

	namePayload := "x; touch " + marker + " #"
	for _, action := range []string{"branch", "tag"} {
		rec = refActionPost(t, h, "?host=ci.local&project="+repo, map[string]any{"action": action, "target": head, "name": namePayload})
		if _, err := os.Stat(marker); err == nil {
			t.Fatalf("%s name injection executed on the remote host (status %d): %s", action, rec.Code, rec.Body.String())
		}
	}

	for _, p := range []string{"$(touch " + marker + ")", "`touch " + marker + "`"} {
		refActionPost(t, h, "?host=ci.local&project="+repo, map[string]any{"action": "checkout", "target": p})
		if _, err := os.Stat(marker); err == nil {
			t.Fatalf("target %q executed on the remote host", p)
		}
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
		{"action": "checkout", "target": first},
		{"action": "branch-checkout", "target": first, "name": "during-turn"},
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

func TestGitRefActionRemoteRejectedInput(t *testing.T) {
	h, repo, _, _ := remoteRefFixture(t)
	rec := refActionPost(t, h, "?host=ci.local&project="+repo, map[string]string{"action": "checkout", "target": "-x"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("dash target status %d: %s", rec.Code, rec.Body.String())
	}
	// The exact reason proves the remote handler ran: the local fallback for an
	// unregistered project answers "unknown project" with the same 400.
	if !strings.Contains(rec.Body.String(), "target must be a commit hash or branch name") {
		t.Errorf("body %q is not the ref-action validation message", rec.Body.String())
	}
}

// TestGitRefActionLocalRemoteValidationParity pins BOTH transports to the same
// literal 400 reason. Comparing them to each other alone would prove nothing
// about correctness: both now run the shared validateGitRefActionUsing rule
// table, so a corrupted rule yields the same wrong answer on each side. The
// literal is what catches a rule or message change; the cross-check catches a
// transport that stops routing through the shared table. Worktree-writing
// actions are excluded because the turn guard runs after validation and their
// outcomes depend on a session, not on these rules.
func TestGitRefActionLocalRemoteValidationParity(t *testing.T) {
	installCountingFakeSSH(t)
	repo := t.TempDir()
	initGitRepo(t, repo)
	writeFile(t, filepath.Join(repo, "f.txt"), "one\n")
	run(t, repo, "git", "add", ".")
	run(t, repo, "git", "commit", "-m", "first")
	head := strings.TrimSpace(runOut(t, repo, "git", "rev-parse", "HEAD"))

	local := NewHandler()
	local.SetWorkDir(repo)
	remote := newTestHandlerWithRemote(t, "ci.local", repo)
	remoteQuery := "?host=ci.local&project=" + repo

	cases := []struct {
		name string
		body map[string]any
		want string
	}{
		{"unknown action", map[string]any{"action": "nope", "target": head}, "nope is not a supported ref action"},
		{"empty target", map[string]any{"action": "checkout", "target": ""}, "target must be a commit hash or branch name"},
		{"dash target", map[string]any{"action": "checkout", "target": "-x"}, "target must be a commit hash or branch name"},
		{"unresolved hash", map[string]any{"action": "checkout", "target": "deadbeef"}, `target "deadbeef" does not resolve to a commit`},
		{"unresolved branch", map[string]any{"action": "checkout", "target": "no-such-branch"}, `target "no-such-branch" does not resolve to a commit`},
		{"branch-only with hash", map[string]any{"action": "branch-delete", "target": head}, "branch-delete needs a branch name"},
		{"missing name", map[string]any{"action": "branch", "target": head}, "name is required"},
		{"name on name-less action", map[string]any{"action": "checkout", "target": head, "name": "x"}, "checkout does not take a name"},
		{"dash name", map[string]any{"action": "branch", "target": head, "name": "-x"}, `invalid name "-x"`},
		{"bad branch name", map[string]any{"action": "branch", "target": head, "name": "bad name"}, `invalid branch name "bad name"`},
		{"bad tag name", map[string]any{"action": "tag", "target": head, "name": "bad name"}, `invalid tag name "bad name"`},
	}
	for _, tc := range cases {
		lrec := refActionPost(t, local, "", tc.body)
		rrec := refActionPost(t, remote, remoteQuery, tc.body)
		if got := refActionError(t, lrec); got != tc.want {
			t.Errorf("%s: local reason %q, want %q", tc.name, got, tc.want)
		}
		if got := refActionError(t, rrec); got != tc.want {
			t.Errorf("%s: remote reason %q, want %q", tc.name, got, tc.want)
		}
		if lrec.Code != rrec.Code || refActionError(t, lrec) != refActionError(t, rrec) {
			t.Errorf("%s: local (%d, %q) != remote (%d, %q)",
				tc.name, lrec.Code, refActionError(t, lrec), rrec.Code, refActionError(t, rrec))
		}
	}
}

// TestRemoteRefValidationUsesOneRoundTrip pins the batched probe script: every
// probe the shared rule table can issue must ride in ONE ssh invocation, the
// same convention remoteGitStatus follows (TestRemoteGitStatusUsesOneBatchedRoundTrip).
// A regression to per-probe calls makes this count 2 or more.
func TestRemoteRefValidationUsesOneRoundTrip(t *testing.T) {
	logPath := installCountingFakeSSH(t)
	repo := t.TempDir()
	initGitRepo(t, repo)
	writeFile(t, filepath.Join(repo, "f.txt"), "one\n")
	run(t, repo, "git", "add", ".")
	run(t, repo, "git", "commit", "-m", "first")
	head := strings.TrimSpace(runOut(t, repo, "git", "rev-parse", "HEAD"))

	target, err := remote.ParseTarget("ci.local")
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}
	rw := remoteWork{Target: target, Path: repo}

	// tag is the worst case: target-hash, target-resolve, target-format,
	// name-format and tag-format probes are all emitted.
	reason, err := validateGitRefActionRemote(context.Background(), rw, "tag", gitRefActions["tag"],
		GitRefActionRequest{Target: head, Name: "v1"})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if reason != "" {
		t.Fatalf("reason = %q, want none", reason)
	}

	if n := strings.Count(readSSHInvocations(t, logPath), "-o BatchMode"); n != 1 {
		t.Errorf("validation made %d ssh round trips, want 1 (probes must ride in one batched script)", n)
	}
}

// TestRemoteRefProbeParseFailsClosed pins the batched probe parser's refusal
// direction: a truncated or garbled reply must answer "no" for every probe it
// does not carry, never "yes" by omission.
func TestRemoteRefProbeParseFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name string
		out  string
	}{
		{"empty", ""},
		{"one line", "0\n"},
		{"garbage", "yes\nno\nmaybe\n"},
		{"blank lines", "\n\n\n\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := parseRemoteRefProbes(tc.out, "main", "v1")
			if p.refResolves("main^{commit}") {
				t.Error("target resolve answered yes from an incomplete reply")
			}
			if p.branchNameValid("main") {
				t.Error("target format answered yes from an incomplete reply")
			}
			if p.branchNameValid("v1") {
				t.Error("name format answered yes from an incomplete reply")
			}
			if p.refFormatValid("refs/tags/v1") {
				t.Error("tag format answered yes from an incomplete reply")
			}
			if p.refResolves("refs/heads/someone-else") {
				t.Error("an unknown query answered yes")
			}
		})
	}

	// The success direction still works: line 0 (branch at target) and line 4
	// (tag format) are the only true answers here.
	p := parseRemoteRefProbes("0\n1\n1\n1\n0\n", "main", "v1")
	if !p.refResolves("refs/heads/main") {
		t.Error("branch-at-target should be true")
	}
	if p.refResolves("main^{commit}") {
		t.Error("target resolve should be false")
	}
	if !p.refFormatValid("refs/tags/v1") {
		t.Error("tag format should be true")
	}
}
