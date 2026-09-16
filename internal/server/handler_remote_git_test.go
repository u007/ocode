package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/projects"
	"github.com/u007/ocode/internal/remote"
)

// installFakeSSH puts a fake `ssh` binary on PATH that translates every
// invocation into a local /bin/sh run of its final argument, so the remote
// git/files pipeline is exercised end-to-end (ExecCommand → exec → parse)
// without a network.

func installFakeSSH(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "ssh")
	script := "#!/bin/sh\n" +
		// Drop our own control options, then treat the last arg as the
		// command and run it locally.
		"args=()\n" +
		"for a in \"$@\"; do args+=(\"$a\"); done\n" +
		"cmd=\"${args[${#args[@]}-1]}\"\n" +
		"exec /bin/sh -c \"$cmd\"\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

// registerRemoteProject saves a remote project entry pointing at localDir —
// the "remote" commands will actually run on this machine through the fake
// ssh shim, so assertions operate on a real git repo.
func registerRemoteProject(t *testing.T, h *Handler, host, path string) {
	t.Helper()
	store, err := projects.NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("projects store: %v", err)
	}
	h.projects = store
	target, err := remote.ParseTarget(host)
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}
	if err := store.AddRemote(target.String(), path); err != nil {
		t.Fatalf("add remote: %v", err)
	}
}

func newTestHandlerWithRemote(t *testing.T, host, path string) *Handler {
	t.Helper()
	h := NewHandler()
	h.SetWorkDir(t.TempDir())
	registerRemoteProject(t, h, host, path)
	return h
}

// getJSON issues a GET and decodes the JSON body into out.
func getJSON(t *testing.T, h *Handler, url string, out interface{}) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", url, nil)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/git/status", h.HandleGitStatus)
	mux.HandleFunc("GET /api/git/workspace", h.HandleGitWorkspace)
	mux.HandleFunc("GET /api/git/log", h.HandleGitLog)
	mux.HandleFunc("GET /api/files/tree", h.HandleFileTree)
	mux.HandleFunc("GET /api/files/content", h.HandleFileContent)
	mux.ServeHTTP(w, r)
	if out != nil && w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
			t.Fatalf("decode %s: %v", url, err)
		}
	}
	return w
}

func TestRemoteWorkForRequiresRegisteredPair(t *testing.T) {
	h := NewHandler()
	h.SetWorkDir(t.TempDir())
	if _, err := h.remoteWorkFor("somehost", "/x"); err == nil {
		t.Fatal("expected error for unregistered host/path")
	}
}

func TestRemoteGitStatusOverFakeSSH(t *testing.T) {
	installFakeSSH(t)
	repo := t.TempDir()
	initGitRepo(t, repo)
	writeFile(t, filepath.Join(repo, "tracked.txt"), "hello\n")
	run(t, repo, "git", "add", ".")
	run(t, repo, "git", "commit", "-m", "init")
	writeFile(t, filepath.Join(repo, "tracked.txt"), "changed\n")
	writeFile(t, filepath.Join(repo, "new.txt"), "untracked\n")

	h := newTestHandlerWithRemote(t, "ci.local", repo)
	var status GitStatus
	w := getJSON(t, h, "/api/git/status?host=ci.local&project="+repo, &status)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if !status.IsRepo {
		t.Errorf("IsRepo = false, want true")
	}
	if status.Branch != "main" && status.Branch != "master" {
		t.Errorf("Branch = %q, want main/master", status.Branch)
	}
	if !sliceContains(status.ChangedFiles, "tracked.txt") {
		t.Errorf("ChangedFiles missing tracked.txt: %v", status.ChangedFiles)
	}
	if !sliceContains(status.ChangedFiles, "new.txt") {
		t.Errorf("ChangedFiles missing untracked new.txt: %v", status.ChangedFiles)
	}
	if !status.HasChanges {
		t.Errorf("HasChanges = false, want true")
	}
}

