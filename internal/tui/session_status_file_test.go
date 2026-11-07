package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// useStatusDir points the status file at a temp dir for one test.
func useStatusDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	prev := sessionStatusDirOverride
	sessionStatusDirOverride = dir
	t.Cleanup(func() { sessionStatusDirOverride = prev })
	return dir
}

func readStatusRecord(t *testing.T, dir, id string) sessionStatusRecord {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if err != nil {
		t.Fatalf("read status file: %v", err)
	}
	var rec sessionStatusRecord
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatalf("parse status file %q: %v", b, err)
	}
	return rec
}

// TestSessionStatusRecordCarriesPIDAndTimestamp: the record must carry the pid
// and updated_at the server uses to age out a crashed TUI's turn_active=true.
func TestSessionStatusRecordCarriesPIDAndTimestamp(t *testing.T) {
	dir := useStatusDir(t)
	writeSessionStatusNow("ses_stat_a", true)
	rec := readStatusRecord(t, dir, "ses_stat_a")
	if !rec.TurnActive || rec.PID != os.Getpid() || rec.UpdatedAt == "" {
		t.Fatalf("record = %+v, want turn_active with this pid and an updated_at", rec)
	}
	writeSessionStatusNow("ses_stat_a", false)
	if rec := readStatusRecord(t, dir, "ses_stat_a"); rec.TurnActive {
		t.Fatalf("turn-end did not write turn_active=false: %+v", rec)
	}
}

// TestSessionStatusStaleSequenceDropped: a write that finishes late must not
// overwrite a newer state for the same session.
func TestSessionStatusStaleSequenceDropped(t *testing.T) {
	dir := useStatusDir(t)
	writeSessionStatusRecord(10, newSessionStatusRecord("ses_stat_b", false))
	writeSessionStatusRecord(4, newSessionStatusRecord("ses_stat_b", true))
	if rec := readStatusRecord(t, dir, "ses_stat_b"); rec.TurnActive {
		t.Fatalf("late seq 4 overwrote seq 10: %+v", rec)
	}
}

// TestSessionStatusWriteLeavesNoTempFiles: the temp+rename write must not leave
// a .tmp file behind.
func TestSessionStatusWriteLeavesNoTempFiles(t *testing.T) {
	dir := useStatusDir(t)
	for i := 0; i < 5; i++ {
		writeSessionStatusNow("ses_stat_c", i%2 == 0)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}
