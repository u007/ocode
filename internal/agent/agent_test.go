package agent

import (
	"encoding/json"
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

func TestFallbackAllProviderModelsIncludesDeepSeek(t *testing.T) {
	models := fallbackAllProviderModels()
	for _, model := range models {
		if model == "deepseek/deepseek-chat" {
			return
		}
	}
	t.Fatalf("expected DeepSeek model in fallback list, got %#v", models)
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
		config: &config.Config{Ocode: &config.OcodeConfig{Compact: config.CompactConfig{
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
}

func (m *MockTool) Name() string        { return m.name }
func (m *MockTool) Description() string { return "" }
func (m *MockTool) Definition() map[string]interface{} {
	return map[string]interface{}{"name": m.name}
}
func (m *MockTool) Execute(args json.RawMessage) (string, error) {
	return m.result, nil
}
func (m *MockTool) Parallel() bool { return true }
