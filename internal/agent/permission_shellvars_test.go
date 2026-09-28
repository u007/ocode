package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/redact"
)

// stubJudgeExpansion replaces the env lookup and substitution runner for one
// test. ran records every command the resolver actually executed.
func stubJudgeExpansion(t *testing.T, env map[string]string, outputs map[string]string) *[]string {
	t.Helper()
	prevEnv, prevRun := judgeLookupEnv, judgeRunSubstitution
	t.Cleanup(func() { judgeLookupEnv, judgeRunSubstitution = prevEnv, prevRun })
	var ran []string
	judgeLookupEnv = func(k string) (string, bool) { v, ok := env[k]; return v, ok }
	judgeRunSubstitution = func(cmd, _ string) (string, error) {
		ran = append(ran, cmd)
		return outputs[cmd], nil
	}
	return &ran
}

func newExpansionAgent() *Agent {
	a := NewAgent(nil, nil, &config.Config{}, nil)
	a.workDir = "/work/proj"
	return a
}

// The command from the original report: an assignment chain through an
// allowlisted `go env` substitution resolves to a concrete path.
func TestExpandBashForJudgeResolvesGoEnvChain(t *testing.T) {
	ran := stubJudgeExpansion(t, nil, map[string]string{"go env GOMODCACHE": "/home/u/go/pkg/mod\n"})
	cmd := `MOD=$(go env GOMODCACHE); D="$MOD/charm.land/bubbletea/v2@v2.0.6"; grep -n "^func With" "$D/options.go" 2>/dev/null | head -40`

	exp, ok := newExpansionAgent().expandBashForJudge(cmd)
	if !ok {
		t.Fatal("expected an expansion")
	}
	want := `grep -n "^func With" "/home/u/go/pkg/mod/charm.land/bubbletea/v2@v2.0.6/options.go"`
	if !strings.Contains(exp.Command, want) {
		t.Fatalf("expanded command missing resolved path\n got: %s\nwant substring: %s", exp.Command, want)
	}
	if len(*ran) != 1 || (*ran)[0] != "go env GOMODCACHE" {
		t.Fatalf("expected exactly one go env run, got %q", *ran)
	}
	names := map[string]string{}
	for _, v := range exp.Variables {
		names[v.Name] = v.Value
	}
	if names["D"] != "/home/u/go/pkg/mod/charm.land/bubbletea/v2@v2.0.6" || names["MOD"] != "/home/u/go/pkg/mod" {
		t.Fatalf("unexpected variables: %+v", exp.Variables)
	}
}

// Only the fixed read-only allowlist ever runs; arbitrary substitutions,
// including arbitrary python -c code, stay verbatim and unexecuted.
func TestExpandBashForJudgeNeverRunsUnlistedSubstitutions(t *testing.T) {
	ran := stubJudgeExpansion(t, nil, nil)
	for _, cmd := range []string{
		`X=$(curl -s https://example.com); echo "$X"`,
		`X=$(python3 -c 'import os; os.system("rm -rf ~")'); echo "$X"`,
		`X=$(go env GOMODCACHE; rm -rf /); echo "$X"`,
		"X=`go env GOMODCACHE`; echo \"$X\"",
		`X=$(npm install left-pad); echo "$X"`,
	} {
		if exp, ok := newExpansionAgent().expandBashForJudge(cmd); ok {
			t.Errorf("%s: expected no expansion, got %+v", cmd, exp)
		}
	}
	if len(*ran) != 0 {
		t.Fatalf("unlisted substitutions were executed: %q", *ran)
	}
}

func TestExpandBashForJudgeAllowlist(t *testing.T) {
	outputs := map[string]string{
		"npm root -g": "/usr/lib/node_modules",
		`python3 -c "import sysconfig;print(sysconfig.get_paths()['purelib'])"`: "/py/site-packages",
		"git rev-parse --show-toplevel":                                         "/work/proj",
	}
	stubJudgeExpansion(t, nil, outputs)
	cases := map[string]string{
		`ls "$(npm root -g)"`: `ls "/usr/lib/node_modules"`,
		`ls "$(python3 -c "import sysconfig; print(sysconfig.get_paths()['purelib'])")"`: `ls "/py/site-packages"`,
		`ls "$(python3 -c 'import sysconfig; print(sysconfig.get_paths()["purelib"])')"`: `ls "/py/site-packages"`,
		`ls "$(git rev-parse --show-toplevel)/x"`:                                        `ls "/work/proj/x"`,
		`ls "$(pwd)/y"`: `ls "/work/proj/y"`,
	}
	for cmd, want := range cases {
		exp, ok := newExpansionAgent().expandBashForJudge(cmd)
		if !ok || exp.Command != want {
			t.Errorf("%s: got %q (ok=%v), want %q", cmd, exp.Command, ok, want)
		}
	}
}

