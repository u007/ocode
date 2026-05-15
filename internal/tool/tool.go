package tool

import (
	"encoding/json"
)

type Tool interface {
	Name() string
	Description() string
	Definition() map[string]interface{}
	Execute(args json.RawMessage) (string, error)
}

func LoadBuiltins() []Tool {
	return []Tool{
		&ReadTool{},
		&WriteTool{},
		&DeleteTool{},
		&GlobTool{},
		&GrepTool{},
		&BashTool{},
		&EditTool{},
		&MultiEditTool{},
		&PatchTool{},
		&TodoWriteTool{},
		&SkillTool{},
		&QuestionTool{},
		&WebFetchTool{},
		&WebSearchTool{},
		&ListTool{},
		&LSPTool{},
	}
}
