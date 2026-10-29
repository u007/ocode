package remote

import (
	"context"
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

const ssBusy = `State  Recv-Q Send-Q Local Address:Port Peer Address:Port Process
LISTEN 0      128        127.0.0.1:3845      0.0.0.0:*     users:(("sshd",pid=777,fd=9))
LISTEN 0      128        127.0.0.1:38450     0.0.0.0:*     users:(("other",pid=1,fd=3))
`

func holderTransport(ss string) *fakeTransport {
	ft := newFakeTransport()
	ft.execResults["ss -ltnp 2>/dev/null || true"] = ExecResult{Stdout: ss}
	ft.execResults["ps -o args= -p 777 2>/dev/null || true"] = ExecResult{Stdout: "sshd: james\n"}
	return ft
}

func TestFindPortHolderReadsPidAndCommand(t *testing.T) {
	h, err := FindPortHolder(context.Background(), holderTransport(ssBusy), 3845)
	if err != nil {
		t.Fatal(err)
	}
	if h.PID != 777 || h.Command != "sshd: james" {
		t.Fatalf("got %+v", h)
	}
}

func TestFindPortHolderFreePort(t *testing.T) {
	ft := holderTransport("State Recv-Q\n")
	_, err := FindPortHolder(context.Background(), ft, 3845)
	if !IsPortFree(err) {
		t.Fatalf("want port-free error, got %v", err)
	}
}

func TestKillPortHolderRefusesChangedPid(t *testing.T) {
	ft := holderTransport(ssBusy)
	err := KillPortHolder(context.Background(), ft, 3845, 999)
	if err == nil || !strings.Contains(err.Error(), "now held by pid 777") {
		t.Fatalf("want changed-holder error, got %v", err)
	}
	for _, c := range ft.execCalls {
		if strings.HasPrefix(c, "kill ") {
			t.Fatalf("must not kill a pid that no longer holds the port: %v", ft.execCalls)
		}
	}
}

func TestKillPortHolderRefusesInit(t *testing.T) {
	if err := KillPortHolder(context.Background(), newFakeTransport(), 3845, 1); err == nil {
		t.Fatal("pid 1 must be refused")
	}
}

func TestKillPortHolderKillsVerifiedPid(t *testing.T) {
	ks := &killSeq{fakeTransport: holderTransport(ssBusy)}
	if err := KillPortHolder(context.Background(), ks, 3845, 777); err != nil {
		t.Fatal(err)
	}
	if !ks.killed {
		t.Fatal("kill not issued")
	}
}

// killSeq flips the ss answer to "empty" once `kill` has run.
type killSeq struct {
	*fakeTransport
	killed bool
	kills  []string
}

func (k *killSeq) ExecContext(ctx context.Context, command string) (ExecResult, error) {
	if strings.HasPrefix(command, "kill ") {
		k.killed = true
		k.kills = append(k.kills, command)
	}
	if k.killed && command == "ss -ltnp 2>/dev/null || true" {
		return ExecResult{Stdout: "State Recv-Q\n"}, nil
	}
	return k.fakeTransport.ExecContext(ctx, command)
}

// hangingTransport never answers: every exec blocks until its context ends,
// like an ssh child stuck on a dead host.
type hangingTransport struct {
	*fakeTransport
}

func (h *hangingTransport) ExecContext(ctx context.Context, command string) (ExecResult, error) {
	<-ctx.Done()
	return ExecResult{}, ctx.Err()
}

func TestPortBusyErrorKeepsMessageAndMatches(t *testing.T) {
	base := errors.New("forward did not become ready (ssh: remote port forwarding failed)")
	var err error = &PortBusyError{Err: base}
	if err.Error() != base.Error() {
		t.Fatal("message must be unchanged")
	}
	var pb *PortBusyError
	if !errors.As(err, &pb) {
		t.Fatal("errors.As must match")
	}
}

func stubLocal(t *testing.T, find func(context.Context, int) ([]int, error), kill func(int) error) {
	t.Helper()
	of, oc, ok := findLocalListener, localCmdline, signalLocalTerm
	findLocalListener, localCmdline, signalLocalTerm = find, func(int) (string, error) { return "node dev.js\n", nil }, kill
	t.Cleanup(func() { findLocalListener, localCmdline, signalLocalTerm = of, oc, ok })
}

func TestFindLocalPortHolder(t *testing.T) {
	stubLocal(t, func(context.Context, int) ([]int, error) { return []int{55}, nil }, nil)
	h, err := FindLocalPortHolder(context.Background(), 3000)
	if err != nil || h.PID != 55 || h.Command != "node dev.js" {
		t.Fatalf("got %+v, %v", h, err)
	}
}

func TestKillLocalPortHolderRefusesChangedPidSelfAndInit(t *testing.T) {
	stubLocal(t, func(context.Context, int) ([]int, error) { return []int{55}, nil }, func(int) error {
		t.Fatal("must not signal")
		return nil
	})
	ctx := context.Background()
	if err := KillLocalPortHolder(ctx, 3000, 56); err == nil || !strings.Contains(err.Error(), "now held by pid 55") {
		t.Fatalf("want changed-holder error, got %v", err)
	}
	if err := KillLocalPortHolder(ctx, 3000, 1); err == nil {
		t.Fatal("pid 1 must be refused")
	}
	if err := KillLocalPortHolder(ctx, 3000, os.Getpid()); err == nil {
		t.Fatal("ocode's own pid must be refused")
	}
}

func TestKillLocalPortHolderSignalsAndWaitsForFree(t *testing.T) {
	killed := false
	stubLocal(t, func(context.Context, int) ([]int, error) {
		if killed {
			return nil, errNoListener
		}
		return []int{55}, nil
	}, func(pid int) error { killed = pid == 55; return nil })
	if err := KillLocalPortHolder(context.Background(), 3000, 55); err != nil || !killed {
		t.Fatalf("err=%v killed=%v", err, killed)
	}
}

// A socket shared by a master and its workers is held by every one of them:
// SIGTERM to only the reported pid leaves the port bound. Every listener must
// be signalled, and the port must be confirmed free.
func TestKillLocalPortHolderSignalsEverySharedListener(t *testing.T) {
	var signalled []int
	freed := false
	stubLocal(t, func(context.Context, int) ([]int, error) {
		if freed {
			return nil, errNoListener
		}
		return []int{55, 56, 57}, nil
	}, func(pid int) error {
		signalled = append(signalled, pid)
		if len(signalled) == 3 {
			freed = true
		}
		return nil
	})
	if err := KillLocalPortHolder(context.Background(), 3000, 56); err != nil {
		t.Fatal(err)
	}
	if len(signalled) != 3 || signalled[0] != 55 || signalled[1] != 56 || signalled[2] != 57 {
		t.Fatalf("want every listener signalled, got %v", signalled)
	}
}

// A master can reap its worker between the listing and the signal. That worker
// is already gone, so the kill still succeeds once the port frees.
func TestKillLocalPortHolderToleratesListenerGoneBeforeSignal(t *testing.T) {
	var signalled []int
	freed := false
	stubLocal(t, func(context.Context, int) ([]int, error) {
		if freed {
			return nil, errNoListener
		}
		return []int{55, 56}, nil
	}, func(pid int) error {
		signalled = append(signalled, pid)
		if pid == 56 {
			return syscall.ESRCH
		}
		freed = true
		return nil
	})
	if err := KillLocalPortHolder(context.Background(), 3000, 55); err != nil {
		t.Fatalf("a listener that exited before SIGTERM must not fail the kill: %v", err)
	}
	if len(signalled) != 2 {
		t.Fatalf("want both listeners signalled, got %v", signalled)
	}
}

// One refused member means nothing is signalled: a partial kill of a shared
// socket is worse than no kill.
func TestKillLocalPortHolderRefusesSetContainingInit(t *testing.T) {
	stubLocal(t, func(context.Context, int) ([]int, error) { return []int{55, 1}, nil }, func(int) error {
		t.Fatal("must not signal any member when one is pid 1")
		return nil
	})
	if err := KillLocalPortHolder(context.Background(), 3000, 55); err == nil || !strings.Contains(err.Error(), "shares local port") {
		t.Fatalf("want refusal, got %v", err)
	}
}

// The wait for the port to free is bounded by the caller's deadline, not the
// poll count: a holder that never lets go must return promptly.
func TestKillLocalPortHolderBoundedByDeadline(t *testing.T) {
	stubLocal(t, func(context.Context, int) ([]int, error) { return []int{55}, nil }, func(int) error { return nil })
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := KillLocalPortHolder(ctx, 3000, 55)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want deadline error, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("deadline not honoured, took %v", elapsed)
	}
}

