package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/projects"
	"github.com/u007/ocode/internal/remote"
)

// installHTROpener swaps the reverse-forward opener for the test.
func installHTROpener(t *testing.T, fn htrReverseForwardFn) {
	t.Helper()
	orig := htrReverseForwardOpener()
	startHTRReverseForward.Store(&fn)
	t.Cleanup(func() { startHTRReverseForward.Store(&orig) })
}

func TestOpenHTRReverseRecordsHolderOnBusyPort(t *testing.T) {
	h, _ := newTestPortMapsHandler(t, "user@devbox", "/srv/app")
	target, _ := remote.ParseTarget("user@devbox")
	ref := projects.ProjectRef{Host: target.String(), Path: "/srv/app"}

	installHTROpener(t, func(*remote.ForwardManager, []projects.PortMap) error {
		return &remote.PortBusyError{Err: errors.New("remote port forwarding failed")}
	})
	origFind := findRemotePortHolder
	findRemotePortHolder = func(context.Context, remote.BoundedTransport, int) (*remote.PortHolder, error) {
		return &remote.PortHolder{PID: 4242, Command: "htrcli serve"}, nil
	}
	t.Cleanup(func() { findRemotePortHolder = origFind })

	if err := h.openHTRReverse(ref, target, 3845); err == nil {
		t.Fatal("want the busy-port error")
	}
	hh := h.portMaps.htrHost(target)
	if got := hh.getHolder(); got == nil || got.PID != 4242 {
		t.Fatalf("holder not recorded: %+v", got)
	}

	// A later successful open clears it.
	installHTROpener(t, func(*remote.ForwardManager, []projects.PortMap) error { return nil })
	if err := h.openHTRReverse(ref, target, 3845); err != nil {
		t.Fatal(err)
	}
	if hh.getHolder() != nil {
		t.Fatal("holder must be cleared after a successful open")
	}
}

func TestOpenHTRReverseIgnoresNonBusyFailure(t *testing.T) {
	h, _ := newTestPortMapsHandler(t, "user@devbox", "/srv/app")
	target, _ := remote.ParseTarget("user@devbox")
	ref := projects.ProjectRef{Host: target.String(), Path: "/srv/app"}
	installHTROpener(t, func(*remote.ForwardManager, []projects.PortMap) error { return errors.New("host unreachable") })
	origFind := findRemotePortHolder
	findRemotePortHolder = func(context.Context, remote.BoundedTransport, int) (*remote.PortHolder, error) {
		t.Fatal("must not probe the remote for a failure that is not a busy port")
		return nil, nil
	}
	t.Cleanup(func() { findRemotePortHolder = origFind })
	_ = h.openHTRReverse(ref, target, 3845)
}

func TestReclaimRefusesPidThatIsNotTheReportedHolder(t *testing.T) {
	h, mux := newTestPortMapsHandler(t, "user@devbox", "/srv/app")
	mux.HandleFunc("POST /api/portmaps/htr/reclaim", h.HandleHTRReclaim)
	target, _ := remote.ParseTarget("user@devbox")
	h.portMaps.htrHost(target).setHolder(&remote.PortHolder{PID: 4242})

	origKill := killRemotePortHolder
	killRemotePortHolder = func(context.Context, remote.BoundedTransport, int, int) error {
		t.Fatal("kill must not run for an unreported pid")
		return nil
	}
	t.Cleanup(func() { killRemotePortHolder = origKill })

	rec := doPortMaps(t, mux, http.MethodPost,
		"/api/portmaps/htr/reclaim?host=user@devbox&project=/srv/app", []byte(`{"pid":999}`))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "changed") {
		t.Fatalf("want 409 holder-changed, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestReclaimLocalForwardRefusesUnreportedPid(t *testing.T) {
	h, mux := newTestPortMapsHandler(t, "user@devbox", "/srv/app")
	mux.HandleFunc("POST /api/portmaps/{port}/reclaim", h.HandleReclaimPortMap)
	target, _ := remote.ParseTarget("user@devbox")
	ref := projects.ProjectRef{Host: target.String(), Path: "/srv/app"}
	if err := h.projects.AddPortMap(ref, 80, 9333); err != nil {
		t.Fatal(err)
	}
	orig := killLocalPortHolder
	killLocalPortHolder = func(context.Context, int, int) error {
		t.Fatal("kill must not run without a reported holder")
		return nil
	}
	t.Cleanup(func() { killLocalPortHolder = orig })

	rec := doPortMaps(t, mux, http.MethodPost,
		"/api/portmaps/80/reclaim?host=user@devbox&project=/srv/app", []byte(`{"pid":55}`))
	if rec.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d %s", rec.Code, rec.Body.String())
	}
}

// A hung holder probe must not hold the open request: the probe is cut at its
// deadline, the forward error still reaches the caller, and no holder is recorded.
func TestOpenHTRReverseProbeTimeoutRecordsNoHolder(t *testing.T) {
	h, _ := newTestPortMapsHandler(t, "user@devbox", "/srv/app")
	target, _ := remote.ParseTarget("user@devbox")
	ref := projects.ProjectRef{Host: target.String(), Path: "/srv/app"}

	installHTROpener(t, func(*remote.ForwardManager, []projects.PortMap) error {
		return &remote.PortBusyError{Err: errors.New("remote port forwarding failed")}
	})
	origTimeout := htrHolderProbeTimeout
	htrHolderProbeTimeout = 20 * time.Millisecond
	t.Cleanup(func() { htrHolderProbeTimeout = origTimeout })
	origFind := findRemotePortHolder
	findRemotePortHolder = func(ctx context.Context, _ remote.BoundedTransport, _ int) (*remote.PortHolder, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	t.Cleanup(func() { findRemotePortHolder = origFind })

	start := time.Now()
	err := h.openHTRReverse(ref, target, 3845)
	if err == nil || !strings.Contains(err.Error(), "remote port forwarding failed") {
		t.Fatalf("want the busy-port error to reach the caller, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("probe not bounded by its deadline, took %v", elapsed)
	}
	if got := h.portMaps.htrHost(target).getHolder(); got != nil {
		t.Fatalf("timed-out probe must record no holder, got %+v", got)
	}
}

// A live forward makes a recorded squatter stale; a dead one keeps it.
func TestDropHolderIfLiveClearsOnlyWhenLive(t *testing.T) {
	hh := &htrHost{}
	hh.setHolder(&remote.PortHolder{PID: 4242})
	hh.dropHolderIfLive(false)
	if hh.getHolder() == nil {
		t.Fatal("holder must survive while the forward is not live")
	}
	hh.dropHolderIfLive(true)
	if hh.getHolder() != nil {
		t.Fatal("holder must be dropped once the forward is live")
	}
}
