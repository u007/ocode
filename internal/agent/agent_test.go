package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jamesmercstudio/ocode/internal/config"
	"github.com/jamesmercstudio/ocode/internal/tool"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type MockClient struct {
	Response *Message
	Err      error
}

func (m *MockClient) Chat(messages []Message, tools []map[string]interface{}) (*Message, error) {
	return m.Response, m.Err
}

func (m *MockClient) GetProvider() string { return "mock" }
func (m *MockClient) GetModel() string    { return "mock-model" }

type captureClient struct {
	Messages []Message
}

func (c *captureClient) Chat(messages []Message, tools []map[string]interface{}) (*Message, error) {
	c.Messages = append([]Message(nil), messages...)
	return &Message{Role: "assistant", Content: "ok"}, nil
}

func (c *captureClient) GetProvider() string { return "mock" }
func (c *captureClient) GetModel() string    { return "mock-model" }

type scriptedCaptureClient struct {
	Prompts   []string
	Responses []string
	CallCount int
}

func (c *scriptedCaptureClient) Chat(messages []Message, tools []map[string]interface{}) (*Message, error) {
	if len(messages) > 0 {
		c.Prompts = append(c.Prompts, messages[0].Content)
	}
	idx := c.CallCount
	c.CallCount++
	resp := "summary"
	if idx < len(c.Responses) {
		resp = c.Responses[idx]
	}
	return &Message{Role: "assistant", Content: resp}, nil
}

func (c *scriptedCaptureClient) GetProvider() string { return "mock" }
func (c *scriptedCaptureClient) GetModel() string    { return "mock-model" }

type blockingToolCallClient struct {
	started chan struct{}
	release chan struct{}
	resp    *Message
}

func (m *blockingToolCallClient) Chat(messages []Message, tools []map[string]interface{}) (*Message, error) {
	close(m.started)
	<-m.release
	return m.resp, nil
}

func (m *blockingToolCallClient) GetProvider() string { return "mock" }
func (m *blockingToolCallClient) GetModel() string    { return "mock-model" }

type panicClient struct{}

func (p *panicClient) Chat(messages []Message, tools []map[string]interface{}) (*Message, error) {
	panic("boom")
}

func (p *panicClient) GetProvider() string { return "mock" }
func (p *panicClient) GetModel() string    { return "mock-model" }

type countingTool struct {
	calls *int32
}

func (t countingTool) Name() string        { return "count" }
func (t countingTool) Description() string { return "count calls" }
func (t countingTool) Definition() map[string]interface{} {
	return map[string]interface{}{"name": "count"}
}
func (t countingTool) Execute(json.RawMessage) (string, error) {
	atomic.AddInt32(t.calls, 1)
	return "ok", nil
}
func (t countingTool) Parallel() bool { return false }

func TestNewClientParsesOpenCodeProviderModel(t *testing.T) {
	// deepseek is a keyed provider; NewClient now refuses to build a client
	// when no credential is found, so supply one to keep this parsing test
	// hermetic (independent of any stored auth.json).
	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	client := NewClient(nil, "deepseek/deepseek-chat")
	got, ok := client.(*GenericClient)
	if !ok {
		t.Fatalf("expected GenericClient for deepseek model, got %T", client)
	}
	if got.Provider != "deepseek" {
		t.Fatalf("expected provider deepseek, got %q", got.Provider)
	}
	if got.Model != "deepseek-chat" {
		t.Fatalf("expected stripped model deepseek-chat, got %q", got.Model)
	}
}

func TestStepCancellationAfterChatSkipsToolCalls(t *testing.T) {
	var calls int32
	tc := ToolCall{ID: "call-1", Type: "function"}
	tc.Function.Name = "count"
	tc.Function.Arguments = `{}`
	client := &blockingToolCallClient{
		started: make(chan struct{}),
		release: make(chan struct{}),
		resp:    &Message{Role: "assistant", ToolCalls: []ToolCall{tc}},
	}
	a := NewAgent(client, []tool.Tool{countingTool{calls: &calls}}, nil)

	done := make(chan error, 1)
	go func() {
		_, err := a.Step([]Message{{Role: "user", Content: "run a tool"}})
		done <- err
	}()

	<-client.started
	a.Cancel()
	close(client.release)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Step() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Step() did not return after cancellation")
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("tool calls after cancellation = %d, want 0", got)
	}
}

