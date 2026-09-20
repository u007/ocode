package session

import (
	"errors"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
)

// TestRekeyForDirPreservesTranscriptAndCreatedAt pins the core /reset-id
// contract: the conversation survives under a NEW id, created_at and metadata
// are preserved verbatim, and the old id no longer resolves.
func TestRekeyForDirPreservesTranscriptAndCreatedAt(t *testing.T) {
	root := t.TempDir()
	SetWorkDir(root)
	t.Cleanup(func() { SetWorkDir("") })

	oldID := "ses_2026-01-02-030405-deadbeef"
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	meta := map[string]any{"spend": 0.25, "permission_mode": "sandbox"}
	msgs := []agent.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
	}
	// Write the seed directly so created_at is distinctive (Save sets it to now).
	dir := mustStorageDir(t, root)
	seed := Session{
		ID:        oldID,
		Title:     "My Chat",
		Messages:  msgs,
		CreatedAt: created,
		UpdatedAt: created,
		Metadata:  meta,
	}
	if err := writeSqliteSessionFull(dir, seed); err != nil {
		t.Fatal(err)
	}
	if err := refreshIndexRow(dir, oldID); err != nil {
		t.Fatal(err)
	}
	newID, err := RekeyForDir(root, oldID)
	if err != nil {
		t.Fatalf("RekeyForDir: %v", err)
	}
	if newID == oldID || newID == "" {
		t.Fatalf("expected a fresh id, got %q", newID)
	}

	// The old id must be gone from every read path.
	if _, err := LoadForDir(root, oldID); err == nil {
		t.Fatal("old id still resolves after rekey")
	}
	if refs, err := ListRefsForDir(root); err == nil {
		for _, r := range refs {
			if r.ID == oldID {
				t.Fatalf("old id %s still listed", oldID)
			}
		}
	}

	moved, err := LoadForDir(root, newID)
	if err != nil {
		t.Fatalf("load rekeyed session: %v", err)
	}
	if moved.Title != "My Chat" {
		t.Errorf("title = %q, want %q", moved.Title, "My Chat")
	}
	if !moved.CreatedAt.Equal(created) {
		t.Errorf("created_at = %v, want %v (must survive rekey)", moved.CreatedAt, created)
	}
	if len(moved.Messages) != 2 || moved.Messages[0].Content != "hello" || moved.Messages[1].Content != "hi there" {
		t.Fatalf("transcript not preserved: %#v", moved.Messages)
	}
	if moved.Metadata == nil || moved.Metadata["permission_mode"] != "sandbox" {
		t.Fatalf("metadata not preserved: %#v", moved.Metadata)
	}
}

// TestRekeyForDirMissingTranscript checks the no-op case is a typed error the
// handler can map to 404 instead of minting an empty session.
func TestRekeyForDirMissingTranscript(t *testing.T) {
	root := t.TempDir()
	SetWorkDir(root)
	t.Cleanup(func() { SetWorkDir("") })

	if _, err := RekeyForDir(root, "ses_does-not-exist"); !errors.Is(err, ErrNoStoredSession) {
		t.Fatalf("err = %v, want ErrNoStoredSession", err)
	}
	if _, err := RekeyForDir(root, ""); !errors.Is(err, ErrNoStoredSession) {
		t.Fatalf("empty id err = %v, want ErrNoStoredSession", err)
	}
}

// TestRekeyForDirRawTranscriptKeepsOrphanRows pins that the rekey copies the
// RAW stored transcript, not the loader's filtered view: an orphan tool result
// (no matching tool_call) must survive, because a filtered copy would then
// conflict on the next full save.
func TestRekeyForDirRawTranscriptKeepsOrphanRows(t *testing.T) {
	root := t.TempDir()
	SetWorkDir(root)
	t.Cleanup(func() { SetWorkDir("") })

	oldID := NewSessionID()
	tc := agent.ToolCall{ID: "call_1", Type: "function"}
	tc.Function.Name = "bash"
	tc.Function.Arguments = "{}"
	msgs := []agent.Message{
		{Role: "user", Content: "run it"},
		{Role: "assistant", ToolCalls: []agent.ToolCall{tc}},
		{Role: "tool", ToolID: "call_1", Content: "output"},
		// Orphan: a tool result with no matching assistant tool_call.
		{Role: "tool", ToolID: "call_orphan", Content: "stranded"},
	}
	if err := SaveForDir(root, oldID, "", msgs, nil); err != nil {
		t.Fatal(err)
	}

	newID, err := RekeyForDir(root, oldID)
	if err != nil {
		t.Fatalf("RekeyForDir: %v", err)
	}
	raw, err := loadRawSessionFromDir(mustStorageDir(t, root), newID)
	if err != nil || raw == nil {
		t.Fatalf("raw load: %v", err)
	}
	found := false
	for _, m := range raw.Messages {
		if m.ToolID == "call_orphan" {
			found = true
		}
	}
	if !found {
		t.Fatalf("orphan tool row dropped by rekey: %#v", raw.Messages)
	}
}

func mustStorageDir(t *testing.T, root string) string {
	t.Helper()
	dir, err := GetStorageDirForPath(root)
	if err != nil {
		t.Fatal(err)
	}
	return dir
}