func TestRemoteGitWorkspaceOverFakeSSH(t *testing.T) {
	installFakeSSH(t)
	repo := t.TempDir()
	initGitRepo(t, repo)
	writeFile(t, filepath.Join(repo, "a.txt"), "line1\n")
	run(t, repo, "git", "add", ".")
	run(t, repo, "git", "commit", "-m", "init")
	writeFile(t, filepath.Join(repo, "a.txt"), "line1\nline2\n")
	writeFile(t, filepath.Join(repo, "staged.txt"), "staged\n")
	run(t, repo, "git", "add", "staged.txt")

	h := newTestHandlerWithRemote(t, "ci.local", repo)
	var ws GitWorkspace
	w := getJSON(t, h, "/api/git/workspace?host=ci.local&project="+repo, &ws)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if !ws.Status.IsRepo {
		t.Errorf("IsRepo = false, want true")
	}
	if len(ws.Staged) != 1 || ws.Staged[0].Path != "staged.txt" {
		t.Errorf("Staged = %+v, want staged.txt", ws.Staged)
	}
	foundUnstaged := false
	for _, f := range ws.Unstaged {
		if f.Path == "a.txt" && strings.Contains(f.Patch, "+line2") {
			foundUnstaged = true
		}
	}
	if !foundUnstaged {
		t.Errorf("Unstaged missing a.txt +line2 patch: %+v", ws.Unstaged)
	}
}

// Regression: an empty pathFilter must not become a pathspec. ShellQuote of
// the empty string is a quoted empty token (truthy), so the old
// quoting-gated condition appended an empty pathspec argument — which
// matches nothing — and the workspace returned empty
// staged/unstaged lists while /api/git/status still reported the files
// (badge showed N changes, Git tab list was blank).
func TestRemoteGitWorkspaceNoEmptyPathspec(t *testing.T) {
	installFakeSSH(t)
	repo := t.TempDir()
	initGitRepo(t, repo)
	writeFile(t, filepath.Join(repo, "m.txt"), "one\n")
	run(t, repo, "git", "add", ".")
	run(t, repo, "git", "commit", "-m", "init")
	writeFile(t, filepath.Join(repo, "m.txt"), "two\n")

	h := newTestHandlerWithRemote(t, "ci.local", repo)
	rw, err := h.remoteWorkFor("ci.local", repo)
	if err != nil {
		t.Fatal(err)
	}
	staged := remoteDiffFiles(t.Context(), rw, true, "")
	unstaged := remoteDiffFiles(t.Context(), rw, false, "")
	if len(staged) != 0 {
		t.Errorf("staged = %+v, want empty (nothing is staged)", staged)
	}
	found := false
	for _, f := range unstaged {
		if f.Path == "m.txt" && strings.Contains(f.Patch, "+two") {
			found = true
		}
	}
	if !found {
		t.Errorf("unstaged missing m.txt patch: %+v", unstaged)
	}
}

func TestRemoteGitLogOverFakeSSH(t *testing.T) {
	installFakeSSH(t)
	repo := t.TempDir()
	initGitRepo(t, repo)
	writeFile(t, filepath.Join(repo, "f.txt"), "one\n")
	run(t, repo, "git", "add", ".")
	run(t, repo, "git", "commit", "-m", "first")
	writeFile(t, filepath.Join(repo, "f.txt"), "two\n")
	run(t, repo, "git", "add", ".")
	run(t, repo, "git", "commit", "-m", "second")

	h := newTestHandlerWithRemote(t, "ci.local", repo)
	var commits []GitCommit
	w := getJSON(t, h, "/api/git/log?host=ci.local&project="+repo, &commits)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	if len(commits) != 2 {
		t.Fatalf("commits = %d, want 2", len(commits))
	}
	if commits[0].Message != "second" {
		t.Errorf("newest = %q, want second", commits[0].Message)
	}
}

