package agent

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/config"
)

func pulseActivationResp(choice string, confidence float64) *TypesafeResponse {
	return &TypesafeResponse{
		Model:   "jev-latest",
		Answers: map[string]TypesafeAnswer{"activate": {Type: "choice", Choice: choice, Confidence: confidence}},
		Usage:   TypesafeUsage{InputTokens: 11, OutputTokens: 3},
	}
}

// stubPulseActivationJudge wires stub in as the pulse_activation backend. A nil
// stub means newClientFn returns no client (no credential). Not parallel-safe:
// newClientFn is a package var.
func stubPulseActivationJudge(t *testing.T, stub LLMClient) {
	t.Helper()
	prev := newClientFn
	t.Cleanup(func() { newClientFn = prev })
	newClientFn = func(_ *config.Config, _ string) LLMClient {
		return stub
	}
}

var pulseActivationTestEvents = []PulseActivationEvent{
	{Kind: "turn_done", Project: "ocode", ID: "ses_1", Summary: "turn finished", At: time.Now()},
}

func TestJudgePulseActivation_WakeAndSkip(t *testing.T) {
	cfg := &config.Config{}
	cases := []struct {
		name       string
		resp       *TypesafeResponse
		wantWake   bool
		wantChoice string
	}{
		{"confident wake", pulseActivationResp("wake", 0.9), true, "wake"},
		{"confident skip", pulseActivationResp("skip", 0.9), false, "skip"},
		{"wake exactly at the floor", pulseActivationResp("wake", pulseActivationMinConfidence), true, "wake"},
		{"wake below the floor is skipped", pulseActivationResp("wake", 0.4), false, "wake"},
		{"NaN confidence is below the floor", pulseActivationResp("wake", math.NaN()), false, "wake"},
		{"confidence above 1 is below the floor", pulseActivationResp("wake", 1.5), false, "wake"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stubPulseActivationJudge(t, &goalStubDecider{resp: tc.resp})
			v := JudgePulseActivation(context.Background(), cfg, "wake me for failures", pulseActivationTestEvents)
			if v.Err != nil {
				t.Fatalf("unexpected error: %v", v.Err)
			}
			if v.Activate != tc.wantWake || v.Choice != tc.wantChoice {
				t.Fatalf("Activate=%v Choice=%q, want %v %q (detail %q)", v.Activate, v.Choice, tc.wantWake, tc.wantChoice, v.Detail)
			}
			if v.InTokens != 11 || v.OutTokens != 3 {
				t.Fatalf("usage = %d/%d, want 11/3", v.InTokens, v.OutTokens)
			}
			if v.Label != "typesafe/jev-latest" {
				t.Fatalf("label = %q, want the backend's own provider/model", v.Label)
			}
		})
	}
}

// A judge that cannot decide must never wake the assistant: saving tokens is the
// gate's purpose, so every failure leaves it idle and reports why.
func TestJudgePulseActivation_FailuresNeverWake(t *testing.T) {
	cfg := &config.Config{}
	cases := []struct {
		name string
		stub *goalStubDecider
	}{
		{"no credential", nil},
		{"judge error", &goalStubDecider{err: errors.New("boom")}},
		{"nil response", &goalStubDecider{}},
		{"missing answer", &goalStubDecider{resp: &TypesafeResponse{Answers: map[string]TypesafeAnswer{}}}},
		{"wrong type", &goalStubDecider{resp: &TypesafeResponse{Answers: map[string]TypesafeAnswer{"activate": {Type: "score", Score: 1}}}}},
		{"unoffered choice", &goalStubDecider{resp: pulseActivationResp("always", 1)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var client LLMClient // untyped nil: a typed nil pointer would not read as "no client"
			if tc.stub != nil {
				client = tc.stub
			}
			stubPulseActivationJudge(t, client)
			v := JudgePulseActivation(context.Background(), cfg, "wake me for failures", pulseActivationTestEvents)
			if v.Activate {
				t.Fatalf("a failed judge woke the assistant: %+v", v)
			}
			if v.Err == nil || v.Detail == "" {
				t.Fatalf("failure must carry an error and a detail, got %+v", v)
			}
		})
	}
}

// With nothing to judge the judge must not even be called.
func TestJudgePulseActivation_EmptyInputsSkipWithoutCalling(t *testing.T) {
	stub := &goalStubDecider{resp: pulseActivationResp("wake", 1)}
	stubPulseActivationJudge(t, stub)
	if v := JudgePulseActivation(context.Background(), &config.Config{}, "  ", pulseActivationTestEvents); v.Activate || v.Err != nil {
		t.Fatalf("blank condition: %+v", v)
	}
	if v := JudgePulseActivation(context.Background(), &config.Config{}, "x", nil); v.Activate || v.Err != nil {
		t.Fatalf("no events: %+v", v)
	}
	if stub.calls != 0 {
		t.Fatalf("judge called %d times for empty input, want 0", stub.calls)
	}
}

