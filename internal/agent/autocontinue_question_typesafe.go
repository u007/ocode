package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/crashguard"
	"github.com/u007/ocode/internal/tool"
)

// AutoContinueAnswerQuestionAction is the chain-history key a host records when
// this judge answers a question on the user's behalf. It is not a key of
// autoContinueActions: this judge never resumes with a fixed prompt, it answers
// the question the assistant asked through the question tool.
const AutoContinueAnswerQuestionAction = "answer_question"

// autoContinueQuestionMaxPrompts caps the prompts in one question tool call.
// Each prompt costs Jev one owner question plus its answer questions, so the cap
// bounds the request size.
const autoContinueQuestionMaxPrompts = 3

// autoContinueQuestionMaxOptions caps the answerable options of one prompt. A
// wider menu is a choice for the user, not a fact the transcript settles.
const autoContinueQuestionMaxOptions = 6

// autoContinueQuestionMaxPerChain bounds the answers one auto-continue chain may
// give. Each answer acts for the user, so a chain never gets an unbounded run.
const autoContinueQuestionMaxPerChain = 2

// autoContinueQuestionMinProbability is the single-select floor on
// probabilities[choice]. It is its own constant, not the permission floor,
// because the answer acts for the user rather than granting a tool call.
const autoContinueQuestionMinProbability = 0.80

// autoContinueQuestionOwnerMinProbability is the floor on
// probabilities["determined_by_transcript"] for the owner question. The judge
// may answer only when it is confident the user already settled the matter.
const autoContinueQuestionOwnerMinProbability = 0.80

// autoContinueQuestionSelectNoul is the multi-select noul at or above which an
// option counts as selected. Its own constant because a noul is a different
// statistic from the single-select probability.
const autoContinueQuestionSelectNoul = 0.85

// autoContinueQuestionRejectNoul is the multi-select noul at or below which an
// option counts as clearly not selected. Anything between the reject and select
// thresholds defers the whole prompt.
const autoContinueQuestionRejectNoul = 0.15

// autoContinueQuestionDeferNoul is the multi-select defer noul at or above which
// the question is sent to the user regardless of the per-option answers.
const autoContinueQuestionDeferNoul = 0.50

// typesafeQuestionDeferChoice is the reserved single-select choice meaning "no
// option is determined; the user must choose". A label may not equal it.
const typesafeQuestionDeferChoice = "defer_to_user"

// typesafeQuestionOwnerDetermined and typesafeQuestionOwnerPreference are the
// owner question's two answers: the transcript settles the question, or the
// question is a preference only the user can state.
const (
	typesafeQuestionOwnerDetermined = "determined_by_transcript"
	typesafeQuestionOwnerPreference = "user_preference"
)

// autoContinueQuestionRiskWords are whole words that mark a question as the
// user's decision even when the transcript looks settled. They are matched
// against the question, header, labels and descriptions.
var autoContinueQuestionRiskWords = []string{
	"delete", "drop", "force", "deploy", "production", "prod", "secret", "password",
	"token", "overwrite", "wipe", "purge", "reset", "rm", "payment", "pay",
	"purchase", "billing", "publish", "release",
}

// typesafeAutoContinueQuestionInstructions is the shared rubric for every
// question this judge sends. It is wrapped with config.ApplyPromptOverride.
const typesafeAutoContinueQuestionInstructions = `An AI coding assistant paused on a question with a fixed set of options and asked the user. You answer that question ON THE USER'S BEHALF, so you may choose an option only when the transcript already determines the answer.
Rules:
- original_request and the earlier user messages are the user's own words. An option is determined only when they already state it, or state a preference that leaves exactly one option.
- The assistant's own question is not the user's decision. A preference the user never stated is NOT determined: choose user_preference for the owner question, or defer_to_user for the answer. This covers taste, scope, cost, naming, architecture and any judgment the user owns.
- Anything destructive, irreversible, costly or production-facing is never determined by a vague request. Defer it.
- Never invent an answer the options do not offer. The only answers are the listed options.
- chain_actions_fired lists what already fired in this chain.
- When in doubt, defer. A wrong answer acts for the user without their knowledge, while a deferred question costs one message.`

// AutoContinueQuestionDecision is one question-judge outcome. Answered=false on
// any veto, defer, error or invalid answer (fail closed), and then Answers is
// nil. Answers holds one set per prompt, in prompt order, with Label only.
// Detail is the user-facing one-liner naming the judge, the outcome and why.
type AutoContinueQuestionDecision struct {
	Answered bool
	Answers  []tool.QuestionAnswerSet
	Detail   string
}

