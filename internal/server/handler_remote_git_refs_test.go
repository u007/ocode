package server

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/remote"
)

// TestRemoteGitRefRunRidesOutTransientIndexLock pins the remote half of the
// ref-action lock retry. The remote exit error is only "exit status 128"; git's
// "Unable to create ... index.lock" text lives in the captured output. Without
// folding that output into the error, gitexec.LockHeld never matches and the
// retry never fires. The stub command fails twice with git's real lock text and
// then succeeds, so success plus three attempts proves the retry ran.
func TestRemoteGitRefRunRidesOutTransientIndexLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell command")
	}

	installFakeSSH(t)
	target, err := remote.ParseTarget("ci.local")
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}
	rw := remoteWork{Target: target, Path: t.TempDir()}

	counter := filepath.Join(t.TempDir(), "attempts")
	// The brace group lets the appended " 2>&1" capture the stderr lines too,
	// as it does for the real command (a git invocation at the end of the chain).
	command := "{ n=$(cat " + strconv.Quote(counter) + " 2>/dev/null || echo 0); " +
		"n=$((n+1)); echo $n > " + strconv.Quote(counter) + "; " +
		"if [ $n -le 2 ]; then " +
		"echo \"fatal: Unable to create '/repo/.git/index.lock': File exists.\" >&2; " +
		"echo \"Another git process seems to be running in this repository, or the lock file may be stale\" >&2; " +
		"exit 128; fi; echo updated; }"

	out, err := remoteGitRefRun(context.Background(), rw, command)
	if err != nil {
		t.Fatalf("remote ref action did not ride out a held index lock: %v", err)
	}
	if out != "updated" {
		t.Fatalf("output = %q, want %q", out, "updated")
	}
	attempts, err := os.ReadFile(counter)
	if err != nil {
		t.Fatalf("read attempt counter: %v", err)
	}
	if got := strings.TrimSpace(string(attempts)); got != "3" {
		t.Fatalf("command ran %s time(s), want 3 (two lock failures then success)", got)
	}
}
