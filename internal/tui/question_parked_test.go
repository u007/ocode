package tui

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/u007/ocode/internal/tool"
)

// parkedAskResult is what a parked asker returned to its caller.
type parkedAskResult struct {
	answers   []tool.QuestionAnswerSet
	dismissed bool
}

// startParkedQuestion runs a parked asker in a goroutine, the way the Claude
// Code bridge calls it, and returns the channel its result lands on. The asker is
// built from the model's own channels, the same ones wireCompactCallbacks uses.
func startParkedQuestion(t *testing.T, ctx context.Context, m model, done <-chan struct{}, prompts ...tool.QuestionPrompt) <-chan parkedAskResult {
	t.Helper()
	return startParkedQuestionWithDone(t, ctx, m, func() <-chan struct{} { return done }, prompts...)
}

// startParkedQuestionWithDone is startParkedQuestion with the stop channel read
// per ask, as the TUI wires it (pass agent.Done for a live agent).
func startParkedQuestionWithDone(t *testing.T, ctx context.Context, m model, done func() <-chan struct{}, prompts ...tool.QuestionPrompt) <-chan parkedAskResult {
	t.Helper()
	asker := newParkedQuestionAsker(m.parkedQuestionCh, m.parkedQuestionMu, done)
	out := make(chan parkedAskResult, 1)
	go func() {
		answers, dismissed := asker(ctx, prompts)
		out <- parkedAskResult{answers: answers, dismissed: dismissed}
	}()
	return out
}

// nextParkedAsk receives the ask through the real listener command.
func nextParkedAsk(t *testing.T, m model) parkedQuestionAskMsg {
	t.Helper()
	msg := listenParkedQuestion(m.parkedQuestionCh, context.Background())()
	ask, ok := msg.(parkedQuestionAskMsg)
	if !ok {
		t.Fatalf("listener returned %T, want parkedQuestionAskMsg", msg)
	}
	return ask
}

func awaitParkedResult(t *testing.T, ch <-chan parkedAskResult) parkedAskResult {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(2 * time.Second):
		t.Fatal("parked asker did not return")
		return parkedAskResult{}
	}
}

func TestQuestionParkedOpensNormalDialog(t *testing.T) {
	m := newPermQueueTestModel(t)
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startParkedQuestion(t, ctx, m, done, askOne("Scope", "Which package?"))

	upd, _ := m.Update(nextParkedAsk(t, m))
	m = derefTestModel(t, upd)

	if !m.showQuestionDialog {
		t.Fatal("showQuestionDialog = false, want the parked question dialog open")
	}
	if m.parkedQuestion == nil {
		t.Fatal("parkedQuestion = nil, want the open dialog marked as parked")
	}
	if m.questionToolCallID != "" {
		t.Errorf("questionToolCallID = %q, want empty: a parked ask has no tool call to resume", m.questionToolCallID)
	}
	if len(m.questionPrompts) != 1 || m.questionPrompts[0].Header != "Scope" {
		t.Fatalf("questionPrompts = %+v, want the single 'Scope' prompt", m.questionPrompts)
	}
	if view := stripANSI(m.renderQuestionDialog(88)); !strings.Contains(view, "Which package?") || !strings.Contains(view, "yes") {
		t.Errorf("dialog view missing the question or options:\n%s", view)
	}
}

func TestQuestionParkedSelectionDeliversAnswerSet(t *testing.T) {
	m := newPermQueueTestModel(t)
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := startParkedQuestion(t, ctx, m, done, askOne("Scope", "Which package?"))

	upd, _ := m.Update(nextParkedAsk(t, m))
	m = derefTestModel(t, upd)
	upd, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = derefTestModel(t, upd)
	upd, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = derefTestModel(t, upd)

	got := awaitParkedResult(t, result)
	if got.dismissed {
		t.Fatal("dismissed = true, want an answered ask")
	}
	want := []tool.QuestionAnswerSet{{
		Header:   "Scope",
		Question: "Which package?",
		Answers:  []tool.QuestionAnswer{{Label: "no"}},
	}}
	if !reflect.DeepEqual(got.answers, want) {
		t.Fatalf("answers = %#v, want %#v", got.answers, want)
	}
	if m.showQuestionDialog || m.parkedQuestion != nil {
		t.Errorf("dialog still open after answer: show=%v parked=%v", m.showQuestionDialog, m.parkedQuestion != nil)
	}
	for _, msg := range m.messages {
		if msg.raw != nil && msg.raw.Role == "tool" {
			t.Fatalf("parked answer wrote a tool transcript row %q; it must go only to the asker", msg.raw.Content)
		}
	}
}

