package desktop

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/u007/ocode/internal/projects"
	"github.com/u007/ocode/internal/remote"
	"github.com/u007/ocode/internal/tool"
)

func newTestPortMapsHandler(t *testing.T) (*portMapsHandler, *http.ServeMux) {
	t.Helper()
	store, err := projects.NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	ref := projects.ProjectRef{Host: "user@host", Path: "/proj"}
	if err := store.AddRemote(ref.Host, ref.Path); err != nil {
		t.Fatalf("AddRemote: %v", err)
	}
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
	fm := remote.NewForwardManager(sup, remote.Target{Kind: remote.KindSSH, Host: "host"})
	h := &portMapsHandler{fm: fm, store: store, ref: ref, localToken: "desktop-tok"}
	mux := http.NewServeMux()
	h.register(mux)
	return h, mux
}

func doPortMapsReq(mux *http.ServeMux, method, path, token string, body []byte) *httptest.ResponseRecorder {
	url := path
	if token != "" {
		if bytes.ContainsRune([]byte(path), '?') {
			url += "&token=" + token
		} else {
			url += "?token=" + token
		}
	}
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

func TestPortMapsRequiresLocalToken(t *testing.T) {
	_, mux := newTestPortMapsHandler(t)
	rec := doPortMapsReq(mux, "GET", "/api/desktop/portmaps", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET without token = %d, want 401", rec.Code)
	}
	rec = doPortMapsReq(mux, "GET", "/api/desktop/portmaps", "wrong-token", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET with wrong token = %d, want 401", rec.Code)
	}
}

func TestPortMapsListEmptyInitially(t *testing.T) {
	_, mux := newTestPortMapsHandler(t)
	rec := doPortMapsReq(mux, "GET", "/api/desktop/portmaps", "desktop-tok", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got []portMapView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("initial list = %+v, want empty", got)
	}
}

func TestPortMapsAddPersistsButLiveOpenFailsWithoutRealSSH(t *testing.T) {
	// fm.Start shells out to a real `ssh` process against an unreachable
	// host, so it's expected to fail here — this test only verifies the add
	// request persists the entry before attempting to open it live.
	h, mux := newTestPortMapsHandler(t)
	body, _ := json.Marshal(map[string]int{"remote_port": 3000, "local_port": 3000})
	rec := doPortMapsReq(mux, "POST", "/api/desktop/portmaps", "desktop-tok", body)
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("POST add unexpectedly unauthorized")
	}

	maps, err := h.store.PortMaps(h.ref)
	if err != nil {
		t.Fatalf("PortMaps: %v", err)
	}
	if len(maps) != 1 || maps[0].RemotePort != 3000 {
		t.Fatalf("PortMaps = %+v, want one entry for remote port 3000", maps)
	}
}

func TestPortMapsRemoveDeletesEntry(t *testing.T) {
	h, mux := newTestPortMapsHandler(t)
	if err := h.store.AddPortMap(h.ref, 4000, 4000); err != nil {
		t.Fatalf("AddPortMap: %v", err)
	}
	rec := doPortMapsReq(mux, "DELETE", "/api/desktop/portmaps/4000", "desktop-tok", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	maps, _ := h.store.PortMaps(h.ref)
	if len(maps) != 0 {
		t.Fatalf("PortMaps after DELETE = %+v, want empty", maps)
	}
}

func TestPortMapsDisableStopsWithoutRemoving(t *testing.T) {
	h, mux := newTestPortMapsHandler(t)
	if err := h.store.AddPortMap(h.ref, 5000, 5000); err != nil {
		t.Fatalf("AddPortMap: %v", err)
	}
	rec := doPortMapsReq(mux, "POST", "/api/desktop/portmaps/5000/disable", "desktop-tok", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	maps, _ := h.store.PortMaps(h.ref)
	if len(maps) != 1 || maps[0].Enabled {
		t.Fatalf("PortMaps after disable = %+v, want one disabled entry", maps)
	}
}

func TestPortMapsRemoveUnknownPortNotFound(t *testing.T) {
	_, mux := newTestPortMapsHandler(t)
	rec := doPortMapsReq(mux, "DELETE", "/api/desktop/portmaps/9999", "desktop-tok", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE unknown port = %d, want 404", rec.Code)
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

// installSleepingSSH puts a fake ssh on PATH that ignores its arguments and
// sleeps, so a forward's child stays alive (and therefore "live") without
// contacting anything.
func installSleepingSSH(t *testing.T, d time.Duration) string {
	t.Helper()
	dir := t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\nsleep %d\n", int(d.Seconds()))
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

// autoStartEnabled is the loop startRemoteServer runs off the critical path.
// Extraction must not change what it opens: every persisted ENABLED forward, and
// nothing else.
//
// Start's readiness probe only dials 127.0.0.1:<localPort>, so a listener bound
// here satisfies it immediately and the fake ssh keeps the child alive — which
// is what makes "live" observable.
func TestAutoStartEnabledOpensOnlyEnabledForwards(t *testing.T) {
	installSleepingSSH(t, 30*time.Second)
	h, _ := newTestPortMapsHandler(t)

	live := freeLocalPort(t)
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", live))
	if err != nil {
		t.Fatalf("bind local forward port: %v", err)
	}
	defer ln.Close()
	// The disabled forward's local port is bound too, on purpose. If the
	// enabled-check were dropped, its open would SUCCEED and IsLive(4000) would
	// catch it; with an unbound port the open would fail on the readiness probe
	// and the assertion would pass either way.
	offline := freeLocalPort(t)
	offlineLn, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", offline))
	if err != nil {
		t.Fatalf("bind disabled forward port: %v", err)
	}
	defer offlineLn.Close()

	if err := h.store.AddPortMap(h.ref, 3000, live); err != nil {
		t.Fatalf("AddPortMap(enabled): %v", err)
	}
	if err := h.store.AddPortMap(h.ref, 4000, offline); err != nil {
		t.Fatalf("AddPortMap(to disable): %v", err)
	}
	if err := h.store.SetPortMapEnabled(h.ref, 4000, false); err != nil {
		t.Fatalf("SetPortMapEnabled: %v", err)
	}

	h.autoStartEnabled()

	if !h.fm.IsLive(3000) {
		t.Fatal("enabled forward 3000 is not live after autoStartEnabled")
	}
	if h.fm.IsLive(4000) {
		t.Fatal("disabled forward 4000 was opened by autoStartEnabled")
	}
}

// A nil store is a best-effort boot (the store failed to open), not a crash.
func TestAutoStartEnabledToleratesNilStore(t *testing.T) {
	h, _ := newTestPortMapsHandler(t)
	h.store = nil
	h.autoStartEnabled() // must not panic
}
