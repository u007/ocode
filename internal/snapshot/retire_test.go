package snapshot

import (
	"os"
	"testing"
)

// TestStoreRetireRejectsLaterWrites is the regression guard for the phantom
// cross-agent conflict: Agent.Shutdown now bounds its wait on abandoned
// orphan-recovery goroutines, so a straggler's RegisterWrite can land AFTER
// the store is torn down. If Retire did not poison the store, that late call
// would re-seed the process-global fileWrites registry under a dead agent id
// and nothing would ever unregister it — permanently blocking other agents'
// undos on that path.
func TestStoreRetireRejectsLaterWrites(t *testing.T) {
	s, _ := newTempStore(t)
	if err := os.WriteFile("a.txt", []byte("v1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// A live agent writes the same path first, so we have a seq to compare.
	live := NewStore("live-agent", "")
	if err := live.Backup("a.txt", "tc-live"); err != nil {
		t.Fatal(err)
	}
	live.RegisterWrite("a.txt", "tc-live")
	liveSeq := globalWriteSeq.Load()

	// The retiring agent is discarded.
	s.Backup("a.txt", "tc-old") //nolint:errcheck
	s.RegisterWrite("a.txt", "tc-old")
	s.Retire()

	if !s.Retired() {
		t.Fatal("Retired() = false after Retire()")
	}

	// A straggler write arrives after teardown (the abandoned goroutine).
	s.Backup("a.txt", "tc-late") //nolint:errcheck
	s.RegisterWrite("a.txt", "tc-late")

	if w := crossAgentWriteAfterSeq("a.txt", "live-agent", liveSeq); w != nil && w.AgentID == s.agentID {
		t.Fatalf("retired store re-seeded the registry: %+v", w)
	}
	live.Reset() //nolint:errcheck
}

// TestStoreResetDoesNotRetire pins the distinction that makes Retire a
// separate method: Reset runs on LIVE, reused stores (SwitchSession rebinding
// a rebuilt agent, the long-lived process-global store). If Reset set the
// retired flag, those stores would silently stop tracking every later write.
func TestStoreResetDoesNotRetire(t *testing.T) {
	s, _ := newTempStore(t)
	if err := os.WriteFile("b.txt", []byte("v1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	s.Backup("b.txt", "tc1") //nolint:errcheck
	s.Reset()

	if s.Retired() {
		t.Fatal("Reset() retired the store; live/reused stores must stay writable")
	}
	// Writes after Reset must still register.
	s.RegisterWrite("b.txt", "tc2")
	if w := crossAgentWriteAfterSeq("b.txt", "someone-else", 0); w == nil {
		t.Fatal("expected a registry entry after Reset + RegisterWrite, found none")
	}
	s.Retire()
}
