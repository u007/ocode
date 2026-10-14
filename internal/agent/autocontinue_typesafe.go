package agent

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/u007/ocode/internal/tool"
)

// Typesafe auto-continue triage question keys. The verdict question picks the
// next action and decides; the reason question is advisory except for the two
// awaiting_* reasons (Jev cannot write prose, so it picks a typed category
// explaining the verdict).
const (
	typesafeAutoContinueVerdictKey = "verdict"
	typesafeAutoContinueReasonKey  = "reason"
)

// autoContinueMinConfidenceDefault is the TypeSafe confidence floor for a
// resume-class auto-continue action. It is deliberately LOWER than the shared
// permission floor (autoJudgeMinConfidenceDefault = 0.80): resuming is
// low-stakes and reversible (the chain is bounded at AutoContinueChainCap and
// one extra LLM round is cheap), whereas an auto-granted tool call is not.
// TypeSafe's own guidance is explicit that "a confidence threshold is not one
// number" and that the threshold should scale with the consequences of getting
// it wrong.
//
// Reusing the permission floor here was a real defect: Jev's `confidence` is a
// distribution-shape statistic that runs systematically below the selected
// option's `probabilities[choice]`, so legitimate mid-task cutoff verdicts
// (confidence 0.6-0.8 with p(choice) >= 0.85) were failed closed and the turn
// silently ended. 0.6 sits just above TypeSafe's "genuinely unsure" boundary
// (confidence < 0.5 => do not act) while still rejecting coin-flip verdicts.
const autoContinueMinConfidenceDefault = 0.6

// autoContinueAnswerMinConfidence is the floor for an answer-class action
// (proceed/skip/reject): the judge is answering the assistant's question ON
// THE USER'S BEHALF, which is a grant-like act, so it uses the permission
// default rather than the cheap resume floor. Deliberately the constant, not
// resolveAutoJudgeMinConfidence(): a user who raised the permission floor for
// tool calls has not said anything about auto-answers, and a lower configured
// permission floor must not loosen this one.
const autoContinueAnswerMinConfidence = autoJudgeMinConfidenceDefault

// resolveAutoContinueMinConfidence returns the confidence floor for a
// resume-class auto-continue action. Per the risk-scaling rule above it is
// intentionally decoupled from permissions.auto.min_confidence — that key
// remains the high-stakes permission/discovery floor, and overloading it here
// was what suppressed low-confidence-but-correct resumes.
func (a *Agent) resolveAutoContinueMinConfidence() float64 {
	return autoContinueMinConfidenceDefault
}

// Resume prompts shared by every host (TUI, headless server). The step-limit
// variant explicitly countermands the cutoff's "stop using tools and
// summarise" instruction, which is otherwise the most recent one in the
// transcript, or the model re-summarises instead of resuming.
const (
	AutoContinueResumePrompt    = "Continue the task from where you left off; do not just repeat any previous summary."
	AutoContinueStepLimitPrompt = "The step limit has been reset — you may use tools again. " + AutoContinueResumePrompt
)

// Typed reasons that gate the verdict. awaiting_decision is decisive: no
// action but end/await_user may fire. awaiting_confirmation admits only the
// answer-class actions (proceed/skip/reject) — a bare "continue" on a
// confirmation question would be an implicit yes the judge never stated.
const (
	typesafeAutoContinueAwaitingConfirmation = "awaiting_confirmation"
	typesafeAutoContinueAwaitingDecision     = "awaiting_decision"
)

