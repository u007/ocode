package tui

import (
	"encoding/json"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/tool"
)

// Regression: the question prompt had the same single-slot defect the
// permission dialog had.
//
// `question` is Parallel() == false, so no parallel batch is needed to reach it
// — a single assistant message carrying TWO question tool calls is enough.
// internal/agent/agent.go's Step collects the whole round's results and hands
// each to OnMessage in order, so both QUESTION_PROMPT: tool messages arrive
// back-to-back in one frame. appendAgentMessage's parseQuestionPrompt branch
// then called startQuestionPrompt and assigned m.rcPendingQuestion
// unconditionally, so the second prompt overwrote the first: the user answered
// the later question while the earlier one sat in the transcript unanswered.
//
// The invariant: question prompts are presented one at a time, in the order the
// agent produced them, and none is discarded.

func questionToolMsg(t *testing.T, toolCallID string, prompts ...tool.QuestionPrompt) agent.Message {
	t.Helper()
	payload, err := json.Marshal(prompts)
	if err != nil {
		t.Fatalf("marshal question prompts: %v", err)
	}
	return agent.Message{
		Role:    "tool",
		ToolID:  toolCallID,
		Content: tool.SentinelQuestionPrompt + "\n" + string(payload),
	}
}

func askOne(header, question string) tool.QuestionPrompt {
	return tool.QuestionPrompt{
		Header:   header,
		Question: question,
		Options:  []tool.QuestionOption{{Label: "yes"}, {Label: "no"}},
	}
}

func TestQuestionQueue_SecondPromptDoesNotReplaceTheFirst(t *testing.T) {
	m := newPermQueueTestModel(t)
	m = deliverAsks(t, m,
		questionToolMsg(t, "q_a", askOne("Scope", "Which package?")),
		questionToolMsg(t, "q_b", askOne("Deploy", "Staging or prod?")),
	)

	if !m.showQuestionDialog {
		t.Fatal("showQuestionDialog = false, want the first prompt's dialog open")
	}
	if m.questionToolCallID != "q_a" {
		t.Errorf("questionToolCallID = %q, want q_a (the first prompt must win the dialog)", m.questionToolCallID)
	}
	if len(m.questionPrompts) != 1 || m.questionPrompts[0].Header != "Scope" {
		t.Errorf("questionPrompts = %+v, want the single 'Scope' prompt", m.questionPrompts)
	}
	if len(m.questionAskQueue) != 1 {
		t.Fatalf("questionAskQueue has %d entries, want 1 (the second prompt must be queued, not dropped)", len(m.questionAskQueue))
	}
	if m.questionAskQueue[0].toolCallID != "q_b" {
		t.Errorf("queued toolCallID = %q, want q_b", m.questionAskQueue[0].toolCallID)
	}
	// Both prompts still get a transcript line, in order.
	seenA, seenB := 0, 0
	for _, msg := range m.messages {
		if msg.text == renderQuestionTranscriptNotice([]tool.QuestionPrompt{askOne("Scope", "Which package?")}) {
			seenA++
		}
		if msg.text == renderQuestionTranscriptNotice([]tool.QuestionPrompt{askOne("Deploy", "Staging or prod?")}) {
			seenB++
		}
	}
	if seenA != 1 || seenB != 1 {
		t.Errorf("transcript prompt lines: first=%d second=%d, want 1 and 1", seenA, seenB)
	}
}

