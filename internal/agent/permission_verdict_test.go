package agent

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
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
	a := newTestAgent(nil, nil, cfg, nil)
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

// TestAskPermissionModelRepromptRecovery covers the strict-format retry: when
// the judge's first reply has no parseable verdict, one reprompt demanding the
// bare verdict line runs before falling through to a human prompt.
func TestAskPermissionModelRepromptRecovery(t *testing.T) {
	newAgentWithScript := func(responses []string) (*Agent, *scriptedCaptureClient) {
		cfg := &config.Config{}
		cfg.Ocode.Permissions.Auto = &config.AutoPermissionConfig{Enabled: true, Model: "mock/model"}
		a := newTestAgent(nil, nil, cfg, nil)
		client := &scriptedCaptureClient{Responses: responses}
		prevClientFn := newClientFn
		t.Cleanup(func() { newClientFn = prevClientFn })
		newClientFn = func(_ *config.Config, _ string) LLMClient { return client }
		return a, client
	}

	t.Run("retry recovers allow", func(t *testing.T) {
		a, client := newAgentWithScript([]string{
			"This looks safe to me and I would approve the call.", // no parseable verdict
			"ALLOW: safe mock tool call",                          // strict retry answer
		})
		allowed, reason, _ := a.askPermissionModel("mock_tool", json.RawMessage(`{}`), nil)
		if !allowed {
			t.Fatalf("expected retry to recover ALLOW, got denied: %q", reason)
		}
		if client.CallCount != 2 {
			t.Fatalf("expected exactly 2 model calls (initial + reprompt), got %d", client.CallCount)
		}
	})

	t.Run("still ambiguous after retry falls through", func(t *testing.T) {
		a, client := newAgentWithScript([]string{
			"This looks safe to me and I would approve the call.",
			"I really do think this should be approved.", // still no verdict
		})
		allowed, reason, _ := a.askPermissionModel("mock_tool", json.RawMessage(`{}`), nil)
		if allowed {
			t.Fatal("ambiguous retry must not auto-allow")
		}
		if !strings.Contains(reason, "ambiguous") {
			t.Fatalf("expected ambiguous reason, got %q", reason)
		}
		if client.CallCount != 2 {
			t.Fatalf("expected exactly 2 model calls, got %d", client.CallCount)
		}
	})

	t.Run("clean verdict skips retry", func(t *testing.T) {
		a, client := newAgentWithScript([]string{"ALLOW: fine"})
		allowed, _, _ := a.askPermissionModel("mock_tool", json.RawMessage(`{}`), nil)
		if !allowed {
			t.Fatal("expected allow")
		}
		if client.CallCount != 1 {
			t.Fatalf("expected single model call, got %d", client.CallCount)
		}
	})
}

// TestConsultPermissionModelFiresOnPermissionCheck verifies the hook a
// headless caller uses to show a "checking permission" indicator: it must
// fire exactly once with active=true before the consult and once with
// active=false after, even on a DENY verdict.
func TestConsultPermissionModelFiresOnPermissionCheck(t *testing.T) {
	cfg := &config.Config{}
	cfg.Ocode.Permissions.Auto = &config.AutoPermissionConfig{Enabled: true, Model: "mock/model"}
	a := newTestAgent(nil, nil, cfg, nil)
	prevClientFn := newClientFn
	t.Cleanup(func() { newClientFn = prevClientFn })
	newClientFn = func(_ *config.Config, _ string) LLMClient {
		return &scriptedCaptureClient{Responses: []string{"DENY: nope"}}
	}

	var calls []string
	a.OnPermissionCheck = func(toolName, modelLabel string, active bool) {
		calls = append(calls, fmt.Sprintf("%s/%t", toolName, active))
	}

	a.consultPermissionModel("bash", json.RawMessage(`{"command":"ps -p 1"}`), nil)

	want := []string{"bash/true", "bash/false"}
	if len(calls) != len(want) || calls[0] != want[0] || calls[1] != want[1] {
		t.Fatalf("expected %v, got %v", want, calls)
	}
}

