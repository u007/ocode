package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/tool"
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
// call, then the tool result. Used to prove non-plan results publish nothing.
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

	// A real plan write fires the store hook; no tool-result message is driven.
	ctx := tool.WithTodoScope(context.Background(), tool.TodoScope{ProjectRoot: proj, SessionID: id})
	args, _ := json.Marshal(map[string]any{"revision": 2, "todoText": "- [✓] first\n- [•] second\n- [ ] third"})
	if _, err := (tool.TodoWriteTool{}).ExecuteCtx(ctx, args); err != nil {
		t.Fatalf("todowrite: %v", err)
	}

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
	if data.Revision != 3 {
		t.Errorf("Revision = %d, want 3 (the file was at 2 and the write bumped it)", data.Revision)
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
	for _, key := range []string{"session_id", "revision", "done", "total", "current", "items"} {
		if _, ok := wire[key]; !ok {
			t.Errorf("wire payload missing %q: %v", key, wire)
		}
	}
}

func TestTodoUpdatedPushedForTaskLinkAndCarriesDetail(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)
	writeSessionTodoFile(t, proj, id, "# Todo (revision 1)\n- [ ] t1 build\n- [ ] t2 test\n  deps: t1\n")

	sub := h.bus.Subscribe(nil)
	defer h.bus.Unsubscribe(sub)

	if _, err := tool.LinkTodoItemToRun(tool.TodoScope{ProjectRoot: proj, SessionID: id}, "t1", "agent-run-4"); err != nil {
		t.Fatalf("LinkTodoItemToRun: %v", err)
	}
	env := waitForTodoUpdated(t, sub, id)
	raw, _ := json.Marshal(env.Data)
	var data TodoUpdatedEvent
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(data.Items) != 2 || data.Items[0].RunID != "agent-run-4" || data.Items[0].State != "in_progress" {
		t.Errorf("item 0 = %+v, want in_progress linked to agent-run-4", data.Items)
	}
	if !data.Items[1].Blocked || len(data.Items[1].Deps) != 1 || data.Items[1].Deps[0] != "t1" {
		t.Errorf("item 1 = %+v, want blocked by t1", data.Items[1])
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

func TestHandleGetSessionTodoMatchesEventShape(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)
	writeSessionTodoFile(t, proj, id, "# Todo (revision 1)\n- [•] t1 build\n- [ ] t2 test\n  deps: t1\n")

	rec := httptest.NewRecorder()
	h.HandleGetSessionTodo(rec, httptest.NewRequest(http.MethodGet, "/api/sessions/"+id+"/todo", nil), id)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var got TodoUpdatedEvent
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want, found, err := readTodoUpdatedEvent(proj, id)
	if err != nil || !found {
		t.Fatalf("readTodoUpdatedEvent found=%v err=%v", found, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("endpoint = %+v, event helper = %+v", got, want)
	}
	if got.Total != 2 || len(got.Items) != 2 || !got.Items[1].Blocked {
		t.Errorf("got %+v, want 2 items with t2 blocked", got)
	}
}

func TestHandleGetSessionTodoWithoutPlanIsEmpty(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)

	rec := httptest.NewRecorder()
	h.HandleGetSessionTodo(rec, httptest.NewRequest(http.MethodGet, "/api/sessions/"+id+"/todo", nil), id)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var wire map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	items, ok := wire["items"].([]any)
	if !ok || len(items) != 0 || wire["total"] != float64(0) || wire["session_id"] != id {
		t.Errorf("wire = %v, want empty items array, total 0, session_id %s", wire, id)
	}
}

func TestBuildRunDTOCarriesToolCallID(t *testing.T) {
	dto := buildRunDTO(&agent.AgentRun{ID: "agent-run-1", ToolCallID: "call_9"})
	if dto.ToolCallID != "call_9" {
		t.Errorf("ToolCallID = %q, want call_9", dto.ToolCallID)
	}
	raw, _ := json.Marshal(dto)
	if !strings.Contains(string(raw), `"toolCallId":"call_9"`) || strings.Contains(string(raw), "todoItem") {
		t.Errorf("wire = %s, want toolCallId present and todoItem omitted when unlinked", raw)
	}
}
