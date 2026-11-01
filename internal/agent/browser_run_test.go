package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

const brGoal = "add the item to the cart"

// brDeniedMsg is what the browser tool returns when the user denies a call.
const brDeniedMsg = `denied: tool "browser" denied by user. Do not retry the same call.`

// brFakeBrowser is a scripted browser tool named "browser". Controls reads get the
// pages in order, and the last page repeats. Every other action is recorded and
// answered "ok", except denyAct, which gets the permission-denied message.
type brFakeBrowser struct {
	t          *testing.T
	pages      []brPage
	denyAct    string
	staleRef   string // a click on this ref fails as a stale ref, as the browser tool reports it
	userTab    bool
	onControls func(n int) // runs before the nth controls read is answered (1-based)

	mu       sync.Mutex
	calls    []brCall
	controls int
}

type brCall struct {
	action string
	args   map[string]any
}

var (
	_ tool.PageContextTool = (*brFakeBrowser)(nil)
	_ tool.Tool            = (*brFakeBrowser)(nil)
)

func newBrFakeBrowser(t *testing.T, pages ...brPage) *brFakeBrowser {
	return &brFakeBrowser{t: t, pages: pages}
}

func (b *brFakeBrowser) Name() string        { return "browser" }
func (b *brFakeBrowser) Description() string { return "scripted browser for browser_run tests" }
func (b *brFakeBrowser) Parallel() bool      { return false }
func (b *brFakeBrowser) Definition() map[string]interface{} {
	return map[string]interface{}{"name": "browser"}
}

// PageContext describes the page the way the real tool does. The reads that ask
// on a user tab depend on opened_by_agent, so userTab flips it.
func (b *brFakeBrowser) PageContext(json.RawMessage) (map[string]any, bool) {
	if b.userTab {
		return map[string]any{"targets_page": true, "tab_id": "t1", "opened_by_agent": false, "page_url": "https://bank.example/", "page_host": "bank.example"}, true
	}
	return map[string]any{"targets_page": true, "tab_id": "t1", "opened_by_agent": true, "page_url": "https://shop.example/", "page_host": "shop.example"}, true
}

func (b *brFakeBrowser) Execute(args json.RawMessage) (string, error) {
	var m map[string]any
	if err := json.Unmarshal(args, &m); err != nil {
		return "", fmt.Errorf("fake browser: %w", err)
	}
	action, _ := m["action"].(string)
	b.mu.Lock()
	b.calls = append(b.calls, brCall{action: action, args: m})
	if action != "controls" {
		denied := action == b.denyAct
		stale := action == "click" && b.staleRef != "" && m["target"] == b.staleRef
		b.mu.Unlock()
		if stale {
			return "", errors.New("stale ref " + b.staleRef + ": the element is gone, take a new snapshot")
		}
		if denied {
			return brDeniedMsg, nil
		}
		return "ok: " + action, nil
	}
	b.controls++
	n := b.controls
	b.mu.Unlock()

	if b.onControls != nil {
		b.onControls(n)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.pages) == 0 {
		return "", errors.New("fake browser: no controls page scripted")
	}
	return brControlsResult(b.t, b.pages[min(n-1, len(b.pages)-1)]), nil
}

// callsFor returns the recorded arguments of every call with this action, in order.
func (b *brFakeBrowser) callsFor(action string) []map[string]any {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []map[string]any
	for _, c := range b.calls {
		if c.action == action {
			out = append(out, c.args)
		}
	}
	return out
}

// brPage is one controls read.
type brPage struct {
	fingerprint string
	text        string
	controls    []browserRunControl
	// unsettled and textTruncated mirror the browser tool's own flags. The zero
	// value is the settled, unclipped page.
	unsettled     bool
	textTruncated bool
}

func brShopPage(fingerprint string) brPage {
	return brPage{
		fingerprint: fingerprint,
		text:        "Your cart is empty.",
		controls: []browserRunControl{
			{Ref: "@e1", Role: "textbox", Label: "Email", States: []string{}, Operations: []string{"fill"}},
			{Ref: "@e2", Role: "button", Label: "Add to cart", States: []string{}, Operations: []string{"click"}},
		},
	}
}

// brControlsResult renders a page the way the controls action does: a header line
// ending in a colon, then one JSON object.
func brControlsResult(t *testing.T, p brPage) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"url":            "https://shop.example/cart",
		"title":          "Cart",
		"fingerprint":    p.fingerprint,
		"settled":        !p.unsettled,
		"text":           p.text,
		"text_truncated": p.textTruncated,
		"controls":       p.controls,
		"omitted":        0,
	})
	if err != nil {
		t.Errorf("marshal controls: %v", err)
		return ""
	}
	return fmt.Sprintf("controls for tab t1 (%d controls):\n%s", len(p.controls), body)
}

// brFakeJev is a TypeSafe /systemone stand-in. Each decision call gets the next
// scripted answer set. A call past the script fails the test.
type brFakeJev struct {
	t       *testing.T
	replies []map[string]any

	mu     sync.Mutex
	bodies []string
}