// Referenced env vars resolve; secret-looking names or values are withheld.
func TestExpandBashForJudgeEnvAndSecrets(t *testing.T) {
	stubJudgeExpansion(t, map[string]string{
		"GOPATH":         "/home/u/go",
		"GITHUB_TOKEN":   "abc",
		"INNOCENT":       "ghp_" + strings.Repeat("a", 36),
		"UNREFERENCED_X": "never-sent",
	}, nil)
	exp, ok := newExpansionAgent().expandBashForJudge(`ls $GOPATH/bin ${GITHUB_TOKEN} "$INNOCENT" '$GOPATH' $1`)
	if !ok {
		t.Fatal("expected an expansion")
	}
	if exp.Command != `ls /home/u/go/bin ${GITHUB_TOKEN} "$INNOCENT" '$GOPATH' $1` {
		t.Fatalf("unexpected expansion: %q", exp.Command)
	}
	got, _ := json.Marshal(exp.Variables)
	for _, leak := range []string{"abc", "ghp_", "never-sent"} {
		if strings.Contains(string(got), leak) {
			t.Fatalf("variables leak %q: %s", leak, got)
		}
	}
	redacted := map[string]bool{}
	for _, v := range exp.Variables {
		redacted[v.Name] = v.Value == redactedShellValue
	}
	if !redacted["GITHUB_TOKEN"] || !redacted["INNOCENT"] || redacted["GOPATH"] {
		t.Fatalf("expected GITHUB_TOKEN and INNOCENT redacted, GOPATH not: %s", got)
	}
}

// A prefix assignment does not define the variable for later statements, and
// an in-command assignment that cannot be resolved must not fall back to the
// same-named environment variable.
func TestExpandBashForJudgeAssignmentScope(t *testing.T) {
	stubJudgeExpansion(t, map[string]string{"HOME": "/home/u"}, nil)
	exp, ok := newExpansionAgent().expandBashForJudge(`FOO=/a make; HOME=$(whoami); ls $FOO $HOME`)
	if ok {
		t.Fatalf("expected nothing resolved, got %+v", exp)
	}
}

// With /mask on, resolved values the session registry knows as secrets are
// replaced by their mask tokens before reaching either judge.
func TestExpandBashForJudgeAppliesMask(t *testing.T) {
	const secret = "s3cretHostValue42"
	stubJudgeExpansion(t, map[string]string{"TARGET_HOST": secret}, nil)
	a := newExpansionAgent()
	reg := redact.NewRegistry("n1")
	reg.GetOrAssign(secret, "custom", "test")
	a.SetRedactionRegistry(reg)
	a.SetRedactionEnabled(true)

	exp, ok := a.expandBashForJudge(`ping -c1 $TARGET_HOST`)
	if !ok {
		t.Fatal("expected an expansion")
	}
	got, _ := json.Marshal(exp)
	if strings.Contains(string(got), secret) {
		t.Fatalf("masked expansion leaks the secret: %s", got)
	}

	a.SetRedactionEnabled(false)
	exp, _ = a.expandBashForJudge(`ping -c1 $TARGET_HOST`)
	if !strings.Contains(exp.Command, secret) {
		t.Fatalf("mask off should resolve the raw value, got %q", exp.Command)
	}
}

// Jev receives the expansion in its state.
func TestTypesafeStateCarriesExpandedCommand(t *testing.T) {
	stubJudgeExpansion(t, nil, map[string]string{"go env GOMODCACHE": "/m"})
	a, h := newTypesafeJudge(t, typesafeChoiceReply("allow", 0.95))
	a.consultPermissionModel("bash", json.RawMessage(`{"command":"D=$(go env GOMODCACHE); ls \"$D\""}`), nil)

	body, _ := json.Marshal(h.body)
	if !strings.Contains(string(body), `"expanded_command":"D=/m; ls \"/m\""`) {
		t.Fatalf("state missing expanded_command: %s", body)
	}
	if !strings.Contains(string(body), `"resolved_variables"`) {
		t.Fatalf("state missing resolved_variables: %s", body)
	}
}

