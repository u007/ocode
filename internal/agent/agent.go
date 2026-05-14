package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jamesmercstudio/ocode/internal/config"
	"github.com/jamesmercstudio/ocode/internal/mcp"
	"github.com/jamesmercstudio/ocode/internal/tool"
)

type Agent struct {
	client LLMClient
	tools  map[string]tool.Tool
	config *config.Config
}

func NewAgent(client LLMClient, tools []tool.Tool, cfg *config.Config) *Agent {
	toolMap := make(map[string]tool.Tool)
	for _, t := range tools {
		toolMap[t.Name()] = t
	}
	a := &Agent{
		client: client,
		tools:  toolMap,
		config: cfg,
	}
	a.tools["agent"] = AgentTool{mainAgent: a}
	return a
}

type AgentTool struct {
	mainAgent *Agent
}

func (t AgentTool) Name() string        { return "agent" }
func (t AgentTool) Description() string { return "Delegate a specific task to a specialized sub-agent" }
func (t AgentTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "agent",
		"description": "Spawn a sub-agent with a specific scope to handle a task autonomously.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"prompt": map[string]interface{}{
					"type":        "string",
					"description": "The specific task or instructions for the sub-agent.",
				},
				"context": map[string]interface{}{
					"type":        "string",
					"description": "Additional background context relevant to the task.",
				},
			},
			"required": []string{"prompt"},
		},
	}
}

func (t AgentTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		Prompt  string `json:"prompt"`
		Context string `json:"context"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	subAgentMsgs := []Message{
		{Role: "system", Content: "You are a specialized sub-agent. Your goal is to complete the task provided by the main agent. " +
			"Be concise and return only the final result or relevant code. " +
			"Background Context: " + params.Context},
		{Role: "user", Content: params.Prompt},
	}

	resp, err := t.mainAgent.Step(subAgentMsgs)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	for _, m := range resp {
		if m.Role == "assistant" && m.Content != "" {
			b.WriteString(m.Content)
		}
	}
	return b.String(), nil
}

func (a *Agent) Step(messages []Message) ([]Message, error) {
	if a.client == nil {
		return []Message{{Role: "assistant", Content: "(no llm client configured)"}}, nil
	}

	messages = a.compactContext(messages)
	toolDefs := a.GetToolDefinitions()
	var newMsgs []Message

	for i := 0; i < 10; i++ {
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

func (a *Agent) compactContext(messages []Message) []Message {
	maxMessages := 20
	if len(messages) <= maxMessages {
		return messages
	}

	keepFront := 2
	keepBack := 8

	compacted := make([]Message, 0)
	compacted = append(compacted, messages[:keepFront]...)

	summaryPrompt := "The following is a part of a long conversation that is being compacted. " +
		"Summarize the key events and outcomes of this segment:\n\n"
	for _, m := range messages[keepFront : len(messages)-keepBack] {
		if m.Role == "user" || m.Role == "assistant" {
			summaryPrompt += fmt.Sprintf("[%s]: %s\n", m.Role, m.Content)
		}
	}

	summaryResp, err := a.client.Chat([]Message{{Role: "user", Content: summaryPrompt}}, nil)
	if err == nil && summaryResp.Content != "" {
		compacted = append(compacted, Message{
			Role:    "system",
			Content: "Previous conversation summary: " + summaryResp.Content,
		})
	} else {
		compacted = append(compacted, Message{
			Role:    "system",
			Content: "...[Conversation history truncated]...",
		})
	}

	compacted = append(compacted, messages[len(messages)-keepBack:]...)
	return compacted
}

func (a *Agent) HandleToolCall(name string, args json.RawMessage) (string, error) {
	t, ok := a.tools[name]
	if !ok {
		return "", fmt.Errorf("tool %s not found", name)
	}

	if name == "glob" || name == "grep" || name == "list" {
		var params map[string]interface{}
		if err := json.Unmarshal(args, &params); err == nil && a.config != nil && len(a.config.Watcher.Ignore) > 0 {
			params["ignore"] = a.config.Watcher.Ignore
			if merged, err := json.Marshal(params); err == nil {
				args = merged
			}
		}
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

func (a *Agent) GetProvider() string {
	return a.client.GetProvider()
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
	custom := tool.LoadCustomTools()
	a.AddTools(custom)

	if cfg != nil {
		for name, mcpCfg := range cfg.MCP {
			if !mcpCfg.Enabled {
				continue
			}
			var client *mcp.MCPClient
			var err error
			if mcpCfg.Type == "local" {
				client, err = mcp.NewLocalClient(name, mcpCfg)
			} else if mcpCfg.Type == "remote" {
				client, err = mcp.NewRemoteClient(name, mcpCfg)
			}

			if err == nil && client != nil {
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
