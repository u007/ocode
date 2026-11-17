package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/agent"
)

// TestElevationAskBlocksAutoContinueJudge proves the real gate: a pending
// NeedsPassword (elevation) ask suppresses the auto-continue resume, so the
// triage judge is never consulted on a secret ask. The baseline first shows the
// same stub judge DOES resume when no ask is pending, so the result is not a
// vacuous "no judge configured" outcome.
//
// This is the server-side twin of the TUI's queueDrainBlocked gate: both sit
// before AutoContinueJudgeSync, so no auto path ever answers a secret ask.
// See autoContinueShouldResume (agent_session.go) and sessionHasOutstandingAsk.
func TestElevationAskBlocksAutoContinueJudge(t *testing.T) {
	url, bodies := newSystemoneStub(t, continueVerdictJSON)
	h, id, _ := autoContinueTestServer(t, url)
	h.mu.Lock()
	h.cfg.Ocode.AutoContinueEnabled = true
	h.mu.Unlock()
	as := h.lookupAgentSession(id)
	as.agent.SetMaxSteps(0)

	// Baseline: no pending ask -> the stub judge resumes.
	fire, _ := h.autoContinueShouldResume(id, as, 0, nil)
	if fire.Action != "continue" {
		t.Fatalf("baseline resume Action = %q, want continue", fire.Action)
	}
	judgeCalls := len(bodies())
	if judgeCalls == 0 {
		t.Fatal("baseline: the stub judge was never consulted")
	}

	// Park an elevation ask in the transcript tail, exactly as the agent does
	// when it pauses on the human-only elevation prompt.
	ask := samplePermissionRequest()
	ask.NeedsPassword = true
	ask.ElevationCommand = "sudo whoami"
	as.messages = append(as.messages, agent.Message{
		Role:    "tool",
		ToolID:  "call-elev",
		Content: permissionAskContent(t, ask),
	})
	if !h.sessionHasOutstandingAsk(as) {
		t.Fatal("sessionHasOutstandingAsk = false with a pending elevation ask")
	}
	fire, _ = h.autoContinueShouldResume(id, as, 0, nil)
	if fire.Action != "" || fire.Prompt != "" {
		t.Fatalf("auto-continue fired past a pending elevation ask: %+v", fire)
	}
	if got := len(bodies()); got != judgeCalls {
		t.Fatalf("judge consulted %d time(s) with a pending elevation ask, want %d (it must be suppressed)", got, judgeCalls)
	}
}

// TestPulseResolversHaveNoPasswordChannel pins that neither Pulse resolver
// tool exposes a password/secret field. The assistant therefore has no channel
// to supply the secret an elevation ask needs, regardless of what it tries.
func TestPulseResolversHaveNoPasswordChannel(t *testing.T) {
	h := pulseTestHandler(t)
	for _, name := range []string{"permission_resolve", "question_answer"} {
		pt := pulseToolByName(t, h, name)
		def := pt.Definition()
		params, ok := def["parameters"].(map[string]interface{})
		if !ok {
			t.Fatalf("%s: parameters = %v", name, def["parameters"])
		}
		props, ok := params["properties"].(map[string]interface{})
		if !ok {
			t.Fatalf("%s: properties = %v", name, params["properties"])
		}
		for _, banned := range []string{"password", "secret", "elevation_password"} {
			if _, exists := props[banned]; exists {
				t.Errorf("%s exposes a %q property; Pulse must not be able to supply an elevation secret", name, banned)
			}
		}
	}
}

// TestPulsePermissionResolveRefusesElevationAsk drives the real Pulse tool
// against a pending NeedsPassword (elevation) ask and asserts an EXPLICIT
// refusal: the Pulse-specific chokepoint rejects the ask before it reaches the
// shared resolve handler, so no password store is primed and no privileged
// command runs. The guard is Pulse-only so web/TUI elevation approvals still
// work (see TestPulsePermissionResolveStillResolvesOrdinaryAsk and the shared
// HandleResolvePermission tests).
func TestPulsePermissionResolveRefusesElevationAsk(t *testing.T) {
	ask := samplePermissionRequest()
	ask.NeedsPassword = true
	ask.ElevationCommand = "sudo whoami"

	h := pulseTestHandler(t)
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
	h.agents["sess-pulse"] = as
	h.sessions.Register("sess-pulse", t.TempDir())

	var (
		storeCalled bool
		gotPassword string
	)
	origStore := storeResolveElevationSecret
	origExec := executeApprovedWithTempPathFn
	storeResolveElevationSecret = func(_ *agent.Agent, requestID, password string) {
		storeCalled = true
		gotPassword = password
	}
	executeApprovedWithTempPathFn = func(*agent.Agent, string, json.RawMessage, string, string) (string, error) {
		return "unprivileged-output", nil
	}
	t.Cleanup(func() {
		storeResolveElevationSecret = origStore
		executeApprovedWithTempPathFn = origExec
	})

	out, err := runPulseTool(t, h, "permission_resolve",
		`{"session_id":"sess-pulse","request_id":"call-1","decision":"allow"}`)
	if err == nil {
		t.Fatalf("Pulse permission_resolve accepted an elevation ask; out = %s", out)
	}
	if !strings.Contains(err.Error(), "interactive") || !strings.Contains(err.Error(), "password") {
		t.Fatalf("refusal error = %q, want it to name the interactive password requirement", err.Error())
	}
	if storeCalled {
		t.Fatal("Pulse reached the elevation secret store; the refusal must happen before it")
	}
	if gotPassword != "" {
		t.Fatalf("Pulse supplied a password %q", gotPassword)
	}
}

// TestPulsePermissionResolveStillResolvesOrdinaryAsk pins the no-regression
// side of the explicit refusal: an ordinary (non-elevation) ask still flows
// through HandleResolvePermission and re-executes the approved call. The
// Pulse guard must key on NeedsPassword, not on Pulse acting at all.
func TestPulsePermissionResolveStillResolvesOrdinaryAsk(t *testing.T) {
	ask := samplePermissionRequest() // NeedsPassword = false

	h := pulseTestHandler(t)
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
	h.agents["sess-pulse"] = as
	h.sessions.Register("sess-pulse", t.TempDir())

	var execCalled bool
	origExec := executeApprovedWithTempPathFn
	executeApprovedWithTempPathFn = func(*agent.Agent, string, json.RawMessage, string, string) (string, error) {
		execCalled = true
		return "ordinary-output", nil
	}
	t.Cleanup(func() { executeApprovedWithTempPathFn = origExec })

	if _, err := runPulseTool(t, h, "permission_resolve",
		`{"session_id":"sess-pulse","request_id":"call-1","decision":"allow"}`); err != nil {
		t.Fatalf("pulse permission_resolve refused an ordinary ask: %v", err)
	}
	waitForAskContinuation(t, as)
	if !execCalled {
		t.Fatal("ordinary ask did not re-execute through HandleResolvePermission")
	}
}
