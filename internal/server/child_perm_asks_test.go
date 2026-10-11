package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/tool"
)

// ── fixtures ────────────────────────────────────────────────────────────────

// childAskClient is a scripted LLM for a sub-agent: one tool call, then a plain
// reply. The tool has no permission rule, so Decide returns Ask and the asker's
// callback runs — which is the whole point of these tests.
type childAskClient struct {
	mu        sync.Mutex
	responses []*agent.Message
	idx       int
}

func (c *childAskClient) Chat([]agent.Message, []map[string]interface{}) (*agent.Message, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.idx >= len(c.responses) {
		return &agent.Message{Role: "assistant", Content: "child done"}, nil
	}
	msg := c.responses[c.idx]
	c.idx++
	return msg, nil
}
func (c *childAskClient) GetProvider() string { return "fake" }
func (c *childAskClient) GetModel() string    { return "fake-model" }

// countingTool records how many times it ran, so a test can prove a denied ask
// never executed it and an allowed one did.
type countingTool struct {
	name   string
	result string

	mu    sync.Mutex
	calls int
}

func (t *countingTool) Name() string        { return t.name }
func (t *countingTool) Description() string { return "" }
func (t *countingTool) Definition() map[string]interface{} {
	return map[string]interface{}{"name": t.name}
}
func (t *countingTool) Parallel() bool { return true }
func (t *countingTool) Execute(json.RawMessage) (string, error) {
	t.mu.Lock()
	t.calls++
	t.mu.Unlock()
	return t.result, nil
}
func (t *countingTool) callCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls
}

func childAskToolCall(name string) agent.ToolCall {
	return agent.ToolCall{ID: "child-call-1", Type: "function", Function: struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}{Name: name, Arguments: `{}`}}
}

// childAskRequest is a realistic sub-agent ask: a bash command that tripped an
// interpreter rule and was left below the judge's confidence floor.
func childAskRequest() agent.PermissionRequest {
	return agent.PermissionRequest{
		ToolName:   "bash",
		Args:       json.RawMessage(`{"command":"python3 -c 'print(1)'"}`),
		Command:    "python3 -c 'print(1)'",
		Scope:      agent.PermissionScopeBashPrefix,
		Rule:       "bash.interpreter.python",
		DenyReason: "TypeSafe judge leaned allow but confidence 0.76 is below the 0.85 floor",
		AgentName:  "context",
	}
}

// childAskHarness is a handler with ONE session, its parent agent wired to the
// server sub-agent asker, and the asker itself handed back so a test can play
// either side: `ask` for "a child asked", or attach it to a real child Agent.
type childAskHarness struct {
	t         *testing.T
	h         *Handler
	as        *agentSession
	sessionID string
	asker     func(agent.PermissionRequest) agent.PermissionResponse
}

// newChildAskHarness builds that harness. timeout <= 0 keeps the production
// 10-minute park; a short value is what lets the timeout test finish.
func newChildAskHarness(t *testing.T, timeout time.Duration) *childAskHarness {
	t.Helper()
	h := NewHandler()
	h.SetWorkDir(t.TempDir())
	const sessionID = "sess-child"
	as := &agentSession{
		model:       "fake-model",
		credVersion: auth.CredentialVersion(),
		childAsks:   newChildPermAsks(),
	}
	if timeout > 0 {
		as.childAsks.timeout = timeout
	}
	as.agent = agent.NewAgent(&childAskClient{}, nil, nil, nil)
	asker := h.newServerSubAgentAsker(sessionID, as)
	as.agent.SetSubAgentPermAsker(asker)
	h.agents[sessionID] = as
	h.sessions.Register(sessionID, t.TempDir())
	// The registry entry needs the agent too, or ReleaseAgent (the real eviction
	// path) has nothing to release.
	h.sessions.setAgent(sessionID, as)
	return &childAskHarness{t: t, h: h, as: as, sessionID: sessionID, asker: asker}
}

// ask models a child's permission-ask callback: it blocks until a decision
// arrives, exactly as the real one does inside the child's Step goroutine. The
// returned channel receives whatever the child would have acted on.
func (harn *childAskHarness) ask(req agent.PermissionRequest) chan agent.PermissionResponse {
	harn.t.Helper()
	respCh := make(chan agent.PermissionResponse, 1)
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		respCh <- harn.asker(req)
	}()
	// Sweep before waiting: a test whose body never resolves the ask would
	// otherwise block its own cleanup on a parked child forever.
	harn.t.Cleanup(func() {
		harn.as.childAsks.denyAll()
		<-exited
	})
	waitForRegistryLen(harn.t, harn.as, 1)
	return respCh
}

// parkMany is ask() for the concurrency tests: n children parked at once, each
// with its own channel, without waiting for the registry between them.
func (harn *childAskHarness) parkMany(n int) []chan agent.PermissionResponse {
	harn.t.Helper()
	out := make([]chan agent.PermissionResponse, n)
	exited := make([]chan struct{}, n)
	for i := 0; i < n; i++ {
		out[i] = make(chan agent.PermissionResponse, 1)
		exited[i] = make(chan struct{})
		go func(i int) {
			defer close(exited[i])
			out[i] <- harn.asker(childAskRequest())
		}(i)
	}
	harn.t.Cleanup(func() {
		harn.as.childAsks.denyAll()
		for i := range exited {
			<-exited[i]
		}
	})
	waitForRegistryLen(harn.t, harn.as, n)
	return out
}

// resolve issues POST /api/permissions/resolve for this harness's session.
func (harn *childAskHarness) resolve(requestID, decision string) *httptest.ResponseRecorder {
	harn.t.Helper()
	return resolvePost(harn.t, harn.h, harn.sessionID, requestID, decision)
}

