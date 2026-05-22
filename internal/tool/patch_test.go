package tool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPatchExecuteRejectsOutsideWorkspaceTargets(t *testing.T) {
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
	args, err := json.Marshal(map[string]string{
		"patchText": "*** Begin Patch\n*** Update File: " + externalFile + "\n@@ \n-outside\n+inside\n*** End Patch\n",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = tool.Execute(args)
	if err == nil {
		t.Fatal("expected patch execution to fail for external path")
	}

	entries, err := os.ReadDir(filepath.Join(tmpDir, ".opencode", "snapshots"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no snapshots, got %d", len(entries))
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
	// "-world" won't match "hello" so deriveNewContents will fail after snapshot.
	args, err := json.Marshal(map[string]string{
		"patchText": "*** Begin Patch\n*** Update File: inside.txt\n@@ \n-world\n+planet\n*** End Patch\n",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = tool.Execute(args)
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
