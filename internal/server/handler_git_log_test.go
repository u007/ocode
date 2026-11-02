package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// gitLogGet calls HandleGitLog with the given query and decodes the commits.
func gitLogGet(t *testing.T, h *Handler, query string) (int, []GitCommit) {
	t.Helper()
	w := httptest.NewRecorder()
	h.HandleGitLog(w, httptest.NewRequest("GET", "/api/git/log"+query, nil))
	if w.Code != http.StatusOK {
		return w.Code, nil
	}
	var commits []GitCommit
	if err := json.NewDecoder(w.Body).Decode(&commits); err != nil {
		t.Fatalf("decode log: %v\n%s", err, w.Body.String())
	}
	return w.Code, commits
}

func headHash(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(runOut(t, dir, "git", "rev-parse", "HEAD"))
}

func TestParseGitDecorations(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []GitRef
	}{
		{
			name: "checked-out branch with remote and tag",
			in:   "HEAD -> refs/heads/main, refs/remotes/origin/main, tag: refs/tags/v1",
			want: []GitRef{
				{Name: "main", Kind: "head", Current: true},
				{Name: "origin/main", Kind: "remote"},
				{Name: "v1", Kind: "tag"},
			},
		},
		{
			name: "detached HEAD",
			in:   "HEAD",
			want: []GitRef{{Name: "HEAD", Kind: "head", Current: true}},
		},
		{
			name: "plain local branch",
			in:   "refs/heads/feat",
			want: []GitRef{{Name: "feat", Kind: "head"}},
		},
		{
			name: "remote-tracking branch",
			in:   "refs/remotes/origin/feat",
			want: []GitRef{{Name: "origin/feat", Kind: "remote"}},
		},
		{
			name: "tag with a slash-free name",
			in:   "tag: refs/tags/v1.2",
			want: []GitRef{{Name: "v1.2", Kind: "tag"}},
		},
		{
			name: "stash and notes are skipped",
			in:   "refs/stash, refs/notes/commits",
			want: []GitRef{},
		},
		{
			name: "no decorations is an empty, non-nil list",
			in:   "",
			want: []GitRef{},
		},
		{
			name: "comma in branch, tag and remote names stays whole",
			in:   "HEAD -> refs/heads/a,b, tag: refs/tags/v,1, refs/remotes/origin/feat,x",
			want: []GitRef{
				{Name: "a,b", Kind: "head", Current: true},
				{Name: "v,1", Kind: "tag"},
				{Name: "origin/feat,x", Kind: "remote"},
			},
		},
		{
			name: "remote symbolic HEAD is skipped",
			in:   "HEAD -> refs/heads/main, refs/remotes/origin/main, refs/remotes/origin/HEAD",
			want: []GitRef{
				{Name: "main", Kind: "head", Current: true},
				{Name: "origin/main", Kind: "remote"},
			},
		},
		{
			name: "only remote symbolic HEAD is an empty list",
			in:   "refs/remotes/origin/HEAD",
			want: []GitRef{},
		},
		{
			name: "detached HEAD with a comma tag",
			in:   "HEAD, tag: refs/tags/v,1",
			want: []GitRef{
				{Name: "HEAD", Kind: "head", Current: true},
				{Name: "v,1", Kind: "tag"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseGitDecorations(tc.in)
			if got == nil {
				t.Fatal("parseGitDecorations returned nil; want a non-nil slice")
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseGitDecorations(%q) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestGitLogReturnsParentsAndRefs(t *testing.T) {
	dir := worktreeFixture(t)
	base := headHash(t, dir)
	run(t, dir, "git", "checkout", "-b", "side")
	writeFile(t, filepath.Join(dir, "side.txt"), "side\n")
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-m", "side work")
	run(t, dir, "git", "checkout", "main")
	writeFile(t, filepath.Join(dir, "main.txt"), "main\n")
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-m", "main work")
	mainTip := headHash(t, dir)
	run(t, dir, "git", "merge", "--no-ff", "-m", "merge side", "side")

	h := NewHandler()
	h.SetWorkDir(dir)
	code, commits := gitLogGet(t, h, "")
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if len(commits) != 4 {
		t.Fatalf("commits = %d, want 4", len(commits))
	}

	merge := commits[0]
	if merge.Message != "merge side" {
		t.Fatalf("newest = %q, want merge side", merge.Message)
	}
	if len(merge.Parents) != 2 || merge.Parents[0] != mainTip {
		t.Errorf("merge parents = %v, want [%s, <side>] first-parent first", merge.Parents, mainTip)
	}
	if !reflect.DeepEqual(merge.Refs, []GitRef{{Name: "main", Kind: "head", Current: true}}) {
		t.Errorf("merge refs = %+v, want only the checked-out main", merge.Refs)
	}

	root := commits[len(commits)-1]
	if root.Hash != base {
		t.Fatalf("oldest = %s, want the fixture commit %s", root.Hash, base)
	}
	if root.Parents == nil || len(root.Parents) != 0 {
		t.Errorf("root parents = %#v, want an empty non-nil slice", root.Parents)
	}
	if root.Refs == nil {
		t.Errorf("root refs is nil, want an empty non-nil slice")
	}
}

func TestGitLogAllIncludesOtherBranch(t *testing.T) {
	dir := worktreeFixture(t)
	run(t, dir, "git", "checkout", "-b", "only")
	writeFile(t, filepath.Join(dir, "only.txt"), "x\n")
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-m", "only here")
	run(t, dir, "git", "checkout", "main")

	h := NewHandler()
	h.SetWorkDir(dir)
	_, def := gitLogGet(t, h, "")
	if len(def) != 1 {
		t.Fatalf("default log = %d commits, want 1 (branch-only commit excluded)", len(def))
	}
	_, all := gitLogGet(t, h, "?all=1")
	if len(all) != 2 {
		t.Fatalf("all=1 log = %d commits, want 2", len(all))
	}
	if all[0].Message != "only here" {
		t.Errorf("all=1 newest = %q, want only here", all[0].Message)
	}
	var onlyRef bool
	for _, ref := range all[0].Refs {
		if ref == (GitRef{Name: "only", Kind: "head"}) {
			onlyRef = true
		}
	}
	if !onlyRef {
		t.Errorf("all=1 refs = %+v, want the only branch", all[0].Refs)
	}
}

// TestGitLogDecorationsFromRealGit checks the parse against what git itself
// writes for a comma-named branch and tag, and for the origin/HEAD symref that
// a clone leaves behind.
func TestGitLogDecorationsFromRealGit(t *testing.T) {
	dir := worktreeFixture(t)
	run(t, dir, "git", "branch", "a,b")
	run(t, dir, "git", "tag", "v,1")
	run(t, dir, "git", "update-ref", "refs/remotes/origin/main", "HEAD")
	run(t, dir, "git", "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")

	h := NewHandler()
	h.SetWorkDir(dir)
	_, commits := gitLogGet(t, h, "?all=1")
	if len(commits) != 1 {
		t.Fatalf("commits = %d, want 1", len(commits))
	}
	want := []GitRef{
		{Name: "main", Kind: "head", Current: true},
		{Name: "a,b", Kind: "head"},
		{Name: "v,1", Kind: "tag"},
		{Name: "origin/main", Kind: "remote"},
	}
	got := commits[0].Refs
	if len(got) != len(want) {
		t.Fatalf("refs = %+v, want %+v", got, want)
	}
	for _, w := range want {
		if !slices.Contains(got, w) {
			t.Errorf("refs = %+v, missing %+v", got, w)
		}
	}
}

func TestGitLogSkipPaginates(t *testing.T) {
	dir := worktreeFixture(t)
	for _, msg := range []string{"second", "third"} {
		writeFile(t, filepath.Join(dir, msg+".txt"), msg)
		run(t, dir, "git", "add", "-A")
		run(t, dir, "git", "commit", "-m", msg)
	}
	h := NewHandler()
	h.SetWorkDir(dir)

	cases := []struct {
		query string
		want  []string
	}{
		{"?limit=1", []string{"third"}},
		{"?limit=1&skip=1", []string{"second"}},
		{"?limit=1&skip=2", []string{"init"}},
		{"?limit=5&skip=3", []string{}},
	}
	for _, tc := range cases {
		_, got := gitLogGet(t, h, tc.query)
		msgs := make([]string, 0, len(got))
		for _, c := range got {
			msgs = append(msgs, c.Message)
		}
		if !reflect.DeepEqual(msgs, tc.want) {
			t.Errorf("%s: messages = %v, want %v", tc.query, msgs, tc.want)
		}
	}
}

func TestGitLogRejectsBadSkip(t *testing.T) {
	dir := worktreeFixture(t)
	h := NewHandler()
	h.SetWorkDir(dir)
	for _, query := range []string{"?skip=-1", "?skip=abc"} {
		code, _ := gitLogGet(t, h, query)
		if code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", query, code)
		}
	}
}
