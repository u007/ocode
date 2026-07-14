package tool

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/lsp"
)

// expectedBuiltinTools lists the canonical tool names in the order they
// must appear. Any addition or removal from InitBuiltinTools must update
// this list — it is the single source of truth for "what tools exist".
var expectedBuiltinTools = []string{
	"read",
	"undo_file_change",
	"write",
	"replace_lines",
	"delete",
	"glob",
	"grep",
	"bash",
	"edit",
	"multiedit",
	"multi_file_edit",
	"apply_patch",
	"todowrite",
	"todoread",
	"skill",
	"question",
	"webfetch",
	"websearch",
	"repo_clone",
	"repo_overview",
	"plan_enter",
	"plan_exit",
	"list",
	"lsp",
	"lsp_diagnostics",
	"format",
	"github_pr",
	"github_issue",
	"github_workflow",
	"ocr",
	"imagegen",
}

// ast and ast_grep are conditionally appended; they are tested separately
// in TestBuiltinToolsIncludesConditionalNames.

func TestInitBuiltinToolsReturnsAllTools(t *testing.T) {
	lspMgr := lsp.NewManager(".")
	tools := InitBuiltinTools(lspMgr, nil)

	// Collect unconditionally-registered names up to the conditional append
	// point. We verify the full conditional set separately below.
	got := make([]string, 0, len(tools))
	for _, tt := range tools {
		got = append(got, tt.Name())
	}

	// Check all expected tools are present (regardless of conditional adds).
	for _, want := range expectedBuiltinTools {
		found := false
		for _, g := range got {
			if g == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("InitBuiltinTools missing expected tool %q", want)
		}
	}

	// Verify no unexpected tools either.
	for _, g := range got {
		if g == "ast" || g == "ast_grep" {
			continue // handled by TestBuiltinToolsIncludesConditionalNames
		}
		found := false
		for _, want := range expectedBuiltinTools {
			if g == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("InitBuiltinTools returned unexpected tool %q", g)
		}
	}
}

func TestBuiltinToolsIncludesConditionalNames(t *testing.T) {
	lspMgr := lsp.NewManager(".")
	tools := InitBuiltinTools(lspMgr, nil)

	got := make([]string, 0, len(tools))
	for _, tt := range tools {
		got = append(got, tt.Name())
	}

	hasAST := false
	hasAstGrep := false
	for _, g := range got {
		if g == "ast" {
			hasAST = true
		}
		if g == "ast_grep" {
			hasAstGrep = true
		}
	}

	// ast is included when LSP is installed on PATH (varies per environment).
	// We can't predict it here, but verify it's present at most once.
	astCount := 0
	for _, g := range got {
		if g == "ast" {
			astCount++
		}
	}
	if astCount > 1 {
		t.Errorf("ast appears %d times (should be 0 or 1)", astCount)
	}

	// ast_grep requires plugins.ast to be enabled; nil config means disabled,
	// so it should NOT appear.
	if hasAstGrep {
		t.Error("ast_grep should not be present when config is nil (plugins.ast disabled)")
	}

	// ast is included when lsp.AnyServerInstalled() is true.
	_ = hasAST // environment-dependent; just check it didn't duplicate
}

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

