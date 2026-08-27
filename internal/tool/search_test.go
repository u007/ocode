package tool

import (
	"context"
	"encoding/json"
	"fmt"
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

// .gitignore must be read from the resolved search root, not the process
// cwd — the desktop shell launched from Finder has cwd "/", so a
// cwd-relative os.ReadFile(".gitignore") silently finds nothing there and
// every search walks build output/vendor dirs unfiltered.
func TestIgnoreMatcherUsesSearchRootNotCwd(t *testing.T) {
	projDir := t.TempDir()
	otherDir := t.TempDir() // process cwd during the test

	origWd, _ := os.Getwd()
	if err := os.Chdir(otherDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWd)

	os.WriteFile(filepath.Join(projDir, ".gitignore"), []byte("ignored.txt\n"), 0644)
	os.WriteFile(filepath.Join(projDir, "ignored.txt"), []byte("cherry"), 0644)
	os.WriteFile(filepath.Join(projDir, "kept.txt"), []byte("cherry"), 0644)

	ctx := WithWorkDir(context.Background(), projDir)
	res, err := GrepTool{}.ExecuteCtx(ctx, json.RawMessage(`{"pattern":"cherry"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !contains(res, "kept.txt") {
		t.Errorf("grep did not find non-ignored match, got: %s", res)
	}
	if contains(res, "ignored.txt") {
		t.Errorf(".gitignore in project root was not honored (cwd-relative lookup bug), got: %s", res)
	}
}

// An unscoped grep (no path, no include) over a tree bigger than
// maxUnscopedFiles must stop early instead of reading every file — this is
// the guard against the desktop backend RSS spike traced to an agent issuing
// repo-wide greps with neither filter set.
func TestGrepToolUnscopedCap(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	for i := 0; i < maxUnscopedFiles+50; i++ {
		os.WriteFile(fmt.Sprintf("f%d.txt", i), []byte("unrelated content"), 0644)
	}

	res, err := GrepTool{}.Execute(json.RawMessage(`{"pattern":"nomatch"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res, fmt.Sprintf("first %d files", maxUnscopedFiles)) {
		t.Errorf("expected truncation note for unscoped cap, got: %s", res)
	}
}

// The server hosts sessions for many projects in one process, so search tools
// must anchor their default/relative paths on the session's project root
// (WithWorkDir context), never the process cwd — a desktop app launched from
// Finder has cwd "/" and a bare glob would otherwise walk the whole disk.
func TestSearchToolsUseContextWorkDir(t *testing.T) {
	projDir := t.TempDir()
	otherDir := t.TempDir() // process cwd during the test — must NOT be searched

	origWd, _ := os.Getwd()
	if err := os.Chdir(otherDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWd)

	os.MkdirAll(filepath.Join(projDir, "dir1"), 0755)
	os.WriteFile(filepath.Join(projDir, "file1.txt"), []byte("apple"), 0644)
	os.WriteFile(filepath.Join(projDir, "dir1", "file2.txt"), []byte("cherry"), 0644)
	// Decoy in the cwd: found = tool wrongly searched the process cwd.
	os.WriteFile(filepath.Join(otherDir, "decoy.txt"), []byte("cherry"), 0644)

	ctx := WithWorkDir(context.Background(), projDir)

	// Glob: default path resolves to the project root, results project-relative.
	globArgs, _ := json.Marshal(map[string]string{"pattern": "**/*.txt"})
	res, err := GlobTool{}.ExecuteCtx(ctx, globArgs)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(res, "file1.txt") || !contains(res, "dir1/file2.txt") {
		t.Errorf("glob did not search project root, got: %s", res)
	}
	if contains(res, "decoy.txt") {
		t.Errorf("glob searched process cwd instead of project root: %s", res)
	}

	// Glob: relative path param joins onto the project root and prefixes output.
	globArgs, _ = json.Marshal(map[string]string{"pattern": "*.txt", "path": "dir1"})
	res, err = GlobTool{}.ExecuteCtx(ctx, globArgs)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(res, "dir1/file2.txt") {
		t.Errorf("glob relative path param not project-rooted, got: %s", res)
	}

	// Grep: default path resolves to the project root.
	grepArgs, _ := json.Marshal(map[string]string{"pattern": "cherry"})
	res, err = GrepTool{}.ExecuteCtx(ctx, grepArgs)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(res, "dir1/file2.txt:1:cherry") {
		t.Errorf("grep did not search project root, got: %s", res)
	}
	if contains(res, "decoy.txt") {
		t.Errorf("grep searched process cwd instead of project root: %s", res)
	}

	// List: default path resolves to the project root.
	res, err = ListTool{}.ExecuteCtx(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !contains(res, "file1.txt") || !contains(res, "dir1/") {
		t.Errorf("list did not read project root, got: %s", res)
	}
	if contains(res, "decoy.txt") {
		t.Errorf("list read process cwd instead of project root: %s", res)
	}
}
