package tool

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jamesmercstudio/ocode/internal/skill"
)

type SkillTool struct{}

func (t SkillTool) Name() string        { return "skill" }
func (t SkillTool) Description() string { return "Load a skill definition" }
func (t SkillTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "skill",
		"description": "Load a skill definition from a SKILL.md file",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Name of the skill to load",
				},
			},
			"required": []string{"name"},
		},
	}
}

func (t SkillTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	if strings.Contains(params.Name, "/") || strings.Contains(params.Name, "\\") || strings.Contains(params.Name, "..") {
		return "", fmt.Errorf("invalid skill name %q", params.Name)
	}

	s, err := skill.LoadSkill(params.Name)
	if err != nil {
		return "", err
	}
	if s == nil {
		return "", fmt.Errorf("skill %s not found", params.Name)
	}

	return s.Content, nil
}

type QuestionTool struct{}

func (t QuestionTool) Name() string        { return "question" }
func (t QuestionTool) Description() string { return "Ask the user a question" }
func (t QuestionTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "question",
		"description": "Ask the user a question during execution",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"question": map[string]interface{}{
					"type":        "string",
					"description": "The question to ask",
				},
			},
			"required": []string{"question"},
		},
	}
}

func (t QuestionTool) Execute(args json.RawMessage) (string, error) {
	// In a TUI, this needs to be handled by the update loop to prompt the user
	// For now, return a special message that the agent/TUI can catch
	return "WAITING_FOR_USER_RESPONSE", nil
}
