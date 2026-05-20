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
func (t SkillTool) Parallel() bool      { return true }
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

type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

type QuestionPrompt struct {
	Header   string           `json:"header"`
	Question string           `json:"question"`
	Options  []QuestionOption `json:"options"`
	Multiple bool             `json:"multiple"`
}

type QuestionTool struct{}

func (t QuestionTool) Name() string        { return "question" }
func (t QuestionTool) Description() string { return "Ask the user questions during execution" }
func (t QuestionTool) Parallel() bool      { return false }
func (t QuestionTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "question",
		"description": "Ask the user one or more questions with selectable options. Users can pick from options or type a custom answer. Returns the user's selections.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"questions": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"header": map[string]interface{}{
								"type":        "string",
								"description": "Very short label (max 30 chars) shown as the question header.",
							},
							"question": map[string]interface{}{
								"type":        "string",
								"description": "The full question text shown to the user.",
							},
							"options": map[string]interface{}{
								"type": "array",
								"items": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"label": map[string]interface{}{
											"type":        "string",
											"description": "Display text for the option (1-5 words, concise).",
										},
										"description": map[string]interface{}{
											"type":        "string",
											"description": "Explanation of what selecting this option does.",
										},
									},
									"required": []string{"label", "description"},
								},
								"description": "Available choices. A 'Type your own answer' option is added automatically.",
							},
							"multiple": map[string]interface{}{
								"type":        "boolean",
								"description": "Allow selecting multiple choices (default: false).",
							},
						},
						"required": []string{"question", "header", "options"},
					},
					"description": "One or more questions to ask the user.",
				},
			},
			"required": []string{"questions"},
		},
	}
}

func (t QuestionTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		Questions []QuestionPrompt `json:"questions"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	if len(params.Questions) == 0 {
		return "", fmt.Errorf("at least one question is required")
	}

	var b strings.Builder
	b.WriteString("QUESTION_PROMPT:\n")
	data, _ := json.Marshal(params.Questions)
	b.Write(data)
	b.WriteString("\n\nWAITING_FOR_USER_RESPONSE")

	return b.String(), nil
}
