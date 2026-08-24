package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/u007/ocode/internal/tool"
)

// Advisor checkpoint names, matching cfg.Ocode.Advisor.Checkpoints values.
const (
	checkpointPlan = "plan"
	checkpointDone = "done"
)

// doneCheckpointMinToolCalls sets the threshold for the "done" checkpoint: a
// turn must have at least this many total tool calls OR at least one write-class
// tool call to be considered non-trivial. Trivial turns (e.g. a single read +
// "here's the answer") skip the completion review to avoid noise.
//
// This is a deliberate policy default chosen to balance thoroughness (catching
// incomplete work) against annoyance (blocking on trivial queries). It is not
// currently configurable; change it here to adjust the project-wide behavior.
const doneCheckpointMinToolCalls = 5

// advisorCheckpointState tracks per-Step checkpoint bookkeeping. Each Step
// invocation gets a fresh state, so every checkpoint fires at most once per
// user turn.
type advisorCheckpointState struct {
	userGoal    string
	planChecked bool
	doneChecked bool
	toolCalls   int
	writeCalls  int
}

func (a *Agent) newAdvisorCheckpointState(userGoal string) *advisorCheckpointState {
	return &advisorCheckpointState{userGoal: userGoal}
}

func lastUserContent(messages []Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].Content
		}
	}
	return ""
}

// countBatch records an executed tool batch toward the "done" checkpoint's
// non-triviality threshold.
func (st *advisorCheckpointState) countBatch(calls []ToolCall) {
	st.toolCalls += len(calls)
	for _, tc := range calls {
		if isWriteTool(tc.Function.Name) {
			st.writeCalls++
		}
	}
}

// advisorCheckpointEnabled reports whether the named checkpoint should fire:
// the agent must be the top-level agent (sub-agents never checkpoint —
// parallel sub-agents would contend on the advisor recursion guard), the
// advisor must be enabled (runtime gate, reactive to mid-run toggles),
// and the checkpoint must be listed in cfg.Ocode.Advisor.Checkpoints.
// The advisor model is resolved by AdvisorTool.resolveModel() with a
// built-in default fallback, so absence of an explicit model is not a
// gate — the advisor call itself handles client creation failure gracefully.
func (a *Agent) advisorCheckpointEnabled(name string) bool {
	if a.spec != nil {
		return false
	}
	enabled := a.advisorEnabled.Load()
	if a.parentAdvisorEnabled != nil {
		enabled = a.parentAdvisorEnabled.Load()
	}
	if !enabled {
		return false
	}
	if a.config == nil {
		return false
	}
	for _, c := range a.config.Ocode.Advisor.Checkpoints {
		if strings.EqualFold(strings.TrimSpace(c), name) {
			return true
		}
	}
	return false
}

// runAdvisorCheckpoint executes the advisor tool with the given prompt.
// Advisor failure must never block the agent loop, so errors are logged and
// reported as ok=false — the caller proceeds without advice.
//
// The call is bound to the agent's stop channel, so Escape aborts an in-flight
// advisor instead of leaving the loop parked on it (a Claude Code advisor
// subprocess runs up to 5 minutes). OnAdvisorCheckpoint brackets the call so
// the UI can show that the loop is waiting on a second model.
func (a *Agent) runAdvisorCheckpoint(kind, prompt string) (string, bool) {
	t, ok := a.tools["advisor"]
	if !ok {
		return "", false
	}
	args, err := json.Marshal(map[string]string{"prompt": prompt})
	if err != nil {
		a.emitDebug("ADVISOR", fmt.Sprintf("%s checkpoint: marshal failed: %v", kind, err))
		return "", false
	}
	a.emitDebug("ADVISOR", fmt.Sprintf("%s checkpoint firing", kind))
	a.activity.toolStarted("advisor")
	if a.OnAdvisorCheckpoint != nil {
		a.OnAdvisorCheckpoint(kind, true)
	}
	ctx, cancel := stopChContext(a.StopCh())
	var advice string
	if ct, isCtx := t.(tool.ContextualTool); isCtx {
		advice, err = ct.ExecuteCtx(ctx, args)
	} else {
		advice, err = t.Execute(args)
	}
	cancel()
	if a.OnAdvisorCheckpoint != nil {
		a.OnAdvisorCheckpoint(kind, false)
	}
	a.activity.toolDone("advisor")
	if err != nil {
		a.emitDebug("ADVISOR", fmt.Sprintf("%s checkpoint failed (continuing without advice): %v", kind, err))
		return "", false
	}
	advice = strings.TrimSpace(advice)
	if advice == "" {
		a.emitDebug("ADVISOR", fmt.Sprintf("%s checkpoint returned empty advice (continuing)", kind))
		return "", false
	}
	return advice, true
}