// TestReadToolOffsetLimitAliases guards against the silent-pagination bug where
// a model emits Claude-Code-style {offset, limit} keys (instead of
// start_line/end_line). Previously Execute ignored them, so every paginated
// read returned lines 1..50 again — an infinite reread loop in sub-agents.
func TestReadToolOffsetLimitAliases(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tool-test-read")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origWd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWd) //nolint:errcheck

	// 100 lines: "line1".."line100".
	var b strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&b, "line%d\n", i)
	}
	writeArgs, _ := json.Marshal(map[string]string{"path": "big.txt", "content": b.String()})
	if _, err := (WriteTool{}).Execute(writeArgs); err != nil {
		t.Fatalf("WriteTool failed: %v", err)
	}

	readTool := ReadTool{}

	// offset/limit aliases must advance and bound the window: lines 51..60.
	readArgs, _ := json.Marshal(struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}{"big.txt", 51, 10})
	res, err := readTool.Execute(readArgs)
	if err != nil {
		t.Fatalf("ReadTool failed: %v", err)
	}
	if !strings.Contains(res, "51\tline51\n") {
		t.Errorf("offset=51 should return line 51, got:\n%s", res)
	}
	if strings.Contains(res, "1\tline1\n") {
		t.Errorf("offset=51 must NOT return line 1 (the reread bug), got:\n%s", res)
	}
	if strings.Contains(res, "61\tline61\n") {
		t.Errorf("limit=10 must stop at line 60, got:\n%s", res)
	}
	if !strings.Contains(res, "60\tline60\n") {
		t.Errorf("limit=10 from offset=51 should include line 60, got:\n%s", res)
	}
}

func TestReadToolImageReturnsStubNotBinary(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tool-test-img")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origWd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWd) //nolint:errcheck

	// A 3x2 PNG written straight to disk.
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("pic.png", buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	readArgs, _ := json.Marshal(map[string]string{"path": "pic.png"})

	// Text path must return a description, never split binary into "lines".
	res, err := (ReadTool{}).Execute(readArgs)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res, "image file: pic.png") || !strings.Contains(res, "3x2") || !strings.Contains(res, "image/png") {
		t.Errorf("stub missing expected fields, got: %q", res)
	}
	if strings.Contains(res, "1\t") {
		t.Errorf("image must not be rendered as numbered text lines, got: %q", res)
	}

	// ExecuteImage returns the raw bytes and sniffed mime for the vision path.
	raw, mime, err := (ReadTool{}).ExecuteImage(readArgs)
	if err != nil {
		t.Fatalf("ExecuteImage: %v", err)
	}
	if mime != "image/png" {
		t.Errorf("mime = %q, want image/png", mime)
	}
	if !bytes.Equal(raw, buf.Bytes()) {
		t.Errorf("ExecuteImage returned %d bytes, want the %d source bytes", len(raw), buf.Len())
	}
}

func TestReadToolNonImageIsNotSniffedAsImage(t *testing.T) {
	if got := sniffImageMIME([]byte("package main\n\nfunc main() {}\n")); got != "" {
		t.Errorf("Go source sniffed as image: %q", got)
	}
}

func TestWriteToolAllowsTempDirOutsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	targetRoot := t.TempDir()

	origWd, _ := os.Getwd()
	if err := os.Chdir(workspace); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWd) //nolint:errcheck

	want := "hello temp"
	target := filepath.Join(targetRoot, "temp.txt")
	args, _ := json.Marshal(map[string]string{
		"path":    target,
		"content": want,
	})

	if _, err := (WriteTool{}).Execute(args); err != nil {
		t.Fatalf("expected temp-dir write to succeed, got %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read temp file: %v", err)
	}
	if string(got) != want {
		t.Fatalf("expected %q, got %q", want, string(got))
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

func TestExpandTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home directory")
	}

	tests := []struct {
		input string
		want  string
	}{
		{"~/foo", filepath.Join(home, "foo")},
		{"~", home},
		{"~/", home},
		{"~/path/to/file.txt", filepath.Join(home, "path/to/file.txt")},
		{"./relative", "./relative"},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := expandTilde(tt.input)
			if got != tt.want {
				t.Errorf("expandTilde(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestConfinedPathExpandsTildeToToolResults(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home directory")
	}

	// confinedPath should accept ~/... paths that resolve into allowed roots
	// such as the tool-results cache directory.
	p := filepath.Join("~", ".local", "state", "opencode", "tool-results")
	got, err := confinedPath(p)
	if err != nil {
		t.Fatalf("confinedPath(%q) error: %v", p, err)
	}
	want := filepath.Join(home, ".local", "state", "opencode", "tool-results")
	if got != want {
		t.Errorf("confinedPath(%q) = %q, want %q", p, got, want)
	}
}
