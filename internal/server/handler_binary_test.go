package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestHandleFileContentBinaryDetection(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	textPath := filepath.Join(tmpDir, "text.txt")
	if err := os.WriteFile(textPath, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	binPath := filepath.Join(tmpDir, "binary.bin")
	if err := os.WriteFile(binPath, []byte("hello\x00world"), 0644); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name   string
		path   string
		binary bool
	}{
		{"text", textPath, false},
		{"binary", binPath, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.HandleFileContent(w, httptest.NewRequest("GET", "/api/files/content?path="+filepath.Base(tc.path), nil))
			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", w.Code)
			}
			var resp struct {
				Content  string `json:"content"`
				IsBinary bool   `json:"is_binary"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if resp.IsBinary != tc.binary {
				t.Errorf("expected is_binary=%v, got %v", tc.binary, resp.IsBinary)
			}
		})
	}
}

// TestHandleFileContentTranscodesUTF16 pins the handler wiring: a UTF-16 file
// (full of NUL bytes) must be served as UTF-8 text, not flagged binary.
func TestHandleFileContentTranscodesUTF16(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	const want = "SELECT 1;\n-- 注釈\n"
	path := filepath.Join(tmpDir, "utf16.sql")
	if err := os.WriteFile(path, utf16Bytes(want, true, true), 0644); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	h.HandleFileContent(w, httptest.NewRequest("GET", "/api/files/content?path="+filepath.Base(path), nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Content  string `json:"content"`
		IsBinary bool   `json:"is_binary"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.IsBinary {
		t.Errorf("UTF-16 text must not be binary")
	}
	if resp.Content != want {
		t.Errorf("content mismatch:\n got %q\nwant %q", resp.Content, want)
	}
}
