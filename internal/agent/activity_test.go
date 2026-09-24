package agent

import "testing"

// TestActivityTrackerSnapshotIsDefensiveCopy pins the PULL half of the activity
// feed (Snapshot) against the PUSH half (Notify). The headless server reads
// Snapshot to stamp the web/desktop status bar while the TUI blocks on Notify;
// if Snapshot handed back the tracker's own slices, a consumer mutating what it
// got would corrupt the state the other consumer is about to publish.
func TestActivityTrackerSnapshotIsDefensiveCopy(t *testing.T) {
	tr := newActivityTracker()

	if snap := tr.Snapshot(); snap.LLMRunning || len(snap.ActiveTools) != 0 || len(snap.ActiveAgents) != 0 {
		t.Fatalf("fresh tracker should be idle, got %+v", snap)
	}

	tr.setLLMRunning(true)
	tr.toolStarted("bash")
	tr.agentStarted("code-reviewer")

	snap := tr.Snapshot()
	if !snap.LLMRunning {
		t.Error("Snapshot().LLMRunning = false, want true")
	}
	if len(snap.ActiveTools) != 1 || snap.ActiveTools[0].Name != "bash" {
		t.Errorf("Snapshot().ActiveTools = %+v, want one bash entry", snap.ActiveTools)
	}
	if snap.ActiveTools[0].StartedAt.IsZero() {
		t.Error("Snapshot().ActiveTools[0].StartedAt is zero; the status bar renders it as an elapsed timer")
	}
	if len(snap.ActiveAgents) != 1 || snap.ActiveAgents[0] != "code-reviewer" {
		t.Errorf("Snapshot().ActiveAgents = %v, want [code-reviewer]", snap.ActiveAgents)
	}

	// Mutating the returned snapshot must not reach back into the tracker.
	snap.ActiveTools[0].Name = "mutated"
	snap.ActiveAgents[0] = "mutated"
	if again := tr.Snapshot(); again.ActiveTools[0].Name != "bash" || again.ActiveAgents[0] != "code-reviewer" {
		t.Fatalf("Snapshot aliases tracker internals: %+v", again)
	}

	// A completed tool must disappear from the next pull.
	tr.toolDone("bash")
	tr.setLLMRunning(false)
	after := tr.Snapshot()
	if after.LLMRunning || len(after.ActiveTools) != 0 {
		t.Fatalf("after toolDone/LLM stop the tracker should be idle, got %+v", after)
	}
}
