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

// uploadRequest builds a raw-body upload request. The body is NOT JSON — the
// endpoint takes application/octet-stream because pasted files can be binary.
func uploadRequest(t *testing.T, target string, body []byte) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, target, strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/octet-stream")
	return r
}

func uploadURL(project, destDir, name string) string {
	return uploadURLHost("", project, destDir, name)
}

// uploadURLHost adds the ?host= selector that routes an upload to a registered
// remote project instead of the local filesystem.
func uploadURLHost(host, project, destDir, name string) string {
	v := url.Values{}
	v.Set("project", project)
	v.Set("dest_dir", destDir)
	if host != "" {
		v.Set("host", host)
	}
	if name != "" {
		v.Set("name", name)
	}
	return "/api/fs/upload?" + v.Encode()
}

// uploadURLAt is the common case: destination inside the project itself.
func uploadURLAt(dir, name string) string { return uploadURL(dir, dir, name) }

func uploadOK(t *testing.T, h *Handler, r *http.Request) (string, string) {
	t.Helper()
	w := httptest.NewRecorder()
	h.HandleFSUpload(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("upload = %d body=%s", w.Code, w.Body.String())
	}
	var res struct {
		Success bool   `json:"success"`
		Path    string `json:"path"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !res.Success || res.Path == "" {
		t.Fatalf("unexpected response %+v", res)
	}
	return res.Path, w.Body.String()
}

func TestHandleFSUploadWritesRawBytes(t *testing.T) {
	h := newGitTestHandler(t, false)
	path, _ := uploadOK(t, h, uploadRequest(t, uploadURLAt(h.workDir, "pasted.bin"), []byte{0x00, 0x01, 0xff}))

	if want := filepath.Join(h.workDir, "pasted.bin"); path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != string([]byte{0x00, 0x01, 0xff}) {
		t.Fatalf("bytes = %v", got)
	}
}

func TestHandleFSUploadIntoSubdirectory(t *testing.T) {
	h := newGitTestHandler(t, false)
	sub := filepath.Join(h.workDir, "docs")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path, _ := uploadOK(t, h, uploadRequest(t, uploadURL(h.workDir, sub, "note.txt"), []byte("hello")))

	if filepath.Dir(path) != sub {
		t.Fatalf("file landed in %q, want %q", filepath.Dir(path), sub)
	}
}

// A paste must never clobber an existing file: the second upload of the same
// name gets a Finder-style "copy" name and the original bytes survive.
func TestHandleFSUploadNeverOverwrites(t *testing.T) {
	h := newGitTestHandler(t, false)
	orig := filepath.Join(h.workDir, "a.txt")
	if err := os.WriteFile(orig, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}

	path, _ := uploadOK(t, h, uploadRequest(t, uploadURLAt(h.workDir, "a.txt"), []byte("pasted")))

	if path == orig {
		t.Fatal("upload reused the existing path")
	}
	if want := filepath.Join(h.workDir, "a copy.txt"); path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	if got, _ := os.ReadFile(orig); string(got) != "original" {
		t.Fatalf("original was modified: %q", got)
	}
	if got, _ := os.ReadFile(path); string(got) != "pasted" {
		t.Fatalf("pasted file = %q", got)
	}
}

func TestHandleFSUploadRejectsUnsafeNames(t *testing.T) {
	h := newGitTestHandler(t, false)
	for _, name := range []string{
		"",
		".",
		"..",
		"a/b",
		"a\\b",
		"a:b",
		"a*b",
		"a?b",
		"a\"b",
		"a<b",
		"a>b",
		"a|b",
		"a.",
		"a ",
		"CON",
		"nul.txt",
		"lpt1.log",
		"NUL.tar.gz",
		"CON.backup.txt",
		"a\x00b",
		strings.Repeat("x", 256),
	} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.HandleFSUpload(w, uploadRequest(t, uploadURLAt(h.workDir, name), []byte("x")))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("name %q = %d body=%s, want 400", name, w.Code, w.Body.String())
			}
		})
	}
}

func TestHandleFSUploadRejectsDestOutsideProject(t *testing.T) {
	h := newGitTestHandler(t, false)
	outside := t.TempDir()
	for _, dest := range []string{outside, "../escape"} {
		w := httptest.NewRecorder()
		h.HandleFSUpload(w, uploadRequest(t, uploadURL(h.workDir, dest, "x.txt"), []byte("x")))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("dest %q = %d body=%s, want 400", dest, w.Code, w.Body.String())
		}
	}
	if _, err := os.Stat(filepath.Join(outside, "x.txt")); err == nil {
		t.Fatal("file was written outside the project")
	}
}

func TestHandleFSUploadRejectsGitDir(t *testing.T) {
	h := newGitTestHandler(t, true)
	gitDir := filepath.Join(h.workDir, ".git")
	w := httptest.NewRecorder()
	h.HandleFSUpload(w, uploadRequest(t, uploadURL(h.workDir, gitDir, "hook.sh"), []byte("x")))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("upload into .git = %d body=%s, want 400", w.Code, w.Body.String())
	}
}

func TestHandleFSUploadRejectsFileAsDestDir(t *testing.T) {
	h := newGitTestHandler(t, false)
	file := filepath.Join(h.workDir, "not-a-dir.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.HandleFSUpload(w, uploadRequest(t, uploadURL(h.workDir, file, "a.txt"), []byte("x")))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("file as dest_dir = %d body=%s, want 400", w.Code, w.Body.String())
	}
}

func TestHandleFSUploadTooLargeIs413(t *testing.T) {
	prev := fsUploadMaxBytes
	fsUploadMaxBytes = 4
	t.Cleanup(func() { fsUploadMaxBytes = prev })

	h := newGitTestHandler(t, false)
	w := httptest.NewRecorder()
	h.HandleFSUpload(w, uploadRequest(t, uploadURLAt(h.workDir, "big.txt"), []byte("way too many bytes")))
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize = %d body=%s, want 413", w.Code, w.Body.String())
	}
	// The partial file must not survive the aborted body.
	if _, err := os.Stat(filepath.Join(h.workDir, "big.txt")); err == nil {
		t.Fatal("a truncated file was left behind")
	}
}

// A handler-only test cannot catch a missing route: the SPA fallback turns
// that into a 404 the client reports as a parse error. Drive the real mux.
func TestFSUploadRouteIsRegistered(t *testing.T) {
	home := t.TempDir()
	setHomeTree(t, home)
	srv := New("127.0.0.1:0", "", "", nil)
	dir := t.TempDir()
	srv.handler.workDir = dir
	mux := srv.serveHandler()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, uploadURLAt(dir, "routed.txt"), strings.NewReader("hi"))
	req.Header.Set("Content-Type", "application/octet-stream")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/fs/upload = %d body=%s (404 means the route is missing)", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "routed.txt")); err != nil {
		t.Fatalf("file not written through the mux: %v", err)
	}
}

// The remote branch pipes the body into `cat >` over the (fake) ssh transport;
// the fake shim executes the command locally, so the bytes land in the local
// temp dir registered as the "remote" project.
func TestHandleFSUploadRemoteStreamsOverStdin(t *testing.T) {
	installFakeSSH(t)
	remoteDir := t.TempDir()
	const host = "user@example-host"
	h := newTestHandlerWithRemote(t, host, remoteDir)

	w := httptest.NewRecorder()
	h.HandleFSUpload(w, uploadRequest(t, uploadURLHost(host, remoteDir, remoteDir, "over-ssh.txt"), []byte("streamed bytes")))
	if w.Code != http.StatusOK {
		t.Fatalf("remote upload = %d body=%s", w.Code, w.Body.String())
	}
	got, err := os.ReadFile(filepath.Join(remoteDir, "over-ssh.txt"))
	if err != nil {
		t.Fatalf("read remote file: %v", err)
	}
	if string(got) != "streamed bytes" {
		t.Fatalf("content = %q", got)
	}
}

// A dangling symlink reads as "free" to the `[ -e ]` probe, but `cat >` would
// create its target. The exclusive create must refuse it and leave both the
// link and the would-be target untouched.
func TestHandleFSUploadRemoteRefusesDanglingSymlink(t *testing.T) {
	installFakeSSH(t)
	remoteDir := t.TempDir()
	const host = "user@example-host"
	h := newTestHandlerWithRemote(t, host, remoteDir)
	victim := filepath.Join(remoteDir, "victim.txt")
	link := filepath.Join(remoteDir, "linked.txt")
	if err := os.Symlink(victim, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	w := httptest.NewRecorder()
	h.HandleFSUpload(w, uploadRequest(t, uploadURLHost(host, remoteDir, remoteDir, "linked.txt"), []byte("payload")))
	if w.Code != http.StatusConflict {
		t.Fatalf("remote upload over dangling link = %d body=%s, want 409", w.Code, w.Body.String())
	}
	if _, err := os.Lstat(victim); !os.IsNotExist(err) {
		t.Fatalf("victim written through the dangling link (lstat err = %v)", err)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("link removed: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link replaced by %v, want it left as a symlink", info.Mode())
	}
}

func TestValidUploadNameAcceptsOrdinaryNames(t *testing.T) {
	for _, name := range []string{"a.txt", "Screen Shot 2026-10-27 at 10.00.00.png", "ünïcode.md", ".env.example", "Makefile", "CONFIG.tar.gz", "console.log"} {
		if err := validUploadName(name); err != nil {
			t.Fatalf("validUploadName(%q) = %v, want nil", name, err)
		}
	}
}
