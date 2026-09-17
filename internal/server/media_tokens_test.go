package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// postMediaToken drives the real mux so the auth middleware is exercised.
func postMediaToken(s *Server, body, bearer string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/api/files/media-token", strings.NewReader(body))
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	return rec
}

func rawGet(s *Server, query, bearer string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/api/files/raw?"+query, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	return rec
}

func issueToken(t *testing.T, s *Server, body string) string {
	t.Helper()
	rec := postMediaToken(s, body, "secret")
	if rec.Code != http.StatusOK {
		t.Fatalf("issue status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("bad issue response: %v", err)
	}
	if out.Token == "" {
		t.Fatal("empty token")
	}
	return out.Token
}

// TestMediaTokenIssuanceGuards pins the issuing endpoint's validation: auth
// required, media extensions only, and local-only (remote media keeps the
// buffered blob path).
func TestMediaTokenIssuanceGuards(t *testing.T) {
	dir := t.TempDir()
	s := New("127.0.0.1:0", "", "secret", nil)
	s.SetWorkDir(dir)

	if rec := postMediaToken(s, `{"path":"clip.mp4"}`, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated issue = %d, want 401", rec.Code)
	}
	if rec := postMediaToken(s, `{"path":"report.pdf"}`, "secret"); rec.Code != http.StatusBadRequest {
		t.Errorf("non-media issue = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
	if rec := postMediaToken(s, `{"path":"clip.mp4","host":"ci.local"}`, "secret"); rec.Code != http.StatusBadRequest {
		t.Errorf("remote issue = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
	if rec := postMediaToken(s, `{"path":""}`, "secret"); rec.Code != http.StatusBadRequest {
		t.Errorf("empty path issue = %d, want 400", rec.Code)
	}
	if rec := postMediaToken(s, `{"path":"clip.mp4"}`, "secret"); rec.Code != http.StatusOK {
		t.Errorf("media issue = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
}

// TestMediaTokenGrantsExactlyOneFile: the capability authorizes the exact
// (path, project_root, host) triple and nothing else — never another file,
// never a non-media file, and never on its own without the endpoint's auth.
func TestMediaTokenGrantsExactlyOneFile(t *testing.T) {
	dir := t.TempDir()
	payload := []byte("0123456789abcdef")
	if err := os.WriteFile(filepath.Join(dir, "clip.mp4"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.mp4"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	s := New("127.0.0.1:0", "", "secret", nil)
	s.SetWorkDir(dir)

	tok := issueToken(t, s, `{"path":"clip.mp4"}`)

	rec := rawGet(s, "path=clip.mp4&media_token="+tok, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("capability read = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != string(payload) {
		t.Error("body bytes mismatch")
	}

	// Another file with the same token is rejected.
	if rec := rawGet(s, "path=other.mp4&media_token="+tok, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("cross-file capability read = %d, want 401", rec.Code)
	}
	// No token at all is rejected.
	if rec := rawGet(s, "path=clip.mp4", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("tokenless read = %d, want 401", rec.Code)
	}
	// A bogus token is rejected.
	if rec := rawGet(s, "path=clip.mp4&media_token=deadbeef", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("bogus token read = %d, want 401", rec.Code)
	}
	// The normal credential still works (the middleware didn't narrow it).
	if rec := rawGet(s, "path=clip.mp4", "secret"); rec.Code != http.StatusOK {
		t.Errorf("bearer read = %d, want 200", rec.Code)
	}
	// A media token cannot authorize a non-media file: the ext gate keeps it
	// off the capability path entirely.
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if rec := rawGet(s, "path=notes.txt&media_token="+tok, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("capability on non-media = %d, want 401", rec.Code)
	}
}

// TestMediaRawStreamsRange: local media is served with range support (206 +
// Content-Range) so a player can seek without pulling the whole file.
func TestMediaRawStreamsRange(t *testing.T) {
	dir := t.TempDir()
	payload := []byte("0123456789abcdef")
	if err := os.WriteFile(filepath.Join(dir, "clip.mp4"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	s := New("127.0.0.1:0", "", "", nil) // no auth configured → mux bypass
	s.SetWorkDir(dir)

	// Full read.
	full := rawGet(s, "path=clip.mp4", "")
	if full.Code != http.StatusOK {
		t.Fatalf("full read = %d, want 200", full.Code)
	}
	if full.Body.String() != string(payload) {
		t.Error("full body mismatch")
	}
	if got := full.Header().Get("Content-Type"); got != "video/mp4" {
		t.Errorf("content-type = %q, want video/mp4", got)
	}

	// Range read.
	req := httptest.NewRequest("GET", "/api/files/raw?path=clip.mp4", nil)
	req.Header.Set("Range", "bytes=0-3")
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("range read = %d, want 206", rec.Code)
	}
	if rec.Body.String() != "0123" {
		t.Errorf("range body = %q, want %q", rec.Body.String(), "0123")
	}
	if cr := rec.Header().Get("Content-Range"); cr == "" {
		t.Error("missing Content-Range on a partial response")
	}
}

// TestMediaTokenRejectionsDoNotLockOutTheIP: a bad capability attempt without
// a credential must not feed the credential rate limiter, or a media element's
// retry loop would lock the client out of the whole API. A request that also
// presents a credential is still counted.
func TestMediaTokenRejectionsDoNotLockOutTheIP(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "clip.mp4"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New("127.0.0.1:0", "", "secret", nil)
	s.SetWorkDir(dir)

	// 6 bad capabilities, no credential — comfortably past the 5-failure
	// threshold if they were being counted.
	for i := 0; i < 6; i++ {
		if rec := rawGet(s, "path=clip.mp4&media_token=bogus", ""); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d, want 401", i, rec.Code)
		}
	}
	// The credential path is untouched.
	if rec := rawGet(s, "path=clip.mp4", "secret"); rec.Code != http.StatusOK {
		t.Fatalf("bearer after capability attempts = %d, want 200", rec.Code)
	}
	// A fresh valid capability still works.
	tok := issueToken(t, s, `{"path":"clip.mp4"}`)
	if rec := rawGet(s, "path=clip.mp4&media_token="+tok, ""); rec.Code != http.StatusOK {
		t.Fatalf("valid capability after attempts = %d, want 200", rec.Code)
	}

	// Contrast: wrong-bearer attempts DO trip the limiter (429 on the 6th).
	for i := 0; i < 5; i++ {
		rawGet(s, "path=clip.mp4&media_token=bogus", "wrong")
	}
	if rec := rawGet(s, "path=clip.mp4", "secret"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("after wrong-bearer attempts = %d, want 429", rec.Code)
	}
}