func TestQuestionQueue_AnsweringOnePromotesTheNextAndHoldsTheTurn(t *testing.T) {
	m := newPermQueueTestModel(t)
	m = deliverAsks(t, m,
		questionToolMsg(t, "q_a", askOne("Scope", "Which package?")),
		questionToolMsg(t, "q_b", askOne("Deploy", "Staging or prod?")),
		questionToolMsg(t, "q_c", askOne("Owner", "Who reviews?")),
	)
	if len(m.questionAskQueue) != 2 {
		t.Fatalf("questionAskQueue has %d entries, want 2", len(m.questionAskQueue))
	}

	// Answer the first question: select an option and submit.
	m.selectQuestionOption(0, 0)
	upd, cmd := m.submitQuestionAnswers()
	m = upd.(model)

	// The turn must NOT resume while prompts remain unanswered — resuming would
	// let the agent act on a transcript whose remaining QUESTION_PROMPT
	// sentinels are stripped by buildAgentMessagesSnapshot, so each would come
	// back as an orphan re-execution rather than the user's decision.
	if cmd != nil {
		t.Error("submitQuestionAnswers returned a command (askAgent) while 2 prompts remain unanswered")
	}
	if !m.showQuestionDialog {
		t.Fatal("showQuestionDialog = false after answering prompt 1, want prompt 2 promoted")
	}
	if m.questionToolCallID != "q_b" {
		t.Errorf("questionToolCallID = %q, want q_b", m.questionToolCallID)
	}
	if len(m.questionAskQueue) != 1 || m.questionAskQueue[0].toolCallID != "q_c" {
		t.Fatalf("questionAskQueue = %+v, want exactly [q_c]", m.questionAskQueue)
	}
	// The answered prompt's tool result must carry its own id.
	var answered string
	for _, msg := range m.messages {
		if msg.raw != nil && msg.raw.Role == "tool" && msg.raw.Content != "" &&
			msg.raw.Content[0] == '[' && msg.raw.ToolID != "" {
			answered = msg.raw.ToolID
		}
	}
	if answered != "q_a" {
		t.Errorf("answer was recorded against tool id %q, want q_a", answered)
	}

	// Answering the second promotes the third, still without resuming.
	m.selectQuestionOption(0, 0)
	upd, cmd = m.submitQuestionAnswers()
	m = upd.(model)
	if cmd != nil {
		t.Error("turn resumed while 1 prompt remained unanswered")
	}
	if m.questionToolCallID != "q_c" {
		t.Errorf("questionToolCallID = %q, want q_c", m.questionToolCallID)
	}

	// The final answer drains the queue and resumes the turn.
	m.selectQuestionOption(0, 0)
	upd, cmd = m.submitQuestionAnswers()
	m = upd.(model)
	if cmd == nil {
		t.Error("turn did not resume after the last prompt was answered")
	}
	if m.showQuestionDialog {
		t.Error("showQuestionDialog = true after the final answer, want false")
	}
	if len(m.questionAskQueue) != 0 {
		t.Errorf("questionAskQueue = %+v, want empty", m.questionAskQueue)
	}
	if m.questionToolCallID != "" {
		t.Errorf("questionToolCallID = %q, want \"\" once the queue drains", m.questionToolCallID)
	}
}

// Esc dismisses the current prompt WITHOUT answering it and does not resume the
// turn. It is still a terminal outcome for that prompt, so the next one must be
// promoted — otherwise the rest of the round is stranded behind a closed dialog.
func TestQuestionQueue_DismissPromotesTheNextWithoutResuming(t *testing.T) {
	m := newPermQueueTestModel(t)
	m = deliverAsks(t, m,
		questionToolMsg(t, "q_a", askOne("Scope", "Which package?")),
		questionToolMsg(t, "q_b", askOne("Deploy", "Staging or prod?")),
	)

	upd, cmd := m.handleQuestionKeys(tea.KeyPressMsg{Code: tea.KeyEscape}, nil, nil)
	m = upd.(model)
	if cmd != nil {
		t.Error("Esc returned a command; dismissing a prompt must not resume the agent")
	}
	if !m.showQuestionDialog {
		t.Fatal("showQuestionDialog = false after Esc; the next prompt must be promoted")
	}
	if m.questionToolCallID != "q_b" {
		t.Errorf("questionToolCallID = %q, want q_b", m.questionToolCallID)
	}
}

// A queued question is just as dead as a dropped permission ask once its
// sentinel is stripped on resume, so the turn must be held while one is pending
// and released only when the queue drains.
func TestQuestionQueue_UnansweredPromptHoldsTheTurn(t *testing.T) {
	m := newPermQueueTestModel(t)
	m = deliverAsks(t, m,
		questionToolMsg(t, "q_a", askOne("Scope", "Which package?")),
		questionToolMsg(t, "q_b", askOne("Deploy", "Staging or prod?")),
	)

	// A tool-result delivery carrying no QUESTION_PROMPT sentinel of its own
	// cannot be stopped by the sentinel scan — only the dialog guard can.
	repl := agent.Message{Role: "tool", ToolID: "some_other_call", Content: "ok"}
	upd, cmd := m.Update([]agent.Message{repl})
	after := upd.(model)
	if cmd != nil {
		t.Error("Update returned a command (askAgent) while a question prompt is still unanswered")
	}
	if !after.showQuestionDialog || after.questionToolCallID != "q_a" {
		t.Errorf("dialog=%v toolCallID=%q, want prompt q_a still pending", after.showQuestionDialog, after.questionToolCallID)
	}
}

// An ask names a tool call in the round that produced it, so the queue must not
// survive a session change.
func TestQuestionQueue_ClearedWhenTheTranscriptIsRebuilt(t *testing.T) {
	m := newPermQueueTestModel(t)
	m = deliverAsks(t, m,
		questionToolMsg(t, "q_a", askOne("Scope", "Which package?")),
		questionToolMsg(t, "q_b", askOne("Deploy", "Staging or prod?")),
	)
	if !m.showQuestionDialog || len(m.questionAskQueue) != 1 {
		t.Fatalf("precondition: dialog=%v queue=%d", m.showQuestionDialog, len(m.questionAskQueue))
	}

	m.clearQuestionAskState()
	if m.showQuestionDialog {
		t.Error("showQuestionDialog = true after clearQuestionAskState, want false")
	}
	if len(m.questionAskQueue) != 0 {
		t.Errorf("questionAskQueue = %+v, want empty", m.questionAskQueue)
	}
	if m.questionToolCallID != "" || len(m.questionPrompts) != 0 || m.rcPendingQuestion != nil {
		t.Errorf("question state not cleared: callID=%q prompts=%d rc=%v", m.questionToolCallID, len(m.questionPrompts), m.rcPendingQuestion != nil)
	}
}