func TestQuestionParkedMultiSelectDeliversBothLabels(t *testing.T) {
	m := newPermQueueTestModel(t)
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	prompt := tool.QuestionPrompt{
		Header:   "Targets",
		Question: "Which targets?",
		Options:  []tool.QuestionOption{{Label: "alpha"}, {Label: "beta"}, {Label: "gamma"}},
		Multiple: true,
	}
	result := startParkedQuestion(t, ctx, m, done, prompt)

	upd, _ := m.Update(nextParkedAsk(t, m))
	m = derefTestModel(t, upd)
	// Pick alpha (index 0) and gamma (index 2), then move onto Submit.
	m.toggleQuestionSelection(0, 0)
	m.toggleQuestionSelection(0, 2)
	m.questionCursor[0] = questionSubmitIndex(prompt)
	upd, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = derefTestModel(t, upd)

	got := awaitParkedResult(t, result)
	if got.dismissed {
		t.Fatal("dismissed = true, want an answered ask")
	}
	if len(got.answers) != 1 {
		t.Fatalf("got %d answer sets, want 1", len(got.answers))
	}
	var labels []string
	for _, a := range got.answers[0].Answers {
		labels = append(labels, a.Label)
	}
	if !reflect.DeepEqual(labels, []string{"alpha", "gamma"}) {
		t.Fatalf("labels = %v, want [alpha gamma]", labels)
	}
}

func TestQuestionParkedEscDeliversDismissed(t *testing.T) {
	m := newPermQueueTestModel(t)
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := startParkedQuestion(t, ctx, m, done, askOne("Scope", "Which package?"))

	upd, _ := m.Update(nextParkedAsk(t, m))
	m = derefTestModel(t, upd)
	upd, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = derefTestModel(t, upd)

	got := awaitParkedResult(t, result)
	if !got.dismissed {
		t.Fatalf("dismissed = false, want true after Esc; answers=%#v", got.answers)
	}
	if got.answers != nil {
		t.Errorf("answers = %#v, want nil on dismiss", got.answers)
	}
	if m.showQuestionDialog || m.parkedQuestion != nil {
		t.Errorf("dialog still open after Esc: show=%v parked=%v", m.showQuestionDialog, m.parkedQuestion != nil)
	}
}

func TestQuestionParkedContextCancelUnblocksAsDismissed(t *testing.T) {
	m := newPermQueueTestModel(t)
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := startParkedQuestion(t, ctx, m, done, askOne("Scope", "Which package?"))

	ask := nextParkedAsk(t, m)
	upd, _ := m.Update(ask)
	m = derefTestModel(t, upd)
	if !m.showQuestionDialog {
		t.Fatal("expected the parked dialog to open before cancelling")
	}

	cancel()
	got := awaitParkedResult(t, result)
	if !got.dismissed || got.answers != nil {
		t.Fatalf("got answers=%#v dismissed=%v, want nil and dismissed=true", got.answers, got.dismissed)
	}

	// The end watcher reports the cancel to the TUI, which closes the dialog.
	end, ok := waitParkedQuestionEnd(ask.q)().(parkedQuestionEndedMsg)
	if !ok {
		t.Fatal("end watcher did not report the ask as ended")
	}
	upd, _ = m.Update(end)
	m = derefTestModel(t, upd)
	if m.showQuestionDialog || m.parkedQuestion != nil {
		t.Errorf("cancelled ask left its dialog open: show=%v parked=%v", m.showQuestionDialog, m.parkedQuestion != nil)
	}
}

func TestQuestionParkedQueuesBehindOpenDialog(t *testing.T) {
	m := newPermQueueTestModel(t)
	m = deliverAsks(t, m, questionToolMsg(t, "q_a", askOne("Scope", "Which package?")))

	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := startParkedQuestion(t, ctx, m, done, askOne("Deploy", "Staging or prod?"))
	ask := nextParkedAsk(t, m)
	upd, _ := m.Update(ask)
	m = derefTestModel(t, upd)

	if m.questionToolCallID != "q_a" || m.parkedQuestion != nil {
		t.Fatalf("parked ask took the dialog from q_a: toolCallID=%q parked=%v", m.questionToolCallID, m.parkedQuestion != nil)
	}
	if len(m.questionAskQueue) != 1 || m.questionAskQueue[0].parked != ask.q {
		t.Fatalf("parked ask not queued behind the open dialog: queue=%+v", m.questionAskQueue)
	}

	// Answering the normal prompt promotes the parked one, and that answer
	// does not resume the agent or write a transcript row.
	upd, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = derefTestModel(t, upd)
	if m.parkedQuestion != ask.q || !m.showQuestionDialog {
		t.Fatalf("parked ask not promoted after the normal answer: parked=%v show=%v", m.parkedQuestion != nil, m.showQuestionDialog)
	}
	upd, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = derefTestModel(t, upd)
	if got := awaitParkedResult(t, result); !got.dismissed {
		t.Fatalf("expected dismissed after Esc on the promoted parked prompt, got %#v", got)
	}
}
