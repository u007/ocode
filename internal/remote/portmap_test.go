package remote

import (
	"context"
	"fmt"
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

// installFakeSSHExiting puts a fake `ssh` on PATH that opens nothing and exits
// on its own after lifetime — the shape of a real forward dying with nobody
// calling Stop (Wi-Fi change, laptop sleep/wake, remote host reboot).
func installFakeSSHExiting(t *testing.T, lifetime time.Duration) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "ssh")
	script := fmt.Sprintf("#!/bin/sh\nsleep %.3f\nexit 3\n", lifetime.Seconds())
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// installFakeSSHOnceThenSleep puts a fake `ssh` on PATH that exits immediately
// on its first invocation and sleeps on every later one, so a test can make a
// forward die on its own and then watch the replacement child stay up.
func installFakeSSHOnceThenSleep(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	marker := filepath.Join(dir, "invoked")
	bin := filepath.Join(dir, "ssh")
	// The marker is an on-disk handshake between the two invocations; a
	// script-local shell variable would not survive process exit.
	script := "#!/bin/sh\nif [ -f " + marker + " ]; then exec sleep 30; fi\n: > " + marker + "\nexit 3\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// listenLocalPort opens a throwaway listener so Start's readiness probe
// succeeds (Start only dials the local port — the documented limitation shared
// by all three call sites), returning it and its port.
func listenLocalPort(t *testing.T) (net.Listener, int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, err := strconv.Atoi(portStr)
	if err != nil {
		_ = ln.Close()
		t.Fatalf("parse listener port %q: %v", portStr, err)
	}
	return ln, port
}

// waitForCond polls cond until it holds, failing the test if it never does.
func waitForCond(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", timeout, what)
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

// TestForwardManagerReportsDeadAfterChildExits is the regression for "the port
// map seems dead". m.live was only ever written by Start and deleted by Stop,
// so an ssh child that died on its own (network drop, sleep/wake, host reboot)
// left the entry behind. IsLive — a bare map lookup — then answered true
// forever, and Start short-circuited on the same stale entry and returned nil
// without opening anything. The panel showed a live forward with nothing
// listening, and Disable → Enable could not revive it.
func TestForwardManagerReportsDeadAfterChildExits(t *testing.T) {
	installFakeSSHExiting(t, 150*time.Millisecond)
	ln, localPort := listenLocalPort(t)
	defer ln.Close()

	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{GracePeriod: 10 * time.Millisecond})
	defer func() { _ = sup.Shutdown(context.Background()) }()
	fm := NewForwardManager(sup, Target{Kind: KindSSH, Host: "example.invalid"})
	pm := ProjectPortMap{RemotePort: 3510, LocalPort: localPort, Enabled: true}

	if err := fm.Start(pm); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !fm.IsLive(pm.RemotePort) {
		t.Fatal("IsLive = false immediately after a successful Start")
	}
	waitForCond(t, 10*time.Second, "the forward to report itself dead after its child exited", func() bool {
		return !fm.IsLive(pm.RemotePort)
	})
}

// TestForwardManagerMarksSupervisorRecordTerminalOnUnexpectedExit pins the
// second half of the same defect. StartSupervised installs a no-op waitFn for
// these children ("the manager owns Wait"), and nothing ever performed that
// Wait, so the record stayed ProcRunning forever after an unexpected exit.
// That is what made a restart attempt fail: canReplace in
// process_supervisor.go only replaces a *terminal* record, so the retry took
// the duplicate-running path, killed its own freshly started child, and
// returned "process remote-portmap-N already registered".
func TestForwardManagerMarksSupervisorRecordTerminalOnUnexpectedExit(t *testing.T) {
	installFakeSSHExiting(t, 150*time.Millisecond)
	ln, localPort := listenLocalPort(t)
	defer ln.Close()

	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{GracePeriod: 10 * time.Millisecond})
	defer func() { _ = sup.Shutdown(context.Background()) }()
	fm := NewForwardManager(sup, Target{Kind: KindSSH, Host: "example.invalid"})
	pm := ProjectPortMap{RemotePort: 3510, LocalPort: localPort, Enabled: true}

	if err := fm.Start(pm); err != nil {
		t.Fatalf("Start: %v", err)
	}
	id := forwardRegistrationID(pm.RemotePort)
	if rec, ok := sup.Lookup(id); !ok {
		t.Fatalf("supervisor has no record for %q", id)
	} else if rec.Status != tool.ProcRunning {
		t.Fatalf("record status = %q while the child runs, want %q", rec.Status, tool.ProcRunning)
	}
	waitForCond(t, 10*time.Second, "the supervisor record to go terminal", func() bool {
		rec, ok := sup.Lookup(id)
		return ok && rec.Status != tool.ProcRunning
	})
}

