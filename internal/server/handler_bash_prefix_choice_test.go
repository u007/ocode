package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/u007/ocode/internal/agent"
)

// bashAskWithCandidates is a pending "pnpm add example-pkg" bash ask carrying
// the rule granularities the dialog may offer. `pnpm add` is deliberately
// NOT on the built-in subcommand allowlist (unlike `pnpm run`), so the derived
// rules are actually load-bearing.
func bashAskWithCandidates() agent.PermissionRequest {
	return agent.PermissionRequest{
		ToolName:         "bash",
		Command:          "pnpm add example-pkg",
		Args:             json.RawMessage(`{"command":"pnpm add example-pkg"}`),
		Scope:            agent.PermissionScopeBashPrefix,
		Prefix:           "pnpm add example-pkg",
		PrefixCandidates: []string{"pnpm add example-pkg", "pnpm add", "pnpm"},
		Rule:             "bash.prefix.pnpm add example-pkg",
	}
}

func bashAskSession(t *testing.T, ag *agent.Agent, req agent.PermissionRequest) *agentSession {
	t.Helper()
	return &agentSession{
		agent: ag,
		model: "fake-model",
		messages: []agent.Message{
			{Role: "user", Content: "install the dependency"},
			{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: "call-1"}}},
			{Role: "tool", ToolID: "call-1", Content: permissionAskContent(t, req)},
		},
	}
}

func savedBashPrefixes(t *testing.T) map[string]any {
	t.Helper()
	cfg, ok := savedOcodeConfig(t)
	if !ok {
		t.Fatalf("expected ocodeconfig.json to be written")
	}
	perms, _ := cfg["permissions"].(map[string]any)
	bash, _ := perms["bash"].(map[string]any)
	prefixes, _ := bash["prefixes"].(map[string]any)
	return prefixes
}

