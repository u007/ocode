package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/tool"
)

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

func TestHandleAnswerQuestionRejectedWhenBridged(t *testing.T) {
	h := NewHandler()
	h.rc = &RCBridge{SessionID: "tui-sess"}

	body := `{"request_id":"call-1","answers":[{"question":"q","answers":[{"label":"Staging"}]}]}`
	req := httptest.NewRequest("POST", "/api/questions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.HandleAnswerQuestion(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d (body=%s)", rec.Code, http.StatusConflict, rec.Body.String())
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