// decisionCalls reports how many decision requests the fake judge has received.
func (j *brFakeJev) decisionCalls() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return len(j.bodies)
}

func (j *brFakeJev) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		j.t.Errorf("read decision request: %v", err)
		http.Error(w, "read failed", http.StatusBadRequest)
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.bodies = append(j.bodies, string(raw))
	i := len(j.bodies) - 1
	if i >= len(j.replies) {
		j.t.Errorf("decision call %d was not scripted", i+1)
		http.Error(w, "unscripted", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"model":   "jev-latest",
		"answers": j.replies[i],
		"usage":   map[string]any{"input_tokens": 100, "output_tokens": 10},
	})
}

func (j *brFakeJev) requestCount() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return len(j.bodies)
}

func (j *brFakeJev) request(i int) string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.bodies[i]
}

// decodeState returns the state object of decision request i.
func (j *brFakeJev) decodeState(t *testing.T, i int) map[string]any {
	t.Helper()
	var body struct {
		State map[string]any `json:"state"`
	}
	if err := json.Unmarshal([]byte(j.request(i)), &body); err != nil {
		t.Fatalf("decode decision request %d: %v", i+1, err)
	}
	return body.State
}

func brDoneAnswer(noul float64) map[string]any {
	return map[string]any{"type": "noul", "noul": noul, "confidence": 0.9}
}

// brChoiceAnswer is a choice answer whose chosen option has probability 0.9.
func brChoiceAnswer(choice string, confidence float64) map[string]any {
	return brChoiceWithProbability(choice, 0.9, confidence)
}

// brChoiceWithProbability is a choice answer whose chosen option has the given
// probability, with the rest of the mass on one other option.
func brChoiceWithProbability(choice string, probability, confidence float64) map[string]any {
	return map[string]any{
		"type":          "choice",
		"choice":        choice,
		"confidence":    confidence,
		"probabilities": map[string]any{choice: probability, "other": 1 - probability},
	}
}

// brActStep is a decision that is not done, picks op, and picks target for it.
func brActStep(op, target string, opConfidence float64) map[string]any {
	return map[string]any{
		"done":         brDoneAnswer(0.1),
		"operation":    brChoiceAnswer(op, opConfidence),
		"target_" + op: brChoiceAnswer(target, 0.9),
	}
}

// brValueReply is the reply to the value request that follows a fill step: the
// data key chosen for the field, at probability 0.9.
func brValueReply(key string) map[string]any {
	return map[string]any{brValueKey: brChoiceAnswer(key, 0.9)}
}

func brDoneReply() map[string]any {
	return map[string]any{"done": brDoneAnswer(0.95)}
}

// newBrowserRunAgent builds an agent with br as its browser tool (none when br is
// nil). The browser_run judge is served by jev (no judge when jev is nil). The
// content guard is pointed at a model no factory serves, so the only decision
// traffic is the browser_run judge's.
func newBrowserRunAgent(t *testing.T, br tool.Tool, jev *brFakeJev) *Agent {
	t.Helper()
	cfg := &config.Config{}
	cfg.Ocode.ContentGuard.JudgeModel = "typesafe/content-guard-off"
	var tools []tool.Tool
	if br != nil {
		tools = []tool.Tool{br}
	}
	ag := newTestAgent(nil, tools, cfg, nil)

	srvURL := ""
	if jev != nil {
		srv := httptest.NewServer(jev)
		t.Cleanup(srv.Close)
		srvURL = srv.URL
	}
	prev := newClientFn
	t.Cleanup(func() { newClientFn = prev })
	newClientFn = func(_ *config.Config, model string) LLMClient {
		if jev == nil || model != defaultJudgeModel {
			return nil
		}
		return newTypesafeClient("k", "jev-latest", srvURL)
	}
	return ag
}

// runBrowserRun calls browser_run through the agent's own tool-call path, as the
// main model does, and returns the tool result.
func runBrowserRun(t *testing.T, ag *Agent, args string) string {
	t.Helper()
	out, err := ag.handleToolCall(browserRunToolName, json.RawMessage(args), nil, "")
	if err != nil {
		t.Fatalf("browser_run: %v", err)
	}
	return out
}

func brYolo(ag *Agent) {
	ag.permissions.SetMode(PermissionModeYOLO)
}

func brNormal(ag *Agent) {
	ag.permissions.SetMode(PermissionModeNormal)
}

func brAssertContains(t *testing.T, out string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Fatalf("result lacks %q:\n%s", want, out)
		}
	}
}

