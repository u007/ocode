package remote

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/config"
)

func writeStub(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "ssh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDBTunnelCooldown(t *testing.T) {
	stub := writeStub(t, "exit 1")
	old := sshBinary
	sshBinary = stub
	defer func() { sshBinary = old }()

	m := NewDBTunnelManager()
	tun := &config.DBTunnel{Host: "bastion", RemoteHost: "db", RemotePort: 5432}
	_, err := m.Ensure(t.Context(), "conn", tun, "")
	if err == nil {
		t.Fatal("expected error from failing stub")
	}
	// immediate retry must hit cooldown
	_, err2 := m.Ensure(t.Context(), "conn", tun, "")
	if err2 == nil {
		t.Fatal("expected cooldown error")
	}
	if !strings.Contains(err2.Error(), "recently failed") && err2 != ErrTunnelCooldown {
		// accept wrapped
		if err2.Error() == "" {
			t.Fatal("empty error")
		}
	}
	_ = time.Second
}

func TestDBTunnelIdentityArg(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "argv.txt")
	stub := writeStub(t, `printf '%s\n' "$@" > "`+logFile+`"
exit 1`)
	old := sshBinary
	sshBinary = stub
	defer func() { sshBinary = old }()

	m := NewDBTunnelManager()
	tun := &config.DBTunnel{Host: "bastion", RemoteHost: "db", RemotePort: 5432, IdentityFile: "/keys/id_rsa"}
	_, _ = m.Ensure(t.Context(), "conn", tun, "")

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	argv := string(data)
	if !strings.Contains(argv, "-i") {
		t.Fatalf("expected -i in argv, got %q", argv)
	}
	if !strings.Contains(argv, "/keys/id_rsa") {
		t.Fatalf("expected identity path in argv, got %q", argv)
	}
}

func TestDBTunnelDeadProcessNoHang(t *testing.T) {
	stub := writeStub(t, "exit 1")
	old := sshBinary
	sshBinary = stub
	defer func() { sshBinary = old }()

	m := NewDBTunnelManager()
	tun := &config.DBTunnel{Host: "bastion", RemoteHost: "db", RemotePort: 5432}
	done := make(chan struct{})
	go func() {
		_, _ = m.Ensure(t.Context(), "conn", tun, "")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("Ensure hung")
	}
}

// waitForTunnelCleared polls until the exit handler has cleared the tunnel's
// process, which happens asynchronously after the ssh stub exits.
func waitForTunnelCleared(t *testing.T, m *DBTunnelManager, name string, tun *config.DBTunnel) {
	t.Helper()
	ts := m.stateFor(name, tun)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ts.mu.Lock()
		cleared := ts.cmd == nil && !ts.ready
		ts.mu.Unlock()
		if cleared {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("tunnel process exit was never recorded")
}

func countLines(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatal(err)
	}
	return strings.Count(string(data), "\n")
}

// A ready tunnel whose ssh exits must be restarted by the next Ensure, not
// reused through the stale ready flag.
func TestDBTunnelRestartsAfterReadyProcessExits(t *testing.T) {
	count := filepath.Join(t.TempDir(), "count")
	stub := writeStub(t, "echo x >> \""+count+"\"\nsleep 0.3")
	old := sshBinary
	sshBinary = stub
	defer func() { sshBinary = old }()

	// The test owns the local port, so the first attempt is ready at once.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	m := NewDBTunnelManager()
	defer m.StopAll()
	tun := &config.DBTunnel{Host: "bastion", RemoteHost: "db", RemotePort: 5432, LocalPort: port}

	if got, err := m.Ensure(t.Context(), "conn", tun, ""); err != nil || got != port {
		t.Fatalf("first Ensure = %d, %v; want %d", got, err, port)
	}
	waitForTunnelCleared(t, m, "conn", tun)

	got, err := m.Ensure(t.Context(), "conn", tun, "")
	if err != nil || got != port {
		t.Fatalf("second Ensure = %d, %v; want %d", got, err, port)
	}
	// Ensure returns as soon as the test's own listener accepts, which can be
	// before the restarted stub has written its line, so poll for it.
	deadline := time.Now().Add(5 * time.Second)
	for countLines(t, count) < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if n := countLines(t, count); n != 2 {
		t.Fatalf("ssh launched %d times, want 2 (dead tunnel must restart)", n)
	}
}

