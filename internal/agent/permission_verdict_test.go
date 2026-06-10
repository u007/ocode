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
			// Captured local-model failure: verdict prefixed with an "Answer:"
			// label inside markdown bold. The label must be stripped so the
			// verdict word lands at line start.
			name:        "labeled allow in bold",
			text:        "I have read the directory listing.\n\n**Answer: ALLOW: The command is within the allowed filesystem scope and appears to be a standard build-and-run operation.**",
			wantDecided: true, wantAllow: true,
			wantReason: "The command is within the allowed filesystem scope and appears to be a standard build-and-run operation.",
		},
		{
			// Captured local-model failure: bold closes BETWEEN the verdict word
			// and its colon ("**Allow**:"), so the closing "**" landed in the
			// boundary check and was rejected → auto-deny → human Ask.
			name: "bold closes before colon",
			text: "The target path is a directory, which is suitable for a `cd` command.\n\n" +
				"**Allow**: The command is targeting a known valid directory.",
			wantDecided: true, wantAllow: true,
			wantReason: "The command is targeting a known valid directory.",
		},
		{
			name:        "labeled deny",
			text:        "Verdict: DENY: writes outside the repo",
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
		{
			// The last-line fallback is DENY-only: a buried ALLOW is NOT
			// auto-granted (prose conditionals like "only after" cannot be fully
			// enumerated), so it falls through to a human prompt.
			name:        "buried allow on final line is not auto-granted",
			text:        "The script writes models.py inside the working dir.\nBased on the information provided, I would ALLOW this operation because it is in scope.",
			wantDecided: false,
		},
		{
			// Buried DENY fails closed — the fallback honours it.
			name:        "buried deny on final line",
			text:        "This touches files outside the repo, so I would DENY it.",
			wantDecided: true, wantAllow: false, wantReason: "",
		},
		{
			// Conditional ALLOW phrasings the negation set does not catch must
			// still NOT auto-allow, now guaranteed by the DENY-only fallback.
			name:        "allow only after is conditional not a verdict",
			text:        "I would ALLOW this, but only after the user confirms the target.",
			wantDecided: false,
		},
		{
			name:        "allow provided that is conditional not a verdict",
			text:        "I would ALLOW this provided that the path stays in the repo.",
			wantDecided: false,
		},
		{
			name:        "allow as long as is conditional not a verdict",
			text:        "Fine to ALLOW as long as it does not touch system files.",
			wantDecided: false,
		},
		{
			// Negation must not flip a buried DENY into an allow either.
			name:        "negated deny does not flip to allow",
			text:        "I would not DENY this if it stayed in scope, but it does not.",
			wantDecided: false,
		},
		{
			// Final line mentions BOTH verdict words — genuinely ambiguous, so the
			// fallback must NOT guess; falls through to deny.
			name:        "both words on final line is ambiguous",
			text:        "I could ALLOW or DENY this depending on intent.",
			wantDecided: false,
		},
		{
			// Fail-closed: negation inverts a buried ALLOW. Must NOT auto-allow.
			name:        "negated allow does not flip to allow",
			text:        "This writes outside the repo, so I would not ALLOW this operation.",
			wantDecided: false,
		},
		{
			name:        "cannot allow contraction-free negation",
			text:        "Given the risk I cannot ALLOW this command.",
			wantDecided: false,
		},
		{
			name:        "allow only if is conditional not a verdict",
			text:        "I would ALLOW only if the user confirms the target.",
			wantDecided: false,
		},
		{
			name:        "wont allow contraction",
			text:        "I won't ALLOW writes to system paths.",
			wantDecided: false,
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
