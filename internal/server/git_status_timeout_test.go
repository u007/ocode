package server

import (
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
