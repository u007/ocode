package server

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/u007/ocode/internal/browse"
)

// TestSmokeBrowseProxySecondListener verifies the second listener binds,
// serves the gate, and Shutdown closes it. This is the closest thing to a
// real smoke test we can run in-process.
func TestSmokeBrowseProxySecondListener(t *testing.T) {
	bs := browse.New("", nil)
	s := New("127.0.0.1:0", "", "", nil)
	s.EnableBrowse("http://127.0.0.1:54321", bs)
	// Manually bind the second listener (as StartBrowse does).
	proxyLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	proxyPort := proxyLn.Addr().(*net.TCPAddr).Port
	proxyMux := http.NewServeMux()
	proxyMux.Handle("/b/", s.browseProxyGate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.browseProxy.ServeHTTP(w, r)
	})))
	proxyHS := &http.Server{Handler: proxyMux}
	s.shutdownMu.Lock()
	s.browseProxyLn = proxyLn
	s.browseProxyPort = proxyPort
	s.browseProxyHS = proxyHS
	s.shutdownMu.Unlock()
	go func() { _ = proxyHS.Serve(proxyLn) }()

	// 1. Gate rejects ungranted.
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/b/tab:x/https/example.com/", proxyPort))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("ungranted: status = %d, want 401", resp.StatusCode)
	}

	// 2. handleBrowseConfig returns the second listener URL.
	r := httptest.NewRequest("GET", "/api/browse/config", nil)
	r.Host = "localhost:4096"
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("config: status = %d", w.Code)
	}
	t.Logf("config base_url = %s", w.Body.String())

	// 3. Shutdown closes the listener.
	ctx := t.Context()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	// Verify listener is closed
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", proxyPort))
	if err == nil {
		conn.Close()
		t.Fatal("listener still accepting after shutdown")
	}
}
