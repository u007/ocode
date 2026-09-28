package server

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/u007/ocode/internal/remote"
)

// fakeClock is a manually advanced clock so the retry policy's timing is
// testable without real waits.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// newTestPortMapPolicy builds a policy over a counting stub, so a test can
// drive failures and the clock without a real ssh child (Start's readiness
// probe alone costs seconds).
func newTestPortMapPolicy(fail error) (*portMapPolicy, *fakeClock, *int) {
	clock := newFakeClock()
	attempts := 0
	p := newPortMapPolicy(
		func(remote.ProjectPortMap) error {
			attempts++
			return fail
		},
		func(int) error { return nil },
	)
	p.now = clock.now
	return p, clock, &attempts
}

func (p *portMapPolicy) failures(remotePort int) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state(remotePort).failures
}

var testPM = remote.ProjectPortMap{RemotePort: 3510, LocalPort: 3510, Enabled: true}

// TestPortMapPolicyRetriesAForwardThatDiedHealthy covers the plain case: a
// forward that ran for a long time and then died is re-opened at once, and its
// earlier failures are forgiven. The seeding flap matters: with a fresh policy
// the failure count is already zero, so the test could not tell a "cleared on
// healthy death" implementation from one that never clears at all.
func TestPortMapPolicyRetriesAForwardThatDiedHealthy(t *testing.T) {
	p, _, attempts := newTestPortMapPolicy(nil)
	// One earlier failure, which a long-lived forward must erase.
	p.noteExit(testPM.RemotePort, time.Second)
	if got := p.failures(testPM.RemotePort); got != 1 {
		t.Fatalf("setup: failures = %d after a flap, want 1", got)
	}
	p.noteExit(testPM.RemotePort, time.Hour)

	if got := p.failures(testPM.RemotePort); got != 0 {
		t.Fatalf("failures = %d after a forward ran for an hour, want 0 (a long-lived forward is forgiven)", got)
	}
	ok, err := p.tryStart(testPM)
	if err != nil {
		t.Fatalf("tryStart: %v", err)
	}
	if !ok {
		t.Fatal("tryStart = not attempted for a forward that died after running for an hour")
	}
	if *attempts != 1 {
		t.Fatalf("start attempts = %d, want 1", *attempts)
	}
}

// TestPortMapPolicyForgivesAfterAHealthyForward covers the same reset by the
// other route: a forward that is observed live on a pass must not still be
// carrying failures from before it came back.
func TestPortMapPolicyForgivesAfterAHealthyForward(t *testing.T) {
	p, _, _ := newTestPortMapPolicy(nil)
	p.noteExit(testPM.RemotePort, time.Second)
	p.noteExit(testPM.RemotePort, time.Second)
	if got := p.failures(testPM.RemotePort); got != 2 {
		t.Fatalf("setup: failures = %d, want 2", got)
	}
	p.noteHealthy(testPM.RemotePort)
	if got := p.failures(testPM.RemotePort); got != 0 {
		t.Fatalf("failures = %d for a forward seen live, want 0", got)
	}
}

// TestPortMapPolicyBacksOffAfterAFlappingChild is the anti-hot-loop guard. A
// child that dies inside the settle window never counts as a recovery, so a
// forward that opens and dies immediately cannot be re-opened on every tick.
func TestPortMapPolicyBacksOffAfterAFlappingChild(t *testing.T) {
	p, clock, attempts := newTestPortMapPolicy(nil)
	p.noteExit(testPM.RemotePort, time.Second)

	if got := p.failures(testPM.RemotePort); got != 1 {
		t.Fatalf("failures = %d after a flap, want 1", got)
	}
	if ok, _ := p.tryStart(testPM); ok {
		t.Fatal("tryStart re-opened a forward inside its backoff window")
	}
	if *attempts != 0 {
		t.Fatalf("start attempts = %d during backoff, want 0", *attempts)
	}
	clock.advance(portMapBaseBackoff)
	if ok, err := p.tryStart(testPM); !ok || err != nil {
		t.Fatalf("tryStart after the backoff window: attempted=%v err=%v", ok, err)
	}
	if *attempts != 1 {
		t.Fatalf("start attempts = %d after the backoff window, want 1", *attempts)
	}
}

