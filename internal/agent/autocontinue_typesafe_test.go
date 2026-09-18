package agent

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

// newAutoContinueTypesafeJudge stands up a fake /systemone endpoint answering
// the triage verdict question (and reuses the permission harness's reply
// shape — the answer key differs, so the reply is built here).
func newAutoContinueTypesafeJudge(t *testing.T, reply string) (*Agent, *typesafeJudgeHarness) {
	t.Helper()
	h := &typesafeJudgeHarness{reply: reply}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		defer h.mu.Unlock()
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		h.body = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(h.reply))
	}))
	t.Cleanup(srv.Close)

	cfg := &config.Config{}
	cfg.Ocode.AutoContinueModel = "typesafe/jev-latest"
	cfg.Ocode.AutoContinueEnabled = true
	a := NewAgent(nil, nil, cfg, nil)
	prev := newClientFn
	t.Cleanup(func() { newClientFn = prev })
	newClientFn = func(_ *config.Config, _ string) LLMClient {
		return newTypesafeClient("k", "jev-latest", srv.URL)
	}
	return a, h
}

func autoContinueVerdictReply(choice string, confidence float64, reason string) string {
	return `{"model":"jev-latest","answers":{"verdict":{"type":"choice","choice":"` + choice + `","probabilities":{"continue":0.5,"end":0.5},"confidence":` + jsonFloat(confidence) + `},"reason":{"type":"choice","choice":"` + reason + `","probabilities":{},"confidence":0.8}},"usage":{"input_tokens":7,"output_tokens":2}}`
}

func TestAutoContinueTypesafeContinueVerdict(t *testing.T) {
	a, h := newAutoContinueTypesafeJudge(t, autoContinueVerdictReply("continue", 0.95, "truncated"))
	a.SetMaxSteps(0)
	resume, detail, err := a.AutoContinueJudgeSync([]Message{
		{Role: "user", Content: "build the thing"},
		{Role: "assistant", Content: "Here is the first half of the imp"},
	}, nil)
	if err != nil || !resume {
		t.Fatalf("expected resume, got resume=%v err=%v detail=%q", resume, err, detail)
	}
	if !strings.Contains(detail, "jev-latest") || !strings.Contains(detail, "cut off") {
		t.Fatalf("detail should name the model and the resume: %q", detail)
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	state, ok := h.body["state"].(map[string]any)
	if !ok {
		t.Fatalf("state should be an object, got %T", h.body["state"])
	}
	tail, ok := state["transcript_tail"].([]any)
	if !ok || len(tail) != 2 {
		t.Fatalf("transcript_tail = %v", state["transcript_tail"])
	}
	if state["turn_ended"] == "" {
		t.Fatal("state.turn_ended should describe the natural stop")
	}
	qs := h.body["questions"].(map[string]any)
	verdict := qs["verdict"].(map[string]any)
	criteria := verdict["criteria"].(map[string]any)
	if _, ok := criteria["continue"]; !ok {
		t.Fatalf("criteria missing continue: %v", criteria)
	}
	if _, ok := criteria["end"]; !ok {
		t.Fatalf("criteria missing end: %v", criteria)
	}
	reason := qs["reason"].(map[string]any)
	if len(reason["criteria"].(map[string]any)) != len(typesafeAutoContinueReasons) {
		t.Fatalf("reason criteria mismatch")
	}
}

func TestAutoContinueTypesafeEndVerdict(t *testing.T) {
	a, _ := newAutoContinueTypesafeJudge(t, autoContinueVerdictReply("end", 0.97, "finished"))
	resume, detail, err := a.AutoContinueJudgeSync([]Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "Done — all tests pass."},
	}, nil)
	if err != nil || resume {
		t.Fatalf("expected no resume, got resume=%v err=%v", resume, err)
	}
	if !strings.Contains(detail, "finished") {
		t.Fatalf("detail should carry the typed reason: %q", detail)
	}
}

func TestAutoContinueTypesafeLowConfidenceFailsClosed(t *testing.T) {
	a, _ := newAutoContinueTypesafeJudge(t, autoContinueVerdictReply("continue", 0.4, "mid_task"))
	resume, detail, err := a.AutoContinueJudgeSync([]Message{
		{Role: "assistant", Content: "working on it"},
	}, nil)
	if err != nil || resume {
		t.Fatalf("expected fail-closed no-resume, got resume=%v err=%v", resume, err)
	}
	if !strings.Contains(detail, "confidence") || !strings.Contains(detail, "floor") {
		t.Fatalf("detail should explain the confidence floor: %q", detail)
	}
}

