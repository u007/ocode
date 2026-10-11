package agent

import (
	"encoding/json"
	"testing"

	"github.com/u007/ocode/internal/tool"
)

// checkpointFakeWrite is a minimal write-class tool so a checkpoint test can
// drive a real executed batch without touching the filesystem.
type checkpointFakeWrite struct{ calls int }

func (f *checkpointFakeWrite) Name() string        { return "write" }
func (f *checkpointFakeWrite) Description() string { return "fake write" }
func (f *checkpointFakeWrite) Definition() map[string]interface{} {
	return map[string]interface{}{"name": "write"}
}
func (f *checkpointFakeWrite) Parallel() bool { return true }
func (f *checkpointFakeWrite) Execute(json.RawMessage) (string, error) {
	f.calls++
	return "wrote the file", nil
}

// pendingAskTranscript is the shape of a round that paused on a permission
// ask: an assistant tool-call row followed by its tool result, whose content is
// still the unanswered PERMISSION_ASK sentinel. The desktop/web app renders one
// dialog per unresolved sentinel, and a round may pause on several at once
// (parallel dispatch runs every call before the pause check — see
// trailingToolRunStart in internal/server/run_states.go).
func pendingAskTranscript() []Message {
	return []Message{
		{Role: "user", Content: "fix the failing build"},
		{Role: "assistant", ToolCalls: []ToolCall{newToolCall("call-bash", "bash", `{"command":"go test ./..."}`)}},
		{Role: "tool", ToolID: "call-bash", Content: tool.SentinelPermissionAsk + `{"tool":"bash","summary":"go test ./..."}`},
	}
}

// resolvedAskTranscript is the same round after the user answered: the ask row's
// content was replaced in place, so no dialog is on screen any more.
func resolvedAskTranscript() []Message {
	msgs := pendingAskTranscript()
	msgs[2].Content = "go test ./... (exit 0)"
	return msgs
}

// TestAdvisorCheckpointsDoNotFireWhileAskPending is the regression test for the
// desktop bug where an advisor call started with a permission/question dialog
// still on screen. The round that raised the ask paused with MORE THAN ONE
// unresolved sentinel; the user answered one of them, and the continuation Step
// then ran with the other dialog still up. Both checkpoints therefore had to be
// suppressed for that Step — starting a second model (minutes for the Claude
// Code backend) underneath an unanswered dialog is the reported bug.
//
// Note the plan checkpoint is the one that slipped through: its guard is the
// `pauseAfterResults` early return in Step, which only sees asks raised by the
// batch THIS iteration executed. A continuation Step appends its own results
// after the pending row, so the pending ask is no longer in the trailing tool
// run and the plan checkpoint fired with the dialog still open.
func TestAdvisorCheckpointsDoNotFireWhileAskPending(t *testing.T) {
	t.Setenv("OPENCODE_ADVISOR_MODEL", "")
	a, fake := checkpointTestAgent(t, []string{"plan", "done"})
	writeTool := &checkpointFakeWrite{}
	a.tools["write"] = writeTool
	// Three scripted responses so the assertions below are reached either way:
	// with the gate shut the turn finishes on response 2, and with the gate open
	// (a mutant) both checkpoints fire and each injects a review, driving the loop
	// around to response 3. The run then completes and the fake.calls assertion
	// below reports the bug directly instead of dying on a spent script.
	a.client = &scriptedClient{msgs: []*Message{
		{Role: "assistant", Content: "writing the fix", ToolCalls: []ToolCall{newToolCall("call-w", "write", `{"path":"a.go","content":"x"}`)}},
		{Role: "assistant", Content: "the review landed, finishing up"},
		{Role: "assistant", Content: "done"},
	}, errs: []error{nil, nil, nil}}

	msgs, err := a.Step(pendingAskTranscript())
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if fake.calls != 0 {
		t.Fatalf("advisor ran %d time(s) while a permission dialog was still pending; it must wait for the user", fake.calls)
	}
	// The write still happened — the gate is about the advisor, not the turn.
	if writeTool.calls != 1 {
		t.Fatalf("expected the continuation's write to still execute, got %d calls", writeTool.calls)
	}
	if len(msgs) == 0 {
		t.Fatal("Step returned no messages")
	}
}

// TestAdvisorCheckpointsFireOnceAsksResolved is the other half of the
// contract: the suppression is scoped to the Step that ran with a dialog up.
// Once every ask in the round is answered the checkpoints must fire again —
// otherwise the gate would silently disable the advisor for the rest of the
// session's turns, since a continuation Step is where the completion review
// normally happens.
func TestAdvisorCheckpointsFireOnceAsksResolved(t *testing.T) {
	t.Setenv("OPENCODE_ADVISOR_MODEL", "")
	a, fake := checkpointTestAgent(t, []string{"plan", "done"})
	a.tools["write"] = &checkpointFakeWrite{}
	// Three responses: the write batch, then the loop running once more because the
	// plan checkpoint injected its review as a user message, then the final answer
	// the done checkpoint reviews.
	a.client = &scriptedClient{msgs: []*Message{
		{Role: "assistant", Content: "writing the fix", ToolCalls: []ToolCall{newToolCall("call-w", "write", `{"path":"a.go","content":"x"}`)}},
		{Role: "assistant", Content: "the review landed, finishing up"},
		{Role: "assistant", Content: "done"},
	}, errs: []error{nil, nil, nil}}

	if _, err := a.Step(resolvedAskTranscript()); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if fake.calls == 0 {
		t.Fatal("advisor never ran even though every ask in the round was answered; the gate must not disable it permanently")
	}
}

