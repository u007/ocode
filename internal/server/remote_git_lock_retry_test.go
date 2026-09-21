package server

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/remote"
)

// TestRemoteGitCommandDisablesOptionalLocks pins the remote half of the
// environment contract. The command built here runs git on the user's other
// machine, where the same rule applies: a plain `git status`/`git diff` probe
// refreshes the index and takes that repo's .git/index.lock, contending with
// whatever the user is running there. The env assignment must sit between the
// `cd ... &&` and the git invocation so it reaches git.
func TestRemoteGitCommandDisablesOptionalLocks(t *testing.T) {
	got := remoteGitCommand("/repo", "status", "--porcelain")
	if !strings.Contains(got, "&& GIT_OPTIONAL_LOCKS=0 git status --porcelain") {
		t.Fatalf("remoteGitCommand = %q, want the env assignment immediately before `git`", got)
	}
	if strings.Contains(got, "&& git ") {
		t.Fatalf("remoteGitCommand = %q still invokes git without GIT_OPTIONAL_LOCKS=0", got)
	}
}

// stubGitFailsAddTimes prepends a `git` wrapper to PATH that fails the first n
// `add` invocations with git's real index-lock error and delegates everything
// else — and later adds — to the real git. It returns the path of the counter
// file recording how many `add`s were attempted, so a test can prove the retry
// happened instead of inferring it from a timing window (a transient
// .git/index.lock released on a timer is racy here: the remote path spends long
// enough in sh/ssh startup that the lock can be gone before `git add` runs at
// all, which would let the test pass with the retry removed).
func stubGitFailsAddTimes(t *testing.T, n int) string {
	t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("look up the real git: %v", err)
	}
	dir := t.TempDir()
	counter := filepath.Join(dir, "add-attempts")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"add\" ]; then\n" +
		"  count=0\n" +
		"  [ -f " + strconv.Quote(counter) + " ] && count=$(cat " + strconv.Quote(counter) + ")\n" +
		"  count=$((count+1))\n" +
		"  echo \"$count\" > " + strconv.Quote(counter) + "\n" +
		"  if [ \"$count\" -le " + strconv.Itoa(n) + " ]; then\n" +
		"    echo \"fatal: Unable to create '/repo/.git/index.lock': File exists.\" >&2\n" +
		"    echo \"\" >&2\n" +
		"    echo \"Another git process seems to be running in this repository, or the lock file may be stale\" >&2\n" +
		"    exit 128\n" +
		"  fi\n" +
		"fi\n" +
		"exec " + strconv.Quote(realGit) + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return counter
}

// TestRemoteGitMutationRidesOutTransientIndexLock is the remote counterpart of
// TestGitStageRidesOutTransientIndexLock. The fake-ssh shim runs the remote
// command on this machine, and the git wrapper fails the first two `add`s with
// git's own lock message — so success and "three attempts" both prove the
// remote mutation retried on the host-side lock rather than giving up.
func TestRemoteGitMutationRidesOutTransientIndexLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell stub for git")
	}

	installFakeSSH(t)
	counter := stubGitFailsAddTimes(t, 2)
	repo := t.TempDir()
	initGitRepo(t, repo)
	writeFile(t, filepath.Join(repo, "a.txt"), "hello\n")

	target, err := remote.ParseTarget("ci.local")
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}
	rw := remoteWork{Target: target, Path: repo}

	if err := remoteGitMutation(context.Background(), rw, remoteGitCommand(repo, "add", "--", "a.txt")); err != nil {
		t.Fatalf("remote git add did not ride out a held index lock: %v", err)
	}
	attempts, err := os.ReadFile(counter)
	if err != nil {
		t.Fatalf("read add-attempt counter: %v", err)
	}
	if strings.TrimSpace(string(attempts)) != "3" {
		t.Fatalf("add attempted %s time(s), want 3 (two lock failures then success)", strings.TrimSpace(string(attempts)))
	}
	if out := gitOutput(t, repo, "diff", "--cached", "--name-only"); out != "a.txt" {
		t.Fatalf("staged files = %q, want a.txt", out)
	}
}

// TestRemoteGitStageSurfacesStaleLockAfterRetries is deterministic where a
// transient-lock race is not: the lock is never released, so the request must
// fail — and the body must carry ocode's "attempted this git command" suffix,
// which only the retrying path can produce. It therefore pins both that
// HandleGitStage's remote branch routes through remoteGitMutation and that the
// user still gets git's own message naming the lock file.
func TestRemoteGitStageSurfacesStaleLockAfterRetries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("relies on POSIX lock-file timing")
	}

	installFakeSSH(t)
	repo := t.TempDir()
	initGitRepo(t, repo)
	writeFile(t, filepath.Join(repo, "a.txt"), "hello\n")
	h := newTestHandlerWithRemote(t, "ci.local", repo)

	lock := filepath.Join(repo, ".git", "index.lock")
	if err := os.WriteFile(lock, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(lock)

	target := "/api/git/stage?host=ci.local&project=" + url.QueryEscape(repo)
	r, w := call(target, map[string]any{"paths": []string{"a.txt"}})
	h.HandleGitStage(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("remote staging with a held index.lock returned %d, want 500: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "index.lock") {
		t.Fatalf("response lost git's own message naming the lock file: %s", body)
	}
	if !strings.Contains(body, "attempted this git command") {
		t.Fatalf("response shows no retry happened, so the remote mutation bypasses remoteGitMutation: %s", body)
	}
}