// TestBrowserRunHappyPathTwoSteps covers a click, a page change, then done.
func TestBrowserRunHappyPathTwoSteps(t *testing.T) {
	br := newBrFakeBrowser(t, brShopPage("before"), brShopPage("after"))
	jev := &brFakeJev{t: t, replies: []map[string]any{brActStep("click", "@e2", 0.9), brDoneReply()}}
	ag := newBrowserRunAgent(t, br, jev)
	brYolo(ag)

	out := runBrowserRun(t, ag, `{"goal":"`+brGoal+`"}`)

	if !strings.HasPrefix(out, browserRunUntrustedNotice) {
		t.Fatalf("result lacks the untrusted-content notice first:\n%s", out)
	}
	brAssertContains(t, out,
		"browser_run: status=done steps=1 jev_calls=2 page=https://shop.example/cart \"Cart\"",
		`1. click @e2 button "Add to cart" → page changed`,
	)
	if n := br.callsFor("controls"); len(n) != 2 {
		t.Fatalf("controls reads = %d, want 2 (before and after the click)", len(n))
	}
	clicks := br.callsFor("click")
	if len(clicks) != 1 {
		t.Fatalf("click calls = %d, want 1", len(clicks))
	}
	if clicks[0]["target"] != "@e2" {
		t.Fatalf("click target = %v, want @e2", clicks[0]["target"])
	}
	rs, ok := clicks[0]["run_step"].(map[string]any)
	if !ok {
		t.Fatalf("click carries no run_step object: %v", clicks[0])
	}
	if rs["n"] != float64(1) || rs["of"] != float64(browserRunDefaultMaxSteps) || rs["label"] != "Add to cart" || rs["goal"] != brGoal {
		t.Fatalf("run_step = %v, want n=1 of=%d label=%q goal=%q", rs, browserRunDefaultMaxSteps, "Add to cart", brGoal)
	}
}

// TestBrowserRunFillSendsTheDataValueButNotToTheJudge checks that the value reaches
// the browser and that only the key name reaches the decision model.
func TestBrowserRunFillSendsTheDataValueButNotToTheJudge(t *testing.T) {
	const secret = "jane.doe@secret.example"
	// The controls read after the fill reports the field's current text, which is
	// exactly how a typed value could reach the judge. The state must not carry it.
	after := brShopPage("after")
	after.controls[0].Value = secret
	br := newBrFakeBrowser(t, brShopPage("before"), after)
	jev := &brFakeJev{t: t, replies: []map[string]any{brActStep("fill", "@e1", 0.9), brValueReply("email"), brDoneReply()}}
	ag := newBrowserRunAgent(t, br, jev)
	brYolo(ag)

	out := runBrowserRun(t, ag, `{"goal":"`+brGoal+`","data":{"email":"`+secret+`"}}`)

	brAssertContains(t, out, "status=done", "jev_calls=3", `1. fill @e1 textbox "Email" ← data.email → page changed`)
	if strings.Contains(jev.request(0), brValueKey) {
		t.Fatal("the step request asks the value question; it must come in its own request")
	}
	valueReq := jev.request(1)
	for _, want := range []string{"Email", `"email"`, "the value of data key email"} {
		if !strings.Contains(valueReq, want) {
			t.Fatalf("value request does not contain %q", want)
		}
	}
	fills := br.callsFor("fill")
	if len(fills) != 1 {
		t.Fatalf("fill calls = %d, want 1", len(fills))
	}
	if fills[0]["value"] != secret {
		t.Fatalf("fill value = %v, want the data value", fills[0]["value"])
	}
	if rs, ok := fills[0]["run_step"].(map[string]any); !ok || rs["n"] != float64(1) {
		t.Fatalf("fill run_step = %v, want n=1", fills[0]["run_step"])
	}
	if !strings.Contains(jev.request(0), `"email"`) {
		t.Fatalf("decision request does not name the data key email")
	}
	for i := 0; i < jev.requestCount(); i++ {
		if strings.Contains(jev.request(i), secret) {
			t.Fatalf("decision request %d carries the data value", i+1)
		}
	}
}

// TestBrowserRunNeedsInputWhenNoDataKeyFits sends no mutation when no key fits.
func TestBrowserRunNeedsInputWhenNoDataKeyFits(t *testing.T) {
	br := newBrFakeBrowser(t, brShopPage("a"))
	jev := &brFakeJev{t: t, replies: []map[string]any{brActStep("fill", "@e1", 0.9), brValueReply(browserRunNoneChoice)}}
	ag := newBrowserRunAgent(t, br, jev)
	brYolo(ag)

	out := runBrowserRun(t, ag, `{"goal":"`+brGoal+`","data":{"email":"x@example.com"}}`)

	brAssertContains(t, out, "status=needs_input", "jev_calls=2", "next:")
	if n := len(br.callsFor("fill")); n != 0 {
		t.Fatalf("fill calls = %d, want none", n)
	}
}

// TestBrowserRunUnsureBelowConfidenceFloor sends no mutation under the floor.
func TestBrowserRunUnsureBelowConfidenceFloor(t *testing.T) {
	br := newBrFakeBrowser(t, brShopPage("a"))
	jev := &brFakeJev{t: t, replies: []map[string]any{brActStep("click", "@e2", 0.4)}}
	ag := newBrowserRunAgent(t, br, jev)
	brYolo(ag)

	out := runBrowserRun(t, ag, `{"goal":"`+brGoal+`"}`)

	brAssertContains(t, out, "status=unsure", "reason: operation confidence 0.40 is below the 0.55 floor", "next:", "@e2 button")
	if n := len(br.callsFor("click")); n != 0 {
		t.Fatalf("click calls = %d, want none", n)
	}
}

