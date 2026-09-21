package server

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestGitRunInDirDisablesOptionalLocks pins the environment contract for
// ocode's git children. Without GIT_OPTIONAL_LOCKS=0, ocode's own probes sit on
// the other side of the field bug: `git status`/`git diff` refresh the index
// and take .git/index.lock as a side effect, so the git-status emitter (every
// viewed project, every 10s) and the file-tree badge probe contend with the
// user's own git commands — which then fail with "Unable to create
// '<repo>/.git/index.lock': File exists".
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

// TestGitStageRidesOutTransientIndexLock is the field bug as a regression test:
// something else holds .git/index.lock for a moment (the user's terminal, an
// editor, another ocode tab) while the user clicks Stage in the Git tab. The
// request must succeed once the holder releases the lock, instead of returning
// "git add failed: exit status 128: fatal: Unable to create ... index.lock".
func TestGitStageRidesOutTransientIndexLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("relies on POSIX lock-file timing")
	}

	h := newGitTestHandler(t, true)
	dir := h.workDir
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	lock := filepath.Join(dir, ".git", "index.lock")
	if err := os.WriteFile(lock, []byte("held by another git process"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Release the lock well inside the retry window: the first attempts must
	// fail, a later one must find the lock gone and stage the file.
	released := make(chan struct{})
	go func() {
		defer close(released)
		time.Sleep(120 * time.Millisecond)
		_ = os.Remove(lock)
	}()

	r, w := call("/api/git/stage", map[string]any{"paths": []string{"a.txt"}})
	h.HandleGitStage(w, r)
	<-released

	if w.Code != http.StatusOK {
		t.Fatalf("staging with a transient index.lock holder returned %d, want 200: %s", w.Code, w.Body.String())
	}
	staged := false
	for _, f := range gitStatusForDir(dir).StagedFiles {
		if f == "a.txt" {
			staged = true
		}
	}
	if !staged {
		t.Fatalf("a.txt was not staged after the lock holder released it")
	}
}

// TestGitStageSurfacesStaleLockAfterRetries covers the case a retry cannot fix:
// the lock file is still there after ocode has waited. The user must then get
// both halves of the story — git's own message (which names the lock path) and
// ocode's note that it already retried, so a lock still present is
// distinguishable from a transient one at a glance.
func TestGitStageSurfacesStaleLockAfterRetries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("relies on POSIX lock-file timing")
	}

	h := newGitTestHandler(t, true)
	dir := h.workDir
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(dir, ".git", "index.lock")
	if err := os.WriteFile(lock, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(lock)

	r, w := call("/api/git/stage", map[string]any{"paths": []string{"a.txt"}})
	h.HandleGitStage(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("a persistently held index.lock returned %d, want 500: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "git add failed") {
		t.Fatalf("response lost the action context: %s", body)
	}
	if !strings.Contains(body, "index.lock") {
		t.Fatalf("response lost git's own message naming the lock file: %s", body)
	}
	if !strings.Contains(body, "attempted this git command") {
		t.Fatalf("response does not say ocode already retried, so a stale lock reads as a live one: %s", body)
	}
}
