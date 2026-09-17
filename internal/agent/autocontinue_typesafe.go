package agent

import (
	"fmt"
	"strconv"
	"strings"
)

// autoContinueMinConfidence is the confidence floor for a TypeSafe triage
// "continue" verdict, mirroring the auto-permission judge's
// permissions.auto.min_confidence default: an ambiguous "looks cut off" below
// the floor fails closed (no resume).
const autoContinueMinConfidence = 0.85

// Typesafe auto-continue triage question keys. The verdict question decides;
// the reason question is advisory (Jev cannot write prose, so it picks a
// typed category explaining the verdict).
const (
	typesafeAutoContinueVerdictKey = "verdict"
	typesafeAutoContinueReasonKey  = "reason"
)

// typesafeAutoContinueReasons is the closed set of triage outcome categories,
// in the order they are described to the model. "finished" must stay first:
// it is the expected answer for a reply that naturally completed.
var typesafeAutoContinueReasons = []typesafeConcern{
	{"finished", "the reply completed the request; nothing is missing"},
	{"mid_task", "the reply stopped mid-task and work visibly remains (it says it will continue, or the task is plainly unfinished)"},
	{"truncated", "the reply looks mechanically truncated (an unclosed code block, a cut-off sentence)"},
	{"awaiting_user", "the reply ends by asking the user a question or requesting input"},
	{"errored", "the reply reports an error or failure and stopped"},
}

func typesafeAutoContinueReasonLabel(key string) string {
	for _, c := range typesafeAutoContinueReasons {
		if c.Key == key {
			return c.Label
		}
	}
	return "unrecognised reason " + strconv.Quote(key)
}

// typesafeAutoContinueInstructions is the triage rubric sent as the verdict
// question's instructions. The transcript travels as structured state; this
// text only explains how to judge it.
const typesafeAutoContinueInstructions = `You are triaging whether an AI coding assistant's most recent reply finished its task or was cut off mid-work. Decide whether the assistant should automatically continue working (without new human input) or stop.
Rules:
- Judge the last assistant reply in the transcript; earlier turns are context.
- Continue only when there is concrete remaining work the reply itself acknowledges or leaves visible: it says it will continue, a code block or sentence is cut off mid-way, or the requested task is plainly incomplete.
- Stop when the reply reads as a natural completion: it answered the request, delivered the result, and ends cleanly — including short acknowledgements and status reports.
- Stop when the reply ends with a question for the user or a report of a blocking error: continuing would not help.
- When the state says the turn was ended by the step limit, the reply is a forced summary of unfinished work: continue.
- When the state says the turn failed with an error, do not continue: the caller surfaces the error instead.
Choose "continue" only when the evidence is unambiguous; otherwise choose "end" so the turn finishes.`

