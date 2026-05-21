package agent

import (
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

func TestNewClientParsesOpenCodeProviderModel(t *testing.T) {
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

func TestNewClientUsesChutesLLMEndpoint(t *testing.T) {
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
	msg, err := client.chatOpenAIResponses([]Message{{Role: "system", Content: "be terse"}, {Role: "user", Content: "hi"}}, nil)
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
	msg, err := client.chatOpenAIResponses([]Message{{Role: "user", Content: "read"}}, nil)
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
	if _, err := client.chatOpenAIResponses(messages, nil); err != nil {
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
	if _, err := client.chatOpenAIResponses([]Message{{Role: "user", Content: "hi"}}, nil); err != nil {
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
	_, err := client.chatOpenAIResponses([]Message{{Role: "user", Content: "hi"}}, nil)
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
