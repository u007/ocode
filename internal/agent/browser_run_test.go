package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/u007/ocode/internal/browse/cdp"
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
	// controlsErr, when set, is the error every controls read fails with.
	controlsErr error
	// shot is the PNG the last screenshot call holds, which ExecuteImage returns.
	shot []byte

	mu       sync.Mutex
	calls    []brCall
	controls int
}

type brCall struct {
	action string
	args   map[string]any
}

var (
	_ tool.PageContextTool    = (*brFakeBrowser)(nil)
	_ tool.Tool               = (*brFakeBrowser)(nil)
	_ tool.ImageProducingTool = (*brFakeBrowser)(nil)
)

// ProducesImage reports that a screenshot call returns an image, as the real tool does.
func (b *brFakeBrowser) ProducesImage(args json.RawMessage) bool {
	var m map[string]any
	if err := json.Unmarshal(args, &m); err != nil {
		return false
	}
	return m["action"] == "screenshot"
}

// ExecuteImage returns the PNG the last screenshot call held.
func (b *brFakeBrowser) ExecuteImage(json.RawMessage) ([]byte, string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.shot == nil {
		return nil, "", errors.New("fake browser: no screenshot scripted")
	}
	return b.shot, "image/png", nil
}

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
	if b.controlsErr != nil {
		return "", b.controlsErr
	}
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
	// viewportW and viewportH are the viewport's size in CSS pixels.
	viewportW, viewportH int
	// scrollX and scrollY are the document's scroll offset in CSS pixels.
	scrollX, scrollY float64
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
		"url":             "https://shop.example/cart",
		"title":           "Cart",
		"fingerprint":     p.fingerprint,
		"settled":         !p.unsettled,
		"text":            p.text,
		"text_truncated":  p.textTruncated,
		"controls":        p.controls,
		"omitted":         0,
		"viewport_width":  p.viewportW,
		"viewport_height": p.viewportH,
		"scroll_x":        p.scrollX,
		"scroll_y":        p.scrollY,
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

	mu      sync.Mutex
	bodies  []string
	headers []http.Header
}

// header returns the request headers of decision request i.
func (j *brFakeJev) header(i int) http.Header {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.headers[i]
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
	j.headers = append(j.headers, r.Header.Clone())
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

// TestBrowserRunOutdatedClientExtensionReadsControlsOnce checks that a controls read
// failing with ErrClientExtensionOutdated is not retried: the run reports status=error
// after that one read, and the decision model is never asked.
func TestBrowserRunOutdatedClientExtensionReadsControlsOnce(t *testing.T) {
	br := newBrFakeBrowser(t, brShopPage("a"))
	br.controlsErr = fmt.Errorf("browser: controls of tab t1: %w", cdp.ErrClientExtensionOutdated)
	jev := &brFakeJev{t: t}
	ag := newBrowserRunAgent(t, br, jev)
	brYolo(ag)

	out := runBrowserRun(t, ag, `{"goal":"`+brGoal+`"}`)

	brAssertContains(t, out, "browser_run: status=error steps=0 jev_calls=0")
	if n := len(br.callsFor("controls")); n != 1 {
		t.Fatalf("controls reads = %d, want 1: an outdated client extension must not be retried", n)
	}
	if n := jev.requestCount(); n != 0 {
		t.Fatalf("decision calls = %d, want 0", n)
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

// brWhitePNG is a white screenshot of w x h pixels.
func brWhitePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode screenshot: %v", err)
	}
	return buf.Bytes()
}

// brDensePage is a page of n controls on a 1280x800 viewport. The first control is
// a text field that holds a value, on the screen. Of the rest, the first inView are
// buttons on the screen and the others lie far below it.
func brDensePage(fingerprint string, n, inView int) brPage {
	p := brPage{fingerprint: fingerprint, text: "Options", viewportW: 1280, viewportH: 800}
	p.controls = append(p.controls, browserRunControl{
		Ref: "@e1", Role: "textbox", Label: "Secret field", Value: "jordan@example.com",
		States: []string{}, Operations: []string{"click", "fill"},
		Box: &browserRunBox{X: 10, Y: 700, W: 300, H: 40}, InViewport: true,
	})
	for i := 2; i <= n; i++ {
		c := browserRunControl{
			Ref: fmt.Sprintf("@e%d", i), Role: "button", Label: fmt.Sprintf("Secret label %d", i),
			States: []string{}, Operations: []string{"click"},
		}
		if i <= inView+1 {
			c.Box = &browserRunBox{X: float64(10 + (i%8)*150), Y: float64(10 + (i/8)*40), W: 120, H: 30}
			c.InViewport = true
		} else {
			c.Box = &browserRunBox{X: 10, Y: 3000, W: 120, H: 30}
		}
		p.controls = append(p.controls, c)
	}
	return p
}

// brRequestAssets returns the assets object of a decision request body.
func brRequestAssets(t *testing.T, body string) map[string]TypesafeAsset {
	t.Helper()
	var req struct {
		Assets map[string]TypesafeAsset `json:"assets"`
	}
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("decode decision request: %v", err)
	}
	return req.Assets
}

