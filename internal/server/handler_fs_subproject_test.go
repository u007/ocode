package server

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A ?project= that lives INSIDE a registered root is a legal mutation scope
// (the file tree browses into a subfolder and sends it). The operation is
// anchored there, so containment still applies — see the escape tests below.
func TestFSSubDirectoryProjectAccepted(t *testing.T) {
	h := newGitTestHandler(t, false)
	sub := filepath.Join(h.workDir, "src")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	r, w := call("/api/fs/new-file?project="+url.QueryEscape(sub), map[string]any{"path": "main.go"})
	h.HandleFSNewFile(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("new-file with a sub-directory project failed: %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(sub, "main.go")); err != nil {
		t.Fatalf("created file missing in the sub-directory: %v", err)
	}
}

func TestFSSubDirectoryProjectOutsideRejected(t *testing.T) {
	h := newGitTestHandler(t, false)
	outside := t.TempDir()
	r, w := call("/api/fs/new-file?project="+url.QueryEscape(outside), map[string]any{"path": "x.txt"})
	h.HandleFSNewFile(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a directory outside every project, got %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "unknown project") {
		t.Fatalf("error should name the admission rule, got %s", w.Body.String())
	}
}

// The read path accepts a lexically-inside path through a symlink so linked
// folders can be browsed; a WRITE must not, or the anchor would sit outside the
// project.
func TestFSSubDirectoryProjectSymlinkEscapeRejected(t *testing.T) {
	h := newGitTestHandler(t, false)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(h.workDir, "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	r, w := call("/api/fs/new-file?project="+url.QueryEscape(filepath.Join(h.workDir, "link")),
		map[string]any{"path": "escaped.txt"})
	h.HandleFSNewFile(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a symlinked project path, got %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(outside, "escaped.txt")); !os.IsNotExist(err) {
		t.Fatalf("a file was created outside the project: %v", err)
	}
}

// A relative ?project= must never be resolved against the server process CWD
// (a Finder/Dock-launched desktop app starts anywhere).
func TestFSRelativeProjectParamRejected(t *testing.T) {
	h := newGitTestHandler(t, false)
	if err := os.MkdirAll(filepath.Join(h.workDir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	r, w := call("/api/fs/new-file?project=src", map[string]any{"path": "x.txt"})
	h.HandleFSNewFile(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a relative project param, got %d %s", w.Code, w.Body.String())
	}
}
