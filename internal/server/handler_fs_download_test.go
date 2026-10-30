package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func downloadURLHost(host, project, p string) string {
	v := url.Values{}
	v.Set("project", project)
	v.Set("path", p)
	if host != "" {
		v.Set("host", host)
	}
	return "/api/fs/download?" + v.Encode()
}

func downloadGET(t *testing.T, h *Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.HandleFSDownload(w, httptest.NewRequest(http.MethodGet, target, nil))
	return w
}

func writeProjectFile(t *testing.T, dir, rel string, data []byte) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestHandleFSDownloadStreamsFileAsAttachment(t *testing.T) {
	h := newGitTestHandler(t, false)
	writeProjectFile(t, h.workDir, "notes.txt", []byte("hello bytes"))

	w := downloadGET(t, h, downloadURLHost("", h.workDir, "notes.txt"))
	if w.Code != http.StatusOK {
		t.Fatalf("download = %d body=%s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != "hello bytes" {
		t.Fatalf("body = %q", got)
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, `attachment; filename="notes.txt"`) {
		t.Fatalf("Content-Disposition = %q", cd)
	}
	if cl := w.Header().Get("Content-Length"); cl != "11" {
		t.Fatalf("Content-Length = %q, want 11", cl)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Fatalf("Content-Type = %q", ct)
	}
	if nosniff := w.Header().Get("X-Content-Type-Options"); nosniff != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", nosniff)
	}
}

func TestAttachmentDispositionEncodesNames(t *testing.T) {
	cases := map[string]string{
		"plain.txt":   `attachment; filename="plain.txt"; filename*=UTF-8''plain.txt`,
		`a"b.txt`:     `attachment; filename="a_b.txt"; filename*=UTF-8''a%22b.txt`,
		"café (1).md": `attachment; filename="caf_ (1).md"; filename*=UTF-8''caf%C3%A9%20%281%29.md`,
	}
	for name, want := range cases {
		if got := attachmentDisposition(name); got != want {
			t.Errorf("attachmentDisposition(%q)\n got %q\nwant %q", name, got, want)
		}
	}
}

