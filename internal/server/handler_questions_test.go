package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/tool"
)

// questionFakeClientAsk: first call returns a `question` tool call, second
// returns a plain assistant message ending the loop. Drives a real agent Step
// so the question-tool sentinel, OnMessage emission and turn pause all run
// for real (unlike the static-transcript tests).
type questionFakeClientAsk struct{ calls int }

func (f *questionFakeClientAsk) Chat([]agent.Message, []map[string]interface{}) (*agent.Message, error) {
	f.calls++
	if f.calls == 1 {
		return &agent.Message{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: "q-call-1", Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "question", Arguments: `{"questions":[{"header":"Deploy target","question":"Where should I deploy?","options":[{"label":"Staging","description":"Push to staging"},{"label":"Production","description":"Push to production"}]}]}`}}}}, nil
	}
	return &agent.Message{Role: "assistant", Content: "thanks, deploying to staging"}, nil
}
func (f *questionFakeClientAsk) GetProvider() string { return "fake" }
func (f *questionFakeClientAsk) GetModel() string    { return "fake-model" }

// questionAskContent builds the tool-result content the `question` tool emits
// for one prompt, matching tool.QuestionTool.Execute.
func questionAskContent(t *testing.T, prompts ...tool.QuestionPrompt) string {
	t.Helper()
	data, err := json.Marshal(prompts)
	if err != nil {
		t.Fatalf("marshal prompts: %v", err)
	}
	return tool.SentinelQuestionPrompt + "\n" + string(data) + "\n\n" + tool.SentinelWaitingForUser
}

func sampleQuestion() tool.QuestionPrompt {
	return tool.QuestionPrompt{
		Header:   "Deploy target",
		Question: "Where should I deploy?",
		Options: []tool.QuestionOption{
			{Label: "Staging", Description: "Push to staging"},
			{Label: "Production", Description: "Push to production"},
		},
	}
}

func TestParseQuestionAsk(t *testing.T) {
	content := questionAskContent(t, sampleQuestion())
	prompts, ok := parseQuestionAsk(content)
	if !ok {
		t.Fatalf("parseQuestionAsk returned ok=false for a valid prompt")
	}
	if len(prompts) != 1 || prompts[0].Header != "Deploy target" || len(prompts[0].Options) != 2 {
		t.Fatalf("unexpected parse result: %+v", prompts)
	}

	if _, ok := parseQuestionAsk("just a normal tool result"); ok {
		t.Fatalf("parseQuestionAsk should reject non-question content")
	}
	if _, ok := parseQuestionAsk(tool.SentinelQuestionPrompt + "\nnot-json\n\n" + tool.SentinelWaitingForUser); ok {
		t.Fatalf("parseQuestionAsk should reject malformed JSON payload")
	}
}

func TestTailIsQuestionAsk(t *testing.T) {
	ask := agent.Message{Role: "tool", ToolID: "call-1", Content: questionAskContent(t, sampleQuestion())}

	if !tailIsQuestionAsk([]agent.Message{{Role: "user", Content: "hi"}, ask}) {
		t.Fatalf("expected tail question ask to be detected")
	}
	// Resolved: a message follows the ask.
	if tailIsQuestionAsk([]agent.Message{ask, {Role: "assistant", Content: "done"}}) {
		t.Fatalf("resolved ask should not count as pending")
	}
	// A permission ask is not a question ask.
	perm := agent.Message{Role: "tool", Content: tool.SentinelPermissionAsk + `{"toolName":"bash"}`}
	if tailIsQuestionAsk([]agent.Message{perm}) {
		t.Fatalf("permission ask should not be a question ask")
	}
	if tailIsQuestionAsk(nil) {
		t.Fatalf("empty history should not be pending")
	}
}

func TestApplyQuestionAnswer(t *testing.T) {
	msgs := []agent.Message{
		{Role: "user", Content: "deploy"},
		{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: "call-1"}}},
		{Role: "tool", ToolID: "call-1", Content: questionAskContent(t, sampleQuestion())},
	}

	if applyQuestionAnswer(msgs, "wrong-id", `[]`) {
		t.Fatalf("mismatched request_id should not apply")
	}

	answer := `[{"question":"Where should I deploy?","answers":[{"label":"Staging"}]}]`
	if !applyQuestionAnswer(msgs, "call-1", answer) {
		t.Fatalf("expected answer to apply")
	}
	last := msgs[len(msgs)-1]
	if last.Content != answer {
		t.Fatalf("tool result not replaced in place: %q", last.Content)
	}
	if isQuestionAsk(last.Content) {
		t.Fatalf("replaced content should no longer be a pending ask")
	}
	// Second apply is a no-op: the ask is already resolved.
	if applyQuestionAnswer(msgs, "call-1", `[]`) {
		t.Fatalf("already-answered ask should not re-apply")
	}
}

