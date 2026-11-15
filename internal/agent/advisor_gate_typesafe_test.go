package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/snapshot"
	"github.com/u007/ocode/internal/tool"
)

// gateStub answers the advisor gate question. answer picks the response from
// the moment, so one test can script before_commit and end_of_turn separately.
// Any other question (a content or permission judge sharing this client) gets
// an error, so it takes its own fallback rather than a gate answer.
type gateStub struct {
	LLMClient
	answer func(moment string) (*TypesafeResponse, error)
	calls  map[string]int
}

func (g *gateStub) Decide(state any, qs map[string]TypesafeQuestion) (*TypesafeResponse, error) {
	if _, ok := qs[advisorGateKey]; !ok {
		return nil, errors.New("not an advisor gate question")
	}
	moment, _ := state.(map[string]any)["moment"].(string)
	if g.calls == nil {
		g.calls = map[string]int{}
	}
	g.calls[moment]++
	if g.answer == nil {
		return nil, errors.New("no scripted answer")
	}
	return g.answer(moment)
}

func (g *gateStub) DecideCtx(_ context.Context, state any, qs map[string]TypesafeQuestion) (*TypesafeResponse, error) {
	return g.Decide(state, qs)
}

func (g *gateStub) GetProvider() string { return "typesafe" }
func (g *gateStub) GetModel() string    { return "jev-latest" }

// gateChoice is a gate answer that chooses one option with the given probability.
func gateChoice(choice string, prob float64) *TypesafeResponse {
	return &TypesafeResponse{
		Model: "jev-latest",
		Answers: map[string]TypesafeAnswer{
			advisorGateKey: {
				Type:          "choice",
				Choice:        choice,
				Probabilities: map[string]float64{choice: prob, "other": 1 - prob},
				Confidence:    0.9,
			},
		},
		Usage: TypesafeUsage{InputTokens: 5, OutputTokens: 1},
	}
}

// gateByMoment returns the same answer at every moment.
func gateByMoment(resp *TypesafeResponse, err error) func(string) (*TypesafeResponse, error) {
	return func(string) (*TypesafeResponse, error) { return resp, err }
}

// gateTestAgent is a checkpoint test agent whose gate judge is stub. A nil stub
// means no judge is configured.
func gateTestAgent(t *testing.T, stub *gateStub) (*Agent, *checkpointFakeAdvisor) {
	t.Helper()
	a, fake := checkpointTestAgent(t, []string{"done"})
	// checkpointTestAgent installs a no-client stub and a restore cleanup of its
	// own; save THAT value so this override is undone in the same order instead
	// of relying on that helper continuing to touch newClientFn.
	prev := newClientFn
	t.Cleanup(func() { newClientFn = prev })
	newClientFn = func(*config.Config, string) LLMClient {
		if stub == nil {
			return nil
		}
		return stub
	}
	return a, fake
}

// gateRecordingTool counts its executions. parallel makes it a parallel-class
// tool, which is what the DAG gate inspects.
type gateRecordingTool struct {
	name     string
	calls    int
	parallel bool
}

func (r *gateRecordingTool) Name() string        { return r.name }
func (r *gateRecordingTool) Description() string { return r.name }
func (r *gateRecordingTool) Definition() map[string]interface{} {
	return map[string]interface{}{"name": r.name}
}
func (r *gateRecordingTool) Parallel() bool { return r.parallel }
func (r *gateRecordingTool) Execute(json.RawMessage) (string, error) {
	r.calls++
	return "ok", nil
}

// gateCancellingAdvisor cancels the agent while it runs, then returns advice.
type gateCancellingAdvisor struct {
	checkpointFakeAdvisor
	a *Agent
}

func (c *gateCancellingAdvisor) Execute(args json.RawMessage) (string, error) {
	c.a.Cancel()
	return c.checkpointFakeAdvisor.Execute(args)
}

const gateCommitArgs = `{"command":"git add -A && git commit -m fix"}`

func gateWriteCall(id string) ToolCall {
	return namedToolCall(id, "write", `{"path":"a.go","content":"x"}`)
}

// assertOneToolMessagePerCall checks every tool call got exactly one tool message.
func assertOneToolMessagePerCall(t *testing.T, msgs []Message, calls []ToolCall) {
	t.Helper()
	seen := map[string]int{}
	for _, m := range msgs {
		if m.Role == "tool" {
			seen[m.ToolID]++
		}
	}
	for _, tc := range calls {
		if seen[tc.ID] != 1 {
			t.Errorf("tool call %s has %d tool messages, want exactly 1", tc.ID, seen[tc.ID])
		}
	}
}

