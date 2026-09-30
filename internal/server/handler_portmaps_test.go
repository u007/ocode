package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

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

// TestRemovePortMapUnknownPortDoesNotSuppress pins the ordering contract: a
// DELETE for a port the store does not know must 404 WITHOUT tearing down and
// suppressing the forward. The old order stopped the forward first, then 404'd
// on the store write, leaving the port permanently suppressed (the watchdog
// could never restart it; only a manual re-enable cleared the tombstone).
func TestRemovePortMapUnknownPortDoesNotSuppress(t *testing.T) {
	h, mux := newTestPortMapsHandler(t, "user@devbox", "/srv/app")
	// A list probe creates the registry entry even before any forward exists.
	target, err := remote.ParseTarget("user@devbox")
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}
	entry := h.portMaps.entry(target, "/srv/app")

	rec := doPortMaps(t, mux, "DELETE", "/api/portmaps/4096?host=user@devbox&project=%2Fsrv%2Fapp", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE unknown port = %d, want 404: %s", rec.Code, rec.Body.String())
	}
	entry.policy.mu.Lock()
	suppressed := entry.policy.isSuppressed(4096)
	entry.policy.mu.Unlock()
	if suppressed {
		t.Fatal("failed removal left the port suppressed; the watchdog can never restart it")
	}
}

// The first list for a project auto-starts its persisted enabled forwards, and
// ForwardManager.Start's readiness probe is ~5s of dialing (25 x 200ms,
// connect.go tunnelReadyAttempts/tunnelReadyInterval) when the forward cannot
// come up. That open runs on its own goroutine, so the list answers from
// persisted state immediately instead of stalling on the tunnel.
//
// The canary is a SECOND project: its list also goes through
// portMapRegistry.entry, so it takes the same reg.mu the auto-start would take
// if a future change held the registry lock across the open. It must answer
// immediately too. Both halves are asserted; the isolation is mutation-verified
// (holding reg.mu across autoStart makes the canary take ~4.7s).
func TestPortMapsListDoesNotBlockOnAutoStart(t *testing.T) {
	const slowHost = "user@no-such-host.invalid"
	const fastHost = "user@other.invalid"
	h, mux := newTestPortMapsHandler(t, slowHost, "/srv/app")

	// A second registered project, no forwards: its list is pure bookkeeping.
	fastTarget, err := remote.ParseTarget(fastHost)
	if err != nil {
		t.Fatalf("parse target %q: %v", fastHost, err)
	}
	if err := h.projects.AddRemote(fastTarget.String(), "/srv/other"); err != nil {
		t.Fatalf("AddRemote: %v", err)
	}

	// One enabled forward on a local port nothing listens on: its open cannot
	// succeed, so the readiness probe runs its full budget in the background.
	slowTarget, err := remote.ParseTarget(slowHost)
	if err != nil {
		t.Fatalf("parse target %q: %v", slowHost, err)
	}
	ref := projects.ProjectRef{Host: slowTarget.String(), Path: "/srv/app"}
	if err := h.projects.AddPortMap(ref, 3000, freeLocalPort(t)); err != nil {
		t.Fatalf("AddPortMap: %v", err)
	}

	start := time.Now()
	rec := doPortMaps(t, mux, "GET", "/api/portmaps?host="+url.QueryEscape(slowHost)+"&project=%2Fsrv%2Fapp", nil)
	elapsed := time.Since(start)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET list = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	// The persisted row is still reported even though its open has not run yet.
	got := decodePortMaps(t, rec)
	if len(got) != 1 || got[0].RemotePort != 3000 || !got[0].Enabled || got[0].Live {
		t.Fatalf("list = %+v, want one enabled, not-yet-live entry for remote 3000", got)
	}
	// ~5s of readiness probing used to happen inline here.
	if elapsed > 2*time.Second {
		t.Fatalf("list took %s; the auto-start is running inline instead of in the background", elapsed)
	}
	t.Logf("list answered in %s while the forward open runs in the background", elapsed.Round(time.Millisecond))

	// The canary: another project's list must not be affected either way.
	start = time.Now()
	rec = doPortMaps(t, mux, "GET", "/api/portmaps?host="+url.QueryEscape(fastHost)+"&project=%2Fsrv%2Fother", nil)
	elapsed = time.Since(start)
	if rec.Code != http.StatusOK {
		t.Fatalf("concurrent list = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if elapsed > 2*time.Second {
		t.Fatalf("another project's list took %s while a port-map auto-start was probing; "+
			"the slow open is holding the registry lock", elapsed)
	}
}

// A fast-returning list is only correct if the open still HAPPENS. This pins
// that: the auto-start runs after the response, and the forward reaches live.
//
// The readiness probe only dials 127.0.0.1:<localPort>, so a listener bound
// here satisfies it immediately — which is what lets the fake ssh (a plain
// sleep, never connecting anywhere) leave the forward live and observable.
func TestPortMapsAutoStartRunsAfterListResponds(t *testing.T) {
	const host = "user@devbox"
	installSleepingSSH(t, 30*time.Second)
	h, mux := newTestPortMapsHandler(t, host, "/srv/app")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = h.procSup.TerminateAll(ctx)
	})

	local := freeLocalPort(t)
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", local))
	if err != nil {
		t.Fatalf("bind local forward port: %v", err)
	}
	defer ln.Close()

	ref := projects.ProjectRef{Host: host, Path: "/srv/app"}
	if err := h.projects.AddPortMap(ref, 3000, local); err != nil {
		t.Fatalf("AddPortMap: %v", err)
	}

	rec := doPortMaps(t, mux, "GET", "/api/portmaps?host="+url.QueryEscape(host)+"&project=%2Fsrv%2Fapp", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET list = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	target, err := remote.ParseTarget(host)
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}
	fm := h.portMaps.entry(target, "/srv/app").fm

	deadline := time.Now().Add(15 * time.Second)
	for !fm.IsLive(3000) {
		if time.Now().After(deadline) {
			t.Fatal("forward never went live; the async auto-start did not run")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// installSleepingSSH puts a fake ssh on PATH that ignores its arguments and
// sleeps, so a port forward's child stays alive (and therefore "live") without
// contacting anything.
func installSleepingSSH(t *testing.T, d time.Duration) {
	t.Helper()
	dir := t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\nsleep %d\n", int(d.Seconds()))
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
