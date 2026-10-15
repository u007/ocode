package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

// Expanding a folder in the Files tree requests a subdirectory of a remote
// project as ?path=. The registered project is only the root, so admission
// must accept any path inside it, and returned Path values must stay anchored
// to the project root: the frontend passes a child's Path back as
// projectRoot/Path on the next expand.
func TestRemoteFileTreeExpandsSubdirectory(t *testing.T) {
	installFakeSSH(t)
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, "top.txt"), "top\n")
	if err := os.MkdirAll(filepath.Join(repo, "src", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repo, "src", "a.go"), "package a\n")
	writeFile(t, filepath.Join(repo, "src", "nested", "b.go"), "package b\n")

	h := newTestHandlerWithRemote(t, "ci.local", repo)

	var root FileTreeResponse
	getJSON(t, h, "/api/files/tree?host=ci.local&depth=1&path="+url.QueryEscape(repo), &root)
	if len(root.Children) != 2 {
		t.Fatalf("root children = %+v, want src/ and top.txt", root.Children)
	}

	var src FileTreeResponse
	w := getJSON(t, h, "/api/files/tree?host=ci.local&depth=1&path="+url.QueryEscape(repo+"/src"), &src)
	if w.Code != http.StatusOK {
		t.Fatalf("expand src: status %d: %s", w.Code, w.Body.String())
	}
	got := map[string]bool{}
	for _, c := range src.Children {
		got[c.Path] = c.IsDir
	}
	if _, ok := got["src/a.go"]; !ok {
		t.Fatalf("expand src children = %v, want project-root-relative src/a.go", got)
	}
	if !got["src/nested"] {
		t.Fatalf("expand src children = %v, want dir src/nested", got)
	}

	var nested FileTreeResponse
	w = getJSON(t, h, "/api/files/tree?host=ci.local&depth=1&path="+url.QueryEscape(repo+"/src/nested"), &nested)
	if w.Code != http.StatusOK {
		t.Fatalf("expand src/nested: status %d: %s", w.Code, w.Body.String())
	}
	if len(nested.Children) != 1 || nested.Children[0].Path != "src/nested/b.go" {
		t.Fatalf("expand src/nested children = %+v, want src/nested/b.go", nested.Children)
	}
}

// Git badges are computed in the walked subdirectory's frame; they must still
// land on the right child after the paths are re-anchored to the project root.
func TestRemoteFileTreeExpandBadgesChangedFile(t *testing.T) {
	installFakeSSH(t)
	repo := t.TempDir()
	initGitRepo(t, repo)
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repo, "src", "a.go"), "package a\n")
	writeFile(t, filepath.Join(repo, "src", "keep.go"), "package keep\n")
	run(t, repo, "git", "add", ".")
	run(t, repo, "git", "commit", "-m", "init")
	writeFile(t, filepath.Join(repo, "src", "a.go"), "package a // edited\n")

	h := newTestHandlerWithRemote(t, "ci.local", repo)
	var src FileTreeResponse
	w := getJSON(t, h, "/api/files/tree?host=ci.local&depth=1&path="+url.QueryEscape(repo+"/src"), &src)
	if w.Code != http.StatusOK {
		t.Fatalf("expand src: status %d: %s", w.Code, w.Body.String())
	}
	badges := map[string]string{}
	for _, c := range src.Children {
		badges[c.Path] = c.GitStatus
	}
	if badges["src/a.go"] != "M" {
		t.Errorf("src/a.go badge = %q, want M (badges=%v)", badges["src/a.go"], badges)
	}
	if badges["src/keep.go"] != "" {
		t.Errorf("src/keep.go badge = %q, want none", badges["src/keep.go"])
	}
}

// Admission widened from exact project path to containment must not admit
// paths that merely share the project root's string prefix, or that escape it.
func TestRemoteFileTreeRejectsPathsOutsideProject(t *testing.T) {
	installFakeSSH(t)
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, "top.txt"), "top\n")

	h := newTestHandlerWithRemote(t, "ci.local", repo)
	for _, p := range []string{repo + "2", repo + "2/x", repo + "/../other", repo + "/src/../../etc"} {
		req := httptest.NewRequest(http.MethodGet, "/api/files/tree?host=ci.local&depth=1&path="+url.QueryEscape(p), nil)
		rr := httptest.NewRecorder()
		h.HandleFileTree(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("path %q: status = %d, want 400 (body=%s)", p, rr.Code, rr.Body.String())
		}
	}
}
