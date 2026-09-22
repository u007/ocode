package session

import (
	"bytes"
	"log"
	"strings"
	"testing"
	"time"
)

// captureLog routes the standard logger into a buffer for the test's
// lifetime and returns it.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })
	return &buf
}

// TestLiveSnapshotsDroppedAfterMidTurnReplaceAreLogged reproduces
// ses_2026-09-22-124117-caa39c49: wireLivePersist pre-persists the base
// transcript (the user message), the turn's step messages are queued as live
// snapshots, and an authoritative Replace lands mid-turn. Every later live
// snapshot must still drop (never resurrect replaced history), but the drop
// and the replace must be visible in the log instead of leaving a transcript
// frozen at the user row with no trace of why.
func TestLiveSnapshotsDroppedAfterMidTurnReplaceAreLogged(t *testing.T) {
	buf := captureLog(t)
	dir := t.TempDir()
	id := "ses_live-drop-log"

	// Pre-persisted base: the turn's user message.
	if err := saveToDir(dir, id, "", liveMsgs("user"), nil, true, 0); err != nil {
		t.Fatalf("base live persist: %v", err)
	}

	// Mid-turn authoritative replacement rewrites the user row (same
	// length, different content) and bumps history_gen.
	if err := persistToDir(dir, id, "", liveMsgs("user-rewritten"), nil, false, 0, true); err != nil {
		t.Fatalf("replace: %v", err)
	}

	// Live snapshots taken against the pre-replace generation (gen 0):
	// superseded, must drop.
	changed, err := appendSqliteSession(dir, id, "", liveMsgs("user", "step1"), nil, true, 0, false)
	if err != nil || changed {
		t.Fatalf("superseded-gen live write: changed=%v err=%v", changed, err)
	}
	// Live snapshot taken against the current generation but whose stored
	// prefix no longer matches: foreign/stale, must drop.
	gen, err := readHistoryGen(dir, id)
	if err != nil {
		t.Fatalf("readHistoryGen: %v", err)
	}
	changed, err = appendSqliteSession(dir, id, "", liveMsgs("user", "step1", "step2"), nil, true, gen, false)
	if err != nil || changed {
		t.Fatalf("prefix-mismatch live write: changed=%v err=%v", changed, err)
	}
	if err := flushDir(dir, id, 10*time.Second); err != nil {
		t.Fatalf("flushDir: %v", err)
	}
	assertContents(t, readStoredContents(t, dir, id), "user-rewritten")

	out := buf.String()
	for _, want := range []string{
		"replace " + id + ": rewrite",
		"history_gen 0 -> 1",
		"live snapshot dropped for " + id + ": superseded generation",
		"live snapshot dropped for " + id + ": stored rows are not a prefix",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("log missing %q\n--- log ---\n%s", want, out)
		}
	}
}

// TestReplaceShrinkIsLogged: a shrinking replacement (truncate/compaction)
// logs the row counts and the generation bump.
func TestReplaceShrinkIsLogged(t *testing.T) {
	buf := captureLog(t)
	dir := t.TempDir()
	id := "ses_replace-shrink-log"
	mustSeedSession(t, dir, id, "m0", "m1", "m2")
	if err := persistToDir(dir, id, "", liveMsgs("m0"), nil, false, 0, true); err != nil {
		t.Fatalf("replace: %v", err)
	}
	for _, want := range []string{"replace " + id + ": shrink 3 -> 1 stored rows", "history_gen 0 -> 1"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("log missing %q\n--- log ---\n%s", want, buf.String())
		}
	}
}