// TestConsultPermissionModelReportsUnconsulted covers the infrastructure-failure
// paths: when no verdict is obtained the consulted flag must be false so callers
// present the request as an ordinary human ask, not as an LLM denial.
func TestConsultPermissionModelReportsUnconsulted(t *testing.T) {
	t.Run("client creation fails", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Ocode.Permissions.Auto = &config.AutoPermissionConfig{Enabled: true, Model: "mock/model"}
		a := newTestAgent(nil, nil, cfg, nil)
		prevClientFn := newClientFn
		t.Cleanup(func() { newClientFn = prevClientFn })
		newClientFn = func(_ *config.Config, _ string) LLMClient { return nil }

		allowed, reason, _, consulted := a.consultPermissionModel("bash", json.RawMessage(`{"command":"ps -p 1"}`), nil)
		if allowed {
			t.Fatal("failed client must not allow")
		}
		if consulted {
			t.Fatalf("expected consulted=false when the client cannot be built, reason=%q", reason)
		}
	})

	t.Run("real verdict is consulted", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Ocode.Permissions.Auto = &config.AutoPermissionConfig{Enabled: true, Model: "mock/model"}
		a := newTestAgent(nil, nil, cfg, nil)
		prevClientFn := newClientFn
		t.Cleanup(func() { newClientFn = prevClientFn })
		newClientFn = func(_ *config.Config, _ string) LLMClient {
			return &scriptedCaptureClient{Responses: []string{"DENY: touches files outside the workspace"}}
		}

		allowed, reason, _, consulted := a.consultPermissionModel("bash", json.RawMessage(`{"command":"ps -p 1"}`), nil)
		if allowed {
			t.Fatal("DENY verdict must not allow")
		}
		if !consulted {
			t.Fatal("a real DENY verdict must report consulted=true")
		}
		if !strings.Contains(reason, "outside the workspace") {
			t.Fatalf("expected the model's reason, got %q", reason)
		}
	})
}

// envAskThroughDecide runs cmd through Decide and returns the ask it raised,
// so the env-var tests exercise the real ask builder and verifyAutoGrant.
func envAskThroughDecide(t *testing.T, a *Agent, cmd string) (PermissionDecision, json.RawMessage) {
	t.Helper()
	args := json.RawMessage(`{"command":` + jsonStr(cmd) + `}`)
	dec := a.permissions.Decide("bash", args)
	if dec.Level != PermissionAsk || dec.Request == nil {
		t.Fatalf("Decide(%q) level = %s, want ask", cmd, dec.Level)
	}
	return dec, args
}

// Only toolchain cache env vars are judge-eligible when they point outside the
// roots; code/config-loading and install/output-target vars stay vetoed.
func TestEnvVarAskJudgeEligibilityByVar(t *testing.T) {
	a := newTestAgent(nil, nil, &config.Config{}, nil)
	a.permissions.SetWorkDir(t.TempDir())
	for _, tc := range []struct {
		env       string
		wantJudge bool
	}{
		{"GOCACHE", true},
		{"PIP_CACHE_DIR", true},
		{"GOPATH", false},
		{"GOMODCACHE", false},
		{"JAVA_HOME", false},
		{"PYTHONPATH", false},
		{"LD_PRELOAD", false},
	} {
		cmd := tc.env + "=/nonexistent-root-xyz/sdk go test ./..."
		dec, args := envAskThroughDecide(t, a, cmd)
		if dec.Request.Rule != "bash.env.out_of_scope" {
			t.Fatalf("%s: rule = %q, want bash.env.out_of_scope", tc.env, dec.Request.Rule)
		}
		if dec.Request.JudgeEligible != tc.wantJudge {
			t.Fatalf("%s: JudgeEligible = %v, want %v", tc.env, dec.Request.JudgeEligible, tc.wantJudge)
		}
		if ok, reason := a.verifyAutoGrant("bash", args, dec.Request); ok != tc.wantJudge {
			t.Fatalf("%s: verifyAutoGrant ok = %v (reason=%q), want %v", tc.env, ok, reason, tc.wantJudge)
		}
	}
}

