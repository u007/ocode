package agent

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// contractScriptClient distinguishes the child's Step calls from the
// verifier's calls by inspecting the message content: the verifier prompt
// always contains "strict output verifier", the child's never does. Each call
// type consumes from its own scripted response list, so a test can express
// "first child run returns X, verifier says NOT_SATISFIED, retried child
// returns Y, verifier says SATISFIED".
type contractScriptClient struct {
	childResponses   []string
	verdictResponses []string
	childCalls       int
	verdictCalls     int
	lastChildMsgs    []Message // messages of the most recent child Step call
}

func (c *contractScriptClient) Chat(messages []Message, tools []map[string]interface{}) (*Message, error) {
	for _, m := range messages {
		if strings.Contains(m.Content, "strict output verifier") {
			resp := "VERDICT: SATISFIED"
			if c.verdictCalls < len(c.verdictResponses) {
				resp = c.verdictResponses[c.verdictCalls]
			}
			c.verdictCalls++
			return &Message{Role: "assistant", Content: resp}, nil
		}
	}
	c.lastChildMsgs = append([]Message(nil), messages...)
	resp := "child result"
	if c.childCalls < len(c.childResponses) {
		resp = c.childResponses[c.childCalls]
	}
	c.childCalls++
	return &Message{Role: "assistant", Content: resp}, nil
}

func (c *contractScriptClient) GetProvider() string { return "mock" }
func (c *contractScriptClient) GetModel() string    { return "mock-model" }

// newContractTaskTool builds a TaskTool wired exactly like NewAgent does: the
// tool is registered in a.tools["task"] so the child dispatch resolves tools
// from the main agent, and runs are tracked in the shared registry.
func newContractTaskTool(client LLMClient) (*Agent, *TaskTool) {
	a := NewAgent(client, nil, nil, nil)
	taskTool, ok := a.tools["task"].(*TaskTool)
	if !ok {
		panic("task tool not registered")
	}
	return a, taskTool
}

func TestSyncDispatchRetriesOnceOnUnmetContract(t *testing.T) {
	client := &contractScriptClient{
		childResponses:   []string{"partial result without the file list", "complete result with the full file list"},
		verdictResponses: []string{"VERDICT: NOT_SATISFIED\nDEFICIENCY: missing the file list", "VERDICT: SATISFIED"},
	}
	a, taskTool := newContractTaskTool(client)

	result, err := taskTool.Execute(json.RawMessage(`{"prompt": "list the files", "agent": "general", "expected_output": "the full file list"}`))
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}

	// The child was stepped exactly twice (initial + one retry), then the
	// verifier ran twice (initial + re-verify).
	if client.childCalls != 2 {
		t.Fatalf("child Step calls = %d, want 2 (initial + retry)", client.childCalls)
	}
	if client.verdictCalls != 2 {
		t.Fatalf("verifier calls = %d, want 2", client.verdictCalls)
	}

	// The retry transcript carried the deficiency message appended to the
	// child's full prior transcript.
	joined := ""
	for _, m := range client.lastChildMsgs {
		joined += m.Content + "\n"
	}
	if !strings.Contains(joined, "missing the file list") {
		t.Errorf("retry transcript should contain the deficiency, got: %s", joined)
	}
	if !strings.Contains(joined, "partial result") {
		t.Errorf("retry transcript should contain the child's prior output, got: %s", joined)
	}

	// Satisfied after retry: the result is returned unchanged, no warning.
	if !strings.Contains(result, "complete result with the full file list") {
		t.Errorf("expected the retried result, got: %q", result)
	}
	if strings.Contains(result, "Output contract not met") {
		t.Errorf("satisfied-after-retry result must not carry the warning prefix, got: %q", result)
	}

	// Verdict recorded on the run: checked + satisfied.
	runs := a.Runs().Snapshot()
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	checked, satisfied, deficiency := runs[0].ContractVerdict()
	if !checked || !satisfied || deficiency != "" {
		t.Errorf("verdict = checked=%v satisfied=%v deficiency=%q, want checked=true satisfied=true", checked, satisfied, deficiency)
	}
}