// typesafeAutoContinueReasons is the closed set of triage outcome categories,
// in the order they are described to the model. "finished" must stay first:
// it is the expected answer for a reply that naturally completed.
var typesafeAutoContinueReasons = []typesafeConcern{
	{"finished", "the reply completed the request; nothing is missing"},
	{"mid_task", "the reply stopped mid-task and work visibly remains (it says it will continue, or the task is plainly unfinished)"},
	{"truncated", "the reply looks mechanically truncated (an unclosed code block, a cut-off sentence)"},
	{typesafeAutoContinueAwaitingConfirmation, "the reply asks the user for a yes/no go-ahead on a plan or step it has already described in full (\"shall I proceed?\", \"want me to apply this?\"), where the step is reversible and no open design choice remains"},
	{typesafeAutoContinueAwaitingDecision, "the reply is waiting on a decision only the user can make: an open choice between approaches, an ambiguous or missing requirement, a destructive or irreversible action, or any question that is not a plain go-ahead"},
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

// autoContinueActionClass says what firing an action does. The class, not the
// key, drives the gating rules and the confidence floor.
type autoContinueActionClass int

const (
	// actionResume re-prompts the assistant with a fixed instruction; the
	// chain continues.
	actionResume autoContinueActionClass = iota
	// actionAnswer answers the assistant's confirmation question on the
	// user's behalf; the chain continues. Admitted only with the
	// awaiting_confirmation reason and the higher answer floor.
	actionAnswer
	// actionEnd ends the turn.
	actionEnd
)

// autoContinueAction is one option the judge may pick. Every option is a
// closed key with a FIXED resume prompt: Jev cannot write prose, so the judge
// chooses between pre-written instructions rather than composing one.
type autoContinueAction struct {
	Key      string
	Summary  string // short user-facing phrase for the outcome line
	Criteria string // the condition under which the judge should pick it
	Prompt   string // the user-role resume prompt; "" for actionEnd
	Class    autoContinueActionClass
	// OncePerChain actions are offered at most once per auto-continue chain:
	// a judge that keeps asking for a plan, an advisor or a summary would
	// otherwise loop the same instruction until the chain cap.
	OncePerChain bool
}

// autoContinueActions is the full option set, in the order presented to the
// judge. Keys are static; never enumerate per-turn state into them.
var autoContinueActions = []autoContinueAction{
	{Key: "continue", Summary: "looks cut off", Class: actionResume,
		Criteria: "the reply was cut off mid-work or mechanically truncated and concrete work remains; the assistant should pick up exactly where it stopped",
		Prompt:   AutoContinueResumePrompt},
	{Key: "next", Summary: "item done, plan has open items", Class: actionResume,
		Criteria: "the current step is done and the todo plan still has open items; the assistant should move to the next one",
		Prompt:   "That item is done. Move to the next open item in the todo plan; do not repeat work already completed."},
	{Key: "retry", Summary: "transient failure, retrying once", Class: actionResume, OncePerChain: true,
		Criteria: "the reply stopped on a transient failure (network error, rate limit, flaky test, timeout) that is likely to succeed on a second attempt",
		Prompt:   "Retry the step that failed once. If it fails again, stop and report the failure instead of retrying further."},
	{Key: "fix_errors", Summary: "build or tests failing", Class: actionResume,
		Criteria: "the latest output shows a failing build, failing tests or compile errors and the reply stopped without fixing them",
		Prompt:   "The build or tests are failing. Fix the failures before doing anything else, then re-run them and show the output."},
	{Key: "verify", Summary: "done but unverified", Class: actionResume, OncePerChain: true,
		Criteria: "the reply claims the work is done but shows no test, build or runtime verification of it",
		Prompt:   "Before finishing, verify the change: run the relevant tests or build and show the output."},
	{Key: "review_first", Summary: "large diff, self-review first", Class: actionResume, OncePerChain: true,
		Criteria: "the reply wrote a large or risky diff and declared it done without reviewing it for regressions or drift from the request",
		Prompt:   "Review your own diff before finishing: check it for regressions, missed call sites and drift from the original request, then report what you found."},
	{Key: "write_plan", Summary: "no plan for a large task", Class: actionResume, OncePerChain: true,
		Criteria: "the task is large or multi-step, there is no todo plan, and the reply is wandering or jumped straight into edits",
		Prompt:   "Stop and write a todo plan with todowrite that covers the whole task, then start on the first item."},
	{Key: "ask_advisor", Summary: "stuck, consulting the advisor", Class: actionResume, OncePerChain: true,
		Criteria: "the reply is torn between approaches, stuck, or unsure how to proceed and an advisor is available",
		Prompt:   "Consult the advisor tool with your exact question and the options you are weighing, then act on its answer."},
	{Key: "summarize", Summary: "done but unsummarised", Class: actionResume, OncePerChain: true,
		Criteria: "the work is done but the reply never summarised what changed and what remains",
		Prompt:   "Give a final summary: what changed, what was verified, and anything left undone. Then stop."},
	{Key: "proceed", Summary: "answering yes on the user's behalf", Class: actionAnswer,
		Criteria: "the reply asks for a yes/no go-ahead on a plan it already described, the plan matches the user's request and the step is reversible",
		Prompt:   "Yes, proceed as described."},
	{Key: "skip", Summary: "skipping the item on the user's behalf", Class: actionAnswer,
		Criteria: "the reply asks whether to do an optional or blocked sub-item and the rest of the task does not depend on it",
		Prompt:   "Skip that item, record it in TODO.md, and continue with the rest of the task."},
	{Key: "reject", Summary: "rejecting the proposal on the user's behalf", Class: actionAnswer,
		Criteria: "the reply asks for a go-ahead on something that is off-spec, outside the user's request, or destructive",
		Prompt:   "No. Do not do that. Stay within the original request and propose a safer alternative."},
	{Key: "end", Summary: "reply finished", Class: actionEnd,
		Criteria: "the reply completed the request and ends cleanly, including short acknowledgements and status reports; or the reply reports a blocking error"},
	{Key: "await_user", Summary: "reply is waiting on the user", Class: actionEnd,
		Criteria: "the reply is waiting on a decision only the user can make (an open choice, an ambiguous requirement, a destructive or irreversible action); the next move belongs to the user"},
}

func autoContinueActionByKey(key string) (autoContinueAction, bool) {
	for _, act := range autoContinueActions {
		if act.Key == key {
			return act, true
		}
	}
	return autoContinueAction{}, false
}

// AutoContinueActionKeys lists every action key, in presentation order. Hosts
// use it only for display; the judge is offered the filtered set.
func AutoContinueActionKeys() []string {
	keys := make([]string, 0, len(autoContinueActions))
	for _, act := range autoContinueActions {
		keys = append(keys, act.Key)
	}
	return keys
}

// autoContinueOffered filters the option set for one judge call: once-per-chain
// actions already fired in this chain are dropped, ask_advisor needs a live
// advisor, next needs an open todo item, and the chat judge (allowAnswers=false)
// never sees the answer class because it has no typed reason to gate it with.
func (a *Agent) autoContinueOffered(prior []string, allowAnswers bool, todoOpen bool) []autoContinueAction {
	fired := make(map[string]bool, len(prior))
	for _, k := range prior {
		fired[k] = true
	}
	advisor := a.advisorAvailable()
	out := make([]autoContinueAction, 0, len(autoContinueActions))
	for _, act := range autoContinueActions {
		switch {
		case act.OncePerChain && fired[act.Key]:
			continue
		case act.Key == "ask_advisor" && !advisor:
			continue
		case act.Key == "next" && !todoOpen:
			continue
		case act.Class == actionAnswer && !allowAnswers:
			continue
		}
		out = append(out, act)
	}
	return out
}

// autoContinueTodoOpen reports whether THIS agent's session plan has an open
// item, which is the gate for offering `next`. It reads by the agent's own
// todoScope (project root + session id), the same address the todo tools and
// the re-anchor use, never a process-global current session: that global is
// unset on a multi-session server (so `next` was never offered there) and in
// the TUI it named whichever session was current rather than the one being
// judged. An unreadable file reads as no open item, so a corrupt plan never
// makes the judge resume into it.
func (a *Agent) autoContinueTodoOpen() bool {
	return tool.ReadTodoSnapshot(a.todoScope()).HasOpenItems
}

// advisorAvailable reports whether a resumed turn could actually call the
// advisor tool: the runtime toggle is on and the tool is installed.
func (a *Agent) advisorAvailable() bool {
	if !a.AdvisorEnabled() {
		return false
	}
	a.toolsMu.RLock()
	defer a.toolsMu.RUnlock()
	_, ok := a.tools[advisorAgentName]
	return ok
}

// typesafeAutoContinueInstructions is the triage rubric sent as the verdict
// question's instructions. The transcript travels as structured state; this
// text only explains how to judge it.
const typesafeAutoContinueInstructions = `You are triaging how an AI coding assistant's most recent reply ended and choosing the single next action for the harness. The assistant will not receive new human input unless you choose an action that ends the turn.
Rules:
- Judge the last assistant reply in the transcript; earlier turns are context.
- Pick a resume action only when there is concrete remaining work the reply itself acknowledges or leaves visible, or a concrete gap (failing tests, no verification, no plan) the action names. Each action's criteria say exactly when it applies.
- Choose "end" when the reply reads as a natural completion: it answered the request, delivered the result, and ends cleanly — including short acknowledgements and status reports — or when it reports a blocking error that continuing would not help.
- A reply that ends by asking the user something is NOT a cut-off mid-task reply. Classify the question: a plain yes/no go-ahead on a plan the reply has already described in full is a confirmation, and only "proceed", "skip" or "reject" may answer it. Any other question — an open choice between approaches, a missing or ambiguous requirement, a destructive or irreversible action — is a decision for the user: choose "await_user".
- Never choose "proceed" when the described step deletes data, rewrites git history, force-pushes, deploys, or otherwise cannot be undone.
- The state lists actions already fired in this chain; do not ask for the same plan, review, advisor or retry twice.
- When the state says the turn was ended by the step limit, the reply is a forced summary of unfinished work: choose "continue".
- When the state says the turn failed with an error, choose "end": the caller surfaces the error instead.
Choose a resume or answer action only when the evidence is unambiguous; otherwise choose "end" so the turn finishes.`

// AutoContinueDecision is one triage outcome. Resume=false on any error,
// veto or ambiguous answer (fail closed). Prompt is the fixed user-role
// message the host must send next when Resume is true. Action is the key the
// judge chose ("" when it returned no usable verdict). Detail is the
// user-facing one-liner naming the judge, the action and why.
type AutoContinueDecision struct {
	Resume bool
	Action string
	Prompt string
	Detail string
}

// runAutoContinueJudgeTypesafe consults the TypeSafe System One model (Jev)
// for one triage decision: how did the most recent assistant reply end, and
// which fixed next action (if any) should the harness fire? Unlike the chat
// judge it receives the conversation tail as structured state and answers
// typed choices — TypesafeClient.Chat always fails with
// ErrTypesafeDecisionOnly, so a typesafe AutoContinueModel cannot go through
// runAutoContinueJudge.
//
// prior lists the action keys already fired in this auto-continue chain so
// once-per-chain actions are not offered again.
//
// Gating, in order: an action the judge was not offered fails closed; an
// awaiting_decision reason vetoes every non-end action; an
// awaiting_confirmation reason admits only the answer class; an answer-class
// action without an awaiting_confirmation reason is vetoed (nobody asked);
// "proceed" is vetoed when the reply's own commands are harmful; then the
// class floor applies (answer actions use the permission default).
//
// Precondition: the caller owns the hard /max-step signal. Every production
// dispatcher (server autoContinueShouldResume, the TUI's shouldAutoContinue
// guards) checks StepLimitHit first and resumes deterministically without a
// triage call, so this function is reached only for naturally-ended turns.
// It deliberately has no step-limit short-circuit: a caller that skipped that
// guard would otherwise be handed a resume it might mistake for a judge
// verdict (and fire the wrong resume prompt). Instead the state reports the
// real end reason (buildTypesafeAutoContinueState) and Jev decides.
func (a *Agent) runAutoContinueJudgeTypesafe(client Decider, messages []Message, stepErr error, prior []string) (AutoContinueDecision, error) {
	todoOpen := a.autoContinueTodoOpen()
	offered := a.autoContinueOffered(prior, true, todoOpen)
	state := a.buildTypesafeAutoContinueState(messages, stepErr, prior, todoOpen)
	reasonCriteria := make(map[string]string, len(typesafeAutoContinueReasons))
	for _, r := range typesafeAutoContinueReasons {
		reasonCriteria[r.Key] = r.Label
	}
	verdictCriteria := make(map[string]string, len(offered))
	for _, act := range offered {
		verdictCriteria[act.Key] = act.Criteria
	}
	questions := map[string]TypesafeQuestion{
		typesafeAutoContinueVerdictKey: {
			Type:         "choice",
			Instructions: typesafeAutoContinueInstructions,
			Criteria:     verdictCriteria,
		},
		typesafeAutoContinueReasonKey: {
			Type:         "choice",
			Instructions: typesafeAutoContinueInstructions + "\nName the single best description of how the last reply ended, or \"finished\" if it completed the request.",
			Criteria:     reasonCriteria,
		},
	}

	model := deciderLabel(client)
	resp, err := client.Decide(state, questions)
	if err != nil {
		return AutoContinueDecision{Detail: model + " triage failed: " + err.Error()}, err
	}
	a.RecordSideUsage(resp.Usage.InputTokens, resp.Usage.OutputTokens, 0, 0, model)

	ans, ok := resp.Answers[typesafeAutoContinueVerdictKey]
	if !ok || ans.Type != "choice" {
		return AutoContinueDecision{Detail: fmt.Sprintf("%s triage returned no verdict (answers=%d)", model, len(resp.Answers))}, nil
	}

	reasonKey := ""
	if r, ok := resp.Answers[typesafeAutoContinueReasonKey]; ok && r.Type == "choice" {
		reasonKey = r.Choice
	}
	reason := ""
	if reasonKey != "" {
		reason = "; reason: " + typesafeAutoContinueReasonLabel(reasonKey)
	}
	a.emitDebug("AGENT", fmt.Sprintf("autocontinue_typesafe verdict=%s confidence=%.3f reason=%s prior=%v", ans.Choice, ans.Confidence, reasonKey, prior))

	var act autoContinueAction
	found := false
	for _, o := range offered {
		if o.Key == ans.Choice {
			act, found = o, true
			break
		}
	}
	if !found {
		return AutoContinueDecision{Detail: fmt.Sprintf("%s returned unknown choice %q", model, ans.Choice)}, nil
	}
	dec := AutoContinueDecision{Action: act.Key}

	if act.Class == actionEnd {
		dec.Detail = fmt.Sprintf("%s: %s%s (%.0f%% confidence)", model, act.Summary, reason, ans.Confidence*100)
		return dec, nil
	}
	// Awaiting the user: a decision is decisive, a confirmation admits only
	// an explicit typed answer. Jev sometimes reads a question ending
	// ("Which option do you want?") as unfinished work; a bare resume would
	// answer the user's question on their behalf without ever saying so.
	switch {
	case reasonKey == typesafeAutoContinueAwaitingDecision:
		dec.Detail = fmt.Sprintf("%s says %s but the reply is waiting on the user for a decision only they can make — not resuming", model, act.Key)
		return dec, nil
	case reasonKey == typesafeAutoContinueAwaitingConfirmation && act.Class != actionAnswer:
		dec.Detail = fmt.Sprintf("%s says %s but the reply is waiting on confirmation and no explicit answer was chosen — not resuming", model, act.Key)
		return dec, nil
	case act.Class == actionAnswer && reasonKey != typesafeAutoContinueAwaitingConfirmation:
		dec.Detail = fmt.Sprintf("%s chose %s but the reply is not asking for confirmation%s — not resuming", model, act.Key, reason)
		return dec, nil
	case act.Key == "proceed" && replyProposesHarmfulCommand(messages):
		dec.Detail = fmt.Sprintf("%s chose proceed but the reply proposes a destructive command — leaving the decision to the user", model)
		return dec, nil
	}
	floor := a.resolveAutoContinueMinConfidence()
	if act.Class == actionAnswer {
		floor = autoContinueAnswerMinConfidence
	}
	if ans.Confidence < floor {
		dec.Detail = fmt.Sprintf("%s leaned %s but confidence %.2f is below the %.2f floor%s", model, act.Key, ans.Confidence, floor, reason)
		return dec, nil
	}
	dec.Resume = true
	dec.Prompt = act.Prompt
	dec.Detail = fmt.Sprintf("%s: %s, %s (%.0f%% confidence)%s — resuming", model, act.Key, act.Summary, ans.Confidence*100, reason)
	return dec, nil
}

// replyProposesHarmfulCommand reports whether the most recent assistant reply
// quotes a shell command the permission layer classifies as harmful (git
// history rewrites, force pushes, destructive rm, …). It scans fenced code
// blocks and inline backtick spans line by line with IsHarmfulBashCommand, so
// "proceed" can never auto-approve a plan whose own commands would have hit a
// permission Ask.
func replyProposesHarmfulCommand(messages []Message) bool {
	var reply string
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" && strings.TrimSpace(messages[i].Content) != "" {
			reply = messages[i].Content
			break
		}
	}
	if reply == "" {
		return false
	}
	for _, cand := range quotedCommandCandidates(reply) {
		if IsHarmfulBashCommand(cand) {
			return true
		}
	}
	return false
}