func TestRemoteFileTreeOverFakeSSH(t *testing.T) {
	installFakeSSH(t)
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "b.txt"), "x")
	writeFile(t, filepath.Join(root, "a.txt"), "y")
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "sub", "c.txt"), "z")

	h := newTestHandlerWithRemote(t, "ci.local", root)
	var resp FileTreeResponse
	w := getJSON(t, h, "/api/files/tree?host=ci.local&path="+root+"&depth=1", &resp)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if len(resp.Children) != 3 {
		t.Fatalf("children = %d (%+v), want 3", len(resp.Children), resp.Children)
	}
	// Directories first, then name-sorted — same ordering as the local walk.
	if !resp.Children[0].IsDir || resp.Children[0].Name != "sub" {
		t.Errorf("first child = %+v, want dir sub", resp.Children[0])
	}
	if resp.Children[1].Name != "a.txt" || resp.Children[2].Name != "b.txt" {
		t.Errorf("file order wrong: %+v", resp.Children)
	}
}

func TestRemoteFileContentOverFakeSSH(t *testing.T) {
	installFakeSSH(t)
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "hello.txt"), "hello remote")

	h := newTestHandlerWithRemote(t, "ci.local", root)
	var out struct {
		Path     string `json:"path"`
		Content  string `json:"content"`
		IsBinary bool   `json:"is_binary"`
	}
	w := getJSON(t, h, "/api/files/content?host=ci.local&project_root="+root+"&path=f.txt", &out)
	// path is relative → resolved against the root
	if w.Code != http.StatusOK {
		w2 := getJSON(t, h, "/api/files/content?host=ci.local&project_root="+root+"&path=hello.txt", &out)
		if w2.Code != http.StatusOK {
			t.Fatalf("status %d then %d: %s / %s", w.Code, w2.Code, w.Body.String(), w2.Body.String())
		}
	}
}

func TestRemoteUnregisteredPairRejected(t *testing.T) {
	installFakeSSH(t)
	h := NewHandler()
	h.SetWorkDir(t.TempDir())
	w := getJSON(t, h, "/api/git/status?host=evil.host&project=/tmp/x", nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (unregistered remote pair)", w.Code)
	}
	if !strings.Contains(w.Body.String(), "not a remote project registered") {
		t.Errorf("body = %s, want admission error", w.Body.String())
	}
}

func TestRemoteSafeSpec(t *testing.T) {
	good := []string{"a.txt", "src/main.go", "deep/nested/file.ts"}
	for _, g := range good {
		if _, err := remoteSafeSpec(g); err != nil {
			t.Errorf("remoteSafeSpec(%q) = %v, want nil", g, err)
		}
	}
	bad := []string{"a;rm -rf.txt", "$(whoami)", "a b `id`", "/abs/path", "back`tick", "semi;colon", "pipe|it"}
	for _, b := range bad {
		if _, err := remoteSafeSpec(b); err == nil {
			t.Errorf("remoteSafeSpec(%q) accepted, want error", b)
		}
	}
}

func TestRemoteRelCheck(t *testing.T) {
	root := "/home/u/proj"
	ok := map[string]string{
		"/home/u/proj":           ".",
		"/home/u/proj/a.txt":     "a.txt",
		"/home/u/proj/sub/b.txt": "sub/b.txt",
	}
	for in, want := range ok {
		got, err := remoteRelCheck(root, in)
		if err != nil || got != want {
			t.Errorf("remoteRelCheck(%q) = %q,%v want %q", in, got, err, want)
		}
	}
	bad := []string{"/etc/passwd", "/home/u/other/x", "/home/u/proj2/x", "/home/u/proj/../etc", "/home/u/proj/.git/config"}
	for _, b := range bad {
		if _, err := remoteRelCheck(root, b); err == nil {
			t.Errorf("remoteRelCheck(%q) accepted, want error", b)
		}
	}
}

