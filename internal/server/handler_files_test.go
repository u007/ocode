package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