// TestBrowserRunTargetBelowProbabilityFloorIsUnsure checks that a target whose chosen
// control has probability 0.5 is not acted on, even with a high confidence.
func TestBrowserRunTargetBelowProbabilityFloorIsUnsure(t *testing.T) {
	br := newBrFakeBrowser(t, brShopPage("a"))
	step := brActStep("click", "@e2", 0.9)
	step["target_click"] = brChoiceWithProbability("@e2", 0.5, 0.9)
	jev := &brFakeJev{t: t, replies: []map[string]any{step}}
	ag := newBrowserRunAgent(t, br, jev)
	brYolo(ag)

	out := runBrowserRun(t, ag, `{"goal":"`+brGoal+`"}`)

	brAssertContains(t, out, "status=unsure", "reason: target probability 0.50 is below the 0.60 floor", "next:", "@e2 button")
	if n := len(br.callsFor("click")); n != 0 {
		t.Fatalf("click calls = %d, want none", n)
	}
}

// TestBrowserRunValueGatesOnProbabilityNotConfidence checks that a value whose chosen
// key has probability 0.9 is typed even when its confidence is only 0.3.
func TestBrowserRunValueGatesOnProbabilityNotConfidence(t *testing.T) {
	const secret = "jordan@example.com"
	br := newBrFakeBrowser(t, brShopPage("before"), brShopPage("after"))
	valueAnswer := map[string]any{brValueKey: brChoiceWithProbability("email", 0.9, 0.3)}
	jev := &brFakeJev{t: t, replies: []map[string]any{brActStep("fill", "@e1", 0.9), valueAnswer, brDoneReply()}}
	ag := newBrowserRunAgent(t, br, jev)
	brYolo(ag)

	out := runBrowserRun(t, ag, `{"goal":"`+brGoal+`","data":{"email":"`+secret+`"}}`)

	brAssertContains(t, out, "status=done", `1. fill @e1 textbox "Email" ← data.email → page changed`)
	fills := br.callsFor("fill")
	if len(fills) != 1 || fills[0]["value"] != secret {
		t.Fatalf("fill calls = %v, want one fill with the data value", fills)
	}
}

// TestBrowserRunRejectsAnOperationThatWasNotOffered treats an unoffered choice as unsure.
func TestBrowserRunRejectsAnOperationThatWasNotOffered(t *testing.T) {
	br := newBrFakeBrowser(t, brShopPage("a"))
	jev := &brFakeJev{t: t, replies: []map[string]any{brActStep("uncheck", "@e2", 0.9)}}
	ag := newBrowserRunAgent(t, br, jev)
	brYolo(ag)

	out := runBrowserRun(t, ag, `{"goal":"`+brGoal+`"}`)

	brAssertContains(t, out, "status=unsure", "reason: operation answer rejected:")
	if n := len(br.callsFor("click")) + len(br.callsFor("uncheck")); n != 0 {
		t.Fatalf("a mutation ran for a rejected operation: %d calls", n)
	}
}

// TestBrowserRunStopsWhenTheBrowserDenies stops after the denied step.
func TestBrowserRunStopsWhenTheBrowserDenies(t *testing.T) {
	br := newBrFakeBrowser(t, brShopPage("a"))
	br.denyAct = "click"
	jev := &brFakeJev{t: t, replies: []map[string]any{brActStep("click", "@e2", 0.9), brActStep("click", "@e2", 0.9)}}
	ag := newBrowserRunAgent(t, br, jev)
	brYolo(ag)

	out := runBrowserRun(t, ag, `{"goal":"`+brGoal+`"}`)

	brAssertContains(t, out, "browser_run: status=denied steps=1 jev_calls=1", `reason: denied: tool "browser"`, `1. click @e2 button "Add to cart" → denied`)
	if n := jev.requestCount(); n != 1 {
		t.Fatalf("decision calls = %d, want 1: the loop must stop at the denial", n)
	}
}

// TestBrowserRunStallsAfterThreeUnchangedSteps stops on the third unchanged fingerprint.
func TestBrowserRunStallsAfterThreeUnchangedSteps(t *testing.T) {
	br := newBrFakeBrowser(t, brShopPage("same"))
	step := brActStep("click", "@e2", 0.9)
	jev := &brFakeJev{t: t, replies: []map[string]any{step, step, step}}
	ag := newBrowserRunAgent(t, br, jev)
	brYolo(ag)

	out := runBrowserRun(t, ag, `{"goal":"`+brGoal+`"}`)

	brAssertContains(t, out, "browser_run: status=stalled steps=3 jev_calls=3", "reason: page fingerprint unchanged for 3 steps")
	if n := len(br.callsFor("click")); n != 3 {
		t.Fatalf("click calls = %d, want 3", n)
	}
}