func TestAdvisorGate_EndOfTurnSkipChoicesSkipTheReview(t *testing.T) {
	skips := []string{
		advisorGateChoiceSkipReviewed,
		advisorGateChoiceSkipDeclined,
		advisorGateChoiceSkipNotSubst,
		advisorGateChoiceSkipUnfinished,
	}
	for _, choice := range skips {
		t.Run(choice, func(t *testing.T) {
			t.Setenv("OPENCODE_ADVISOR_MODEL", "")
			stub := &gateStub{answer: gateByMoment(gateChoice(choice, 0.9), nil)}
			a, fake := gateTestAgent(t, stub)
			st := &advisorCheckpointState{userGoal: "fix it"}
			st.countBatch([]ToolCall{gateWriteCall("tc1")})

			if f := a.advisorDoneCheckpoint(st, &Message{Role: "assistant", Content: "Done."}); f != nil {
				t.Fatalf("%s must skip the review, got a follow-up", choice)
			}
			if fake.calls != 0 {
				t.Fatalf("advisor ran %d time(s) after %s", fake.calls, choice)
			}
			if !st.doneChecked {
				t.Fatal("a decided skip must mark the done checkpoint checked")
			}
			if stub.calls[advisorMomentEndOfTurn] != 1 {
				t.Fatalf("gate consulted %d times at end_of_turn, want 1", stub.calls[advisorMomentEndOfTurn])
			}
		})
	}
}

func TestAdvisorGate_EndOfTurnReviewNowRunsAndRecordsTheReview(t *testing.T) {
	t.Setenv("OPENCODE_ADVISOR_MODEL", "")
	stub := &gateStub{answer: gateByMoment(gateChoice(advisorGateChoiceReview, 0.9), nil)}
	a, fake := gateTestAgent(t, stub)
	fake.advice = "gap: tests not run"
	st := &advisorCheckpointState{userGoal: "fix it"}
	st.countBatch([]ToolCall{gateWriteCall("tc1")})

	f := a.advisorDoneCheckpoint(st, &Message{Role: "assistant", Content: "Done, bug fixed."})
	if f == nil || !strings.Contains(f.Content, "gap: tests not run") {
		t.Fatalf("review_now must inject the advice, got %+v", f)
	}
	if fake.calls != 1 {
		t.Fatalf("advisor calls = %d, want 1", fake.calls)
	}
	if len(a.advisorReviews) != 1 || a.advisorReviews[0].Moment != advisorMomentEndOfTurn || a.advisorReviews[0].Verdict != "gap: tests not run" {
		t.Fatalf("ledger = %+v, want one end_of_turn entry with the verdict", a.advisorReviews)
	}
}

// TestAdvisorGate_UndecidedAnswersFallBackToThreshold checks each way the gate
// can fail to decide. With a write the threshold fires, so a review runs. With
// one read the threshold stays quiet, so no review runs.
func TestAdvisorGate_UndecidedAnswersFallBackToThreshold(t *testing.T) {
	cases := []struct {
		name string
		stub *gateStub
	}{
		{"skip below floor", &gateStub{answer: gateByMoment(gateChoice(advisorGateChoiceSkipNotSubst, 0.5), nil)}},
		{"judge error", &gateStub{answer: gateByMoment(nil, errors.New("timeout"))}},
		{"nil response", &gateStub{answer: gateByMoment(nil, nil)}},
		{"answer missing", &gateStub{answer: gateByMoment(&TypesafeResponse{Answers: map[string]TypesafeAnswer{}}, nil)}},
		{"choice not offered", &gateStub{answer: gateByMoment(gateChoice("maybe", 0.9), nil)}},
		{"no judge configured", nil},
	}
	for _, tc := range cases {
		for _, positive := range []bool{true, false} {
			name := tc.name + "/read-only"
			if positive {
				name = tc.name + "/write"
			}
			t.Run(name, func(t *testing.T) {
				t.Setenv("OPENCODE_ADVISOR_MODEL", "")
				a, fake := gateTestAgent(t, tc.stub)
				st := &advisorCheckpointState{userGoal: "fix it"}
				if positive {
					st.countBatch([]ToolCall{gateWriteCall("tc1")})
				} else {
					st.countBatch([]ToolCall{namedToolCall("tc1", "read", `{}`)})
				}
				f := a.advisorDoneCheckpoint(st, &Message{Role: "assistant", Content: "Done."})
				if positive && (f == nil || fake.calls != 1) {
					t.Fatalf("threshold met: want a review, got follow-up=%v calls=%d", f != nil, fake.calls)
				}
				if !positive && (f != nil || fake.calls != 0) {
					t.Fatalf("threshold not met: want no review, got follow-up=%v calls=%d", f != nil, fake.calls)
				}
			})
		}
	}
}

