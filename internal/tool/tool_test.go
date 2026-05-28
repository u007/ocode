package tool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestToolResultCacheDirWindowsFallbackUsesLocalAppData(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-specific path behavior")
	}
	base := filepath.Join(`C:\`, "Users", "tester", "AppData", "Local")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("LOCALAPPDATA", base)
	if got, want := toolResultCacheDir(), filepath.Join(base, "opencode", "tool-results"); got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestFileTools(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tool-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origWd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWd) //nolint:errcheck

	content := "hello world"

	writeTool := WriteTool{}
	writeArgs, _ := json.Marshal(map[string]string{
		"path":    "test.txt",
		"content": content,
	})
	if _, err = writeTool.Execute(writeArgs); err != nil {
		t.Fatalf("WriteTool failed: %v", err)
	}

	readTool := ReadTool{}
	readArgs, _ := json.Marshal(map[string]string{
		"path": "test.txt",
	})
	res, err := readTool.Execute(readArgs)
	if err != nil {
		t.Fatalf("ReadTool failed: %v", err)
	}
	wantRead := "1\thello world\n"
	if res != wantRead {
		t.Errorf("expected %s, got %s", wantRead, res)
	}

	badArgs, _ := json.Marshal(map[string]string{"path": "../../../etc/passwd"})
	if _, err := readTool.Execute(badArgs); err == nil {
		t.Error("expected error for path traversal, got nil")
	}
}

func TestConfinedPathAllowsConfiguredExtraRoot(t *testing.T) {
	workspace := t.TempDir()
	extra := t.TempDir()

	origWd, _ := os.Getwd()
	if err := os.Chdir(workspace); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWd) //nolint:errcheck

	setExtraAllowedPaths([]string{extra})
	t.Cleanup(func() { setExtraAllowedPaths(nil) })

	target := filepath.Join(extra, "allowed.txt")
	if err := os.WriteFile(target, []byte("ok"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := confinedPath(target); err != nil {
		t.Fatalf("expected path to be allowed, got error: %v", err)
	}
}

func TestAddExtraAllowedPath_AllowsNonexistentRootThenConfinesInsideIt(t *testing.T) {
	workspace := t.TempDir()
	extraBase := t.TempDir()
	nonexistentRoot := filepath.Join(extraBase, "newdir")

	origWd, _ := os.Getwd()
	if err := os.Chdir(workspace); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWd) //nolint:errcheck

	setExtraAllowedPaths(nil)
	t.Cleanup(func() { setExtraAllowedPaths(nil) })

	if ok := AddExtraAllowedPath(nonexistentRoot); !ok {
		t.Fatalf("expected AddExtraAllowedPath to accept nonexistent root %q", nonexistentRoot)
	}
	if err := os.MkdirAll(nonexistentRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(nonexistentRoot, "allowed.txt")
	if err := os.WriteFile(target, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := confinedPath(target); err != nil {
		t.Fatalf("expected path under newly-added root to be allowed, got error: %v", err)
	}
}

func TestRemoveExtraAllowedPath_RemovesOnlyAddedRoot(t *testing.T) {
	workspace := t.TempDir()
	extra := t.TempDir()

	origWd, _ := os.Getwd()
	if err := os.Chdir(workspace); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWd) //nolint:errcheck

	setExtraAllowedPaths(nil)
	t.Cleanup(func() { setExtraAllowedPaths(nil) })

	if ok := AddExtraAllowedPath(extra); !ok {
		t.Fatalf("expected AddExtraAllowedPath to succeed for %q", extra)
	}
	if !HasExtraAllowedPath(extra) {
		t.Fatalf("expected %q in extra allowlist", extra)
	}
	if !RemoveExtraAllowedPath(extra) {
		t.Fatalf("expected %q to be removed", extra)
	}
	if HasExtraAllowedPath(extra) {
		t.Fatalf("did not expect %q in allowlist after removal", extra)
	}
}

func TestTemporaryAllowedPath_ReferenceCounted(t *testing.T) {
	setExtraAllowedPaths(nil)
	t.Cleanup(func() { setExtraAllowedPaths(nil) })

	root := filepath.Join(t.TempDir(), "outside")
	if ok := AcquireTemporaryAllowedPath(root); !ok {
		t.Fatalf("expected first temporary acquire to succeed")
	}
	if ok := AcquireTemporaryAllowedPath(root); !ok {
		t.Fatalf("expected second temporary acquire to succeed")
	}
	if !HasExtraAllowedPath(root) {
		t.Fatalf("expected %q to be temporarily allowed", root)
	}
	if !ReleaseTemporaryAllowedPath(root) {
		t.Fatalf("expected first temporary release to succeed")
	}
	if !HasExtraAllowedPath(root) {
		t.Fatalf("expected %q to remain allowed after one release", root)
	}
	if !ReleaseTemporaryAllowedPath(root) {
		t.Fatalf("expected second temporary release to succeed")
	}
	if HasExtraAllowedPath(root) {
		t.Fatalf("did not expect %q to remain allowed after final release", root)
	}
}

func TestTemporaryRelease_DoesNotRemovePersistentRoot(t *testing.T) {
	setExtraAllowedPaths(nil)
	t.Cleanup(func() { setExtraAllowedPaths(nil) })

	root := filepath.Join(t.TempDir(), "outside")
	if ok := AddExtraAllowedPath(root); !ok {
		t.Fatalf("expected persistent add to succeed")
	}
	if ok := AcquireTemporaryAllowedPath(root); !ok {
		t.Fatalf("expected temporary acquire to succeed")
	}
	if !ReleaseTemporaryAllowedPath(root) {
		t.Fatalf("expected temporary release to succeed")
	}
	if !HasExtraAllowedPath(root) {
		t.Fatalf("expected persistent root %q to remain allowed", root)
	}
}

func TestMultiEditToolSequentialReplaceAndDiff(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWd) //nolint:errcheck

	path := "sample.txt"
	if err := os.WriteFile(path, []byte("alpha\nbeta\nalpha\n"), 0644); err != nil {
		t.Fatal(err)
	}

	tool := MultiEditTool{}
	args, _ := json.Marshal(map[string]interface{}{
		"file_path": path,
		"edits": []map[string]interface{}{
			{"oldString": "beta", "newString": "gamma"},
			{"oldString": "alpha", "newString": "delta", "replaceAll": true},
		},
	})

	res, err := tool.Execute(args)
	if err != nil {
		t.Fatalf("MultiEditTool failed: %v", err)
	}
	if !strings.HasPrefix(res, "DIFF:"+path) {
		t.Fatalf("expected diff output, got %q", res)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "delta\ngamma\ndelta\n" {
		t.Fatalf("unexpected file contents: %q", string(got))
	}
}

func TestMultiEditToolCreateNewFile(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWd) //nolint:errcheck

	path := "new.txt"
	tool := MultiEditTool{}
	args, _ := json.Marshal(map[string]interface{}{
		"file_path": path,
		"edits": []map[string]interface{}{
			{"oldString": "", "newString": "hello\nworld\n"},
			{"oldString": "world", "newString": "gopher"},
		},
	})

	if _, err := tool.Execute(args); err != nil {
		t.Fatalf("MultiEditTool create failed: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello\ngopher\n" {
		t.Fatalf("unexpected created file contents: %q", string(got))
	}
}

func TestMultiEditToolMultipleMatchesRequireReplaceAll(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWd) //nolint:errcheck

	path := "repeat.txt"
	if err := os.WriteFile(path, []byte("x\nx\n"), 0644); err != nil {
		t.Fatal(err)
	}

	tool := MultiEditTool{}
	args, _ := json.Marshal(map[string]interface{}{
		"file_path": path,
		"edits": []map[string]interface{}{
			{"oldString": "x", "newString": "y"},
		},
	})

	_, err := tool.Execute(args)
	if err == nil || !strings.Contains(err.Error(), "Found multiple matches for oldString") {
		t.Fatalf("expected multiple match error, got %v", err)
	}
}

func TestMultiFileEditToolAcrossFiles(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWd) //nolint:errcheck

	if err := os.WriteFile("a.txt", []byte("foo\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("b.txt", []byte("bar\nbar\n"), 0644); err != nil {
		t.Fatal(err)
	}

	tool := MultiFileEditTool{}
	args, _ := json.Marshal(map[string]interface{}{
		"edits": []map[string]interface{}{
			{"path": "a.txt", "search": "foo", "replace": "baz"},
			{"path": "b.txt", "search": "bar", "replace": "qux", "replace_all": true},
		},
	})

	res, err := tool.Execute(args)
	if err != nil {
		t.Fatalf("MultiFileEditTool failed: %v", err)
	}
	if !strings.Contains(res, "Successfully performed 2 edits across 2 file(s)") {
		t.Fatalf("unexpected result: %q", res)
	}

	gotA, _ := os.ReadFile("a.txt")
	gotB, _ := os.ReadFile("b.txt")
	if string(gotA) != "baz\n" || string(gotB) != "qux\nqux\n" {
		t.Fatalf("unexpected contents: a=%q b=%q", string(gotA), string(gotB))
	}
}