// TestBrowserRunStopsAtMaxSteps stops at the step limit, before a stall can fire.
func TestBrowserRunStopsAtMaxSteps(t *testing.T) {
	br := newBrFakeBrowser(t, brShopPage("same"))
	step := brActStep("click", "@e2", 0.9)
	jev := &brFakeJev{t: t, replies: []map[string]any{step, step, step}}
	ag := newBrowserRunAgent(t, br, jev)
	brYolo(ag)

	out := runBrowserRun(t, ag, `{"goal":"`+brGoal+`","max_steps":2}`)

	brAssertContains(t, out, "browser_run: status=max_steps steps=2", "reason: reached the 2-step limit")
	if n := len(br.callsFor("click")); n != 2 {
		t.Fatalf("click calls = %d, want 2", n)
	}
	if n := jev.decisionCalls(); n != 2 {
		t.Fatalf("decision calls = %d, want exactly max_steps (2)", n)
	}
}

// TestBrowserRunTrimsOversizedPageTextAndSignalsIt checks the clipped text and the
// projection marker the judge needs to know its view was clipped.
func TestBrowserRunTrimsOversizedPageTextAndSignalsIt(t *testing.T) {
	page := brShopPage("big")
	page.text = strings.Repeat("lorem ipsum ", 3500)
	br := newBrFakeBrowser(t, page)
	jev := &brFakeJev{t: t, replies: []map[string]any{brDoneReply()}}
	ag := newBrowserRunAgent(t, br, jev)
	brYolo(ag)

	out := runBrowserRun(t, ag, `{"goal":"`+brGoal+`"}`)
	brAssertContains(t, out, "status=done")

	state := jev.decodeState(t, 0)
	pg, ok := state["page"].(map[string]any)
	if !ok {
		t.Fatalf("state carries no page object: %v", state["page"])
	}
	text, _ := pg["text"].(string)
	before, _, found := strings.Cut(text, "\n... [truncated for the judge")
	if !found {
		t.Fatalf("page text is not marked as clipped")
	}
	if len(before) > browserRunTextBytes {
		t.Fatalf("kept %d bytes of page text, want at most %d", len(before), browserRunTextBytes)
	}
	projection, ok := state[stateProjectionKey].(map[string]any)
	if !ok || projection["applied"] != true {
		t.Fatalf("state lacks a %s marker for the clipped page text: %v", stateProjectionKey, state[stateProjectionKey])
	}
	if fields := brProjectionFields(t, state); !strings.Contains(strings.Join(fields, ","), "page.text") {
		t.Fatalf("_projection fields = %v, want page.text named", fields)
	}
	if elements, _ := state["elements"].([]any); len(elements) != len(page.controls) {
		t.Fatalf("elements = %d, want all %d kept", len(elements), len(page.controls))
	}
}

// TestBrowserRunDropsElementsOverBudgetAndCountsThem checks that over-budget
// elements are dropped from the end and counted, and the state stays in budget.
func TestBrowserRunDropsElementsOverBudgetAndCountsThem(t *testing.T) {
	page := brShopPage("many")
	page.controls = nil
	for i := 0; i < browserRunMaxControls; i++ {
		page.controls = append(page.controls, browserRunControl{
			Ref: fmt.Sprintf("@e%d", i+1), Role: "button", Label: strings.Repeat("x", 400) + fmt.Sprint(i),
			States: []string{}, Operations: []string{"click"},
		})
	}
	br := newBrFakeBrowser(t, page)
	jev := &brFakeJev{t: t, replies: []map[string]any{brDoneReply()}}
	ag := newBrowserRunAgent(t, br, jev)
	brYolo(ag)

	out := runBrowserRun(t, ag, `{"goal":"`+brGoal+`"}`)
	brAssertContains(t, out, "status=done")

	state := jev.decodeState(t, 0)
	elements, _ := state["elements"].([]any)
	omitted, _ := state["omitted_elements"].(float64)
	if omitted <= 0 || len(elements)+int(omitted) != browserRunMaxControls {
		t.Fatalf("elements=%d omitted_elements=%v, want the two to add up to %d with some omitted", len(elements), omitted, browserRunMaxControls)
	}
	size, err := marshalledDecisionStateSize(state)
	if err != nil {
		t.Fatalf("measure state: %v", err)
	}
	if size > browserRunStateBudgetBytes {
		t.Fatalf("sent state is %d bytes, over the %d byte budget", size, browserRunStateBudgetBytes)
	}
	// The kept count must be the largest that fits: one more element overflows the budget.
	nextElem, err := json.Marshal(page.controls[len(elements)])
	if err != nil {
		t.Fatalf("encode next element: %v", err)
	}
	var nextMap map[string]any
	if err := json.Unmarshal(nextElem, &nextMap); err != nil {
		t.Fatalf("decode next element: %v", err)
	}
	grown := make(map[string]any, len(state))
	for k, v := range state {
		grown[k] = v
	}
	grown["elements"] = append(append([]any{}, elements...), nextMap)
	grown["omitted_elements"] = omitted - 1
	grownSize, err := marshalledDecisionStateSize(grown)
	if err != nil {
		t.Fatalf("measure grown state: %v", err)
	}
	if grownSize <= browserRunStateBudgetBytes {
		t.Fatalf("kept %d elements, but %d still fit the budget at %d bytes", len(elements), len(elements)+1, grownSize)
	}
	// The page text was not clipped here, so the marker must name the elements and
	// must not claim page.text was cut.
	fields := strings.Join(brProjectionFields(t, state), ",")
	if !strings.Contains(fields, "elements") {
		t.Fatalf("_projection fields = %q, want elements named", fields)
	}
	if strings.Contains(fields, "page.text") {
		t.Fatalf("_projection fields = %q, want page.text NOT named (it was not clipped)", fields)
	}
}

