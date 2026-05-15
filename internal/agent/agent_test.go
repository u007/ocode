package agent

import (
	"encoding/json"
	"testing"

	"github.com/jamesmercstudio/ocode/internal/config"
	"github.com/jamesmercstudio/ocode/internal/tool"
)

type MockClient struct {
	Response *Message
	Err      error
}

func (m *MockClient) Chat(messages []Message, tools []map[string]interface{}) (*Message, error) {
	return m.Response, m.Err
}

func (m *MockClient) GetProvider() string { return "mock" }
func (m *MockClient) GetModel() string    { return "mock-model" }

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
