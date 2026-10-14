package tui

import (
	"errors"
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/tui/fastviewport"
)

// newPausedGoalSettleModel returns a model whose turn has ended, with a paused
// goal. showQuestionDialog keeps the post-turn goal judge from running, so only
// the settle path can change the goal's status.
func newPausedGoalSettleModel(t *testing.T) model {
	t.Helper()
	m := model{
		ready:              true,
		width:              80,
		height:             24,
		streaming:          true,
		showQuestionDialog: true,
		agent:              newTestAgent(retryTestClient{}, nil, nil, nil),
		input:              newTestTextarea(),
		viewport:           fastviewport.New(76, 20),
		styles:             ApplyThemeColors("tokyonight"),
	}
	m.layout()
	m.agent.RestoreGoal(agent.GoalState{Condition: "all tests pass", Status: agent.GoalPaused, Turns: 2})
	return m
}

// A step-limit auto-continue turn is not a human turn, so when it ends the goal
// paused on awaiting_user must stay paused.
func TestStreamDoneAutoContinueTurnKeepsPausedGoal(t *testing.T) {
	m := newPausedGoalSettleModel(t)
	m.lastTurnWasAutoContinue = true

	updated, _ := m.Update(streamDoneMsg{})
	got := derefTestModel(t, updated)
	if gs, _ := got.agent.Goal(); gs.Status != agent.GoalPaused {
		t.Fatalf("auto-continue turn reactivated the paused goal, got status %s", gs.Status)
	}
}

// An errored turn is not a human turn either, so it must not reactivate a
// paused goal.
func TestStreamDoneErroredTurnKeepsPausedGoal(t *testing.T) {
	m := newPausedGoalSettleModel(t)

	updated, _ := m.Update(streamDoneMsg{err: errors.New("provider timeout")})
	got := derefTestModel(t, updated)
	if gs, _ := got.agent.Goal(); gs.Status != agent.GoalPaused {
		t.Fatalf("errored turn reactivated the paused goal, got status %s", gs.Status)
	}
}

// A goal cleared in a session that was never loaded from disk must still be
// removed by the next save. A nil metadata map would keep the stored goal.
func TestSessionSidebarMetadataDropsClearedGoal(t *testing.T) {
	m := newGoalCommandTestModel()
	if err := m.agent.SetGoal("all tests pass"); err != nil {
		t.Fatalf("SetGoal: %v", err)
	}
	if _, ok := m.sessionSidebarMetadata()[agent.GoalMetadataKey]; !ok {
		t.Fatal("goal missing from metadata before the clear")
	}

	m.agent.ClearGoal()
	meta := m.sessionSidebarMetadata()
	if meta == nil {
		t.Fatal("cleared goal saved as nil metadata, which keeps the stored goal")
	}
	if _, ok := meta[agent.GoalMetadataKey]; ok {
		t.Fatalf("cleared goal still in save metadata: %v", meta)
	}
}
