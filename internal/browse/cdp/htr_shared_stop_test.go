package cdp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/filelock"
)

// Fixed coordinates for the stop-rule fixtures. The port is never bound: the
// predicate reads the coordinates out of the owner marker, so what matters is
// that marker, lease and predicate all agree on the same triple.
const (
	stopRuleIdentity = "tok"
	stopRulePort     = 3845
	stopRuleSocket   = "/tmp/htr-stop-rule.sock"
	stopRuleExe      = "/bin/htrcli"
)

// impossiblePID is far above any real pid_max, so the real pidAlive probe
// answers false for it. Used to prove a seam is consulted rather than the
// function behind it.
const impossiblePID = 1 << 30

// stopRuleOwner builds the record the stop rule is exercised against. daemonPID
// is the recorded daemon and startedBy is the process that spawned it. They are
// separate parameters on purpose: the whole point of StartedByPID is that the
// two answers are allowed to differ.
func stopRuleOwner(daemonPID, startedBy int) htrOwner {
	return htrOwner{
		Identity: stopRuleIdentity,
		PID:      daemonPID,
		// OwnerPID here is only what the fixture intends; htrWriteOwner
		// overwrites it with this process, because writing the marker is what
		// it means. Set it so the struct reads as the record it describes.
		OwnerPID:     startedBy,
		StartedByPID: startedBy,
		Port:         stopRulePort,
		Socket:       stopRuleSocket,
		Executable:   stopRuleExe,
	}
}

// writeOwnerForTest persists o as ocode's owner marker and then READS IT BACK,
// returning the decoded record. Going through the file is what pins the JSON
// tag: a StartedByPID that was never serialised decodes to the zero value, and
// a test that asserted on the in-memory struct would pass with the field
// dropped from the wire format entirely.
func writeOwnerForTest(t *testing.T, o htrOwner) htrOwner {
	t.Helper()
	if err := htrWriteOwner(o.Identity, o.Port, o.PID, o.Socket, o.Executable, time.Now(), o.StartedByPID); err != nil {
		t.Fatal(err)
	}
	got, err := readHTROwner()
	if err != nil {
		t.Fatalf("owner marker did not round-trip: %v", err)
	}
	if got.StartedByPID != o.StartedByPID {
		t.Fatalf("started_by_pid = %d on disk, want %d — the marker is not carrying spawn provenance", got.StartedByPID, o.StartedByPID)
	}
	return got
}

// readOwnerForTest persists a marker for a daemon spawned by startedBy and
// returns the decoded record. The daemon PID is this test process so the
// liveness probe has something real to find; see stubStopRuleLiveness.
func readOwnerForTest(t *testing.T, startedBy int) htrOwner {
	t.Helper()
	return writeOwnerForTest(t, stopRuleOwner(os.Getpid(), startedBy))
}

// livenessRecorder collects what the stop rule asked the two liveness probes.
type livenessRecorder struct {
	alive   []int
	matched []int
}

// stubStopRuleLiveness makes the predicate see a live, attributable daemon and
// returns the recorder of what it was asked. The recorded daemon is the test
// process itself, so these seams decide the branch under test rather than
// reporting a fact about the test runner. The recorder is also usable by
// callers that only need the stubbing.
func stubStopRuleLiveness(t *testing.T) *livenessRecorder {
	t.Helper()
	rec := &livenessRecorder{}
	origAlive, origMatch := pidAliveFn, processMatchesFn
	pidAliveFn = func(pid int) bool { rec.alive = append(rec.alive, pid); return pid > 0 }
	processMatchesFn = func(o htrOwner) bool { rec.matched = append(rec.matched, o.PID); return true }
	t.Cleanup(func() { pidAliveFn, processMatchesFn = origAlive, origMatch })
	return rec
}

