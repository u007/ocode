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
	})
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
	})
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
	})
	if err != nil || resume {
		t.Fatalf("expected fail-closed no-resume, got resume=%v err=%v", resume, err)
	}
	if !strings.Contains(detail, "confidence") || !strings.Contains(detail, "floor") {
		t.Fatalf("detail should explain the confidence floor: %q", detail)
	}
}

func TestAutoContinueTypesafeUnknownChoiceIsNotResumed(t *testing.T) {
	a, _ := newAutoContinueTypesafeJudge(t, autoContinueVerdictReply("maybe", 0.9, "finished"))
	resume, detail, err := a.AutoContinueJudgeSync(nil)
	if err != nil || resume {
		t.Fatalf("expected no resume on unknown choice, got resume=%v err=%v", resume, err)
	}
	if !strings.Contains(detail, `"maybe"`) {
		t.Fatalf("detail should quote the unknown choice: %q", detail)
	}
}

func TestAutoContinueTypesafeMissingVerdictFailsClosed(t *testing.T) {
	a, _ := newAutoContinueTypesafeJudge(t, `{"model":"jev-latest","answers":{},"usage":{"input_tokens":1,"output_tokens":1}}`)
	resume, detail, err := a.AutoContinueJudgeSync(nil)
	if err != nil || resume {
		t.Fatalf("expected no resume, got resume=%v err=%v", resume, err)
	}
	if !strings.Contains(detail, "no verdict") {
		t.Fatalf("detail should say the verdict was missing: %q", detail)
	}
}

func TestAutoContinueTypesafeStepLimitShortCircuits(t *testing.T) {
	a, h := newAutoContinueTypesafeJudge(t, autoContinueVerdictReply("end", 0.99, "finished"))
	// Simulate the step-limit cutoff: the hard signal must resume WITHOUT a
	// triage call (the fake would answer "end", which must be ignored).
	a.stepLimitHit.Store(true)
	resume, detail, err := a.AutoContinueJudgeSync(nil)
	if err != nil || !resume {
		t.Fatalf("expected hard-signal resume, got resume=%v err=%v", resume, err)
	}
	if !strings.Contains(detail, "step-limit") {
		t.Fatalf("detail should name the step-limit path: %q", detail)
	}
	h.mu.Lock()
	called := h.body != nil
	h.mu.Unlock()
	if called {
		t.Fatal("step-limited turns must not spend a triage call")
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
	resume, detail, err := a.AutoContinueJudgeSync(nil)
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

func TestAutoContinueJudgeAsyncChatJudgeDetail(t *testing.T) {
	// The chat judge path must still work and carry a Detail naming the model.
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
	if !contains(r.Detail, "chat judge") {
		t.Fatalf("Detail should name the chat judge: %q", r.Detail)
	}
}

func TestAutoContinueJudgeSyncNoModelNoCall(t *testing.T) {
	a := &Agent{client: &MockClient{}, config: &config.Config{}}
	resume, detail, err := a.AutoContinueJudgeSync(nil)
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
	state := a.buildTypesafeAutoContinueState(msgs)
	tail := state["transcript_tail"].([]map[string]any)
	if len(tail) != 6 {
		t.Fatalf("tail = %d entries, want 6", len(tail))
	}
}
