package agent

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/tool"
)

func chdirTemp(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	origWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWd) })
	resetTodoStoreForTest()
}

func resetTodoStoreForTest() {
	// Test-local copy: reach in via SetTodoSession("") and ResetTodoState
	// to clear the in-memory map for the current goroutine. Subsequent
	// SetTodoSession("x") then loads cleanly.
	tool.SetTodoSession("")
	tool.ResetTodoState()
}

func TestInjectTodoTailUserRoleWithMarker(t *testing.T) {
	chdirTemp(t)
	tool.SetTodoSession("ses-inject")
	tool.TodoWriteTool{}.Execute(mustRawJSON(t, map[string]any{"revision": 0, "todoText": "- [ ] first\n- [✓] done"}))

	msgs := []Message{{Role: "user", Content: "what now?"}}
	out := injectTodoTail(msgs)

	if len(out) != len(msgs)+1 {
		t.Fatalf("expected one injected message, got %d (was %d)", len(out), len(msgs))
	}
	added := out[len(out)-1]
	if added.Role != "user" {
		t.Fatalf("expected user-role block (NOT system-role), got role=%q", added.Role)
	}
	if !strings.HasPrefix(added.Content, "[ocode:todo]") {
		t.Fatalf("expected [ocode:todo] marker, got %q", added.Content[:min(60, len(added.Content))])
	}
	if !strings.Contains(added.Content, "- [ ] t1 first") {
		t.Fatalf("expected injected block to carry the open item, got %q", added.Content)
	}
	if !strings.Contains(added.Content, "revision 1") {
		t.Fatalf("expected injected block to carry the revision, got %q", added.Content)
	}
	// System-role messages must NOT carry the todo content anywhere.
	for _, m := range out {
		if m.Role == "system" && strings.Contains(m.Content, "[ocode:todo]") {
			t.Fatalf("system-role message carried the todo marker — would bust the cached system block: %q", m.Content)
		}
	}
}

func TestInjectTodoTailSkipsWhenAllDone(t *testing.T) {
	chdirTemp(t)
	tool.SetTodoSession("ses-done")
	tool.TodoWriteTool{}.Execute(mustRawJSON(t, map[string]any{"revision": 0, "todoText": "- [✓] one\n- [✓] two"}))

	msgs := []Message{{Role: "user", Content: "?"}}
	out := injectTodoTail(msgs)
	if len(out) != len(msgs) {
		t.Fatalf("expected no injection when all items are done, got %d (was %d)", len(out), len(msgs))
	}
}

func TestInjectTodoTailSkipsWhenEmpty(t *testing.T) {
	chdirTemp(t)
	tool.SetTodoSession("ses-empty")

	msgs := []Message{{Role: "user", Content: "?"}}
	out := injectTodoTail(msgs)
	if len(out) != len(msgs) {
		t.Fatalf("expected no injection when no todo exists, got %d", len(out))
	}
}

func mustRawJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
