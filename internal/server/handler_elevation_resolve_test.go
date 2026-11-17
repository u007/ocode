package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/agent"
)

// TestChildPermResolveCarriesPasswordIntoSubAgentResponse proves the web path
// does not discard a sub-agent elevation password: the write-only body field is
// delivered to the parked child in its PermissionResponse, and never echoed in
// the HTTP response or the broadcast SSE frame.
func TestChildPermResolveCarriesPasswordIntoSubAgentResponse(t *testing.T) {
	const secret = "childElevSecret-9f3a"
	h := NewHandler()
	as := &agentSession{
		agent:     agent.NewAgent(questionFakeClient{}, nil, nil, nil),
		model:     "fake-model",
		childAsks: newChildPermAsks(),
	}
	h.agents["sess-child-elev"] = as

	req := samplePermissionRequest()
	req.NeedsPassword = true
	req.ElevationCommand = "sudo whoami"
	const reqID = "childperm-elev-1"
	respCh := make(chan agent.PermissionResponse, 1)
	if !as.childAsks.add(reqID, &childPermAsk{req: req, event: newPermissionEvent(reqID, req), respCh: respCh}) {
		t.Fatal("could not park child elevation ask")
	}

	rec := httptest.NewRecorder()
	if !h.handleChildPermResolve(rec, "sess-child-elev", reqID, PermDecisionAllow, "", secret) {
		t.Fatal("child perm resolve did not own the request")
	}
	select {
	case resp := <-respCh:
		if resp.Level != agent.PermissionAllow {
			t.Fatalf("delivered level = %v, want allow", resp.Level)
		}
		if resp.ElevationPassword != secret {
			t.Fatalf("delivered elevation password = %q, want %q", resp.ElevationPassword, secret)
		}
	default:
		t.Fatal("no response delivered to the parked child")
	}
	if strings.Contains(rec.Body.String(), secret) {
		t.Fatalf("secret leaked into HTTP response: %q", rec.Body.String())
	}
}

// TestResolveElevationStoresSecretBeforeReExecution closes the wiring gap
// between the write-only password field on /api/permissions/resolve and the
// agent's per-request elevation store. The bash tool fails closed when the
// flag is set but no secret was stored, so the store MUST happen before the
// approved call re-executes. The spy seam records the order; a real response
// and every SSE frame are checked for echoes of the secret.
func TestResolveElevationStoresSecretBeforeReExecution(t *testing.T) {
	const secret = "elev-store-secret-77a1"

	ask := samplePermissionRequest()
	ask.NeedsPassword = true
	ask.ElevationCommand = "sudo whoami"

	h := NewHandler()
	ag := agent.NewAgent(questionFakeClient{}, nil, nil, nil)
	as := &agentSession{
		agent: ag,
		model: "fake-model",
		messages: []agent.Message{
			{Role: "user", Content: "who am i"},
			{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: "call-1"}}},
			{Role: "tool", ToolID: "call-1", Content: permissionAskContent(t, ask)},
		},
	}
	h.agents["sess-1"] = as
	h.sessions.Register("sess-1", t.TempDir())

	var order []string
	origExec := executeApprovedWithTempPathFn
	origStore := storeResolveElevationSecret
	executeApprovedWithTempPathFn = func(*agent.Agent, string, json.RawMessage, string, string) (string, error) {
		order = append(order, "execute")
		return "root", nil
	}
	storeResolveElevationSecret = func(_ *agent.Agent, requestID, password string) {
		order = append(order, "store:"+requestID+":"+password)
	}
	t.Cleanup(func() {
		executeApprovedWithTempPathFn = origExec
		storeResolveElevationSecret = origStore
	})

	sub := h.subscribeHeadless()
	defer h.unsubscribeHeadless(sub)

	body := fmt.Sprintf(`{"request_id":"call-1","decision":"allow","password":%q}`, secret)
	req := httptest.NewRequest(http.MethodPost, "/api/permissions/resolve", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.HandleResolvePermission(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (body=%s)", rec.Code, rec.Body.String())
	}
	waitForAskContinuation(t, as)

	if len(order) != 2 || order[0] != "store:call-1:"+secret || order[1] != "execute" {
		t.Fatalf("store must precede re-execution with the exact password; got %v", order)
	}
	if strings.Contains(rec.Body.String(), secret) {
		t.Errorf("password echoed in resolve response: %s", rec.Body.String())
	}
	for drained := false; !drained; {
		select {
		case ev := <-sub:
			wire, _ := json.Marshal(ev)
			if strings.Contains(string(wire), secret) {
				t.Errorf("password leaked into SSE frame %s: %s", ev.Event, wire)
			}
		default:
			drained = true
		}
	}
}

// TestResolveOrdinaryAskIgnoresPassword keeps the password path narrow: a
// non-elevation ask never reaches the store even if a hand-crafted request
// carries the field. Ordinary approvals stay byte-identical to before.
func TestResolveOrdinaryAskIgnoresPassword(t *testing.T) {
	ask := samplePermissionRequest() // NeedsPassword is false

	h := NewHandler()
	ag := agent.NewAgent(questionFakeClient{}, nil, nil, nil)
	as := &agentSession{
		agent: ag,
		model: "fake-model",
		messages: []agent.Message{
			{Role: "user", Content: "clean the build"},
			{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: "call-1"}}},
			{Role: "tool", ToolID: "call-1", Content: permissionAskContent(t, ask)},
		},
	}
	h.agents["sess-1"] = as
	h.sessions.Register("sess-1", t.TempDir())

	var storeCalled bool
	origExec := executeApprovedWithTempPathFn
	origStore := storeResolveElevationSecret
	executeApprovedWithTempPathFn = func(*agent.Agent, string, json.RawMessage, string, string) (string, error) {
		return "ok", nil
	}
	storeResolveElevationSecret = func(*agent.Agent, string, string) { storeCalled = true }
	t.Cleanup(func() {
		executeApprovedWithTempPathFn = origExec
		storeResolveElevationSecret = origStore
	})

	body := `{"request_id":"call-1","decision":"allow","password":"should-be-ignored"}`
	req := httptest.NewRequest(http.MethodPost, "/api/permissions/resolve", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.HandleResolvePermission(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (body=%s)", rec.Code, rec.Body.String())
	}
	waitForAskContinuation(t, as)
	if storeCalled {
		t.Fatal("ordinary ask reached the elevation secret store")
	}
}