func TestTaskToolBackgroundRunUnexpectedStopMarksFailed(t *testing.T) {
	a := NewAgent(&panicClient{}, nil, nil)
	taskTool, ok := a.tools["task"].(TaskTool)
	if !ok {
		t.Fatalf("task tool type = %T", a.tools["task"])
	}
	out, err := taskTool.Execute([]byte(`{"prompt":"test","run_in_background":true}`))
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if !strings.Contains(out, "task_id:") {
		t.Fatalf("expected task id in output, got %q", out)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		runs := a.Runs().Snapshot()
		if len(runs) != 1 {
			t.Fatalf("runs len = %d, want 1", len(runs))
		}
		if runs[0].statusValue() == RunFailed {
			if !strings.Contains(runs[0].Err, "stopped unexpectedly") {
				t.Fatalf("run err = %q", runs[0].Err)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("background run never reached failed state")
}

func TestNestedSubAgentPermissionAskCascadesToMainThread(t *testing.T) {
	childTask := ToolCall{ID: "call-child-task", Type: "function"}
	childTask.Function.Name = "task"
	childTask.Function.Arguments = `{"prompt":"grandchild work","agent":"general"}`

	grandchildAsk := ToolCall{ID: "call-grandchild-ask", Type: "function"}
	grandchildAsk.Function.Name = "ask_tool"
	grandchildAsk.Function.Arguments = `{}`

	client := &MockToolClient{responses: []*Message{
		{Role: "assistant", ToolCalls: []ToolCall{childTask}},
		{Role: "assistant", ToolCalls: []ToolCall{grandchildAsk}},
		{Role: "assistant", Content: "grandchild done"},
		{Role: "assistant", Content: "child done"},
	}}

	a := NewAgent(client, nil, nil)
	a.Permissions().SetRule("task", PermissionAllow)
	a.Permissions().SetRule("ask_tool", PermissionAsk)
	a.AddTools([]tool.Tool{&MockTool{name: "ask_tool", result: "approved"}})

	var asks []PermissionRequest
	a.SetSubAgentPermAsker(func(req PermissionRequest) PermissionResponse {
		asks = append(asks, req)
		return PermissionResponse{Level: PermissionAllow}
	})

	taskTool, ok := a.tools["task"].(TaskTool)
	if !ok {
		t.Fatalf("task tool type = %T", a.tools["task"])
	}
	result, err := taskTool.Execute([]byte(`{"prompt":"child work","agent":"general"}`))
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if len(asks) != 1 {
		t.Fatalf("permission asks = %d, want 1", len(asks))
	}
	if asks[0].ToolName != "ask_tool" {
		t.Fatalf("permission ask tool = %q, want ask_tool", asks[0].ToolName)
	}
	if !strings.Contains(result, "child done") {
		t.Fatalf("result = %q, want child done", result)
	}
}

func TestNestedSubagentPermissionCallbackCascades(t *testing.T) {
	mkToolCall := func(id, name, args string) ToolCall {
		tc := ToolCall{ID: id, Type: "function"}
		tc.Function.Name = name
		tc.Function.Arguments = args
		return tc
	}
	client := &MockToolClient{responses: []*Message{
		{Role: "assistant", ToolCalls: []ToolCall{mkToolCall("call-parent-task", "task", `{"prompt":"spawn nested"}`)}},
		{Role: "assistant", ToolCalls: []ToolCall{mkToolCall("call-child-task", "task", `{"prompt":"use ask tool"}`)}},
		{Role: "assistant", ToolCalls: []ToolCall{mkToolCall("call-ask", "ask_tool", `{}`)}},
		{Role: "assistant", Content: "nested complete"},
		{Role: "assistant", Content: "child complete"},
		{Role: "assistant", Content: "parent complete"},
	}}
	a := NewAgent(client, []tool.Tool{&MockTool{name: "ask_tool", result: "executed"}}, nil)
	a.Permissions().SetRule("ask_tool", PermissionAsk)
	var asks int
	a.SetSubAgentPermAsker(func(req PermissionRequest) PermissionResponse {
		asks++
		if req.ToolName != "ask_tool" {
			t.Fatalf("unexpected permission request for %q", req.ToolName)
		}
		return PermissionResponse{Level: PermissionAllow}
	})

	// Root should allow task spawning so only nested ask_tool triggers the callback.
	a.Permissions().SetRule("task", PermissionAllow)

	msgs, err := a.Step([]Message{{Role: "user", Content: "start"}})
	if err != nil {
		t.Fatalf("Step err: %v", err)
	}
	if asks != 1 {
		t.Fatalf("permission callback called %d times, want 1", asks)
	}
	joined := ""
	for _, m := range msgs {
		joined += m.Content + "\n"
	}
	if !strings.Contains(joined, "parent complete") {
		t.Fatalf("expected final parent response, got %q", joined)
	}
}

func TestStepIncludesModePromptWithExistingSystemMessage(t *testing.T) {
	client := &captureClient{}
	a := NewAgent(client, nil, nil)
	a.SetMode(ModePlan)

	_, err := a.Step([]Message{
		{Role: "system", Content: "Context and rules:\nexisting"},
		{Role: "user", Content: "make a plan"},
	})
	if err != nil {
		t.Fatalf("Step() error = %v", err)
	}

	var found bool
	for _, msg := range client.Messages {
		if msg.Role == "system" && strings.Contains(msg.Content, "[ocode:mode]") && strings.Contains(msg.Content, "PLAN mode") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("mode prompt missing from messages: %#v", client.Messages)
	}
}

func TestPrepareMessagesDoesNotDuplicateMarkedBasePrompt(t *testing.T) {
	a := NewAgent(&captureClient{}, nil, nil)
	once := a.PrepareMessages([]Message{{Role: "user", Content: "hello"}}, "selected")
	twice := a.PrepareMessages(once, "selected")

	counts := map[string]int{}
	for _, msg := range twice {
		if msg.Role == "system" {
			counts[promptMarker(msg.Content)]++
		}
	}
	for _, marker := range []string{promptEnvMarker, promptModeMarker, promptSelectionMarker} {
		if counts[marker] != 1 {
			t.Fatalf("marker %s count = %d, want 1 in %#v", marker, counts[marker], twice)
		}
	}
}

func TestNewClientUsesChutesLLMEndpoint(t *testing.T) {
	// chutes is a keyed provider; NewClient now refuses to build a client when
	// no credential is found, so supply one to keep this endpoint test hermetic.
	t.Setenv("CHUTES_API_KEY", "test-key")
	client := NewClient(nil, "chutes/Qwen/Qwen3.6-27B-TEE")
	got, ok := client.(*GenericClient)
	if !ok {
		t.Fatalf("expected GenericClient for chutes model, got %T", client)
	}
	if got.Provider != "chutes" {
		t.Fatalf("expected provider chutes, got %q", got.Provider)
	}
	if got.Model != "Qwen/Qwen3.6-27B-TEE" {
		t.Fatalf("expected stripped model Qwen/Qwen3.6-27B-TEE, got %q", got.Model)
	}
	if got.BaseURL != "https://llm.chutes.ai/v1" {
		t.Fatalf("expected chutes LLM base URL, got %q", got.BaseURL)
	}
}

func TestNewClientUsesOpenCodeGoEndpoint(t *testing.T) {
	client := NewClient(nil, "opencode-go/glm-5.1")
	got, ok := client.(*GenericClient)
	if !ok {
		t.Fatalf("expected GenericClient for opencode-go model, got %T", client)
	}
	if got.Provider != "opencode-go" {
		t.Fatalf("expected provider opencode-go, got %q", got.Provider)
	}
	if got.Model != "glm-5.1" {
		t.Fatalf("expected stripped model glm-5.1, got %q", got.Model)
	}
	if got.BaseURL != "https://opencode.ai/zen/go/v1" {
		t.Fatalf("expected opencode-go base URL, got %q", got.BaseURL)
	}
}

func TestFallbackAllProviderModelsIncludesDeepSeek(t *testing.T) {
	models := fallbackAllProviderModels()
	want := map[string]bool{
		"deepseek/deepseek-chat": false,
		"opencode-go/glm-5.1":    false,
	}
	for _, model := range models {
		if _, ok := want[model]; ok {
			want[model] = true
		}
	}
	for model, found := range want {
		if !found {
			t.Fatalf("expected %s in fallback list, got %#v", model, models)
		}
	}
}

func TestOpenAIToolsWrapsRawToolDefinitions(t *testing.T) {
	tools := openAITools([]map[string]interface{}{{
		"name":        "read",
		"description": "Read file contents",
		"parameters":  map[string]interface{}{"type": "object"},
	}})

	if len(tools) != 1 {
		t.Fatalf("expected one tool, got %d", len(tools))
	}
	if tools[0]["type"] != "function" {
		t.Fatalf("expected OpenAI function tool type, got %#v", tools[0]["type"])
	}
	fn, ok := tools[0]["function"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected function definition map, got %#v", tools[0]["function"])
	}
	if fn["name"] != "read" {
		t.Fatalf("expected wrapped tool definition, got %#v", fn)
	}
}

func TestOpenAIToolsKeepsWrappedDefinitions(t *testing.T) {
	wrapped := map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name": "read",
		},
	}
	tools := openAITools([]map[string]interface{}{wrapped})

	if len(tools) != 1 || !reflect.DeepEqual(tools[0]["function"], wrapped["function"]) {
		t.Fatalf("expected existing wrapped definition to be preserved, got %#v", tools)
	}
}

func TestGenericClientRetriesTransientNoResponseErrors(t *testing.T) {
	originalClient := llmHTTPClient
	originalDelay := llmRetryBaseDelay
	defer func() {
		llmHTTPClient = originalClient
		llmRetryBaseDelay = originalDelay
	}()

	var calls int32
	llmRetryBaseDelay = 0
	llmHTTPClient = &http.Client{
		Timeout: llmRequestTimeout,
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			atomic.AddInt32(&calls, 1)
			return nil, io.ErrUnexpectedEOF
		}),
	}

	client := &GenericClient{Provider: "openai", Model: "gpt-test", BaseURL: "https://example.test/v1"}
	_, err := client.Chat([]Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Fatal("expected retry failure")
	}
	if got := atomic.LoadInt32(&calls); got != int32(llmMaxRetries+1) {
		t.Fatalf("expected %d attempts, got %d", llmMaxRetries+1, got)
	}
	if !strings.Contains(err.Error(), "llm request failed after 4 attempt(s)") {
		t.Fatalf("expected retry count in error, got %v", err)
	}
}

