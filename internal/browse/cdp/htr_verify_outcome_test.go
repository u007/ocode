package cdp

import (
	"log"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/u007/ocode/internal/tool"
)

// exitedProcessRecord is the supervisor record the spawn path would have built
// for cmd — watchHTRExitNotify reads only the PID and StartedAt.
func exitedProcessRecord(pid int) tool.ProcessRecord {
	return tool.ProcessRecord{ID: htrServeID, Name: "htrcli", Kind: tool.ProcessKindHTR, PID: pid, StartedAt: time.Now()}
}

// The verification dedupe is keyed on (port, ready) and remembers nothing about
// WHICH process answered. A daemon that dies and is then re-ensured produces a
// NEW process on the same port, so its verification failure is a NEW event — but
// without a reset the dedupe compares against the dead daemon's (port, false) and
// stays silent. The operator is left with a daemon that is up, silent, and not
// answering, and no new complaint about why.
//
// Catches: dropping the reset from the death path (noteHTRVerifyOutcome then
// swallows every repeat failure on that port).
func TestVerifyOutcomeIsReportedAgainAfterTheDaemonDies(t *testing.T) {
	isolateHTROwnerState(t)
	resetHTRVerifyOutcome()
	t.Cleanup(resetHTRVerifyOutcome)

	// First failure on this port: reported.
	if !noteHTRVerifyOutcome(htrVerifyOutcome{port: 3845, ready: false}) {
		t.Fatal("the first verification failure must be reported")
	}
	// A repeat within the same daemon's lifetime: suppressed.
	if noteHTRVerifyOutcome(htrVerifyOutcome{port: 3845, ready: false}) {
		t.Fatal("a repeat of the same (port, ready) must stay silent")
	}
	// A different port is a different answer, still reported.
	if !noteHTRVerifyOutcome(htrVerifyOutcome{port: 9911, ready: false}) {
		t.Fatal("a different port must be reported")
	}

	// The daemon dies: the next ensure brings a new process, so its failure must
	// be reported even though the (port, ready) pair is identical.
	resetHTRVerifyOutcome()
	if !noteHTRVerifyOutcome(htrVerifyOutcome{port: 3845, ready: false}) {
		t.Fatal("a replacement daemon's verification failure was swallowed as a repeat")
	}
}

// A ready verdict must still be deduped across a reset-free run, and clearing
// must not leave a stale value behind that suppresses the NEXT port's report.
func TestVerifyOutcomeResetClearsTheRememberedPort(t *testing.T) {
	isolateHTROwnerState(t)
	resetHTRVerifyOutcome()
	t.Cleanup(resetHTRVerifyOutcome)

	if !noteHTRVerifyOutcome(htrVerifyOutcome{port: 3845, ready: true}) {
		t.Fatal("the first ready verdict must be reported")
	}
	if noteHTRVerifyOutcome(htrVerifyOutcome{port: 3845, ready: true}) {
		t.Fatal("a repeat ready verdict must stay silent")
	}
	// A change on the same port is a new answer.
	if !noteHTRVerifyOutcome(htrVerifyOutcome{port: 3845, ready: false}) {
		t.Fatal("ready -> not-ready on one port must be reported")
	}
	resetHTRVerifyOutcome()
	if lastHTRVerifyOutcome.Load() != nil {
		t.Fatal("reset must clear the remembered outcome, not leave it in place")
	}
}

// A daemon that dies leaves an owner marker naming a pid that is no longer
// running. Nothing else retracts it: the watcher's own sweep runs BEFORE it
// signals death, so a boot path that was mid-ensure can write the marker in the
// gap between the sweep and the signal (confirmSharedSpawnAlive saw the process
// alive, then it died, then htrWriteOwner recorded it). StopHTRServe and the
// next ensure both believe that marker.
//
// The second sweep runs after the signal precisely to make the death path the
// authoritative cleanup whatever the interleaving.
func TestDaemonDeathRetractsAMarkerWrittenForIt(t *testing.T) {
	isolateHTROwnerState(t)

	// A process that has already exited — the same state cmd.Wait reports.
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	pid := cmd.Process.Pid

	// The marker a racing ensure would have written for this pid.
	if err := htrWriteOwner("id-race", 3845, pid, "/tmp/htr.sock", "/bin/true", time.Now(), os.Getpid()); err != nil {
		t.Fatalf("write owner: %v", err)
	}
	path, err := htrOwnerPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("marker not written: %v", err)
	}

	died := make(chan struct{})
	watchHTRExitNotify(cmd, nil, exitedProcessRecord(pid), "id-race", log.Default(), died)
	<-died

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the marker for the dead pid %d survived its death (stat err = %v)", pid, err)
	}
}

// The sweep must be scoped to the daemon that died: a marker naming some OTHER
// live daemon is not ours to delete, and removing it would disown a running
// daemon that StopHTRServe is entitled to stop.
func TestDaemonDeathLeavesAnotherDaemonsMarkerAlone(t *testing.T) {
	isolateHTROwnerState(t)

	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}

	// A marker for a different pid entirely.
	const otherPID = 424242
	if err := htrWriteOwner("id-other", 9911, otherPID, "/tmp/other.sock", "/bin/true", time.Now(), os.Getpid()); err != nil {
		t.Fatalf("write owner: %v", err)
	}
	path, err := htrOwnerPath()
	if err != nil {
		t.Fatal(err)
	}

	died := make(chan struct{})
	watchHTRExitNotify(cmd, nil, exitedProcessRecord(cmd.Process.Pid), "id-other", log.Default(), died)
	<-died

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the death of an unrelated daemon removed its marker: %v", err)
	}
}

// The reset has to be WIRED into the death path, not merely exist. A test that
// calls resetHTRVerifyOutcome() directly proves the helper's semantics and
// nothing about whether the daemon's death ever reaches it — so dropping the
// call from watchHTRExitNotify would leave every other test green while a
// replacement daemon's verification failure went unreported again.
//
// This runs the real death path (watchHTRExitNotify over an exited process)
// with a verdict already published, and asserts the memory is cleared.
func TestDaemonDeathResetsTheVerificationOutcome(t *testing.T) {
	isolateHTROwnerState(t)
	resetHTRVerifyOutcome()
	t.Cleanup(resetHTRVerifyOutcome)

	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}

	// A verification failure this daemon produced just before it died.
	if !noteHTRVerifyOutcome(htrVerifyOutcome{port: 3845, ready: false}) {
		t.Fatal("setup: the first verdict must be reported")
	}
	if lastHTRVerifyOutcome.Load() == nil {
		t.Fatal("setup: nothing was remembered, so the test would pass vacuously")
	}

	died := make(chan struct{})
	watchHTRExitNotify(cmd, nil, exitedProcessRecord(cmd.Process.Pid), "id-died", log.Default(), died)
	<-died

	if lastHTRVerifyOutcome.Load() != nil {
		t.Fatal("the daemon's death did not reset the verification outcome — a replacement " +
			"daemon failing on the same port would be silently swallowed as a repeat")
	}
}
