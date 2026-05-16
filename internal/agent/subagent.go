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
			"Be concise in your output.",
	},
	{
		Name:        "explore",
		Description: "Fast read-only codebase exploration",
		SystemPrompt: "You are an exploration sub-agent. Your goal is to quickly investigate the codebase and return findings. " +
			"Use only read, glob, grep, list, and lsp tools. Do not modify any files. " +
			"Return a concise summary of what you found.",
		Tools: []string{"read", "glob", "grep", "list", "lsp"},
	},
	{
		Name:        "scout",
		Description: "External docs, dependency research",
		SystemPrompt: "You are a scout sub-agent. Research external documentation, APIs, and dependencies. " +
			"Use webfetch and websearch to find relevant information. " +
			"Return a concise summary with key findings and source URLs.",
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
	mainAgent *Agent
}

func (t TaskTool) Name() string        { return "task" }
func (t TaskTool) Description() string { return "Delegate a task to a specialized sub-agent" }
func (t TaskTool) Definition() map[string]interface{} {
	subAgentNames := make([]string, len(DefaultSubAgents))
	subAgentDescs := make([]string, len(DefaultSubAgents))
	for i, sa := range DefaultSubAgents {
		subAgentNames[i] = sa.Name
		subAgentDescs[i] = fmt.Sprintf("%s: %s", sa.Name, sa.Description)
	}

	return map[string]interface{}{
		"name":        "task",
		"description": "Spawn a sub-agent with a specific scope to handle a task autonomously.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"prompt": map[string]interface{}{
					"type":        "string",
					"description": "The specific task or instructions for the sub-agent.",
				},
				"agent": map[string]interface{}{
					"type":        "string",
					"description": "The sub-agent type to use. Options: " + strings.Join(subAgentNames, ", "),
					"enum":        subAgentNames,
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

func (t TaskTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		Prompt  string `json:"prompt"`
		Agent   string `json:"agent"`
		Context string `json:"context"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	if params.Prompt == "" {
		return "", fmt.Errorf("prompt is required")
	}

	spec := FindSubAgentSpec(params.Agent)
	if spec == nil {
		spec = &DefaultSubAgents[0]
	}

	tools := t.getToolsForSubAgent(spec)

	subAgent := NewAgent(t.mainAgent.client, tools, t.mainAgent.config)
	subAgent.mode = t.mainAgent.mode

	systemPrompt := spec.SystemPrompt
	if params.Context != "" {
		systemPrompt += "\nBackground Context: " + params.Context
	}

	subAgentMsgs := []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: params.Prompt},
	}

	resp, err := subAgent.Step(subAgentMsgs)
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

func (t TaskTool) getToolsForSubAgent(spec *SubAgentSpec) []tool.Tool {
	if len(spec.Tools) == 0 {
		return t.mainAgent.GetTools()
	}

	allTools := t.mainAgent.GetTools()
	result := make([]tool.Tool, 0, len(spec.Tools))
	for _, t := range allTools {
		for _, allowed := range spec.Tools {
			if t.Name() == allowed {
				result = append(result, t)
				break
			}
		}
	}
	return result
}
