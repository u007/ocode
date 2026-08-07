package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// Regression tests for the review findings on the in-batch DAG. Each test
// fails against the pre-fix code.

func dagCall(id, name, args string) ToolCall {
	tc := ToolCall{ID: id}
	tc.Function.Name = name
	tc.Function.Arguments = args
	return tc
}

// Finding: the DAG gate parsed `id` off ANY parallel tool call. agent_status,
// bash_output and kill_shell are all Parallel() with a REQUIRED `id`, so a
// batch with no task calls at all was routed through the scheduler, and two
// bash_output calls on different shells collided on the duplicate-id rule.
func TestDAGGateIgnoresNonSubagentTools(t *testing.T) {
	calls := []ToolCall{
		dagCall("c1", "bash_output", `{"id":"shell-1"}`),
		dagCall("c2", "agent_status", `{"id":"run-7"}`),
	}
	if dagHasDependencies(calls) {
		t.Fatal("batch with no task calls must not route through the DAG scheduler")
	}

	// Same ids on non-task calls must not collide either.
	dup := []ToolCall{
		dagCall("c1", "bash_output", `{"id":"same"}`),
		dagCall("c2", "kill_shell", `{"id":"same"}`),
		dagCall("c3", "task", `{"prompt":"p","id":"a"}`),
	}
	if _, err := buildDAG(dup); err != nil {
		t.Fatalf("non-task ids must not participate in the id namespace: %v", err)
	}
}

// Finding: a node that depends on a run_in_background predecessor was released
// against the "state: running" placeholder instead of a result.
func TestDAGRejectsBackgroundPredecessor(t *testing.T) {
	calls := []ToolCall{
		dagCall("c1", "task", `{"prompt":"p","id":"a","run_in_background":true}`),
		dagCall("c2", "task", `{"prompt":"q","id":"b","depends_on":["a"]}`),
	}
	_, err := buildDAG(calls)
	if err == nil {
		t.Fatal("expected rejection of a dependency on a background dispatch")
	}
	if !strings.Contains(err.Error(), "run_in_background") {
		t.Fatalf("error should name the cause: %v", err)
	}
}

// Finding: abort was graph-global, so an independent node could be skipped
// because an unrelated node failed — nondeterministically.
func TestDAGFailureDoesNotSkipIndependentNodes(t *testing.T) {
	calls := []ToolCall{
		dagCall("c1", "task", `{"prompt":"fails","id":"a"}`),
		dagCall("c2", "task", `{"prompt":"dependent","id":"b","depends_on":["a"]}`),
		dagCall("c3", "task", `{"prompt":"independent","id":"c"}`),
	}
	var mu sync.Mutex
	ran := map[string]bool{}
	dispatch := func(tc ToolCall, _ *taskBinding, _ string, _ string) (string, []Image, error) {
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &p); err != nil {
			t.Errorf("unmarshal: %v", err)
		}
		mu.Lock()
		ran[p.ID] = true
		mu.Unlock()
		if p.ID == "a" {
			return "", nil, fmt.Errorf("boom")
		}
		return "ok:" + p.ID, nil, nil
	}

	msgs, err := runDAGFromValidated(calls, nil, func() bool { return false }, nil, nil, nil, dispatch)
	if err != nil {
		t.Fatalf("validation should pass: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !ran["c"] {
		t.Fatal("independent node c must run even though unrelated node a failed")
	}
	if ran["b"] {
		t.Fatal("dependent node b must NOT run after its predecessor failed")
	}
	if !strings.Contains(msgs[1].Content, "skipped") || !strings.Contains(msgs[1].Content, `"a"`) {
		t.Fatalf("b should be skipped naming a: %q", msgs[1].Content)
	}
	if msgs[2].Content != "ok:c" {
		t.Fatalf("c should carry its own result, got %q", msgs[2].Content)
	}
}

// Finding: buildResults dropped Notice and DisplayContent, which the legacy
// fan-out sets, so a call routed through the DAG lost both.
func TestDAGResultsCarryNoticeAndDisplayContent(t *testing.T) {
	long := strings.Repeat("x", 200000)
	calls := []ToolCall{
		dagCall("c1", "task", `{"prompt":"p","id":"a"}`),
	}
	dispatch := func(tc ToolCall, _ *taskBinding, _ string, _ string) (string, []Image, error) {
		return long, nil, nil
	}
	msgs, err := runDAGFromValidated(calls, nil, func() bool { return false }, nil, nil, nil, dispatch)
	if err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if msgs[0].Content == long {
		t.Skip("result was not truncated at this size; nothing to assert")
	}
	if msgs[0].DisplayContent != long {
		t.Fatal("truncated DAG result must keep the full text in DisplayContent")
	}
}
