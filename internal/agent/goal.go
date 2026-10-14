package agent

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/u007/ocode/internal/crashguard"
)

// GoalStatus is the lifecycle state of a /goal condition.
type GoalStatus string

const (
	// GoalActive: the agent keeps taking turns until the judge decides otherwise.
	GoalActive GoalStatus = "active"
	// GoalPaused: the goal is retained but no longer auto-resumed. A human turn reactivates it.
	GoalPaused GoalStatus = "paused"
	// GoalMet: the judge confirmed the condition. Terminal.
	GoalMet GoalStatus = "met"
	// GoalImpossible: the judge confirmed the condition cannot be met. Terminal.
	GoalImpossible GoalStatus = "impossible"
)

const (
	GoalMaxConditionLen = 4000
	GoalMaxTurns        = 50
	// goalStallTurns is how many consecutive judged turns without any tool call
	// it takes to pause the goal. A model that only talks is not making progress.
	goalStallTurns = 3
	// goalMinConfidenceDefault is the confidence floor for a met or impossible
	// verdict. A lower confidence is treated as not met. Same reasoning as
	// autoContinueMinConfidenceDefault: the stakes are one more turn, not a grant.
	goalMinConfidenceDefault = 0.6
	// goalTailN is how many trailing messages the judge sees.
	goalTailN = 12
	// GoalMetadataKey is the session metadata key holding the persisted goal.
	GoalMetadataKey = "goal"
	goalMarker      = "[ocode:goal]"
)

// GoalState is one session's /goal condition and the judge's bookkeeping for it.
type GoalState struct {
	Condition   string
	Status      GoalStatus
	Turns       int
	NoToolTurns int
	// LastReason is the human-readable outcome of the most recent evaluation.
	LastReason string
	SetAt      time.Time
}

// GoalJudgeResult is the outcome of one goal evaluation. Resume is true only
// when the host should send GoalResumeMessage and run another turn.
//
// Verdict is one of met, not_met, impossible, stalled, capped, unavailable, or
// stale. stale means the goal was cleared, replaced, or changed by a human turn
// while the judge ran, so the verdict was dropped and nothing was written back.
// It is distinct from unavailable, which means the judge itself could not decide.
type GoalJudgeResult struct {
	Gen     uint64
	Verdict string
	Status  GoalStatus
	Detail  string
	Resume  bool
	Err     error
}

const (
	goalVerdictKey   = "verdict"
	goalReasonKey    = "reason"
	goalAwaitingUser = "awaiting_user"
)

// goalReasons is the closed set of reasons, in the order they are described to
// the judge. The reason is advisory: the verdict decides, except awaiting_user,
// which pauses the goal.
var goalReasons = []typesafeConcern{
	{"all_satisfied", "every part of the condition is satisfied and tool output shows it"},
	{"work_remains", "concrete work toward the condition remains"},
	{"unverified_claim", "the assistant claims completion without tool output that supports it"},
	{"blocked_by_error", "the last tool or command failed and nothing in the transcript fixes it yet"},
	{goalAwaitingUser, "the reply is waiting on the user: it asks a question or requests a decision, confirmation or input"},
	{"cannot_be_done", "the condition contradicts itself or needs something the assistant demonstrably cannot reach"},
}

const goalJudgeInstructions = `You are judging whether an AI coding assistant has met a goal the user set. The transcript is structured state: the condition, how many turns have run, and the conversation tail. Decide whether the condition is met now.
Rules:
- Judge only what the transcript shows. Do not assume work happened that the transcript does not show.
- A claim of completion with no supporting tool output in the transcript is "not_met" with reason "unverified_claim".
- Answer "impossible" only when the condition contradicts itself or needs something the assistant demonstrably cannot reach. Difficulty is not impossibility.
- A reply that ends on a question for the user, or waits on their decision, confirmation or input, is "not_met" with reason "awaiting_user".
- Answer "met" only when the transcript shows every part of the condition satisfied.
Choose "not_met" when the evidence is incomplete or unclear.`

// goalTurnUsedTools reports whether any assistant message after the last user
// message made a tool call. Only that window is this turn's work.
func goalTurnUsedTools(messages []Message) bool {
	start := 0
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			start = i + 1
			break
		}
	}
	for _, m := range messages[start:] {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			return true
		}
	}
	return false
}

func goalReasonLabel(key string) string {
	for _, r := range goalReasons {
		if r.Key == key {
			return r.Label
		}
	}
	return ""
}

