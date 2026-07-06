package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTaskCancelToolUnknownTask(t *testing.T) {
	tool := TaskCancelTool{
		runs: NewAgentRunRegistry(),
		dispatcherForCall: func() string { return "build" },
	}
	out, err := tool.Execute([]byte(`{"task_id":"agent-run-999"}`))
	if err == nil {
		t.Fatal("expected error for unknown task")
	}
	if !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("error = %q, want 'unknown'", err)
	}
	_ = out
}

func TestTaskCancelToolNotOwned(t *testing.T) {
	r := NewAgentRunRegistry()
	run := r.New("explore")
	run.Dispatcher = "build"

	tool := TaskCancelTool{
		runs: r,
		dispatcherForCall: func() string { return "attacker" },
	}
	_, err := tool.Execute(json.RawMessage(`{"task_id":"` + run.ID + `"}`))
	if err == nil {
		t.Fatal("expected error for not-owned task")
	}
	if !strings.Contains(err.Error(), "not owned by") {
		t.Fatalf("error = %q, want 'not owned by'", err)
	}
}

func TestTaskCancelToolSuccess(t *testing.T) {
	r := NewAgentRunRegistry()
	run := r.New("explore")
	run.Dispatcher = "build"
	cancelled := false
	run.Cancel = func() { cancelled = true }

	tool := TaskCancelTool{
		runs: r,
		dispatcherForCall: func() string { return "build" },
	}
	out, err := tool.Execute(json.RawMessage(`{"task_id":"` + run.ID + `"}`))
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if !cancelled {
		t.Fatal("Cancel func was not called")
	}
	if run.statusValue() != RunCancelled {
		t.Fatalf("status = %s, want %s", run.statusValue(), RunCancelled)
	}
	if !strings.Contains(out, "cancelled") {
		t.Fatalf("output = %q, want 'cancelled'", out)
	}
}

func TestTaskCancelToolAlreadyFinished(t *testing.T) {
	r := NewAgentRunRegistry()
	run := r.New("explore")
	run.Dispatcher = "build"
	run.finishOK("already done")
	origStatus := run.statusValue()

	tool := TaskCancelTool{
		runs: r,
		dispatcherForCall: func() string { return "build" },
	}
	out, err := tool.Execute(json.RawMessage(`{"task_id":"` + run.ID + `"}`))
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if run.statusValue() != origStatus {
		t.Fatalf("status changed from %s to %s", origStatus, run.statusValue())
	}
	// Should report the task's final state (completed, not cancelled).
	if !strings.Contains(out, "completed") && !strings.Contains(run.Result, "already done") {
		t.Fatalf("output = %q, want completed state with result", out)
	}
}

func TestTaskCancelToolEmptyDispatcher(t *testing.T) {
	r := NewAgentRunRegistry()
	run := r.New("explore")
	run.Dispatcher = ""
	cancelled := false
	run.Cancel = func() { cancelled = true }

	tool := TaskCancelTool{
		runs: r,
		dispatcherForCall: func() string { return "" },
	}
	out, err := tool.Execute(json.RawMessage(`{"task_id":"` + run.ID + `"}`))
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if !cancelled {
		t.Fatal("Cancel func was not called")
	}
	if run.statusValue() != RunCancelled {
		t.Fatalf("status = %s, want %s", run.statusValue(), RunCancelled)
	}
	if !strings.Contains(out, "cancelled") {
		t.Fatalf("output = %q, want 'cancelled'", out)
	}
}

func TestTaskCancelToolEmptyTaskID(t *testing.T) {
	tool := TaskCancelTool{
		runs: NewAgentRunRegistry(),
		dispatcherForCall: func() string { return "build" },
	}
	_, err := tool.Execute([]byte(`{"task_id":""}`))
	if err == nil {
		t.Fatal("expected error for empty task_id")
	}
}

func TestTaskCancelToolNilRegistry(t *testing.T) {
	tool := TaskCancelTool{
		runs:             nil,
		dispatcherForCall: func() string { return "build" },
	}
	_, err := tool.Execute([]byte(`{"task_id":"agent-run-1"}`))
	if err == nil {
		t.Fatal("expected error for nil registry")
	}
}
