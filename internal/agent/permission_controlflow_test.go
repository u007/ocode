package agent

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

// Control-flow headers (for/select/case) are shell syntax, not commands. They
// must never surface as a command fragment with a bogus prefix ("for", "case",
// a loop variable, a list word, or a case pattern label) — that produced a
// needless permission ask and a misleading "unknown command" verdict from the
// auto-permission judge.
func TestPermissions_BashControlFlowHeadersNeverBecomePrefixes(t *testing.T) {
	cases := []struct {
		command    string
		wantPrefix []string // cmdWords[0] of every fragment, in order
	}{
		{"for i in 1 2 3; do echo $i; done", []string{"echo"}},
		{"for i in 1 2 3; do\n  echo $i\ndone", []string{"echo"}},
		{"for f in *.txt; do echo $f; done", []string{"echo"}},
		{"select x in a b; do echo $x; done", []string{"echo"}},
		{"case $x in a) echo a;; esac", []string{"echo"}},
		{"case $x in a) echo a;; b) echo b;; esac", []string{"echo", "echo"}},
		{"for i in 1 2; do rm -rf /tmp/x; done", []string{"rm"}},
	}
	for _, tc := range cases {
		cmds, err := parseShellCommandLine(tc.command)
		if err != nil {
			t.Fatalf("%q: parse: %v", tc.command, err)
		}
		if len(cmds) != len(tc.wantPrefix) {
			t.Fatalf("%q: got %d fragments (%+v), want %d", tc.command, len(cmds), cmds, len(tc.wantPrefix))
		}
		for i, c := range cmds {
			if len(c.cmdWords) == 0 {
				t.Fatalf("%q: fragment %d has no command words: %+v", tc.command, i, c)
			}
			if got := c.cmdWords[0]; got != tc.wantPrefix[i] {
				t.Fatalf("%q: fragment %d prefix = %q, want %q", tc.command, i, got, tc.wantPrefix[i])
			}
		}
	}
}

// A loop or case arm whose constituent commands are individually harmless must
// auto-allow without consulting the permission model at all.
func TestPermissions_BashControlFlowAutoAllowed(t *testing.T) {
	pm := NewPermissionManager()
	allowed := []string{
		`for i in 1 2 3; do echo $i; done`,
		"for i in 1 2 3; do\n  echo $i\ndone\n",
		`for f in *.txt; do echo $f; done`,
		`select x in a b; do echo $x; done`,
		`case $x in a) echo a;; esac`,
		`case $x in a) echo a;; b) echo b;; esac`,
		`case $x in a) echo a; esac`, // single-arm form closing with ";" not ";;"
		`case $x in a) echo a;; *) echo other;; esac`,
		`for i in 1; do case j in a) echo;; esac; done`,
		`i=0; while [ $i -lt 3 ]; do echo $i; i=$((i+1)); done`,
	}
	for _, c := range allowed {
		dec := pm.Decide("bash", json.RawMessage(`{"command":`+strconv.Quote(c)+`}`))
		if dec.Level != PermissionAllow {
			t.Errorf("expected allow for %q, got level=%s prefix=%q", c, dec.Level, reqPrefix(dec))
		}
	}
}