// quotedCommandCandidates returns every line inside ``` fences plus every
// inline `span`, with a leading "$ " prompt stripped.
func quotedCommandCandidates(text string) []string {
	var out []string
	inFence := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			out = append(out, strings.TrimPrefix(trimmed, "$ "))
			continue
		}
		for {
			start := strings.IndexByte(trimmed, '`')
			if start < 0 {
				break
			}
			rest := trimmed[start+1:]
			end := strings.IndexByte(rest, '`')
			if end < 0 {
				break
			}
			out = append(out, strings.TrimPrefix(strings.TrimSpace(rest[:end]), "$ "))
			trimmed = rest[end+1:]
		}
	}
	return out
}

// buildTypesafeAutoContinueState assembles the structured triage request: the
// conversation tail (roles + bounded content), how the turn ended so Jev does
// not have to infer it, the actions already fired in this chain, and whether
// the todo plan has open items. stepErr is the Step error that ended the turn
// (nil on a clean stop). The tail mirrors runAutoContinueJudge's budget — the
// last 6 non-empty messages, each trimmed, so one huge tool result cannot blow
// up the request.
//
// In production stepErr is always nil and StepLimitHit is always false: both
// dispatchers short-circuit the hard signals before calling (runTurn returns on
// a Step error; the TUI and server resume deterministically on StepLimitHit).
// The error/step-limit branches are the fallback for a direct caller, keeping
// the rubric honest if one is ever handed a cut-off or failed turn.
func (a *Agent) buildTypesafeAutoContinueState(messages []Message, stepErr error, prior []string, todoOpen bool) map[string]any {
	if prior == nil {
		prior = []string{}
	}
	state := map[string]any{
		"transcript_tail":     typesafeTranscriptTail(messages, 6),
		"turn_ended":          a.typesafeTurnEnded(stepErr),
		"chain_actions_fired": prior,
		"todo_has_open_items": todoOpen,
		"advisor_available":   a.advisorAvailable(),
	}
	if a.config != nil {
		state["auto_continue_model"] = a.config.Ocode.AutoContinueModel
	}
	return state
}

