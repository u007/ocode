package agent

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

// browserBoxStartArgs and browserBoxTerminateArgs are the calls the box actions take.
const (
	browserBoxStartArgs     = `{"action":"start","backend":"upstash"}`
	browserBoxTerminateArgs = `{"action":"terminate","backend":"upstash"}`
)

// TestPermissions_BoxStartAsksWithItsOwnCategory pins that start is a static ask in
// normal and sandbox modes, under its own category, and that the category is
// recognised by IsBrowserBoxStartRequest.
func TestPermissions_BoxStartAsksWithItsOwnCategory(t *testing.T) {
	for _, mode := range []PermissionMode{PermissionModeNormal, PermissionModeSandbox} {
		t.Run(string(mode), func(t *testing.T) {
			pm := NewPermissionManager()
			pm.SetMode(mode)
			dec := pm.Decide("browser", json.RawMessage(browserBoxStartArgs))
			if dec.Level != PermissionAsk {
				t.Fatalf("start in %s: level = %s, want Ask", mode, dec.Level)
			}
			if dec.Request == nil || dec.Request.Rule != BrowserBoxStartRule {
				t.Fatalf("start in %s: request rule = %+v, want %q", mode, dec.Request, BrowserBoxStartRule)
			}
			if !IsBrowserBoxStartRequest(*dec.Request) {
				t.Fatalf("start in %s: IsBrowserBoxStartRequest = false for its own request", mode)
			}
			if !strings.Contains(dec.Request.Command, "browser box") {
				t.Fatalf("start in %s: dialog summary = %q, want it to name the box", mode, dec.Request.Command)
			}
		})
	}
}

// TestPermissions_BoxStartLockedDeniesYoloAllows pins the two modes that bracket the ask.
func TestPermissions_BoxStartLockedDeniesYoloAllows(t *testing.T) {
	pm := NewPermissionManager()
	pm.SetMode(PermissionModeLocked)
	if dec := pm.Decide("browser", json.RawMessage(browserBoxStartArgs)); dec.Level != PermissionDeny {
		t.Fatalf("start in locked: level = %s, want Deny", dec.Level)
	}
	pm.SetMode(PermissionModeYOLO)
	if dec := pm.Decide("browser", json.RawMessage(browserBoxStartArgs)); dec.Level != PermissionAllow {
		t.Fatalf("start in yolo: level = %s, want Allow", dec.Level)
	}
}

// TestPermissions_BoxStartIgnoresAllowRules pins that no allow rule turns start into
// an Allow: not a tool rule, not a wildcard pattern, not a user-confirmed rule. A
// deny rule still denies it, as a hard deny.
func TestPermissions_BoxStartIgnoresAllowRules(t *testing.T) {
	args := json.RawMessage(browserBoxStartArgs)
	allowers := map[string]func(*PermissionManager){
		"tool rule allow":        func(pm *PermissionManager) { pm.SetRule("browser", PermissionAllow) },
		"user confirmed allow":   func(pm *PermissionManager) { pm.SetUserConfirmedRule("browser", PermissionAllow) },
		"wildcard pattern allow": func(pm *PermissionManager) { pm.SetRule("*", PermissionAllow) },
	}
	for name, setup := range allowers {
		t.Run(name, func(t *testing.T) {
			pm := NewPermissionManager()
			setup(pm)
			dec := pm.Decide("browser", args)
			if dec.Level != PermissionAsk {
				t.Fatalf("start with %s: level = %s, want Ask", name, dec.Level)
			}
		})
	}
	t.Run("tool rule deny is hard", func(t *testing.T) {
		pm := NewPermissionManager()
		pm.SetRule("browser", PermissionDeny)
		dec := pm.Decide("browser", args)
		if dec.Level != PermissionDeny || !dec.HardDeny {
			t.Fatalf("start with browser deny: level = %s hard = %v, want a hard Deny", dec.Level, dec.HardDeny)
		}
	})
}

// TestPermissions_BoxStartOffersNoAlwaysChoice pins that a box start can never
// persist a rule: neither the always-rule nor the always-tool choice is offered,
// because a persisted browser allow would cover every other browser action.
func TestPermissions_BoxStartOffersNoAlwaysChoice(t *testing.T) {
	pm := NewPermissionManager()
	dec := pm.Decide("browser", json.RawMessage(browserBoxStartArgs))
	if dec.Request == nil {
		t.Fatal("start raised no request")
	}
	if AlwaysRuleChoiceAvailable(*dec.Request) {
		t.Error("AlwaysRuleChoiceAvailable(box start) = true; want false")
	}
	if AlwaysToolChoiceAvailable(*dec.Request) {
		t.Error("AlwaysToolChoiceAvailable(box start) = true; want false")
	}
	// The other browser asks keep both choices: the withholding is for this category only.
	click := pm.Decide("browser", json.RawMessage(`{"action":"click","x":1,"y":2}`))
	if click.Request == nil || !AlwaysRuleChoiceAvailable(*click.Request) {
		t.Error("a browser click lost its always-rule choice; only the box start category may withhold it")
	}
}

