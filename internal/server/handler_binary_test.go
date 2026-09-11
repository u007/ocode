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
				Content   string `json:"content"`
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
