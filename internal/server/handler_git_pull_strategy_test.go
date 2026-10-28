package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

// makeDivergentRepo builds a local repo whose branch and upstream have both
// moved, i.e. exactly the state where a bare `git pull` dies with
// "fatal: Need to specify how to reconcile divergent branches".
//
// The test environment disables every user/system git config so the machine
// running the suite cannot supply a pull.rebase/pull.ff answer and mask the
// bug. Before the fix, every case here failed with git's reconcile error.
func makeDivergentRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)

	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	runGit(t, root, "init", "--bare", remote)
	runGit(t, remote, "symbolic-ref", "HEAD", "refs/heads/main")

	setUser := func(dir string) {
		t.Helper()
		runGit(t, dir, "config", "user.email", "test@example.com")
		runGit(t, dir, "config", "user.name", "Test")
	}
	commitFile := func(dir, name, content, message string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		runGit(t, dir, "add", name)
		runGit(t, dir, "commit", "-m", message)
	}

	seed := filepath.Join(root, "seed")
	runGit(t, root, "clone", remote, seed)
	setUser(seed)
	commitFile(seed, "base.txt", "base", "base")
	runGit(t, seed, "push", "origin", "HEAD:main")

	work := filepath.Join(root, "work")
	runGit(t, root, "clone", remote, work)
	setUser(work)
	commitFile(work, "local.txt", "local", "local work that must be reconciled")

	other := filepath.Join(root, "other")
	runGit(t, root, "clone", remote, other)
	setUser(other)
	commitFile(other, "remote.txt", "remote", "upstream work")
	runGit(t, other, "push", "origin", "HEAD:main")

	return work
}

// countCommits returns (total commits reachable from HEAD, merge commits).
func countCommits(t *testing.T, dir string) (int, int) {
	t.Helper()
	parse := func(args ...string) int {
		t.Helper()
		out := strings.TrimSpace(gitOutput(t, dir, args...))
		n, err := strconv.Atoi(out)
		if err != nil {
			t.Fatalf("git %v = %q, not a number: %v", args, out, err)
		}
		return n
	}
	return parse("rev-list", "--count", "HEAD"), parse("rev-list", "--count", "--merges", "HEAD")
}

func pullOnce(t *testing.T, h *Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, target, nil)
	w := httptest.NewRecorder()
	h.HandleGitPull(w, r)
	return w
}

// TestGitPullAlwaysPassesAReconcileStrategy pins the whole point of the
// setting: ocode answers git's merge-vs-rebase question explicitly, so a
// divergent pull works on a repo with no pull.rebase/pull.ff configured, and
// the answer is the one configured in Settings → Git.
func TestGitPullAlwaysPassesAReconcileStrategy(t *testing.T) {
	cases := []struct {
		name        string
		cfg         *config.Config
		wantCommits int
		wantMerges  int
	}{
		{
			name:        "default (unset) rebases",
			cfg:         &config.Config{},
			wantCommits: 3, // base + upstream + rebased local
			wantMerges:  0,
		},
		{
			name:        "explicit rebase rebases",
			cfg:         &config.Config{Ocode: config.OcodeConfig{Git: config.GitConfig{PullStrategy: config.GitPullStrategyRebase}}},
			wantCommits: 3,
			wantMerges:  0,
		},
		{
			name:        "merge creates a merge commit",
			cfg:         &config.Config{Ocode: config.OcodeConfig{Git: config.GitConfig{PullStrategy: config.GitPullStrategyMerge}}},
			wantCommits: 4, // base + upstream + local + merge
			wantMerges:  1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := makeDivergentRepo(t)
			h := NewHandler()
			h.workDir = repo
			h.cfg = tc.cfg

			w := pullOnce(t, h, "/api/git/pull")
			if w.Code != http.StatusOK {
				t.Fatalf("pull failed with %d: %s", w.Code, w.Body.String())
			}
			commits, merges := countCommits(t, repo)
			if commits != tc.wantCommits || merges != tc.wantMerges {
				t.Fatalf("after pull: %d commits / %d merges, want %d / %d",
					commits, merges, tc.wantCommits, tc.wantMerges)
			}
		})
	}
}

