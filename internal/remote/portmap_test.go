package remote

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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
	if got := forwardRegistrationID(Target{Kind: KindSSH, Host: "example.invalid"}, 4096); got == "remote-tunnel-4096" {
		t.Fatalf("forwardRegistrationID(4096) = %q, collides with StartTunnel's ID space", got)
	}
}

// TestForwardRegistrationIDIsTargetScoped pins the cross-project collision: all
// projects' ForwardManagers share ONE process supervisor, so a port-only ID
// meant a second project forwarding the same remote port hit "already
// registered" and its forward could never open.
func TestForwardRegistrationIDIsTargetScoped(t *testing.T) {
	a := Target{Kind: KindSSH, User: "u", Host: "host-a"}
	b := Target{Kind: KindSSH, User: "u", Host: "host-b"}
	if forwardRegistrationID(a, 4096) == forwardRegistrationID(b, 4096) {
		t.Fatal("two projects' forwards on the same remote port share a supervisor ID; the second can never start")
	}
}

// TestForwardStartSameRemotePortTwoProjects is the integration form: with real
// Start calls against one supervisor, the second project must succeed where
// the old port-only ID made it fail with "already registered".
func TestForwardStartSameRemotePortTwoProjects(t *testing.T) {
	installFakeSSHExiting(t, 10*time.Second)
	lnA, portA := listenLocalPort(t)
	defer lnA.Close()
	lnB, portB := listenLocalPort(t)
	defer lnB.Close()

	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{GracePeriod: 10 * time.Millisecond})
	defer func() { _ = sup.Shutdown(context.Background()) }()

	fmA := NewForwardManager(sup, Target{Kind: KindSSH, User: "u", Host: "host-a"})
	fmB := NewForwardManager(sup, Target{Kind: KindSSH, User: "u", Host: "host-b"})
	if err := fmA.Start(ProjectPortMap{RemotePort: 3510, LocalPort: portA, Enabled: true}); err != nil {
		t.Fatalf("project A Start: %v", err)
	}
	if err := fmB.Start(ProjectPortMap{RemotePort: 3510, LocalPort: portB, Enabled: true}); err != nil {
		t.Fatalf("project B Start on the same remote port: %v", err)
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
	id := forwardRegistrationID(fm.target, pm.RemotePort)
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

// lockedBuf is a log sink safe to read while the goroutine under test writes it.
type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// An unrecovered panic on ANY goroutine terminates the whole process, so a
// background forward open that panicked would take down the app and every
// session in it. RunAsync must absorb its own and say so in the log.
func TestRunAsyncRecoversAndLogsPanic(t *testing.T) {
	buf := &lockedBuf{}
	orig := log.Writer()
	log.SetOutput(buf)
	t.Cleanup(func() { log.SetOutput(orig) })

	RunAsync("test", func() { panic("boom") })

	// RunAsync's recover runs AFTER fn's own defers unwind, so poll rather than
	// synchronize on anything inside fn.
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(buf.String(), "panicked") {
		if time.Now().After(deadline) {
			t.Fatalf("panic was neither recovered nor logged, got %q", buf.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := buf.String(); !strings.Contains(got, "boom") || !strings.Contains(got, "goroutine") {
		t.Fatalf("panic log lacks the value or a stack: %q", got)
	}
}

// RunAsync exists so a slow forward open never delays its caller. This pins
// that half too: fn has not finished when RunAsync returns.
func TestRunAsyncReturnsBeforeFnFinishes(t *testing.T) {
	release := make(chan struct{})
	finished := make(chan struct{})
	RunAsync("test", func() {
		<-release
		close(finished)
	})
	select {
	case <-finished:
		t.Fatal("RunAsync ran fn to completion before returning; it is not asynchronous")
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("fn never finished after release")
	}
}

// TestForwardArgsDefaultIsLocalForward pins that the -L path is unchanged by
// the reverse branch: same tunnelArgs shape, no -R anywhere.
func TestForwardArgsDefaultIsLocalForward(t *testing.T) {
	got := strings.Join(forwardArgs(ProjectPortMap{RemotePort: 4000, LocalPort: 5000}, Target{Kind: KindSSH, Host: "devbox"}), " ")
	if !strings.Contains(got, "-L 5000:127.0.0.1:4000") {
		t.Fatalf("default forward args = %q, want -L 5000:127.0.0.1:4000", got)
	}
	if strings.Contains(got, "-R") {
		t.Fatalf("default forward args = %q, must not carry -R", got)
	}
}

// TestForwardArgsReverse pins the -R argv: both ends on loopback, the explicit
// bind address on the remote side, and ExitOnForwardFailure so a busy remote
// port fails ssh instead of leaving a silent tunnel.
func TestForwardArgsReverse(t *testing.T) {
	got := forwardArgs(ProjectPortMap{RemotePort: 9222, LocalPort: 9222, Reverse: true}, Target{Kind: KindSSH, User: "u", Host: "devbox"})
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "-R 127.0.0.1:9222:127.0.0.1:9222") {
		t.Fatalf("reverse args = %q, want -R 127.0.0.1:9222:127.0.0.1:9222", joined)
	}
	if !strings.Contains(joined, "ExitOnForwardFailure=yes") {
		t.Fatalf("reverse args = %q, want ExitOnForwardFailure=yes", joined)
	}
	if strings.Contains(joined, " -L ") {
		t.Fatalf("reverse args = %q, must not carry -L", joined)
	}
	if got[len(got)-1] != "u@devbox" {
		t.Fatalf("reverse args = %q, target must be the last argument", joined)
	}
}

// TestForwardArgsReverseCarriesSSHPort checks a non-default ssh port lands
// before the target, the same as the -L path.
func TestForwardArgsReverseCarriesSSHPort(t *testing.T) {
	got := forwardArgs(ProjectPortMap{RemotePort: 9222, LocalPort: 9222, Reverse: true}, Target{Kind: KindSSH, Host: "devbox", Port: 2222})
	joined := strings.Join(got, " ")
	if !strings.HasSuffix(joined, "-p 2222 devbox") {
		t.Fatalf("reverse args = %q, want -p 2222 immediately before the target", joined)
	}
}

// TestForwardStartReverseComesUpWhileChildLives: a -R child that stays alive
// through the probe budget is ready, even though nothing listens on the
// local port the test passes in (the remote bind is what is being proved).
func TestForwardStartReverseComesUpWhileChildLives(t *testing.T) {
	installFakeSSH(t)
	withShrunkTunnelReadyTimings(t, 3, 10*time.Millisecond)
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{GracePeriod: 10 * time.Millisecond})
	defer func() { _ = sup.Shutdown(context.Background()) }()

	fm := NewForwardManager(sup, Target{Kind: KindSSH, Host: "devbox"})
	defer func() { _ = fm.Stop(9222) }()
	if err := fm.Start(ProjectPortMap{RemotePort: 9222, LocalPort: 9222, Enabled: true, Reverse: true}); err != nil {
		t.Fatalf("reverse Start: %v", err)
	}
	if !fm.IsLive(9222) {
		t.Fatal("reverse forward reported not live after a successful Start")
	}
}

// TestForwardStartReverseFailsFastWithSSHReason: ssh refusing the remote bind
// exits at once, so Start must fail on the exit (not wait out the budget) and
// surface ssh's own stderr, which is the only thing that says why.
func TestForwardStartReverseFailsFastWithSSHReason(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "ssh")
	script := "#!/bin/sh\necho 'Error: remote port forwarding failed for listen port 9222' >&2\nexit 255\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	// The budget must outlast a shell's startup, or the budget fires before the
	// child exits and this test would measure the timer, not the early exit.
	withShrunkTunnelReadyTimings(t, 100, 20*time.Millisecond)
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{GracePeriod: 10 * time.Millisecond})
	defer func() { _ = sup.Shutdown(context.Background()) }()

	fm := NewForwardManager(sup, Target{Kind: KindSSH, Host: "devbox"})
	began := time.Now()
	err := fm.Start(ProjectPortMap{RemotePort: 9222, LocalPort: 9222, Enabled: true, Reverse: true})
	if err == nil {
		t.Fatal("reverse Start succeeded although ssh exited at once")
	}
	if elapsed := time.Since(began); elapsed > time.Second {
		t.Fatalf("reverse Start took %s; an early ssh exit must fail well inside the 2s budget", elapsed)
	}
	if !strings.Contains(err.Error(), "remote port forwarding failed for listen port 9222") {
		t.Fatalf("reverse Start error = %v, want ssh's stderr in it", err)
	}
	if fm.IsLive(9222) {
		t.Fatal("failed reverse forward left live")
	}
}

