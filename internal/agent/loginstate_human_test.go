package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/redact"
	"github.com/u007/ocode/internal/tool"
)

// loginStateFoldWant is the case-insensitivity every login-state match must
// have on this platform: APFS (the macOS default) and NTFS resolve
// Browse/Login-States to the same directory, Linux filesystems do not.
func loginStateFoldWant() bool {
	return runtime.GOOS == "darwin" || runtime.GOOS == "windows"
}

// loginStateAskPayload decodes the pending human ask a HandleToolCall returned.
func loginStateAskPayload(t *testing.T, res string) PermissionRequest {
	t.Helper()
	if !strings.HasPrefix(res, tool.SentinelPermissionAsk) {
		t.Fatalf("result = %q, want a pending human ask", res)
	}
	var req PermissionRequest
	if err := json.Unmarshal([]byte(strings.TrimPrefix(res, tool.SentinelPermissionAsk)), &req); err != nil {
		t.Fatalf("unmarshal ask payload: %v", err)
	}
	return req
}

// TestLoginStateNeverReachesTheJudge runs the agent's own tool-call path with the
// auto-permission judge on and a judge that would allow anything. A login-state
// read, apply_patch, multi_file_edit and bash cat each stay a pending human ask
// and the judge is never consulted. The browser click is the positive control.
func TestLoginStateNeverReachesTheJudge(t *testing.T) {
	f := newLoginStateFixture(t)
	cfg := &config.Config{}
	cfg.Ocode.Permissions.Auto = &config.AutoPermissionConfig{Enabled: true, Model: "mock/model"}
	a := NewAgent(nil, nil, cfg, nil)
	a.SetWorkDir(t.TempDir())
	a.Permissions().SetAutoPermissionEnabled(true)
	a.AddTools([]tool.Tool{tool.ReadTool{}, tool.PatchTool{}, tool.MultiFileEditTool{}, tool.BashTool{}, newBrFakeBrowser(t)})

	var judged atomic.Int32
	prevClient := newClientFn
	t.Cleanup(func() { newClientFn = prevClient })
	newClientFn = func(_ *config.Config, _ string) LLMClient {
		judged.Add(1)
		return &MockClient{Response: &Message{Role: "assistant", Content: "ALLOW: go ahead"}}
	}
	prevCheck := a.OnPermissionCheck
	a.OnPermissionCheck = func(string, string, bool) { judged.Add(1) }
	t.Cleanup(func() { a.OnPermissionCheck = prevCheck })

	q := func(v any) string { b, _ := json.Marshal(v); return string(b) }
	cases := []struct {
		name string
		tool string
		args string
	}{
		{"read", "read", `{"path":` + q(f.lsFile) + `}`},
		{"apply_patch", "apply_patch", `{"patchText":` + q("*** Begin Patch\n*** Update File: "+f.lsFile+"\n@@\n-a\n+b\n*** End Patch") + `}`},
		{"multi_file_edit", "multi_file_edit", `{"edits":[{"path":` + q(f.lsFile) + `,"search":"a","replace":"b"}]}`},
		{"bash cat", "bash", `{"command":` + q("cat "+f.lsFile) + `}`},
	}

	// Positive control: an ordinary browser click does reach the judge, so a
	// zero count below means the gate, not a harness that never calls it.
	if _, err := a.HandleToolCall("browser", json.RawMessage(`{"action":"click","x":1,"y":2}`)); err != nil {
		t.Fatalf("click HandleToolCall: %v", err)
	}
	if judged.Load() == 0 {
		t.Fatal("positive control: the judge was never consulted for a click; the harness proves nothing")
	}

	for _, c := range cases {
		judged.Store(0)
		res, err := a.HandleToolCall(c.tool, json.RawMessage(c.args))
		if err != nil {
			t.Fatalf("%s HandleToolCall: %v", c.name, err)
		}
		if n := judged.Load(); n != 0 {
			t.Errorf("%s: the judge was consulted %d time(s) for a login-state ask; it must never be", c.name, n)
		}
		req := loginStateAskPayload(t, res)
		if !IsLoginStateRequest(req) {
			t.Errorf("%s: pending ask rule = %q prefix = %q, want the login-state category", c.name, req.Rule, req.Prefix)
		}
	}
}

