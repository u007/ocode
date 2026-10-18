package server

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/u007/ocode/internal/browse"
)

// browseGateServer builds a Server with the browse proxy routes registered
// on the second listener (pointing at a stub loopback origin) so the gate
// can be exercised through the real listener.
func browseGateServer(t *testing.T, upstream *httptest.Server) (*Server, string) {
	t.Helper()
	s := New("127.0.0.1:0", "", "", nil)
	s.EnableBrowse(upstream.URL, browse.New("", nil))
	proxyLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { proxyLn.Close() })
	proxyPort := proxyLn.Addr().(*net.TCPAddr).Port
	proxyMux := http.NewServeMux()
	proxyMux.Handle("/b/", s.browseProxyGate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.browseProxy.ServeHTTP(w, r)
	})))
	proxyMux.Handle("/__ocode_capture.js", s.browseProxyGate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.browseProxy.ServeHTTP(w, r)
	})))
	proxyMux.HandleFunc("/api/remote/{host}/browse/{rest...}", s.browseProxyGate(func(w http.ResponseWriter, r *http.Request) {
		s.handleRemoteBrowseProxy(w, r, r.PathValue("host"))
	}))
	proxyHS := &http.Server{Handler: proxyMux}
	s.shutdownMu.Lock()
	s.browseProxyLn = proxyLn
	s.browseProxyPort = proxyPort
	s.browseProxyHS = proxyHS
	s.shutdownMu.Unlock()
	go func() { _ = proxyHS.Serve(proxyLn) }()
	base := fmt.Sprintf("http://127.0.0.1:%d", proxyPort)
	return s, base
}

// doProxy issues a request to the second listener and returns the response.
func doProxy(t *testing.T, base, path string, fn func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	req, err := http.NewRequest("GET", base+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if fn != nil {
		fn(req)
	}
	rr := httptest.NewRecorder()
	// Use a real client to hit the listener
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	rr.Code = resp.StatusCode
	return rr
}

// TestBrowseProxyGateRejectsUngranted: a request with neither a `__grant`
// query param nor an `ocode_browse` cookie is refused by the gate itself
// (401), so it never reaches the loopback upstream.
func TestBrowseProxyGateRejectsUngranted(t *testing.T) {
	var upstreamHits int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits++
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	_, base := browseGateServer(t, upstream)

	resp, err := http.Get(base + "/b/tab:x/https/example.com/")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	if upstreamHits != 0 {
		t.Fatalf("upstream was reached %d times, want 0 (gate must reject before proxying)", upstreamHits)
	}
}

// TestBrowseProxyGateAllowsGrant: a request carrying a `__grant` passes the
// gate (the upstream then does its own redemption).
func TestBrowseProxyGateAllowsGrant(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	_, base := browseGateServer(t, upstream)

	resp, err := http.Get(base + "/b/tab:x/https/example.com/?__grant=abc")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (grant must pass the gate)", resp.StatusCode)
	}
}

// TestBrowseProxyGateAllowsCookie: a request carrying the browse session
// cookie (no grant) passes the gate.
func TestBrowseProxyGateAllowsCookie(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	_, base := browseGateServer(t, upstream)

	req, _ := http.NewRequest("GET", base+"/b/tab:x/https/example.com/style.css", nil)
	req.AddCookie(&http.Cookie{Name: "ocode_browse", Value: "sess"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (cookie must pass the gate)", resp.StatusCode)
	}
}

// TestBrowseProxyGateRateLimits: five ungranted probes from one IP lock it
// out; the sixth is 429 even though it carries a grant.
func TestBrowseProxyGateRateLimits(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	_, base := browseGateServer(t, upstream)

	for i := 0; i < 5; i++ {
		resp, err := http.Get(base + "/b/tab:x/https/example.com/")
		if err != nil {
			t.Fatalf("probe %d: %v", i, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("probe %d: status = %d, want 401", i, resp.StatusCode)
		}
	}
	// Now blocked — even a well-formed request is refused.
	resp, err := http.Get(base + "/b/tab:x/https/example.com/?__grant=abc")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 after lockout", resp.StatusCode)
	}
}