// TestBrowserRunStaleRefCostsAStepAndDecidesAgain checks that a stale ref does not
// stop the run. The controls are read again, and the next decision uses that fresh
// listing even though the old ref is gone from it. The stale click is never re-sent.
func TestBrowserRunStaleRefCostsAStepAndDecidesAgain(t *testing.T) {
	fresh := brPage{
		fingerprint: "checkout",
		text:        "Checkout",
		controls: []browserRunControl{
			{Ref: "@e3", Role: "button", Label: "Checkout", States: []string{}, Operations: []string{"click"}},
		},
	}
	br := newBrFakeBrowser(t, brShopPage("before"), fresh)
	br.staleRef = "@e2"
	jev := &brFakeJev{t: t, replies: []map[string]any{brActStep("click", "@e2", 0.9), brActStep("click", "@e3", 0.9), brDoneReply()}}
	ag := newBrowserRunAgent(t, br, jev)
	brYolo(ag)

	out := runBrowserRun(t, ag, `{"goal":"`+brGoal+`"}`)

	brAssertContains(t, out,
		"browser_run: status=done steps=2 jev_calls=3",
		`1. click @e2 button "Add to cart" → stale ref`,
		`2. click @e3 button "Checkout" → ok`,
	)
	clicks := br.callsFor("click")
	if len(clicks) != 2 || clicks[0]["target"] != "@e2" || clicks[1]["target"] != "@e3" {
		t.Fatalf("click calls = %v, want @e2 once then @e3", clicks)
	}
}

// TestBrowserRunFillHintsWhenTheFieldAlreadyHoldsAValue checks that a field which
// already holds text is named "(already filled)" to the value question, and that its
// current text never reaches the decision model.
func TestBrowserRunFillHintsWhenTheFieldAlreadyHoldsAValue(t *testing.T) {
	const old = "previous.entry@old.example"
	before := brShopPage("before")
	before.controls[0].Value = old
	br := newBrFakeBrowser(t, before, brShopPage("after"))
	jev := &brFakeJev{t: t, replies: []map[string]any{brActStep("fill", "@e1", 0.9), brValueReply("email"), brDoneReply()}}
	ag := newBrowserRunAgent(t, br, jev)
	brYolo(ag)

	out := runBrowserRun(t, ag, `{"goal":"`+brGoal+`","data":{"email":"new@example.com"}}`)

	brAssertContains(t, out, "status=done")
	if !strings.Contains(jev.request(1), "(already filled)") {
		t.Fatal("value request does not mark the field as already filled")
	}
	for i := 0; i < jev.requestCount(); i++ {
		if strings.Contains(jev.request(i), old) {
			t.Fatalf("decision request %d carries the field's current value", i+1)
		}
	}
}

// brProjectionFields returns the fields named by the state's projection marker.
func brProjectionFields(t *testing.T, state map[string]any) []string {
	t.Helper()
	projection, ok := state[stateProjectionKey].(map[string]any)
	if !ok {
		t.Fatalf("state has no %s marker: %v", stateProjectionKey, state[stateProjectionKey])
	}
	raw, ok := projection["fields"].([]any)
	if !ok {
		t.Fatalf("%s has no fields list: %v", stateProjectionKey, projection["fields"])
	}
	out := make([]string, 0, len(raw))
	for _, f := range raw {
		s, _ := f.(string)
		out = append(out, s)
	}
	return out
}

// TestBrowserRunMarksServerClippedTextAsProjected checks that a page text the
// browser tool already clipped is signalled to the judge even though this loop
// had nothing to trim.
func TestBrowserRunMarksServerClippedTextAsProjected(t *testing.T) {
	page := brShopPage("clipped")
	page.textTruncated = true
	br := newBrFakeBrowser(t, page)
	jev := &brFakeJev{t: t, replies: []map[string]any{brDoneReply()}}
	ag := newBrowserRunAgent(t, br, jev)
	brYolo(ag)

	brAssertContains(t, runBrowserRun(t, ag, `{"goal":"`+brGoal+`"}`), "status=done")
	state := jev.decodeState(t, 0)
	if fields := strings.Join(brProjectionFields(t, state), ","); !strings.Contains(fields, "page.text") {
		t.Fatalf("_projection fields = %q, want page.text named for the server-side clip", fields)
	}
	if elements, _ := state["elements"].([]any); len(elements) != len(page.controls) {
		t.Fatalf("elements = %d, want all %d kept", len(elements), len(page.controls))
	}
}

