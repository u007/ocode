package server

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/u007/ocode/internal/version"
)

// TestHandleHealthUnauthenticatedInRemoteMode: a --remote server's own
// health probe (internal/remote/serve.go's healthProbeCmd) runs as a plain
// curl command on the remote host itself, before any tunnel or token has
// been established, and deliberately never carries a token — /api/health
// must stay reachable with no credentials in remote mode.
func TestHandleHealthUnauthenticatedInRemoteMode(t *testing.T) {
	s := New("127.0.0.1:0", "", "tok123", nil)
	s.SetRemoteMode(true)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/health", nil) // no credentials
	s.mux.ServeHTTP(w, r)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON body: %v", err)
	}
	if body.Version != version.Version {
		t.Errorf("version = %q, want %q", body.Version, version.Version)
	}
}

// TestHandleHealthRequiresAuthOutsideRemoteMode: a normal password-protected
// (non-remote) server must not expose /api/health — or anything else — with
// no credentials.
func TestHandleHealthRequiresAuthOutsideRemoteMode(t *testing.T) {
	s := New("127.0.0.1:0", "user", "pass", nil)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/health", nil) // no credentials
	s.mux.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("expected 401 with no credentials on a non-remote authenticated server, got %d: %s", w.Code, w.Body.String())
	}

	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("GET", "/api/health", nil)
	r2.SetBasicAuth("user", "pass")
	s.mux.ServeHTTP(w2, r2)
	if w2.Code != 200 {
		t.Fatalf("expected 200 with valid credentials, got %d: %s", w2.Code, w2.Body.String())
	}
}
