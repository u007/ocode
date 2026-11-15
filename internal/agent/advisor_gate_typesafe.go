package agent

import (
	"fmt"
	"math"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

// The advisor gate is a decision judge (slot advisor_gate) that answers one
// question before each advisor review: is a review worth running now? It is
// consulted only at the two moments. `review_now` runs the review, a `skip_*`
// above the floor suppresses it, and any failure leaves the caller on its own
// fallback rule.

// Moments at which the gate is consulted.
const (
	advisorMomentBeforeCommit = "before_commit"
	advisorMomentEndOfTurn    = "end_of_turn"
)

const (
	advisorGateKey = "review"

	advisorGateChoiceReview         = "review_now"
	advisorGateChoiceSkipReviewed   = "skip_already_reviewed"
	advisorGateChoiceSkipDeclined   = "skip_user_declined"
	advisorGateChoiceSkipNotSubst   = "skip_not_substantive"
	advisorGateChoiceSkipUnfinished = "skip_work_unfinished"

	// advisorGateChoiceFloor is the chosen-option probability any answer needs
	// before it is acted on, skip or review. Below it the caller falls back to
	// its own threshold rule, so a zero-confidence answer can neither suppress a
	// review nor withhold a commit.
	advisorGateChoiceFloor = 0.6

	advisorGateRequestRunes = 1500
	advisorGateCommandRunes = 200
	advisorGateNameRunes    = 40
	advisorGateVerdictRunes = 600
	advisorGateListMax      = 100
	advisorGateCommandsMax  = 30
	advisorGateReviewsMax   = 5
	advisorGateTodoLinesMax = 30
	advisorGateToolNamesMax = 20
)

// advisorGateInstructions is the builtin rubric for the gate.
const advisorGateInstructions = `You decide whether a second, slower model should review an AI coding assistant's work (the executor). A review takes minutes, so run one only when it is worth the wait.

The state holds:
- moment: before_commit (a git commit is about to run) or end_of_turn (the executor gave its final report).
- user_request: what the user asked for this turn.
- todo_plan: the task list, "[ ]" open and "[x]" done.
- session_changed_files: every file changed in the session.
- facts.review_count: how many reviews already ran in this session.
- facts.unreviewed_file_count and changed_since_last_review: files changed that NO review has covered yet. With review_count 0 that is every changed file.
- facts.files_written_this_turn: how many files this turn itself edited.
- reviews: earlier reviews, the files each covered and what it concluded.
- turn: this turn's tool calls, the files it wrote and its bash commands.
- commit_command or final_report.

How to decide:
- The user declines a review only by asking for a commit and nothing else ("commit", "just commit", "commit and push"), or by saying not to review or validate. A request to fix, implement or change something AND commit it is not declining: that work still needs its review, before the commit.
- "Already reviewed" is only possible when facts.review_count is at least 1 AND facts.unreviewed_file_count is 0. Files changed after the last review, such as fixes made in response to it, are unreviewed work.
- Unreviewed changes that only touch comments, wording or formatting are not substantive.
- A commit, push, tag or status check is housekeeping, not work to review. When user_request is a question and facts.files_written_this_turn is 0, the turn only investigated or answered: that is not substantive, even if older unreviewed changes exist.
- Only at end_of_turn: when todo_plan still has open items, or the report says more steps follow, the work is unfinished and a completion review is premature.
- Otherwise, when substantive unreviewed changes exist, review now.

Answer with one choice.`

// advisorGateVerdict is the gate's decision for one moment.
type advisorGateVerdict struct {
	// Decided is true only when the judge gave an answer the caller may act on.
	Decided bool
	// Review is true when the answer is review_now. It is meaningful only when Decided.
	Review bool
	// Choice is the judge's answer, empty when the judge gave none.
	Choice string
	// Detail says why the verdict was or was not decided. It goes to the debug log.
	Detail string
}

// advisorGateQuestions builds the single choice question. skip_work_unfinished
// is offered only at end_of_turn, so before_commit can never receive it.
func advisorGateQuestions(moment string) map[string]TypesafeQuestion {
	criteria := map[string]string{
		advisorGateChoiceReview:       "substantive code changes exist that no review has covered (facts.unreviewed_file_count is above 0), the user did not decline, and the work is not mid-way",
		advisorGateChoiceSkipReviewed: "facts.review_count is at least 1 and facts.unreviewed_file_count is 0: a review covered the changes and no file changed since",
		advisorGateChoiceSkipDeclined: "the user's request is only to commit or push, with no fix or change requested, or the user said not to review or validate",
		advisorGateChoiceSkipNotSubst: "this turn made no substantive change: only housekeeping, a status check, investigation, an answer to a question, or a comment, wording or formatting edit",
	}
	if moment == advisorMomentEndOfTurn {
		criteria[advisorGateChoiceSkipUnfinished] = "todo_plan still has open items or the final report says more steps follow, so a completion review is premature"
	}
	return map[string]TypesafeQuestion{
		advisorGateKey: {
			Type:         "choice",
			Instructions: config.ApplyPromptOverride("advisor_gate", advisorGateInstructions),
			Criteria:     criteria,
		},
	}
}

// advisorGateDecide asks the gate whether to review at moment. subject is the
// commit command (before_commit) or the executor's final report (end_of_turn).
// Every failure returns Decided false with a Detail, so the caller can fall back.
// It logs exactly one ADVISOR line and never panics out to the agent loop.
func (a *Agent) advisorGateDecide(st *advisorCheckpointState, moment, subject string) (v advisorGateVerdict) {
	defer func() {
		if r := recover(); r != nil {
			v = advisorGateVerdict{Detail: fmt.Sprintf("advisor gate panicked: %v", r)}
			a.emitDebug("ADVISOR", fmt.Sprintf("gate moment=%s not decided: %s\n%s", moment, v.Detail, debug.Stack()))
		}
	}()
	v, prob := a.advisorGateAsk(st, moment, subject)
	a.emitDebug("ADVISOR", fmt.Sprintf("gate moment=%s choice=%s probability=%.2f decided=%t review=%t: %s",
		moment, v.Choice, prob, v.Decided, v.Review, v.Detail))
	return v
}

// advisorGateAsk runs the judge call. It returns the verdict and the probability
// of the chosen option, for the debug line.
func (a *Agent) advisorGateAsk(st *advisorCheckpointState, moment, subject string) (advisorGateVerdict, float64) {
	if config.PromptDisabled("advisor_gate") {
		return advisorGateVerdict{Detail: "advisor gate prompt is disabled"}, 0
	}
	d := a.resolveDecider(slotAdvisorGate)
	if d == nil {
		return advisorGateVerdict{Detail: fmt.Sprintf("advisor gate model %s is unconfigured or has no API key", a.slotModel(slotAdvisorGate))}, 0
	}
	label := deciderLabel(d)
	questions := advisorGateQuestions(moment)
	ctx, cancel := stopChContext(a.StopCh())
	defer cancel()
	resp, err := d.DecideCtx(ctx, a.advisorGateState(st, moment, subject), questions)
	if err != nil {
		return advisorGateVerdict{Detail: label + " judge failed: " + err.Error()}, 0
	}
	if resp == nil {
		return advisorGateVerdict{Detail: label + " judge returned no response"}, 0
	}
	a.RecordSideUsage(resp.Usage.InputTokens, resp.Usage.OutputTokens, 0, 0, label)

	ans, ok := resp.Answers[advisorGateKey]
	if !ok {
		return advisorGateVerdict{Detail: label + " judge returned no answer"}, 0
	}
	if err := validateAnswer(questions[advisorGateKey], ans); err != nil {
		return advisorGateVerdict{Choice: ans.Choice, Detail: "answer rejected: " + err.Error()}, 0
	}
	prob, hasProb := ans.Probabilities[ans.Choice]
	if !hasProb {
		return advisorGateVerdict{Choice: ans.Choice, Detail: "answer rejected: no probability for " + ans.Choice}, 0
	}
	// NaN fails the comparison and an infinity is caught explicitly. The floor
	// covers every answer: a zero-confidence review_now must not withhold a
	// commit, and a zero-confidence skip must not suppress a review.
	if !(prob >= advisorGateChoiceFloor) || math.IsInf(prob, 0) {
		return advisorGateVerdict{Choice: ans.Choice, Detail: fmt.Sprintf("%s below the %.2f answer floor", ans.Choice, advisorGateChoiceFloor)}, prob
	}
	return advisorGateVerdict{
		Decided: true,
		Review:  ans.Choice == advisorGateChoiceReview,
		Choice:  ans.Choice,
		Detail:  label + ": " + ans.Choice,
	}, prob
}

// advisorGateState builds the judge's structured state. Every string is clipped
// and every list capped, so the shared decision budget is never the limit.
func (a *Agent) advisorGateState(st *advisorCheckpointState, moment, subject string) map[string]any {
	unreviewed := a.advisorChangedSinceLastReview()
	a.advisorLedgerMu.Lock()
	reviewCount := len(a.advisorReviews)
	a.advisorLedgerMu.Unlock()
	state := map[string]any{
		"moment": moment,
		"facts": map[string]any{
			"review_count":            reviewCount,
			"unreviewed_file_count":   len(unreviewed),
			"files_written_this_turn": len(st.filesWritten),
		},
		"user_request":              advisorGateClip(st.userGoal, advisorGateRequestRunes),
		"todo_plan":                 a.advisorGateTodoPlan(),
		"session_changed_files":     advisorGateCapList(a.ChangedFiles(), advisorGateListMax),
		"changed_since_last_review": advisorGateCapList(unreviewed, advisorGateListMax),
		"reviews":                   a.advisorGateReviews(),
		"turn": map[string]any{
			"tool_calls_by_name": advisorGateToolNames(st.toolCallsByName),
			"files_written":      advisorGateCapList(st.filesWritten, advisorGateListMax),
			"bash_commands":      advisorGateCommands(st.bashCommands),
		},
	}
	if moment == advisorMomentBeforeCommit {
		state["commit_command"] = advisorGateClip(subject, advisorGateCommandRunes)
	} else {
		state["final_report"] = advisorGateClip(subject, advisorGateRequestRunes)
	}
	return state
}

// advisorGateTodoPlan returns the session todo plan as text, read the same way
// injectTodoTail reads it (this agent's own scope, never the TUI cursor).
func (a *Agent) advisorGateTodoPlan() string {
	content := tool.ReadTodoSnapshot(a.todoScope()).Content
	if content == "" {
		return ""
	}
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	if len(lines) > advisorGateTodoLinesMax {
		lines = append(lines[:advisorGateTodoLinesMax], fmt.Sprintf("…(%d more lines)", len(lines)-advisorGateTodoLinesMax))
	}
	return advisorGateClip(strings.Join(lines, "\n"), advisorGateRequestRunes)
}

// advisorGateReviews returns the last few ledger entries, oldest first.
func (a *Agent) advisorGateReviews() []map[string]any {
	a.advisorLedgerMu.Lock()
	entries := a.advisorReviews
	if len(entries) > advisorGateReviewsMax {
		entries = entries[len(entries)-advisorGateReviewsMax:]
	}
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		out = append(out, map[string]any{
			"moment":  e.Moment,
			"files":   advisorGateCapList(e.Files, advisorGateListMax),
			"verdict": e.Verdict,
		})
	}
	a.advisorLedgerMu.Unlock()
	return out
}

