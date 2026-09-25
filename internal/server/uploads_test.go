package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/projects"
)

// newUploadsTestHandler builds a handler whose project registry holds the
// EXPANDED project root, mirroring what the host-side `ocode serve --remote`
// stores: its projects.Add expands "~" and Cleans before saving, while the
// desktop keeps sending the verbatim tilde form through the remote proxy.
func newUploadsTestHandler(t *testing.T, projectDir string) *Handler {
	t.Helper()
	h := NewHandler()
	h.SetWorkDir(t.TempDir())
	store, err := projects.NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("projects store: %v", err)
	}
	if err := store.Add(projectDir); err != nil {
		t.Fatalf("add project: %v", err)
	}
	h.projects = store
	return h
}

// A remote project's uploads request arrives through the remote proxy with the
// verbatim tilde path the desktop registered, but the host resolves that
// against its own home directory when it checks the project trust gate. Before
// the fix this answered 400 "unknown project" (and, when the tilde path was
// matched locally instead, 500 from creating a relative "~/..." directory).
func TestHandleUploadsAcceptsTildeFormRegisteredProject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	projectDir := filepath.Join(home, "www", "aimsai2")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	h := newUploadsTestHandler(t, projectDir)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/uploads?project="+url.QueryEscape("~/www/aimsai2"), nil)
	h.HandleUploads(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for a registered tilde-form project, got %d: %s", w.Code, w.Body.String())
	}
	var files []uploadFileInfo
	if err := json.NewDecoder(w.Body).Decode(&files); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("expected an empty listing, got %+v", files)
	}
	// The uploads directory must land under the EXPANDED project root. A
	// leading "~" is not absolute, so an unexpanded path would have been
	// created relative to the server process cwd instead.
	if _, err := os.Stat(filepath.Join(projectDir, ".ocode", "uploads")); err != nil {
		t.Fatalf("expected uploads dir under the expanded project root: %v", err)
	}
}

// Expanding "~" must not widen the trust boundary: a tilde-form path that is
// not a registered project root stays rejected.
func TestHandleUploadsRejectsUnregisteredTildeProject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	h := newUploadsTestHandler(t, t.TempDir())

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/uploads?project="+url.QueryEscape("~/not-registered"), nil)
	h.HandleUploads(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an unregistered tilde project, got %d: %s", w.Code, w.Body.String())
	}
}
