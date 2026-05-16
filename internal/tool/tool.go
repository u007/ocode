package tool

import (
	"encoding/json"

	"github.com/jamesmercstudio/ocode/internal/config"
)

type Tool interface {
	Name() string
	Description() string
	Definition() map[string]interface{}
	Execute(args json.RawMessage) (string, error)
}

func LoadBuiltins(cfg *config.Config) []Tool {
	return []Tool{
		&ReadTool{},
		&WriteTool{Config: cfg},
		&DeleteTool{},
		&GlobTool{},
		&GrepTool{},
		&BashTool{},
		&EditTool{Config: cfg},
		&MultiEditTool{},
		&PatchTool{},
		&TodoWriteTool{},
		&SkillTool{},
		&QuestionTool{},
		&WebFetchTool{},
		&WebSearchTool{},
		&ListTool{},
		&LSPTool{},
		&FormatTool{Config: cfg},
		&GitHubPRTool{},
		&GitHubIssueTool{},
		&GitHubWorkflowTool{},
	}
}
