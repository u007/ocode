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

func newSaveHandler(t *testing.T) (*Handler, string) {
	t.Helper()
	tmpDir := t.TempDir()
	h := NewHandler()
	h.workDir = tmpDir
	return h, tmpDir
}

func TestHashContentUnicodeSpan(t *testing.T) {
	// Exact JS vectors from web/src/components/Files/editorTabsPersistence.ts
	// hashContent via Node: ""->45h, "hello"->4bj995, "a"->3t3a, "😀"->4lyxu, "a😀b"->zlbdid, "abc"->3772q3
	cases := map[string]string{
		"":      "45h",
		"hello": "4bj995",
		"a":     "3t3a",
		"😀":     "4lyxu",
		"a😀b":   "zlbdid",
		"abc":   "3772q3",
	}
	for input, want := range cases {
		if got := hashContent(input); got != want {
			t.Fatalf("hashContent(%q) = %q, want %q", input, got, want)
		}
	}
	if hashContent("") == "__missing__" {
		t.Fatalf("hash collision with sentinel")
	}
	if hashContent("a\uFFFD"+"b") == hashContent("a😀b") {
		t.Fatalf("surrogate handling not distinct")
	}
}

func TestSaveFileContentGuardMatchSucceeds(t *testing.T) {
	h, tmpDir := newSaveHandler(t)
	path := filepath.Join(tmpDir, "file.txt")
	if err := os.WriteFile(path, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	expected := hashContent("hello")
	body, _ := json.Marshal(map[string]interface{}{
		"path":          path,
		"content":       "hello world",
		"expected_hash": expected,
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/files/content", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	h.HandleSaveFileContent(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	data, _ := os.ReadFile(path)
	if string(data) != "hello world" {
		t.Fatalf("unexpected content %q", string(data))
	}
}

func TestSaveFileContentGuardMismatch409(t *testing.T) {
	h, tmpDir := newSaveHandler(t)
	path := filepath.Join(tmpDir, "file.txt")
	if err := os.WriteFile(path, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	// External edit to "changed" before save.
	if err := os.WriteFile(path, []byte("changed"), 0644); err != nil {
		t.Fatal(err)
	}
	expected := hashContent("hello") // stale
	body, _ := json.Marshal(map[string]interface{}{
		"path":          path,
		"content":       "new content",
		"expected_hash": expected,
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/files/content", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	h.HandleSaveFileContent(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	// File should remain "changed", not overwritten.
	data, _ := os.ReadFile(path)
	if string(data) != "changed" {
		t.Fatalf("file should not be overwritten, got %q", string(data))
	}
}

func TestSaveFileContentGuardForceSucceeds(t *testing.T) {
	h, tmpDir := newSaveHandler(t)
	path := filepath.Join(tmpDir, "file.txt")
	if err := os.WriteFile(path, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("changed"), 0644); err != nil {
		t.Fatal(err)
	}
	expected := hashContent("hello")
	body, _ := json.Marshal(map[string]interface{}{
		"path":          path,
		"content":       "forced",
		"expected_hash": expected,
		"force":         true,
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/files/content", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	h.HandleSaveFileContent(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with force, got %d: %s", w.Code, w.Body.String())
	}
	data, _ := os.ReadFile(path)
	if string(data) != "forced" {
		t.Fatalf("expected forced content, got %q", string(data))
	}
}

func TestSaveFileContentDeletionEmptyFile409(t *testing.T) {
	h, tmpDir := newSaveHandler(t)
	path := filepath.Join(tmpDir, "empty.txt")
	if err := os.WriteFile(path, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}
	expected := hashContent("") // empty file's hash
	// Delete the file externally.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]interface{}{
		"path":          path,
		"content":       "new",
		"expected_hash": expected,
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/files/content", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	h.HandleSaveFileContent(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 for deleted empty file, got %d: %s", w.Code, w.Body.String())
	}
	// Force should recreate.
	body2, _ := json.Marshal(map[string]interface{}{
		"path":          path,
		"content":       "new",
		"expected_hash": expected,
		"force":         true,
	})
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("PUT", "/api/files/content", bytes.NewReader(body2))
	r2.Header.Set("Content-Type", "application/json")
	h.HandleSaveFileContent(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 with force for deleted file, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestSaveFileContentPresentEmptyMatchSucceeds(t *testing.T) {
	h, tmpDir := newSaveHandler(t)
	path := filepath.Join(tmpDir, "empty_present.txt")
	if err := os.WriteFile(path, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}
	expected := hashContent("") // empty present file
	body, _ := json.Marshal(map[string]interface{}{
		"path":          path,
		"content":       "new content",
		"expected_hash": expected,
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/files/content", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	h.HandleSaveFileContent(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for present empty file, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSaveFileContentDeletionNonEmpty409(t *testing.T) {
	h, tmpDir := newSaveHandler(t)
	path := filepath.Join(tmpDir, "nonempty.txt")
	if err := os.WriteFile(path, []byte("old content"), 0644); err != nil {
		t.Fatal(err)
	}
	expected := hashContent("old content")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]interface{}{
		"path":          path,
		"content":       "new",
		"expected_hash": expected,
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/files/content", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	h.HandleSaveFileContent(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 for deleted non-empty file, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSaveFileContentConcurrentSecond409(t *testing.T) {
	h, tmpDir := newSaveHandler(t)
	path := filepath.Join(tmpDir, "concurrent.txt")
	if err := os.WriteFile(path, []byte("base"), 0644); err != nil {
		t.Fatal(err)
	}
	expected := hashContent("base")
	// First save succeeds.
	body1, _ := json.Marshal(map[string]interface{}{
		"path":          path,
		"content":       "first",
		"expected_hash": expected,
	})
	w1 := httptest.NewRecorder()
	r1 := httptest.NewRequest("PUT", "/api/files/content", bytes.NewReader(body1))
	r1.Header.Set("Content-Type", "application/json")
	h.HandleSaveFileContent(w1, r1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first save expected 200, got %d", w1.Code)
	}
	// Second save with same stale expected hash should 409.
	body2, _ := json.Marshal(map[string]interface{}{
		"path":          path,
		"content":       "second",
		"expected_hash": expected,
	})
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("PUT", "/api/files/content", bytes.NewReader(body2))
	r2.Header.Set("Content-Type", "application/json")
	h.HandleSaveFileContent(w2, r2)
	if w2.Code != http.StatusConflict {
		t.Fatalf("second concurrent save expected 409, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestSaveFileContentConcurrentGoroutines(t *testing.T) {
	h, tmpDir := newSaveHandler(t)
	path := filepath.Join(tmpDir, "concurrent_race.txt")
	if err := os.WriteFile(path, []byte("base"), 0644); err != nil {
		t.Fatal(err)
	}
	expected := hashContent("base")
	results := make(chan int, 2)
	run := func(content string) {
		body, _ := json.Marshal(map[string]interface{}{
			"path":          path,
			"content":       content,
			"expected_hash": expected,
		})
		w := httptest.NewRecorder()
		r := httptest.NewRequest("PUT", "/api/files/content", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		h.HandleSaveFileContent(w, r)
		results <- w.Code
	}
	go run("from-goroutine-1")
	go run("from-goroutine-2")
	c1 := <-results
	c2 := <-results
	// Exactly one should succeed (200), the other 409, serialized by saveLockFor.
	if !((c1 == http.StatusOK && c2 == http.StatusConflict) || (c1 == http.StatusConflict && c2 == http.StatusOK)) {
		t.Fatalf("expected one 200 and one 409, got %d and %d", c1, c2)
	}
}

// Regression: PUT /api/files/content routes on ?host= only — the frontend
// sends host in the query string (never the JSON body). A remote save with
// ?host= must take the remote branch even when the body carries no host.
func TestSaveFileContentRemoteRoutedByQueryHost(t *testing.T) {
	installFakeSSH(t)
	remoteDir := t.TempDir()
	remoteFile := filepath.Join(remoteDir, "note.txt")
	if err := os.WriteFile(remoteFile, []byte("remote hello"), 0644); err != nil {
		t.Fatal(err)
	}
	h := newTestHandlerWithRemote(t, "ci.local", remoteDir)
	body, _ := json.Marshal(map[string]interface{}{
		"path":          "note.txt",
		"content":       "remote hello world",
		"project_root":  remoteDir,
		"expected_hash": hashContent("remote hello"),
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/files/content?host=ci.local", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	h.HandleSaveFileContent(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	data, err := os.ReadFile(remoteFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "remote hello world" {
		t.Fatalf("remote file not written, got %q", string(data))
	}
}
