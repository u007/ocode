package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/knowledge"
	"github.com/u007/ocode/internal/snapshot"
)

func TestDocWriteTrackedInSnapshotStore(t *testing.T) {
	td, err := os.MkdirTemp("", "doc-snap-track")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(td)
	if err := knowledge.InitBundle(td); err != nil {
		t.Fatalf("InitBundle: %v", err)
	}
	store := snapshot.NewStore("test-agent", filepath.Join(td, ".opencode", "snapshots"))
	ctx := snapshot.WithStore(context.Background(), store)
	ctx = snapshot.WithToolCallID(ctx, "tc-doc-1")

	tools, err := newDocTools(td)
	if err != nil {
		t.Fatalf("newDocTools: %v", err)
	}
	var writeTool *DocWriteTool
	for _, dt := range tools {
		if dt.Name() == "doc_write" {
			writeTool = dt.(*DocWriteTool)
			break
		}
	}
	if writeTool == nil {
		t.Fatal("doc_write not found")
	}
	args, _ := json.Marshal(map[string]string{"path": "decisions/test.md", "type": "Decision", "title": "Test", "description": "desc", "body": "body content"})
	if _, err := writeTool.ExecuteCtx(ctx, args); err != nil {
		t.Fatalf("ExecuteCtx: %v", err)
	}
	if store.Len() == 0 {
		t.Fatalf("expected snapshots after doc_write, got 0")
	}
	fullPath := filepath.Join(td, "docs", "decisions/test.md")
	found := false
	for _, f := range store.ChangedFiles() {
		if f == fullPath {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected %s in ChangedFiles, got %v", fullPath, store.ChangedFiles())
	}
	logPath := filepath.Join(td, "docs", "log.md")
	indexPath := filepath.Join(td, "docs", "index.md")
	hasLog, hasIndex := false, false
	for _, f := range store.ChangedFiles() {
		if f == logPath {
			hasLog = true
		}
		if f == indexPath {
			hasIndex = true
		}
	}
	if !hasLog {
		t.Fatalf("expected log.md in ChangedFiles, got %v", store.ChangedFiles())
	}
	if !hasIndex {
		t.Fatalf("expected index.md in ChangedFiles, got %v", store.ChangedFiles())
	}
}

func TestDocDeprecateTrackedInSnapshotStore(t *testing.T) {
	td, err := os.MkdirTemp("", "doc-snap-deprecate")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(td)
	if err := knowledge.InitBundle(td); err != nil {
		t.Fatalf("InitBundle: %v", err)
	}
	store := snapshot.NewStore("test-agent", filepath.Join(td, ".opencode", "snapshots"))
	ctx := snapshot.WithStore(context.Background(), store)
	ctx = snapshot.WithToolCallID(ctx, "tc-write-1")
	tools, err := newDocTools(td)
	if err != nil {
		t.Fatalf("newDocTools: %v", err)
	}
	var writeTool *DocWriteTool
	var depTool *DocDeprecateTool
	for _, dt := range tools {
		if dt.Name() == "doc_write" {
			writeTool = dt.(*DocWriteTool)
		}
		if dt.Name() == "doc_deprecate" {
			depTool = dt.(*DocDeprecateTool)
		}
	}
	if writeTool == nil || depTool == nil {
		t.Fatal("tools not found")
	}
	args, _ := json.Marshal(map[string]string{"path": "decisions/dep.md", "type": "Decision", "title": "ToDep", "body": "body"})
	if _, err := writeTool.ExecuteCtx(ctx, args); err != nil {
		t.Fatalf("write ExecuteCtx: %v", err)
	}
	ctx2 := snapshot.WithStore(context.Background(), store)
	ctx2 = snapshot.WithToolCallID(ctx2, "tc-dep-1")
	args2, _ := json.Marshal(map[string]string{"path": "decisions/dep.md", "reason": "outdated"})
	if _, err := depTool.ExecuteCtx(ctx2, args2); err != nil {
		t.Fatalf("deprecate ExecuteCtx: %v", err)
	}
	// Should have 2*3 snapshots (write + deprecate)
	if store.Len() != 6 {
		t.Fatalf("expected 6 snapshots (write*3+deprecate*3), got %d, snaps=%v", store.Len(), store.Snapshots())
	}
}

func TestDocWriteFallbackExecute(t *testing.T) {
	td, err := os.MkdirTemp("", "doc-snap-fallback")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(td)
	if err := knowledge.InitBundle(td); err != nil {
		t.Fatalf("InitBundle: %v", err)
	}
	tools, err := newDocTools(td)
	if err != nil {
		t.Fatalf("newDocTools: %v", err)
	}
	var writeTool *DocWriteTool
	for _, dt := range tools {
		if dt.Name() == "doc_write" {
			writeTool = dt.(*DocWriteTool)
			break
		}
	}
	if writeTool == nil {
		t.Fatal("doc_write not found")
	}
	// Fallback Execute uses Background -> globalStore, but should still write file.
	args, _ := json.Marshal(map[string]string{"path": "decisions/fallback.md", "type": "Decision", "title": "Fallback", "body": "fallback body"})
	// Ensure no panic and file created.
	snapshot.Reset()
	defer snapshot.Reset()
	if _, err := writeTool.Execute(args); err != nil {
		t.Fatalf("fallback Execute: %v", err)
	}
	if _, err := os.Stat(filepath.Join(td, "docs", "decisions/fallback.md")); err != nil {
		t.Fatalf("expected file created via fallback Execute: %v", err)
	}
}

func TestDocWriteInvalidPathNoOrphanSnapshot(t *testing.T) {
	td, err := os.MkdirTemp("", "doc-snap-invalid")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(td)
	if err := knowledge.InitBundle(td); err != nil {
		t.Fatalf("InitBundle: %v", err)
	}
	store := snapshot.NewStore("test-agent", filepath.Join(td, ".opencode", "snapshots"))
	ctx := snapshot.WithStore(context.Background(), store)
	ctx = snapshot.WithToolCallID(ctx, "tc-invalid-1")
	tools, err := newDocTools(td)
	if err != nil {
		t.Fatalf("newDocTools: %v", err)
	}
	var writeTool *DocWriteTool
	for _, dt := range tools {
		if dt.Name() == "doc_write" {
			writeTool = dt.(*DocWriteTool)
			break
		}
	}
	if writeTool == nil {
		t.Fatal("doc_write not found")
	}
	cases := []string{
		"../outside.md",
		"index.md",
		"log.md",
		"decisions/../traversal.md",
	}
	for _, bad := range cases {
		args, _ := json.Marshal(map[string]string{"path": bad, "type": "Decision", "title": "Bad"})
		_, err := writeTool.ExecuteCtx(ctx, args)
		if err == nil {
			t.Fatalf("expected error for invalid path %q, got nil", bad)
		}
	}
	if store.Len() != 0 {
		t.Fatalf("expected no snapshots for invalid paths, got %d: %v", store.Len(), store.Snapshots())
	}
	// Also check that a valid write after invalid still works.
	ctx2 := snapshot.WithStore(context.Background(), store)
	ctx2 = snapshot.WithToolCallID(ctx2, "tc-valid-after")
	args, _ := json.Marshal(map[string]string{"path": "decisions/valid-after.md", "type": "Decision", "title": "Valid"})
	if _, err := writeTool.ExecuteCtx(ctx2, args); err != nil {
		t.Fatalf("valid write after invalid: %v", err)
	}
	if store.Len() != 3 {
		t.Fatalf("expected 3 snapshots for valid write, got %d", store.Len())
	}
}

func TestDocWriteUndoRestoresTrio(t *testing.T) {
	td, err := os.MkdirTemp("", "doc-snap-undo")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(td)
	if err := knowledge.InitBundle(td); err != nil {
		t.Fatalf("InitBundle: %v", err)
	}
	store := snapshot.NewStore("test-agent", filepath.Join(td, ".opencode", "snapshots"))
	tools, err := newDocTools(td)
	if err != nil {
		t.Fatalf("newDocTools: %v", err)
	}
	var writeTool *DocWriteTool
	for _, dt := range tools {
		if dt.Name() == "doc_write" {
			writeTool = dt.(*DocWriteTool)
			break
		}
	}
	if writeTool == nil {
		t.Fatal("doc_write not found")
	}
	// Capture pre-write content of log/index
	logPath := filepath.Join(td, "docs", "log.md")
	indexPath := filepath.Join(td, "docs", "index.md")
	logBefore, _ := os.ReadFile(logPath)
	indexBefore, _ := os.ReadFile(indexPath)

	ctx := snapshot.WithStore(context.Background(), store)
	ctx = snapshot.WithToolCallID(ctx, "tc-undo-1")
	args, _ := json.Marshal(map[string]string{"path": "decisions/undo.md", "type": "Decision", "title": "UndoTest", "description": "desc", "body": "v1"})
	if _, err := writeTool.ExecuteCtx(ctx, args); err != nil {
		t.Fatalf("ExecuteCtx: %v", err)
	}
	docPath := filepath.Join(td, "docs", "decisions/undo.md")
	if _, err := os.Stat(docPath); err != nil {
		t.Fatalf("doc not created: %v", err)
	}
	// Undo via same store/tc
	restored, err := store.UndoByToolCallID("tc-undo-1", 100)
	if err != nil {
		t.Fatalf("UndoByToolCallID: %v", err)
	}
	if len(restored) != 3 {
		t.Fatalf("expected 3 restored files, got %d: %v", len(restored), restored)
	}
	if _, err := os.Stat(docPath); !os.IsNotExist(err) {
		t.Fatalf("expected doc deleted after undo, got err %v", err)
	}
	logAfter, _ := os.ReadFile(logPath)
	indexAfter, _ := os.ReadFile(indexPath)
	if string(logAfter) != string(logBefore) {
		t.Fatalf("log.md not restored: before %q after %q", string(logBefore), string(logAfter))
	}
	if string(indexAfter) != string(indexBefore) {
		t.Fatalf("index.md not restored: before %q after %q", string(indexBefore), string(indexAfter))
	}
	// Failed call should not create snapshot, so undo should fail with ErrNotFound
	ctx2 := snapshot.WithStore(context.Background(), store)
	ctx2 = snapshot.WithToolCallID(ctx2, "tc-fail-undo")
	// Use invalid path that will fail validation before backup
	argsBad, _ := json.Marshal(map[string]string{"path": "index.md", "type": "Decision", "title": "Bad"})
	if _, err := writeTool.ExecuteCtx(ctx2, argsBad); err == nil {
		t.Fatalf("expected failure for reserved path")
	}
	// No snapshot created, undo should be not found
	if _, err := store.UndoByToolCallID("tc-fail-undo", 100); err == nil {
		t.Fatalf("expected undo to fail for orphan tc, got nil")
	}
}
