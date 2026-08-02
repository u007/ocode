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
	run.markTeardownDone()  // ...and the signal runBackgroundDispatch/runSyncDispatch send once it's done
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
	run.markTeardownDone()
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

	// By the time Execute returns, beginResume already ran synchronously
	// (before the background goroutine was launched) and replaced run.done
	// with a fresh channel for this new dispatch cycle — so, unlike the
	// original terminal transition's channel, THIS one is still open and
	// waiting on it here actually blocks for the resumed run's completion.
	select {
	case <-run.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("background resume did not finish within 5s")
	}
	if run.statusValue() != RunDone {
		t.Fatalf("status = %s, want done", run.statusValue())
	}
	if run.Result != "ok" {
		t.Fatalf("Result = %q, want the resumed sub-agent's response", run.Result)
	}
}

// TestTaskToolResumeChainResumeResume proves a run can be resumed, finish,
// and be resumed AGAIN: the second resume runs shutdownTransient (from the
// first resume's dispatch) followed by a second RearmMaintenance, exercising
// the fresh sync.Once guards + recreated channels in sequence.
func TestTaskToolResumeChainResumeResume(t *testing.T) {
	caller := buildResumeCallerAgent(t, "build")
	run := caller.runs.New("explore")
	run.Dispatcher = "build"
	run.appendTranscript(Message{Role: "user", Content: "first prompt"})
	run.finishOK("first result")

	capture := &captureClient{}
	sub := NewAgent(capture, nil, nil, nil)
	sub.shutdownTransient()
	run.markTeardownDone()
	run.Sub = sub
	run.Cancel = sub.Cancel

	tool := TaskTool{mainAgent: caller, registry: DefaultAgentRegistry, runs: caller.runs}

	// Resume 1 (sync): goes terminal again (RunDone) with teardown.
	if _, err := tool.Execute(json.RawMessage(`{"prompt":"second prompt","resume_task_id":"` + run.ID + `"}`)); err != nil {
		t.Fatalf("first resume err: %v", err)
	}
	if run.statusValue() != RunDone {
		t.Fatalf("after first resume: status = %s, want done", run.statusValue())
	}
	if run.Result != "ok" {
		t.Fatalf("after first resume: Result = %q, want ok", run.Result)
	}

	// Resume 2 (sync): must work against the freshly re-armed channels.
	if _, err := tool.Execute(json.RawMessage(`{"prompt":"third prompt","resume_task_id":"` + run.ID + `"}`)); err != nil {
		t.Fatalf("second resume err: %v", err)
	}
	if run.statusValue() != RunDone {
		t.Fatalf("after second resume: status = %s, want done", run.statusValue())
	}
	if run.Result != "ok" {
		t.Fatalf("after second resume: Result = %q, want ok", run.Result)
	}

	// Transcript continuity across the chain: every prompt is present, in order.
	var contents []string
	for _, m := range capture.Messages {
		contents = append(contents, m.Content)
	}
	want := []string{"first prompt", "second prompt", "third prompt"}
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
}
