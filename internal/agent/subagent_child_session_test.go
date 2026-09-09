package agent

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestSubAgentMessagesStayOutOfParentTranscript guards the isolation contract
// between a dispatched child and its parent: child messages must reach
// OnSubAgentMessage (tagged with the run) and the child session persister,
// but never the parent's OnMessage/OnDelta — those feed the parent's
// live-persist buffer and chat transcript, and forwarding there wrote
// background children's turns into the parent session file.
func TestSubAgentMessagesStayOutOfParentTranscript(t *testing.T) {
	client := &contractScriptClient{childResponses: []string{"child says hi"}}
	a, taskTool := newContractTaskTool(client)
	a.SetSessionID("ses_parent")

	var mu sync.Mutex
	var parentMsgs []Message
	var subMsgs []Message
	var subRun *AgentRun
	type persisted struct {
		id     string
		title  string
		msgs   []Message
		status string
	}
	var saves []persisted

	a.OnMessage = func(m Message) {
		mu.Lock()
		defer mu.Unlock()
		parentMsgs = append(parentMsgs, m)
	}
	a.OnDelta = func(kind, text string) { t.Errorf("parent OnDelta must not receive sub-agent deltas (%s)", kind) }
	a.OnSubAgentMessage = func(run *AgentRun, m Message) {
		mu.Lock()
		defer mu.Unlock()
		subRun = run
		subMsgs = append(subMsgs, m)
	}
	a.SetChildSessionPersistence(func(id, title string, msgs []Message, meta map[string]any) error {
		mu.Lock()
		defer mu.Unlock()
		status, _ := meta["status"].(string)
		saves = append(saves, persisted{id: id, title: title, msgs: append([]Message(nil), msgs...), status: status})
		if meta["parent_session_id"] != "ses_parent" {
			t.Errorf("parent_session_id = %v, want ses_parent", meta["parent_session_id"])
		}
		if meta["run_id"] == "" || meta["run_id"] == nil {
			t.Errorf("run_id missing from child metadata")
		}
		return nil
	})
	// SetChildSessionPersistence replaces the registered tool value; re-fetch.
	taskTool = a.tools["task"].(*TaskTool)

	result, err := taskTool.Execute(json.RawMessage(`{"prompt": "do a thing", "agent": "general"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(parentMsgs) != 0 {
		t.Fatalf("parent OnMessage received %d sub-agent messages; want 0: %+v", len(parentMsgs), parentMsgs)
	}
	if len(subMsgs) == 0 || subRun == nil {
		t.Fatalf("OnSubAgentMessage not invoked (msgs=%d run=%v)", len(subMsgs), subRun)
	}
	if subRun.SessionID == "" || subRun.SessionID == "ses_parent" || !strings.HasPrefix(subRun.SessionID, "ses_parent_child_general_") {
		t.Fatalf("run.SessionID = %q, want ses_parent_child_general_<ts>", subRun.SessionID)
	}
	if !strings.Contains(result, "(Child session: "+subRun.SessionID+")") {
		t.Fatalf("result should reference child session %s: %q", subRun.SessionID, result)
	}
	if len(saves) < 2 {
		t.Fatalf("expected a running + terminal child save, got %d", len(saves))
	}
	for _, s := range saves {
		if s.id != subRun.SessionID {
			t.Fatalf("child saved under %q, want %q", s.id, subRun.SessionID)
		}
		if s.title != "Child: general" {
			t.Fatalf("child title = %q", s.title)
		}
	}
	if saves[0].status != "running" {
		t.Fatalf("first save status = %q, want running", saves[0].status)
	}
	last := saves[len(saves)-1]
	if last.status != string(RunDone) {
		t.Fatalf("final save status = %q, want %s", last.status, RunDone)
	}
	if last.msgs[0].Role != "user" || last.msgs[0].Content != "do a thing" {
		t.Fatalf("final child transcript should start with the prompt, got %+v", last.msgs[0])
	}
	if got := last.msgs[len(last.msgs)-1]; got.Role != "assistant" || got.Content != "child says hi" {
		t.Fatalf("final child transcript should end with child reply, got %+v", got)
	}
}

// Background dispatch persists the child transcript with a terminal status
// once the run finishes, without touching the parent's OnMessage.
func TestBackgroundSubAgentPersistsChildSession(t *testing.T) {
	client := &contractScriptClient{childResponses: []string{"bg done"}}
	a, taskTool := newContractTaskTool(client)
	a.SetSessionID("ses_parent")

	var mu sync.Mutex
	parentCalls := 0
	var finalStatus, finalID string
	a.OnMessage = func(Message) { mu.Lock(); parentCalls++; mu.Unlock() }
	a.SetChildSessionPersistence(func(id, title string, msgs []Message, meta map[string]any) error {
		mu.Lock()
		defer mu.Unlock()
		finalID = id
		finalStatus, _ = meta["status"].(string)
		return nil
	})
	taskTool = a.tools["task"].(*TaskTool)

	out, err := taskTool.Execute(json.RawMessage(`{"prompt": "bg thing", "agent": "general", "run_in_background": true}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "task_id:") {
		t.Fatalf("expected background handoff, got %q", out)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		st := finalStatus
		mu.Unlock()
		if st == string(RunDone) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("background child never persisted with done status (last=%q)", st)
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if parentCalls != 0 {
		t.Fatalf("parent OnMessage received %d sub-agent messages; want 0", parentCalls)
	}
	if !strings.HasPrefix(finalID, "ses_parent_child_general_") {
		t.Fatalf("child session id = %q", finalID)
	}
}
