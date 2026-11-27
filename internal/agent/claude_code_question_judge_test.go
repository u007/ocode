package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

const claudeCodeDatabaseQuestion = "Which database should the notes service use?"

// claudeCodeQuestionInput is the AskUserQuestion input for qDatabasePrompt.
func claudeCodeQuestionInput() json.RawMessage {
	return json.RawMessage(`{"questions":[{"question":"Which database should the notes service use?","header":"Database","multiSelect":false,"options":[{"label":"PostgreSQL","description":"Already running in docker-compose"},{"label":"SQLite","description":"Embedded file database"},{"label":"Something else","description":"Describe your own"}]}]}`)
}

// claudeCodeQuestionTranscript ends on the user prompt, as a real turn does: the
// AskUserQuestion call lives inside the Claude Code run, not in ocode's transcript.
func claudeCodeQuestionTranscript() []Message {
	return []Message{{Role: "user", Content: "Add persistence to the notes service"}}
}

// claudeCodeQuestionAgent builds an agent whose question judge is stub and whose
// human asker answers PostgreSQL and counts its calls.
func claudeCodeQuestionAgent(t *testing.T, stub *questionStubDecider, autoContinue bool) (*Agent, *int) {
	t.Helper()
	prev := newClientFn
	t.Cleanup(func() { newClientFn = prev })
	newClientFn = func(_ *config.Config, _ string) LLMClient { return stub }

	cfg := &config.Config{}
	cfg.Ocode.AutoContinueModel = "typesafe/jev-latest"
	a := newTestAgent(nil, nil, cfg, nil)
	a.SetAutoContinueEnabled(autoContinue)
	asks := new(int)
	a.SetQuestionAsker(func(_ context.Context, _ []tool.QuestionPrompt) ([]tool.QuestionAnswerSet, bool) {
		*asks++
		return []tool.QuestionAnswerSet{{
			Header:   "Database",
			Question: claudeCodeDatabaseQuestion,
			Answers:  []tool.QuestionAnswer{{Label: "PostgreSQL"}},
		}}, false
	})
	return a, asks
}

// claudeCodeQuestionJudgeSQLite is a stub answer where the transcript determines
// SQLite (opt1) with high confidence.
func claudeCodeQuestionJudgeSQLite(map[string]TypesafeQuestion) *TypesafeResponse {
	return qResp(map[string]TypesafeAnswer{
		"q0_owner":  qOwnerDetermined(0.9),
		"q0_answer": qAnsChoice("opt1", map[string]float64{"opt1": 0.9, typesafeQuestionDeferChoice: 0.1}),
	})
}

// claudeCodeAnswerFor returns the answer keyed by the database question in
// updatedInput.
func claudeCodeAnswerFor(t *testing.T, updated json.RawMessage) string {
	t.Helper()
	var got struct {
		Answers map[string]string `json:"answers"`
	}
	if err := json.Unmarshal(updated, &got); err != nil {
		t.Fatalf("updatedInput is not JSON: %v (%s)", err, updated)
	}
	return got.Answers[claudeCodeDatabaseQuestion]
}

func TestClaudeCodeQuestionGateOffNeverConsultsJudge(t *testing.T) {
	stub := &questionStubDecider{respond: claudeCodeQuestionJudgeSQLite}
	a, asks := claudeCodeQuestionAgent(t, stub, false)
	ctx := withClaudeCodeTurn(context.Background(), newClaudeCodeTurn(claudeCodeQuestionTranscript()))

	allowed, updated, msg := a.answerClaudeCodeQuestion(ctx, claudeCodeQuestionInput())
	if !allowed {
		t.Fatalf("denied: %s", msg)
	}
	if stub.calls != 0 {
		t.Fatalf("judge consulted %d times with auto-continue off, want 0", stub.calls)
	}
	if *asks != 1 {
		t.Fatalf("human asker called %d times, want 1", *asks)
	}
	if got := claudeCodeAnswerFor(t, updated); got != "PostgreSQL" {
		t.Fatalf("answer = %q, want the human answer PostgreSQL", got)
	}
}

