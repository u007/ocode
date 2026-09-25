package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Phase 05: remote (SSH/WSL) parity for conflict detection and in-progress
// operation recovery. These tests drive the REAL remote code path through the
// existing fake-SSH harness, which runs each remote command locally against a
// genuine temp git repository — so the batched script, the shared phase-01
// parser, the path-safety gate and the mutation wiring are all exercised
// end-to-end rather than mocked.
//
// The local cases in handler_git_conflict_resolve_test.go are mirrored here
// deliberately: remote must behave identically to local, so the same fixture
// and the same assertions are the contract.

// remotePostJSON issues a POST with a JSON body and decodes the response into
// out (only on 200), mirroring getJSON's shape for the new POST endpoints.
func remotePostJSON(t *testing.T, h *Handler, url string, body interface{}, out interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		t.Fatalf("encode body: %v", err)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", url, &buf)
	r.Header.Set("Content-Type", "application/json")
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/git/conflict/resolve", h.HandleGitResolveConflict)
	mux.HandleFunc("POST /api/git/operation", h.HandleGitOperation)
	mux.ServeHTTP(w, r)
	if out != nil && w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
			t.Fatalf("decode %s: %v", url, err)
		}
	}
	return w
}

// remoteStatusJSON fetches remote status through the real handler.
func remoteStatusJSON(t *testing.T, h *Handler, host, project string) GitStatus {
	t.Helper()
	var status GitStatus
	w := getJSON(t, h, "/api/git/status?host="+host+"&project="+project, &status)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	return status
}

// gitDirForTest resolves a repo's git directory for a test fixture.
func gitDirForTest(t *testing.T, repo string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "--absolute-git-dir")
	cmd.Dir = repo
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rev-parse --absolute-git-dir: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// --- Status: conflicts and operation state ---

// A remote conflicted file must produce a conflict entry with the right code
// and the right ours/theirs booleans, and must make HasChanges true.
func TestRemoteGitStatusReportsConflicts(t *testing.T) {
	installFakeSSH(t)
	repo := gitConflictedMergeFile(t, "conflicted.txt")

	h := newTestHandlerWithRemote(t, "ci.local", repo)
	status := remoteStatusJSON(t, h, "ci.local", repo)

	if !status.IsRepo {
		t.Fatalf("IsRepo = false, want true")
	}
	if len(status.Conflicts) != 1 {
		t.Fatalf("Conflicts = %+v, want exactly 1", status.Conflicts)
	}
	got := status.Conflicts[0]
	if got.Path != "conflicted.txt" {
		t.Errorf("Path = %q, want conflicted.txt", got.Path)
	}
	if got.Code != "UU" {
		t.Errorf("Code = %q, want UU", got.Code)
	}
	if !got.Ours || !got.Theirs {
		t.Errorf("Ours/Theirs = %v/%v, want both true for a UU record", got.Ours, got.Theirs)
	}
	if !status.HasChanges {
		t.Errorf("HasChanges = false, want true when conflicts are present")
	}
	// A conflicted path must not ALSO appear in staged or changed: the local
	// pipeline excludes it, and remote must agree or the Git tab double-counts
	// the same file.
	if sliceContains(status.StagedFiles, "conflicted.txt") {
		t.Errorf("StagedFiles contains a conflicted path: %v", status.StagedFiles)
	}
	if sliceContains(status.ChangedFiles, "conflicted.txt") {
		t.Errorf("ChangedFiles contains a conflicted path: %v", status.ChangedFiles)
	}
}

// A remote merge must be reported from the remote's OWN state files, proving
// the probe block and the shared phase-01 parser agree.
func TestRemoteGitStatusReportsMergeOperation(t *testing.T) {
	installFakeSSH(t)
	repo := gitConflictedMergeFile(t, "conflicted.txt")

	h := newTestHandlerWithRemote(t, "ci.local", repo)
	status := remoteStatusJSON(t, h, "ci.local", repo)

	if status.Operation == nil {
		t.Fatalf("Operation = nil, want a merge from the remote's own state files")
	}
	if status.Operation.Kind != "merge" {
		t.Errorf("Kind = %q, want merge", status.Operation.Kind)
	}
	if !strings.Contains(status.Operation.Label, "Merg") {
		t.Errorf("Label = %q, want it to mention merging", status.Operation.Label)
	}
}