// TestForwardManagerRestartAfterUnexpectedExit proves a forward that died
// without a Stop can be re-opened at all. Before the fix, Start found the stale
// live entry and returned nil as a no-op, so the forward stayed dead.
func TestForwardManagerRestartAfterUnexpectedExit(t *testing.T) {
	installFakeSSHOnceThenSleep(t)
	ln, localPort := listenLocalPort(t)
	defer ln.Close()

	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{GracePeriod: 10 * time.Millisecond})
	defer func() { _ = sup.Shutdown(context.Background()) }()
	fm := NewForwardManager(sup, Target{Kind: KindSSH, Host: "example.invalid"})
	pm := ProjectPortMap{RemotePort: 3510, LocalPort: localPort, Enabled: true}

	if err := fm.Start(pm); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	waitForCond(t, 10*time.Second, "the first child to exit on its own", func() bool {
		return !fm.IsLive(pm.RemotePort)
	})

	if err := fm.Start(pm); err != nil {
		if strings.Contains(err.Error(), "already registered") {
			t.Fatalf("Start after an unexpected exit collided on the supervisor ID: %v", err)
		}
		t.Fatalf("Start after an unexpected exit: %v", err)
	}
	if !fm.IsLive(pm.RemotePort) {
		t.Fatal("IsLive = false after restarting a forward that died unexpectedly")
	}
	if err := fm.Stop(pm.RemotePort); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

// TestForwardManagerStopAfterUnexpectedExitDoesNotHang guards the ownership
// change that makes the detection work: the manager now performs cmd.Wait, so
// Stop must kill and then wait for the reaper instead of calling Wait itself.
// Double-Waiting a process, or waiting on a signal nobody sends, hangs.
func TestForwardManagerStopAfterUnexpectedExitDoesNotHang(t *testing.T) {
	installFakeSSHExiting(t, 100*time.Millisecond)
	ln, localPort := listenLocalPort(t)
	defer ln.Close()

	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{GracePeriod: 10 * time.Millisecond})
	defer func() { _ = sup.Shutdown(context.Background()) }()
	fm := NewForwardManager(sup, Target{Kind: KindSSH, Host: "example.invalid"})
	pm := ProjectPortMap{RemotePort: 3510, LocalPort: localPort, Enabled: true}

	if err := fm.Start(pm); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForCond(t, 10*time.Second, "the child to exit on its own", func() bool {
		return !fm.IsLive(pm.RemotePort)
	})

	done := make(chan error, 1)
	go func() { done <- fm.Stop(pm.RemotePort) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Stop after an unexpected exit: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Stop hung after the forward's child had already exited")
	}
}

// TestForwardManagerStopDoesNotFireOnExit pins the fix for the orphaned-forward
// bug: reaping a child that was torn down on purpose must NOT run the monitor
// hook. If it did, a removal's forget (which clears retry state) would be
// followed by noteExit recreating it, and the watchdog could re-open a forward
// that is being deleted — leaving an ssh child nothing tracks.
func TestForwardManagerStopDoesNotFireOnExit(t *testing.T) {
	installFakeSSH(t) // stays up until killed
	ln, localPort := listenLocalPort(t)
	defer ln.Close()

	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{GracePeriod: 10 * time.Millisecond})
	defer func() { _ = sup.Shutdown(context.Background()) }()
	fm := NewForwardManager(sup, Target{Kind: KindSSH, Host: "example.invalid"})

	fired := make(chan struct{}, 1)
	fm.SetOnExit(func(int, int, time.Duration) { fired <- struct{}{} })

	pm := ProjectPortMap{RemotePort: 3510, LocalPort: localPort, Enabled: true}
	if err := fm.Start(pm); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := fm.Stop(pm.RemotePort); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	select {
	case <-fired:
		t.Fatal("onExit fired for a requested Stop; the watchdog could re-open a forward being removed")
	case <-time.After(300 * time.Millisecond):
	}
}

// TestForwardManagerStartFailureDoesNotFireOnExit is the double-count
// regression: when the readiness probe fails, Start itself reports the failure
// to its caller (which counts it once). The reaper must not also fire the
// monitor hook, or every failed restart counts twice — halving the attempts
// before give-up and doubling the backoff growth.
func TestForwardManagerStartFailureDoesNotFireOnExit(t *testing.T) {
	installFakeSSH(t) // child stays up while the readiness probe fails
	// Reserve then close a port so nothing is listening on it: the probe dials
	// 127.0.0.1:port and must fail.
	ln, localPort := listenLocalPort(t)
	_ = ln.Close()

	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{GracePeriod: 10 * time.Millisecond})
	defer func() { _ = sup.Shutdown(context.Background()) }()
	fm := NewForwardManager(sup, Target{Kind: KindSSH, Host: "example.invalid"})

	fired := make(chan struct{}, 1)
	fm.SetOnExit(func(int, int, time.Duration) { fired <- struct{}{} })

	pm := ProjectPortMap{RemotePort: 3510, LocalPort: localPort, Enabled: true}
	if err := fm.Start(pm); err == nil {
		t.Fatal("Start succeeded with nothing listening on the local port, want a readiness failure")
	}

	select {
	case <-fired:
		t.Fatal("onExit fired for Start's own readiness failure; the failure is double-counted")
	case <-time.After(300 * time.Millisecond):
	}
}
