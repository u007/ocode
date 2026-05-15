package tool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPatchTargets(t *testing.T) {
	got, err := patchTargets("" +
		"diff --git a/foo.txt b/foo.txt\n" +
		"--- a/foo.txt\n" +
		"+++ b/foo.txt\n" +
		"*** Update File: bar.txt\n" +
		"*** Delete File: baz.txt\n")
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"foo.txt", "bar.txt", "baz.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestPatchTargetsKeepsSpacesInFilenames(t *testing.T) {
	got, err := patchTargets("diff --git a/docs/my file.txt b/docs/my file.txt\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"docs/my file.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

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
		"patchText": "diff --git a/" + externalFile + " b/" + externalFile + "\n--- a/" + externalFile + "\n+++ b/" + externalFile + "\n@@ -1 +1 @@\n-outside\n+inside\n",
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
	args, err := json.Marshal(map[string]string{
		"patchText": "diff --git a/inside.txt b/inside.txt\n--- a/inside.txt\n+++ b/inside.txt\n@@ -1 +1 @@\n-world\n+planet\n",
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
