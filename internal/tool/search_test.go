package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	withoutRipgrep(t)
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

// TestSearchTools_CancelPromptly proves glob, grep (fallback walker) and list
// stop as soon as the caller's context is cancelled: a pre-cancelled call
// returns the cancellation with no output, and a cancel fired from inside the
// walk stops the walk before it visits another entry.
func TestSearchTools_CancelPromptly(t *testing.T) {
	dir := t.TempDir()
	for d := 0; d < 40; d++ {
		sub := filepath.Join(dir, fmt.Sprintf("d%02d", d))
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		for f := 0; f < 200; f++ {
			if err := os.WriteFile(filepath.Join(sub, fmt.Sprintf("f%03d.txt", f)), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	prevRg := rgBinResolver
	rgBinResolver = func() string { return "" } // force the in-process walker
	t.Cleanup(func() { rgBinResolver = prevRg })

	base := WithWorkDir(context.Background(), dir)
	tools := []struct {
		name string
		t    interface {
			ExecuteCtx(context.Context, json.RawMessage) (string, error)
		}
		args string
	}{
		{"glob", GlobTool{}, `{"pattern":"**/*.txt"}`},
		{"grep", GrepTool{}, `{"pattern":"x","intent":"test"}`},
		{"list", ListTool{}, `{"path":"d00"}`},
	}

	for _, tc := range tools {
		t.Run(tc.name+"/pre_cancelled", func(t *testing.T) {
			ctx, cancel := context.WithCancel(base)
			cancel()
			out, err := tc.t.ExecuteCtx(ctx, json.RawMessage(tc.args))
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("err = %v, want context.Canceled", err)
			}
			if out != "" {
				t.Fatalf("cancelled call returned output %q", out)
			}
		})
	}

	// Cancel from inside the walk via the search policy's Blocked callback:
	// the walk must not visit any entry after the cancel fires.
	for _, tc := range []struct {
		name string
		t    interface {
			ExecuteCtx(context.Context, json.RawMessage) (string, error)
		}
		args string
	}{
		{"glob", GlobTool{}, `{"pattern":"**/*.txt"}`},
		{"grep", GrepTool{}, `{"pattern":"x","intent":"test"}`},
		{"list", ListTool{}, `{"path":"d00"}`},
	} {
		t.Run(tc.name+"/mid_walk", func(t *testing.T) {
			ctx, cancel := context.WithCancel(base)
			defer cancel()
			var calls, afterCancel int
			cancelled := false
			pol := SearchPolicy{Blocked: func(string) bool {
				if cancelled {
					afterCancel++
				}
				calls++
				if calls == 25 {
					cancel()
					cancelled = true
				}
				return false
			}}
			out, err := tc.t.ExecuteCtx(WithSearchPolicy(ctx, pol), json.RawMessage(tc.args))
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("err = %v, want context.Canceled", err)
			}
			if out != "" {
				t.Fatalf("cancelled walk returned output %q", out)
			}
			if afterCancel != 0 {
				t.Fatalf("walk visited %d entries after cancel", afterCancel)
			}
		})
	}
}

// TestGrepFallback_BlockedBeforeOpen drives the in-process grep walker with a
// policy that blocks a FIFO. Opening a FIFO blocks until a writer appears, so
// if the walker opened the blocked entry this test would hang instead of
// returning.
func TestGrepFallback_BlockedBeforeOpen(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mkfifo is POSIX-only")
	}
	dir := t.TempDir()
	if out, err := exec.Command("mkfifo", filepath.Join(dir, "pipe.txt")).CombinedOutput(); err != nil {
		t.Fatalf("mkfifo: %v: %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(dir, "ok.txt"), []byte("needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prevRg := rgBinResolver
	rgBinResolver = func() string { return "" }
	t.Cleanup(func() { rgBinResolver = prevRg })

	ctx := WithSearchPolicy(WithWorkDir(context.Background(), dir), SearchPolicy{
		Blocked: func(p string) bool { return filepath.Base(p) == "pipe.txt" },
	})
	out, err := GrepTool{}.ExecuteCtx(ctx, json.RawMessage(`{"pattern":"needle","intent":"test"}`))
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if !contains(out, "ok.txt") {
		t.Fatalf("expected ok.txt match, got %q", out)
	}
}