// TestAdvisorGate_NoSessionChangesDoesNotAskTheJudge checks the short-circuit
// before the gate: a snapshot store with no changed file and no write this Step
// means no review and no judge call, even when the tool-call threshold is met.
func TestAdvisorGate_NoSessionChangesDoesNotAskTheJudge(t *testing.T) {
	t.Setenv("OPENCODE_ADVISOR_MODEL", "")
	stub := &gateStub{answer: gateByMoment(gateChoice(advisorGateChoiceReview, 0.9), nil)}
	a, fake := gateTestAgent(t, stub)
	a.snapshotStore = snapshot.NewStore("gate-test", t.TempDir())

	st := &advisorCheckpointState{userGoal: "explain the code"}
	reads := make([]ToolCall, doneCheckpointMinToolCalls+1)
	for i := range reads {
		reads[i] = namedToolCall(fmt.Sprintf("r%d", i), "read", `{}`)
	}
	st.countBatch(reads)

	if f := a.advisorDoneCheckpoint(st, &Message{Role: "assistant", Content: "The code does X."}); f != nil {
		t.Fatal("a read-only session with no changed files must not be reviewed")
	}
	if n := stub.calls[advisorMomentEndOfTurn]; n != 0 {
		t.Fatalf("judge consulted %d time(s) with no session changes, want 0", n)
	}
	if fake.calls != 0 {
		t.Fatalf("advisor ran %d time(s) with no session changes", fake.calls)
	}
}

// TestAdvisorGate_PanicInJudgeIsNotDecided checks that a panicking judge stays
// inside the gate: the caller falls back to the threshold and the turn goes on.
func TestAdvisorGate_PanicInJudgeIsNotDecided(t *testing.T) {
	t.Setenv("OPENCODE_ADVISOR_MODEL", "")
	stub := &gateStub{answer: func(string) (*TypesafeResponse, error) { panic("judge exploded") }}
	a, fake := gateTestAgent(t, stub)
	st := &advisorCheckpointState{userGoal: "fix it"}
	st.countBatch([]ToolCall{gateWriteCall("tc1")})

	v := a.advisorGateDecide(st, advisorMomentEndOfTurn, "Done.")
	if v.Decided {
		t.Fatal("a panicking judge must not produce a decision")
	}
	if !strings.Contains(v.Detail, "judge exploded") {
		t.Fatalf("detail = %q, want the panic message", v.Detail)
	}
	if f := a.advisorDoneCheckpoint(st, &Message{Role: "assistant", Content: "Done."}); f == nil || fake.calls != 1 {
		t.Fatalf("threshold fallback after a panic: follow-up=%v calls=%d, want a review", f != nil, fake.calls)
	}
}

// TestAdvisorCommitHold_UndecidedOrSkippedRunsCommit: each case below must leave
// the commit unheld, with the judge consulted at most once for the turn.
func TestAdvisorCommitHold_UndecidedOrSkippedRunsCommit(t *testing.T) {
	cases := []struct {
		name string
		stub *gateStub
	}{
		{"skip above floor", &gateStub{answer: gateByMoment(gateChoice(advisorGateChoiceSkipNotSubst, 0.9), nil)}},
		{"skip below floor", &gateStub{answer: gateByMoment(gateChoice(advisorGateChoiceSkipNotSubst, 0.3), nil)}},
		{"skip_work_unfinished before commit", &gateStub{answer: gateByMoment(gateChoice(advisorGateChoiceSkipUnfinished, 0.9), nil)}},
		{"judge error", &gateStub{answer: gateByMoment(nil, errors.New("down"))}},
		{"invalid choice", &gateStub{answer: gateByMoment(gateChoice("maybe", 0.9), nil)}},
		{"no judge configured", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OPENCODE_ADVISOR_MODEL", "")
			a, fake := gateTestAgent(t, tc.stub)
			resp := &Message{Role: "assistant", Content: "committing", ToolCalls: []ToolCall{
				gateWriteCall("tc1"),
				namedToolCall("tc2", "bash", gateCommitArgs),
			}}
			st := a.newAdvisorCheckpointState("fix it", false, false)
			st.countBatch(resp.ToolCalls)

			if held := a.advisorCommitHold(st, resp, nil); held != nil {
				t.Fatalf("undecided or skipped gate must not hold the commit, held %v", held)
			}
			if fake.calls != 0 {
				t.Fatalf("advisor ran %d time(s) without a review_now", fake.calls)
			}
			if !st.commitAsked {
				t.Fatal("the gate was consulted, so commitAsked must be set")
			}
			if tc.stub != nil && tc.stub.calls[advisorMomentBeforeCommit] != 1 {
				t.Fatalf("gate consulted %d times, want 1", tc.stub.calls[advisorMomentBeforeCommit])
			}
		})
	}
}

