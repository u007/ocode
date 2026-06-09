package agent

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/config"
)

func TestParsePermissionVerdict(t *testing.T) {
	cases := []struct {
		name        string
		text        string
		wantDecided bool
		wantAllow   bool
		wantReason  string
	}{
		{
			name:        "bare allow",
			text:        "ALLOW: safe tool call",
			wantDecided: true, wantAllow: true, wantReason: "safe tool call",
		},
		{
			name:        "bare deny",
			text:        "DENY: too risky",
			wantDecided: true, wantAllow: false, wantReason: "too risky",
		},
		{
			// The captured real-world failure: prose reasoning followed by a
			// markdown-bolded verdict line. Old strict prefix match denied this.
			name: "prose then bolded allow",
			text: "The `.git/config` file contains Git configuration settings. " +
				"Since the operation seems valid, I will **ALLOW** the request.\n\n" +
				"**ALLOW: The Git configuration file is valid and the operation is within the allowed scope.**",
			wantDecided: true, wantAllow: true,
			wantReason: "The Git configuration file is valid and the operation is within the allowed scope.",
		},
		{
			// Last verdict wins, but a non-verdict trailing line must not flip an
			// earlier DENY — "ALLOW only if" is prose, not a verdict.
			name:        "deny not flipped by prose allow",
			text:        "DENY: writes outside the repo\nALLOW only if you trust the source",
			wantDecided: true, wantAllow: false, wantReason: "writes outside the repo",
		},
		{
			name:        "allowed is not a verdict",
			text:        "This would be ALLOWED in most cases but I am unsure.",
			wantDecided: false,
		},
		{
			name:        "no verdict",
			text:        "I cannot determine whether this is safe.",
			wantDecided: false,
		},
		{
			name:        "bare word allow no colon",
			text:        "ALLOW",
			wantDecided: true, wantAllow: true, wantReason: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decided, allow, reason := parsePermissionVerdict(tc.text)
			if decided != tc.wantDecided {
				t.Fatalf("decided = %v, want %v", decided, tc.wantDecided)
			}
			if !decided {
				return
			}
			if allow != tc.wantAllow {
				t.Fatalf("allow = %v, want %v", allow, tc.wantAllow)
			}
			if reason != tc.wantReason {
				t.Fatalf("reason = %q, want %q", reason, tc.wantReason)
			}
		})
	}
}

func TestVerifyAutoGrantGuardrails(t *testing.T) {
	wd := t.TempDir()
	cfg := &config.Config{}
	a := NewAgent(nil, nil, cfg, nil)
	a.permissions.SetWorkDir(wd)

	inRoot := filepath.Join(wd, "notes.txt")
	outOfRoot := "/nonexistent-root-xyz/elsewhere.txt" // outside every allowed root
	sensitive := filepath.Join(wd, ".env")

	cases := []struct {
		name   string
		tool   string
		args   string
		wantOK bool
	}{
		{"delete in root", "delete", `{"path":` + jsonStr(inRoot) + `}`, true},
		{"delete sensitive denied", "delete", `{"path":` + jsonStr(sensitive) + `}`, false},
		{"delete out of roots denied", "delete", `{"path":` + jsonStr(outOfRoot) + `}`, false},
		{"bash non-blocked ok", "bash", `{"command":"git config --local --list"}`, true},
		{"bash hard-blocked denied", "bash", `{"command":"rm -rf /"}`, false},
		{"non-path tool ok", "ask_tool", `{}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := &PermissionRequest{ToolName: tc.tool}
			if tc.tool == "bash" {
				var p struct {
					Command string `json:"command"`
				}
				_ = json.Unmarshal([]byte(tc.args), &p)
				req.Command = p.Command
			}
			ok, reason := a.verifyAutoGrant(tc.tool, json.RawMessage(tc.args), req)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v (reason=%q), want %v", ok, reason, tc.wantOK)
			}
		})
	}
}

func jsonStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
