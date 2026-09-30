package server

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/remote"
)

// A burst of remote execs against one host must queue at the per-host cap
// instead of exceeding sshd's MaxSessions on the shared ControlMaster
// connection — and a full host must not slow down another host.
func TestRemoteExecSlotsCapPerHost(t *testing.T) {
	busy := remote.Target{Kind: remote.KindSSH, Host: "slots-busy.example"}
	other := remote.Target{Kind: remote.KindSSH, Host: "slots-other.example"}

	var releases []func()
	for i := 0; i < remoteExecSlotsPerHost; i++ {
		rel, err := acquireRemoteExecSlot(context.Background(), busy, time.Second, remoteExecForeground)
		if err != nil {
			t.Fatalf("slot %d: %v", i, err)
		}
		releases = append(releases, rel)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := acquireRemoteExecSlot(ctx, busy, 50*time.Millisecond, remoteExecForeground); err == nil {
		t.Fatal("slot past the cap was granted")
	} else if !isRemoteTransportError(err) || !strings.Contains(err.Error(), "waiting for a free") {
		t.Fatalf("over-cap error = %v, want a transport timeout naming the slot wait", err)
	}

	rel, err := acquireRemoteExecSlot(context.Background(), other, time.Second, remoteExecForeground)
	if err != nil {
		t.Fatalf("another host must not share the cap: %v", err)
	}
	rel()

	got := make(chan error, 1)
	go func() {
		rel, err := acquireRemoteExecSlot(context.Background(), busy, time.Second, remoteExecForeground)
		if err == nil {
			rel()
		}
		got <- err
	}()
	releases[0]()
	select {
	case err := <-got:
		if err != nil {
			t.Fatalf("queued exec after a release: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("queued exec was not granted the released slot")
	}
	for _, r := range releases[1:] {
		r()
	}
}

// Waiting for a slot counts toward runBounded's timeout, and the command is
// never started when no slot frees up in time.
func TestRunBoundedSlotWaitCountsTowardTimeout(t *testing.T) {
	target := remote.Target{Kind: remote.KindSSH, Host: "slots-bounded.example"}
	var releases []func()
	for i := 0; i < remoteExecSlotsPerHost; i++ {
		rel, err := acquireRemoteExecSlot(context.Background(), target, time.Second, remoteExecForeground)
		if err != nil {
			t.Fatalf("slot %d: %v", i, err)
		}
		releases = append(releases, rel)
	}
	defer func() {
		for _, r := range releases {
			r()
		}
	}()

	cmd := exec.Command("true")
	start := time.Now()
	err := runBounded(context.Background(), target, cmd, 100*time.Millisecond, remoteExecForeground)
	if err == nil || !strings.Contains(err.Error(), "remote command timed out after") {
		t.Fatalf("err = %v, want timeout while waiting for a slot", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("runBounded took %s, want ~100ms", elapsed)
	}
	if cmd.Process != nil {
		t.Fatal("command started without a slot")
	}
}

// The pool must key on the WHOLE target identity, not the port-free
// Target.String(). Two projects on the same host reached over different SSH
// ports are different connections with different sshd MaxSessions budgets, so
// they must not share one pool — the current key (user@host) conflates them.
func TestRemoteExecSlotsSeparatePorts(t *testing.T) {
	p22 := remote.Target{Kind: remote.KindSSH, Host: "slots-port.example", Port: 22}
	p2222 := remote.Target{Kind: remote.KindSSH, Host: "slots-port.example", Port: 2222}

	var releases []func()
	for i := 0; i < remoteExecSlotsPerHost; i++ {
		rel, err := acquireRemoteExecSlot(context.Background(), p22, time.Second, remoteExecForeground)
		if err != nil {
			t.Fatalf("slot %d: %v", i, err)
		}
		releases = append(releases, rel)
	}
	defer func() {
		for _, r := range releases {
			r()
		}
	}()

	// A different port on the same host is a different connection, so it must
	// still be grantable while the first port is saturated.
	rel, err := acquireRemoteExecSlot(context.Background(), p2222, time.Second, remoteExecForeground)
	if err != nil {
		t.Fatalf("a second port on the same host shared the saturated pool: %v", err)
	}
	rel()
}

// The user is part of the connection too: u1@host and u2@host are two accounts,
// each with its own sshd budget.
func TestRemoteExecSlotsSeparateUsers(t *testing.T) {
	u1 := remote.Target{Kind: remote.KindSSH, User: "u1", Host: "slots-user.example"}
	u2 := remote.Target{Kind: remote.KindSSH, User: "u2", Host: "slots-user.example"}

	var releases []func()
	for i := 0; i < remoteExecSlotsPerHost; i++ {
		rel, err := acquireRemoteExecSlot(context.Background(), u1, time.Second, remoteExecForeground)
		if err != nil {
			t.Fatalf("slot %d: %v", i, err)
		}
		releases = append(releases, rel)
	}
	defer func() {
		for _, r := range releases {
			r()
		}
	}()

	rel, err := acquireRemoteExecSlot(context.Background(), u2, time.Second, remoteExecForeground)
	if err != nil {
		t.Fatalf("a second user on the same host shared the saturated pool: %v", err)
	}
	rel()
}

// Long-running interactive shell commands must not be able to consume every
// slot: a saturated pool makes the 10s git_status poll time out, so the whole
// git panel for that host goes stale. A share of the cap is reserved for
// short-lived foreground work (git status, diff, file reads), which is what
// must never be starved.
func TestRemoteExecSlotsReserveForForeground(t *testing.T) {
	target := remote.Target{Kind: remote.KindSSH, Host: "slots-reserve.example"}

	// Saturate with LONG-running work, as a pile of remote shell commands
	// would. Each acquisition beyond the reserved share must be refused, which
	// is what keeps slots free for git status.
	var releases []func()
	granted := 0
	for i := 0; i < remoteExecSlotsPerHost+4; i++ {
		rel, err := acquireRemoteExecSlot(context.Background(), target, time.Second, remoteExecLongRunning)
		if err != nil {
			break
		}
		releases = append(releases, rel)
		granted++
	}
	defer func() {
		for _, r := range releases {
			r()
		}
	}()

	if granted >= remoteExecSlotsPerHost {
		t.Fatalf("long-running commands took all %d slots (granted %d); the foreground reserve is not enforced",
			remoteExecSlotsPerHost, granted)
	}
	if granted < 1 {
		t.Fatal("long-running commands were refused entirely; the reserve must leave them capacity")
	}

	// A foreground probe (git status) must still get through while the
	// long-running commands hold everything they are allowed.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	rel, err := acquireRemoteExecSlot(ctx, target, 200*time.Millisecond, remoteExecForeground)
	if err != nil {
		t.Fatalf("foreground git status was starved by long-running commands: %v", err)
	}
	rel()
}
