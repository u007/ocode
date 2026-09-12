package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestPermissions_ComputerDefaultAsk verifies that the computer tool
// defaults to PermissionAsk (not auto-allow) in a fresh manager.
func TestPermissions_ComputerDefaultAsk(t *testing.T) {
	pm := NewPermissionManager()
	if got := pm.Check("computer"); got != PermissionAsk {
		t.Fatalf("Check(\"computer\") = %s; want PermissionAsk", got)
	}
}

// TestPermissions_ComputerObserveActionsAllowed verifies that purely
// observational actions (screenshot, cursor_position, wait) are
// auto-allowed without prompting the user.
func TestPermissions_ComputerObserveActionsAllowed(t *testing.T) {
	cases := []struct {
		action string
		args   string
	}{
		{"screenshot", `{"action":"screenshot"}`},
		{"cursor_position", `{"action":"cursor_position"}`},
		{"wait", `{"action":"wait"}`},
		{"wait_with_duration", `{"action":"wait","duration":2}`},
	}
	for _, tc := range cases {
		t.Run(tc.action, func(t *testing.T) {
			pm := NewPermissionManager()
			dec := pm.Decide("computer", json.RawMessage(tc.args))
			if dec.Level != PermissionAllow {
				t.Fatalf("action=%s: expected Allow, got %s", tc.action, dec.Level)
			}
		})
	}
}

// TestPermissions_ComputerInputActionsAsk verifies that input actions
// that can modify the screen (click, type, key, scroll) produce an Ask
// decision carrying Rule "tool.computer" and a Command summary.
func TestPermissions_ComputerInputActionsAsk(t *testing.T) {
	cases := []struct {
		name       string
		args       string
		cmdSubstr  string
	}{
		{"left_click", `{"action":"left_click","coordinate":[412,300]}`, "left_click at 412,300"},
		{"type", `{"action":"type","text":"hello"}`, "type"},
		{"key", `{"action":"key","key":"ctrl+s"}`, "key"},
		{"scroll", `{"action":"scroll","direction":"down"}`, "scroll"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pm := NewPermissionManager()
			dec := pm.Decide("computer", json.RawMessage(tc.args))
			if dec.Level != PermissionAsk {
				t.Fatalf("action=%s: expected Ask, got %s", tc.name, dec.Level)
			}
			if dec.Request == nil {
				t.Fatalf("action=%s: expected Request, got nil", tc.name)
			}
			if dec.Request.Rule != "tool.computer" {
				t.Fatalf("action=%s: Rule=%q; want \"tool.computer\"", tc.name, dec.Request.Rule)
			}
			if tc.cmdSubstr != "" && dec.Request.Command == "" {
				t.Fatalf("action=%s: expected Command to contain %q, got empty", tc.name, tc.cmdSubstr)
			}
			if tc.cmdSubstr != "" && !strings.Contains(dec.Request.Command, tc.cmdSubstr) {
				t.Fatalf("action=%s: Command=%q; want it to contain %q", tc.name, dec.Request.Command, tc.cmdSubstr)
			}
		})
	}
}

// TestPermissions_ComputerAlwaysPersists verifies that after a user
// confirms "always allow" for the computer tool (via SetRule), input
// actions proceed without asking.
func TestPermissions_ComputerAlwaysPersists(t *testing.T) {
	pm := NewPermissionManager()
	pm.SetRule("computer", PermissionAllow)
	dec := pm.Decide("computer", json.RawMessage(`{"action":"left_click","coordinate":[412,300]}`))
	if dec.Level != PermissionAllow {
		t.Fatalf("after SetRule Allow: expected Allow, got %s", dec.Level)
	}
}

// TestPermissions_ComputerLockedDenied verifies that in locked mode
// even observational actions (screenshot) are denied, because isReadOnlyTool
// does not include "computer".
func TestPermissions_ComputerLockedDenied(t *testing.T) {
	pm := NewPermissionManager()
	pm.SetMode(PermissionModeLocked)
	dec := pm.Decide("computer", json.RawMessage(`{"action":"screenshot"}`))
	if dec.Level != PermissionDeny {
		t.Fatalf("locked mode screenshot: expected Deny, got %s", dec.Level)
	}
}