func goalVerdictValid(choice string) bool {
	switch choice {
	case "met", "not_met", "impossible":
		return true
	}
	return false
}

func goalQuestions() map[string]TypesafeQuestion {
	reasonCriteria := make(map[string]string, len(goalReasons))
	for _, r := range goalReasons {
		reasonCriteria[r.Key] = r.Label
	}
	return map[string]TypesafeQuestion{
		goalVerdictKey: {
			Type:         "choice",
			Instructions: goalJudgeInstructions,
			Criteria: map[string]string{
				"met":        "every part of the condition is satisfied, and the transcript's tool output shows it",
				"not_met":    "the condition is not yet satisfied: work remains, the claim is unverified, or the assistant is waiting on the user",
				"impossible": "the condition cannot be met: it contradicts itself or needs something the assistant demonstrably cannot reach",
			},
		},
		goalReasonKey: {
			Type:         "choice",
			Instructions: goalJudgeInstructions + "\nName the single best reason for the verdict.",
			Criteria:     reasonCriteria,
		},
	}
}

// SetGoal sets the session's goal, replacing any previous one, and starts it active.
func (a *Agent) SetGoal(condition string) error {
	condition = strings.TrimSpace(condition)
	if condition == "" {
		return errors.New("goal condition is empty")
	}
	if n := utf8.RuneCountInString(condition); n > GoalMaxConditionLen {
		return fmt.Errorf("goal condition is %d characters; the limit is %d", n, GoalMaxConditionLen)
	}
	a.goalMu.Lock()
	defer a.goalMu.Unlock()
	a.goal = &GoalState{Condition: condition, Status: GoalActive, SetAt: time.Now()}
	a.goalGen++
	return nil
}

// ClearGoal removes the session's goal. An in-flight judge result for it is discarded.
func (a *Agent) ClearGoal() {
	a.goalMu.Lock()
	defer a.goalMu.Unlock()
	a.goal = nil
	a.goalGen++
}

// Goal returns a copy of the session's goal state. ok is false when no goal was
// ever set or it was cleared.
func (a *Agent) Goal() (GoalState, bool) {
	a.goalMu.Lock()
	defer a.goalMu.Unlock()
	if a.goal == nil {
		return GoalState{}, false
	}
	return *a.goal, true
}

// GoalEvaluable reports whether the session has a goal the judge should still
// evaluate: active or paused.
func (a *Agent) GoalEvaluable() bool {
	gs, ok := a.Goal()
	return ok && (gs.Status == GoalActive || gs.Status == GoalPaused)
}

// RestoreGoal reinstates a goal read back from session metadata. A record with
// no condition is not a goal, so there is nothing to restore.
func (a *Agent) RestoreGoal(gs GoalState) {
	if gs.Condition == "" {
		return
	}
	a.goalMu.Lock()
	defer a.goalMu.Unlock()
	restored := gs
	a.goal = &restored
	a.goalGen++
}

// GoalMarkHumanTurn reactivates a paused goal when the human sends a message.
// A met or impossible goal stays terminal, and an active one is left alone.
func (a *Agent) GoalMarkHumanTurn() {
	a.goalMu.Lock()
	defer a.goalMu.Unlock()
	if a.goal == nil || a.goal.Status != GoalPaused {
		return
	}
	a.goal.Status = GoalActive
	a.goal.NoToolTurns = 0
	a.goalGen++
}

// GoalKickoffPrompt is the first user turn after /goal <text>.
func (a *Agent) GoalKickoffPrompt() string {
	gs, ok := a.Goal()
	if !ok {
		return ""
	}
	return "Work toward this goal until it is met: " + gs.Condition + "\nVerify with tools before claiming it is met; do not stop to ask the user unless genuinely blocked."
}

// GoalResumeMessage is the user-role turn that keeps the agent working after a
// judge verdict of not_met.
func (a *Agent) GoalResumeMessage() Message {
	gs, _ := a.Goal()
	return Message{
		Role: "user",
		Content: goalMarker + "\n" +
			"Goal not yet met: " + gs.Condition + "\n" +
			"Evaluator: " + gs.LastReason + "\n" +
			"Continue working toward the goal; verify with tools before reporting it met.",
	}
}

// GoalStatusText is the shared status copy for the TUI /goal command and the GET endpoint.
func (a *Agent) GoalStatusText() string {
	gs, ok := a.Goal()
	if !ok {
		return "No goal set."
	}
	judge := "unavailable"
	if d := a.resolveDecider(slotGoal); d != nil {
		judge = deciderLabel(d)
	}
	return GoalStatusTextFor(gs, judge)
}