// A state file whose CONTENT contains a tab and a newline (MERGE_MSG can hold a
// commit message) must not corrupt the state transport. The parser only needs
// MERGE_MSG's PRESENCE to identify a merge, so the operation must be unchanged.
func TestRemoteGitStatusOperationStateWithTabsAndNewlines(t *testing.T) {
	installFakeSSH(t)
	repo := gitConflictedMergeFile(t, "conflicted.txt")
	gitDir := gitDirForTest(t, repo)
	if err := os.WriteFile(filepath.Join(gitDir, "MERGE_MSG"),
		[]byte("Merge branch 'other'\n\nwith\ta tab and a second line\n"), 0o644); err != nil {
		t.Fatalf("write MERGE_MSG: %v", err)
	}

	h := newTestHandlerWithRemote(t, "ci.local", repo)
	status := remoteStatusJSON(t, h, "ci.local", repo)

	if status.Operation == nil || status.Operation.Kind != "merge" {
		t.Fatalf("Operation = %+v, want a merge despite tabs/newlines in MERGE_MSG", status.Operation)
	}
	if !status.IsRepo {
		t.Errorf("IsRepo = false, want true")
	}
}

// A synthetic rebase-merge must be reported with the branch prefix stripped —
// identical to what the LOCAL path produces for the same state. Rebase cannot
// be produced for real here (git rebase is denied in this sandbox), so the
// state files are written directly, as phase 01 also had to do.
func TestRemoteGitStatusReportsRebaseOperation(t *testing.T) {
	installFakeSSH(t)
	repo := t.TempDir()
	initGitRepo(t, repo)
	writeFile(t, filepath.Join(repo, "a.txt"), "a\n")
	run(t, repo, "git", "add", ".")
	run(t, repo, "git", "commit", "-m", "init")

	gitDir := gitDirForTest(t, repo)
	rm := filepath.Join(gitDir, "rebase-merge")
	if err := os.MkdirAll(rm, 0o755); err != nil {
		t.Fatalf("mkdir rebase-merge: %v", err)
	}
	for name, val := range map[string]string{
		"msgnum":    "2\n",
		"end":       "5\n",
		"head-name": "refs/heads/feature\n",
		"onto":      "abc123\n",
	} {
		if err := os.WriteFile(filepath.Join(rm, name), []byte(val), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	h := newTestHandlerWithRemote(t, "ci.local", repo)
	status := remoteStatusJSON(t, h, "ci.local", repo)

	if status.Operation == nil {
		t.Fatalf("Operation = nil, want a rebase")
	}
	if status.Operation.Kind != "rebase" {
		t.Errorf("Kind = %q, want rebase", status.Operation.Kind)
	}
	if status.Operation.Step != 2 || status.Operation.Total != 5 {
		t.Errorf("Step/Total = %d/%d, want 2/5", status.Operation.Step, status.Operation.Total)
	}
	if strings.Contains(status.Operation.Label, "refs/heads/") {
		t.Errorf("Label = %q, want the refs/heads/ prefix stripped", status.Operation.Label)
	}

	// The remote result must EQUAL the local result for the same state: the
	// whole point of the shared parser is that the transport is invisible.
	localOp := gitOperationStateFor(gitStateEntriesInDir(gitDir))
	if localOp == nil {
		t.Fatalf("local parser returned nil for the same state")
	}
	if localOp.Kind != status.Operation.Kind || localOp.Label != status.Operation.Label ||
		localOp.Step != status.Operation.Step || localOp.Total != status.Operation.Total {
		t.Errorf("remote = %+v, local = %+v; the shared parser must make these identical", *status.Operation, *localOp)
	}
}

// A non-repo must still report is_repo:false. Appending the new probe sections
// must not change the batched script's exit code, or a non-repo would come back
// as a completely EMPTY status instead.
func TestRemoteGitStatusNonRepoStillReportsIsRepoFalse(t *testing.T) {
	installFakeSSH(t)
	dir := t.TempDir()

	h := newTestHandlerWithRemote(t, "ci.local", dir)
	status := remoteStatusJSON(t, h, "ci.local", dir)

	if status.IsRepo {
		t.Errorf("IsRepo = true, want false for a plain directory")
	}
	if status.Conflicts == nil {
		t.Errorf("Conflicts = nil, want an empty slice (never nil, per the phase-02 contract)")
	}
	if status.Operation != nil {
		t.Errorf("Operation = %+v, want nil outside an operation", status.Operation)
	}
}

// The new conflict + operation probes must ride in the EXISTING batched
// script, not as extra round trips: each extra probe is an extra ssh
// invocation over a high-latency link.
func TestRemoteGitStatusUsesOneBatchedRoundTrip(t *testing.T) {
	logPath := installCountingFakeSSH(t)
	repo := gitConflictedMergeFile(t, "conflicted.txt")

	h := newTestHandlerWithRemote(t, "ci.local", repo)
	_ = remoteStatusJSON(t, h, "ci.local", repo)

	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read ssh invocation log: %v", err)
	}
	// Count round trips, not script sections. The shared
	// installCountingFakeSSH shim logs `"$*"` expanded, so one call puts
	// every ssh option AND every line of its command on separate log lines:
	// a batched script shows up as many lines, not one. Each invocation is
	// unambiguously introduced by its control option, so counting those
	// measures what actually crosses the network.
	invocations := strings.Count(string(b), "-o BatchMode")
	// Baseline before this phase is 3: the batched script, the upstream
	// fallback, and the is-inside-work-tree recheck. The conflict and
	// operation-state probes must ride INSIDE the batch, so this must not
	// grow — issuing them as separate calls would make it 5.
	if invocations > 3 {
		t.Errorf("status made %d remote round trips, want 3: the new probes must ride in the existing batch, not as extra calls", invocations)
	}
}

// --- Resolve over the transport ---

// Resolving "ours" then "theirs" must match the local behavior. Each
// resolution gets its OWN conflicted repo, exactly like the local table test:
// once a side is resolved and staged the path is no longer conflicted, so a
// second resolve on the same repo would (correctly) be refused as 409.
func TestRemoteGitResolveConflictOursThenTheirs(t *testing.T) {
	installFakeSSH(t)
	for _, tc := range []struct{ resolution, want, absent string }{
		{resolution: "ours", want: "OURS", absent: "THEIRS"},
		{resolution: "theirs", want: "THEIRS", absent: "OURS"},
	} {
		t.Run(tc.resolution, func(t *testing.T) {
			repo := gitConflictedMergeFile(t, "conflicted.txt")
			h := newTestHandlerWithRemote(t, "ci.local", repo)
			var res GitWorkspace
			w := remotePostJSON(t, h, "/api/git/conflict/resolve?host=ci.local&project="+repo,
				GitConflictResolveRequest{Path: "conflicted.txt", Resolution: tc.resolution}, &res)
			if w.Code != http.StatusOK {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			body := readFileString(t, filepath.Join(repo, "conflicted.txt"))
			if !strings.Contains(body, tc.want) {
				t.Errorf("working tree = %q, want it to contain %q", body, tc.want)
			}
			if strings.Contains(body, tc.absent) {
				t.Errorf("working tree = %q, must not contain the rejected side %q", body, tc.absent)
			}
			if strings.Contains(body, "<<<<<<<") {
				t.Errorf("working tree still has conflict markers: %q", body)
			}
			for _, c := range res.Status.Conflicts {
				if c.Path == "conflicted.txt" {
					t.Error("path still reported as conflicted after resolving")
				}
			}
			// The merge is still in progress, so the operation must remain
			// reported — resolving a file is not continuing or aborting it.
			if res.Status.Operation == nil || res.Status.Operation.Kind != "merge" {
				t.Errorf("returned operation = %+v, want the merge still in progress", res.Status.Operation)
			}
		})
	}
}

// A side that was DELETED must be materialized with `git rm`, not
// checkout --ours/--theirs (which cannot produce a deleted side).
func TestRemoteGitResolveConflictDeletedSideUsesGitRm(t *testing.T) {
	installFakeSSH(t)
	repo := t.TempDir()
	initGitRepo(t, repo)
	path := filepath.Join(repo, "gone.txt")
	writeFile(t, path, "base\n")
	run(t, repo, "git", "add", ".")
	run(t, repo, "git", "commit", "-m", "base")
	run(t, repo, "git", "switch", "-c", "other")
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}
	run(t, repo, "git", "commit", "-am", "delete it")
	run(t, repo, "git", "switch", "-")
	writeFile(t, path, "ours survives\n")
	run(t, repo, "git", "commit", "-am", "ours")
	if out, err := gitCombined(t, repo, "git", "merge", "other"); err == nil {
		t.Fatalf("expected conflict, got:\n%s", out)
	}

	h := newTestHandlerWithRemote(t, "ci.local", repo)
	// "theirs" is the deletion, so the file must end up REMOVED.
	w := remotePostJSON(t, h, "/api/git/conflict/resolve?host=ci.local&project="+repo,
		GitConflictResolveRequest{Path: "gone.txt", Resolution: "theirs"}, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file still exists after resolving a deleted side to theirs; want git rm")
	}
}

// "mark" must REFUSE while real conflict markers remain, exactly as local.
func TestRemoteGitResolveConflictMarkRefusesLeftoverMarkers(t *testing.T) {
	installFakeSSH(t)
	repo := gitConflictedMergeFile(t, "conflicted.txt")
	h := newTestHandlerWithRemote(t, "ci.local", repo)

	w := remotePostJSON(t, h, "/api/git/conflict/resolve?host=ci.local&project="+repo,
		GitConflictResolveRequest{Path: "conflicted.txt", Resolution: "mark"}, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400 while markers remain: %s", w.Code, w.Body.String())
	}

	// After editing the markers away, the same call must succeed.
	path := filepath.Join(repo, "conflicted.txt")
	body := strings.ReplaceAll(readFileString(t, path), "<<<<<<<", "")
	body = strings.ReplaceAll(body, ">>>>>>>", "")
	body = strings.ReplaceAll(body, "=======", "")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write cleaned file: %v", err)
	}
	w = remotePostJSON(t, h, "/api/git/conflict/resolve?host=ci.local&project="+repo,
		GitConflictResolveRequest{Path: "conflicted.txt", Resolution: "mark"}, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d after clearing markers, want 200: %s", w.Code, w.Body.String())
	}
	status := remoteStatusJSON(t, h, "ci.local", repo)
	if len(status.Conflicts) != 0 {
		t.Errorf("Conflicts = %+v, want none after mark", status.Conflicts)
	}
}

