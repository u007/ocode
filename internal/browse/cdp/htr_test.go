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

func TestHTRHealthy(t *testing.T) {
	lg := log.Default()
	identity := "test-managed"
	socket := "test-socket"
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			w.WriteHeader(200)
			_, _ = fmt.Fprintf(w, `{"service":"htrcli","managed":true,"identity":%q,"port":%d,"socket":%q}`, identity, testPort(r), socket)
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
			w.WriteHeader(200)
			_, _ = fmt.Fprintf(w, `{"service":"htrcli","managed":true,"identity":%q,"port":%d,"socket":%q}`, identity, testPort(r), socket)
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