// GoalStatusTextFor renders gs as the shared status copy. An empty judge leaves
// the Judge line out, for callers that hold a persisted goal and no agent to
// resolve a judge from.
func GoalStatusTextFor(gs GoalState, judge string) string {
	reason := gs.LastReason
	if reason == "" {
		reason = "none yet"
	}
	text := fmt.Sprintf("Goal: %s\nStatus: %s\nTurns: %d\nLast reason: %s", gs.Condition, gs.Status, gs.Turns, reason)
	if judge != "" {
		text += "\nJudge: " + judge
	}
	return text
}

// GoalJudgeAsync runs GoalJudgeSync in the background and delivers the result to
// OnGoalJudge tagged with gen. It returns false, and dispatches nothing, when the
// goal is not evaluable or a judge call is already in flight.
func (a *Agent) GoalJudgeAsync(messages []Message, gen uint64) bool {
	if !a.GoalEvaluable() {
		return false
	}
	if !a.goalJudgeMu.TryLock() {
		return false
	}
	snapshot := make([]Message, len(messages))
	copy(snapshot, messages)
	crashguard.Go(func() {
		defer a.goalJudgeMu.Unlock()
		res := a.GoalJudgeSync(snapshot)
		res.Gen = gen
		if a.OnGoalJudge != nil {
			a.OnGoalJudge(res)
		}
	})
	return true
}

// GoalJudgeSync evaluates the goal after a turn: stall and cap guards first, then
// the judge's verdict, which can pause or end the goal. It mutates the goal state.
//
// The judge call runs without goalMu held. The verdict is written back only if
// the goal is unchanged since the snapshot. Otherwise the verdict is discarded,
// so a /goal clear or reset that lands mid-call is never overwritten.
func (a *Agent) GoalJudgeSync(messages []Message) GoalJudgeResult {
	a.goalMu.Lock()
	if a.goal == nil || a.goal.Status == GoalMet || a.goal.Status == GoalImpossible {
		a.goalMu.Unlock()
		return GoalJudgeResult{Verdict: "unavailable", Detail: "no goal is being evaluated"}
	}
	gen := a.goalGen
	gs := *a.goal
	a.goalMu.Unlock()

	res := a.decideGoal(&gs, messages)
	res.Status = gs.Status

	a.goalMu.Lock()
	if a.goal == nil || a.goalGen != gen {
		res = GoalJudgeResult{Verdict: "stale", Detail: "goal changed while it was being judged; verdict discarded"}
		if a.goal != nil {
			res.Status = a.goal.Status
		}
		a.goalMu.Unlock()
	} else {
		*a.goal = gs
		a.goalGen++
		a.goalMu.Unlock()
	}
	a.emitDebug("AGENT", fmt.Sprintf("goal verdict=%s status=%s turns=%d detail=%s", res.Verdict, res.Status, gs.Turns, res.Detail))
	return res
}

