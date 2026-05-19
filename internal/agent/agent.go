package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/jamesmercstudio/ocode/internal/config"
	"github.com/jamesmercstudio/ocode/internal/hooks"
	"github.com/jamesmercstudio/ocode/internal/mcp"
	"github.com/jamesmercstudio/ocode/internal/tool"
)

var DebugAppend func(kind, msg string)

func emitDebug(kind, msg string) {
	if DebugAppend != nil {
		DebugAppend(kind, msg)
	}
}

type Agent struct {
	client      LLMClient
	tools       map[string]tool.Tool
	mcpTools    map[string]struct{}
	mcpErrors   []string
	config      *config.Config
	mode        Mode
	spec        *AgentSpec
	permissions *PermissionManager
	activity    *ActivityTracker
	// OnMessage, if set, is invoked for each message produced during Step
	// (assistant replies and tool results) as soon as they are generated,
	// enabling live UI updates between iterations of the tool-call loop.
	OnMessage func(Message)
	// maxSteps limits the number of agentic iterations. 0 = unlimited.
	maxSteps int
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
		activity:    newActivityTracker(),
	}
	a.tools["agent"] = AgentTool{mainAgent: a}
	a.tools["task"] = TaskTool{mainAgent: a, registry: DefaultAgentRegistry}
	if cfg != nil {
		a.permissions.LoadFromConfig(cfg.Permission)
		if cfg.Ocode != nil {
			a.permissions.LoadFromOcode(cfg.Ocode.Permissions)
		}
	}
	// Set workDir for path-scoped permission checks
	if wd, err := os.Getwd(); err == nil {
		a.permissions.SetWorkDir(wd)
	}
	return a
}

