package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"path/filepath"
	"testing"
)

func TestGitDiffNoRepo(t *testing.T) {
	// Use a temp dir that's definitely not in a git repo
	dir := t.TempDir()
	h := NewHandler()
	h.SetWorkDir(dir)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/git/diff", nil)
	h.HandleGitDiff(w, r)

	// Should return 200 with empty list (no repo = no changes)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var result []GitDiffFile
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty list, got %d files", len(result))
	}
}

func TestGitDiffCleanRepo(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	h := NewHandler()
	h.SetWorkDir(dir)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/git/diff", nil)
	h.HandleGitDiff(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var result []GitDiffFile
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty list for clean repo, got %d files", len(result))
	}
}

func TestGitDiffCleanRepoReturnsEmptyArray(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	h := NewHandler()
	h.SetWorkDir(dir)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/git/diff", nil)
	h.HandleGitDiff(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	// Check raw body: must be JSON array "[]" not "null"
	// Decoding into Go []GitDiffFile hides the difference because
	// json.Decode of null into a slice leaves it nil (len(nil)==0).
	body := w.Body.String()
	if body == "null\n" || body == "null" {
		t.Fatal("clean repo returned JSON null instead of empty array [] — frontend crashes on .find()")
	}
	if !strings.HasPrefix(body, "[") {
		t.Fatalf("expected JSON array starting with '[', got: %s", body)
	}
}

func TestGitDiffWithChanges(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	// Create and modify a file
	writeFile(t, filepath.Join(dir, "test.txt"), "hello")
	run(t, dir, "git", "add", "test.txt")
	run(t, dir, "git", "commit", "-m", "initial")
	writeFile(t, filepath.Join(dir, "test.txt"), "hello world")

	h := NewHandler()
	h.SetWorkDir(dir)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/git/diff", nil)
	h.HandleGitDiff(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var result []GitDiffFile
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 changed file, got %d", len(result))
	}
	if result[0].Path != "test.txt" {
		t.Errorf("expected path test.txt, got %s", result[0].Path)
	}
	if result[0].Status != "modified" {
		t.Errorf("expected status modified, got %s", result[0].Status)
	}
	if result[0].Patch == "" {
		t.Error("expected non-empty patch")
	}
}

func TestGitDiffWithPathFilter(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	writeFile(t, filepath.Join(dir, "a.txt"), "aaa")
	writeFile(t, filepath.Join(dir, "b.txt"), "bbb")
	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "commit", "-m", "initial")
	writeFile(t, filepath.Join(dir, "a.txt"), "aaa modified")
	writeFile(t, filepath.Join(dir, "b.txt"), "bbb modified")

	h := NewHandler()
	h.SetWorkDir(dir)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/git/diff?path=a.txt", nil)
	h.HandleGitDiff(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var result []GitDiffFile
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 file for path filter, got %d", len(result))
	}
	if result[0].Path != "a.txt" {
		t.Errorf("expected path a.txt, got %s", result[0].Path)
	}
}

func TestGitDiffUntrackedFile(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	writeFile(t, filepath.Join(dir, "new.txt"), "new content")

	h := NewHandler()
	h.SetWorkDir(dir)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/git/diff", nil)
	h.HandleGitDiff(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var result []GitDiffFile
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 untracked file, got %d", len(result))
	}
	if result[0].Path != "new.txt" {
		t.Errorf("expected path new.txt, got %s", result[0].Path)
	}
	if result[0].Status != "untracked" {
		t.Errorf("expected status untracked, got %s", result[0].Status)
	}
}

// --- helpers ---

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	run(t, dir, "git", "init")
	run(t, dir, "git", "config", "user.email", "test@test.com")
	run(t, dir, "git", "config", "user.name", "Test")
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command %v failed: %v\n%s", args, err, out)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
