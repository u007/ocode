package server

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/browse"
)

func TestBrowseConfigEndpoint(t *testing.T) {
	s := New("127.0.0.1:0", "", "", nil) // no-auth loopback form
	s.EnableBrowse("http://127.0.0.1:54321", browse.New("", nil))
	r := httptest.NewRequest("GET", "/api/browse/config", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	var body struct {
		BaseURL string `json:"base_url"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.BaseURL != "http://127.0.0.1:54321" {
		t.Fatalf("base_url = %q", body.BaseURL)
	}
}

// The browse cookie is SameSite=Lax, so the iframe origin must be same-site
// with the SPA. Chrome treats localhost and 127.0.0.1 as different sites:
// the advertised base must reuse the loopback hostname the SPA was loaded
// from, otherwise the cookie is dropped on the iframe redirect and every
// local-mode navigation dies with 401.
func TestBrowseConfigEndpointReusesRequestLoopbackHost(t *testing.T) {
	cases := []struct{ reqHost, want string }{
		{"localhost:4096", "http://localhost:54321"},
		{"app.localhost:4096", "http://app.localhost:54321"},
		{"[::1]:4096", "http://[::1]:54321"},
		{"127.0.0.1:4096", "http://127.0.0.1:54321"},
		{"192.168.1.20:4096", "http://127.0.0.1:54321"}, // not loopback: leave as-is
	}
	for _, tc := range cases {
		s := New("127.0.0.1:0", "", "", nil)
		s.EnableBrowse("http://127.0.0.1:54321", browse.New("", nil))
		r := httptest.NewRequest("GET", "/api/browse/config", nil)
		r.Host = tc.reqHost
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, r)
		var body struct {
			BaseURL string `json:"base_url"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: decode: %v", tc.reqHost, err)
		}
		if body.BaseURL != tc.want {
			t.Errorf("Host %s: base_url = %q, want %q", tc.reqHost, body.BaseURL, tc.want)
		}
	}
}

// TestBrowseGrantEndpointRoundTrip proves the main server mints grants on the
// browse server it was wired with: the returned grant must redeem (302 +
// session cookie) on that browse server's handler.
func TestBrowseGrantEndpointRoundTrip(t *testing.T) {
	bs := browse.New("apitoken", nil)
	s := New("127.0.0.1:0", "", "", nil)
	s.EnableBrowse("http://127.0.0.1:9999", bs)

	r := httptest.NewRequest("POST", "/api/browse/grant",
		strings.NewReader(`{"state_key":"tab:abc"}`))
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("grant: got %d want 200", w.Code)
	}
	var resp struct {
		Grant string `json:"grant"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode grant: %v", err)
	}
	if resp.Grant == "" {
		t.Fatal("empty grant")
	}

	nav := httptest.NewRequest("GET", "/b/tab:abc/https/example.com/?__grant="+resp.Grant, nil)
	nw := httptest.NewRecorder()
	bs.Handler().ServeHTTP(nw, nav)
	if nw.Code != http.StatusFound {
		t.Fatalf("grant redeem on browse origin: got %d want 302", nw.Code)
	}
}

func TestBrowseGrantRejectsEmptyStateKey(t *testing.T) {
	s := New("127.0.0.1:0", "", "", nil)
	s.EnableBrowse("http://127.0.0.1:9999", browse.New("", nil))
	r := httptest.NewRequest("POST", "/api/browse/grant", strings.NewReader(`{"state_key":""}`))
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty state_key: got %d want 400", w.Code)
	}
}

func TestBrowseGrantRejectsBadJSON(t *testing.T) {
	s := New("127.0.0.1:0", "", "", nil)
	s.EnableBrowse("http://127.0.0.1:9999", browse.New("", nil))
	r := httptest.NewRequest("POST", "/api/browse/grant", strings.NewReader(`{oops`))
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad JSON: got %d want 400", w.Code)
	}
}

// The upload endpoint stores chooser files under a per-stateKey temp dir and
// hands them to the browse server. Without Chrome mode configured that
// hand-off fails (502) but the files are already on disk; revoke removes
// them.
func TestBrowseUploadEndpointSavesFilesAndRevokeCleans(t *testing.T) {
	s := New("127.0.0.1:0", "", "", nil)
	s.EnableBrowse("http://127.0.0.1:54321", browse.New("", nil))

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("state_key", "tab:up1")
	for _, name := range []string{"a.txt", "../../evil.txt"} {
		fw, _ := mw.CreateFormFile("files", name)
		_, _ = fw.Write([]byte("hello " + name))
	}
	_ = mw.Close()
	r := httptest.NewRequest("POST", "/api/browse/upload", &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d body=%s (want 502: chrome mode not configured)", w.Code, w.Body.String())
	}
	dir := browseUploadDir("tab:up1")
	for _, name := range []string{"a.txt", "evil.txt"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("expected saved file %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "..", "..", "evil.txt")); err == nil {
		t.Error("path traversal: file written outside upload dir")
	}

	// Missing state_key → 400.
	var buf2 bytes.Buffer
	mw2 := multipart.NewWriter(&buf2)
	fw, _ := mw2.CreateFormFile("files", "x")
	_, _ = fw.Write([]byte("x"))
	_ = mw2.Close()
	r2 := httptest.NewRequest("POST", "/api/browse/upload", &buf2)
	r2.Header.Set("Content-Type", mw2.FormDataContentType())
	w2 := httptest.NewRecorder()
	s.mux.ServeHTTP(w2, r2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("missing state_key: status = %d", w2.Code)
	}

	// Revoke removes the upload dir.
	r3 := httptest.NewRequest("POST", "/api/browse/revoke", strings.NewReader(`{"state_key":"tab:up1"}`))
	w3 := httptest.NewRecorder()
	s.mux.ServeHTTP(w3, r3)
	if w3.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d", w3.Code)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("upload dir still present after revoke: %v", err)
	}
}
