package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// canaryName is a legal filename that holds every shell metacharacter a
// remote command could be tricked by. If any endpoint splices it into the
// remote shell unquoted, the command substitutions and separators below
// create PWNED* files. The name must reach git as one literal pathspec.
const canaryName = "x;touch PWNED;$(touch PWNED2)`touch PWNED3`a'b\"&c#d.txt"

// payloadName is balanced shell syntax: if a call site splices it unquoted,
// the remote shell runs the substitutions and creates PWNED* files. The
// quote-bearing canaryName instead fails to parse when spliced raw, which
// proves quoting but not that nothing executes.
const payloadName = "x;touch PWNED;$(touch PWNED2)`touch PWNED3`&c#d.txt"

// canaryRepo builds a repo with a base commit. When committed is true the
// canary is tracked and at "v1"; otherwise it is an untracked file.
func canaryRepo(t *testing.T, name string, committed bool) string {
	t.Helper()
	installFakeSSH(t)
	repo := t.TempDir()
	initGitRepo(t, repo)
	writeFile(t, filepath.Join(repo, "base.txt"), "base\n")
	run(t, repo, "git", "add", "base.txt")
	if committed {
		writeFile(t, filepath.Join(repo, name), "v1\n")
		run(t, repo, "git", "add", name)
	}
	run(t, repo, "git", "commit", "-m", "init")
	if !committed {
		writeFile(t, filepath.Join(repo, name), "v1\n")
	}
	return repo
}

// assertNoCanaryFiles fails if an injected command created a PWNED* file in
// the repo (the remote command runs with cd into it) or in the test's working
// directory.
func assertNoCanaryFiles(t *testing.T, repo string) {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for _, dir := range []string{repo, cwd} {
		matches, err := filepath.Glob(filepath.Join(dir, "PWNED*"))
		if err != nil {
			t.Fatalf("glob: %v", err)
		}
		if len(matches) > 0 {
			t.Fatalf("shell injection: canary command created %v", matches)
		}
	}
}

func stagedNames(t *testing.T, repo string) []string {
	t.Helper()
	return strings.Split(gitOutput(t, repo, "diff", "--cached", "--name-only", "-z"), "\x00")
}

func postRemote(t *testing.T, h *Handler, handler func(*Handler, http.ResponseWriter, *http.Request), route, repo string, body any) *httptest.ResponseRecorder {
	t.Helper()
	r, w := call(route+"?host=ci.local&project="+url.QueryEscape(repo), body)
	handler(h, w, r)
	// Checked before the caller inspects the response: an injected command
	// can also break the request, and that must not hide the injection.
	assertNoCanaryFiles(t, repo)
	return w
}

func TestRemoteGitCanaryPathsStayLiteral(t *testing.T) {
	for _, name := range []string{canaryName, payloadName} {
		t.Run(name, func(t *testing.T) { canaryMatrix(t, name) })
	}
}