// TestAdvisorCommitHold_RunsOnlyWhenACommitIsInTheBatch checks the trigger: a
// batch with no git commit never reaches the judge.
func TestAdvisorCommitHold_RunsOnlyWhenACommitIsInTheBatch(t *testing.T) {
	t.Setenv("OPENCODE_ADVISOR_MODEL", "")
	stub := &gateStub{answer: gateByMoment(gateChoice(advisorGateChoiceReview, 0.9), nil)}
	a, _ := gateTestAgent(t, stub)
	resp := &Message{Role: "assistant", ToolCalls: []ToolCall{
		gateWriteCall("tc1"),
		namedToolCall("tc2", "bash", `{"command":"go test ./..."}`),
	}}
	st := a.newAdvisorCheckpointState("fix it", false, false)
	st.countBatch(resp.ToolCalls)
	if held := a.advisorCommitHold(st, resp, nil); held != nil {
		t.Fatal("a batch without a commit must not be held")
	}
	if st.commitAsked || stub.calls[advisorMomentBeforeCommit] != 0 {
		t.Fatal("the gate must not be consulted for a batch without a commit")
	}
}

// TestAdvisorCommitHold_SkillSuppressedStandsDown checks that a skill which opts
// out of the advisor (skillSuppressed) stops the commit hold before any judge is
// asked, even when the judge would answer review_now at probability 1.0 for a
// batch with a git commit and session changes. The control runs the identical
// setup without the suppression and must hold the commit.
func TestAdvisorCommitHold_SkillSuppressedStandsDown(t *testing.T) {
	run := func(t *testing.T, suppressed bool) (map[int]Message, *gateStub, *checkpointFakeAdvisor, *advisorCheckpointState) {
		t.Helper()
		t.Setenv("OPENCODE_ADVISOR_MODEL", "")
		stub := &gateStub{answer: gateByMoment(gateChoice(advisorGateChoiceReview, 1.0), nil)}
		a, fake := gateTestAgent(t, stub)
		fake.advice = "gap: the migration is untested"
		td := t.TempDir()
		store := snapshot.NewStore("gate-test", filepath.Join(td, "snaps"))
		a.snapshotStore = store
		if err := store.Backup(filepath.Join(td, "a.go"), "tc1"); err != nil {
			t.Fatal(err)
		}
		resp := &Message{Role: "assistant", Content: "committing", ToolCalls: []ToolCall{
			gateWriteCall("tc1"),
			namedToolCall("tc2", "bash", gateCommitArgs),
		}}
		st := a.newAdvisorCheckpointState("fix it", false, false)
		st.countBatch(resp.ToolCalls)
		st.skillSuppressed = suppressed
		return a.advisorCommitHold(st, resp, nil), stub, fake, st
	}

	held, stub, fake, st := run(t, true)
	if held != nil {
		t.Fatalf("skillSuppressed must stand the commit hold down, held %v", held)
	}
	if len(stub.calls) != 0 {
		t.Fatalf("judge consulted %v under skillSuppressed, want no calls", stub.calls)
	}
	if fake.calls != 0 {
		t.Fatalf("advisor ran %d time(s) under skillSuppressed", fake.calls)
	}
	if st.commitAsked {
		t.Fatal("commitAsked must stay false when the hold stands down before the gate")
	}

	// Control: the same setup without the suppression must hold the commit, so
	// the assertions above cannot pass vacuously.
	held, stub, _, st = run(t, false)
	if len(held) == 0 {
		t.Fatal("control: identical setup without skillSuppressed must hold the commit")
	}
	if stub.calls[advisorMomentBeforeCommit] != 1 || !st.commitAsked {
		t.Fatalf("control: gate calls=%d commitAsked=%v, want the gate consulted once", stub.calls[advisorMomentBeforeCommit], st.commitAsked)
	}
}

