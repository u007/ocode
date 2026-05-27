package agent

import (
	"encoding/json"
	"testing"
)

// TestPermissions_ToolAllow_BypassesSensitiveAndOutOfWorkdir locks in the
// current intended Decide() semantics: a tool-level "allow" rule (typically
// set via "always allow this rule/tool") short-circuits the sensitive-path,
// out-of-workdir, and delete prompts. This is a deliberate UX trade-off — if
// the policy is ever tightened so allow no longer bypasses these scopes,
// update this test to assert the new (asking) behaviour.
func TestPermissions_ToolAllow_BypassesSensitiveAndOutOfWorkdir(t *testing.T) {
	cases := []struct {
		name string
		tool string
		args string
	}{
		{"write_sensitive", "write", `{"file_path":".env","content":"x"}`},
		{"write_out_of_scope", "write", `{"file_path":"/var/tmp/foreign/file","content":"x"}`},
		{"delete_in_workdir", "delete", `{"file_path":"sub/file.txt"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pm := NewPermissionManager()
			pm.SetWorkDir("/Users/test/project")
			pm.SetRule(tc.tool, PermissionAllow)
			dec := pm.Decide(tc.tool, json.RawMessage(tc.args))
			if dec.Level != PermissionAllow {
				t.Fatalf("tool=%s args=%s: expected Allow under tool-allow rule, got %s", tc.tool, tc.args, dec.Level)
			}
		})
	}
}

// TestPermissions_NoToolAllow_StillAsksForSensitive verifies the gates remain
// effective when there is no explicit tool-level allow: sensitive paths,
// out-of-workdir paths, and delete still produce an Ask decision with the
// correct rule scope. This guards against accidentally turning the bypass
// into a default.
func TestPermissions_NoToolAllow_StillAsksForSensitive(t *testing.T) {
	cases := []struct {
		name     string
		tool     string
		ruleSet  PermissionLevel
		args     string
		wantRule string
	}{
		{
			name:     "sensitive_path_under_ask",
			tool:     "write",
			ruleSet:  PermissionAsk,
			args:     `{"file_path":".env","content":"x"}`,
			wantRule: "tool.write.sensitive_path",
		},
		{
			name:     "out_of_workdir_under_ask",
			tool:     "write",
			ruleSet:  PermissionAsk,
			args:     `{"file_path":"/var/tmp/foreign/file","content":"x"}`,
			wantRule: "tool.write.out_of_scope",
		},
		{
			name:     "delete_under_default_ask",
			tool:     "delete",
			ruleSet:  PermissionAsk,
			args:     `{"file_path":"sub/file.txt"}`,
			wantRule: "tool.delete.delete",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pm := NewPermissionManager()
			pm.SetWorkDir("/Users/test/project")
			pm.SetRule(tc.tool, tc.ruleSet)
			dec := pm.Decide(tc.tool, json.RawMessage(tc.args))
			if dec.Level != PermissionAsk {
				t.Fatalf("expected Ask, got %s", dec.Level)
			}
			if dec.Request == nil {
				t.Fatalf("expected non-nil Request with rule %q", tc.wantRule)
			}
			if dec.Request.Rule != tc.wantRule {
				t.Fatalf("rule = %q, want %q", dec.Request.Rule, tc.wantRule)
			}
		})
	}
}

// TestPermissions_YOLO_Allows confirms YOLO mode short-circuits before any
// path-based gate. (Sanity check; not part of the regression set.)
func TestPermissions_YOLO_Allows(t *testing.T) {
	pm := NewPermissionManager()
	pm.SetWorkDir("/Users/test/project")
	pm.SetMode(PermissionModeYOLO)
	dec := pm.Decide("write", json.RawMessage(`{"file_path":".env","content":"x"}`))
	if dec.Level != PermissionAllow {
		t.Fatalf("YOLO should allow, got %s", dec.Level)
	}
}
