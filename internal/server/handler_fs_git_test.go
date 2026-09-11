package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newGitTestHandler builds a Handler pointed at a fresh temp dir (optionally a
// git repo) for exercising the git/fs mutation endpoints without the auth
// middleware.
func newGitTestHandler(t *testing.T, initRepo bool) *Handler {
	t.Helper()
	dir := t.TempDir()
	if initRepo {
		runGit(t, dir, "init")
		runGit(t, dir, "config", "user.email", "test@example.com")
		runGit(t, dir, "config", "user.name", "Test")
	}
	h := NewHandler()
	h.workDir = dir
	return h
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v failed: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// call posts JSON and returns the request + recorder so the caller can invoke
// the handler directly (bypassing the auth middleware).
func call(target string, body any) (*http.Request, *httptest.ResponseRecorder) {
	var r *http.Request
	if body != nil {
		buf, _ := json.Marshal(body)
		r = httptest.NewRequest(http.MethodPost, target, bytes.NewReader(buf))
	} else {
		r = httptest.NewRequest(http.MethodPost, target, nil)
	}
	return r, httptest.NewRecorder()
}

func TestGitNetworkActionsRejectNonRepository(t *testing.T) {
	h := newGitTestHandler(t, false)
	for _, action := range []struct {
		name   string
		handle http.HandlerFunc
		body   any
	}{
		{name: "fetch", handle: h.HandleGitFetch, body: map[string]any{}},
		{name: "pull", handle: h.HandleGitPull, body: map[string]any{}},
		{name: "push", handle: h.HandleGitPush, body: map[string]any{"force": false}},
		{name: "reset remote", handle: h.HandleGitResetRemote, body: map[string]any{}},
	} {
		t.Run(action.name, func(t *testing.T) {
			r, w := call("/api/git/"+strings.ReplaceAll(action.name, " ", "-"), action.body)
			action.handle(w, r)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestGitNetworkRoutesAreRegistered(t *testing.T) {
	srv := New("127.0.0.1:0", "", "", nil)
	srv.handler.workDir = t.TempDir()
	for _, path := range []string{"/api/git/fetch", "/api/git/pull", "/api/git/push", "/api/git/reset-remote"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(`{"force":false}`)))
			w := httptest.NewRecorder()
			srv.mux.ServeHTTP(w, req)
			if w.Code == http.StatusNotFound {
				t.Fatalf("route %s is not registered", path)
			}
		})
	}
}

func TestNonInteractiveSSHCommandOverridesBatchMode(t *testing.T) {
	for _, command := range []string{
		"ssh -i ~/.ssh/id_ed25519 -o BatchMode=no",
		"ssh -oBatchMode=no -o ConnectTimeout=5",
	} {
		got := nonInteractiveSSHCommand(command)
		if strings.Contains(strings.ToLower(got), "batchmode=no") {
			t.Fatalf("command still permits SSH prompts: %q", got)
		}
		if !strings.HasSuffix(got, "-o BatchMode=yes") {
			t.Fatalf("command does not force batch mode: %q", got)
		}
	}
}

func TestGitPushForceUsesForceWithLease(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	local := filepath.Join(root, "local")
	runGit(t, root, "init", "--bare", remote)
	if err := os.Mkdir(local, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, local, "init")
	runGit(t, local, "config", "user.email", "test@example.com")
	runGit(t, local, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(local, "file.txt"), []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, local, "add", "file.txt")
	runGit(t, local, "commit", "-m", "first")
	runGit(t, local, "remote", "add", "origin", remote)
	runGit(t, local, "push", "-u", "origin", "HEAD")

	nested := filepath.Join(local, "nested")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	h := NewHandler()
	// Network actions normalize a nested project path to the repository root.
	h.workDir = nested
	if err := os.WriteFile(filepath.Join(local, "file.txt"), []byte("second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, local, "add", "file.txt")
	runGit(t, local, "commit", "--amend", "-m", "second")

	r, w := call("/api/git/push", map[string]any{"force": false})
	h.HandleGitPush(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("normal push should reject non-fast-forward, got %d: %s", w.Code, w.Body.String())
	}

	r, w = call("/api/git/push", map[string]any{"force": true})
	h.HandleGitPush(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("force push failed: %d: %s", w.Code, w.Body.String())
	}
	localHead := gitOutput(t, local, "rev-parse", "HEAD")
	remoteHead := gitOutput(t, remote, "rev-parse", "HEAD")
	if localHead != remoteHead {
		t.Fatalf("remote head %s does not match local head %s", remoteHead, localHead)
	}
}

func TestGitStageNonRepoRejected(t *testing.T) {
	h := newGitTestHandler(t, false)
	r, w := call("/api/git/stage", map[string]any{"paths": []string{"a.txt"}})
	h.HandleGitStage(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-repo, got %d", w.Code)
	}
}

func TestGitStageDeletedFile(t *testing.T) {
	h := newGitTestHandler(t, true)
	dir := h.workDir
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "init")
	if err := os.Remove(filepath.Join(dir, "a.txt")); err != nil {
		t.Fatal(err)
	}
	r, w := call("/api/git/stage", map[string]any{"paths": []string{"a.txt"}})
	h.HandleGitStage(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 staging a deleted file, got %d: %s", w.Code, w.Body.String())
	}
	st := gitStatusForDir(dir)
	found := false
	for _, f := range st.StagedFiles {
		if f == "a.txt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a.txt staged after deletion, got %+v", st.StagedFiles)
	}
}

func TestGitCommitRequiresMessage(t *testing.T) {
	h := newGitTestHandler(t, true)
	r, w := call("/api/git/commit", map[string]any{"message": ""})
	h.HandleGitCommit(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty message, got %d", w.Code)
	}
}

func TestGitStageTraversalRejected(t *testing.T) {
	h := newGitTestHandler(t, true)
	r, w := call("/api/git/stage", map[string]any{"paths": []string{"../../etc/passwd"}})
	h.HandleGitStage(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for traversal, got %d", w.Code)
	}
}

func TestGitStageDotGitRejected(t *testing.T) {
	h := newGitTestHandler(t, true)
	r, w := call("/api/git/stage", map[string]any{"paths": []string{".git/config"}})
	h.HandleGitStage(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for .git path, got %d", w.Code)
	}
}

func TestGitStageSymlinkEscapeRejected(t *testing.T) {
	h := newGitTestHandler(t, true)
	dir := h.workDir
	if err := os.Symlink(t.TempDir(), filepath.Join(dir, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "escape", "secret.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, w := call("/api/git/stage", map[string]any{"paths": []string{"escape/secret.txt"}})
	h.HandleGitStage(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for symlink escape, got %d: %s", w.Code, w.Body.String())
	}
}

func TestFSCopyAndDelete(t *testing.T) {
	h := newGitTestHandler(t, false)
	dir := h.workDir
	src := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(src, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	destDir := filepath.Join(dir, "sub")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatal(err)
	}
	r, w := call("/api/fs/copy", map[string]any{"paths": []string{"src.txt"}, "dest_dir": "sub"})
	h.HandleFSCopy(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("copy failed: %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(destDir, "src.txt")); err != nil {
		t.Fatalf("copied file missing: %v", err)
	}
	r, w = call("/api/fs/delete", map[string]any{"paths": []string{"src.txt", "sub/src.txt"}})
	h.HandleFSDelete(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("delete failed: %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("src not deleted")
	}
}

func TestFSRenameAndNewFile(t *testing.T) {
	h := newGitTestHandler(t, false)
	dir := h.workDir
	if err := os.WriteFile(filepath.Join(dir, "old.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, w := call("/api/fs/rename", map[string]any{"path": "old.txt", "new_name": "new.txt"})
	h.HandleFSRename(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("rename failed: %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); err != nil {
		t.Fatalf("renamed file missing: %v", err)
	}
	r, w = call("/api/fs/new-file", map[string]any{"path": "created.txt"})
	h.HandleFSNewFile(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("new-file failed: %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "created.txt")); err != nil {
		t.Fatalf("created file missing: %v", err)
	}
}

func TestFSDeleteTraversalRejected(t *testing.T) {
	h := newGitTestHandler(t, false)
	r, w := call("/api/fs/delete", map[string]any{"paths": []string{"../../etc/passwd"}})
	h.HandleFSDelete(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for traversal, got %d", w.Code)
	}
}

func TestFSNewFileDotGitRejected(t *testing.T) {
	h := newGitTestHandler(t, true)
	r, w := call("/api/fs/new-file", map[string]any{"path": ".git/hook"})
	h.HandleFSNewFile(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for .git path, got %d", w.Code)
	}
}