// TestPermissions_RetainedBoxStartIsVisiblyDifferent pins that a retained start asks
// under the same human-only category as a plain start, but its request says it is a
// retained box, which outlives the session, so the approver is not misled.
func TestPermissions_RetainedBoxStartIsVisiblyDifferent(t *testing.T) {
	pm := NewPermissionManager()
	plain := pm.Decide("browser", json.RawMessage(browserBoxStartArgs))
	retained := pm.Decide("browser", json.RawMessage(`{"action":"start","backend":"upstash","retain":true}`))
	if retained.Level != PermissionAsk || retained.Request == nil {
		t.Fatalf("retained start: level = %s, request = %+v; want an ask", retained.Level, retained.Request)
	}
	if retained.Request.Rule != BrowserBoxStartRule || !IsBrowserBoxStartRequest(*retained.Request) {
		t.Fatalf("retained start: rule = %q, want the box start category %q", retained.Request.Rule, BrowserBoxStartRule)
	}
	want := "start a RETAINED Upstash browser box (kept between sessions, paused when idle)"
	if retained.Request.Command != want {
		t.Fatalf("retained start summary = %q, want %q", retained.Request.Command, want)
	}
	if plain.Request == nil || plain.Request.Command == retained.Request.Command {
		t.Fatalf("a plain start and a retained start share the summary %q", retained.Request.Command)
	}
	if AlwaysRuleChoiceAvailable(*retained.Request) || AlwaysToolChoiceAvailable(*retained.Request) {
		t.Error("a retained start offers an always-allow choice; the category must withhold it")
	}
}

// TestPermissions_BoxTerminateNeedsNoAsk pins that terminate, which only stops billing,
// is allowed in normal, sandbox and yolo modes, and is denied where a browser deny
// rule or locked mode applies.
func TestPermissions_BoxTerminateNeedsNoAsk(t *testing.T) {
	args := json.RawMessage(browserBoxTerminateArgs)
	for _, mode := range []PermissionMode{PermissionModeNormal, PermissionModeSandbox, PermissionModeYOLO} {
		pm := NewPermissionManager()
		pm.SetMode(mode)
		if dec := pm.Decide("browser", args); dec.Level != PermissionAllow {
			t.Fatalf("terminate in %s: level = %s, want Allow", mode, dec.Level)
		}
	}
	locked := NewPermissionManager()
	locked.SetMode(PermissionModeLocked)
	if dec := locked.Decide("browser", args); dec.Level != PermissionDeny {
		t.Fatalf("terminate in locked: level = %s, want Deny", dec.Level)
	}
	denied := NewPermissionManager()
	denied.SetRule("browser", PermissionDeny)
	if dec := denied.Decide("browser", args); dec.Level != PermissionDeny {
		t.Fatalf("terminate with browser deny: level = %s, want Deny", dec.Level)
	}
}