// advisorPlanCheckpoint fires once per Step, immediately after the first
// executed tool batch that contained a write-class call. The batch is NOT
// deferred: the writes are already applied, so the model never regenerates
// them. The review comes back as a user message injected into the loop, which
// the model acts on next iteration (correcting the applied change if the
// advisor found a problem). Returns nil when the checkpoint does not apply, or
// when the advisor is unavailable, in which case the loop continues unchanged.
func (a *Agent) advisorPlanCheckpoint(st *advisorCheckpointState, resp *Message) *Message {
	if st.planChecked || len(resp.ToolCalls) == 0 || !a.advisorCheckpointEnabled(checkpointPlan) {
		return nil
	}
	hasWrite := false
	for _, tc := range resp.ToolCalls {
		if isWriteTool(tc.Function.Name) {
			hasWrite = true
			break
		}
	}
	if !hasWrite {
		return nil
	}
	st.planChecked = true

	var plan strings.Builder
	for _, tc := range resp.ToolCalls {
		fmt.Fprintf(&plan, "- %s %s\n", tc.Function.Name, summarizeToolArgs(tc.Function.Name, tc.Function.Arguments))
	}
	prompt := fmt.Sprintf(`PLAN CHECKPOINT: the executor has just made its first code changes for this task. Review the approach now, while it is still cheap to correct.

User goal:
%s

Executor's reasoning:
%s

Tool calls just applied:
%s
Advise: is this the right approach? Wrong files, missing prior exploration, simpler alternative, risks? Answer with either "proceed" plus cautions, or a corrected approach. This is an automated checkpoint — the executor cannot answer follow-up questions; if context is thin, give your best read with explicit caveats rather than requesting more information.`,
		st.userGoal, resp.Content, plan.String())

	advice, ok := a.runAdvisorCheckpoint(checkpointPlan, prompt)
	if !ok {
		return nil
	}

	return &Message{
		Role: "user",
		Content: "[advisor plan checkpoint] An advisor reviewed the changes you just made:\n\n" + advice +
			"\n\nIf the review confirms your approach, carry on. Otherwise correct the changes now — they are already applied, so fix them in place rather than re-issuing the same calls. This checkpoint fires only once per turn.",
	}
}

// advisorDoneCheckpoint fires once per Step when the model produces a final
// message (no tool calls) after non-trivial work: at least one write-class
// call or doneCheckpointMinToolCalls total tool calls this turn. It returns a
// user message carrying the advisor's completion review, or nil to let the
// turn end.
func (a *Agent) advisorDoneCheckpoint(st *advisorCheckpointState, resp *Message) *Message {
	if st.doneChecked || !a.advisorCheckpointEnabled(checkpointDone) {
		return nil
	}
	if st.writeCalls == 0 && st.toolCalls < doneCheckpointMinToolCalls {
		return nil
	}
	st.doneChecked = true

	prompt := fmt.Sprintf(`COMPLETION CHECKPOINT: the executor believes this task is complete. Verify before it reports done.

User goal:
%s

Files changed this session:
%s

Executor's final report:
%s

Check: does the report satisfy the full goal? Any unvalidated claims (tests not run, build not checked), missed requirements, or loose ends? Answer "complete" if satisfied, otherwise enumerate the specific gaps to fix. This is an automated checkpoint — the executor cannot answer follow-up questions; if context is thin, give your best read with explicit caveats rather than requesting more information.`,
		st.userGoal, formatChangedFiles(a.ChangedFiles()), resp.Content)

	advice, ok := a.runAdvisorCheckpoint(checkpointDone, prompt)
	if !ok {
		return nil
	}
	return &Message{
		Role: "user",
		Content: "[advisor completion checkpoint] Before finishing, an advisor reviewed your final report:\n\n" + advice +
			"\n\nIf gaps were identified, address them now. If the review confirms completion, restate your final answer. This checkpoint fires only once per turn.",
	}
}

// summarizeToolArgs renders a compact one-line description of a tool call's
// arguments for the plan prompt: the target file path when extractable,
// otherwise the raw args whitespace-collapsed and truncated.
func summarizeToolArgs(toolName, args string) string {
	if p := extractTouchFilePath(toolName, args); p != "" {
		return p
	}
	const max = 160
	s := strings.Join(strings.Fields(args), " ")
	if len(s) > max {
		s = s[:max] + "…"
	}
	return s
}

func formatChangedFiles(files []string) string {
	if len(files) == 0 {
		return "(none tracked)"
	}
	return strings.Join(files, "\n")
}
