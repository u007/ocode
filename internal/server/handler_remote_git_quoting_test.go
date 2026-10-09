package server

import (
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Remote listings read `git status --porcelain` without -z, so git C-quotes
// a non-ASCII name. The remote pipeline must decode it before the name is
// listed or handed back to a stage request.

func TestRemoteGitStatusListsNonASCIIPathsUnquoted(t *testing.T) {
	installFakeSSH(t)
	repo := setupNonASCIIRepo(t)

	h := newTestHandlerWithRemote(t, "ci.local", repo)
	var status GitStatus
	w := getJSON(t, h, "/api/git/status?host=ci.local&project="+repo, &status)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	for _, want := range []string{nonASCIITracked, nonASCIIUntracked} {
		if !slices.Contains(status.ChangedFiles, want) {
			t.Fatalf("ChangedFiles missing %q (raw name), got %q", want, status.ChangedFiles)
		}
	}
}

func TestRemoteGitWorkspaceListsNonASCIIPathsUnquoted(t *testing.T) {
	installFakeSSH(t)
	repo := setupNonASCIIRepo(t)

	h := newTestHandlerWithRemote(t, "ci.local", repo)
	var ws GitWorkspace
	w := getJSON(t, h, "/api/git/workspace?host=ci.local&project="+repo, &ws)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var untracked *GitDiffFile
	foundTracked := false
	for i, f := range ws.Unstaged {
		if f.Path == nonASCIITracked && f.Patch != "" {
			foundTracked = true
		}
		if f.Path == nonASCIIUntracked {
			untracked = &ws.Unstaged[i]
		}
	}
	if !foundTracked {
		t.Fatalf("Unstaged missing modified %q with a patch: %+v", nonASCIITracked, ws.Unstaged)
	}
	if untracked == nil || untracked.Status != "untracked" || untracked.Patch == "" {
		t.Fatalf("Unstaged missing untracked %q with a patch: %+v", nonASCIIUntracked, ws.Unstaged)
	}
}

func TestRemoteGitStageNonASCIIUntrackedPath(t *testing.T) {
	installFakeSSH(t)
	repo := setupNonASCIIRepo(t)

	h := newTestHandlerWithRemote(t, "ci.local", repo)
	r, w := call("/api/git/stage?host=ci.local&project="+repo, map[string]any{"paths": []string{nonASCIIUntracked}})
	h.HandleGitStage(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 staging %q, got %d: %s", nonASCIIUntracked, w.Code, w.Body.String())
	}
	staged := strings.Split(gitOutput(t, repo, "diff", "--cached", "--name-only", "-z"), "\x00")
	if !slices.Contains(staged, nonASCIIUntracked) {
		t.Fatalf("expected %q staged on the remote repo", nonASCIIUntracked)
	}
}

// A name with a double quote used to be skipped by the remote listing. It is
// a legal git name and a safe one once quoted, so it must be listed and
// stageable like any other file.
func TestRemoteGitWorkspaceListsAndStagesQuotedName(t *testing.T) {
	installFakeSSH(t)
	repo := setupNonASCIIRepo(t)
	const quoted = `say "hi".txt`
	if err := os.WriteFile(filepath.Join(repo, quoted), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := newTestHandlerWithRemote(t, "ci.local", repo)
	var ws GitWorkspace
	w := getJSON(t, h, "/api/git/workspace?host=ci.local&project="+repo, &ws)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if !slices.ContainsFunc(ws.Unstaged, func(f GitDiffFile) bool { return f.Path == quoted }) {
		t.Fatalf("workspace missing %q: %+v", quoted, ws.Unstaged)
	}

	r, sw := call("/api/git/stage?host=ci.local&project="+repo, map[string]any{"paths": []string{quoted}})
	h.HandleGitStage(sw, r)
	if sw.Code != http.StatusOK {
		t.Fatalf("expected 200 staging %q, got %d: %s", quoted, sw.Code, sw.Body.String())
	}
	staged := strings.Split(gitOutput(t, repo, "diff", "--cached", "--name-only", "-z"), "\x00")
	if !slices.Contains(staged, quoted) {
		t.Fatalf("expected %q staged, got %q", quoted, staged)
	}
}

func TestRemoteGitSpecValidator(t *testing.T) {
	allowed := []string{
		"a.txt",
		"src/main.go",
		"Technical Lead & AI Specialist.pdf",
		"say \"hi\".txt",
		"it's.txt",
		"a;b.txt",
		"$HOME.txt",
		"x|y<z>.txt",
		"bang!.txt",
		"(paren){brace}.txt",
		"hash#.txt",
		"back\\slash.txt",
		"mid:colon.txt",
		"Résumé — draft.md",
	}
	for _, p := range allowed {
		if _, err := remoteGitSpec(p); err != nil {
			t.Errorf("remoteGitSpec(%q) = %v, want accepted", p, err)
		}
	}
	refused := []string{
		"",
		"/abs/path",
		"line\nbreak",
		"tab\tname",
		"star*.txt",
		"question?.txt",
		"class[1].txt",
		":!exclude",
		":(literal)x",
		"C:foo",
		"c:\\foo",
	}
	for _, p := range refused {
		if _, err := remoteGitSpec(p); err == nil {
			t.Errorf("remoteGitSpec(%q) accepted, want error", p)
		}
	}
}