// A path that escapes the repository must be refused by the remote safety
// gate BEFORE any command is built.
func TestRemoteGitResolveConflictRefusesPathOutsideRepo(t *testing.T) {
	installFakeSSH(t)
	repo := gitConflictedMergeFile(t, "conflicted.txt")
	h := newTestHandlerWithRemote(t, "ci.local", repo)

	for _, path := range []string{"../../etc/passwd", "/etc/passwd", "a\x01b"} {
		w := remotePostJSON(t, h, "/api/git/conflict/resolve?host=ci.local&project="+repo,
			GitConflictResolveRequest{Path: path, Resolution: "ours"}, nil)
		if w.Code != http.StatusBadRequest {
			t.Errorf("path %q: status %d, want 400", path, w.Code)
		}
	}
}

// A literal-pathspec filename is a REAL local/remote divergence, and this
// test pins the agreed behavior rather than pretending parity.
//
// Locally, `:(top)f.txt` resolves fine because the endpoint sets
// GIT_LITERAL_PATHSPECS=1. Remotely, remoteSafeSpec rejects `:` (along with
// quotes, `;`, backticks, `$&|<>`, `\!*?[](){}#`) BEFORE any command is
// built. That validator is shared by every remote git mutation, so relaxing
// it here would widen the shell-injection guard for unrelated endpoints — the
// deliberate decision is to refuse and report, not to loosen. See TODO.md.
//
// The contract under test: a clear 400, and the file is left completely
// untouched (no partial mutation).
func TestRemoteGitResolveConflictRefusesPathspecMagicName(t *testing.T) {
	installFakeSSH(t)
	const magic = ":(top)f.txt"
	repo := gitConflictedMergeFile(t, magic)
	if _, err := os.Stat(filepath.Join(repo, magic)); err != nil {
		t.Fatalf("fixture did not create the literal name: %v", err)
	}
	before := readFileString(t, filepath.Join(repo, magic))
	h := newTestHandlerWithRemote(t, "ci.local", repo)

	w := remotePostJSON(t, h, "/api/git/conflict/resolve?host=ci.local&project="+repo,
		GitConflictResolveRequest{Path: magic, Resolution: "ours"}, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400 for a path remoteSafeSpec rejects: %s", w.Code, w.Body.String())
	}
	// The refusal must be reported, not silently swallowed: the user needs to
	// know WHY the file could not be resolved.
	if !strings.Contains(w.Body.String(), "unsupported characters") {
		t.Errorf("body = %s, want the validator's reason surfaced to the user", w.Body.String())
	}
	if after := readFileString(t, filepath.Join(repo, magic)); after != before {
		t.Errorf("file was mutated by a refused request: %q -> %q", before, after)
	}
}