// Auto-continue is low-stakes and reversible, so it must use its OWN lower
// confidence floor — not the shared permission floor (0.85). Reusing the
// permission floor failed closed on legitimate mid-task verdicts whose Jev
// `confidence` sits in the 0.6-0.8 band even though the chosen option's
// probability is high (a real report: "auto continue llm via jev does not
// work"). This pins the risk-scaling split: a 0.66-confidence `continue`
// resumes, while the permission floor would have rejected it.
func TestAutoContinueTypesafeUsesRiskScaledFloorNotPermissionFloor(t *testing.T) {
	a, _ := newAutoContinueTypesafeJudge(t, autoContinueVerdictReply("continue", 0.66, "mid_task"))
	// Even with a high configured permission floor, auto-continue must not
	// inherit it.
	a.config.Ocode.Permissions = config.PermissionConfig{
		Auto: &config.AutoPermissionConfig{MinConfidence: 0.95},
	}
	if got := a.resolveAutoJudgeMinConfidence(); got != 0.95 {
		t.Fatalf("permission floor = %v, want 0.95 (sanity)", got)
	}
	if got := a.resolveAutoContinueMinConfidence(); got != autoContinueMinConfidenceDefault {
		t.Fatalf("auto-continue floor = %v, want %v", got, autoContinueMinConfidenceDefault)
	}
	resume, detail, err := a.AutoContinueJudgeSync([]Message{
		{Role: "assistant", Content: "I've updated the schema. Next I'll regenerate the migrations and run the tests."},
	}, nil)
	if err != nil || !resume {
		t.Fatalf("0.66-confidence continue should resume at the auto-continue floor, got resume=%v err=%v detail=%q", resume, err, detail)
	}
}

// A coin-flip verdict ("continue" at 0.4, below TypeSafe's documented
// "genuinely unsure / do not act" band of <0.5) still fails closed.
func TestAutoContinueTypesafeBelowUnsureBandFailsClosed(t *testing.T) {
	a, _ := newAutoContinueTypesafeJudge(t, autoContinueVerdictReply("continue", 0.49, "mid_task"))
	resume, _, err := a.AutoContinueJudgeSync([]Message{
		{Role: "assistant", Content: "hmm"},
	}, nil)
	if err != nil || resume {
		t.Fatalf("sub-0.5 confidence must fail closed, got resume=%v err=%v", resume, err)
	}
}

func TestAutoContinueTypesafeUnknownChoiceIsNotResumed(t *testing.T) {
	a, _ := newAutoContinueTypesafeJudge(t, autoContinueVerdictReply("maybe", 0.9, "finished"))
	resume, detail, err := a.AutoContinueJudgeSync(nil, nil)
	if err != nil || resume {
		t.Fatalf("expected no resume on unknown choice, got resume=%v err=%v", resume, err)
	}
	if !strings.Contains(detail, `"maybe"`) {
		t.Fatalf("detail should quote the unknown choice: %q", detail)
	}
}

func TestAutoContinueTypesafeMissingVerdictFailsClosed(t *testing.T) {
	a, _ := newAutoContinueTypesafeJudge(t, `{"model":"jev-latest","answers":{},"usage":{"input_tokens":1,"output_tokens":1}}`)
	resume, detail, err := a.AutoContinueJudgeSync(nil, nil)
	if err != nil || resume {
		t.Fatalf("expected no resume, got resume=%v err=%v", resume, err)
	}
	if !strings.Contains(detail, "no verdict") {
		t.Fatalf("detail should say the verdict was missing: %q", detail)
	}
}

