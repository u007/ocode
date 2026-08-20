package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/changes"
	"github.com/u007/ocode/internal/snapshot"
)

// runReplaceLines / runWrite drive the two file-editing tools through the
// same ContextualTool path the agent loop uses, then report the changes list.
func runEditTool(t *testing.T, tool Tool, workDir, fullPath string, args map[string]interface{}) []changes.FileChange {
	t.Helper()
	store := snapshot.NewStore("main", filepath.Join(workDir, "snapshots"))
	reg := changes.NewRegistry()
	if err := reg.AttachSnapshotStore("main", store); err != nil {
		t.Fatal(err)
	}
	ctx := snapshot.WithStore(context.Background(), store)
	ctx = snapshot.WithToolCallID(ctx, "tc-1")
	ctx = WithWorkDir(ctx, workDir)

	ct, ok := tool.(ContextualTool)
	if !ok {
		t.Fatalf("tool %s does not implement ContextualTool", tool.Name())
	}
	raw, _ := json.Marshal(args)
	if _, err := ct.ExecuteCtx(ctx, raw); err != nil {
		t.Fatalf("tool %s failed: %v", tool.Name(), err)
	}
	return reg.List()
}

func TestStripLineRef(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"/a/b.go:197-224", "/a/b.go"},
		{"/a/b.go:197", "/a/b.go"},
		{"/a/b.go", "/a/b.go"},
		{"/a/b:abc", "/a/b:abc"},             // non-digit suffix untouched
		{"/a/b:1.2", "/a/b:1.2"},             // decimal untouched
		{"C:\\Users\\x:197", "C:\\Users\\x"}, // windows drive letter preserved, trailing ref stripped
		{"C:\\Users\\x", "C:\\Users\\x"},     // windows path untouched
		{"/a/b:199-", "/a/b:199-"},           // incomplete range untouched
		{"/a/b:-224", "/a/b:-224"},           // incomplete range untouched
	}
	for _, c := range cases {
		if got := stripLineRef(c.in); got != c.want {
			t.Errorf("stripLineRef(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestReplaceLinesReflectsOnChangesTab(t *testing.T) {
	tmp := t.TempDir()
	full := filepath.Join(tmp, "providers.go")
	// 5 lines so replacing 2..4 is valid.
	seed := "line1\nline2\nline3\nline4\nline5\n"
	if err := os.WriteFile(full, []byte(seed), 0644); err != nil {
		t.Fatal(err)
	}

	rl := ReplaceLinesToolImpl{}
	out := runEditTool(t, rl, tmp, full, map[string]interface{}{
		"path":       full,
		"start_line": 2,
		"end_line":   4,
		"content":    "REPLACED\n",
	})

	found := false
	for _, fc := range out {
		if fc.OriginalPath == full {
			found = true
		}
	}
	if !found {
		t.Fatalf("replace_lines edit NOT reflected on changes tab; list=%v", out)
	}
}

func TestReplaceLinesWithLineRangeInPath(t *testing.T) {
	tmp := t.TempDir()
	full := filepath.Join(tmp, "providers.go")
	seed := "line1\nline2\nline3\nline4\nline5\n"
	if err := os.WriteFile(full, []byte(seed), 0644); err != nil {
		t.Fatal(err)
	}

	rl := ReplaceLinesToolImpl{}
	store := snapshot.NewStore("main", filepath.Join(tmp, "snapshots"))
	reg := changes.NewRegistry()
	if err := reg.AttachSnapshotStore("main", store); err != nil {
		t.Fatal(err)
	}
	ctx := snapshot.WithStore(context.Background(), store)
	ctx = snapshot.WithToolCallID(ctx, "tc-1")
	ctx = WithWorkDir(ctx, tmp)

	// Agent passes the path WITH a ":start-end" suffix (vim-style), as the
	// user described: replace_lines /abs/path:197-224. The tool must strip the
	// line reference and edit the real file (so it shows on the changes tab).
	badPath := full + ":2-4"
	raw, _ := json.Marshal(map[string]interface{}{
		"path":       badPath,
		"start_line": 2,
		"end_line":   4,
		"content":    "REPLACED\n",
	})
	if _, err := rl.ExecuteCtx(ctx, raw); err != nil {
		t.Fatalf("replace_lines with line-range path failed: %v", err)
	}

	// The edit must have landed on the real file...
	data, rerr := os.ReadFile(full)
	if rerr != nil {
		t.Fatalf("real file missing: %v", rerr)
	}
	// content "REPLACED\n" splits into ["REPLACED",""] so a blank line remains
	// before line5 — that is the correct, intended behavior of replace_lines.
	want := "line1\nREPLACED\n\nline5\n"
	if string(data) != want {
		t.Fatalf("edit did not land on real file; got %q want %q", string(data), want)
	}

	// ...and must appear on the changes tab.
	found := false
	for _, fc := range reg.List() {
		if fc.OriginalPath == full {
			found = true
		}
	}
	if !found {
		t.Fatalf("replace_lines (line-range path) not reflected on changes tab; list=%v", reg.List())
	}
}

func TestWriteReflectsOnChangesTab(t *testing.T) {
	tmp := t.TempDir()
	full := filepath.Join(tmp, "providers.go")
	if err := os.WriteFile(full, []byte("old\n"), 0644); err != nil {
		t.Fatal(err)
	}

	w := WriteTool{}
	out := runEditTool(t, w, tmp, full, map[string]interface{}{
		"path":    full,
		"content": "new\n",
	})

	found := false
	for _, fc := range out {
		if fc.OriginalPath == full {
			found = true
		}
	}
	if !found {
		t.Fatalf("write edit NOT reflected on changes tab; list=%v", out)
	}
}