func TestRemoteReadFileEncodeDecode(t *testing.T) {
	// The transport-independent part: marker split + base64 round trip.
	payload := "binary\x00data\xff with newline\ntail"
	enc := base64.StdEncoding.EncodeToString([]byte(payload))
	marker := "OCODE_EOF_9f7c_END"
	wire := enc + "\n" + marker
	trimmed := strings.TrimRight(wire, "\n")
	idx := strings.LastIndex(trimmed, "\n"+marker)
	if idx < 0 {
		t.Fatal("marker not found")
	}
	b64 := strings.ReplaceAll(trimmed[:idx], "\n", "")
	dec, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(dec) != payload {
		t.Errorf("round trip = %q, want %q", dec, payload)
	}
}

func TestRemoteGitWorkspaceNonRepo(t *testing.T) {
	installFakeSSH(t)
	plain := t.TempDir()
	h := newTestHandlerWithRemote(t, "ci.local", plain)
	var ws GitWorkspace
	w := getJSON(t, h, "/api/git/workspace?host=ci.local&project="+plain, &ws)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	if ws.Status.IsRepo {
		t.Errorf("IsRepo = true, want false for non-repo")
	}
	if len(ws.Staged) != 0 || len(ws.Unstaged) != 0 {
		t.Errorf("expected empty diffs for non-repo, got %+v", ws)
	}
}

// Regression: ?commit= is interpolated into a remote shell command, so a
// metachar payload must not escape the git invocation. The rev is
// ShellQuoted with ^{commit} appended as a fixed literal —
// "x;touch MARKER^{commit}" must resolve as (unknown commit), never create
// the marker file. The fake-ssh shim runs the command locally, so a shell
// escape would plant the file in the repo dir.
func TestRemoteGitShowRejectsShellInjection(t *testing.T) {
	installFakeSSH(t)
	repo := t.TempDir()
	initGitRepo(t, repo)
	writeFile(t, filepath.Join(repo, "a.txt"), "one\n")
	run(t, repo, "git", "add", ".")
	run(t, repo, "git", "commit", "-m", "init")

	h := newTestHandlerWithRemote(t, "ci.local", repo)
	rw, err := h.remoteWorkFor("ci.local", repo)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(repo, "PWNED_BY_INJECTION_TEST")
	payloads := []string{
		"x;touch " + marker,
		"HEAD;touch " + marker,
		"$(touch " + marker + ")",
		"`touch " + marker + "`",
		"HEAD|touch " + marker,
		"HEAD&&touch " + marker,
		"-upload-pack=touch",
	}
	for _, p := range payloads {
		if _, err := remoteGitShow(t.Context(), rw, p); err == nil {
			t.Errorf("remoteGitShow(%q) succeeded, want unknown commit", p)
		}
		if _, serr := os.Stat(marker); !os.IsNotExist(serr) {
			t.Fatalf("remoteGitShow(%q) executed shell: marker exists", p)
		}
	}
}

// A real commit id still resolves through the quoting (regression guard
// for the ShellQuote fix above: quoting must not break legitimate revs).
func TestRemoteGitShowResolvesRealCommit(t *testing.T) {
	installFakeSSH(t)
	repo := t.TempDir()
	initGitRepo(t, repo)
	writeFile(t, filepath.Join(repo, "a.txt"), "one\ntwo\n")
	run(t, repo, "git", "add", ".")
	run(t, repo, "git", "commit", "-m", "init")

	h := newTestHandlerWithRemote(t, "ci.local", repo)
	rw, err := h.remoteWorkFor("ci.local", repo)
	if err != nil {
		t.Fatal(err)
	}
	headOut, headErr := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if headErr != nil {
		t.Fatalf("rev-parse HEAD: %v", headErr)
	}
	head := strings.TrimSpace(string(headOut))
	for _, rev := range []string{head, head[:12], "HEAD"} {
		files, err := remoteGitShow(t.Context(), rw, rev)
		if err != nil {
			t.Errorf("remoteGitShow(%q) failed: %v", rev, err)
			continue
		}
		if len(files) != 1 || files[0].Path != "a.txt" {
			t.Errorf("remoteGitShow(%q) = %+v, want single a.txt diff", rev, files)
		}
	}
}

// contains reports whether list contains s.
func sliceContains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
