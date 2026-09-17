package server

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/projects"
	"github.com/u007/ocode/internal/remote"
	"github.com/u007/ocode/internal/tool"
)

// newTestPortMapsHandler builds an authenticated-free handler + mux for the
// project-scoped port-map routes. Auth is exercised generically for every
// /api route elsewhere; these tests focus on admission + persistence.
// host/path register the remote project the requests target ("" skips
// registration, for the unregistered-host case).
func newTestPortMapsHandler(t *testing.T, host, path string) (*Handler, *http.ServeMux) {
	t.Helper()
	h := NewHandler()
	h.procSup = tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
	h.portMaps = newPortMapRegistry(h.procSup)

	store, err := projects.NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("projects.NewStoreAt: %v", err)
	}
	h.projects = store
	if host != "" {
		target, err := remote.ParseTarget(host)
		if err != nil {
			t.Fatalf("parse target %q: %v", host, err)
		}
		if err := store.AddRemote(target.String(), path); err != nil {
			t.Fatalf("AddRemote: %v", err)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/portmaps", h.HandleListPortMaps)
	mux.HandleFunc("POST /api/portmaps", h.HandleAddPortMap)
	mux.HandleFunc("DELETE /api/portmaps/{port}", h.HandleRemovePortMap)
	mux.HandleFunc("POST /api/portmaps/{port}/enable", h.HandleEnablePortMap)
	mux.HandleFunc("POST /api/portmaps/{port}/disable", h.HandleDisablePortMap)
	return h, mux
}

func doPortMaps(t *testing.T, mux *http.ServeMux, method, url string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, url, bytes.NewReader(body))
	} else {
		req = httptest.NewRequest(method, url, nil)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func decodePortMaps(t *testing.T, rec *httptest.ResponseRecorder) []portMapView {
	t.Helper()
	var got []portMapView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return got
}

func TestPortMapsListEmptyInitially(t *testing.T) {
	_, mux := newTestPortMapsHandler(t, "user@devbox", "/srv/app")
	rec := doPortMaps(t, mux, "GET", "/api/portmaps?host=user@devbox&project=%2Fsrv%2Fapp", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got := decodePortMaps(t, rec); len(got) != 0 {
		t.Fatalf("initial list = %+v, want empty", got)
	}
}

// An unregistered (host, path) pair must be refused — the endpoint can never be
// aimed at an arbitrary SSH destination.
func TestPortMapsRejectUnregisteredHost(t *testing.T) {
	_, mux := newTestPortMapsHandler(t, "user@devbox", "/srv/app")
	rec := doPortMaps(t, mux, "GET", "/api/portmaps?host=user@evil&project=%2Fsrv%2Fapp", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unregistered host = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	rec = doPortMaps(t, mux, "GET", "/api/portmaps?project=%2Fsrv%2Fapp", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing host = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

// WSL shares the Windows loopback, so an extra forward is a no-op; the request
// is refused (on non-Windows it fails the target-OS check, on Windows our
// KindSSH check — both are 400).
func TestPortMapsRejectWSLTarget(t *testing.T) {
	_, mux := newTestPortMapsHandler(t, "wsl:Ubuntu", "/srv/app")
	rec := doPortMaps(t, mux, "GET", "/api/portmaps?host=wsl%3AUbuntu&project=%2Fsrv%2Fapp", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("WSL target = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

// Persisted forwards are listed with their enabled flag; a disabled forward is
// never started so it must report live=false. Ports are drawn from the OS so
// the assertion can't be satisfied by an unrelated listener on this machine's
// 127.0.0.1 (ForwardManager's readiness probe only proves *something* accepts
// on the local port).
func TestPortMapsListReportsPersistedEntries(t *testing.T) {
	h, mux := newTestPortMapsHandler(t, "user@devbox", "/srv/app")
	ref := projects.ProjectRef{Host: "user@devbox", Path: "/srv/app"}
	livePort := freeLocalPort(t)
	offPort := freeLocalPort(t)
	if err := h.projects.AddPortMap(ref, 3000, livePort); err != nil {
		t.Fatalf("AddPortMap: %v", err)
	}
	if err := h.projects.AddPortMap(ref, 8080, offPort); err != nil {
		t.Fatalf("AddPortMap: %v", err)
	}
	if err := h.projects.SetPortMapEnabled(ref, 8080, false); err != nil {
		t.Fatalf("SetPortMapEnabled: %v", err)
	}

	rec := doPortMaps(t, mux, "GET", "/api/portmaps?host=user@devbox&project=%2Fsrv%2Fapp", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	got := decodePortMaps(t, rec)
	if len(got) != 2 {
		t.Fatalf("list = %+v, want 2 entries", got)
	}
	byPort := map[int]portMapView{}
	for _, m := range got {
		byPort[m.RemotePort] = m
	}
	// Enabled + unreachable ssh: the entry stays listed, and the failed open
	// leaves it not-live on a port nothing is listening on.
	if m := byPort[3000]; !m.Enabled || m.Live || m.LocalPort != livePort {
		t.Fatalf("3000 = %+v, want enabled, not live, local %d", m, livePort)
	}
	if m := byPort[8080]; m.Enabled || m.Live || m.LocalPort != offPort {
		t.Fatalf("8080 = %+v, want disabled, not live, local %d", m, offPort)
	}
}

// freeLocalPort returns a currently-unused 127.0.0.1 TCP port.
func freeLocalPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// Add persists before attempting the live open. Without a reachable ssh host
// the open fails, so the status is 502 — but the entry must be on disk either
// way (mirrors the desktop handler's contract).
func TestPortMapsAddPersistsBeforeLiveOpen(t *testing.T) {
	h, mux := newTestPortMapsHandler(t, "user@no-such-host.invalid", "/srv/app")
	body, _ := json.Marshal(map[string]int{"remote_port": 4000, "local_port": 4000})
	rec := doPortMaps(t, mux, "POST", "/api/portmaps?host=user@no-such-host.invalid&project=%2Fsrv%2Fapp", body)
	if rec.Code != http.StatusOK && rec.Code != http.StatusBadGateway {
		t.Fatalf("POST add = %d, want 200 or 502: %s", rec.Code, rec.Body.String())
	}
	maps, err := h.projects.PortMaps(projects.ProjectRef{Host: "user@no-such-host.invalid", Path: "/srv/app"})
	if err != nil {
		t.Fatalf("PortMaps: %v", err)
	}
	if len(maps) != 1 || maps[0].RemotePort != 4000 || maps[0].LocalPort != 4000 || !maps[0].Enabled {
		t.Fatalf("PortMaps = %+v, want one enabled entry for remote 4000/local 4000", maps)
	}
}

// local_port defaults to remote_port when omitted (0), matching the panel's
// "local port (optional)" field.
func TestPortMapsAddDefaultsLocalToRemote(t *testing.T) {
	h, mux := newTestPortMapsHandler(t, "user@no-such-host.invalid", "/srv/app")
	body, _ := json.Marshal(map[string]int{"remote_port": 5173})
	_ = doPortMaps(t, mux, "POST", "/api/portmaps?host=user@no-such-host.invalid&project=%2Fsrv%2Fapp", body)
	maps, err := h.projects.PortMaps(projects.ProjectRef{Host: "user@no-such-host.invalid", Path: "/srv/app"})
	if err != nil {
		t.Fatalf("PortMaps: %v", err)
	}
	if len(maps) != 1 || maps[0].LocalPort != 5173 {
		t.Fatalf("PortMaps = %+v, want local_port 5173", maps)
	}
}

func TestPortMapsAddRejectsInvalidPort(t *testing.T) {
	_, mux := newTestPortMapsHandler(t, "user@devbox", "/srv/app")
	body, _ := json.Marshal(map[string]int{"remote_port": 70000})
	rec := doPortMaps(t, mux, "POST", "/api/portmaps?host=user@devbox&project=%2Fsrv%2Fapp", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("out-of-range port = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	rec = doPortMaps(t, mux, "POST", "/api/portmaps?host=user@devbox&project=%2Fsrv%2Fapp", []byte("not json"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed body = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestPortMapsRemoveDeletesEntry(t *testing.T) {
	h, mux := newTestPortMapsHandler(t, "user@devbox", "/srv/app")
	ref := projects.ProjectRef{Host: "user@devbox", Path: "/srv/app"}
	if err := h.projects.AddPortMap(ref, 4000, 4000); err != nil {
		t.Fatalf("AddPortMap: %v", err)
	}
	rec := doPortMaps(t, mux, "DELETE", "/api/portmaps/4000?host=user@devbox&project=%2Fsrv%2Fapp", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	maps, _ := h.projects.PortMaps(ref)
	if len(maps) != 0 {
		t.Fatalf("PortMaps after DELETE = %+v, want empty", maps)
	}
}

func TestPortMapsRemoveUnknownPortNotFound(t *testing.T) {
	_, mux := newTestPortMapsHandler(t, "user@devbox", "/srv/app")
	rec := doPortMaps(t, mux, "DELETE", "/api/portmaps/9999?host=user@devbox&project=%2Fsrv%2Fapp", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE unknown port = %d, want 404: %s", rec.Code, rec.Body.String())
	}
	rec = doPortMaps(t, mux, "DELETE", "/api/portmaps/nope?host=user@devbox&project=%2Fsrv%2Fapp", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("DELETE malformed port = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestPortMapsDisableStopsWithoutRemoving(t *testing.T) {
	h, mux := newTestPortMapsHandler(t, "user@devbox", "/srv/app")
	ref := projects.ProjectRef{Host: "user@devbox", Path: "/srv/app"}
	if err := h.projects.AddPortMap(ref, 5000, 5000); err != nil {
		t.Fatalf("AddPortMap: %v", err)
	}
	rec := doPortMaps(t, mux, "POST", "/api/portmaps/5000/disable?host=user@devbox&project=%2Fsrv%2Fapp", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	maps, _ := h.projects.PortMaps(ref)
	if len(maps) != 1 || maps[0].Enabled {
		t.Fatalf("PortMaps after disable = %+v, want one disabled entry", maps)
	}
}

// Every registered /api/portmaps* pattern must resolve to the port-map handler
// rather than the SPA's unknown-/api JSON 404. With no ?host= the handler
// answers 400 "host is required" — that is the proof of routing; a typo'd or
// shadowed pattern would 404.
func TestPortMapsRoutesRegisteredOnServer(t *testing.T) {
	s := New("127.0.0.1:0", "", "", nil)
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/portmaps"},
		{"POST", "/api/portmaps"},
		{"DELETE", "/api/portmaps/3000"},
		{"POST", "/api/portmaps/3000/enable"},
		{"POST", "/api/portmaps/3000/disable"},
	} {
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s %s = %d (%s), want 400 from the port-map handler", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}

// The registry keys a project by the same canonical identity the store uses, so
// two requests (with and without an explicit port on the host string) share one
// manager.
func TestPortMapRegistryKeyMatchesStoreRef(t *testing.T) {
	h, _ := newTestPortMapsHandler(t, "user@devbox", "/srv/app")
	target, err := remote.ParseTarget("user@devbox")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	first := h.portMaps.entry(target, "/srv/app")
	second := h.portMaps.entry(target, "/srv/app")
	if first != second {
		t.Fatal("same project produced two forward managers")
	}
	targetPort, err := remote.ParseTarget("user@devbox")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	targetPort.Port = 2222
	if third := h.portMaps.entry(targetPort, "/srv/app"); third != first {
		t.Fatal("explicit ssh port changed the manager identity; store refs would diverge")
	}
	if other := h.portMaps.entry(target, "/srv/other"); other == first {
		t.Fatal("different remote path shared a manager")
	}
}