func TestAutoContinueTypesafeStepLimitStateReportedNotShortCircuited(t *testing.T) {
	a, h := newAutoContinueTypesafeJudge(t, autoContinueVerdictReply("end", 0.99, "finished"))
	// The caller owns the hard /max-step signal: every production dispatcher
	// checks StepLimitHit first and resumes deterministically without a triage
	// call. If a caller were to invoke the judge anyway, the function must not
	// fabricate a resume it might mistake for a judge verdict — it reports the
	// real end reason in the state and lets the judge decide.
	a.stepLimitHit.Store(true)
	resume, _, err := a.AutoContinueJudgeSync(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resume {
		t.Fatal("step-limited turn must not fabricate a resume without a triage call")
	}

	h.mu.Lock()
	body := h.body
	h.mu.Unlock()
	if body == nil {
		t.Fatal("the triage call should have been made (no step-limit short-circuit)")
	}
	state, _ := body["state"].(map[string]any)
	if got, _ := state["turn_ended"].(string); !strings.Contains(got, "step limit") {
		t.Fatalf("state.turn_ended = %q, want the step-limit wording", got)
	}
}

func TestAutoContinueTypesafeTransportErrorFailsClosed(t *testing.T) {
	a := &Agent{
		client: &GenericClient{Provider: "opencode-go", Model: "m"},
		config: &config.Config{Ocode: config.OcodeConfig{AutoContinueModel: "typesafe/jev-latest", AutoContinueEnabled: true}},
	}
	prev := newClientFn
	t.Cleanup(func() { newClientFn = prev })
	newClientFn = func(_ *config.Config, _ string) LLMClient {
		return newTypesafeClient("k", "jev-latest", "http://127.0.0.1:1")
	}
	resume, detail, err := a.AutoContinueJudgeSync(nil, nil)
	if err == nil || resume {
		t.Fatalf("expected error + no resume, got resume=%v err=%v", resume, err)
	}
	if !strings.Contains(detail, "triage failed") {
		t.Fatalf("detail should report the failure: %q", detail)
	}
}

func TestAutoContinueJudgeAsyncTypesafeDetail(t *testing.T) {
	a, _ := newAutoContinueTypesafeJudge(t, autoContinueVerdictReply("continue", 0.95, "truncated"))
	done := make(chan AutoContinueJudgeResult, 1)
	a.OnAutoContinueJudge = func(r AutoContinueJudgeResult) { done <- r }
	if !a.AutoContinueJudgeAsync([]Message{{Role: "assistant", Content: "partial res"}}, 1) {
		t.Fatal("Async dispatch should start")
	}
	r := <-done
	if r.Err != nil || !r.Resume {
		t.Fatalf("expected async resume, got %+v", r)
	}
	if r.Detail == "" || !contains(r.Detail, "jev-latest") {
		t.Fatalf("Detail should name the judge: %q", r.Detail)
	}
}

func TestAutoContinueJudgeAsyncContinuousJudgeDetail(t *testing.T) {
	// The continuous judge (chat/prose path) must still work and carry a Detail naming the model.
	cfg := &config.Config{}
	cfg.Ocode.AutoContinueModel = "opencode-go/mimo-v2.5"
	a := NewAgent(nil, nil, cfg, nil)
	prev := newClientFn
	t.Cleanup(func() { newClientFn = prev })
	newClientFn = func(_ *config.Config, _ string) LLMClient {
		return &MockClient{Response: &Message{Role: "assistant", Content: "YES"}}
	}
	done := make(chan AutoContinueJudgeResult, 1)
	a.OnAutoContinueJudge = func(r AutoContinueJudgeResult) { done <- r }
	if !a.AutoContinueJudgeAsync([]Message{{Role: "assistant", Content: "partial"}}, 1) {
		t.Fatal("Async dispatch should start")
	}
	r := <-done
	if r.Err != nil || !r.Resume {
		t.Fatalf("expected async resume, got %+v", r)
	}
	if !contains(r.Detail, "continuous judge") {
		t.Fatalf("Detail should name the continuous judge: %q", r.Detail)
	}
}

func TestAutoContinueJudgeAsyncContinuousJudgeDetailOnError(t *testing.T) {
	// A failed continuous-judge call must carry the error in Detail: the TUI renders
	// only Detail on a non-resume verdict, so without it a judge failure looks
	// like a silent finish (the defect this feature exists to remove).
	cfg := &config.Config{}
	cfg.Ocode.AutoContinueModel = "opencode-go/mimo-v2.5"
	a := NewAgent(nil, nil, cfg, nil)
	prev := newClientFn
	t.Cleanup(func() { newClientFn = prev })
	newClientFn = func(_ *config.Config, _ string) LLMClient {
		return &MockClient{Err: errors.New("judge transport down")}
	}
	done := make(chan AutoContinueJudgeResult, 1)
	a.OnAutoContinueJudge = func(r AutoContinueJudgeResult) { done <- r }
	if !a.AutoContinueJudgeAsync([]Message{{Role: "assistant", Content: "partial"}}, 1) {
		t.Fatal("Async dispatch should start")
	}
	r := <-done
	if r.Err == nil {
		t.Fatalf("expected the judge error to propagate, got %+v", r)
	}
	if !contains(r.Detail, "judge transport down") {
		t.Fatalf("Detail must describe the failure, got %q", r.Detail)
	}
}

func TestAutoContinueJudgeSyncNoModelNoCall(t *testing.T) {
	a := &Agent{client: &MockClient{}, config: &config.Config{}}
	resume, detail, err := a.AutoContinueJudgeSync(nil, nil)
	if resume || err != nil || detail != "" {
		t.Fatalf("no judge configured: expected (false,\"\",nil), got (%v,%q,%v)", resume, detail, err)
	}
}

func TestStepLimitHitDetail(t *testing.T) {
	a := &Agent{}
	if got := a.StepLimitHitDetail(nil); got != "" {
		t.Fatalf("natural stop detail = %q, want empty", got)
	}
	a.stepLimitHit.Store(true)
	if got := a.StepLimitHitDetail(nil); got == "" {
		t.Fatal("step-limit detail should be non-empty")
	}
	a.stepLimitHit.Store(false)
	if got := a.StepLimitHitDetail(errors.New("boom")); !strings.Contains(got, "boom") {
		t.Fatalf("error detail should carry the error: %q", got)
	}
}

func TestAutoContinueTypesafeStateTailBounded(t *testing.T) {
	a := &Agent{config: &config.Config{}}
	msgs := make([]Message, 0, 20)
	for i := 0; i < 20; i++ {
		msgs = append(msgs, Message{Role: "assistant", Content: "long content here"})
	}
	state := a.buildTypesafeAutoContinueState(msgs, nil)
	tail := state["transcript_tail"].([]map[string]any)
	if len(tail) != 6 {
		t.Fatalf("tail = %d entries, want 6", len(tail))
	}
}

func TestAutoContinueTypesafeStateTurnEndedReflectsReason(t *testing.T) {
	a := &Agent{config: &config.Config{}}
	msgs := []Message{{Role: "assistant", Content: "partial"}}

	if got, _ := a.buildTypesafeAutoContinueState(msgs, nil)["turn_ended"].(string); !strings.Contains(got, "finished") {
		t.Fatalf("natural-stop turn_ended = %q, want the natural completion wording", got)
	}
	if got, _ := a.buildTypesafeAutoContinueState(msgs, errors.New("boom"))["turn_ended"].(string); !strings.Contains(got, "boom") {
		t.Fatalf("errored-turn turn_ended = %q, want the error", got)
	}
	a.stepLimitHit.Store(true)
	if got, _ := a.buildTypesafeAutoContinueState(msgs, nil)["turn_ended"].(string); !strings.Contains(got, "step limit") {
		t.Fatalf("step-limited turn_ended = %q, want the step-limit wording", got)
	}
}

func TestAutoContinueTypesafeStateContentCapped(t *testing.T) {
	a := &Agent{config: &config.Config{}}
	big := strings.Repeat("x", 5000)
	state := a.buildTypesafeAutoContinueState([]Message{{Role: "assistant", Content: big}}, nil)

	tail, ok := state["transcript_tail"].([]map[string]any)
	if !ok || len(tail) != 1 {
		t.Fatalf("transcript_tail = %v, want one entry", state["transcript_tail"])
	}
	content, _ := tail[0]["content"].(string)
	if !strings.HasSuffix(content, "…(truncated)") {
		head := content
		if len(head) > 40 {
			head = head[:40]
		}
		t.Fatalf("content not marked truncated: %q", head)
	}
	// The cap is 4000 chars of body plus the marker; anything larger would blow
	// up the Decide request.
	if got, want := len(content), 4000+len("…(truncated)"); got != want {
		t.Fatalf("capped content length = %d, want %d", got, want)
	}
}

func TestAutoContinueTypesafeReasonLabelRendered(t *testing.T) {
	a, _ := newAutoContinueTypesafeJudge(t, autoContinueVerdictReply("continue", 0.95, "mid_task"))
	a.SetMaxSteps(0)
	_, detail, err := a.AutoContinueJudgeSync([]Message{
		{Role: "assistant", Content: "I will continue with the next part"},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := typesafeAutoContinueReasonLabel("mid_task")
	if !strings.Contains(detail, want) {
		t.Fatalf("detail should carry the reason label %q, got %q", want, detail)
	}
	// The raw key must not leak through: the label is the user-facing text.
	if strings.Contains(detail, "reason: mid_task") {
		t.Fatalf("detail rendered the raw reason key instead of its label: %q", detail)
	}
}
