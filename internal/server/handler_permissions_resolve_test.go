package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

	// Guard the no-broadcast invariant: in bridge mode the server must NOT
	// emit permission_resolved on headlessSubs — the bridged web client listens
	// on the TUI's bridge channel, and the TUI broadcasts the dismissal itself
	// once it applies the resolution.
	evCh := h.subscribeHeadless()
	defer h.unsubscribeHeadless(evCh)

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
		select {
		case ev := <-evCh:
			if ev.Event == "permission_resolved" {
				t.Fatalf("approved=%v server must not broadcast permission_resolved in bridge mode (got SessionID=%q)", tc.approved, ev.SessionID)
			}
		case <-time.After(100 * time.Millisecond):
			// Expected: no headless-channel event for the bridged resolution.
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

// ─── Always-allow decisions (web/desktop parity with the TUI dialog) ────────

// isolatedConfigHome redirects HOME to a temp dir so the ocodeconfig.json
// writes performed by always-allow persistence (config.SaveSingleToolRule &
// friends target $HOME/.config/opencode/ocodeconfig.json) never touch real
// developer state. It also redirects OPENCODE_CONFIG_DIR for good measure.
func isolatedConfigHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("OPENCODE_CONFIG_DIR", filepath.Join(tmp, "cfgdir"))
	return tmp
}

// savedOcodeConfig reads back the ocodeconfig.json written under the isolated
// HOME. The second return is false when no file was written at all.
func savedOcodeConfig(t *testing.T) (map[string]any, bool) {
	t.Helper()
	path := filepath.Join(os.Getenv("HOME"), ".config", "opencode", "ocodeconfig.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false
		}
		t.Fatalf("read ocodeconfig.json: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse ocodeconfig.json: %v", err)
	}
	return m, true
}

func resolveBody(t *testing.T, h *Handler, as *agentSession, body string) *httptest.ResponseRecorder {
	t.Helper()
	h.agents["sess-1"] = as
	req := httptest.NewRequest("POST", "/api/permissions/resolve", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.HandleResolvePermission(rec, req)
	return rec
}

func TestHandleResolvePermissionAlwaysRulePersistsBashPrefix(t *testing.T) {
	isolatedConfigHome(t)
	h := NewHandler()
	ag := agent.NewAgent(questionFakeClient{}, nil, nil, nil)
	askReq := agent.PermissionRequest{
		ToolName: "bash",
		Command:  "rm -rf build",
		Args:     json.RawMessage(`{"command":"rm -rf build"}`),
		Scope:    agent.PermissionScopeBashPrefix,
		Prefix:   "rm",
		Rule:     "bash.prefix.rm",
	}
	as := &agentSession{
		agent: ag,
		model: "fake-model",
		messages: []agent.Message{
			{Role: "user", Content: "clean the build"},
			{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: "call-1"}}},
			{Role: "tool", ToolID: "call-1", Content: permissionAskContent(t, askReq)},
		},
	}

	rec := resolveBody(t, h, as, `{"request_id":"call-1","decision":"always_rule"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}

	// The prefix rule must be persisted to config.
	cfg, ok := savedOcodeConfig(t)
	if !ok {
		t.Fatalf("expected ocodeconfig.json to be written by always_rule persistence")
	}
	perms, _ := cfg["permissions"].(map[string]any)
	bash, _ := perms["bash"].(map[string]any)
	prefixes, _ := bash["prefixes"].(map[string]any)
	if prefixes == nil || prefixes["rm"] != "allow" {
		t.Fatalf("expected permissions.bash.prefixes.rm=allow, got %v", prefixes)
	}

	// And future rm commands must now decide allow without asking.
	d := ag.Permissions().Decide("bash", json.RawMessage(`{"command":"rm -rf build"}`))
	if d.Level != agent.PermissionAllow {
		t.Fatalf("expected Decide(bash, rm ...) = allow after always_rule, got %v", d.Level)
	}
}

func TestHandleResolvePermissionAlwaysRuleWebfetchDomainIsSessionScoped(t *testing.T) {
	isolatedConfigHome(t)
	h := NewHandler()
	ag := agent.NewAgent(questionFakeClient{}, nil, nil, nil)
	askReq := agent.PermissionRequest{
		ToolName: "webfetch",
		Command:  "https://example.com/docs",
		Args:     json.RawMessage(`{"url":"https://example.com/docs"}`),
		Scope:    agent.PermissionScopeTool,
		Rule:     "webfetch.domain.example.com",
	}
	as := &agentSession{
		agent: ag,
		model: "fake-model",
		messages: []agent.Message{
			{Role: "user", Content: "fetch the docs"},
			{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: "call-1"}}},
			{Role: "tool", ToolID: "call-1", Content: permissionAskContent(t, askReq)},
		},
	}

	rec := resolveBody(t, h, as, `{"request_id":"call-1","decision":"always_rule"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}

	// Same domain → allowed from the session cache; no tool rule persisted.
	d := ag.Permissions().Decide("webfetch", json.RawMessage(`{"url":"https://example.com/more"}`))
	if d.Level != agent.PermissionAllow {
		t.Fatalf("expected session domain cache allow, got %v", d.Level)
	}
	if _, ok := savedOcodeConfig(t); ok {
		t.Fatalf("webfetch domain grants are session-scoped — no ocodeconfig.json write expected")
	}
}