func TestJudgePulseActivation_StateCarriesConditionAndBoundedEvents(t *testing.T) {
	var got any
	stub := &goalStubDecider{resp: pulseActivationResp("skip", 1)}
	stub.onDecide = func() {}
	stubPulseActivationJudge(t, &captureDecider{goalStubDecider: stub, state: &got})

	events := make([]PulseActivationEvent, 0, pulseActivationBatch+5)
	for i := 0; i < pulseActivationBatch+5; i++ {
		events = append(events, PulseActivationEvent{Kind: "turn_done", ID: "ses", Summary: strings.Repeat("x", 2000), At: time.Now()})
	}
	JudgePulseActivation(context.Background(), &config.Config{}, "cond", events)

	state, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("state = %T, want map", got)
	}
	if state["condition"] != "cond" {
		t.Fatalf("condition = %v", state["condition"])
	}
	evs, _ := state["events"].([]map[string]any)
	if len(evs) != pulseActivationBatch {
		t.Fatalf("events = %d, want the newest %d", len(evs), pulseActivationBatch)
	}
	if s, _ := evs[0]["summary"].(string); len([]rune(s)) > pulseActivationCutRns+1 {
		t.Fatalf("summary not clipped: %d runes", len([]rune(s)))
	}
}

// captureDecider records the state a judge sent.
type captureDecider struct {
	*goalStubDecider
	state *any
}

func (c *captureDecider) DecideCtx(ctx context.Context, state any, q map[string]TypesafeQuestion) (*TypesafeResponse, error) {
	*c.state = state
	return c.goalStubDecider.DecideCtx(ctx, state, q)
}

func manyEvents(n int, kind, project, id, summary string) []PulseActivationEvent {
	out := make([]PulseActivationEvent, n)
	for i := range out {
		out[i] = PulseActivationEvent{Kind: kind, Project: project, ID: id, Summary: summary, At: time.Now()}
	}
	return out
}

// One over-long field must not push a whole batch over the request limit.
func TestPulseActivationState_ClipsEveryFreeTextField(t *testing.T) {
	long := strings.Repeat("x", 10_000)
	state, err := pulseActivationState("cond", manyEvents(3, long, long, long, long))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range state["events"].([]map[string]any) {
		if n := len([]rune(e["kind"].(string))); n > pulseActivationCutKind {
			t.Fatalf("kind not clipped: %d runes", n)
		}
		if n := len([]rune(e["project"].(string))); n > pulseActivationCutProject {
			t.Fatalf("project not clipped: %d runes", n)
		}
		if n := len([]rune(e["id"].(string))); n > pulseActivationCutID {
			t.Fatalf("id not clipped: %d runes", n)
		}
	}
	if _, clipped := state["clipped"]; clipped {
		t.Fatalf("a small batch should not need summary shrinking")
	}
}

// Marshalled size, not payload size, decides: '<' marshals to 6 bytes (\u003c),
// so a full batch of them is several times its rune count.
func TestPulseActivationState_ShrinksSummariesToFitAndSaysSo(t *testing.T) {
	events := manyEvents(PulseActivationBatchMax, "turn_done", strings.Repeat("<", 20), "ses_x", strings.Repeat("<", 200))
	state, err := pulseActivationState("cond", events)
	if err != nil {
		t.Fatal(err)
	}
	size, err := marshalledDecisionStateSize(state)
	if err != nil || size > pulseActivationStateCap {
		t.Fatalf("state marshals to %d bytes (err %v), want <= %d", size, err, pulseActivationStateCap)
	}
	if _, clipped := state["clipped"]; !clipped {
		t.Fatalf("summaries were shrunk but the judge was not told")
	}
}

// When even summary-less events do not fit, refuse instead of sending an
// over-budget state; the judge is never called and nothing wakes.
func TestJudgePulseActivation_OverBudgetBatchIsRefusedNotSent(t *testing.T) {
	stub := &goalStubDecider{resp: pulseActivationResp("wake", 1)}
	stubPulseActivationJudge(t, stub)
	hostile := strings.Repeat("<", 200)
	events := manyEvents(PulseActivationBatchMax, strings.Repeat("<", 40), strings.Repeat("<", 80), strings.Repeat("<", 120), hostile)
	v := JudgePulseActivation(context.Background(), &config.Config{}, "cond", events)
	if v.Activate || v.Err == nil || !strings.Contains(v.Detail, "cap") {
		t.Fatalf("over-budget batch: %+v", v)
	}
	if stub.calls != 0 {
		t.Fatalf("judge called %d times with an over-budget state", stub.calls)
	}
}