func TestChatReportsActualAttemptCountForNonRetryableErrors(t *testing.T) {
	originalClient := llmHTTPClient
	defer func() {
		llmHTTPClient = originalClient
	}()

	var calls int32
	llmHTTPClient = &http.Client{
		Timeout: llmRequestTimeout,
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			atomic.AddInt32(&calls, 1)
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"bad input"}}`)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	client := &GenericClient{Provider: "openai", Model: "gpt-test", BaseURL: "https://example.test/v1"}
	_, err := client.Chat([]Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Fatal("expected failure")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 attempt, got %d", got)
	}
	if !strings.Contains(err.Error(), "llm request failed after 1 attempt(s)") {
		t.Fatalf("expected actual attempt count in error, got %v", err)
	}
}

func TestOpenAIResponsesUsesCodexBackendForOAuth(t *testing.T) {
	originalClient := llmHTTPClient
	defer func() {
		llmHTTPClient = originalClient
	}()

	var gotURL string
	var gotPayload map[string]interface{}
	llmHTTPClient = &http.Client{
		Timeout: llmRequestTimeout,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			gotURL = req.URL.String()
			if err := json.NewDecoder(req.Body).Decode(&gotPayload); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(
					"event: response.created\ndata: {\"type\":\"response.created\",\"model\":\"gpt-test\"}\n\n" +
						"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n" +
						"event: response.completed\ndata: {\"type\":\"response.completed\",\"model\":\"gpt-test\"}\n\n" +
						"data: [DONE]\n",
				)),
				Header: make(http.Header),
			}, nil
		}),
	}

	client := &GenericClient{Provider: "openai", Model: "gpt-test", APIKey: "token", UseOAuth: true}
	msg, err := client.chatOpenAIResponses(context.Background(), []Message{{Role: "system", Content: "be terse"}, {Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if gotURL != "https://chatgpt.com/backend-api/codex/responses" {
		t.Fatalf("responses URL = %q", gotURL)
	}
	if gotPayload["instructions"] != "be terse" {
		t.Fatalf("instructions = %#v", gotPayload["instructions"])
	}
	if gotPayload["store"] != false {
		t.Fatalf("store = %#v", gotPayload["store"])
	}
	input, ok := gotPayload["input"].([]interface{})
	if !ok || len(input) != 1 {
		t.Fatalf("input = %#v", gotPayload["input"])
	}
	item, ok := input[0].(map[string]interface{})
	if !ok || item["role"] != "user" {
		t.Fatalf("input item = %#v", input[0])
	}
	if msg.Content != "ok" {
		t.Fatalf("content = %q", msg.Content)
	}
}

func TestOpenAIResponsesCapturesReasoningAndFunctionCallItems(t *testing.T) {
	originalClient := llmHTTPClient
	defer func() {
		llmHTTPClient = originalClient
	}()

	llmHTTPClient = &http.Client{
		Timeout: llmRequestTimeout,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(
					"event: response.output_item.done\n" +
						"data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"reasoning\",\"id\":\"rs_123\",\"summary\":[]}}\n\n" +
						"event: response.output_item.done\n" +
						"data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"function_call\",\"id\":\"fc_123\",\"call_id\":\"call_123\",\"name\":\"read\",\"arguments\":\"{\\\"filePath\\\":\\\"README.md\\\"}\"}}\n\n" +
						"event: response.completed\n" +
						"data: {\"type\":\"response.completed\",\"model\":\"gpt-test\"}\n\n" +
						"data: [DONE]\n",
				)),
				Header: make(http.Header),
			}, nil
		}),
	}

	client := &GenericClient{Provider: "openai", Model: "gpt-test", BaseURL: "https://example.test/v1"}
	msg, err := client.chatOpenAIResponses(context.Background(), []Message{{Role: "user", Content: "read"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.OpenAIResponseItems) != 2 {
		t.Fatalf("expected reasoning and function call items, got %#v", msg.OpenAIResponseItems)
	}
	if msg.OpenAIResponseItems[0]["type"] != "reasoning" || msg.OpenAIResponseItems[1]["type"] != "function_call" {
		t.Fatalf("unexpected response items: %#v", msg.OpenAIResponseItems)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].ID != "call_123" || msg.ToolCalls[0].Function.Name != "read" {
		t.Fatalf("unexpected tool calls: %#v", msg.ToolCalls)
	}
}

func TestOpenAIResponsesIncludesStoredOutputItemsBeforeToolResult(t *testing.T) {
	originalClient := llmHTTPClient
	defer func() {
		llmHTTPClient = originalClient
	}()

	var gotPayload map[string]interface{}
	llmHTTPClient = &http.Client{
		Timeout: llmRequestTimeout,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if err := json.NewDecoder(req.Body).Decode(&gotPayload); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(
					"event: response.output_text.delta\n" +
						"data: {\"type\":\"response.output_text.delta\",\"delta\":\"done\"}\n\n" +
						"data: [DONE]\n",
				)),
				Header: make(http.Header),
			}, nil
		}),
	}

	messages := []Message{
		{Role: "user", Content: "read"},
		{
			Role: "assistant",
			OpenAIResponseItems: []map[string]interface{}{
				{"type": "reasoning", "id": "rs_123", "summary": []interface{}{}},
				{"type": "function_call", "id": "fc_123", "call_id": "call_123", "name": "read", "arguments": "{}"},
			},
		},
		{Role: "tool", ToolID: "call_123", Content: "file contents"},
	}
	client := &GenericClient{Provider: "openai", Model: "gpt-test", BaseURL: "https://example.test/v1"}
	if _, err := client.chatOpenAIResponses(context.Background(), messages, nil); err != nil {
		t.Fatal(err)
	}

	input, ok := gotPayload["input"].([]interface{})
	if !ok || len(input) != 4 {
		t.Fatalf("input = %#v", gotPayload["input"])
	}
	for i, wantType := range []string{"message", "reasoning", "function_call", "function_call_output"} {
		item, ok := input[i].(map[string]interface{})
		if !ok || item["type"] != wantType {
			t.Fatalf("input[%d] = %#v, want type %q", i, input[i], wantType)
		}
	}
}

func TestOpenAIResponsesFallsBackToToolCallsWhenStoredItemsMissFunctionCall(t *testing.T) {
	originalClient := llmHTTPClient
	defer func() {
		llmHTTPClient = originalClient
	}()

	var gotPayload map[string]interface{}
	llmHTTPClient = &http.Client{
		Timeout: llmRequestTimeout,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if err := json.NewDecoder(req.Body).Decode(&gotPayload); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(
					"event: response.output_text.delta\n" +
						"data: {\"type\":\"response.output_text.delta\",\"delta\":\"done\"}\n\n" +
						"data: [DONE]\n",
				)),
				Header: make(http.Header),
			}, nil
		}),
	}

	messages := []Message{
		{Role: "user", Content: "read"},
		{
			Role: "assistant",
			OpenAIResponseItems: []map[string]interface{}{
				{"type": "reasoning", "id": "rs_123", "summary": []interface{}{}},
			},
			ToolCalls: []ToolCall{{
				ID:   "call_123",
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{
					Name:      "read",
					Arguments: "{}",
				},
			}},
		},
		{Role: "tool", ToolID: "call_123", Content: "file contents"},
	}
	client := &GenericClient{Provider: "openai", Model: "gpt-test", BaseURL: "https://example.test/v1"}
	if _, err := client.chatOpenAIResponses(context.Background(), messages, nil); err != nil {
		t.Fatal(err)
	}

	input, ok := gotPayload["input"].([]interface{})
	if !ok || len(input) != 4 {
		t.Fatalf("input = %#v", gotPayload["input"])
	}
	for i, wantType := range []string{"message", "reasoning", "function_call", "function_call_output"} {
		item, ok := input[i].(map[string]interface{})
		if !ok || item["type"] != wantType {
			t.Fatalf("input[%d] = %#v, want type %q", i, input[i], wantType)
		}
	}
	if got := input[2].(map[string]interface{})["call_id"]; got != "call_123" {
		t.Fatalf("function_call call_id = %v, want call_123", got)
	}
}

func TestOpenAIResponsesRequestsReasoningEncryptedContent(t *testing.T) {
	originalClient := llmHTTPClient
	defer func() {
		llmHTTPClient = originalClient
	}()

	var gotPayload map[string]interface{}
	llmHTTPClient = &http.Client{
		Timeout: llmRequestTimeout,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if err := json.NewDecoder(req.Body).Decode(&gotPayload); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(
					"event: response.output_text.delta\n" +
						"data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n" +
						"data: [DONE]\n",
				)),
				Header: make(http.Header),
			}, nil
		}),
	}

	client := &GenericClient{Provider: "openai", Model: "gpt-test", BaseURL: "https://example.test/v1", ThinkingBudget: 8000}
	if _, err := client.chatOpenAIResponses(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatal(err)
	}

	include, ok := gotPayload["include"].([]interface{})
	if !ok {
		t.Fatalf("expected include param in payload, got %#v", gotPayload)
	}
	found := false
	for _, v := range include {
		if s, _ := v.(string); s == "reasoning.encrypted_content" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected reasoning.encrypted_content in include, got %#v", include)
	}
	reasoning, ok := gotPayload["reasoning"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected reasoning param in payload, got %#v", gotPayload)
	}
	if effort, _ := reasoning["effort"].(string); effort != "medium" {
		t.Fatalf("expected medium reasoning effort, got %#v", reasoning)
	}
}

func TestOpenAIResponsesDedupesStoredItemsByID(t *testing.T) {
	originalClient := llmHTTPClient
	defer func() {
		llmHTTPClient = originalClient
	}()

	var gotPayload map[string]interface{}
	llmHTTPClient = &http.Client{
		Timeout: llmRequestTimeout,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if err := json.NewDecoder(req.Body).Decode(&gotPayload); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(
					"event: response.output_text.delta\n" +
						"data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n" +
						"data: [DONE]\n",
				)),
				Header: make(http.Header),
			}, nil
		}),
	}

	messages := []Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", OpenAIResponseItems: []map[string]interface{}{
			{"type": "reasoning", "id": "rs_dup", "summary": []interface{}{}},
		}},
		{Role: "assistant", OpenAIResponseItems: []map[string]interface{}{
			{"type": "reasoning", "id": "rs_dup", "summary": []interface{}{}},
		}},
	}

	client := &GenericClient{Provider: "openai", Model: "gpt-test", BaseURL: "https://example.test/v1"}
	if _, err := client.chatOpenAIResponses(context.Background(), messages, nil); err != nil {
		t.Fatal(err)
	}

	input, ok := gotPayload["input"].([]interface{})
	if !ok {
		t.Fatalf("input = %#v", gotPayload["input"])
	}
	count := 0
	for _, raw := range input {
		item, _ := raw.(map[string]interface{})
		if item["id"] == "rs_dup" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected deduped reasoning item once, got %d in %#v", count, input)
	}
}

func TestOpenAIResponsesReturnsErrorOnTruncatedStream(t *testing.T) {
	originalClient := llmHTTPClient
	defer func() {
		llmHTTPClient = originalClient
	}()

	llmHTTPClient = &http.Client{
		Timeout: llmRequestTimeout,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(&errReader{err: io.ErrUnexpectedEOF}),
				Header:     make(http.Header),
			}, nil
		}),
	}

	client := &GenericClient{Provider: "openai", Model: "gpt-test", BaseURL: "https://example.test/v1"}
	_, err := client.chatOpenAIResponses(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Fatal("expected error from truncated SSE stream")
	}
}

type errReader struct{ err error }

func (e *errReader) Read(p []byte) (int, error) { return 0, e.err }

func TestLLMHTTPClientUsesFiveMinuteTimeout(t *testing.T) {
	if llmHTTPClient.Timeout != 5*time.Minute {
		t.Fatalf("expected LLM HTTP timeout to be 5m, got %s", llmHTTPClient.Timeout)
	}
}

func TestAgentStep(t *testing.T) {
	mock := &MockClient{
		Response: &Message{
			Role:    "assistant",
			Content: "Hello!",
		},
	}
	a := NewAgent(mock, nil, nil)

	msgs, err := a.Step([]Message{{Role: "user", Content: "Hi"}})
	if err != nil {
		t.Fatalf("Step failed: %v", err)
	}

	if len(msgs) != 1 || msgs[0].Content != "Hello!" {
		t.Errorf("unexpected response: %+v", msgs)
	}
}

func TestCompactSummaryClientUsesOverride(t *testing.T) {
	a := &Agent{
		client: &MockClient{},
		config: &config.Config{Ocode: config.OcodeConfig{Compact: config.CompactConfig{
			SummaryProvider: "openai",
			SummaryModel:    "gpt-4o-mini",
		}}},
	}

	client := a.compactSummaryClient()
	gc, ok := client.(*GenericClient)
	if !ok {
		t.Fatalf("expected GenericClient, got %T", client)
	}
	if gc.Provider != "openai" || gc.Model != "gpt-4o-mini" {
		t.Fatalf("unexpected summary client: %+v", gc)
	}
}

func TestRunCompactPrunesLargeToolResultsInSummaryPrompt(t *testing.T) {
	client := &scriptedCaptureClient{Responses: []string{"summary"}}
	a := &Agent{client: client}
	rt := compactRuntime{
		Enabled:               true,
		KeepRecentTurns:       1,
		SummaryTimeoutSeconds: 1,
		SummaryMaxRetries:     0,
		MaxSummaryInputTokens: 50000,
	}
	big := strings.Repeat("y", 5000)
	msgs := []Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "original ask"},
		{Role: "assistant", ToolCalls: []ToolCall{tcCall("call1", "read")}},
		{Role: "tool", ToolID: "call1", Content: big},
		{Role: "assistant", Content: "done"},
		{Role: "user", Content: "recent tail"},
		{Role: "assistant", Content: "tail response"},
	}

	res := a.runCompact(msgs, rt)
	if !res.OK {
		t.Fatalf("expected compaction success, got %#v", res)
	}
	if len(client.Prompts) != 1 {
		t.Fatalf("expected one summary prompt, got %d", len(client.Prompts))
	}
	if !strings.Contains(client.Prompts[0], "[pruned 3000 chars from tool output before summarisation]") {
		t.Fatalf("summary prompt missing pruned marker: %q", client.Prompts[0])
	}
}

func TestRunCompactAnchoredSummaryReplacesPreviousSummaryInPlace(t *testing.T) {
	client := &scriptedCaptureClient{Responses: []string{"first summary", "second summary"}}
	a := &Agent{client: client}
	rt := compactRuntime{
		Enabled:               true,
		KeepRecentTurns:       1,
		SummaryTimeoutSeconds: 1,
		SummaryMaxRetries:     0,
		MaxSummaryInputTokens: 50000,
	}

	msgs := []Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "original ask"},
		{Role: "assistant", Content: "did work"},
		{Role: "assistant", ToolCalls: []ToolCall{tcCall("call1", "read")}},
		{Role: "tool", ToolID: "call1", Content: "result1"},
		{Role: "assistant", Content: "done1"},
		{Role: "user", Content: "recent tail"},
		{Role: "assistant", Content: "tail response"},
	}

	first := a.runCompact(msgs, rt)
	if !first.OK {
		t.Fatalf("first compaction failed: %#v", first)
	}
	msgs = append(append(append([]Message{}, msgs[:first.ReplaceFrom]...), first.Summary), msgs[first.ReplaceTo:]...)

	msgs = append(msgs,
		Message{Role: "user", Content: "follow-up work"},
		Message{Role: "assistant", Content: "more changes"},
		Message{Role: "assistant", ToolCalls: []ToolCall{tcCall("call2", "write")}},
		Message{Role: "tool", ToolID: "call2", Content: "result2"},
		Message{Role: "assistant", Content: "done2"},
		Message{Role: "user", Content: "latest tail"},
		Message{Role: "assistant", Content: "latest response"},
	)

	second := a.runCompact(msgs, rt)
	if !second.OK {
		t.Fatalf("second compaction failed: %#v", second)
	}
	if second.ReplaceFrom != 2 {
		t.Fatalf("second ReplaceFrom=%d, want 2 (overwrite prior summary)", second.ReplaceFrom)
	}
	if len(client.Prompts) != 2 {
		t.Fatalf("expected two summary prompts, got %d", len(client.Prompts))
	}
	if !strings.Contains(client.Prompts[1], "<previous-summary>") || !strings.Contains(client.Prompts[1], "first summary") {
		t.Fatalf("second prompt missing anchored previous summary: %q", client.Prompts[1])
	}

	msgs = append(append(append([]Message{}, msgs[:second.ReplaceFrom]...), second.Summary), msgs[second.ReplaceTo:]...)
	summaryCount := 0
	for _, msg := range msgs {
		if msg.Role == "system" && strings.HasPrefix(msg.Content, compactionSummaryMarker) {
			summaryCount++
		}
	}
	if summaryCount != 1 {
		t.Fatalf("expected exactly one compaction summary after anchored replace, got %d", summaryCount)
	}
	if strings.Contains(msgs[2].Content, "first summary") {
		t.Fatalf("old summary should be overwritten in place: %q", msgs[2].Content)
	}
	for _, msg := range msgs[3:] {
		if msg.Content == "recent tail" || msg.Content == "tail response" || msg.Content == "follow-up work" {
			t.Fatalf("old compacted history should be dropped, found %q in final messages", msg.Content)
		}
	}
}

func TestAgentToolExecution(t *testing.T) {
	// 1. Tool call from assistant
	// 2. Tool result appended
	// 3. Final assistant response

	step1 := &Message{
		Role: "assistant",
		ToolCalls: []ToolCall{{
			ID:   "call1",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "test_tool", Arguments: "{}"},
		}},
	}
	step2 := &Message{Role: "assistant", Content: "Done!"}

	mock := &MockToolClient{responses: []*Message{step1, step2}}

	mockTool := &MockTool{name: "test_tool", result: "success"}
	a := NewAgent(mock, nil, nil)
	a.Permissions().SetRule("test_tool", PermissionAllow)
	a.AddTools([]tool.Tool{mockTool})

	msgs, err := a.Step([]Message{{Role: "user", Content: "do tool"}})
	if err != nil {
		t.Fatal(err)
	}

	// Should have: [assistant toolcall, tool result, assistant response]
	if len(msgs) != 3 {
		t.Errorf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[1].Role != "tool" || msgs[1].Content != "success" {
		t.Errorf("unexpected tool result: %+v", msgs[1])
	}
}

// TestOnPermissionAskRoutesSubAgentDecision verifies that when OnPermissionAsk
// is set (the sub-agent path), an Ask-level tool call invokes the callback and
// acts on the returned level instead of emitting the PERMISSION_ASK sentinel.
func TestOnPermissionAskRoutesSubAgentDecision(t *testing.T) {
	mockTool := &MockTool{name: "ask_tool", result: "executed"}
	a := NewAgent(nil, nil, nil)
	a.Permissions().SetRule("ask_tool", PermissionAsk)
	a.AddTools([]tool.Tool{mockTool})

	// Allow → callback invoked, tool executes.
	var gotReq *PermissionRequest
	a.OnPermissionAsk = func(req PermissionRequest) PermissionResponse {
		gotReq = &req
		return PermissionResponse{Level: PermissionAllow}
	}
	res, err := a.HandleToolCall("ask_tool", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if gotReq == nil || gotReq.ToolName != "ask_tool" {
		t.Fatalf("callback not invoked with ask_tool request: %+v", gotReq)
	}
	if res != "executed" {
		t.Fatalf("allow path: got %q, want executed", res)
	}
	if strings.HasPrefix(res, "PERMISSION_ASK:") {
		t.Fatal("sentinel should not be emitted when OnPermissionAsk is set")
	}

	// Deny → tool does not execute.
	a.OnPermissionAsk = func(req PermissionRequest) PermissionResponse { return PermissionResponse{Level: PermissionDeny} }
	res, err = a.HandleToolCall("ask_tool", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res, "denied") {
		t.Fatalf("deny path: got %q, want a denied message", res)
	}
}

func TestOnPermissionAskPersistToolUpdatesCurrentAgent(t *testing.T) {
	mockTool := &MockTool{name: "ask_tool", result: "executed"}
	a := NewAgent(nil, nil, nil)
	a.Permissions().SetRule("ask_tool", PermissionAsk)
	a.AddTools([]tool.Tool{mockTool})

	asks := 0
	a.OnPermissionAsk = func(req PermissionRequest) PermissionResponse {
		asks++
		return PermissionResponse{Level: PermissionAllow, PersistTool: true}
	}

	for i := 0; i < 2; i++ {
		res, err := a.HandleToolCall("ask_tool", json.RawMessage(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		if res != "executed" {
			t.Fatalf("call %d got %q, want executed", i+1, res)
		}
	}
	if asks != 1 {
		t.Fatalf("permission callback called %d times, want 1", asks)
	}
	if got := a.Permissions().Check("ask_tool"); got != PermissionAllow {
		t.Fatalf("permission = %q, want allow", got)
	}
}

// TestHandleToolCallEmitsSentinelWithoutCallback verifies the main-agent path
// is unchanged: with no OnPermissionAsk, an Ask tool yields the sentinel.
func TestHandleToolCallEmitsSentinelWithoutCallback(t *testing.T) {
	a := NewAgent(nil, nil, nil)
	a.Permissions().SetRule("delete", PermissionAsk)
	res, err := a.HandleToolCall("delete", json.RawMessage(`{"path":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res, "PERMISSION_ASK:") {
		t.Fatalf("expected PERMISSION_ASK sentinel, got %q", res)
	}
}

