package agent

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// buildResumeCallerAgent returns a minimal *Agent that plays the role of the
// caller dispatching the resume — its spec.Name becomes the ownership
// "dispatcher" identity that resumeEligibleRun checks against.
func buildResumeCallerAgent(t *testing.T, dispatcherName string) *Agent {
	t.Helper()
	caller := NewAgent(&MockClient{}, nil, nil, nil)
	caller.SetSpec(&AgentSpec{Name: dispatcherName})
	return caller
}

func TestTaskToolResumeUnknownID(t *testing.T) {
	caller := buildResumeCallerAgent(t, "build")
	tool := TaskTool{mainAgent: caller, registry: DefaultAgentRegistry, runs: caller.runs}

	_, err := tool.Execute(json.RawMessage(`{"prompt":"continue","resume_task_id":"agent-run-999"}`))
	if err == nil {
		t.Fatal("expected error for unknown resume_task_id")
	}
	if !strings.Contains(err.Error(), "unknown task") {
		t.Fatalf("error = %q, want 'unknown task'", err)
	}
}

func TestTaskToolResumeWrongDispatcher(t *testing.T) {
	caller := buildResumeCallerAgent(t, "attacker")
	run := caller.runs.New("explore")
	run.Dispatcher = "build"
	run.tryFinishCancelled()
	sub := NewAgent(&MockClient{}, nil, nil, nil)
	run.Sub = sub

	tool := TaskTool{mainAgent: caller, registry: DefaultAgentRegistry, runs: caller.runs}
	_, err := tool.Execute(json.RawMessage(`{"prompt":"continue","resume_task_id":"` + run.ID + `"}`))
	if err == nil {
		t.Fatal("expected error for dispatcher mismatch")
	}
	if !strings.Contains(err.Error(), "not owned by") {
		t.Fatalf("error = %q, want 'not owned by'", err)
	}
}

func TestTaskToolResumeRejectsRunningStatus(t *testing.T) {
	caller := buildResumeCallerAgent(t, "build")
	run := caller.runs.New("explore") // New() leaves status RunRunning
	run.Dispatcher = "build"
	run.Sub = NewAgent(&MockClient{}, nil, nil, nil)

	tool := TaskTool{mainAgent: caller, registry: DefaultAgentRegistry, runs: caller.runs}
	_, err := tool.Execute(json.RawMessage(`{"prompt":"continue","resume_task_id":"` + run.ID + `"}`))
	if err == nil {
		t.Fatal("expected error for resuming a running task")
	}
	if !strings.Contains(err.Error(), "cannot be resumed") {
		t.Fatalf("error = %q, want 'cannot be resumed'", err)
	}
}

func TestTaskToolResumeRejectsFailedStatus(t *testing.T) {
	caller := buildResumeCallerAgent(t, "build")
	run := caller.runs.New("explore")
	run.Dispatcher = "build"
	run.finishErr("boom")
	run.Sub = NewAgent(&MockClient{}, nil, nil, nil)

	tool := TaskTool{mainAgent: caller, registry: DefaultAgentRegistry, runs: caller.runs}
	_, err := tool.Execute(json.RawMessage(`{"prompt":"continue","resume_task_id":"` + run.ID + `"}`))
	if err == nil {
		t.Fatal("expected error for resuming a failed task")
	}
	if !strings.Contains(err.Error(), "cannot be resumed") {
		t.Fatalf("error = %q, want 'cannot be resumed'", err)
	}
}

func TestTaskToolResumeCancelledSucceedsSync(t *testing.T) {
	caller := buildResumeCallerAgent(t, "build")
	run := caller.runs.New("explore")
	run.Dispatcher = "build"
	run.appendTranscript(Message{Role: "user", Content: "original prompt"})
	run.appendTranscript(Message{Role: "assistant", Content: "original answer"})
	run.tryFinishCancelled()

	capture := &captureClient{}
	sub := NewAgent(capture, nil, nil, nil)
	sub.shutdownTransient() // mirrors the real teardown that ran when this run first went terminal
	run.Sub = sub
	run.Cancel = sub.Cancel

	tool := TaskTool{mainAgent: caller, registry: DefaultAgentRegistry, runs: caller.runs}
	result, err := tool.Execute(json.RawMessage(`{"prompt":"keep going","resume_task_id":"` + run.ID + `"}`))
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if !strings.Contains(result, "ok") { // captureClient's Chat always responds with content "ok"
		t.Fatalf("result = %q, want the resumed sub-agent's response", result)
	}
	if run.statusValue() != RunDone {
		t.Fatalf("status = %s, want done", run.statusValue())
	}

	// Transcript continuity: the resumed Step call must have seen the full
	// prior conversation plus the new follow-up prompt, in order.
	var contents []string
	for _, m := range capture.Messages {
		contents = append(contents, m.Content)
	}
	want := []string{"original prompt", "original answer", "keep going"}
	for _, w := range want {
		found := false
		for _, c := range contents {
			if c == w {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("captured messages %v missing expected content %q", contents, w)
		}
	}
	if contents[len(contents)-1] != "keep going" {
		t.Fatalf("last captured message = %q, want the new follow-up prompt last", contents[len(contents)-1])
	}
}

func TestTaskToolResumeDoneSucceedsBackground(t *testing.T) {
	caller := buildResumeCallerAgent(t, "build")
	run := caller.runs.New("explore")
	run.Dispatcher = "build"
	run.appendTranscript(Message{Role: "user", Content: "original prompt"})
	run.finishOK("original result")

	capture := &captureClient{}
	sub := NewAgent(capture, nil, nil, nil)
	sub.shutdownTransient()
	run.Sub = sub
	run.Cancel = sub.Cancel

	tool := TaskTool{mainAgent: caller, registry: DefaultAgentRegistry, runs: caller.runs}
	result, err := tool.Execute(json.RawMessage(`{"prompt":"keep going","resume_task_id":"` + run.ID + `","run_in_background":true}`))
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if !strings.Contains(result, "resumed") {
		t.Fatalf("result = %q, want it to mention 'resumed'", result)
	}
	if !strings.Contains(result, run.ID) {
		t.Fatalf("result = %q, want it to reuse task_id %s", result, run.ID)
	}

	// Wait for the resumed background run to actually finish. run.Done() is
	// already closed from the ORIGINAL finishOK (the done channel is
	// doneOnce-guarded and beginResume deliberately does not re-arm it), so
	// waiting on it is vacuous — poll the terminal status instead.
	deadline := time.Now().Add(5 * time.Second)
	for run.statusValue() != RunDone {
		if time.Now().After(deadline) {
			t.Fatalf("background resume did not finish within 5s (status = %s)", run.statusValue())
		}
		time.Sleep(5 * time.Millisecond)
	}
	if run.statusValue() != RunDone {
		t.Fatalf("status = %s, want done", run.statusValue())
	}
	if run.Result != "ok" {
		t.Fatalf("Result = %q, want the resumed sub-agent's response", run.Result)
	}
}
