package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/tool"
)

// TestUnknownToolReturnsNotFoundWithoutPermissionAsk is the regression test
// for the hallucination guard: a tool name that is not present in the agent's
// registered tools must be answered with "tool not found" immediately, never
// routed through the permission layer. Before the guard, an unknown symbol fell
// through to permissions.Decide → PermissionAsk → a PERMISSION_ASK prompt for
// a tool that can never run.
func TestUnknownToolReturnsNotFoundWithoutPermissionAsk(t *testing.T) {
	askCalled := false
	a := &Agent{
		tools: map[string]tool.Tool{"read": fakeTool{"read"}},
		// A wired ask callback so any accidental routing to the ask path is
		// observable. It must NOT fire for an unknown tool.
		OnPermissionAsk: func(req PermissionRequest) PermissionResponse {
			askCalled = true
			return PermissionResponse{Level: PermissionAsk}
		},
	}

	// The unknown-tool guard runs before arg validation (duplicate-key skip,
	// gating, permissions), so even malformed args must still return
	// "tool not found" without an ask.
	cases := []json.RawMessage{
		json.RawMessage(`{"arg": 1}`),
		json.RawMessage(`{"arg": }`),
	}
	for _, args := range cases {
		res, _, err := a.handleToolCallWithImages("definitely_not_a_tool", args, nil, "")

		if err != nil {
			t.Fatalf("unknown tool returned a Go error %v; want a normal tool result", err)
		}
		if !strings.Contains(res, "tool not found") {
			t.Fatalf("unknown tool result = %q; want it to contain %q", res, "tool not found")
		}
		if askCalled {
			t.Fatalf("permission ask was triggered for an unknown tool: %v", askCalled)
		}
	}
}
