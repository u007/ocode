package server

import (
	"encoding/json"
	"testing"

	"github.com/u007/ocode/internal/agent"
)

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

// TestPulsePermissionResolveCannotSupplyElevationSecret drives the real Pulse
// tool against a pending elevation ask. A Pulse `allow` carries no password, so
// the store seam receives an empty string and the approved bash call has no
// secret to type — the tool fails closed instead of running privileged. Pulse
// can defeat a secret ask, never grant it.
func TestPulsePermissionResolveCannotSupplyElevationSecret(t *testing.T) {
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
	if err != nil {
		t.Fatalf("pulse permission_resolve: %v", err)
	}
	_ = out
	waitForAskContinuation(t, as)

	if !storeCalled {
		t.Fatal("elevation ask was not routed through the secret store")
	}
	if gotPassword != "" {
		t.Fatalf("Pulse supplied a password %q; it must not be able to satisfy a secret ask", gotPassword)
	}
}
