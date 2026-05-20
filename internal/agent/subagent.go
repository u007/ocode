package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jamesmercstudio/ocode/internal/tool"
)

type SubAgentSpec struct {
	Name         string
	Description  string
	SystemPrompt string
	Tools        []string
}

var DefaultSubAgents = []SubAgentSpec{
	{
		Name:        "general",
		Description: "Multi-step tasks, parallel work",
		SystemPrompt: "You are a general-purpose sub-agent. Complete the task efficiently and return the final result. " +
			"Use a User Expectation Checklist for multi-step work, validate each checklist item with the strongest practical check available, and iterate if validation fails. " +
			"Be concise in your output and include checklist status, validation performed, and remaining gaps.",
	},
	{
		Name:        "explore",
		Description: "Fast read-only codebase exploration",
		SystemPrompt: "You are an exploration sub-agent. Your goal is to quickly investigate the codebase and return findings. " +
			"Use only read, glob, grep, list, and lsp tools. Do not modify any files. " +
			"Return a concise summary of what you found, which user expectations the findings cover, and any remaining unknowns.",
		Tools: []string{"read", "glob", "grep", "list", "lsp"},
	},
	{
		Name:        "scout",
		Description: "External docs, dependency research",
		SystemPrompt: "You are a scout sub-agent. Research external documentation, APIs, and dependencies. " +
			"Use webfetch and websearch to find relevant information. " +
			"Return a concise summary with key findings, source URLs, which user expectations the sources cover, and any remaining unknowns.",
		Tools: []string{"webfetch", "websearch", "read"},
	},
}

func FindSubAgentSpec(name string) *SubAgentSpec {
	for i := range DefaultSubAgents {
		if DefaultSubAgents[i].Name == name {
			return &DefaultSubAgents[i]
		}
	}
	return nil
}

type TaskTool struct {
	mainAgent        *Agent
	registry         *AgentRegistry
	runs             *AgentRunRegistry
	persistChildSess func(sessionID, title string, messages []Message, metadata map[string]any) error
}

func (t TaskTool) Name() string        { return "task" }
func (t TaskTool) Description() string { return "Delegate a task to a specialized sub-agent" }
func (t TaskTool) Parallel() bool      { return true }
func (t TaskTool) Definition() map[string]interface{} {
	subAgents := t.registrySubAgents()
	subAgentNames := make([]string, 0)
	subAgentDescs := make([]string, 0)
	visibleAgentNames := make([]string, 0)
	for _, sa := range subAgents {
		subAgentNames = append(subAgentNames, sa.Name)
		if !sa.Hidden {
			visibleAgentNames = append(visibleAgentNames, sa.Name)
			subAgentDescs = append(subAgentDescs, fmt.Sprintf("%s: %s", sa.Name, sa.Description))
		}
	}

	return map[string]interface{}{
		"name":        "task",
		"description": "Spawn a sub-agent with a specific scope to handle a task autonomously. Available agents: " + strings.Join(subAgentDescs, ", "),
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"prompt": map[string]interface{}{
					"type":        "string",
					"description": "The specific task or instructions for the sub-agent.",
				},
				"agent": map[string]interface{}{
					"type":        "string",
					"description": "The sub-agent type to use. Options: " + strings.Join(visibleAgentNames, ", "),
					"enum":        subAgentNames,
				},
				"context": map[string]interface{}{
					"type":        "string",
					"description": "Additional background context relevant to the task.",
				},
				"run_in_background": map[string]interface{}{
					"type":        "boolean",
					"description": "If true, run the subagent in the background and return immediately with the run ID. Poll with agent_status.",
				},
			},
			"required": []string{"prompt"},
		},
	}
}

