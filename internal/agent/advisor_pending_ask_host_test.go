package agent

import (
	"testing"

	"github.com/u007/ocode/internal/tool"
)

// strippedSnapshotTranscript is what the TUI actually hands to Step: the
// PERMISSION_ASK / QUESTION_PROMPT sentinel row is REMOVED, because a raw
// sentinel in the LLM input is a malformed tool-call/tool-result pairing the
// model cannot recover from (buildAgentMessagesSnapshot in internal/tui).
//
// The assistant row that RAISED the ask survives, so the round still looks
// like it produced a tool call whose result is missing.
func strippedSnapshotTranscript() []Message {
	return []Message{
		{Role: "user", Content: "fix the failing build"},
		{Role: "assistant", ToolCalls: []ToolCall{newToolCall("call-bash", "bash", `{"command":"go test ./..."}`)}},
	}
}

// TestAdvisorCheckpointsDoNotFireOnHostDeclaredPendingAsk is the regression
// test for the TUI half of the "an advisor call started underneath a permission
// dialog" bug.
//
// The agent-side transcript scan (messagesHavePendingAsk) is only sound when the
// ask SENTINEL is still in the slice the agent was handed. The TUI strips it
// before Step, so the scan sees nothing and the advisor checkpoint gate was
// INERT there: a plan/done checkpoint could start a second model (minutes, for
// the Claude Code backend) while the user was still looking at the dialog.
//
// The fix is a host-declared flag (Agent.SetPendingAsk). This test drives a
// real Step with the stripped snapshot plus the flag set, and asserts the
// advisor did NOT run.
func TestAdvisorCheckpointsDoNotFireOnHostDeclaredPendingAsk(t *testing.T) {
	t.Setenv("OPENCODE_ADVISOR_MODEL", "")
	a, fake := checkpointTestAgent(t, []string{"plan", "done"})
	a.tools["write"] = &checkpointFakeWrite{}
	// Three scripted responses so the assertion below is reached either way:
	// with the gate shut the turn finishes early; with the gate open (a mutant)
	// both checkpoints fire and drive the loop around to response 3, and the
	// fake.calls assertion reports the bug directly instead of dying on a
	// spent script.
	a.client = &scriptedClient{msgs: []*Message{
		{Role: "assistant", Content: "writing the fix", ToolCalls: []ToolCall{newToolCall("call-w", "write", `{"path":"a.go","content":"x"}`)}},
		{Role: "assistant", Content: "the review landed, finishing up"},
		{Role: "assistant", Content: "done"},
	}, errs: []error{nil, nil, nil}}

	// The host declares that a dialog is on screen — exactly what the TUI does
	// now that it strips the sentinel.
	a.SetPendingAsk(true)

	msgs, err := a.Step(strippedSnapshotTranscript())
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if fake.calls != 0 {
		t.Fatalf("advisor ran %d time(s) while a permission dialog was still pending; it must wait for the user", fake.calls)
	}
	if len(msgs) == 0 {
		t.Fatal("Step returned no messages")
	}
}

// TestAdvisorCheckpointsFireAfterHostClearsPendingAsk is the other half of the
// contract: the suppression is scoped to the ask. Once the host clears the
// flag the checkpoints must fire again — otherwise the gate would silently
// disable the advisor for the rest of the session.
//
// This is the shape of the real TUI flow: the user answers the dialog, the
// host clears the flag, and the CONTINUATION Step is where the completion
// review normally happens.
func TestAdvisorCheckpointsFireAfterHostClearsPendingAsk(t *testing.T) {
	t.Setenv("OPENCODE_ADVISOR_MODEL", "")
	a, fake := checkpointTestAgent(t, []string{"plan", "done"})
	a.tools["write"] = &checkpointFakeWrite{}
	a.client = &scriptedClient{msgs: []*Message{
		{Role: "assistant", Content: "writing the fix", ToolCalls: []ToolCall{newToolCall("call-w", "write", `{"path":"a.go","content":"x"}`)}},
		{Role: "assistant", Content: "the review landed, finishing up"},
		{Role: "assistant", Content: "done"},
	}, errs: []error{nil, nil, nil}}

	// The dialog was answered: the host clears the flag before the
	// continuation Step.
	a.SetPendingAsk(false)

	if _, err := a.Step(strippedSnapshotTranscript()); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if fake.calls == 0 {
		t.Fatal("advisor never ran even though the host cleared the pending-ask flag; the gate must not disable it permanently")
	}
}

