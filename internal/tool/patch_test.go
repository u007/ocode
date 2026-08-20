package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/changes"
	"github.com/u007/ocode/internal/snapshot"
)

func TestPatchExecuteAllowsTempDirTargetsOutsideWorkspace(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	externalDir := t.TempDir()
	externalFile := filepath.Join(externalDir, "outside.txt")
	if err := os.WriteFile(externalFile, []byte("outside\n"), 0644); err != nil {
		t.Fatal(err)
	}

	tool := PatchTool{}
	store := snapshot.NewStore("main", filepath.Join(tmpDir, ".opencode", "snapshots"))
	ctx := snapshot.WithStore(context.Background(), store)
	args, err := json.Marshal(map[string]string{
		"patchText": "*** Begin Patch\n*** Update File: " + externalFile + "\n@@ \n-outside\n+inside\n*** End Patch\n",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = tool.ExecuteCtx(ctx, args)
	if err != nil {
		t.Fatalf("expected patch execution to succeed for temp-dir path, got %v", err)
	}

	got, err := os.ReadFile(externalFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "inside\n" {
		t.Fatalf("expected patched file to contain inside, got %q", string(got))
	}

	entries, err := os.ReadDir(filepath.Join(tmpDir, ".opencode", "snapshots"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatalf("expected snapshots to be created for patch execution")
	}
}

func TestPatchExecuteRollsBackSnapshotsOnFailure(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile("inside.txt", []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}

	tool := PatchTool{}
	store := snapshot.NewStore("main", filepath.Join(tmpDir, ".opencode", "snapshots"))
	ctx := snapshot.WithStore(context.Background(), store)
	ctx = snapshot.WithToolCallID(ctx, "tc-rollback")
	// "-world" won't match "hello" so deriveNewContents will fail after snapshot.
	args, err := json.Marshal(map[string]string{
		"patchText": "*** Begin Patch\n*** Update File: inside.txt\n@@ \n-world\n+planet\n*** End Patch\n",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = tool.ExecuteCtx(ctx, args)
	if err == nil {
		t.Fatal("expected patch execution to fail")
	}

	entries, err := os.ReadDir(filepath.Join(tmpDir, ".opencode", "snapshots"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected rollback to leave no snapshots, got %d", len(entries))
	}
}

func TestPatchAddFile(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	tool := PatchTool{}
	args, _ := json.Marshal(map[string]string{
		"patchText": "*** Begin Patch\n*** Add File: hello.txt\n+Hello world\n*** End Patch\n",
	})
	result, err := tool.Execute(args)
	if err != nil {
		t.Fatal(err)
	}
	if result == "" {
		t.Fatal("expected non-empty result")
	}
	content, err := os.ReadFile(filepath.Join(tmpDir, "hello.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "Hello world\n" {
		t.Fatalf("unexpected content: %q", content)
	}
}

func TestPatchAddFileAppearsInChangesRegistry(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	store := snapshot.NewStore("main", filepath.Join(tmpDir, "snapshots"))
	ctx := snapshot.WithToolCallID(
		snapshot.WithStore(context.Background(), store),
		"tc-add",
	)
	args, err := json.Marshal(map[string]string{
		"patchText": "*** Begin Patch\n*** Add File: added.txt\n+Hello world\n*** End Patch\n",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := (PatchTool{}).ExecuteCtx(ctx, args); err != nil {
		t.Fatalf("apply_patch failed: %v", err)
	}

	path, err := filepath.Abs("added.txt")
	if err != nil {
		t.Fatal(err)
	}
	registry := changes.NewRegistry()
	if err := registry.AttachSnapshotStore("main", store); err != nil {
		t.Fatal(err)
	}
	got := registry.List()
	if len(got) != 1 {
		t.Fatalf("expected added file in changes registry, got %d entries: %+v", len(got), got)
	}
	if got[0].OriginalPath != path {
		t.Fatalf("expected tracked path %q, got %q", path, got[0].OriginalPath)
	}
	if got[0].Status != changes.FileAdded {
		t.Errorf("expected added status, got %v", got[0].Status)
	}
	if got[0].UndoAllTCID != "tc-add" {
		t.Errorf("expected tool call id tc-add, got %q", got[0].UndoAllTCID)
	}
}

func TestPatchMoveTracksBothPathsInChangesRegistry(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("source.txt", []byte("old\n"), 0644); err != nil {
		t.Fatal(err)
	}

	store := snapshot.NewStore("main", filepath.Join(tmpDir, "snapshots"))
	ctx := snapshot.WithToolCallID(
		snapshot.WithStore(context.Background(), store),
		"tc-move",
	)
	args, err := json.Marshal(map[string]string{
		"patchText": "*** Begin Patch\n*** Update File: source.txt\n*** Move to: destination.txt\n@@ \n-old\n+new\n*** End Patch\n",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := (PatchTool{}).ExecuteCtx(ctx, args); err != nil {
		t.Fatalf("apply_patch move failed: %v", err)
	}

	registry := changes.NewRegistry()
	if err := registry.AttachSnapshotStore("main", store); err != nil {
		t.Fatal(err)
	}
	got := registry.List()
	if len(got) != 2 {
		t.Fatalf("expected both move paths in changes registry, got %d entries: %+v", len(got), got)
	}
	statuses := make(map[string]changes.FileStatus, len(got))
	for _, file := range got {
		statuses[file.OriginalPath] = file.Status
	}
	source, _ := filepath.Abs("source.txt")
	destination, _ := filepath.Abs("destination.txt")
	if statuses[source] != changes.FileDeleted {
		t.Errorf("expected source status FileDeleted, got %v", statuses[source])
	}
	if statuses[destination] != changes.FileAdded {
		t.Errorf("expected destination status FileAdded, got %v", statuses[destination])
	}
}

func TestPatchRollbackRemovesAddedFilesOnFailure(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile("inside.txt", []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}

	tool := PatchTool{}
	args, err := json.Marshal(map[string]string{
		"patchText": "*** Begin Patch\n*** Add File: hello.txt\n+Hello world\n*** Update File: inside.txt\n@@ \n-world\n+planet\n*** End Patch\n",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = tool.Execute(args)
	if err == nil {
		t.Fatal("expected patch execution to fail")
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "hello.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected added file to be removed during rollback, stat err=%v", err)
	}
}

func TestPatchUpdateFile(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile("greet.txt", []byte("Hello\nWorld\n"), 0644); err != nil {
		t.Fatal(err)
	}

	tool := PatchTool{}
	args, _ := json.Marshal(map[string]string{
		"patchText": "*** Begin Patch\n*** Update File: greet.txt\n@@ \n-World\n+Planet\n*** End Patch\n",
	})
	_, err := tool.Execute(args)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(tmpDir, "greet.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "Hello\nPlanet\n" {
		t.Fatalf("unexpected content: %q", content)
	}
}

func TestPatchDeleteFile(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile("bye.txt", []byte("bye\n"), 0644); err != nil {
		t.Fatal(err)
	}

	tool := PatchTool{}
	args, _ := json.Marshal(map[string]string{
		"patchText": "*** Begin Patch\n*** Delete File: bye.txt\n*** End Patch\n",
	})
	_, err := tool.Execute(args)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "bye.txt")); !os.IsNotExist(err) {
		t.Fatal("expected file to be deleted")
	}
}

func TestPatchRejectsMissingMarkers(t *testing.T) {
	tool := PatchTool{}
	args, _ := json.Marshal(map[string]string{
		"patchText": "diff --git a/foo.txt b/foo.txt\n--- a/foo.txt\n+++ b/foo.txt\n@@ -1 +1 @@\n-old\n+new\n",
	})
	_, err := tool.Execute(args)
	if err == nil {
		t.Fatal("expected error for patch without Begin/End markers")
	}
}
