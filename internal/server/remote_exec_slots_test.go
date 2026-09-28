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
		rel, err := acquireRemoteExecSlot(context.Background(), busy, time.Second)
		if err != nil {
			t.Fatalf("slot %d: %v", i, err)
		}
		releases = append(releases, rel)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := acquireRemoteExecSlot(ctx, busy, 50*time.Millisecond); err == nil {
		t.Fatal("slot past the cap was granted")
	} else if !isRemoteTransportError(err) || !strings.Contains(err.Error(), "waiting for a free") {
		t.Fatalf("over-cap error = %v, want a transport timeout naming the slot wait", err)
	}

	rel, err := acquireRemoteExecSlot(context.Background(), other, time.Second)
	if err != nil {
		t.Fatalf("another host must not share the cap: %v", err)
	}
	rel()

	got := make(chan error, 1)
	go func() {
		rel, err := acquireRemoteExecSlot(context.Background(), busy, time.Second)
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
		rel, err := acquireRemoteExecSlot(context.Background(), target, time.Second)
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
	err := runBounded(context.Background(), target, cmd, 100*time.Millisecond)
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