// AutoContinueQuestionJudgeResult is delivered to OnAutoContinueQuestionJudge
// when an async question-judge call finishes. ToolCallID names the question
// tool call the result answers, so a host can match it to the pending ask.
type AutoContinueQuestionJudgeResult struct {
	ToolCallID string
	Answered   bool
	Answers    []tool.QuestionAnswerSet
	Detail     string
	Err        error
}

// questionChoice is one answerable option. key is an identifier ("opt0",
// "opt1", ... in prompt order) that Jev uses as a criteria key. Labels are free
// text, so they never become keys; label is the original text the answer
// carries back to the question tool and reaches Jev through the criteria
// description.
type questionChoice struct {
	key         string
	label       string
	description string
}

// questionAnswerableChoices returns the non-custom options of a prompt in prompt
// order, each with its identifier key. Custom options are never offered to Jev.
func questionAnswerableChoices(p tool.QuestionPrompt) []questionChoice {
	out := make([]questionChoice, 0, len(p.Options))
	for _, o := range p.Options {
		if tool.QuestionOptionIsCustom(o.Label) {
			continue
		}
		out = append(out, questionChoice{
			key:         fmt.Sprintf("opt%d", len(out)),
			label:       strings.TrimSpace(o.Label),
			description: strings.TrimSpace(o.Description),
		})
	}
	return out
}

// questionChoiceLabel maps an identifier key back to the option's original label.
func questionChoiceLabel(choices []questionChoice, key string) (string, bool) {
	for _, c := range choices {
		if c.key == key {
			return c.label, true
		}
	}
	return "", false
}

// questionRiskWord returns the first risk word that appears as a whole word in
// text, or "" when none does. Words are split on anything that is not a letter
// or digit, so "prod" does not match "product" and "force-push" matches "force".
func questionRiskWord(text string) string {
	tokens := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for _, tok := range tokens {
		for _, w := range autoContinueQuestionRiskWords {
			if tok == w {
				return w
			}
		}
	}
	return ""
}

// questionAlreadyAsked reports whether an earlier question tool result in messages
// is an answer to a question that is asked again now. Only tool rows answering a
// "question" tool call count, so a different tool that happens to return JSON
// with a question field is never a prior answer. The last message is skipped: it
// is the pending ask itself.
func questionAlreadyAsked(messages []Message, prompts []tool.QuestionPrompt) (string, bool) {
	asked := make(map[string]bool, len(prompts))
	for _, p := range prompts {
		asked[strings.ToLower(strings.TrimSpace(p.Question))] = true
	}
	questionCalls := make(map[string]bool)
	for _, m := range messages {
		if m.Role != "assistant" {
			continue
		}
		for _, tc := range m.ToolCalls {
			if tc.Function.Name == "question" {
				questionCalls[tc.ID] = true
			}
		}
	}
	for i := 0; i < len(messages)-1; i++ {
		if messages[i].Role != "tool" || !questionCalls[messages[i].ToolID] {
			continue
		}
		var sets []tool.QuestionAnswerSet
		if err := json.Unmarshal([]byte(messages[i].Content), &sets); err != nil {
			// intentionally not logged: an unanswered question row holds a sentinel rather than JSON, so a decode failure is the expected case
			continue
		}
		for _, s := range sets {
			q := strings.ToLower(strings.TrimSpace(s.Question))
			if q != "" && asked[q] {
				return s.Question, true
			}
		}
	}
	return "", false
}

