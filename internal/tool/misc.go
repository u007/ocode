package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/u007/ocode/internal/skill"
)

const (
	SentinelQuestionPrompt = "QUESTION_PROMPT:"
	SentinelWaitingForUser = "WAITING_FOR_USER_RESPONSE"
	SentinelPermissionAsk  = "PERMISSION_ASK:"
)

// QuestionDismissedResult is the tool result written in place of an unanswered
// `question` prompt when the user dismisses it (web/desktop Cancel, TUI Esc).
// It deliberately carries no sentinel, so the transcript stops reading as a
// pending ask (no dialog reopens on reload/reconcile) while the model still
// learns that the question went unanswered when the next turn runs. The turn
// is NOT resumed on dismissal — the user's next message starts a fresh turn.
const QuestionDismissedResult = "The user dismissed the question prompt without answering."

// ToolCancelledResult is the tool result written for a tool call that the user
// interrupted (Stop / Escape) before it produced a result of its own.
//
// It exists because an assistant tool_call with no matching tool message is an
// ORPHAN: every surface renders it as still in flight (the web ToolBlock shows
// a pulsing "running…" because resultContent is undefined), and
// Agent.recoverOrphanedToolCalls RE-EXECUTES it on the next turn — so a tool
// the user deliberately stopped silently ran again. Answering every call keeps
// the transcript protocol-valid and the round terminal.
//
// Like QuestionDismissedResult it carries no sentinel prefix on purpose: a new
// prefix would have to be threaded through every consumer that pattern-matches
// tool content (web isSentinelToolContent hides PERMISSION_ASK/QUESTION_PROMPT,
// tool.UnansweredAsk keys dialogs off them) and a mismatch in any one of them
// would suppress the block or fake a dialog. Plain prose needs no such
// coordination — any non-empty content already clears "pending".
const ToolCancelledResult = "Cancelled by the user before this tool call finished. It did not complete; do not assume any result."

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
	return t.ExecuteCtx(context.Background(), args)
}

// ExecuteCtx resolves the skill against the session's project root carried
// by WithWorkDir — not the process cwd, which is "/" for the desktop app — and
// returns it with its base directory (skill.ForModel).
func (t SkillTool) ExecuteCtx(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	if strings.Contains(params.Name, "/") || strings.Contains(params.Name, "\\") || strings.Contains(params.Name, "..") {
		return "", fmt.Errorf("invalid skill name %q", params.Name)
	}

	s, err := skill.LoadSkillForRoot(params.Name, workDirFromContext(ctx))
	if err != nil {
		return "", err
	}
	if s == nil {
		return "", fmt.Errorf("skill %s not found", params.Name)
	}

	return s.ForModel(), nil
}

// SkillAliasTool registers "load_skill" as an alias for SkillTool. Models
// (including Claude Code itself, which names its own skill tool "Skill")
// sometimes guess this name instead of "skill" — register it so that guess
// works instead of tripping the unregistered-tool hallucination guard.
type SkillAliasTool struct{ SkillTool }

func (t SkillAliasTool) Name() string { return "load_skill" }
func (t SkillAliasTool) Definition() map[string]interface{} {
	def := t.SkillTool.Definition()
	def["name"] = "load_skill"
	return def
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

// QuestionAnswer is a single selected answer to a QuestionPrompt. It is the
// shared wire type between the agent, the TUI, the web UI, and external clients
// (e.g. the Telegram bot) so every surface submits answers identically.
type QuestionAnswer struct {
	Label  string `json:"label"`
	Text   string `json:"text,omitempty"`
	Custom bool   `json:"custom,omitempty"`
}

// QuestionAnswerSet is the full answer to one QuestionPrompt: every selected
// option (multiple when the prompt allows it). It is the RC bridge wire type so
// multi-question prompts and multi-select questions survive end-to-end, instead
// of collapsing to a single answer per question.
type QuestionAnswerSet struct {
	Header   string           `json:"header,omitempty"`
	Question string           `json:"question"`
	Answers  []QuestionAnswer `json:"answers"`
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
	b.WriteString(SentinelQuestionPrompt + "\n")
	data, _ := json.Marshal(params.Questions)
	b.Write(data)
	b.WriteString("\n\n" + SentinelWaitingForUser)

	return b.String(), nil
}
