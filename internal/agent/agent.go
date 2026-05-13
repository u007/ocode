package agent

import (
	"encoding/json"
	"fmt"

	"github.com/jamesmercstudio/ocode/internal/tool"
)

type Agent struct {
	client LLMClient
	tools  map[string]tool.Tool
}

func NewAgent(client LLMClient, tools []tool.Tool) *Agent {
	toolMap := make(map[string]tool.Tool)
	for _, t := range tools {
		toolMap[t.Name()] = t
	}
	return &Agent{
		client: client,
		tools:  toolMap,
	}
}

func (a *Agent) Step(messages []Message) ([]Message, error) {
	if a.client == nil {
		return []Message{{Role: "assistant", Content: "(no llm client configured)"}}, nil
	}

	toolDefs := a.GetToolDefinitions()
	var newMsgs []Message

	for i := 0; i < 10; i++ { // Limit iterations
		resp, err := a.client.Chat(messages, toolDefs)
		if err != nil {
			return nil, err
		}

		newMsgs = append(newMsgs, *resp)
		messages = append(messages, *resp)

		if len(resp.ToolCalls) == 0 {
			break
		}

		for _, tc := range resp.ToolCalls {
			result, err := a.HandleToolCall(tc.Function.Name, json.RawMessage(tc.Function.Arguments))
			if err != nil {
				result = fmt.Sprintf("Error: %v", err)
			}
			toolMsg := Message{
				Role:    "tool",
				ToolID:  tc.ID,
				Content: result,
			}
			newMsgs = append(newMsgs, toolMsg)
			messages = append(messages, toolMsg)
		}
	}

	return newMsgs, nil
}

func (a *Agent) HandleToolCall(name string, args json.RawMessage) (string, error) {
	t, ok := a.tools[name]
	if !ok {
		return "", fmt.Errorf("tool %s not found", name)
	}
	return t.Execute(args)
}

func (a *Agent) GetToolDefinitions() []map[string]interface{} {
	defs := make([]map[string]interface{}, 0, len(a.tools))
	for _, t := range a.tools {
		defs = append(defs, t.Definition())
	}
	return defs
}