// canaryMatrix runs every path-taking endpoint against one canary filename.
func canaryMatrix(t *testing.T, canaryName string) {
	canary := []string{canaryName}

	t.Run("stage", func(t *testing.T) {
		repo := canaryRepo(t, canaryName, false)
		h := newTestHandlerWithRemote(t, "ci.local", repo)
		w := postRemote(t, h, (*Handler).HandleGitStage, "/api/git/stage", repo, map[string]any{"paths": canary})
		if w.Code != http.StatusOK {
			t.Fatalf("stage %d: %s", w.Code, w.Body.String())
		}
		if !slices.Contains(stagedNames(t, repo), canaryName) {
			t.Fatalf("canary not staged as a literal path: %q", stagedNames(t, repo))
		}
		assertNoCanaryFiles(t, repo)
	})

	t.Run("unstage", func(t *testing.T) {
		repo := canaryRepo(t, canaryName, true)
		writeFile(t, filepath.Join(repo, canaryName), "v2\n")
		run(t, repo, "git", "add", canaryName)
		h := newTestHandlerWithRemote(t, "ci.local", repo)
		w := postRemote(t, h, (*Handler).HandleGitUnstage, "/api/git/unstage", repo, map[string]any{"paths": canary})
		if w.Code != http.StatusOK {
			t.Fatalf("unstage %d: %s", w.Code, w.Body.String())
		}
		if slices.Contains(stagedNames(t, repo), canaryName) {
			t.Fatalf("canary still staged after unstage")
		}
		assertNoCanaryFiles(t, repo)
	})

	t.Run("discard", func(t *testing.T) {
		repo := canaryRepo(t, canaryName, true)
		writeFile(t, filepath.Join(repo, canaryName), "v2\n")
		h := newTestHandlerWithRemote(t, "ci.local", repo)
		w := postRemote(t, h, (*Handler).HandleGitDiscard, "/api/git/discard", repo, map[string]any{"paths": canary})
		if w.Code != http.StatusOK {
			t.Fatalf("discard %d: %s", w.Code, w.Body.String())
		}
		got, err := os.ReadFile(filepath.Join(repo, canaryName))
		if err != nil || string(got) != "v1\n" {
			t.Fatalf("canary not reverted to v1: %q err=%v", got, err)
		}
		assertNoCanaryFiles(t, repo)
	})

	t.Run("commit", func(t *testing.T) {
		repo := canaryRepo(t, canaryName, true)
		writeFile(t, filepath.Join(repo, canaryName), "v2\n")
		run(t, repo, "git", "add", canaryName)
		h := newTestHandlerWithRemote(t, "ci.local", repo)
		w := postRemote(t, h, (*Handler).HandleGitCommit, "/api/git/commit", repo,
			map[string]any{"message": "canary commit", "paths": canary})
		if w.Code != http.StatusOK {
			t.Fatalf("commit %d: %s", w.Code, w.Body.String())
		}
		files := strings.Split(gitOutput(t, repo, "show", "--name-only", "--format=", "-z", "HEAD"), "\x00")
		if !slices.Contains(files, canaryName) {
			t.Fatalf("HEAD commit does not contain the literal canary: %q", files)
		}
		assertNoCanaryFiles(t, repo)
	})

	t.Run("stash push and apply", func(t *testing.T) {
		repo := canaryRepo(t, canaryName, true)
		writeFile(t, filepath.Join(repo, canaryName), "v2\n")
		h := newTestHandlerWithRemote(t, "ci.local", repo)
		w := postRemote(t, h, (*Handler).HandleGitStash, "/api/git/stash", repo,
			map[string]any{"message": "canary stash", "paths": canary})
		if w.Code != http.StatusOK {
			t.Fatalf("stash push %d: %s", w.Code, w.Body.String())
		}
		if got, _ := os.ReadFile(filepath.Join(repo, canaryName)); string(got) != "v1\n" {
			t.Fatalf("stash push did not revert the canary: %q", got)
		}
		w = postRemote(t, h, (*Handler).HandleGitStashApply, "/api/git/stash/apply", repo,
			map[string]any{"index": 0, "paths": canary})
		if w.Code != http.StatusOK {
			t.Fatalf("stash apply %d: %s", w.Code, w.Body.String())
		}
		if got, _ := os.ReadFile(filepath.Join(repo, canaryName)); string(got) != "v2\n" {
			t.Fatalf("stash apply did not restore the canary: %q", got)
		}
		assertNoCanaryFiles(t, repo)
	})

	t.Run("hunk stage", func(t *testing.T) {
		repo := canaryRepo(t, canaryName, true)
		writeFile(t, filepath.Join(repo, canaryName), "v2\n")
		h := newTestHandlerWithRemote(t, "ci.local", repo)
		w := postRemote(t, h, (*Handler).HandleGitHunk, "/api/git/hunk", repo,
			map[string]any{"path": canaryName, "hunk_index": 0, "action": "stage"})
		if w.Code != http.StatusOK {
			t.Fatalf("hunk stage %d: %s", w.Code, w.Body.String())
		}
		if !slices.Contains(stagedNames(t, repo), canaryName) {
			t.Fatalf("hunk stage did not stage the literal canary: %q", stagedNames(t, repo))
		}
		assertNoCanaryFiles(t, repo)
	})

	t.Run("diff path filter", func(t *testing.T) {
		repo := canaryRepo(t, canaryName, true)
		writeFile(t, filepath.Join(repo, canaryName), "v2\n")
		h := newTestHandlerWithRemote(t, "ci.local", repo)
		// Called on the handler: the route wrapper is auth-only and is not
		// under test here.
		r := httptest.NewRequest(http.MethodGet,
			"/api/git/diff?host=ci.local&project="+url.QueryEscape(repo)+"&path="+url.QueryEscape(canaryName), nil)
		w := httptest.NewRecorder()
		h.HandleGitDiff(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("diff %d: %s", w.Code, w.Body.String())
		}
		var files []GitDiffFile
		if err := json.Unmarshal(w.Body.Bytes(), &files); err != nil {
			t.Fatalf("decode diff: %v: %s", err, w.Body.String())
		}
		if len(files) != 1 || files[0].Path != canaryName || !strings.Contains(files[0].Patch, "+v2") {
			t.Fatalf("path filter did not return the literal canary: %+v", files)
		}
		assertNoCanaryFiles(t, repo)
	})

	// Conflict resolution is not covered here: setting up a real merge
	// conflict over the fake-ssh harness is a separate fixture.
}