// decideGoal applies the evaluation steps in order, each returning when it decides.
// It works on gs, a private copy, and the caller writes it back.
func (a *Agent) decideGoal(gs *GoalState, messages []Message) GoalJudgeResult {
	gs.Turns++
	if goalTurnUsedTools(messages) {
		gs.NoToolTurns = 0
	} else {
		gs.NoToolTurns++
	}
	if gs.NoToolTurns >= goalStallTurns {
		return goalPause(gs, "stalled", fmt.Sprintf("no tool use in the last %d turns", gs.NoToolTurns), nil)
	}
	if gs.Turns >= GoalMaxTurns {
		return goalPause(gs, "capped", fmt.Sprintf("reached the %d-turn cap", GoalMaxTurns), nil)
	}

	d := a.resolveDecider(slotGoal)
	if d == nil {
		return goalPause(gs, "unavailable", fmt.Sprintf("goal judge model %s is unconfigured or has no API key", a.slotModel(slotGoal)), nil)
	}
	label := deciderLabel(d)

	state := map[string]any{
		"condition":       gs.Condition,
		"turns":           gs.Turns,
		"transcript_tail": typesafeTranscriptTail(messages, goalTailN),
		"turn_ended":      a.typesafeTurnEnded(nil),
	}
	resp, err := d.Decide(state, goalQuestions())
	if err != nil {
		return goalPause(gs, "unavailable", label+" judge failed: "+err.Error(), err)
	}
	if resp == nil {
		return goalPause(gs, "unavailable", label+" judge returned no response", nil)
	}
	a.RecordSideUsage(resp.Usage.InputTokens, resp.Usage.OutputTokens, 0, 0, label)

	ans, ok := resp.Answers[goalVerdictKey]
	if !ok || ans.Type != "choice" || !goalVerdictValid(ans.Choice) {
		return goalPause(gs, "unavailable", fmt.Sprintf("%s judge returned no valid verdict", label), nil)
	}

	reasonKey := ""
	if r, ok := resp.Answers[goalReasonKey]; ok && r.Type == "choice" {
		reasonKey = r.Choice
	}
	reason := goalReasonLabel(reasonKey)
	if reason == "" {
		reason = "no reason given"
	}
	gs.LastReason = reason

	// The NaN and out-of-range cases fall out of the comparisons: NaN >= floor is
	// false, and an infinity fails the <= 1 bound. Either way the verdict is below floor.
	conf := ans.Confidence
	verdict := ans.Choice
	detail := fmt.Sprintf("%s: %s (%.0f%% confidence); reason: %s", label, verdict, conf*100, reason)
	if verdict != "not_met" && !(conf >= goalMinConfidenceDefault && conf <= 1) {
		detail = fmt.Sprintf("%s said %s at %.0f%% confidence, below the %.2f floor; treated as not met (reason: %s)", label, verdict, conf*100, goalMinConfidenceDefault, reason)
		verdict = "not_met"
	}
	switch verdict {
	case "met":
		gs.Status = GoalMet
		return GoalJudgeResult{Verdict: "met", Detail: detail}
	case "impossible":
		gs.Status = GoalImpossible
		return GoalJudgeResult{Verdict: "impossible", Detail: detail}
	}
	if reasonKey == goalAwaitingUser {
		gs.Status = GoalPaused
		return GoalJudgeResult{Verdict: "not_met", Detail: detail}
	}
	gs.Status = GoalActive
	return GoalJudgeResult{Verdict: "not_met", Detail: detail, Resume: true}
}

// goalPause records a decision that stops the goal without a usable verdict.
func goalPause(gs *GoalState, verdict, detail string, err error) GoalJudgeResult {
	gs.Status = GoalPaused
	gs.LastReason = detail
	return GoalJudgeResult{Verdict: verdict, Detail: detail, Err: err}
}

// GoalStateToMetadata returns the session metadata map holding gs under GoalMetadataKey.
func GoalStateToMetadata(gs GoalState) map[string]any {
	return map[string]any{
		GoalMetadataKey: map[string]any{
			"condition":     gs.Condition,
			"status":        string(gs.Status),
			"turns":         gs.Turns,
			"no_tool_turns": gs.NoToolTurns,
			"last_reason":   gs.LastReason,
			"set_at":        gs.SetAt.UTC().Format(time.RFC3339),
		},
	}
}

// GoalStateFromMetadata reads a goal back from session metadata. ok is false when
// the record is absent or malformed.
func GoalStateFromMetadata(meta map[string]any) (GoalState, bool) {
	raw, ok := meta[GoalMetadataKey].(map[string]any)
	if !ok {
		return GoalState{}, false
	}
	condition, _ := raw["condition"].(string)
	status, _ := raw["status"].(string)
	lastReason, _ := raw["last_reason"].(string)
	setAtText, _ := raw["set_at"].(string)
	setAt, err := time.Parse(time.RFC3339, setAtText)
	turns, okTurns := goalMetaInt(raw["turns"])
	noToolTurns, okNoTool := goalMetaInt(raw["no_tool_turns"])
	gs := GoalState{
		Condition:   condition,
		Status:      GoalStatus(status),
		Turns:       turns,
		NoToolTurns: noToolTurns,
		LastReason:  lastReason,
		SetAt:       setAt,
	}
	if condition == "" || !okTurns || !okNoTool || err != nil || !goalStatusValid(gs.Status) {
		return GoalState{}, false
	}
	return gs, true
}

// goalMetaInt accepts int (in-process) and float64 (after a JSON round trip).
func goalMetaInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case float64:
		if n != math.Trunc(n) {
			return 0, false
		}
		return int(n), true
	}
	return 0, false
}

func goalStatusValid(s GoalStatus) bool {
	switch s {
	case GoalActive, GoalPaused, GoalMet, GoalImpossible:
		return true
	}
	return false
}
