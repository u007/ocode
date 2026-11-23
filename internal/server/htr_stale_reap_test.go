package server

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/projects"
	"github.com/u007/ocode/internal/remote"
)

func busyError() error {
	return &remote.PortBusyError{Err: errors.New("remote port forwarding failed for listen port 3845")}
}

// countingReaper installs a reaper that counts calls and answers with res.
func countingReaper(t *testing.T, res *remote.StaleForward, err error) *int {
	t.Helper()
	calls := 0
	orig := reapStaleReverseForward
	reapStaleReverseForward = func(context.Context, remote.BoundedTransport, int) (*remote.StaleForward, error) {
		calls++
		return res, err
	}
	t.Cleanup(func() { reapStaleReverseForward = orig })
	return &calls
}

func stubNoHolder(t *testing.T) {
	t.Helper()
	orig := findRemotePortHolder
	findRemotePortHolder = func(context.Context, remote.BoundedTransport, int) (*remote.PortHolder, error) {
		return nil, remote.ErrNoStaleForward
	}
	t.Cleanup(func() { findRemotePortHolder = orig })
}

// The request that motivated the reaper: the port is held by an sshd reverse
// forward the holder probe cannot see. Reaping the dead session and retrying
// once maps the relay instead of failing.
func TestOpenHTRReverseRetriesAfterReapingStaleForward(t *testing.T) {
	h, _ := newTestPortMapsHandler(t, "user@devbox", "/srv/app")
	target, _ := remote.ParseTarget("user@devbox")
	ref := projects.ProjectRef{Host: target.String(), Path: "/srv/app"}
	stubNoHolder(t)
	reaps := countingReaper(t, &remote.StaleForward{Session: "63", Peer: "203.0.113.9", PIDs: []int{227283}}, nil)

	opens := 0
	installHTROpener(t, func(*remote.ForwardManager, []projects.PortMap) error {
		opens++
		if opens == 1 {
			return busyError()
		}
		return nil
	})

	if err := h.openHTRReverse(ref, target, 3845); err != nil {
		t.Fatalf("the retry after a reap must succeed, got %v", err)
	}
	if opens != 2 {
		t.Fatalf("want one retry (2 opens), got %d", opens)
	}
	if *reaps != 1 {
		t.Fatalf("want exactly one reap, got %d", *reaps)
	}
	if got := h.portMaps.htrHost(target).getHolder(); got != nil {
		t.Fatalf("a successful retry clears the holder, got %+v", got)
	}
}

// A visible squatter keeps the existing behaviour: offer to kill it, never reap
// an ssh session.
func TestOpenHTRReverseDoesNotReapWhenTheHolderIsVisible(t *testing.T) {
	h, _ := newTestPortMapsHandler(t, "user@devbox", "/srv/app")
	target, _ := remote.ParseTarget("user@devbox")
	ref := projects.ProjectRef{Host: target.String(), Path: "/srv/app"}
	orig := findRemotePortHolder
	findRemotePortHolder = func(context.Context, remote.BoundedTransport, int) (*remote.PortHolder, error) {
		return &remote.PortHolder{PID: 4242, Command: "htrcli serve"}, nil
	}
	t.Cleanup(func() { findRemotePortHolder = orig })
	reaps := countingReaper(t, nil, errors.New("must not be called"))

	installHTROpener(t, func(*remote.ForwardManager, []projects.PortMap) error { return busyError() })
	if err := h.openHTRReverse(ref, target, 3845); err == nil {
		t.Fatal("want the busy-port error")
	}
	if *reaps != 0 {
		t.Fatalf("a visible holder must not trigger a reap, got %d", *reaps)
	}
}

// A second busy answer is reported unchanged: the retry is not a loop.
func TestOpenHTRReverseRetriesOnlyOnce(t *testing.T) {
	h, _ := newTestPortMapsHandler(t, "user@devbox", "/srv/app")
	target, _ := remote.ParseTarget("user@devbox")
	ref := projects.ProjectRef{Host: target.String(), Path: "/srv/app"}
	stubNoHolder(t)
	countingReaper(t, &remote.StaleForward{Session: "63", PIDs: []int{227283}}, nil)

	opens := 0
	installHTROpener(t, func(*remote.ForwardManager, []projects.PortMap) error {
		opens++
		return busyError()
	})
	err := h.openHTRReverse(ref, target, 3845)
	if err == nil || !strings.Contains(err.Error(), "remote port forwarding failed") {
		t.Fatalf("want the busy-port error, got %v", err)
	}
	if opens != 2 {
		t.Fatalf("want exactly one retry, got %d opens", opens)
	}
}

// Nothing provably stale means no signal and no retry.
func TestOpenHTRReverseDoesNotRetryWhenNotStale(t *testing.T) {
	h, _ := newTestPortMapsHandler(t, "user@devbox", "/srv/app")
	target, _ := remote.ParseTarget("user@devbox")
	ref := projects.ProjectRef{Host: target.String(), Path: "/srv/app"}
	stubNoHolder(t)
	reaps := countingReaper(t, nil, remote.ErrNoStaleForward)

	opens := 0
	installHTROpener(t, func(*remote.ForwardManager, []projects.PortMap) error {
		opens++
		return busyError()
	})
	if err := h.openHTRReverse(ref, target, 3845); err == nil {
		t.Fatal("want the busy-port error")
	}
	if opens != 1 {
		t.Fatalf("no reap means no retry, got %d opens", opens)
	}
	if *reaps != 1 {
		t.Fatalf("want the stale check to run once, got %d", *reaps)
	}
}
