package agent

import (
	"encoding/json"
	"fmt"

	"github.com/jamesmercstudio/ocode/internal/tool"
	"github.com/jamesmercstudio/ocode/internal/config"
	"github.com/jamesmercstudio/ocode/internal/mcp"
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
			if result == "WAITING_FOR_USER_RESPONSE" {
				// We must provide a tool result message even if we stop,
				// but since we want to wait for user, we'll return now.
				// On the next turn, the TUI will have appended the user's answer as a tool result.
				// Wait, the TUI currently just appends a 'user' message.
				// We need to fix the protocol.
				return newMsgs, nil
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

func (a *Agent) GetTools() []tool.Tool {
	tools := make([]tool.Tool, 0, len(a.tools))
	for _, t := range a.tools {
		tools = append(tools, t)
	}
	return tools
}

func (a *Agent) AddTools(tools []tool.Tool) {
	for _, t := range tools {
		a.tools[t.Name()] = t
	}
}

func (a *Agent) LoadExternalTools(cfg *config.Config) {
	// Load Custom Tools
	custom := tool.LoadCustomTools()
	a.AddTools(custom)

	// Load MCP Tools
	if cfg != nil {
		for name, mcpCfg := range cfg.MCP {
			if mcpCfg.Enabled && mcpCfg.Type == "local" {
				client, err := mcp.NewLocalClient(name, mcpCfg)
				if err == nil {
					mcpTools, err := client.ListTools()
					if err == nil {
						var tools []tool.Tool
						for _, mt := range mcpTools {
							tools = append(tools, mt)
						}
						a.AddTools(tools)
					}
				}
			}
		}
	}
}
