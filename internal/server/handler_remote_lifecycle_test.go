package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/u007/ocode/internal/projects"
	"github.com/u007/ocode/internal/remote"
	"github.com/u007/ocode/internal/version"
)

// newLifecycleServer builds a real Server (real mux) with one saved remote
// project and an injected registry connect function, so the lifecycle routes
// are exercised end to end without SSH.
func newLifecycleServer(t *testing.T, host, path string, port int, connectFn func(remote.Target, string) (remoteHostWorkspace, error)) *Server {
	t.Helper()
	s := New("127.0.0.1:0", "", "", nil)
	store, err := projects.NewStoreAt(t.TempDir() + "/projects.json")
	if err != nil {
		t.Fatalf("projects store: %v", err)
	}
	if port > 0 {
		err = store.AddRemote(host, path, port)
	} else {
		err = store.AddRemote(host, path)
	}
	if err != nil {
		t.Fatalf("add remote project: %v", err)
	}
	s.handler.projects = store
	s.handler.remoteHosts = newTestRegistry(connectFn)
	return s
}

func doLifecycle(s *Server, method, target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(method, target, nil)
	s.mux.ServeHTTP(w, r)
	return w
}

func TestRemoteLifecycleUnknownHost(t *testing.T) {
	s := newLifecycleServer(t, "user@realhost", "/p", 0, func(remote.Target, string) (remoteHostWorkspace, error) {
		t.Fatal("connect must not be called for an unknown host")
		return nil, nil
	})
	cases := []struct{ method, path string }{
		{"GET", "/api/remote/unknownhost/status"},
		{"POST", "/api/remote/unknownhost/connect"},
		{"POST", "/api/remote/unknownhost/restart"},
	}
	for _, tc := range cases {
		w := doLifecycle(s, tc.method, tc.path)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s %s: status = %d, want 403", tc.method, tc.path, w.Code)
		}
	}
}

// TestRemoteLifecycleStatusNeverConnectsAndDecodesHost: the exact route must
// resolve through the real mux (not the proxy), percent-decode the host, and
// report connected=false without triggering a connect.
func TestRemoteLifecycleStatusNeverConnectsAndDecodesHost(t *testing.T) {
	s := newLifecycleServer(t, "user@realhost", "/p", 0, func(remote.Target, string) (remoteHostWorkspace, error) {
		t.Fatal("status must never connect")
		return nil, nil
	})
	w := doLifecycle(s, "GET", "/api/remote/user%40realhost/status")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var st remoteHostStatus
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if st.Host != "user@realhost" {
		t.Errorf("host = %q, want user@realhost (percent-decoded)", st.Host)
	}
	if st.Connected {
		t.Error("expected connected=false for a never-connected host")
	}
	if st.LocalVersion != version.Version {
		t.Errorf("local_version = %q, want %q", st.LocalVersion, version.Version)
	}
}

func TestRemoteLifecycleConnectUsesSavedPathAndPort(t *testing.T) {
	ws := &fakeWorkspace{
		apiURL: "http://127.0.0.1:9",
		token:  "t",
		state:  remote.ServeState{Version: version.Version, PID: 5},
	}
	var gotPath string
	var gotPort int
	s := newLifecycleServer(t, "user@realhost", "/srv/app", 2222, func(target remote.Target, path string) (remoteHostWorkspace, error) {
		gotPath = path
		gotPort = target.Port
		return ws, nil
	})
	w := doLifecycle(s, "POST", "/api/remote/user%40realhost/connect")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if gotPath != "/srv/app" || gotPort != 2222 {
		t.Fatalf("connect got path=%q port=%d, want /srv/app and 2222", gotPath, gotPort)
	}
	var st remoteHostStatus
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !st.Connected || st.PID != 5 {
		t.Fatalf("status = %+v, want connected with pid 5", st)
	}
}

func TestRemoteLifecycleRestartReturnsNewStatus(t *testing.T) {
	ws := &fakeWorkspace{
		apiURL: "http://127.0.0.1:9",
		token:  "t",
		state:  remote.ServeState{Version: version.Version, PID: 99},
	}
	s := newLifecycleServer(t, "user@realhost", "/p", 0, func(remote.Target, string) (remoteHostWorkspace, error) {
		return ws, nil
	})
	w := doLifecycle(s, "POST", "/api/remote/user%40realhost/restart")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var st remoteHostStatus
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !st.Connected || st.PID != 99 {
		t.Fatalf("status = %+v, want connected with pid 99", st)
	}
}

// TestRemoteLifecycleRestartErrorCarriesStage: a kill failure surfaces as a
// 502 whose stage comes from the registry error.
func TestRemoteLifecycleRestartErrorCarriesStage(t *testing.T) {
	tpt := &recordingTransport{killErr: errors.New("kill: operation not permitted")}
	oldWS := &fakeWorkspace{
		apiURL:    "http://127.0.0.1:9",
		token:     "t",
		transport: tpt,
		state:     remote.ServeState{Version: "1.0.0", PID: 42},
	}
	s := newLifecycleServer(t, "user@realhost", "/p", 0, func(remote.Target, string) (remoteHostWorkspace, error) {
		return oldWS, nil
	})
	// Seed a connected entry so restart has a transport+pid to kill.
	if _, err := s.handler.remoteHosts.workspaceForPort("user@realhost", "/p", 0); err != nil {
		t.Fatalf("seed connect: %v", err)
	}

	w := doLifecycle(s, "POST", "/api/remote/user%40realhost/restart")
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502: %s", w.Code, w.Body.String())
	}
	var body struct {
		Error string `json:"error"`
		Stage string `json:"stage"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Stage != "remote-kill" {
		t.Fatalf("stage = %q, want remote-kill", body.Stage)
	}
	if body.Error == "" {
		t.Fatal("expected a non-empty error message")
	}
}
