package cdp

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/tool"
)

func testPort(r *http.Request) int {
	_, port, _ := net.SplitHostPort(r.Host)
	n, _ := strconv.Atoi(port)
	return n
}

func newTestSupervisor(t *testing.T) *tool.ProcessSupervisor {
	t.Helper()
	return tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{GracePeriod: time.Second})
}

func TestNormalizeHTRPort(t *testing.T) {
	if p, err := NormalizeHTRPort(0); err != nil || p != DefaultHTRPort {
		t.Fatalf("port 0 → (%d, %v), want (%d, nil)", p, err, DefaultHTRPort)
	}
	if p, err := NormalizeHTRPort(3845); err != nil || p != 3845 {
		t.Fatalf("port 3845 → (%d, %v)", p, err)
	}
	for _, bad := range []int{-1, 65536} {
		if _, err := NormalizeHTRPort(bad); err == nil {
			t.Fatalf("port %d must fail", bad)
		}
	}
}

func TestResolveHTRCliBinary(t *testing.T) {
	dir := t.TempDir()
	name := "htrcli-test-bin"
	if os.Getenv("OS") != "" {
		_ = name
	}
	p := filepath.Join(dir, "htrcli")
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Configured path wins.
	got, err := ResolveHTRCliBinary(p)
	if err != nil || got != p {
		t.Fatalf("configured → (%q, %v), want (%q, nil)", got, err, p)
	}
	// Env fallback.
	t.Setenv("OCODE_HTRCLI_PATH", p)
	got, err = ResolveHTRCliBinary("")
	if err != nil || got != p {
		t.Fatalf("env → (%q, %v), want (%q, nil)", got, err, p)
	}
	// Missing everywhere errors loudly (PATH isolated to an empty dir —
	// note the binary above lives in dir, not emptyDir, so LookPath fails).
	emptyDir := t.TempDir()
	t.Setenv("OCODE_HTRCLI_PATH", filepath.Join(dir, "missing"))
	t.Setenv("HTRCLI_PATH", filepath.Join(dir, "missing2"))
	t.Setenv("PATH", emptyDir)
	if _, err := ResolveHTRCliBinary(""); err == nil {
		t.Fatal("expected error when htrcli unresolvable")
	}
	_ = name
}

// serveHTRHealth mirrors the real htrcli daemon: every route (health included)
// requires the bearer token and the body is wrapped as {"ok":true,"data":{...}}.
// The managed daemon's token is its identity.
func serveHTRHealth(w http.ResponseWriter, r *http.Request, identity, socket string) {
	if r.Header.Get("Authorization") != "Bearer "+identity {
		w.WriteHeader(401)
		_, _ = fmt.Fprint(w, `{"error":"unauthorized","ok":false}`)
		return
	}
	w.WriteHeader(200)
	_, _ = fmt.Fprintf(w, `{"ok":true,"data":{"service":"htrcli","managed":true,"identity":%q,"port":%d,"socket":%q,"status":"running","connectedTabs":0,"uptime":0}}`, identity, testPort(r), socket)
}

func TestHTRHealthy(t *testing.T) {
	lg := log.Default()
	identity := "test-managed"
	socket := "test-socket"
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			serveHTRHealth(w, r, identity, socket)
			return
		}
		w.WriteHeader(404)
	}))
	defer ok.Close()
	u, _ := url.Parse(ok.URL)
	port, _ := strconv.Atoi(strings.Split(u.Host, ":")[1])
	if !htrHealthyForInstance(port, socket, identity) {
		t.Fatal("healthy server must probe true")
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer bad.Close()
	bu, _ := url.Parse(bad.URL)
	bport, _ := strconv.Atoi(strings.Split(bu.Host, ":")[1])
	if HTRHealthy(bport, lg) {
		t.Fatal("500 server must probe false")
	}
	if HTRHealthy(1, lg) {
		t.Fatal("closed port must probe false")
	}
}

func TestEnsureHTRServe_ReusesHealthy(t *testing.T) {
	identity := "reuse-managed"
	socket := "reuse-socket"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			serveHTRHealth(w, r, identity, socket)
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(strings.Split(u.Host, ":")[1])
	// nil supervisor: reuse-only mode must succeed without spawning.
	t.Setenv("HOME", t.TempDir())
	if err := htrWriteOwner(identity, port, os.Getpid(), socket, os.Args[0], time.Now()); err != nil {
		t.Fatal(err)
	}
	st, err := EnsureHTRServe(nil, HTROptions{Enabled: true, Port: port, SocketPath: socket}, log.Default())
	if err != nil {
		t.Fatalf("reuse → %v", err)
	}
	if !st.Running || st.Owned {
		t.Fatalf("reuse must be running+unowned: %+v", st)
	}
}

func TestEnsureHTRServe_NoSupervisorMissing(t *testing.T) {
	if _, err := EnsureHTRServe(nil, HTROptions{Enabled: true, Port: 1}, log.Default()); err == nil {
		t.Fatal("missing daemon without supervisor must error")
	}
}

