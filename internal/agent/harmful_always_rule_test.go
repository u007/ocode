package agent

import (
	"encoding/json"
	"testing"
)

// alwaysRuleBashReq builds a bash ask whose whole line is command and whose
// asked segment is asked (Command), persisting prefix.
func alwaysRuleBashReq(command, asked, prefix string, candidates ...string) PermissionRequest {
	args, _ := json.Marshal(map[string]string{"command": command})
	return PermissionRequest{
		ToolName:         "bash",
		Args:             args,
		Command:          asked,
		Scope:            PermissionScopeBashPrefix,
		Prefix:           prefix,
		PrefixCandidates: candidates,
	}
}

// A harmful sibling segment vetoes an always-allow rule: the rule is judged
// against the whole line, not only the segment the ask names.
func TestIsHarmfulAlwaysRuleRefusesHarmfulSiblingSegment(t *testing.T) {
	script := "/tmp/ocode-outproj2 serve -port 34873 &\ncurl -s \"$B/api\""
	req := alwaysRuleBashReq(script, "/tmp/ocode-outproj2 serve -port 34873", "/tmp/ocode-outproj2")
	if !IsHarmfulRequest(req) {
		t.Fatal("precondition: whole script should be harmful")
	}
	if !IsHarmfulAlwaysRule(req) {
		t.Fatal("always-rule must be refused when a sibling segment of the line is harmful")
	}
}

func TestIsHarmfulAlwaysRuleRefusesHarmfulSegmentAnywhereInLine(t *testing.T) {
	cases := []struct {
		name    string
		command string
		asked   string
		prefix  string
	}{
		{"harmful after &&", "go test ./... && git reset --hard", "go test ./...", "go test"},
		{"harmful after ;", "go vet ./...; git stash drop", "go vet ./...", "go vet"},
		{"harmful after newline", "go vet ./...\ngit checkout -- file", "go vet ./...", "go vet"},
		{"harmful behind wrapper", "go vet ./... && env git stash drop", "go vet ./...", "go vet"},
		{"harmful inside command substitution", "echo $(git reset --hard)", "echo", "echo"},
		{"harmful inside backticks", "echo `git reset --hard`", "echo", "echo"},
		{"harmful inside sh -c body", "ls && bash -c 'git reset --hard'", "ls", "ls"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := alwaysRuleBashReq(tc.command, tc.asked, tc.prefix)
			if !IsHarmfulAlwaysRule(req) {
				t.Fatalf("always-rule for %q must be refused", tc.command)
			}
		})
	}
}

func TestIsHarmfulAlwaysRuleRefusesOpaqueOrUnparseableLine(t *testing.T) {
	cases := []struct {
		name    string
		command string
		asked   string
		prefix  string
	}{
		{"unterminated quote", `echo "unterminated`, "echo", "echo"},
		{"expansion command word", "$g stash", "$g stash", ""},
		{"eval of a variable", `eval "$x"`, `eval "$x"`, "eval"},
		{"expansion behind a launcher", `env "$g" stash`, `env "$g" stash`, ""},
		{"pipe into bare sh", "cat notes.txt | sh", "cat notes.txt", "cat"},
		{"sh -c with unparseable body", `ls && sh -c 'echo "unterminated'`, "ls", "ls"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := alwaysRuleBashReq(tc.command, tc.asked, tc.prefix)
			if !IsHarmfulAlwaysRule(req) {
				t.Fatalf("always-rule for %q must be refused (fail closed)", tc.command)
			}
		})
	}
}

func TestIsHarmfulAlwaysRuleRefusesBroadPrefix(t *testing.T) {
	cases := []struct {
		name    string
		command string
		prefix  string
		cands   []string
	}{
		{"bare sh", "sh deploy.sh", "sh", nil},
		{"bash via path", "/bin/bash build.sh", "/bin/bash", nil},
		{"bare eval", "eval make", "eval", nil},
		{"xargs launcher", "xargs rm", "xargs", nil},
		{"env launcher", "env FOO=1 make", "env", nil},
		{"interpreter", "python3 script.py", "python3", nil},
		{"broad candidate offered", "go test ./...", "go test", []string{"sh"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := alwaysRuleBashReq(tc.command, tc.command, tc.prefix, tc.cands...)
			if !IsHarmfulAlwaysRule(req) {
				t.Fatalf("always-rule with prefix %q must be refused", tc.prefix)
			}
		})
	}
}

func TestIsHarmfulAlwaysRuleAllowsHarmlessLine(t *testing.T) {
	cases := []struct {
		name    string
		command string
		prefix  string
	}{
		{"single command", "go test ./internal/agent/", "go test"},
		{"compound of harmless commands", "ls -la && cat README.md", "ls"},
		{"quoted substitution of harmless command", `echo "$(date)"`, "echo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := alwaysRuleBashReq(tc.command, tc.command, tc.prefix)
			if IsHarmfulAlwaysRule(req) {
				t.Fatalf("always-rule for harmless %q must be allowed", tc.command)
			}
		})
	}
}

func TestIsHarmfulAlwaysRuleNonBashUsesRequestGate(t *testing.T) {
	req := PermissionRequest{ToolName: "webfetch", Rule: "webfetch.domain.example.com"}
	if IsHarmfulAlwaysRule(req) {
		t.Fatal("non-bash always-rule with no harmful request must be allowed")
	}
}

func TestIsHarmfulAlwaysDecisionFailsClosedOnUnknownDecision(t *testing.T) {
	req := alwaysRuleBashReq("go test ./...", "go test ./...", "go test")
	if !IsHarmfulAlwaysDecision("allow", req) {
		t.Fatal("an unknown always-style decision must be refused")
	}
	if !IsHarmfulAlwaysDecision("", req) {
		t.Fatal("an empty decision must be refused")
	}
}

func TestIsHarmfulAlwaysDecisionSeparatesRuleFromTool(t *testing.T) {
	// A harmful sibling refuses the rule, and the tool-level check (which
	// judges the same line) refuses it too.
	harmful := alwaysRuleBashReq("go test ./... && git reset --hard", "go test ./...", "go test")
	if !IsHarmfulAlwaysDecision(AlwaysDecisionRule, harmful) {
		t.Fatal("always_rule must be refused for a harmful line")
	}
	if !IsHarmfulAlwaysDecision(AlwaysDecisionTool, harmful) {
		t.Fatal("always_tool must be refused for a harmful line")
	}

	// A line that is only broad (not harmful) refuses the rule but the tool
	// gate, which has no prefix concept, lets it through.
	broad := alwaysRuleBashReq("sh deploy.sh", "sh deploy.sh", "sh")
	if !IsHarmfulAlwaysDecision(AlwaysDecisionRule, broad) {
		t.Fatal("always_rule must be refused for a broad prefix")
	}
	if IsHarmfulAlwaysDecision(AlwaysDecisionTool, broad) {
		t.Fatal("always_tool gate judges harm only; a broad prefix is not harmful")
	}
}
