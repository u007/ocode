package desktop

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/remote"
)

// TestProxy_ForwardsRequest verifies that an /api/ request reaches
// the remote server and the Authorization header carries the remote
// token from ServeState.
func TestProxy_ForwardsRequest(t *testing.T) {
	var receivedAuth string
	remoteSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		if r.URL.Query().Get("token") != "" {
			t.Errorf("remote received query token %q, want empty", r.URL.Query().Get("token"))
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer remoteSrv.Close()

	workspace := &remote.RemoteWorkspace{
		State: remote.ServeState{Token: "remote-token-123", Port: 1234, BrowsePort: 5678},
	}
	workspace.SetAPIURL(remoteSrv.URL)

	proxy, err := NewRemoteProxy(workspace, "local-desktop-token")
	if err != nil {
		t.Fatalf("NewRemoteProxy: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/models?token=local-desktop-token", nil)
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got %d, want %d", rec.Code, http.StatusOK)
	}
	if receivedAuth != "Bearer remote-token-123" {
		t.Errorf("got Authorization %q, want %q", receivedAuth, "Bearer remote-token-123")
	}
	body, _ := io.ReadAll(rec.Body)
	if string(body) != "ok" {
		t.Errorf("got body %q, want %q", string(body), "ok")
	}
}

// TestProxy_StripsToken verifies that ?token=LOCAL from the browser
// is not forwarded to the remote server.
func TestProxy_StripsToken(t *testing.T) {
	var receivedToken string
	remoteSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedToken = r.URL.Query().Get("token")
		w.WriteHeader(http.StatusOK)
	}))
	defer remoteSrv.Close()

	workspace := &remote.RemoteWorkspace{
		State: remote.ServeState{Token: "remote-token", Port: 1234, BrowsePort: 5678},
	}
	workspace.SetAPIURL(remoteSrv.URL)

	proxy, err := NewRemoteProxy(workspace, "local-desktop-token")
	if err != nil {
		t.Fatalf("NewRemoteProxy: %v", err)
	}

	req := httptest.NewRequest("POST", "/api/chat?token=should-be-stripped&windowId=abc", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if receivedToken != "" {
		t.Errorf("remote received query token %q, want empty (stripped)", receivedToken)
	}
}

// TestProxy_InjectsRemoteToken verifies that the Authorization header
// on the proxied request contains the remote token from ServeState.
func TestProxy_InjectsRemoteToken(t *testing.T) {
	var receivedAuth string
	remoteSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer remoteSrv.Close()

	workspace := &remote.RemoteWorkspace{
		State: remote.ServeState{Token: "remote-secret-456", Port: 1234, BrowsePort: 5678},
	}
	workspace.SetAPIURL(remoteSrv.URL)

	proxy, err := NewRemoteProxy(workspace, "local-desktop-token")
	if err != nil {
		t.Fatalf("NewRemoteProxy: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if receivedAuth != "Bearer remote-secret-456" {
		t.Errorf("got Authorization %q, want %q", receivedAuth, "Bearer remote-secret-456")
	}
}

// TestProxy_NonAPIReturns404 verifies that non-/api/ requests
// return 404.
func TestProxy_NonAPIReturns404(t *testing.T) {
	var reachedRemote bool
	remoteSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reachedRemote = true
		w.WriteHeader(http.StatusOK)
	}))
	defer remoteSrv.Close()

	workspace := &remote.RemoteWorkspace{
		State: remote.ServeState{Token: "tok", Port: 1234, BrowsePort: 5678},
	}
	workspace.SetAPIURL(remoteSrv.URL)

	proxy, err := NewRemoteProxy(workspace, "local-desktop-token")
	if err != nil {
		t.Fatalf("NewRemoteProxy: %v", err)
	}

	req := httptest.NewRequest("GET", "/some-page", nil)
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if reachedRemote {
		t.Error("non-API request should not reach remote server")
	}
	if rec.Code != http.StatusNotFound {
		t.Errorf("got %d, want %d (404 for non-API routes)", rec.Code, http.StatusNotFound)
	}
}

// TestProxy_RejectsInvalidLocalToken verifies that requests
// with an incorrect local desktop token are rejected.
func TestProxy_RejectsInvalidLocalToken(t *testing.T) {
	remoteSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("request should not reach remote server — local token invalid")
		w.WriteHeader(http.StatusOK)
	}))
	defer remoteSrv.Close()

	workspace := &remote.RemoteWorkspace{
		State: remote.ServeState{Token: "remote-token", Port: 1234, BrowsePort: 5678},
	}
	workspace.SetAPIURL(remoteSrv.URL)

	proxy, err := NewRemoteProxy(workspace, "correct-local-token")
	if err != nil {
		t.Fatalf("NewRemoteProxy: %v", err)
	}

	// Request with wrong local token
	req := httptest.NewRequest("GET", "/api/models?token=wrong-token", nil)
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want %d (401 for invalid local token)", rec.Code, http.StatusUnauthorized)
	}
}

// TestProxy_AcitsValidLocalToken verifies that requests
// with the correct local desktop token are accepted.
func TestProxy_AcceptsValidLocalToken(t *testing.T) {
	remoteSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer remoteSrv.Close()

	workspace := &remote.RemoteWorkspace{
		State: remote.ServeState{Token: "remote-token", Port: 1234, BrowsePort: 5678},
	}
	workspace.SetAPIURL(remoteSrv.URL)

	proxy, err := NewRemoteProxy(workspace, "correct-local-token")
	if err != nil {
		t.Fatalf("NewRemoteProxy: %v", err)
	}

	// Request with correct local token
	req := httptest.NewRequest("GET", "/api/models?token=correct-local-token", nil)
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got %d, want %d (200 for valid local token)", rec.Code, http.StatusOK)
	}
}
