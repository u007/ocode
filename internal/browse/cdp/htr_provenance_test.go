package cdp

import (
	"os"
	"testing"
)

// HTRProvenanceFor is the read-only view the settings API puts on the wire, and
// the Stop button is gated on StartedByOcode. These cases reuse the stop-rule
// fixtures on purpose: the point of the test is that this function reports
// shouldStopSharedDaemon's verdict rather than a cheaper approximation of it, so
// the fixtures are exactly the ones the rule was already argued about with.

// A daemon this process spawned is reported with its pid and stoppable=true.
// The pid assertion is what makes this more than a boolean restatement: a
// function that answered only StartedByOcode would satisfy every other case.
func TestProvenanceReportsOwnDaemonAsStoppable(t *testing.T) {
	isolateHTROwnerState(t)
	stubStopRuleLiveness(t)
	want := stopRuleOwner(os.Getpid(), os.Getpid())
	if got := writeOwnerForTest(t, want); got.PID != os.Getpid() {
		t.Fatalf("fixture daemon pid = %d, want this process", got.PID)
	}

	prov := HTRProvenanceFor(stopRulePort)
	if prov.DaemonPID != os.Getpid() {
		t.Errorf("DaemonPID = %d, want %d", prov.DaemonPID, os.Getpid())
	}
	if !prov.StartedByOcode {
		t.Error("StartedByOcode = false for a daemon this process spawned; the Stop button would be permanently dead")
	}
}

// A daemon another process spawned must be reported as not ocode's, WITH its
// pid still shown: the pid is provenance the user can act on, and the flag is
// the permission. Collapsing them — reporting 0 — would be a different bug, and
// this case is what stops the flag being faked from the pid being absent.
func TestProvenanceRefusesForeignDaemonButStillNamesIt(t *testing.T) {
	isolateHTROwnerState(t)
	stubStopRuleLiveness(t)
	writeOwnerForTest(t, stopRuleOwner(os.Getpid(), os.Getpid()+1)) // spawned elsewhere

	prov := HTRProvenanceFor(stopRulePort)
	if prov.StartedByOcode {
		t.Error("StartedByOcode = true for a daemon another process spawned; the Stop button would offer a kill ocode refuses")
	}
	if prov.DaemonPID != os.Getpid() {
		t.Errorf("DaemonPID = %d, want %d — a refusal must still name the daemon", prov.DaemonPID, os.Getpid())
	}
}

// A marker recorded against a different port belongs to another daemon. Reading
// it here would let a port-3845 settings page claim pid/provenance for a daemon
// on 3846, so the answer must be the zero value.
func TestProvenanceIgnoresAMarkerForAnotherPort(t *testing.T) {
	isolateHTROwnerState(t)
	stubStopRuleLiveness(t)
	writeOwnerForTest(t, stopRuleOwner(os.Getpid(), os.Getpid()))

	prov := HTRProvenanceFor(stopRulePort + 1)
	if prov != (HTRProvenance{}) {
		t.Errorf("provenance for an unrecorded port = %+v, want the zero value", prov)
	}
}

// No marker at all is the cold-start case every ocode launch begins in. It must
// be the zero value rather than a panic or a fabricated pid, and it must not
// consult the liveness seams — there is nothing to ask about.
func TestProvenanceIsZeroWithoutAMarker(t *testing.T) {
	isolateHTROwnerState(t)
	stubStopRuleLiveness(t)

	prov := HTRProvenanceFor(stopRulePort)
	if prov != (HTRProvenance{}) {
		t.Errorf("provenance with no owner marker = %+v, want the zero value", prov)
	}
}

// A marker whose recorded pid is gone still yields that pid — this function
// reports the RECORD, and suppressing stale numbers is the caller's job (the
// settings handler only asks when its own probe says the daemon is running).
// What must be false here is the entitlement: a pid nobody can find is not a
// daemon ocode may kill, and StartedByOcode is the field the Stop button reads.
func TestProvenanceHidesADeadDaemon(t *testing.T) {
	// Deliberately NOT stubStopRuleLiveness: that helper makes every pid > 0
	// answer "alive", so with it this case would be measuring the stub and the
	// entitlement would come back true. impossiblePID is far above pid_max, so
	// the REAL probe answers "dead" — which is the input under test. The rule
	// returns at that liveness check, so nothing shells out to `ps`.
	isolateHTROwnerState(t)
	writeOwnerForTest(t, stopRuleOwner(impossiblePID, os.Getpid()))

	prov := HTRProvenanceFor(stopRulePort)
	if prov.StartedByOcode {
		t.Error("StartedByOcode = true for a dead daemon")
	}
	if prov.DaemonPID != impossiblePID {
		t.Errorf("DaemonPID = %d, want the recorded %d — the marker is read even when the daemon is gone, so the stop rule is what refuses", prov.DaemonPID, impossiblePID)
	}
}

// An unparseable marker is unreadable, not absent: it must degrade to the zero
// value (no claims) rather than to a partial read that could name a pid nobody
// verified.
func TestProvenanceIsZeroForAnUnreadableMarker(t *testing.T) {
	isolateHTROwnerState(t)
	path, err := htrOwnerPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	prov := HTRProvenanceFor(stopRulePort)
	if prov != (HTRProvenance{}) {
		t.Errorf("provenance for an unparseable marker = %+v, want the zero value", prov)
	}
}