// typesafeTranscriptTail is the bounded conversation tail sent to a TypeSafe
// judge: the last n messages with empty ones dropped and each trimmed to 4000
// characters, so one huge tool result cannot blow up the request.
func typesafeTranscriptTail(messages []Message, n int) []map[string]any {
	tail := messages
	if len(tail) > n {
		tail = tail[len(tail)-n:]
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
	return entries
}

// typesafeTurnEnded describes how a turn ended, so a TypeSafe judge does not have
// to infer it. stepErr is the Step error that ended the turn (nil on a clean
// stop). Production callers reach the auto-continue judge only for a
// naturally-ended turn, so in practice this is "finished within the step
// budget"; the other two cases keep a direct caller from describing a cut-off or
// failed turn as a natural finish.
func (a *Agent) typesafeTurnEnded(stepErr error) string {
	switch {
	case stepErr != nil:
		return "the turn failed with an error: " + stepErr.Error()
	case a.StepLimitHit():
		return "the turn was cut off by the step limit (max steps reached); the reply is a forced summary of unfinished work"
	default:
		return "the reply finished within the step budget"
	}
}

// AutoContinueJudgeSync is the server-turn variant of AutoContinueJudgeAsync:
// headless turns run inside a synchronous HTTP goroutine with no event loop
// to receive OnAutoContinueJudge, so the triage runs inline and returns the
// decision directly. Same fail-closed contract: no judge configured → zero
// decision, nil error; any judge error → Resume=false with the failure in
// Detail and the error returned.
//
// prior lists the action keys already fired in this chain (once-per-chain
// filtering). stepErr lets a direct caller describe a failed turn to the
// typesafe judge. Production dispatchers pass nil — runTurn returns on a Step
// error before reaching auto-continue, and the step-limit case is resumed
// deterministically before the judge is consulted — so the parameter is a
// safety net, not a signal the current hosts send.
func (a *Agent) AutoContinueJudgeSync(messages []Message, stepErr error, prior []string) (AutoContinueDecision, error) {
	client, isTypesafe := a.autoContinueJudgeClientTyped()
	if client == nil {
		return AutoContinueDecision{}, nil
	}
	if isTypesafe {
		d, ok := client.(Decider)
		if !ok {
			return AutoContinueDecision{Detail: "auto-continue judge client is not a decision backend"}, nil
		}
		return a.runAutoContinueJudgeTypesafe(d, messages, stepErr, prior)
	}
	return a.runAutoContinueJudge(client, messages, prior)
}

// AutoContinueEnabled reports whether the auto-continue feature is switched
// on for this agent's config. Server turns consult it before spending a
// triage call; the TUI tracks its own toggle state.
func (a *Agent) AutoContinueEnabled() bool {
	return a.config != nil && a.config.Ocode.AutoContinueEnabled
}

// AutoContinueChainCap is the single bound on consecutive auto-fired resumes
// shared by every host (TUI, headless server, scheduler); the TUI aliases it and
// the server's autoContinueChainCap wraps it.
const AutoContinueChainCap = 4
