package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/config"
)

// goalStubDecider is a scripted Decider standing in for the goal judge backend.
// It embeds a nil LLMClient only to satisfy the interface newClientFn returns;
// the goal judge never calls Chat, so a Chat call through it would panic.
type goalStubDecider struct {
	LLMClient
	resp     *TypesafeResponse
	err      error
	calls    int
	onDecide func()
}

func (s *goalStubDecider) Decide(_ any, _ map[string]TypesafeQuestion) (*TypesafeResponse, error) {
	s.calls++
	if s.onDecide != nil {
		s.onDecide()
	}
	return s.resp, s.err
}

func (s *goalStubDecider) DecideCtx(_ context.Context, state any, questions map[string]TypesafeQuestion) (*TypesafeResponse, error) {
	return s.Decide(state, questions)
}

func (s *goalStubDecider) GetProvider() string { return "typesafe" }
func (s *goalStubDecider) GetModel() string    { return "jev-latest" }

// newGoalTestAgent wires stub in as the goal judge. A nil stub makes
// newClientFn return no client at all, which is the "no judge credential" case.
// Tests that use it must not run in parallel: newClientFn is a package var.
func newGoalTestAgent(t *testing.T, stub *goalStubDecider) *Agent {
	t.Helper()
	cfg := &config.Config{}
	cfg.Ocode.Goal.JudgeModel = "typesafe/jev-latest"
	a := newTestAgent(nil, nil, cfg, nil)
	prev := newClientFn
	t.Cleanup(func() { newClientFn = prev })
	newClientFn = func(_ *config.Config, _ string) LLMClient {
		if stub == nil {
			return nil
		}
		return stub
	}
	return a
}

func goalVerdictResp(verdict string, confidence float64, reason string) *TypesafeResponse {
	return &TypesafeResponse{
		Model: "jev-latest",
		Answers: map[string]TypesafeAnswer{
			"verdict": {Type: "choice", Choice: verdict, Confidence: confidence},
			"reason":  {Type: "choice", Choice: reason, Confidence: 0.8},
		},
		Usage: TypesafeUsage{InputTokens: 7, OutputTokens: 2},
	}
}

// goalTranscriptNoTools ends on a claim with no tool call after the last user turn.
func goalTranscriptNoTools() []Message {
	return []Message{
		{Role: "user", Content: "make the tests pass"},
		{Role: "assistant", Content: "Done, all tests pass."},
	}
}

// goalTranscriptWithTool runs a tool after the last user turn.
func goalTranscriptWithTool() []Message {
	return []Message{
		{Role: "user", Content: "make the tests pass"},
		{Role: "assistant", Content: "Running them.", ToolCalls: []ToolCall{{ID: "t1", Type: "function"}}},
		{Role: "tool", Content: "ok", ToolID: "t1"},
		{Role: "assistant", Content: "Done."},
	}
}

func TestSetGoalValidation(t *testing.T) {
	a := newGoalTestAgent(t, nil)
	if err := a.SetGoal(""); err == nil {
		t.Fatal("empty condition: want error")
	}
	if err := a.SetGoal("   "); err == nil {
		t.Fatal("whitespace-only condition: want error")
	}
	if err := a.SetGoal(strings.Repeat("x", GoalMaxConditionLen+1)); err == nil {
		t.Fatal("4001-char condition: want error")
	}
	if err := a.SetGoal(strings.Repeat("x", GoalMaxConditionLen)); err != nil {
		t.Fatalf("4000-char condition: unexpected error %v", err)
	}

	a.RestoreGoal(GoalState{Condition: "old", Status: GoalPaused, Turns: 5, NoToolTurns: 2, SetAt: time.Now()})
	if err := a.SetGoal("  fresh  "); err != nil {
		t.Fatalf("SetGoal: %v", err)
	}
	gs, ok := a.Goal()
	if !ok {
		t.Fatal("Goal() ok=false after SetGoal")
	}
	if gs.Condition != "fresh" || gs.Status != GoalActive || gs.Turns != 0 || gs.NoToolTurns != 0 {
		t.Fatalf("SetGoal did not reset state: %+v", gs)
	}
}

func TestGoalJudge_MetEndsGoal(t *testing.T) {
	stub := &goalStubDecider{resp: goalVerdictResp("met", 0.9, "all_satisfied")}
	a := newGoalTestAgent(t, stub)
	if err := a.SetGoal("tests pass"); err != nil {
		t.Fatal(err)
	}
	res := a.GoalJudgeSync(goalTranscriptWithTool())
	if res.Err != nil || res.Verdict != "met" || res.Status != GoalMet || res.Resume {
		t.Fatalf("met verdict: got %+v", res)
	}
	if a.GoalEvaluable() {
		t.Fatal("met goal must not be evaluable")
	}
}