// TestForwardStartReverseRejectsWSL: WSL shares the Windows loopback, so a
// -R forward from it is meaningless. Refused before any ssh is spawned.
func TestForwardStartReverseRejectsWSL(t *testing.T) {
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{GracePeriod: 10 * time.Millisecond})
	defer func() { _ = sup.Shutdown(context.Background()) }()
	fm := NewForwardManager(sup, Target{Kind: KindWSL, Distro: "Ubuntu"})
	err := fm.Start(ProjectPortMap{RemotePort: 9222, LocalPort: 9222, Enabled: true, Reverse: true})
	if err == nil || !strings.Contains(err.Error(), "needs an SSH target") {
		t.Fatalf("reverse Start on WSL error = %v, want SSH-target refusal", err)
	}
}

// installFakeSSHScript puts a fake `ssh` on PATH running body verbatim.
func installFakeSSHScript(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "ssh")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestForwardStopBoundedWhenGrandchildHoldsStderr: a ProxyCommand-style helper
// inherits ssh's stderr pipe and outlives the killed ssh. Stop must return
// within the wait delay, not wait for the helper to exit.
func TestForwardStopBoundedWhenGrandchildHoldsStderr(t *testing.T) {
	// The marker is written only after the background sleep has forked. Without
	// it Stop can kill the shell before the grandchild exists, and the test
	// would pass without ever holding the pipe.
	marker := filepath.Join(t.TempDir(), "forked")
	installFakeSSHScript(t, "sleep 10 &\n: > "+marker+"\nsleep 10")
	ln, port := listenLocalPort(t)
	defer ln.Close()
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{GracePeriod: 10 * time.Millisecond})
	defer func() { _ = sup.Shutdown(context.Background()) }()

	fm := NewForwardManager(sup, Target{Kind: KindSSH, Host: "devbox"})
	if err := fm.Start(ProjectPortMap{RemotePort: 4000, LocalPort: port, Enabled: true}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForFile(t, marker)
	began := time.Now()
	if err := fm.Stop(4000); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if elapsed := time.Since(began); elapsed > 2*forwardWaitDelay+time.Second {
		t.Fatalf("Stop took %s with a grandchild holding stderr; want it bounded by the wait delay", elapsed)
	}
}