func TestHandleFSDownloadRefusesDirectory(t *testing.T) {
	h := newGitTestHandler(t, false)
	if err := os.MkdirAll(filepath.Join(h.workDir, "folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	w := downloadGET(t, h, downloadURLHost("", h.workDir, "folder"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("directory download = %d body=%s, want 400", w.Code, w.Body.String())
	}
}

func TestHandleFSDownloadRefusesOutsideProject(t *testing.T) {
	h := newGitTestHandler(t, false)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := downloadGET(t, h, downloadURLHost("", h.workDir, "../"+filepath.Base(filepath.Dir(outside))+"/secret.txt"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("traversal download = %d body=%s, want 400", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "nope") {
		t.Fatalf("outside file content leaked")
	}

	link := filepath.Join(h.workDir, "link.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	w = downloadGET(t, h, downloadURLHost("", h.workDir, "link.txt"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("symlink-out download = %d body=%s, want 400", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "nope") {
		t.Fatalf("symlink target content leaked")
	}
}

func TestHandleFSDownloadMissingIs400(t *testing.T) {
	h := newGitTestHandler(t, false)
	w := downloadGET(t, h, downloadURLHost("", h.workDir, "absent.txt"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing download = %d body=%s, want 400", w.Code, w.Body.String())
	}
}

func TestHandleFSDownloadOverCapIs413(t *testing.T) {
	h := newGitTestHandler(t, false)
	writeProjectFile(t, h.workDir, "big.bin", []byte("0123456789"))
	prev := fsDownloadMaxBytes
	fsDownloadMaxBytes = 4
	t.Cleanup(func() { fsDownloadMaxBytes = prev })

	w := downloadGET(t, h, downloadURLHost("", h.workDir, "big.bin"))
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("over-cap download = %d body=%s, want 413", w.Code, w.Body.String())
	}
}

func TestFSDownloadRouteIsRegistered(t *testing.T) {
	home := t.TempDir()
	setHomeTree(t, home)
	srv := New("127.0.0.1:0", "", "", nil)
	dir := t.TempDir()
	srv.handler.workDir = dir
	writeProjectFile(t, dir, "routed.txt", []byte("via mux"))
	mux := srv.serveHandler()

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, downloadURLHost("", dir, "routed.txt"), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/fs/download = %d body=%s (404 means the route is missing)", rec.Code, rec.Body.String())
	}
	if body, _ := io.ReadAll(rec.Body); string(body) != "via mux" {
		t.Fatalf("body = %q", body)
	}
}

func TestHandleFSDownloadRemoteStreamsOverSSH(t *testing.T) {
	installFakeSSH(t)
	remoteDir := t.TempDir()
	const host = "user@example-host"
	h := newTestHandlerWithRemote(t, host, remoteDir)
	writeProjectFile(t, remoteDir, "docs/readme.md", []byte("remote bytes"))

	w := downloadGET(t, h, downloadURLHost(host, remoteDir, "docs/readme.md"))
	if w.Code != http.StatusOK {
		t.Fatalf("remote download = %d body=%s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != "remote bytes" {
		t.Fatalf("body = %q", got)
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, `filename="readme.md"`) {
		t.Fatalf("Content-Disposition = %q", cd)
	}
	if cl := w.Header().Get("Content-Length"); cl != "12" {
		t.Fatalf("Content-Length = %q, want 12", cl)
	}
}

func TestHandleFSDownloadRemoteEmptyFile(t *testing.T) {
	installFakeSSH(t)
	remoteDir := t.TempDir()
	const host = "user@example-host"
	h := newTestHandlerWithRemote(t, host, remoteDir)
	writeProjectFile(t, remoteDir, "empty.txt", nil)

	w := downloadGET(t, h, downloadURLHost(host, remoteDir, "empty.txt"))
	if w.Code != http.StatusOK {
		t.Fatalf("empty remote download = %d body=%s", w.Code, w.Body.String())
	}
	if w.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty", w.Body.String())
	}
}

func TestHandleFSDownloadRemoteRefusesDirectory(t *testing.T) {
	installFakeSSH(t)
	remoteDir := t.TempDir()
	const host = "user@example-host"
	h := newTestHandlerWithRemote(t, host, remoteDir)
	if err := os.MkdirAll(filepath.Join(remoteDir, "folder"), 0o755); err != nil {
		t.Fatal(err)
	}

	w := downloadGET(t, h, downloadURLHost(host, remoteDir, "folder"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("remote directory download = %d body=%s, want 400", w.Code, w.Body.String())
	}
}

func TestHandleFSDownloadRemoteRefusesOutsideProject(t *testing.T) {
	installFakeSSH(t)
	remoteDir := t.TempDir()
	const host = "user@example-host"
	h := newTestHandlerWithRemote(t, host, remoteDir)

	w := downloadGET(t, h, downloadURLHost(host, remoteDir, "../../etc/passwd"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("remote traversal download = %d body=%s, want 400", w.Code, w.Body.String())
	}
}

func TestHandleFSDownloadRemoteOverCapIs413(t *testing.T) {
	installFakeSSH(t)
	remoteDir := t.TempDir()
	const host = "user@example-host"
	h := newTestHandlerWithRemote(t, host, remoteDir)
	writeProjectFile(t, remoteDir, "big.bin", []byte("0123456789"))
	prev := fsDownloadRemoteMaxBytes
	fsDownloadRemoteMaxBytes = 4
	t.Cleanup(func() { fsDownloadRemoteMaxBytes = prev })

	w := downloadGET(t, h, downloadURLHost(host, remoteDir, "big.bin"))
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("remote over-cap download = %d body=%s, want 413", w.Code, w.Body.String())
	}
}