// TestBoxStartNeverReachesTheJudge runs the agent's own tool-call path with the
// auto-permission judge on and a judge that would allow anything. A click reaches the
// judge (the positive control); a box start must not, and must stay a pending human ask.
func TestBoxStartNeverReachesTheJudge(t *testing.T) {
	cfg := &config.Config{}
	cfg.Ocode.Permissions.Auto = &config.AutoPermissionConfig{Enabled: true, Model: "mock/model"}
	a := NewAgent(nil, nil, cfg, nil)
	a.Permissions().SetAutoPermissionEnabled(true)
	a.AddTools([]tool.Tool{newBrFakeBrowser(t)})

	var judged atomic.Int32
	prev := newClientFn
	t.Cleanup(func() { newClientFn = prev })
	newClientFn = func(_ *config.Config, _ string) LLMClient {
		judged.Add(1)
		return &MockClient{Response: &Message{Role: "assistant", Content: "ALLOW: go ahead"}}
	}

	// Positive control: an ordinary browser action does go to the judge, which allows it.
	res, err := a.HandleToolCall("browser", json.RawMessage(`{"action":"click","x":1,"y":2}`))
	if err != nil {
		t.Fatalf("click HandleToolCall: %v", err)
	}
	if judged.Load() == 0 {
		t.Fatalf("positive control: the judge was never consulted for a click (result %q); the harness proves nothing", res)
	}

	judged.Store(0)
	res, err = a.HandleToolCall("browser", json.RawMessage(browserBoxStartArgs))
	if err != nil {
		t.Fatalf("start HandleToolCall: %v", err)
	}
	if n := judged.Load(); n != 0 {
		t.Fatalf("the judge was consulted %d time(s) for a box start; it must never be", n)
	}
	if !strings.HasPrefix(res, tool.SentinelPermissionAsk) {
		t.Fatalf("box start without a human asker = %q, want a pending human ask", res)
	}
	var req PermissionRequest
	if err := json.Unmarshal([]byte(strings.TrimPrefix(res, tool.SentinelPermissionAsk)), &req); err != nil {
		t.Fatalf("unmarshal ask payload: %v", err)
	}
	if req.Rule != BrowserBoxStartRule {
		t.Fatalf("pending ask rule = %q, want %q", req.Rule, BrowserBoxStartRule)
	}
}

// TestPermissions_UpstashURLRefusedBeforeAnyAsk pins that the box backend's stricter
// address list is a hard deny in Decide, so a loopback URL on upstash raises no ask.
// The same URL on the local backend keeps its existing behaviour: loopback is allowed.
func TestPermissions_UpstashURLRefusedBeforeAnyAsk(t *testing.T) {
	pm := NewPermissionManager()
	for _, action := range []string{"navigate", "open"} {
		upstash := pm.Decide("browser", json.RawMessage(fmt.Sprintf(`{"action":%q,"url":"http://127.0.0.1:9222/","backend":"upstash"}`, action)))
		if upstash.Level != PermissionDeny || !upstash.HardDeny {
			t.Errorf("%s on upstash to loopback: level = %s hard = %v, want a hard Deny", action, upstash.Level, upstash.HardDeny)
		}
		local := pm.Decide("browser", json.RawMessage(fmt.Sprintf(`{"action":%q,"url":"http://127.0.0.1:9222/"}`, action)))
		if local.Level == PermissionDeny && local.HardDeny {
			t.Errorf("%s on local to loopback is hard-denied; the local behaviour must not change", action)
		}
	}
	public := pm.Decide("browser", json.RawMessage(`{"action":"navigate","url":"https://example.com/","backend":"upstash"}`))
	if public.Level == PermissionDeny {
		t.Errorf("navigate on upstash to a public host = %s; want it not denied at the static layer", public.Level)
	}
}

// TestBrowserRunBackendThreadsIntoEverySubStep pins that with the session's box
// backend on, browser_run passes backend to every sub-step it issues, and never
// issues start itself.
func TestBrowserRunBackendThreadsIntoEverySubStep(t *testing.T) {
	br := newBrFakeBrowser(t, brShopPage("before"), brShopPage("after"))
	jev := &brFakeJev{t: t, replies: []map[string]any{brActStep("click", "@e2", 0.9), brDoneReply()}}
	ag := newBrowserRunAgent(t, br, jev)
	brYolo(ag)
	ag.SetBrowserBoxBackend(true)

	out := runBrowserRun(t, ag, `{"goal":"`+brGoal+`","backend":"upstash"}`)
	brAssertContains(t, out, "browser_run: status=done")

	if len(br.calls) == 0 {
		t.Fatal("no sub-step reached the browser")
	}
	for _, c := range br.calls {
		if c.args["backend"] != "upstash" {
			t.Errorf("sub-step %q carries backend %v; want upstash", c.action, c.args["backend"])
		}
		if c.action == "start" || c.action == "terminate" {
			t.Errorf("browser_run issued %q; it must never start or stop a box", c.action)
		}
	}
}

// TestBrowserRunBackendNeedsTheSessionFlag pins that backend is refused, not ignored,
// when the session does not have the box backend.
func TestBrowserRunBackendNeedsTheSessionFlag(t *testing.T) {
	ag := newBrowserRunAgent(t, newBrFakeBrowser(t), nil)
	brYolo(ag)
	tl := ag.tools[browserRunToolName].(*browserRunTool)
	_, err := tl.ExecuteCtx(t.Context(), json.RawMessage(`{"goal":"`+brGoal+`","backend":"upstash"}`))
	if err == nil || !strings.Contains(err.Error(), "backend is not available in this session") {
		t.Fatalf("browser_run backend without the flag: err = %v, want the not-available refusal", err)
	}
}

