package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

// todoTestAgent returns an agent bound to a throwaway project root and session
// id — the two halves of its todoScope — so the plan it writes and re-anchors
// lives under that root, independent of the process cwd and of the TUI's
// sidebar session.
func todoTestAgent(t *testing.T, sessionID string) *Agent {
	t.Helper()
	a := newTestAgent(nil, nil, &config.Config{}, nil)
	a.SetWorkDir(t.TempDir())
	a.SetSessionID(sessionID)
	return a
}

func writeAgentTodo(t *testing.T, a *Agent, text string) {
	t.Helper()
	ctx := tool.WithTodoScope(context.Background(), a.todoScope())
	if _, err := (tool.TodoWriteTool{}).ExecuteCtx(ctx, mustRawJSON(t, map[string]any{"revision": 0, "todoText": text})); err != nil {
		t.Fatalf("todowrite: %v", err)
	}
}

func TestInjectTodoTailUserRoleWithMarker(t *testing.T) {
	a := todoTestAgent(t, "ses-inject")
	writeAgentTodo(t, a, "- [ ] first\n- [✓] done")

	msgs := []Message{{Role: "user", Content: "what now?"}}
	out := a.injectTodoTail(msgs)

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
	a := todoTestAgent(t, "ses-done")
	writeAgentTodo(t, a, "- [✓] one\n- [✓] two")

	msgs := []Message{{Role: "user", Content: "?"}}
	out := a.injectTodoTail(msgs)
	if len(out) != len(msgs) {
		t.Fatalf("expected no injection when all items are done, got %d (was %d)", len(out), len(msgs))
	}
}

func TestInjectTodoTailSkipsWhenEmpty(t *testing.T) {
	a := todoTestAgent(t, "ses-empty")

	msgs := []Message{{Role: "user", Content: "?"}}
	out := a.injectTodoTail(msgs)
	if len(out) != len(msgs) {
		t.Fatalf("expected no injection when no todo exists, got %d", len(out))
	}
}

// Two agents in one process (the server shape) each re-anchor their own plan:
// one session's open items never leak into another's tail, and neither needs
// the TUI's global current todo session.
func TestInjectTodoTailIsScopedPerAgent(t *testing.T) {
	a := todoTestAgent(t, "ses-one")
	b := todoTestAgent(t, "ses-two")
	writeAgentTodo(t, a, "- [ ] only in one")

	msgs := []Message{{Role: "user", Content: "?"}}
	if out := b.injectTodoTail(msgs); len(out) != len(msgs) {
		t.Fatalf("agent b must not re-anchor agent a's plan, got %d messages", len(out))
	}
	out := a.injectTodoTail(msgs)
	if len(out) != len(msgs)+1 || !strings.Contains(out[len(out)-1].Content, "only in one") {
		t.Fatalf("agent a must re-anchor its own plan, got %+v", out)
	}
}

// The agent's tool dispatch attaches its todoScope, so a todowrite issued
// through the agent lands under the agent's project root and session id.
func TestExecuteToolCallCarriesTodoScope(t *testing.T) {
	a := todoTestAgent(t, "ses-dispatch")
	a.tools["todowrite"] = tool.TodoWriteTool{}
	if _, err := a.executeToolCallWithContext(context.Background(), "todowrite", mustRawJSON(t, map[string]any{"revision": 0, "todoText": "- [ ] via agent"}), nil, "call-1"); err != nil {
		t.Fatalf("todowrite through the agent: %v", err)
	}
	snap := tool.ReadTodoSnapshot(a.todoScope())
	if !snap.HasOpenItems || !strings.Contains(snap.Content, "via agent") {
		t.Fatalf("plan not written under the agent's scope: %+v", snap)
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