// brTextOnlyJudge is a decision backend without the image method. It embeds only
// the Decider methods, so it is not an imageDecider.
type brTextOnlyJudge struct{ Decider }

func (brTextOnlyJudge) Chat(_ []Message, _ []map[string]any) (*Message, error) {
	return nil, ErrTypesafeDecisionOnly
}

// newBrTextOnlyAgent builds the browser_run agent of newBrowserRunAgent, with a judge
// that cannot take images.
func newBrTextOnlyAgent(t *testing.T, br tool.Tool, jev *brFakeJev) *Agent {
	t.Helper()
	ag := newBrowserRunAgent(t, br, jev)
	srv := httptest.NewServer(jev)
	t.Cleanup(srv.Close)
	prev := newClientFn
	t.Cleanup(func() { newClientFn = prev })
	newClientFn = func(_ *config.Config, model string) LLMClient {
		if model != defaultJudgeModel {
			return nil
		}
		return brTextOnlyJudge{Decider: newTypesafeClient("k", "jev-latest", srv.URL)}
	}
	return ag
}

// TestBrowserRunTextViewUnchangedForSmallPage: a page that fits the text state is
// judged in the text view, with no image, no header and no screenshot call.
func TestBrowserRunTextViewUnchangedForSmallPage(t *testing.T) {
	br := newBrFakeBrowser(t, brShopPage("a"))
	jev := &brFakeJev{t: t, replies: []map[string]any{brDoneReply()}}
	ag := newBrowserRunAgent(t, br, jev)
	brYolo(ag)

	out := runBrowserRun(t, ag, `{"goal":"`+brGoal+`"}`)
	brAssertContains(t, out, "status=done")

	state := jev.decodeState(t, 0)
	want := []string{"goal", "date", "page", "data_keys", "recent_actions", "elements", "omitted_elements"}
	if len(state) != len(want) {
		t.Fatalf("text state keys = %v, want exactly %v", state, want)
	}
	for _, k := range want {
		if _, ok := state[k]; !ok {
			t.Fatalf("text state lacks %q", k)
		}
	}
	if pg := state["page"].(map[string]any); pg["text"] != "Your cart is empty." {
		t.Fatalf("text view dropped the page text: %v", pg)
	}
	if assets := brRequestAssets(t, jev.request(0)); assets != nil {
		t.Fatalf("text view sent assets: %v", assets)
	}
	if h := jev.header(0).Get(TypesafeImagesHeader); h != "" {
		t.Fatalf("text view sent the images header %q", h)
	}
	if n := len(br.callsFor("screenshot")); n != 0 {
		t.Fatalf("text view took %d screenshots, want none", n)
	}
}