// TestBrowserRunCarriesUnsettledPageToTheJudge checks that a page that never went
// quiet is reported as such, so the judge can discount a mid-render view.
func TestBrowserRunCarriesUnsettledPageToTheJudge(t *testing.T) {
	page := brShopPage("busy")
	page.unsettled = true
	br := newBrFakeBrowser(t, page)
	jev := &brFakeJev{t: t, replies: []map[string]any{brDoneReply()}}
	ag := newBrowserRunAgent(t, br, jev)
	brYolo(ag)

	brAssertContains(t, runBrowserRun(t, ag, `{"goal":"`+brGoal+`"}`), "status=done")
	state := jev.decodeState(t, 0)
	pg, ok := state["page"].(map[string]any)
	if !ok {
		t.Fatalf("state carries no page object: %v", state["page"])
	}
	if settled, _ := pg["settled"].(bool); settled {
		t.Fatalf("page.settled = true, want false for an unsettled page")
	}
}

// TestBrowserRunPageTooDenseSendsNothingToTheJudge checks that a state which cannot
// fit even after trimming is refused before any decision request.
func TestBrowserRunPageTooDenseSendsNothingToTheJudge(t *testing.T) {
	br := newBrFakeBrowser(t, brShopPage("a"))
	jev := &brFakeJev{t: t}
	ag := newBrowserRunAgent(t, br, jev)
	brYolo(ag)

	out := runBrowserRun(t, ag, `{"goal":"`+strings.Repeat("g", 31_000)+`"}`)

	brAssertContains(t, out, "status=page_too_dense")
	if n := jev.requestCount(); n != 0 {
		t.Fatalf("decision requests = %d, want none", n)
	}
}

// TestBrowserRunWithoutJudgeIsAnError checks the no-judge error and that the page
// is not read.
func TestBrowserRunWithoutJudgeIsAnError(t *testing.T) {
	br := newBrFakeBrowser(t, brShopPage("a"))
	ag := newBrowserRunAgent(t, br, nil)
	brYolo(ag)

	_, err := ag.handleToolCall(browserRunToolName, json.RawMessage(`{"goal":"`+brGoal+`"}`), nil, "")
	if err == nil || !strings.Contains(err.Error(), "no decision judge configured") {
		t.Fatalf("err = %v, want the no-judge error", err)
	}
	if n := len(br.callsFor("controls")) + len(br.callsFor("click")); n != 0 {
		t.Fatalf("browser calls = %d, want none", n)
	}
}

// TestBrowserRunStopsOnCancelledContext checks that a cancelled turn ends the run
// at the next decision, with no further browser calls.
func TestBrowserRunStopsOnCancelledContext(t *testing.T) {
	br := newBrFakeBrowser(t, brShopPage("before"), brShopPage("after"))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	br.onControls = func(n int) {
		if n == 2 {
			cancel()
		}
	}
	jev := &brFakeJev{t: t, replies: []map[string]any{brActStep("click", "@e2", 0.9)}}
	ag := newBrowserRunAgent(t, br, jev)
	brYolo(ag)

	bt, ok := ag.tools[browserRunToolName].(tool.ContextualTool)
	if !ok {
		t.Fatalf("browser_run is not registered as a contextual tool")
	}
	start := time.Now()
	out, err := bt.ExecuteCtx(ctx, json.RawMessage(`{"goal":"`+brGoal+`"}`))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ExecuteCtx error = %v, want one wrapping context.Canceled", err)
	}
	if out != "" {
		t.Fatalf("cancelled run returned a result instead of an error: %q", out)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("cancelled run took %v", elapsed)
	}
	if n := len(br.callsFor("controls")); n != 2 {
		t.Fatalf("controls reads = %d, want 2", n)
	}
	if n := len(br.callsFor("click")); n != 1 {
		t.Fatalf("click calls = %d, want 1", n)
	}
}

// brAskRecorder records the permission requests a sub-agent asker received.
type brAskRecorder struct {
	mu   sync.Mutex
	reqs []PermissionRequest
}

func (r *brAskRecorder) ask(level PermissionLevel) func(PermissionRequest) PermissionResponse {
	return func(req PermissionRequest) PermissionResponse {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.reqs = append(r.reqs, req)
		return PermissionResponse{Level: level}
	}
}

func (r *brAskRecorder) requests() []PermissionRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]PermissionRequest(nil), r.reqs...)
}

