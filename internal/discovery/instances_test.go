package discovery

import (
	"testing"

	"github.com/u007/ocode/internal/tool"
)

func TestAssignChatPortDeterministicBySortedID(t *testing.T) {
	ids := []string{"local/bonsai-8b-1bit", "local/aardvark-1b", "local/zeta-70b"}
	// Sorted: aardvark-1b(0), bonsai-8b-1bit(1), zeta-70b(2) -> ports 11458,11459,11460
	port, err := AssignChatPort("local/bonsai-8b-1bit", ids)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if port != chatPortRangeStart+1 {
		t.Fatalf("port = %d, want %d", port, chatPortRangeStart+1)
	}
}

func TestAssignChatPortUnknownModelErrors(t *testing.T) {
	ids := []string{"local/aardvark-1b"}
	if _, err := AssignChatPort("local/not-registered", ids); err == nil {
		t.Fatal("expected error for a modelID not present in registeredIDs")
	}
}

func TestAssignChatPortRejectsMoreThanRangeSize(t *testing.T) {
	ids := make([]string, chatPortRangeSize+1)
	for i := range ids {
		ids[i] = string(rune('a' + i))
	}
	if _, err := AssignChatPort(ids[chatPortRangeSize], ids); err == nil {
		t.Fatal("expected 'no free local-model port' error beyond the reserved range")
	}
}

func TestStartModelInstanceUnknownManifestErrors(t *testing.T) {
	spawn := func(string) error { return nil }
	err := StartModelInstance(spawn, "local/does-not-exist", 19999, 1, t.TempDir())
	if err == nil {
		t.Fatal("expected error for a model id with no chat manifest")
	}
}

func TestGetModelInstanceUnknownReturnsFalse(t *testing.T) {
	if _, ok := GetModelInstance("local/never-started"); ok {
		t.Fatal("expected ok=false for a model that was never started")
	}
}

func TestStopModelInstanceNotRunningIsNoop(t *testing.T) {
	procs := tool.NewProcessRegistry()
	if err := StopModelInstance(procs, "local/never-started"); err != nil {
		t.Fatalf("stopping a never-started instance should be a no-op, got: %v", err)
	}
}
