package tui

import (
	"strings"
	"testing"
)

func TestToolArgsRedundant(t *testing.T) {
	for _, name := range []string{"read", "write", "bash"} {
		if !toolArgsRedundant(name) {
			t.Fatalf("toolArgsRedundant(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"grep", "glob", "edit", "apply_patch", "list", ""} {
		if toolArgsRedundant(name) {
			t.Fatalf("toolArgsRedundant(%q) = true, want false", name)
		}
	}
}

// TestRenderDetailToolRequestBoxOmitsRawArgsForReadWriteBash pins the user-facing
// contract: the detail view's "tool request" box shows the one-line
// formatToolCallHint summary for read/write/bash, without appending the raw
// argument JSON (write's JSON is dominated by the whole file body).
func TestRenderDetailToolRequestBoxOmitsRawArgsForReadWriteBash(t *testing.T) {
	cases := []struct {
		name string
		args string
	}{
		{"read", `{"path":"/src/app.ts","offset":10,"limit":20}`},
		{"write", `{"path":"/src/new.ts","content":"SECRET_BODY"}`},
		{"bash", `{"command":"ls -la /tmp"}`},
	}
	for _, tc := range cases {
		got := stripANSI(renderDetailToolRequestBox(makeToolCall(tc.name, tc.args), 120, true))
		if !strings.Contains(got, "tool request · "+tc.name) {
			t.Fatalf("%s: missing request header:\n%s", tc.name, got)
		}
		for _, leak := range []string{`"path"`, `"file_path"`, `"command"`, `"content"`, "SECRET_BODY"} {
			if strings.Contains(got, leak) {
				t.Fatalf("%s: raw args %s leaked into detail view:\n%s", tc.name, leak, got)
			}
		}
	}

	// Non-target tools keep the raw arguments: grep's pattern is quoted in the
	// hint, but its path scope is not.
	grep := stripANSI(renderDetailToolRequestBox(makeToolCall("grep", `{"pattern":"foo","path":"/src"}`), 120, true))
	if !strings.Contains(grep, `"pattern"`) {
		t.Fatalf("expected grep raw args to remain, got:\n%s", grep)
	}
}
