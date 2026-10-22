package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The editor/preview must be able to open a file OUTSIDE the active project,
// without the grant leaking into the AGENT's permission scope.
//
// The rules these tests pin:
//
//   - An absolute path with NO project_root used to be UNSCOPED on the read
//     endpoints (200 for any file on disk). It is now confined to the project
//     roots, the extra allowed paths and the editor grants.
//   - A path the user GRANTED is readable, writable and openable.
//   - A path the user did NOT grant is refused with a message the web can
//     recognise, so it can offer the grant.
//   - Traversal is still refused.
//   - The agent's permission scope is unaffected by a grant.

func grantPath(t *testing.T, h *Handler, path string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	body := `{"path":"` + path + `"}`
	h.HandleGrantFilePath(rec, httptest.NewRequest("POST", "/api/files/grant", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("grant: status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Root string `json:"root"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("grant decode: %v", err)
	}
	if resp.Root == "" {
		t.Fatalf("grant returned no root: %s", rec.Body.String())
	}
	return resp.Root
}

func TestFileGrant_AllowsReadOutsideProject(t *testing.T) {
	h, tmpDir := newFilesHandler(t)
	outside := t.TempDir()
	target := filepath.Join(outside, "notes.md")
	if err := os.WriteFile(target, []byte("outside content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Before the grant: refused.
	rec := httptest.NewRecorder()
	h.HandleFileContent(rec, httptest.NewRequest("GET", "/api/files/content?path="+url.QueryEscape(target), nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("before grant: status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "outside the project root") {
		t.Fatalf("before grant: body = %s, want the containment refusal", rec.Body.String())
	}

	// After the grant: readable, both as an absolute path and anchored to the
	// granted root.
	root := grantPath(t, h, target)
	// The canonical root is symlink-resolved, so on macOS the temp dir comes
	// back under /private/var. Compare the resolved forms.
	wantRoot := outside
	if real, err := filepath.EvalSymlinks(outside); err == nil {
		wantRoot = real
	}
	if root != wantRoot {
		t.Fatalf("grant root = %q, want %q", root, wantRoot)
	}

	for _, q := range []string{
		"/api/files/content?path=" + url.QueryEscape(target),
		"/api/files/content?path=" + url.QueryEscape("notes.md") + "&project_root=" + url.QueryEscape(root),
		"/api/files/content?path=" + url.QueryEscape(target) + "&project_root=" + url.QueryEscape(root),
	} {
		rec := httptest.NewRecorder()
		h.HandleFileContent(rec, httptest.NewRequest("GET", q, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("after grant %q: status = %d body=%s", q, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "outside content") {
			t.Fatalf("after grant %q: body = %s, want the file content", q, rec.Body.String())
		}
	}
	_ = tmpDir
}

func TestFileGrant_AllowsSaveOutsideProject(t *testing.T) {
	h, _ := newFilesHandler(t)
	outside := t.TempDir()
	target := filepath.Join(outside, "notes.md")
	if err := os.WriteFile(target, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}

	// Without the grant the write is refused.
	rec := httptest.NewRecorder()
	h.HandleSaveFileContent(rec, httptest.NewRequest("PUT", "/api/files/content",
		strings.NewReader(`{"path":"`+target+`","content":"hacked"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("before grant: status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
	data, _ := os.ReadFile(target)
	if string(data) != "original" {
		t.Fatalf("before grant: file was modified: %q", string(data))
	}

	// With the grant the write succeeds and the file is changed.
	root := grantPath(t, h, target)
	rec2 := httptest.NewRecorder()
	h.HandleSaveFileContent(rec2, httptest.NewRequest("PUT", "/api/files/content",
		strings.NewReader(`{"path":"notes.md","content":"edited","project_root":"`+root+`"}`)))
	if rec2.Code != http.StatusOK {
		t.Fatalf("after grant: status = %d body=%s", rec2.Code, rec2.Body.String())
	}
	data, _ = os.ReadFile(target)
	if string(data) != "edited" {
		t.Fatalf("after grant: file content = %q, want %q", string(data), "edited")
	}
}

// A grant covers the granted DIRECTORY, not the whole disk.
func TestFileGrant_SiblingFileInGrantedDirIsAllowed(t *testing.T) {
	h, _ := newFilesHandler(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "a.txt"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "b.txt"), []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}
	grantPath(t, h, filepath.Join(outside, "a.txt"))

	rec := httptest.NewRecorder()
	h.HandleFileContent(rec, httptest.NewRequest("GET", "/api/files/content?path="+url.QueryEscape(filepath.Join(outside, "b.txt")), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("sibling file: status = %d body=%s", rec.Code, rec.Body.String())
	}
}

// A grant is a DIRECTORY grant: a path OUTSIDE it stays refused even though
// the grant exists.
func TestFileGrant_DoesNotCoverPathOutsideGrantedDir(t *testing.T) {
	h, _ := newFilesHandler(t)
	granted := t.TempDir()
	elsewhere := t.TempDir()
	target := filepath.Join(elsewhere, "secret.txt")
	if err := os.WriteFile(target, []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}
	grantPath(t, h, granted)

	rec := httptest.NewRecorder()
	h.HandleFileContent(rec, httptest.NewRequest("GET", "/api/files/content?path="+url.QueryEscape(target), nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
}

// Revoking a grant takes the access away again.
func TestFileGrant_RevokeRemovesAccess(t *testing.T) {
	h, _ := newFilesHandler(t)
	outside := t.TempDir()
	target := filepath.Join(outside, "notes.md")
	if err := os.WriteFile(target, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	root := grantPath(t, h, target)

	rec := httptest.NewRecorder()
	h.HandleRevokeFilePath(rec, httptest.NewRequest("DELETE", "/api/files/grant",
		strings.NewReader(`{"path":"`+root+`"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke: status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Revoked bool `json:"revoked"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("revoke decode: %v", err)
	}
	if !resp.Revoked {
		t.Fatalf("revoke reported revoked=false: %s", rec.Body.String())
	}

	rec2 := httptest.NewRecorder()
	h.HandleFileContent(rec2, httptest.NewRequest("GET", "/api/files/content?path="+url.QueryEscape(target), nil))
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("after revoke: status = %d, want 400 (body=%s)", rec2.Code, rec2.Body.String())
	}
}

// The grant must not become a traversal hole.
func TestFileGrant_TraversalStillRefused(t *testing.T) {
	h, _ := newFilesHandler(t)
	outside := t.TempDir()
	target := filepath.Join(outside, "notes.md")
	if err := os.WriteFile(target, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	root := grantPath(t, h, target)

	// A ".." component in the raw request value is refused even when the
	// granted root is named.
	rec := httptest.NewRecorder()
	h.HandleFileContent(rec, httptest.NewRequest("GET",
		"/api/files/content?path="+url.QueryEscape("../secret.txt")+"&project_root="+url.QueryEscape(root), nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("traversal: status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
}

// The grant must not let the WRITE surface escape the granted directory.
func TestFileGrant_SaveCannotEscapeGrantedDir(t *testing.T) {
	h, _ := newFilesHandler(t)
	outside := t.TempDir()
	target := filepath.Join(outside, "notes.md")
	if err := os.WriteFile(target, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	root := grantPath(t, h, target)

	rec := httptest.NewRecorder()
	h.HandleSaveFileContent(rec, httptest.NewRequest("PUT", "/api/files/content",
		strings.NewReader(`{"path":"../escaped.txt","content":"x","project_root":"`+root+`"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
}

// A grant is EDITOR-ONLY: it must not enter the agent's permission scope.
func TestFileGrant_DoesNotWidenAgentScope(t *testing.T) {
	h, _ := newFilesHandler(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "notes.md"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	grantPath(t, h, filepath.Join(outside, "notes.md"))

	// The file surfaces' root set includes the grant…
	if !h.pathWithinAnyFileRoot(filepath.Join(outside, "notes.md")) {
		t.Fatal("grant did not reach the file access roots")
	}
	// …but the DB browser's root set (which mirrors the agent's project
	// boundary) must NOT include it.
	for _, root := range h.dbRoots() {
		if root == outside {
			t.Fatalf("grant leaked into the DB/agent root set: %v", h.dbRoots())
		}
	}
	if h.pathWithinAllowedRoots(filepath.Join(outside, "notes.md")) {
		t.Fatal("grant leaked into pathWithinAllowedRoots")
	}
}

// The list endpoint reports the current grants.
func TestFileGrant_ListReportsGrants(t *testing.T) {
	h, _ := newFilesHandler(t)
	outside := t.TempDir()
	root := grantPath(t, h, outside)

	rec := httptest.NewRecorder()
	h.HandleListFileGrants(rec, httptest.NewRequest("GET", "/api/files/grants", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Roots []string `json:"roots"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, r := range resp.Roots {
		if r == root {
			found = true
		}
	}
	if !found {
		t.Fatalf("grant list %v does not contain %q", resp.Roots, root)
	}
}

// An absolute path with no project_root used to be UNSCOPED on the read
// endpoints. It must now be confined.
func TestFileContent_AbsolutePathWithoutRootIsScoped(t *testing.T) {
	h, _ := newFilesHandler(t)
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(target, []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	h.HandleFileContent(rec, httptest.NewRequest("GET", "/api/files/content?path="+url.QueryEscape(target), nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("content: status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}

	// The raw endpoint had the same hole.
	if err := os.WriteFile(filepath.Join(outside, "pic.png"), []byte("PNG"), 0644); err != nil {
		t.Fatal(err)
	}
	rec2 := httptest.NewRecorder()
	h.HandleFileRaw(rec2, httptest.NewRequest("GET", "/api/files/raw?path="+url.QueryEscape(filepath.Join(outside, "pic.png")), nil))
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("raw: status = %d, want 400 (body=%s)", rec2.Code, rec2.Body.String())
	}
}

// A granted directory is browsable as a tree root too, so the Files tab can
// list it.
func TestFileGrant_GrantedDirIsTreeRoot(t *testing.T) {
	h, _ := newFilesHandler(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "a.txt"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	root := grantPath(t, h, outside)

	rec := httptest.NewRecorder()
	h.HandleFileTree(rec, httptest.NewRequest("GET", "/api/files/tree?path="+url.QueryEscape(root)+"&depth=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("tree: status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "a.txt") {
		t.Fatalf("tree body = %s, want a.txt", rec.Body.String())
	}
}
