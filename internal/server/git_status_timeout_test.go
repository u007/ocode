package server

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestGitStatusForDirBoundedWhenGitHangs proves one project's wedged git can
// never pin the caller forever. Before the fix gitStatusForDir ran
// exec.Command("git", ...) with no deadline, so a repo whose git blocks (stalled
// network mount, index.lock held by a dead process) hung its own request and,
// because the git-status emitter ran projects sequentially on one goroutine,
// stalled every other project's git_status too. The status must come back
// within the bound and report an empty, non-repo result.
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

	done := make(chan GitStatus, 1)
	go func() { done <- gitStatusForDir(dir) }()

	select {
	case st := <-done:
		if st.IsRepo || st.HasChanges || st.Branch != "" || len(st.StagedFiles) != 0 || len(st.ChangedFiles) != 0 {
			t.Fatalf("hung git returned %+v, want an empty non-repo status", st)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("gitStatusForDir did not return within 5s — a hung git still wedges the caller")
	}
}
