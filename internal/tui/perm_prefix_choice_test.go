package tui

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/tool"
)

func bashPrefixAskMsg(t *testing.T, toolCallID, command string, prefix string, candidates []string) agent.Message {
	t.Helper()
	req := agent.PermissionRequest{
		ToolName:         "bash",
		Scope:            agent.PermissionScopeBashPrefix,
		Rule:             "bash.prefix." + prefix,
		Prefix:           prefix,
		PrefixCandidates: candidates,
		Command:          command,
		Args:             json.RawMessage(`{"command":"` + command + `"}`),
	}
	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal permission request: %v", err)
	}
	return agent.Message{Role: "tool", ToolID: toolCallID, Content: tool.SentinelPermissionAsk + string(payload)}
}

// The confirmation step lists every granularity and a digit key writes the one
// the user picked — not the ask's default.
func TestPermPrefixChoice_NumberedChoicePersistsThatPrefix(t *testing.T) {
	m := newPermQueueTestModel(t)
	m = deliverAsks(t, m, bashPrefixAskMsg(t, "call_a", "npm run build", "npm run build", []string{"npm run build", "npm run", "npm"}))

	body := renderPermConfirmBody(m.pendingPermission, m.pendingToolName, "a")
	for _, want := range []string{"[1]", "[2]", "[3]", "npm run build", "npm run", "npm"} {
		if !strings.Contains(body, want) {
			t.Fatalf("confirm body missing %q:\n%s", want, body)
		}
	}

	if _, _ = m.permDialogInput("a"); m.permConfirm != "a" {
		t.Fatalf("permConfirm = %q, want the always-rule confirmation step", m.permConfirm)
	}
	if _, closed := m.permDialogInput("2"); !closed {
		t.Fatal("a numbered choice must resolve the ask")
	}
	allowed := m.agent.Permissions().BashAllowedPrefixes()
	if !slices.Contains(allowed, "npm run") {
		t.Fatalf("picked rule not persisted: %v", allowed)
	}
	if slices.Contains(allowed, "npm") {
		t.Fatalf("a broader rule than the picked one was persisted: %v", allowed)
	}
}

// Confirming without picking a digit keeps the safest default: the narrowest
// candidate the ask derived.
func TestPermPrefixChoice_ConfirmUsesNarrowestByDefault(t *testing.T) {
	m := newPermQueueTestModel(t)
	m = deliverAsks(t, m, bashPrefixAskMsg(t, "call_a", "npm run build", "npm run build", []string{"npm run build", "npm run", "npm"}))

	m.permDialogInput("a")
	m.permDialogInput("y")

	allowed := m.agent.Permissions().BashAllowedPrefixes()
	if !slices.Contains(allowed, "npm run build") {
		t.Fatalf("default rule not persisted: %v", allowed)
	}
	if slices.Contains(allowed, "npm") {
		t.Fatalf("default persisted a broader rule: %v", allowed)
	}
}

// A bash ask that carries no prefix candidates (parse failure, synthetic gate)
// must not offer always-rule at all: it used to write a blanket
// permissions.tools.bash = allow.
func TestPermPrefixChoice_NoCandidatesOffersNoAlwaysRule(t *testing.T) {
	req := agent.PermissionRequest{ToolName: "bash", Scope: agent.PermissionScopeTool, Rule: "tool.bash", Command: "echo $((1+1"}
	if permAlwaysRuleAvailable(req) {
		t.Fatal("a bash ask with no command prefix must not offer always-rule")
	}
}
