package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/tool"
)

// bashWholeLineAsk is a pending bash ask whose asked segment is harmless but
// whose whole line carries a harmful sibling. The TUI must refuse the same
// always-style choices the web resolve endpoint refuses for this request.
func bashWholeLineAsk(t *testing.T) agent.Message {
	t.Helper()
	req := agent.PermissionRequest{
		ToolName: "bash",
		Command:  "go test ./...",
		Args:     json.RawMessage(`{"command":"go test ./... && git reset --hard HEAD~1"}`),
		Scope:    agent.PermissionScopeBashPrefix,
		Prefix:   "go test",
		Rule:     "bash.prefix.go test",
	}
	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal permission request: %v", err)
	}
	return agent.Message{Role: "tool", ToolID: "call_wl", Content: tool.SentinelPermissionAsk + string(payload)}
}

func permRefusalText(m model) string {
	if len(m.messages) == 0 {
		return ""
	}
	return m.messages[len(m.messages)-1].text
}

func TestPermAlwaysRuleRefusesHarmfulSiblingLine(t *testing.T) {
	m := deliverAsks(t, newPermQueueTestModel(t), bashWholeLineAsk(t))

	cmd, handled := m.answerPermAsk("a")
	if handled || cmd != nil {
		t.Fatalf("refused always-rule must not be handled (handled=%v cmd=%v)", handled, cmd != nil)
	}
	if !strings.Contains(permRefusalText(m), "considered harmful") {
		t.Fatalf("refusal message = %q, want it to say the operation is harmful", permRefusalText(m))
	}
	if !m.showPermDialog {
		t.Fatal("a refused always-rule must keep the permission dialog open")
	}
}

func TestPermAlwaysToolRefusedForBash(t *testing.T) {
	m := deliverAsks(t, newPermQueueTestModel(t), bashWholeLineAsk(t))

	cmd, handled := m.answerPermAsk("t")
	if handled || cmd != nil {
		t.Fatalf("refused always-tool must not be handled (handled=%v cmd=%v)", handled, cmd != nil)
	}
	if !strings.Contains(permRefusalText(m), "not offered") {
		t.Fatalf("refusal message = %q, want it to say the choice is not offered", permRefusalText(m))
	}
	if !m.showPermDialog {
		t.Fatal("a refused always-tool must keep the permission dialog open")
	}
}

// A refused sub-agent always-rule must deliver nothing to the parked child and
// keep the sub-agent dialog armed, as an invalid choice does.
func TestPermAlwaysRuleRefusedForParkedSubAgentDeliversNothing(t *testing.T) {
	m := deliverAsks(t, newPermQueueTestModel(t), bashWholeLineAsk(t))
	respCh := make(chan agent.PermissionResponse, 1)
	m.pendingSubAgentResp = respCh

	cmd, handled := m.answerPermAsk("a")
	if handled || cmd != nil {
		t.Fatalf("refused sub-agent always-rule must not be handled (handled=%v cmd=%v)", handled, cmd != nil)
	}
	select {
	case resp := <-respCh:
		t.Fatalf("refused sub-agent always-rule resumed the child with %v", resp.Level)
	default:
	}
	if m.pendingSubAgentResp != respCh {
		t.Fatal("refused sub-agent always-rule must keep the response channel armed")
	}
	if !m.showPermDialog && m.pendingSubAgentResp == nil {
		t.Fatal("refused sub-agent always-rule must keep the sub-agent dialog open")
	}
}
