package agent

import (
	"sort"

	"github.com/jamesmercstudio/ocode/internal/config"
)

var DefaultAgentRegistry *AgentRegistry

func init() {
	DefaultAgentRegistry = NewAgentRegistry()
	DefaultAgentRegistry.LoadMarkdownAgents()
}

type AgentMode string

const (
	AgentModePrimary  AgentMode = "primary"
	AgentModeSubagent AgentMode = "subagent"
	AgentModeAll      AgentMode = "all"
)

type AgentDefinition struct {
	Name         string
	Description  string
	SystemPrompt string
	Tools        []string
	DeniedTools  []string
	Mode         AgentMode
	Hidden       bool
	Permissions  map[string]interface{}
	Source       string
	MaxSteps     int
	// Model is an optional OpenCode-style override in "provider/model" or
	// "provider:model" form. Empty means inherit the session's current model.
	Model string
}

type LoadDiagnostic struct {
	Level   string
	File    string
	Message string
}

type AgentRegistry struct {
	defs       []AgentDefinition
	diagnostic []LoadDiagnostic
}

func NewAgentRegistry() *AgentRegistry {
	r := &AgentRegistry{}
	r.registerBuiltins()
	return r
}

func (r *AgentRegistry) registerBuiltins() {
	r.defs = []AgentDefinition{
		{
			Name:        "build",
			Description: "Full development work with all tools enabled",
			Mode:        AgentModePrimary,
			Source:      "builtin",
		},
		{
			Name:        "plan",
			Description: "Analysis and planning without making changes",
			Mode:        AgentModePrimary,
			Source:      "builtin",
		},
		{
			Name:        "review",
			Description: "Code review with read-only access",
			Mode:        AgentModePrimary,
			Source:      "builtin",
		},
		{
			Name:        "debug",
			Description: "Focused investigation with bash and read tools",
			Tools:       []string{"read", "glob", "grep", "list", "lsp", "bash", "webfetch", "websearch", "skill"},
			Mode:        AgentModePrimary,
			Source:      "builtin",
		},
		{
			Name:        "docs",
			Description: "Documentation writing with file operations",
			Tools:       []string{"read", "write", "edit", "glob", "grep", "list", "delete", "webfetch", "websearch", "skill"},
			Mode:        AgentModePrimary,
			Source:      "builtin",
		},
		{
			Name:         "general",
			Description:  "Multi-step tasks, parallel work",
				SystemPrompt: generalSubAgentPrompt,
			Mode:         AgentModeSubagent,
			Source:       "builtin",
		},
		{
			Name:         "explore",
			Description:  "Fast read-only codebase exploration",
				SystemPrompt: exploreSubAgentPrompt,
				Tools:        []string{"read", "glob", "grep", "list", "lsp", "bash", "webfetch", "websearch"},
			Mode:         AgentModeSubagent,
			Source:       "builtin",
		},
		{
			Name:         "scout",
			Description:  "External docs, dependency research",
				SystemPrompt: scoutSubAgentPrompt,
				Tools:        []string{"repo_clone", "repo_overview", "glob", "grep", "list", "read", "webfetch", "websearch"},
			Mode:         AgentModeSubagent,
			Source:       "builtin",
		},
	}
}

func (r *AgentRegistry) Get(name string) *AgentDefinition {
	for i := range r.defs {
		if r.defs[i].Name == name {
			return &r.defs[i]
		}
	}
	return nil
}

func (r *AgentRegistry) SubAgents() []AgentDefinition {
	var result []AgentDefinition
	for _, d := range r.defs {
		if d.Mode == AgentModeSubagent || d.Mode == AgentModeAll {
			result = append(result, d)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func (r *AgentRegistry) PrimaryAgents() []AgentDefinition {
	var result []AgentDefinition
	for _, d := range r.defs {
		if d.Mode == AgentModePrimary || d.Mode == AgentModeAll {
			result = append(result, d)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func (r *AgentRegistry) All() []AgentDefinition {
	result := make([]AgentDefinition, len(r.defs))
	copy(result, r.defs)
	return result
}

func (r *AgentRegistry) Diagnostics() []LoadDiagnostic {
	return r.diagnostic
}

func (r *AgentRegistry) addLoaded(def AgentDefinition) {
	for i := range r.defs {
		if r.defs[i].Name == def.Name {
			r.defs[i] = def
			return
		}
	}
	r.defs = append(r.defs, def)
}

func ApplyAgentConfig(cfg *config.Config) {
	if cfg == nil || cfg.Agent == nil {
		return
	}
	for name, raw := range cfg.Agent {
		agentCfg, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		steps, ok := extractSteps(agentCfg)
		if !ok {
			continue
		}
		for i := range DefaultAgents {
			if DefaultAgents[i].Name == name {
				DefaultAgents[i].MaxSteps = steps
			}
		}
		def := DefaultAgentRegistry.Get(name)
		if def != nil {
			def.MaxSteps = steps
		}
	}
}

func extractSteps(cfg map[string]interface{}) (int, bool) {
	if v, ok := cfg["steps"]; ok {
		switch n := v.(type) {
		case float64:
			if int(n) > 0 {
				return int(n), true
			}
		case int:
			if n > 0 {
				return n, true
			}
		}
	}
	if v, ok := cfg["maxSteps"]; ok {
		switch n := v.(type) {
		case float64:
			if int(n) > 0 {
				return int(n), true
			}
		case int:
			if n > 0 {
				return n, true
			}
		}
	}
	return 0, false
}