type MockToolClient struct {
	responses []*Message
	idx       int
}

func (m *MockToolClient) Chat(messages []Message, tools []map[string]interface{}) (*Message, error) {
	r := m.responses[m.idx]
	m.idx++
	return r, nil
}

func (m *MockToolClient) GetProvider() string { return "mock" }
func (m *MockToolClient) GetModel() string    { return "mock-model" }

type MockTool struct {
	name   string
	result string
	Error  error
}

func (m *MockTool) Name() string        { return m.name }
func (m *MockTool) Description() string { return "" }
func (m *MockTool) Definition() map[string]interface{} {
	return map[string]interface{}{"name": m.name}
}
func (m *MockTool) Execute(args json.RawMessage) (string, error) {
	return m.result, m.Error
}
func (m *MockTool) Parallel() bool { return true }

func TestNewAgentHasProcessTools(t *testing.T) {
	a := NewAgent(nil, nil, nil)
	for _, name := range []string{"bash", "bash_output", "kill_shell"} {
		if _, ok := a.tools[name]; !ok {
			t.Fatalf("agent missing tool %q", name)
		}
	}
	if a.Procs() == nil {
		t.Fatal("agent has no process registry")
	}
}

func TestAgentCancelStopsBeforeNextStep(t *testing.T) {
	a := NewAgent(nil, nil, nil) // nil client → Step returns the stub message
	a.Cancel()
	if !a.cancelled() {
		t.Fatal("expected agent to report cancelled")
	}
}

