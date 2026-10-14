package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/server"
	"github.com/u007/ocode/internal/tui/fastviewport"
)

// TestAutoContinueChainCap simulates streamDoneMsg's guard-then-fire sequence
// across repeated step-limit cutoffs, and asserts the chain stops at
// autoContinueMaxChain rather than looping forever.
func TestAutoContinueChainCap(t *testing.T) {
	count := 0
	lastWasAuto := false
	fires := 0

	// Returns whether this turn fired an auto-continue (i.e. whether a next
	// turn happens at all — without a fire, nothing calls askAgent() again,
	// so streamDoneMsg never re-enters this logic on its own).
	simulateTurn := func(stepLimitHit bool) bool {
		// Mirrors the streamDoneMsg handler: reset unless the PRIOR turn was
		// itself an auto-continue fire.
		if !lastWasAuto {
			count = 0
		}
		lastWasAuto = false
		if shouldAutoContinue(true /* enabled */, stepLimitHit, count) {
			count++
			lastWasAuto = true
			fires++
		}
		return lastWasAuto
	}

	// A runaway model keeps hitting the step limit turn after turn. Each
	// fired auto-continue triggers exactly one more turn; once the cap
	// declines to fire, nothing drives a further turn on its own — so drive
	// the loop only as long as it keeps firing, and confirm it stops instead
	// of running unbounded.
	for i := 0; i < 20 && simulateTurn(true); i++ {
	}
	if fires != autoContinueMaxChain {
		t.Errorf("consecutive step-limit turns: got %d fires, want %d (the cap)", fires, autoContinueMaxChain)
	}
	if count != autoContinueMaxChain {
		t.Errorf("count = %d, want %d after hitting the cap", count, autoContinueMaxChain)
	}

	// A turn that does NOT hit the step limit (human sent a normal message,
	// or the model just finished) must reset the chain.
	simulateTurn(false)
	if count != 0 {
		t.Errorf("count = %d after a non-step-limit turn, want 0 (reset)", count)
	}

	// And the chain can fire again from zero afterwards.
	fires = 0
	for i := 0; i < 2; i++ {
		simulateTurn(true)
	}
	if fires != 2 {
		t.Errorf("got %d fires after reset, want 2", fires)
	}
}

// The model-set confirmation must say the enable gate is still off — setting a
// judge model does not arm auto-continue. Before this, `/autocontinue model
// typesafe/jev-latest` printed "Auto-continue judge model updated … Persisted
// to config", which reads as "it is on" and matches the report "auto continue
// llm via jev does not work" (the gate was left disabled).
func TestAutoContinueModelSetNoteSurfacesDisabledGate(t *testing.T) {
	off := autoContinueModelSetNote(false)
	if !strings.Contains(off, "DISABLED") {
		t.Errorf("note when disabled should warn the gate is off: %q", off)
	}
	on := autoContinueModelSetNote(true)
	if !strings.Contains(on, "Persisted") {
		t.Errorf("note when enabled should confirm persistence: %q", on)
	}
	if strings.Contains(on, "DISABLED") {
		t.Errorf("note when enabled must not claim it is disabled: %q", on)
	}
}

func TestShouldAutoContinueGuards(t *testing.T) {
	cases := []struct {
		name         string
		enabled      bool
		stepLimitHit bool
		count        int
		want         bool
	}{
		{"disabled", false, true, 0, false},
		{"no step-limit signal", true, false, 0, false},
		{"at cap", true, true, autoContinueMaxChain, false},
		{"eligible", true, true, 0, true},
	}
	for _, c := range cases {
		got := shouldAutoContinue(c.enabled, c.stepLimitHit, c.count)
		if got != c.want {
			t.Errorf("%s: shouldAutoContinue(%v,%v,%d) = %v, want %v",
				c.name, c.enabled, c.stepLimitHit, c.count, got, c.want)
		}
	}
}

