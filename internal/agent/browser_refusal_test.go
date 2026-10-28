package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

// refusingBrowserStub is a browser tool that refuses one action, as the server's
// browser tool refuses a mutating action for a read-only agent type.
type refusingBrowserStub struct {
	browserStubTool
	refused string
}

func (r *refusingBrowserStub) Refusal(args json.RawMessage) string {
	var p struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Action != r.refused {
		return ""
	}
	return "browser: action " + p.Action + " is not permitted for the explore agent"
}

var _ tool.RefusingTool = (*refusingBrowserStub)(nil)

// TestBrowserRefusal_RefusedBeforeAnyPermissionDecision pins that a refused action
// returns its refusal as the tool error, raises no ask, and runs nothing, whatever
// the permission rules say: the default rule, an always-allow rule, or YOLO.
func TestBrowserRefusal_RefusedBeforeAnyPermissionDecision(t *testing.T) {
	cases := []struct {
		name  string
		setup func(ag *Agent)
	}{
		{"default rule", func(*Agent) {}},
		{"always-allow rule", func(ag *Agent) { ag.permissions.SetRule("browser", PermissionAllow) }},
		{"yolo mode", func(ag *Agent) { ag.permissions.SetMode(PermissionModeYOLO) }},
	}
	args := json.RawMessage(`{"action":"click","tab":"b1","target":"@e1"}`)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bt := &refusingBrowserStub{browserStubTool: browserStubTool{ok: true, page: stripTab}, refused: "click"}
			ag := newTestAgent(nil, []tool.Tool{bt}, &config.Config{}, nil)
			tc.setup(ag)
			asks := 0
			ag.OnPermissionAsk = func(PermissionRequest) PermissionResponse {
				asks++
				return PermissionResponse{Level: PermissionAllow, PersistRule: true}
			}
			out, err := ag.handleToolCall("browser", args, nil, "")
			if err == nil || !strings.Contains(err.Error(), "not permitted for the explore agent") {
				t.Fatalf("refused click error = %v, want the refusal", err)
			}
			if out != "" || asks != 0 || bt.executed != 0 {
				t.Fatalf("refused click: out=%q asks=%d executed=%d; want no ask and no run", out, asks, bt.executed)
			}
		})
	}
}

// TestBrowserRefusal_AllowedActionStillRuns pins that a non-refused action on the
// same tool is decided as before: an own-tab read runs.
func TestBrowserRefusal_AllowedActionStillRuns(t *testing.T) {
	bt := &refusingBrowserStub{browserStubTool: browserStubTool{ok: true, page: ownTab}, refused: "click"}
	ag := newTestAgent(nil, []tool.Tool{bt}, &config.Config{}, nil)
	out, err := ag.handleToolCall("browser", json.RawMessage(`{"action":"text","tab":"agent1"}`), nil, "")
	if err != nil || strings.HasPrefix(out, tool.SentinelPermissionAsk) || bt.executed != 1 {
		t.Fatalf("text = (%q, %v) executed %d; want it run", out, err, bt.executed)
	}
}
