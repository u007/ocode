package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/snapshot"
	"github.com/u007/ocode/internal/tool"
)

// newTodoLinkTaskTool builds a main agent whose plan lives under a temp dir
// (never the repo's .ocode/todo) and seeds it with planText.
func newTodoLinkTaskTool(t *testing.T, client LLMClient, planText string) (*Agent, *TaskTool) {
	t.Helper()
	a, taskTool := newContractTaskTool(client)
	a.SetWorkDir(t.TempDir())
	// Unique per test: the store keeps an in-memory copy keyed by session id
	// alone, which a later test would otherwise read as a legacy plan.
	a.SetSessionID("ses_todo_link_" + t.Name())
	args, _ := json.Marshal(map[string]any{"revision": 0, "todoText": planText})
	if _, err := (tool.TodoWriteTool{}).ExecuteCtx(tool.WithTodoScope(context.Background(), a.todoScope()), args); err != nil {
		t.Fatalf("seed plan: %v", err)
	}
	return a, taskTool
}

func readPlan(t *testing.T, a *Agent) string {
	t.Helper()
	out, err := (tool.TodoReadTool{}).ExecuteCtx(tool.WithTodoScope(context.Background(), a.todoScope()), nil)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func setDeps(t *testing.T, a *Agent, item string, deps ...string) {
	t.Helper()
	snap := tool.ReadTodoSnapshot(a.todoScope())
	args, _ := json.Marshal(map[string]any{"revision": snap.Revision, "op": "set_deps", "item_id": item, "deps": deps})
	if _, err := (tool.TodoUpdateTool{}).ExecuteCtx(tool.WithTodoScope(context.Background(), a.todoScope()), args); err != nil {
		t.Fatalf("set_deps: %v", err)
	}
}

func TestTaskTodoItemRejectedWhenBlockedOrUnknown(t *testing.T) {
	client := &contractScriptClient{childResponses: []string{"never"}}
	a, taskTool := newTodoLinkTaskTool(t, client, "- [ ] first\n- [ ] second\n")
	setDeps(t, a, "t2", "t1")
	before := readPlan(t, a)

	_, err := taskTool.Execute(json.RawMessage(`{"prompt":"x","agent":"general","todo_item":"t2"}`))
	if err == nil || !strings.Contains(err.Error(), "t2 is blocked by t1") {
		t.Fatalf("blocked: err = %v", err)
	}
	_, err = taskTool.Execute(json.RawMessage(`{"prompt":"x","agent":"general","todo_item":"t9"}`))
	if err == nil || !strings.Contains(err.Error(), "t9") {
		t.Fatalf("unknown: err = %v", err)
	}
	if client.childCalls != 0 {
		t.Fatalf("a rejected dispatch spawned the child (%d Step calls)", client.childCalls)
	}
	if got := readPlan(t, a); got != before {
		t.Fatalf("a rejected dispatch changed the plan:\n%s\nvs\n%s", got, before)
	}
}

func TestTaskTodoItemSyncSuccessWritesResultAndDone(t *testing.T) {
	client := &contractScriptClient{childResponses: []string{"child says hi\nsecond line"}}
	a, taskTool := newTodoLinkTaskTool(t, client, "- [ ] first\n- [ ] second\n")

	out, err := taskTool.Execute(json.RawMessage(`{"prompt":"do it","agent":"general","todo_item":"t1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Todo item t1 marked done") {
		t.Fatalf("result lacks the plan note: %q", out)
	}
	plan := readPlan(t, a)
	for _, want := range []string{"- [✓] t1 first", "  run: agent-run-1", "  result: child says hi\n", "  started: ", "  finished: ", "- [ ] t2 second"} {
		if !strings.Contains(plan, want) {
			t.Errorf("plan missing %q:\n%s", want, plan)
		}
	}
	if strings.Contains(plan, "second line") {
		t.Errorf("only the first result line belongs in the plan:\n%s", plan)
	}
}

func TestTaskTodoItemFailureLeavesInProgress(t *testing.T) {
	a, taskTool := newTodoLinkTaskTool(t, &MockClient{Err: errors.New("provider exploded")}, "- [ ] first\n")

	if _, err := taskTool.Execute(json.RawMessage(`{"prompt":"do it","agent":"general","todo_item":"t1"}`)); err == nil {
		t.Fatal("expected the dispatch to fail")
	}
	plan := readPlan(t, a)
	if !strings.Contains(plan, "- [•] t1 first") || !strings.Contains(plan, "  result: FAILED: ") || strings.Contains(plan, "finished:") {
		t.Fatalf("plan after failure:\n%s", plan)
	}
}

func TestTaskTodoItemCancelledRunIsPrefixed(t *testing.T) {
	a, taskTool := newTodoLinkTaskTool(t, &MockClient{}, "- [ ] first\n")
	run := a.runs.New("general")
	if _, err := tool.LinkTodoItemToRun(a.todoScope(), "t1", run.ID); err != nil {
		t.Fatal(err)
	}
	run.todoItem = "t1"
	run.tryFinishCancelled()

	if note := taskTool.recordTodoOutcome(run, "", errors.New("stopped by user")); !strings.Contains(note, "left in_progress") {
		t.Fatalf("note = %q", note)
	}
	if plan := readPlan(t, a); !strings.Contains(plan, "- [•] t1 first") || !strings.Contains(plan, "  result: CANCELLED: stopped by user") {
		t.Fatalf("plan:\n%s", plan)
	}
}

func TestTaskTodoItemBackgroundWritesBackBeforeDone(t *testing.T) {
	client := &contractScriptClient{childResponses: []string{"bg finished"}}
	a, taskTool := newTodoLinkTaskTool(t, client, "- [ ] first\n")

	out, err := taskTool.Execute(json.RawMessage(`{"prompt":"bg","agent":"general","run_in_background":true,"todo_item":"t1"}`))
	if err != nil || !strings.Contains(out, "task_id:") {
		t.Fatalf("out=%q err=%v", out, err)
	}
	run, _ := a.runs.Get("agent-run-1")
	select {
	case <-run.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("background run never finished")
	}
	// Run.Done() closes only after the write-back, so the plan is already final.
	if plan := readPlan(t, a); !strings.Contains(plan, "- [✓] t1 first") || !strings.Contains(plan, "  result: bg finished") {
		t.Fatalf("plan:\n%s", plan)
	}
}

func TestTaskTodoItemRefusedFromSubAgent(t *testing.T) {
	client := &contractScriptClient{}
	a, taskTool := newTodoLinkTaskTool(t, client, "- [ ] first\n")
	a.isSubAgent = true

	_, err := taskTool.Execute(json.RawMessage(`{"prompt":"x","agent":"general","todo_item":"t1"}`))
	if err == nil || !strings.Contains(err.Error(), "only the main agent") {
		t.Fatalf("err = %v", err)
	}
	if client.childCalls != 0 {
		t.Fatal("child was spawned")
	}
}

func TestTaskTodoItemUnblocksDependentAfterDone(t *testing.T) {
	client := &contractScriptClient{childResponses: []string{"one", "two"}}
	a, taskTool := newTodoLinkTaskTool(t, client, "- [ ] first\n- [ ] second\n")
	setDeps(t, a, "t2", "t1")

	if _, err := taskTool.Execute(json.RawMessage(`{"prompt":"a","agent":"general","todo_item":"t1"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := taskTool.Execute(json.RawMessage(`{"prompt":"b","agent":"general","todo_item":"t2"}`)); err != nil {
		t.Fatalf("dependent still blocked after its dep finished: %v", err)
	}
	if plan := readPlan(t, a); !strings.Contains(plan, "- [✓] t2 second") {
		t.Fatalf("plan:\n%s", plan)
	}
}

func TestTaskToolDefinitionDocumentsTodoItem(t *testing.T) {
	_, taskTool := newContractTaskTool(&MockClient{})
	props := taskTool.Definition()["parameters"].(map[string]interface{})["properties"].(map[string]interface{})
	if _, ok := props["todo_item"]; !ok {
		t.Fatal("task definition lacks todo_item")
	}
	for _, name := range []string{"agent_status", "task_status"} {
		def := a2def(t, name)
		if strings.Contains(def, "todo_item") {
			t.Errorf("%s must not take todo_item", name)
		}
	}
}

func a2def(t *testing.T, name string) string {
	t.Helper()
	a := newTestAgent(&MockClient{}, nil, nil, nil)
	b, err := json.Marshal(a.tools[name].Definition())
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// hookClient runs before on every Chat call, then delegates.
type hookClient struct {
	LLMClient
	before func()
}

func (h *hookClient) Chat(messages []Message, tools []map[string]interface{}) (*Message, error) {
	h.before()
	return h.LLMClient.Chat(messages, tools)
}

func TestTaskExecuteCtxRecordsToolCallID(t *testing.T) {
	client := &contractScriptClient{childResponses: []string{"one", "two", "three"}}
	a, taskTool := newTodoLinkTaskTool(t, client, "- [ ] first\n")

	// Synchronous dispatch through the tool's own ExecuteCtx.
	ctx := snapshot.WithToolCallID(context.Background(), "call-sync")
	if _, err := taskTool.ExecuteCtx(ctx, json.RawMessage(`{"prompt":"a","agent":"general"}`)); err != nil {
		t.Fatal(err)
	}
	run, _ := a.runs.Get("agent-run-1")
	if run.ToolCallID != "call-sync" {
		t.Fatalf("sync run ToolCallID = %q", run.ToolCallID)
	}

	// Background dispatch through the agent loop's dispatcher, which must reach ExecuteCtx.
	if _, err := a.executeToolCallWithContext(context.Background(), "task", json.RawMessage(`{"prompt":"b","agent":"general","run_in_background":true}`), nil, "call-bg"); err != nil {
		t.Fatal(err)
	}
	bg, _ := a.runs.Get("agent-run-2")
	if bg.ToolCallID != "call-bg" {
		t.Fatalf("background run ToolCallID = %q", bg.ToolCallID)
	}
	<-bg.Done()

	// A resume hands the card to the latest call.
	ctx2 := snapshot.WithToolCallID(context.Background(), "call-resume")
	if _, err := taskTool.ExecuteCtx(ctx2, json.RawMessage(`{"prompt":"c","resume_task_id":"agent-run-1"}`)); err != nil {
		t.Fatal(err)
	}
	if run.ToolCallID != "call-resume" {
		t.Fatalf("resumed run ToolCallID = %q", run.ToolCallID)
	}
}

func TestTaskResumeLostRaceLeavesPlanItemUntouched(t *testing.T) {
	a, taskTool := newTodoLinkTaskTool(t, &contractScriptClient{}, "- [ ] first\n")
	scope := a.todoScope()
	run := a.runs.New("general")
	run.Sub = a
	if _, err := tool.LinkTodoItemToRun(scope, "t1", run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.FinishTodoItemRun(scope, "t1", run.ID, tool.TodoRunDone, "original result"); err != nil {
		t.Fatal(err)
	}
	run.setTodoItem("t1")
	run.finishOK("x")
	before := readPlan(t, a)

	// Teardown is still open when resumeEligibleRun passes; the status flips
	// before awaitTeardown returns, so beginResume loses.
	run.mu.Lock()
	run.teardownDone = make(chan struct{})
	run.mu.Unlock()
	go func() {
		time.Sleep(50 * time.Millisecond)
		run.mu.Lock()
		run.Status = RunRunning
		run.mu.Unlock()
		run.markTeardownDone()
	}()

	out, err := taskTool.Execute(json.RawMessage(`{"prompt":"again","resume_task_id":"` + run.ID + `"}`))
	if err != nil || !strings.Contains(out, "changed state") {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if got := readPlan(t, a); got != before {
		t.Fatalf("a lost resume changed the plan:\n%s\nvs\n%s", got, before)
	}
}

func TestTaskBackgroundWriteBackFailureNoteReachesResult(t *testing.T) {
	client := &contractScriptClient{childResponses: []string{"bg finished"}}
	a, taskTool := newTodoLinkTaskTool(t, client, "- [ ] first\n")
	// While the child runs, another run takes the item, so the write-back is refused.
	hooked := &hookClient{LLMClient: client, before: func() {
		if _, err := tool.LinkTodoItemToRun(a.todoScope(), "t1", "other-run"); err != nil {
			t.Errorf("relink: %v", err)
		}
	}}
	a.client = hooked

	if _, err := taskTool.Execute(json.RawMessage(`{"prompt":"bg","agent":"general","run_in_background":true,"todo_item":"t1"}`)); err != nil {
		t.Fatal(err)
	}
	run, _ := a.runs.Get("agent-run-1")
	select {
	case <-run.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("background run never finished")
	}
	if !strings.Contains(run.Result, "Todo item t1 was not updated") {
		t.Fatalf("run.Result lacks the write-back note: %q", run.Result)
	}
}
