package server

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// gitBranchesFor reads GET /api/git/branches against the handler's work dir.
func gitBranchesFor(t *testing.T, h *Handler) GitBranches {
	t.Helper()
	var got GitBranches
	if rec := getJSON(t, h, "/api/git/branches", &got); rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	return got
}

func findBranch(list []GitBranch, name string) (GitBranch, bool) {
	for _, b := range list {
		if b.Name == name {
			return b, true
		}
	}
	return GitBranch{}, false
}

func TestGitBranchesListsLocalRemoteAndTags(t *testing.T) {
	dir := worktreeFixture(t)
	run(t, dir, "git", "branch", "feature")
	run(t, dir, "git", "tag", "v1")

	// A remote-tracking ref without any network: the fixture repo points at
	// itself and gets an origin/main plus the symbolic origin/HEAD pointer,
	// which the listing must skip.
	run(t, dir, "git", "remote", "add", "origin", dir)
	head := headHash(t, dir)
	run(t, dir, "git", "update-ref", "refs/remotes/origin/main", head)
	run(t, dir, "git", "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	run(t, dir, "git", "branch", "--set-upstream-to=origin/main", "main")

	// One commit ahead of the upstream we just pinned.
	writeFile(t, filepath.Join(dir, "second.txt"), "two\n")
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-m", "second")

	h := NewHandler()
	h.SetWorkDir(dir)
	got := gitBranchesFor(t, h)

	main, ok := findBranch(got.Local, "main")
	if !ok {
		t.Fatalf("local branches = %+v, want main", got.Local)
	}
	if !main.Current {
		t.Error("main must be marked current")
	}
	if main.Kind != "local" || main.RefName != "refs/heads/main" {
		t.Errorf("main = %+v, want kind local and refs/heads/main", main)
	}
	if main.Upstream != "origin/main" {
		t.Errorf("upstream = %q, want origin/main", main.Upstream)
	}
	if main.Ahead != 1 || main.Behind != 0 {
		t.Errorf("ahead/behind = %d/%d, want 1/0", main.Ahead, main.Behind)
	}
	if main.Subject != "second" || main.Date == "" {
		t.Errorf("subject/date = %q/%q, want second and a timestamp", main.Subject, main.Date)
	}

	feature, ok := findBranch(got.Local, "feature")
	if !ok {
		t.Fatalf("local branches = %+v, want feature", got.Local)
	}
	if feature.Current {
		t.Error("feature must not be current")
	}

	if _, ok := findBranch(got.Remote, "origin/main"); !ok {
		t.Fatalf("remote branches = %+v, want origin/main", got.Remote)
	}
	if _, ok := findBranch(got.Remote, "origin/HEAD"); ok {
		t.Errorf("remote branches = %+v, must skip origin/HEAD", got.Remote)
	}
	if remote, _ := findBranch(got.Remote, "origin/main"); remote.Remote != "origin" {
		t.Errorf("remote field = %q, want origin", remote.Remote)
	}

	if tag, ok := findBranch(got.Tags, "v1"); !ok || tag.Kind != "tag" {
		t.Fatalf("tags = %+v, want v1 as a tag", got.Tags)
	}
}

func TestGitBranchesEmptyOutsideRepository(t *testing.T) {
	h := NewHandler()
	h.SetWorkDir(t.TempDir())
	got := gitBranchesFor(t, h)
	if got.Local == nil || got.Remote == nil || got.Tags == nil {
		t.Fatalf("lists must be empty slices, not nil: %+v", got)
	}
	if len(got.Local)+len(got.Remote)+len(got.Tags) != 0 {
		t.Errorf("got %+v, want no refs", got)
	}
}

func TestParseUpstreamTrack(t *testing.T) {
	cases := []struct {
		in     string
		ahead  int
		behind int
		gone   bool
	}{
		{"", 0, 0, false},
		{"[ahead 3]", 3, 0, false},
		{"[behind 2]", 0, 2, false},
		{"[ahead 1, behind 4]", 1, 4, false},
		{"[gone]", 0, 0, true},
	}
	for _, tc := range cases {
		ahead, behind, gone := parseUpstreamTrack(tc.in)
		if ahead != tc.ahead || behind != tc.behind || gone != tc.gone {
			t.Errorf("parseUpstreamTrack(%q) = %d,%d,%v want %d,%d,%v",
				tc.in, ahead, behind, gone, tc.ahead, tc.behind, tc.gone)
		}
	}
}

func TestGitRefActionNewBranchActions(t *testing.T) {
	dir := worktreeFixture(t)
	h := NewHandler()
	h.SetWorkDir(dir)

	run(t, dir, "git", "branch", "old")
	if rec := refActionPost(t, h, "", map[string]string{"action": "branch-rename", "target": "old", "name": "renamed"}); rec.Code != http.StatusOK {
		t.Fatalf("branch-rename: status %d: %s", rec.Code, rec.Body.String())
	}
	if !gitRefResolves(dir, "refs/heads/renamed") || gitRefResolves(dir, "refs/heads/old") {
		t.Error("branch-rename did not move refs/heads/old to refs/heads/renamed")
	}

	// An unmerged branch cannot be deleted without force.
	run(t, dir, "git", "checkout", "-b", "unmerged")
	writeFile(t, filepath.Join(dir, "u.txt"), "u\n")
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-m", "unmerged work")
	run(t, dir, "git", "checkout", "main")
	if rec := refActionPost(t, h, "", map[string]string{"action": "branch-delete", "target": "unmerged"}); rec.Code != http.StatusConflict {
		t.Fatalf("branch-delete of an unmerged branch: status %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if rec := refActionPost(t, h, "", map[string]string{"action": "branch-force-delete", "target": "unmerged"}); rec.Code != http.StatusOK {
		t.Fatalf("branch-force-delete: status %d: %s", rec.Code, rec.Body.String())
	}
	if gitRefResolves(dir, "refs/heads/unmerged") {
		t.Error("branch-force-delete left refs/heads/unmerged")
	}

	run(t, dir, "git", "tag", "v1")
	if rec := refActionPost(t, h, "", map[string]string{"action": "tag-delete", "target": "v1"}); rec.Code != http.StatusOK {
		t.Fatalf("tag-delete: status %d: %s", rec.Code, rec.Body.String())
	}
	if gitRefResolves(dir, "refs/tags/v1") {
		t.Error("tag-delete left refs/tags/v1")
	}
}

func TestGitRefActionSetAndUnsetUpstream(t *testing.T) {
	dir := worktreeFixture(t)
	h := NewHandler()
	h.SetWorkDir(dir)
	head := headHash(t, dir)
	// git refuses to track a remote-tracking ref whose remote is not
	// configured, so the fixture needs the remote entry as well as the ref.
	run(t, dir, "git", "remote", "add", "origin", dir)
	run(t, dir, "git", "update-ref", "refs/remotes/origin/main", head)

	if rec := refActionPost(t, h, "", map[string]string{"action": "set-upstream", "target": "main", "name": "origin/main"}); rec.Code != http.StatusOK {
		t.Fatalf("set-upstream: status %d: %s", rec.Code, rec.Body.String())
	}
	if got := strings.TrimSpace(runOut(t, dir, "git", "rev-parse", "--abbrev-ref", "main@{upstream}")); got != "origin/main" {
		t.Fatalf("upstream = %q, want origin/main", got)
	}
	if rec := refActionPost(t, h, "", map[string]string{"action": "unset-upstream", "target": "main"}); rec.Code != http.StatusOK {
		t.Fatalf("unset-upstream: status %d: %s", rec.Code, rec.Body.String())
	}
	out, err := gitRunInDir(dir, "config", "--get", "branch.main.merge")
	if err == nil && strings.TrimSpace(out) != "" {
		t.Errorf("branch.main.merge = %q after unset-upstream", out)
	}
}

func TestGitRefActionRebase(t *testing.T) {
	dir := worktreeFixture(t)
	h := NewHandler()
	h.SetWorkDir(dir)

	run(t, dir, "git", "checkout", "-b", "feature")
	writeFile(t, filepath.Join(dir, "f.txt"), "f\n")
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-m", "feature work")

	run(t, dir, "git", "checkout", "main")
	writeFile(t, filepath.Join(dir, "m.txt"), "m\n")
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-m", "main work")
	run(t, dir, "git", "checkout", "feature")

	if rec := refActionPost(t, h, "", map[string]string{"action": "rebase", "target": "main"}); rec.Code != http.StatusOK {
		t.Fatalf("rebase: status %d: %s", rec.Code, rec.Body.String())
	}
	// The test succeeds only when main is now an ancestor of feature.
	if _, err := gitRunInDir(dir, "merge-base", "--is-ancestor", "main", "feature"); err != nil {
		t.Errorf("main is not an ancestor of feature after rebase: %v", err)
	}
}

func TestGitRefActionNewActionValidation(t *testing.T) {
	dir := worktreeFixture(t)
	h := NewHandler()
	h.SetWorkDir(dir)
	run(t, dir, "git", "branch", "topic")

	cases := []struct {
		name string
		body map[string]string
		want string
	}{
		{"tag-delete with a branch", map[string]string{"action": "tag-delete", "target": "main"}, "needs a tag name"},
		{"rename without a name", map[string]string{"action": "branch-rename", "target": "topic"}, "name is required"},
		{"set-upstream to a missing remote ref", map[string]string{"action": "set-upstream", "target": "topic", "name": "nosuch/main"}, "not a remote-tracking branch"},
		{"force delete with a hash", map[string]string{"action": "branch-force-delete", "target": headHash(t, dir)}, "needs a branch name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := refActionPost(t, h, "", tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status %d, want 400: %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.want) {
				t.Errorf("body %q does not contain %q", rec.Body.String(), tc.want)
			}
		})
	}
}