// advisorGateCommands clips each bash command and caps the list, keeping the
// most recent commands.
func advisorGateCommands(cmds []string) []string {
	if len(cmds) > advisorGateCommandsMax {
		cmds = cmds[len(cmds)-advisorGateCommandsMax:]
	}
	out := make([]string, 0, len(cmds))
	for _, c := range cmds {
		out = append(out, advisorGateClip(c, advisorGateCommandRunes))
	}
	return out
}

// advisorGateToolNames caps the per-turn tool-call histogram at the
// most-called names. Tool names come from the model, so the map is bounded here
// rather than left to the shared decision budget, which it would otherwise be
// the only uncapped part of the state to reach.
func advisorGateToolNames(counts map[string]int) map[string]int {
	out := make(map[string]int, advisorGateToolNamesMax+1)
	if len(counts) == 0 {
		return out
	}
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	slices.SortFunc(names, func(a, b string) int {
		if counts[a] != counts[b] {
			return counts[b] - counts[a]
		}
		return strings.Compare(a, b)
	})
	for i, name := range names {
		if i == advisorGateToolNamesMax {
			out[fmt.Sprintf("…(%d more not shown)", len(names)-i)] = 0
			break
		}
		out[advisorGateClip(name, advisorGateNameRunes)] = counts[name]
	}
	return out
}

// advisorGateCapList copies at most limit items. A truncated list ends with a
// marker, so the judge can see that it is not the whole list.
func advisorGateCapList(items []string, limit int) []string {
	if len(items) <= limit {
		return append([]string(nil), items...)
	}
	out := make([]string, 0, limit+1)
	out = append(out, items[:limit]...)
	return append(out, fmt.Sprintf("…(%d more not shown)", len(items)-limit))
}

// advisorGateClip cuts s to n runes and marks the cut.
func advisorGateClip(s string, n int) string {
	if cut := truncateRunes(s, n); cut != s {
		return cut + "…(truncated)"
	}
	return s
}
