package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/snapshot"
)

// withTempWorkdir changes into a fresh temp dir for the duration of the test.
// confinedPath (and the .opencode/snapshots/ directory) resolve relative to
// os.Getwd(), so this scopes the test to avoid touching the real workspace.
func withTempWorkdir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	origWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origWd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestUndoTool_RoundTrip(t *testing.T) {
	_ = withTempWorkdir(t)
	store := snapshot.NewStore(snapshot.NewAgentID(), "")
	ctx := snapshot.WithStore(context.Background(), store)
	ctx = snapshot.WithToolCallID(ctx, "write-tc1")

	if err := os.WriteFile("a.txt", []byte("original\n"), 0644); err != nil {
		t.Fatal(err)
	}

	wt := WriteTool{}
	if _, err := wt.ExecuteCtx(ctx, json.RawMessage(`{"path":"a.txt","content":"modified\n"}`)); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := os.ReadFile("a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "modified\n" {
		t.Fatalf("post-write content = %q, want modified", got)
	}

	undo := &UndoTool{}
	res, err := undo.ExecuteCtx(ctx, json.RawMessage(`{"tool_call_id":"write-tc1"}`))
	if err != nil {
		t.Fatalf("undo: %v", err)
	}
	if res == "" {
		t.Fatal("undo returned empty success string")
	}

	got, err = os.ReadFile("a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original\n" {
		t.Fatalf("post-undo content = %q, want original", got)
	}
}

func TestUndoTool_DeleteRoundTrip(t *testing.T) {
	_ = withTempWorkdir(t)
	store := snapshot.NewStore(snapshot.NewAgentID(), "")
	ctx := snapshot.WithStore(context.Background(), store)
	ctx = snapshot.WithToolCallID(ctx, "delete-tc1")

	if err := os.WriteFile("b.txt", []byte("payload\n"), 0644); err != nil {
		t.Fatal(err)
	}

	dt := DeleteTool{}
	if _, err := dt.ExecuteCtx(ctx, json.RawMessage(`{"path":"b.txt"}`)); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat("b.txt"); !os.IsNotExist(err) {
		t.Fatalf("expected b.txt gone, stat err = %v", err)
	}

	undo := &UndoTool{}
	if _, err := undo.ExecuteCtx(ctx, json.RawMessage(`{"tool_call_id":"delete-tc1"}`)); err != nil {
		t.Fatalf("undo: %v", err)
	}
	got, err := os.ReadFile("b.txt")
	if err != nil {
		t.Fatalf("expected b.txt restored, err = %v", err)
	}
	if string(got) != "payload\n" {
		t.Fatalf("restored content = %q, want payload", got)
	}
}

func TestUndoTool_RequiresToolCallID(t *testing.T) {
	_ = withTempWorkdir(t)
	undo := &UndoTool{}
	_, err := undo.Execute(json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error for empty tool_call_id")
	}
}

func TestUndoTool_UnknownToolCallID(t *testing.T) {
	_ = withTempWorkdir(t)
	undo := &UndoTool{}
	_, err := undo.Execute(json.RawMessage(`{"tool_call_id":"never-happened"}`))
	if err == nil {
		t.Fatal("expected error for unknown tool_call_id")
	}
}

func TestMultiFileEdit_AtomicRollbackUsesPerAgentStore(t *testing.T) {
	_ = withTempWorkdir(t)
	store := snapshot.NewStore(snapshot.NewAgentID(), "")
	ctx := snapshot.WithStore(context.Background(), store)
	ctx = snapshot.WithToolCallID(ctx, "mfe-tc1")

	if err := os.WriteFile("x.txt", []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// A multi_file_edit with a non-existent search target should fail BEFORE
	// any file is written — the search-block check short-circuits. Use a
	// valid second edit and an invalid first edit so the validation pass
	// catches it.
	mfe := MultiFileEditTool{Config: nil}
	_, err := mfe.ExecuteCtx(ctx, json.RawMessage(`{"edits":[
		{"path":"x.txt","search":"missing","replace":"world"},
		{"path":"x.txt","search":"hello","replace":"hi"}
	]}`))
	if err == nil {
		t.Fatal("expected error from missing search block")
	}
	// x.txt must be untouched.
	got, err := os.ReadFile("x.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello\n" {
		t.Fatalf("x.txt was modified despite validation failure: %q", got)
	}
	// No leftover backup files.
	entries, err := os.ReadDir(filepath.Join(".opencode", "snapshots"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no snapshot files, got %d", len(entries))
	}
}
