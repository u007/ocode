package server

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
)

// fakeClientAskBashE2E: first call returns a bash tool call, second returns a plain assistant message.
type fakeClientAskBashE2E struct{ calls int }

func (f *fakeClientAskBashE2E) Chat([]agent.Message, []map[string]interface{}) (*agent.Message, error) {
	f.calls++
	if f.calls == 1 {
		return &agent.Message{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: "call-1", Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "bash", Arguments: `{"command":"rm -rf build"}`}}}}, nil
	}
	return &agent.Message{Role: "assistant", Content: "done"}, nil
}
func (f *fakeClientAskBashE2E) GetProvider() string { return "fake" }
func (f *fakeClientAskBashE2E) GetModel() string    { return "fake-model" }

// TestE2EWebPermissionFlow simulates exactly what the browser does:
//  1. GET /api/chat/messages  (mirror subscription)
//  2. POST /api/chat          (first message creates session)
//  3. wait for `permission` SSE frame
//  4. POST /api/permissions/resolve (approve)
//  5. wait for `permission_resolved` SSE frame
func TestE2EWebPermissionFlow(t *testing.T) {
	h := NewHandler()
	if h.cfg != nil {
		h.cfg.Model = "fake-model"
	}
	ag := agent.NewAgent(&fakeClientAskBashE2E{}, nil, nil, nil)
	_ = ag
	// Pre-register a session like the browser would after first message
	h.agents["sess-browser"] = &agentSession{agent: ag, model: "fake-model", messages: nil}

	sub := h.subscribeHeadless()
	defer h.unsubscribeHeadless(sub)

	// Step 2: first message
	body := `{"content":"do something","sessionId":"sess-browser"}`
	req := httptest.NewRequest("POST", "/api/chat", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.HandleChat(rec, req)
	if rec.Code != 200 {
		t.Fatalf("chat failed: %d %s", rec.Code, rec.Body.String())
	}

	// Step 3: wait for the permission frame
	var permReqID string
	deadline := time.After(3 * time.Second)
	for permReqID == "" {
		select {
		case ev := <-sub:
			if ev.Event == "permission" {
				var pe PermissionEvent
				b, _ := json.Marshal(ev.Data)
				_ = json.Unmarshal(b, &pe)
				permReqID = pe.RequestID
				t.Logf("got permission frame: request_id=%s", permReqID)
			}
		case <-deadline:
			t.Fatalf("timed out waiting for permission frame")
		}
	}

	// Step 4: approve
	resBody, _ := json.Marshal(map[string]interface{}{"request_id": permReqID, "session_id": "sess-browser", "approved": true})
	req2 := httptest.NewRequest("POST", "/api/permissions/resolve", strings.NewReader(string(resBody)))
	rec2 := httptest.NewRecorder()
	h.HandleResolvePermission(rec2, req2)
	t.Logf("resolve status=%d body=%s", rec2.Code, rec2.Body.String())
	if rec2.Code != 200 {
		t.Fatalf("resolve failed: %d %s", rec2.Code, rec2.Body.String())
	}

	// Step 5: expect permission_resolved frame on the mirror
	deadline2 := time.After(3 * time.Second)
	for {
		select {
		case ev := <-sub:
			t.Logf("mirror frame: %s", ev.Event)
			if ev.Event == "permission_resolved" {
				t.Logf("permission_resolved received — dialog would close")
				return
			}
			if ev.Event == "error" {
				t.Fatalf("error frame on mirror: %v", ev.Data)
			}
		case <-deadline2:
			t.Fatalf("timed out waiting for permission_resolved frame")
		}
	}
}

var _ = fmt.Sprintf
