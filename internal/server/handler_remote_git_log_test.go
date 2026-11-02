package server

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestRemoteGitLogParentsRefsAllSkipOverFakeSSH runs the remote log transport
// through the fake SSH script, so the argv built by gitLogArgs (--all, --skip,
// --decorate=full, --date-order) reaches a real git and its output is parsed.
func TestRemoteGitLogParentsRefsAllSkipOverFakeSSH(t *testing.T) {
	installFakeSSH(t)
	repo := t.TempDir()
	initGitRepo(t, repo)
	writeFile(t, filepath.Join(repo, "f.txt"), "one\n")
	run(t, repo, "git", "add", ".")
	run(t, repo, "git", "commit", "-m", "first")
	run(t, repo, "git", "checkout", "-b", "only")
	writeFile(t, filepath.Join(repo, "o.txt"), "x\n")
	run(t, repo, "git", "add", ".")
	run(t, repo, "git", "commit", "-m", "branch only")
	run(t, repo, "git", "checkout", "main")
	writeFile(t, filepath.Join(repo, "f.txt"), "two\n")
	run(t, repo, "git", "add", ".")
	run(t, repo, "git", "commit", "-m", "second")
	firstHash := strings.TrimSpace(runOut(t, repo, "git", "rev-parse", "HEAD~1"))

	h := newTestHandlerWithRemote(t, "ci.local", repo)
	base := "/api/git/log?host=ci.local&project=" + repo

	var def []GitCommit
	if w := getJSON(t, h, base, &def); w.Code != 200 {
		t.Fatalf("default status %d", w.Code)
	}
	if len(def) != 2 {
		t.Fatalf("default commits = %d, want 2 (branch-only commit excluded)", len(def))
	}
	top := def[0]
	if top.Message != "second" || len(top.Parents) != 1 || top.Parents[0] != firstHash {
		t.Errorf("top = %+v, want second with parent %s", top, firstHash)
	}
	if !reflect.DeepEqual(top.Refs, []GitRef{{Name: "main", Kind: "head", Current: true}}) {
		t.Errorf("top refs = %+v, want checked-out main", top.Refs)
	}
	root := def[1]
	if root.Parents == nil || len(root.Parents) != 0 || root.Refs == nil {
		t.Errorf("root parents/refs must be empty non-nil slices: %#v / %#v", root.Parents, root.Refs)
	}

	var all []GitCommit
	if w := getJSON(t, h, base+"&all=1", &all); w.Code != 200 {
		t.Fatalf("all status %d", w.Code)
	}
	if len(all) != 3 {
		t.Fatalf("all=1 commits = %d, want 3", len(all))
	}

	var paged []GitCommit
	if w := getJSON(t, h, base+"&limit=1&skip=1", &paged); w.Code != 200 {
		t.Fatalf("skip status %d", w.Code)
	}
	if len(paged) != 1 || paged[0].Message != "first" {
		t.Errorf("skip=1 page = %+v, want the first commit", paged)
	}
}