// runAutoContinueJudgeTypesafe consults the TypeSafe System One model (Jev)
// for one triage decision: was the most recent assistant reply cut off
// mid-task (resume) or did it finish naturally (end)? Unlike the chat judge
// it receives the conversation tail as structured state and answers typed
// choices — TypesafeClient.Chat always fails with ErrTypesafeDecisionOnly, so
// a typesafe AutoContinueModel cannot go through runAutoContinueJudge.
//
// Returns (resume, detail, err): resume=false on any error or ambiguous
// answer (fail closed — never auto-resume on an unclear verdict), and detail
// is a user-facing one-liner naming the verdict and why.
func (a *Agent) runAutoContinueJudgeTypesafe(client *TypesafeClient, messages []Message) (bool, string, error) {
	stepLimited := a.StepLimitHit()
	if stepLimited {
		// The hard signal already justifies a resume; Jev only adds noise and
		// latency here. Report the deterministic outcome directly so callers
		// render one consistent line either way.
		return true, "typesafe/" + client.Model + ": step-limit cutoff — resuming without triage", nil
	}

	state := a.buildTypesafeAutoContinueState(messages)
	reasonCriteria := make(map[string]string, len(typesafeAutoContinueReasons))
	for _, r := range typesafeAutoContinueReasons {
		reasonCriteria[r.Key] = r.Label
	}
	questions := map[string]TypesafeQuestion{
		typesafeAutoContinueVerdictKey: {
			Type:         "choice",
			Instructions: typesafeAutoContinueInstructions,
			Criteria: map[string]string{
				"continue": "The last reply was cut off mid-task and concrete work remains; the assistant should resume.",
				"end":      "The last reply completed the request (or ends on a user question / blocking error); the turn is done.",
			},
		},
		typesafeAutoContinueReasonKey: {
			Type:         "choice",
			Instructions: typesafeAutoContinueInstructions + "\nName the single best description of how the last reply ended, or \"finished\" if it completed the request.",
			Criteria:     reasonCriteria,
		},
	}

	resp, err := client.Decide(state, questions)
	if err != nil {
		detail := "typesafe/" + client.Model + " triage failed: " + err.Error()
		return false, detail, err
	}
	a.RecordSideUsage(resp.Usage.InputTokens, resp.Usage.OutputTokens, 0, 0, "typesafe/"+client.Model)

	ans, ok := resp.Answers[typesafeAutoContinueVerdictKey]
	if !ok || ans.Type != "choice" {
		detail := fmt.Sprintf("typesafe/%s triage returned no verdict (answers=%d)", client.Model, len(resp.Answers))
		return false, detail, nil
	}

	minConfidence := autoContinueMinConfidence
	if a.config != nil && a.config.Ocode.Permissions.Auto != nil && a.config.Ocode.Permissions.Auto.MinConfidence > 0 {
		minConfidence = a.config.Ocode.Permissions.Auto.MinConfidence
	}
	reasonKey := ""
	if r, ok := resp.Answers[typesafeAutoContinueReasonKey]; ok && r.Type == "choice" {
		reasonKey = r.Choice
	}
	reason := ""
	if reasonKey != "" {
		reason = "; reason: " + typesafeAutoContinueReasonLabel(reasonKey)
	}
	model := "typesafe/" + client.Model
	a.emitDebug("AGENT", fmt.Sprintf("autocontinue_typesafe verdict=%s confidence=%.3f min=%.2f reason=%s", ans.Choice, ans.Confidence, minConfidence, reasonKey))

	switch ans.Choice {
	case "continue":
		if ans.Confidence < minConfidence {
			return false, fmt.Sprintf("%s leaned continue but confidence %.2f is below the %.2f floor%s", model, ans.Confidence, minConfidence, reason), nil
		}
		return true, fmt.Sprintf("%s: looks cut off (%.0f%% confidence)%s — resuming", model, ans.Confidence*100, reason), nil
	case "end":
		return false, fmt.Sprintf("%s: reply finished%s (%.0f%% confidence)", model, reason, ans.Confidence*100), nil
	default:
		detail := fmt.Sprintf("%s returned unknown choice %q", model, ans.Choice)
		return false, detail, nil
	}
}

// buildTypesafeAutoContinueState assembles the structured triage request: the
// conversation tail (roles + bounded content), plus how the turn ended so Jev
// does not have to infer it. The tail mirrors runAutoContinueJudge's budget —
// the last 6 non-empty messages, each trimmed, so one huge tool result cannot
// blow up the request.
func (a *Agent) buildTypesafeAutoContinueState(messages []Message) map[string]any {
	const tailN = 6
	tail := messages
	if len(tail) > tailN {
		tail = tail[len(tail)-tailN:]
	}
	entries := make([]map[string]any, 0, len(tail))
	for _, m := range tail {
		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}
		if len(content) > 4000 {
			content = content[:4000] + "…(truncated)"
		}
		entries = append(entries, map[string]any{"role": m.Role, "content": content})
	}
	state := map[string]any{
		"transcript_tail": entries,
	}
	if step := a.StepLimitHitDetail(nil); step != "" {
		state["turn_ended"] = step
	} else {
		state["turn_ended"] = "the reply finished within the step budget"
	}
	if a.config != nil {
		state["auto_continue_model"] = a.config.Ocode.AutoContinueModel
	}
	return state
}

// AutoContinueJudgeSync is the server-turn variant of AutoContinueJudgeAsync:
// headless turns run inside a synchronous HTTP goroutine with no event loop
// to receive OnAutoContinueJudge, so the triage runs inline and returns the
// verdict directly. Same fail-closed contract: no judge configured →
// (false, "", nil); any judge error → (false, detail, err).
func (a *Agent) AutoContinueJudgeSync(messages []Message) (bool, string, error) {
	client, isTypesafe := a.autoContinueJudgeClientTyped()
	if client == nil {
		return false, "", nil
	}
	if isTypesafe {
		return a.runAutoContinueJudgeTypesafe(client.(*TypesafeClient), messages)
	}
	resume, err := a.runAutoContinueJudge(client, messages)
	detail := "chat judge " + client.GetProvider() + "/" + client.GetModel()
	if err != nil {
		detail += ": " + err.Error()
	} else if resume {
		detail += ": looks cut off — resuming"
	} else {
		detail += ": reply finished"
	}
	return resume, detail, err
}

// AutoContinueEnabled reports whether the auto-continue feature is switched
// on for this agent's config. Server turns consult it before spending a
// triage call; the TUI tracks its own toggle state.
func (a *Agent) AutoContinueEnabled() bool {
	return a.config != nil && a.config.Ocode.AutoContinueEnabled
}

// AutoContinueChainCap mirrors the TUI's autoContinueMaxChain so every host
// (TUI, headless server, scheduler) shares one bound on consecutive
// auto-fired resumes.
const AutoContinueChainCap = 4