func TestHandleAnswerQuestionValidation(t *testing.T) {
	cases := []struct {
		name string
		body string
		want int
	}{
		{"bad json", `{`, http.StatusBadRequest},
		{"missing request_id", `{"answers":[]}`, http.StatusBadRequest},
		{"missing answers", `{"request_id":"call-1"}`, http.StatusBadRequest},
		{"no pending question", `{"request_id":"call-1","answers":[]}`, http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHandler()
			req := httptest.NewRequest("POST", "/api/questions", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			h.HandleAnswerQuestion(rec, req)
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d (body=%s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestHandleAnswerQuestionForwardsToBridgeWhenBridged(t *testing.T) {
	resolveCh := make(chan RCResolution, 1)
	h := NewHandler()
	h.rc = &RCBridge{SessionID: "tui-sess", ResolveCh: resolveCh}

	// Guard the no-broadcast invariant: in bridge mode the server must NOT
	// emit question_resolved on headlessSubs — the bridged web client listens
	// on the TUI's bridge channel, and the TUI broadcasts the dismissal itself
	// once it applies the answers.
	evCh := h.subscribeHeadless()
	defer h.unsubscribeHeadless(evCh)

	body := `{"request_id":"call-1","answers":[{"question":"q","answers":[{"label":"Staging"}]}]}`
	req := httptest.NewRequest("POST", "/api/questions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.HandleAnswerQuestion(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	select {
	case res := <-resolveCh:
		if res.RequestID != "call-1" || len(res.Answers) != 1 || len(res.Answers[0].Answers) != 1 || res.Answers[0].Answers[0].Label != "Staging" {
			t.Fatalf("unexpected resolution: %+v", res)
		}
	case <-time.After(time.Second):
		t.Fatalf("no resolution forwarded to the bridge")
	}
	select {
	case ev := <-evCh:
		if ev.Event == "question_resolved" {
			t.Fatalf("server must not broadcast question_resolved in bridge mode (got SessionID=%q)", ev.SessionID)
		}
	case <-time.After(100 * time.Millisecond):
		// Expected: no headless-channel event for the bridged resolution.
	}
}

// questionFakeClient is a minimal LLMClient that always returns a plain
// assistant message, ending the agent loop after the answer is injected.
type questionFakeClient struct{}

func (questionFakeClient) Chat([]agent.Message, []map[string]interface{}) (*agent.Message, error) {
	return &agent.Message{Role: "assistant", Content: "thanks, deploying to staging"}, nil
}
func (questionFakeClient) GetProvider() string { return "fake" }
func (questionFakeClient) GetModel() string    { return "fake-model" }

func TestHandleAnswerQuestionResolvesAndContinues(t *testing.T) {
	h := NewHandler()
	ag := agent.NewAgent(questionFakeClient{}, nil, nil, nil)
	as := &agentSession{
		agent: ag,
		model: "fake-model",
		messages: []agent.Message{
			{Role: "user", Content: "deploy"},
			{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: "call-1"}}},
			{Role: "tool", ToolID: "call-1", Content: questionAskContent(t, sampleQuestion())},
		},
	}
	h.agents["sess-1"] = as

	// Subscribe to the mirror so we can assert the question_resolved frame fires.
	sub := h.subscribeHeadless()
	defer h.unsubscribeHeadless(sub)

	body := `{"request_id":"call-1","answers":[{"header":"Deploy target","question":"Where should I deploy?","answers":[{"label":"Staging"}]}]}`
	req := httptest.NewRequest("POST", "/api/questions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.HandleAnswerQuestion(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}

	// The pending ask must have been replaced with the answer JSON (no sentinel).
	answered := as.messages[2]
	if isQuestionAsk(answered.Content) {
		t.Fatalf("question ask was not resolved: %q", answered.Content)
	}
	if !strings.Contains(answered.Content, `"label":"Staging"`) {
		t.Fatalf("answer JSON not injected: %q", answered.Content)
	}
	// The agent's follow-up assistant message must be appended.
	if last := as.messages[len(as.messages)-1]; last.Role != "assistant" || last.Content == "" {
		t.Fatalf("expected assistant continuation, got %+v", last)
	}

	// A question_resolved event should have been broadcast.
	sawResolved := false
	for drained := false; !drained; {
		select {
		case ev := <-sub:
			if ev.Event == "question_resolved" {
				sawResolved = true
			}
		default:
			drained = true
		}
	}
	if !sawResolved {
		t.Fatalf("expected a question_resolved mirror event")
	}
}

// TestHandleAnswerQuestionForwardsMultipleSelection verifies the RC bridge keeps
// every selected answer for a multi-select question (it must not collapse to the
// first selection as the old code did).
func TestHandleAnswerQuestionForwardsMultipleSelection(t *testing.T) {
	resolveCh := make(chan RCResolution, 1)
	h := NewHandler()
	h.rc = &RCBridge{SessionID: "tui-sess", ResolveCh: resolveCh}

	body := `{"request_id":"call-1","answers":[{"question":"q","answers":[{"label":"A","text":"A"},{"label":"B","text":"B"}]}]}`
	req := httptest.NewRequest("POST", "/api/questions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.HandleAnswerQuestion(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	select {
	case res := <-resolveCh:
		if len(res.Answers) != 1 || len(res.Answers[0].Answers) != 2 {
			t.Fatalf("multi-select collapsed: %+v", res)
		}
		if res.Answers[0].Answers[0].Label != "A" || res.Answers[0].Answers[1].Label != "B" {
			t.Fatalf("unexpected multi-select answers: %+v", res.Answers[0].Answers)
		}
	case <-time.After(time.Second):
		t.Fatalf("no resolution forwarded to the bridge")
	}
}

// TestQuestionEventEmittedOnRealTurn reproduces the live web question flow end to
// end: a real async agent turn (the path web/desktop uses) whose model returns a
// `question` tool call. The agent must execute the real QuestionTool, emit a
// `question` SSE frame on the unified bus carrying the tool-call id and the
// decoded prompts, and pause (the model's second call must NOT happen until an
// answer is submitted). This is the contract a connected browser relies on to
// render the question dialog.
func TestQuestionEventEmittedOnRealTurn(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)
	// Completed MCP cache so bootstrap does not stall.
	ready := make(chan struct{})
	close(ready)
	h.mcpCache = &mcpCache{ready: ready, tools: []tool.Tool{}, errs: nil}
	if h.cfg != nil {
		h.cfg.Model = "fake-model"
	}

	// Register the real question tool so the fake tool call actually executes.
	ag := agent.NewAgent(&questionFakeClientAsk{}, []tool.Tool{&tool.QuestionTool{}}, nil, nil)
	defer ag.Shutdown()
	as := &agentSession{agent: ag, model: "fake-model"}
	h.mu.Lock()
	h.agents[id] = as
	h.mu.Unlock()

	// Subscribe to the unified bus BEFORE dispatching the turn so we are
	// guaranteed to see the question frame (a late subscriber can miss it).
	sub := h.bus.Subscribe(nil)
	defer h.bus.Unsubscribe(sub)

	turnDone := make(chan error, 1)
	go func() {
		_, err := h.runTurn(id, as, "where should I deploy?", turnOptions{})
		turnDone <- err
	}()

	// Wait for the question frame on the bus. The turn must pause on it, so the
	// second model call must not occur beforehand.
	deadline := time.After(10 * time.Second)
	var gotQuestion *Envelope
	secondCallSeen := false
	for gotQuestion == nil {
		select {
		case env := <-sub:
			if env.SessionID != id {
				continue
			}
			if env.Event == "question" {
				ev := env
				gotQuestion = &ev
			}
			if env.Event == "text" || env.Event == "turn_done" {
				secondCallSeen = true
			}
		case <-deadline:
			t.Fatalf("timed out waiting for question event on the bus")
		}
	}

	if secondCallSeen {
		t.Fatalf("turn continued past the question ask (model second call ran); the agent must pause")
	}

	data, ok := gotQuestion.Data.(QuestionEvent)
	if !ok {
		t.Fatalf("question frame payload was %T, want QuestionEvent", gotQuestion.Data)
	}
	if data.RequestID != "q-call-1" {
		t.Fatalf("question request_id = %q, want q-call-1 (the tool-call id)", data.RequestID)
	}
	if len(data.Questions) != 1 {
		t.Fatalf("question has %d prompts, want 1", len(data.Questions))
	}
	if data.Questions[0].Header != "Deploy target" || data.Questions[0].Question != "Where should I deploy?" {
		t.Fatalf("unexpected question prompt: %+v", data.Questions[0])
	}
	if len(data.Questions[0].Options) != 2 {
		t.Fatalf("question has %d options, want 2", len(data.Questions[0].Options))
	}

	as.mu.Lock()
	tail := as.messages[len(as.messages)-1]
	as.mu.Unlock()
	if !strings.HasPrefix(tail.Content, tool.SentinelQuestionPrompt) {
		t.Fatalf("transcript tail = %q, want question sentinel", tail.Content)
	}
	if tail.ToolID != "q-call-1" {
		t.Fatalf("question tool-call id = %q, want q-call-1", tail.ToolID)
	}

	select {
	case <-turnDone:
	case <-time.After(5 * time.Second):
		t.Fatalf("runTurn did not return after the question pause")
	}
}