func TestClaudeCodeQuestionJudgeAnswerSkipsHuman(t *testing.T) {
	stub := &questionStubDecider{respond: claudeCodeQuestionJudgeSQLite}
	a, asks := claudeCodeQuestionAgent(t, stub, true)
	ctx := withClaudeCodeTurn(context.Background(), newClaudeCodeTurn(claudeCodeQuestionTranscript()))

	allowed, updated, msg := a.answerClaudeCodeQuestion(ctx, claudeCodeQuestionInput())
	if !allowed {
		t.Fatalf("denied: %s", msg)
	}
	if stub.calls != 1 {
		t.Fatalf("judge consulted %d times, want 1", stub.calls)
	}
	if *asks != 0 {
		t.Fatalf("human asker called %d times after a judge answer, want 0", *asks)
	}
	if got := claudeCodeAnswerFor(t, updated); got != "SQLite" {
		t.Fatalf("answer = %q, want the judge answer SQLite", got)
	}
}

func TestClaudeCodeQuestionJudgeDeferAsksHuman(t *testing.T) {
	stub := &questionStubDecider{respond: qDeferEverything}
	a, asks := claudeCodeQuestionAgent(t, stub, true)
	ctx := withClaudeCodeTurn(context.Background(), newClaudeCodeTurn(claudeCodeQuestionTranscript()))

	allowed, updated, msg := a.answerClaudeCodeQuestion(ctx, claudeCodeQuestionInput())
	if !allowed {
		t.Fatalf("denied: %s", msg)
	}
	if stub.calls != 1 {
		t.Fatalf("judge consulted %d times, want 1", stub.calls)
	}
	if *asks != 1 {
		t.Fatalf("human asker called %d times after a judge defer, want 1", *asks)
	}
	if got := claudeCodeAnswerFor(t, updated); got != "PostgreSQL" {
		t.Fatalf("answer = %q, want the human answer PostgreSQL", got)
	}
}

func TestClaudeCodeQuestionNoTranscriptSkipsJudge(t *testing.T) {
	stub := &questionStubDecider{respond: claudeCodeQuestionJudgeSQLite}
	a, asks := claudeCodeQuestionAgent(t, stub, true)

	allowed, updated, msg := a.answerClaudeCodeQuestion(context.Background(), claudeCodeQuestionInput())
	if !allowed {
		t.Fatalf("denied: %s", msg)
	}
	if stub.calls != 0 {
		t.Fatalf("judge consulted %d times without a transcript, want 0", stub.calls)
	}
	if *asks != 1 {
		t.Fatalf("human asker called %d times, want 1", *asks)
	}
	if got := claudeCodeAnswerFor(t, updated); got != "PostgreSQL" {
		t.Fatalf("answer = %q, want the human answer PostgreSQL", got)
	}
}

func TestClaudeCodeQuestionEvaluableGoalConsultsJudge(t *testing.T) {
	stub := &questionStubDecider{respond: claudeCodeQuestionJudgeSQLite}
	a, asks := claudeCodeQuestionAgent(t, stub, false)
	if err := a.SetGoal("the notes service persists its data"); err != nil {
		t.Fatalf("SetGoal: %v", err)
	}
	ctx := withClaudeCodeTurn(context.Background(), newClaudeCodeTurn(claudeCodeQuestionTranscript()))

	allowed, updated, msg := a.answerClaudeCodeQuestion(ctx, claudeCodeQuestionInput())
	if !allowed {
		t.Fatalf("denied: %s", msg)
	}
	if stub.calls != 1 || *asks != 0 {
		t.Fatalf("judge calls = %d, human asks = %d; want 1 and 0", stub.calls, *asks)
	}
	if got := claudeCodeAnswerFor(t, updated); got != "SQLite" {
		t.Fatalf("answer = %q, want SQLite", got)
	}
}

