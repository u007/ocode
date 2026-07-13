package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/tool"
)

// permissionAskContent builds the tool-result content a paused permission ask
// carries, matching Agent.handleToolCall's sentinel path.
func permissionAskContent(t *testing.T, req agent.PermissionRequest) string {
	t.Helper()
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal permission request: %v", err)
	}
	return tool.SentinelPermissionAsk + string(data)
}

func samplePermissionRequest() agent.PermissionRequest {
	return agent.PermissionRequest{
		ToolName: "bash",
		Command:  "rm -rf build",
		Args:     json.RawMessage(`{"command":"rm -rf build"}`),
		Scope:    agent.PermissionScopeBashPrefix,
		Rule:     "bash.prefix.rm",
	}
}

func TestParsePermissionAsk(t *testing.T) {
	content := permissionAskContent(t, samplePermissionRequest())
	req, ok := parsePermissionAsk(content)
	if !ok {
		t.Fatalf("parsePermissionAsk returned ok=false for a valid ask")
	}
	if req.ToolName != "bash" || req.Command != "rm -rf build" {
		t.Fatalf("unexpected parse result: %+v", req)
	}

	if _, ok := parsePermissionAsk("just a normal tool result"); ok {
		t.Fatalf("parsePermissionAsk should reject non-permission content")
	}
	if _, ok := parsePermissionAsk(tool.SentinelPermissionAsk + "not-json"); ok {
		t.Fatalf("parsePermissionAsk should reject malformed JSON payload")
	}
	if _, ok := parsePermissionAsk(tool.SentinelPermissionAsk + `{"command":"x"}`); ok {
		t.Fatalf("parsePermissionAsk should reject a request missing tool_name")
	}
}

func TestNewPermissionEvent(t *testing.T) {
	ev := newPermissionEvent("call-1", samplePermissionRequest())
	if ev.RequestID != "call-1" || ev.Tool != "bash" || ev.Command != "rm -rf build" {
		t.Fatalf("unexpected event: %+v", ev)
	}
	// Command falls back to raw args when the request carries no Command.
	req := agent.PermissionRequest{ToolName: "write", Args: json.RawMessage(`{"path":".env"}`)}
	ev = newPermissionEvent("call-2", req)
	if ev.Command != `{"path":".env"}` {
		t.Fatalf("expected command to fall back to args JSON, got %q", ev.Command)
	}
}

func TestTailIsPermissionAskResolve(t *testing.T) {
	ask := agent.Message{Role: "tool", ToolID: "call-1", Content: permissionAskContent(t, samplePermissionRequest())}

	if !tailIsPermissionAsk([]agent.Message{{Role: "user", Content: "hi"}, ask}) {
		t.Fatalf("expected tail permission ask to be detected")
	}
	if tailIsPermissionAsk([]agent.Message{ask, {Role: "assistant", Content: "done"}}) {
		t.Fatalf("resolved ask should not count as pending")
	}
	// A question ask is not a permission ask.
	q := agent.Message{Role: "tool", Content: tool.SentinelQuestionPrompt + "\n[]\n\n" + tool.SentinelWaitingForUser}
	if tailIsPermissionAsk([]agent.Message{q}) {
		t.Fatalf("question ask should not be a permission ask")
	}
}