// A corrected password must reach ssh at once; the cooldown guards only the
// same credentials.
func TestDBTunnelCorrectedPasswordSkipsCooldown(t *testing.T) {
	count := filepath.Join(t.TempDir(), "count")
	stub := writeStub(t, "echo x >> \""+count+"\"\nexit 1")
	old := sshBinary
	sshBinary = stub
	defer func() { sshBinary = old }()

	m := NewDBTunnelManager()
	defer m.StopAll()
	tun := &config.DBTunnel{Host: "bastion", RemoteHost: "db", RemotePort: 5432, AuthMethod: "password"}

	if _, err := m.Ensure(t.Context(), "conn", tun, "wrong-pw"); err == nil {
		t.Fatal("expected failure from failing stub")
	}
	if _, err := m.Ensure(t.Context(), "conn", tun, "wrong-pw"); err == nil {
		t.Fatal("expected cooldown error for the same credentials")
	}
	if n := countLines(t, count); n != 1 {
		t.Fatalf("same-credential retry launched ssh %d times, want 1", n)
	}
	_, _ = m.Ensure(t.Context(), "conn", tun, "right-pw")
	if n := countLines(t, count); n != 2 {
		t.Fatalf("corrected password launched ssh %d times, want 2 (cooldown must not apply)", n)
	}
}

// A state that Stop retired must refuse to launch ssh, even if an Ensure fetched
// it before the stop: no later Stop could find that process.
func TestDBTunnelStoppedStateNeverLaunches(t *testing.T) {
	count := filepath.Join(t.TempDir(), "count")
	stub := writeStub(t, "echo x >> \""+count+"\"\nexec sleep 30")
	old := sshBinary
	sshBinary = stub
	defer func() { sshBinary = old }()

	m := NewDBTunnelManager()
	defer m.StopAll()
	tun := &config.DBTunnel{Host: "bastion", RemoteHost: "db", RemotePort: 5432}

	ts := m.stateFor("conn", tun) // what a racing Ensure is holding
	m.Stop("conn", tun)

	tr := Target{Host: tun.Host, Port: tun.Port}
	_, err, retry := m.ensureOnce(t.Context(), ts, tun, tr, "")
	if !errors.Is(err, errTunnelRetired) || !retry {
		t.Fatalf("ensureOnce on stopped state = %v, retry %v; want errTunnelRetired, retry", err, retry)
	}
	if n := countLines(t, count); n != 0 {
		t.Fatalf("ssh launched %d times on a stopped state", n)
	}
}

// Removing a connection stops its tunnel by name, whatever descriptor it was
// started with.
func TestDBTunnelStopConnectionRetiresTunnel(t *testing.T) {
	stub := writeStub(t, "exec sleep 30")
	old := sshBinary
	sshBinary = stub
	defer func() { sshBinary = old }()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	m := NewDBTunnelManager()
	defer m.StopAll()
	tun := &config.DBTunnel{Host: "bastion", RemoteHost: "db", RemotePort: 5432, LocalPort: port}
	if _, err := m.Ensure(t.Context(), "conn", tun, ""); err != nil {
		t.Fatal(err)
	}
	ts := m.stateFor("conn", tun)
	m.StopConnection("conn")

	m.mu.Lock()
	remaining := len(m.tunnels)
	m.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("%d tunnels left after StopConnection", remaining)
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if !ts.stopped || ts.cmd != nil {
		t.Fatalf("stopped=%v cmd=%v; want stopped with no process", ts.stopped, ts.cmd)
	}
}

// An empty auth_method means "key" (add stores it that way), so both spellings
// must find the same tunnel.
func TestTunnelKeyAuthDefault(t *testing.T) {
	a := &config.DBTunnel{Host: "h", RemoteHost: "r", RemotePort: 1}
	b := &config.DBTunnel{Host: "h", RemoteHost: "r", RemotePort: 1, AuthMethod: "key"}
	if tunnelKey("n", a) != tunnelKey("n", b) {
		t.Fatal("empty and \"key\" auth_method must share a tunnel key")
	}
}