// TestBrowserRunScreenshotViewWhenControlsOverflow: a page with more controls than
// the text view offers is judged on a screenshot. The state and questions carry no
// page text, labels or values, and the one image is in the assets.
func TestBrowserRunScreenshotViewWhenControlsOverflow(t *testing.T) {
	br := newBrFakeBrowser(t, brDensePage("dense", 170, 60), brShopPage("after"))
	br.shot = brWhitePNG(t, 1600, 1000)
	jev := &brFakeJev{t: t, replies: []map[string]any{brActStep("click", "@e2", 0.9), brDoneReply()}}
	ag := newBrowserRunAgent(t, br, jev)
	brYolo(ag)

	out := runBrowserRun(t, ag, `{"goal":"`+brGoal+`"}`)
	brAssertContains(t, out, "status=done", "steps=1")
	if n := jev.requestCount(); n != 2 {
		t.Fatalf("decision requests = %d, want 2 (screenshot step, then text done)", n)
	}

	state := jev.decodeState(t, 0)
	if state["view"] != "screenshot" {
		t.Fatalf("view = %v, want screenshot", state["view"])
	}
	pg := state["page"].(map[string]any)
	if _, ok := pg["text"]; ok {
		t.Fatalf("screenshot state carries page text: %v", pg)
	}
	if elements := state["elements"].([]any); len(elements) != 61 {
		t.Fatalf("elements = %d, want the 61 controls on the screen", len(elements))
	}
	if off := state["offscreen_elements"].(float64); off != 109 {
		t.Fatalf("offscreen_elements = %v, want 109", off)
	}
	for _, secret := range []string{"Secret label", "Secret field", "jordan@example.com", "Options"} {
		if strings.Contains(jev.request(0), secret) {
			t.Fatalf("screenshot request carries %q", secret)
		}
	}

	assets := brRequestAssets(t, jev.request(0))
	placeholder, _ := state["screenshot"].(string)
	if len(assets) != 1 || assets[placeholder].Type != "image" || assets[placeholder].Format != "png" {
		t.Fatalf("assets = %v, want one png under the state's placeholder %q", assets, placeholder)
	}
	raw, err := base64.StdEncoding.DecodeString(assets[placeholder].DataBase64)
	if err != nil {
		t.Fatalf("decode asset: %v", err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("asset is not a png: %v", err)
	}
	if cfg.Width > 1280 || cfg.Height > 800 || cfg.Width != 1280 || cfg.Height != 800 {
		t.Fatalf("image = %dx%d, want the 1600x1000 page scaled to 1280x800", cfg.Width, cfg.Height)
	}
	if h := jev.header(0).Get(TypesafeImagesHeader); h != "true" {
		t.Fatalf("screenshot request header = %q, want true", h)
	}
	if h := jev.header(1).Get(TypesafeImagesHeader); h != "" {
		t.Fatalf("text request sent the images header %q", h)
	}
	if n := len(br.callsFor("screenshot")); n != 1 {
		t.Fatalf("screenshots = %d, want 1", n)
	}
}

// TestBrowserRunScreenshotMasksTextEntryBeforeAnyMark: the text field's box is solid
// black in the image, and the page around it is not.
func TestBrowserRunScreenshotMasksTextEntryBeforeAnyMark(t *testing.T) {
	br := newBrFakeBrowser(t, brDensePage("dense", 170, 60), brShopPage("after"))
	br.shot = brWhitePNG(t, 1280, 800)
	jev := &brFakeJev{t: t, replies: []map[string]any{brDoneReply()}}
	ag := newBrowserRunAgent(t, br, jev)
	brYolo(ag)
	runBrowserRun(t, ag, `{"goal":"`+brGoal+`"}`)

	assets := brRequestAssets(t, jev.request(0))
	var raw []byte
	for _, a := range assets {
		b, err := base64.StdEncoding.DecodeString(a.DataBase64)
		if err != nil {
			t.Fatalf("decode asset: %v", err)
		}
		raw = b
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode image: %v", err)
	}
	// The text field is at (10,700) 300x40 on a 1280x800 viewport, so its centre is
	// clear of the number tag at its top-left corner.
	for _, p := range []image.Point{{160, 720}, {200, 735}, {280, 705}} {
		r, g, b, a := img.At(p.X, p.Y).RGBA()
		if r != 0 || g != 0 || b != 0 || a != 0xffff {
			t.Fatalf("text field pixel %v = (%d,%d,%d,%d), want opaque black", p, r, g, b, a)
		}
	}
	r, g, b, _ := img.At(600, 400).RGBA()
	if r == 0 && g == 0 && b == 0 {
		t.Fatalf("background pixel is black; the mask must not cover the page")
	}
}

// TestBrowserRunScreenshotBudgetRefusesBeforeAnyScreenshot: a state that cannot fit
// beside an image stops the run before the browser is asked for a screenshot.
func TestBrowserRunScreenshotBudgetRefusesBeforeAnyScreenshot(t *testing.T) {
	br := newBrFakeBrowser(t, brDensePage("dense", 170, 60))
	br.shot = brWhitePNG(t, 1280, 800)
	jev := &brFakeJev{t: t}
	ag := newBrowserRunAgent(t, br, jev)
	brYolo(ag)

	out := runBrowserRun(t, ag, `{"goal":"`+strings.Repeat("g", 31_000)+`"}`)
	brAssertContains(t, out, "status=page_too_dense", "byte budget left beside the image")
	if n := len(br.callsFor("screenshot")); n != 0 {
		t.Fatalf("screenshots = %d, want none", n)
	}
	if n := jev.requestCount(); n != 0 {
		t.Fatalf("decision requests = %d, want none", n)
	}
}

// TestBrowserRunWithoutImageDeciderNamesTheScreenshotView: a judge that cannot take
// images keeps the text view, and a page too dense for it stops the run with a
// detail that names the TypeSafe requirement. No screenshot is taken.
func TestBrowserRunWithoutImageDeciderNamesTheScreenshotView(t *testing.T) {
	br := newBrFakeBrowser(t, brDensePage("dense", 170, 60))
	br.shot = brWhitePNG(t, 1280, 800)
	jev := &brFakeJev{t: t}
	ag := newBrTextOnlyAgent(t, br, jev)
	brYolo(ag)

	out := runBrowserRun(t, ag, `{"goal":"`+strings.Repeat("g", 31_000)+`"}`)
	brAssertContains(t, out, "status=page_too_dense", "The screenshot view needs a TypeSafe decision backend")
	if n := len(br.callsFor("screenshot")); n != 0 {
		t.Fatalf("screenshots = %d, want none", n)
	}
	if n := jev.requestCount(); n != 0 {
		t.Fatalf("decision requests = %d, want none", n)
	}
}

// TestBrowserRunScreenshotNeedsBoxes: a listing whose controls carry no boxes stops
// the screenshot view with a clear error, not a blank image.
func TestBrowserRunScreenshotNeedsBoxes(t *testing.T) {
	page := brDensePage("dense", 170, 60)
	for i := range page.controls {
		page.controls[i].Box = nil
		page.controls[i].InViewport = false
	}
	br := newBrFakeBrowser(t, page)
	br.shot = brWhitePNG(t, 1280, 800)
	jev := &brFakeJev{t: t}
	ag := newBrowserRunAgent(t, br, jev)
	brYolo(ag)

	out := runBrowserRun(t, ag, `{"goal":"`+brGoal+`"}`)
	brAssertContains(t, out, "status=error", "no control on the page has a box")
	if n := jev.requestCount(); n != 0 {
		t.Fatalf("decision requests = %d, want none", n)
	}
}

// TestBrowserRunShotMasksTextEntryPastTheDrawCap: the drawn set is capped, but a text
// field past the cap that is on the screen is still masked.
func TestBrowserRunShotMasksTextEntryPastTheDrawCap(t *testing.T) {
	var controls []browserRunControl
	for i := 1; i <= 170; i++ {
		c := browserRunControl{Ref: fmt.Sprintf("@e%d", i), Role: "button", Operations: []string{"click"}, InViewport: true,
			Box: &browserRunBox{X: 10, Y: float64(i), W: 50, H: 10}}
		if i == 170 {
			c.Role = "textbox"
			c.Operations = []string{"click", "fill"}
		}
		controls = append(controls, c)
	}
	drawn, masked, err := browserRunShotControls(browserRunPage{Controls: controls})
	if err != nil {
		t.Fatalf("browserRunShotControls: %v", err)
	}
	if len(drawn) != browserRunMaxControls {
		t.Fatalf("drawn = %d, want the cap %d", len(drawn), browserRunMaxControls)
	}
	if len(masked) != 1 || masked[0].Ref != "@e170" {
		t.Fatalf("masked = %v, want the text field @e170 past the draw cap", masked)
	}
}

// brScrollReply is the reply to a step that chose scroll_down.
func brScrollReply() map[string]any {
	return map[string]any{"done": brDoneAnswer(0.1), "operation": brChoiceAnswer("scroll_down", 0.9)}
}

// TestBrowserRunScrollOnScreenshotViewIsProgress: three scrolls over a listing that
// does not change, with the scroll offset moving each time, are progress on the
// screenshot view, so the run is not stalled and the goal is reached on the fourth
// decision.
func TestBrowserRunScrollOnScreenshotViewIsProgress(t *testing.T) {
	pages := make([]brPage, 4)
	for i := range pages {
		pages[i] = brDensePage("dense", 170, 60)
		pages[i].scrollY = float64(800 * i)
	}
	br := newBrFakeBrowser(t, pages...)
	br.shot = brWhitePNG(t, 1280, 800)
	jev := &brFakeJev{t: t, replies: []map[string]any{brScrollReply(), brScrollReply(), brScrollReply(), brDoneReply()}}
	ag := newBrowserRunAgent(t, br, jev)
	brYolo(ag)

	out := runBrowserRun(t, ag, `{"goal":"`+brGoal+`"}`)
	brAssertContains(t, out, "status=done", "steps=3")
	if n := len(br.callsFor("scroll")); n != 3 {
		t.Fatalf("scroll calls = %d, want 3", n)
	}
	if n := jev.requestCount(); n != 4 {
		t.Fatalf("decision requests = %d, want 4", n)
	}
}

// TestBrowserRunScrollWithoutMovementStillStallsOnScreenshotView: when the offset does
// not move either, the screenshot view stalls as the text view does.
func TestBrowserRunScrollWithoutMovementStillStallsOnScreenshotView(t *testing.T) {
	br := newBrFakeBrowser(t, brDensePage("dense", 170, 60))
	br.shot = brWhitePNG(t, 1280, 800)
	jev := &brFakeJev{t: t, replies: []map[string]any{brScrollReply(), brScrollReply(), brScrollReply()}}
	ag := newBrowserRunAgent(t, br, jev)
	brYolo(ag)

	out := runBrowserRun(t, ag, `{"goal":"`+brGoal+`"}`)
	brAssertContains(t, out, "status=stalled")
}

// TestBrowserRunTinyViewportStopsBeforeAScreenshot: a viewport side under 64 px is
// refused before the screenshot is taken, so no image is sent that the decision
// model would reject.
func TestBrowserRunTinyViewportStopsBeforeAScreenshot(t *testing.T) {
	page := brDensePage("dense", 170, 60)
	page.viewportW = 40
	br := newBrFakeBrowser(t, page)
	br.shot = brWhitePNG(t, 1280, 800)
	jev := &brFakeJev{t: t}
	ag := newBrowserRunAgent(t, br, jev)
	brYolo(ag)

	out := runBrowserRun(t, ag, `{"goal":"`+brGoal+`"}`)
	brAssertContains(t, out, "status=error", "side under 64 px")
	if n := len(br.callsFor("screenshot")); n != 0 {
		t.Fatalf("screenshots = %d, want none", n)
	}
	if n := jev.requestCount(); n != 0 {
		t.Fatalf("decision requests = %d, want none", n)
	}
}

// TestBrowserRunShotDrawsAtDevicePixelRatio: a screenshot at 2x device pixels covers
// the same viewport, so the masks and outlines land on the same CSS positions as at
// 1x. Each mapping is by image pixels per CSS pixel, per axis, after scaling.
func TestBrowserRunShotDrawsAtDevicePixelRatio(t *testing.T) {
	drawn := []browserRunControl{{Ref: "@e7", Role: "button", InViewport: true,
		Box: &browserRunBox{X: 500, Y: 100, W: 120, H: 30}}}
	masked := []browserRunControl{{Ref: "@e1", Role: "textbox", InViewport: true,
		Box: &browserRunBox{X: 10, Y: 700, W: 300, H: 40}}}
	page := browserRunPage{ViewportWidth: 1280, ViewportHeight: 800}

	raw, w, h, err := browserRunDrawShot(brWhitePNG(t, 2560, 1600), page, drawn, masked)
	if err != nil {
		t.Fatalf("browserRunDrawShot: %v", err)
	}
	if w != 1280 || h != 800 {
		t.Fatalf("image = %dx%d, want 1280x800", w, h)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode image: %v", err)
	}
	isBlack := func(x, y int) bool {
		r, g, b, a := img.At(x, y).RGBA()
		return r == 0 && g == 0 && b == 0 && a == 0xffff
	}
	isMark := func(x, y int) bool {
		r, g, b, _ := img.At(x, y).RGBA()
		mr, mg, mb, _ := browserRunShotMark.RGBA()
		return r == mr && g == mg && b == mb
	}
	if !isBlack(10, 720) || !isBlack(309, 720) {
		t.Fatalf("text field edges (10,720) and (309,720) are not black; the mask is off by the device ratio")
	}
	if isBlack(9, 720) || isBlack(310, 720) {
		t.Fatalf("pixels just outside the text field are black; the mask is too large")
	}
	if !isMark(500, 115) {
		t.Fatalf("button outline at (500,115) is not drawn; the outline is off by the device ratio")
	}
	if isMark(499, 115) {
		t.Fatalf("pixel (499,115) outside the button is drawn")
	}
}