func TestHandleResolvePermissionAlwaysToolPersistsUserConfirmedRule(t *testing.T) {
	isolatedConfigHome(t)
	h := NewHandler()
	ag := agent.NewAgent(questionFakeClient{}, nil, nil, nil)
	outPath := filepath.Join(t.TempDir(), "secret.txt")
	askReq := agent.PermissionRequest{
		ToolName: "delete",
		Command:  outPath,
		Args:     json.RawMessage(`{"path":"` + outPath + `"}`),
		Scope:    agent.PermissionScopeTool,
		Rule:     "tool.delete",
	}
	as := &agentSession{
		agent: ag,
		model: "fake-model",
		messages: []agent.Message{
			{Role: "user", Content: "remove the file"},
			{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: "call-1"}}},
			{Role: "tool", ToolID: "call-1", Content: permissionAskContent(t, askReq)},
		},
	}

	rec := resolveBody(t, h, as, `{"request_id":"call-1","decision":"always_tool"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}

	cfg, ok := savedOcodeConfig(t)
	if !ok {
		t.Fatalf("expected ocodeconfig.json to be written by always_tool persistence")
	}
	perms, _ := cfg["permissions"].(map[string]any)
	tools, _ := perms["tools"].(map[string]any)
	if tools == nil || tools["delete"] != "allow" {
		t.Fatalf("expected permissions.tools.delete=allow, got %v", tools)
	}

	// The user-confirmed rule bypasses the out-of-scope path gate.
	d := ag.Permissions().Decide("delete", json.RawMessage(`{"path":"`+outPath+`"}`))
	if d.Level != agent.PermissionAllow {
		t.Fatalf("expected user-confirmed delete to bypass path gate, got %v", d.Level)
	}
}

func TestHandleResolvePermissionAlwaysRefusedForHarmfulRequest(t *testing.T) {
	isolatedConfigHome(t)
	for _, decision := range []string{"always_rule", "always_tool"} {
		t.Run(decision, func(t *testing.T) {
			h := NewHandler()
			ag := agent.NewAgent(questionFakeClient{}, nil, nil, nil)
			askReq := samplePermissionRequest() // rm -rf build is not harmful per se; use a hard-blocked git command
			askReq = agent.PermissionRequest{
				ToolName: "bash",
				Command:  "git reset --hard HEAD~1",
				Args:     json.RawMessage(`{"command":"git reset --hard HEAD~1"}`),
				Scope:    agent.PermissionScopeBashPrefix,
				Prefix:   "git reset",
				Rule:     "bash.prefix.git reset",
			}
			as := &agentSession{
				agent: ag,
				model: "fake-model",
				messages: []agent.Message{
					{Role: "user", Content: "undo last commit"},
					{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: "call-1"}}},
					{Role: "tool", ToolID: "call-1", Content: permissionAskContent(t, askReq)},
				},
			}

			rec := resolveBody(t, h, as, `{"request_id":"call-1","decision":"`+decision+`"}`)
			if rec.Code != http.StatusConflict {
				t.Fatalf("status = %d, want 409 (body=%s)", rec.Code, rec.Body.String())
			}
			// The ask must stay pending so the user can pick allow-once/deny.
			if !tailIsPermissionAsk(as.messages) {
				t.Fatalf("harmful always-allow refusal must keep the ask pending")
			}
			if _, ok := savedOcodeConfig(t); ok {
				t.Fatalf("no config write expected when harmful request refuses persistence")
			}
		})
	}
}

func TestHandleResolvePermissionAlwaysAvailabilityGuards(t *testing.T) {
	cases := []struct {
		name     string
		askReq   agent.PermissionRequest
		decision string
	}{
		{
			name:     "always_tool unavailable for bash",
			askReq:   samplePermissionRequest(),
			decision: "always_tool",
		},
		{
			name: "always_rule unavailable for git prefix",
			askReq: agent.PermissionRequest{
				ToolName: "bash",
				Command:  "git push origin main",
				Args:     json.RawMessage(`{"command":"git push origin main"}`),
				Scope:    agent.PermissionScopeBashPrefix,
				Prefix:   "git push",
				Rule:     "bash.prefix.git push",
			},
			decision: "always_rule",
		},
		{
			name: "always_rule unavailable for shell control keyword",
			askReq: agent.PermissionRequest{
				ToolName: "bash",
				Command:  "while true; do sleep 1; done",
				Args:     json.RawMessage(`{"command":"while true; do sleep 1; done"}`),
				Scope:    agent.PermissionScopeBashPrefix,
				Prefix:   "while",
				Rule:     "bash.prefix.while",
			},
			decision: "always_rule",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolatedConfigHome(t)
			h := NewHandler()
			ag := agent.NewAgent(questionFakeClient{}, nil, nil, nil)
			as := &agentSession{
				agent: ag,
				model: "fake-model",
				messages: []agent.Message{
					{Role: "user", Content: "run it"},
					{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: "call-1"}}},
					{Role: "tool", ToolID: "call-1", Content: permissionAskContent(t, tc.askReq)},
				},
			}
			rec := resolveBody(t, h, as, `{"request_id":"call-1","decision":"`+tc.decision+`"}`)
			if rec.Code != http.StatusConflict {
				t.Fatalf("status = %d, want 409 (body=%s)", rec.Code, rec.Body.String())
			}
			if !tailIsPermissionAsk(as.messages) {
				t.Fatalf("availability refusal must keep the ask pending")
			}
		})
	}
}

func TestHandleResolvePermissionRequiresDecisionOrApproved(t *testing.T) {
	h := NewHandler()
	req := httptest.NewRequest("POST", "/api/permissions/resolve", strings.NewReader(`{"request_id":"call-1"}`))
	rec := httptest.NewRecorder()
	h.HandleResolvePermission(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest("POST", "/api/permissions/resolve", strings.NewReader(`{"request_id":"call-1","decision":"whenever"}`))
	rec = httptest.NewRecorder()
	h.HandleResolvePermission(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid decision: status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestHandleResolvePermissionForwardsNewDecisionsToBridge(t *testing.T) {
	resolveCh := make(chan RCResolution, 1)
	h := NewHandler()
	h.rc = &RCBridge{SessionID: "tui-sess", ResolveCh: resolveCh}

	for _, tc := range []struct {
		body string
		want string
	}{
		{`{"request_id":"call-1","decision":"always_rule"}`, "always"},
		{`{"request_id":"call-1","decision":"always_tool"}`, "always_tool"},
	} {
		req := httptest.NewRequest("POST", "/api/permissions/resolve", strings.NewReader(tc.body))
		rec := httptest.NewRecorder()
		h.HandleResolvePermission(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", tc.body, rec.Code)
		}
		select {
		case res := <-resolveCh:
			if res.Decision != tc.want {
				t.Fatalf("%s forwarded decision = %q, want %q", tc.body, res.Decision, tc.want)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s no resolution forwarded to the bridge", tc.body)
		}
	}
}

// The dismissal broadcast must fire BEFORE the continuation round: dialogs
// close the moment the decision lands, not after the next model round-trip.
func TestHandleResolvePermissionBroadcastsResolvedBeforeContinuation(t *testing.T) {
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

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.HandleResolvePermission(rec, req)
	}()

	var order []string
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-sub:
			if !ok {
				t.Fatalf("event channel closed before turn_done")
			}
			order = append(order, ev.Event)
			if ev.Event == "turn_done" {
				goto asserted
			}
		case <-deadline:
			t.Fatalf("timed out waiting for turn_done; got %v", order)
		}
	}
asserted:
	<-done

	resolvedIdx := -1
	messagesIdx := -1
	for i, e := range order {
		switch e {
		case "permission_resolved":
			resolvedIdx = i
		case "messages":
			if messagesIdx < 0 {
				messagesIdx = i
			}
		}
	}
	if resolvedIdx < 0 {
		t.Fatalf("permission_resolved not broadcast; events: %v", order)
	}
	if messagesIdx < 0 || messagesIdx < resolvedIdx {
		t.Fatalf("permission_resolved must precede the messages snapshot; events: %v", order)
	}
}

// Legacy boolean bodies keep working after the decision field was introduced.
func TestHandleResolvePermissionLegacyApprovedStillResolves(t *testing.T) {
	isolatedConfigHome(t)
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
	rec := resolveBody(t, h, as, `{"request_id":"call-1","approved":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	if tailIsPermissionAsk(as.messages) {
		t.Fatalf("legacy approved=true must resolve the pending ask")
	}
}