func TestHandleResolvePermissionValidation(t *testing.T) {
	cases := []struct {
		name string
		body string
		want int
	}{
		{"bad json", `{`, http.StatusBadRequest},
		{"missing request_id", `{"approved":true}`, http.StatusBadRequest},
		{"no pending permission", `{"request_id":"call-1","approved":true}`, http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHandler()
			req := httptest.NewRequest("POST", "/api/permissions/resolve", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			h.HandleResolvePermission(rec, req)
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d (body=%s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestHandleResolvePermissionForwardsToBridgeWhenBridged(t *testing.T) {
	resolveCh := make(chan RCResolution, 1)
	h := NewHandler()
	h.rc = &RCBridge{SessionID: "tui-sess", ResolveCh: resolveCh}

	// Approve must forward an "allow" resolution; deny an explicit "deny".
	for _, tc := range []struct {
		approved bool
		want     string
	}{
		{true, "allow"},
		{false, "deny"},
	} {
		body := `{"request_id":"call-1","approved":` + strconv.FormatBool(tc.approved) + `}`
		req := httptest.NewRequest("POST", "/api/permissions/resolve", strings.NewReader(body))
		rec := httptest.NewRecorder()
		h.HandleResolvePermission(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("approved=%v status = %d, want 200 (body=%s)", tc.approved, rec.Code, rec.Body.String())
		}
		select {
		case res := <-resolveCh:
			if res.RequestID != "call-1" || res.Decision != tc.want {
				t.Fatalf("approved=%v unexpected resolution: %+v", tc.approved, res)
			}
		case <-time.After(time.Second):
			t.Fatalf("approved=%v no resolution forwarded to the bridge", tc.approved)
		}
	}
}

// TestHandleResolvePermissionDeniesAndContinues exercises the deny path, which
// is structurally identical to the approve path (replace the sentinel in place,
// re-Step, broadcast) but injects no tool execution — so it works with an agent
// that has no registered tools. The approve path's execution is covered by the
// TUI's HandleApprovedToolCall tests.
func TestHandleResolvePermissionDeniesAndContinues(t *testing.T) {
	h := NewHandler()
	ag := agent.NewAgent(questionFakeClient{}, nil, nil, nil)
	as := &agentSession{
		agent: ag,
		model: "fake-model",
		messages: []agent.Message{
			{Role: "user", Content: "clean the build"},
			{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: "call-1"}}},
			{Role: "tool", ToolID: "call-1", Content: permissionAskContent(t, samplePermissionRequest())},
		},
	}
	h.agents["sess-1"] = as

	sub := h.subscribeHeadless()
	defer h.unsubscribeHeadless(sub)

	body := `{"request_id":"call-1","approved":false}`
	req := httptest.NewRequest("POST", "/api/permissions/resolve", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.HandleResolvePermission(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}

	// The pending ask must have been replaced with a denied tool result.
	resolved := as.messages[2]
	if strings.HasPrefix(resolved.Content, tool.SentinelPermissionAsk) {
		t.Fatalf("permission ask was not resolved: %q", resolved.Content)
	}
	if !strings.Contains(resolved.Content, "denied") {
		t.Fatalf("expected a denied tool result, got %q", resolved.Content)
	}
	// The agent's follow-up assistant message must be appended.
	if last := as.messages[len(as.messages)-1]; last.Role != "assistant" || last.Content == "" {
		t.Fatalf("expected assistant continuation, got %+v", last)
	}

	sawResolved := false
	for drained := false; !drained; {
		select {
		case ev := <-sub:
			if ev.Event == "permission_resolved" {
				sawResolved = true
			}
		default:
			drained = true
		}
	}
	if !sawResolved {
		t.Fatalf("expected a permission_resolved mirror event")
	}
}

// TestHandleResolvePermissionAlreadyResolved verifies the under-lock re-check:
// once a later message follows the ask, a resolve attempt is a 409.
func TestHandleResolvePermissionAlreadyResolved(t *testing.T) {
	h := NewHandler()
	ag := agent.NewAgent(questionFakeClient{}, nil, nil, nil)
	as := &agentSession{
		agent: ag,
		model: "fake-model",
		messages: []agent.Message{
			{Role: "tool", ToolID: "call-1", Content: permissionAskContent(t, samplePermissionRequest())},
			{Role: "assistant", Content: "already continued"},
		},
	}
	h.agents["sess-1"] = as

	// The tail is no longer a permission ask, so lookup finds nothing → 404.
	body := `{"request_id":"call-1","approved":false}`
	req := httptest.NewRequest("POST", "/api/permissions/resolve", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.HandleResolvePermission(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body=%s)", rec.Code, rec.Body.String())
	}
}