// --- Operation over the transport ---

// Aborting a remote merge must clear the remote's operation state and return a
// workspace identical in type to the local response.
func TestRemoteGitOperationAbort(t *testing.T) {
	installFakeSSH(t)
	repo := gitConflictedMergeFile(t, "conflicted.txt")
	h := newTestHandlerWithRemote(t, "ci.local", repo)

	var res GitOperationResult
	w := remotePostJSON(t, h, "/api/git/operation?host=ci.local&project="+repo,
		GitOperationRequest{Action: "abort", Kind: "merge"}, &res)
	if w.Code != http.StatusOK {
		t.Fatalf("abort: status %d: %s", w.Code, w.Body.String())
	}
	if res.Workspace.Status.Operation != nil {
		t.Errorf("Operation = %+v, want nil after abort", res.Workspace.Status.Operation)
	}
	status := remoteStatusJSON(t, h, "ci.local", repo)
	if len(status.Conflicts) != 0 {
		t.Errorf("Conflicts = %+v, want none after abort", status.Conflicts)
	}
}

// Continuing a remote merge after the conflict is resolved must complete the
// merge, and must NOT invoke an editor (the ssh session has no TTY, so a real
// editor would hang the request). GIT_EDITOR is cleared in the test
// environment so an unsuppressed editor would be observable.
func TestRemoteGitOperationContinueSuppressesEditor(t *testing.T) {
	installFakeSSH(t)
	repo := gitConflictedMergeFile(t, "conflicted.txt")
	h := newTestHandlerWithRemote(t, "ci.local", repo)
	url := "/api/git/conflict/resolve?host=ci.local&project=" + repo
	remotePostJSON(t, h, url, GitConflictResolveRequest{Path: "conflicted.txt", Resolution: "ours"}, nil)
	// Make an editor invocation fail loudly rather than silently succeed, so
	// dropping GIT_EDITOR=true turns this test red instead of hanging CI.
	t.Setenv("GIT_EDITOR", "false")
	t.Setenv("EDITOR", "false")

	var res GitOperationResult
	w := remotePostJSON(t, h, "/api/git/operation?host=ci.local&project="+repo,
		GitOperationRequest{Action: "continue", Kind: "merge"}, &res)
	if w.Code != http.StatusOK {
		t.Fatalf("continue: status %d: %s", w.Code, w.Body.String())
	}
	if res.Workspace.Status.Operation != nil {
		t.Errorf("Operation = %+v, want nil after a successful continue", res.Workspace.Status.Operation)
	}
	// The merge must have actually committed. `rev-parse --verify` writes
	// its fatal diagnostic to stderr, which gitCombined captures, so the
	// check must be on the ERROR, not on empty output.
	if _, err := gitCombined(t, repo, "git", "rev-parse", "--verify", "MERGE_HEAD"); err == nil {
		t.Error("MERGE_HEAD still present after continue: the merge did not commit")
	}
}