func TestSyncDispatchFailedContractPrefixesAndRecordsVerdict(t *testing.T) {
	client := &contractScriptClient{
		childResponses:   []string{"still missing files", "still missing files"},
		verdictResponses: []string{"VERDICT: NOT_SATISFIED\nDEFICIENCY: no file paths at all", "VERDICT: NOT_SATISFIED\nDEFICIENCY: no file paths at all"},
	}
	a, taskTool := newContractTaskTool(client)

	result, err := taskTool.Execute(json.RawMessage(`{"prompt": "list the files", "agent": "general", "expected_output": "the full file list"}`))
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}

	// Two failures: the result is prefixed with a warning naming the
	// contract and the deficiency, with the child's result still present.
	if !strings.Contains(result, "Output contract not met") {
		t.Errorf("expected warning prefix, got: %q", result)
	}
	if !strings.Contains(result, "the full file list") {
		t.Errorf("warning should name the contract, got: %q", result)
	}
	if !strings.Contains(result, "no file paths at all") {
		t.Errorf("warning should name the deficiency, got: %q", result)
	}
	if !strings.Contains(result, "still missing files") {
		t.Errorf("child result must still be present in full, got: %q", result)
	}

	runs := a.Runs().Snapshot()
	checked, satisfied, deficiency := runs[0].ContractVerdict()
	if !checked || satisfied {
		t.Errorf("verdict = checked=%v satisfied=%v, want checked=true satisfied=false", checked, satisfied)
	}
	if deficiency == "" {
		t.Error("deficiency should be recorded on the run")
	}
}

func TestSyncDispatchNoContractIsUnchanged(t *testing.T) {
	client := &contractScriptClient{childResponses: []string{"plain result"}}
	a, taskTool := newContractTaskTool(client)

	result, err := taskTool.Execute(json.RawMessage(`{"prompt": "do a thing", "agent": "general"}`))
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if client.verdictCalls != 0 {
		t.Fatalf("verifier calls = %d, want 0 (no contract → no verification)", client.verdictCalls)
	}
	if !strings.Contains(result, "plain result") {
		t.Errorf("result should pass through unchanged, got: %q", result)
	}
	// No contract → run has no verdict recorded.
	runs := a.Runs().Snapshot()
	checked, _, _ := runs[0].ContractVerdict()
	if checked {
		t.Error("run should have no contract verdict when no contract applies")
	}
}

func TestTaskStatusSurfacesContractVerdict(t *testing.T) {
	run := &AgentRun{ID: "run-1", Name: "general", Status: RunDone, Result: "the child result"}
	run.SetContractVerdict(false, "missing file list")

	out := formatTaskRunStatus("run-1", run)
	if !strings.Contains(out, "Contract NOT satisfied") {
		t.Errorf("task_status output should surface the failed contract, got: %q", out)
	}
	if !strings.Contains(out, "missing file list") {
		t.Errorf("task_status output should carry the deficiency, got: %q", out)
	}
	if !strings.Contains(out, "the child result") {
		t.Errorf("task_status output should still carry the full child result, got: %q", out)
	}
}

func TestContractDeficiencySuffix(t *testing.T) {
	// The AgentStatusTool and TaskStatusTool surfaces share this helper; the
	// full integration (verdict recorded on a background run, surfaced in
	// the status output) is covered by TestBackgroundDispatch and
	// TestTaskStatusSurfacesContractVerdict.
	if suffix := contractDeficiencySuffix(""); suffix != "" {
		t.Errorf("empty deficiency should yield no suffix, got %q", suffix)
	}
	if suffix := contractDeficiencySuffix("nope"); suffix != " — nope" {
		t.Errorf("deficiency suffix = %q, want ' — nope'", suffix)
	}
}

func TestBackgroundDispatchNotDoneUntilVerdictExists(t *testing.T) {
	client := &contractScriptClient{
		childResponses:   []string{"bg result one", "bg result two"},
		verdictResponses: []string{"VERDICT: NOT_SATISFIED\nDEFICIENCY: missing detail", "VERDICT: SATISFIED"},
	}
	a, taskTool := newContractTaskTool(client)

	out, err := taskTool.Execute(json.RawMessage(`{"prompt": "do it", "agent": "general", "expected_output": "the required detail", "run_in_background": true}`))
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if !strings.Contains(out, "task_id:") {
		t.Fatalf("expected background task id, got %q", out)
	}

	// Poll until terminal; the run must be done AND carry the verdict, and
	// the result must be the retried (satisfied) output.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		runs := a.Runs().Snapshot()
		if len(runs) == 0 {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		run := runs[0]
		if run.statusValue() == RunDone {
			checked, satisfied, _ := run.ContractVerdict()
			if !checked {
				t.Fatalf("background run reached done without a contract verdict")
			}
			if !satisfied {
				t.Fatalf("background run verdict = not satisfied, want satisfied after retry")
			}
			if !strings.Contains(run.Result, "bg result two") {
				t.Fatalf("background run result = %q, want the retried output", run.Result)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("background run never reached done with a verdict")
}
