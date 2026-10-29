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

func TestInjectTodoTailNotesBlockedItems(t *testing.T) {
	a := todoTestAgent(t, "ses-inject-blocked")
	writeAgentTodo(t, a, "- [ ] first\n- [ ] second\n- [ ] third")
	setDeps(t, a, "t2", "t1")
	setDeps(t, a, "t3", "t1", "t2")

	out := a.injectTodoTail([]Message{{Role: "user", Content: "go"}})
	tail := out[len(out)-1]
	if tail.Role != "user" || !strings.Contains(tail.Content, "- [ ] t3 third\nBlocked: t2 by t1; t3 by t1, t2") {
		t.Fatalf("blocked line must follow the list:\n%s", tail.Content)
	}
	if strings.Contains(tail.Content, "  blocked by") {
		t.Fatalf("no non-grammar line inside the list:\n%s", tail.Content)
	}

	// A model that echoes the shown list into todowrite must not hit a parse error.
	var list []string
	for _, l := range strings.Split(tail.Content, "\n") {
		if strings.HasPrefix(l, "- [") {
			list = append(list, l)
		}
	}
	snap := tool.ReadTodoSnapshot(a.todoScope())
	args := mustRawJSON(t, map[string]any{"revision": snap.Revision, "todoText": strings.Join(list, "\n")})
	ctx := tool.WithTodoScope(context.Background(), a.todoScope())
	if _, err := (tool.TodoWriteTool{}).ExecuteCtx(ctx, args); err != nil {
		t.Fatalf("echoed plan rejected: %v", err)
	}
}

func TestInjectTodoTailOmitsBlockedLineWhenNothingBlocked(t *testing.T) {
	a := todoTestAgent(t, "ses-inject-unblocked")
	writeAgentTodo(t, a, "- [ ] first")
	out := a.injectTodoTail([]Message{{Role: "user", Content: "go"}})
	if strings.Contains(out[len(out)-1].Content, "Blocked:") {
		t.Fatalf("unexpected blocked line:\n%s", out[len(out)-1].Content)
	}
}

func TestInjectTodoTailDirectsStartingAnItem(t *testing.T) {
	a := todoTestAgent(t, "ses-inject-start")
	writeAgentTodo(t, a, "- [ ] first\n- [ ] second")

	out := a.injectTodoTail([]Message{{Role: "user", Content: "go"}})
	tail := out[len(out)-1].Content

	if !strings.Contains(tail, "no plan item is in_progress") {
		t.Fatalf("missing the start directive:\n%s", tail)
	}
	for _, want := range []string{"todo_update", `"item_id": "t1"`, `"status": "in_progress"`, `"revision": 1`, "t1: first", "If you are continuing this plan"} {
		if !strings.Contains(tail, want) {
			t.Errorf("start directive missing %q:\n%s", want, tail)
		}
	}
}

func TestInjectTodoTailDirectsFinishingTheCurrentItem(t *testing.T) {
	a := todoTestAgent(t, "ses-inject-finish")
	writeAgentTodo(t, a, "- [•] first\n- [ ] second")

	out := a.injectTodoTail([]Message{{Role: "user", Content: "go"}})
	tail := out[len(out)-1].Content

	for _, want := range []string{"t1 is in_progress", "todo_update", `"item_id": "t1"`, `"status": "done"`, `"result"`, "cite the revision"} {
		if !strings.Contains(tail, want) {
			t.Errorf("finish directive missing %q:\n%s", want, tail)
		}
	}
}

func TestInjectTodoTailRecommendsAReadyItemNotABlockedOne(t *testing.T) {
	a := todoTestAgent(t, "ses-inject-ready")
	writeAgentTodo(t, a, "- [ ] blocked\n- [ ] ready")

	snap := tool.ReadTodoSnapshot(a.todoScope())
	ctx := tool.WithTodoScope(context.Background(), a.todoScope())
	args := mustRawJSON(t, map[string]any{
		"revision": snap.Revision,
		"op":       "set_deps",
		"item_id":  "t1",
		"deps":     []string{"t2"},
	})
	if _, err := (tool.TodoUpdateTool{}).ExecuteCtx(ctx, args); err != nil {
		t.Fatalf("set_deps: %v", err)
	}

	out := a.injectTodoTail([]Message{{Role: "user", Content: "go"}})
	tail := out[len(out)-1].Content
	if !strings.Contains(tail, `"item_id": "t2"`) {
		t.Fatalf("expected the ready item t2 to be recommended:\n%s", tail)
	}
	if strings.Contains(tail, `"item_id": "t1"`) {
		t.Fatalf("a blocked item must not be recommended:\n%s", tail)
	}
}