// autoContinueQuestionVeto runs the pre-call gates in order. It returns the
// reason the judge must not be asked ("" when it may be asked) and the
// answerable options of each prompt. A veto makes no Decide call.
func autoContinueQuestionVeto(messages []Message, prompts []tool.QuestionPrompt, prior []string) (string, [][]questionChoice) {
	if len(prompts) == 0 {
		return "the question has no prompts", nil
	}
	if len(prompts) > autoContinueQuestionMaxPrompts {
		return fmt.Sprintf("%d prompts exceed the %d-prompt cap", len(prompts), autoContinueQuestionMaxPrompts), nil
	}
	used := 0
	for _, key := range prior {
		if key == AutoContinueAnswerQuestionAction {
			used++
		}
	}
	if used >= autoContinueQuestionMaxPerChain {
		return fmt.Sprintf("this chain already answered %d questions (cap %d)", used, autoContinueQuestionMaxPerChain), nil
	}

	choices := make([][]questionChoice, len(prompts))
	for i, p := range prompts {
		choices[i] = questionAnswerableChoices(p)
		n := len(choices[i])
		if n == 0 || n > autoContinueQuestionMaxOptions {
			return fmt.Sprintf("prompt %d has %d answerable options, want 1 to %d", i, n, autoContinueQuestionMaxOptions), nil
		}
		seen := make(map[string]bool, n)
		for _, c := range choices[i] {
			folded := strings.ToLower(c.label)
			if folded == "" {
				return fmt.Sprintf("prompt %d has an empty option label", i), nil
			}
			if folded == typesafeQuestionDeferChoice {
				return fmt.Sprintf("prompt %d uses the reserved label %s", i, typesafeQuestionDeferChoice), nil
			}
			if seen[folded] {
				return fmt.Sprintf("prompt %d repeats the option label %q", i, c.label), nil
			}
			seen[folded] = true
		}
	}

	if replyProposesHarmfulCommand(messages) {
		return "the reply proposes a destructive command", nil
	}
	for i, p := range prompts {
		texts := []string{p.Header, p.Question}
		for _, o := range p.Options {
			texts = append(texts, o.Label, o.Description)
		}
		for _, text := range texts {
			if w := questionRiskWord(text); w != "" {
				return fmt.Sprintf("prompt %d mentions %q, a decision the user owns", i, w), nil
			}
		}
		for _, cand := range quotedCommandCandidates(p.Question) {
			if IsHarmfulBashCommand(cand) {
				return fmt.Sprintf("prompt %d quotes a harmful command", i), nil
			}
		}
		for _, o := range p.Options {
			for _, cand := range quotedCommandCandidates(o.Description) {
				if IsHarmfulBashCommand(cand) {
					return fmt.Sprintf("prompt %d option %q quotes a harmful command", i, o.Label), nil
				}
			}
		}
	}

	if q, ok := questionAlreadyAsked(messages, prompts); ok {
		return fmt.Sprintf("the assistant already asked %q once; a repeat is a decision for the user", q), nil
	}
	return "", choices
}

// questionPromptContext renders one prompt for a Jev question's instructions:
// its header, question text and answerable options.
func questionPromptContext(i int, p tool.QuestionPrompt, choices []questionChoice) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\nPrompt %d. Header: %q. Question: %q.\nOptions:", i, p.Header, p.Question)
	for _, c := range choices {
		fmt.Fprintf(&b, "\n- %s: %s", c.label, c.description)
	}
	return b.String()
}

// typesafeAutoContinueQuestionNoulLine opens each multi-select option question.
// It replaces the full rubric, which is sent only on the choice questions.
const typesafeAutoContinueQuestionNoulLine = "You answer on the user's behalf: say yes only when original_request or an earlier user message already determines this option."

// typesafeAutoContinueQuestionDeferLine opens the multi-select defer question. Its
// yes means the user is needed, so the polarity is the reverse of the option line.
const typesafeAutoContinueQuestionDeferLine = "You answer on the user's behalf: say yes only when original_request and the earlier user messages leave part of the selection undetermined."

// questionNoulContext is the prompt context for a multi-select noul question:
// index, header and question only. The option list is not repeated per option.
func questionNoulContext(i int, p tool.QuestionPrompt) string {
	return fmt.Sprintf("\nPrompt %d. Header: %q. Question: %q.", i, p.Header, p.Question)
}

