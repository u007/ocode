package remote

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/tool"
)

// installFakeSSH puts a fake `ssh` on PATH that just sleeps, so ForwardManager
// can start/stop a real supervised child without a network. The local port is
// opened by the test itself, which is enough for Start's readiness probe (it
// only checks that something accepts on the local port — the documented
// limitation shared by all three ForwardManager call sites).
func installFakeSSH(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "ssh")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestForwardRegistrationIDDistinctFromTunnelID(t *testing.T) {
	// StartTunnel keys its own registration on fmt.Sprintf("remote-tunnel-%d",
	// apiPort); ForwardManager must never collide with that ID space for the
	// same numeric port, or ProcessSupervisor.Register would reject the
	// second registration as a duplicate.
	if got := forwardRegistrationID(4096); got == "remote-tunnel-4096" {
		t.Fatalf("forwardRegistrationID(4096) = %q, collides with StartTunnel's ID space", got)
	}
}

func TestForwardManagerIsLiveFalseBeforeStart(t *testing.T) {
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
	fm := NewForwardManager(sup, Target{Kind: KindSSH, Host: "example.invalid"})
	if fm.IsLive(3000) {
		t.Fatal("IsLive(3000) = true before any Start call")
	}
}

func TestForwardManagerStopNonLiveIsNoop(t *testing.T) {
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
	fm := NewForwardManager(sup, Target{Kind: KindSSH, Host: "example.invalid"})
	if err := fm.Stop(3000); err != nil {
		t.Fatalf("Stop on a never-started port returned an error: %v", err)
	}
}

// TestForwardManagerRestartAfterStop covers the panel's Disable → Enable cycle
// (and a remove → re-add of the same remote port). The forward's supervisor
// registration ID is deliberately stable per port, and Stop marks that record
// terminal — the supervisor retains terminal records, so before ReplaceTerminal
// the second Start failed with "process \"remote-portmap-N\" already registered"
// and the handler surfaced it as 502 "enabled, but failed to open now".
func TestForwardManagerRestartAfterStop(t *testing.T) {
	installFakeSSH(t)

	// A listener on the local port makes Start's readiness probe succeed.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	localPort, _ := strconv.Atoi(portStr)

	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{GracePeriod: 10 * time.Millisecond})
	defer func() { _ = sup.Shutdown(context.Background()) }()
	fm := NewForwardManager(sup, Target{Kind: KindSSH, Host: "example.invalid"})
	pm := ProjectPortMap{RemotePort: 3510, LocalPort: localPort, Enabled: true}

	if err := fm.Start(pm); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if !fm.IsLive(pm.RemotePort) {
		t.Fatal("IsLive = false after a successful Start")
	}

	if err := fm.Stop(pm.RemotePort); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if fm.IsLive(pm.RemotePort) {
		t.Fatal("IsLive = true after Stop")
	}

	// The re-enable must not collide with the stopped (terminal) record.
	if err := fm.Start(pm); err != nil {
		if strings.Contains(err.Error(), "already registered") {
			t.Fatalf("Start after Stop collided on the supervisor ID: %v", err)
		}
		t.Fatalf("Start after Stop: %v", err)
	}
	if !fm.IsLive(pm.RemotePort) {
		t.Fatal("IsLive = false after re-enabling")
	}
	if err := fm.Stop(pm.RemotePort); err != nil {
		t.Fatalf("final Stop: %v", err)
	}
}