// TestRemoteGitPullCarriesTheReconcileFlag proves the strategy travels in the
// remote command, because a remote project's pull runs on the host and must not
// be decided by that host's git config either.
func TestRemoteGitPullCarriesTheReconcileFlag(t *testing.T) {
	installFakeSSH(t)
	repo := makeDivergentRepo(t)

	h := newTestHandlerWithRemote(t, "ci.local", repo)
	h.cfg = &config.Config{Ocode: config.OcodeConfig{Git: config.GitConfig{PullStrategy: config.GitPullStrategyMerge}}}

	w := pullOnce(t, h, "/api/git/pull?host=ci.local&project="+repo)
	if w.Code != http.StatusOK {
		t.Fatalf("remote pull failed with %d: %s", w.Code, w.Body.String())
	}
	if _, merges := countCommits(t, repo); merges != 1 {
		t.Fatalf("remote pull did not merge (merges=%d); the flag did not reach the host", merges)
	}
}

// TestGitConfigRoutesAreRegistered guards the mux wiring: a handler-only test
// cannot catch a missing route.
func TestGitConfigRoutesAreRegistered(t *testing.T) {
	srv := New("127.0.0.1:0", "", "", nil)
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/api/config/ocode/git"},
		{http.MethodPut, "/api/config/ocode/git"},
	} {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			req := httptest.NewRequest(route.method, route.path, strings.NewReader(`{"pull_strategy":"rebase"}`))
			w := httptest.NewRecorder()
			srv.mux.ServeHTTP(w, req)
			if w.Code == http.StatusNotFound {
				t.Fatalf("route %s %s is not registered", route.method, route.path)
			}
		})
	}
}

func TestGitConfigHandlerGetPut(t *testing.T) {
	setHomeTree(t, t.TempDir())
	h := NewHandler()
	h.cfg = &config.Config{}

	// Default is rebase, even when the config block was never written.
	w := httptest.NewRecorder()
	h.HandleGetGitConfig(w, httptest.NewRequest(http.MethodGet, "/api/config/ocode/git", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET failed with %d: %s", w.Code, w.Body.String())
	}
	var got map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if got["pull_strategy"] != config.GitPullStrategyRebase {
		t.Fatalf("GET pull_strategy = %q, want %q", got["pull_strategy"], config.GitPullStrategyRebase)
	}

	// PUT merge persists, updates the live handler, and is visible to the next pull.
	w = httptest.NewRecorder()
	h.HandleSetGitConfig(w, httptest.NewRequest(http.MethodPut, "/api/config/ocode/git",
		strings.NewReader(`{"pull_strategy":"merge"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("PUT failed with %d: %s", w.Code, w.Body.String())
	}
	if flag := h.gitPullFlag(); flag != "--no-rebase" {
		t.Fatalf("gitPullFlag() = %q after PUT merge, want --no-rebase", flag)
	}
	stored, err := config.LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("load persisted config: %v", err)
	}
	if stored.Git.PullStrategy != config.GitPullStrategyMerge {
		t.Fatalf("persisted pull strategy = %q, want %q", stored.Git.PullStrategy, config.GitPullStrategyMerge)
	}

	// Switching back to rebase is accepted and shown by GET.
	w = httptest.NewRecorder()
	h.HandleSetGitConfig(w, httptest.NewRequest(http.MethodPut, "/api/config/ocode/git",
		strings.NewReader(`{"pull_strategy":"rebase"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("PUT rebase failed with %d: %s", w.Code, w.Body.String())
	}
	if flag := h.gitPullFlag(); flag != "--rebase" {
		t.Fatalf("gitPullFlag() = %q after PUT rebase, want --rebase", flag)
	}
}

func TestGitConfigHandlerRejectsBadInput(t *testing.T) {
	setHomeTree(t, t.TempDir())
	h := NewHandler()
	h.cfg = &config.Config{}

	for _, body := range []string{
		`{}`,                               // missing key must not silently mean "rebase"
		`{"pull_strategy":""}`,             // explicit empty is not a strategy
		`{"pull_strategy":"fast-forward"}`, // unknown value
		`not json`,
	} {
		w := httptest.NewRecorder()
		h.HandleSetGitConfig(w, httptest.NewRequest(http.MethodPut, "/api/config/ocode/git", strings.NewReader(body)))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("PUT %s = %d, want 400: %s", body, w.Code, w.Body.String())
		}
	}
	// A rejected request must not rewrite the live config, and therefore the
	// pull flag stays at the default.
	if flag := h.gitPullFlag(); flag != "--rebase" {
		t.Fatalf("gitPullFlag() = %q after rejected writes, want --rebase", flag)
	}
}