// questionJudgeQuestions builds the typed questions sent to Jev. The map keys
// are for code only; each question's Instructions carry the full meaning.
func questionJudgeQuestions(rubric string, prompts []tool.QuestionPrompt, choices [][]questionChoice) map[string]TypesafeQuestion {
	out := make(map[string]TypesafeQuestion)
	for i, p := range prompts {
		ctx := questionPromptContext(i, p, choices[i])
		out[fmt.Sprintf("q%d_owner", i)] = TypesafeQuestion{
			Type:         "choice",
			Instructions: rubric + ctx + "\nSay whether the transcript already settles this question (determined_by_transcript), or whether the user has not stated the preference (user_preference).",
			Criteria: map[string]string{
				typesafeQuestionOwnerDetermined: "the original request or an earlier user message already fixes this answer; the assistant is asking something the user has effectively already said",
				typesafeQuestionOwnerPreference: "the user has not stated this preference; the answer depends on taste, scope, cost, naming, architecture or any judgment the user owns",
			},
		}
		if !p.Multiple {
			criteria := make(map[string]string, len(choices[i])+1)
			for _, c := range choices[i] {
				text := c.label
				if c.description != "" {
					text += ": " + c.description
				}
				criteria[c.key] = text
			}
			criteria[typesafeQuestionDeferChoice] = "no option is determined by the transcript; the user must choose"
			out[fmt.Sprintf("q%d_answer", i)] = TypesafeQuestion{
				Type:         "choice",
				Instructions: rubric + ctx + "\nChoose the one option the transcript determines, or defer_to_user.",
				Criteria:     criteria,
			}
			continue
		}
		// The noul questions repeat per option, so they carry one line of
		// rubric instead of the full one. The choice questions above keep it.
		noulCtx := questionNoulContext(i, p)
		for j, c := range choices[i] {
			out[fmt.Sprintf("q%d_opt%d", i, j)] = TypesafeQuestion{
				Type:         "noul",
				Instructions: typesafeAutoContinueQuestionNoulLine + noulCtx + fmt.Sprintf("\nOption: %s (%s). Should it be selected?", c.label, c.description),
			}
		}
		out[fmt.Sprintf("q%d_defer", i)] = TypesafeQuestion{
			Type:         "noul",
			Instructions: typesafeAutoContinueQuestionDeferLine + noulCtx + "\nDoes any part of the selection need the user?",
		}
	}
	return out
}

// questionJudgeState builds the structured request: the selected transcript, the
// original request, the chain history and the answerable questions. kept is the
// transcript the verdict sees, after selection and fitting.
func questionJudgeState(kept []Message, prompts []tool.QuestionPrompt, choices [][]questionChoice, prior []string) map[string]any {
	state := map[string]any{
		"transcript_tail":     typesafeTranscriptEntries(kept),
		"chain_actions_fired": prior,
	}
	entries := make([]map[string]any, 0, len(prompts))
	for i, p := range prompts {
		opts := make([]map[string]any, 0, len(choices[i]))
		for _, c := range choices[i] {
			opts = append(opts, map[string]any{"label": c.label, "description": c.description})
		}
		entries = append(entries, map[string]any{
			"index":    i,
			"header":   p.Header,
			"question": p.Question,
			"multiple": p.Multiple,
			"options":  opts,
		})
	}
	state["questions"] = entries
	if origIdx := typesafeOriginalRequestIndex(kept); origIdx >= 0 {
		state["original_request"] = typesafeCapContent(strings.TrimSpace(kept[origIdx].Content))
	}
	return state
}

// validQuestionAnswer fetches one answer and validates it against the question
// that was sent. A missing or invalid answer returns a defer reason.
func validQuestionAnswer(key string, questions map[string]TypesafeQuestion, resp *TypesafeResponse) (TypesafeAnswer, string) {
	ans, ok := resp.Answers[key]
	if !ok {
		return TypesafeAnswer{}, "no answer for " + key
	}
	if err := validateAnswer(questions[key], ans); err != nil {
		return TypesafeAnswer{}, fmt.Sprintf("answer %s rejected: %v", key, err)
	}
	return ans, ""
}