// browserRunDefinitionSHA is the sha256 of json.Marshal(browser_run's Definition) as
// it stood before the box backend existed. With the flag off the definition must
// still hash to it: the tools array is part of the prompt cache.
const browserRunDefinitionSHA = "e02369bbf9e6b86e8da4c06d9a09a1a4fc278755805068cd8ea9254705702daf"

// TestBrowserRunDefinitionByteIdenticalWithoutBoxBackend pins the flag-off definition
// to its pre-change bytes, and checks the flag-on definition adds only backend.
func TestBrowserRunDefinitionByteIdenticalWithoutBoxBackend(t *testing.T) {
	ag := &Agent{}
	off, err := json.Marshal((&browserRunTool{owner: ag}).Definition())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(off)); got != browserRunDefinitionSHA {
		t.Fatalf("flag-off browser_run definition changed: sha256 %s, want %s", got, browserRunDefinitionSHA)
	}
	if strings.Contains(string(off), `"backend"`) {
		t.Fatal("flag-off browser_run definition names backend")
	}

	ag.SetBrowserBoxBackend(true)
	on1, _ := json.Marshal((&browserRunTool{owner: ag}).Definition())
	on2, _ := json.Marshal((&browserRunTool{owner: ag}).Definition())
	if string(on1) != string(on2) {
		t.Fatal("flag-on browser_run definition is not deterministic")
	}
	if !strings.Contains(string(on1), `"backend"`) || !strings.Contains(string(on1), `"enum":["local","upstash"]`) {
		t.Fatalf("flag-on definition lacks the backend property: %s", on1)
	}
	if strings.Contains(string(on1), `"start"`) {
		t.Fatal("browser_run's definition names the start action; only the browser tool offers it")
	}
}

// TestBoxStartAlwaysAllowPersistsNothing pins that an allow-always answer to a box
// start, from a host that does not check the always-choice gate (the ACP bridge),
// installs no rule: the browser tool stays an ask, and start still asks.
func TestBoxStartAlwaysAllowPersistsNothing(t *testing.T) {
	a := NewAgent(nil, nil, &config.Config{}, nil)
	dec := a.permissions.Decide("browser", json.RawMessage(browserBoxStartArgs))
	if dec.Request == nil {
		t.Fatal("start raised no request")
	}
	for _, resp := range []PermissionResponse{
		{Level: PermissionAllow, PersistRule: true},
		{Level: PermissionAllow, PersistTool: true},
	} {
		a.applyPermissionResponse(*dec.Request, resp)
	}
	if got := a.permissions.Check("browser"); got != PermissionAsk {
		t.Fatalf("browser tool rule after a box-start always-allow = %s, want Ask", got)
	}
	if again := a.permissions.Decide("browser", json.RawMessage(browserBoxStartArgs)); again.Level != PermissionAsk {
		t.Fatalf("start after a box-start always-allow = %s, want Ask", again.Level)
	}
}

// TestPermissions_BoxStartNamesTheAssignedLoginState pins that the human
// approving a start sees which saved identity the box will restore. The server
// sets the resolver from the session's project assignment; with none, the
// summary must not invent one.
func TestPermissions_BoxStartNamesTheAssignedLoginState(t *testing.T) {
	pm := NewPermissionManager()
	dec := pm.Decide("browser", json.RawMessage(browserBoxStartArgs))
	if dec.Request == nil {
		t.Fatal("start raised no request")
	}
	if strings.Contains(dec.Request.Command, "login state") {
		t.Fatalf("summary %q names a login state with none assigned", dec.Request.Command)
	}

	pm.SetBrowserBoxLoginStateDetail(func() string { return "bank" })
	dec = pm.Decide("browser", json.RawMessage(browserBoxStartArgs))
	if dec.Request == nil {
		t.Fatal("start with a login state raised no request")
	}
	if want := `login state "bank" is restored`; !strings.Contains(dec.Request.Command, want) {
		t.Fatalf("summary = %q, want it to contain %q", dec.Request.Command, want)
	}

	dec = pm.Decide("browser", json.RawMessage(`{"action":"start","backend":"upstash","retain":true}`))
	if dec.Request == nil {
		t.Fatal("retained start raised no request")
	}
	if !strings.Contains(dec.Request.Command, "RETAINED") || !strings.Contains(dec.Request.Command, `login state "bank"`) {
		t.Fatalf("retained summary = %q, want RETAINED and the login state", dec.Request.Command)
	}
}
