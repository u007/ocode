package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestToolResultsSweep_RemovesStaleAndRearms: the hourly tick prunes a
// tool-result file older than two days and schedules the next tick.
func TestToolResultsSweep_RemovesStaleAndRearms(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	dir := filepath.Join(state, "opencode", "tool-results")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dir, "call_old.txt")
	if err := os.WriteFile(stale, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-72 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	var m model
	_, cmd := m.Update(toolResultsSweepTickMsg{})
	if cmd == nil {
		t.Fatal("sweep tick returned no command, so the sweep would not re-arm")
	}
	if msg := sweepToolResultsCmd()(); msg != nil {
		t.Fatalf("sweep command returned %T, want nil", msg)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale tool-result file should be removed, err=%v", err)
	}
}