// questionAnswerFromResponse turns Jev's answers for one prompt into the answer
// set the question tool would receive. A non-empty reason means the prompt
// defers to the user and carries no answer.
func questionAnswerFromResponse(i int, p tool.QuestionPrompt, choices []questionChoice, questions map[string]TypesafeQuestion, resp *TypesafeResponse) (tool.QuestionAnswerSet, string) {
	set := tool.QuestionAnswerSet{Header: p.Header, Question: p.Question}

	owner, reason := validQuestionAnswer(fmt.Sprintf("q%d_owner", i), questions, resp)
	if reason != "" {
		return set, reason
	}
	if owner.Choice != typesafeQuestionOwnerDetermined {
		return set, "the transcript does not settle it (the judge chose " + owner.Choice + ")"
	}
	if pr := owner.Probabilities[typesafeQuestionOwnerDetermined]; pr < autoContinueQuestionOwnerMinProbability {
		return set, fmt.Sprintf("owner probability %.2f is below the %.2f floor", pr, autoContinueQuestionOwnerMinProbability)
	}

	if !p.Multiple {
		ans, reason := validQuestionAnswer(fmt.Sprintf("q%d_answer", i), questions, resp)
		if reason != "" {
			return set, reason
		}
		if ans.Choice == typesafeQuestionDeferChoice {
			return set, "the judge chose " + typesafeQuestionDeferChoice
		}
		if pr := ans.Probabilities[ans.Choice]; pr < autoContinueQuestionMinProbability {
			return set, fmt.Sprintf("probability %.2f for %q is below the %.2f floor", pr, ans.Choice, autoContinueQuestionMinProbability)
		}
		label, ok := questionChoiceLabel(choices, ans.Choice)
		if !ok {
			return set, fmt.Sprintf("%q is not an offered option", ans.Choice)
		}
		set.Answers = []tool.QuestionAnswer{{Label: label}}
		return set, ""
	}

	deferAns, reason := validQuestionAnswer(fmt.Sprintf("q%d_defer", i), questions, resp)
	if reason != "" {
		return set, reason
	}
	if deferAns.Noul >= autoContinueQuestionDeferNoul {
		return set, fmt.Sprintf("the question needs the user (defer %.2f)", deferAns.Noul)
	}
	var picked []tool.QuestionAnswer
	for j, c := range choices {
		opt, reason := validQuestionAnswer(fmt.Sprintf("q%d_opt%d", i, j), questions, resp)
		if reason != "" {
			return set, reason
		}
		switch {
		case opt.Noul >= autoContinueQuestionSelectNoul:
			picked = append(picked, tool.QuestionAnswer{Label: c.label})
		case opt.Noul <= autoContinueQuestionRejectNoul:
			// clearly not selected
		default:
			return set, fmt.Sprintf("option %q is undetermined (noul %.2f)", c.label, opt.Noul)
		}
	}
	if len(picked) == 0 {
		return set, "no option is determined by the transcript"
	}
	set.Answers = picked
	return set, ""
}

// runAutoContinueQuestionJudgeTypesafe asks Jev whether the transcript already
// determines the answer to a structured question the assistant asked. It answers
// ON THE USER'S BEHALF, so every gate fails closed: any veto, defer, error or
// invalid answer returns Answered=false, and a single deferred prompt defers the
// whole question (no partial answers).
//
// prior lists the action keys already recorded in this chain, so the
// per-chain cap can be enforced. The chat judge is never used here: a prose
// answer cannot carry the typed probabilities this decision needs.
func (a *Agent) runAutoContinueQuestionJudgeTypesafe(client Decider, messages []Message, prompts []tool.QuestionPrompt, prior []string) (AutoContinueQuestionDecision, error) {
	if prior == nil {
		prior = []string{}
	}
	veto, choices := autoContinueQuestionVeto(messages, prompts, prior)
	if veto != "" {
		return AutoContinueQuestionDecision{Detail: "auto-continue question judge skipped: " + veto}, nil
	}
	return a.askAutoContinueQuestionJudge(client, messages, prompts, choices, prior)
}

// askAutoContinueQuestionJudge runs the post-veto path: select and fit the
// transcript, make the one Decide call and convert the answers. It is split out
// so the veto gate and its choices stay in one place.
func (a *Agent) askAutoContinueQuestionJudge(client Decider, messages []Message, prompts []tool.QuestionPrompt, choices [][]questionChoice, prior []string) (AutoContinueQuestionDecision, error) {
	model := deciderLabel(client)

	transcript := typesafeNonEmpty(messages)
	selected, err := a.selectTypesafeTranscript(client, transcript)
	if err != nil {
		a.emitDebug("AGENT", fmt.Sprintf("autocontinue_question selection failed: %v", err))
		return AutoContinueQuestionDecision{Detail: fmt.Sprintf("%s selection failed: %v", model, err)}, err
	}
	buildState := func(kept []Message) map[string]any {
		state := questionJudgeState(kept, prompts, choices, prior)
		if len(kept) < len(transcript) {
			state["transcript_selection"] = map[string]any{
				"chain_messages": len(transcript),
				"sent":           len(kept),
				"dropped":        len(transcript) - len(kept),
			}
		}
		return state
	}
	tail, err := fitTypesafeTranscript(selected, buildState)
	if err != nil {
		return AutoContinueQuestionDecision{Detail: model + " question judge failed: " + err.Error()}, err
	}

	rubric := config.ApplyPromptOverride("auto_continue_question_judge", typesafeAutoContinueQuestionInstructions)
	questions := questionJudgeQuestions(rubric, prompts, choices)
	resp, err := client.Decide(buildState(tail), questions)
	if err != nil {
		return AutoContinueQuestionDecision{Detail: model + " question judge failed: " + err.Error()}, err
	}
	if resp == nil {
		err := fmt.Errorf("%s question judge returned no response", model)
		return AutoContinueQuestionDecision{Detail: err.Error()}, err
	}
	a.RecordSideUsage(resp.Usage.InputTokens, resp.Usage.OutputTokens, 0, 0, model)

	answers := make([]tool.QuestionAnswerSet, 0, len(prompts))
	for i, p := range prompts {
		set, reason := questionAnswerFromResponse(i, p, choices[i], questions, resp)
		if reason != "" {
			a.emitDebug("AGENT", fmt.Sprintf("autocontinue_question prompt=%d header=%q deferred: %s", i, p.Header, reason))
			return AutoContinueQuestionDecision{Detail: fmt.Sprintf("%s deferred prompt %d (%s) to you: %s", model, i, p.Header, reason)}, nil
		}
		answers = append(answers, set)
	}
	a.emitDebug("AGENT", fmt.Sprintf("autocontinue_question answered %d prompt(s) prior=%v", len(answers), prior))

	parts := make([]string, 0, len(answers))
	for _, set := range answers {
		labels := make([]string, 0, len(set.Answers))
		for _, ans := range set.Answers {
			labels = append(labels, ans.Label)
		}
		parts = append(parts, set.Header+" → "+strings.Join(labels, ", "))
	}
	return AutoContinueQuestionDecision{
		Answered: true,
		Answers:  answers,
		Detail:   fmt.Sprintf("%s answered on your behalf: %s", model, strings.Join(parts, "; ")),
	}, nil
}