func (a *Agent) SetChildSessionPersistence(persist func(sessionID, title string, messages []Message, metadata map[string]any) error) {
	if taskTool, ok := a.tools["task"].(TaskTool); ok {
		taskTool.persistChildSess = persist
		a.tools["task"] = taskTool
	}
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

func (t AgentTool) Parallel() bool { return true }

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
			"Use a User Expectation Checklist for multi-step work, validate each checklist item with the strongest practical check available, and iterate if validation fails. " +
			"Be concise and return only the final result, relevant code, checklist status, validation performed, and remaining gaps. " +
			"Background Context: " + params.Context},
		{Role: "user", Content: params.Prompt},
	}

	t.mainAgent.activity.agentStarted("agent")
	resp, err := t.mainAgent.Step(subAgentMsgs)
	t.mainAgent.activity.agentDone("agent")
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
		hasSystem := len(messages) > 0 && messages[0].Role == "system"
		if !hasSystem {
			messages = append([]Message{{Role: "system", Content: prompt}}, messages...)
		}
	}
	messages = a.compactContext(messages)
	toolDefs := a.GetToolDefinitions()
	var newMsgs []Message

	for i := 0; ; i++ {
		limit := a.maxSteps
		if limit <= 0 {
			limit = 100
		}
		if i >= limit {
			summarizeMsg := Message{
				Role:    "system",
				Content: "You have reached the maximum number of steps (" + strconv.Itoa(limit) + "). Stop using tools and respond with a summary of your work and any remaining tasks.",
			}
			newMsgs = append(newMsgs, summarizeMsg)
			messages = append(messages, summarizeMsg)
			resp, err := a.client.Chat(messages, toolDefs)
			if err != nil {
				return nil, err
			}
			newMsgs = append(newMsgs, *resp)
			if a.OnMessage != nil {
				a.OnMessage(*resp)
			}
			return newMsgs, nil
		}
		emitDebug("LLM", fmt.Sprintf("→ %s/%s [%d msgs]", a.client.GetProvider(), a.client.GetModel(), len(messages)))
		a.activity.setLLMRunning(true)
		resp, err := a.client.Chat(messages, toolDefs)
		a.activity.setLLMRunning(false)
		if err != nil {
			emitDebug("ERROR", fmt.Sprintf("LLM error: %v", err))
			return nil, err
		}
		if resp.Usage != nil {
			in, out := int64(0), int64(0)
			if resp.Usage.PromptTokens != nil {
				in = *resp.Usage.PromptTokens
			}
			if resp.Usage.CompletionTokens != nil {
				out = *resp.Usage.CompletionTokens
			}
			emitDebug("LLM", fmt.Sprintf("← tokens in=%d out=%d", in, out))
		}

		newMsgs = append(newMsgs, *resp)
		messages = append(messages, *resp)
		if a.OnMessage != nil {
			a.OnMessage(*resp)
		}

		if len(resp.ToolCalls) == 0 {
			break
		}

		type tcResult struct {
			idx int
			msg Message
		}

		var parallelTCs, sequentialTCs []int
		for j, tc := range resp.ToolCalls {
			t, ok := a.tools[tc.Function.Name]
			if ok && t.Parallel() {
				parallelTCs = append(parallelTCs, j)
			} else {
				sequentialTCs = append(sequentialTCs, j)
			}
		}

		results := make([]Message, len(resp.ToolCalls))

		if len(parallelTCs) > 0 {
			var wg sync.WaitGroup
			for _, i := range parallelTCs {
				wg.Add(1)
				go func(idx int, tc ToolCall) {
					defer wg.Done()
					a.activity.toolStarted(tc.Function.Name)
					result, err := a.HandleToolCall(tc.Function.Name, json.RawMessage(tc.Function.Arguments))
					a.activity.toolDone(tc.Function.Name)
					if err != nil {
						result = fmt.Sprintf("Error: %v", err)
					}
					result = TruncateToolResult(tc.ID, result)
					results[idx] = Message{Role: "tool", ToolID: tc.ID, Content: result}
				}(i, resp.ToolCalls[i])
			}
			wg.Wait()
		}

		for _, i := range sequentialTCs {
			tc := resp.ToolCalls[i]
			a.activity.toolStarted(tc.Function.Name)
			result, err := a.HandleToolCall(tc.Function.Name, json.RawMessage(tc.Function.Arguments))
			a.activity.toolDone(tc.Function.Name)
			if err != nil {
				result = fmt.Sprintf("Error: %v", err)
			}
			result = TruncateToolResult(tc.ID, result)
			results[i] = Message{Role: "tool", ToolID: tc.ID, Content: result}
		}

		for _, toolMsg := range results {
			newMsgs = append(newMsgs, toolMsg)
			messages = append(messages, toolMsg)
			if a.OnMessage != nil {
				a.OnMessage(toolMsg)
			}
			if toolMsg.Content == "WAITING_FOR_USER_RESPONSE" || strings.HasPrefix(toolMsg.Content, "PERMISSION_ASK:") {
				return newMsgs, nil
			}
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
	var summaryText string
	if err == nil && summaryResp.Content != "" {
		summaryText = "\n\nPrevious conversation summary: " + summaryResp.Content
	} else {
		summaryText = "\n\n...[Conversation history truncated]..."
	}
	if len(compacted) > 0 && compacted[0].Role == "system" {
		compacted[0].Content += summaryText
	} else {
		compacted = append(compacted, Message{Role: "system", Content: summaryText})
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
		decision := a.permissions.Decide(name, args)
		if decision.Level == PermissionDeny {
			return fmt.Sprintf("denied: tool %q is not permitted by permission rules", name), nil
		}
		if decision.Level == PermissionAsk {
			payload, err := json.Marshal(decision.Request)
			if err != nil {
				return "", fmt.Errorf("marshal permission request: %w", err)
			}
			return "PERMISSION_ASK:" + string(payload), nil
		}
	}

	return a.executeToolCall(name, args)
}

func (a *Agent) HandleApprovedToolCall(name string, args json.RawMessage) (string, error) {
	return a.executeToolCall(name, args)
}

func (a *Agent) executeToolCall(name string, args json.RawMessage) (string, error) {
	emitDebug("TOOL", fmt.Sprintf("→ %s %s", name, truncateDebugArgs(args, 120)))
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

	if err != nil {
		emitDebug("ERROR", fmt.Sprintf("tool %s: %v", name, err))
	} else {
		emitDebug("TOOL", fmt.Sprintf("← %s (ok)", name))
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
	if spec != nil && spec.MaxSteps > 0 {
		a.maxSteps = spec.MaxSteps
	}
}

func (a *Agent) Spec() *AgentSpec {
	return a.spec
}

func (a *Agent) Activity() *ActivityTracker {
	return a.activity
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

func truncateDebugArgs(args json.RawMessage, max int) string {
	s := string(args)
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