// TestPortMapPolicyBacksOffAfterAFailedOpen covers an unreachable host, where
// Start itself fails rather than the child dying.
func TestPortMapPolicyBacksOffAfterAFailedOpen(t *testing.T) {
	p, clock, attempts := newTestPortMapPolicy(errors.New("ssh: connect to host: connection refused"))

	if ok, err := p.tryStart(testPM); !ok || err == nil {
		t.Fatalf("first tryStart: attempted=%v err=%v, want attempted with an error", ok, err)
	}
	if got := p.failures(testPM.RemotePort); got != 1 {
		t.Fatalf("failures = %d after a failed open, want 1", got)
	}
	if ok, _ := p.tryStart(testPM); ok {
		t.Fatal("tryStart retried immediately after a failed open")
	}
	clock.advance(portMapBaseBackoff * 4)
	if ok, err := p.tryStart(testPM); !ok || err == nil {
		t.Fatalf("tryStart after backoff: attempted=%v err=%v", ok, err)
	}
	if *attempts != 2 {
		t.Fatalf("start attempts = %d, want 2", *attempts)
	}
}

// TestPortMapPolicyGivesUpAfterRepeatedFailures pins the requested policy: a
// host that is genuinely down is tried a bounded number of times, then left
// down (the panel shows the row as not-live) rather than retried forever.
func TestPortMapPolicyGivesUpAfterRepeatedFailures(t *testing.T) {
	p, clock, attempts := newTestPortMapPolicy(errors.New("ssh: connect to host: no route to host"))

	for i := 0; i < portMapMaxFailures; i++ {
		ok, err := p.tryStart(testPM)
		if !ok || err == nil {
			t.Fatalf("attempt %d: attempted=%v err=%v, want an attempted failure", i+1, ok, err)
		}
		clock.advance(portMapMaxBackoff)
	}
	if got := *attempts; got != portMapMaxFailures {
		t.Fatalf("start attempts = %d, want %d", got, portMapMaxFailures)
	}
	if !p.givenUp(testPM.RemotePort) {
		t.Fatal("policy did not give up after the failure limit")
	}
	// A long wait must not resurrect it: the forward stays down for the user
	// to act on.
	clock.advance(24 * time.Hour)
	if ok, _ := p.tryStart(testPM); ok {
		t.Fatal("tryStart kept attempting after the policy gave up")
	}
	if *attempts != portMapMaxFailures {
		t.Fatalf("start attempts = %d after giving up, want %d", *attempts, portMapMaxFailures)
	}
}

// TestPortMapPolicyGivesUpOnARepeatedlyFlappingForward covers the case the two
// tests above miss: Start *succeeds* every time and the replacement child dies
// right away. Clearing the failure count on a successful open would make every
// one of those look like a recovery, so the forward would be re-opened forever
// and the give-up state would never be reached.
func TestPortMapPolicyGivesUpOnARepeatedlyFlappingForward(t *testing.T) {
	p, clock, attempts := newTestPortMapPolicy(nil)
	for i := 0; i < portMapMaxFailures; i++ {
		ok, err := p.tryStart(testPM)
		if !ok || err != nil {
			t.Fatalf("attempt %d: attempted=%v err=%v, want an attempted success", i+1, ok, err)
		}
		// The child we just opened dies inside the settle window.
		p.noteExit(testPM.RemotePort, time.Second)
		clock.advance(portMapMaxBackoff)
	}
	if !p.givenUp(testPM.RemotePort) {
		t.Fatalf("a forward that flapped %d times did not reach the give-up state (failures=%d)",
			portMapMaxFailures, p.failures(testPM.RemotePort))
	}
	clock.advance(24 * time.Hour)
	if ok, _ := p.tryStart(testPM); ok {
		t.Fatal("a flapping forward was still being re-opened after giving up")
	}
	if *attempts != portMapMaxFailures {
		t.Fatalf("start attempts = %d, want %d", *attempts, portMapMaxFailures)
	}
}

func TestPortMapPolicyForgetsDisabledForwards(t *testing.T) {
	p, _, _ := newTestPortMapPolicy(nil)
	p.noteExit(testPM.RemotePort, time.Second)
	if got := p.failures(testPM.RemotePort); got != 1 {
		t.Fatalf("failures = %d, want 1", got)
	}
	p.forget(testPM.RemotePort)
	if got := p.failures(testPM.RemotePort); got != 0 {
		t.Fatalf("failures = %d after forget, want 0", got)
	}
}

func TestPortMapPolicyHealthyForwardClearsFailures(t *testing.T) {
	p, _, _ := newTestPortMapPolicy(nil)
	p.noteExit(testPM.RemotePort, time.Second)
	p.noteHealthy(testPM.RemotePort)
	if got := p.failures(testPM.RemotePort); got != 0 {
		t.Fatalf("failures = %d for a live forward, want 0", got)
	}
	if p.givenUp(testPM.RemotePort) {
		t.Fatal("a live forward stayed marked given-up")
	}
}