// stubAlwaysLive makes every recorded pid look alive and attributable, so the
// recorded-pid guard is the only thing left that can refuse. Used where a
// "dead" answer from the liveness probe would otherwise produce the same
// refusal for the wrong reason.
func stubAlwaysLive(t *testing.T) {
	t.Helper()
	origAlive, origMatch := pidAliveFn, processMatchesFn
	pidAliveFn = func(int) bool { return true }
	processMatchesFn = func(htrOwner) bool { return true }
	t.Cleanup(func() { pidAliveFn, processMatchesFn = origAlive, origMatch })
}

// startIdleHelperProcess re-executes this test binary into a test that only
// sleeps, and returns the live child. A genuinely running process is required
// wherever the rule asks "is it alive" or "does this lease belong to somebody
// else": a fabricated pid answers "not alive" from the real probe, which is the
// correct production answer, so such a test would measure the stub instead of
// the rule. No cleanup is registered — callers own the child's lifetime,
// because the stop tests have to Wait on it to prove it survived.
func startIdleHelperProcess(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHTROwnerIdleHelper$")
	cmd.Env = append(os.Environ(), "OCODE_HTR_IDLE_HELPER=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	return cmd
}

// TestHTROwnerIdleHelper is not a test. It is the body of the re-executed child
// process started by startIdleHelperProcess, whose only job is to exist.
func TestHTROwnerIdleHelper(t *testing.T) {
	if os.Getenv("OCODE_HTR_IDLE_HELPER") != "1" {
		t.Skip("helper process; only runs when re-executed by startIdleHelperProcess")
	}
	time.Sleep(2 * time.Minute)
}

// startForeignProcess returns the pid of a live process that is not this one.
func startForeignProcess(t *testing.T) int {
	t.Helper()
	cmd := startIdleHelperProcess(t)
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd.Process.Pid
}

// acquireForeignLease writes a lease held by a live process other than this one:
// "a second ocode instance is still using this daemon". It goes through
// htrLeaseDir/htrLeaseLockPath/atomicWriteFile and reproduces acquireHTRLease's
// "lease-<pid>-<nano>.json" name, because activeHTRLeases skips any entry that
// does not carry that prefix — a differently-named fixture would be invisible to
// the scan and the test would pass without exercising anything.
func acquireForeignLease(t *testing.T, port int, socket, identity string) {
	t.Helper()
	other := startForeignProcess(t)
	data, err := json.Marshal(htrLease{PID: other, Port: port, Socket: socket, Identity: identity, Heartbeat: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	dir, err := htrLeaseDir()
	if err != nil {
		t.Fatal(err)
	}
	lockPath, err := htrLeaseLockPath()
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(dir, fmt.Sprintf("lease-%d-%d.json", other, time.Now().UnixNano()))
	if err := filelock.WithFileLock(lockPath, func() error {
		return atomicWriteFile(name, data, 0o600)
	}); err != nil {
		t.Fatal(err)
	}
}

func TestStopRuleStopsOwnDaemon(t *testing.T) {
	isolateHTROwnerState(t)
	stubStopRuleLiveness(t)
	stopped, err := shouldStopSharedDaemon(readOwnerForTest(t, os.Getpid()))
	if err != nil {
		t.Fatal(err)
	}
	if !stopped {
		t.Error("a daemon this process spawned must be stopped on exit — that is the designed behaviour")
	}
}

func TestStopRuleRefusesForeignDaemon(t *testing.T) {
	isolateHTROwnerState(t)
	stubStopRuleLiveness(t)
	owner := readOwnerForTest(t, os.Getpid()+1) // started by another process
	stopped, err := shouldStopSharedDaemon(owner)
	if err != nil {
		t.Fatal(err)
	}
	if stopped {
		t.Error("a daemon started by another ocode instance or an external run must never be stopped")
	}
}

func TestStopRuleDefersToLiveLease(t *testing.T) {
	isolateHTROwnerState(t)
	stubStopRuleLiveness(t)
	owner := readOwnerForTest(t, os.Getpid())
	// Simulate a second live ocode process holding a lease.
	acquireForeignLease(t, owner.Port, owner.Socket, owner.Identity)
	stopped, err := shouldStopSharedDaemon(owner)
	if err != nil {
		t.Fatal(err)
	}
	if stopped {
		t.Error("with another ocode instance holding a live lease, the daemon must survive this one exiting")
	}
}

func TestStopRuleUsesStartedByPIDNotOwnerPID(t *testing.T) {
	isolateHTROwnerState(t)
	stubStopRuleLiveness(t)
	// Adopting rewrites the marker, so OwnerPID becomes this process while the
	// daemon the marker records was spawned by somebody else.
	owner := writeOwnerForTest(t, htrOwner{
		Identity:     stopRuleIdentity,
		PID:          os.Getpid(),
		OwnerPID:     os.Getpid(),
		StartedByPID: os.Getpid() + 1,
		Port:         stopRulePort,
		Socket:       stopRuleSocket,
		Executable:   stopRuleExe,
	})
	if owner.OwnerPID != os.Getpid() {
		t.Fatalf("OwnerPID = %d, want this process — the fixture no longer models an adopting writer", owner.OwnerPID)
	}
	stopped, err := shouldStopSharedDaemon(owner)
	if err != nil {
		t.Fatal(err)
	}
	if stopped {
		t.Error("provenance must come from StartedByPID; adopting rewrites OwnerPID and must not grant stop rights")
	}
}

// TestStopRuleRefusesLegacyMarkerWithoutProvenance covers a marker written by an
// ocode release from before StartedByPID existed. "Unknown" is not "mine": with
// no provenance on disk, the conservative answer is to leave the daemon alone.
func TestStopRuleRefusesLegacyMarkerWithoutProvenance(t *testing.T) {
	isolateHTROwnerState(t)
	stubStopRuleLiveness(t)
	path, err := htrOwnerPath()
	if err != nil {
		t.Fatal(err)
	}
	legacy := stopRuleOwner(os.Getpid(), os.Getpid())
	legacy.StartedByPID = 0
	encoded, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("started_by_pid")) {
		t.Fatal("fixture is not legacy: started_by_pid is still on the wire")
	}
	if err := atomicWriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	owner, err := readHTROwner()
	if err != nil {
		t.Fatal(err)
	}
	if owner.StartedByPID != 0 {
		t.Fatalf("StartedByPID = %d, want 0 for a marker written before the field existed", owner.StartedByPID)
	}
	stopped, err := shouldStopSharedDaemon(owner)
	if err != nil {
		t.Fatal(err)
	}
	if stopped {
		t.Error("a marker with no spawn provenance must not be stopped; unknown is not the same as ours")
	}
}

func TestStopRuleRefusesUnrecordedDaemonPID(t *testing.T) {
	isolateHTROwnerState(t)
	// Always-live, not stubStopRuleLiveness: a liveness probe that answers
	// "not alive" for pid 0 would refuse for the wrong reason, and the test
	// would still pass with the recorded-pid guard deleted.
	stubAlwaysLive(t)
	owner := writeOwnerForTest(t, stopRuleOwner(0, os.Getpid()))
	stopped, err := shouldStopSharedDaemon(owner)
	if err != nil {
		t.Fatal(err)
	}
	if stopped {
		t.Error("a marker with no recorded daemon pid names nothing to stop")
	}
}

// TestStopRuleLivenessSeamsAreHonoured guards the seams themselves. A predicate
// that called pidAlive/processMatchesOwner directly would still return a
// plausible answer for every other fixture, so this test asserts the stubs were
// consulted with the recorded daemon instead of assuming it.
func TestStopRuleLivenessSeamsAreHonoured(t *testing.T) {
	isolateHTROwnerState(t)
	rec := stubStopRuleLiveness(t)
	owner := writeOwnerForTest(t, stopRuleOwner(impossiblePID, os.Getpid()))
	stopped, err := shouldStopSharedDaemon(owner)
	if err != nil || !stopped {
		t.Fatalf("stopped=%v err=%v; the stubbed liveness probes were not consulted", stopped, err)
	}
	if !reflect.DeepEqual(rec.alive, []int{impossiblePID}) {
		t.Errorf("pidAliveFn saw %v, want [%d] — the predicate bypassed the seam", rec.alive, impossiblePID)
	}
	if !reflect.DeepEqual(rec.matched, []int{impossiblePID}) {
		t.Errorf("processMatchesFn saw %v, want [%d] — the predicate bypassed the seam", rec.matched, impossiblePID)
	}
}

// TestStopRuleIgnoresOwnLease pins the exclusion that keeps the explicit Stop
// action working. The server process still holds its own lease from
// EnsureHTRServe when the user presses Stop, so counting that lease would make
// Stop a permanent no-op for a single-instance user.
func TestStopRuleIgnoresOwnLease(t *testing.T) {
	isolateHTROwnerState(t)
	stubStopRuleLiveness(t)
	owner := readOwnerForTest(t, os.Getpid())
	lease, err := acquireHTRLease(owner.Port, owner.Socket, owner.Identity, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		select {
		case <-lease.stop:
		default:
			close(lease.stop)
		}
		_ = os.Remove(lease.path)
	})
	stopped, err := shouldStopSharedDaemon(owner)
	if err != nil {
		t.Fatal(err)
	}
	if !stopped {
		t.Error("this process's own lease must not veto its own stop: the Stop action would become a permanent no-op")
	}
}

// TestStopRuleRefusesUnverifiableDaemon keeps the pre-existing refusal: a
// process attributable neither by executable/start token nor by the
// bearer-protected managed health probe is never killed, and the refusal is an
// error rather than a silent no-op.
func TestStopRuleRefusesUnverifiableDaemon(t *testing.T) {
	isolateHTROwnerState(t)
	origAlive, origMatch := pidAliveFn, processMatchesFn
	pidAliveFn = func(int) bool { return true }
	processMatchesFn = func(htrOwner) bool { return false }
	t.Cleanup(func() { pidAliveFn, processMatchesFn = origAlive, origMatch })
	withStubbedProbes(t, false, false) // neither probe attributes the process

	owner := readOwnerForTest(t, os.Getpid())
	stopped, err := shouldStopSharedDaemon(owner)
	if err == nil {
		t.Error("an unattributable daemon must be refused with an error, not silently skipped")
	}
	if stopped {
		t.Error("an unattributable daemon must not be reported as stoppable")
	}
}

// TestStopHTRServeLeavesAdoptedDaemonRunning is the end-to-end statement of the
// user contract through the exported entry point: StopHTRServe must return
// without an error, report the daemon still running, and leave the process
// alone when this instance did not spawn it.
func TestStopHTRServeLeavesAdoptedDaemonRunning(t *testing.T) {
	isolateHTROwnerState(t)
	// Hermetic port: htrPortEnv reads HTR_PORT, so a developer's exported value
	// would silently redirect the call at a different daemon.
	t.Setenv("HTR_PORT", "")
	stubStopRuleLiveness(t)
	// Everything downstream of provenance answers "yes": if the provenance check
	// were dropped, the predicate would return true and the daemon below would
	// be killed, which is exactly what this test forbids.
	withStubbedProbes(t, true, true)

	daemon := startIdleHelperProcess(t)
	defer func() { _ = daemon.Process.Kill() }()

	writeOwnerForTest(t, stopRuleOwner(daemon.Process.Pid, os.Getpid()+1))

	st, err := StopHTRServe(nil, stopRulePort, discardLogger())
	if err != nil {
		t.Fatalf("refusing to stop is not an error: %v", err)
	}
	if !st.Running {
		t.Error("the refused daemon must still be reported as running")
	}

	// The proof that nothing was killed: a killed child is reaped at once, so
	// Wait returning at all means StopHTRServe reached for it.
	exited := make(chan error, 1)
	go func() { exited <- daemon.Wait() }()
	select {
	case err := <-exited:
		t.Fatalf("StopHTRServe killed a daemon this process did not spawn (child exited: %v)", err)
	case <-time.After(250 * time.Millisecond):
	}
}

// writeLeaseRaw writes a lease file under the exact name activeHTRLeases scans
// for, bypassing acquireHTRLease so a fixture can carry fields the real writer
// never produces.
func writeLeaseRaw(t *testing.T, name string, lease htrLease) {
	t.Helper()
	data, err := json.Marshal(lease)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := htrLeaseDir()
	if err != nil {
		t.Fatal(err)
	}
	lockPath, err := htrLeaseLockPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := filelock.WithFileLock(lockPath, func() error {
		return atomicWriteFile(filepath.Join(dir, name), data, 0o600)
	}); err != nil {
		t.Fatal(err)
	}
}

// TestActiveHTRLeasesExcludeIsInertAtZero pins the parameter's contract at both
// ends. excludePID 0 must skip NOTHING — that is the "is anyone else left"
// question on the last-lease-release path, and it behaved that way before this
// parameter existed. A lease naming no process at all (pid 0) is unattributed
// rather than ours, so it counts as a live user of the daemon under either
// argument; erring towards "somebody is still using it" is the direction that
// keeps the user's daemon alive.
func TestActiveHTRLeasesExcludeIsInertAtZero(t *testing.T) {
	isolateHTROwnerState(t)
	writeLeaseRaw(t, "lease-corrupt-1.json", htrLease{
		Port: stopRulePort, Socket: stopRuleSocket, Identity: stopRuleIdentity,
		Heartbeat: time.Now(),
	})

	active, err := activeHTRLeases(stopRulePort, stopRuleSocket, stopRuleIdentity, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !active {
		t.Error("excludePID 0 must skip nothing; a lease naming no process is still a live user of the daemon")
	}

	active, err = activeHTRLeases(stopRulePort, stopRuleSocket, stopRuleIdentity, os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if !active {
		t.Error("a lease with pid 0 does not belong to this process and must not be excluded")
	}
}

// TestStopHTRServeDefersToAnotherInstancesLease is the same contract reached
// through the entry point by the other refusal reason. The marker is this
// process's own — it spawned the daemon — so nothing but the lease clause can
// hold the stop back, and that clause must still be a quiet success rather than
// an error, with the record intact.
func TestStopHTRServeDefersToAnotherInstancesLease(t *testing.T) {
	isolateHTROwnerState(t)
	t.Setenv("HTR_PORT", "")
	stubStopRuleLiveness(t)
	withStubbedProbes(t, true, true)

	daemon := startIdleHelperProcess(t)
	defer func() { _ = daemon.Process.Kill() }()
	owner := writeOwnerForTest(t, stopRuleOwner(daemon.Process.Pid, os.Getpid()))
	acquireForeignLease(t, owner.Port, owner.Socket, owner.Identity)

	st, err := StopHTRServe(nil, stopRulePort, discardLogger())
	if err != nil {
		t.Fatalf("yielding to another instance is not an error: %v", err)
	}
	if !st.Running {
		t.Error("the deferred daemon must still be reported as running")
	}
	if _, err := readHTROwner(); err != nil {
		t.Errorf("a lease-based refusal must keep the owner marker: %v", err)
	}

	exited := make(chan error, 1)
	go func() { exited <- daemon.Wait() }()
	select {
	case err := <-exited:
		t.Fatalf("a live foreign lease did not protect the daemon (child exited: %v)", err)
	case <-time.After(250 * time.Millisecond):
	}
}

// TestStopHTRServeKeepsMarkerOnRefusal pins that a refusal leaves the record
// alone. Removing the marker would erase the provenance every later refusal
// depends on, and would make a subsequent stop look like "no daemon at all".
func TestStopHTRServeKeepsMarkerOnRefusal(t *testing.T) {
	isolateHTROwnerState(t)
	t.Setenv("HTR_PORT", "")
	stubStopRuleLiveness(t)
	withStubbedProbes(t, true, true)

	daemon := startIdleHelperProcess(t)
	defer func() { _ = daemon.Process.Kill() }()
	foreign := os.Getpid() + 1
	writeOwnerForTest(t, stopRuleOwner(daemon.Process.Pid, foreign))

	if _, err := StopHTRServe(nil, stopRulePort, discardLogger()); err != nil {
		t.Fatalf("refusal must not be an error: %v", err)
	}
	owner, err := readHTROwner()
	if err != nil {
		t.Fatalf("a refused stop must keep the owner marker: %v", err)
	}
	if owner.StartedByPID != foreign {
		t.Errorf("StartedByPID = %d, want %d — the refusal must not rewrite provenance", owner.StartedByPID, foreign)
	}
}

// TestStopHTRServeStopsOwnDaemon is the positive half through the same entry
// point: with this process recorded as the spawner, and nothing else using the
// daemon, the stop must go through.
func TestStopHTRServeStopsOwnDaemon(t *testing.T) {
	isolateHTROwnerState(t)
	t.Setenv("HTR_PORT", "")
	stubStopRuleLiveness(t)
	withStubbedProbes(t, true, true)

	daemon := startIdleHelperProcess(t)
	defer func() { _ = daemon.Process.Kill() }()
	writeOwnerForTest(t, stopRuleOwner(daemon.Process.Pid, os.Getpid()))

	st, err := StopHTRServe(nil, stopRulePort, discardLogger())
	if err != nil {
		t.Fatalf("stop own daemon → %v", err)
	}
	if st.Running {
		t.Errorf("a stopped daemon must not be reported running: %+v", st)
	}
	exited := make(chan error, 1)
	go func() { exited <- daemon.Wait() }()
	select {
	case err := <-exited:
		if err == nil {
			t.Error("the daemon exited cleanly, expected it to be killed")
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("daemon pid %d still alive after StopHTRServe granted the stop", daemon.Process.Pid)
	}
	if _, err := readHTROwner(); err == nil {
		t.Error("owner marker must be removed after a granted stop")
	}
}

// finalReleaseOwner is stopRuleOwner with an Executable that actually names
// the child startIdleHelperProcess started, so the DIRECT
// processMatchesOwner also answers true for it. That matters because the two
// final-lease-release tests must be able to distinguish "the provenance gate
// said no" from "the daemon was left alone for some unrelated reason": with a
// placeholder executable, a stop path that skips the predicate entirely would
// leave the daemon running anyway and the test would pass without ever
// reaching the kill. Do not "simplify" this back to stopRuleExe.
func finalReleaseOwner(daemonPID, startedBy int) htrOwner {
	o := stopRuleOwner(daemonPID, startedBy)
	o.Executable = os.Args[0]
	return o
}

// TestFinalLeaseReleaseStopsOwnDaemon is the non-regression half of the stop
// rule on the path that actually runs: htrLeaseHandle.release, which is what
// terminates the daemon when the last lease goes away. One process spawns it,
// holds a lease, releases the last one, and the marker then names this process
// as the spawner — so the provenance gate must permit the kill. Driving release
// rather than terminateManagedHTR directly is deliberate: release is the
// production path, and it removes this process's own lease before asking,
// which is the only ordering under which the "is anybody else left" question
// means anything.
func TestFinalLeaseReleaseStopsOwnDaemon(t *testing.T) {
	isolateHTROwnerState(t)
	stubStopRuleLiveness(t)
	withStubbedProbes(t, true, true)

	daemon := startIdleHelperProcess(t)
	defer func() { _ = daemon.Process.Kill() }()

	owner := writeOwnerForTest(t, finalReleaseOwner(daemon.Process.Pid, os.Getpid()))
	if owner.StartedByPID != os.Getpid() {
		t.Fatalf("StartedByPID = %d, want this process — the fixture no longer models a self-spawned daemon", owner.StartedByPID)
	}

	lease, err := acquireHTRLease(owner.Port, owner.Socket, owner.Identity, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	lease.release()

	exited := make(chan error, 1)
	go func() { exited <- daemon.Wait() }()
	select {
	case err := <-exited:
		if err == nil {
			t.Error("the daemon exited cleanly, expected it to be killed")
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("daemon pid %d survived the final lease release; the provenance gate must not block a daemon this process spawned", daemon.Process.Pid)
	}
	if _, err := readHTROwner(); err == nil {
		t.Error("owner marker must be removed after a granted stop")
	}
}

// TestFinalLeaseReleaseDoesNotKillAnAdoptedDaemon is the adopter half of the
// same path, and the case the rule exists for. The sequence modelled is: A
// spawns the daemon, B adopts it, A exits, B releases the final lease. What B
// finds in the marker is itself as the writer (OwnerPID) and A as the spawner
// (StartedByPID) — so B releasing the last lease must NOT kill A's daemon, and
// must not erase the marker that records who did spawn it.
func TestFinalLeaseReleaseDoesNotKillAnAdoptedDaemon(t *testing.T) {
	isolateHTROwnerState(t)
	stubStopRuleLiveness(t)
	// Everything downstream of provenance answers "yes": with the gate missing,
	// the predicate would return true and the daemon below would be killed,
	// which is exactly what this test forbids.
	withStubbedProbes(t, true, true)

	daemon := startIdleHelperProcess(t)
	defer func() { _ = daemon.Process.Kill() }()

	// A: the process that spawned the daemon and has since exited. One above
	// this one is the established "somebody else" stand-in in these fixtures.
	spawner := os.Getpid() + 1
	owner := writeOwnerForTest(t, finalReleaseOwner(daemon.Process.Pid, spawner))
	if owner.OwnerPID != os.Getpid() {
		t.Fatalf("OwnerPID = %d, want this process — the fixture no longer models an adopting writer", owner.OwnerPID)
	}
	if owner.StartedByPID != spawner {
		t.Fatalf("StartedByPID = %d, want the original spawner %d", owner.StartedByPID, spawner)
	}

	var logged bytes.Buffer
	lease, err := acquireHTRLease(owner.Port, owner.Socket, owner.Identity, log.New(&logged, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	lease.release()

	// The proof that nothing was killed: a killed child is reaped at once, so
	// Wait returning at all means the release reached for it.
	exited := make(chan error, 1)
	go func() { exited <- daemon.Wait() }()
	select {
	case err := <-exited:
		t.Fatalf("the final-lease release killed a daemon this process did not spawn (child exited: %v)", err)
	case <-time.After(250 * time.Millisecond):
	}

	kept, err := readHTROwner()
	if err != nil {
		t.Fatalf("a refused kill must keep the owner marker: %v", err)
	}
	if kept.StartedByPID != spawner {
		t.Errorf("StartedByPID = %d, want %d — a refusal must not rewrite provenance", kept.StartedByPID, spawner)
	}

	// A refusal is otherwise silent, so the single line carrying the pid and
	// the reason is the only evidence this was a decision rather than a miss.
	got := strings.TrimSpace(logged.String())
	if lines := strings.Count(got, "\n"); lines != 0 {
		t.Errorf("refusal must be one line, got %d: %q", lines+1, got)
	}
	if !strings.Contains(got, fmt.Sprintf("pid %d", daemon.Process.Pid)) {
		t.Errorf("refusal line must name the daemon pid, got %q", got)
	}
	if !strings.Contains(got, "did not spawn it") {
		t.Errorf("refusal line must give the reason, got %q", got)
	}
}