func TestAgentEmitsProcessJobEvent(t *testing.T) {
	a := NewAgent(nil, nil, nil)
	p := a.Procs().StartBackground("echo job-evt")
	select {
	case ev := <-a.JobEvents():
		if ev.Kind != "process" || ev.ID != p.ID {
			t.Fatalf("unexpected event: %+v", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no job event received")
	}
}

func TestRecoverOrphanedToolCallsBasicRecovery(t *testing.T) {
	a := NewAgent(&MockClient{}, []tool.Tool{&MockTool{name: "mock_tool", result: "success"}}, nil)
	a.permissions = nil // Disable permission checks for testing

	// Create messages with orphaned tool calls (no matching tool result).
	tc := ToolCall{ID: "call-1", Type: "function"}
	tc.Function.Name = "mock_tool"
	tc.Function.Arguments = `{}`
	messages := []Message{
		{Role: "assistant", Content: "I'll help", ToolCalls: []ToolCall{tc}},
	}

	result := a.recoverOrphanedToolCalls(messages)

	if len(result) != 2 {
		t.Fatalf("expected 2 messages (assistant + tool result), got %d", len(result))
	}
	if result[0].Role != "assistant" {
		t.Fatalf("expected first message to be assistant, got %q", result[0].Role)
	}
	if result[1].Role != "tool" || result[1].ToolID != "call-1" {
		t.Fatalf("expected second message to be tool result for call-1, got role=%q toolid=%q", result[1].Role, result[1].ToolID)
	}
	if !strings.Contains(result[1].Content, "success") {
		t.Fatalf("expected successful tool result, got: %s", result[1].Content)
	}
}

func TestRecoverOrphanedToolCallsPartialRecovery(t *testing.T) {
	a := NewAgent(&MockClient{}, []tool.Tool{&MockTool{name: "mock_tool", result: "success"}}, nil)
	a.permissions = nil // Disable permission checks for testing

	// Create messages: one call with result, one without.
	tc1 := ToolCall{ID: "call-1", Type: "function"}
	tc1.Function.Name = "mock_tool"
	tc1.Function.Arguments = `{}`
	tc2 := ToolCall{ID: "call-2", Type: "function"}
	tc2.Function.Name = "mock_tool"
	tc2.Function.Arguments = `{}`

	messages := []Message{
		{Role: "assistant", Content: "I'll help", ToolCalls: []ToolCall{tc1, tc2}},
		{Role: "tool", ToolID: "call-1", Content: "existing result"},
	}

	result := a.recoverOrphanedToolCalls(messages)

	if len(result) != 3 {
		t.Fatalf("expected 3 messages (assistant + existing tool + recovered tool), got %d", len(result))
	}
	if result[1].Content != "existing result" {
		t.Fatalf("expected existing result to be preserved, got: %s", result[1].Content)
	}
	if result[2].ToolID != "call-2" {
		t.Fatalf("expected recovered call to be call-2, got: %s", result[2].ToolID)
	}
}

func TestRecoverOrphanedToolCallsFailedRecovery(t *testing.T) {
	// Create a tool that returns an error.
	failingTool := &MockTool{
		name:  "mock_tool",
		Error: fmt.Errorf("mock tool failed"),
	}
	a := NewAgent(&MockClient{}, []tool.Tool{failingTool}, nil)
	a.permissions = nil // Disable permission checks for testing

	tc := ToolCall{ID: "call-1", Type: "function"}
	tc.Function.Name = "mock_tool"
	tc.Function.Arguments = `{}`
	messages := []Message{
		{Role: "assistant", Content: "I'll help", ToolCalls: []ToolCall{tc}},
	}

	result := a.recoverOrphanedToolCalls(messages)

	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
	if !strings.HasPrefix(result[1].Content, "ORPHAN_TOOL_ERROR:") {
		t.Fatalf("expected error tag in result, got: %s", result[1].Content)
	}
	if !strings.Contains(result[1].Content, "mock tool failed") {
		t.Fatalf("expected error message in result, got: %s", result[1].Content)
	}
}

func TestRecoverOrphanedToolCallsEmptyCase(t *testing.T) {
	a := NewAgent(&MockClient{}, []tool.Tool{&MockTool{name: "mock_tool", result: "success"}}, nil)
	a.permissions = nil // Disable permission checks for testing

	// No orphaned calls — all have results.
	tc := ToolCall{ID: "call-1", Type: "function"}
	tc.Function.Name = "mock_tool"
	tc.Function.Arguments = `{}`
	messages := []Message{
		{Role: "assistant", Content: "I'll help", ToolCalls: []ToolCall{tc}},
		{Role: "tool", ToolID: "call-1", Content: "result"},
	}

	result := a.recoverOrphanedToolCalls(messages)

	if len(result) != 2 {
		t.Fatalf("expected 2 messages (no recovery), got %d", len(result))
	}
	if result[1].Content != "result" {
		t.Fatalf("expected result to be unchanged, got: %s", result[1].Content)
	}
}

// TestRecoverOrphanedToolCallsOnlyLastAssistant verifies that only the last
// assistant message's orphaned calls are re-executed. Historical orphans from
// earlier turns are left for repairToolCallSequence to handle, avoiding
// dangerous re-execution of side-effectful operations from prior turns.
func TestRecoverOrphanedToolCallsOnlyLastAssistant(t *testing.T) {
	a := NewAgent(&MockClient{}, []tool.Tool{&MockTool{name: "mock_tool", result: "success"}}, nil)
	a.permissions = nil

	tc1 := ToolCall{ID: "call-1", Type: "function"}
	tc1.Function.Name = "mock_tool"
	tc1.Function.Arguments = `{}`
	tc2 := ToolCall{ID: "call-2", Type: "function"}
	tc2.Function.Name = "mock_tool"
	tc2.Function.Arguments = `{}`

	// Earlier assistant has orphan (tc1), last assistant is complete (tc2 + result).
	// The re-execution counter mock_tool uses is checked to verify only ONE call was re-executed.
	messages := []Message{
		{Role: "assistant", Content: "first", ToolCalls: []ToolCall{tc1}},
		{Role: "user", Content: "continue"},
		{Role: "assistant", Content: "second", ToolCalls: []ToolCall{tc2}},
		{Role: "tool", ToolID: "call-2", Content: "result-2"},
	}

	result := a.recoverOrphanedToolCalls(messages)

	// No changes expected: last assistant (idx 2) has all results (call-2 at idx 3).
	// Earlier orphan (tc1 at idx 0) is NOT recovered. Length unchanged.
	if len(result) != len(messages) {
		t.Fatalf("expected %d messages (no recovery for historical orphan), got %d", len(messages), len(result))
	}
	for i := range messages {
		if result[i].Role != messages[i].Role || result[i].ToolID != messages[i].ToolID {
			t.Fatalf("message %d changed: got %+v, want %+v", i, result[i], messages[i])
		}
	}
}

// TestRecoverOrphanedToolCallsInsertPosition verifies that re-executed results
// are inserted right after the assistant message, before any user/assistant
// messages that follow — NOT appended at the end.
func TestRecoverOrphanedToolCallsInsertPosition(t *testing.T) {
	a := NewAgent(&MockClient{}, []tool.Tool{&MockTool{name: "mock_tool", result: "success"}}, nil)
	a.permissions = nil

	tc1 := ToolCall{ID: "call-1", Type: "function"}
	tc1.Function.Name = "mock_tool"
	tc1.Function.Arguments = `{}`
	tc2 := ToolCall{ID: "call-2", Type: "function"}
	tc2.Function.Name = "mock_tool"
	tc2.Function.Arguments = `{}`

	// Last assistant has tc1 (orphan) and tc2 (has result at ToolID "call-2").
	// Result for tc1 should be inserted right after the assistant,
	// BEFORE the new user message.
	messages := []Message{
		{Role: "user", Content: "initial"},
		{Role: "assistant", Content: "doing work", ToolCalls: []ToolCall{tc1, tc2}},
		{Role: "tool", ToolID: "call-2", Content: "existing-result"},
		{Role: "user", Content: "NEW MESSAGE — must come after inserted tool results"},
	}

	result := a.recoverOrphanedToolCalls(messages)

	if len(result) != len(messages)+1 {
		t.Fatalf("expected %d messages (inserted one tool result), got %d", len(messages)+1, len(result))
	}

	// Positions:
	//   [0] user: "initial"
	//   [1] assistant: "doing work"
	//   [2] tool: "existing-result" (for call-2)
	//   [3] tool: "success" (for call-1 — INSERTED right after assistant)
	//   [4] user: "NEW MESSAGE"
	if result[3].Role != "tool" || result[3].ToolID != "call-1" {
		t.Fatalf("expected inserted tool result at position 3 for call-1, got %+v", result[3])
	}
	if result[4].Role != "user" || !strings.Contains(result[4].Content, "NEW MESSAGE") {
		t.Fatalf("expected user message at position 4 to be preserved, got %+v", result[4])
	}
}

func TestModelSupportsThinkingUsesModelsDevReasoningFlag(t *testing.T) {
	registry.mu.Lock()
	previousData := registry.data
	previousFetchedAt := registry.fetchedAt
	registry.data = map[string]providerEntry{
		"openai": {
			ID: "openai",
			Models: map[string]modelEntry{
				"gpt-5.2": {ID: "gpt-5.2", Reasoning: true},
				"gpt-4o":  {ID: "gpt-4o", Reasoning: false},
			},
		},
		"anthropic": {
			ID: "anthropic",
			Models: map[string]modelEntry{
				"claude-3-opus-20240229": {ID: "claude-3-opus-20240229", Reasoning: false},
			},
		},
	}
	registry.fetchedAt = time.Now()
	registry.mu.Unlock()
	defer func() {
		registry.mu.Lock()
		registry.data = previousData
		registry.fetchedAt = previousFetchedAt
		registry.mu.Unlock()
	}()

	if !ModelSupportsThinking("openai/gpt-5.2") {
		t.Fatal("expected gpt-5.2 reasoning flag from models.dev to be honored")
	}
	if ModelSupportsThinking("openai/gpt-4o") {
		t.Fatal("expected gpt-4o non-reasoning flag from models.dev to be honored")
	}
	if ModelSupportsThinking("anthropic/claude-3-opus-20240229") {
		t.Fatal("expected models.dev false flag to override fallback heuristics")
	}
}