// TestPendingAskFlagIsPerAgent pins that the flag is a per-agent atomic, not a
// package global: two agents in the same process (a main agent and a dispatched
// sub-agent) must never affect each other. A package global would let one
// session's open dialog mute another session's advisor.
func TestPendingAskFlagIsPerAgent(t *testing.T) {
	t.Setenv("OPENCODE_ADVISOR_MODEL", "")
	a1, _ := checkpointTestAgent(t, []string{"plan"})
	a2, _ := checkpointTestAgent(t, []string{"plan"})

	a1.SetPendingAsk(true)
	if !a1.PendingAsk() {
		t.Fatal("agent 1 should report a pending ask")
	}
	if a2.PendingAsk() {
		t.Fatal("agent 2 must not see agent 1's pending ask")
	}

	a1.SetPendingAsk(false)
	if a1.PendingAsk() {
		t.Fatal("clearing the flag on agent 1 must take effect")
	}
}

// TestHostDeclaredPendingAskAlsoBlocksAskRaisedByTheStepItself pins that the
// LIVE flag is consulted at checkpoint time, not only when the Step started.
//
// A Step can raise its OWN ask (a tool call that needs permission). The flag
// is only updated by the host AFTER the Step returns, so a checkpoint that
// fired inside that same Step would still start a second model underneath a
// dialog the user is about to be shown.
func TestHostDeclaredPendingAskAlsoBlocksAskRaisedByTheStepItself(t *testing.T) {
	t.Setenv("OPENCODE_ADVISOR_MODEL", "")
	a, fake := checkpointTestAgent(t, []string{"plan", "done"})
	a.tools["write"] = &checkpointFakeWrite{}
	a.client = &scriptedClient{msgs: []*Message{
		{Role: "assistant", Content: "writing the fix", ToolCalls: []ToolCall{newToolCall("call-w", "write", `{"path":"a.go","content":"x"}`)}},
		{Role: "assistant", Content: "done"},
	}, errs: []error{nil, nil}}

	st := a.newAdvisorCheckpointState("fix the build", false, false)
	resp := &Message{
		Role:      "assistant",
		Content:   "I will edit a.go.",
		ToolCalls: []ToolCall{newToolCall("call-w", "write", `{"path":"a.go","content":"x"}`)},
	}
	st.countBatch(resp.ToolCalls)

	// The flag is still false at this point, so the plan checkpoint may fire.
	if review := a.advisorPlanCheckpoint(st, resp); review == nil {
		t.Fatal("expected the plan checkpoint to fire when no ask is pending")
	}
	if fake.calls != 1 {
		t.Fatalf("expected 1 advisor call, got %d", fake.calls)
	}

	// Now the host raises the flag: the SAME step's later checkpoint must
	// stand down even though the checkpoint state's snapshot said "no ask".
	a.SetPendingAsk(true)
	st2 := a.newAdvisorCheckpointState("fix the build", false, false)
	st2.countBatch(resp.ToolCalls)
	if review := a.advisorPlanCheckpoint(st2, resp); review != nil {
		t.Fatal("expected the plan checkpoint to stand down once the host declared a pending ask")
	}
	if fake.calls != 1 {
		t.Fatalf("advisor ran again (%d calls) despite a declared pending ask", fake.calls)
	}
}

// TestPendingAskFlagDoesNotMaskTheTranscriptScan pins that the two sources are
// OR'd, not replaced. A host that never calls SetPendingAsk (the server keeps
// the sentinel in as.messages) must still be protected by the scan.
func TestPendingAskFlagDoesNotMaskTheTranscriptScan(t *testing.T) {
	t.Setenv("OPENCODE_ADVISOR_MODEL", "")
	a, fake := checkpointTestAgent(t, []string{"plan", "done"})
	a.tools["write"] = &checkpointFakeWrite{}
	a.client = &scriptedClient{msgs: []*Message{
		{Role: "assistant", Content: "writing the fix", ToolCalls: []ToolCall{newToolCall("call-w", "write", `{"path":"a.go","content":"x"}`)}},
		{Role: "assistant", Content: "the review landed, finishing up"},
		{Role: "assistant", Content: "done"},
	}, errs: []error{nil, nil, nil}}

	// No host flag at all — the sentinel is still in the transcript.
	if _, err := a.Step(pendingAskTranscript()); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if fake.calls != 0 {
		t.Fatalf("advisor ran %d time(s) while the transcript still carried an unanswered ask", fake.calls)
	}
}