// With no human surface, a judge answer still resolves the question, and a judge
// defer falls through to the existing no-surface denial.
func TestClaudeCodeQuestionNoSurfaceOnlyJudgeCanAnswer(t *testing.T) {
	t.Run("judge answers", func(t *testing.T) {
		stub := &questionStubDecider{respond: claudeCodeQuestionJudgeSQLite}
		a, _ := claudeCodeQuestionAgent(t, stub, true)
		a.SetQuestionAsker(nil)
		ctx := withClaudeCodeTurn(context.Background(), newClaudeCodeTurn(claudeCodeQuestionTranscript()))

		allowed, updated, msg := a.answerClaudeCodeQuestion(ctx, claudeCodeQuestionInput())
		if !allowed {
			t.Fatalf("denied: %s", msg)
		}
		if got := claudeCodeAnswerFor(t, updated); got != "SQLite" {
			t.Fatalf("answer = %q, want SQLite", got)
		}
	})
	t.Run("judge defers", func(t *testing.T) {
		stub := &questionStubDecider{respond: qDeferEverything}
		a, _ := claudeCodeQuestionAgent(t, stub, true)
		a.SetQuestionAsker(nil)
		ctx := withClaudeCodeTurn(context.Background(), newClaudeCodeTurn(claudeCodeQuestionTranscript()))

		allowed, _, msg := a.answerClaudeCodeQuestion(ctx, claudeCodeQuestionInput())
		if allowed {
			t.Fatal("allowed with no surface and a deferred judge, want denied")
		}
		if msg != claudeCodeNoSurfaceMessage {
			t.Fatalf("message = %q, want %q", msg, claudeCodeNoSurfaceMessage)
		}
	})
}

func TestClaudeCodeApproveForTurnAttachesRunState(t *testing.T) {
	turn := newClaudeCodeTurn(claudeCodeQuestionTranscript())
	var got *claudeCodeTurn
	var found bool
	inner := func(ctx context.Context, _ string, _ json.RawMessage, _ string) (bool, json.RawMessage, string) {
		got, found = claudeCodeTurnFromContext(ctx)
		return true, nil, ""
	}
	wrapped := claudeCodeApproveForTurn(inner, turn)
	if allowed, _, _ := wrapped(context.Background(), "AskUserQuestion", nil, "toolu_1"); !allowed {
		t.Fatal("wrapper dropped the inner decision")
	}
	if !found || got != turn {
		t.Fatalf("run state on the inner context = %p (found %v), want %p", got, found, turn)
	}
}

// The cap is per run: answers stop at AutoContinueChainCap, the next question
// goes to the human, and a new run gets a fresh allowance.
func TestClaudeCodeQuestionJudgeCapSendsLaterQuestionsToHuman(t *testing.T) {
	stub := &questionStubDecider{respond: claudeCodeQuestionJudgeSQLite}
	a, asks := claudeCodeQuestionAgent(t, stub, true)
	ctx := withClaudeCodeTurn(context.Background(), newClaudeCodeTurn(claudeCodeQuestionTranscript()))

	for i := 0; i < AutoContinueChainCap; i++ {
		allowed, updated, msg := a.answerClaudeCodeQuestion(ctx, claudeCodeQuestionInput())
		if !allowed {
			t.Fatalf("answer %d denied: %s", i+1, msg)
		}
		if got := claudeCodeAnswerFor(t, updated); got != "SQLite" {
			t.Fatalf("answer %d = %q, want the judge answer SQLite", i+1, got)
		}
	}
	if stub.calls != AutoContinueChainCap || *asks != 0 {
		t.Fatalf("before the cap: judge calls = %d, human asks = %d; want %d and 0", stub.calls, *asks, AutoContinueChainCap)
	}

	allowed, updated, msg := a.answerClaudeCodeQuestion(ctx, claudeCodeQuestionInput())
	if !allowed {
		t.Fatalf("capped question denied: %s", msg)
	}
	if stub.calls != AutoContinueChainCap {
		t.Fatalf("judge consulted after the cap: calls = %d, want %d", stub.calls, AutoContinueChainCap)
	}
	if *asks != 1 {
		t.Fatalf("human asker called %d times after the cap, want 1", *asks)
	}
	if got := claudeCodeAnswerFor(t, updated); got != "PostgreSQL" {
		t.Fatalf("capped answer = %q, want the human answer PostgreSQL", got)
	}

	fresh := withClaudeCodeTurn(context.Background(), newClaudeCodeTurn(claudeCodeQuestionTranscript()))
	if _, updated, _ := a.answerClaudeCodeQuestion(fresh, claudeCodeQuestionInput()); claudeCodeAnswerFor(t, updated) != "SQLite" {
		t.Fatal("a new run did not get a fresh judge allowance")
	}
	if *asks != 1 {
		t.Fatalf("new run asked the human: asks = %d, want still 1", *asks)
	}
}

