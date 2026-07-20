package agent

import (
	"encoding/json"
	"fmt"
	"strings"
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
	// deferredWriteIntent is set when the plan checkpoint deferred a write batch.
	// It keeps the final-answer checkpoint from being bypassed if the model
	// acknowledges the review and then stops without re-issuing the writes.
	deferredWriteIntent bool
	toolCalls           int
	writeCalls          int
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
// non-triviality threshold. Deferred (plan-checkpointed) batches are not
// counted — their calls never executed.
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
func (a *Agent) runAdvisorCheckpoint(kind, prompt string) (string, bool) {
	t, ok := a.tools["advisor"]
	if !ok {
		return "", false
	}
	args, err := json.Marshal(map[string]string{"prompt": prompt})
	if err != nil {
		emitDebug("ADVISOR", fmt.Sprintf("%s checkpoint: marshal failed: %v", kind, err))
		return "", false
	}
	emitDebug("ADVISOR", fmt.Sprintf("%s checkpoint firing", kind))
	a.activity.toolStarted("advisor")
	advice, err := t.Execute(args)
	a.activity.toolDone("advisor")
	if err != nil {
		emitDebug("ADVISOR", fmt.Sprintf("%s checkpoint failed (continuing without advice): %v", kind, err))
		return "", false
	}
	advice = strings.TrimSpace(advice)
	if advice == "" {
		emitDebug("ADVISOR", fmt.Sprintf("%s checkpoint returned empty advice (continuing)", kind))
		return "", false
	}
	return advice, true
}

// advisorPlanCheckpoint fires once per Step, on the first assistant batch that
// contains a write-class tool call. The whole batch is deferred: every call
// gets a synthetic tool result carrying (or referencing) the advisor's review,
// and the loop continues so the model can re-issue or adjust. Returns nil when
// the checkpoint does not apply (or the advisor is unavailable), in which case
// the batch executes normally.
func (a *Agent) advisorPlanCheckpoint(st *advisorCheckpointState, resp *Message) []Message {
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
	prompt := fmt.Sprintf(`PLAN CHECKPOINT: the executor is about to make its first code changes for this task. Review the plan before implementation.

User goal:
%s

Executor's reasoning:
%s

Proposed tool calls:
%s
Advise: is this the right approach? Wrong files, missing prior exploration, simpler alternative, risks? Answer with either "proceed" plus cautions, or a corrected approach.`,
		st.userGoal, resp.Content, plan.String())

	advice, ok := a.runAdvisorCheckpoint(checkpointPlan, prompt)
	if !ok {
		return nil
	}
	st.deferredWriteIntent = true

	msgs := make([]Message, 0, len(resp.ToolCalls))
	for j, tc := range resp.ToolCalls {
		content := "[advisor plan checkpoint] Call deferred, not executed. See the advisor review in the first tool result of this batch; re-issue this call if the plan stands."
		if j == 0 {
			content = "[advisor plan checkpoint] Call deferred, not executed. Advisor review of your plan:\n\n" + advice +
				"\n\nIf the review confirms your approach, re-issue the deferred calls unchanged. Otherwise adjust your plan first. This checkpoint fires only once per turn."
		}
		msgs = append(msgs, Message{Role: "tool", ToolID: tc.ID, Content: content})
	}
	return msgs
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
	if st.writeCalls == 0 && st.toolCalls < doneCheckpointMinToolCalls && !st.deferredWriteIntent {
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

Check: does the report satisfy the full goal? Any unvalidated claims (tests not run, build not checked), missed requirements, or loose ends? Answer "complete" if satisfied, otherwise enumerate the specific gaps to fix.`,
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