// TestLoginStateDenyNeverReachesTheJudge: a user deny pattern on a login-state
// path is a Deny, not an Ask, and the judge must not be able to overturn it.
func TestLoginStateDenyNeverReachesTheJudge(t *testing.T) {
	f := newLoginStateFixture(t)
	cfg := &config.Config{}
	cfg.Ocode.Permissions.Auto = &config.AutoPermissionConfig{Enabled: true, Model: "mock/model"}
	a := NewAgent(nil, nil, cfg, nil)
	a.SetWorkDir(t.TempDir())
	a.Permissions().SetAutoPermissionEnabled(true)
	a.AddTools([]tool.Tool{tool.ReadTool{}})
	a.Permissions().SetPathRule("read", f.lsFile, PermissionDeny)

	var judged atomic.Int32
	prevClient := newClientFn
	t.Cleanup(func() { newClientFn = prevClient })
	newClientFn = func(_ *config.Config, _ string) LLMClient {
		judged.Add(1)
		return &MockClient{Response: &Message{Role: "assistant", Content: "ALLOW: go ahead"}}
	}

	args := json.RawMessage(`{"path":` + strings.TrimSpace(jsonQuote(f.lsFile)) + `}`)
	res, err := a.HandleToolCall("read", args)
	if err != nil {
		t.Fatalf("read HandleToolCall: %v", err)
	}
	if n := judged.Load(); n != 0 {
		t.Errorf("the judge was consulted %d time(s) for a denied login-state read; it must never be", n)
	}
	if strings.Contains(res, "{}") {
		t.Errorf("denied login-state read returned file contents: %q", res)
	}
}

// TestLoginStateAlwaysAllowPersistsNothing: no always-choice is offered for a
// login-state ask, and an allow-always answer that a host sends anyway installs
// no tool rule, no user-confirmed tool and no bash prefix rule.
func TestLoginStateAlwaysAllowPersistsNothing(t *testing.T) {
	f := newLoginStateFixture(t)
	q := func(v any) string { b, _ := json.Marshal(v); return string(b) }
	cases := []struct {
		name string
		tool string
		args string
	}{
		{"read", "read", `{"path":` + q(f.lsFile) + `}`},
		{"apply_patch", "apply_patch", `{"patchText":` + q("*** Begin Patch\n*** Update File: "+f.lsFile+"\n@@\n-a\n+b\n*** End Patch") + `}`},
		{"bash cat", "bash", `{"command":` + q("cat "+f.lsFile) + `}`},
	}
	for _, c := range cases {
		a := NewAgent(nil, nil, &config.Config{}, nil)
		a.SetWorkDir(t.TempDir())
		dec := a.permissions.Decide(c.tool, json.RawMessage(c.args))
		if dec.Request == nil || !IsLoginStateRequest(*dec.Request) {
			t.Fatalf("%s: decision %s has no login-state request", c.name, dec.Level)
		}
		req := *dec.Request
		if AlwaysRuleChoiceAvailable(req) {
			t.Errorf("%s: AlwaysRuleChoiceAvailable = true, want false", c.name)
		}
		if AlwaysToolChoiceAvailable(req) {
			t.Errorf("%s: AlwaysToolChoiceAvailable = true, want false", c.name)
		}
		for _, resp := range []PermissionResponse{
			{Level: PermissionAllow, PersistRule: true},
			{Level: PermissionAllow, PersistTool: true},
		} {
			a.applyPermissionResponse(req, resp)
		}
		pm := a.permissions
		if c.tool != "bash" && pm.IsUserConfirmedRule(c.tool) {
			t.Errorf("%s: an always-allow answer installed a user-confirmed %s rule", c.name, c.tool)
		}
		if _, ok := pm.bashPrefixes.load()["permission.login_state"]; ok {
			t.Errorf("%s: an always-allow answer persisted a bash prefix rule for the login-state gate", c.name)
		}
		if again := pm.Decide(c.tool, json.RawMessage(c.args)); !IsLoginStateRequest(*again.Request) || again.Level != PermissionAsk {
			t.Errorf("%s: after an always-allow answer the login-state ask is %s, want Ask", c.name, again.Level)
		}
	}
}

