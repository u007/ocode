package tui

import (
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/tool"
)

// TestSyncPendingAskFlagTracksEveryLiveAsk pins the TUI half of the fix.
//
// WHY: the TUI strips the PERMISSION_ASK / QUESTION_PROMPT sentinel rows from
// the transcript BEFORE handing it to Agent.Step — a raw sentinel in the LLM
// input is a malformed tool-call/tool-result pairing the model cannot recover
// from. That strip also hid the ask from the AGENT's own "is a dialog open"
// scan, so the advisor checkpoint gate was INERT in the TUI: a plan/done
// checkpoint could start a second model (minutes, for the Claude Code backend)
// underneath a permission dialog the user was still looking at.
//
// The TUI therefore declares the ask explicitly. This test pins that the
// declaration covers EVERY live ask — the dialog slot, a queued ask of a
// multi-ask round, a parked sub-agent ask, and a queued question prompt — and
// that it clears when the last one goes away.
func TestSyncPendingAskFlagTracksEveryLiveAsk(t *testing.T) {
	cases := []struct {
		name string
		set  func(m *model)
		want bool
	}{
		{
			name: "no ask at all",
			set:  func(m *model) {},
			want: false,
		},
		{
			name: "permission dialog slot open",
			set:  func(m *model) { m.showPermDialog = true },
			want: true,
		},
		{
			name: "queued permission ask (not yet shown)",
			set: func(m *model) {
				m.permAskQueue = append(m.permAskQueue, queuedPermAsk{toolCallID: "c1"})
			},
			want: true,
		},
		{
			name: "parked sub-agent ask",
			set: func(m *model) {
				m.pendingSubAgentResp = make(chan agent.PermissionResponse, 1)
			},
			want: true,
		},
		{
			name: "question dialog open",
			set:  func(m *model) { m.showQuestionDialog = true },
			want: true,
		},
		{
			name: "queued question prompt (not yet shown)",
			set: func(m *model) {
				m.questionAskQueue = append(m.questionAskQueue, queuedQuestionAsk{toolCallID: "q1"})
			},
			want: true,
		},
		{
			name: "RC permission ask awaiting a remote decision",
			set: func(m *model) {
				m.rcPendingPerm = &rcPendingPerm{requestID: "r1"}
			},
			want: true,
		},
		{
			name: "RC question awaiting a remote answer",
			set: func(m *model) {
				m.rcPendingQuestion = &rcPendingQuestion{requestID: "r2"}
			},
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel()
			m.installAgent(newTestAgent(retryTestClient{}, nil, nil, nil))
			tc.set(&m)
			m.syncPendingAskFlag()
			if got := m.agent.PendingAsk(); got != tc.want {
				t.Fatalf("agent.PendingAsk() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestShowPermAskDeclaresPendingAsk pins that OPENING a permission dialog
// declares it to the agent. The declaration must happen at the moment the
// dialog appears — not only before a Step — because the advisor can fire from
// a checkpoint in the MIDDLE of a turn, with no Step boundary in between.
func TestShowPermAskDeclaresPendingAsk(t *testing.T) {
	m := newTestModel()
	m.installAgent(newTestAgent(retryTestClient{}, nil, nil, nil))
	if m.agent.PendingAsk() {
		t.Fatal("a fresh model must not report a pending ask")
	}

	m.showPermAsk(queuedPermAsk{req: agent.PermissionRequest{ToolName: "bash"}, toolCallID: "c1"})
	if !m.agent.PendingAsk() {
		t.Fatal("opening a permission dialog must declare the pending ask to the agent")
	}

	// Answering it clears the declaration — a stale true would mute the
	// advisor for the rest of the session.
	m.clearPendingPermAskSlot()
	if m.agent.PendingAsk() {
		t.Fatal("clearing the permission slot must clear the declared pending ask")
	}
}

// TestShowPermAskDeclaresPendingAskWithQueuedRemainder pins the multi-ask
// case: a round can raise SEVERAL asks (parallel dispatch runs every call
// before the pause check). Answering the one in the slot must NOT clear the
// declaration while another ask is still queued — that ask is still a dialog
// the user owes an answer.
func TestShowPermAskDeclaresPendingAskWithQueuedRemainder(t *testing.T) {
	m := newTestModel()
	m.installAgent(newTestAgent(retryTestClient{}, nil, nil, nil))

	// The slot is busy, so the second ask is queued behind it.
	m.showPermAsk(queuedPermAsk{req: agent.PermissionRequest{ToolName: "bash"}, toolCallID: "c1"})
	m.permAskQueue = append(m.permAskQueue, queuedPermAsk{req: agent.PermissionRequest{ToolName: "write"}, toolCallID: "c2"})
	m.syncPendingAskFlag()
	if !m.agent.PendingAsk() {
		t.Fatal("a queued ask must keep the pending-ask declaration set")
	}

	// The user answers the one in the slot; the queued one is still waiting.
	m.clearPendingPermAskSlot()
	if !m.agent.PendingAsk() {
		t.Fatal("the queued ask must keep the pending-ask declaration set after the slot is cleared")
	}
}

// TestEnqueuePermAskDeclaresPendingAsk pins that a queued ask (the slot was
// busy) is declared. Without this the agent would see "no pending ask" and
// start the advisor underneath a dialog the user has not even been shown yet.
func TestEnqueuePermAskDeclaresPendingAsk(t *testing.T) {
	m := newTestModel()
	m.installAgent(newTestAgent(retryTestClient{}, nil, nil, nil))

	// Occupy the slot so the next ask is queued rather than shown.
	m.showPermAsk(queuedPermAsk{req: agent.PermissionRequest{ToolName: "bash"}, toolCallID: "c1"})
	m.permAskQueue = nil
	m.syncPendingAskFlag()

	m.enqueuePermAsk(queuedPermAsk{req: agent.PermissionRequest{ToolName: "write"}, toolCallID: "c2"})
	if !m.agent.PendingAsk() {
		t.Fatal("a queued permission ask must declare the pending ask")
	}
}

// TestEnqueueQuestionAskDeclaresPendingAsk pins the question-prompt twin of
// the permission path.
func TestEnqueueQuestionAskDeclaresPendingAsk(t *testing.T) {
	m := newTestModel()
	m.installAgent(newTestAgent(retryTestClient{}, nil, nil, nil))

	m.startQuestionPrompt("q1", []tool.QuestionPrompt{{Header: "h", Question: "which one?"}})
	if !m.agent.PendingAsk() {
		t.Fatal("opening a question dialog must declare the pending ask")
	}

	m.clearQuestionPrompt()
	if m.agent.PendingAsk() {
		t.Fatal("clearing the question dialog must clear the declared pending ask")
	}

	// A queued prompt behind a busy slot is still an ask.
	m.showQuestionDialog = true
	m.questionAskQueue = append(m.questionAskQueue, queuedQuestionAsk{toolCallID: "q2"})
	m.syncPendingAskFlag()
	if !m.agent.PendingAsk() {
		t.Fatal("a queued question prompt must declare the pending ask")
	}
}

// TestSyncPendingAskFlagIsSafeWithoutAnAgent pins that the helper tolerates a
// model with no agent (some TUI states have none yet). It must not panic.
func TestSyncPendingAskFlagIsSafeWithoutAnAgent(t *testing.T) {
	m := newTestModel()
	m.agent = nil
	m.showPermDialog = true
	m.syncPendingAskFlag() // must not panic
}

// TestQueueDrainBlockedCoversEveryOutstandingAsk pins the widening of
// queueDrainBlocked from the dialog slots alone to every outstanding ask.
//
// queueDrainBlocked gates the RESUME paths — the auto-continue judge, the goal
// judge, the advisor's plan/done checkpoint, the queued-input drain, the
// background-job resume. Before this widening it read only the two dialog
// slots, so a QUEUED ask (a round can raise several) or a parked sub-agent ask
// did NOT block a resume: the second model would start underneath a dialog the
// user was still looking at.
//
// This is the same failure class as the advisor gate, on the TUI side.
func TestQueueDrainBlockedCoversEveryOutstandingAsk(t *testing.T) {
	cases := []struct {
		name string
		set  func(m *model)
		want bool
	}{
		{"nothing outstanding", func(m *model) {}, false},
		{"permission dialog", func(m *model) { m.showPermDialog = true }, true},
		{"question dialog", func(m *model) { m.showQuestionDialog = true }, true},
		{
			name: "queued permission ask",
			set: func(m *model) {
				m.permAskQueue = append(m.permAskQueue, queuedPermAsk{toolCallID: "c1"})
			},
			want: true,
		},
		{
			name: "queued question prompt",
			set: func(m *model) {
				m.questionAskQueue = append(m.questionAskQueue, queuedQuestionAsk{toolCallID: "q1"})
			},
			want: true,
		},
		{
			name: "parked sub-agent ask",
			set: func(m *model) {
				m.pendingSubAgentResp = make(chan agent.PermissionResponse, 1)
			},
			want: true,
		},
		{
			name: "RC permission ask",
			set: func(m *model) {
				m.rcPendingPerm = &rcPendingPerm{requestID: "r1"}
			},
			want: true,
		},
		{
			name: "RC question",
			set: func(m *model) {
				m.rcPendingQuestion = &rcPendingQuestion{requestID: "r2"}
			},
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := model{}
			tc.set(&m)
			if got := m.queueDrainBlocked(); got != tc.want {
				t.Fatalf("queueDrainBlocked() = %v, want %v", got, tc.want)
			}
		})
	}
}
