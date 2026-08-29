package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
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
