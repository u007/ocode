package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/u007/ocode/internal/projects"
	"github.com/u007/ocode/internal/remote"
	"github.com/u007/ocode/internal/tool"
)

// Boot-time auto-start of persisted port forwards.
//
// The forward's open was triggered ONLY by the first LIST REQUEST for a
// project (HandleListPortMaps), and the web UI issues that request only for
// the ACTIVE project. A remote project that is saved but not active at launch
// therefore never had its persisted forwards opened: the app came up with the
// rows persisted and the tunnels down, and the user had to click the project
// to make it connect.
//
// These tests pin the boot sweep that closes that gap: at boot every saved SSH
// project with enabled forwards gets its forwards opened, without waiting for
// a list request.

// newBootSweepHandler builds a handler whose project store holds the given
// remote projects (host -> remote path) with no forwards yet. Forwards are
// added by the caller so it can pick free local ports.
func newBootSweepHandler(t *testing.T, hosts ...string) *Handler {
	t.Helper()
	h := NewHandler()
	h.procSup = tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
	h.portMaps = newPortMapRegistry(h.procSup)

	store, err := projects.NewStoreAt(t.TempDir() + "/projects.json")
	if err != nil {
		t.Fatalf("projects.NewStoreAt: %v", err)
	}
	h.projects = store
	for _, host := range hosts {
		target, err := remote.ParseTarget(host)
		if err != nil {
			t.Fatalf("parse target %q: %v", host, err)
		}
		if err := store.AddRemote(target.String(), "/srv/app"); err != nil {
			t.Fatalf("AddRemote(%q): %v", host, err)
		}
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = h.procSup.TerminateAll(ctx)
	})
	return h
}