// A deferral does not use up the cap: the judge can defer and still answer
// AutoContinueChainCap later questions in the same run.
func TestClaudeCodeQuestionJudgeDeferDoesNotCountTowardCap(t *testing.T) {
	calls := 0
	stub := &questionStubDecider{respond: func(q map[string]TypesafeQuestion) *TypesafeResponse {
		calls++
		if calls == 1 {
			return qDeferEverything(q)
		}
		return claudeCodeQuestionJudgeSQLite(q)
	}}
	a, asks := claudeCodeQuestionAgent(t, stub, true)
	ctx := withClaudeCodeTurn(context.Background(), newClaudeCodeTurn(claudeCodeQuestionTranscript()))

	a.answerClaudeCodeQuestion(ctx, claudeCodeQuestionInput())
	if *asks != 1 {
		t.Fatalf("deferred question did not reach the human: asks = %d", *asks)
	}
	for i := 0; i < AutoContinueChainCap; i++ {
		allowed, updated, msg := a.answerClaudeCodeQuestion(ctx, claudeCodeQuestionInput())
		if !allowed {
			t.Fatalf("answer %d denied: %s", i+1, msg)
		}
		if got := claudeCodeAnswerFor(t, updated); got != "SQLite" {
			t.Fatalf("answer %d = %q, want SQLite", i+1, got)
		}
	}
	if *asks != 1 {
		t.Fatalf("human asked after judge answers under the cap: asks = %d, want 1", *asks)
	}
}

// A judge answer leaves one notice line on the run, drained exactly once into the
// tool rows that become Message.Notice.
func TestClaudeCodeJudgeAnswerNoticeReachesToolRows(t *testing.T) {
	stub := &questionStubDecider{respond: claudeCodeQuestionJudgeSQLite}
	a, _ := claudeCodeQuestionAgent(t, stub, true)
	turn := newClaudeCodeTurn(claudeCodeQuestionTranscript())
	ctx := withClaudeCodeTurn(context.Background(), turn)

	if allowed, _, msg := a.answerClaudeCodeQuestion(ctx, claudeCodeQuestionInput()); !allowed {
		t.Fatalf("denied: %s", msg)
	}
	c := &GenericClient{}
	want := "AskUserQuestion answered by the auto-continue judge: " + claudeCodeDatabaseQuestion + " → SQLite"
	rows := c.appendClaudeJudgeNotices([]string{"← Bash ok"}, turn)
	if len(rows) != 2 || rows[1] != want {
		t.Fatalf("tool rows = %q, want the judge line %q appended after the existing row", rows, want)
	}
	if again := c.appendClaudeJudgeNotices(nil, turn); len(again) != 0 {
		t.Fatalf("notice drained twice: %q", again)
	}
	if got := c.appendClaudeJudgeNotices(nil, nil); got != nil {
		t.Fatalf("nil run state produced rows: %q", got)
	}
}

// A deferred question leaves no notice: only a judge answer is shown.
func TestClaudeCodeJudgeDeferLeavesNoNotice(t *testing.T) {
	stub := &questionStubDecider{respond: qDeferEverything}
	a, _ := claudeCodeQuestionAgent(t, stub, true)
	turn := newClaudeCodeTurn(claudeCodeQuestionTranscript())
	if allowed, _, msg := a.answerClaudeCodeQuestion(withClaudeCodeTurn(context.Background(), turn), claudeCodeQuestionInput()); !allowed {
		t.Fatalf("denied: %s", msg)
	}
	if got := turn.takeJudgeNotices(); len(got) != 0 {
		t.Fatalf("deferred question left notices: %q", got)
	}
}
