package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/u007/ocode/internal/remote"
)

// browseConfigFor drives /api/browse/config?host=<host> through the Server,
// with the browser's own Host header (the SPA's origin) set on the request.
func browseConfigFor(t *testing.T, s *Server, host, browserHost string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("GET", "/api/browse/config?host="+host, nil)
	r.Host = browserHost
	w := httptest.NewRecorder()
	s.handleBrowseConfig(w, r)
	return w
}

// TestRemoteBrowseConfigUsesHostBrowseOrigin: a remote project's browse base
// is the local end of the workspace's browse tunnel (same port number as the
// host's browse origin), rewritten to the browser's loopback name so the
// browse cookie stays same-site with the SPA.
func TestRemoteBrowseConfigUsesHostBrowseOrigin(t *testing.T) {
	ws := &fakeTestWorkspace{state: remote.ServeState{BrowsePort: 40263}}
	h := newTestProxyHandler(t, "user@realhost", "/home/user/project", ws)
	s := &Server{handler: h, rl: newRateLimiter()}
	s.browseProxyPort = 9999

	w := browseConfigFor(t, s, "user@realhost", "localhost:9999")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body %q, want 200", w.Code, w.Body.String())
	}
	var body struct {
		BaseURL    string `json:"base_url"`
		RemoteMode bool   `json:"remote_mode"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if want := "http://localhost:9999/api/remote/user@realhost/browse"; body.BaseURL != want {
		t.Errorf("base_url = %q, want %q", body.BaseURL, want)
	}
	if !body.RemoteMode {
		t.Error("remote_mode = false, want true for a remote host")
	}
}

// TestRemoteBrowseConfigRejectsUnsavedHost: a host with no saved project is
// never connected to, and never answered with this machine's browse origin.
func TestRemoteBrowseConfigRejectsUnsavedHost(t *testing.T) {
	ws := &fakeTestWorkspace{state: remote.ServeState{BrowsePort: 40263}}
	h := newTestProxyHandler(t, "user@realhost", "/home/user/project", ws)
	s := &Server{handler: h}

	w := browseConfigFor(t, s, "evil@elsewhere", "localhost:9999")
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d body %q, want 403", w.Code, w.Body.String())
	}
}

// TestRemoteBrowseConfigFailsWithoutBrowseOrigin: a remote that reports no
// browse origin is an error. It must not fall back to a local base, which
// would make localhost resolve on this machine instead of the host.
func TestRemoteBrowseConfigFailsWithoutBrowseOrigin(t *testing.T) {
	ws := &fakeTestWorkspace{state: remote.ServeState{BrowsePort: 0}}
	h := newTestProxyHandler(t, "user@realhost", "/home/user/project", ws)
	s := &Server{handler: h}

	w := browseConfigFor(t, s, "user@realhost", "localhost:9999")
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d body %q, want 502", w.Code, w.Body.String())
	}
}