func TestGitPushArgs(t *testing.T) {
	cases := []struct {
		name string
		req  gitPushRequest
		want []string
		err  string
	}{
		{"default", gitPushRequest{}, []string{"push"}, ""},
		{"force", gitPushRequest{Force: true}, []string{"push", "--force-with-lease"}, ""},
		{"branch and upstream", gitPushRequest{Remote: "origin", Branch: "feat", SetUpstream: true},
			[]string{"push", "--set-upstream", "origin", "feat"}, ""},
		{"delete remote branch", gitPushRequest{Remote: "origin", Branch: "feat", Delete: true},
			[]string{"push", "origin", "--delete", "feat"}, ""},
		{"delete without branch", gitPushRequest{Remote: "origin", Delete: true}, nil, "needs remote and branch"},
		{"option-looking branch", gitPushRequest{Branch: "--all"}, nil, "invalid branch"},
		{"upstream without remote", gitPushRequest{SetUpstream: true}, nil, "needs a remote"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := gitPushArgs(tc.req)
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("err = %v, want %q", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if strings.Join(got, " ") != strings.Join(tc.want, " ") {
				t.Errorf("args = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGitPushRejectsUnknownRemote(t *testing.T) {
	dir := worktreeFixture(t)
	h := NewHandler()
	h.SetWorkDir(dir)
	rec := worktreePost(t, h, "/api/git/push", gitPushRequest{Remote: "nope", Branch: "main"}, h.HandleGitPush)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "unknown remote") {
		t.Errorf("body %q does not name the remote", rec.Body.String())
	}
}

// TestRemoteGitBranchesOverFakeSSH pins the remote transport: the
// for-each-ref format holds parentheses, which must be quoted before the
// remote shell sees them.
func TestRemoteGitBranchesOverFakeSSH(t *testing.T) {
	installFakeSSH(t)
	repo := t.TempDir()
	initGitRepo(t, repo)
	writeFile(t, filepath.Join(repo, "f.txt"), "one\n")
	run(t, repo, "git", "add", "-A")
	run(t, repo, "git", "commit", "-m", "first")
	run(t, repo, "git", "branch", "feature")
	run(t, repo, "git", "tag", "v1")
	head := strings.TrimSpace(runOut(t, repo, "git", "rev-parse", "HEAD"))
	run(t, repo, "git", "update-ref", "refs/remotes/origin/main", head)

	h := newTestHandlerWithRemote(t, "ci.local", repo)
	var got GitBranches
	url := "/api/git/branches?host=ci.local&project=" + repo
	if rec := getJSON(t, h, url, &got); rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if _, ok := findBranch(got.Local, "main"); !ok {
		t.Fatalf("local = %+v, want main", got.Local)
	}
	if _, ok := findBranch(got.Remote, "origin/main"); !ok {
		t.Fatalf("remote = %+v, want origin/main", got.Remote)
	}
	if _, ok := findBranch(got.Tags, "v1"); !ok {
		t.Fatalf("tags = %+v, want v1", got.Tags)
	}
}