func TestResolveMaskModelArg(t *testing.T) {
	m := &model{}
	m.config = &config.Config{
		Ocode: config.OcodeConfig{
			LocalModels: map[string]config.LocalModelConfig{
				"local/bonsai-8b-mlx-1bit": {Enabled: true, MaxParallel: 1},
			},
		},
	}

	cases := []struct {
		name string
		want string
	}{
		{"bonsai-8b-mlx-1bit", "local/bonsai-8b-mlx-1bit"},       // bare registered local id → prefixed
		{"local/bonsai-8b-mlx-1bit", "local/bonsai-8b-mlx-1bit"}, // already prefixed → unchanged
		{"lmstudio/some-model", "lmstudio/some-model"},           // has "/" already → unchanged
		{"unregistered-name", "unregistered-name"},               // not a registered local id → unchanged
	}
	for _, c := range cases {
		got := m.resolveMaskModelArg(c.name)
		if got != c.want {
			t.Errorf("resolveMaskModelArg(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestAgentAutoContinueDeclineDetail covers the streamDone decline-notice
// helper: a step-limited turn that will not auto-continue must produce a
// user-facing one-liner (off toggle or exhausted cap), and a natural
// completion must produce nothing.
func TestAgentAutoContinueDeclineDetail(t *testing.T) {
	m := &model{
		autoContinueEnabled: true,
		autoContinueCount:   0,
		agent:               &agent.Agent{},
	}
	if got := m.agentAutoContinueDeclineDetail(nil, false); got != "" {
		t.Errorf("natural completion detail = %q, want empty", got)
	}
	if got := m.agentAutoContinueDeclineDetail(nil, true); got != "" {
		// Enabled and under the cap: the chain fires, so the decline helper
		// must stay silent (the fire path renders its own hint).
		t.Errorf("enabled+under-cap step-limit detail = %q, want empty (chain fires)", got)
	}
	m.autoContinueEnabled = false
	got := m.agentAutoContinueDeclineDetail(nil, true)
	if !strings.Contains(got, "auto-continue is off") {
		t.Errorf("disabled detail should say auto-continue is off: %q", got)
	}
	m.autoContinueEnabled = true
	m.autoContinueCount = autoContinueMaxChain
	got = m.agentAutoContinueDeclineDetail(nil, true)
	if !strings.Contains(got, "cap") {
		t.Errorf("capped-out detail should name the chain cap: %q", got)
	}
	// Errors are surfaced by the error path, never by this helper.
	if got := m.agentAutoContinueDeclineDetail(errTestStream{}, true); got != "" {
		t.Errorf("errored turn detail = %q, want empty", got)
	}
}

type errTestStream struct{}

func (errTestStream) Error() string { return "boom" }

func TestStreamDoneDispatchesGoalJudgeNotAutoContinue(t *testing.T) {
	m := model{
		ready:               true,
		width:               80,
		height:              24,
		streaming:           true,
		autoContinueEnabled: true,
		agent:               newTestAgent(retryTestClient{}, nil, nil, nil),
		input:               newTestTextarea(),
		viewport:            fastviewport.New(76, 20),
		styles:              ApplyThemeColors("tokyonight"),
	}
	m.layout()
	if err := m.agent.SetGoal("all tests pass"); err != nil {
		t.Fatalf("SetGoal: %v", err)
	}
	goalGen, autoGen := m.goalGen, m.autoContinueGen

	updated, _ := m.Update(streamDoneMsg{})
	got := derefTestModel(t, updated)
	if got.goalGen != goalGen+1 {
		t.Fatalf("expected goal judge dispatch to bump goalGen %d -> %d, got %d", goalGen, goalGen+1, got.goalGen)
	}
	if got.autoContinueGen != autoGen {
		t.Fatalf("expected auto-continue judge not to dispatch while a goal is evaluable, autoContinueGen %d -> %d", autoGen, got.autoContinueGen)
	}
}

func TestStreamDoneGoalSkipsWhenQueueDrainBlocked(t *testing.T) {
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
	if err := m.agent.SetGoal("all tests pass"); err != nil {
		t.Fatalf("SetGoal: %v", err)
	}
	goalGen := m.goalGen

	updated, _ := m.Update(streamDoneMsg{})
	got := derefTestModel(t, updated)
	if got.goalGen != goalGen {
		t.Fatalf("expected no goal judge dispatch while a question dialog is active, goalGen %d -> %d", goalGen, got.goalGen)
	}
}

func TestGoalJudgeFinishedResumesOnNotMet(t *testing.T) {
	m := newGoalCommandTestModel()
	if err := m.agent.SetGoal("all tests pass"); err != nil {
		t.Fatalf("SetGoal: %v", err)
	}
	m.goalGen = 3
	result := agent.GoalJudgeResult{Gen: 3, Verdict: "not_met", Status: agent.GoalActive, Detail: "judge: not_met", Resume: true}

	updated, cmd := m.Update(goalJudgeFinishedMsg{gen: 3, result: result})
	got := derefTestModel(t, updated)
	last := got.messages[len(got.messages)-1]
	if last.role != roleUser || !strings.HasPrefix(last.text, "[ocode:goal]") {
		t.Fatalf("expected resume to append a user-role [ocode:goal] message, got %#v", last)
	}
	if last.raw == nil || last.raw.Content != last.text {
		t.Fatalf("expected resume message to carry raw agent message, got %#v", last.raw)
	}
	if cmd == nil {
		t.Fatal("expected resume to return a turn-start command")
	}
	if !got.lastTurnWasGoalResume {
		t.Fatal("expected resume to mark the turn as goal-initiated")
	}

	before := len(got.messages)
	updated, _ = got.Update(goalJudgeFinishedMsg{gen: 2, result: result})
	got = derefTestModel(t, updated)
	if len(got.messages) != before {
		t.Fatalf("expected stale-gen verdict to be ignored, transcript grew %d -> %d", before, len(got.messages))
	}
}

func TestGoalJudgeStaleVerdictRendersNothing(t *testing.T) {
	m := newGoalCommandTestModel()
	if err := m.agent.SetGoal("all tests pass"); err != nil {
		t.Fatalf("SetGoal: %v", err)
	}
	m.goalGen = 4
	before := len(m.messages)

	updated, cmd := m.Update(goalJudgeFinishedMsg{gen: 4, result: agent.GoalJudgeResult{Gen: 4, Verdict: "stale", Resume: true}})
	got := derefTestModel(t, updated)
	if len(got.messages) != before {
		t.Fatalf("expected stale verdict to render nothing, transcript grew %d -> %d", before, len(got.messages))
	}
	if got.streaming {
		t.Fatal("expected stale verdict not to start a turn")
	}
	if cmd == nil {
		t.Fatal("expected the judge listener to be re-armed after a stale verdict")
	}
}

func TestGoalStateSurvivesSessionSaveRestore(t *testing.T) {
	m := newGoalCommandTestModel()
	if err := m.agent.SetGoal("all tests pass"); err != nil {
		t.Fatalf("SetGoal: %v", err)
	}

	raw, err := json.Marshal(m.sessionSidebarMetadata())
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if _, ok := meta[agent.GoalMetadataKey]; !ok {
		t.Fatalf("expected sidebar metadata to hold the goal, got %s", raw)
	}

	fresh := newGoalCommandTestModel()
	fresh.restoreGoalState(meta)
	gs, ok := fresh.agent.Goal()
	if !ok || gs.Condition != "all tests pass" || gs.Status != agent.GoalActive {
		t.Fatalf("expected restored goal, got ok=%v state=%+v", ok, gs)
	}
}

func TestStreamDoneReactivatesPausedGoalOnHumanTurn(t *testing.T) {
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

	updated, _ := m.Update(streamDoneMsg{})
	got := derefTestModel(t, updated)
	if gs, _ := got.agent.Goal(); gs.Status != agent.GoalActive {
		t.Fatalf("expected a completed human turn to reactivate the paused goal, got status %s", gs.Status)
	}
}

func TestStreamDoneGoalResumeTurnKeepsPausedGoal(t *testing.T) {
	m := model{
		ready:                 true,
		width:                 80,
		height:                24,
		streaming:             true,
		showQuestionDialog:    true,
		lastTurnWasGoalResume: true,
		agent:                 newTestAgent(retryTestClient{}, nil, nil, nil),
		input:                 newTestTextarea(),
		viewport:              fastviewport.New(76, 20),
		styles:                ApplyThemeColors("tokyonight"),
	}
	m.layout()
	m.agent.RestoreGoal(agent.GoalState{Condition: "all tests pass", Status: agent.GoalPaused, Turns: 2})

	updated, _ := m.Update(streamDoneMsg{})
	got := derefTestModel(t, updated)
	if gs, _ := got.agent.Goal(); gs.Status != agent.GoalPaused {
		t.Fatalf("expected the agent's own goal resume turn to leave the paused goal alone, got status %s", gs.Status)
	}
	if got.lastTurnWasGoalResume {
		t.Fatal("expected the goal resume marker to be consumed at turn end")
	}
}

// An /rc-originated turn ends with a goal evaluable: the goal judge is dispatched
// (goalGen bumps, the auto-continue judge stays off) and the /rc listener is
// left un-armed until the verdict lands.
func TestStreamDoneRCDispatchesGoalJudge(t *testing.T) {
	m := model{
		ready:               true,
		width:               80,
		height:              24,
		streaming:           true,
		autoContinueEnabled: true,
		pendingRC:           &server.RCRequest{},
		agent:               newTestAgent(retryTestClient{}, nil, nil, nil),
		input:               newTestTextarea(),
		viewport:            fastviewport.New(76, 20),
		styles:              ApplyThemeColors("tokyonight"),
	}
	m.layout()
	if err := m.agent.SetGoal("all tests pass"); err != nil {
		t.Fatalf("SetGoal: %v", err)
	}
	goalGen, autoGen := m.goalGen, m.autoContinueGen

	updated, cmd := m.Update(streamDoneMsg{})
	got := derefTestModel(t, updated)
	if got.goalGen != goalGen+1 {
		t.Fatalf("expected RC turn to dispatch the goal judge (goalGen %d -> %d), got %d", goalGen, goalGen+1, got.goalGen)
	}
	if got.autoContinueGen != autoGen {
		t.Fatalf("expected auto-continue judge not to dispatch alongside the goal, autoContinueGen %d -> %d", autoGen, got.autoContinueGen)
	}
	if !got.goalJudgeForRC {
		t.Fatal("expected the goal judge to be marked as /rc-originated")
	}
	if got.streaming {
		t.Fatal("expected the completed /rc turn to settle streaming before the verdict lands")
	}
	if cmd != nil {
		t.Fatal("expected the /rc listener to stay un-armed while the goal judge is in flight")
	}
}

// A goal resume that is itself an /rc chain iteration (pendingRCAutoContinue set,
// no pending request) also dispatches the goal judge.
func TestStreamDoneRCChainContinuationDispatchesGoalJudge(t *testing.T) {
	m := model{
		ready:                 true,
		width:                 80,
		height:                24,
		streaming:             true,
		pendingRCAutoContinue: true,
		agent:                 newTestAgent(retryTestClient{}, nil, nil, nil),
		input:                 newTestTextarea(),
		viewport:              fastviewport.New(76, 20),
		styles:                ApplyThemeColors("tokyonight"),
	}
	m.layout()
	if err := m.agent.SetGoal("all tests pass"); err != nil {
		t.Fatalf("SetGoal: %v", err)
	}
	goalGen := m.goalGen

	updated, _ := m.Update(streamDoneMsg{})
	got := derefTestModel(t, updated)
	if got.goalGen != goalGen+1 {
		t.Fatalf("expected RC chain iteration to dispatch the goal judge (goalGen %d -> %d), got %d", goalGen, goalGen+1, got.goalGen)
	}
	if !got.goalJudgeForRC || !got.pendingRCAutoContinue {
		t.Fatalf("expected chain iteration to keep the /rc listener un-armed (goalJudgeForRC=%v pendingRCAutoContinue=%v)", got.goalJudgeForRC, got.pendingRCAutoContinue)
	}
}

// A goal verdict for an /rc turn re-arms the /rc listener when it does not
// resume, and keeps it un-armed when it does resume into the chain.
func TestGoalJudgeFinishedRearmsRCListener(t *testing.T) {
	newRCGoalModel := func(t *testing.T) *model {
		t.Helper()
		m := newGoalCommandTestModel()
		m.rcCh = make(chan server.RCRequest)
		if err := m.agent.SetGoal("all tests pass"); err != nil {
			t.Fatalf("SetGoal: %v", err)
		}
		m.goalGen = 5
		m.goalJudgeForRC = true
		m.pendingRCAutoContinue = true
		return m
	}

	t.Run("paused verdict re-arms", func(t *testing.T) {
		m := newRCGoalModel(t)
		updated, cmd := m.Update(goalJudgeFinishedMsg{gen: 5, result: agent.GoalJudgeResult{Gen: 5, Verdict: "impossible", Status: agent.GoalImpossible, Detail: "judge"}})
		got := derefTestModel(t, updated)
		if got.pendingRCAutoContinue || got.goalJudgeForRC {
			t.Fatalf("expected verdict to clear the RC chain flags, got pendingRCAutoContinue=%v goalJudgeForRC=%v", got.pendingRCAutoContinue, got.goalJudgeForRC)
		}
		if cmd == nil {
			t.Fatal("expected the /rc listener and judge listener to be re-armed")
		}
		if got.streaming {
			t.Fatal("expected no turn to start on a non-resuming verdict")
		}
	})

	t.Run("resume keeps the listener un-armed", func(t *testing.T) {
		m := newRCGoalModel(t)
		updated, cmd := m.Update(goalJudgeFinishedMsg{gen: 5, result: agent.GoalJudgeResult{Gen: 5, Verdict: "not_met", Status: agent.GoalActive, Detail: "judge", Resume: true}})
		got := derefTestModel(t, updated)
		if !got.pendingRCAutoContinue {
			t.Fatal("expected a resumed /rc goal turn to keep the chain's listener un-armed")
		}
		if last := got.messages[len(got.messages)-1]; !strings.HasPrefix(last.text, "[ocode:goal]") {
			t.Fatalf("expected the resume marker as the last message, got %q", last.text)
		}
		if cmd == nil {
			t.Fatal("expected a turn-start command")
		}
	})

	t.Run("stale verdict still re-arms", func(t *testing.T) {
		m := newRCGoalModel(t)
		updated, cmd := m.Update(goalJudgeFinishedMsg{gen: 4, result: agent.GoalJudgeResult{Gen: 4, Verdict: "not_met", Resume: true}})
		got := derefTestModel(t, updated)
		if got.pendingRCAutoContinue || got.goalJudgeForRC {
			t.Fatal("expected a superseded /rc verdict to release the listener")
		}
		if cmd == nil {
			t.Fatal("expected the /rc listener to be re-armed after a superseded verdict")
		}
	})
}

// A Resume verdict that arrives while a compaction is pending must not start a
// turn. It is deferred and fires once compactFinishedMsg lands.
func TestGoalResumeDeferredUntilCompactionFinishes(t *testing.T) {
	m := newGoalCommandTestModel()
	if err := m.agent.SetGoal("all tests pass"); err != nil {
		t.Fatalf("SetGoal: %v", err)
	}
	m.goalGen = 3
	m.pendingCompactUIIdx = []int{0}

	updated, _ := m.Update(goalJudgeFinishedMsg{gen: 3, result: agent.GoalJudgeResult{Gen: 3, Verdict: "not_met", Status: agent.GoalActive, Detail: "judge", Resume: true}})
	got := derefTestModel(t, updated)
	if last := got.messages[len(got.messages)-1]; strings.HasPrefix(last.text, "[ocode:goal]") {
		t.Fatal("expected no goal resume while a compaction is pending")
	}
	if !got.goalResumePending {
		t.Fatal("expected the resume to be deferred")
	}

	updated, _ = got.Update(compactFinishedMsg{result: agent.CompactResult{}})
	got = derefTestModel(t, updated)
	if last := got.messages[len(got.messages)-1]; last.role != roleUser || !strings.HasPrefix(last.text, "[ocode:goal]") {
		t.Fatalf("expected the deferred resume to append the goal marker after compaction, got %#v", last)
	}
	if got.goalResumePending {
		t.Fatal("expected the deferred resume flag to clear once it fires")
	}
}

// /new ends the session the deferred resume belonged to, so compaction finishing
// afterwards must not resume the goal.
func TestGoalResumeDeferredClearedByNewSession(t *testing.T) {
	m := newGoalCommandTestModel()
	if err := m.agent.SetGoal("all tests pass"); err != nil {
		t.Fatalf("SetGoal: %v", err)
	}
	m.goalGen = 3
	m.pendingCompactUIIdx = []int{0}

	updated, _ := m.Update(goalJudgeFinishedMsg{gen: 3, result: agent.GoalJudgeResult{Gen: 3, Verdict: "not_met", Status: agent.GoalActive, Detail: "judge", Resume: true}})
	got := derefTestModel(t, updated)
	if !got.goalResumePending {
		t.Fatal("expected the resume to be deferred before /new")
	}

	updated, _ = got.handleCommand("/new")
	got = *updated.(*model)
	if got.goalResumePending {
		t.Fatal("expected /new to clear the deferred resume")
	}
	got.pendingCompactUIIdx = nil

	updated, _ = got.Update(compactFinishedMsg{result: agent.CompactResult{}})
	got = derefTestModel(t, updated)
	for _, msg := range got.messages {
		if strings.HasPrefix(msg.text, "[ocode:goal]") {
			t.Fatalf("expected no goal resume after /new, got %#v", msg)
		}
	}
}