// installWatchdogFakeSSH puts a fake `ssh` on PATH that exits on its own after
// lifetime, the shape of a real forward dying without anyone calling Stop.
func installWatchdogFakeSSH(t *testing.T, lifetime time.Duration) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "ssh")
	script := fmt.Sprintf("#!/bin/sh\nsleep %.3f\nexit 3\n", lifetime.Seconds())
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// watchdogListen opens a throwaway listener so Start's readiness probe
// succeeds (it only dials the local port) and returns that port.
func watchdogListen(t *testing.T) (net.Listener, int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse listener port %q: %v", portStr, err)
	}
	return ln, port
}

// watchdogTestEntry registers a remote project and returns its registry entry.
func watchdogTestEntry(t *testing.T, h *Handler, path string) *portMapEntry {
	t.Helper()
	target, err := remote.ParseTarget("example.invalid")
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}
	if err := h.projects.AddRemote(target.String(), path); err != nil {
		t.Fatalf("AddRemote: %v", err)
	}
	return h.portMaps.entry(target, path)
}

// TestPortMapEntryWakesMonitorWhenForwardDies is the end-to-end wiring test:
// a real ForwardManager child that exits must (a) stop being reported live and
// (b) nudge the monitor. Without SetOnExit wiring the watchdog would only
// notice on its slow safety-net tick, and without the reaper IsLive would stay
// true forever.
func TestPortMapEntryWakesMonitorWhenForwardDies(t *testing.T) {
	installWatchdogFakeSSH(t, 300*time.Millisecond)
	h, _ := newTestPortMapsHandler(t, "example.invalid", "/srv/app")
	entry := watchdogTestEntry(t, h, "/srv/app")
	_, localPort := watchdogListen(t)

	pm := remote.ProjectPortMap{RemotePort: 3510, LocalPort: localPort, Enabled: true}
	if err := entry.fm.Start(pm); err != nil {
		t.Fatalf("Start: %v", err)
	}

	select {
	case <-h.portMaps.wake:
	case <-time.After(15 * time.Second):
		t.Fatal("no monitor wake-up after the forward's child died")
	}
	if entry.fm.IsLive(pm.RemotePort) {
		t.Fatal("IsLive = true after the forward's child died")
	}
}

// TestPortMapWatchdogPassRestartsOnlyEnabledDeadForwards pins the pass logic:
// a persisted+enabled forward with no live child is re-opened, a disabled one
// is not touched, and a live one is not restarted.
func TestPortMapWatchdogPassRestartsOnlyEnabledDeadForwards(t *testing.T) {
	h, _ := newTestPortMapsHandler(t, "example.invalid", "/srv/app")
	entry := watchdogTestEntry(t, h, "/srv/app")

	ref := entry.ref
	if err := h.projects.AddPortMap(ref, 3510, 3510); err != nil {
		t.Fatalf("AddPortMap 3510: %v", err)
	}
	if err := h.projects.AddPortMap(ref, 4000, 4000); err != nil {
		t.Fatalf("AddPortMap 4000: %v", err)
	}
	if err := h.projects.SetPortMapEnabled(ref, 4000, false); err != nil {
		t.Fatalf("disable 4000: %v", err)
	}

	attempted := map[int]int{}
	entry.policy.start = func(pm remote.ProjectPortMap) error {
		attempted[pm.RemotePort]++
		return nil
	}

	h.portMapWatchdogPass()

	if got := attempted[3510]; got != 1 {
		t.Fatalf("enabled dead forward 3510 was attempted %d times, want 1", got)
	}
	if got := attempted[4000]; got != 0 {
		t.Fatalf("disabled forward 4000 was attempted %d times, want 0", got)
	}
}