// AutoContinueQuestionJudgeSync is the synchronous variant for a headless turn.
// It runs the question judge inline and returns the decision. Same fail-closed
// contract as the triage judge: no judge configured returns a zero decision and
// nil error; a judge error returns Answered=false with the failure in Detail.
// The chat judge is never used here: a non-decision judge model returns
// Answered=false without a call.
func (a *Agent) AutoContinueQuestionJudgeSync(messages []Message, prompts []tool.QuestionPrompt, prior []string) (AutoContinueQuestionDecision, error) {
	if config.PromptDisabled("auto_continue_question_judge") {
		return AutoContinueQuestionDecision{Detail: "auto-continue question judge prompt is disabled"}, nil
	}
	client, _ := a.autoContinueJudgeClientTyped()
	if client == nil {
		return AutoContinueQuestionDecision{}, nil
	}
	d, isDecision := client.(Decider)
	if !isDecision {
		return AutoContinueQuestionDecision{Detail: "auto-continue question judge needs a decision backend; the chat judge never answers on the user's behalf"}, nil
	}
	return a.runAutoContinueQuestionJudgeTypesafe(d, messages, prompts, prior)
}

// copyQuestionPrompts deep-copies prompts, options included, so an async judge
// never reads a slice the caller may still be writing.
func copyQuestionPrompts(prompts []tool.QuestionPrompt) []tool.QuestionPrompt {
	out := make([]tool.QuestionPrompt, len(prompts))
	for i, p := range prompts {
		p.Options = append([]tool.QuestionOption(nil), p.Options...)
		out[i] = p
	}
	return out
}

// AutoContinueQuestionJudgeAsync asks the question judge in the background and
// reports through OnAutoContinueQuestionJudge. It returns false and makes no
// call when the prompt is disabled, no decision backend is configured, or a
// judge call is already in flight (it shares autoContinueJudgeMu with the triage
// judge, so the two never run at once).
func (a *Agent) AutoContinueQuestionJudgeAsync(messages []Message, toolCallID string, prompts []tool.QuestionPrompt, prior []string) bool {
	if config.PromptDisabled("auto_continue_question_judge") {
		return false
	}
	client, _ := a.autoContinueJudgeClientTyped()
	if client == nil {
		return false
	}
	d, isDecision := client.(Decider)
	if !isDecision {
		return false
	}
	if !a.autoContinueJudgeMu.TryLock() {
		return false
	}
	snapshot := make([]Message, len(messages))
	copy(snapshot, messages)
	promptsCopy := copyQuestionPrompts(prompts)
	priorCopy := append([]string(nil), prior...)
	crashguard.Go(func() {
		defer a.autoContinueJudgeMu.Unlock()
		dec, err := a.runAutoContinueQuestionJudgeTypesafe(d, snapshot, promptsCopy, priorCopy)
		if a.OnAutoContinueQuestionJudge != nil {
			a.OnAutoContinueQuestionJudge(AutoContinueQuestionJudgeResult{
				ToolCallID: toolCallID,
				Answered:   dec.Answered,
				Answers:    dec.Answers,
				Detail:     dec.Detail,
				Err:        err,
			})
		}
	})
	return true
}
