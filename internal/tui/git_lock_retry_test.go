package tui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestGitRunInDirDisablesOptionalLocks pins the TUI half of the environment
// contract: git children the TUI spawns must set GIT_OPTIONAL_LOCKS=0, because
// the file-tree badge ticker and the git tab run `git status`/`git diff`
// constantly and those refresh the index (taking .git/index.lock) unless told
// not to.
func TestGitRunInDirDisablesOptionalLocks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell stub for git")
	}

	dir := t.TempDir()
	stub := filepath.Join(dir, "git-stub")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nprintf %s \"$GIT_OPTIONAL_LOCKS\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	oldBin := gitBinary
	gitBinary = stub
	defer func() { gitBinary = oldBin }()

	got, err := gitRunInDir(dir, "status")
	if err != nil {
		t.Fatalf("gitRunInDir via stub: %v", err)
	}
	if got != "0" {
		t.Fatalf("GIT_OPTIONAL_LOCKS=%q in the git child env, want 0", got)
	}
}

// TestGitRunInDirRidesOutTransientIndexLock matches the server-side regression
// test for the TUI's stage path: a git command that loses the .git/index.lock
// race to a short-lived holder must succeed once the holder releases it,
// instead of failing with "Unable to create ... index.lock".
func TestGitRunInDirRidesOutTransientIndexLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("relies on POSIX lock-file timing")
	}

	dir := initGitRepoForPathSeparatorTest(t)
	writeFileForGitTest(t, dir, "a.txt", "hello\n")

	lock := filepath.Join(dir, ".git", "index.lock")
	if err := os.WriteFile(lock, []byte("held by another git process"), 0o644); err != nil {
		t.Fatal(err)
	}
	released := make(chan struct{})
	go func() {
		defer close(released)
		time.Sleep(120 * time.Millisecond)
		_ = os.Remove(lock)
	}()

	_, err := gitRunInDir(dir, "add", "--", "a.txt")
	<-released
	if err != nil {
		t.Fatalf("git add did not ride out a transient index.lock: %v", err)
	}
	if staged := gitOutputForTest(t, dir, "diff", "--cached", "--name-only"); staged != "a.txt" {
		t.Fatalf("staged files = %q, want a.txt", staged)
	}
}

// TestGitRunInDirReportsExhaustedLockRetries keeps the terminal error honest:
// when the lock never frees, git's own message survives (it names the lock
// path) and the error says ocode already retried, which is what tells a user
// the remaining lock file is stale rather than live.
func TestGitRunInDirReportsExhaustedLockRetries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("relies on POSIX lock-file timing")
	}

	dir := initGitRepoForPathSeparatorTest(t)
	writeFileForGitTest(t, dir, "a.txt", "hello\n")

	lock := filepath.Join(dir, ".git", "index.lock")
	if err := os.WriteFile(lock, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(lock)

	_, err := gitRunInDir(dir, "add", "--", "a.txt")
	if err == nil {
		t.Fatal("expected an error while the index lock stays held")
	}
	if !strings.Contains(err.Error(), "index.lock") {
		t.Fatalf("error dropped git's own message naming the lock file: %v", err)
	}
	if !strings.Contains(err.Error(), "attempted this git command") {
		t.Fatalf("error does not say ocode already retried, so a stale lock reads as a live one: %v", err)
	}
}