// TestForwardLiveClearsWhenGrandchildHoldsStderr: ssh exits on its own while a
// helper still holds its stderr. The forward must read dead within the wait
// delay, not stay live until the helper exits.
func TestForwardLiveClearsWhenGrandchildHoldsStderr(t *testing.T) {
	installFakeSSHScript(t, "sleep 10 &\nexit 3")
	ln, port := listenLocalPort(t)
	defer ln.Close()
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{GracePeriod: 10 * time.Millisecond})
	defer func() { _ = sup.Shutdown(context.Background()) }()

	fm := NewForwardManager(sup, Target{Kind: KindSSH, Host: "devbox"})
	// The local listener makes -L readiness pass before ssh exits, so Start
	// returns and the death is observed by the reaper alone.
	if err := fm.Start(ProjectPortMap{RemotePort: 4000, LocalPort: port, Enabled: true}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	deadline := time.Now().Add(2*forwardWaitDelay + 2*time.Second)
	for fm.IsLive(4000) {
		if time.Now().After(deadline) {
			t.Fatal("exited forward still reads live while a grandchild holds its stderr")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestForwardEarlyExitDuringReadinessSkipsMonitorHook: a refused -R bind exits
// ssh before the forward is ready. Start reports it through its error; the
// monitor hook must not also fire, or one refusal counts twice.
func TestForwardEarlyExitDuringReadinessSkipsMonitorHook(t *testing.T) {
	installFakeSSHScript(t, "echo 'remote port forwarding failed' >&2\nexit 255")
	withShrunkTunnelReadyTimings(t, 100, 20*time.Millisecond)
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{GracePeriod: 10 * time.Millisecond})
	defer func() { _ = sup.Shutdown(context.Background()) }()

	fm := NewForwardManager(sup, Target{Kind: KindSSH, Host: "devbox"})
	fired := make(chan struct{}, 1)
	fm.SetOnExit(func(int, int, time.Duration) { fired <- struct{}{} })
	if err := fm.Start(ProjectPortMap{RemotePort: 9222, LocalPort: 9222, Enabled: true, Reverse: true}); err == nil {
		t.Fatal("reverse Start succeeded although ssh exited at once")
	}
	select {
	case <-fired:
		t.Fatal("monitor hook fired for an exit during readiness; Start's error already counts it")
	case <-time.After(100 * time.Millisecond):
	}
}

// TestForwardExitClaimIsSingle pins the rule that one exit gets exactly one
// report. An exit before readiness wins the claim, so Start's readiness check
// fails and the monitor hook stays silent. An exit after readiness is routed
// to the monitor hook, and Start has already returned success.
func TestForwardExitClaimIsSingle(t *testing.T) {
	early := &forwardProcess{}
	if early.markExited() {
		t.Fatal("an exit before readiness was routed to the monitor hook")
	}
	if early.markReady() {
		t.Fatal("Start claimed readiness after the child exited; the exit would be counted twice")
	}

	late := &forwardProcess{}
	if !late.markReady() {
		t.Fatal("Start could not claim readiness for a live child")
	}
	if !late.markExited() {
		t.Fatal("an exit after readiness was not routed to the monitor hook")
	}
}

// waitForFile polls until path exists, failing the test after a few seconds.
func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never appeared", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
