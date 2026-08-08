package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/tool"
)

// fakeClientAskBash: first call returns a bash tool call, second returns a plain assistant message.
type fakeClientAskBash struct{ calls int }

func (f *fakeClientAskBash) Chat([]agent.Message, []map[string]interface{}) (*agent.Message, error) {
	f.calls++
	if f.calls == 1 {
		return &agent.Message{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: "call-1", Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "bash", Arguments: `{"command":"rm -rf build"}`}}}}, nil
	}
	return &agent.Message{Role: "assistant", Content: "done"}, nil
}
func (f *fakeClientAskBash) GetProvider() string { return "fake" }
func (f *fakeClientAskBash) GetModel() string    { return "fake-model" }

func TestReproPermissionResolveHeadless(t *testing.T) {
	h := NewHandler()
	ag := agent.NewAgent(&fakeClientAskBash{}, nil, nil, nil)
	as := &agentSession{agent: ag, model: "fake-model", messages: nil}
	h.agents["sess-1"] = as

	body := `{"content":"list files","sessionId":"sess-1"}`
	req := httptest.NewRequest("POST", "/api/chat", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.HandleChat(rec, req)
	if rec.Code != 200 {
		t.Fatalf("chat failed: %d %s", rec.Code, rec.Body.String())
	}
	last := as.messages[len(as.messages)-1]
	if !strings.HasPrefix(last.Content, tool.SentinelPermissionAsk) {
		t.Fatalf("expected PERMISSION_ASK at tail, got %q", last.Content)
	}
	permReqID := last.ToolID

	body2, _ := json.Marshal(map[string]interface{}{"request_id": permReqID, "session_id": "sess-1", "approved": true})
	req2 := httptest.NewRequest("POST", "/api/permissions/resolve", strings.NewReader(string(body2)))
	rec2 := httptest.NewRecorder()
	h.HandleResolvePermission(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("resolve failed: %d %s", rec2.Code, rec2.Body.String())
	}
	t.Logf("headless resolve OK")
}

// TestReproPermissionResolveRCBridge simulates the web->TUI bridge path.
func TestReproPermissionResolveRCBridge(t *testing.T) {
	resolveCh := make(chan RCResolution, 4)
	h := NewHandler()
	h.rc = &RCBridge{SessionID: "tui-sess", ResolveCh: resolveCh}

	// Subscribe a headless sub (the web browser connects via rc.Subscribe in
	// HandleSessionMessages, but for the resolve broadcast the server uses
	// broadcastEvent -> headlessSubs only when rc==nil; in RC mode the TUI
	// broadcasts. Simulate the browser mirror via bridge subscribe.)
	sub := h.rc.Subscribe()
	defer h.rc.Unsubscribe(sub)

	body := `{"request_id":"call-1","approved":true}`
	req := httptest.NewRequest("POST", "/api/permissions/resolve", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.HandleResolvePermission(rec, req)
	if rec.Code != 200 {
		t.Fatalf("resolve failed: %d %s", rec.Code, rec.Body.String())
	}
	t.Logf("rc-bridge resolve returned 200")

	// The web closes the dialog on POST 200 (resolvePermission dispatches
	// PERMISSION_RESOLVED). The SSE permission_resolved is a secondary signal.
	select {
	case res := <-resolveCh:
		t.Logf("resolution forwarded to TUI: %+v", res)
	case <-time.After(time.Second):
		t.Fatalf("no resolution forwarded to TUI")
	}
}