// The parser change must not weaken enforcement: a destructive command inside a
// loop or case arm is still caught by the banned-prefix / harmful checks.
func TestPermissions_BashControlFlowBodyStillChecked(t *testing.T) {
	pm := NewPermissionManager()
	pm.SetBashPrefixRule("rm", PermissionDeny)

	denied := []string{
		`for i in 1 2; do rm -rf /tmp/x; done`,
		"for i in 1 2; do\n  rm -rf /tmp/x\ndone\n",
		`while true; do rm -rf /tmp/x; done`,
		`until false; do rm -rf /tmp/x; done`,
		`if true; then rm -rf /tmp/x; fi`,
		`if true; then echo hi; else rm -rf /tmp/x; fi`,
		`case $x in a) rm -rf /tmp/x;; esac`,
		`case $x in a) echo ok;; b) rm -rf /tmp/x;; esac`,
		`case $x in a) rm -rf /tmp/x; esac`, // single-arm form closing with ";" not ";;"
		`case $x in *) rm -rf /tmp/x;; esac`,
		`for i in $(echo 1); do rm -rf /tmp/x; done`,
		`for i in 1; do for j in 2; do rm -rf /tmp/x; done; done`,
		`for i in 1; do if true; then rm -rf /tmp/x; fi; done`,
		`for i in 1; do ( rm -rf /tmp/x ); done`,
		`while read l; do rm -rf /tmp/x; done < /tmp/in`,
		"case $x in\n  a)\n    rm -rf /tmp/x\n    ;;\nesac\n",
	}
	for _, c := range denied {
		dec := pm.Decide("bash", json.RawMessage(`{"command":`+strconv.Quote(c)+`}`))
		if dec.Level != PermissionDeny || !dec.HardDeny {
			t.Errorf("expected hard deny for %q, got level=%s hardDeny=%v", c, dec.Level, dec.HardDeny)
		}
	}

	// A command substitution in the for/select header still EXECUTES, so it
	// must be evaluated even though the header words themselves are dropped.
	pmSubst := NewPermissionManager()
	pmSubst.SetBashPrefixRule("rm", PermissionDeny)
	for _, c := range []string{
		`for i in $(rm -rf /tmp/x); do echo $i; done`,
		`select i in $(rm -rf /tmp/x); do echo $i; done`,
		`case $(rm -rf /tmp/x) in a) echo;; esac`,
	} {
		dec := pmSubst.Decide("bash", json.RawMessage(`{"command":`+strconv.Quote(c)+`}`))
		if dec.Level != PermissionDeny || !dec.HardDeny {
			t.Errorf("command substitution in header not evaluated for %q: level=%s hard=%v", c, dec.Level, dec.HardDeny)
		}
	}

	// A destructive git form inside a loop body must still be routed to Ask
	// rather than auto-allowed.
	pm2 := NewPermissionManager()
	askOnly := []string{
		`for d in a b; do git reset --hard; done`,
		`for d in a b; do git clean -fd; done`,
		`case $x in a) git stash;; esac`,
	}
	for _, c := range askOnly {
		dec := pm2.Decide("bash", json.RawMessage(`{"command":`+strconv.Quote(c)+`}`))
		if dec.Level == PermissionAllow {
			t.Errorf("expected non-allow for %q, got %s", c, dec.Level)
		}
	}
}

// A malformed/incomplete control-flow construct must fail safe — dropping the
// header words must never swallow a real command and turn it into an allow.
func TestPermissions_BashMalformedControlFlowFailsSafe(t *testing.T) {
	pm := NewPermissionManager()
	pm.SetBashPrefixRule("rm", PermissionDeny)

	for _, c := range []string{
		`case x in rm -rf /tmp/x`,    // never reaches ")"
		`case rm -rf /tmp/x`,         // never reaches "in"
		`for i in 1 2 rm -rf /tmp/x`, // never reaches "do"
	} {
		dec := pm.Decide("bash", json.RawMessage(`{"command":`+strconv.Quote(c)+`}`))
		if dec.Level == PermissionAllow {
			t.Errorf("malformed %q must not auto-allow, got allow", c)
		}
	}
}

