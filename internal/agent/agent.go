package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jamesmercstudio/ocode/internal/config"
	"github.com/jamesmercstudio/ocode/internal/hooks"
	"github.com/jamesmercstudio/ocode/internal/mcp"
	"github.com/jamesmercstudio/ocode/internal/tool"
)

type Agent struct {
	client      LLMClient
	tools       map[string]tool.Tool
	mcpTools    map[string]struct{}
	mcpErrors   []string
	config      *config.Config
	mode        Mode
	spec        *AgentSpec
	permissions *PermissionManager
}

func NewAgent(client LLMClient, tools []tool.Tool, cfg *config.Config) *Agent {
	toolMap := make(map[string]tool.Tool)
	for _, t := range tools {
		toolMap[t.Name()] = t
	}
	a := &Agent{
		client:      client,
		tools:       toolMap,
		mcpTools:    make(map[string]struct{}),
		config:      cfg,
		mode:        ModeBuild,
		permissions: NewPermissionManager(),
	}
	a.tools["agent"] = AgentTool{mainAgent: a}
	a.tools["task"] = TaskTool{mainAgent: a}
	if cfg != nil {
		a.permissions.LoadFromConfig(cfg.Permission)
	}
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

	if prompt := a.Mode().SystemPrompt(); prompt != "" {
		hasMode := false
		for _, m := range messages {
			if m.Role == "system" && strings.HasPrefix(m.Content, "You are in ") {
				hasMode = true
				break
			}
		}
		if !hasMode {
			messages = append([]Message{{Role: "system", Content: prompt}}, messages...)
		}
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
	if a.config == nil || a.config.Ocode == nil || !a.config.Ocode.Compact.Enabled {
		return messages
	}

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

	summaryClient := a.compactSummaryClient()
	summaryResp, err := summaryClient.Chat([]Message{{Role: "user", Content: summaryPrompt}}, nil)
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

func (a *Agent) compactSummaryClient() LLMClient {
	if a.config == nil || a.config.Ocode == nil {
		return a.client
	}

	compact := a.config.Ocode.Compact
	if compact.SummaryProvider == "" && compact.SummaryModel == "" {
		return a.client
	}

	provider := compact.SummaryProvider
	if provider == "" {
		provider = a.client.GetProvider()
	}

	model := compact.SummaryModel
	if model == "" {
		model = a.client.GetModel()
	}
	if model == "" {
		return a.client
	}

	targetModel := model
	if provider != "" {
		targetModel = provider + ":" + model
	}

	if client := NewClient(a.config, targetModel); client != nil {
		return client
	}
	return a.client
}

func (a *Agent) Mode() Mode {
	if a.mode == "" {
		return ModeBuild
	}
	return a.mode
}

func (a *Agent) SetMode(m Mode) {
	if !m.Valid() {
		return
	}
	a.mode = m
}

func (a *Agent) HandleToolCall(name string, args json.RawMessage) (string, error) {
	if deny, ok := gateToolCall(a.Mode(), name, args); !ok {
		return deny, nil
	}

	if a.permissions != nil {
		level := a.permissions.Check(name)
		if level == PermissionDeny {
			return fmt.Sprintf("denied: tool %q is not permitted by permission rules", name), nil
		}
		if level == PermissionAsk {
			return fmt.Sprintf("PERMISSION_ASK:%s", name), nil
		}
	}

	if !a.isToolAllowed(name) {
		return fmt.Sprintf("denied: tool %q is not allowed for this agent", name), nil
	}

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

	var hooksCfg map[string]config.HookConfig
	if a.config != nil {
		hooksCfg = a.config.Hooks
	}

	argsStr := string(args)
	if err := hooks.RunPreHook(name, argsStr, hooksCfg); err != nil {
		return fmt.Sprintf("pre-hook blocked: %v", err), nil
	}

	result, err := t.Execute(args)

	if hooksCfg != nil {
		resultStr := ""
		if err == nil {
			resultStr = result
		}
		_ = hooks.RunPostHook(name, argsStr, resultStr, hooksCfg)
	}

	return result, err
}

func (a *Agent) GetToolDefinitions() []map[string]interface{} {
	defs := make([]map[string]interface{}, 0, len(a.tools))
	for _, t := range a.tools {
		if !a.isToolAllowed(t.Name()) {
			continue
		}
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
		if !a.isToolAllowed(t.Name()) {
			continue
		}
		tools = append(tools, t)
	}
	return tools
}

func (a *Agent) isToolAllowed(name string) bool {
	if a.spec != nil && len(a.spec.Tools) > 0 {
		found := false
		for _, allowed := range a.spec.Tools {
			if allowed == name {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if a.spec != nil {
		for _, denied := range a.spec.DeniedTools {
			if denied == name {
				return false
			}
		}
	}
	if a.permissions != nil {
		level := a.permissions.Check(name)
		if level == PermissionDeny {
			return false
		}
	}
	return true
}

func (a *Agent) SetSpec(spec *AgentSpec) {
	a.spec = spec
	if spec != nil && spec.Mode.Valid() {
		a.mode = spec.Mode
	}
}

func (a *Agent) Spec() *AgentSpec {
	return a.spec
}

func (a *Agent) Permissions() *PermissionManager {
	return a.permissions
}

func (a *Agent) AddTools(tools []tool.Tool) {
	for _, t := range tools {
		a.tools[t.Name()] = t
	}
}

func (a *Agent) addMCPTools(tools []tool.Tool) {
	for _, t := range tools {
		a.tools[t.Name()] = t
		a.mcpTools[t.Name()] = struct{}{}
	}
}

func (a *Agent) MCPToolNames() []string {
	names := make([]string, 0, len(a.mcpTools))
	for name := range a.mcpTools {
		names = append(names, name)
	}
	return names
}

func (a *Agent) RestoreMCPToolNames(names []string) {
	for _, name := range names {
		a.mcpTools[name] = struct{}{}
	}
}

func (a *Agent) MCPToolCount() int {
	return len(a.mcpTools)
}

func (a *Agent) MCPErrors() []string {
	return a.mcpErrors
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

			if err != nil {
				a.mcpErrors = append(a.mcpErrors, fmt.Sprintf("%s: %v", name, err))
				continue
			}

			if client != nil {
				mcpTools, err := client.ListTools()
				if err != nil {
					a.mcpErrors = append(a.mcpErrors, fmt.Sprintf("%s: %v", name, err))
					continue
				}
				var tools []tool.Tool
				for _, mt := range mcpTools {
					tools = append(tools, mt)
				}
				a.addMCPTools(tools)
			}
		}
	}
}
