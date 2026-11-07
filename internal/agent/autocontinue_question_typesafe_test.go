package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

// questionStubDecider is a scripted Decider for the question judge. It records
// the state and questions of the last call and counts calls. It embeds a nil
// LLMClient only to satisfy the interface newClientFn returns; the question
// judge never calls Chat through it.
type questionStubDecider struct {
	LLMClient
	respond   func(map[string]TypesafeQuestion) *TypesafeResponse
	calls     int
	state     any
	questions map[string]TypesafeQuestion
}

func (s *questionStubDecider) Decide(state any, q map[string]TypesafeQuestion) (*TypesafeResponse, error) {
	s.calls++
	s.state = state
	s.questions = q
	return s.respond(q), nil
}

func (s *questionStubDecider) DecideCtx(_ context.Context, state any, q map[string]TypesafeQuestion) (*TypesafeResponse, error) {
	return s.Decide(state, q)
}

func (s *questionStubDecider) GetProvider() string { return "typesafe" }
func (s *questionStubDecider) GetModel() string    { return "jev-latest" }

func qAnsChoice(choice string, probs map[string]float64) TypesafeAnswer {
	return TypesafeAnswer{Type: "choice", Choice: choice, Probabilities: probs, Confidence: 0.7}
}

func qAnsNoul(n float64) TypesafeAnswer {
	return TypesafeAnswer{Type: "noul", Noul: n, Confidence: 0.7}
}

func qOwnerDetermined(p float64) TypesafeAnswer {
	return qAnsChoice(typesafeQuestionOwnerDetermined, map[string]float64{
		typesafeQuestionOwnerDetermined: p,
		typesafeQuestionOwnerPreference: 1 - p,
	})
}

func qResp(answers map[string]TypesafeAnswer) *TypesafeResponse {
	return &TypesafeResponse{Model: "jev-latest", Answers: answers, Usage: TypesafeUsage{InputTokens: 11, OutputTokens: 3}}
}

// qDeferEverything answers every question with the value that defers: the owner
// question says determined, every choice says defer_to_user, every defer noul is
// high and every option noul is low.
func qDeferEverything(q map[string]TypesafeQuestion) *TypesafeResponse {
	ans := make(map[string]TypesafeAnswer, len(q))
	for k, qq := range q {
		switch {
		case strings.HasSuffix(k, "_owner"):
			ans[k] = qOwnerDetermined(1)
		case qq.Type == "choice":
			ans[k] = qAnsChoice(typesafeQuestionDeferChoice, map[string]float64{typesafeQuestionDeferChoice: 1})
		case strings.HasSuffix(k, "_defer"):
			ans[k] = qAnsNoul(1)
		default:
			ans[k] = qAnsNoul(0)
		}
	}
	return qResp(ans)
}

// qDatabasePrompt is a single-select prompt with one custom option that must
// never reach Jev.
func qDatabasePrompt() tool.QuestionPrompt {
	return tool.QuestionPrompt{
		Header:   "Database",
		Question: "Which database should the notes service use?",
		Options: []tool.QuestionOption{
			{Label: "PostgreSQL", Description: "Already running in docker-compose"},
			{Label: "SQLite", Description: "Embedded file database"},
			{Label: "Something else", Description: "Describe your own"},
		},
	}
}

// qLoginPrompt is a multi-select prompt.
func qLoginPrompt() tool.QuestionPrompt {
	return tool.QuestionPrompt{
		Header:   "Login",
		Question: "Which login methods should ship first?",
		Multiple: true,
		Options: []tool.QuestionOption{
			{Label: "OAuth", Description: "Google and GitHub sign-in"},
			{Label: "Sessions", Description: "Cookie sessions"},
			{Label: "Magic link", Description: "Email link login"},
		},
	}
}

func qMessages() []Message {
	return []Message{
		{Role: "user", Content: "Add persistence and login to the notes service"},
		{Role: "assistant", Content: "Which database should the notes service use?"},
	}
}

// qToolCall is one tool call by id and name, as an assistant message records it.
func qToolCall(id, name string) ToolCall {
	tc := ToolCall{ID: id, Type: "function"}
	tc.Function.Name = name
	return tc
}

func qNewAgent() *Agent {
	return newTestAgent(nil, nil, &config.Config{}, nil)
}

