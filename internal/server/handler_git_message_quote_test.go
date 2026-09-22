package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/remote"
)

// Regression: the remote git pipeline builds a shell command line
// (remoteGitCommand), unlike the local path which passes argv straight to
// exec. A free-form commit message therefore has to be shell-quoted, or the
// remote shell word-splits it and git parses the tail as pathspecs:
//
//	git commit -m review pagination
//	→ error: pathspec 'pagination' did not match any file(s) known to git
//
// These tests run through the fake-ssh shim (a real /bin/sh), so the
// word-splitting is exercised end-to-end.

func TestRemoteGitCommitShellQuotesMessage(t *testing.T) {
	installFakeSSH(t)
	repo := t.TempDir()
	initGitRepo(t, repo)
	writeFile(t, filepath.Join(repo, "a.txt"), "one\n")
	run(t, repo, "git", "add", "-A")

	h := newTestHandlerWithRemote(t, "ci.local", repo)
	msg := "review pagination changes"
	w := doJSON(t, h, h.HandleGitCommit, "POST",
		"/api/git/commit?host=ci.local&project="+urlQueryEscape(repo),
		gitActionRequest{Message: msg})
	if w.Code != http.StatusOK {
		t.Fatalf("commit status = %d, body %s", w.Code, w.Body.String())
	}
	if got := gitOutput(t, repo, "log", "-1", "--pretty=%s"); got != msg {
		t.Errorf("commit subject = %q, want %q", got, msg)
	}
}

// The same quoting also closes a remote command-injection hole: an unquoted
// message containing shell metacharacters executes on the host.
func TestRemoteGitCommitQuotesShellMetacharacters(t *testing.T) {
	installFakeSSH(t)
	repo := t.TempDir()
	initGitRepo(t, repo)
	writeFile(t, filepath.Join(repo, "a.txt"), "one\n")
	run(t, repo, "git", "add", "-A")

	marker := filepath.Join(repo, "PWNED")
	msg := "fix: pagination; touch " + marker

	h := newTestHandlerWithRemote(t, "ci.local", repo)
	w := doJSON(t, h, h.HandleGitCommit, "POST",
		"/api/git/commit?host=ci.local&project="+urlQueryEscape(repo),
		gitActionRequest{Message: msg})
	if w.Code != http.StatusOK {
		t.Fatalf("commit status = %d, body %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatalf("commit message executed on the host: %s was created", marker)
	}
	if got := gitOutput(t, repo, "log", "-1", "--pretty=%s"); got != msg {
		t.Errorf("commit subject = %q, want %q", got, msg)
	}
}

func TestRemoteGitStashShellQuotesMessage(t *testing.T) {
	installFakeSSH(t)
	repo := t.TempDir()
	initGitRepo(t, repo)
	writeFile(t, filepath.Join(repo, "a.txt"), "one\n")
	run(t, repo, "git", "add", "-A")
	run(t, repo, "git", "commit", "-m", "init")
	writeFile(t, filepath.Join(repo, "a.txt"), "changed\n")

	h := newTestHandlerWithRemote(t, "ci.local", repo)
	msg := "wip pagination review"
	w := doJSON(t, h, h.HandleGitStash, "POST",
		"/api/git/stash?host=ci.local&project="+urlQueryEscape(repo),
		gitActionRequest{Message: msg})
	if w.Code != http.StatusOK {
		t.Fatalf("stash status = %d, body %s", w.Code, w.Body.String())
	}
	if got := gitOutput(t, repo, "stash", "list", "--format=%s"); !strings.Contains(got, msg) {
		t.Errorf("stash subject = %q, want it to contain %q", got, msg)
	}
}

// The local transport passes argv directly to exec, so it must NOT quote:
// quoting there would embed literal quote characters in the message.
func TestStashPushArgsQuoteOnlyForRemote(t *testing.T) {
	req := gitActionRequest{Message: "wip pagination"}
	specs := []string{"a.txt"}

	local := stashPushArgs(req, specs)
	if len(local) < 4 || local[3] != "wip pagination" {
		t.Errorf("local stash argv = %v, want raw message at index 3", local)
	}

	remoteArgs := remoteStashPushArgs(req, specs)
	if len(remoteArgs) < 4 || remoteArgs[3] != remote.ShellQuote("wip pagination") {
		t.Errorf("remote stash argv = %v, want shell-quoted message at index 3", remoteArgs)
	}

	// Both variants still append the validated pathspecs identically.
	for _, argv := range [][]string{local, remoteArgs} {
		if argv[len(argv)-2] != "--" || argv[len(argv)-1] != "a.txt" {
			t.Errorf("argv = %v, want trailing -- a.txt", argv)
		}
	}
}

// Guard: a stash with no message must not gain an empty quoted argument.
func TestStashPushArgsOmitEmptyMessage(t *testing.T) {
	remoteArgs := remoteStashPushArgs(gitActionRequest{}, []string{"a.txt"})
	for _, a := range remoteArgs {
		if a == "" {
			t.Fatalf("empty message leaked into remote stash argv: %v", remoteArgs)
		}
	}
	if remoteArgs[0] != "stash" || remoteArgs[1] != "push" {
		t.Errorf("remote stash argv = %v, want stash push …", remoteArgs)
	}
}
