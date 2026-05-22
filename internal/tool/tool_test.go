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
