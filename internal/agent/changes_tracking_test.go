package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/tool"
)

// TestApprovedToolCallWithEmptyIDTracksInChanges reproduces a reported bug:
// tool writes executed with an empty toolCallID (e.g. HandleApprovedToolCall
// called without the original tool_call id) silently fall back to the
// package-level global snapshot store instead of the agent's own store, so
// the write never appears in the Changes tab even though it succeeded on
// disk. Approving/executing with a real toolCallID must show the change.
func TestApprovedToolCallWithEmptyIDTracksInChanges(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "foo.go")
	if err := os.WriteFile(target, []byte("package foo\n\nfunc A() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	a := NewAgent(nil, []tool.Tool{tool.PatchTool{}}, nil, nil)
	a.SetWorkDir(dir)

	patchText := "*** Begin Patch\n*** Update File: foo.go\n@@\n-func A() {}\n+func A() { println(\"hi\") }\n*** End Patch"
	args := []byte(`{"patchText":` + `"` + escapeJSON(patchText) + `"}`)

	// toolCallID is empty here on purpose: HandleApprovedToolCall (and other
	// direct/test callers per its own doc comment) may be invoked without the
	// originating tool_call id. The write must still land in this agent's own
	// snapshot store — not the disconnected package-level global store — so
	// it surfaces in the Changes tab.
	if _, err := a.HandleApprovedToolCall("apply_patch", args, ""); err != nil {
		t.Fatalf("apply_patch failed: %v", err)
	}

	files := a.Changes().List()
	if len(files) != 1 {
		t.Fatalf("expected 1 tracked file after approved call with empty toolCallID, got %d: %+v", len(files), files)
	}
}

func escapeJSON(s string) string {
	out := make([]byte, 0, len(s))
	for _, c := range []byte(s) {
		switch c {
		case '"':
			out = append(out, '\\', '"')
		case '\\':
			out = append(out, '\\', '\\')
		case '\n':
			out = append(out, '\\', 'n')
		default:
			out = append(out, c)
		}
	}
	return string(out)
}