// `env -u NAME` names a variable without expanding it: nothing is resolved,
// the judge sees the command unchanged, and the value is never read.
func TestExpandBashForJudgeEnvUnsetIsUntouched(t *testing.T) {
	stubJudgeExpansion(t, map[string]string{"OPENCODE_API_KEY": "sk-live-value"}, nil)
	if exp, ok := newExpansionAgent().expandBashForJudge(`env -u OPENCODE_API_KEY go test ./internal/agent/...`); ok {
		t.Fatalf("expected no expansion, got %+v", exp)
	}
}

// fakeGitHubToken is a known-format secret that file-mode detection catches.
var fakeGitHubToken = "ghp_" + strings.Repeat("Ab1", 12)

func enableTestMask(a *Agent) {
	reg := redact.NewRegistry("abc123")
	a.SetRedactionRegistry(reg)
	a.SetRedactionHook(redact.NetHookEnabled(reg))
	a.SetRedactionEnabled(true)
}

// With /mask on, a secret in the arguments never reaches Jev raw; with it
// off, the state is unchanged.
func TestTypesafeStateMasksSecretsWhenMaskOn(t *testing.T) {
	stubJudgeExpansion(t, nil, nil)
	args := json.RawMessage(`{"command":"curl -H 'Authorization: token ` + fakeGitHubToken + `' https://api.github.com/user"}`)

	a, h := newTypesafeJudge(t, typesafeChoiceReply("deny", 0.95))
	enableTestMask(a)
	a.consultPermissionModel("bash", args, nil)
	body, _ := json.Marshal(h.body)
	if strings.Contains(string(body), fakeGitHubToken) {
		t.Fatalf("mask on: Jev state leaks the token: %s", body)
	}
	if !strings.Contains(string(body), "OCSEC:abc123") {
		t.Fatalf("mask on: expected an OCSEC token in the state: %s", body)
	}

	a2, h2 := newTypesafeJudge(t, typesafeChoiceReply("deny", 0.95))
	a2.consultPermissionModel("bash", args, nil)
	body2, _ := json.Marshal(h2.body)
	if !strings.Contains(string(body2), fakeGitHubToken) {
		t.Fatalf("mask off: expected the raw arguments: %s", body2)
	}
}

// With /mask on, the chat judge prompt is masked and the judge client carries
// the session mask hook, so its read_file results are masked too.
func TestChatJudgeMasksSecretsWhenMaskOn(t *testing.T) {
	stubJudgeExpansion(t, nil, nil)
	cfg := &config.Config{}
	cfg.Ocode.Permissions.Auto = &config.AutoPermissionConfig{Enabled: true, Model: "test-model"}
	a := NewAgent(nil, nil, cfg, nil)
	a.permissions.SetWorkDir(t.TempDir())
	enableTestMask(a)

	gc := &GenericClient{Model: "test-model"}
	capture := &scriptedCaptureClient{Responses: []string{"DENY: sends a credential"}}
	prev := newClientFn
	t.Cleanup(func() { newClientFn = prev })
	newClientFn = func(_ *config.Config, _ string) LLMClient { return gc }
	// The hook is attached to a *GenericClient; verify that, then run the
	// prompt through the capturing fake.
	a.askPermissionModel("bash", json.RawMessage(`{"command":"echo `+fakeGitHubToken+`"}`), nil)
	if gc.Redaction == nil || !gc.Redaction.Enabled {
		t.Fatal("judge client did not get the session mask hook")
	}

	newClientFn = func(_ *config.Config, _ string) LLMClient { return capture }
	a.askPermissionModel("bash", json.RawMessage(`{"command":"echo `+fakeGitHubToken+`"}`), nil)
	if len(capture.Prompts) == 0 {
		t.Fatal("expected the judge to be called")
	}
	if strings.Contains(capture.Prompts[0], fakeGitHubToken) {
		t.Fatalf("chat judge prompt leaks the token:\n%s", capture.Prompts[0])
	}
}