// TestStrippedSnapshotHidesTheAskFromTheScan documents WHY the host flag
// exists. If this ever starts failing, the TUI's strip changed shape and the
// host flag may no longer be needed (or may be hiding a different bug).
func TestStrippedSnapshotHidesTheAskFromTheScan(t *testing.T) {
	if !messagesHavePendingAsk(pendingAskTranscript()) {
		t.Fatal("the raw transcript should show a pending ask")
	}
	if messagesHavePendingAsk(strippedSnapshotTranscript()) {
		t.Fatal("expected the stripped snapshot to hide the ask from the scan")
	}
	// And the sentinel the TUI strips is the one the scan looks for.
	if !tool.UnansweredAsk(tool.SentinelPermissionAsk + `{"tool":"bash"}`) {
		t.Fatal("the stripped content is not an unanswered ask")
	}
}

// TestAdvisorCheckpointStateOrsBothPendingAskSources pins the merge rule of
// the two sources.
//
// The host flag and the transcript scan are OR'd into ONE field, so each source
// alone must be enough to block, and only a state with BOTH clear may proceed.
// This is the unit-level contract behind the Step-level tests: the scan is the
// server's source (the sentinel survives there), the flag is the TUI's (the
// sentinel is stripped).
func TestAdvisorCheckpointStateOrsBothPendingAskSources(t *testing.T) {
	t.Setenv("OPENCODE_ADVISOR_MODEL", "")
	a, _ := checkpointTestAgent(t, []string{"plan"})

	cases := []struct {
		name        string
		hostFlag    bool
		scanPending bool
		wantPending bool
	}{
		{"neither source", false, false, false},
		{"host flag only", true, false, true},
		{"transcript scan only", false, true, true},
		{"both sources", true, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := a.newAdvisorCheckpointState("goal", tc.hostFlag, tc.scanPending)
			if st.pendingAsk != tc.wantPending {
				t.Fatalf("pendingAsk = %v, want %v", st.pendingAsk, tc.wantPending)
			}
		})
	}
}

// TestAdvisorCheckpointBlockedByEitherPendingAskSource pins that BOTH the
// snapshot AND the live flag block the checkpoint.
//
// The snapshot is what the Step captured when it began; the live flag is what
// the host declares right now. Either one standing down must keep the advisor
// quiet — a checkpoint that only consulted one of them would start a second
// model in exactly the case where a dialog is up.
func TestAdvisorCheckpointBlockedByEitherPendingAskSource(t *testing.T) {
	t.Setenv("OPENCODE_ADVISOR_MODEL", "")
	a, fake := checkpointTestAgent(t, []string{"plan", "done"})

	resp := &Message{
		Role:      "assistant",
		Content:   "I will edit a.go.",
		ToolCalls: []ToolCall{newToolCall("call-w", "write", `{"path":"a.go","content":"x"}`)},
	}

	t.Run("snapshot only", func(t *testing.T) {
		a.SetPendingAsk(false)
		st := a.newAdvisorCheckpointState("goal", true, false)
		st.countBatch(resp.ToolCalls)
		if review := a.advisorPlanCheckpoint(st, resp); review != nil {
			t.Fatal("the snapshot's pending-ask state must block the checkpoint")
		}
	})

	t.Run("live flag only", func(t *testing.T) {
		a.SetPendingAsk(true)
		defer a.SetPendingAsk(false)
		st := a.newAdvisorCheckpointState("goal", false, false)
		st.countBatch(resp.ToolCalls)
		if review := a.advisorPlanCheckpoint(st, resp); review != nil {
			t.Fatal("the live pending-ask flag must block the checkpoint")
		}
	})

	if fake.calls != 0 {
		t.Fatalf("advisor ran %d time(s) while an ask was pending", fake.calls)
	}
}
