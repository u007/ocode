package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/tool"
)

// A permission-ask sentinel is CONTROL FLOW, not tool output. Truncating it
// mid-JSON produces an unparseable payload, and every host that looks for an ask
// (the TUI's parsePermissionRequest, the server's parsePermissionAsk,
// livePendingAsks) then silently reports "no ask here" — so the ask vanishes and
// the model receives mangled sentinel text instead of a decision.
//
// The content guardrail made this reachable: its ask carries the FULL flagged
// result so the user can review it, which routinely exceeds the 12k tool-output
// budget. Measured before the fix: a 12KB content ask was truncated and 0 bytes
// of content recovered; 200KB likewise.
func TestTruncateToolResultNeverTruncatesAnAskSentinel(t *testing.T) {
	for _, n := range []int{500, 12000, 40000, 200000} {
		req := PermissionRequest{
			ToolName:         "some_mcp",
			Scope:            PermissionScopeContent,
			Rule:             "content.some_mcp",
			UntrustedContent: strings.Repeat("A", n),
			UntrustedSource:  "MCP some_mcp",
			UntrustedSummary: "instruction_override, confidence 0.95",
		}
		payload, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		sentinel := tool.SentinelPermissionAsk + string(payload)

		got := TruncateToolResult("call-1", sentinel)
		if got != sentinel {
			t.Errorf("content=%d: the ask sentinel was modified (len %d -> %d)", n, len(sentinel), len(got))
		}

		var parsed PermissionRequest
		if err := json.Unmarshal([]byte(strings.TrimPrefix(got, tool.SentinelPermissionAsk)), &parsed); err != nil {
			t.Fatalf("content=%d: the ask no longer parses after truncation: %v", n, err)
		}
		if len(parsed.UntrustedContent) != n {
			t.Errorf("content=%d: recovered %d bytes of flagged content", n, len(parsed.UntrustedContent))
		}
	}
}

// The question sentinel carries a JSON payload too and has the same exposure.
func TestTruncateToolResultNeverTruncatesAQuestionSentinel(t *testing.T) {
	big := tool.SentinelQuestionPrompt + "\n" +
		strings.Repeat("q", 50000) + "\n\n" + tool.SentinelWaitingForUser

	got := TruncateToolResult("call-1", big)
	if got != big {
		t.Fatalf("the question sentinel was modified (len %d -> %d)", len(big), len(got))
	}
	if !strings.Contains(got, tool.SentinelWaitingForUser) {
		t.Fatal("truncation dropped the WAITING_FOR_USER_RESPONSE terminator, so the ask can never be detected")
	}
}

// Ordinary tool output must still be truncated — the sentinel exemption is not a
// hole that disables the budget for everything.
func TestTruncateToolResultStillTruncatesOrdinaryOutput(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	big := strings.Repeat("x", maxToolResultChars+5000)
	got := TruncateToolResult("call-2", big)
	if got == big {
		t.Fatal("ordinary tool output must still be truncated")
	}
	if !strings.Contains(got, "[output truncated:") {
		t.Fatalf("expected the truncation notice, got %q", got)
	}
}