// A sensitive path is vetoed whatever variable carries it, even a toolchain
// variable that is judge-eligible when it points at an ordinary path.
func TestEnvVarAskSensitivePathStaysVetoed(t *testing.T) {
	a := newTestAgent(nil, nil, &config.Config{}, nil)
	a.permissions.SetWorkDir(t.TempDir())
	cmd := "GOCACHE=/nonexistent-root-xyz/.env go build ./..."
	dec, args := envAskThroughDecide(t, a, cmd)
	if dec.Request.Rule != "bash.env.sensitive_path" {
		t.Fatalf("rule = %q, want bash.env.sensitive_path", dec.Request.Rule)
	}
	if ok, reason := a.verifyAutoGrant("bash", args, dec.Request); ok {
		t.Fatalf("sensitive env path auto-granted (reason=%q)", reason)
	}
}

// A toolchain variable outside the roots is judge-eligible only when it is the
// sole flagged assignment: a later non-exempt assignment wins the ask.
func TestEnvVarAskExemptThenLdPreloadStaysVetoed(t *testing.T) {
	a := newTestAgent(nil, nil, &config.Config{}, nil)
	a.permissions.SetWorkDir(t.TempDir())
	cmd := "GOCACHE=/nonexistent-root-xyz/cache LD_PRELOAD=/nonexistent-root-xyz/evil.so go test ./..."
	dec, args := envAskThroughDecide(t, a, cmd)
	if dec.Request.JudgeEligible {
		t.Fatalf("ask for LD_PRELOAD is judge-eligible; a non-exempt assignment must win")
	}
	if ok, reason := a.verifyAutoGrant("bash", args, dec.Request); ok {
		t.Fatalf("LD_PRELOAD behind an exempt var auto-granted (reason=%q)", reason)
	}
}

// Decide keeps the first Ask across segments, so an exempt ask in one segment
// must not hide a non-exempt env path in a later segment from verifyAutoGrant.
func TestEnvVarAskExemptSegmentDoesNotHideLaterSegment(t *testing.T) {
	a := newTestAgent(nil, nil, &config.Config{}, nil)
	a.permissions.SetWorkDir(t.TempDir())
	cmd := "GOCACHE=/nonexistent-root-xyz/cache go build ./... && LD_PRELOAD=/nonexistent-root-xyz/evil.so ls"
	dec, args := envAskThroughDecide(t, a, cmd)
	if ok, reason := a.verifyAutoGrant("bash", args, dec.Request); ok {
		t.Fatalf("later-segment LD_PRELOAD auto-granted behind exempt segment (reason=%q)", reason)
	}
}

