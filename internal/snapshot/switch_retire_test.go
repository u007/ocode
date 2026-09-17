package snapshot

import (
	"os"
	"testing"
)

// TestSwitchSessionResetDoesNotRetire pins the invariant that forced Retire to
// be a separate method from Reset: SwitchSession calls Reset internally when
// rebinding a store to a different session (the agent-rebuild / session-load
// path). That store stays live and must keep tracking writes — if Reset had
// set the retired flag, every agent rebuild would silently stop updating the
// changes tab.
func TestSwitchSessionResetDoesNotRetire(t *testing.T) {
	s, _ := newTempStore(t)
	if err := os.WriteFile("c.txt", []byte("v1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	s.SetSessionID("sess-a")
	s.Backup("c.txt", "tc-a") //nolint:errcheck

	// Rebinding to a different session drops live history via Reset.
	s.SwitchSession("sess-b")

	if s.Retired() {
		t.Fatal("SwitchSession retired the store; a rebound store must stay writable")
	}
	if s.Len() != 0 {
		t.Fatalf("SwitchSession left %d snapshots; expected the live set dropped", s.Len())
	}

	// The rebound store must still register writes.
	if err := os.WriteFile("c.txt", []byte("v2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	s.Backup("c.txt", "tc-b") //nolint:errcheck
	s.RegisterWrite("c.txt", "tc-b")
	if w := crossAgentWriteAfterSeq("c.txt", "someone-else", 0); w == nil {
		t.Fatal("rebound store did not register a write; changes tracking would go dark")
	}
	s.Retire()
}