func TestGoalJudge_NotMetResumes(t *testing.T) {
	stub := &goalStubDecider{resp: goalVerdictResp("not_met", 0.9, "work_remains")}
	a := newGoalTestAgent(t, stub)
	if err := a.SetGoal("tests pass"); err != nil {
		t.Fatal(err)
	}
	res := a.GoalJudgeSync(goalTranscriptWithTool())
	if !res.Resume || res.Verdict != "not_met" {
		t.Fatalf("not_met verdict: got %+v", res)
	}
	gs, _ := a.Goal()
	if gs.Turns != 1 || gs.Status != GoalActive {
		t.Fatalf("state after not_met: %+v", gs)
	}
}

func TestGoalJudge_ImpossibleEndsGoal(t *testing.T) {
	stub := &goalStubDecider{resp: goalVerdictResp("impossible", 0.9, "cannot_be_done")}
	a := newGoalTestAgent(t, stub)
	if err := a.SetGoal("reach the moon"); err != nil {
		t.Fatal(err)
	}
	res := a.GoalJudgeSync(goalTranscriptWithTool())
	if res.Status != GoalImpossible || res.Resume {
		t.Fatalf("impossible verdict: got %+v", res)
	}
}

func TestGoalJudge_LowConfidenceMetTreatedNotMet(t *testing.T) {
	stub := &goalStubDecider{resp: goalVerdictResp("met", 0.4, "all_satisfied")}
	a := newGoalTestAgent(t, stub)
	if err := a.SetGoal("tests pass"); err != nil {
		t.Fatal(err)
	}
	res := a.GoalJudgeSync(goalTranscriptWithTool())
	if !res.Resume {
		t.Fatalf("low-confidence met must resume: got %+v", res)
	}
	if !strings.Contains(res.Detail, "0.60") {
		t.Fatalf("detail should name the confidence floor: %q", res.Detail)
	}
}

func TestGoalJudge_AwaitingUserPauses(t *testing.T) {
	stub := &goalStubDecider{resp: goalVerdictResp("not_met", 0.9, "awaiting_user")}
	a := newGoalTestAgent(t, stub)
	if err := a.SetGoal("tests pass"); err != nil {
		t.Fatal(err)
	}
	res := a.GoalJudgeSync(goalTranscriptWithTool())
	if res.Resume || res.Status != GoalPaused {
		t.Fatalf("awaiting_user must pause: got %+v", res)
	}
}

// A below-floor verdict is treated as not_met, and that not_met still pauses on awaiting_user.
func TestGoalJudge_LowConfidenceAwaitingUserPauses(t *testing.T) {
	stub := &goalStubDecider{resp: goalVerdictResp("met", 0.4, "awaiting_user")}
	a := newGoalTestAgent(t, stub)
	if err := a.SetGoal("tests pass"); err != nil {
		t.Fatal(err)
	}
	res := a.GoalJudgeSync(goalTranscriptWithTool())
	if res.Resume || res.Status != GoalPaused || res.Verdict != "not_met" {
		t.Fatalf("below-floor met with awaiting_user must pause: got %+v", res)
	}
}

func TestGoalJudge_NoDeciderPauses(t *testing.T) {
	a := newGoalTestAgent(t, nil)
	if err := a.SetGoal("tests pass"); err != nil {
		t.Fatal(err)
	}
	res := a.GoalJudgeSync(goalTranscriptWithTool())
	if res.Verdict != "unavailable" || res.Status != GoalPaused {
		t.Fatalf("no decider: got %+v", res)
	}
	gs, ok := a.Goal()
	if !ok || gs.Condition != "tests pass" {
		t.Fatalf("condition must be retained: %+v ok=%v", gs, ok)
	}
}

func TestGoalJudge_ErrorPauses(t *testing.T) {
	stub := &goalStubDecider{err: context.DeadlineExceeded}
	a := newGoalTestAgent(t, stub)
	if err := a.SetGoal("tests pass"); err != nil {
		t.Fatal(err)
	}
	res := a.GoalJudgeSync(goalTranscriptWithTool())
	if res.Err == nil || res.Status != GoalPaused {
		t.Fatalf("judge error must pause with Err set: got %+v", res)
	}
}