// TestPortMapWatchdogPassSkipsLiveForwards guards against a pass that
// restarts a working forward every tick.
func TestPortMapWatchdogPassSkipsLiveForwards(t *testing.T) {
	installWatchdogFakeSSH(t, 30*time.Second)
	h, _ := newTestPortMapsHandler(t, "example.invalid", "/srv/app")
	entry := watchdogTestEntry(t, h, "/srv/app")
	_, localPort := watchdogListen(t)

	ref := entry.ref
	if err := h.projects.AddPortMap(ref, 3510, localPort); err != nil {
		t.Fatalf("AddPortMap: %v", err)
	}
	if err := entry.fm.Start(remote.ProjectPortMap{RemotePort: 3510, LocalPort: localPort, Enabled: true}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	attempts := 0
	entry.policy.start = func(remote.ProjectPortMap) error {
		attempts++
		return nil
	}

	h.portMapWatchdogPass()
	if attempts != 0 {
		t.Fatalf("a live forward was restarted %d times, want 0", attempts)
	}
}

// TestPortMapEnableResetsGiveUp covers the escape hatch: once the monitor has
// given up, the user's own Enable must clear the state so the forward is tried
// again immediately. The policy is driven to its give-up state on a fake clock
// first, so the setup costs no real waits; the Enable call itself goes through
// the real handler against a real (fake-ssh) forward.
func TestPortMapEnableResetsGiveUp(t *testing.T) {
	installWatchdogFakeSSH(t, 30*time.Second)
	h, mux := newTestPortMapsHandler(t, "example.invalid", "/srv/app")
	entry := watchdogTestEntry(t, h, "/srv/app")
	_, localPort := watchdogListen(t)
	t.Cleanup(func() { _ = entry.fm.Stop(3510) })

	ref := entry.ref
	if err := h.projects.AddPortMap(ref, 3510, localPort); err != nil {
		t.Fatalf("AddPortMap: %v", err)
	}

	clock := newFakeClock()
	policy := newPortMapPolicy(
		func(remote.ProjectPortMap) error { return nil },
		func(int) error { return nil },
	)
	policy.now = clock.now
	pm := remote.ProjectPortMap{RemotePort: 3510, LocalPort: localPort, Enabled: true}
	for i := 0; i < portMapMaxFailures; i++ {
		if ok, err := policy.tryStart(pm); !ok || err != nil {
			t.Fatalf("setup attempt %d: attempted=%v err=%v", i+1, ok, err)
		}
		// Every open is followed by a child that dies inside the settle window.
		policy.noteExit(3510, time.Second)
		clock.advance(portMapMaxBackoff)
	}
	if !policy.givenUp(3510) {
		t.Fatalf("test setup never reached the give-up state (failures=%d)", policy.failures(3510))
	}
	attempts := 0
	policy.start = func(remote.ProjectPortMap) error {
		attempts++
		return nil
	}
	entry.policy = policy

	rec := doPortMaps(t, mux, "POST", "/api/portmaps/3510/enable?host=example.invalid&project=/srv/app", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("enable status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if entry.policy.givenUp(3510) {
		t.Fatal("Enable did not clear the give-up state")
	}
	if ok, _ := entry.policy.tryStart(pm); !ok {
		t.Fatal("the forward was not retryable after Enable")
	}
	if attempts != 1 {
		t.Fatalf("start attempts after Enable = %d, want 1", attempts)
	}
}

// TestPortMapPolicySuppressedForwardIsNeverReopened pins the removal/disable
// race fix: once a port is suppressed, tryStart refuses it even after forget
// drops the retry state, because a watchdog pass holding a stale "enabled"
// snapshot can still call tryStart. Only the user's own re-add/enable (reset)
// clears the suppression.
func TestPortMapPolicySuppressedForwardIsNeverReopened(t *testing.T) {
	p, _, attempts := newTestPortMapPolicy(nil)
	p.suppress(testPM.RemotePort)

	if ok, _ := p.tryStart(testPM); ok {
		t.Fatal("tryStart opened a suppressed forward")
	}
	if *attempts != 0 {
		t.Fatalf("start attempts = %d for a suppressed forward, want 0", *attempts)
	}
	// forget drops retry bookkeeping but must NOT clear the tombstone.
	p.forget(testPM.RemotePort)
	if ok, _ := p.tryStart(testPM); ok {
		t.Fatal("tryStart re-opened a suppressed forward after forget")
	}
	if *attempts != 0 {
		t.Fatalf("start attempts = %d after forget, want 0", *attempts)
	}
	// The user's deliberate re-add/enable clears it and retries immediately.
	p.reset(testPM.RemotePort)
	if ok, err := p.tryStart(testPM); !ok || err != nil {
		t.Fatalf("tryStart after reset: attempted=%v err=%v", ok, err)
	}
	if *attempts != 1 {
		t.Fatalf("start attempts = %d after reset, want 1", *attempts)
	}
}

// TestPortMapPolicyClosesAForwardThatARemovalRacedIntoStarting is the second
// half: suppression can land WHILE the (slow) open is in flight. The policy
// must notice after the open and stop the child it just created, so a removal
// cannot leave an orphan behind.
func TestPortMapPolicyClosesAForwardThatARemovalRacedIntoStarting(t *testing.T) {
	clock := newFakeClock()
	var p *portMapPolicy
	stopped := 0
	p = newPortMapPolicy(
		func(remote.ProjectPortMap) error {
			// The user removes the port mid-open.
			p.suppress(testPM.RemotePort)
			return nil
		},
		func(int) error { stopped++; return nil },
	)
	p.now = clock.now

	ok, err := p.tryStart(testPM)
	if !ok || err != nil {
		t.Fatalf("tryStart: attempted=%v err=%v", ok, err)
	}
	if stopped != 1 {
		t.Fatalf("stop calls = %d, want 1: a forward opened during a teardown must be closed", stopped)
	}
}
