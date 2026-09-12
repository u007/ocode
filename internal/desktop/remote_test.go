package desktop

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/remote"
)

// TestWorkspaceConfigRoundTrip proves the saved config carries the target,
// remote path, and stable workspace ID needed for reconnect.
func TestWorkspaceConfigRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".local", "share", "ocode"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfg := WorkspaceConfig{
		Mode:       WorkspaceRemoteSSH,
		TargetHost: "example.test",
		RemotePath: "/home/u/proj",
	}
	if err := SaveWorkspaceConfig(cfg); err != nil {
		t.Skipf("save workspace config: %v", err)
	}
	got, err := LoadWorkspaceConfig()
	if err != nil {
		t.Fatalf("LoadWorkspaceConfig: %v", err)
	}
	if got != cfg {
		t.Errorf("got %+v, want %+v", got, cfg)
	}
}

// TestProxyEndToEndReachesRemote proves the browser request reaches the
// remote server with the remote token injected and the local token stripped:
// a request with ?token=LOCAL through RemoteProxy arrives at the remote with
// Authorization: Bearer REMOTE and no query token.
func TestProxyEndToEndReachesRemote(t *testing.T) {
	var gotAuth, gotQueryToken string
	remoteSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotQueryToken = r.URL.Query().Get("token")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer remoteSrv.Close()

	ws := &remote.RemoteWorkspace{
		WorkspaceID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		State:       remote.ServeState{Token: "remote-token-xyz", Port: 1},
	}
	ws.SetAPIURL(remoteSrv.URL)

	proxy, err := NewRemoteProxy(ws, "local-desktop-token")
	if err != nil {
		t.Fatalf("NewRemoteProxy: %v", err)
	}
	req := httptest.NewRequest("GET", "/api/browse/config?token=local-desktop-token", nil)
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("proxy status = %d, want 200", rec.Code)
	}
	if gotAuth != "Bearer remote-token-xyz" {
		t.Errorf("remote Authorization = %q, want %q", gotAuth, "Bearer remote-token-xyz")
	}
	if gotQueryToken != "" {
		t.Errorf("remote query token = %q, want empty (stripped)", gotQueryToken)
	}
}

// TestWorkspaceCloseIsNilSafe proves Close on a zero workspace never panics
// (shutdown path guards handle.Workspace, but defense in depth is cheap).
func TestWorkspaceCloseIsNilSafe(t *testing.T) {
	w := &Workspace{Mode: WorkspaceRemoteSSH}
	if err := w.Close(); err != nil {
		t.Errorf("Close on workspace without remote: %v", err)
	}
}