// Bash arithmetic expansion "$((...))" is not a command. It previously leaked a
// bogus numeric fragment ("i+1") that surfaced as a needless ask — e.g.
// "i=$((i+1))" inside a while loop.
func TestPermissions_ArithmeticExpansionIsNotACommand(t *testing.T) {
	pm := NewPermissionManager()
	allowed := []string{
		`echo $((1+2))`,
		`x=$((1+2))`,
		`i=$((i+1))`,
		`i=0; while [ $i -lt 3 ]; do echo $i; i=$((i+1)); done`,
	}
	for _, c := range allowed {
		dec := pm.Decide("bash", json.RawMessage(`{"command":`+strconv.Quote(c)+`}`))
		if dec.Level != PermissionAllow {
			t.Errorf("expected allow for %q, got level=%s prefix=%q", c, dec.Level, reqPrefix(dec))
		}
	}

	// A command substitution nested inside arithmetic still executes and must
	// still be evaluated.
	cmds, err := parseShellCommandLine(`echo $(( $(rm -rf /tmp/x) + 1 ))`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var sawRM bool
	for _, c := range cmds {
		if len(c.cmdWords) > 0 && c.cmdWords[0] == "rm" {
			sawRM = true
		}
	}
	if !sawRM {
		t.Fatalf("nested command substitution inside $((...)) was not evaluated: %+v", cmds)
	}

	// ...and the nested rm is still gated.
	pm2 := NewPermissionManager()
	pm2.SetBashPrefixRule("rm", PermissionDeny)
	dec := pm2.Decide("bash", json.RawMessage(`{"command":`+strconv.Quote(`echo $(( $(rm -rf /tmp/x) + 1 ))`)+`}`))
	if dec.Level != PermissionDeny || !dec.HardDeny {
		t.Fatalf("nested rm inside arithmetic must be hard-denied, got level=%s hardDeny=%v", dec.Level, dec.HardDeny)
	}
}

// The auto-permission judge's "Command analysis" line described a loop as
// "Execute 'for' (unknown command)", which nudged the judge toward refusal.
func TestExplainBashCommandDescribesControlFlow(t *testing.T) {
	cases := map[string]string{
		"for i in 1 2 3; do echo $i; done": "loop",
		"while true; do echo hi; done":     "loop",
		"if true; then echo hi; fi":        "conditional",
		"case $x in a) echo a;; esac":      "case",
	}
	for cmd, want := range cases {
		got := explainBashCommand(cmd)
		if !strings.Contains(strings.ToLower(got), want) {
			t.Errorf("explainBashCommand(%q) = %q, want it to mention %q", cmd, got, want)
		}
		if strings.Contains(got, "unknown command") {
			t.Errorf("explainBashCommand(%q) = %q, must not call a control-flow keyword an unknown command", cmd, got)
		}
	}
}

// The reported symptom, replayed through the real tool-dispatch path: a plain
// numeric loop must execute WITHOUT consulting the auto-permission judge at
// all. Before the parser fix it reached the judge as `Execute 'for' (unknown
// command)`, which a cautious judge denied or escalated to a human ask.
func TestPermissions_BenignLoopNeedsNoJudge(t *testing.T) {
	cfg := &config.Config{}
	cfg.Ocode.Permissions.Auto = &config.AutoPermissionConfig{Enabled: true, Model: "mock/model"}
	a := NewAgent(nil, nil, cfg, nil)
	a.Permissions().SetAutoPermissionEnabled(true)
	a.AddTools([]tool.Tool{&MockTool{name: "bash", result: "ran"}})

	client := &scriptedCaptureClient{Responses: []string{"DENY: unknown command 'for'"}}
	prev := newClientFn
	t.Cleanup(func() { newClientFn = prev })
	newClientFn = func(_ *config.Config, _ string) LLMClient { return client }

	for _, script := range []string{
		"for i in 1 2 3; do echo $i; done\n",
		"for f in *.txt; do echo $f; done\n",
		"i=0\nwhile [ $i -lt 3 ]; do echo $i; i=$((i+1)); done\n",
	} {
		before := client.CallCount
		res, err := a.HandleToolCall("bash", json.RawMessage(`{"command":`+jsonStr(script)+`}`))
		if err != nil {
			t.Fatalf("%q: %v", script, err)
		}
		if res != "ran" {
			t.Errorf("%q: expected execution, got %q", script, truncate(res, 120))
		}
		if client.CallCount != before {
			t.Errorf("%q: judge consulted %d time(s); a benign loop must not need it",
				script, client.CallCount-before)
		}
	}
}

func reqPrefix(dec PermissionDecision) string {
	if dec.Request == nil {
		return ""
	}
	return dec.Request.Prefix
}
