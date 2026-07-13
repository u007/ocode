package tui

import (
	"strings"
	"testing"

	"github.com/u007/ocode/internal/agent"
)

func makePatchCall(patchText string) agent.ToolCall {
	tc := agent.ToolCall{}
	tc.Function.Name = "apply_patch"
	args := `{"patchText":` + mustJSONString(patchText) + `}`
	tc.Function.Arguments = args
	return tc
}

func TestFormatPatchHintSingleUpdate(t *testing.T) {
	tc := makePatchCall("*** Begin Patch\n*** Update File: apps/web/foo.tsx\n@@\n- old\n+ new\n*** End Patch")
	got := formatToolCallHint(tc)
	want := "✏  patch apps/web/foo.tsx"
	if got != want {
		t.Fatalf("single update hint:\n got %q\nwant %q", got, want)
	}
}

func TestFormatPatchHintAddAndDelete(t *testing.T) {
	add := makePatchCall("*** Begin Patch\n*** Add File: a.txt\n+hello\n*** End Patch")
	if got := formatToolCallHint(add); got != "✏  create a.txt" {
		t.Fatalf("add hint: got %q", got)
	}
	del := makePatchCall("*** Begin Patch\n*** Delete File: b.txt\n*** End Patch")
	if got := formatToolCallHint(del); got != "✏  delete b.txt" {
		t.Fatalf("delete hint: got %q", got)
	}
}

func TestFormatPatchHintRename(t *testing.T) {
	tc := makePatchCall("*** Begin Patch\n*** Update File: a.ts\n*** Move to: b.ts\n@@\n- x\n+ y\n*** End Patch")
	got := formatToolCallHint(tc)
	want := "✏  patch a.ts -> b.ts"
	if got != want {
		t.Fatalf("rename hint:\n got %q\nwant %q", got, want)
	}
}

func TestFormatPatchHintMultipleFiles(t *testing.T) {
	tc := makePatchCall("*** Begin Patch\n*** Add File: a.txt\n+x\n*** Delete File: b.txt\n*** End Patch")
	got := formatToolCallHint(tc)
	want := "✏  apply_patch 2 files"
	if got != want {
		t.Fatalf("multi-file hint:\n got %q\nwant %q", got, want)
	}
}

func TestFormatPatchHintUnparseableFallsBack(t *testing.T) {
	tc := makePatchCall("not a real patch")
	got := formatToolCallHint(tc)
	if !strings.HasPrefix(got, "✏  apply_patch") {
		t.Fatalf("expected apply_patch fallback, got %q", got)
	}
}

func TestRenderPatchRequestShowsOldAndNew(t *testing.T) {
	tc := makePatchCall("*** Begin Patch\n*** Update File: apps/web/foo.tsx\n@@ some-context\n- old line\n+ new line\n*** End Patch")
	got := renderPatchRequest(tc, currentStyles())
	if !strings.Contains(got, "Update: apps/web/foo.tsx") {
		t.Fatalf("expected file header, got:\n%s", got)
	}
	if !strings.Contains(got, "- old line") {
		t.Fatalf("expected removed line, got:\n%s", got)
	}
	if !strings.Contains(got, "+ new line") {
		t.Fatalf("expected added line, got:\n%s", got)
	}
}

func TestRenderPatchRequestAdd(t *testing.T) {
	tc := makePatchCall("*** Begin Patch\n*** Add File: new.txt\n+line one\n+line two\n*** End Patch")
	got := renderPatchRequest(tc, currentStyles())
	if !strings.Contains(got, "Add: new.txt") {
		t.Fatalf("expected Add header, got:\n%s", got)
	}
	if !strings.Contains(got, "+line one") || !strings.Contains(got, "+line two") {
		t.Fatalf("expected added lines, got:\n%s", got)
	}
}

// TestRenderPatchRequestPreservesLineOrder verifies the preview renderer keeps
// context/removed/added lines in their original source order instead of
// bucketing them by type (which would misrepresent the diff).
func TestRenderPatchRequestPreservesLineOrder(t *testing.T) {
	patch := "*** Begin Patch\n" +
		"*** Update File: a.txt\n" +
		"@@ hunk\n" +
		" context1\n" +
		"- removed1\n" +
		"+ added1\n" +
		" context2\n" +
		"- removed2\n" +
		"+ added2\n" +
		"*** End Patch"
	tc := makePatchCall(patch)
	got := renderPatchRequest(tc, currentStyles())

	order := []string{" context1", "- removed1", "+ added1", " context2", "- removed2", "+ added2"}
	last := -1
	for _, marker := range order {
		idx := strings.Index(got, marker)
		if idx < 0 {
			t.Fatalf("rendered patch missing %q:\n%s", marker, got)
		}
		if idx < last {
			t.Fatalf("line order broken: %q appears before earlier line in:\n%s", marker, got)
		}
		last = idx
	}
}

func mustJSONString(s string) string {
	// Minimal JSON string escaping for test fixtures.
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