func TestGoalJudge_StallPausesAfterThreeNoToolTurns(t *testing.T) {
	stub := &goalStubDecider{resp: goalVerdictResp("not_met", 0.9, "unverified_claim")}
	a := newGoalTestAgent(t, stub)
	if err := a.SetGoal("tests pass"); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 2; i++ {
		if res := a.GoalJudgeSync(goalTranscriptNoTools()); res.Verdict == "stalled" {
			t.Fatalf("turn %d: stalled too early", i)
		}
	}
	if res := a.GoalJudgeSync(goalTranscriptNoTools()); res.Verdict != "stalled" {
		t.Fatalf("turn 3: want stalled, got %+v", res)
	}

	// A tool call in between resets the counter, so two more no-tool turns do not stall.
	a = newGoalTestAgent(t, stub)
	if err := a.SetGoal("tests pass"); err != nil {
		t.Fatal(err)
	}
	a.GoalJudgeSync(goalTranscriptNoTools())
	a.GoalJudgeSync(goalTranscriptNoTools())
	a.GoalJudgeSync(goalTranscriptWithTool())
	a.GoalJudgeSync(goalTranscriptNoTools())
	if res := a.GoalJudgeSync(goalTranscriptNoTools()); res.Verdict == "stalled" {
		t.Fatalf("tool call did not reset the stall counter: %+v", res)
	}
}

// A not_met at the cap pauses instead of resuming. The judge still runs on the
// capped turn, so a met verdict on that turn is honoured.
func TestGoalJudge_TurnCapPauses(t *testing.T) {
	stub := &goalStubDecider{resp: goalVerdictResp("not_met", 0.9, "work_remains")}
	a := newGoalTestAgent(t, stub)
	a.RestoreGoal(GoalState{Condition: "tests pass", Status: GoalActive, Turns: GoalMaxTurns - 1, SetAt: time.Now()})
	res := a.GoalJudgeSync(goalTranscriptWithTool())
	if res.Verdict != "capped" || res.Status != GoalPaused || res.Resume {
		t.Fatalf("turn cap: got %+v", res)
	}
	if stub.calls != 1 {
		t.Fatalf("capped turn must still be judged, calls=%d", stub.calls)
	}
	gs, ok := a.Goal()
	if !ok || gs.Status != GoalPaused || gs.Turns != GoalMaxTurns {
		t.Fatalf("stored goal after cap: %+v ok=%v", gs, ok)
	}
	if !strings.Contains(gs.LastReason, "50-turn cap") {
		t.Fatalf("cap reason should name the cap: %q", gs.LastReason)
	}
}

// A goal finished on its last turn closes as met, not capped.
func TestGoalJudge_MetAtCapIsTerminal(t *testing.T) {
	stub := &goalStubDecider{resp: goalVerdictResp("met", 0.9, "all_satisfied")}
	a := newGoalTestAgent(t, stub)
	a.RestoreGoal(GoalState{Condition: "tests pass", Status: GoalActive, Turns: GoalMaxTurns - 1, SetAt: time.Now()})
	res := a.GoalJudgeSync(goalTranscriptWithTool())
	if res.Verdict != "met" || res.Status != GoalMet || res.Resume {
		t.Fatalf("met at cap: got %+v", res)
	}
	if a.GoalEvaluable() {
		t.Fatal("met goal at cap must not be evaluable")
	}
}

// A downgraded met must not tell the model the judge was sure of it: the resume
// text carries the confidence-floor reason, not the judge's reason label.
func TestGoalJudge_DowngradedMetResumeTextSaysUnconfirmed(t *testing.T) {
	stub := &goalStubDecider{resp: goalVerdictResp("met", 0.4, "all_satisfied")}
	a := newGoalTestAgent(t, stub)
	if err := a.SetGoal("tests pass"); err != nil {
		t.Fatal(err)
	}
	res := a.GoalJudgeSync(goalTranscriptWithTool())
	if !res.Resume {
		t.Fatalf("downgraded met must resume: got %+v", res)
	}
	msg := a.GoalResumeMessage().Content
	if !strings.Contains(msg, goalUnconfirmedReason) {
		t.Fatalf("resume text should say the verdict was unconfirmed: %q", msg)
	}
	if strings.Contains(msg, "every part of the condition is satisfied") {
		t.Fatalf("resume text must not carry the judge's met reason: %q", msg)
	}
}

func TestGoalJudge_ClearDuringJudgeIsNotResurrected(t *testing.T) {
	stub := &goalStubDecider{resp: goalVerdictResp("not_met", 0.9, "work_remains")}
	a := newGoalTestAgent(t, stub)
	if err := a.SetGoal("tests pass"); err != nil {
		t.Fatal(err)
	}
	stub.onDecide = a.ClearGoal
	res := a.GoalJudgeSync(goalTranscriptWithTool())
	if res.Verdict != "stale" || res.Status != "" || res.Resume {
		t.Fatalf("verdict for a cleared goal must be stale with no status and no resume: %+v", res)
	}
	if _, ok := a.Goal(); ok {
		t.Fatal("cleared goal was written back by the judge")
	}
}