// A kind mismatch must be refused with 409, so a stale panel cannot abort a
// different operation that started since it was rendered.
func TestRemoteGitOperationRejectsStaleKind(t *testing.T) {
	installFakeSSH(t)
	repo := gitConflictedMergeFile(t, "conflicted.txt")
	h := newTestHandlerWithRemote(t, "ci.local", repo)

	w := remotePostJSON(t, h, "/api/git/operation?host=ci.local&project="+repo,
		GitOperationRequest{Action: "abort", Kind: "rebase"}, nil)
	if w.Code != http.StatusConflict {
		t.Fatalf("status %d, want 409 for a kind mismatch: %s", w.Code, w.Body.String())
	}
	// And the merge must still be in progress: the refusal is not destructive.
	status := remoteStatusJSON(t, h, "ci.local", repo)
	if status.Operation == nil || status.Operation.Kind != "merge" {
		t.Errorf("Operation = %+v, want the merge untouched", status.Operation)
	}
}

// An unsupported (kind, action) pair must be refused with 400.
func TestRemoteGitOperationRejectsUnsupportedPair(t *testing.T) {
	installFakeSSH(t)
	repo := gitConflictedMergeFile(t, "conflicted.txt")
	h := newTestHandlerWithRemote(t, "ci.local", repo)

	// A merge has no skip.
	w := remotePostJSON(t, h, "/api/git/operation?host=ci.local&project="+repo,
		GitOperationRequest{Action: "skip", Kind: "merge"}, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400 for merge+skip: %s", w.Code, w.Body.String())
	}
}

