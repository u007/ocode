package tool

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/u007/ocode/internal/snapshot"
)

func TestBashTool_UndoRoundTrip_Rm(t *testing.T) {
	_ = withTempWorkdir(t)
	store := snapshot.NewStore(snapshot.NewAgentID(), "")
	ctx := snapshot.WithStore(context.Background(), store)
	ctx = snapshot.WithToolCallID(ctx, "bash-tc1")

	if err := os.WriteFile("a.txt", []byte("original\n"), 0644); err != nil {
		t.Fatal(err)
	}

	bt := BashTool{}
	if _, err := bt.ExecuteCtx(ctx, json.RawMessage(`{"command":"rm a.txt"}`)); err != nil {
		t.Fatalf("rm: %v", err)
	}
	if _, err := os.Stat("a.txt"); !os.IsNotExist(err) {
		t.Fatalf("expected a.txt to be removed, stat err = %v", err)
	}

	ut := &UndoTool{}
	if _, err := ut.ExecuteCtx(ctx, json.RawMessage(`{"tool_call_id":"bash-tc1"}`)); err != nil {
		t.Fatalf("undo: %v", err)
	}

	got, err := os.ReadFile("a.txt")
	if err != nil {
		t.Fatalf("a.txt not restored: %v", err)
	}
	if string(got) != "original\n" {
		t.Fatalf("restored content = %q, want original", got)
	}
}

func TestBashTool_UndoRoundTrip_SedInPlace(t *testing.T) {
	_ = withTempWorkdir(t)
	store := snapshot.NewStore(snapshot.NewAgentID(), "")
	ctx := snapshot.WithStore(context.Background(), store)
	ctx = snapshot.WithToolCallID(ctx, "bash-tc2")

	if err := os.WriteFile("b.txt", []byte("hello world\n"), 0644); err != nil {
		t.Fatal(err)
	}

	bt := BashTool{}
	args, _ := json.Marshal(map[string]string{"command": `sed -i '' 's/hello/goodbye/' b.txt`})
	if _, err := bt.ExecuteCtx(ctx, args); err != nil {
		t.Fatalf("sed: %v", err)
	}

	got, err := os.ReadFile("b.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "goodbye world\n" {
		t.Fatalf("post-sed content = %q", got)
	}

	ut := &UndoTool{}
	if _, err := ut.ExecuteCtx(ctx, json.RawMessage(`{"tool_call_id":"bash-tc2"}`)); err != nil {
		t.Fatalf("undo: %v", err)
	}

	got, err = os.ReadFile("b.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello world\n" {
		t.Fatalf("restored content = %q, want original", got)
	}
}

func TestBashTool_NoBackupWithoutToolCallID(t *testing.T) {
	_ = withTempWorkdir(t)
	if err := os.WriteFile("c.txt", []byte("data\n"), 0644); err != nil {
		t.Fatal(err)
	}
	bt := BashTool{}
	if _, err := bt.Execute(json.RawMessage(`{"command":"rm c.txt"}`)); err != nil {
		t.Fatalf("rm: %v", err)
	}
	if _, err := os.Stat("c.txt"); !os.IsNotExist(err) {
		t.Fatalf("expected c.txt removed")
	}
	// No tool_call_id in context -> nothing to undo, and no crash.
}
