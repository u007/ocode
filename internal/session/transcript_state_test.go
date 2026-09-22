package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/agent"
)

// TestStoredTranscriptStateForDirReadsTail pins the extended cheap read: one
// sqlite open yields both the revision token and the last stored row.
func TestStoredTranscriptStateForDirReadsTail(t *testing.T) {
	_, dir := isolatedProjectRoot(t)
	id := "ses_state-tail"

	// No stored session → no tail and the stable empty token.
	rev, tail, err := storedTranscriptStateForDir(dir, id)
	if err != nil {
		t.Fatalf("missing session: %v", err)
	}
	if rev != "" || tail != nil {
		t.Fatalf("missing session = (%q, %v), want empty/nil", rev, tail)
	}

	msgs := []agent.Message{
		{Role: "user", Content: "q0"},
		{Role: "assistant", Content: "a0"},
	}
	if err := saveToDir(dir, id, "", msgs, nil, false, 0); err != nil {
		t.Fatalf("seed: %v", err)
	}
	rev, tail, err = storedTranscriptStateForDir(dir, id)
	if err != nil {
		t.Fatalf("seeded read: %v", err)
	}
	if rev == "" {
		t.Fatal("seeded revision is empty")
	}
	if len(tail) != 1 || tail[0].Role != "assistant" || tail[0].Content != "a0" {
		t.Fatalf("tail = %+v, want the last stored assistant row", tail)
	}
	// The revision-only helper must stay a thin wrapper: same token, no caller
	// change.
	wantRev, err := storedRevisionForDir(dir, id)
	if err != nil {
		t.Fatalf("storedRevisionForDir: %v", err)
	}
	if rev != wantRev {
		t.Fatalf("revision = %q, want wrapper's %q", rev, wantRev)
	}
}

// TestStoredTranscriptStateForDirFailsOpen: legacy formats and an unreadable
// sqlite still serve a file-token revision but yield NO tail — a format we
// cannot classify must never look like an interrupted turn.
func TestStoredTranscriptStateForDirFailsOpen(t *testing.T) {
	t.Run("legacy ojsonl", func(t *testing.T) {
		_, dir := isolatedProjectRoot(t)
		id := "ses_state-legacy"
		if err := os.WriteFile(filepath.Join(dir, id+".ojsonl"), []byte("{}\n"), 0o644); err != nil {
			t.Fatalf("write legacy: %v", err)
		}
		rev, tail, err := storedTranscriptStateForDir(dir, id)
		if err != nil {
			t.Fatalf("legacy read: %v", err)
		}
		if rev == "" {
			t.Fatal("legacy revision is empty, want file token")
		}
		if tail != nil {
			t.Fatalf("legacy tail = %+v, want nil (fail open)", tail)
		}
	})

	t.Run("unreadable sqlite", func(t *testing.T) {
		_, dir := isolatedProjectRoot(t)
		id := "ses_state-badsqlite"
		if err := os.WriteFile(filepath.Join(dir, id+".sqlite"), []byte("not a database"), 0o644); err != nil {
			t.Fatalf("write bogus sqlite: %v", err)
		}
		rev, tail, err := storedTranscriptStateForDir(dir, id)
		if err != nil {
			t.Fatalf("bogus sqlite read: %v", err)
		}
		if rev == "" {
			t.Fatal("bogus sqlite revision is empty, want file token fallback")
		}
		if tail != nil {
			t.Fatalf("bogus sqlite tail = %+v, want nil (fail open)", tail)
		}
	})

	t.Run("empty transcript", func(t *testing.T) {
		_, dir := isolatedProjectRoot(t)
		id := "ses_state-empty"
		if err := saveToDir(dir, id, "", nil, nil, false, 0); err != nil {
			t.Fatalf("seed empty: %v", err)
		}
		rev, tail, err := storedTranscriptStateForDir(dir, id)
		if err != nil {
			t.Fatalf("empty read: %v", err)
		}
		if rev == "" {
			t.Fatal("empty session revision is empty")
		}
		if tail != nil {
			t.Fatalf("empty tail = %+v, want nil", tail)
		}
	})
}