// A nested project path must be normalized to the repo toplevel, so the
// operation cannot be scoped to a subdirectory.
func TestRemoteGitOperationFromNestedPath(t *testing.T) {
	installFakeSSH(t)
	repo := gitConflictedMergeFile(t, "conflicted.txt")
	nested := filepath.Join(repo, "sub", "dir")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	h := newTestHandlerWithRemote(t, "ci.local", nested)
	w := remotePostJSON(t, h, "/api/git/operation?host=ci.local&project="+nested,
		GitOperationRequest{Action: "abort", Kind: "merge"}, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", w.Code, w.Body.String())
	}
	// Query through the same registered path: the handler normalized the work
	// to the toplevel internally, which is exactly the behavior under test.
	status := remoteStatusJSON(t, h, "ci.local", nested)
	if status.Operation != nil {
		t.Errorf("Operation = %+v, want nil after abort from a nested path", status.Operation)
	}
}

// Every probe name must be a safe shell literal. remoteGitOperationStateProbe
// embeds these names directly, so a future edit that introduced a quote, space
// or metacharacter would break the block (or worse); this pins the invariant the
// probe's security comment depends on.
func TestRemoteGitOperationStateNamesAreSafeProbeLiterals(t *testing.T) {
	for _, name := range gitOperationStateNames {
		if !regexp.MustCompile(`^[A-Za-z0-9_/-]+$`).MatchString(name) {
			t.Errorf("state name %q is not a safe probe literal", name)
		}
	}
}

// TestRemoteGitOperationReusesOneStatusProbe locks in the single-probe fix in
// remoteGitOperation.
//
// remoteGitOperation needs the operation state (to reject a caller that has
// gone stale) AND the conflict list (to refuse `continue` while conflicts
// remain). Both live in remoteGitStatus, which is a whole batched ssh script.
// The handler used to call it twice on the continue path, costing a second
// full round trip — the exact cost the batched probe exists to avoid.
//
// This exercises `continue` WITH conflicts, because that is the only path that
// consults both values. The request is refused with 409, so no mutation and no
// post-mutation workspace refresh follow: the probe count must therefore be
// exactly one. A second probe means the regression is back.
//
// The probe is counted by its distinctive `status --porcelain=v2` text in the
// fake-SSH invocation log, not by counting BatchMode: the transport harness
// logs every line of a multi-line script separately, and other commands can
// contribute BatchMode invocations of their own.
func TestRemoteGitOperationReusesOneStatusProbe(t *testing.T) {
	logPath := installCountingFakeSSH(t)
	repo := gitConflictedMergeFile(t, "conflicted.txt")
	h := newTestHandlerWithRemote(t, "ci.local", repo)

	_ = os.Truncate(logPath, 0)
	var res GitOperationResult
	w := remotePostJSON(t, h, "/api/git/operation?host=ci.local&project="+repo,
		GitOperationRequest{Action: "continue", Kind: "merge"}, &res)

	// Precondition: the request must be refused FOR THE RIGHT REASON. A 409
	// from "no operation is in progress" or a kind mismatch would return
	// before the conflict check, and the probe count below would then prove
	// nothing at all.
	if w.Code != http.StatusConflict {
		t.Fatalf("continue: status %d, want 409: %s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, "resolve the remaining conflicts") {
		t.Fatalf("continue: body %q, want the unresolved-conflicts refusal; "+
			"a different 409 means the probe count below is meaningless", body)
	}

	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read ssh invocation log: %v", err)
	}
	//
	// One remoteGitStatus script contains the string twice: the
	// `status --porcelain=v2 --branch` section and the `status --porcelain=v2
	// -z` conflict section. So ONE call is 2 occurrences, and the regressed
	// double call is 4. Asserting the exact count pins the fix instead of
	// merely noticing "it went up".
	probes := strings.Count(string(b), "porcelain=v2")
	if probes != 2 {
		t.Errorf("continue logged %d porcelain=v2 occurrences, want 2 "+
			"(exactly one batched remoteGitStatus, which emits the string once for "+
			"the --branch section and once for the conflicts -z section): the "+
			"operation state and the conflict list must come from ONE status call",
			probes)
	}
}
