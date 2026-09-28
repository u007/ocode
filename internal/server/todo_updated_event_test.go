package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
)

// waitForTodoUpdated drains sub until a todo_updated envelope for sessionID
// arrives, or the deadline expires. Other event types are ignored.
func waitForTodoUpdated(t *testing.T, sub chan Envelope, sessionID string) Envelope {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case env := <-sub:
			if env.Event == "todo_updated" && env.SessionID == sessionID {
				return env
			}
		case <-deadline:
			t.Fatalf("no todo_updated event for %s within deadline", sessionID)
		}
	}
}

// driveToolMessages feeds wireHeadlessAgentCallbacks the two-message sequence a
// real turn produces for one tool call: the assistant message declaring the
// call, then the tool result. The map the callback builds from the assistant
// ToolCalls is the only way it can learn which tool a ToolID refers to.
func driveToolMessages(t *testing.T, h *Handler, sessionID, callID, toolName string) {
	t.Helper()
	ag := &agent.Agent{}
	h.wireHeadlessAgentCallbacks(sessionID, ag)
	ag.OnMessage(agent.Message{
		Role: "assistant",
		ToolCalls: []agent.ToolCall{{ID: callID, Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: toolName, Arguments: `{}`}}},
	})
	ag.OnMessage(agent.Message{Role: "tool", ToolID: callID, Content: "ok"})
}

func writeSessionTodoFile(t *testing.T, root, sessionID, content string) {
	t.Helper()
	dir := filepath.Join(root, ".ocode", "todo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, sessionID+".md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write todo file: %v", err)
	}
}

func TestTodoUpdatedEventCarriesSummary(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)
	writeSessionTodoFile(t, proj, id, "# Todo (revision 2)\n- [✓] t1 first\n- [•] t2 second\n- [ ] t3 third\n")

	sub := h.bus.Subscribe(nil)
	defer h.bus.Unsubscribe(sub)

	driveToolMessages(t, h, id, "call_1", "todowrite")

	env := waitForTodoUpdated(t, sub, id)
	if env.Project != proj {
		t.Errorf("envelope Project = %q, want %q", env.Project, proj)
	}
	var data TodoUpdatedEvent
	raw, err := json.Marshal(env.Data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if data.SessionID != id {
		t.Errorf("SessionID = %q, want %q", data.SessionID, id)
	}
	if data.Done != 1 || data.Total != 3 {
		t.Errorf("Done/Total = %d/%d, want 1/3", data.Done, data.Total)
	}
	if data.Current != "second" {
		t.Errorf("Current = %q, want %q", data.Current, "second")
	}
	if len(data.Items) != 3 || data.Items[1].State != "in_progress" {
		t.Errorf("Items = %+v, want 3 items with t2 in_progress", data.Items)
	}

	// The wire shape is a contract with the web client, not just this struct.
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("unmarshal generic: %v", err)
	}
	for _, key := range []string{"session_id", "done", "total", "current", "items"} {
		if _, ok := wire[key]; !ok {
			t.Errorf("wire payload missing %q: %v", key, wire)
		}
	}
}

func TestTodoUpdatedNotPublishedForOtherTools(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)
	// A todo file exists for this session, but the call that finished was not
	// todowrite — publishing here would fire on every tool result in a turn.
	writeSessionTodoFile(t, proj, id, "# Todo (revision 1)\n- [ ] t1 work\n")

	sub := h.bus.Subscribe(nil)
	defer h.bus.Unsubscribe(sub)

	driveToolMessages(t, h, id, "call_1", "bash")

	deadline := time.After(300 * time.Millisecond)
	for {
		select {
		case env := <-sub:
			if env.Event == "todo_updated" {
				t.Fatalf("todo_updated published for a bash tool result: %+v", env.Data)
			}
		case <-deadline:
			return
		}
	}
}

func TestTodoUpdatedSkippedForUnknownToolID(t *testing.T) {
	// A tool result whose ToolID was never declared (e.g. a resumed transcript
	// whose assistant message was trimmed) must not guess a tool name.
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)
	writeSessionTodoFile(t, proj, id, "# Todo (revision 1)\n- [ ] t1 work\n")

	sub := h.bus.Subscribe(nil)
	defer h.bus.Unsubscribe(sub)

	ag := &agent.Agent{}
	h.wireHeadlessAgentCallbacks(id, ag)
	ag.OnMessage(agent.Message{Role: "tool", ToolID: "never_declared", Content: "ok"})

	deadline := time.After(300 * time.Millisecond)
	for {
		select {
		case env := <-sub:
			if env.Event == "todo_updated" {
				t.Fatalf("todo_updated published for an undeclared ToolID: %+v", env.Data)
			}
		case <-deadline:
			return
		}
	}
}

func TestTodoUpdatedIsSessionScoped(t *testing.T) {
	// sessionScopedEvents is the bus's loud guard against an untagged event; a
	// registration typo would make Publish log an error and drop the event.
	if !sessionScopedEvents["todo_updated"] {
		t.Error(`sessionScopedEvents is missing "todo_updated"`)
	}
	if liveFrameEvents["todo_updated"] {
		t.Error(`"todo_updated" must NOT be in liveFrameEvents — it is a momentary reading, replaying it after a reconnect would show a stale plan`)
	}
}
