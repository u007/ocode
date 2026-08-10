package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func newFilesHandler(t *testing.T) (*Handler, string) {
	t.Helper()
	tmpDir := t.TempDir()
	h := NewHandler()
	h.SetWorkDir(tmpDir)
	return h, tmpDir
}

// flattenTree returns every node path in the returned tree.
func flattenTree(t *testing.T, nodes []FileNode) []string {
	t.Helper()
	var out []string
	var walk func(ns []FileNode)
	walk = func(ns []FileNode) {
		for _, n := range ns {
			out = append(out, n.Path)
			if n.IsDir {
				walk(n.Children)
			}
		}
	}
	walk(nodes)
	return out
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func TestHandleFileTreeAnchorsToWorkDir(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	if err := os.WriteFile(filepath.Join(tmpDir, "project.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/files/tree", nil)
	h.HandleFileTree(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var nodes []FileNode
	if err := json.Unmarshal(w.Body.Bytes(), &nodes); err != nil {
		t.Fatalf("decode: %v", err)
	}
	paths := flattenTree(t, nodes)
	if !contains(paths, "project.txt") {
		t.Fatalf("expected tree anchored to workDir to contain project.txt, got %v", paths)
	}
	// The tree must not contain the test process CWD files (the package dir)
	// — the pre-fix behavior of anchoring to ".".
	if contains(paths, "handler_files.go") {
		t.Fatalf("tree leaked process CWD files; expected workDir anchor, got %v", paths)
	}
}

func TestHandleFileTreeDepthCap(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	// sub/deep/leaf.txt is 4 path components deep (within the default cap).
	if err := os.MkdirAll(filepath.Join(tmpDir, "sub", "deep"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "sub", "deep", "leaf.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	// a/b/c/d/very.txt is 5 components deep (beyond the default cap).
	if err := os.MkdirAll(filepath.Join(tmpDir, "a", "b", "c", "d"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "a", "b", "c", "d", "very.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	// Default depth: shallow file present, deep file absent.
	w := httptest.NewRecorder()
	h.HandleFileTree(w, httptest.NewRequest("GET", "/api/files/tree", nil))
	var nodes []FileNode
	if err := json.Unmarshal(w.Body.Bytes(), &nodes); err != nil {
		t.Fatalf("decode: %v", err)
	}
	paths := flattenTree(t, nodes)
	if !contains(paths, filepath.Join("sub", "deep", "leaf.txt")) {
		t.Fatalf("expected leaf.txt (depth 4) in default tree, got %v", paths)
	}
	if contains(paths, filepath.Join("a", "b", "c", "d", "very.txt")) {
		t.Fatalf("expected very.txt (depth 5) to be capped out, got %v", paths)
	}

	// depth=0: full tree includes the deep file.
	w2 := httptest.NewRecorder()
	h.HandleFileTree(w2, httptest.NewRequest("GET", "/api/files/tree?depth=0", nil))
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	var nodes2 []FileNode
	if err := json.Unmarshal(w2.Body.Bytes(), &nodes2); err != nil {
		t.Fatalf("decode: %v", err)
	}
	paths2 := flattenTree(t, nodes2)
	if !contains(paths2, filepath.Join("a", "b", "c", "d", "very.txt")) {
		t.Fatalf("expected very.txt (depth 5) in depth=0 tree, got %v", paths2)
	}
}

func TestHandleFileTreePathParam(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	sub := filepath.Join(tmpDir, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "in.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	// Relative ?path= resolves against workDir.
	w := httptest.NewRecorder()
	h.HandleFileTree(w, httptest.NewRequest("GET", "/api/files/tree?path="+url.QueryEscape("sub"), nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for relative path, got %d: %s", w.Code, w.Body.String())
	}
	var nodes []FileNode
	if err := json.Unmarshal(w.Body.Bytes(), &nodes); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !contains(flattenTree(t, nodes), "in.txt") {
		t.Fatalf("expected sub/in.txt via relative ?path=, got %v", nodes)
	}

	// Absolute ?path= inside workDir works.
	w2 := httptest.NewRecorder()
	h.HandleFileTree(w2, httptest.NewRequest("GET", "/api/files/tree?path="+url.QueryEscape(sub), nil))
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 for absolute path, got %d: %s", w2.Code, w2.Body.String())
	}

	// Absolute ?path= outside workDir is rejected.
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	w3 := httptest.NewRecorder()
	h.HandleFileTree(w3, httptest.NewRequest("GET", "/api/files/tree?path="+url.QueryEscape(outside), nil))
	if w3.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for path outside workDir, got %d: %s", w3.Code, w3.Body.String())
	}
}

func TestHandleFileContentResolvesAgainstWorkDir(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	if err := os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	// Relative path resolves against workDir even though the process CWD
	// (the test package dir) does not contain a.txt.
	w := httptest.NewRecorder()
	h.HandleFileContent(w, httptest.NewRequest("GET", "/api/files/content?path=a.txt", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Content != "hello" {
		t.Fatalf("expected content hello, got %q", resp.Content)
	}
}

func TestHandleSaveFileContentWritesFile(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	target := filepath.Join(tmpDir, "a.txt")
	if err := os.WriteFile(target, []byte("original\n"), 0644); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]string{"path": target, "content": "changed\n"})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/files/content", bytes.NewReader(body))
	h.HandleSaveFileContent(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Path  string `json:"path"`
		Saved bool   `json:"saved"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Saved {
		t.Fatalf("expected saved=true, got %+v", resp)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "changed\n" {
		t.Fatalf("expected file content %q, got %q", "changed\n", string(got))
	}
}

func TestHandleSaveFileContentMissingPath(t *testing.T) {
	h, _ := newFilesHandler(t)
	body, _ := json.Marshal(map[string]string{"content": "x"})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/files/content", bytes.NewReader(body))
	h.HandleSaveFileContent(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleSaveFileContentRejectsPathEscape(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	outside := filepath.Join(filepath.Dir(tmpDir), "outside.txt")

	body, _ := json.Marshal(map[string]string{"path": outside, "content": "x"})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/files/content", bytes.NewReader(body))
	h.HandleSaveFileContent(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for path escape, got %d: %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatalf("expected outside.txt to not be created")
	}
}

func TestHandleSaveFileContentRejectsDotDotEscape(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	escaping := filepath.Join(tmpDir, "..", "escaped.txt")

	body, _ := json.Marshal(map[string]string{"path": escaping, "content": "x"})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/files/content", bytes.NewReader(body))
	h.HandleSaveFileContent(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for .. escape, got %d: %s", w.Code, w.Body.String())
	}
}