// TestBrowserRunHumanAskAllowRunsTheStep checks that a mutating sub-step blocks on
// the host's asker, is stamped with the browser_run name, and runs once allowed.
func TestBrowserRunHumanAskAllowRunsTheStep(t *testing.T) {
	br := newBrFakeBrowser(t, brShopPage("before"), brShopPage("after"))
	jev := &brFakeJev{t: t, replies: []map[string]any{brActStep("click", "@e2", 0.9), brDoneReply()}}
	ag := newBrowserRunAgent(t, br, jev)
	brNormal(ag)
	rec := &brAskRecorder{}
	ag.SetSubAgentPermAsker(rec.ask(PermissionAllow))

	out := runBrowserRun(t, ag, `{"goal":"`+brGoal+`"}`)

	brAssertContains(t, out, "status=done steps=1")
	reqs := rec.requests()
	if len(reqs) != 1 {
		t.Fatalf("asker saw %d requests, want 1 (the click; reads need no ask)", len(reqs))
	}
	if reqs[0].AgentName != browserRunToolName {
		t.Fatalf("asker AgentName = %q, want %q", reqs[0].AgentName, browserRunToolName)
	}
	if !strings.Contains(reqs[0].Command, "browser_run step 1/") {
		t.Fatalf("asker Command = %q, want it to name browser_run step 1/", reqs[0].Command)
	}
	if n := len(br.callsFor("click")); n != 1 {
		t.Fatalf("click calls = %d, want 1 after the allow", n)
	}
}

// TestBrowserRunHumanAskDenyStopsTheRun checks that a denied sub-step ends the run
// without running the click or asking again.
func TestBrowserRunHumanAskDenyStopsTheRun(t *testing.T) {
	br := newBrFakeBrowser(t, brShopPage("before"), brShopPage("after"))
	jev := &brFakeJev{t: t, replies: []map[string]any{brActStep("click", "@e2", 0.9), brActStep("click", "@e2", 0.9)}}
	ag := newBrowserRunAgent(t, br, jev)
	brNormal(ag)
	rec := &brAskRecorder{}
	ag.SetSubAgentPermAsker(rec.ask(PermissionDeny))

	out := runBrowserRun(t, ag, `{"goal":"`+brGoal+`"}`)

	brAssertContains(t, out, "browser_run: status=denied steps=1", `reason: denied: tool "browser"`)
	if n := len(rec.requests()); n != 1 {
		t.Fatalf("asker saw %d requests, want 1", n)
	}
	if n := len(br.callsFor("click")); n != 0 {
		t.Fatalf("click calls = %d, want none after the deny", n)
	}
	if n := len(br.callsFor("controls")); n != 1 {
		t.Fatalf("controls reads = %d, want 1: no sub-step after the deny", n)
	}
}

// TestBrowserRunNeedsApprovalWithoutAnyAsker checks that with no asker, a normal
// mode mutation stops as needs_approval, never runs, and leaks no sentinel.
func TestBrowserRunNeedsApprovalWithoutAnyAsker(t *testing.T) {
	br := newBrFakeBrowser(t, brShopPage("a"))
	jev := &brFakeJev{t: t, replies: []map[string]any{brActStep("click", "@e2", 0.9)}}
	ag := newBrowserRunAgent(t, br, jev)
	brNormal(ag)

	out := runBrowserRun(t, ag, `{"goal":"`+brGoal+`"}`)

	brAssertContains(t, out, "browser_run: status=needs_approval steps=1", "would need approval")
	if strings.Contains(out, tool.SentinelPermissionAsk) || strings.Contains(out, "PERMISSION_ASK") {
		t.Fatalf("result leaks the permission sentinel:\n%s", out)
	}
	if n := len(br.callsFor("click")); n != 0 {
		t.Fatalf("click calls = %d, want none: the mutation must not run unapproved", n)
	}
}

// TestBrowserRunReadOfUserTabNeedsApproval checks that a read of a tab the agent
// did not open asks, and with no asker stops the run before any decision.
func TestBrowserRunReadOfUserTabNeedsApproval(t *testing.T) {
	br := newBrFakeBrowser(t, brShopPage("a"))
	br.userTab = true
	jev := &brFakeJev{t: t}
	ag := newBrowserRunAgent(t, br, jev)
	brNormal(ag)

	out := runBrowserRun(t, ag, `{"goal":"`+brGoal+`"}`)

	brAssertContains(t, out, "status=needs_approval steps=0", "reading the page controls would need approval")
	if n := jev.requestCount(); n != 0 {
		t.Fatalf("decision requests = %d, want none", n)
	}
}

// TestBrowserRunIsRegisteredOnlyBesideABrowserTool checks registration and its
// default allow rule.
func TestBrowserRunIsRegisteredOnlyBesideABrowserTool(t *testing.T) {
	without := newBrowserRunAgent(t, nil, nil)
	if _, ok := without.tools[browserRunToolName]; ok {
		t.Fatalf("browser_run is registered on an agent with no browser tool")
	}

	with := newBrowserRunAgent(t, newBrFakeBrowser(t), nil)
	if _, ok := with.tools[browserRunToolName]; !ok {
		t.Fatalf("browser_run is not registered on an agent with a browser tool")
	}
	if level := with.permissions.Check(browserRunToolName); level != PermissionAllow {
		t.Fatalf("browser_run default rule = %v, want allow", level)
	}
}