func (t TaskTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		Prompt          string `json:"prompt"`
		Agent           string `json:"agent"`
		Context         string `json:"context"`
		RunInBackground bool   `json:"run_in_background"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	if params.Prompt == "" {
		return "", fmt.Errorf("prompt is required")
	}

	spec := t.findAgent(params.Agent)
	if spec == nil {
		if params.Agent != "" && t.registry != nil {
			return "", fmt.Errorf("unknown agent: %s", params.Agent)
		}
		defaultSpec := t.findAgent("general")
		if defaultSpec == nil {
			return "", fmt.Errorf("no agent available")
		}
		spec = defaultSpec
	}

	tools := t.getToolsForDef(spec)

	subAgent := NewAgent(t.mainAgent.client, tools, t.mainAgent.config)
	subAgent.mode = t.mainAgent.mode
	if spec.MaxSteps > 0 {
		subAgent.maxSteps = spec.MaxSteps
	}

	if len(spec.Permissions) > 0 {
		_, pm := buildPermissionManagerFromAgentWithDiags(spec.Permissions)
		subAgent.permissions = pm
	}

	systemPrompt := spec.SystemPrompt
	if params.Context != "" {
		systemPrompt += "\nBackground Context: " + params.Context
	}

	subAgentMsgs := []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: params.Prompt},
	}

	// Background mode
	if params.RunInBackground && t.runs != nil {
		run := t.runs.New(spec.Name)
		run.Procs = subAgent.Procs()
		run.Sub = subAgent
		run.Cancel = subAgent.Cancel

		go func() {
			if t.mainAgent.activity != nil {
				t.mainAgent.activity.agentStarted(spec.Name)
			}
			resp, err := subAgent.Step(subAgentMsgs)
			if t.mainAgent.activity != nil {
				t.mainAgent.activity.agentDone(spec.Name)
			}
			if err != nil {
				run.finishErr(err.Error())
				t.runs.notifyDone(run)
				return
			}
			var b strings.Builder
			for _, m := range resp {
				if m.Role == "assistant" && m.Content != "" {
					b.WriteString(m.Content)
				}
			}
			run.finishOK(b.String())
			t.runs.notifyDone(run)
		}()

		return fmt.Sprintf("Background agent started: %s (agent: %s). Poll with agent_status.", run.ID, spec.Name), nil
	}

	// Synchronous mode
	if t.mainAgent.activity != nil {
		t.mainAgent.activity.agentStarted(spec.Name)
	}
	resp, err := subAgent.Step(subAgentMsgs)
	if t.mainAgent.activity != nil {
		t.mainAgent.activity.agentDone(spec.Name)
	}
	if err != nil {
		return "", err
	}

	sessionID := childSessionID("parent", spec.Name)
	metadata := childSessionMetadata("parent", spec.Name)
	if t.persistChildSess != nil {
		if err := t.persistChildSess(sessionID, fmt.Sprintf("Child: %s", spec.Name), resp, metadata); err != nil {
			emitDebug("SESSION", fmt.Sprintf("failed to persist child session: %v", err))
		}
	}

	var b strings.Builder
	for _, m := range resp {
		if m.Role == "assistant" && m.Content != "" {
			b.WriteString(m.Content)
		}
	}
	result := b.String()
	if sessionID != "" {
		result += fmt.Sprintf("\n\n(Child session: %s)", sessionID)
	}
	return result, nil
}

func (t TaskTool) registrySubAgents() []AgentDefinition {
	if t.registry != nil {
		return t.registry.SubAgents()
	}
	var result []AgentDefinition
	for _, sa := range DefaultSubAgents {
		result = append(result, AgentDefinition{
			Name:         sa.Name,
			Description:  sa.Description,
			SystemPrompt: sa.SystemPrompt,
			Tools:        sa.Tools,
			Mode:         AgentModeSubagent,
			Source:       "builtin",
		})
	}
	return result
}

func (t TaskTool) findAgent(name string) *AgentDefinition {
	if t.registry != nil {
		return t.registry.Get(name)
	}
	spec := FindSubAgentSpec(name)
	if spec != nil {
		return &AgentDefinition{
			Name:         spec.Name,
			Description:  spec.Description,
			SystemPrompt: spec.SystemPrompt,
			Tools:        spec.Tools,
			Mode:         AgentModeSubagent,
			Source:       "builtin",
		}
	}
	return nil
}

func (t TaskTool) getToolsForDef(spec *AgentDefinition) []tool.Tool {
	if len(spec.Tools) == 0 {
		return t.mainAgent.GetTools()
	}
	allTools := t.mainAgent.GetTools()
	result := make([]tool.Tool, 0, len(spec.Tools))
	for _, mainTool := range allTools {
		for _, allowed := range spec.Tools {
			if mainTool.Name() == allowed {
				result = append(result, mainTool)
				break
			}
		}
	}
	return result
}

// AgentStatusTool returns the status of a background agent run.
type AgentStatusTool struct {
	runs *AgentRunRegistry
}

func (t AgentStatusTool) Name() string        { return "agent_status" }
func (t AgentStatusTool) Description() string { return "Check the status of a background agent run" }
func (t AgentStatusTool) Parallel() bool      { return true }
func (t AgentStatusTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "agent_status",
		"description": "Check the status and latest output of a background agent run.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]interface{}{
					"type":        "string",
					"description": "The agent run id to check.",
				},
			},
			"required": []string{"id"},
		},
	}
}

func (t AgentStatusTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}
	if t.runs == nil {
		return "", fmt.Errorf("no agent run registry")
	}
	run, ok := t.runs.Get(params.ID)
	if !ok {
		return fmt.Sprintf("Error: unknown agent run %s", params.ID), nil
	}
	status := run.statusValue()
	var b strings.Builder
	b.WriteString(fmt.Sprintf("[agent run %s status=%s", params.ID, status))
	if status != RunRunning {
		b.WriteString(fmt.Sprintf(" agent=%s", run.Name))
	}
	b.WriteString("]")
	if status == RunRunning {
		lines := run.LastLines(5)
		if len(lines) > 0 {
			b.WriteString("\nLatest output:\n")
			for _, ln := range lines {
				b.WriteString("  " + ln + "\n")
			}
		} else {
			b.WriteString("\n(no output yet)")
		}
	}
	if status == RunDone {
		b.WriteString("\nResult: " + run.Result)
	}
	if status == RunFailed {
		b.WriteString("\nError: " + run.Err)
	}
	return b.String(), nil
}