func TestKillPortHolderBoundedByDeadline(t *testing.T) {
	hang := &hangingTransport{fakeTransport: newFakeTransport()}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := KillPortHolder(ctx, hang, 3845, 777)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want deadline error from a hung ssh, got %v", err)
	}
}

// A port that never frees must stop at the deadline, not after all polls.
func TestKillPortHolderStopsAtDeadlineWhilePortStaysBound(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := KillPortHolder(ctx, &stuckPortTransport{fakeTransport: holderTransport(ssBusy)}, 3845, 777)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want deadline error, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("deadline not honoured, took %v", elapsed)
	}
}

// stuckPortTransport accepts every kill, but the ss answer stays busy forever.
type stuckPortTransport struct {
	*fakeTransport
}

const ssShared = `State  Recv-Q Send-Q Local Address:Port Peer Address:Port Process
LISTEN 0      511        0.0.0.0:3845      0.0.0.0:*     users:(("node",pid=777,fd=9),("node",pid=778,fd=9),("node",pid=779,fd=9))
`

// A socket shared by a remote master and workers: every holder is signalled in
// one kill, and the reported pid is the first listener.
func TestKillPortHolderSignalsEverySharedRemoteListener(t *testing.T) {
	ft := holderTransport(ssShared)
	ft.execResults["ps -o args= -p 777 2>/dev/null || true"] = ExecResult{Stdout: "node server.js\n"}
	ks := &killSeq{fakeTransport: ft}
	if err := KillPortHolder(context.Background(), ks, 3845, 778); err != nil {
		t.Fatal(err)
	}
	if len(ks.kills) != 1 || ks.kills[0] != "kill 777 778 779" {
		t.Fatalf("want one kill of every listener, got %v", ks.kills)
	}
}

func TestLocalBindRefusedMatchesSSHMessages(t *testing.T) {
	for _, m := range []string{"bind [127.0.0.1]:3000: Address already in use", "channel_setup_fwd_listener_tcpip: cannot listen to port: 3000"} {
		if !localBindRefused(m) {
			t.Fatalf("%q must count as a refused bind", m)
		}
	}
	if localBindRefused("Connection refused") {
		t.Fatal("unrelated ssh error matched")
	}
}
