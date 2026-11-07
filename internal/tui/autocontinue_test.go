package tui

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/server"
	"github.com/u007/ocode/internal/tool"
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

// A human message that starts a turn reactivates a paused goal.
func TestHumanSendReactivatesPausedGoal(t *testing.T) {
	m := newGoalCommandTestModel()
	m.restoreGoalTurns(t, "all tests pass", agent.GoalPaused, 2)

	updated, _ := m.Update(fileSearchFinishedMsg{processedText: "keep going"})
	got := derefTestModel(t, updated)
	if gs, _ := got.agent.Goal(); gs.Status != agent.GoalActive {
		t.Fatalf("expected a human-sent turn to reactivate the paused goal, got status %s", gs.Status)
	}
}

// A goal resume turn is the agent's own, so it leaves a paused goal paused.
func TestStreamDoneGoalResumeTurnKeepsPausedGoal(t *testing.T) {
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
	if gs, _ := got.agent.Goal(); gs.Status != agent.GoalPaused {
		t.Fatalf("expected the agent's own goal resume turn to leave the paused goal alone, got status %s", gs.Status)
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

// A goal cleared after load must not come back on save. Telemetry is absent, so
// the saved map is built from the loaded base alone, and it must still be non-nil
// (a nil save keeps the stored goal) while carrying the goal key neither way.
func TestClearedGoalDoesNotResurrectOnSave(t *testing.T) {
	stored := agent.GoalStateToMetadata(agent.GoalState{Condition: "all tests pass", Status: agent.GoalActive})
	stored["source"] = "tui"

	m := newGoalCommandTestModel()
	m.restoreSessionMetadata(stored)
	if _, ok := m.agent.Goal(); !ok {
		t.Fatal("expected the loaded goal to be restored before it is cleared")
	}
	m.agent.ClearGoal()

	meta := m.sessionSidebarMetadata()
	if meta == nil {
		t.Fatal("expected a non-nil save map: a nil map keeps the stored goal")
	}
	if _, ok := meta[agent.GoalMetadataKey]; ok {
		t.Fatalf("expected the cleared goal to be absent from the save map, got %#v", meta)
	}
	if meta["source"] != "tui" {
		t.Fatalf("expected unowned loaded metadata to carry forward, got %#v", meta)
	}
}

// A judge verdict carries its own action and fixed prompt; the TUI must send
// that prompt (not the generic continue) and record the action in the chain.
func TestAutoContinueJudgeFinishedFiresActionPrompt(t *testing.T) {
	m := newGoalCommandTestModel()
	m.autoContinueEnabled = true
	m.autoContinueGen = 5
	const prompt = "The build or tests are failing. Fix the failures before doing anything else, then re-run them and show the output."

	updated, cmd := m.Update(autoContinueJudgeFinishedMsg{gen: 5, resume: true, action: "fix_errors", prompt: prompt, detail: "jev: fix_errors — resuming"})
	got := derefTestModel(t, updated)
	last := got.messages[len(got.messages)-1]
	if last.role != roleUser || last.text != prompt {
		t.Fatalf("expected the judge's fixed prompt as the resume message, got %#v", last)
	}
	if last.raw == nil || last.raw.Content != prompt {
		t.Fatalf("resume message must carry the raw agent message, got %#v", last.raw)
	}
	hint := got.messages[len(got.messages)-2]
	if !strings.Contains(hint.text, "fix_errors") {
		t.Fatalf("transcript hint should name the action, got %q", hint.text)
	}
	if cmd == nil {
		t.Fatal("expected a turn-start command")
	}
	if len(got.autoContinueActions) != 1 || got.autoContinueActions[0] != "fix_errors" || got.autoContinueCount != 1 {
		t.Fatalf("chain state = %v/%d, want [fix_errors]/1", got.autoContinueActions, got.autoContinueCount)
	}

	// A non-auto turn settling the chain clears the action history too.
	got.lastTurnWasAutoContinue = false
	got.settleAutoContinueChain()
	if got.autoContinueActions != nil || got.autoContinueCount != 0 {
		t.Fatalf("settle must reset the chain, got %v/%d", got.autoContinueActions, got.autoContinueCount)
	}
}

// A goal lives on the agent. Replacing the agent (model switch, rebuild) must
// keep the goal, or the next save deletes it from disk.
func TestGoalSurvivesAgentReplacement(t *testing.T) {
	m := newGoalCommandTestModel()
	m.restoreGoalTurns(t, "all tests pass", agent.GoalPaused, 4)

	m.installAgent(newTestAgent(retryTestClient{}, nil, nil, nil))

	gs, ok := m.agent.Goal()
	if !ok || gs.Condition != "all tests pass" || gs.Status != agent.GoalPaused || gs.Turns != 4 {
		t.Fatalf("goal after agent replacement: %+v ok=%v (want the carried paused goal with 4 turns)", gs, ok)
	}
	if _, ok := m.sessionSidebarMetadata()[agent.GoalMetadataKey]; !ok {
		t.Fatal("goal dropped from sidebar metadata after agent replacement")
	}
}

// A verdict from the outgoing agent's in-flight judge is stale after a replacement:
// it must not resume the session on the new agent.
func TestGoalJudgeFinishedIgnoredAfterAgentReplacement(t *testing.T) {
	m := newGoalCommandTestModel()
	m.restoreGoalTurns(t, "all tests pass", agent.GoalActive, 1)
	m.goalGen = 3
	m.installAgent(newTestAgent(retryTestClient{}, nil, nil, nil))
	before := len(m.messages)

	updated, _ := m.Update(goalJudgeFinishedMsg{gen: 3, result: agent.GoalJudgeResult{Gen: 3, Verdict: "not_met", Status: agent.GoalActive, Detail: "judge", Resume: true}})
	got := derefTestModel(t, updated)
	if len(got.messages) != before || got.streaming {
		t.Fatalf("verdict from the replaced agent must render nothing and start no turn: transcript %d -> %d, streaming=%v", before, len(got.messages), got.streaming)
	}
}

// A turn that ends in error has no judge behind it, so a resume deferred during
// it must not fire after the error.
func TestGoalResumePendingClearedOnTurnError(t *testing.T) {
	m := newGoalCommandTestModel()
	m.streaming = true
	m.goalResumePending = true

	updated, _ := m.Update(streamDoneMsg{err: errors.New("provider stream failed")})
	got := derefTestModel(t, updated)
	if got.goalResumePending {
		t.Fatal("goalResumePending survived an errored turn; a later compaction would resume a stopped goal")
	}
}

// restoreGoalTurns reinstates a goal with a known status and turn count on the
// test model's agent.
func (m *model) restoreGoalTurns(t *testing.T, condition string, status agent.GoalStatus, turns int) {
	t.Helper()
	m.agent.RestoreGoal(agent.GoalState{Condition: condition, Status: status, Turns: turns, SetAt: time.Now()})
	if _, ok := m.agent.Goal(); !ok {
		t.Fatal("RestoreGoal did not install the goal")
	}
}

// questionJudgeTestModel opens a one-prompt question dialog (call_1) on a test
// model with auto-continue armed: the state the question judge runs from.
func questionJudgeTestModel(t *testing.T, prompts []tool.QuestionPrompt) model {
	t.Helper()
	m := newGoalCommandTestModel()
	m.questionInput = newTestTextarea()
	m.autoContinueEnabled = true
	m.startQuestionPrompt("call_1", prompts)
	if !m.showQuestionDialog {
		t.Fatal("expected question dialog to open")
	}
	return *m
}

func questionJudgeDatabasePrompt() []tool.QuestionPrompt {
	return []tool.QuestionPrompt{{
		Header:   "Database",
		Question: "Which database?",
		Options:  []tool.QuestionOption{{Label: "Postgres"}, {Label: "SQLite"}},
	}}
}

func answeredQuestionVerdict(toolCallID string, labels ...string) autoContinueQuestionJudgeFinishedMsg {
	answers := make([]tool.QuestionAnswer, 0, len(labels))
	for _, l := range labels {
		answers = append(answers, tool.QuestionAnswer{Label: l})
	}
	return autoContinueQuestionJudgeFinishedMsg{
		toolCallID: toolCallID,
		answered:   true,
		answers:    []tool.QuestionAnswerSet{{Header: "Database", Question: "Which database?", Answers: answers}},
		detail:     "jev answered on your behalf: Database → " + strings.Join(labels, ", "),
	}
}

func TestQuestionJudgeFinishedSubmitsMatchedLabels(t *testing.T) {
	m := questionJudgeTestModel(t, questionJudgeDatabasePrompt())

	updated, cmd := m.Update(answeredQuestionVerdict("call_1", "postgres"))
	got := derefTestModel(t, updated)
	if got.showQuestionDialog {
		t.Fatal("expected dialog to close once the judge answered")
	}
	var row *agent.Message
	for i := len(got.messages) - 1; i >= 0; i-- {
		if raw := got.messages[i].raw; raw != nil && raw.Role == "tool" && raw.ToolID == "call_1" {
			row = raw
			break
		}
	}
	if row == nil {
		t.Fatal("expected an answered tool row for call_1 in the transcript")
	}
	var sets []questionAnswerPayload
	if err := json.Unmarshal([]byte(row.Content), &sets); err != nil {
		t.Fatalf("answered row is not a payload: %v (%q)", err, row.Content)
	}
	if len(sets) != 1 || len(sets[0].Answers) != 1 || sets[0].Answers[0].Label != "Postgres" {
		t.Fatalf("expected the payload to select the original label Postgres, got %#v", sets)
	}
	if len(got.autoContinueActions) != 1 || got.autoContinueActions[0] != agent.AutoContinueAnswerQuestionAction || got.autoContinueCount != 1 {
		t.Fatalf("chain state = %v/%d, want [answer_question]/1", got.autoContinueActions, got.autoContinueCount)
	}
	if !got.lastTurnWasAutoContinue {
		t.Fatal("expected the answer turn to be marked as an auto-continue step")
	}
	if cmd == nil {
		t.Fatal("expected a turn-start command")
	}
}

func TestQuestionJudgeFinishedIgnoresTouchedDialog(t *testing.T) {
	m := questionJudgeTestModel(t, questionJudgeDatabasePrompt())
	m.questionSelected[0][1] = true

	updated, _ := m.Update(answeredQuestionVerdict("call_1", "Postgres"))
	got := derefTestModel(t, updated)
	if !got.showQuestionDialog || got.questionToolCallID != "call_1" {
		t.Fatal("expected the touched dialog to stay open")
	}
	if got.autoContinueCount != 0 || len(got.autoContinueActions) != 0 {
		t.Fatalf("touched dialog must not advance the chain, got %v/%d", got.autoContinueActions, got.autoContinueCount)
	}
	if !got.questionSelected[0][1] || got.questionSelected[0][0] {
		t.Fatalf("user selection must be unchanged, got %v", got.questionSelected[0])
	}
}

func TestQuestionJudgeFinishedStaleToolCallIgnored(t *testing.T) {
	m := questionJudgeTestModel(t, questionJudgeDatabasePrompt())

	updated, _ := m.Update(answeredQuestionVerdict("call_other", "Postgres"))
	got := derefTestModel(t, updated)
	if !got.showQuestionDialog || got.questionToolCallID != "call_1" {
		t.Fatal("a verdict for another tool call must not touch the open dialog")
	}
	if got.autoContinueCount != 0 || len(got.questionSelected[0]) != 0 {
		t.Fatalf("stale verdict changed state: count=%d selected=%v", got.autoContinueCount, got.questionSelected[0])
	}
}

func TestQuestionJudgeFinishedUnmatchedLabelKeepsDialog(t *testing.T) {
	m := questionJudgeTestModel(t, questionJudgeDatabasePrompt())

	updated, _ := m.Update(answeredQuestionVerdict("call_1", "MySQL"))
	got := derefTestModel(t, updated)
	if !got.showQuestionDialog {
		t.Fatal("expected dialog to stay open when the judge label matches no option")
	}
	if len(got.questionSelected[0]) != 0 || got.autoContinueCount != 0 {
		t.Fatalf("unmatched label must not select or advance, selected=%v count=%d", got.questionSelected[0], got.autoContinueCount)
	}
	last := got.messages[len(got.messages)-1]
	if !strings.Contains(last.text, "did not match") {
		t.Fatalf("expected a hint that the auto-answer did not match, got %q", last.text)
	}
}

func TestQuestionJudgeFinishedPartialMatchSelectsNothing(t *testing.T) {
	prompts := []tool.QuestionPrompt{
		{Header: "Database", Question: "Which database?", Options: []tool.QuestionOption{{Label: "Postgres"}, {Label: "SQLite"}}},
		{Header: "Hosting", Question: "Where?", Options: []tool.QuestionOption{{Label: "Yes"}, {Label: "No"}}},
	}
	m := questionJudgeTestModel(t, prompts)
	verdict := autoContinueQuestionJudgeFinishedMsg{
		toolCallID: "call_1",
		answered:   true,
		answers: []tool.QuestionAnswerSet{
			{Header: "Database", Question: "Which database?", Answers: []tool.QuestionAnswer{{Label: "Postgres"}}},
			{Header: "Hosting", Question: "Where?", Answers: []tool.QuestionAnswer{{Label: "Maybe"}}},
		},
	}

	updated, _ := m.Update(verdict)
	got := derefTestModel(t, updated)
	if !got.showQuestionDialog {
		t.Fatal("expected dialog to stay open when any prompt fails to match")
	}
	if len(got.questionSelected[0]) != 0 || len(got.questionSelected[1]) != 0 {
		t.Fatalf("a rejected verdict must leave every prompt unanswered, got %v / %v", got.questionSelected[0], got.questionSelected[1])
	}
	if got.autoContinueCount != 0 {
		t.Fatalf("a rejected verdict must not advance the chain, count=%d", got.autoContinueCount)
	}
}

func TestQuestionJudgeFinishedDroppedWhenAutoContinueOff(t *testing.T) {
	m := questionJudgeTestModel(t, questionJudgeDatabasePrompt())
	m.autoContinueEnabled = false

	updated, _ := m.Update(answeredQuestionVerdict("call_1", "Postgres"))
	got := derefTestModel(t, updated)
	if !got.showQuestionDialog || got.autoContinueCount != 0 {
		t.Fatalf("auto-continue turned off mid-flight must not answer, dialog=%v count=%d", got.showQuestionDialog, got.autoContinueCount)
	}
}

func TestQuestionJudgeEligibleGuards(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(m *model)
		eligible bool
	}{
		{name: "open untouched dialog", mutate: func(m *model) {}, eligible: true},
		{name: "goal evaluable without auto-continue", mutate: func(m *model) {
			m.autoContinueEnabled = false
			if err := m.agent.SetGoal("all tests pass"); err != nil {
				t.Fatalf("SetGoal: %v", err)
			}
		}, eligible: true},
		{name: "auto-continue off and no goal", mutate: func(m *model) { m.autoContinueEnabled = false }},
		{name: "no dialog open", mutate: func(m *model) { m.showQuestionDialog = false }},
		{name: "rc-owned prompt", mutate: func(m *model) { m.rcPendingQuestion = &rcPendingQuestion{} }},
		{name: "permission dialog up", mutate: func(m *model) { m.showPermDialog = true }},
		{name: "chain at cap", mutate: func(m *model) { m.autoContinueCount = autoContinueMaxChain }},
		{name: "second question queued", mutate: func(m *model) {
			m.questionAskQueue = []queuedQuestionAsk{{toolCallID: "call_2", prompts: questionJudgeDatabasePrompt()}}
		}},
		{name: "permission ask queued", mutate: func(m *model) { m.permAskQueue = []queuedPermAsk{{}} }},
		{name: "user already selected", mutate: func(m *model) { m.questionSelected[0][0] = true }},
		{name: "user typed custom text", mutate: func(m *model) { m.questionCustom[0] = "MySQL" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := questionJudgeTestModel(t, questionJudgeDatabasePrompt())
			tc.mutate(&m)
			if got := m.questionJudgeEligible(); got != tc.eligible {
				t.Fatalf("questionJudgeEligible() = %v, want %v", got, tc.eligible)
			}
		})
	}
}