func TestInjectTodoTailIsStableForAnUnchangedPlan(t *testing.T) {
	a := todoTestAgent(t, "ses-inject-stable")
	writeAgentTodo(t, a, "- [ ] first")

	first := a.injectTodoTail([]Message{{Role: "user", Content: "go"}})
	second := a.injectTodoTail([]Message{{Role: "user", Content: "go"}})
	if first[len(first)-1].Content != second[len(second)-1].Content {
		t.Fatalf("unchanged plan produced different tails:\n--- first\n%s\n--- second\n%s", first[len(first)-1].Content, second[len(second)-1].Content)
	}
}

func TestInjectTodoTailDirectiveCarriesTheCurrentRevision(t *testing.T) {
	a := todoTestAgent(t, "ses-inject-rev")
	writeAgentTodo(t, a, "- [ ] first")
	ctx := tool.WithTodoScope(context.Background(), a.todoScope())

	snap := tool.ReadTodoSnapshot(a.todoScope())
	args := mustRawJSON(t, map[string]any{"revision": snap.Revision, "op": "set_status", "item_id": "t1", "status": "in_progress"})
	if _, err := (tool.TodoUpdateTool{}).ExecuteCtx(ctx, args); err != nil {
		t.Fatalf("set_status: %v", err)
	}

	out := a.injectTodoTail([]Message{{Role: "user", Content: "go"}})
	tail := out[len(out)-1].Content
	if !strings.Contains(tail, `"revision": 2`) {
		t.Fatalf("directive must cite the post-update revision:\n%s", tail)
	}
}

func TestInjectTodoTailSkipsWhenPlanPromptDisabled(t *testing.T) {
	a := todoTestAgent(t, "ses-inject-child")
	writeAgentTodo(t, a, "- [ ] first")
	// Sub-agents, ask helpers and the advisor opt out of the plan fragment and
	// do not have todo_update; they must not receive the re-anchor or its
	// imperative directive (they would be told to call a tool they lack).
	a.SetPlanPromptEnabled(false)

	out := a.injectTodoTail([]Message{{Role: "user", Content: "go"}})
	if len(out) != 1 {
		t.Fatalf("plan prompt disabled must inject nothing, got %d messages", len(out))
	}
	if strings.Contains(out[0].Content, "[ocode:todo]") {
		t.Fatalf("plan prompt disabled must not inject the marker: %q", out[0].Content)
	}
}

func TestInjectTodoTailNamesDependencyRecoveryForABlockedRunningItem(t *testing.T) {
	a := todoTestAgent(t, "ses-inject-blocked-running")
	writeAgentTodo(t, a, "- [•] first\n- [ ] second")
	ctx := tool.WithTodoScope(context.Background(), a.todoScope())

	// set_deps does not check dependencies, so an in-progress item can become
	// blocked; a done transition would then be rejected by the store.
	snap := tool.ReadTodoSnapshot(a.todoScope())
	args := mustRawJSON(t, map[string]any{
		"revision": snap.Revision,
		"op":       "set_deps",
		"item_id":  "t1",
		"deps":     []string{"t2"},
	})
	if _, err := (tool.TodoUpdateTool{}).ExecuteCtx(ctx, args); err != nil {
		t.Fatalf("set_deps: %v", err)
	}

	out := a.injectTodoTail([]Message{{Role: "user", Content: "go"}})
	tail := out[len(out)-1].Content
	for _, want := range []string{"t1 is in_progress but blocked", `"op": "set_deps"`, `"deps": []`} {
		if !strings.Contains(tail, want) {
			t.Errorf("blocked-running directive missing %q:\n%s", want, tail)
		}
	}
	if strings.Contains(tail, `"status": "done"`) {
		t.Fatalf("must not name a done transition the store will reject:\n%s", tail)
	}
}