func waitForRegistryLen(t *testing.T, as *agentSession, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if n := len(as.childAsks.list()); n == want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("registry never reached %d entries (has %d)", want, len(as.childAsks.list()))
}

// resolvePost issues POST /api/permissions/resolve.
func resolvePost(t *testing.T, h *Handler, sessionID, requestID, decision string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"request_id":"` + requestID + `","session_id":"` + sessionID + `","decision":"` + decision + `"}`
	req := httptest.NewRequest("POST", "/api/permissions/resolve", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.HandleResolvePermission(rec, req)
	return rec
}

// waitForSSEEvent drains sub until an event of the given kind arrives and returns
// it, failing the test if the deadline passes first. Distinct from the
// Envelope-channel waitForEvent in handler_questions_test.go.
func waitForSSEEvent(t *testing.T, sub chan SSEEvent, name string, timeout time.Duration) SSEEvent {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case ev := <-sub:
			if ev.Event == name {
				return ev
			}
		case <-deadline:
			t.Fatalf("no %q frame within %s", name, timeout)
			return SSEEvent{}
		}
	}
}

// eventPayload decodes an SSE frame's data payload into out.
func eventPayload(t *testing.T, ev SSEEvent, out any) {
	t.Helper()
	raw, err := json.Marshal(ev.Data)
	if err != nil {
		t.Fatalf("marshal event data: %v", err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("decode event data %s: %v", raw, err)
	}
}

// ── 1. the regression: a child ask must be visible while the turn lock is held ──

// TestLivePendingAsksReportsChildAskWhileTurnLockHeld is THE regression. The
// observed bug: a sub-agent hit a permission ask, nothing surfaced it, the child
// aborted mid-work and its run was recorded as done. Root cause: runTurn holds
// as.mu for the whole turn and a synchronous sub-agent dispatch parks INSIDE
// that turn, so livePendingAsks' TryLock always failed for exactly as long as
// the ask existed.
//
// It fails on the pre-fix code (which returned nil whenever TryLock failed) and
// is the test that would have caught agent-run-4.
func TestLivePendingAsksReportsChildAskWhileTurnLockHeld(t *testing.T) {
	harn := newChildAskHarness(t, 0)
	h, as, sessionID := harn.h, harn.as, harn.sessionID

	// Reproduce the real lock state: the turn holds as.mu for its whole life.
	as.mu.Lock()
	defer as.mu.Unlock()
	if as.mu.TryLock() {
		t.Fatal("test setup did not hold as.mu")
	}

	harn.ask(childAskRequest())
	requestID := as.childAsks.list()[0].RequestID

	asks := h.livePendingAsks(sessionID)
	if asks == nil || len(asks.Permissions) != 1 {
		t.Fatalf("pending_asks = %+v, want exactly 1 child permission while as.mu is held", asks)
	}
	got := asks.Permissions[0]
	if got.RequestID != requestID {
		t.Fatalf("request_id = %q, want %q", got.RequestID, requestID)
	}
	if got.AgentName != "context" {
		t.Fatalf("agent_name = %q, want the asking sub-agent's name", got.AgentName)
	}
	if got.Rule != "bash.interpreter.python" || got.Tool != "bash" {
		t.Fatalf("child ask lost its rule/tool: %+v", got)
	}
	// Resolving must not need as.mu either — it never takes it on this path.
	if rec := harn.resolve(requestID, PermDecisionDeny); rec.Code != http.StatusOK {
		t.Fatalf("resolve while as.mu held: status %d (%s)", rec.Code, rec.Body.String())
	}
}

// TestLivePendingAsksNeverBlocksOnTheTurnLock: livePendingAsks runs inside the
// reconcile HTTP handler the browser and watchdog poll. With the registry read
// added it must still return promptly while a turn holds as.mu — a blocking
// acquire here is the "stuck session" class (CLAUDE.md).
func TestLivePendingAsksNeverBlocksOnTheTurnLock(t *testing.T) {
	harn := newChildAskHarness(t, 0)
	harn.as.mu.Lock()
	defer harn.as.mu.Unlock()

	returned := make(chan *PendingAsks, 1)
	go func() { returned <- harn.h.livePendingAsks(harn.sessionID) }()
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("livePendingAsks blocked behind the turn lock")
	}
}

// ── 2. merging both sources ─────────────────────────────────────────────────

func TestLivePendingAsksMergesBothSources(t *testing.T) {
	harn := newChildAskHarness(t, 0)
	// A main-agent sentinel ask in the transcript tail, alongside a parked child.
	harn.as.messages = []agent.Message{
		{Role: "user", Content: "go"},
		{Role: "tool", ToolID: "call-main", Content: permissionAskContent(t, samplePermissionRequest())},
	}
	harn.ask(childAskRequest())
	childID := harn.as.childAsks.list()[0].RequestID

	asks := harn.h.livePendingAsks(harn.sessionID)
	if asks == nil || len(asks.Permissions) != 2 {
		t.Fatalf("pending_asks = %+v, want both the main-agent and the child ask", asks)
	}
	// Main agent first (the transcript scan's established order), child appended.
	if asks.Permissions[0].RequestID != "call-main" {
		t.Fatalf("first entry = %q, want the main-agent ask", asks.Permissions[0].RequestID)
	}
	if asks.Permissions[1].RequestID != childID {
		t.Fatalf("second entry = %q, want the child ask %q", asks.Permissions[1].RequestID, childID)
	}
	if asks.Permissions[1].AgentName != "context" {
		t.Fatal("merged child ask lost its agent_name")
	}
}

func TestLivePendingAsksRegistryOnly(t *testing.T) {
	harn := newChildAskHarness(t, 0)
	harn.ask(childAskRequest())
	asks := harn.h.livePendingAsks(harn.sessionID)
	if asks == nil || len(asks.Permissions) != 1 || len(asks.Questions) != 0 {
		t.Fatalf("pending_asks = %+v, want exactly one registry-sourced permission", asks)
	}
}

// TestLivePendingAsksNilRegistryPreservesNil: a session built directly as a
// literal (the shape most tests use) has a nil registry, which must behave
// exactly like the pre-fix empty case — nil, not an empty non-nil struct that
// would make the client think something is pending.
func TestLivePendingAsksNilRegistryPreservesNil(t *testing.T) {
	h := NewHandler()
	h.SetWorkDir(t.TempDir())
	h.agents["sess-nil"] = &agentSession{agent: agent.NewAgent(&childAskClient{}, nil, nil, nil), model: "fake-model"}
	h.sessions.Register("sess-nil", t.TempDir())

	if asks := h.livePendingAsks("sess-nil"); asks != nil {
		t.Fatalf("pending_asks = %+v, want nil for a session with no asks at all", asks)
	}
	var nilReg *childPermAsks
	if nilReg.add("x", &childPermAsk{}) {
		t.Fatal("nil registry accepted an add")
	}
	if nilReg.list() != nil || nilReg.get("x") != nil || nilReg.take("x") != nil {
		t.Fatal("nil registry reads should be empty, not panic or fabricate entries")
	}
	if nilReg.wasResolvedRecently("x") {
		t.Fatal("nil registry claimed a resolved id")
	}
	nilReg.denyAll() // must not panic
}

// ── 3. resolve delivers the right level ─────────────────────────────────────

func TestResolveChildAskDeliversDecision(t *testing.T) {
	for _, tc := range []struct {
		decision string
		want     agent.PermissionLevel
	}{
		{PermDecisionAllow, agent.PermissionAllow},
		{PermDecisionDeny, agent.PermissionDeny},
	} {
		t.Run(tc.decision, func(t *testing.T) {
			harn := newChildAskHarness(t, 0)
			respCh := harn.ask(childAskRequest())
			requestID := harn.as.childAsks.list()[0].RequestID

			rec := harn.resolve(requestID, tc.decision)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d (%s), want 200", rec.Code, rec.Body.String())
			}
			select {
			case resp := <-respCh:
				if resp.Level != tc.want {
					t.Fatalf("child received %v, want %v", resp.Level, tc.want)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("the parked child was never resumed")
			}
			waitForRegistryLen(t, harn.as, 0)
		})
	}
}

// TestResolveChildAskDenyDoesNotAbortTheChild: a denial is a normal tool result
// the child can route around, not a run abort (design "Lifecycles" #7), and it
// must not poison the session.
func TestResolveChildAskDenyDoesNotAbortTheChild(t *testing.T) {
	harn := newChildAskHarness(t, 0)
	respCh := harn.ask(childAskRequest())
	requestID := harn.as.childAsks.list()[0].RequestID

	if rec := harn.resolve(requestID, PermDecisionDeny); rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if resp := <-respCh; resp.Level != agent.PermissionDeny {
		t.Fatalf("child received %v, want Deny", resp.Level)
	}
	waitForRegistryLen(t, harn.as, 0)
	if harn.h.livePendingAsks(harn.sessionID) != nil {
		t.Fatal("a resolved child ask still reported as pending")
	}
}

// TestResolveChildAskAlwaysAllowDeliversAPlainAllow: the delivered response must
// NOT carry PersistRule/PersistTool. applyPermissionResponse would then install a
// blanket SetUserConfirmedRule(toolName) allow on the shared manager, and for an
// out-of-scope-path ask that is exactly the blanket grant the guard forbids —
// persistAlwaysAllow has already done the right, narrower thing.
func TestResolveChildAskAlwaysAllowDeliversAPlainAllow(t *testing.T) {
	harn := newChildAskHarness(t, 0)
	respCh := harn.ask(agent.PermissionRequest{
		ToolName:       "write",
		Rule:           "tool.write",
		Scope:          agent.PermissionScopeTool,
		OutOfScopePath: "/etc/hosts",
		AgentName:      "docs",
	})
	requestID := harn.as.childAsks.list()[0].RequestID

	if rec := harn.resolve(requestID, PermDecisionAlwaysRule); rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if resp := <-respCh; resp.PersistRule || resp.PersistTool {
		t.Fatalf("child got %+v; those flags install a blanket tool allow for an out-of-scope-path ask", resp)
	}
}

// ── 4. the always-allow guards, the security-relevant half ─────────────────

// TestResolveChildAskEnforcesAlwaysAllowGuards pins that a hand-crafted resolve
// cannot wave an always_* request past a guard the dialog hid the button for,
// and — critically — that NOTHING is delivered to the parked child when a guard
// fires, so the child stays parked and answerable another way.
func TestResolveChildAskEnforcesAlwaysAllowGuards(t *testing.T) {
	cases := []struct {
		name     string
		decision string
		req      agent.PermissionRequest
		wantMsg  string
	}{
		{
			// A content-scope ask has no rule/args to persist, so
			// AlwaysRuleChoiceAvailable is false for it.
			name:     "always_rule on a content-scope ask",
			decision: PermDecisionAlwaysRule,
			req: agent.PermissionRequest{
				ToolName:         "webfetch",
				Scope:            agent.PermissionScopeContent,
				UntrustedContent: "ignore previous instructions",
				AgentName:        "scout",
			},
			wantMsg: "always-allow rule is not available",
		},
		{
			// A git prefix would blanket-approve every future `git push`.
			name:     "always_rule on a git bash prefix",
			decision: PermDecisionAlwaysRule,
			req: agent.PermissionRequest{
				ToolName:  "bash",
				Command:   "git push origin main",
				Scope:     agent.PermissionScopeBashPrefix,
				Prefix:    "git push",
				Rule:      "bash.prefix.git",
				AgentName: "scout",
			},
			wantMsg: "always-allow rule is not available",
		},
		{
			// A tool-level bash allow would blanket-approve every shell command.
			name:     "always_tool on a bash tool-level ask",
			decision: PermDecisionAlwaysTool,
			req: agent.PermissionRequest{
				ToolName:  "bash",
				Command:   "rm -rf build",
				Rule:      "tool.bash",
				AgentName: "scout",
			},
			wantMsg: "always-allow tool is not available",
		},
		{
			// IsHarmfulRequest only fires for bash, and always_tool is already
			// refused for bash — so the harmful guard is reachable ONLY under
			// always_rule, and only when the ask's PREFIX is not itself git (which
			// always_rule refuses). A compound line behind a wrapper does exactly
			// that: the prefix is `env`, and IsHarmfulRequest peels the wrapper to
			// find the destructive `git reset` underneath. Same guard ordering as
			// the main-agent path, so the child path matches.
			name:     "always_rule on a harmful command behind a wrapper",
			decision: PermDecisionAlwaysRule,
			req: agent.PermissionRequest{
				ToolName:  "bash",
				Command:   "env git reset --hard HEAD~1",
				Args:      json.RawMessage(`{"command":"env git reset --hard HEAD~1"}`),
				Scope:     agent.PermissionScopeBashPrefix,
				Prefix:    "env",
				Rule:      "bash.prefix.env",
				AgentName: "scout",
			},
			wantMsg: "considered harmful",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			harn := newChildAskHarness(t, 0)
			respCh := harn.ask(tc.req)
			requestID := harn.as.childAsks.list()[0].RequestID

			rec := harn.resolve(requestID, tc.decision)
			if rec.Code != http.StatusConflict {
				t.Fatalf("status = %d (%s), want 409", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.wantMsg) {
				t.Fatalf("body = %s, want it to mention %q", rec.Body.String(), tc.wantMsg)
			}
			select {
			case resp := <-respCh:
				t.Fatalf("guard failed but the child was still resumed with %v", resp.Level)
			case <-time.After(150 * time.Millisecond):
			}
			waitForRegistryLen(t, harn.as, 1)
		})
	}
}

// TestResolveChildAskAlwaysAllowPersistsRule: an accepted always_* must reach the
// SHARED PermissionManager, which is what makes it apply to the session's
// sub-agents too.
func TestResolveChildAskAlwaysAllowPersistsRule(t *testing.T) {
	harn := newChildAskHarness(t, 0)
	// A plain tool-level ask is the case persistAlwaysAllow routes to a
	// user-confirmed tool rule, and the only one pm.Check reflects directly — a
	// webfetch-domain grant lives in the session domain cache, which Decide
	// consults rather than Check.
	respCh := harn.ask(agent.PermissionRequest{
		ToolName:  "delete",
		Args:      json.RawMessage(`{"path":"docs/old.md"}`),
		Rule:      "tool.delete",
		Scope:     agent.PermissionScopeTool,
		AgentName: "docs",
	})
	requestID := harn.as.childAsks.list()[0].RequestID

	if rec := harn.resolve(requestID, PermDecisionAlwaysRule); rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if resp := <-respCh; resp.Level != agent.PermissionAllow {
		t.Fatalf("child received %v, want Allow", resp.Level)
	}
	pm := harn.as.agent.Permissions()
	if pm == nil {
		t.Fatal("session agent has no PermissionManager")
	}
	if got := pm.Check("delete"); got != agent.PermissionAllow {
		t.Fatalf("delete after always_rule = %v, want Allow (sub-agents share this manager)", got)
	}
}

// ── 5/6. fall-through and stale ids ────────────────────────────────────────

// TestResolveChildAskFallsThroughToMainAgentPath: a registry miss must leave the
// main-agent path in charge, so the existing sentinel flow is untouched.
func TestResolveChildAskFallsThroughToMainAgentPath(t *testing.T) {
	h := NewHandler()
	h.SetWorkDir(t.TempDir())
	as := &agentSession{
		agent:       agent.NewAgent(&childAskClient{}, nil, nil, nil),
		model:       "fake-model",
		credVersion: auth.CredentialVersion(),
		childAsks:   newChildPermAsks(),
		messages: []agent.Message{
			{Role: "user", Content: "go"},
			{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: "call-main"}}},
			{Role: "tool", ToolID: "call-main", Content: permissionAskContent(t, samplePermissionRequest())},
		},
	}
	h.agents["sess-main"] = as
	h.sessions.Register("sess-main", t.TempDir())

	if rec := resolvePost(t, h, "sess-main", "call-main", PermDecisionDeny); rec.Code != http.StatusAccepted {
		t.Fatalf("main-agent resolve status = %d (%s), want 202 (unchanged path)", rec.Code, rec.Body.String())
	}
	// Join the background continuation so no goroutine outlives the test. The
	// shared 3s helper is too tight here: under -race, with the whole package's
	// tests resident, the continuation's re-Step can take longer than that.
	waitForContinuationSlow(t, as, 20*time.Second)
}

// waitForContinuationSlow blocks until as.mu is free again, i.e. the background
// continuation dispatched by a resolve has finished.
func waitForContinuationSlow(t *testing.T, as *agentSession, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if as.mu.TryLock() {
			as.mu.Unlock()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("ask continuation did not finish within %s", timeout)
}

// TestResolveChildAskStaleID404s: an id nothing holds must get today's error
// shape — no new error vocabulary for the client to learn.
func TestResolveChildAskStaleID404s(t *testing.T) {
	harn := newChildAskHarness(t, 0)
	rec := harn.resolve("childperm-deadbeefdeadbeefdead", PermDecisionAllow)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d (%s), want 404", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "no pending permission found for request_id") {
		t.Fatalf("body = %s, want the existing 404 message", rec.Body.String())
	}
}

// TestResolveChildAskAfterResolutionReturns404WithoutBlocking: the click that
// races a resolution must NOT reach findPendingSession, which takes a BLOCKING
// as.mu — the turn holding exactly that lock is what the child is parked inside.
// Falling through would pin the HTTP connection for the rest of the park.
func TestResolveChildAskAfterResolutionReturns404WithoutBlocking(t *testing.T) {
	harn := newChildAskHarness(t, 0) // 10-minute park: the test must not wait it out
	harn.as.mu.Lock()                // the turn's lock, held for the whole park
	defer harn.as.mu.Unlock()

	harn.ask(childAskRequest())
	requestID := harn.as.childAsks.list()[0].RequestID
	// Resolve once (the answer), then resolve again as a stale duplicate click.
	if rec := harn.resolve(requestID, PermDecisionAllow); rec.Code != http.StatusOK {
		t.Fatalf("first resolve status = %d (%s)", rec.Code, rec.Body.String())
	}

	returned := make(chan *httptest.ResponseRecorder, 1)
	go func() { returned <- resolvePost(t, harn.h, harn.sessionID, requestID, PermDecisionAllow) }()
	select {
	case rec := <-returned:
		if rec.Code != http.StatusNotFound {
			t.Fatalf("duplicate resolve status = %d (%s), want 404", rec.Code, rec.Body.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a duplicate resolve blocked behind the turn lock")
	}
}

// TestResolveChildAskWithoutSessionIDScanFindsIt: the client may omit
// session_id; the scan must find the ask and the broadcast must still be
// attributed to the owning session.
func TestResolveChildAskWithoutSessionIDScanFindsIt(t *testing.T) {
	harn := newChildAskHarness(t, 0)
	respCh := harn.ask(childAskRequest())
	requestID := harn.as.childAsks.list()[0].RequestID

	sub := harn.h.subscribeHeadless()
	defer harn.h.unsubscribeHeadless(sub)

	body := `{"request_id":"` + requestID + `","decision":"deny"}`
	req := httptest.NewRequest("POST", "/api/permissions/resolve", strings.NewReader(body))
	rec := httptest.NewRecorder()
	harn.h.HandleResolvePermission(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	if resp := <-respCh; resp.Level != agent.PermissionDeny {
		t.Fatalf("child received %v, want Deny", resp.Level)
	}

	ev := waitForSSEEvent(t, sub, "permission_resolved", 3*time.Second)
	if ev.SessionID != harn.sessionID {
		t.Fatalf("permission_resolved session = %q, want %q", ev.SessionID, harn.sessionID)
	}
	var payload struct {
		RequestID string `json:"request_id"`
	}
	eventPayload(t, ev, &payload)
	if payload.RequestID != requestID {
		t.Fatalf("permission_resolved carried request_id %q, want %q", payload.RequestID, requestID)
	}
}

// ── 7/8. the asker's lifecycles ─────────────────────────────────────────────

// TestAskerParentCancelAutoDenies: Stop (Escape / POST /cancel) closes the
// parent's stop channel, which the asker watches, so a parked child unblocks with
// a plain deny instead of hanging.
func TestAskerParentCancelAutoDenies(t *testing.T) {
	harn := newChildAskHarness(t, 0)
	respCh := harn.ask(childAskRequest())
	harn.as.agent.Cancel()

	select {
	case resp := <-respCh:
		if resp.Level != agent.PermissionDeny {
			t.Fatalf("child received %v, want Deny on parent cancel", resp.Level)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("parent cancel did not unblock the parked child")
	}
	waitForRegistryLen(t, harn.as, 0)
}

// TestAskerParkTimeoutAutoDenies: this — not the cancel path — is what actually
// bounds the exposure, because parent cancellation does not reach a sub-agent
// mid-Step. Without it a forgotten dialog holds a turn open forever.
func TestAskerParkTimeoutAutoDenies(t *testing.T) {
	harn := newChildAskHarness(t, 40*time.Millisecond)
	respCh := harn.ask(childAskRequest())

	select {
	case resp := <-respCh:
		if resp.Level != agent.PermissionDeny {
			t.Fatalf("child received %v, want Deny on park timeout", resp.Level)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the park timeout did not fire")
	}
	waitForRegistryLen(t, harn.as, 0)
}

// TestAskerAutoDenyBroadcastsPermissionResolved: without this the browser's
// dialog sits on screen inviting a click that can only 404. The frame carries the
// exact request id, so the reducer cannot dismiss a DIFFERENT dialog.
func TestAskerAutoDenyBroadcastsPermissionResolved(t *testing.T) {
	harn := newChildAskHarness(t, 40*time.Millisecond)
	sub := harn.h.subscribeHeadless()
	defer harn.h.unsubscribeHeadless(sub)

	respCh := harn.ask(childAskRequest())

	ev := waitForSSEEvent(t, sub, "permission", 3*time.Second)
	var asked PermissionEvent
	eventPayload(t, ev, &asked)
	if asked.RequestID == "" {
		t.Fatal("permission frame carried no request_id")
	}

	<-respCh // the auto-deny has fired by the time the child returns

	ev = waitForSSEEvent(t, sub, "permission_resolved", 3*time.Second)
	if ev.SessionID != harn.sessionID {
		t.Fatalf("permission_resolved session = %q, want %q", ev.SessionID, harn.sessionID)
	}
	var payload struct {
		RequestID string `json:"request_id"`
	}
	eventPayload(t, ev, &payload)
	if payload.RequestID != asked.RequestID {
		t.Fatalf("permission_resolved carried request_id %q, want %q — a mismatch can dismiss the wrong dialog", payload.RequestID, asked.RequestID)
	}
}

// TestDenyAllAutoDeniesAndEmptiesRegistry: idle eviction, a rebuild and server
// shutdown all end the session; no child goroutine may outlive it.
func TestDenyAllAutoDeniesAndEmptiesRegistry(t *testing.T) {
	for _, name := range []string{"denyAll", "eviction", "rebuild"} {
		t.Run(name, func(t *testing.T) {
			harn := newChildAskHarness(t, 0)
			const n = 3
			resps := harn.parkMany(n)

			switch name {
			case "denyAll":
				harn.as.childAsks.denyAll()
			case "eviction":
				// The real eviction path: the SessionManager onEvict hook denies
				// the asks and then shuts the agent down.
				if !harn.h.sessions.ReleaseAgent(harn.sessionID) {
					t.Fatal("ReleaseAgent did not release; the eviction path was not exercised")
				}
			case "rebuild":
				// replaceAgentSession denies the outgoing session's asks before
				// shutting its agent down (no turn active).
				harn.h.replaceAgentSession(harn.sessionID, &agentSession{
					model:       "fake-model",
					credVersion: auth.CredentialVersion(),
					childAsks:   newChildPermAsks(),
				})
			}

			for i := 0; i < n; i++ {
				select {
				case resp := <-resps[i]:
					if resp.Level != agent.PermissionDeny {
						t.Fatalf("child %d received %v, want Deny", i, resp.Level)
					}
				case <-time.After(3 * time.Second):
					t.Fatalf("child %d was still parked after %s", i, name)
				}
			}
			waitForRegistryLen(t, harn.as, 0)
		})
	}
}

// TestDenyAllDeliversOutsideTheLock: a child resumed by denyAll can call back
// into the registry, so delivery must not hold the registry mutex.
func TestDenyAllDeliversOutsideTheLock(t *testing.T) {
	reg := newChildPermAsks()
	if !reg.add("a", &childPermAsk{respCh: make(chan agent.PermissionResponse, 1)}) {
		t.Fatal("add failed")
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		reg.denyAll()
		// Re-entering the registry from a resumed child must not deadlock.
		reg.list()
		reg.add("b", &childPermAsk{respCh: make(chan agent.PermissionResponse, 1)})
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("denyAll deadlocked against a re-entrant registry call")
	}
}

// TestAskerNilRegistryDeniesRatherThanParks: a session with no registry must not
// leave a child blocked on a channel nobody will ever answer.
func TestAskerNilRegistryDeniesRatherThanParks(t *testing.T) {
	h := NewHandler()
	h.SetWorkDir(t.TempDir())
	as := &agentSession{agent: agent.NewAgent(&childAskClient{}, nil, nil, nil), model: "fake-model"}
	got := h.newServerSubAgentAsker("sess-x", as)(childAskRequest())
	if got.Level != agent.PermissionDeny {
		t.Fatalf("level = %v, want Deny when there is no registry to park in", got.Level)
	}
}

// TestAskerDuplicateIDDenies: two children must never share one channel.
func TestAskerDuplicateIDDenies(t *testing.T) {
	reg := newChildPermAsks()
	if !reg.add("dup", &childPermAsk{respCh: make(chan agent.PermissionResponse, 1)}) {
		t.Fatal("first add failed")
	}
	if reg.add("dup", &childPermAsk{respCh: make(chan agent.PermissionResponse, 1)}) {
		t.Fatal("duplicate id accepted — two children would share one response channel")
	}
}

// TestChildPermRequestIDsAreUnguessable: the resolve endpoint takes ONE
// request_id for both kinds of ask, so a sub-agent ask's id must not be
// predictable or collide.
func TestChildPermRequestIDsAreUnguessable(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 200; i++ {
		id := newChildPermRequestID()
		if !strings.HasPrefix(id, "childperm-") {
			t.Fatalf("id %q has no prefix, so it is indistinguishable from a tool-call id", id)
		}
		if seen[id] {
			t.Fatalf("duplicate request id %q after %d draws", id, i)
		}
		seen[id] = true
	}
}

// ── 10. wire stability ─────────────────────────────────────────────────────

// TestPermissionEventAgentNameOmittedForMainAgent: every pre-existing
// permission frame must stay byte-identical — the same omitempty precedent as
// the Untrusted* fields.
func TestPermissionEventAgentNameOmittedForMainAgent(t *testing.T) {
	ev := newPermissionEvent("call-1", samplePermissionRequest())
	if ev.AgentName != "" {
		t.Fatalf("main-agent event carries agent_name %q, want empty", ev.AgentName)
	}
	raw, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "agent_name") {
		t.Fatalf("agent_name leaked into a main-agent frame: %s", raw)
	}
	ev = newPermissionEvent("childperm-1", childAskRequest())
	if ev.AgentName != "context" {
		t.Fatalf("child event agent_name = %q, want %q", ev.AgentName, "context")
	}
}

// ── 11. end to end: a real child agent parks, is reported, and finishes ─────

// TestChildAgentAskIsVisibleAndResumable is the agent-run-4 shape end to end: a
// REAL agent.Step issues a tool call whose permission needs a decision, the
// server asker parks it, the pending-asks payload reports it WHILE the parent's
// turn lock is held, and resolving lets the child's tool call actually run.
//
// The only stand-in for a real dispatch is how the child is wired to the asker
// (a real dispatch goes through subagent.go's attribution wrapper, covered by
// internal/agent's TestSubagentPermissionAskCarriesAgentName); everything from
// Decide onward is production code.
func TestChildAgentAskIsVisibleAndResumable(t *testing.T) {
	h := NewHandler()
	h.SetWorkDir(t.TempDir())
	const sessionID = "sess-e2e"

	gate := &countingTool{name: "gated_tool", result: "gate opened"}
	child := agent.NewAgent(&childAskClient{responses: []*agent.Message{
		{Role: "assistant", ToolCalls: []agent.ToolCall{childAskToolCall("gated_tool")}},
		{Role: "assistant", Content: "child finished its work"},
	}}, []tool.Tool{gate}, nil, nil)

	as := &agentSession{
		agent:       agent.NewAgent(&childAskClient{}, nil, nil, nil),
		model:       "fake-model",
		credVersion: auth.CredentialVersion(),
		childAsks:   newChildPermAsks(),
		messages:    []agent.Message{{Role: "user", Content: "write the doc"}},
	}
	child.OnPermissionAsk = h.newServerSubAgentAsker(sessionID, as)
	h.agents[sessionID] = as
	h.sessions.Register(sessionID, t.TempDir())

	// The parent's turn holds as.mu for the child's whole life — the exact
	// condition that made the ask invisible.
	as.mu.Lock()
	defer as.mu.Unlock()

	stepDone := make(chan error, 1)
	go func() {
		_, err := child.Step([]agent.Message{{Role: "user", Content: "do the work"}})
		stepDone <- err
	}()

	waitForRegistryLen(t, as, 1)
	asks := h.livePendingAsks(sessionID)
	if asks == nil || len(asks.Permissions) != 1 {
		t.Fatalf("pending_asks = %+v, want the child's ask surfaced while the turn lock is held", asks)
	}
	if got := gate.callCount(); got != 0 {
		t.Fatalf("gated_tool ran %d times BEFORE the decision, want 0", got)
	}

	rec := resolvePost(t, h, sessionID, asks.Permissions[0].RequestID, PermDecisionAllow)
	if rec.Code != http.StatusOK {
		t.Fatalf("resolve status = %d (%s)", rec.Code, rec.Body.String())
	}
	select {
	case err := <-stepDone:
		if err != nil {
			t.Fatalf("child Step returned %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the child never finished after its ask was answered")
	}
	if got := gate.callCount(); got != 1 {
		t.Fatalf("gated_tool ran %d times after an ALLOW, want 1 — the child did not resume its tool call", got)
	}
	waitForRegistryLen(t, as, 0)
}

// TestChildAgentDenyDoesNotRunTheTool is the same path with a deny: the child's
// tool must NOT run, and the child must still finish its turn (a denial is a
// result to route around, not an abort).
func TestChildAgentDenyDoesNotRunTheTool(t *testing.T) {
	h := NewHandler()
	h.SetWorkDir(t.TempDir())
	const sessionID = "sess-e2e-deny"

	gate := &countingTool{name: "gated_tool", result: "must not run"}
	child := agent.NewAgent(&childAskClient{responses: []*agent.Message{
		{Role: "assistant", ToolCalls: []agent.ToolCall{childAskToolCall("gated_tool")}},
		{Role: "assistant", Content: "moved on"},
	}}, []tool.Tool{gate}, nil, nil)

	as := &agentSession{
		agent:       agent.NewAgent(&childAskClient{}, nil, nil, nil),
		model:       "fake-model",
		credVersion: auth.CredentialVersion(),
		childAsks:   newChildPermAsks(),
	}
	child.OnPermissionAsk = h.newServerSubAgentAsker(sessionID, as)
	h.agents[sessionID] = as
	h.sessions.Register(sessionID, t.TempDir())

	stepDone := make(chan error, 1)
	go func() {
		_, err := child.Step([]agent.Message{{Role: "user", Content: "do the work"}})
		stepDone <- err
	}()
	waitForRegistryLen(t, as, 1)
	requestID := as.childAsks.list()[0].RequestID

	if rec := resolvePost(t, h, sessionID, requestID, PermDecisionDeny); rec.Code != http.StatusOK {
		t.Fatalf("resolve status = %d (%s)", rec.Code, rec.Body.String())
	}
	select {
	case <-stepDone:
	case <-time.After(15 * time.Second):
		t.Fatal("the child never finished after a DENY")
	}
	if got := gate.callCount(); got != 0 {
		t.Fatalf("gated_tool ran %d times after a DENY, want 0", got)
	}
	waitForRegistryLen(t, as, 0)
}

// TestPendingPermissionAsksCountsChildAsksWhileTurnLockHeld: PendingPermissionAsks
// feeds the desktop badge watcher and the quit dialog, and it shares the
// TryLock-skip shape with livePendingAsks — so it had the same blind spot. A
// session parked on a sub-agent ask is mid-turn BY CONSTRUCTION (the parent's
// turn holds as.mu for the whole dispatch), so skipping a busy session would
// under-count exactly the sessions the badge exists to flag.
func TestPendingPermissionAsksCountsChildAsksWhileTurnLockHeld(t *testing.T) {
	h := NewHandler()
	h.SetWorkDir(t.TempDir())

	parked := &agentSession{
		agent:       agent.NewAgent(&childAskClient{}, nil, nil, nil),
		model:       "fake-model",
		credVersion: auth.CredentialVersion(),
		childAsks:   newChildPermAsks(),
	}
	idle := &agentSession{
		agent:       agent.NewAgent(&childAskClient{}, nil, nil, nil),
		model:       "fake-model",
		credVersion: auth.CredentialVersion(),
		childAsks:   newChildPermAsks(),
	}
	h.agents["sess-parked"] = parked
	h.agents["sess-idle"] = idle

	parked.mu.Lock() // the parent's turn
	defer parked.mu.Unlock()
	parkedID := newChildPermRequestID()
	if !parked.childAsks.add(parkedID, &childPermAsk{
		req:    childAskRequest(),
		event:  newPermissionEvent(parkedID, childAskRequest()),
		respCh: make(chan agent.PermissionResponse, 1),
	}) {
		t.Fatal("add failed")
	}
	t.Cleanup(func() { parked.childAsks.denyAll() })

	if got := h.PendingPermissionAsks(); got != 1 {
		t.Fatalf("PendingPermissionAsks = %d, want 1 — a parked child ask must count", got)
	}
}

// TestPendingPermissionAsksStillCountsMainAgentSentinelAsks pins that the merge
// did not replace the transcript source.
func TestPendingPermissionAsksStillCountsMainAgentSentinelAsks(t *testing.T) {
	h := NewHandler()
	h.SetWorkDir(t.TempDir())
	as := &agentSession{
		agent:       agent.NewAgent(&childAskClient{}, nil, nil, nil),
		model:       "fake-model",
		credVersion: auth.CredentialVersion(),
		childAsks:   newChildPermAsks(),
		messages: []agent.Message{
			{Role: "user", Content: "go"},
			{Role: "tool", ToolID: "call-1", Content: permissionAskContent(t, samplePermissionRequest())},
		},
	}
	h.agents["sess-main"] = as
	if got := h.PendingPermissionAsks(); got != 1 {
		t.Fatalf("PendingPermissionAsks = %d, want 1 for a main-agent sentinel ask", got)
	}
}

// ── 12. a parked turn must not look stalled ─────────────────────────────────

// TestTurnHeartbeatKeepsPulsingWhileChildAskParks: a turn parked on a child ask
// keeps publishing turn_heartbeat, so the web's 30s stall watchdog does not paint
// a false "stalled" badge. Trading a silent failure for a false alarm would be
// worse than the bug being fixed.
func TestTurnHeartbeatKeepsPulsingWhileChildAskParks(t *testing.T) {
	harn := newChildAskHarness(t, 0)
	h := harn.h
	h.turnHeartbeatInterval = 20 * time.Millisecond

	// turn_heartbeat rides the unified BUS (publishBusEvent), not the headless
	// SSE mirror, so subscribe there — the client's /api/events stream is what
	// the stall watchdog actually reads.
	sub := h.bus.Subscribe(nil)

	h.sessions.setTurnActive(harn.sessionID, true)
	stop := h.startTurnHeartbeat(harn.sessionID)
	defer func() {
		stop()
		h.sessions.setTurnActive(harn.sessionID, false)
	}()
	defer h.bus.Unsubscribe(sub)

	// Park a child ask for several heartbeat intervals with as.mu held — the
	// real shape of a turn blocked on a sub-agent.
	harn.as.mu.Lock()
	respCh := harn.ask(childAskRequest())

	beats := 0
	deadline := time.After(3 * time.Second)
	for beats < 3 {
		select {
		case ev := <-sub:
			if ev.Event == "turn_heartbeat" {
				beats++
			}
		case <-deadline:
			t.Fatalf("only %d turn_heartbeat frames while parked on a child ask; the 30s stall watchdog would fire", beats)
		}
	}
	if !h.sessions.IsTurnActive(harn.sessionID) {
		t.Fatal("turn stopped being active while parked on a child ask")
	}

	harn.as.childAsks.denyAll()
	if resp := <-respCh; resp.Level != agent.PermissionDeny {
		t.Fatalf("child received %v, want Deny from denyAll", resp.Level)
	}
	harn.as.mu.Unlock()
}

// Catches: a rebuild that strands a parked ask. An MCP/plugin/model toggle
// replaces the session while its turn keeps running on the old agent; the
// child's ask sits in the old registry, so the replacement must inherit it or
// resolve 404s and the turn hangs until the park times out.
func TestReplaceMidTurnKeepsChildAskResolvable(t *testing.T) {
	harn := newChildAskHarness(t, 0)
	harn.h.sessions.setTurnActive(harn.sessionID, true)
	defer harn.h.sessions.setTurnActive(harn.sessionID, false)

	respCh := harn.ask(childAskRequest())
	pending := harn.as.childAsks.list()
	if len(pending) != 1 {
		t.Fatalf("parked asks = %d, want 1", len(pending))
	}

	harn.h.replaceAgentSession(harn.sessionID, &agentSession{
		model:       "fake-model",
		credVersion: auth.CredentialVersion(),
		childAsks:   newChildPermAsks(),
	})

	if w := harn.resolve(pending[0].RequestID, "allow"); w.Code != http.StatusOK {
		t.Fatalf("resolve after a mid-turn replace = %d, want 200: %s", w.Code, w.Body.String())
	}
	select {
	case resp := <-respCh:
		if resp.Level != agent.PermissionAllow {
			t.Fatalf("child received %v, want Allow", resp.Level)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("child was still parked after the ask was resolved")
	}
}