// addForward persists an enabled forward and binds its local port so the
// readiness probe (which only dials 127.0.0.1:<localPort>) succeeds.
func addForward(t *testing.T, h *Handler, host string, remotePort int) int {
	t.Helper()
	local := freeLocalPort(t)
	ref := projects.ProjectRef{Host: host, Path: "/srv/app"}
	if err := h.projects.AddPortMap(ref, remotePort, local); err != nil {
		t.Fatalf("AddPortMap(%d): %v", remotePort, err)
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", local))
	if err != nil {
		t.Fatalf("bind local forward port %d: %v", local, err)
	}
	t.Cleanup(func() { ln.Close() })
	return local
}

// waitLive waits for the forward on remotePort to report live.
func waitLive(t *testing.T, fm *remote.ForwardManager, remotePort int) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !fm.IsLive(remotePort) {
		if time.Now().After(deadline) {
			t.Fatalf("forward %d never went live", remotePort)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// countForwardChildren counts the supervisor's live ssh-portmap children for a
// target. The registration id is "remote-portmap-<target>-<port>", and the
// manager keeps one child per port, so this is the observable for "did a
// second ssh child get opened for the same port?".
func countForwardChildren(t *testing.T, h *Handler, host string) int {
	t.Helper()
	n := 0
	for _, rec := range h.procSup.Snapshot() {
		if rec.Kind != tool.ProcessKindRemote {
			continue
		}
		if strings.HasPrefix(rec.ID, "remote-portmap-"+host+"-") {
			n++
		}
	}
	return n
}

// recordHTRReverseAsks installs the startHTRReverseForward seam and returns a
// reader for how many times the htrcli reverse forward was ASKED for target.
// The real opener still runs, so the seam only observes.
//
// The counter is written by the BACKGROUND auto-start goroutine and read by the
// test's goroutine, so it is locked. The seam is package-level and shared with
// every test in the package, so an earlier test's leftover goroutine can fire
// later; only an ask for THIS target counts, so it cannot fail this test.
func recordHTRReverseAsks(t *testing.T, target string) func() int {
	t.Helper()
	var (
		mu    sync.Mutex
		asked int
	)
	orig := htrReverseForwardOpener()
	seam := htrReverseForwardFn(func(fm *remote.ForwardManager, persisted []projects.PortMap) error {
		if fm != nil && fm.Target() == target {
			mu.Lock()
			asked++
			mu.Unlock()
		}
		return orig(fm, persisted)
	})
	startHTRReverseForward.Store(&seam)
	t.Cleanup(func() {
		restored := htrReverseForwardFn(orig)
		startHTRReverseForward.Store(&restored)
	})
	return func() int {
		mu.Lock()
		defer mu.Unlock()
		return asked
	}
}

// TestPortMapBootSweepOpensPersistedForwards is the core regression: a saved
// remote project with an enabled forward must have that forward open at boot,
// with NO list request having been made.
//
// The fake ssh (installSleepingSSH) keeps the child alive without contacting
// anything, and a local listener on the forward's local port satisfies the
// readiness probe — so "live" is observable.
func TestPortMapBootSweepOpensPersistedForwards(t *testing.T) {
	installSleepingSSH(t, 30*time.Second)
	h := newBootSweepHandler(t, "user@devbox")
	addForward(t, h, "user@devbox", 3000)

	// The sweep — exactly what Serve() calls at boot.
	h.portMapAutoStartAll()

	target, err := remote.ParseTarget("user@devbox")
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}
	fm := h.portMaps.entry(target, "/srv/app").fm
	waitLive(t, fm, 3000)
}

// TestPortMapBootSweepSkipsDisabledAndLocalAndWSL pins the selection rules:
// a disabled forward stays down, a LOCAL project has no forwards at all, and a
// WSL project is refused (WSL2 shares the Windows loopback, so an extra
// forward would be a no-op — the same rule the list handler enforces).
func TestPortMapBootSweepSkipsDisabledAndLocalAndWSL(t *testing.T) {
	installSleepingSSH(t, 30*time.Second)
	h := newBootSweepHandler(t, "user@devbox")

	// A DISABLED forward on an SSH project.
	ref := projects.ProjectRef{Host: "user@devbox", Path: "/srv/app"}
	if err := h.projects.AddPortMap(ref, 3100, freeLocalPort(t)); err != nil {
		t.Fatalf("AddPortMap: %v", err)
	}
	if err := h.projects.SetPortMapEnabled(ref, 3100, false); err != nil {
		t.Fatalf("SetPortMapEnabled: %v", err)
	}

	// A LOCAL project (no host) — never a forward target.
	if err := h.projects.Add("/srv/local"); err != nil {
		t.Fatalf("Add local: %v", err)
	}

	// A WSL project — refused by the sweep.
	if err := h.projects.AddRemote("wsl:Ubuntu", "/srv/wsl"); err != nil {
		t.Fatalf("AddRemote wsl: %v", err)
	}
	wslRef := projects.ProjectRef{Host: "wsl:Ubuntu", Path: "/srv/wsl"}
	if err := h.projects.AddPortMap(wslRef, 3200, freeLocalPort(t)); err != nil {
		t.Fatalf("AddPortMap wsl: %v", err)
	}

	h.portMapAutoStartAll()

	// Give the background opens a generous window; they must NOT happen.
	time.Sleep(2 * time.Second)

	target, err := remote.ParseTarget("user@devbox")
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}
	entry := h.portMaps.entry(target, "/srv/app")
	if entry.fm.IsLive(3100) {
		t.Fatal("a DISABLED forward was opened by the boot sweep")
	}
	// A WSL project must not even get a registry entry.
	h.portMaps.mu.Lock()
	wsleries := 0
	for _, e := range h.portMaps.byKey {
		if e.ref.Host == "wsl:Ubuntu" {
			wsleries++
		}
	}
	h.portMaps.mu.Unlock()
	if wsleries != 0 {
		t.Fatalf("boot sweep created %d registry entries for a WSL project; WSL shares the Windows loopback", wsleries)
	}
}

// TestPortMapBootSweepDoesNotOpenReverseForward pins that the boot sweep
// opens ONLY the persisted forwards — never the ephemeral htrcli reverse
// forward.
//
// The reverse forward exposes a LOCAL port to the remote host's loopback, and
// it is a relay for the project the user is actually looking at. Opening it
// for every saved remote project at boot would add one reverse ssh child per
// project. The list request (the user opening the project) is where it
// belongs.
//
// The gate is observed through the startHTRReverseForward seam: the real
// implementation is a no-op when the local htrcli daemon is not running, so
// counting children cannot tell "not asked for" from "asked for and the
// daemon was down". The seam records the ASK.
func TestPortMapBootSweepDoesNotOpenReverseForward(t *testing.T) {
	installSleepingSSH(t, 30*time.Second)
	h := newBootSweepHandler(t, "user@devbox")
	addForward(t, h, "user@devbox", 3000)

	asks := recordHTRReverseAsks(t, "user@devbox")

	h.portMapAutoStartAll()

	target, err := remote.ParseTarget("user@devbox")
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}
	entry := h.portMaps.entry(target, "/srv/app")
	waitLive(t, entry.fm, 3000)

	// Give the background auto-start a moment to finish asking.
	deadline := time.Now().Add(5 * time.Second)
	for {
		n := asks()
		if n != 0 || time.Now().After(deadline) {
			if n != 0 {
				t.Fatalf("the boot sweep asked for the htrcli reverse forward %d times, want 0", n)
			}
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Exactly one child: the persisted forward. No reverse child.
	if n := countForwardChildren(t, h, "user@devbox"); n != 1 {
		t.Fatalf("boot sweep opened %d forward children, want 1 (the persisted forward only, no reverse)", n)
	}
}

// TestPortMapBootSweepSkipsProjectsWithoutForwards pins that the sweep does
// NOT create a registry entry for a saved remote project that has no
// persisted forwards.
//
// The watchdog visits every registry entry on each tick, so an entry for a
// forward-less project is pure overhead. It also pins that the sweep opens no
// reverse (htrcli) forward for such a project.
func TestPortMapBootSweepSkipsProjectsWithoutForwards(t *testing.T) {
	installSleepingSSH(t, 30*time.Second)
	h := newBootSweepHandler(t, "user@devbox")
	// No forward added: the project is saved but has no port_maps.

	h.portMapAutoStartAll()
	time.Sleep(2 * time.Second)

	h.portMaps.mu.Lock()
	entries := len(h.portMaps.byKey)
	h.portMaps.mu.Unlock()
	if entries != 0 {
		t.Fatalf("boot sweep created %d registry entries for a project with no forwards, want 0", entries)
	}
	if n := countForwardChildren(t, h, "user@devbox"); n != 0 {
		t.Fatalf("boot sweep opened %d forward children for a project with no forwards, want 0", n)
	}
}

// TestPortMapBootSweepCarriesSavedSSHPort pins that the sweep opens the
// forward on the project's SAVED ssh port.
//
// Target.String() is port-free, so the port lives in the project's
// RemotePort field; the sweep has to carry it across explicitly, exactly as
// remoteWorkFor does when it resolves a request's host. Without it, a project
// saved on a non-default ssh port would have its forward dialled on 22.
//
// The fake ssh records its argv, so the port is observable.
func TestPortMapBootSweepCarriesSavedSSHPort(t *testing.T) {
	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv")
	script := fmt.Sprintf("#!/bin/sh\necho \"$@\" >> %s\nsleep 300\n", argvFile)
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	h := newBootSweepHandler(t, "user@devbox")
	// Save the project with an explicit ssh port.
	if _, err := h.projects.UpdateRemote(
		projects.ProjectRef{Host: "user@devbox", Path: "/srv/app"},
		remote.Target{Kind: remote.KindSSH, User: "user", Host: "devbox", Port: 2222},
		"/srv/app",
	); err != nil {
		t.Fatalf("UpdateRemote: %v", err)
	}
	addForward(t, h, "user@devbox", 3000)

	h.portMapAutoStartAll()

	target, err := remote.ParseTarget("user@devbox")
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}
	target.Port = 2222
	fm := h.portMaps.entry(target, "/srv/app").fm
	waitLive(t, fm, 3000)

	// The readiness probe only dials the LOCAL port, so it can succeed before
	// the fake ssh has written its argv. Poll for the argv instead of reading
	// it once.
	deadline := time.Now().Add(15 * time.Second)
	argv := ""
	for {
		data, err := os.ReadFile(argvFile)
		if err == nil {
			argv = string(data)
			if strings.Contains(argv, "-p 2222") {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("forward argv = %q, want it to dial the saved ssh port (-p 2222)", strings.TrimSpace(argv))
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestPortMapBootSweepAndListDoNotDoubleStart pins that the sweep and the
// list handler share ONE guard: a list request after the sweep must not open
// a SECOND ssh child for the same port.
func TestPortMapBootSweepAndListDoNotDoubleStart(t *testing.T) {
	installSleepingSSH(t, 30*time.Second)
	h := newBootSweepHandler(t, "user@devbox")
	addForward(t, h, "user@devbox", 3000)

	h.portMapAutoStartAll()

	target, err := remote.ParseTarget("user@devbox")
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}
	fm := h.portMaps.entry(target, "/srv/app").fm
	waitLive(t, fm, 3000)

	if n := countForwardChildren(t, h, "user@devbox"); n != 1 {
		t.Fatalf("after the sweep there are %d forward children, want 1", n)
	}

	// Now the list request arrives (the web UI's capability probe). It must
	// not open a second child.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/portmaps", h.HandleListPortMaps)
	rec := doPortMaps(t, mux, "GET", "/api/portmaps?host="+url.QueryEscape("user@devbox")+"&project=%2Fsrv%2Fapp", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET list = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	time.Sleep(2 * time.Second)

	if !fm.IsLive(3000) {
		t.Fatal("the forward died after the list request")
	}
	if n := countForwardChildren(t, h, "user@devbox"); n != 1 {
		t.Fatalf("after the list request there are %d forward children, want 1 (the list re-started a live forward)", n)
	}
}

// TestPortMapListStartsHTRReverseForwardAfterBootSweep pins that the boot sweep
// does not consume the list request's chance to open the htrcli reverse
// forward.
//
// The sweep and the list both trigger the persisted-forward open, but only the
// list opens the reverse forward. When both shared one Once, the sweep's run
// swallowed the list's, so a saved project the user opened after boot never got
// its htrcli relay. The first list must ask for it, and a second list inside
// htrAutoRetryInterval must not (the retry is throttled, not one-shot).
func TestPortMapListStartsHTRReverseForwardAfterBootSweep(t *testing.T) {
	installSleepingSSH(t, 30*time.Second)
	h := newBootSweepHandler(t, "user@devbox")
	addForward(t, h, "user@devbox", 3000)
	asks := recordHTRReverseAsks(t, "user@devbox")

	h.portMapAutoStartAll()

	target, err := remote.ParseTarget("user@devbox")
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}
	fm := h.portMaps.entry(target, "/srv/app").fm
	waitLive(t, fm, 3000)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/portmaps", h.HandleListPortMaps)
	listURL := "/api/portmaps?host=" + url.QueryEscape("user@devbox") + "&project=%2Fsrv%2Fapp"

	rec := doPortMaps(t, mux, "GET", listURL, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("first GET list = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	deadline := time.Now().Add(15 * time.Second)
	for asks() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the first list after the boot sweep never asked for the htrcli reverse forward")
		}
		time.Sleep(20 * time.Millisecond)
	}

	rec = doPortMaps(t, mux, "GET", listURL, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("second GET list = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	time.Sleep(2 * time.Second)

	if n := asks(); n != 1 {
		t.Fatalf("htrcli reverse forward was asked for %d times across two lists, want exactly 1", n)
	}
}