func TestAutoContinueQuestionSingleDeterminedAnswersWithOriginalLabel(t *testing.T) {
	stub := &questionStubDecider{respond: func(map[string]TypesafeQuestion) *TypesafeResponse {
		return qResp(map[string]TypesafeAnswer{
			"q0_owner":  qOwnerDetermined(0.9),
			"q0_answer": qAnsChoice("opt0", map[string]float64{"opt0": 0.9, typesafeQuestionDeferChoice: 0.1}),
		})
	}}
	dec, err := qNewAgent().runAutoContinueQuestionJudgeTypesafe(stub, qMessages(), []tool.QuestionPrompt{qDatabasePrompt()}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !dec.Answered {
		t.Fatalf("want answered, got %+v", dec)
	}
	if len(dec.Answers) != 1 || len(dec.Answers[0].Answers) != 1 {
		t.Fatalf("answers = %+v", dec.Answers)
	}
	got := dec.Answers[0].Answers[0]
	if got.Label != "PostgreSQL" || got.Custom || got.Text != "" {
		t.Fatalf("answer = %+v, want label PostgreSQL with no custom or text", got)
	}
	if dec.Answers[0].Header != "Database" || dec.Answers[0].Question != "Which database should the notes service use?" {
		t.Fatalf("answer set header/question = %+v", dec.Answers[0])
	}
	want := "typesafe/jev-latest answered on your behalf: Database → PostgreSQL"
	if dec.Detail != want {
		t.Fatalf("detail = %q, want %q", dec.Detail, want)
	}
}

func TestAutoContinueQuestionSingleBelowFloorDefers(t *testing.T) {
	stub := &questionStubDecider{respond: func(map[string]TypesafeQuestion) *TypesafeResponse {
		return qResp(map[string]TypesafeAnswer{
			"q0_owner":  qOwnerDetermined(0.9),
			"q0_answer": qAnsChoice("opt0", map[string]float64{"opt0": 0.7, typesafeQuestionDeferChoice: 0.3}),
		})
	}}
	dec, err := qNewAgent().runAutoContinueQuestionJudgeTypesafe(stub, qMessages(), []tool.QuestionPrompt{qDatabasePrompt()}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec.Answered || dec.Answers != nil {
		t.Fatalf("want deferred, got %+v", dec)
	}
	if !strings.Contains(dec.Detail, "floor") {
		t.Fatalf("detail should name the floor: %q", dec.Detail)
	}
}

func TestAutoContinueQuestionJevDefersToUser(t *testing.T) {
	stub := &questionStubDecider{respond: func(map[string]TypesafeQuestion) *TypesafeResponse {
		return qResp(map[string]TypesafeAnswer{
			"q0_owner":  qOwnerDetermined(0.9),
			"q0_answer": qAnsChoice(typesafeQuestionDeferChoice, map[string]float64{typesafeQuestionDeferChoice: 0.95, "opt0": 0.05}),
		})
	}}
	dec, err := qNewAgent().runAutoContinueQuestionJudgeTypesafe(stub, qMessages(), []tool.QuestionPrompt{qDatabasePrompt()}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec.Answered {
		t.Fatalf("defer_to_user must not answer: %+v", dec)
	}
}

func TestAutoContinueQuestionUserPreferenceDefersEvenWhenAnswerIsConfident(t *testing.T) {
	stub := &questionStubDecider{respond: func(map[string]TypesafeQuestion) *TypesafeResponse {
		return qResp(map[string]TypesafeAnswer{
			"q0_owner": qAnsChoice(typesafeQuestionOwnerPreference, map[string]float64{
				typesafeQuestionOwnerPreference: 0.9,
				typesafeQuestionOwnerDetermined: 0.1,
			}),
			"q0_answer": qAnsChoice("opt0", map[string]float64{"opt0": 0.95, typesafeQuestionDeferChoice: 0.05}),
		})
	}}
	dec, err := qNewAgent().runAutoContinueQuestionJudgeTypesafe(stub, qMessages(), []tool.QuestionPrompt{qDatabasePrompt()}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec.Answered {
		t.Fatalf("a user_preference owner must not answer: %+v", dec)
	}
}

func TestAutoContinueQuestionMultiSelectAnswersDeterminedOptions(t *testing.T) {
	cases := []struct {
		name     string
		opt1Noul float64
		answered bool
	}{
		{name: "two options determined", opt1Noul: 0.9, answered: true},
		{name: "one option undetermined defers", opt1Noul: 0.5, answered: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &questionStubDecider{respond: func(map[string]TypesafeQuestion) *TypesafeResponse {
				return qResp(map[string]TypesafeAnswer{
					"q0_owner": qOwnerDetermined(0.9),
					"q0_opt0":  qAnsNoul(0.9),
					"q0_opt1":  qAnsNoul(tc.opt1Noul),
					"q0_opt2":  qAnsNoul(0.05),
					"q0_defer": qAnsNoul(0.1),
				})
			}}
			dec, err := qNewAgent().runAutoContinueQuestionJudgeTypesafe(stub, qMessages(), []tool.QuestionPrompt{qLoginPrompt()}, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if dec.Answered != tc.answered {
				t.Fatalf("answered = %v, want %v: %+v", dec.Answered, tc.answered, dec)
			}
			if !tc.answered {
				return
			}
			var labels []string
			for _, ans := range dec.Answers[0].Answers {
				labels = append(labels, ans.Label)
			}
			if strings.Join(labels, ",") != "OAuth,Sessions" {
				t.Fatalf("labels = %v, want OAuth then Sessions in option order", labels)
			}
		})
	}
}

func TestAutoContinueQuestionRiskWordVetoesWithoutCall(t *testing.T) {
	prompt := qDatabasePrompt()
	prompt.Options[1].Description = "Run deploy first"
	stub := &questionStubDecider{respond: qDeferEverything}
	dec, err := qNewAgent().runAutoContinueQuestionJudgeTypesafe(stub, qMessages(), []tool.QuestionPrompt{prompt}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec.Answered || stub.calls != 0 {
		t.Fatalf("risk word must veto with no call: answered=%v calls=%d", dec.Answered, stub.calls)
	}
	if !strings.Contains(dec.Detail, "deploy") {
		t.Fatalf("detail should name the risk word: %q", dec.Detail)
	}
}

func TestAutoContinueQuestionRiskWordIsWholeWord(t *testing.T) {
	prompt := qDatabasePrompt()
	prompt.Options[1].Description = "Works with the product catalogue"
	stub := &questionStubDecider{respond: qDeferEverything}
	if _, err := qNewAgent().runAutoContinueQuestionJudgeTypesafe(stub, qMessages(), []tool.QuestionPrompt{prompt}, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stub.calls != 1 {
		t.Fatalf("\"product\" is not the whole word \"prod\"; want one call, got %d", stub.calls)
	}
}

func TestAutoContinueQuestionShapeVetoesWithoutCall(t *testing.T) {
	many := make([]tool.QuestionOption, 7)
	for i := range many {
		many[i] = tool.QuestionOption{Label: fmt.Sprintf("Option %c", 'A'+i), Description: "plain choice"}
	}
	cases := map[string][]tool.QuestionPrompt{
		"no prompts": nil,
		"four prompts": {
			qDatabasePrompt(), qDatabasePrompt(), qDatabasePrompt(), qDatabasePrompt(),
		},
		"seven answerable options": {{Header: "Pick", Question: "Which one?", Options: many}},
	}
	for name, prompts := range cases {
		t.Run(name, func(t *testing.T) {
			stub := &questionStubDecider{respond: qDeferEverything}
			dec, err := qNewAgent().runAutoContinueQuestionJudgeTypesafe(stub, qMessages(), prompts, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if dec.Answered || stub.calls != 0 {
				t.Fatalf("shape veto must make no call: answered=%v calls=%d detail=%q", dec.Answered, stub.calls, dec.Detail)
			}
		})
	}
}

func TestAutoContinueQuestionPerChainCap(t *testing.T) {
	stub := &questionStubDecider{respond: qDeferEverything}
	a := qNewAgent()
	capped := []string{AutoContinueAnswerQuestionAction, AutoContinueAnswerQuestionAction}
	dec, err := a.runAutoContinueQuestionJudgeTypesafe(stub, qMessages(), []tool.QuestionPrompt{qDatabasePrompt()}, capped)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec.Answered || stub.calls != 0 {
		t.Fatalf("two prior answers must veto with no call: answered=%v calls=%d", dec.Answered, stub.calls)
	}

	once := []string{AutoContinueAnswerQuestionAction}
	if _, err := a.runAutoContinueQuestionJudgeTypesafe(stub, qMessages(), []tool.QuestionPrompt{qDatabasePrompt()}, once); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stub.calls != 1 {
		t.Fatalf("one prior answer is under the cap; want one call, got %d", stub.calls)
	}
}

func TestAutoContinueQuestionRepeatedAskVetoesWithoutCall(t *testing.T) {
	answered, err := json.Marshal([]tool.QuestionAnswerSet{{
		Header:   "Database",
		Question: "Which database should the notes service use?",
		Answers:  []tool.QuestionAnswer{{Label: "SQLite"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	repeated := []Message{
		{Role: "user", Content: "Add persistence to the notes service"},
		{Role: "assistant", ToolCalls: []ToolCall{qToolCall("call_db_1", "question")}},
		{Role: "tool", ToolID: "call_db_1", Content: string(answered)},
		{Role: "assistant", Content: "Which database should the notes service use?"},
	}
	stub := &questionStubDecider{respond: qDeferEverything}
	dec, err := qNewAgent().runAutoContinueQuestionJudgeTypesafe(stub, repeated, []tool.QuestionPrompt{qDatabasePrompt()}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec.Answered || stub.calls != 0 {
		t.Fatalf("repeated question must veto with no call: answered=%v calls=%d", dec.Answered, stub.calls)
	}

	// A tool result that is not an answer set must not count as a repeat.
	notAnswer := []Message{
		{Role: "user", Content: "Add persistence to the notes service"},
		{Role: "assistant", ToolCalls: []ToolCall{qToolCall("call_read_1", "read")}},
		{Role: "tool", ToolID: "call_read_1", Content: "plain text output"},
		{Role: "assistant", Content: "Which database should the notes service use?"},
	}
	stub = &questionStubDecider{respond: qDeferEverything}
	if _, err := qNewAgent().runAutoContinueQuestionJudgeTypesafe(stub, notAnswer, []tool.QuestionPrompt{qDatabasePrompt()}, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stub.calls != 1 {
		t.Fatalf("a non-answer tool result must not veto; want one call, got %d", stub.calls)
	}
}

// TestAutoContinueQuestionForeignFAQJSONDoesNotVeto covers a tool that is not
// the question tool but returns JSON shaped like an answer set. Its result must
// not count as a prior answer, even though the question text matches.
func TestAutoContinueQuestionForeignFAQJSONDoesNotVeto(t *testing.T) {
	faq, err := json.Marshal([]tool.QuestionAnswerSet{{
		Header:   "Database",
		Question: "Which database should the notes service use?",
		Answers:  []tool.QuestionAnswer{{Label: "SQLite"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	messages := []Message{
		{Role: "user", Content: "Add persistence to the notes service"},
		{Role: "assistant", ToolCalls: []ToolCall{qToolCall("call_faq_1", "faq_search")}},
		{Role: "tool", ToolID: "call_faq_1", Content: string(faq)},
		{Role: "assistant", Content: "Which database should the notes service use?"},
	}
	stub := &questionStubDecider{respond: qDeferEverything}
	if _, err := qNewAgent().runAutoContinueQuestionJudgeTypesafe(stub, messages, []tool.QuestionPrompt{qDatabasePrompt()}, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stub.calls != 1 {
		t.Fatalf("a non-question tool result must not veto; want one call, got %d", stub.calls)
	}
}

func TestAutoContinueQuestionAllOrNothing(t *testing.T) {
	stub := &questionStubDecider{respond: func(q map[string]TypesafeQuestion) *TypesafeResponse {
		return qResp(map[string]TypesafeAnswer{
			"q0_owner":  qOwnerDetermined(0.9),
			"q0_answer": qAnsChoice("opt0", map[string]float64{"opt0": 0.95, typesafeQuestionDeferChoice: 0.05}),
			"q1_owner":  qOwnerDetermined(0.9),
			"q1_opt0":   qAnsNoul(0.9),
			"q1_opt1":   qAnsNoul(0.05),
			"q1_opt2":   qAnsNoul(0.05),
			"q1_defer":  qAnsNoul(0.9),
		})
	}}
	prompts := []tool.QuestionPrompt{qDatabasePrompt(), qLoginPrompt()}
	dec, err := qNewAgent().runAutoContinueQuestionJudgeTypesafe(stub, qMessages(), prompts, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec.Answered || dec.Answers != nil {
		t.Fatalf("one deferred prompt must defer the whole question: %+v", dec)
	}
	if !strings.Contains(dec.Detail, "Login") {
		t.Fatalf("detail should name the deferred prompt: %q", dec.Detail)
	}
}

func TestAutoContinueQuestionSentQuestionsHideCustomOption(t *testing.T) {
	stub := &questionStubDecider{respond: qDeferEverything}
	prompts := []tool.QuestionPrompt{qDatabasePrompt(), qLoginPrompt()}
	if _, err := qNewAgent().runAutoContinueQuestionJudgeTypesafe(stub, qMessages(), prompts, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stub.calls != 1 {
		t.Fatalf("calls = %d, want 1", stub.calls)
	}

	for key, q := range stub.questions {
		for criterion := range q.Criteria {
			if strings.Contains(strings.ToLower(criterion), "something else") {
				t.Fatalf("question %s offers custom criterion %q", key, criterion)
			}
		}
		if strings.Contains(strings.ToLower(q.Instructions), "something else") {
			t.Fatalf("question %s instructions mention the custom option", key)
		}
	}
	single := stub.questions["q0_answer"]
	if _, ok := single.Criteria[typesafeQuestionDeferChoice]; !ok {
		t.Fatalf("single-select criteria lack %s: %v", typesafeQuestionDeferChoice, single.Criteria)
	}
	if got := single.Criteria["opt0"]; got != "PostgreSQL: Already running in docker-compose" {
		t.Fatalf("opt0 criterion = %q, want the label followed by its description", got)
	}
	if _, ok := single.Criteria["postgresql"]; ok {
		t.Fatalf("criteria keys must be identifiers, not labels: %v", single.Criteria)
	}
	if !strings.Contains(single.Instructions, "PostgreSQL") {
		t.Fatalf("single-select instructions lack the option list: %q", single.Instructions)
	}
	if _, ok := stub.questions["q1_opt0"]; !ok {
		t.Fatalf("multi-select option question missing: keys=%v", stub.questions)
	}

	state, ok := stub.state.(map[string]any)
	if !ok {
		t.Fatalf("state = %T, want map", stub.state)
	}
	entries, ok := state["questions"].([]map[string]any)
	if !ok || len(entries) != 2 {
		t.Fatalf("state questions = %#v", state["questions"])
	}
	if entries[0]["multiple"] != false || entries[1]["multiple"] != true {
		t.Fatalf("multiple flags = %v / %v", entries[0]["multiple"], entries[1]["multiple"])
	}
	opts, _ := entries[0]["options"].([]map[string]any)
	if len(opts) != 2 {
		t.Fatalf("answerable options = %d, want 2 (custom dropped): %v", len(opts), opts)
	}
	if _, ok := state["chain_actions_fired"].([]string); !ok {
		t.Fatalf("chain_actions_fired = %T, want []string", state["chain_actions_fired"])
	}
}

func TestAutoContinueQuestionInvalidAnswersDefer(t *testing.T) {
	cases := map[string]TypesafeAnswer{
		"choice not offered":       qAnsChoice("mysql", map[string]float64{"mysql": 0.99, typesafeQuestionDeferChoice: 0.01}),
		"probabilities not summed": qAnsChoice("opt0", map[string]float64{"opt0": 0.9, typesafeQuestionDeferChoice: 0.3}),
		"answer type mismatch":     qAnsNoul(0.99),
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			stub := &questionStubDecider{respond: func(map[string]TypesafeQuestion) *TypesafeResponse {
				return qResp(map[string]TypesafeAnswer{
					"q0_owner":  qOwnerDetermined(0.9),
					"q0_answer": bad,
				})
			}}
			dec, err := qNewAgent().runAutoContinueQuestionJudgeTypesafe(stub, qMessages(), []tool.QuestionPrompt{qDatabasePrompt()}, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if dec.Answered {
				t.Fatalf("invalid answer must not answer: %+v", dec)
			}
		})
	}
}

func TestAutoContinueQuestionSyncNeverUsesChatJudge(t *testing.T) {
	prev := newClientFn
	t.Cleanup(func() { newClientFn = prev })
	chat := &scriptedCaptureClient{Responses: []string{"postgresql"}}
	newClientFn = func(_ *config.Config, _ string) LLMClient { return chat }

	cfg := &config.Config{}
	cfg.Ocode.AutoContinueModel = "opencode-go/gpt-x"
	a := newTestAgent(nil, nil, cfg, nil)
	dec, err := a.AutoContinueQuestionJudgeSync(qMessages(), []tool.QuestionPrompt{qDatabasePrompt()}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec.Answered || chat.CallCount != 0 {
		t.Fatalf("chat judge must never answer: answered=%v calls=%d", dec.Answered, chat.CallCount)
	}
	if !strings.Contains(dec.Detail, "decision backend") {
		t.Fatalf("detail should say a decision backend is needed: %q", dec.Detail)
	}
}

func TestAutoContinueQuestionSyncUsesDecisionBackend(t *testing.T) {
	stub := &questionStubDecider{respond: func(map[string]TypesafeQuestion) *TypesafeResponse {
		return qResp(map[string]TypesafeAnswer{
			"q0_owner":  qOwnerDetermined(0.9),
			"q0_answer": qAnsChoice("opt1", map[string]float64{"opt1": 0.9, typesafeQuestionDeferChoice: 0.1}),
		})
	}}
	prev := newClientFn
	t.Cleanup(func() { newClientFn = prev })
	newClientFn = func(_ *config.Config, _ string) LLMClient { return stub }

	cfg := &config.Config{}
	cfg.Ocode.AutoContinueModel = "typesafe/jev-latest"
	a := newTestAgent(nil, nil, cfg, nil)
	dec, err := a.AutoContinueQuestionJudgeSync(qMessages(), []tool.QuestionPrompt{qDatabasePrompt()}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !dec.Answered || stub.calls != 1 {
		t.Fatalf("decision backend should answer once: answered=%v calls=%d detail=%q", dec.Answered, stub.calls, dec.Detail)
	}
	if got := dec.Answers[0].Answers[0].Label; got != "SQLite" {
		t.Fatalf("label = %q, want SQLite", got)
	}
}

func TestAutoContinueQuestionAsyncNeverDispatchesChatJudge(t *testing.T) {
	prev := newClientFn
	t.Cleanup(func() { newClientFn = prev })
	chat := &scriptedCaptureClient{Responses: []string{"postgresql"}}
	newClientFn = func(_ *config.Config, _ string) LLMClient { return chat }

	cfg := &config.Config{}
	cfg.Ocode.AutoContinueModel = "opencode-go/gpt-x"
	a := newTestAgent(nil, nil, cfg, nil)
	if a.AutoContinueQuestionJudgeAsync(qMessages(), "call-1", []tool.QuestionPrompt{qDatabasePrompt()}, nil) {
		t.Fatal("a chat judge must not dispatch the question judge")
	}
	if chat.CallCount != 0 {
		t.Fatalf("chat client called %d times", chat.CallCount)
	}
}

// TestAutoContinueQuestionRequestSizeWorstCase marshals the worst case the
// builder can produce (3 multi-select prompts of 6 options each) with a
// representative transcript and checks it stays under Jev's 64 KB request limit.
func TestAutoContinueQuestionRequestSizeWorstCase(t *testing.T) {
	prompts := make([]tool.QuestionPrompt, autoContinueQuestionMaxPrompts)
	for i := range prompts {
		opts := make([]tool.QuestionOption, autoContinueQuestionMaxOptions)
		for j := range opts {
			opts[j] = tool.QuestionOption{
				Label:       fmt.Sprintf("Option %d-%d", i, j),
				Description: strings.Repeat("A plausible option description. ", 4),
			}
		}
		prompts[i] = tool.QuestionPrompt{
			Header:   fmt.Sprintf("Section %d", i),
			Question: "Which of these should ship in the first release of the notes service?",
			Multiple: true,
			Options:  opts,
		}
	}
	choices := make([][]questionChoice, len(prompts))
	for i, p := range prompts {
		choices[i] = questionAnswerableChoices(p)
	}
	rubric := config.ApplyPromptOverride("auto_continue_question_judge", typesafeAutoContinueQuestionInstructions)
	questions := questionJudgeQuestions(rubric, prompts, choices)

	messages := []Message{{Role: "user", Content: "Add persistence and login to the notes service"}}
	for k := 0; k < 7; k++ {
		role := "assistant"
		if k%2 == 1 {
			role = "user"
		}
		messages = append(messages, Message{Role: role, Content: strings.Repeat("x", 1500)})
	}
	state := questionJudgeState(messages, prompts, choices, []string{AutoContinueAnswerQuestionAction})

	qBytes, err := json.Marshal(questions)
	if err != nil {
		t.Fatal(err)
	}
	sBytes, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	total := len(qBytes) + len(sBytes)
	t.Logf("questions=%d questions_bytes=%d state_bytes=%d total_bytes=%d limit=%d", len(questions), len(qBytes), len(sBytes), total, 64*1024)
	if total >= 64*1024 {
		t.Fatalf("worst-case request is %d bytes, want under %d", total, 64*1024)
	}
}
