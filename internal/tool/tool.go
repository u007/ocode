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
	Parallel() bool
}

func LoadBuiltins(cfg *config.Config) []Tool {
	if cfg != nil {
		setExtraAllowedPaths(cfg.Ocode.ExtraAllowedPaths)
	} else {
		setExtraAllowedPaths(nil)
	}
	return []Tool{
		&ReadTool{},
		&WriteTool{Config: cfg},
		&ReplaceLinesToolImpl{Config: cfg},
		&DeleteTool{},
		&GlobTool{},
		&GrepTool{},
		&BashTool{},
		&EditTool{Config: cfg},
		&MultiEditTool{Config: cfg},
		&MultiFileEditTool{Config: cfg},
		&PatchTool{},
		&TodoWriteTool{},
		&TodoReadTool{},
		&SkillTool{},
		&QuestionTool{},
		&WebFetchTool{},
		&WebSearchTool{},
		&RepoCloneTool{},
		&RepoOverviewTool{},
		&PlanEnterTool{},
		&PlanExitTool{},
		&ListTool{},
		&LSPTool{},
		&FormatTool{Config: cfg},
		&GitHubPRTool{},
		&GitHubIssueTool{},
		&GitHubWorkflowTool{},
	}
}