// TestAdvisorCheckpointSkipsDoNotConsumeTheCheckpoint pins that a skipped
// checkpoint stays ARMED. A continuation Step builds a fresh
// advisorCheckpointState, so "skip" has to mean "not this Step, and not
// forever" — consuming it would silently drop the completion review from the
// turn that finally gets to run.
func TestAdvisorCheckpointSkipsDoNotConsumeTheCheckpoint(t *testing.T) {
	t.Setenv("OPENCODE_ADVISOR_MODEL", "")
	a, fake := checkpointTestAgent(t, []string{"plan", "done"})
	st := a.newAdvisorCheckpointState("fix the build", true)

	resp := &Message{
		Role:      "assistant",
		Content:   "I will edit a.go.",
		ToolCalls: []ToolCall{newToolCall("call-w", "write", `{"path":"a.go","content":"x"}`)},
	}
	st.pendingAsk = true
	if review := a.advisorPlanCheckpoint(st, resp); review != nil {
		t.Fatal("expected the plan checkpoint to stay silent while an ask is pending")
	}
	if st.planChecked {
		t.Fatal("a skipped plan checkpoint must stay armed for a later Step")
	}
	if fake.calls != 0 {
		t.Fatalf("advisor ran %d time(s) while an ask was pending", fake.calls)
	}

	// Dialog answered — the same state now fires.
	st.pendingAsk = false
	st.countBatch(resp.ToolCalls)
	if review := a.advisorPlanCheckpoint(st, resp); review == nil {
		t.Fatal("expected the plan checkpoint to fire once the ask was answered")
	}
	if fake.calls != 1 {
		t.Fatalf("expected exactly 1 advisor call after the ask cleared, got %d", fake.calls)
	}
}

// TestAdvisorPendingAskGateMatchesServerPendingAsks pins the gate to the SAME
// predicate the server uses for `pending_asks`, so "the advisor is waiting" and
// "a dialog is on screen" can never disagree. internal/server/run_states.go and
// internal/session/transcript_tail.go answer a different question (the trailing
// round only), so they keep their own helpers.
func TestAdvisorPendingAskGateMatchesServerPendingAsks(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{"unresolved permission ask", tool.SentinelPermissionAsk + `{"tool":"bash"}`, true},
		{"unresolved question", tool.SentinelQuestionPrompt + `[{"header":"h","question":"q","options":[]}]` + "\n\n" + tool.SentinelWaitingForUser, true},
		{"answered permission ask", "go test ./... (exit 0)", false},
		{"dismissed question", tool.QuestionDismissedResult, false},
		{"ordinary tool result", "wrote the file", false},
		{"unrelated mention of the marker", "grep found WAITING_FOR_USER_RESPONSE in agent.go", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := messagesHavePendingAsk([]Message{{Role: "tool", Content: tc.content}}); got != tc.want {
				t.Fatalf("messagesHavePendingAsk = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestAdvisorGateIgnoresAsksOutsideTheCurrentTurn guards the scan's blast
// radius. The scan stops at the first assistant row walking backwards, so an ask
// the model has already spoken past is stale history and must NOT mute the
// advisor forever — otherwise one permission the user ignored (typing a new
// message instead of clicking Allow) would disable every later checkpoint in the
// session. Each ask row is preceded by the assistant tool-call row that raised
// it, as in a real transcript.
func TestAdvisorGateIgnoresAsksOutsideTheCurrentTurn(t *testing.T) {
	stale := []Message{
		{Role: "user", Content: "first request"},
		{Role: "assistant", ToolCalls: []ToolCall{newToolCall("c1", "bash", `{"command":"go test ./..."}`)}},
		{Role: "tool", ToolID: "c1", Content: tool.SentinelPermissionAsk + `{"tool":"bash"}`},
		{Role: "assistant", Content: "I could not run that, here is what I found instead"},
	}
	if messagesHavePendingAsk(stale) {
		t.Fatal("an ask the model has already replied past is stale history and must not count as a live dialog")
	}

	// The same round, unanswered: the ask is the last thing said, so a dialog is up.
	live := []Message{
		{Role: "user", Content: "first request"},
		{Role: "assistant", ToolCalls: []ToolCall{newToolCall("c1", "bash", `{"command":"go test ./..."}`)}},
		{Role: "tool", ToolID: "c1", Content: tool.SentinelPermissionAsk + `{"tool":"bash"}`},
	}
	if !messagesHavePendingAsk(live) {
		t.Fatal("an unanswered ask at the tail is a live dialog and must block the advisor")
	}

	// A tail injector appending a user-role row AFTER the ask must not hide it —
	// this is the reason the scan walks back from the end instead of starting at
	// the last user message.
	withInjectedTail := append(append([]Message(nil), live...),
		Message{Role: "user", Content: "[ocode:todo] still to do: fix the build"})
	if !messagesHavePendingAsk(withInjectedTail) {
		t.Fatal("a user-role tail injection after the ask must not hide a live dialog")
	}
}