// A human turn that reactivates the goal while the judge is in flight supersedes
// the judged turn: the verdict is dropped as stale, and the goal stays active with
// the human turn's bookkeeping, not the dropped verdict's.
func TestGoalJudge_HumanTurnDuringJudgeDropsVerdict(t *testing.T) {
	stub := &goalStubDecider{resp: goalVerdictResp("not_met", 0.9, "work_remains")}
	a := newGoalTestAgent(t, stub)
	a.RestoreGoal(GoalState{Condition: "tests pass", Status: GoalPaused, Turns: 4, NoToolTurns: 1, SetAt: time.Now()})
	stub.onDecide = a.GoalMarkHumanTurn
	res := a.GoalJudgeSync(goalTranscriptWithTool())
	if stub.calls != 1 {
		t.Fatalf("judge should have been called once, calls=%d", stub.calls)
	}
	if res.Verdict != "stale" || res.Resume || res.Status != GoalActive {
		t.Fatalf("interleaved human turn: want stale/active/no-resume, got %+v", res)
	}
	gs, ok := a.Goal()
	if !ok || gs.Status != GoalActive || gs.Turns != 4 || gs.NoToolTurns != 0 {
		t.Fatalf("goal after dropped verdict: %+v ok=%v (want active, Turns=4, NoToolTurns=0)", gs, ok)
	}
}

