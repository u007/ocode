package server

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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

// TestRemoteBrowseConfig_HTS_WhenTLS verifies that when the browse listener
// serves TLS (localTLS set) and the request arrives over a real TLS connection,
// the advertised remote browse base URL uses https://.
func TestRemoteBrowseConfig_HTS_WhenTLS(t *testing.T) {
	ws := &fakeTestWorkspace{state: remote.ServeState{BrowsePort: 40263}}
	h := newTestProxyHandler(t, "user@realhost", "/home/user/project", ws)
	s := &Server{handler: h, rl: newRateLimiter()}
	s.browseProxyPort = 9999
	s.localTLS = &LocalCert{}

	r := httptest.NewRequest("GET", "/api/browse/config?host=user@realhost", nil)
	r.Host = "localhost:9999"
	r.TLS = &tls.ConnectionState{}
	w := httptest.NewRecorder()
	s.handleBrowseConfig(w, r)
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
	if want := "https://localhost:9999/api/remote/user@realhost/browse"; body.BaseURL != want {
		t.Errorf("base_url = %q, want %q", body.BaseURL, want)
	}
	if !body.RemoteMode {
		t.Error("remote_mode = false, want true for a remote host")
	}
}

// TestRemoteBrowseConfig_IPv6HostKeepsBrackets verifies that an IPv6 Host header
// yields a base URL that parses with the address intact. Both a bracketed host
// with a port and a bare bracketed host must survive the join.
func TestRemoteBrowseConfig_IPv6HostKeepsBrackets(t *testing.T) {
	for _, reqHost := range []string{"[::1]:4096", "[::1]"} {
		ws := &fakeTestWorkspace{state: remote.ServeState{BrowsePort: 40263}}
		h := newTestProxyHandler(t, "user@realhost", "/home/user/project", ws)
		s := &Server{handler: h, rl: newRateLimiter()}
		s.browseProxyPort = 9999

		r := httptest.NewRequest("GET", "/api/browse/config?host=user@realhost", nil)
		r.Host = reqHost
		w := httptest.NewRecorder()
		s.handleBrowseConfig(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("Host %s: status = %d body %q, want 200", reqHost, w.Code, w.Body.String())
		}
		var body struct {
			BaseURL string `json:"base_url"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		u, err := url.Parse(body.BaseURL)
		if err != nil {
			t.Fatalf("Host %s: base_url %q does not parse: %v", reqHost, body.BaseURL, err)
		}
		if u.Hostname() != "::1" {
			t.Errorf("Host %s: base_url hostname = %q, want ::1", reqHost, u.Hostname())
		}
		if u.Port() != "9999" {
			t.Errorf("Host %s: base_url port = %q, want 9999", reqHost, u.Port())
		}
	}
}

// TestRemoteBrowseConfig_HTTP_WithoutTLS verifies that without TLS, the remote
// browse base URL uses http://.
func TestRemoteBrowseConfig_HTTP_WithoutTLS(t *testing.T) {
	ws := &fakeTestWorkspace{state: remote.ServeState{BrowsePort: 40263}}
	h := newTestProxyHandler(t, "user@realhost", "/home/user/project", ws)
	s := &Server{handler: h, rl: newRateLimiter()}
	s.browseProxyPort = 9999

	r := httptest.NewRequest("GET", "/api/browse/config?host=user@realhost", nil)
	r.Host = "localhost:9999"
	w := httptest.NewRecorder()
	s.handleBrowseConfig(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body %q, want 200", w.Code, w.Body.String())
	}
	var body struct {
		BaseURL string `json:"base_url"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if want := "http://localhost:9999/api/remote/user@realhost/browse"; body.BaseURL != want {
		t.Errorf("base_url = %q, want %q", body.BaseURL, want)
	}
}

// TestRemoteBrowseConfigPrefersSharedBrowseURL: a tailnet client (non-loopback
// peer) must be handed the SHARED browse URL for a remote project too — the
// bare proxy port is not reachable from a phone, so its iframe loaded nothing.
func TestRemoteBrowseConfigPrefersSharedBrowseURL(t *testing.T) {
	ws := &fakeTestWorkspace{state: remote.ServeState{BrowsePort: 40263}}
	h := newTestProxyHandler(t, "user@realhost", "/home/user/project", ws)
	h.tailscaleShareSnapshot = func() tailscaleShareStatus {
		return tailscaleShareStatus{Running: true, BrowseURL: "https://desk.tailnet.ts.net:8444"}
	}
	s := &Server{handler: h, rl: newRateLimiter()}
	s.browseProxyPort = 9999

	// httptest.NewRequest's peer is 192.0.2.1, a non-loopback client.
	w := browseConfigFor(t, s, "user@realhost", "desk.tailnet.ts.net:8443")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body %q, want 200", w.Code, w.Body.String())
	}
	var body struct {
		BaseURL string `json:"base_url"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if want := "https://desk.tailnet.ts.net:8444/api/remote/user@realhost/browse"; body.BaseURL != want {
		t.Errorf("base_url = %q, want %q", body.BaseURL, want)
	}
}
