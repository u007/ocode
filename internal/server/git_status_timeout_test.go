package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestGitStatusForDirBoundedWhenGitHangs proves one project's wedged git can
// never pin the caller forever. Before the fix gitStatusForDir ran
// exec.Command("git", ...) with no deadline, so a repo whose git blocks (stalled
// network mount, index.lock held by a dead process) hung its own request and,
// because the git-status emitter ran projects sequentially on one goroutine,
// stalled every other project's git_status too. The call must come back
// within the bound with an error — never an empty status, which the UI would
// render as a clean repository.
func TestGitStatusForDirBoundedWhenGitHangs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell stub for git")
	}

	dir := t.TempDir()
	stub := filepath.Join(dir, "git")
	// `exec` replaces the shell so the killed process IS the sleeper — no
	// orphaned child lingers after the deadline fires.
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexec sleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	oldBin, oldTimeout := gitBinary, gitStatusTimeout
	gitBinary, gitStatusTimeout = stub, 200*time.Millisecond
	defer func() { gitBinary, gitStatusTimeout = oldBin, oldTimeout }()

	type result struct {
		st  GitStatus
		err error
	}
	done := make(chan result, 1)
	go func() {
		st, err := gitStatusForDir(dir)
		done <- result{st, err}
	}()

	select {
	case r := <-done:
		if r.err == nil || !strings.Contains(r.err.Error(), "timed out") {
			t.Fatalf("hung git returned status %+v, err %v; want a timeout error", r.st, r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("gitStatusForDir did not return within 5s — a hung git still wedges the caller")
	}
}

// mustGitStatus is gitStatusForDir for tests that expect the probe to work.
func mustGitStatus(t *testing.T, dir string) GitStatus {
	t.Helper()
	st, err := gitStatusForDir(dir)
	if err != nil {
		t.Fatalf("gitStatusForDir(%s): %v", dir, err)
	}
	return st
}

// A plain directory is a normal answer (IsRepo=false), not an error.
func TestGitStatusForDirNonRepoIsNotAnError(t *testing.T) {
	st, err := gitStatusForDir(t.TempDir())
	if err != nil {
		t.Fatalf("non-repo returned error %v, want IsRepo=false", err)
	}
	if st.IsRepo || st.HasChanges {
		t.Fatalf("non-repo status = %+v, want empty with IsRepo=false", st)
	}
}

// A repository whose status probe fails must surface the git error, never an
// empty status that reads as a clean repository.
func TestGitStatusForDirBrokenRepoIsAnError(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	writeFile(t, filepath.Join(dir, "a.txt"), "a\n")
	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "commit", "-m", "init")
	if err := os.WriteFile(filepath.Join(dir, ".git", "index"), []byte("not an index"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := gitStatusForDir(dir)
	if err == nil {
		t.Fatalf("corrupt index returned status %+v with no error", st)
	}
	if !strings.Contains(err.Error(), "index") {
		t.Fatalf("error %q does not carry git's message", err)
	}
}

// gitMissingStubs points gitBinary at a path that does not exist, so every
// probe fails with exec.ErrNotFound rather than a git exit status.
func gitMissingStub(t *testing.T) {
	t.Helper()
	oldBin, oldTimeout := gitBinary, gitStatusTimeout
	gitBinary = filepath.Join(t.TempDir(), "definitely-not-git")
	gitStatusTimeout = 5 * time.Second
	t.Cleanup(func() { gitBinary, gitStatusTimeout = oldBin, oldTimeout })
}

// A missing git binary is NOT the same answer as "not a repository". The probe
// used to return IsRepo=false for every non-timeout rev-parse failure, so a
// server with no git on PATH (or one invoked from an environment with an empty
// PATH) reported every project as a plain directory and logged nothing.
func TestGitStatusForDirMissingGitIsAnError(t *testing.T) {
	gitMissingStub(t)
	st, err := gitStatusForDir(t.TempDir())
	if err == nil {
		t.Fatalf("missing git returned status %+v with no error; want the real cause", st)
	}
	if st.IsRepo {
		t.Fatalf("status %+v reports IsRepo on a failed probe", st)
	}
	if !strings.Contains(err.Error(), "git") {
		t.Fatalf("error %q does not name the failing tool", err)
	}
}

// A repository git refuses to touch (safe.directory / dubious ownership, an
// unreadable .git) must surface, not be laundered into "not a repo". Git exits
// 128 for BOTH this and a genuine non-repo, so the distinction has to come from
// stderr — see gitProbeNotARepo.
func TestGitStatusForDirUnreadableRepoIsAnError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX env var to simulate foreign ownership")
	}
	dir := t.TempDir()
	initGitRepo(t, dir)
	// git's own test hook for the ownership check, so the failure is a real git
	// refusal rather than a stubbed message.
	t.Setenv("GIT_TEST_ASSUME_DIFFERENT_OWNER", "1")
	// t.Setenv restores GIT_TEST_ASSUME_DIFFERENT_OWNER, but the probe child
	// inherits the process env, so this covers the real code path.
	st, err := gitStatusForDir(dir)
	if err == nil {
		t.Fatalf("dubious-ownership repo returned status %+v with no error; want the git refusal", st)
	}
	if !strings.Contains(err.Error(), "ownership") {
		t.Fatalf("error %q does not carry git's reason", err)
	}
}

// The classification must not leak into the normal case: a healthy repository
// still answers 200 with IsRepo=true. A blanket "rev-parse failed" error would
// have broken this, which is why the healthy path is pinned alongside the two
// fault cases above.
func TestGitStatusForDirHealthyRepoUnaffected(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	writeFile(t, filepath.Join(dir, "a.txt"), "a\n")
	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "commit", "-m", "init")

	rec := httptest.NewRecorder()
	writeLocalGitStatus(rec, dir)
	if rec.Code != http.StatusOK {
		t.Fatalf("healthy repo status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	var st GitStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if !st.IsRepo {
		t.Fatalf("healthy repo reported IsRepo=false: %+v", st)
	}
}

// The user-visible half of the fix: a probe fault reaches the client as a 500
// carrying the cause, instead of a 200 with an empty status that the UI renders
// as "not a repository".
func TestGitStatusForDirFaultReachesClientAsError(t *testing.T) {
	gitMissingStub(t)
	rec := httptest.NewRecorder()
	writeLocalGitStatus(rec, t.TempDir())
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("missing git status = %d body=%s, want 500", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "git") {
		t.Fatalf("500 body does not name the cause: %s", rec.Body.String())
	}
}
