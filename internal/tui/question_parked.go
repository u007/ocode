package tui

import (
	"context"
	"slices"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/tool"
)

// Parked questions: a blocking question ask from a host-side caller (the Claude
// Code approval bridge's AskUserQuestion) that the TUI answers through its
// normal question dialog. The calling goroutine is parked on respCh until the
// user submits (answers), dismisses (Esc), or the ask ends (ctx or agent done).
//
// This mirrors the sub-agent permission asker in model.go: the asker runs in the
// caller's goroutine, hands the request to the Update loop over parkedQuestionCh,
// and a re-armed listener command picks it up. Unlike a model question, a parked
// one never writes a transcript sentinel and never re-steps the agent; its answer
// goes straight back to the asker as []tool.QuestionAnswerSet.

// parkedQuestionListenTimeout bounds how long listenParkedQuestion blocks. On
// expiry it re-arms via parkedQuestionKeepAliveMsg so the goroutine never leaks.
const parkedQuestionListenTimeout = 15 * time.Second

// parkedQuestion is one blocking ask parked on the TUI.
type parkedQuestion struct {
	prompts []tool.QuestionPrompt
	// respCh is buffered (1). The TUI delivers exactly once, then forgets the
	// parked pointer, so the send never blocks.
	respCh chan parkedQuestionResult
	// ctx and agentDone end the wait early; settled is closed by the asker when
	// it returns so the TUI's end watcher can exit. agentDone is the stop channel
	// the asker read for THIS ask (see newParkedQuestionAsker), and the end watcher
	// uses that same value, so the watcher and the asker cannot disagree.
	ctx       context.Context
	agentDone <-chan struct{}
	settled   chan struct{}
}

type parkedQuestionResult struct {
	answers   []tool.QuestionAnswerSet
	dismissed bool
}

// deliver hands the result to the parked asker. It never blocks.
func (p *parkedQuestion) deliver(r parkedQuestionResult) {
	select {
	case p.respCh <- r:
	default:
	}
}

// newParkedQuestionAsker returns the agent.QuestionAsker the TUI installs. ch is
// the listener channel the Update loop reads, mu serialises asks (one dialog at a
// time), and done returns the agent's CURRENT stop channel. It is called once per
// ask, after the dialog lock is taken: Agent.ResetCancellation replaces a closed
// stop channel at the start of each turn after an Esc, so a channel captured at
// wiring time would stay closed and dismiss every later question.
func newParkedQuestionAsker(ch chan *parkedQuestion, mu *sync.Mutex, done func() <-chan struct{}) agent.QuestionAsker {
	return func(ctx context.Context, prompts []tool.QuestionPrompt) ([]tool.QuestionAnswerSet, bool) {
		if ctx == nil {
			ctx = context.Background()
		}
		if len(prompts) == 0 {
			return nil, true
		}
		mu.Lock()
		defer mu.Unlock()
		if ctx.Err() != nil {
			return nil, true
		}
		agentDone := done()
		p := &parkedQuestion{
			prompts:   prompts,
			respCh:    make(chan parkedQuestionResult, 1),
			ctx:       ctx,
			agentDone: agentDone,
			settled:   make(chan struct{}),
		}
		defer close(p.settled)
		select {
		case ch <- p:
		case <-ctx.Done():
			return nil, true
		case <-agentDone:
			return nil, true
		}
		select {
		case r := <-p.respCh:
			return r.answers, r.dismissed
		case <-ctx.Done():
			return nil, true
		case <-agentDone:
			return nil, true
		}
	}
}

// parkedQuestionAskMsg carries a parked ask into the Update loop.
type parkedQuestionAskMsg struct{ q *parkedQuestion }

// parkedQuestionEndedMsg reports that a parked ask's wait ended (answered,
// cancelled, or the agent stopped). Handling it is idempotent: it only acts when
// the ask still holds the dialog or a queue slot.
type parkedQuestionEndedMsg struct{ q *parkedQuestion }

type parkedQuestionKeepAliveMsg struct{}

// listenParkedQuestion blocks for the next parked ask and returns it as a message.
func listenParkedQuestion(ch chan *parkedQuestion, ctx context.Context) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		select {
		case p := <-ch:
			return parkedQuestionAskMsg{q: p}
		case <-ctx.Done():
			// Cancelled by armParkedQuestionListener (re-arm) or model shutdown.
			return nil
		case <-time.After(parkedQuestionListenTimeout):
			return parkedQuestionKeepAliveMsg{}
		}
	}
}

// armParkedQuestionListener cancels any previously armed listener so re-arming
// (on Init, after each ask, or on keep-alive) never multiplies goroutines.
func (m *model) armParkedQuestionListener() tea.Cmd {
	if m.parkedQuestionCh == nil {
		return nil
	}
	if m.parkedQuestionCancel != nil {
		m.parkedQuestionCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.parkedQuestionCancel = cancel
	return listenParkedQuestion(m.parkedQuestionCh, ctx)
}

// waitParkedQuestionEnd returns when the parked ask's wait is over, whichever
// way it ended, and reports it so the TUI closes the dialog or queue slot. It
// always reports, even after a normal answer; the handler treats that as a no-op.
// Reporting on every exit avoids a race where ctx and settled are both ready and
// a "cancelled" exit would otherwise return nothing and leave the dialog open.
func waitParkedQuestionEnd(p *parkedQuestion) tea.Cmd {
	return func() tea.Msg {
		select {
		case <-p.ctx.Done():
		case <-p.agentDone:
		case <-p.settled:
		}
		return parkedQuestionEndedMsg{q: p}
	}
}

// releaseParkedQuestion closes a parked ask that ended on its own. If it holds the
// dialog, the dialog closes and the next queued ask is promoted. If it is still
// queued, its slot is removed. Anything else is a no-op.
func (m *model) releaseParkedQuestion(p *parkedQuestion) {
	if p == nil {
		return
	}
	if m.parkedQuestion == p {
		m.parkedQuestion = nil
		m.clearQuestionPrompt()
		m.promoteNextQueuedAsk()
		return
	}
	for i, q := range m.questionAskQueue {
		if q.parked == p {
			m.questionAskQueue = slices.Delete(m.questionAskQueue, i, i+1)
			m.syncPendingAskFlag()
			return
		}
	}
}