// A judge-eligible ask must keep its eligibility through the JSON round trip
// that the server and remote surfaces perform.
func TestEnvVarAskJudgeEligibilitySurvivesJSON(t *testing.T) {
	a := newTestAgent(nil, nil, &config.Config{}, nil)
	a.permissions.SetWorkDir(t.TempDir())
	dec, args := envAskThroughDecide(t, a, "GOCACHE=/nonexistent-root-xyz/cache go build ./...")
	raw, err := json.Marshal(dec.Request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	var back PermissionRequest
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if !back.JudgeEligible {
		t.Fatalf("JudgeEligible lost in JSON round trip: %s", raw)
	}
	if ok, reason := a.verifyAutoGrant("bash", args, &back); !ok {
		t.Fatalf("round-tripped exempt ask refused by verifyAutoGrant: %s", reason)
	}
}

// A library-preload var into a temp dir asks and is vetoed; other env vars
// pointing into temp dirs stay in scope.
func TestEnvVarPreloadInTempDirAsks(t *testing.T) {
	a := newTestAgent(nil, nil, &config.Config{}, nil)
	a.permissions.SetWorkDir(t.TempDir())
	for _, env := range []string{"LD_PRELOAD", "DYLD_INSERT_LIBRARIES", "LD_LIBRARY_PATH"} {
		dec, _ := envAskThroughDecide(t, a, env+"=/tmp/evil.so go test ./...")
		if dec.Request.JudgeEligible || dec.Request.OutOfScopePath == "" {
			t.Fatalf("%s into /tmp must be vetoed, got %+v", env, dec.Request)
		}
	}
	for _, cmd := range []string{"FOO=/tmp/x go test ./...", "GOCACHE=/tmp/gocache go build ./..."} {
		dec := a.permissions.Decide("bash", json.RawMessage(`{"command":`+jsonStr(cmd)+`}`))
		if dec.Level == PermissionAsk && dec.Request != nil && dec.Request.Rule == "bash.env.out_of_scope" {
			t.Fatalf("%q must not ask for env scope", cmd)
		}
	}
}

// A sensitive redirection target inside the allowed roots raises an Ask with no
// OutOfScopePath; verifyAutoGrant must still veto it, in any segment, while
// ordinary redirections keep being grantable.
func TestVerifyAutoGrantRedirectionSensitiveVeto(t *testing.T) {
	a := newTestAgent(nil, nil, &config.Config{}, nil)
	work := t.TempDir()
	a.permissions.SetWorkDir(work)
	grant := func(cmd string) (bool, string) {
		args := json.RawMessage(`{"command":` + jsonStr(cmd) + `}`)
		return a.verifyAutoGrant("bash", args, &PermissionRequest{ToolName: "bash", Command: cmd})
	}
	for _, cmd := range []string{
		"echo KEY=1 > .env",
		"echo x >> .env.local",
		"echo ok && echo KEY=1 > .env",
	} {
		if ok, _ := grant(cmd); ok {
			t.Fatalf("%q: sensitive redirection must be vetoed", cmd)
		}
	}
	for _, cmd := range []string{
		"echo hi > out.txt",
		"echo hi >> notes.log",
		"ls 2>/dev/null",
		"echo hi > .env.example",
		"echo hi > /tmp/scratch.txt",
		"echo x > .git/config", // repo metadata deliberately reaches the judge
		"echo hi && echo there > out2.txt",
	} {
		if ok, reason := grant(cmd); !ok {
			t.Fatalf("%q: ordinary redirection must stay grantable, vetoed: %s", cmd, reason)
		}
	}
}

func TestUserAllowListCoversLine(t *testing.T) {
	pm := NewPermissionManager()
	pm.SetWorkDir(t.TempDir())
	pm.SetMode(PermissionModeSandbox)
	pm.SetBashPrefixRule("htrcli", PermissionAllow)

	cases := []struct {
		name string
		cmd  string
		want bool
	}{
		{"user rule alone", "htrcli --tab 35 page", true},
		{"pipeline of user rule and builtin", "htrcli --tab 35 page 2>&1 | head -4", true},
		{"compound reported line", "htrcli --tab 35 reload 2>&1 | head -2; sleep 3; htrcli --tab 35 text body 2>&1 | head -c 600", true},
		{"unlisted segment", "htrcli --tab 35 page && curl https://example.com", false},
		{"pipe into unlisted shell", "htrcli --tab 35 page | sh", false},
		{"no user rule, only builtins", "sleep 3", false},
		{"redirect to file", "htrcli --tab 35 page > out.txt", false},
		{"env assignment", "FOO=1 htrcli --tab 35 page", false},
	}
	for _, c := range cases {
		if got := pm.userAllowListCoversLine(c.cmd); got != c.want {
			t.Errorf("%s: userAllowListCoversLine(%q) = %v, want %v", c.name, c.cmd, got, c.want)
		}
	}

	pm.SetBashPrefixRule("htrcli --tab 99", PermissionDeny)
	if pm.userAllowListCoversLine("htrcli --tab 99 page") {
		t.Error("a deny rule must beat the allow rule")
	}
}