// A panic inside the judge still delivers an unavailable result, so the host
// (/rc in particular) is never left waiting on OnGoalJudge.
func TestGoalJudgeAsync_PanicDeliversUnavailable(t *testing.T) {
	stub := &goalStubDecider{resp: goalVerdictResp("not_met", 0.9, "work_remains")}
	stub.onDecide = func() { panic("judge exploded") }
	a := newGoalTestAgent(t, stub)
	if err := a.SetGoal("tests pass"); err != nil {
		t.Fatal(err)
	}
	done := make(chan GoalJudgeResult, 1)
	a.OnGoalJudge = func(r GoalJudgeResult) { done <- r }
	if !a.GoalJudgeAsync(goalTranscriptWithTool(), 3) {
		t.Fatal("async judge should dispatch")
	}
	select {
	case r := <-done:
		if r.Gen != 3 || r.Verdict != "unavailable" || r.Resume {
			t.Fatalf("panic result: %+v", r)
		}
		if !strings.Contains(r.Detail, "judge exploded") {
			t.Fatalf("panic detail should carry the panic: %q", r.Detail)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("OnGoalJudge not called after a judge panic")
	}
}

func TestGoalJudgeAsync_DeliversGen(t *testing.T) {
	stub := &goalStubDecider{resp: goalVerdictResp("not_met", 0.9, "work_remains")}
	a := newGoalTestAgent(t, stub)
	if a.GoalJudgeAsync(goalTranscriptWithTool(), 1) {
		t.Fatal("no goal set: async judge must not dispatch")
	}
	if err := a.SetGoal("tests pass"); err != nil {
		t.Fatal(err)
	}
	done := make(chan GoalJudgeResult, 1)
	a.OnGoalJudge = func(r GoalJudgeResult) { done <- r }
	if !a.GoalJudgeAsync(goalTranscriptWithTool(), 7) {
		t.Fatal("async judge should dispatch")
	}
	select {
	case r := <-done:
		if r.Gen != 7 || !r.Resume {
			t.Fatalf("async result: %+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("OnGoalJudge not called")
	}
}

func TestGoalMarkHumanTurnReactivatesPaused(t *testing.T) {
	a := newGoalTestAgent(t, nil)
	if err := a.SetGoal("tests pass"); err != nil {
		t.Fatal(err)
	}
	a.RestoreGoal(GoalState{Condition: "tests pass", Status: GoalPaused, NoToolTurns: 2, SetAt: time.Now()})
	a.GoalMarkHumanTurn()
	gs, _ := a.Goal()
	if gs.Status != GoalActive || gs.NoToolTurns != 0 {
		t.Fatalf("paused goal not reactivated: %+v", gs)
	}

	a.RestoreGoal(GoalState{Condition: "tests pass", Status: GoalMet, SetAt: time.Now()})
	a.GoalMarkHumanTurn()
	if gs, _ := a.Goal(); gs.Status != GoalMet {
		t.Fatalf("met goal must stay met: %+v", gs)
	}
}

func TestGoalResumeMessageIsUserRoleWithMarker(t *testing.T) {
	stub := &goalStubDecider{resp: goalVerdictResp("not_met", 0.9, "work_remains")}
	a := newGoalTestAgent(t, stub)
	if err := a.SetGoal("ship it"); err != nil {
		t.Fatal(err)
	}
	a.GoalJudgeSync(goalTranscriptWithTool())
	msg := a.GoalResumeMessage()
	if msg.Role != "user" {
		t.Fatalf("role = %q, want user", msg.Role)
	}
	if !strings.HasPrefix(msg.Content, "[ocode:goal]") {
		t.Fatalf("content missing marker: %q", msg.Content)
	}
	if !strings.Contains(msg.Content, "ship it") {
		t.Fatalf("content missing condition: %q", msg.Content)
	}
}

func TestGoalMetadataRoundTrip(t *testing.T) {
	orig := GoalState{
		Condition:   "ship it",
		Status:      GoalPaused,
		Turns:       7,
		NoToolTurns: 2,
		LastReason:  "no tool use",
		SetAt:       time.Now().Truncate(time.Second),
	}
	raw, err := json.Marshal(GoalStateToMetadata(orig))
	if err != nil {
		t.Fatal(err)
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatal(err)
	}
	got, ok := GoalStateFromMetadata(meta)
	if !ok {
		t.Fatalf("GoalStateFromMetadata ok=false for %s", raw)
	}
	if got.Condition != orig.Condition || got.Status != orig.Status || got.Turns != orig.Turns ||
		got.NoToolTurns != orig.NoToolTurns || got.LastReason != orig.LastReason || !got.SetAt.Equal(orig.SetAt) {
		t.Fatalf("round trip mismatch:\n got  %+v\n want %+v", got, orig)
	}
}

// A not_met verdict on a non-human turn must not reactivate a paused goal: only
// GoalMarkHumanTurn does that. The verdict is recorded, but nothing resumes.
func TestGoalJudge_PausedGoalNotMetStaysPausedWithoutResume(t *testing.T) {
	stub := &goalStubDecider{resp: goalVerdictResp("not_met", 0.9, "work_remains")}
	a := newGoalTestAgent(t, stub)
	a.RestoreGoal(GoalState{Condition: "tests pass", Status: GoalPaused, Turns: 2, SetAt: time.Now()})

	res := a.GoalJudgeSync(goalTranscriptWithTool())
	if res.Verdict != "not_met" || res.Resume {
		t.Fatalf("paused goal verdict = %+v, want not_met without resume", res)
	}
	if gs, _ := a.Goal(); gs.Status != GoalPaused {
		t.Fatalf("not_met on a non-human turn reactivated the paused goal: %+v", gs)
	}
}

// A judge panic pauses the goal as unavailable, so the host never reports a
// paused goal while the stored goal is still active.
func TestGoalJudge_PanicPausesGoal(t *testing.T) {
	stub := &goalStubDecider{resp: goalVerdictResp("not_met", 0.9, "work_remains")}
	stub.onDecide = func() { panic("judge exploded") }
	a := newGoalTestAgent(t, stub)
	if err := a.SetGoal("tests pass"); err != nil {
		t.Fatal(err)
	}

	res := a.GoalJudgeSync(goalTranscriptWithTool())
	if res.Verdict != "unavailable" || res.Resume {
		t.Fatalf("panic verdict = %+v, want unavailable without resume", res)
	}
	gs, _ := a.Goal()
	if gs.Status != GoalPaused {
		t.Fatalf("panicked judge left the goal %s, want paused", gs.Status)
	}
	if !strings.Contains(gs.LastReason, "judge exploded") {
		t.Fatalf("paused goal reason should carry the panic, got %q", gs.LastReason)
	}
}

// A present but malformed goal record is rejected, not restored half-formed.
func TestGoalStateFromMetadataRejectsMalformedRecord(t *testing.T) {
	bad := map[string]any{GoalMetadataKey: map[string]any{
		"condition":     "ship it",
		"status":        "bogus",
		"turns":         1.0,
		"no_tool_turns": 0.0,
		"set_at":        "not-a-time",
	}}
	if _, ok := GoalStateFromMetadata(bad); ok {
		t.Fatal("malformed goal record was restored")
	}
	if _, ok := GoalStateFromMetadata(map[string]any{GoalMetadataKey: "oops"}); ok {
		t.Fatal("non-object goal record was restored")
	}
}
