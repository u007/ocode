package tool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchTools(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "search-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	// Create test structure
	os.MkdirAll("dir1/subdir", 0755)
	os.WriteFile("file1.txt", []byte("apple\nbanana"), 0644)
	os.WriteFile("dir1/file2.txt", []byte("cherry\ndate"), 0644)
	os.WriteFile("dir1/subdir/file3.log", []byte("eggplant"), 0644)

	// Test Glob **
	globTool := GlobTool{}
	globArgs, _ := json.Marshal(map[string]string{"pattern": "**/*.txt"})
	res, err := globTool.Execute(globArgs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res, "file1.txt") || !strings.Contains(res, "dir1/file2.txt") {
		t.Errorf("glob **/*.txt failed, got: %s", res)
	}

	// Test Grep
	grepTool := GrepTool{}
	grepArgs, _ := json.Marshal(map[string]string{"pattern": "cherry"})
	res, err = grepTool.Execute(grepArgs)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(res, "dir1/file2.txt:1:cherry") {
		t.Errorf("grep failed, got: %s", res)
	}
}

func contains(s, substr string) bool {
	return strings.Contains(filepath.ToSlash(s), filepath.ToSlash(substr))
}