func TestEnsureHTRServe_MissingBinary(t *testing.T) {
	sup := newTestSupervisor(t)
	t.Setenv("OCODE_HTRCLI_PATH", filepath.Join(t.TempDir(), "missing-htrcli"))
	t.Setenv("HTRCLI_PATH", filepath.Join(t.TempDir(), "missing-htrcli-2"))
	t.Setenv("PATH", t.TempDir())
	if _, err := EnsureHTRServe(sup, HTROptions{Enabled: true, Port: 1}, log.Default()); err == nil {
		t.Fatal("missing binary must error loudly")
	}
}

func TestResolveHTRSocketPathIsNamespaced(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, err := ResolveHTRSocketPath("")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(filepath.ToSlash(path), "/ocode/htr/daemon.sock") {
		t.Fatalf("socket path = %q, want ocode-managed path", path)
	}
	if strings.Contains(path, ".htrcli") {
		t.Fatalf("socket path must not reuse standalone htrcli namespace: %q", path)
	}
}

func TestNativeHostManifestDoesNotTouchStandaloneHost(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("OCODE_HTR_EXTENSION_ORIGIN", "chrome-extension://abcdefghijklmnop/")
	dir, err := nativeMessagingDir("/Applications/Google Chrome.app/Contents/MacOS/Google Chrome")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	standalone := filepath.Join(dir, "com.htrcontrol.host.json")
	if err := os.WriteFile(standalone, []byte("standalone"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureNativeHostManifest("com.ocode.htrcontrol", "/tmp/htrcli", "", "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(standalone)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "standalone" {
		t.Fatalf("standalone native host was modified: %q", data)
	}
	if _, err := os.Stat(filepath.Join(dir, "com.ocode.htrcontrol.json")); err != nil {
		t.Fatalf("namespaced native host missing: %v", err)
	}
	if err := ensureNativeHostManifest("com.htrcontrol.host", "/tmp/htrcli", "", "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"); err == nil {
		t.Fatal("standalone native host name must be rejected")
	}
}

func TestNativeHostBrowserFamilies(t *testing.T) {
	if got := browserFamily("/Applications/Chromium.app/Contents/MacOS/Chromium"); got != "chromium" {
		t.Fatalf("browser family = %q, want chromium", got)
	}
	if got := browserFamily("/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge"); got != "edge" {
		t.Fatalf("browser family = %q, want edge", got)
	}
	if got := nativeMessagingRegistryRoot("brave.exe"); got == "" {
		t.Fatal("Brave must have a native-host registry root")
	}
}

func TestHTRLeaseReleaseRemovesLease(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	lease, err := acquireHTRLease(45678, "test-socket", "test-identity", log.Default())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lease.path); err != nil {
		t.Fatalf("lease file missing: %v", err)
	}
	lease.release()
	if _, err := os.Stat(lease.path); !os.IsNotExist(err) {
		t.Fatalf("lease file still exists after release: %v", err)
	}
}

func TestHTRDaemonExitIsMarkedAndCanBeReplaced(t *testing.T) {
	sup := newTestSupervisor(t)
	cmd := exec.Command("sh", "-c", "exit 7")
	rec, err := tool.StartSupervised(sup, cmd, tool.ProcessRegistration{ID: htrServeID, Name: "htrcli serve", Kind: tool.ProcessKindHTR})
	if err != nil {
		t.Fatal(err)
	}
	go watchHTRExit(cmd, sup, rec, "test-identity", log.Default())
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got, ok := sup.Lookup(htrServeID); ok && got.Status != tool.ProcRunning {
			if got.ExitCode != 7 {
				t.Fatalf("exit code = %d, want 7", got.ExitCode)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("daemon exit was not marked")
}

func TestEmbeddedExtensionGetsStableNativeMessagingOrigin(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(manifest, []byte(`{"manifest_version":3,"name":"test"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := pinExtensionKey(dir); err != nil {
		t.Fatal(err)
	}
	origins, err := nativeHostAllowedOrigins(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(origins) != 1 || !strings.HasPrefix(origins[0], "chrome-extension://") || !strings.HasSuffix(origins[0], "/") {
		t.Fatalf("origins = %#v", origins)
	}
}

func TestEmbeddedExtensionNativeHostNameIsNamespaced(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "worker.js"), []byte(`connectNative("com.htrcontrol.host")`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := rewriteNativeHostName(dir, "com.test.htr"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "worker.js"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "com.htrcontrol.host") || !strings.Contains(string(data), "com.test.htr") {
		t.Fatalf("rewritten extension = %q", data)
	}
}

// freePort returns a currently-unused loopback TCP port so status/stop tests
// never collide with a real managed daemon in the developer environment.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestHTRDaemonStatus(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	identity := "status-managed"
	socket := "status-socket"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			serveHTRHealth(w, r, identity, socket)
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(strings.Split(u.Host, ":")[1])

	// No owner marker: reported stopped but still labeled with addr/port.
	info := HTRDaemonStatus(port, socket)
	if info.Running || info.Managed {
		t.Fatalf("no marker must report stopped: %+v", info)
	}
	if info.Port != port || info.Addr != htrAddr(port) {
		t.Fatalf("addr/port = %q/%d, want %q/%d", info.Addr, info.Port, htrAddr(port), port)
	}

	// Owner marker + healthy daemon: running, managed, binary from the marker.
	if err := htrWriteOwner(identity, port, os.Getpid(), socket, "/opt/htrcli", time.Now()); err != nil {
		t.Fatal(err)
	}
	info = HTRDaemonStatus(port, socket)
	if !info.Running || !info.Managed {
		t.Fatalf("healthy marker must report running: %+v", info)
	}
	if info.Binary != "/opt/htrcli" {
		t.Fatalf("binary = %q, want /opt/htrcli", info.Binary)
	}
}

func TestStopHTRServe_NoMarkerIsNoop(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	st, err := StopHTRServe(nil, freePort(t), log.Default())
	if err != nil {
		t.Fatalf("no-op stop → %v", err)
	}
	if st.Running {
		t.Fatalf("no marker must report not running: %+v", st)
	}
}

func TestStopHTRServe_RemovesStaleMarker(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	port := freePort(t)
	// PID 999999 is not alive; the marker is stale and must be cleaned up.
	if err := htrWriteOwner("stale", port, 999999, "stale-socket", os.Args[0], time.Now()); err != nil {
		t.Fatal(err)
	}
	st, err := StopHTRServe(nil, port, log.Default())
	if err != nil {
		t.Fatalf("stale stop → %v", err)
	}
	if st.Running {
		t.Fatalf("stale marker must report not running: %+v", st)
	}
	if _, err := readHTROwner(); err == nil {
		t.Fatal("stale owner marker must be removed")
	}
}

func TestStopHTRServe_RefusesUnverified(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	port := freePort(t)
	// Our own PID is alive, but neither the executable match nor a managed
	// health probe can attribute it to ocode, so the stop must refuse.
	if err := htrWriteOwner("unverified", port, os.Getpid(), "unverified-socket", filepath.Join(t.TempDir(), "not-a-real-binary"), time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := StopHTRServe(nil, port, log.Default()); err == nil {
		t.Fatal("unverified daemon must not be stopped")
	}
	if _, err := readHTROwner(); err != nil {
		t.Fatal("a refused stop must keep the owner marker")
	}
}

func TestStopHTRServe_KillsVerifiedDaemon(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX sleep helper process")
	}
	t.Setenv("HOME", t.TempDir())
	identity := "stop-managed"
	socket := "stop-socket"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			serveHTRHealth(w, r, identity, socket)
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(strings.Split(u.Host, ":")[1])

	helper := exec.Command("sleep", "30")
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = helper.Process.Kill() }()

	// The executable does not match the helper's comm, so verification comes
	// from the managed health probe.
	if err := htrWriteOwner(identity, port, helper.Process.Pid, socket, os.Args[0], time.Now()); err != nil {
		t.Fatal(err)
	}
	sup := newTestSupervisor(t)
	if _, err := StopHTRServe(sup, port, log.Default()); err != nil {
		t.Fatalf("stop verified daemon → %v", err)
	}
	if _, err := readHTROwner(); err == nil {
		t.Fatal("owner marker must be removed after stop")
	}
	// Wait reaps the killed child; a kill surfaces as a non-nil error.
	done := make(chan error, 1)
	go func() { done <- helper.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("helper exited cleanly, expected it to be killed")
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("helper pid %d still alive after stop", helper.Process.Pid)
	}
}

func TestListHTRTabs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	identity := "tabs-managed"
	socket := "tabs-socket"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+identity {
			w.WriteHeader(401)
			_, _ = fmt.Fprint(w, `{"ok":false,"error":"unauthorized"}`)
			return
		}
		switch r.URL.Path {
		case "/api/health":
			serveHTRHealth(w, r, identity, socket)
		case "/api/tabs":
			w.WriteHeader(200)
			_, _ = fmt.Fprint(w, `{"ok":true,"data":[{"id":7,"url":"https://example.com","title":"Example","active":true,"browser":"chrome"}]}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(strings.Split(u.Host, ":")[1])

	// No marker: a standalone/absent daemon is never queried.
	if _, err := ListHTRTabs(port); err == nil {
		t.Fatal("listing without an owner marker must error")
	}
	if err := htrWriteOwner(identity, port, os.Getpid(), socket, "/opt/htrcli", time.Now()); err != nil {
		t.Fatal(err)
	}
	tabs, err := ListHTRTabs(port)
	if err != nil {
		t.Fatalf("list tabs → %v", err)
	}
	if len(tabs) != 1 {
		t.Fatalf("tabs = %+v, want 1", tabs)
	}
	if tabs[0].ID != 7 || tabs[0].URL != "https://example.com" || !tabs[0].Active || tabs[0].Browser != "chrome" {
		t.Fatalf("tab = %+v", tabs[0])
	}
}
