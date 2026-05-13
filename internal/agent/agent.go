package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jamesmercstudio/ocode/internal/tool"
	"github.com/jamesmercstudio/ocode/internal/config"
	"github.com/jamesmercstudio/ocode/internal/mcp"
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
	// Add subagent tool
	a.tools["agent"] = AgentTool{mainAgent: a}
	return a
}

type AgentTool struct {
	mainAgent *Agent
}

func (t AgentTool) Name() string        { return "agent" }
func (t AgentTool) Description() string { return "Call a subagent to perform a specific task" }
func (t AgentTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "agent",
		"description": "Delegate a task to a specialized subagent",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"prompt": map[string]interface{}{
					"type":        "string",
					"description": "Instructions for the subagent",
				},
			},
			"required": []string{"prompt"},
		},
	}
}

func (t AgentTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		Prompt string `json:"prompt"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	// For simplicity, we just reuse the main agent's client and logic for now
	// In a full implementation, this would spawn a new Agent instance with a scoped history
	resp, err := t.mainAgent.Step([]Message{{Role: "user", Content: params.Prompt}})
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

	// Update tool definitions with global ignore patterns
	if a.config != nil && len(a.config.Watcher.Ignore) > 0 {
		// potential use for 'ignore' var later
	}

	toolDefs := a.GetToolDefinitions()
	// Inject ignore patterns into relevant tools' parameters for the LLM to see (optional, but helps LLM understand constraints)
	// For now, we'll just handle it in Execute.

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

	// Inject global ignore patterns if it's a search tool
	if name == "glob" || name == "grep" || name == "list" {
		var params map[string]interface{}
		json.Unmarshal(args, &params)
		if a.config != nil && len(a.config.Watcher.Ignore) > 0 {
			params["ignore"] = a.config.Watcher.Ignore
			args, _ = json.Marshal(params)
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
	// Load Custom Tools
	custom := tool.LoadCustomTools()
	a.AddTools(custom)

	// Load MCP Tools
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