// The dialog may pick any granularity from the ask's candidates; the chosen one
// is what persists, and a broader choice must actually auto-allow later
// invocations of the same family (a rule that is never looked up again would
// re-ask forever and make the dialog loop).
func TestHandleResolvePermissionPersistsChosenBashPrefix(t *testing.T) {
	isolatedConfigHome(t)
	h := NewHandler()
	ag := agent.NewAgent(questionFakeClient{}, nil, nil, nil)
	as := bashAskSession(t, ag, bashAskWithCandidates())

	rec := resolveBody(t, h, as, `{"request_id":"call-1","decision":"always_rule","prefix":"pnpm add"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (body=%s)", rec.Code, rec.Body.String())
	}

	prefixes := savedBashPrefixes(t)
	if prefixes == nil || prefixes["pnpm add"] != "allow" {
		t.Fatalf("expected permissions.bash.prefixes[pnpm add]=allow, got %v", prefixes)
	}
	if _, ok := prefixes["pnpm"]; ok {
		t.Fatalf("a broader rule than the user chose was persisted: %v", prefixes)
	}

	// A sibling invocation under the chosen prefix is now allowed without
	// asking. This is the lookup the exact-key-only path would miss.
	if d := ag.Permissions().Decide("bash", json.RawMessage(`{"command":"pnpm add some-pkg"}`)); d.Level != agent.PermissionAllow {
		t.Fatalf("pnpm add some-pkg after choosing 'pnpm add' = %v, want allow", d.Level)
	}
	// ...while a different pnpm subcommand still asks.
	if d := ag.Permissions().Decide("bash", json.RawMessage(`{"command":"pnpm publish"}`)); d.Level != agent.PermissionAsk {
		t.Fatalf("pnpm publish after choosing 'pnpm add' = %v, want ask", d.Level)
	}
}

// An older client (or the RC bridge) sends always_rule with no prefix; the
// narrowest derived rule is the safe default.
func TestHandleResolvePermissionDefaultsToNarrowestBashPrefix(t *testing.T) {
	isolatedConfigHome(t)
	h := NewHandler()
	ag := agent.NewAgent(questionFakeClient{}, nil, nil, nil)
	as := bashAskSession(t, ag, bashAskWithCandidates())

	rec := resolveBody(t, h, as, `{"request_id":"call-1","decision":"always_rule"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (body=%s)", rec.Code, rec.Body.String())
	}
	prefixes := savedBashPrefixes(t)
	if prefixes == nil || prefixes["pnpm add example-pkg"] != "allow" {
		t.Fatalf("expected permissions.bash.prefixes[pnpm add example-pkg]=allow, got %v", prefixes)
	}
	if _, ok := prefixes["pnpm"]; ok {
		t.Fatalf("default persisted a broader rule than the ask offered: %v", prefixes)
	}
}

// A hand-crafted resolve must not persist a rule the dialog did not offer.
func TestHandleResolvePermissionRejectsUnsubmittedBashPrefix(t *testing.T) {
	isolatedConfigHome(t)
	h := NewHandler()
	ag := agent.NewAgent(questionFakeClient{}, nil, nil, nil)
	as := bashAskSession(t, ag, bashAskWithCandidates())

	rec := resolveBody(t, h, as, `{"request_id":"call-1","decision":"always_rule","prefix":"pnpm publish"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body=%s)", rec.Code, rec.Body.String())
	}
	if cfg, ok := savedOcodeConfig(t); ok {
		perms, _ := cfg["permissions"].(map[string]any)
		bash, _ := perms["bash"].(map[string]any)
		if prefixes, _ := bash["prefixes"].(map[string]any); prefixes != nil {
			t.Fatalf("rejected prefix must not be persisted: %v", prefixes)
		}
	}
	if !tailIsPermissionAsk(as.messages) {
		t.Fatalf("a rejected resolve must leave the ask pending")
	}
}

// A bash ask with no real command prefix offers no always-allow at all: it used
// to fall through to a tool-level `permissions.tools.bash = allow`, which
// blanket-approves every future shell command.
func TestHandleResolvePermissionRefusesToolLevelBashAllow(t *testing.T) {
	isolatedConfigHome(t)
	h := NewHandler()
	ag := agent.NewAgent(questionFakeClient{}, nil, nil, nil)
	as := bashAskSession(t, ag, agent.PermissionRequest{
		ToolName: "bash",
		Command:  "cat ~/.ssh/id_rsa",
		Args:     json.RawMessage(`{"command":"cat ~/.ssh/id_rsa"}`),
		Scope:    agent.PermissionScopeTool,
		Rule:     "bash.redirection.sensitive_path",
	})

	rec := resolveBody(t, h, as, `{"request_id":"call-1","decision":"always_rule"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body=%s)", rec.Code, rec.Body.String())
	}
	cfg, ok := savedOcodeConfig(t)
	if !ok {
		return // nothing persisted at all, which is the point
	}
	perms, _ := cfg["permissions"].(map[string]any)
	if tools, _ := perms["tools"].(map[string]any); tools != nil && tools["bash"] == "allow" {
		t.Fatalf("tool-level bash allow was persisted: %v", tools)
	}
}

// The SSE frame must carry the candidates, or the dialog has nothing to offer.
func TestPermissionEventCarriesPrefixCandidates(t *testing.T) {
	ev := newPermissionEvent("call-1", bashAskWithCandidates())
	if len(ev.PrefixCandidates) != 3 || ev.PrefixCandidates[0] != "pnpm add example-pkg" {
		t.Fatalf("PrefixCandidates = %q", ev.PrefixCandidates)
	}
	wire, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	var decoded PermissionEvent
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.PrefixCandidates) != 3 {
		t.Fatalf("candidates lost in wire form: %s", wire)
	}

	// Non-bash frames stay byte-identical to before (omitempty).
	plain := newPermissionEvent("call-2", samplePermissionRequest())
	if plain.PrefixCandidates != nil {
		t.Fatalf("unexpected candidates on a bash ask without them: %q", plain.PrefixCandidates)
	}
}
