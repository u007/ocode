package tool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestFileTools(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tool-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "test.txt")
	content := "hello world"

	// Test Write
	writeTool := WriteTool{}
	writeArgs, _ := json.Marshal(map[string]string{
		"path":    testFile,
		"content": content,
	})
	_, err = writeTool.Execute(writeArgs)
	if err != nil {
		t.Fatalf("WriteTool failed: %v", err)
	}

	// Test Read
	readTool := ReadTool{}
	readArgs, _ := json.Marshal(map[string]string{
		"path": testFile,
	})
	res, err := readTool.Execute(readArgs)
	if err != nil {
		t.Fatalf("ReadTool failed: %v", err)
	}
	if res != content {
		t.Errorf("expected %s, got %s", content, res)
	}
}