// TestLoginStateJudgeContextWithholdsContents: the auto-permission judge's
// context must not carry the contents of a login-state file. The file is
// reached through a symlink whose own name and path carry no login-state
// marker, so only the resolved target identifies it.
func TestLoginStateJudgeContextWithholdsContents(t *testing.T) {
	f := newLoginStateFixture(t)
	const secret = "SECRETCOOKIEVALUE7f3a"
	if err := os.WriteFile(f.lsFile, []byte(`{"cookies":[{"name":"sid","value":"`+secret+`"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias.json")
	if err := os.Symlink(f.lsFile, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	a := NewAgent(nil, nil, &config.Config{}, nil)
	a.SetWorkDir(t.TempDir())

	read := a.buildPermissionContext("read", json.RawMessage(`{"path":`+jsonQuote(alias)+`}`), 8192, 10, 40)
	if strings.Contains(read, secret) {
		t.Errorf("judge context for a read of a login-state file carries its contents:\n%s", read)
	}
	if !strings.Contains(read, "contents withheld") {
		t.Errorf("judge context for a read of a login-state file has no withheld marker:\n%s", read)
	}
	bash := a.buildPermissionContext("bash", json.RawMessage(`{"command":`+jsonQuote("cat "+alias)+`}`), 8192, 10, 40)
	if strings.Contains(bash, secret) {
		t.Errorf("judge context for bash cat of a login-state file carries its contents:\n%s", bash)
	}
}

// TestLoginStateApprovedReadIsRedacted: a read of a login-state file that the
// user approved goes through the same tool-result treatment as auth.json, the
// sensitive-file branch of scanToolResult. auth.json is the parity control.
func TestLoginStateApprovedReadIsRedacted(t *testing.T) {
	f := newLoginStateFixture(t)
	const novel = "zzqxNovelCookieValue9999" // no known format, no keyword: tier-1 misses it
	body := []byte(`{"cookies":[{"name":"sid","value":"` + novel + `"}]}`)
	for _, p := range []string{f.lsFile, f.authPath} {
		if err := os.WriteFile(p, body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct{ name, path string }{
		{"login-state file", f.lsFile},
		{"auth.json (parity)", f.authPath},
	} {
		sc := &recordingScanner{target: novel}
		a := &Agent{tools: map[string]tool.Tool{"read": tool.ReadTool{}}}
		a.SetRedactionEnabled(true)
		a.SetRedactionRegistry(redact.NewRegistry("loginst"))
		a.SetRedactionScanner(sc)
		args, _ := json.Marshal(map[string]string{"path": c.path})
		out, err := a.HandleApprovedToolCall("read", json.RawMessage(args), "call-login-1")
		if err != nil {
			t.Fatalf("%s: HandleApprovedToolCall: %v", c.name, err)
		}
		if sc.calls != 1 {
			t.Errorf("%s: tier-2 scanner ran %d time(s), want 1 (the sensitive-file branch)", c.name, sc.calls)
		}
		if strings.Contains(out, novel) {
			t.Errorf("%s: approved read reached the transcript unmasked: %q", c.name, out)
		}
	}
}

// TestLoginStateSegmentCaseFoldCore pins the case-fold core for both values of
// fold, so the segment logic is checked on every platform.
func TestLoginStateSegmentCaseFoldCore(t *testing.T) {
	for _, c := range []struct {
		path string
		fold bool
		want bool
	}{
		{"browse/login-states/x.json", false, true},
		{"Browse/Login-States/x.json", false, false},
		{"Browse/Login-States/x.json", true, true},
		{"BROWSE/LOGIN-STATES", true, true},
		{"Browse/Login-States-old/x.json", true, false},
		{"Browse/Login-States.bak", true, false},
	} {
		if got := loginStateSegmentHitFold(c.path, c.fold); got != c.want {
			t.Errorf("loginStateSegmentHitFold(%q, fold=%v) = %v, want %v", c.path, c.fold, got, c.want)
		}
	}
	if !mentionsLoginStatesFold("cat $D/Browse/Login-States/x", true) {
		t.Error("mentionsLoginStatesFold with fold=true missed a mixed-case mention")
	}
	if mentionsLoginStatesFold("cat $D/Browse/Login-States/x", false) {
		t.Error("mentionsLoginStatesFold with fold=false matched a mixed-case mention")
	}
}

// TestLoginStateCaseFoldFollowsGOOS: the public predicates fold case on darwin
// and windows and stay case-sensitive elsewhere. The sibling spelling is never a
// match, on any platform.
func TestLoginStateCaseFoldFollowsGOOS(t *testing.T) {
	want := loginStateFoldWant()
	f := newLoginStateFixture(t)
	for _, p := range []string{"Browse/Login-States/x.json", "BROWSE/login-states/x.json"} {
		if got := isLoginStatePath(p, ""); got != want {
			t.Errorf("isLoginStatePath(%q) = %v on %s, want %v", p, got, runtime.GOOS, want)
		}
	}
	if got := mentionsLoginStates("cat $HOME/Browse/Login-States/x"); got != want {
		t.Errorf("mentionsLoginStates(mixed case) = %v on %s, want %v", got, runtime.GOOS, want)
	}
	if isLoginStatePath("Browse/Login-States-Old/x.json", "") {
		t.Error("a mixed-case sibling must never match")
	}
	upper := filepath.Join(f.dataDir, "BROWSE", "LOGIN-STATES", "x.json")
	if got := loginStateResolvedHit(upper, loginStateRoots()); got != want {
		t.Errorf("loginStateResolvedHit(%q) = %v on %s, want %v", upper, got, runtime.GOOS, want)
	}
	if !isLoginStatePath(f.lsFile, "") {
		t.Error("the exact-case login-state file must always match")
	}
}

// TestLoginStateRedactTwinAgrees pins the redact package's twin of the segment
// rule to this package's predicate for paths with no name-based sensitivity, so
// the two definitions cannot drift apart.
func TestLoginStateRedactTwinAgrees(t *testing.T) {
	f := newLoginStateFixture(t)
	for _, p := range []string{
		"browse/login-states/x.json",
		"Browse/Login-States/x.json",
		"login-states/x.json",
		"browse/login-states-old/x.json",
		f.lsFile,
		f.lsDir,
		f.sibling,
		f.other,
	} {
		if got, want := redact.IsSensitiveFile(p), isLoginStatePath(p, ""); got != want {
			t.Errorf("redact.IsSensitiveFile(%q) = %v, isLoginStatePath = %v; the twins disagree", p, got, want)
		}
	}
}

// jsonQuote returns s as a JSON string literal.
func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