// TestAdvisorCommitHold_ReviewNowHoldsCommitInStep drives a real Step. The
// commit is withheld with the review message. The other calls in the batch still
// run, each gets exactly one tool message, and a commit re-issued later in the
// same turn runs without a second gate call.
func TestAdvisorCommitHold_ReviewNowHoldsCommitInStep(t *testing.T) {
	t.Setenv("OPENCODE_ADVISOR_MODEL", "")
	stub := &gateStub{answer: func(moment string) (*TypesafeResponse, error) {
		if moment == advisorMomentBeforeCommit {
			return gateChoice(advisorGateChoiceReview, 0.9), nil
		}
		return gateChoice(advisorGateChoiceSkipReviewed, 0.9), nil
	}}
	a, fake := gateTestAgent(t, stub)
	fake.advice = "gap: the migration is untested"
	workdir := t.TempDir()
	a.SetWorkDir(workdir)
	if err := os.WriteFile(filepath.Join(workdir, "b.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.Permissions().SetMode(PermissionModeYOLO)
	a.snapshotStore = snapshot.NewStore("gate-test", t.TempDir())
	write := &checkpointFakeWrite{}
	read := &gateRecordingTool{name: "read"}
	bash := &gateRecordingTool{name: "bash"}
	a.tools["write"] = write
	a.tools["read"] = read
	a.tools["bash"] = bash

	batch := []ToolCall{
		gateWriteCall("tc-w"),
		namedToolCall("tc-c", "bash", gateCommitArgs),
		namedToolCall("tc-r", "read", `{"path":"b.go"}`),
	}
	a.client = &scriptedClient{msgs: []*Message{
		{Role: "assistant", Content: "fixing and committing", ToolCalls: batch},
		{Role: "assistant", Content: "re-issuing the commit", ToolCalls: []ToolCall{namedToolCall("tc-c2", "bash", gateCommitArgs)}},
		{Role: "assistant", Content: "committed"},
	}, errs: []error{nil, nil, nil}}

	msgs, err := a.Step([]Message{{Role: "user", Content: "commit the fix"}})
	if err != nil {
		t.Fatalf("Step: %v", err)
	}

	if stub.calls[advisorMomentBeforeCommit] != 1 {
		t.Fatalf("before_commit gate consulted %d times, want 1 for the turn", stub.calls[advisorMomentBeforeCommit])
	}
	if fake.calls != 1 || !strings.Contains(fake.lastPrompt, "PRE-COMMIT CHECKPOINT") {
		t.Fatalf("advisor calls=%d, want one pre-commit review; prompt=%q", fake.calls, fake.lastPrompt)
	}
	if write.calls != 1 || read.calls != 1 {
		t.Fatalf("write=%d read=%d, want both to run in the held batch", write.calls, read.calls)
	}
	if bash.calls != 1 {
		t.Fatalf("bash ran %d time(s), want 1 (the re-issued commit only)", bash.calls)
	}

	assertOneToolMessagePerCall(t, msgs, batch)
	var held string
	for _, m := range msgs {
		if m.Role == "tool" && m.ToolID == "tc-c" {
			held = m.Content
		}
	}
	if !strings.HasPrefix(held, "Commit not run yet") || !strings.Contains(held, "gap: the migration is untested") {
		t.Fatalf("held commit message = %q", held)
	}
	if len(a.advisorReviews) != 1 || a.advisorReviews[0].Moment != advisorMomentBeforeCommit {
		t.Fatalf("ledger = %+v, want one before_commit entry", a.advisorReviews)
	}
}

// TestAdvisorCommitHold_CancelDuringReviewAnswersEveryCall covers the Stop path:
// the user cancels while the pre-commit review runs. Every call in the round must
// still get exactly one tool message, and the held commit must not be duplicated.
func TestAdvisorCommitHold_CancelDuringReviewAnswersEveryCall(t *testing.T) {
	t.Setenv("OPENCODE_ADVISOR_MODEL", "")
	stub := &gateStub{answer: gateByMoment(gateChoice(advisorGateChoiceReview, 0.9), nil)}
	a, _ := gateTestAgent(t, stub)
	adv := &gateCancellingAdvisor{a: a, checkpointFakeAdvisor: checkpointFakeAdvisor{advice: "gap"}}
	a.tools["advisor"] = adv
	bash := &gateRecordingTool{name: "bash"}
	read := &gateRecordingTool{name: "read"}
	a.tools["bash"] = bash
	a.tools["read"] = read
	a.tools["write"] = &checkpointFakeWrite{}
	a.snapshotStore = snapshot.NewStore("gate-test", t.TempDir())

	batch := []ToolCall{
		gateWriteCall("tc-w"),
		namedToolCall("tc-c", "bash", gateCommitArgs),
		namedToolCall("tc-r", "read", `{}`),
	}
	a.client = &scriptedClient{msgs: []*Message{
		{Role: "assistant", Content: "fixing and committing", ToolCalls: batch},
	}, errs: []error{nil}}

	msgs, err := a.Step([]Message{{Role: "user", Content: "commit the fix"}})
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if bash.calls != 0 || read.calls != 0 {
		t.Fatalf("bash=%d read=%d, want nothing dispatched after cancel", bash.calls, read.calls)
	}
	assertOneToolMessagePerCall(t, msgs, batch)
}

func TestBashRunsGitCommit(t *testing.T) {
	cases := []struct {
		command string
		want    bool
	}{
		{"git commit -m x", true},
		{"git add -A && git commit -m x", true},
		{"cd repo && git -C . commit", true},
		{"git -c user.name=x commit", true},
		{"git --no-pager commit -m x", true},
		{"git commit;git push", true},
		{"git commit&&git push", true},
		{"git --super-prefix x commit -m y", true},
		{"git --exec-path /usr/lib/git commit -m y", true},
		{"git log --grep commit", false},
		{"echo git commit", false},
		{"git commit-tree HEAD^{tree}", false},
		{"git status", false},
		{"go test ./...", false},
	}
	for _, tc := range cases {
		if got := bashRunsGitCommit(tc.command); got != tc.want {
			t.Errorf("bashRunsGitCommit(%q) = %v, want %v", tc.command, got, tc.want)
		}
	}
}

// TestAdvisorLedger_ChangedSinceLastReview checks the ledger after a review and a
// later write: only the file written after the review is reported as unreviewed.
func TestAdvisorLedger_ChangedSinceLastReview(t *testing.T) {
	t.Setenv("OPENCODE_ADVISOR_MODEL", "")
	a, _ := gateTestAgent(t, nil)
	td := t.TempDir()
	store := snapshot.NewStore("ledger-test", filepath.Join(td, "snaps"))
	a.snapshotStore = store
	fileA := filepath.Join(td, "a.go")
	fileB := filepath.Join(td, "b.go")

	if err := store.Backup(fileA, "tc1"); err != nil {
		t.Fatal(err)
	}
	store.RegisterWrite(fileA, "tc1")
	if got := a.advisorChangedSinceLastReview(); !slices.Equal(got, []string{fileA}) {
		t.Fatalf("before any review = %v, want [%s]", got, fileA)
	}

	a.recordAdvisorReview(advisorMomentEndOfTurn, "looks fine")
	if got := a.advisorChangedSinceLastReview(); len(got) != 0 {
		t.Fatalf("right after a review = %v, want nothing unreviewed", got)
	}

	// A rewrite of the file the review already covered is unreviewed work. This
	// is the apply_patch/formatter/bash-edit case: no path extraction is
	// involved, only the store's write watermark.
	if err := store.Backup(fileA, "tc2"); err != nil {
		t.Fatal(err)
	}
	store.RegisterWrite(fileA, "tc2")
	if got := a.advisorChangedSinceLastReview(); !slices.Equal(got, []string{fileA}) {
		t.Fatalf("after rewriting a reviewed file = %v, want [%s]", got, fileA)
	}

	a.recordAdvisorReview(advisorMomentEndOfTurn, "looks fine now")
	if err := store.Backup(fileB, "tc3"); err != nil {
		t.Fatal(err)
	}
	store.RegisterWrite(fileB, "tc3")
	if got := a.advisorChangedSinceLastReview(); !slices.Equal(got, []string{fileB}) {
		t.Fatalf("after a later write = %v, want [%s]", got, fileB)
	}
}

func TestAdvisorLedger_KeepsOnlyTheNewestEntries(t *testing.T) {
	a := newTestAgent(nil, nil, &config.Config{}, nil)
	for i := range advisorLedgerMax + 5 {
		a.recordAdvisorReview(advisorMomentEndOfTurn, fmt.Sprintf("review %d", i))
	}
	if len(a.advisorReviews) != advisorLedgerMax {
		t.Fatalf("ledger holds %d entries, want %d", len(a.advisorReviews), advisorLedgerMax)
	}
	if got := a.advisorReviews[0].Verdict; got != "review 5" {
		t.Fatalf("oldest kept entry = %q, want \"review 5\"", got)
	}
}

func TestAdvisorCheckpointState_TurnSummary(t *testing.T) {
	st := &advisorCheckpointState{}
	st.countBatch([]ToolCall{
		gateWriteCall("tc1"),
		namedToolCall("tc2", "bash", gateCommitArgs),
		namedToolCall("tc3", "read", `{}`),
	})
	if st.toolCallsByName["write"] != 1 || st.toolCallsByName["bash"] != 1 || st.toolCallsByName["read"] != 1 {
		t.Fatalf("toolCallsByName = %v", st.toolCallsByName)
	}
	if !slices.Equal(st.filesWritten, []string{"a.go"}) {
		t.Fatalf("filesWritten = %v", st.filesWritten)
	}
	if !slices.Equal(st.bashCommands, []string{"git add -A && git commit -m fix"}) {
		t.Fatalf("bashCommands = %v", st.bashCommands)
	}
}

func TestAdvisorGateCapList_MarksTruncation(t *testing.T) {
	items := make([]string, advisorGateListMax+7)
	for i := range items {
		items[i] = fmt.Sprintf("f%d", i)
	}
	got := advisorGateCapList(items, advisorGateListMax)
	if len(got) != advisorGateListMax+1 || got[len(got)-1] != "…(7 more not shown)" {
		t.Fatalf("capped list len=%d last=%q", len(got), got[len(got)-1])
	}
	if short := advisorGateCapList([]string{"a"}, advisorGateListMax); !slices.Equal(short, []string{"a"}) {
		t.Fatalf("short list = %v, want unchanged", short)
	}
}

// TestAdvisorCommitHold_BelowAnswerFloorRunsCommit pins the gate's answer floor:
// a review_now the judge is not confident about must not withhold a commit, and
// an answer carrying no probability for the chosen option is not a decision
// either. The gate is still recorded as consulted.
func TestAdvisorCommitHold_BelowAnswerFloorRunsCommit(t *testing.T) {
	cases := []struct {
		name   string
		answer *TypesafeResponse
	}{
		{"below floor", gateChoice(advisorGateChoiceReview, 0.5)},
		{"no probability", &TypesafeResponse{
			Model: "jev-latest",
			Answers: map[string]TypesafeAnswer{
				advisorGateKey: {Type: "choice", Choice: advisorGateChoiceReview, Confidence: 0.9},
			},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OPENCODE_ADVISOR_MODEL", "")
			stub := &gateStub{answer: gateByMoment(tc.answer, nil)}
			a, fake := gateTestAgent(t, stub)
			td := t.TempDir()
			store := snapshot.NewStore("gate-test", filepath.Join(td, "snaps"))
			a.snapshotStore = store
			if err := store.Backup(filepath.Join(td, "a.go"), "tc1"); err != nil {
				t.Fatal(err)
			}
			resp := &Message{Role: "assistant", Content: "committing", ToolCalls: []ToolCall{
				gateWriteCall("tc1"),
				namedToolCall("tc2", "bash", gateCommitArgs),
			}}
			st := a.newAdvisorCheckpointState("fix it", false, false)
			st.countBatch(resp.ToolCalls)
			if held := a.advisorCommitHold(st, resp, nil); held != nil {
				t.Fatalf("commit withheld on an unusable answer: %v", held)
			}
			if fake.calls != 0 {
				t.Fatalf("advisor ran %d time(s) on an unusable answer", fake.calls)
			}
			if !st.commitAsked {
				t.Fatal("the gate was consulted, so commitAsked must be set")
			}
		})
	}
}

// TestAdvisorDoneCheckpoint_SkipsWhenCommitReviewCoveredChanges pins the
// threshold fallback's last clause: when a before-commit review already ran and
// nothing was written since, a completion review must not run again.
func TestAdvisorDoneCheckpoint_SkipsWhenCommitReviewCoveredChanges(t *testing.T) {
	t.Setenv("OPENCODE_ADVISOR_MODEL", "")
	a, fake := gateTestAgent(t, nil) // no judge: the threshold rule decides
	td := t.TempDir()
	store := snapshot.NewStore("gate-test", filepath.Join(td, "snaps"))
	a.snapshotStore = store
	file := filepath.Join(td, "a.go")
	if err := os.WriteFile(file, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.Backup(file, "tc1"); err != nil {
		t.Fatal(err)
	}
	store.RegisterWrite(file, "tc1")
	a.recordAdvisorReview(advisorMomentBeforeCommit, "complete")

	st := a.newAdvisorCheckpointState("commit it", false, false)
	st.countBatch([]ToolCall{gateWriteCall("tc1")})
	st.commitReviewed = true
	if msg := a.advisorDoneCheckpoint(st, &Message{Role: "assistant", Content: "done"}); msg != nil {
		t.Fatalf("a completion review ran after a commit review covered the same files: %q", msg.Content)
	}
	if fake.calls != 0 {
		t.Fatalf("advisor ran %d time(s) after a covering commit review", fake.calls)
	}

	// Control: a write after the commit review is unreviewed work, so the
	// completion review must fire.
	if err := store.Backup(file, "tc2"); err != nil {
		t.Fatal(err)
	}
	store.RegisterWrite(file, "tc2")
	st2 := a.newAdvisorCheckpointState("commit it", false, false)
	st2.countBatch([]ToolCall{gateWriteCall("tc1")})
	st2.commitReviewed = true
	if msg := a.advisorDoneCheckpoint(st2, &Message{Role: "assistant", Content: "done"}); msg == nil {
		t.Fatal("control: a write after the commit review must fire the completion review")
	}
	if fake.calls != 1 {
		t.Fatalf("advisor calls = %d, want exactly the controlled completion review", fake.calls)
	}
}

// TestAdvisorCommitHold_CancelDuringReviewSkipsDAGValidation pins the
// cancelled-round exit that follows a pre-commit gate consult. Every call is
// already skipped by the per-dispatch isCancelled checks, so the exit's only
// observable effect is this one: a cancelled round answers every call with the
// cancellation notice and does NOT also run the DAG gate. Without the exit, the
// malformed task call below reaches buildDAG and its message carries
// errDAGValidationPrefix instead of the cancellation notice.
func TestAdvisorCommitHold_CancelDuringReviewSkipsDAGValidation(t *testing.T) {
	t.Setenv("OPENCODE_ADVISOR_MODEL", "")
	stub := &gateStub{answer: gateByMoment(gateChoice(advisorGateChoiceReview, 0.9), nil)}
	a, _ := gateTestAgent(t, stub)
	adv := &gateCancellingAdvisor{a: a, checkpointFakeAdvisor: checkpointFakeAdvisor{advice: "gap"}}
	a.tools["advisor"] = adv
	bash := &gateRecordingTool{name: "bash"}
	a.tools["bash"] = bash
	a.tools["write"] = &checkpointFakeWrite{}
	// The task tool must be a parallel-class tool for the DAG gate to inspect it.
	a.tools["task"] = &gateRecordingTool{name: "task", parallel: true}
	a.snapshotStore = snapshot.NewStore("gate-test", t.TempDir())

	// depends_on without an id is a hard DAG validation failure, so this call is
	// the canary for whether the DAG gate ran after the cancellation.
	malformed := namedToolCall("tc-task", "task", `{"prompt":"p","agent":"general","depends_on":["nope"]}`)
	batch := []ToolCall{
		gateWriteCall("tc-w"),
		namedToolCall("tc-c", "bash", gateCommitArgs),
		malformed,
	}
	a.client = &scriptedClient{msgs: []*Message{
		{Role: "assistant", Content: "fixing and committing", ToolCalls: batch},
	}, errs: []error{nil}}

	msgs, err := a.Step([]Message{{Role: "user", Content: "commit the fix"}})
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if bash.calls != 0 {
		t.Fatalf("bash ran %d time(s) after the cancel, want 0", bash.calls)
	}
	assertOneToolMessagePerCall(t, msgs, batch)

	var taskMsg string
	for _, m := range msgs {
		if m.Role == "tool" && m.ToolID == "tc-task" {
			taskMsg = m.Content
		}
	}
	if strings.Contains(taskMsg, errDAGValidationPrefix) {
		t.Fatalf("a cancelled round must not report a DAG validation error, got %q", taskMsg)
	}
	if taskMsg != tool.ToolCancelledResult {
		t.Fatalf("task message = %q, want the cancellation notice", taskMsg)
	}
}