// A question prompt and a permission ask are two INDEPENDENT FIFOs: whichever
// arrives first takes the dialog and the other queues in its own queue. They do
// not block each other, and neither can evict the other.
//
// This is a deliberate decision, pinned here because it is otherwise implicit: a
// shared queue would have to pick an interleaving between two different prompt
// shapes (option lists vs a command/permission summary), and either pick is
// arbitrary. Round order within ONE batch is still preserved, because the agent
// hands results to OnMessage in tool-call order.
func TestQuestionQueue_PermissionAndQuestionAreIndependentFifos(t *testing.T) {
	m := newPermQueueTestModel(t)

	// A permission ask first, then a question prompt.
	m = deliverAsks(t, m,
		permAskToolMsg(t, "call_a", "webfetch", "https://a.example"),
		questionToolMsg(t, "q_a", askOne("Scope", "Which package?")),
	)
	if !m.showPermDialog || m.pendingToolCallID != "call_a" {
		t.Fatalf("permission dialog = %v callID=%q, want the permission ask on screen", m.showPermDialog, m.pendingToolCallID)
	}
	if m.showQuestionDialog {
		t.Error("showQuestionDialog = true while the permission dialog holds the slot")
	}
	if len(m.questionAskQueue) != 1 || m.questionAskQueue[0].toolCallID != "q_a" {
		t.Fatalf("questionAskQueue = %+v, want exactly [q_a]", m.questionAskQueue)
	}
	if len(m.permAskQueue) != 0 {
		t.Errorf("permAskQueue = %+v, want empty (the question must not land in the permission queue)", m.permAskQueue)
	}

	// Answering the permission ask promotes the queued question prompt.
	m.permDialogInput("y")
	if !m.showQuestionDialog {
		t.Fatal("showQuestionDialog = false, want the queued question prompt promoted")
	}
	if m.questionToolCallID != "q_a" {
		t.Errorf("questionToolCallID = %q, want q_a", m.questionToolCallID)
	}
	// The permission slot is now free and must not have kept the old ask.
	if m.showPermDialog {
		t.Error("showPermDialog = true after the permission ask was answered")
	}
	if m.pendingToolCallID != "" {
		t.Errorf("pendingToolCallID = %q, want \"\" after the slot drained", m.pendingToolCallID)
	}
}

// Reversed arrival: a question prompt first, then a permission ask. The question
// keeps the dialog and the permission ask queues in its own queue.
func TestQuestionQueue_QuestionFirstThenPermissionQueuesInTheOtherFifo(t *testing.T) {
	m := newPermQueueTestModel(t)
	m = deliverAsks(t, m,
		questionToolMsg(t, "q_a", askOne("Scope", "Which package?")),
		permAskToolMsg(t, "call_a", "webfetch", "https://a.example"),
	)
	if !m.showQuestionDialog || m.questionToolCallID != "q_a" {
		t.Fatalf("question dialog = %v callID=%q, want the question prompt on screen", m.showQuestionDialog, m.questionToolCallID)
	}
	if m.showPermDialog {
		t.Error("showPermDialog = true; the permission ask must queue, not evict the question prompt")
	}
	if len(m.permAskQueue) != 1 || m.permAskQueue[0].toolCallID != "call_a" {
		t.Fatalf("permAskQueue = %+v, want exactly [call_a]", m.permAskQueue)
	}

	// Answering the question hands the screen to the queued permission ask — it
	// is NOT left stranded behind a closed dialog — and the turn does not resume
	// while an ask is still outstanding.
	m.selectQuestionOption(0, 0)
	upd, cmd := m.submitQuestionAnswers()
	m = upd.(model)
	if cmd != nil {
		t.Error("turn resumed while a permission ask was still queued")
	}
	if !m.showPermDialog {
		t.Error("showPermDialog = false, want the queued permission ask promoted after the question was answered")
	}
	if m.pendingToolCallID != "call_a" {
		t.Errorf("pendingToolCallID = %q, want call_a", m.pendingToolCallID)
	}
	if len(m.permAskQueue) != 0 {
		t.Errorf("permAskQueue = %+v, want empty (the ask was promoted, not dropped)", m.permAskQueue)
	}
	if m.showQuestionDialog {
		t.Error("showQuestionDialog = true as well; only ONE modal dialog may be open at a time")
	}
}
