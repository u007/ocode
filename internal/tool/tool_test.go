package tool

import (
	"encoding/json"
	"os"
	"testing"
)

func TestFileTools(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tool-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Change into tmpDir so confinedPath allows relative paths inside it.
	origWd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWd) //nolint:errcheck

	content := "hello world"

	// Test Write using a relative path (confined to tmpDir).
	writeTool := WriteTool{}
	writeArgs, _ := json.Marshal(map[string]string{
		"path":    "test.txt",
		"content": content,
	})
	if _, err = writeTool.Execute(writeArgs); err != nil {
		t.Fatalf("WriteTool failed: %v", err)
	}

	// Test Read
	readTool := ReadTool{}
	readArgs, _ := json.Marshal(map[string]string{
		"path": "test.txt",
	})
	res, err := readTool.Execute(readArgs)
	if err != nil {
		t.Fatalf("ReadTool failed: %v", err)
	}
	if res != content {
		t.Errorf("expected %s, got %s", content, res)
	}

	// Ensure path traversal is rejected.
	badArgs, _ := json.Marshal(map[string]string{"path": "../../../etc/passwd"})
	if _, err := readTool.Execute(badArgs); err == nil {
		t.Error("expected error for path traversal, got nil")
	}
}
